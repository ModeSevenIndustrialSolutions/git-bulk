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
	sshauth "github.com/modesevenindustrialsolutions/go-bulk-git/internal/ssh"
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
	// Enhanced error handling options
	SkipExisting     bool // Skip existing repositories instead of failing
	ValidateClone    bool // Validate that clone was successful
	CleanupOnFailure bool // Remove failed clone directories
	MaxConcurrentOps int  // Maximum concurrent clone operations per worker
	// SSH configuration
	SSHConfig *sshauth.Config
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		WorkerConfig:     worker.DefaultConfig(),
		OutputDir:        "./repositories",
		UseSSH:           false,
		Mirror:           false,
		Bare:             false,
		Depth:            0, // Full clone
		Verbose:          false,
		DryRun:           false,
		ContinueOnFail:   true,
		SkipExisting:     true,
		ValidateClone:    true,
		CleanupOnFailure: true,
		MaxConcurrentOps: 2,
		SSHConfig:        sshauth.DefaultConfig(),
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
	stats        *OperationStats
	sshWrapper   *sshauth.GitSSHWrapper
}

// OperationStats tracks clone operation statistics
type OperationStats struct {
	mu                sync.RWMutex
	TotalRepositories int
	Successful        int
	Failed            int
	Skipped           int
	StartTime         time.Time
	EndTime           time.Time
	Errors            []error
}

// Result represents the result of a clone operation
type Result struct {
	Repository *provider.Repository
	LocalPath  string
	Error      error
	Duration   time.Duration
	JobID      string
	Status     Status
	RetryCount int
}

// Status represents the status of a clone operation
type Status string

const (
	// StatusPending indicates the operation is waiting to start
	StatusPending Status = "pending"
	// StatusRunning indicates the operation is currently running
	StatusRunning Status = "running"
	// StatusSuccess indicates the operation completed successfully
	StatusSuccess Status = "success"
	// StatusFailed indicates the operation failed
	StatusFailed Status = "failed"
	// StatusSkipped indicates the operation was skipped
	StatusSkipped Status = "skipped"
	// StatusExists indicates the target already exists
	StatusExists Status = "exists"
	// StatusValidated indicates the operation was validated
	StatusValidated Status = "validated"
)

// NewManager creates a new clone manager
func NewManager(config *Config, prov provider.Provider, sourceInfo *provider.SourceInfo) *Manager {
	if config == nil {
		config = DefaultConfig()
	}

	manager := &Manager{
		config:       config,
		pool:         worker.NewPool(config.WorkerConfig),
		provider:     prov,
		sourceInfo:   sourceInfo,
		results:      make(chan *Result, config.WorkerConfig.QueueSize),
		cloneResults: make(map[string]*Result),
		stats: &OperationStats{
			StartTime: time.Now(),
		},
	}

	// Initialize SSH wrapper if SSH is enabled
	if config.UseSSH && config.SSHConfig != nil {
		sshWrapper, err := sshauth.NewGitSSHWrapper(config.SSHConfig)
		if err != nil {
			// Log warning but continue without SSH
			fmt.Printf("Warning: SSH wrapper initialization failed: %v\n", err)
		} else {
			manager.sshWrapper = sshWrapper
		}
	}

	return manager
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
			result.Status = StatusExists
			return nil
		}

		// Perform dry run check
		if m.config.DryRun {
			m.logf("[DRY RUN] Would clone %s to %s", cloneURL, localPath)
			result.Status = StatusSkipped
			return nil
		}

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			result.Error = fmt.Errorf("failed to create parent directory: %w", err)
			return result.Error
		}

		// Clone the repository
		result.Error = m.performClone(ctx, cloneURL, localPath)
		if result.Error != nil {
			result.Status = StatusFailed
			// Perform cleanup if enabled
			if m.config.CleanupOnFailure {
				m.logf("Cleaning up partial clone at %s", localPath)
				if err := os.RemoveAll(localPath); err != nil {
					m.logf("Warning: failed to clean up %s: %v", localPath, err)
				}
			}
			return result.Error
		}

		// Validate the clone if required
		if m.config.ValidateClone {
			m.logf("Validating clone for repository %s", repo.Name)
			if err := m.validateClone(localPath); err != nil {
				result.Error = fmt.Errorf("clone validation failed: %w", err)
				result.Status = StatusFailed
				// Perform cleanup if enabled
				if m.config.CleanupOnFailure {
					m.logf("Cleaning up invalid clone at %s", localPath)
					if cleanupErr := os.RemoveAll(localPath); cleanupErr != nil {
						m.logf("Warning: failed to clean up %s: %v", localPath, cleanupErr)
					}
				}
				return result.Error
			}
			result.Status = StatusValidated
		} else {
			result.Status = StatusSuccess
		}

		m.stats.IncSuccessful()
		return nil
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
	// Check for regular repository
	gitDir := filepath.Join(localPath, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return true
	}

	// Check for bare repository
	if _, err := os.Stat(filepath.Join(localPath, "HEAD")); err == nil {
		return true
	}

	// Check if directory exists and has files (potential incomplete clone)
	if stat, err := os.Stat(localPath); err == nil && stat.IsDir() {
		if entries, err := os.ReadDir(localPath); err == nil && len(entries) > 0 {
			m.logf("Directory %s exists but is not a valid git repository", localPath)
			return m.config.SkipExisting // Return based on configuration
		}
	}

	return false
}

// performClone performs the actual git clone operation with enhanced error handling
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

	// Add progress reporting for verbose mode
	if m.config.Verbose {
		args = append(args, "--progress")
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

		// Enhanced error detection
		if strings.Contains(outputStr, "rate limit") ||
			strings.Contains(outputStr, "too many requests") ||
			strings.Contains(outputStr, "abuse detection") ||
			strings.Contains(outputStr, "API rate limit exceeded") {
			m.pool.SetRateLimited(true)
			return &provider.RateLimitError{
				RetryAfter: time.Minute * 2,
				Message:    fmt.Sprintf("Git clone rate limited: %s", outputStr),
			}
		}

		// Check for authentication errors
		if strings.Contains(outputStr, "Authentication failed") ||
			strings.Contains(outputStr, "Permission denied") ||
			strings.Contains(outputStr, "access denied") {
			return fmt.Errorf("authentication failed for %s: %w\nOutput: %s", cloneURL, err, outputStr)
		}

		// Check for network errors
		if strings.Contains(outputStr, "Could not resolve host") ||
			strings.Contains(outputStr, "Connection timed out") ||
			strings.Contains(outputStr, "Network is unreachable") {
			return fmt.Errorf("network error for %s: %w\nOutput: %s", cloneURL, err, outputStr)
		}

		// Check for repository not found
		if strings.Contains(outputStr, "Repository not found") ||
			strings.Contains(outputStr, "does not exist") ||
			strings.Contains(outputStr, "404") {
			return fmt.Errorf("repository not found: %s\nOutput: %s", cloneURL, outputStr)
		}

		// Cleanup failed clone directory if requested
		if m.config.CleanupOnFailure {
			if removeErr := os.RemoveAll(localPath); removeErr != nil {
				m.logf("Failed to cleanup directory %s after clone failure: %v", localPath, removeErr)
			}
		}

		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, outputStr)
	}

	// Clear rate limiting if successful
	m.pool.SetRateLimited(false)

	// Validate clone if requested
	if m.config.ValidateClone {
		if err := m.validateClone(localPath); err != nil {
			if m.config.CleanupOnFailure {
				if cleanupErr := os.RemoveAll(localPath); cleanupErr != nil {
					m.logf("Warning: failed to clean up %s: %v", localPath, cleanupErr)
				}
			}
			return fmt.Errorf("clone validation failed: %w", err)
		}
	}

	m.logf("Successfully cloned %s to %s", cloneURL, localPath)
	return nil
}

// validateClone checks if the cloned repository is valid
func (m *Manager) validateClone(localPath string) error {
	// Check if .git directory exists
	gitDir := filepath.Join(localPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return fmt.Errorf("no .git directory found in %s", localPath)
	}

	// Check if we can run git status
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = localPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git status failed in %s: %w", localPath, err)
	}

	// Check if we have at least one commit
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = localPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("no commits found in %s: %w", localPath, err)
	}

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

// IncSuccessful increments the successful operation counter
func (s *OperationStats) IncSuccessful() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Successful++
}

// IncFailed increments the failed operation counter and adds the error
func (s *OperationStats) IncFailed(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Failed++
	s.Errors = append(s.Errors, err)
}

// IncSkipped increments the skipped operation counter
func (s *OperationStats) IncSkipped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Skipped++
}

// SetEndTime sets the end time for the operations
func (s *OperationStats) SetEndTime(t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.EndTime = t
}

// Duration returns the total duration of the operations
func (s *OperationStats) Duration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.EndTime.Sub(s.StartTime)
}

// LogSummary logs a summary of the operation statistics
func (s *OperationStats) LogSummary() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	log.Printf("\n=== Clone Operation Statistics ===")
	log.Printf("Total repositories: %d", s.TotalRepositories)
	log.Printf("Successfully cloned: %d", s.Successful)
	log.Printf("Failed to clone: %d", s.Failed)
	log.Printf("Skipped repositories: %d", s.Skipped)
	log.Printf("Duration: %s", s.Duration())
	if s.Failed > 0 {
		log.Printf("Errors: %v", s.Errors)
	}
}
