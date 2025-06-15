// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package provider implements Git hosting provider interfaces for GitHub, GitLab, and Gerrit.
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sshauth "github.com/ModeSevenIndustrialSolutions/git-bulk/internal/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/time/rate"
)

const (
	// HTTP content type constants
	contentTypeJSON = "application/json"
	acceptJSON      = "application/json"

	// Error message constants
	errSSHUsernameRequired = "SSH username required for Gerrit"
	errInvalidBaseURL      = "invalid base URL: %w"
	errSSHConnectionFailed = "SSH connection failed: %w"
)

// GerritProvider implements the Provider interface for Gerrit
type GerritProvider struct {
	client    *http.Client
	limiter   *rate.Limiter
	baseURL   string
	username  string
	password  string
	sshAuth   *sshauth.Authenticator
	enableSSH bool
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
	return NewGerritProviderWithSSH(baseURL, username, password, true)
}

// NewGerritProviderWithSSH creates a new Gerrit provider with SSH configuration
func NewGerritProviderWithSSH(baseURL, username, password string, enableSSH bool) (*GerritProvider, error) {
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

	provider := &GerritProvider{
		client:    client,
		limiter:   limiter,
		baseURL:   baseURL,
		username:  username,
		password:  password,
		enableSSH: enableSSH,
	}

	// Initialize SSH authentication if enabled
	if enableSSH {
		sshConfig := &sshauth.Config{
			Timeout: time.Second * 30,
			Verbose: false, // Can be made configurable
		}

		var err error
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

	projects, err := g.getProjects(ctx)
	if err != nil {
		return nil, err
	}

	return g.filterAndConvertProjects(projects, orgName), nil
}

// getProjects attempts to get projects from Gerrit using different API endpoints
func (g *GerritProvider) getProjects(ctx context.Context) (map[string]GerritProject, error) {
	// Try authenticated endpoints first
	projects, err := g.tryAuthenticatedEndpoints(ctx)
	if err == nil {
		return projects, nil
	}

	// Store the original HTTP error for potential reporting
	httpErr := err

	// Only fall back to unauthenticated endpoints if we get specific errors
	// that suggest authentication/authorization issues
	if g.shouldTryUnauthenticated(err) {
		unauthProjects, unauthErr := g.tryUnauthenticatedEndpoints(ctx)
		if unauthErr == nil {
			return unauthProjects, nil
		}
	}

	// If HTTP methods failed and SSH is available, try SSH enumeration
	if g.enableSSH && g.sshAuth != nil && g.username != "" {
		sshProjects, sshErr := g.trySSHProjectListing(ctx)
		if sshErr == nil {
			return sshProjects, nil
		}
		// SSH also failed, but don't return the SSH error as it's a fallback
	}

	return nil, httpErr
}

// shouldTryUnauthenticated determines if we should try unauthenticated endpoints
// based on the error from authenticated endpoints
func (g *GerritProvider) shouldTryUnauthenticated(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	// Try unauthenticated if we get HTML responses, auth errors, missing security prefix, or 401/403 status codes
	return strings.Contains(errStr, "received html response") ||
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "forbidden") ||
		strings.Contains(errStr, "missing security prefix")
}

// tryAuthenticatedEndpoints tries authenticated API endpoints
func (g *GerritProvider) tryAuthenticatedEndpoints(ctx context.Context) (map[string]GerritProject, error) {
	endpoints := []string{"projects/?d", "projects/"}
	return g.tryEndpointsWithMode(ctx, endpoints, g.makeRequest, true)
}

// tryUnauthenticatedEndpoints tries unauthenticated API endpoints
func (g *GerritProvider) tryUnauthenticatedEndpoints(ctx context.Context) (map[string]GerritProject, error) {
	endpoints := []string{"projects/?d", "projects/"}
	return g.tryEndpointsWithMode(ctx, endpoints, g.makeUnauthenticatedRequest, false)
}

// tryEndpointsWithMode tries a list of endpoints using the provided request function and parsing mode
func (g *GerritProvider) tryEndpointsWithMode(ctx context.Context, endpoints []string, requestFunc func(context.Context, string, string, io.Reader) (*http.Response, error), strict bool) (map[string]GerritProject, error) {
	var lastErr error

	for _, endpoint := range endpoints {
		resp, err := requestFunc(ctx, "GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}

		var projects map[string]GerritProject
		err = g.parseResponseWithMode(resp, &projects, strict)
		if err := resp.Body.Close(); err != nil {
			_ = err // Explicitly ignore error to satisfy revive
		}

		if err != nil {
			lastErr = fmt.Errorf("failed to parse response from %s: %w", endpoint, err)
			continue
		}

		return projects, nil
	}

	return nil, fmt.Errorf("all endpoints failed, last error: %w", lastErr)
}

// trySSHProjectListing attempts to list Gerrit projects via SSH using the 'gerrit ls-projects' command
func (g *GerritProvider) trySSHProjectListing(_ context.Context) (map[string]GerritProject, error) {
	if !g.enableSSH || g.sshAuth == nil {
		return nil, fmt.Errorf("SSH not enabled or not available")
	}

	if g.username == "" {
		return nil, errors.New(errSSHUsernameRequired)
	}

	// Parse the base URL to get the host and port
	u, err := url.Parse(g.baseURL)
	if err != nil {
		return nil, fmt.Errorf(errInvalidBaseURL, err)
	}

	host := u.Hostname()
	port := 29418 // Default Gerrit SSH port
	if u.Port() != "" {
		if portNum, err := strconv.Atoi(u.Port()); err == nil {
			port = portNum
		}
	}

	// Get SSH client configuration
	config, err := g.SSHClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH client config: %w", err)
	}

	// Establish SSH connection
	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), config)
	if err != nil {
		return nil, fmt.Errorf(errSSHConnectionFailed, err)
	}
	defer func() {
		_ = conn.Close() // Ignore close error
	}()

	// Create SSH session
	session, err := conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer func() {
		_ = session.Close() // Ignore close error
	}()

	// Execute 'gerrit ls-projects' command
	output, err := session.Output("gerrit ls-projects --format json --description")
	if err != nil {
		// Fallback to basic ls-projects if --format json isn't supported
		session2, err := conn.NewSession()
		if err != nil {
			return nil, fmt.Errorf("failed to create fallback SSH session: %w", err)
		}
		defer func() {
			_ = session2.Close() // Ignore close error
		}()

		output, err = session2.Output("gerrit ls-projects")
		if err != nil {
			return nil, fmt.Errorf("SSH command 'gerrit ls-projects' failed: %w", err)
		}

		return g.parseSSHProjectListOutput(output, false)
	}

	return g.parseSSHProjectListOutput(output, true)
}

// parseSSHProjectListOutput parses the output from 'gerrit ls-projects' command
func (g *GerritProvider) parseSSHProjectListOutput(output []byte, isJSON bool) (map[string]GerritProject, error) {
	projects := make(map[string]GerritProject)

	if isJSON {
		// Try to parse as JSON format (newer Gerrit versions)
		var jsonProjects map[string]GerritProject
		if err := json.Unmarshal(output, &jsonProjects); err == nil {
			return jsonProjects, nil
		}
		// If JSON parsing fails, fall through to line-by-line parsing
	}

	// Parse line-by-line (standard output format)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Basic project entry - just the project name
		projectName := line
		projects[projectName] = GerritProject{
			ID:          projectName,
			Name:        projectName,
			Description: "",       // Not available from basic ls-projects output
			State:       "ACTIVE", // Assume active since it's listed
		}
	}

	return projects, nil
}

// filterAndConvertProjects filters projects by organization and converts them to Repository objects
func (g *GerritProvider) filterAndConvertProjects(projects map[string]GerritProject, orgName string) []*Repository {
	var repositories []*Repository
	for projectName, project := range projects {
		// Filter by organization/prefix if specified
		if orgName != "" && !strings.HasPrefix(projectName, orgName) {
			continue
		}

		repo := g.convertProject(projectName, &project)
		repositories = append(repositories, repo)
	}
	return repositories
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

// CreateRepository creates a new project in Gerrit (not supported for cross-provider)
func (g *GerritProvider) CreateRepository(_ context.Context, _, _, _ string, _ bool) (*Repository, error) {
	return nil, fmt.Errorf("creating repositories is not supported for Gerrit in cross-provider operations")
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
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("Accept", acceptJSON)

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

// makeUnauthenticatedRequest makes an HTTP request to the Gerrit API without authentication
func (g *GerritProvider) makeUnauthenticatedRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	// Try public API endpoint (without /a/ prefix)
	url := g.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Set headers but no authentication
	req.Header.Set("Content-Type", contentTypeJSON)
	req.Header.Set("Accept", acceptJSON)

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
	return g.parseResponseWithMode(resp, target, true) // Default to strict mode
}

// parseResponseWithMode parses a Gerrit API response with configurable strictness
func (g *GerritProvider) parseResponseWithMode(resp *http.Response, target interface{}, strict bool) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	bodyStr := string(body)

	// Check if this looks like an HTML error page (more comprehensive check)
	trimmedBody := strings.TrimSpace(bodyStr)
	if strings.HasPrefix(trimmedBody, "<!DOCTYPE") ||
		strings.HasPrefix(trimmedBody, "<html") ||
		strings.HasPrefix(trimmedBody, "<HTML") {
		return fmt.Errorf("received HTML response instead of JSON - this may indicate authentication is required or the Gerrit API is not accessible at this endpoint")
	}

	// Modern Gerrit responses start with )]}' to prevent CSRF attacks
	if strings.HasPrefix(bodyStr, ")]}'\n") {
		bodyStr = bodyStr[5:]
	} else if strings.HasPrefix(bodyStr, ")]}'") {
		bodyStr = bodyStr[4:]
	} else if strict {
		// In strict mode (authenticated requests), require the security prefix
		return fmt.Errorf("invalid Gerrit response: missing security prefix")
	}
	// In non-strict mode (unauthenticated requests), try to parse as-is

	return json.Unmarshal([]byte(bodyStr), target)
}

// AuthMethod returns the preferred authentication method for Gerrit (HTTP or SSH)
func (g *GerritProvider) AuthMethod() string {
	// Check if SSH is configured and usable
	if g.username != "" && g.password == "" {
		return "ssh"
	}
	return "http"
}

// SSHClientConfig returns SSH client configuration for Gerrit
func (g *GerritProvider) SSHClientConfig() (*ssh.ClientConfig, error) {
	if !g.enableSSH || g.sshAuth == nil {
		return nil, fmt.Errorf("SSH authentication not enabled or available")
	}

	if g.username == "" {
		return nil, errors.New(errSSHUsernameRequired)
	}

	authMethods := g.sshAuth.GetAuthMethods()
	if len(authMethods) == 0 {
		// Fallback to password authentication if available
		if g.password != "" {
			authMethods = append(authMethods, ssh.Password(g.password))
		} else {
			return nil, fmt.Errorf("no SSH authentication methods available")
		}
	}

	return &ssh.ClientConfig{
		User:            g.username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Using same approach as SSH authenticator
		Timeout:         time.Second * 30,
	}, nil
}

// TestSSHConnection tests SSH connectivity to Gerrit server
func (g *GerritProvider) TestSSHConnection() error {
	if !g.enableSSH {
		return fmt.Errorf("SSH not enabled")
	}

	u, err := url.Parse(g.baseURL)
	if err != nil {
		return fmt.Errorf(errInvalidBaseURL, err)
	}

	host := u.Hostname()
	port := 29418 // Default Gerrit SSH port
	if u.Port() != "" {
		if portNum, err := strconv.Atoi(u.Port()); err == nil {
			port = portNum
		}
	}

	config, err := g.SSHClientConfig()
	if err != nil {
		return err
	}

	conn, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), config)
	if err != nil {
		return fmt.Errorf(errSSHConnectionFailed, err)
	}
	defer func() {
		_ = conn.Close() // Ignore close error for test connection
	}()

	return nil
}

// SupportsSSH returns whether SSH authentication is available
func (g *GerritProvider) SupportsSSH() bool {
	return g.enableSSH && g.sshAuth != nil
}
