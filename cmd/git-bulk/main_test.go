// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Constants to reduce code duplication
const (
	testRepo       = "https://github.com/octocat/Hello-World"
	testTempErr    = "Failed to create temp dir: %v"
	testUnexpErr   = "Unexpected error: %v"
	testTimeout    = "Test timed out"
	testTempRemErr = "Failed to remove temp dir: %v"
)

// isRateLimitError checks if an error is due to API rate limiting
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "API rate limit exceeded")
}

// runTestWithTimeout runs a test function with a timeout and proper error handling
func runTestWithTimeout(t *testing.T, testFunc func() error, expectError bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- testFunc()
	}()

	select {
	case err := <-errChan:
		if expectError {
			if err == nil {
				t.Error("Expected error but got none")
			}
		} else {
			if err != nil {
				if isRateLimitError(err) {
					t.Skipf("Skipping test due to API rate limiting: %v", err)
					return
				}
				t.Errorf(testUnexpErr, err)
			}
		}
	case <-ctx.Done():
		t.Error(testTimeout)
	}
}

func TestRunClone(t *testing.T) {
	tests := []struct {
		name          string
		source        string
		cfg           Config
		expectedError bool
	}{
		{
			name:   "dry run with GitHub source",
			source: "github.com/octocat",
			cfg: Config{
				OutputDir: "/tmp",
				DryRun:    true,
				Timeout:   30 * time.Second,
			},
			expectedError: false,
		},
		{
			name:   "invalid source",
			source: "",
			cfg: Config{
				OutputDir: "/tmp",
				Timeout:   30 * time.Second,
			},
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "git-bulk-test")
			if err != nil {
				t.Fatalf(testTempErr, err)
			}
			defer func() {
				if err := os.RemoveAll(tmpDir); err != nil {
					t.Logf(testTempRemErr, err)
				}
			}()

			tt.cfg.OutputDir = tmpDir

			runTestWithTimeout(t, func() error {
				return runClone(tt.cfg, tt.source)
			}, tt.expectedError)
		})
	}
}

func TestConfig(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Config
		expectedErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				OutputDir:  "/tmp",
				Workers:    4,
				MaxRetries: 3,
				Timeout:    30 * time.Second,
				DryRun:     true,
			},
			expectedErr: false,
		},
		{
			name: "invalid worker count",
			cfg: Config{
				OutputDir:  "/tmp",
				Workers:    -1,
				MaxRetries: 3,
				Timeout:    30 * time.Second,
			},
			expectedErr: false, // Worker count validation happens in worker pool
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation of config structure
			if tt.cfg.OutputDir == "" && !tt.expectedErr {
				t.Error("Expected non-empty output directory")
			}
			if tt.cfg.Timeout <= 0 && !tt.expectedErr {
				t.Error("Expected positive timeout")
			}
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}

	// Test that defaults can be set without panics
	if cfg.Workers < 0 {
		t.Error("Worker count should not be negative")
	}

	if cfg.MaxRetries < 0 {
		t.Error("Max retries should not be negative")
	}

	// Test that empty output directory can be handled
	tmpDir, err := os.MkdirTemp("", "git-bulk-default-test")
	if err != nil {
		t.Fatalf(testTempErr, err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf(testTempRemErr, err)
		}
	}()

	cfg.OutputDir = tmpDir
	cfg.DryRun = true
	cfg.Timeout = 5 * time.Second

	// This should not panic
	err = runClone(cfg, "invalid-source")
	if err == nil {
		t.Error("Expected error for invalid source")
	}
}

// Benchmark basic clone operation
func BenchmarkRunClone(b *testing.B) {
	tempDir, err := os.MkdirTemp("", "bench-clone-*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			b.Logf(testTempRemErr, err)
		}
	}()

	cfg := Config{
		OutputDir: tempDir,
		Workers:   1,
		DryRun:    true, // Use dry run to avoid actual cloning
		Timeout:   30 * time.Second,
	}

	for i := 0; i < b.N; i++ {
		subDir := filepath.Join(tempDir, "bench", string(rune('a'+i)))
		if err := os.MkdirAll(subDir, 0755); err != nil {
			b.Fatalf("Failed to create directory: %v", err)
		}
		cfg.OutputDir = subDir

		err := runClone(cfg, "github.com/octocat")
		if err != nil {
			b.Logf("Benchmark iteration %d completed with: %v", i, err)
		}
	}
}
