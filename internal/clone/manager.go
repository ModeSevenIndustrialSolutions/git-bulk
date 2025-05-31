// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package clone provides functionality for cloning and managing Git repositories in bulk.
package clone

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modesevenindustrialsolutions/go-bulk-git/internal/provider"
	"github.com/modesevenindustrialsolutions/go-bulk-git/internal/worker"
)

// Config holds configuration for clone operations
type Config struct {
	WorkerConfig   *worker.Config
	OutputDir      string
	UseSSH         bool
	Mirror         bool
	Bare           bool
	Depth          int
	Verbose        bool
	DryRun         bool
	ContinueOnFail bool
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		WorkerConfig:   worker.DefaultConfig(),
		OutputDir:      "./repositories",
		UseSSH:         false,
		Mirror:         false,
		Bare:           false,
		Depth:          0, // Full clone
		Verbose:        false,
		DryRun:         false,
		ContinueOnFail: true,
	}
}

// Manager handles bulk cloning operations
type Manager struct {
	config       *Config
	pool         *worker.Pool
	provider     provider.Provider
	sourceInfo   *provider.SourceInfo
	results      chan *Result
	mu           sync.RWMutex
	cloneResults map[string]*Result
}

// Result represents the result of a clone operation
type Result struct {
	Repository *provider.Repository
	LocalPath  string
	Error      error
	Duration   time.Duration
	JobID      string
}

// NewManager creates a new clone manager
func NewManager(config *Config, prov provider.Provider, sourceInfo *provider.SourceInfo) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	return &Manager{
		config:       config,
		pool:         worker.NewPool(config.WorkerConfig),
		provider:     prov,
		sourceInfo:   sourceInfo,
		results:      make(chan *Result, config.WorkerConfig.QueueSize),
		cloneResults: make(map[string]*Result),
	}
}

// CloneAll clones all repositories from the configured source
func (m *Manager) CloneAll(ctx context.Context) error {
	m.logf("Starting bulk clone operation from %s", m.sourceInfo.Organization)

	// Start the worker pool
	m.pool.Start()
	defer m.pool.Stop()

	// Start result collector
	go m.resultCollector()

	// Get repositories with rate limit handling
	repos, err := m.provider.ListRepositories(ctx, m.sourceInfo.Organization)
	if err != nil {
		// Check if this is a rate limit error
		if rateLimitErr, isRateLimit := provider.IsRateLimitError(err); isRateLimit {
			m.logf("Rate limit hit when listing repositories. Waiting %v before retrying...", rateLimitErr.RetryAfter)
			
			// Wait for the specified retry period
			select {
			case <-time.After(rateLimitErr.RetryAfter):
				// Retry the operation
				m.logf("Retrying repository listing for: %s", m.sourceInfo.Organization)
				repos, err = m.provider.ListRepositories(ctx, m.sourceInfo.Organization)
				if err != nil {
					return fmt.Errorf("failed to list repositories after rate limit retry: %w", err)
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			return fmt.Errorf("failed to list repositories: %w", err)
		}
	}

	m.logf("Found %d repositories to clone", len(repos))

	// Create output directory
	if err := m.createOutputDir(); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Submit clone jobs
	for _, repo := range repos {
		if err := m.submitCloneJob(ctx, repo); err != nil {
			m.logf("Failed to submit clone job for %s: %v", repo.Name, err)
			if !m.config.ContinueOnFail {
				return err
			}
		}
	}

	// Wait for all jobs to complete
	m.waitForCompletion(ctx)

	// Print summary
	m.printSummary()

	return nil
}

// CloneRepository clones a single repository
func (m *Manager) CloneRepository(ctx context.Context, repo *provider.Repository) error {
	m.logf("Cloning single repository: %s", repo.Name)

	// Start the worker pool
	m.pool.Start()
	defer m.pool.Stop()

	// Start result collector
	go m.resultCollector()

	// Create output directory
	if err := m.createOutputDir(); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Submit clone job
	if err := m.submitCloneJob(ctx, repo); err != nil {
		return fmt.Errorf("failed to submit clone job: %w", err)
	}

	// Wait for completion
	m.waitForCompletion(ctx)

	// Check result
	result := m.getCloneResult(repo.ID)
	if result != nil && result.Error != nil {
		return result.Error
	}

	return nil
}

// CloneRepositories clones a specific list of repositories
func (m *Manager) CloneRepositories(ctx context.Context, repos []*provider.Repository, outputDir string, dryRun bool, useSSH bool) ([]*Result, error) {
	m.logf("Starting bulk clone operation for %d repositories", len(repos))

	// Override config settings for this operation
	originalOutputDir := m.config.OutputDir
	originalDryRun := m.config.DryRun
	originalUseSSH := m.config.UseSSH

	m.config.OutputDir = outputDir
	m.config.DryRun = dryRun
	m.config.UseSSH = useSSH

	// Restore original settings when done
	defer func() {
		m.config.OutputDir = originalOutputDir
		m.config.DryRun = originalDryRun
		m.config.UseSSH = originalUseSSH
	}()

	// Start the worker pool
	m.pool.Start()
	defer m.pool.Stop()

	// Start result collector
	go m.resultCollector()

	// Create output directory
	if err := m.createOutputDir(); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	// Submit clone jobs
	for _, repo := range repos {
		if err := m.submitCloneJob(ctx, repo); err != nil {
			m.logf("Failed to submit clone job for %s: %v", repo.Name, err)
			if !m.config.ContinueOnFail {
				return nil, err
			}
		}
	}

	// Wait for all jobs to complete
	m.waitForCompletion(ctx)

	// Collect results
	var results []*Result
	for _, repo := range repos {
		if result := m.getCloneResult(repo.ID); result != nil {
			results = append(results, result)
		} else {
			// Create a result for repositories that didn't get processed
			results = append(results, &Result{
				Repository: repo,
				LocalPath:  m.getLocalPath(repo),
				Error:      fmt.Errorf("repository was not processed"),
				Duration:   0,
				JobID:      "",
			})
		}
	}

	// Print summary
	m.printSummary()

	return results, nil
}

func (m *Manager) submitCloneJob(_ context.Context, repo *provider.Repository) error {
	jobID := fmt.Sprintf("clone-%s", repo.ID)

	job := &worker.Job{
		ID:          jobID,
		Description: fmt.Sprintf("Clone repository %s", repo.FullName),
		Execute:     m.createCloneTask(repo),
		MaxRetries:  m.config.WorkerConfig.MaxRetries,
	}

	return m.pool.Submit(job)
}

func (m *Manager) createCloneTask(repo *provider.Repository) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		startTime := time.Now()

		result := &Result{
			Repository: repo,
			JobID:      fmt.Sprintf("clone-%s", repo.ID),
		}

		defer func() {
			result.Duration = time.Since(startTime)
			m.recordResult(result)
		}()

		// Determine clone URL
		cloneURL := m.getCloneURL(repo)
		if cloneURL == "" {
			result.Error = fmt.Errorf("no suitable clone URL found for repository %s", repo.Name)
			return result.Error
		}

		// Determine local path
		localPath := m.getLocalPath(repo)
		result.LocalPath = localPath

		// Check if repository already exists
		if m.repositoryExists(localPath) {
			if m.config.Verbose {
				m.logf("Repository %s already exists at %s, skipping", repo.Name, localPath)
			}
			return nil
		}

		// Perform dry run check
		if m.config.DryRun {
			m.logf("[DRY RUN] Would clone %s to %s", cloneURL, localPath)
			return nil
		}

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			result.Error = fmt.Errorf("failed to create parent directory: %w", err)
			return result.Error
		}

		// Clone the repository
		result.Error = m.performClone(ctx, cloneURL, localPath)
		return result.Error
	}
}

func (m *Manager) getCloneURL(repo *provider.Repository) string {
	if m.config.UseSSH && repo.SSHCloneURL != "" {
		return repo.SSHCloneURL
	}
	return repo.CloneURL
}

func (m *Manager) getLocalPath(repo *provider.Repository) string {
	// Preserve hierarchy for nested repositories (e.g., Gerrit)
	var relativePath string
	if repo.Path != "" && strings.Contains(repo.Path, "/") {
		// Use the full path for nested repositories
		relativePath = repo.Path
	} else {
		// Use organization/repository structure
		if m.sourceInfo.Organization != "" {
			relativePath = filepath.Join(m.sourceInfo.Organization, repo.Name)
		} else {
			relativePath = repo.Name
		}
	}

	return filepath.Join(m.config.OutputDir, relativePath)
}

func (m *Manager) repositoryExists(localPath string) bool {
	gitDir := filepath.Join(localPath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return true
	}

	// Check for bare repository
	if _, err := os.Stat(filepath.Join(localPath, "HEAD")); err == nil {
		return true
	}

	return false
}

func (m *Manager) performClone(ctx context.Context, cloneURL, localPath string) error {
	args := []string{"clone"}

	// Add clone options
	if m.config.Mirror {
		args = append(args, "--mirror")
	} else if m.config.Bare {
		args = append(args, "--bare")
	}

	if m.config.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", m.config.Depth))
	}

	args = append(args, cloneURL, localPath)

	m.logf("Executing: git %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "git", args...)

	// Capture output for debugging
	var output strings.Builder
	if m.config.Verbose {
		cmd.Stdout = io.MultiWriter(os.Stdout, &output)
		cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
	}

	err := cmd.Run()
	if err != nil {
		// Check for rate limiting in git output
		outputStr := output.String()
		if strings.Contains(outputStr, "rate limit") ||
			strings.Contains(outputStr, "too many requests") ||
			strings.Contains(outputStr, "abuse detection") {
			m.pool.SetRateLimited(true)
			return &provider.RateLimitError{
				RetryAfter: time.Minute * 2,
				Message:    fmt.Sprintf("Git clone rate limited: %s", outputStr),
			}
		}

		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, outputStr)
	}

	// Clear rate limiting if successful
	m.pool.SetRateLimited(false)

	m.logf("Successfully cloned %s to %s", cloneURL, localPath)
	return nil
}

func (m *Manager) createOutputDir() error {
	if m.config.DryRun {
		return nil
	}

	return os.MkdirAll(m.config.OutputDir, 0755)
}

func (m *Manager) resultCollector() {
	for result := range m.results {
		m.mu.Lock()
		m.cloneResults[result.Repository.ID] = result
		m.mu.Unlock()
	}
}

func (m *Manager) recordResult(result *Result) {
	select {
	case m.results <- result:
	default:
		// Channel full, record directly
		m.mu.Lock()
		m.cloneResults[result.Repository.ID] = result
		m.mu.Unlock()
	}
}

func (m *Manager) getCloneResult(repoID string) *Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cloneResults[repoID]
}

func (m *Manager) waitForCompletion(ctx context.Context) {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logf("Context cancelled, stopping clone operations")
			return
		case <-ticker.C:
			stats := m.pool.GetStats()
			if stats.ActiveJobs == 0 && stats.TotalJobs > 0 {
				m.logf("All clone jobs completed")
				return
			}
			m.logf("Clone progress: %d total, %d completed, %d failed, %d active",
				stats.TotalJobs, stats.CompletedJobs, stats.FailedJobs, stats.ActiveJobs)
		}
	}
}

func (m *Manager) printSummary() {
	stats := m.pool.GetStats()

	m.logf("\n=== Clone Operation Summary ===")
	m.logf("Total repositories: %d", stats.TotalJobs)
	m.logf("Successfully cloned: %d", stats.CompletedJobs)
	m.logf("Failed to clone: %d", stats.FailedJobs)
	m.logf("Retry attempts: %d", stats.RetryJobs)

	// List failed repositories
	if stats.FailedJobs > 0 {
		m.logf("\nFailed repositories:")
		m.mu.RLock()
		for _, result := range m.cloneResults {
			if result.Error != nil {
				m.logf("  - %s: %v", result.Repository.FullName, result.Error)
			}
		}
		m.mu.RUnlock()
	}
}

// GetResults returns all clone results
func (m *Manager) GetResults() map[string]*Result {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Create a copy to avoid data races
	results := make(map[string]*Result)
	for k, v := range m.cloneResults {
		results[k] = v
	}

	return results
}

// Close stops the clone manager and cleans up resources
func (m *Manager) Close() error {
	close(m.results)
	m.pool.Stop()
	return nil
}

func (m *Manager) logf(format string, args ...interface{}) {
	if m.config.Verbose {
		log.Printf("[CLONE] "+format, args...)
	}
}
