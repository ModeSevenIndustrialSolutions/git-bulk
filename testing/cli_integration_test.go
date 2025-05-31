// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	binaryName = "git-bulk"
	testTimeout = 30 * time.Second
)

// TestCLIIntegration tests the CLI functionality
func TestCLIIntegration(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, "./cmd/git-bulk")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove(binaryName)

	tests := []struct {
		name         string
		args         []string
		wantExit     int
		wantContains []string
		timeout      time.Duration
		setupEnv     map[string]string
	}{
		{
			name:         "Help command",
			args:         []string{"--help"},
			wantExit:     0,
			wantContains: []string{"git-bulk", "clone", "Usage:"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Version command",
			args:         []string{"--version"},
			wantExit:     0,
			wantContains: []string{"git-bulk version"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Invalid command",
			args:         []string{"invalid-command"},
			wantExit:     1,
			wantContains: []string{"unknown command"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Clone help",
			args:         []string{"clone", "--help"},
			wantExit:     0,
			wantContains: []string{"Clone repositories", "source", "Examples:"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Clone without source",
			args:         []string{"clone"},
			wantExit:     1,
			wantContains: []string{"accepts 1 arg"},
			timeout:      5 * time.Second,
		},
		{
			name:         "GitHub dry run",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--verbose"},
			wantExit:     0,
			wantContains: []string{"Using provider:", "github", "DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "GitLab dry run",
			args:         []string{"clone", "gitlab.com/gitlab-org", "--dry-run"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "Invalid source format",
			args:         []string{"clone", "invalid-source", "--dry-run"},
			wantExit:     1,
			wantContains: []string{"failed to get provider"},
			timeout:      testTimeout,
		},
		{
			name:         "Worker configuration",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--workers", "8"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "SSH option",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--ssh"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "Max repos limit",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--max-repos", "5"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "Timeout configuration",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--timeout", "10s"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
		},
		{
			name:         "GitHub with token (if available)",
			args:         []string{"clone", "github.com/octocat", "--dry-run", "--verbose"},
			wantExit:     0,
			wantContains: []string{"DRY RUN"},
			timeout:      testTimeout,
			setupEnv: map[string]string{
				"GITHUB_TOKEN": os.Getenv("GITHUB_TOKEN"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Skip tests that require tokens if not available
			if tt.setupEnv != nil {
				if token := tt.setupEnv["GITHUB_TOKEN"]; token == "" {
					t.Skip("GITHUB_TOKEN not set, skipping test")
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "./"+binaryName, tt.args...)

			// Set up environment variables
			if tt.setupEnv != nil {
				env := os.Environ()
				for k, v := range tt.setupEnv {
					if v != "" {
						env = append(env, fmt.Sprintf("%s=%s", k, v))
					}
				}
				cmd.Env = env
			}

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			// Check exit code
			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				} else {
					t.Fatalf("Command failed with error: %v", err)
				}
			}

			if exitCode != tt.wantExit {
				t.Errorf("Expected exit code %d, got %d", tt.wantExit, exitCode)
				t.Logf("Stdout: %s", stdout.String())
				t.Logf("Stderr: %s", stderr.String())
			}

			// Check output contains expected strings
			output := stdout.String() + stderr.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(output, want) {
					t.Errorf("Expected output to contain %q, but it didn't", want)
					t.Logf("Full output: %s", output)
				}
			}
		})
	}
}

// TestActualClone tests actual repository cloning (requires network)
func TestActualClone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping actual clone test in short mode")
	}

	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, "./cmd/git-bulk")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove(binaryName)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "git-bulk-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test cloning a small public repository
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./"+binaryName,
		"clone", "github.com/octocat/Hello-World",
		"--output", tempDir,
		"--workers", "1",
		"--verbose")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Stdout: %s", stdout.String())
		t.Logf("Stderr: %s", stderr.String())
		t.Fatalf("Clone command failed: %v", err)
	}

	// Check if repository was cloned
	repoPath := filepath.Join(tempDir, "Hello-World")
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Errorf("Repository was not cloned to expected path: %s", repoPath)
	}

	// Check if it's a valid git repository
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf("Cloned repository does not contain .git directory")
	}
}

// TestProviderDetection tests provider detection logic
func TestProviderDetection(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, "./cmd/git-bulk")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove(binaryName)

	tests := []struct {
		name     string
		source   string
		wantExit int
		provider string
	}{
		{
			name:     "GitHub URL",
			source:   "github.com/owner",
			wantExit: 0,
			provider: "github",
		},
		{
			name:     "GitLab URL",
			source:   "gitlab.com/group",
			wantExit: 0,
			provider: "gitlab",
		},
		{
			name:     "GitHub HTTPS URL",
			source:   "https://github.com/owner",
			wantExit: 0,
			provider: "github",
		},
		{
			name:     "Invalid URL",
			source:   "invalid.com/owner",
			wantExit: 1,
			provider: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "./"+binaryName, "clone", tt.source, "--dry-run", "--verbose")

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}

			if exitCode != tt.wantExit {
				t.Errorf("Expected exit code %d, got %d", tt.wantExit, exitCode)
				t.Logf("Stdout: %s", stdout.String())
				t.Logf("Stderr: %s", stderr.String())
			}

			if tt.provider != "" {
				output := stdout.String() + stderr.String()
				if !strings.Contains(output, tt.provider) {
					t.Errorf("Expected output to contain provider %q", tt.provider)
					t.Logf("Full output: %s", output)
				}
			}
		})
	}
}

// TestErrorHandling tests various error conditions
func TestErrorHandling(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, "./cmd/git-bulk")
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	defer os.Remove(binaryName)

	tests := []struct {
		name         string
		args         []string
		wantExit     int
		wantContains []string
	}{
		{
			name:         "Invalid worker count",
			args:         []string{"clone", "github.com/octocat", "--workers", "-1", "--dry-run"},
			wantExit:     1,
			wantContains: []string{"invalid"},
		},
		{
			name:         "Invalid timeout",
			args:         []string{"clone", "github.com/octocat", "--timeout", "invalid", "--dry-run"},
			wantExit:     1,
			wantContains: []string{"invalid"},
		},
		{
			name:         "Very short timeout",
			args:         []string{"clone", "github.com/octocat", "--timeout", "1ms", "--dry-run"},
			wantExit:     1,
			wantContains: []string{"timeout", "context deadline exceeded"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()

			cmd := exec.CommandContext(ctx, "./"+binaryName, tt.args...)

			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			err := cmd.Run()

			exitCode := 0
			if err != nil {
				if exitError, ok := err.(*exec.ExitError); ok {
					exitCode = exitError.ExitCode()
				}
			}

			if exitCode != tt.wantExit {
				t.Errorf("Expected exit code %d, got %d", tt.wantExit, exitCode)
				t.Logf("Stdout: %s", stdout.String())
				t.Logf("Stderr: %s", stderr.String())
			}

			output := stdout.String() + stderr.String()
			for _, want := range tt.wantContains {
				if !strings.Contains(strings.ToLower(output), strings.ToLower(want)) {
					t.Errorf("Expected output to contain %q, but it didn't", want)
					t.Logf("Full output: %s", output)
				}
			}
		})
	}
}
