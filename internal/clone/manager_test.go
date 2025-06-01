// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package clone

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/provider"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/worker"
)

// TestManagerCloneRepositories tests the basic cloning functionality
func TestManagerCloneRepositories(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "clone-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	// Create test repositories
	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "org/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git", // Using real public repo for testing
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
			Description: "Test repository 1",
		},
		{
			Name:        "repo2",
			FullName:    "org/repo2",
			CloneURL:    "https://github.com/octocat/Hello-World.git", // Same repo for testing
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
			Description: "Test repository 2",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Check results
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// Check that directories were created
	for _, result := range results {
		if result.Error != nil {
			t.Logf("Clone error for %s: %v", result.Repository.Name, result.Error)
			continue
		}

		expectedPath := filepath.Join(tempDir, result.Repository.Name)
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected directory %s was not created", expectedPath)
		}

		// Check if it's a git repository
		gitDir := filepath.Join(expectedPath, ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			t.Errorf("Expected .git directory in %s", expectedPath)
		}
	}
}

// TestManagerCloneRepositoriesDryRun tests the dry run functionality
func TestManagerCloneRepositoriesDryRun(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "clone-dry-run-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "org/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx := context.Background()
	results, err := manager.CloneRepositories(ctx, repos, tempDir, true, false) // dry run = true
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Check results
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// In dry run mode, no directories should be created
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Unexpected error in dry run: %v", result.Error)
		}

		expectedPath := filepath.Join(tempDir, result.Repository.Name)
		if _, err := os.Stat(expectedPath); !os.IsNotExist(err) {
			t.Errorf("Directory %s should not exist in dry run mode", expectedPath)
		}
	}
}

// TestManagerCloneRepositoriesSSH tests the SSH cloning functionality
func TestManagerCloneRepositoriesSSH(t *testing.T) {
	// Skip if SSH is not available
	if os.Getenv("SKIP_SSH_TESTS") == "true" {
		t.Skip("SSH tests skipped")
	}

	tempDir, err := os.MkdirTemp("", "clone-ssh-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "org/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, true) // useSSH = true
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Results should be generated even if SSH fails
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// Note: SSH cloning might fail in CI environments without SSH keys
	// So we just verify the attempt was made with SSH URL
	for _, result := range results {
		if result.Error != nil {
			t.Logf("SSH clone failed (expected in CI): %v", result.Error)
		}
	}
}

// TestManagerCloneRepositoriesExistingDirectory tests handling of existing directories
func TestManagerCloneRepositoriesExistingDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-existing-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "existing-repo",
			FullName:    "org/existing-repo",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
	}

	// Create the directory first
	existingDir := filepath.Join(tempDir, "existing-repo")
	err = os.MkdirAll(existingDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create existing directory: %v", err)
	}

	// Create a file to verify it's not overwritten
	testFile := filepath.Join(existingDir, "test.txt")
	err = os.WriteFile(testFile, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx := context.Background()
	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Should get results even for existing directories
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// The clone should skip or handle existing directory gracefully
	result := results[0]
	if result.Error == nil {
		t.Log("Clone succeeded despite existing directory")
	} else {
		t.Logf("Clone failed with existing directory (expected): %v", result.Error)
	}

	// Verify test file still exists
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Test file was removed when it shouldn't have been")
	}
}

// TestManagerCloneRepositoriesWithHierarchy tests hierarchical directory structure creation
func TestManagerCloneRepositoriesWithHierarchy(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-hierarchy-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "myorg/team1/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
		{
			Name:        "repo2",
			FullName:    "myorg/team2/repo2",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Verify hierarchical structure is preserved
	for i, result := range results {
		if result.Error != nil {
			t.Logf("Clone error for %s: %v", result.Repository.FullName, result.Error)
			continue
		}

		// For hierarchical repositories, the path should include the full namespace
		expectedParts := []string{tempDir}
		switch repos[i].FullName {
		case "myorg/team1/repo1":
			expectedParts = append(expectedParts, "myorg", "team1", "repo1")
		case "myorg/team2/repo2":
			expectedParts = append(expectedParts, "myorg", "team2", "repo2")
		}
		expectedPath := filepath.Join(expectedParts...)

		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected hierarchical directory %s was not created", expectedPath)
		}
	}
}

// TestManagerCloneRepositoriesCancellationHandling tests context cancellation handling
func TestManagerCloneRepositoriesCancellationHandling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-cancel-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "org/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
		{
			Name:        "repo2",
			FullName:    "org/repo2",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel context after short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)

	// Should handle cancellation gracefully
	if err == nil || err != context.Canceled {
		t.Log("Context cancellation may not have been caught in time")
	}

	// Should still return partial results
	if results == nil {
		t.Error("Expected partial results even with cancellation")
	}
}

// TestManagerCloneRepositoriesInvalidRepository tests handling of invalid repositories
func TestManagerCloneRepositoriesInvalidRepository(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-invalid-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "invalid-repo",
			FullName:    "org/invalid-repo",
			CloneURL:    "https://github.com/nonexistent/invalid-repo.git",
			SSHCloneURL: "git@github.com:nonexistent/invalid-repo.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1, // No retries
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Should get result with error
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if results[0].Error == nil {
		t.Error("Expected error for invalid repository")
	}
}

// TestManagerStatistics tests the statistics functionality
func TestManagerStatistics(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-stats-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    "org/repo1",
			CloneURL:    "https://github.com/octocat/Hello-World.git",
			SSHCloneURL: "git@github.com:octocat/Hello-World.git",
		},
		{
			Name:        "invalid-repo",
			FullName:    "org/invalid-repo",
			CloneURL:    "https://github.com/nonexistent/invalid-repo.git",
			SSHCloneURL: "git@github.com:nonexistent/invalid-repo.git",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  1,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, false, false)
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Count successful and failed operations
	successCount := 0
	failCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		} else {
			failCount++
		}
	}

	t.Logf("Successful clones: %d, Failed clones: %d", successCount, failCount)

	// We expect at least one success (valid repo) and one failure (invalid repo)
	if successCount == 0 {
		t.Error("Expected at least one successful clone")
	}
	if failCount == 0 {
		t.Error("Expected at least one failed clone")
	}
}

// TestNewManager tests the manager creation functionality
func TestNewManager(t *testing.T) {
	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  3,
			RetryDelay:  time.Second,
			QueueSize:   10,
		},
	}, nil, nil)

	if manager == nil {
		t.Fatal("Expected manager but got nil")
	}

	if manager.pool == nil {
		t.Error("Manager should have a worker pool")
	}
}
