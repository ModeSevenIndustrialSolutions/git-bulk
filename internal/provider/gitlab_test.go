// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitLabProvider_ListRepositories(t *testing.T) {
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
				}
			]`,
			mockStatusCode: http.StatusOK,
			expectedCount:  1,
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
		})
	}
}
