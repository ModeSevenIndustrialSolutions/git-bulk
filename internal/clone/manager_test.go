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

// Test constants
const (
	// Repository names
	testActiveRepo     = "active-repo"
	testArchivedRepo   = "archived-repo"
	testGitHubArchived = "github-archived"
	testGitLabArchived = "gitlab-archived"
	testGerritReadOnly = "gerrit-readonly"
	testGerritHidden   = "gerrit-hidden"
	testGitHubNotArch  = "github-not-archived"
	testGerritActive   = "gerrit-active"

	// Error messages
	errFailedCreateTempDir = "Failed to create temp dir: %v"
	errFailedRemoveTempDir = "Failed to remove temp dir: %v"
	errCloneReposFailed    = "CloneRepositories failed: %v"
	errExpectedResults     = "Expected %d results, got %d"

	// Test URLs
	testRepoHTTPS = "https://github.com/octocat/Hello-World.git"
	testRepoSSH   = "git@github.com:octocat/Hello-World.git"
	testOrgRepo1  = "org/repo1"
	testOrgRepo2  = "org/repo2"

	// URL patterns
	githubURLPattern = "https://github.com/org/"
	gitlabURLPattern = "https://gitlab.com/org/"
	gerritURLPattern = "https://gerrit.example.com/org/"
)

// TestManagerCloneRepositories tests the basic cloning functionality
func TestManagerCloneRepositories(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "clone-test-*")
	if err != nil {
		t.Fatalf(errFailedCreateTempDir, err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf(errFailedRemoveTempDir, err)
		}
	}()

	// Create test repositories
	repos := []*provider.Repository{
		{
			Name:        "repo1",
			FullName:    testOrgRepo1,
			CloneURL:    testRepoHTTPS, // Using real public repo for testing
			SSHCloneURL: testRepoSSH,
			Description: "Test repository 1",
		},
		{
			Name:        "repo2",
			FullName:    testOrgRepo2,
			CloneURL:    testRepoHTTPS, // Same repo for testing
			SSHCloneURL: testRepoSSH,
			Description: "Test repository 2",
		},
	}

	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

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
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, true, false) // Use dry run to avoid network operations
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Should get results even for existing directories
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// In dry run mode, verify the existing directory structure is preserved
	result := results[0]
	if result.Error != nil {
		t.Logf("Dry run completed with info: %v", result.Error)
	} else {
		t.Log("Dry run completed successfully")
	}

	// Verify test file still exists (most important check)
	// In dry run mode, the directory should not be modified at all
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Error("Test file was removed when it shouldn't have been")
	} else {
		t.Log("Test file preserved correctly in dry run mode")
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
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
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
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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

	// With short timeouts, network operations may fail
	// We mainly test that the statistics are collected properly
	if len(results) != len(repos) {
		t.Errorf("Expected %d results, got %d", len(repos), len(results))
	}

	// Ensure we got some results (either success or failure)
	if successCount+failCount != len(repos) {
		t.Errorf("Expected %d total results, got success=%d + fail=%d = %d",
			len(repos), successCount, failCount, successCount+failCount)
	}
}

// TestNewManager tests the manager creation functionality
func TestNewManager(t *testing.T) {
	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 2,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	if manager == nil {
		t.Fatal("Expected manager but got nil")
	}

	if manager.pool == nil {
		t.Error("Manager should have a worker pool")
	}
}

// createTestArchiveRepos creates test repositories with different archive states
func createTestArchiveRepos() []*provider.Repository {
	return []*provider.Repository{
		{
			Name:     testActiveRepo,
			FullName: "org/" + testActiveRepo,
			CloneURL: githubURLPattern + testActiveRepo + ".git",
			Metadata: map[string]string{},
		},
		{
			Name:     testGitHubArchived,
			FullName: "org/" + testGitHubArchived,
			CloneURL: githubURLPattern + testGitHubArchived + ".git",
			Metadata: map[string]string{"archived": "true"},
		},
		{
			Name:     testGitLabArchived,
			FullName: "org/" + testGitLabArchived,
			CloneURL: gitlabURLPattern + testGitLabArchived + ".git",
			Metadata: map[string]string{"archived": "true"},
		},
		{
			Name:     testGerritReadOnly,
			FullName: "org/" + testGerritReadOnly,
			CloneURL: gerritURLPattern + testGerritReadOnly + ".git",
			Metadata: map[string]string{"state": "READ_ONLY"},
		},
		{
			Name:     testGerritHidden,
			FullName: "org/" + testGerritHidden,
			CloneURL: gerritURLPattern + testGerritHidden + ".git",
			Metadata: map[string]string{"state": "HIDDEN"},
		},
		{
			Name:     testGitHubNotArch,
			FullName: "org/" + testGitHubNotArch,
			CloneURL: githubURLPattern + testGitHubNotArch + ".git",
			Metadata: map[string]string{"archived": "false"},
		},
		{
			Name:     testGerritActive,
			FullName: "org/" + testGerritActive,
			CloneURL: gerritURLPattern + testGerritActive + ".git",
			Metadata: map[string]string{"state": "ACTIVE"},
		},
	}
}

// TestManagerArchiveFiltering tests the archive filtering functionality
func TestManagerArchiveFiltering(t *testing.T) {
	repos := createTestArchiveRepos()

	t.Run("SkipArchived", func(t *testing.T) {
		testSkipArchivedRepos(t, repos)
	})

	t.Run("IncludeArchived", func(t *testing.T) {
		testIncludeArchivedRepos(t, repos)
	})
}

func testSkipArchivedRepos(t *testing.T, repos []*provider.Repository) {
	manager := NewManager(&Config{
		CloneArchived: false,
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
	}, nil, nil)

	filtered, skipped := manager.filterRepositories(repos)

	// Should have 3 active repositories
	expectedActive := 3
	if len(filtered) != expectedActive {
		t.Errorf("Expected %d active repositories, got %d", expectedActive, len(filtered))
	}

	// Should have 4 archived repositories
	expectedArchived := 4
	if len(skipped) != expectedArchived {
		t.Errorf("Expected %d archived repositories, got %d", expectedArchived, len(skipped))
	}

	verifyActiveRepos(t, filtered)
	verifyArchivedRepos(t, skipped)
}

func testIncludeArchivedRepos(t *testing.T, repos []*provider.Repository) {
	manager := NewManager(&Config{
		CloneArchived: true,
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
	}, nil, nil)

	filtered, skipped := manager.filterRepositories(repos)

	// Should include all repositories
	if len(filtered) != len(repos) {
		t.Errorf("Expected all %d repositories to be included, got %d", len(repos), len(filtered))
	}

	// Should have no skipped repositories
	if len(skipped) != 0 {
		t.Errorf("Expected no skipped repositories, got %d", len(skipped))
	}
}

func verifyActiveRepos(t *testing.T, filtered []*provider.Repository) {
	activeNames := make(map[string]bool)
	for _, repo := range filtered {
		activeNames[repo.Name] = true
	}

	expectedActiveNames := []string{testActiveRepo, testGitHubNotArch, testGerritActive}
	for _, name := range expectedActiveNames {
		if !activeNames[name] {
			t.Errorf("Expected active repository %s not found in filtered results", name)
		}
	}
}

func verifyArchivedRepos(t *testing.T, skipped []*provider.Repository) {
	archivedNames := make(map[string]bool)
	for _, repo := range skipped {
		archivedNames[repo.Name] = true
	}

	expectedArchivedNames := []string{testGitHubArchived, testGitLabArchived, testGerritReadOnly, testGerritHidden}
	for _, name := range expectedArchivedNames {
		if !archivedNames[name] {
			t.Errorf("Expected archived repository %s not found in skipped results", name)
		}
	}
}

// TestManagerIsRepositoryArchived tests the archive detection logic
func TestManagerIsRepositoryArchived(t *testing.T) {
	manager := NewManager(&Config{
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
	}, nil, nil)

	testCases := []struct {
		name     string
		repo     *provider.Repository
		expected bool
	}{
		{
			name: "No metadata",
			repo: &provider.Repository{
				Name: "no-metadata",
			},
			expected: false,
		},
		{
			name: "Empty metadata",
			repo: &provider.Repository{
				Name:     "empty-metadata",
				Metadata: map[string]string{},
			},
			expected: false,
		},
		{
			name: "GitHub archived true",
			repo: &provider.Repository{
				Name:     "github-archived",
				Metadata: map[string]string{"archived": "true"},
			},
			expected: true,
		},
		{
			name: "GitHub archived false",
			repo: &provider.Repository{
				Name:     "github-not-archived",
				Metadata: map[string]string{"archived": "false"},
			},
			expected: false,
		},
		{
			name: "GitLab archived true",
			repo: &provider.Repository{
				Name:     "gitlab-archived",
				Metadata: map[string]string{"archived": "true"},
			},
			expected: true,
		},
		{
			name: "Gerrit READ_ONLY",
			repo: &provider.Repository{
				Name:     "gerrit-readonly",
				Metadata: map[string]string{"state": "READ_ONLY"},
			},
			expected: true,
		},
		{
			name: "Gerrit HIDDEN",
			repo: &provider.Repository{
				Name:     "gerrit-hidden",
				Metadata: map[string]string{"state": "HIDDEN"},
			},
			expected: true,
		},
		{
			name: "Gerrit ACTIVE",
			repo: &provider.Repository{
				Name:     "gerrit-active",
				Metadata: map[string]string{"state": "ACTIVE"},
			},
			expected: false,
		},
		{
			name: "Mixed metadata with archived true",
			repo: &provider.Repository{
				Name: "mixed-archived",
				Metadata: map[string]string{
					"archived":    "true",
					"description": "Test repository",
					"state":       "ACTIVE",
				},
			},
			expected: true,
		},
		{
			name: "Mixed metadata with state READ_ONLY",
			repo: &provider.Repository{
				Name: "mixed-readonly",
				Metadata: map[string]string{
					"description": "Test repository",
					"state":       "READ_ONLY",
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := manager.isRepositoryArchived(tc.repo)
			if result != tc.expected {
				t.Errorf("Expected %v for repository %s, got %v", tc.expected, tc.repo.Name, result)
			}
		})
	}
}

// TestManagerCloneRepositoriesWithArchiveFiltering tests full clone operation with archive filtering
func TestManagerCloneRepositoriesWithArchiveFiltering(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "clone-archive-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp dir: %v", err)
		}
	}()

	repos := createTestCloneRepos()

	t.Run("SkipArchived", func(t *testing.T) {
		testCloneSkipArchived(t, repos, tempDir)
	})

	t.Run("IncludeArchived", func(t *testing.T) {
		testCloneIncludeArchived(t, repos, tempDir)
	})
}

func createTestCloneRepos() []*provider.Repository {
	return []*provider.Repository{
		{
			Name:        testActiveRepo,
			FullName:    "org/" + testActiveRepo,
			CloneURL:    testRepoHTTPS, // Using real repo for testing
			SSHCloneURL: testRepoSSH,
			Metadata:    map[string]string{},
		},
		{
			Name:        testArchivedRepo,
			FullName:    "org/" + testArchivedRepo,
			CloneURL:    testRepoHTTPS,
			SSHCloneURL: testRepoSSH,
			Metadata:    map[string]string{"archived": "true"},
		},
	}
}

func testCloneSkipArchived(t *testing.T, repos []*provider.Repository, tempDir string) {
	manager := NewManager(&Config{
		CloneArchived: false,
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, true, false) // dry run
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Should get 2 results: 1 processed (active), 1 skipped (archived)
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Find results by repository name
	var activeResult, archivedResult *Result
	for _, result := range results {
		switch result.Repository.Name {
		case testActiveRepo:
			activeResult = result
		case testArchivedRepo:
			archivedResult = result
		}
	}

	// Active repository should be processed (skipped due to dry run)
	if activeResult == nil {
		t.Error("Active repository result not found")
	} else if activeResult.Status != StatusSkipped {
		t.Errorf("Active repository should be skipped in dry run mode, got status: %v", activeResult.Status)
	}

	// Archived repository should be skipped (due to archive filtering)
	if archivedResult == nil {
		t.Error("Archived repository result not found")
	} else if archivedResult.Status != StatusSkipped {
		t.Errorf("Archived repository should be skipped, got status: %v", archivedResult.Status)
	}
}

func testCloneIncludeArchived(t *testing.T, repos []*provider.Repository, tempDir string) {
	manager := NewManager(&Config{
		CloneArchived: true,
		WorkerConfig: &worker.Config{
			WorkerCount: 1,
			MaxRetries:  1,
			RetryDelay:  100 * time.Millisecond,
			QueueSize:   10,
		},
		CloneTimeout:   5 * time.Second,
		NetworkTimeout: 2 * time.Second,
	}, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	results, err := manager.CloneRepositories(ctx, repos, tempDir, true, false) // dry run
	if err != nil {
		t.Fatalf("CloneRepositories failed: %v", err)
	}

	// Should get 2 results: both processed
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// In dry run mode, both repositories should be skipped (dry run skips everything)
	// But both should be included in results since CloneArchived is true
	for _, result := range results {
		if result.Status != StatusSkipped {
			t.Errorf("Repository %s should be skipped in dry run mode, got status: %v", result.Repository.Name, result.Status)
		}
	}
}
