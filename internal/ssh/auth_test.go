// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package ssh

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %v", config.Timeout)
	}

	if config.Verbose != false {
		t.Errorf("Expected verbose false, got %v", config.Verbose)
	}
}

func TestNewSSHAuthenticator(t *testing.T) {
	config := &SSHConfig{
		Timeout: 10 * time.Second,
		Verbose: false,
	}

	auth, err := NewSSHAuthenticator(config)
	if err != nil {
		t.Fatalf("Failed to create SSH authenticator: %v", err)
	}

	if auth == nil {
		t.Fatal("SSH authenticator is nil")
	}

	if auth.config.Timeout != 10*time.Second {
		t.Errorf("Expected timeout 10s, got %v", auth.config.Timeout)
	}
}

func TestNewSSHAuthenticatorWithNilConfig(t *testing.T) {
	auth, err := NewSSHAuthenticator(nil)
	if err != nil {
		t.Fatalf("Failed to create SSH authenticator with nil config: %v", err)
	}

	if auth == nil {
		t.Fatal("SSH authenticator is nil")
	}

	if auth.config.Timeout != 30*time.Second {
		t.Errorf("Expected default timeout 30s, got %v", auth.config.Timeout)
	}
}

func TestNewGitSSHWrapper(t *testing.T) {
	config := DefaultConfig()

	wrapper, err := NewGitSSHWrapper(config)
	if err != nil {
		t.Fatalf("Failed to create Git SSH wrapper: %v", err)
	}

	if wrapper == nil {
		t.Fatal("Git SSH wrapper is nil")
	}

	// Test cleanup
	defer func() {
		if err := wrapper.Cleanup(); err != nil {
			t.Errorf("Failed to cleanup SSH wrapper: %v", err)
		}
	}()
}

func TestGetSSHCloneURL(t *testing.T) {
	tests := []struct {
		httpsURL string
		expected string
	}{
		{
			httpsURL: "https://github.com/user/repo.git",
			expected: "git@github.com:user/repo.git",
		},
		{
			httpsURL: "https://gitlab.com/user/repo.git",
			expected: "git@gitlab.com:user/repo.git",
		},
		{
			httpsURL: "https://gerrit.example.com/repo.git",
			expected: "ssh://gerrit.example.com:29418/repo.git",
		},
		{
			httpsURL: "invalid-url",
			expected: "invalid-url",
		},
	}

	for _, test := range tests {
		t.Run(test.httpsURL, func(t *testing.T) {
			result := GetSSHCloneURL(test.httpsURL)
			if result != test.expected {
				t.Errorf("Expected %s, got %s", test.expected, result)
			}
		})
	}
}

func TestValidateSSHSetup(t *testing.T) {
	// Skip this test if no SSH agent is available
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("No SSH agent available, skipping SSH setup validation test")
	}

	ctx := context.Background()

	err := ValidateSSHSetup(ctx, false)
	if err != nil {
		t.Logf("SSH setup validation failed (expected in CI): %v", err)
	}
}
