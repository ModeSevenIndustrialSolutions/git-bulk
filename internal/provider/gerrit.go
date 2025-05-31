// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package provider implements Git hosting provider interfaces for GitHub, GitLab, and Gerrit.
package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// GerritProvider implements the Provider interface for Gerrit
type GerritProvider struct {
	client   *http.Client
	limiter  *rate.Limiter
	baseURL  string
	username string
	password string
}

// GerritProject represents a Gerrit project
type GerritProject struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Parent      string                 `json:"parent,omitempty"`
	Description string                 `json:"description,omitempty"`
	State       string                 `json:"state,omitempty"`
	Branches    map[string]string      `json:"branches,omitempty"`
	WebLinks    []GerritWebLink        `json:"web_links,omitempty"`
	CloneLinks  map[string]GerritClone `json:"clone_links,omitempty"`
}

// GerritWebLink represents a web link for a Gerrit project
type GerritWebLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// GerritClone represents clone information for a Gerrit project
type GerritClone struct {
	URL string `json:"url"`
}

// GerritAccount represents a Gerrit account
type GerritAccount struct {
	ID       int    `json:"_account_id"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

// NewGerritProvider creates a new Gerrit provider
func NewGerritProvider(baseURL, username, password string) (*GerritProvider, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("gerrit base URL is required")
	}

	// Ensure the base URL ends with /
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}

	// Create HTTP client with authentication
	client := &http.Client{
		Timeout: time.Second * 30,
	}

	// Create rate limiter: Conservative rate limit for Gerrit
	limiter := rate.NewLimiter(rate.Every(200*time.Millisecond), 5)

	return &GerritProvider{
		client:   client,
		limiter:  limiter,
		baseURL:  baseURL,
		username: username,
		password: password,
	}, nil
}

// Name returns the provider name
func (g *GerritProvider) Name() string {
	return "gerrit"
}

// ParseSource parses a Gerrit source URL or identifier
func (g *GerritProvider) ParseSource(source string) (*SourceInfo, error) {
	source = strings.TrimSpace(source)

	// Handle Gerrit URLs
	if strings.Contains(source, "://") {
		return g.parseGerritURL(source)
	}

	// Handle SSH URLs
	if strings.Contains(source, "@") && strings.Contains(source, ":") {
		return g.parseGerritSSH(source)
	}

	// Handle hostname only - treat as Gerrit server
	if !strings.Contains(source, "/") {
		return &SourceInfo{
			Provider:     "gerrit",
			Host:         source,
			Organization: "", // Will list all projects
		}, nil
	}

	return nil, fmt.Errorf("unable to parse Gerrit source: %s", source)
}

func (g *GerritProvider) parseGerritURL(source string) (*SourceInfo, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid Gerrit URL: %w", err)
	}

	info := &SourceInfo{
		Provider: "gerrit",
		Host:     u.Host,
	}

	// Extract organization/path from URL path
	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(pathParts) > 0 && pathParts[0] != "" {
		info.Organization = pathParts[0]
		if len(pathParts) > 1 {
			info.Path = strings.Join(pathParts[1:], "/")
		}
	}

	return info, nil
}

func (g *GerritProvider) parseGerritSSH(source string) (*SourceInfo, error) {
	// Parse SSH URLs like username@gerrit.example.com:project/name
	parts := strings.Split(source, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid Gerrit SSH URL: %s", source)
	}

	hostAndPath := parts[1]
	colonIndex := strings.Index(hostAndPath, ":")
	if colonIndex == -1 {
		return nil, fmt.Errorf("invalid Gerrit SSH URL format: %s", source)
	}

	host := hostAndPath[:colonIndex]
	path := hostAndPath[colonIndex+1:]

	info := &SourceInfo{
		Provider: "gerrit",
		Host:     host,
		IsSSH:    true,
	}

	// For Gerrit, the path after : could be the full project path
	if path != "" {
		pathParts := strings.Split(path, "/")
		info.Organization = pathParts[0]
		if len(pathParts) > 1 {
			info.Path = strings.Join(pathParts[1:], "/")
		}
	}

	return info, nil
}

// GetOrganization retrieves Gerrit "organization" information (really just server info)
func (g *GerritProvider) GetOrganization(ctx context.Context, orgName string) (*Organization, error) {
	// For Gerrit, we'll return server information as the "organization"
	// since Gerrit doesn't have a traditional organization concept

	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	// Get server info
	resp, err := g.makeRequest(ctx, "GET", "config/server/info", nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Log error but don't return it since this is in defer
			_ = err // Explicitly ignore error to satisfy revive
		}
	}()

	var serverInfo map[string]interface{}
	if err := g.parseResponse(resp, &serverInfo); err != nil {
		return nil, err
	}

	// Use orgName as the identifier, or use the server host
	name := orgName
	if name == "" {
		u, _ := url.Parse(g.baseURL)
		name = u.Host
	}

	org := &Organization{
		ID:          name,
		Name:        name,
		DisplayName: name,
		Description: "Gerrit Code Review Server",
		URL:         g.baseURL,
		Type:        "server",
		Metadata:    make(map[string]string),
	}

	// Add server info to metadata
	for key, value := range serverInfo {
		if str, ok := value.(string); ok {
			org.Metadata[key] = str
		}
	}

	return org, nil
}

// ListRepositories lists all projects in a Gerrit server
func (g *GerritProvider) ListRepositories(ctx context.Context, orgName string) ([]*Repository, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	// List all projects
	resp, err := g.makeRequest(ctx, "GET", "projects/?d", nil) // d=description
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Log error but don't return it since this is in defer
			_ = err // Explicitly ignore error to satisfy revive
		}
	}()

	var projects map[string]GerritProject
	if err := g.parseResponse(resp, &projects); err != nil {
		return nil, err
	}

	var repositories []*Repository
	for projectName, project := range projects {
		// Filter by organization/prefix if specified
		if orgName != "" && !strings.HasPrefix(projectName, orgName) {
			continue
		}

		repo := g.convertProject(projectName, &project)
		repositories = append(repositories, repo)
	}

	return repositories, nil
}

func (g *GerritProvider) convertProject(projectName string, project *GerritProject) *Repository {
	repo := &Repository{
		ID:          project.ID,
		Name:        extractProjectName(projectName),
		FullName:    projectName,
		Description: project.Description,
		Private:     true,  // Gerrit projects are typically private
		Fork:        false, // Gerrit doesn't have traditional forks
		Path:        projectName,
		CreatedAt:   time.Now().Format(time.RFC3339), // Gerrit doesn't expose creation time
		UpdatedAt:   time.Now().Format(time.RFC3339),
		Metadata: map[string]string{
			"state":  project.State,
			"parent": project.Parent,
		},
	}

	// Set clone URLs from Gerrit response
	if project.CloneLinks != nil {
		if httpClone, exists := project.CloneLinks["http"]; exists {
			repo.CloneURL = httpClone.URL
		}
		if sshClone, exists := project.CloneLinks["ssh"]; exists {
			repo.SSHCloneURL = sshClone.URL
		}
	}

	// If no clone URLs from API, construct them
	if repo.CloneURL == "" {
		repo.CloneURL = g.constructHTTPCloneURL(projectName)
	}
	if repo.SSHCloneURL == "" {
		repo.SSHCloneURL = g.constructSSHCloneURL(projectName)
	}

	return repo
}

func extractProjectName(fullPath string) string {
	parts := strings.Split(fullPath, "/")
	return parts[len(parts)-1]
}

func (g *GerritProvider) constructHTTPCloneURL(projectName string) string {
	u, _ := url.Parse(g.baseURL)
	return fmt.Sprintf("%s://%s/a/%s", u.Scheme, u.Host, projectName)
}

func (g *GerritProvider) constructSSHCloneURL(projectName string) string {
	u, _ := url.Parse(g.baseURL)
	port := u.Port()
	if port == "" {
		port = "29418" // Default Gerrit SSH port
	}
	return fmt.Sprintf("ssh://%s@%s:%s/%s", g.username, u.Hostname(), port, projectName)
}

// CreateFork creates a new project based on an existing one (Gerrit doesn't have traditional forks)
func (g *GerritProvider) CreateFork(_ context.Context, _ *Repository, _ string) (*Repository, error) {
	return nil, fmt.Errorf("forking not supported for Gerrit repositories")
}

// CreateOrganization creates a new organization (not applicable for Gerrit)
func (g *GerritProvider) CreateOrganization(_ context.Context, _, _, _ string) (*Organization, error) {
	return nil, fmt.Errorf("gerrit does not support creating organizations")
}

// SyncRepository synchronizes a repository (not applicable for Gerrit)
func (g *GerritProvider) SyncRepository(_ context.Context, _ *Repository) error {
	return fmt.Errorf("gerrit does not support repository synchronization")
}

// RepositoryExists checks if a project exists in Gerrit
func (g *GerritProvider) RepositoryExists(ctx context.Context, orgName, repoName string) (bool, error) {
	if err := g.limiter.Wait(ctx); err != nil {
		return false, err
	}

	projectPath := repoName
	if orgName != "" {
		projectPath = fmt.Sprintf("%s/%s", orgName, repoName)
	}

	endpoint := fmt.Sprintf("projects/%s", url.PathEscape(projectPath))
	resp, err := g.makeRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			// Log error but don't return it since this is in defer
			_ = err // Explicitly ignore error to satisfy revive
		}
	}()

	return resp.StatusCode == http.StatusOK, nil
}

// Close performs cleanup operations
func (g *GerritProvider) Close() error {
	// HTTP client doesn't require explicit cleanup
	return nil
}

// makeRequest makes an HTTP request to the Gerrit API
func (g *GerritProvider) makeRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	url := g.baseURL + "a/" + endpoint

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Add authentication if provided
	if g.username != "" && g.password != "" {
		req.SetBasicAuth(g.username, g.password)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}

	// Check for rate limiting
	if resp.StatusCode == http.StatusTooManyRequests {
		if err := resp.Body.Close(); err != nil {
			// Log error but continue with rate limit handling
			_ = err // Explicitly ignore error to satisfy revive
		}
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			if seconds, parseErr := strconv.Atoi(retryAfter); parseErr == nil {
				return nil, &RateLimitError{
					RetryAfter: time.Duration(seconds) * time.Second,
					Message:    fmt.Sprintf("Gerrit rate limit exceeded. Retry after: %v seconds", seconds),
				}
			}
		}
		return nil, &RateLimitError{
			RetryAfter: time.Minute,
			Message:    "Gerrit rate limit exceeded. Using default retry delay",
		}
	}

	return resp, nil
}

// parseResponse parses a Gerrit API response, handling the )]}' prefix
func (g *GerritProvider) parseResponse(resp *http.Response, target interface{}) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Gerrit responses start with )]}' to prevent CSRF attacks
	bodyStr := string(body)

	if !strings.HasPrefix(bodyStr, ")]}'\n") {
		return fmt.Errorf("invalid Gerrit response: missing security prefix")
	}
	bodyStr = bodyStr[5:]

	return json.Unmarshal([]byte(bodyStr), target)
}
