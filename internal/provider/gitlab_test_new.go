// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestGitLabProviderListRepositories tests the ListRepositories functionality for GitLab provider
func TestGitLabProviderListRepositories(t *testing.T) {
	tests := []struct {
		name           string
		source         *SourceInfo
		mockResponse   string
		mockStatusCode int
		expectedCount  int
		expectError    bool
	}{
		{
			name: "successful project listing",
			source: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "mygroup",
			},
			mockResponse: `[
				{
					"id": 1,
					"name": "project1",
					"path_with_namespace": "mygroup/project1",
					"description": "Test project 1",
					"http_url_to_repo": "https://gitlab.com/mygroup/project1.git",
					"ssh_url_to_repo": "git@gitlab.com:mygroup/project1.git"
				},
				{
					"id": 2,
					"name": "project2",
					"path_with_namespace": "mygroup/project2",
					"description": "Test project 2",
					"http_url_to_repo": "https://gitlab.com/mygroup/project2.git",
					"ssh_url_to_repo": "git@gitlab.com:mygroup/project2.git"
				}
			]`,
			mockStatusCode: http.StatusOK,
			expectedCount:  2,
		},
		{
			name: "empty project list",
			source: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "emptygroup",
			},
			mockResponse:   `[]`,
			mockStatusCode: http.StatusOK,
			expectedCount:  0,
		},
		{
			name: "unauthorized access",
			source: &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "privategroup",
			},
			mockResponse:   `{"message": "401 Unauthorized"}`,
			mockStatusCode: http.StatusUnauthorized,
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

			provider, err := NewGitLabProvider("test-token", server.URL)
			if err != nil {
				t.Fatalf("Failed to create GitLab provider: %v", err)
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

// TestGitLabProviderCreateFork tests the CreateFork functionality for GitLab provider
func TestGitLabProviderCreateFork(t *testing.T) {
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
				Name:     "test-project",
				FullName: "owner/test-project",
				CloneURL: "https://gitlab.com/owner/test-project.git",
				ID:       "123",
			},
			mockResponse: `{
				"id": 456,
				"name": "test-project",
				"path_with_namespace": "forker/test-project",
				"http_url_to_repo": "https://gitlab.com/forker/test-project.git",
				"ssh_url_to_repo": "git@gitlab.com:forker/test-project.git"
			}`,
			mockStatusCode: http.StatusCreated,
		},
		{
			name: "fork already exists",
			repo: &Repository{
				Name:     "existing-project",
				FullName: "owner/existing-project",
				CloneURL: "https://gitlab.com/owner/existing-project.git",
				ID:       "789",
			},
			mockResponse:   `{"message": ["Project already forked"]}`,
			mockStatusCode: http.StatusConflict,
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

			provider, err := NewGitLabProvider("test-token", server.URL)
			if err != nil {
				t.Fatalf("Failed to create GitLab provider: %v", err)
			}

			fork, err := provider.CreateFork(context.Background(), tt.repo, "target-group")

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

// TestGitLabProviderRateLimitHandling tests rate limit handling for GitLab provider
func TestGitLabProviderRateLimitHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("RateLimit-Limit", "2000")
		w.Header().Set("RateLimit-Remaining", "0")
		w.Header().Set("RateLimit-Reset", "1640995200")
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := w.Write([]byte(`{"message": "429 Too Many Requests"}`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewGitLabProvider("test-token", server.URL)
	if err != nil {
		t.Fatalf("Failed to create GitLab provider: %v", err)
	}

	source := &SourceInfo{
		Provider:     "gitlab",
		Host:         "gitlab.com",
		Organization: "testgroup",
	}

	_, err = provider.ListRepositories(context.Background(), source.Organization)
	if err == nil {
		t.Error("Expected rate limit error but got none")
	}

	// Check if error message indicates rate limiting
	if err.Error() != "rate limit exceeded" {
		t.Errorf("Expected rate limit error message, got: %v", err)
	}
}

// TestGitLabProviderAuthentication tests authentication handling for GitLab provider
func TestGitLabProviderAuthentication(t *testing.T) {
	tests := []struct {
		name        string
		token       string
		expectAuth  bool
		expectError bool
	}{
		{
			name:       "with valid token",
			token:      "glpat-valid-token",
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
				authHeader := r.Header.Get("Private-Token")

				if tt.expectAuth {
					if authHeader == "" {
						t.Error("Expected Private-Token header but got none")
					}
					if authHeader != tt.token {
						t.Errorf("Expected '%s', got '%s'", tt.token, authHeader)
					}
				} else {
					if authHeader != "" {
						t.Error("Expected no Private-Token header but got one")
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(`[]`)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGitLabProvider(tt.token, server.URL)
			if err != nil {
				t.Fatalf("Failed to create GitLab provider: %v", err)
			}

			source := &SourceInfo{
				Provider:     "gitlab",
				Host:         "gitlab.com",
				Organization: "testgroup",
			}

			_, err = provider.ListRepositories(context.Background(), source.Organization)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

// TestNewGitLabProvider tests the creation of new GitLab provider instances
func TestNewGitLabProvider(t *testing.T) {
	tests := []struct {
		name        string
		source      *SourceInfo
		token       string
		expectError bool
	}{
		{
			name: "valid gitlab.com source",
			source: &SourceInfo{
				Provider: "gitlab",
				Host:     "gitlab.com",
			},
			token: "test-token",
		},
		{
			name: "self-hosted gitlab source",
			source: &SourceInfo{
				Provider: "gitlab",
				Host:     "gitlab.example.com",
			},
			token: "test-token",
		},
		{
			name: "without token",
			source: &SourceInfo{
				Provider: "gitlab",
				Host:     "gitlab.com",
			},
			token: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL := "https://" + tt.source.Host
			provider, err := NewGitLabProvider(tt.token, baseURL)

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
