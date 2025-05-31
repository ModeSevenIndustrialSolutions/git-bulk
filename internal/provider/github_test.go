// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubProvider_ListRepositories(t *testing.T) {
	tests := []struct {
		name           string
		source         *SourceInfo
		mockResponse   string
		mockStatusCode int
		expectedCount  int
		expectError    bool
	}{
		{
			name: "successful organization repositories",
			source: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "testorg",
			},
			mockResponse: `[
				{
					"name": "repo1",
					"full_name": "testorg/repo1",
					"clone_url": "https://github.com/testorg/repo1.git",
					"ssh_url": "git@github.com:testorg/repo1.git",
					"description": "Test repository 1",
					"fork": false
				},
				{
					"name": "repo2",
					"full_name": "testorg/repo2",
					"clone_url": "https://github.com/testorg/repo2.git",
					"ssh_url": "git@github.com:testorg/repo2.git",
					"description": "Test repository 2",
					"fork": true
				}
			]`,
			mockStatusCode: http.StatusOK,
			expectedCount:  2,
		},
		{
			name: "successful user repositories",
			source: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "testuser",
				Path:         "",
			},
			mockResponse: `[
				{
					"name": "user-repo",
					"full_name": "testuser/user-repo",
					"clone_url": "https://github.com/testuser/user-repo.git",
					"ssh_url": "git@github.com:testuser/user-repo.git",
					"description": "User repository",
					"fork": false
				}
			]`,
			mockStatusCode: http.StatusOK,
			expectedCount:  1,
		},
		{
			name: "organization not found",
			source: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "nonexistent",
			},
			mockResponse:   `{"message": "Not Found"}`,
			mockStatusCode: http.StatusNotFound,
			expectError:    true,
		},
		{
			name: "rate limited",
			source: &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "testorg",
			},
			mockResponse:   `{"message": "API rate limit exceeded"}`,
			mockStatusCode: http.StatusForbidden,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			// Create provider with mock server URL
			provider, err := NewGitHubProvider("test-token", server.URL+"/")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			repos, err := provider.ListRepositories(context.Background(), tt.source.Organization)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(repos) != tt.expectedCount {
				t.Errorf("Expected %d repositories, got %d", tt.expectedCount, len(repos))
			}

			// Verify repository structure
			if len(repos) > 0 {
				repo := repos[0]
				if repo.Name == "" {
					t.Error("Repository name should not be empty")
				}
				if repo.CloneURL == "" {
					t.Error("Repository clone URL should not be empty")
				}
				if repo.SSHCloneURL == "" {
					t.Error("Repository SSH URL should not be empty")
				}
			}
		})
	}
}

func TestGitHubProvider_CreateFork(t *testing.T) {
	tests := []struct {
		name           string
		repo           *Repository
		mockResponse   string
		mockStatusCode int
		expectError    bool
	}{
		{
			name: "successful fork creation",
			repo: &Repository{
				Name:     "test-repo",
				FullName: "owner/test-repo",
				CloneURL: "https://github.com/owner/test-repo.git",
			},
			mockResponse: `{
				"name": "test-repo",
				"full_name": "forker/test-repo",
				"clone_url": "https://github.com/forker/test-repo.git",
				"ssh_url": "git@github.com:forker/test-repo.git"
			}`,
			mockStatusCode: http.StatusCreated,
		},
		{
			name: "fork already exists",
			repo: &Repository{
				Name:     "existing-repo",
				FullName: "owner/existing-repo",
				CloneURL: "https://github.com/owner/existing-repo.git",
			},
			mockResponse:   `{"message": "Repository already exists"}`,
			mockStatusCode: http.StatusUnprocessableEntity,
			expectError:    true,
		},
		{
			name: "unauthorized",
			repo: &Repository{
				Name:     "private-repo",
				FullName: "owner/private-repo",
				CloneURL: "https://github.com/owner/private-repo.git",
			},
			mockResponse:   `{"message": "Not Found"}`,
			mockStatusCode: http.StatusNotFound,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGitHubProvider("test-token", server.URL+"/")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			fork, err := provider.CreateFork(context.Background(), tt.repo, "target-org")

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if fork == nil {
				t.Fatal("Expected fork to be created but got nil")
			}

			if fork.Name != tt.repo.Name {
				t.Errorf("Expected fork name %s, got %s", tt.repo.Name, fork.Name)
			}
		})
	}
}

func TestGitHubProvider_RateLimitHandling(t *testing.T) {
	// Test rate limit detection and handling
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1640995200") // Some future timestamp
		w.WriteHeader(http.StatusForbidden)
		if _, err := w.Write([]byte(`{"message": "API rate limit exceeded"}`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewGitHubProvider("test-token", server.URL+"/")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	source := &SourceInfo{
		Provider:     "github",
		Host:         "github.com",
		Organization: "testorg",
	}

	_, err = provider.ListRepositories(context.Background(), source.Organization)
	if err == nil {
		t.Error("Expected rate limit error but got none")
	}

	// Check if error message indicates rate limiting
	if err.Error() != "API rate limit exceeded" {
		t.Errorf("Expected rate limit error message, got: %v", err)
	}
}

func TestGitHubProvider_Authentication(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectAuth  bool
		expectError bool
	}{
		{
			name:       "with valid token",
			token:      "ghp_valid_token",
			expectAuth: true,
		},
		{
			name:       "without token",
			token:      "",
			expectAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authHeader := r.Header.Get("Authorization")

				if tt.expectAuth {
					if authHeader == "" {
						t.Error("Expected Authorization header but got none")
					}
					if authHeader != "token "+tt.token {
						t.Errorf("Expected 'token %s', got '%s'", tt.token, authHeader)
					}
				} else {
					if authHeader != "" {
						t.Error("Expected no Authorization header but got one")
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(`[]`)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGitHubProvider(tt.token, server.URL+"/")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			source := &SourceInfo{
				Provider:     "github",
				Host:         "github.com",
				Organization: "testorg",
			}

			_, err = provider.ListRepositories(context.Background(), source.Organization)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestGitHubProvider_PaginationHandling(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if callCount == 1 {
			// First page - set next page link
			w.Header().Set("Link", `<http://example.com/api/v3/orgs/testorg/repos?page=2>; rel="next"`)
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`[{"name": "repo1", "full_name": "org/repo1", "clone_url": "https://github.com/org/repo1.git", "ssh_url": "git@github.com:org/repo1.git"}]`)); err != nil {
				t.Errorf("Failed to write response: %v", err)
			}
		} else {
			// Second page
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte(`[{"name": "repo2", "full_name": "org/repo2", "clone_url": "https://github.com/org/repo2.git", "ssh_url": "git@github.com:org/repo2.git"}]`)); err != nil {
				t.Errorf("Failed to write response: %v", err)
			}
		}
	}))
	defer server.Close()

	provider, err := NewGitHubProvider("test-token", server.URL+"/api/v3/")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	repos, err := provider.ListRepositories(context.Background(), "testorg")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(repos) != 2 {
		t.Errorf("Expected 2 repositories from pagination, got %d", len(repos))
	}

	// Verify the repositories have correct data from both pages
	expectedRepos := []string{"repo1", "repo2"}
	actualRepos := make([]string, len(repos))
	for i, repo := range repos {
		actualRepos[i] = repo.Name
	}
	for i, expected := range expectedRepos {
		if i >= len(actualRepos) || actualRepos[i] != expected {
			t.Errorf("Expected repository %s at index %d, got %v", expected, i, actualRepos)
		}
	}
}

func TestNewGitHubProvider(t *testing.T) {
	tests := []struct {
		name        string
		source      *SourceInfo
		token       string
		expectError bool
	}{
		{
			name: "valid github.com source",
			source: &SourceInfo{
				Provider: "github",
				Host:     "github.com",
			},
			token: "test-token",
		},
		{
			name: "github enterprise source",
			source: &SourceInfo{
				Provider: "github",
				Host:     "github.enterprise.com",
			},
			token: "test-token",
		},
		{
			name: "without token",
			source: &SourceInfo{
				Provider: "github",
				Host:     "github.com",
			},
			token: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL := "https://" + tt.source.Host
			provider, err := NewGitHubProvider(tt.token, baseURL)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if provider == nil {
				t.Fatal("Expected provider but got nil")
			}

			if provider.token != tt.token {
				t.Errorf("Expected token %s, got %s", tt.token, provider.token)
			}
		})
	}
}
