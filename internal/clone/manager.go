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

	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/provider"
	sshauth "github.com/ModeSevenIndustrialSolutions/git-bulk/internal/ssh"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/worker"
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
	// Timeout configuration
	CloneTimeout   time.Duration // Individual git clone operation timeout
	NetworkTimeout time.Duration // Network operation timeout for git commands
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
		CloneTimeout:     30 * time.Minute, // 30 minute timeout per clone
		NetworkTimeout:   5 * time.Minute,  // 5 minute network timeout
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
	// Track submitted jobs to avoid deadlock
	submittedJobs   int
	submittedJobsMu sync.Mutex
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

	// Submit clone jobs with batching to handle large repository counts
	submittedCount := 0
	if err := m.submitJobsInBatches(ctx, repos, &submittedCount); err != nil {
		return err
	}

	m.submittedJobsMu.Lock()
	m.submittedJobs = submittedCount
	m.submittedJobsMu.Unlock()

	m.logf("Successfully submitted %d out of %d jobs", submittedCount, len(repos))

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

	m.submittedJobsMu.Lock()
	m.submittedJobs = 1
	m.submittedJobsMu.Unlock()

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

	// Submit clone jobs with batching to handle large repository counts
	submittedCount := 0
	if err := m.submitJobsInBatches(ctx, repos, &submittedCount); err != nil {
		return nil, err
	}

	m.submittedJobsMu.Lock()
	m.submittedJobs = submittedCount
	m.submittedJobsMu.Unlock()

	m.logf("Successfully submitted %d out of %d jobs", submittedCount, len(repos))

	// Wait for all jobs to complete
	m.waitForCompletion(ctx)

	// Collect results
	var results []*Result
	for _, repo := range repos {
		if result := m.getCloneResult(repo.ID); result != nil {
			results = append(results, result)
		} else {
			// Create a result for repositories that didn't get processed
			// Provide more helpful context about why processing failed
			stats := m.pool.GetStats()
			errorMsg := fmt.Errorf("repository was not processed - possible causes: worker pool full (%d active), context timeout, or job submission failed", stats.ActiveJobs)

			results = append(results, &Result{
				Repository: repo,
				LocalPath:  m.getLocalPath(repo),
				Error:      errorMsg,
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

	// For nested repositories, use the full path but validate it
	if repo.Path != "" && strings.Contains(repo.Path, "/") {
		// Sanitize the path to prevent directory traversal attacks
		cleanPath := filepath.Clean(repo.Path)
		if strings.HasPrefix(cleanPath, "..") || strings.HasPrefix(cleanPath, "/") {
			m.logf("Warning: suspicious path detected for %s: %s, using repository name instead", repo.FullName, repo.Path)
			relativePath = repo.Name
		} else {
			relativePath = cleanPath
		}
	} else if repo.Path != "" {
		// Use the provided path as-is if it doesn't contain hierarchical separators
		relativePath = repo.Path
	} else if strings.Contains(repo.FullName, "/") && strings.Count(repo.FullName, "/") > 1 {
		// Handle deeply nested hierarchical FullName (e.g., "myorg/team1/repo1")
		// Only use hierarchical structure for deeply nested projects (more than 1 level)
		// This is common for Gerrit nested projects
		relativePath = repo.FullName
	} else {
		// Use organization/repository structure for simple cases
		if m.sourceInfo != nil && m.sourceInfo.Organization != "" {
			relativePath = filepath.Join(m.sourceInfo.Organization, repo.Name)
		} else {
			relativePath = repo.Name
		}
	}

	localPath := filepath.Join(m.config.OutputDir, relativePath)

	// Log the path mapping in verbose mode for debugging nested project issues
	if m.config.Verbose && (repo.Path != "" || strings.Contains(repo.FullName, "/")) {
		m.logf("Path mapping for %s: repo.Path='%s' FullName='%s' -> localPath='%s'", repo.FullName, repo.Path, repo.FullName, localPath)
	}

	return localPath
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

// performClone performs the actual git clone operation with enhanced error handling and timeouts
func (m *Manager) performClone(ctx context.Context, cloneURL, localPath string) error {
	// Create a timeout context for this specific clone operation
	cloneCtx, cancel := context.WithTimeout(ctx, m.config.CloneTimeout)
	defer cancel()

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

	// Add timeout configurations for git operations
	if m.config.NetworkTimeout > 0 {
		// Set git config for network timeouts
		timeoutSeconds := int(m.config.NetworkTimeout.Seconds())
		args = append(args, "-c", fmt.Sprintf("http.timeout=%d", timeoutSeconds))
		args = append(args, "-c", fmt.Sprintf("remote.origin.timeout=%d", timeoutSeconds))
	}

	args = append(args, cloneURL, localPath)

	m.logf("Executing: git %s (timeout: %v)", strings.Join(args, " "), m.config.CloneTimeout)

	cmd := exec.CommandContext(cloneCtx, "git", args...)

	// Capture output for debugging
	var output strings.Builder
	if m.config.Verbose {
		cmd.Stdout = io.MultiWriter(os.Stdout, &output)
		cmd.Stderr = io.MultiWriter(os.Stderr, &output)
	} else {
		cmd.Stdout = &output
		cmd.Stderr = &output
	}

	// Execute with timeout monitoring
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		return m.handleCloneResult(err, cloneURL, localPath, output.String())
	case <-cloneCtx.Done():
		// Timeout occurred - forcefully terminate the git process
		if cmd.Process != nil {
			m.logf("Clone timeout reached for %s, terminating git process", cloneURL)
			if err := cmd.Process.Kill(); err != nil {
				m.logf("Error killing process for %s: %v", cloneURL, err)
			}
		}
		return fmt.Errorf("clone operation timed out after %v for %s", m.config.CloneTimeout, cloneURL)
	}
}

// handleCloneResult processes the result of a git clone operation
func (m *Manager) handleCloneResult(err error, cloneURL, localPath, outputStr string) error {
	if err != nil {
		// Check for rate limiting in git output
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

		// Extract exit status for better error reporting
		exitStatus := extractExitStatus(err)
		if exitStatus != "" {
			return fmt.Errorf("git clone failed, exit status %s\nOutput: %s", exitStatus, outputStr)
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
		// Record result
		m.mu.Lock()
		m.cloneResults[result.Repository.ID] = result
		m.mu.Unlock()

		// Print real-time result with immediate feedback
		if result.Error != nil {
			errorMsg := m.formatErrorMessage(result.Error)
			fmt.Printf("❌ %s [%s]\n", result.Repository.FullName, errorMsg)
		} else {
			status := "✅"
			switch result.Status {
			case StatusSkipped:
				status = "⏭️"
			case StatusExists:
				status = "📁"
			}
			fmt.Printf("%s %s\n", status, result.Repository.FullName)
		}
	}
}

// formatErrorMessage formats an error message for concise display
func (m *Manager) formatErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// Handle common error patterns
	if strings.Contains(errStr, "repository was not processed") {
		return "not processed - check worker queue and concurrency settings"
	}

	if strings.Contains(errStr, "exists and is not empty") {
		return "failed, destination path exists and is not empty"
	}

	if strings.Contains(errStr, "authentication failed") {
		return "authentication failed"
	}

	if strings.Contains(errStr, "repository not found") {
		return "repository not found"
	}

	if strings.Contains(errStr, "network error") {
		return "network error"
	}

	// Extract git clone exit status for cleaner output
	if strings.Contains(errStr, "git clone failed") && strings.Contains(errStr, "exit status") {
		// Extract the main error without verbose git output
		lines := strings.Split(errStr, "\n")
		if len(lines) > 0 {
			firstLine := lines[0]
			if strings.Contains(firstLine, "exit status") {
				return "git clone failed, " + firstLine[strings.Index(firstLine, "exit status"):]
			}
		}
	}

	// Extract first meaningful line, truncate if too long
	firstLine := strings.Split(errStr, "\n")[0]
	if len(firstLine) > 80 {
		firstLine = firstLine[:77] + "..."
	}

	return strings.TrimSpace(firstLine)
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

// extractExitStatus extracts exit status from exec.Error for better error reporting
func extractExitStatus(err error) string {
	if err == nil {
		return ""
	}

	// Try to extract exit status from different error types
	errStr := err.Error()
	if strings.Contains(errStr, "exit status") {
		// Look for pattern "exit status X"
		parts := strings.Split(errStr, "exit status ")
		if len(parts) > 1 {
			// Extract just the numeric part
			statusPart := strings.Fields(parts[1])
			if len(statusPart) > 0 {
				return statusPart[0]
			}
		}
	}

	return ""
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
	ticker := time.NewTicker(time.Second * 2)
	defer ticker.Stop()

	lastProgress := time.Now()
	lastCompletedCount := int64(0)
	stuckDetectionThreshold := 5 * time.Minute // Consider stuck after 5 minutes without progress
	maxStuckTime := 10 * time.Minute           // Force timeout after 10 minutes of being stuck

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n⚠️  Operation cancelled by timeout")
			return
		case <-ticker.C:
			stats := m.pool.GetStats()
			currentCompleted := stats.CompletedJobs + stats.FailedJobs

			// Get the number of jobs that were actually submitted
			m.submittedJobsMu.Lock()
			expectedJobs := int64(m.submittedJobs)
			m.submittedJobsMu.Unlock()

			// Check if all submitted jobs are complete
			if stats.ActiveJobs == 0 && expectedJobs > 0 &&
				currentCompleted >= expectedJobs {
				fmt.Printf("\n🎉 All %d submitted jobs completed\n", expectedJobs)
				return
			}

			// Handle case where no jobs were submitted
			if expectedJobs == 0 && stats.ActiveJobs == 0 {
				fmt.Printf("\n⚠️  No jobs were successfully submitted\n")
				return
			}

			// Deadlock detection: Check if we're making progress
			now := time.Now()
			progressStalled := currentCompleted == lastCompletedCount
			timeSinceLastProgress := now.Sub(lastProgress)

			// Update progress tracking
			if currentCompleted > lastCompletedCount {
				lastProgress = now
				lastCompletedCount = currentCompleted
			}

			// Show periodic progress updates (every 10 seconds)
			if timeSinceLastProgress >= 10*time.Second {
				if expectedJobs > 0 {
					percentage := float64(currentCompleted) / float64(expectedJobs) * 100
					fmt.Printf("📊 Progress: %.1f%% (%d/%d completed, %d active, %d failed)\n",
						percentage, currentCompleted, expectedJobs, stats.ActiveJobs, stats.FailedJobs)
				}
				lastProgress = now
			}

			// Deadlock detection and timeout handling
			if progressStalled {
				if stats.ActiveJobs > 0 {
					// Case 1: We have active jobs but no progress
					if timeSinceLastProgress >= stuckDetectionThreshold {
						fmt.Printf("\n⚠️  Warning: No progress for %.1f minutes with %d active jobs\n",
							timeSinceLastProgress.Minutes(), stats.ActiveJobs)

						// Check for stuck jobs and provide more details
						stuckJobs := m.pool.GetStuckJobs(stuckDetectionThreshold)
						if len(stuckJobs) > 0 {
							fmt.Printf("🔍 Found %d stuck jobs:\n", len(stuckJobs))
							for i, job := range stuckJobs {
								if i < 5 { // Show first 5 stuck jobs
									fmt.Printf("  - %s (running for %v)\n", job.ID, time.Since(job.CreatedAt))
								}
							}
							if len(stuckJobs) > 5 {
								fmt.Printf("  ... and %d more stuck jobs\n", len(stuckJobs)-5)
							}
						}

						if timeSinceLastProgress >= maxStuckTime {
							fmt.Printf("❌ Deadlock detected: No progress for %.1f minutes. Forcing timeout.\n",
								timeSinceLastProgress.Minutes())

							// Force kill stuck jobs
							killedCount := m.pool.ForceKillStuckJobs(maxStuckTime)
							if killedCount > 0 {
								fmt.Printf("🔪 Forcefully terminated %d stuck jobs\n", killedCount)
							}

							fmt.Printf("Active jobs may be stuck in long-running operations.\n")
							fmt.Printf("Consider reducing timeout values or checking network connectivity.\n")
							return
						}
					}
				} else if expectedJobs > 0 && currentCompleted < expectedJobs {
					// Case 2: No active jobs but not all expected jobs completed - likely submission failures
					if timeSinceLastProgress >= 30*time.Second { // Shorter timeout for this case
						fmt.Printf("\n❌ Deadlock detected: %d/%d jobs completed but no active jobs\n",
							currentCompleted, expectedJobs)
						fmt.Printf("This suggests job submission failures or worker pool issues.\n")
						fmt.Printf("Check logs for job submission errors.\n")
						return
					}
				}
			}
		}
	}
}

func (m *Manager) printSummary() {
	stats := m.pool.GetStats()

	fmt.Printf("\n=== Clone Operation Summary ===\n")
	fmt.Printf("Total repositories: %d\n", stats.TotalJobs)
	fmt.Printf("Successfully cloned: %d\n", stats.CompletedJobs)
	fmt.Printf("Failed to clone: %d\n", stats.FailedJobs)
	fmt.Printf("Retry attempts: %d\n", stats.RetryJobs)

	// Show completion percentage
	if stats.TotalJobs > 0 {
		percentage := float64(stats.CompletedJobs) / float64(stats.TotalJobs) * 100
		fmt.Printf("Success rate: %.1f%%\n", percentage)
	}
}

// GetResults returns all clone results
func (m *Manager) GetResults() map[string]*Result {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]*Result)
	for k, v := range m.cloneResults {
		results[k] = v
	}
	return results
}

// Close stops the clone manager and cleans up resources
func (m *Manager) Close() error {
	close(m.results)
	if m.sshWrapper != nil {
		return m.sshWrapper.Cleanup()
	}
	return nil
}

// submitJobsInBatches submits jobs in batches to prevent queue overflow
func (m *Manager) submitJobsInBatches(ctx context.Context, repos []*provider.Repository, submittedCount *int) error {
	// Get queue size from worker config (default is 100)
	queueSize := m.config.WorkerConfig.QueueSize

	// Use 90% of queue size to be more aggressive
	batchSize := int(float64(queueSize) * 0.9)
	if batchSize < 20 {
		batchSize = 20 // Minimum batch size
	}

	totalRepos := len(repos)
	m.logf("Submitting %d repositories in batches of %d (queue size: %d)", totalRepos, batchSize, queueSize)

	for batchStart := 0; batchStart < totalRepos; batchStart += batchSize {
		// Handle potential context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Calculate batch end
		batchEnd := batchStart + batchSize
		if batchEnd > totalRepos {
			batchEnd = totalRepos
		}

		currentBatch := repos[batchStart:batchEnd]
		batchNum := (batchStart / batchSize) + 1
		totalBatches := (totalRepos + batchSize - 1) / batchSize

		m.logf("🚀 Starting batch %d/%d: processing repositories %d-%d",
			batchNum, totalBatches, batchStart+1, batchEnd)

		// Submit all jobs in this batch
		batchSubmitted := 0
		for _, repo := range currentBatch {
			if err := m.submitCloneJobWithRetry(ctx, repo); err != nil {
				m.logf("Failed to submit clone job for %s: %v", repo.Name, err)
				if !m.config.ContinueOnFail {
					return err
				}
			} else {
				batchSubmitted++
				*submittedCount++
			}
		}

		m.logf("📤 Batch %d/%d: submitted %d/%d jobs, waiting for completion...",
			batchNum, totalBatches, batchSubmitted, len(currentBatch))

		// Wait for sufficient queue space before submitting next batch
		// This prevents queue overflow and ensures steady progress
		m.waitForQueueSpace(ctx, batchSize)

		m.logf("✅ Batch %d/%d: ready for next batch (submitted %d/%d total)",
			batchNum, totalBatches, *submittedCount, totalRepos)
	}

	m.logf("🎯 All batches submitted: %d/%d repositories processed", *submittedCount, totalRepos)
	return nil
}

// submitCloneJobWithRetry submits a clone job with retry logic for queue full scenarios
func (m *Manager) submitCloneJobWithRetry(ctx context.Context, repo *provider.Repository) error {
	maxRetries := 10                    // Reduced retry count since we're using real batching
	baseDelay := time.Millisecond * 100 // Slightly longer delay

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := m.submitCloneJob(ctx, repo); err != nil {
			if err.Error() == "job queue is full" && attempt < maxRetries-1 {
				// With true batching, this should rarely happen
				m.logf("Queue full for %s, retrying... (attempt %d/%d)",
					repo.Name, attempt+1, maxRetries)

				// Simple linear backoff
				delay := time.Duration(attempt+1) * baseDelay
				if delay > time.Second {
					delay = time.Second // Cap at 1 second
				}

				select {
				case <-time.After(delay):
					continue
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return err
		}
		// Successfully submitted
		if attempt > 0 {
			m.logf("Successfully submitted %s after %d attempts", repo.Name, attempt+1)
		}
		return nil
	}

	return fmt.Errorf("failed to submit job for %s after %d attempts - queue consistently full", repo.Name, maxRetries)
}

// waitForQueueSpace waits until there's sufficient queue space for the next batch
func (m *Manager) waitForQueueSpace(ctx context.Context, requiredSpace int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	queueSize := m.config.WorkerConfig.QueueSize
	m.logf("🔍 Waiting for %d queue slots (queue size: %d)", requiredSpace, queueSize)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := m.pool.GetStats()

			// Estimate current queue usage: active jobs represent jobs in the queue
			// In reality, active jobs are jobs being processed, but we can use this as an approximation
			currentQueueUsage := int(stats.ActiveJobs)
			availableSpace := queueSize - currentQueueUsage

			if availableSpace >= requiredSpace {
				m.logf("✅ Queue space available: %d/%d slots free, proceeding with next batch",
					availableSpace, queueSize)
				return
			}

			m.logf("⏳ Queue space check: %d/%d slots available, need %d (active: %d, completed: %d)",
				availableSpace, queueSize, requiredSpace, stats.ActiveJobs, stats.CompletedJobs)
		}
	}
}
