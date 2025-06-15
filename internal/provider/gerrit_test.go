// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGerritProvider_ListRepositories(t *testing.T) {
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
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			mockResponse: `)]}'
{
  "project1": {
    "id": "project1",
    "description": "Test project 1",
    "state": "ACTIVE"
  },
  "project2": {
    "id": "project2",
    "description": "Test project 2",
    "state": "ACTIVE"
  },
  "archived-project": {
    "id": "archived-project",
    "description": "Archived project",
    "state": "READ_ONLY"
  }
}`,
			mockStatusCode: http.StatusOK,
			expectedCount:  3,
		},
		{
			name: "empty project list",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			mockResponse: `)]}'
{}`,
			mockStatusCode: http.StatusOK,
			expectedCount:  0,
		},
		{
			name: "unauthorized access",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			mockResponse:   `Unauthorized`,
			mockStatusCode: http.StatusUnauthorized,
			expectError:    true,
		},
		{
			name: "server error",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			mockResponse:   `Internal Server Error`,
			mockStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				// The implementation tries multiple endpoints in order:
				// 1. /a/projects/?d (authenticated with description)
				// 2. /a/projects/ (authenticated)
				// 3. /projects/?d (unauthenticated with description) - fallback
				// 4. /projects/ (unauthenticated) - fallback
				validPaths := []string{"/a/projects/?d", "/a/projects/", "/projects/?d", "/projects/"}
				pathValid := false
				for _, validPath := range validPaths {
					if r.URL.Path == validPath || r.URL.Path+"?"+r.URL.RawQuery == validPath {
						pathValid = true
						break
					}
				}
				if !pathValid {
					t.Errorf("Unexpected path %s, expected one of %v", r.URL.Path, validPaths)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.mockStatusCode)
				if _, err := w.Write([]byte(tt.mockResponse)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGerritProvider(server.URL, "testuser", "testpass")
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

func TestGerritProvider_CloneURLGeneration(t *testing.T) {
	tests := []struct {
		name         string
		source       *SourceInfo
		projectName  string
		expectedHTTP string
		expectedSSH  string
	}{
		{
			name: "standard project",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			projectName:  "my-project",
			expectedHTTP: "https://gerrit.example.com/my-project",
			expectedSSH:  "ssh://gerrit.example.com:29418/my-project",
		},
		{
			name: "project with slashes",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			projectName:  "group/subgroup/project",
			expectedHTTP: "https://gerrit.example.com/group/subgroup/project",
			expectedSSH:  "ssh://gerrit.example.com:29418/group/subgroup/project",
		},
		{
			name: "custom SSH port",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com:2222",
			},
			projectName:  "test-project",
			expectedHTTP: "https://gerrit.example.com:2222/test-project",
			expectedSSH:  "ssh://gerrit.example.com:2222/test-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				response := `)]}'
{
  "` + tt.projectName + `": {
    "id": "` + tt.projectName + `",
    "description": "Test project",
    "state": "ACTIVE",
    "clone_links": {
      "http": {"url": "` + tt.expectedHTTP + `"},
      "ssh": {"url": "` + tt.expectedSSH + `"}
    }
  }
}`
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(response)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGerritProvider(server.URL, "testuser", "testpass")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			repos, err := provider.ListRepositories(context.Background(), tt.source.Organization)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(repos) != 1 {
				t.Fatalf("Expected 1 repository, got %d", len(repos))
			}

			repo := repos[0]
			if repo.CloneURL != tt.expectedHTTP {
				t.Errorf("Expected HTTP clone URL %s, got %s", tt.expectedHTTP, repo.CloneURL)
			}
			if repo.SSHCloneURL != tt.expectedSSH {
				t.Errorf("Expected SSH clone URL %s, got %s", tt.expectedSSH, repo.SSHCloneURL)
			}
		})
	}
}

func TestGerritProvider_Authentication(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		password    string
		expectAuth  bool
		expectError bool
	}{
		{
			name:       "with credentials",
			username:   "testuser",
			password:   "testpass",
			expectAuth: true,
		},
		{
			name:       "without credentials",
			username:   "",
			password:   "",
			expectAuth: false,
		},
		{
			name:       "username only",
			username:   "testuser",
			password:   "",
			expectAuth: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, password, ok := r.BasicAuth()

				if tt.expectAuth {
					if !ok {
						t.Error("Expected basic auth but got none")
					}
					if username != tt.username {
						t.Errorf("Expected username %s, got %s", tt.username, username)
					}
					if password != tt.password {
						t.Errorf("Expected password %s, got %s", tt.password, password)
					}
				} else {
					if ok {
						t.Error("Expected no basic auth but got some")
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(`)]}'
{}`)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGerritProvider(server.URL, tt.username, tt.password)
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}
			source := &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			}

			_, err = provider.ListRepositories(context.Background(), source.Organization)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
		})
	}
}

func TestGerritProvider_ResponseParsing(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		expectError  bool
		expectedName string
	}{
		{
			name: "valid gerrit response",
			response: `)]}'
{
  "test-project": {
    "id": "test-project",
    "description": "Test project",
    "state": "ACTIVE"
  }
}`,
			expectedName: "test-project",
		},
		{
			name: "response without magic prefix",
			response: `{
  "test-project": {
    "id": "test-project",
    "description": "Test project",
    "state": "ACTIVE"
  }
}`,
			expectError:  false, // Changed: non-strict mode allows responses without magic prefix
			expectedName: "test-project",
		},
		{
			name:        "invalid JSON",
			response:    `)]}'invalid json{`,
			expectError: true,
		},
		{
			name: "project with read-only state",
			response: `)]}'
{
  "readonly-project": {
    "id": "readonly-project",
    "description": "Read-only project",
    "state": "READ_ONLY"
  }
}`,
			expectedName: "readonly-project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if _, err := w.Write([]byte(tt.response)); err != nil {
					t.Errorf("Failed to write response: %v", err)
				}
			}))
			defer server.Close()

			provider, err := NewGerritProvider(server.URL, "testuser", "testpass")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			source := &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			}

			repos, err := provider.ListRepositories(context.Background(), source.Organization)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(repos) == 0 {
				t.Fatal("Expected at least one repository")
			}

			if repos[0].Name != tt.expectedName {
				t.Errorf("Expected repository name %s, got %s", tt.expectedName, repos[0].Name)
			}
		})
	}
}

func TestGerritProvider_CreateFork(t *testing.T) {
	provider, err := NewGerritProvider("https://gerrit.example.com", "testuser", "testpass")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	repo := &Repository{
		Name:     "test-project",
		FullName: "test-project",
		CloneURL: "https://gerrit.example.com/test-project",
	}

	// Gerrit doesn't support forking via API
	fork, err := provider.CreateFork(context.Background(), repo, "target-org")

	if err == nil {
		t.Error("Expected error for unsupported fork operation")
	}

	if fork != nil {
		t.Error("Expected nil fork result")
	}

	// Check that error message indicates unsupported operation
	if err.Error() != "forking not supported for Gerrit repositories" {
		t.Errorf("Expected specific error message, got: %s", err.Error())
	}
}

func TestGerritProvider_ProjectFiltering(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := `)]}'
{
  "active-project": {
    "id": "active-project",
    "description": "Active project",
    "state": "ACTIVE"
  },
  "hidden-project": {
    "id": "hidden-project",
    "description": "Hidden project",
    "state": "HIDDEN"
  },
  "readonly-project": {
    "id": "readonly-project",
    "description": "Read-only project",
    "state": "READ_ONLY"
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(response)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	provider, err := NewGerritProvider(server.URL, "testuser", "testpass")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	source := &SourceInfo{
		Provider: "gerrit",
		Host:     "gerrit.example.com",
	}

	repos, err := provider.ListRepositories(context.Background(), source.Organization)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should return all projects regardless of state
	if len(repos) != 3 {
		t.Errorf("Expected 3 repositories, got %d", len(repos))
	}

	// Verify all projects are included
	projectNames := make(map[string]bool)
	for _, repo := range repos {
		projectNames[repo.Name] = true
	}

	expectedProjects := []string{"active-project", "hidden-project", "readonly-project"}
	for _, expected := range expectedProjects {
		if !projectNames[expected] {
			t.Errorf("Expected project %s not found in results", expected)
		}
	}
}

func TestNewGerritProvider(t *testing.T) {
	tests := []struct {
		name        string
		source      *SourceInfo
		username    string
		password    string
		expectError bool
	}{
		{
			name: "valid gerrit source with credentials",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			username: "testuser",
			password: "testpass",
		},
		{
			name: "gerrit source without credentials",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com",
			},
			username: "",
			password: "",
		},
		{
			name: "gerrit source with custom port",
			source: &SourceInfo{
				Provider: "gerrit",
				Host:     "gerrit.example.com:8080",
			},
			username: "testuser",
			password: "testpass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewGerritProvider("https://"+tt.source.Host, tt.username, tt.password)

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

			if provider.username != tt.username {
				t.Errorf("Expected username %s, got %s", tt.username, provider.username)
			}
			if provider.password != tt.password {
				t.Errorf("Expected password %s, got %s", tt.password, provider.password)
			}
		})
	}
}

func TestGerritProvider_ShouldTryUnauthenticated(t *testing.T) {
	provider, err := NewGerritProvider("https://gerrit.example.com", "", "")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error should not trigger unauthenticated",
			err:      nil,
			expected: false,
		},
		{
			name:     "missing security prefix error should trigger unauthenticated",
			err:      fmt.Errorf("failed to parse response: invalid Gerrit response: missing security prefix"),
			expected: true,
		},
		{
			name:     "HTML response error should trigger unauthenticated",
			err:      fmt.Errorf("received HTML response instead of JSON"),
			expected: true,
		},
		{
			name:     "authentication error should trigger unauthenticated",
			err:      fmt.Errorf("authentication failed"),
			expected: true,
		},
		{
			name:     "unauthorized error should trigger unauthenticated",
			err:      fmt.Errorf("request unauthorized"),
			expected: true,
		},
		{
			name:     "forbidden error should trigger unauthenticated",
			err:      fmt.Errorf("access forbidden"),
			expected: true,
		},
		{
			name:     "case insensitive missing security prefix should trigger unauthenticated",
			err:      fmt.Errorf("Missing Security Prefix detected"),
			expected: true,
		},
		{
			name:     "partial missing security prefix should trigger unauthenticated",
			err:      fmt.Errorf("some other error: missing security prefix in response"),
			expected: true,
		},
		{
			name:     "generic network error should not trigger unauthenticated",
			err:      fmt.Errorf("network connection failed"),
			expected: false,
		},
		{
			name:     "timeout error should not trigger unauthenticated",
			err:      fmt.Errorf("request timeout"),
			expected: false,
		},
		{
			name:     "JSON parse error should not trigger unauthenticated",
			err:      fmt.Errorf("invalid JSON format"),
			expected: false,
		},
		{
			name:     "combined errors with missing security prefix should trigger unauthenticated",
			err:      fmt.Errorf("failed to connect: server returned missing security prefix"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := provider.shouldTryUnauthenticated(tt.err)
			if result != tt.expected {
				t.Errorf("shouldTryUnauthenticated(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}
