// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"testing"
)

func TestParseGenericSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected *SourceInfo
		hasError bool
	}{
		{
			name:   "GitHub HTTPS URL",
			source: "https://github.com/myorg/myrepo",
			expected: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "myorg",
				Path:         "myrepo",
			},
		},
		{
			name:   "GitHub SSH URL",
			source: "git@github.com:myorg/myrepo.git",
			expected: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "myorg",
				Path:         "myrepo",
				IsSSH:        true,
			},
		},
		{
			name:   "GitLab HTTPS URL",
			source: "https://gitlab.com/mygroup/myproject",
			expected: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "mygroup",
				Path:         "myproject",
			},
		},
		{
			name:   "Gerrit server hostname",
			source: "gerrit.example.com",
			expected: &SourceInfo{
				Host:         "gerrit.example.com",
				Organization: "",
			},
		},
		{
			name:   "Organization/repo format",
			source: "myorg/myrepo",
			expected: &SourceInfo{
				Host:         "myorg",
				Organization: "myrepo",
			},
		},
		{
			name:   "Simple organization name",
			source: "modeseven-lfreleng-actions",
			expected: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "modeseven-lfreleng-actions",
			},
		},
		{
			name:     "Empty source",
			source:   "",
			hasError: true,
		},
		{
			name:     "Invalid URL",
			source:   "://invalid-url",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseGenericSource(tt.source)

			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Provider != tt.expected.Provider {
				t.Errorf("Expected provider %s, got %s", tt.expected.Provider, result.Provider)
			}

			if result.Host != tt.expected.Host {
				t.Errorf("Expected host %s, got %s", tt.expected.Host, result.Host)
			}

			if result.Organization != tt.expected.Organization {
				t.Errorf("Expected organization %s, got %s", tt.expected.Organization, result.Organization)
			}

			if result.Path != tt.expected.Path {
				t.Errorf("Expected path %s, got %s", tt.expected.Path, result.Path)
			}

			if result.IsSSH != tt.expected.IsSSH {
				t.Errorf("Expected IsSSH %t, got %t", tt.expected.IsSSH, result.IsSSH)
			}
		})
	}
}

func TestProviderManager(t *testing.T) {
	config := &Config{
		GitHubToken:    "test-github-token",
		GitLabToken:    "test-gitlab-token",
		GerritUsername: "test-user",
		GerritPassword: "test-pass",
	}

	manager := NewProviderManager(config)

	// Test mock provider registration
	mockProvider := &MockProvider{name: "mock"}
	manager.RegisterProvider("mock", mockProvider)

	// Test getting registered provider
	provider, err := manager.GetProvider("mock")
	if err != nil {
		t.Fatalf("Failed to get mock provider: %v", err)
	}

	if provider.Name() != "mock" {
		t.Errorf("Expected provider name 'mock', got %s", provider.Name())
	}

	// Test getting non-existent provider
	_, err = manager.GetProvider("non-existent")
	if err == nil {
		t.Error("Expected error for non-existent provider")
	}
}

func TestProviderManager_GetProviderForSource(t *testing.T) {
	manager := NewProviderManager(&Config{})

	// Register mock providers
	githubMock := &MockProvider{
		name:           "github",
		parsableSource: "github.com/org",
	}
	gitlabMock := &MockProvider{
		name:           "gitlab",
		parsableSource: "gitlab.com/group",
	}

	manager.RegisterProvider("github", githubMock)
	manager.RegisterProvider("gitlab", gitlabMock)

	// Test getting provider for GitHub source
	provider, sourceInfo, err := manager.GetProviderForSource("github.com/org")
	if err != nil {
		t.Fatalf("Failed to get provider for GitHub source: %v", err)
	}

	if provider.Name() != "github" {
		t.Errorf("Expected GitHub provider, got %s", provider.Name())
	}

	if sourceInfo.Organization != "org" {
		t.Errorf("Expected organization 'org', got %s", sourceInfo.Organization)
	}

	// Test getting provider for unknown source
	_, _, err = manager.GetProviderForSource("unknown.com/org")
	if err == nil {
		t.Error("Expected error for unknown source")
	}
}

// MockProvider for testing
type MockProvider struct {
	name           string
	parsableSource string
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) ParseSource(source string) (*SourceInfo, error) {
	if source == m.parsableSource {
		return &SourceInfo{
			Provider:     m.name,
			Host:         source,
			Organization: "org",
		}, nil
	}
	return nil, &Error{Message: "cannot parse source"}
}

func (m *MockProvider) GetOrganization(_ context.Context, orgName string) (*Organization, error) {
	return &Organization{
		ID:   "1",
		Name: orgName,
	}, nil
}

func (m *MockProvider) ListRepositories(_ context.Context, orgName string) ([]*Repository, error) {
	return []*Repository{
		{
			ID:       "1",
			Name:     "repo1",
			FullName: orgName + "/repo1",
		},
	}, nil
}

func (m *MockProvider) CreateFork(_ context.Context, sourceRepo *Repository, targetOrg string) (*Repository, error) {
	return &Repository{
		ID:       "2",
		Name:     sourceRepo.Name,
		FullName: targetOrg + "/" + sourceRepo.Name,
	}, nil
}

func (m *MockProvider) CreateOrganization(_ context.Context, orgName, displayName, description string) (*Organization, error) {
	return &Organization{
		ID:          "2",
		Name:        orgName,
		DisplayName: displayName,
		Description: description,
	}, nil
}

func (m *MockProvider) SyncRepository(_ context.Context, _ *Repository) error {
	return nil
}

func (m *MockProvider) CreateRepository(_ context.Context, orgName, repoName, description string, private bool) (*Repository, error) {
	return &Repository{
		ID:          "3",
		Name:        repoName,
		FullName:    orgName + "/" + repoName,
		Description: description,
		Private:     private,
	}, nil
}

func (m *MockProvider) RepositoryExists(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func (m *MockProvider) Close() error {
	return nil
}

// Error represents a test error
type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected *SourceInfo
		hasError bool
	}{
		{
			name: "Valid GitHub URL",
			url:  "https://github.com/owner/repo",
			expected: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "owner",
				Path:         "repo",
			},
		},
		{
			name: "Valid GitLab URL",
			url:  "https://gitlab.com/group/project",
			expected: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "group",
				Path:         "project",
			},
		},
		{
			name: "Gerrit URL",
			url:  "https://gerrit.example.com/admin/repos",
			expected: &SourceInfo{
				Provider:     "gerrit",
				Host:         "gerrit.example.com",
				Organization: "admin",
				Path:         "repos",
			},
		},
		{
			name:     "Invalid URL",
			url:      "://invalid",
			hasError: true,
		},
		{
			name: "URL with no path (defaults to Gerrit)",
			url:  "https://example.com",
			expected: &SourceInfo{
				Provider: "gerrit",
				Host:     "example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseURL(tt.url)

			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Provider != tt.expected.Provider {
				t.Errorf("Expected provider %s, got %s", tt.expected.Provider, result.Provider)
			}

			if result.Host != tt.expected.Host {
				t.Errorf("Expected host %s, got %s", tt.expected.Host, result.Host)
			}

			if result.Organization != tt.expected.Organization {
				t.Errorf("Expected organization %s, got %s", tt.expected.Organization, result.Organization)
			}

			if result.Path != tt.expected.Path {
				t.Errorf("Expected path %s, got %s", tt.expected.Path, result.Path)
			}
		})
	}
}

func TestParseSSHURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected *SourceInfo
		hasError bool
	}{
		{
			name: "Valid GitHub SSH URL",
			url:  "git@github.com:owner/repo.git",
			expected: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "owner",
				Path:         "repo",
				IsSSH:        true,
			},
		},
		{
			name: "Valid GitLab SSH URL",
			url:  "git@gitlab.com:group/project.git",
			expected: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "group",
				Path:         "project",
				IsSSH:        true,
			},
		},
		{
			name: "Gerrit SSH URL",
			url:  "git@gerrit.example.com:admin/repos",
			expected: &SourceInfo{
				Provider:     "gerrit",
				Host:         "gerrit.example.com",
				Organization: "admin",
				Path:         "repos",
				IsSSH:        true,
			},
		},
		{
			name:     "Invalid SSH URL format",
			url:      "invalid-ssh-url",
			hasError: true,
		},
		{
			name:     "SSH URL without colon",
			url:      "git@github.com",
			hasError: true,
		},
		{
			name:     "SSH URL with empty path",
			url:      "git@github.com:",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSSHURL(tt.url)

			if tt.hasError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result.Provider != tt.expected.Provider {
				t.Errorf("Expected provider %s, got %s", tt.expected.Provider, result.Provider)
			}

			if result.Host != tt.expected.Host {
				t.Errorf("Expected host %s, got %s", tt.expected.Host, result.Host)
			}

			if result.Organization != tt.expected.Organization {
				t.Errorf("Expected organization %s, got %s", tt.expected.Organization, result.Organization)
			}

			if result.Path != tt.expected.Path {
				t.Errorf("Expected path %s, got %s", tt.expected.Path, result.Path)
			}

			if result.IsSSH != tt.expected.IsSSH {
				t.Errorf("Expected IsSSH %t, got %t", tt.expected.IsSSH, result.IsSSH)
			}
		})
	}
}
