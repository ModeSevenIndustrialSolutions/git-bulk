// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sshauth "github.com/ModeSevenIndustrialSolutions/git-bulk/internal/ssh"
	gitlab "gitlab.com/gitlab-org/api/client-go"
	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
)

// GitLabProvider implements the Provider interface for GitLab
type GitLabProvider struct {
	client    *gitlab.Client
	limiter   *rate.Limiter
	token     string
	baseURL   string
	sshAuth   *sshauth.Authenticator
	enableSSH bool
}

// NewGitLabProvider creates a new GitLab provider
func NewGitLabProvider(token, baseURL string) (*GitLabProvider, error) {
	return NewGitLabProviderWithSSH(token, baseURL, true)
}

// NewGitLabProviderWithSSH creates a new GitLab provider with SSH configuration
func NewGitLabProviderWithSSH(token, baseURL string, enableSSH bool) (*GitLabProvider, error) {
	var client *gitlab.Client
	var err error

	if baseURL != "" {
		client, err = gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	} else {
		client, err = gitlab.NewClient(token)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create GitLab client: %w", err)
	}

	// Create rate limiter: GitLab.com allows 2000 requests per minute for authenticated users
	// This translates to about 33 requests per second, so we'll use 10 requests per second to be safe
	limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 10)

	provider := &GitLabProvider{
		client:    client,
		limiter:   limiter,
		token:     token,
		baseURL:   baseURL,
		enableSSH: enableSSH,
	}

	// Initialize SSH authentication if enabled
	if enableSSH {
		sshConfig := &sshauth.Config{
			Timeout: time.Second * 30,
			Verbose: false,
		}

		provider.sshAuth, err = sshauth.NewAuthenticator(sshConfig)
		if err != nil {
			// SSH authentication initialization failed, but we can still fall back to HTTP
			fmt.Printf("Warning: SSH authentication initialization failed: %v\n", err)
			provider.enableSSH = false
		}
	}

	return provider, nil
}

// Name returns the provider name
func (g *GitLabProvider) Name() string {
	return "gitlab"
}

// ParseSource parses a GitLab source URL or identifier
func (g *GitLabProvider) ParseSource(source string) (*SourceInfo, error) {
	source = strings.TrimSpace(source)

	// Handle various GitLab URL formats
	if strings.Contains(source, "gitlab") {
		return g.parseGitLabURL(source)
	}

	// Handle group name only (must not contain dots, as those indicate hostnames)
	if !strings.Contains(source, "/") && !strings.Contains(source, ".") {
		return &SourceInfo{
			Provider:     "gitlab",
			Host:         "gitlab.com",
			Organization: source,
		}, nil
	}

	// Handle group/subgroup format (must contain slash and not look like a hostname)
	if strings.Contains(source, "/") && !strings.Contains(source, "://") {
		parts := strings.Split(source, "/")
		// Ensure the first part doesn't look like a hostname (contains dots)
		if len(parts) >= 1 && !strings.Contains(parts[0], ".") {
			return &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: parts[0],
				Path:         strings.Join(parts[1:], "/"),
			}, nil
		}
	}

	return nil, fmt.Errorf("unable to parse GitLab source: %s", source)
}

func (g *GitLabProvider) parseGitLabURL(source string) (*SourceInfo, error) {
	// Handle SSH URLs
	if strings.Contains(source, "@") && strings.Contains(source, ":") && !strings.Contains(source, "://") {
		parts := strings.Split(source, "@")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid GitLab SSH URL: %s", source)
		}

		hostAndPath := parts[1]
		colonIndex := strings.Index(hostAndPath, ":")
		if colonIndex == -1 {
			return nil, fmt.Errorf("invalid GitLab SSH URL format: %s", source)
		}

		host := hostAndPath[:colonIndex]
		path := hostAndPath[colonIndex+1:]
		path = strings.TrimSuffix(path, ".git")

		pathParts := strings.Split(path, "/")
		if len(pathParts) < 1 {
			return nil, fmt.Errorf("invalid GitLab SSH URL path: %s", path)
		}

		info := &SourceInfo{
			Provider:     "gitlab",
			Host:         host,
			Organization: pathParts[0],
			IsSSH:        true,
		}

		if len(pathParts) > 1 {
			info.Path = strings.Join(pathParts[1:], "/")
		}

		return info, nil
	}

	// Handle URLs without scheme (e.g., gitlab.com/org or gitlab.com/org/repo)
	if strings.HasPrefix(source, "gitlab.com/") && !strings.Contains(source, "://") {
		path := strings.TrimPrefix(source, "gitlab.com/")
		pathParts := strings.Split(path, "/")
		if len(pathParts) < 1 || pathParts[0] == "" {
			return nil, fmt.Errorf("invalid GitLab URL path: %s", source)
		}

		info := &SourceInfo{
			Provider:     "gitlab",
			Host:         "gitlab.com",
			Organization: pathParts[0],
		}

		if len(pathParts) > 1 {
			info.Path = strings.Join(pathParts[1:], "/")
		}

		return info, nil
	}

	// Handle HTTPS URLs
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid GitLab URL: %w", err)
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) < 1 {
		return nil, fmt.Errorf("invalid GitLab URL path: %s", u.Path)
	}

	info := &SourceInfo{
		Provider:     "gitlab",
		Host:         u.Host,
		Organization: pathParts[0],
	}

	if len(pathParts) > 1 {
		info.Path = strings.Join(pathParts[1:], "/")
	}

	return info, nil
}

// GetOrganization retrieves GitLab group information
func (g *GitLabProvider) GetOrganization(ctx context.Context, groupName string) (*Organization, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	group, resp, err := g.client.Groups.GetGroup(groupName, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// Try as user instead
			return g.getUserAsOrganization(ctx, groupName)
		}
		return nil, g.handleRateLimit(err, resp)
	}

	return &Organization{
		ID:          strconv.Itoa(group.ID),
		Name:        group.Path,
		DisplayName: group.Name,
		Description: group.Description,
		URL:         group.WebURL,
		Type:        "group",
		Metadata: map[string]string{
			"visibility":    string(group.Visibility),
			"full_path":     group.FullPath,
			"full_name":     group.FullName,
			"created_at":    group.CreatedAt.Format(time.RFC3339),
			"project_count": "0", // Use ListGroupProjects to get actual count if needed
		},
	}, nil
}

func (g *GitLabProvider) getUserAsOrganization(_ context.Context, username string) (*Organization, error) {
	// Use ListUsers to find user by username
	users, resp, err := g.client.Users.ListUsers(&gitlab.ListUsersOptions{
		Username: gitlab.Ptr(username),
	})
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user %s not found", username)
	}

	user := users[0]

	return &Organization{
		ID:          strconv.Itoa(user.ID),
		Name:        user.Username,
		DisplayName: user.Name,
		Description: user.Bio,
		URL:         user.WebURL,
		Type:        "user",
		Metadata: map[string]string{
			"state":      user.State,
			"location":   user.Location,
			"website":    user.WebsiteURL,
			"created_at": user.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

// ListRepositories lists all projects in a GitLab group
func (g *GitLabProvider) ListRepositories(ctx context.Context, groupName string) ([]*Repository, error) {
	// First try to get group projects
	repos, err := g.listGroupProjects(ctx, groupName)
	if err == nil {
		return repos, nil
	}

	// If that fails, try to get user projects
	return g.listUserProjects(ctx, groupName)
}

func (g *GitLabProvider) listGroupProjects(ctx context.Context, groupName string) ([]*Repository, error) {
	var allRepos []*Repository

	opts := &gitlab.ListGroupProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
		IncludeSubGroups: gitlab.Ptr(true),
	}

	for {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		projects, resp, err := g.client.Groups.ListGroupProjects(groupName, opts)
		if err != nil {
			return nil, g.handleRateLimit(err, resp)
		}

		for _, project := range projects {
			allRepos = append(allRepos, g.convertProject(project))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (g *GitLabProvider) listUserProjects(ctx context.Context, username string) ([]*Repository, error) {
	var allRepos []*Repository

	// Use ListUsers to find user by username first
	users, resp, err := g.client.Users.ListUsers(&gitlab.ListUsersOptions{
		Username: gitlab.Ptr(username),
	})
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	if len(users) == 0 {
		return nil, fmt.Errorf("user %s not found", username)
	}

	user := users[0]

	opts := &gitlab.ListProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 100,
		},
		Owned: gitlab.Ptr(true),
	}

	for {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		projects, resp, err := g.client.Projects.ListUserProjects(user.ID, opts)
		if err != nil {
			return nil, g.handleRateLimit(err, resp)
		}

		for _, project := range projects {
			allRepos = append(allRepos, g.convertProject(project))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (g *GitLabProvider) convertProject(project *gitlab.Project) *Repository {
	var createdAt, updatedAt string
	if project.CreatedAt != nil {
		createdAt = project.CreatedAt.Format(time.RFC3339)
	}
	if project.LastActivityAt != nil {
		updatedAt = project.LastActivityAt.Format(time.RFC3339)
	}

	repo := &Repository{
		ID:          strconv.Itoa(project.ID),
		Name:        project.Name,
		FullName:    project.PathWithNamespace,
		CloneURL:    project.HTTPURLToRepo,
		SSHCloneURL: project.SSHURLToRepo,
		Description: project.Description,
		Private:     project.Visibility != gitlab.PublicVisibility,
		Fork:        project.ForkedFromProject != nil,
		Size:        0, // GitLab API doesn't provide repository size in project list
		Stars:       project.StarCount,
		Forks:       project.ForksCount,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		Path:        project.PathWithNamespace,
		Metadata: map[string]string{
			"visibility":       string(project.Visibility),
			"default_branch":   project.DefaultBranch,
			"archived":         strconv.FormatBool(project.Archived),
			"issues_enabled":   strconv.FormatBool(project.IssuesAccessLevel != gitlab.DisabledAccessControl),
			"wiki_enabled":     strconv.FormatBool(project.WikiAccessLevel != gitlab.DisabledAccessControl),
			"snippets_enabled": strconv.FormatBool(project.SnippetsAccessLevel != gitlab.DisabledAccessControl),
		},
	}

	// Language information is not available in the basic project list response
	// Would need a separate API call to get language statistics
	repo.Language = "" // Default to empty

	return repo
}

// CreateFork creates a fork of a project in the target group
func (g *GitLabProvider) CreateFork(ctx context.Context, sourceRepo *Repository, targetGroup string) (*Repository, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	projectID, err := strconv.Atoi(sourceRepo.ID)
	if err != nil {
		return nil, fmt.Errorf("invalid project ID: %s", sourceRepo.ID)
	}

	forkOpts := &gitlab.ForkProjectOptions{
		Namespace: &targetGroup,
	}

	fork, resp, err := g.client.Projects.ForkProject(projectID, forkOpts)
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	return g.convertProject(fork), nil
}

// CreateOrganization creates a new GitLab group
func (g *GitLabProvider) CreateOrganization(ctx context.Context, groupName, displayName, description string) (*Organization, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	groupOpts := &gitlab.CreateGroupOptions{
		Name:        &displayName,
		Path:        &groupName,
		Description: &description,
		Visibility:  gitlab.Ptr(gitlab.PrivateVisibility), // Default to private
	}

	group, resp, err := g.client.Groups.CreateGroup(groupOpts)
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	return &Organization{
		ID:          strconv.Itoa(group.ID),
		Name:        group.Path,
		DisplayName: group.Name,
		Description: group.Description,
		URL:         group.WebURL,
		Type:        "group",
		Metadata: map[string]string{
			"visibility": string(group.Visibility),
			"full_path":  group.FullPath,
			"full_name":  group.FullName,
			"created_at": group.CreatedAt.Format(time.RFC3339),
		},
	}, nil
}

// SyncRepository synchronizes a forked project with its upstream
func (g *GitLabProvider) SyncRepository(ctx context.Context, repo *Repository) error {
	if err := g.limiter.Wait(ctx); err != nil {
		return err
	}

	projectID, err := strconv.Atoi(repo.ID)
	if err != nil {
		return fmt.Errorf("invalid project ID: %s", repo.ID)
	}

	// Get the project details to check if it's a fork
	project, resp, err := g.client.Projects.GetProject(projectID, nil)
	if err != nil {
		return g.handleRateLimit(err, resp)
	}

	if project.ForkedFromProject == nil {
		return fmt.Errorf("project %s is not a fork", repo.FullName)
	}

	// Create a merge request to sync with upstream
	// Note: This is a simplified implementation. In practice, you would need to
	// handle branch synchronization more carefully

	mergeRequestOpts := &gitlab.CreateMergeRequestOptions{
		Title:        gitlab.Ptr("Sync with upstream"),
		Description:  gitlab.Ptr("Automated sync with upstream repository"),
		SourceBranch: gitlab.Ptr("main"), // Use default branch name
		TargetBranch: gitlab.Ptr(project.DefaultBranch),
	}

	_, resp, err = g.client.MergeRequests.CreateMergeRequest(projectID, mergeRequestOpts)
	if err != nil {
		return g.handleRateLimit(err, resp)
	}

	return nil
}

// RepositoryExists checks if a project exists in a GitLab group
func (g *GitLabProvider) RepositoryExists(ctx context.Context, groupName, projectName string) (bool, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return false, err
	}

	projectPath := fmt.Sprintf("%s/%s", groupName, projectName)

	_, resp, err := g.client.Projects.GetProject(projectPath, nil)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, g.handleRateLimit(err, resp)
	}

	return true, nil
}

// Close performs cleanup operations
func (g *GitLabProvider) Close() error {
	// GitLab client doesn't require explicit cleanup
	return nil
}

// handleRateLimit checks for rate limiting and returns appropriate errors
func (g *GitLabProvider) handleRateLimit(err error, resp *gitlab.Response) error {
	if resp == nil {
		return err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
				return &RateLimitError{
					RetryAfter: time.Duration(seconds) * time.Second,
					Message:    fmt.Sprintf("GitLab rate limit exceeded. Retry after: %v seconds", seconds),
				}
			}
		}

		return &RateLimitError{
			RetryAfter: time.Minute,
			Message:    "rate limit exceeded",
		}
	}

	return err
}

// SSHClientConfig returns SSH client configuration for GitLab
func (g *GitLabProvider) SSHClientConfig() (*ssh.ClientConfig, error) {
	if !g.enableSSH || g.sshAuth == nil {
		return nil, fmt.Errorf("SSH authentication not enabled or available")
	}

	authMethods := g.sshAuth.GetAuthMethods()
	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH authentication methods available")
	}

	return &ssh.ClientConfig{
		User:            "git", // GitLab uses 'git' as the SSH user
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Handle host key verification properly
		Timeout:         time.Second * 30,
	}, nil
}

// TestSSHConnection tests SSH connectivity to GitLab
func (g *GitLabProvider) TestSSHConnection() error {
	if !g.enableSSH {
		return fmt.Errorf("SSH not enabled")
	}

	config, err := g.SSHClientConfig()
	if err != nil {
		return err
	}

	// Determine the host from baseURL or use default
	host := "gitlab.com"
	if g.baseURL != "" {
		if u, err := url.Parse(g.baseURL); err == nil && u.Host != "" {
			host = u.Host
		}
	}

	conn, err := ssh.Dial("tcp", host+":22", config)
	if err != nil {
		return fmt.Errorf("SSH connection to GitLab failed: %w", err)
	}
	defer func() {
		_ = conn.Close() // Ignore close error for test connection
	}()

	return nil
}

// SupportsSSH returns whether SSH authentication is available
func (g *GitLabProvider) SupportsSSH() bool {
	return g.enableSSH && g.sshAuth != nil
}
