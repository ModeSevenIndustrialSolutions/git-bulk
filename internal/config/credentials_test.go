// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialsLoader(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "credentials-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test credentials file
	credentialsPath := filepath.Join(tempDir, ".credentials")
	credentialsContent := `# Test credentials file
GITHUB_TOKEN="test_github_token"
GITLAB_TOKEN=test_gitlab_token
# Comment line
GERRIT_USERNAME="test_user"

EMPTY_LINE_ABOVE=value
`

	if err := os.WriteFile(credentialsPath, []byte(credentialsContent), 0600); err != nil {
		t.Fatalf("Failed to write test credentials file: %v", err)
	}

	// Test loading credentials
	loader := NewCredentialsLoader(credentialsPath)
	if err := loader.LoadCredentials(); err != nil {
		t.Fatalf("Failed to load credentials: %v", err)
	}

	// Test getting credentials from file
	tests := []struct {
		key      string
		expected string
	}{
		{"GITHUB_TOKEN", "test_github_token"},
		{"GITLAB_TOKEN", "test_gitlab_token"},
		{"GERRIT_USERNAME", "test_user"},
		{"EMPTY_LINE_ABOVE", "value"},
		{"NONEXISTENT", ""},
	}

	for _, test := range tests {
		actual := loader.GetCredential(test.key)
		if actual != test.expected {
			t.Errorf("GetCredential(%s) = %s, expected %s", test.key, actual, test.expected)
		}
	}
}

func TestCredentialsLoaderEnvironmentPriority(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "credentials-env-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test credentials file
	credentialsPath := filepath.Join(tempDir, ".credentials")
	credentialsContent := `GITHUB_TOKEN="file_token"`

	if err := os.WriteFile(credentialsPath, []byte(credentialsContent), 0600); err != nil {
		t.Fatalf("Failed to write test credentials file: %v", err)
	}

	// Set environment variable
	oldEnv := os.Getenv("GITHUB_TOKEN")
	defer func() {
		if oldEnv == "" {
			os.Unsetenv("GITHUB_TOKEN")
		} else {
			os.Setenv("GITHUB_TOKEN", oldEnv)
		}
	}()

	os.Setenv("GITHUB_TOKEN", "env_token")

	// Test that environment variable takes priority
	loader := NewCredentialsLoader(credentialsPath)
	if err := loader.LoadCredentials(); err != nil {
		t.Fatalf("Failed to load credentials: %v", err)
	}

	actual := loader.GetCredential("GITHUB_TOKEN")
	if actual != "env_token" {
		t.Errorf("Expected environment variable to take priority, got %s", actual)
	}
}

func TestCredentialsLoaderNonExistentFile(t *testing.T) {
	loader := NewCredentialsLoader("/nonexistent/path/.credentials")

	// Should not error when file doesn't exist
	if err := loader.LoadCredentials(); err != nil {
		t.Errorf("LoadCredentials should not error for non-existent file: %v", err)
	}

	// Should return empty string for non-existent credentials
	actual := loader.GetCredential("GITHUB_TOKEN")
	if actual != "" {
		t.Errorf("Expected empty string for non-existent credential, got %s", actual)
	}
}

func TestCredentialsLoaderInvalidFormat(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "credentials-invalid-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test credentials file with invalid format
	credentialsPath := filepath.Join(tempDir, ".credentials")
	credentialsContent := `GITHUB_TOKEN="valid_token"
INVALID_LINE_WITHOUT_EQUALS
GITLAB_TOKEN="valid_gitlab_token"`

	if err := os.WriteFile(credentialsPath, []byte(credentialsContent), 0600); err != nil {
		t.Fatalf("Failed to write test credentials file: %v", err)
	}

	loader := NewCredentialsLoader(credentialsPath)
	err = loader.LoadCredentials()

	// Should error on invalid format
	if err == nil {
		t.Error("Expected error for invalid credentials file format")
	}
}

func TestListCredentials(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "credentials-list-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test credentials file
	credentialsPath := filepath.Join(tempDir, ".credentials")
	credentialsContent := `GITHUB_TOKEN="test_token"
GITLAB_TOKEN="test_gitlab_token"`

	if err := os.WriteFile(credentialsPath, []byte(credentialsContent), 0600); err != nil {
		t.Fatalf("Failed to write test credentials file: %v", err)
	}

	loader := NewCredentialsLoader(credentialsPath)
	if err := loader.LoadCredentials(); err != nil {
		t.Fatalf("Failed to load credentials: %v", err)
	}

	credentials := loader.ListCredentials()

	// Check that GitHub and GitLab tokens are detected
	if !credentials["GITHUB_TOKEN"] {
		t.Error("Expected GITHUB_TOKEN to be detected")
	}
	if !credentials["GITLAB_TOKEN"] {
		t.Error("Expected GITLAB_TOKEN to be detected")
	}
	if credentials["GERRIT_USERNAME"] {
		t.Error("Expected GERRIT_USERNAME to not be detected")
	}
}
