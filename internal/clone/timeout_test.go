// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package clone

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/provider"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/worker"
)

// TestManagerCloneTimeout tests that clone operations timeout correctly
func TestManagerCloneTimeout(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-timeout-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create a manager with very short timeout
	config := &Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
		CloneTimeout:   1 * time.Second,        // Very short timeout
		NetworkTimeout: 500 * time.Millisecond, // Very short network timeout
		Verbose:        true,
	}

	manager := NewManager(config, nil, nil)

	// Try to clone a repository that would normally take longer than 1 second
	// Using a non-existent but valid-looking URL to force a timeout
	repos := []*provider.Repository{
		{
			ID:       "timeout-test",
			Name:     "timeout-test",
			FullName: "test/timeout-test",
			CloneURL: "https://192.0.2.1/test/repo.git", // RFC 5737 test address
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	results, _ := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	duration := time.Since(start)

	// Should complete relatively quickly due to timeout
	if duration > 5*time.Second {
		t.Errorf("Clone operation took too long: %v (expected < 5s due to timeout)", duration)
	}

	// Should have results even with timeout
	if len(results) == 0 {
		t.Error("Expected results even with timeout")
	}

	// Should have error indicating timeout
	if len(results) > 0 && results[0].Error == nil {
		t.Error("Expected timeout error but got none")
	}

	if len(results) > 0 && results[0].Error != nil {
		t.Logf("Got expected timeout error: %v", results[0].Error)
	}

	// Verify error indicates timeout
	if len(results) > 0 && results[0].Error != nil {
		errStr := results[0].Error.Error()
		if !contains(errStr, "timeout") && !contains(errStr, "timed out") {
			t.Errorf("Expected timeout-related error, got: %v", results[0].Error)
		}
	}
}

// TestManagerStuckJobDetection tests stuck job detection and handling
func TestManagerStuckJobDetection(t *testing.T) {
	config := &worker.Config{
		WorkerCount: 2,
		MaxRetries:  1,
		QueueSize:   10,
		LogVerbose:  true,
	}

	pool := worker.NewPool(config)
	pool.Start()
	defer pool.Stop()

	// Submit a job that will hang
	hangingJob := &worker.Job{
		ID:          "hanging-job",
		Description: "Job that hangs",
		Execute: func(_ context.Context) error {
			// Simulate a hanging operation that doesn't respect context
			time.Sleep(5 * time.Second)
			return nil
		},
	}

	err := pool.Submit(hangingJob)
	if err != nil {
		t.Fatalf("Failed to submit hanging job: %v", err)
	}

	// Wait a bit for job to start
	time.Sleep(100 * time.Millisecond)

	// Check for stuck jobs
	stuckJobs := pool.GetStuckJobs(50 * time.Millisecond)
	if len(stuckJobs) == 0 {
		t.Error("Expected to find stuck jobs")
	}

	// Test force killing stuck jobs
	killedCount := pool.ForceKillStuckJobs(50 * time.Millisecond)
	if killedCount == 0 {
		t.Error("Expected to kill stuck jobs")
	}

	t.Logf("Successfully detected and killed %d stuck jobs", killedCount)
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsMiddle(s, substr))))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
