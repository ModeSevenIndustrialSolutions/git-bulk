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

	"github.com/google/go-github/v53/github"
	"golang.org/x/time/rate"
)

// githubTokenTransport is a custom transport that adds GitHub token authentication
type githubTokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *githubTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "token "+t.token)
	return t.base.RoundTrip(req)
}

// GitHubProvider implements the Provider interface for GitHub
type GitHubProvider struct {
	client  *github.Client
	limiter *rate.Limiter
	token   string
	baseURL string
}

// NewGitHubProvider creates a new GitHub provider
func NewGitHubProvider(token, baseURL string) (*GitHubProvider, error) {
	var client *github.Client

	if token != "" {
		// Create a custom transport that sets the token header in GitHub format
		transport := &githubTokenTransport{
			token: token,
			base:  http.DefaultTransport,
		}
		httpClient := &http.Client{Transport: transport}
		client = github.NewClient(httpClient)
	} else {
		client = github.NewClient(nil)
	}

	// Set custom base URL for GitHub Enterprise
	if baseURL != "" && baseURL != "https://api.github.com/" {
		var err error
		client.BaseURL, err = url.Parse(baseURL)
		if err != nil {
			return nil, fmt.Errorf("invalid GitHub base URL: %w", err)
		}
	}

	// Create rate limiter: GitHub allows 5000 requests per hour for authenticated users
	// This translates to about 1.4 requests per second, so we'll use 1 request per second
	limiter := rate.NewLimiter(rate.Every(time.Second), 5)

	return &GitHubProvider{
		client:  client,
		limiter: limiter,
		token:   token,
		baseURL: baseURL,
	}, nil
}

// Name returns the provider name
func (g *GitHubProvider) Name() string {
	return "github"
}

// ParseSource parses a GitHub source URL or identifier
func (g *GitHubProvider) ParseSource(source string) (*SourceInfo, error) {
	source = strings.TrimSpace(source)

	// Handle various GitHub URL formats
	if strings.Contains(source, "github.com") {
		return g.parseGitHubURL(source)
	}

	// Handle org name only
	if !strings.Contains(source, "/") && !strings.Contains(source, ".") {
		return &SourceInfo{
			Provider:     "github",
			Host:         "github.com",
			Organization: source,
		}, nil
	}

	// Handle org/repo format
	parts := strings.Split(source, "/")
	if len(parts) == 2 {
		return &SourceInfo{
			Provider:     "github",
			Host:         "github.com",
			Organization: parts[0],
			Path:         parts[1],
		}, nil
	}

	return nil, fmt.Errorf("unable to parse GitHub source: %s", source)
}

func (g *GitHubProvider) parseGitHubURL(source string) (*SourceInfo, error) {
	// Handle SSH URLs
	if strings.HasPrefix(source, "git@github.com:") {
		path := strings.TrimPrefix(source, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) < 1 {
			return nil, fmt.Errorf("invalid GitHub SSH URL: %s", source)
		}

		info := &SourceInfo{
			Provider:     "github",
			Host:         "github.com",
			Organization: parts[0],
			IsSSH:        true,
		}

		if len(parts) > 1 {
			info.Path = strings.Join(parts[1:], "/")
		}

		return info, nil
	}

	// Handle HTTPS URLs
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid GitHub URL: %w", err)
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) < 1 {
		return nil, fmt.Errorf("invalid GitHub URL path: %s", u.Path)
	}

	info := &SourceInfo{
		Provider:     "github",
		Host:         u.Host,
		Organization: pathParts[0],
	}

	if len(pathParts) > 1 {
		info.Path = strings.Join(pathParts[1:], "/")
	}

	return info, nil
}

// GetOrganization retrieves GitHub organization information
func (g *GitHubProvider) GetOrganization(ctx context.Context, orgName string) (*Organization, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	org, resp, err := g.client.Organizations.Get(ctx, orgName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			// Try as user instead
			return g.getUserAsOrganization(ctx, orgName)
		}
		return nil, g.handleRateLimit(err, resp)
	}

	return &Organization{
		ID:          strconv.FormatInt(org.GetID(), 10),
		Name:        org.GetLogin(),
		DisplayName: org.GetName(),
		Description: org.GetDescription(),
		URL:         org.GetHTMLURL(),
		Type:        "organization",
		Metadata: map[string]string{
			"company":    org.GetCompany(),
			"location":   org.GetLocation(),
			"blog":       org.GetBlog(),
			"followers":  strconv.Itoa(org.GetFollowers()),
			"following":  strconv.Itoa(org.GetFollowing()),
			"created_at": org.GetCreatedAt().Format(time.RFC3339),
		},
	}, nil
}

func (g *GitHubProvider) getUserAsOrganization(ctx context.Context, username string) (*Organization, error) {
	user, resp, err := g.client.Users.Get(ctx, username)
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	return &Organization{
		ID:          strconv.FormatInt(user.GetID(), 10),
		Name:        user.GetLogin(),
		DisplayName: user.GetName(),
		Description: user.GetBio(),
		URL:         user.GetHTMLURL(),
		Type:        "user",
		Metadata: map[string]string{
			"company":    user.GetCompany(),
			"location":   user.GetLocation(),
			"blog":       user.GetBlog(),
			"followers":  strconv.Itoa(user.GetFollowers()),
			"following":  strconv.Itoa(user.GetFollowing()),
			"created_at": user.GetCreatedAt().Format(time.RFC3339),
		},
	}, nil
}

// ListRepositories lists all repositories in a GitHub organization
func (g *GitHubProvider) ListRepositories(ctx context.Context, orgName string) ([]*Repository, error) {
	var allRepos []*Repository

	opts := &github.RepositoryListByOrgOptions{
		Type: "all",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		repos, resp, err := g.client.Repositories.ListByOrg(ctx, orgName, opts)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				// Try as user repositories
				return g.listUserRepositories(ctx, orgName)
			}
			return nil, g.handleRateLimit(err, resp)
		}

		for _, repo := range repos {
			allRepos = append(allRepos, g.convertRepository(repo))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (g *GitHubProvider) listUserRepositories(ctx context.Context, username string) ([]*Repository, error) {
	var allRepos []*Repository

	opts := &github.RepositoryListOptions{
		Type: "all",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		if err := g.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		repos, resp, err := g.client.Repositories.List(ctx, username, opts)
		if err != nil {
			return nil, g.handleRateLimit(err, resp)
		}

		for _, repo := range repos {
			allRepos = append(allRepos, g.convertRepository(repo))
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

func (g *GitHubProvider) convertRepository(repo *github.Repository) *Repository {
	return &Repository{
		ID:          strconv.FormatInt(repo.GetID(), 10),
		Name:        repo.GetName(),
		FullName:    repo.GetFullName(),
		CloneURL:    repo.GetCloneURL(),
		SSHCloneURL: repo.GetSSHURL(),
		Description: repo.GetDescription(),
		Private:     repo.GetPrivate(),
		Fork:        repo.GetFork(),
		Language:    repo.GetLanguage(),
		Size:        int64(repo.GetSize()),
		Stars:       repo.GetStargazersCount(),
		Forks:       repo.GetForksCount(),
		CreatedAt:   repo.GetCreatedAt().Format(time.RFC3339),
		UpdatedAt:   repo.GetUpdatedAt().Format(time.RFC3339),
		Metadata: map[string]string{
			"default_branch": repo.GetDefaultBranch(),
			"topics":         strings.Join(repo.Topics, ","),
			"archived":       strconv.FormatBool(repo.GetArchived()),
			"disabled":       strconv.FormatBool(repo.GetDisabled()),
		},
	}
}

// CreateFork creates a fork of a repository in the target organization
func (g *GitHubProvider) CreateFork(ctx context.Context, sourceRepo *Repository, targetOrg string) (*Repository, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Parse source repository
	parts := strings.Split(sourceRepo.FullName, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repository full name: %s", sourceRepo.FullName)
	}

	sourceOwner, sourceRepoName := parts[0], parts[1]

	forkOpts := &github.RepositoryCreateForkOptions{
		Organization: targetOrg,
	}

	fork, resp, err := g.client.Repositories.CreateFork(ctx, sourceOwner, sourceRepoName, forkOpts)
	if err != nil {
		return nil, g.handleRateLimit(err, resp)
	}

	return g.convertRepository(fork), nil
}

// CreateOrganization creates a new GitHub organization (not supported by GitHub API)
func (g *GitHubProvider) CreateOrganization(_ context.Context, _, _, _ string) (*Organization, error) {
	return nil, fmt.Errorf("creating organizations is not supported by GitHub API")
}

// SyncRepository synchronizes a forked repository with its upstream
func (g *GitHubProvider) SyncRepository(ctx context.Context, repo *Repository) error {
	if err := g.limiter.Wait(ctx); err != nil {
		return err
	}

	parts := strings.Split(repo.FullName, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository full name: %s", repo.FullName)
	}

	owner, repoName := parts[0], parts[1]

	// Get the upstream repository information
	upstream, resp, err := g.client.Repositories.Get(ctx, owner, repoName)
	if err != nil {
		return g.handleRateLimit(err, resp)
	}

	if upstream.Parent == nil {
		return fmt.Errorf("repository %s is not a fork", repo.FullName)
	}

	// Create a merge request to sync with upstream
	// Note: This is a simplified implementation. In practice, you might want to
	// fetch and merge specific branches or use GitHub's sync fork feature
	mergeReq := &github.RepositoryMergeRequest{
		Base:          github.String(upstream.GetDefaultBranch()),
		Head:          github.String(fmt.Sprintf("%s:%s", upstream.Parent.Owner.GetLogin(), upstream.Parent.GetDefaultBranch())),
		CommitMessage: github.String("Sync with upstream"),
	}

	_, resp, err = g.client.Repositories.Merge(ctx, owner, repoName, mergeReq)
	if err != nil {
		return g.handleRateLimit(err, resp)
	}

	return nil
}

// RepositoryExists checks if a repository exists in a GitHub organization
func (g *GitHubProvider) RepositoryExists(ctx context.Context, orgName, repoName string) (bool, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return false, err
	}

	_, resp, err := g.client.Repositories.Get(ctx, orgName, repoName)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}
		return false, g.handleRateLimit(err, resp)
	}

	return true, nil
}

// Close performs cleanup operations
func (g *GitHubProvider) Close() error {
	// GitHub client doesn't require explicit cleanup
	return nil
}

// handleRateLimit checks for rate limiting and returns appropriate errors
func (g *GitHubProvider) handleRateLimit(err error, resp *github.Response) error {
	if resp == nil {
		return err
	}

	if resp.StatusCode == http.StatusForbidden {
		if rateLimitErr, ok := err.(*github.RateLimitError); ok {
			return &RateLimitError{
				RetryAfter: time.Until(rateLimitErr.Rate.Reset.Time),
				Message:    "API rate limit exceeded",
			}
		}

		if abuseErr, ok := err.(*github.AbuseRateLimitError); ok {
			var retryAfter time.Duration
			if abuseErr.RetryAfter != nil {
				retryAfter = *abuseErr.RetryAfter
			} else {
				retryAfter = time.Minute // Default retry after 1 minute
			}
			return &RateLimitError{
				RetryAfter: retryAfter,
				Message:    "API rate limit exceeded",
			}
		}

		// Check for rate limit errors by message content
		errorStr := err.Error()
		if strings.Contains(errorStr, "rate limit") || 
		   strings.Contains(errorStr, "API rate limit exceeded") ||
		   strings.Contains(errorStr, "too many requests") {
			// Parse retry after from response headers if available
			var retryAfter time.Duration = time.Minute // default
			if resp.Header != nil {
				if resetTime := resp.Header.Get("X-RateLimit-Reset"); resetTime != "" {
					if timestamp, parseErr := strconv.ParseInt(resetTime, 10, 64); parseErr == nil {
						resetAt := time.Unix(timestamp, 0)
						if time.Until(resetAt) > 0 {
							retryAfter = time.Until(resetAt)
						}
					}
				}
			}
			
			return &RateLimitError{
				RetryAfter: retryAfter,
				Message:    "API rate limit exceeded",
			}
		}
	}

	return err
}
