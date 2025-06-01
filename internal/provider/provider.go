// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RateLimitError represents a rate limiting error
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return e.Message
}

// IsRateLimitError checks if an error is a rate limit error
func IsRateLimitError(err error) (*RateLimitError, bool) {
	if rle, ok := err.(*RateLimitError); ok {
		return rle, true
	}
	return nil, false
}

// Repository represents a repository from any Git hosting provider
type Repository struct {
	ID          string            // Unique identifier
	Name        string            // Repository name
	FullName    string            // Full name (e.g., "org/repo")
	CloneURL    string            // HTTPS clone URL
	SSHCloneURL string            // SSH clone URL
	Description string            // Repository description
	Private     bool              // Whether repository is private
	Fork        bool              // Whether repository is a fork
	Language    string            // Primary programming language
	Size        int64             // Repository size in KB
	Stars       int               // Star count
	Forks       int               // Fork count
	CreatedAt   string            // Creation timestamp
	UpdatedAt   string            // Last update timestamp
	Metadata    map[string]string // Provider-specific metadata
	Path        string            // Hierarchical path (for Gerrit nested projects)
}

// Organization represents an organization/group from any Git hosting provider
type Organization struct {
	ID          string            // Unique identifier
	Name        string            // Organization name
	DisplayName string            // Display name
	Description string            // Organization description
	URL         string            // Organization URL
	Type        string            // Organization type (user, organization, group, etc.)
	Metadata    map[string]string // Provider-specific metadata
}

// SSHProvider defines the interface for SSH-enabled providers
type SSHProvider interface {
	// SupportsSSH returns whether SSH authentication is available
	SupportsSSH() bool

	// TestSSHConnection tests SSH connectivity
	TestSSHConnection() error
}

// Provider defines the interface for Git hosting providers
type Provider interface {
	// Name returns the provider name (e.g., "github", "gitlab", "gerrit")
	Name() string

	// ParseSource parses a source URL/identifier and returns normalized components
	ParseSource(source string) (*SourceInfo, error)

	// GetOrganization retrieves organization information
	GetOrganization(ctx context.Context, orgName string) (*Organization, error)

	// ListRepositories lists all repositories in an organization
	ListRepositories(ctx context.Context, orgName string) ([]*Repository, error)

	// CreateFork creates a fork of a repository in the target organization
	CreateFork(ctx context.Context, sourceRepo *Repository, targetOrg string) (*Repository, error)

	// CreateOrganization creates a new organization (if supported)
	CreateOrganization(ctx context.Context, orgName, displayName, description string) (*Organization, error)

	// SyncRepository synchronizes a forked repository with its upstream
	SyncRepository(ctx context.Context, repo *Repository) error

	// RepositoryExists checks if a repository exists in an organization
	RepositoryExists(ctx context.Context, orgName, repoName string) (bool, error)

	// Close performs any cleanup operations
	Close() error
}

// SourceInfo contains parsed information about a source
type SourceInfo struct {
	Provider     string // Provider name (github, gitlab, gerrit)
	Host         string // Hostname
	Organization string // Organization/group name
	Path         string // Additional path information
	IsSSH        bool   // Whether this is an SSH URL
}

// Config holds configuration for provider authentication
type Config struct {
	GitHubToken    string
	GitLabToken    string
	GerritUsername string
	GerritPassword string
	GerritToken    string
}

// Manager manages multiple Git hosting providers
type Manager struct {
	providers map[string]Provider
	config    *Config
}

// NewProviderManager creates a new provider manager
func NewProviderManager(config *Config) *Manager {
	return &Manager{
		providers: make(map[string]Provider),
		config:    config,
	}
}

// RegisterProvider registers a provider with the manager
func (pm *Manager) RegisterProvider(name string, provider Provider) {
	pm.providers[name] = provider
}

// GetProvider returns a provider by name
func (pm *Manager) GetProvider(name string) (Provider, error) {
	provider, exists := pm.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return provider, nil
}

// GetProviderForSource determines and returns the appropriate provider for a source
func (pm *Manager) GetProviderForSource(source string) (Provider, *SourceInfo, error) {
	// Try to parse the source with each provider
	for _, provider := range pm.providers {
		if sourceInfo, err := provider.ParseSource(source); err == nil {
			return provider, sourceInfo, nil
		}
	}

	// If no provider can parse the source, try to create a Gerrit provider dynamically
	// This handles cases where the source is a Gerrit server not yet registered
	if sourceInfo, err := ParseGenericSource(source); err == nil {
		// If the generic parser determined it's a gerrit provider or unknown, try creating a Gerrit provider
		if sourceInfo.Provider == "gerrit" || sourceInfo.Provider == "" {
			baseURL := "https://" + sourceInfo.Host
			gerritProvider, err := NewGerritProvider(baseURL, pm.config.GerritUsername, pm.config.GerritPassword)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create Gerrit provider for %s: %w", sourceInfo.Host, err)
			}
			// Update the source info to include the provider name
			sourceInfo.Provider = "gerrit"
			return gerritProvider, sourceInfo, nil
		}
	}

	return nil, nil, fmt.Errorf("no suitable provider found for source: %s", source)
}

// ParseGenericSource provides a generic source parser as fallback
func ParseGenericSource(source string) (*SourceInfo, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, fmt.Errorf("empty source")
	}

	// Check if it's a URL
	if strings.Contains(source, "://") {
		return parseURL(source)
	}

	// Check if it's an SSH-style URL (git@host:path)
	if strings.Contains(source, "@") && strings.Contains(source, ":") {
		return parseSSHURL(source)
	}

	// Try to parse as hostname/org format
	parts := strings.Split(source, "/")
	if len(parts) >= 2 {
		hostname := strings.ToLower(parts[0])
		// If the hostname looks like a domain (contains dots) but isn't a known Git hosting service, reject it
		if strings.Contains(parts[0], ".") && !strings.Contains(hostname, "github") && !strings.Contains(hostname, "gitlab") && !strings.Contains(hostname, "gerrit") {
			return nil, fmt.Errorf("unknown Git hosting service: %s", parts[0])
		}
		return &SourceInfo{
			Host:         parts[0],
			Organization: parts[1],
			Path:         strings.Join(parts[2:], "/"),
		}, nil
	}

	// Handle single hostname (like "gerrit.example.com")
	if len(parts) == 1 && strings.Contains(source, ".") {
		return &SourceInfo{
			Host:         source,
			Organization: "",
			// Don't set a provider for a simple hostname
		}, nil
	}

	// Handle simple organization name (default to GitHub)
	if len(parts) == 1 && !strings.Contains(source, ".") {
		return &SourceInfo{
			Host:         "github.com",
			Organization: source,
			Provider:     "github",
		}, nil
	}

	return nil, fmt.Errorf("unable to parse source: %s", source)
}

func parseURL(source string) (*SourceInfo, error) {
	u, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	pathParts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// Filter out empty parts
	var nonEmptyParts []string
	for _, part := range pathParts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}

	// Determine provider from hostname first
	hostname := strings.ToLower(u.Host)
	var providerName string
	switch {
	case strings.Contains(hostname, "github"):
		providerName = "github"
	case strings.Contains(hostname, "gitlab"):
		providerName = "gitlab"
	case strings.Contains(hostname, "gerrit"):
		providerName = "gerrit"
	default:
		// For unknown hosts, default to Gerrit (most flexible)
		// This allows empty paths to mean "list all projects"
		providerName = "gerrit"
	}

	// For GitHub and GitLab, we require at least one path component (org/user)
	// For Gerrit, we allow empty paths to mean "list all projects"
	if providerName != "gerrit" && len(nonEmptyParts) < 1 {
		return nil, fmt.Errorf("invalid URL path: %s", u.Path)
	}

	info := &SourceInfo{
		Host:     u.Host,
		Provider: providerName,
		IsSSH:    u.Scheme == "ssh",
	}

	if len(nonEmptyParts) > 0 {
		info.Organization = nonEmptyParts[0]
		if len(nonEmptyParts) > 1 {
			info.Path = strings.Join(nonEmptyParts[1:], "/")
		}
	}

	return info, nil
}

func parseSSHURL(source string) (*SourceInfo, error) {
	// Parse SSH URLs like git@github.com:org/repo.git
	parts := strings.Split(source, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid SSH URL format: %s", source)
	}

	hostAndPath := parts[1]
	colonIndex := strings.Index(hostAndPath, ":")
	if colonIndex == -1 {
		return nil, fmt.Errorf("invalid SSH URL format: %s", source)
	}

	host := hostAndPath[:colonIndex]
	path := hostAndPath[colonIndex+1:]

	pathParts := strings.Split(strings.TrimSuffix(path, ".git"), "/")
	// Filter out empty parts
	var nonEmptyParts []string
	for _, part := range pathParts {
		if part != "" {
			nonEmptyParts = append(nonEmptyParts, part)
		}
	}

	if len(nonEmptyParts) < 1 {
		return nil, fmt.Errorf("invalid SSH URL path: %s", path)
	}

	info := &SourceInfo{
		Host:         host,
		Organization: nonEmptyParts[0],
		IsSSH:        true,
	}

	if len(nonEmptyParts) > 1 {
		info.Path = strings.Join(nonEmptyParts[1:], "/")
	}

	// Determine provider from hostname
	hostname := strings.ToLower(host)
	switch {
	case strings.Contains(hostname, "github"):
		info.Provider = "github"
	case strings.Contains(hostname, "gitlab"):
		info.Provider = "gitlab"
	default:
		info.Provider = "gerrit"
	}

	return info, nil
}

// Close closes all registered providers
func (pm *Manager) Close() error {
	var errs []string
	for name, provider := range pm.providers {
		if err := provider.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing providers: %s", strings.Join(errs, ", "))
	}

	return nil
}
