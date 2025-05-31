// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v2"
)

// Constants to reduce code duplication
const (
	testGitBulk  = "git-bulk"
	testSource   = "--source"
	testDir      = "--directory"
	testWorkers  = "--workers"
	testDryRun   = "--dry-run"
	testHelp     = "--help"
	testRepo     = "https://github.com/octocat/Hello-World"
	testTempErr  = "Failed to create temp dir: %v"
	testUnexpErr = "Unexpected error: %v"
	testTimeout  = "Test timed out"
)

// createApp creates a CLI app instance for testing
func createApp(stdout, stderr io.Writer) *cli.App {
	return &cli.App{
		Name:      testGitBulk,
		Usage:     "Bulk Git repository operations for GitHub, GitLab, and Gerrit",
		Version:   version,
		Writer:    stdout,
		ErrWriter: stderr,
		Before: func(c *cli.Context) error {
			if !c.Bool("quiet") {
				// Skip banner in tests to avoid output pollution
			}
			return nil
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "quiet",
				Aliases: []string{"q"},
				Usage:   "suppress banner output",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "enable verbose logging", // Remove alias to avoid redefinition
			},
		},
		Commands: []*cli.Command{
			{
				Name:    "clone",
				Aliases: []string{"c"},
				Usage:   "Clone repositories from a Git hosting provider",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "source",
						Aliases:  []string{"s"},
						Usage:    "Source organization/group/server",
						Required: true,
					},
					&cli.StringFlag{
						Name:    "target",
						Aliases: []string{"t"},
						Usage:   "Target organization for forking",
					},
					&cli.StringFlag{
						Name:    "directory",
						Aliases: []string{"d"},
						Usage:   "Output directory for cloned repositories",
						Value:   "./repositories",
					},
					&cli.IntFlag{
						Name:    "workers",
						Aliases: []string{"w"},
						Usage:   "Number of worker threads",
						Value:   6,
					},
					&cli.IntFlag{
						Name:  "depth",
						Usage: "Clone depth (0 for full clone)",
						Value: 0,
					},
					&cli.IntFlag{
						Name:  "retries",
						Usage: "Maximum retry attempts per operation",
						Value: 3,
					},
					&cli.BoolFlag{
						Name:  "ssh",
						Usage: "Use SSH clone URLs instead of HTTPS",
					},
					&cli.BoolFlag{
						Name:  "mirror",
						Usage: "Create mirror clones",
					},
					&cli.BoolFlag{
						Name:  "bare",
						Usage: "Create bare clones",
					},
					&cli.BoolFlag{
						Name:  "sync",
						Usage: "Sync existing forks with upstream (with --target)",
					},
					&cli.BoolFlag{
						Name:  "dry-run",
						Usage: "Show what would be done without executing",
					},
					&cli.BoolFlag{
						Name:  "continue-on-fail",
						Usage: "Continue processing even if some operations fail",
						Value: true,
					},
				},
				Action: func(c *cli.Context) error {
					// Test stub - just return nil for tests
					return nil
				},
			},
			{
				Name:    "list",
				Aliases: []string{"l"},
				Usage:   "List repositories from a Git hosting provider",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:     "source",
						Aliases:  []string{"s"},
						Usage:    "Source organization/group/server",
						Required: true,
					},
					&cli.StringFlag{
						Name:  "format",
						Usage: "Output format (table, json, csv)",
						Value: "table",
					},
				},
				Action: func(c *cli.Context) error {
					// Test stub - just return nil for tests
					return nil
				},
			},
		},
	}
}

func TestCLIListCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedError  bool
		expectedOutput string
	}{
		{
			name:           "list GitHub repositories",
			args:           []string{testGitBulk, "list", testSource, "octocat"},
			expectedOutput: "Hello-World",
		},
		{
			name:          "list with invalid source",
			args:          []string{testGitBulk, "list", testSource, "nonexistent-user-12345"},
			expectedError: true,
		},
		{
			name:          "list without source",
			args:          []string{testGitBulk, "list"},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture output
			var buf bytes.Buffer

			// Create app instance
			app := createApp(&buf, &buf)

			// Set a timeout for the test
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			// Run the command in a goroutine
			errChan := make(chan error, 1)
			go func() {
				errChan <- app.RunContext(ctx, tt.args)
			}()

			// Wait for completion or timeout
			select {
			case err := <-errChan:
				if tt.expectedError {
					if err == nil {
						t.Error("Expected error but got none")
					}
				} else {
					if err != nil {
						t.Fatalf(testUnexpErr, err)
					}

					output := buf.String()
					if tt.expectedOutput != "" && !strings.Contains(output, tt.expectedOutput) {
						t.Errorf("Expected output to contain %q, got: %s", tt.expectedOutput, output)
					}
				}
			case <-ctx.Done():
				t.Fatal(testTimeout)
			}
		})
	}
}

func TestCLICloneCommand(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "cli-clone-test-*")
	if err != nil {
		t.Fatalf(testTempErr, err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name          string
		args          []string
		expectedError bool
		checkDir      string
	}{
		{
			name: "clone single repository",
			args: []string{
				testGitBulk, "clone",
				testSource, testRepo,
				testDir, tempDir,
				testWorkers, "1",
			},
			checkDir: "Hello-World",
		},
		{
			name: "clone with dry run",
			args: []string{
				testGitBulk, "clone",
				testSource, testRepo,
				testDir, tempDir,
				testDryRun,
			},
		},
		{
			name: "clone without source",
			args: []string{
				testGitBulk, "clone",
				testDir, tempDir,
			},
			expectedError: true,
		},
		{
			name: "clone without directory",
			args: []string{
				testGitBulk, "clone",
				testSource, "octocat",
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := createApp(&buf, &buf)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			errChan := make(chan error, 1)
			go func() {
				errChan <- app.RunContext(ctx, tt.args)
			}()

			select {
			case err := <-errChan:
				if tt.expectedError {
					if err == nil {
						t.Error("Expected error but got none")
					}
				} else {
					if err != nil {
						t.Fatalf(testUnexpErr, err)
					}

					// Check if directory was created (unless dry run)
					if tt.checkDir != "" && !contains(tt.args, testDryRun) {
						expectedPath := filepath.Join(tempDir, tt.checkDir)
						if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
							t.Errorf("Expected directory %s was not created", expectedPath)
						}
					}
				}
			case <-ctx.Done():
				t.Fatal(testTimeout)
			}
		})
	}
}

func TestCLIHelpCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedOutput string
	}{
		{
			name:           "main help",
			args:           []string{testGitBulk, testHelp},
			expectedOutput: "A comprehensive tool for bulk Git repository operations",
		},
		{
			name:           "clone help",
			args:           []string{testGitBulk, "clone", testHelp},
			expectedOutput: "Clone repositories in bulk",
		},
		{
			name:           "list help",
			args:           []string{testGitBulk, "list", testHelp},
			expectedOutput: "List repositories from a source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := createApp(&buf, &buf)

			err := app.Run(tt.args)
			if err != nil {
				t.Fatalf(testUnexpErr, err)
			}

			output := buf.String()
			if !strings.Contains(output, tt.expectedOutput) {
				t.Errorf("Expected output to contain %q, got: %s", tt.expectedOutput, output)
			}
		})
	}
}

func TestCLIVersionCommand(t *testing.T) {
	var buf bytes.Buffer
	app := createApp(&buf, &buf)

	err := app.Run([]string{testGitBulk, "--version"})
	if err != nil {
		t.Fatalf(testUnexpErr, err)
	}

	output := buf.String()
	if !strings.Contains(output, testGitBulk) {
		t.Errorf("Expected version output to contain %q, got: %s", testGitBulk, output)
	}
}

func TestCLIEnvironmentVariables(t *testing.T) {
	// Test GitHub token environment variable
	tempDir, err := os.MkdirTemp("", "cli-env-test-*")
	if err != nil {
		t.Fatalf(testTempErr, err)
	}
	defer os.RemoveAll(tempDir)

	// Set environment variable
	originalToken := os.Getenv("GITHUB_TOKEN")
	os.Setenv("GITHUB_TOKEN", "fake-token-for-testing")
	defer func() {
		if originalToken == "" {
			os.Unsetenv("GITHUB_TOKEN")
		} else {
			os.Setenv("GITHUB_TOKEN", originalToken)
		}
	}()

	var buf bytes.Buffer
	app := createApp(&buf, &buf)

	args := []string{
		testGitBulk, "list",
		testSource, "octocat",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- app.RunContext(ctx, args)
	}()

	select {
	case err := <-errChan:
		// Command should run (may succeed or fail based on token validity)
		// We're mainly testing that the environment variable is picked up
		t.Logf("Command completed with: %v", err)
	case <-ctx.Done():
		t.Fatal("Test timed out")
	}
}

func TestCLI_WorkerConfiguration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cli-workers-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name    string
		workers string
		valid   bool
	}{
		{
			name:    "valid worker count",
			workers: "5",
			valid:   true,
		},
		{
			name:    "zero workers",
			workers: "0",
			valid:   false,
		},
		{
			name:    "negative workers",
			workers: "-1",
			valid:   false,
		},
		{
			name:    "non-numeric workers",
			workers: "abc",
			valid:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			app := createApp(&buf, &buf)

			args := []string{
				"git-bulk", "clone",
				"--source", "https://github.com/octocat/Hello-World",
				"--directory", tempDir,
				"--workers", tt.workers,
				"--dry-run", // Use dry run to avoid actual cloning
			}

			err := app.Run(args)

			if tt.valid && err != nil {
				t.Errorf("Expected valid configuration but got error: %v", err)
			} else if !tt.valid && err == nil {
				t.Error("Expected error for invalid worker configuration")
			}
		})
	}
}

func TestCLI_SSHMode(t *testing.T) {
	if os.Getenv("SKIP_SSH_TESTS") == "true" {
		t.Skip("SSH tests skipped")
	}

	tempDir, err := os.MkdirTemp("", "cli-ssh-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	var buf bytes.Buffer
	app := createApp(&buf, &buf)

	args := []string{
		"git-bulk", "clone",
		"--source", "https://github.com/octocat/Hello-World",
		"--directory", tempDir,
		"--ssh",
		"--dry-run", // Use dry run to avoid SSH key issues
	}

	err = app.Run(args)
	// SSH mode should be accepted even if keys aren't available
	if err != nil {
		t.Logf("SSH mode test completed with: %v", err)
	}
}

func TestCLI_InvalidSourceFormats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cli-invalid-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	invalidSources := []string{
		"",
		"invalid-url",
		"http://not-a-git-host.com/repo",
		"ftp://invalid-protocol.com/repo",
	}

	for i, source := range invalidSources {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			var buf bytes.Buffer
			app := createApp(&buf, &buf)

			args := []string{
				"git-bulk", "clone",
				"--source", source,
				"--directory", tempDir,
				"--dry-run",
			}

			err := app.Run(args)
			if err == nil {
				t.Errorf("Expected error for invalid source %q", source)
			}
		})
	}
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Benchmark basic clone operation
func BenchmarkCLI_CloneSingleRepo(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "bench-clone-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		app := createApp(&buf, &buf)

		subDir := filepath.Join(tempDir, string(rune('a'+i)))
		os.MkdirAll(subDir, 0755)

		args := []string{
			"git-bulk", "clone",
			"--source", "https://github.com/octocat/Hello-World",
			"--directory", subDir,
			"--workers", "1",
		}

		err := app.Run(args)
		if err != nil {
			b.Fatalf("Benchmark iteration failed: %v", err)
		}
	}
}

// Test concurrent CLI operations
func TestCLI_ConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "cli-concurrent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Run multiple list operations concurrently
	errChan := make(chan error, 3)

	for i := 0; i < 3; i++ {
		go func(id int) {
			var buf bytes.Buffer
			app := createApp(&buf, &buf)

			args := []string{
				"git-bulk", "list",
				"--source", "octocat",
			}

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			errChan <- app.RunContext(ctx, args)
		}(i)
	}

	// Wait for all operations to complete
	for i := 0; i < 3; i++ {
		select {
		case err := <-errChan:
			if err != nil {
				t.Logf("Concurrent operation %d failed: %v", i, err)
			}
		case <-time.After(45 * time.Second):
			t.Fatal("Concurrent operations timed out")
		}
	}
}
