// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package worker provides a thread pool implementation for concurrent execution of jobs.
package worker

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// JobStatus represents the current status of a job
type JobStatus int

// Job status constants
const (
	// JobPending indicates a job is waiting to be processed
	JobPending JobStatus = iota
	JobRunning
	JobCompleted
	JobFailed
	JobRetrying
)

func (s JobStatus) String() string {
	switch s {
	case JobPending:
		return "pending"
	case JobRunning:
		return "running"
	case JobCompleted:
		return "completed"
	case JobFailed:
		return "failed"
	case JobRetrying:
		return "retrying"
	default:
		return "unknown"
	}
}

// Job represents a unit of work to be executed
type Job struct {
	ID          string
	Description string
	Execute     func(ctx context.Context) error
	Status      JobStatus
	Attempts    int
	MaxRetries  int
	LastError   error
	CreatedAt   time.Time
	CompletedAt *time.Time
	mu          sync.RWMutex
}

// SetStatus safely updates the job status
func (j *Job) SetStatus(status JobStatus) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	if status == JobCompleted || status == JobFailed {
		now := time.Now()
		j.CompletedAt = &now
	}
}

// GetStatus safely retrieves the job status
func (j *Job) GetStatus() JobStatus {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.Status
}

// SetError safely sets the last error
func (j *Job) SetError(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.LastError = err
}

// GetError safely retrieves the last error
func (j *Job) GetError() error {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.LastError
}

// Config holds configuration for the worker pool
type Config struct {
	WorkerCount    int           // Number of worker goroutines
	MaxRetries     int           // Maximum retry attempts per job
	RetryDelay     time.Duration // Initial retry delay
	MaxRetryDelay  time.Duration // Maximum retry delay
	BackoffFactor  float64       // Exponential backoff multiplier
	RateLimitDelay time.Duration // Delay when rate limited
	QueueSize      int           // Size of job queue buffer
	LogVerbose     bool          // Enable verbose logging
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		WorkerCount:    6,
		MaxRetries:     3,
		RetryDelay:     time.Second,
		MaxRetryDelay:  time.Minute * 5,
		BackoffFactor:  2.0,
		RateLimitDelay: time.Minute,
		QueueSize:      100,
		LogVerbose:     false,
	}
}

// Pool manages a pool of workers that execute jobs concurrently
type Pool struct {
	config      *Config
	jobs        chan *Job
	results     chan *Job
	wg          sync.WaitGroup
	ctx         context.Context
	cancel      context.CancelFunc
	jobMap      sync.Map // map[string]*Job for job tracking
	stats       *Stats
	rateLimited int64 // atomic counter for rate limiting detection
	stopMutex   sync.Mutex
	stopped     bool
}

// Stats tracks pool statistics
type Stats struct {
	TotalJobs     int64
	CompletedJobs int64
	FailedJobs    int64
	RetryJobs     int64
	ActiveJobs    int64
	mu            sync.RWMutex
}

// GetStats returns a copy of current statistics
func (s *Stats) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Stats{
		TotalJobs:     atomic.LoadInt64(&s.TotalJobs),
		CompletedJobs: atomic.LoadInt64(&s.CompletedJobs),
		FailedJobs:    atomic.LoadInt64(&s.FailedJobs),
		RetryJobs:     atomic.LoadInt64(&s.RetryJobs),
		ActiveJobs:    atomic.LoadInt64(&s.ActiveJobs),
	}
}

// NewPool creates a new worker pool with the given configuration
func NewPool(config *Config) *Pool {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Pool{
		config:  config,
		jobs:    make(chan *Job, config.QueueSize),
		results: make(chan *Job, config.QueueSize),
		ctx:     ctx,
		cancel:  cancel,
		stats:   &Stats{},
	}
}

// Start initializes and starts the worker pool
func (p *Pool) Start() {
	p.logf("Starting worker pool with %d workers", p.config.WorkerCount)

	// Start workers
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}

	// Start result processor
	p.wg.Add(1)
	go p.resultProcessor()
}

// Stop gracefully shuts down the worker pool
func (p *Pool) Stop() {
	p.stopMutex.Lock()
	defer p.stopMutex.Unlock()

	if p.stopped {
		return // Already stopped
	}

	p.logf("Stopping worker pool...")

	// Cancel context to signal all goroutines to stop
	p.cancel()

	// Close jobs channel to signal workers to stop accepting new jobs
	close(p.jobs)

	// Wait for all goroutines to finish
	p.wg.Wait()

	// Close results channel
	close(p.results)

	p.stopped = true
	p.logf("Worker pool stopped")
}

// Submit adds a job to the worker pool
func (p *Pool) Submit(job *Job) error {
	if job.MaxRetries == 0 {
		job.MaxRetries = p.config.MaxRetries
	}
	job.CreatedAt = time.Now()
	job.SetStatus(JobPending)

	p.jobMap.Store(job.ID, job)
	atomic.AddInt64(&p.stats.TotalJobs, 1)

	select {
	case p.jobs <- job:
		p.logf("Job %s submitted: %s", job.ID, job.Description)
		return nil
	case <-p.ctx.Done():
		return fmt.Errorf("worker pool is shutting down")
	default:
		return fmt.Errorf("job queue is full")
	}
}

// GetJob retrieves a job by ID
func (p *Pool) GetJob(id string) (*Job, bool) {
	if job, ok := p.jobMap.Load(id); ok {
		return job.(*Job), true
	}
	return nil, false
}

// GetStats returns current pool statistics
func (p *Pool) GetStats() Stats {
	return p.stats.GetStats()
}

// worker is the main worker goroutine that processes jobs
func (p *Pool) worker(id int) {
	defer p.wg.Done()
	p.logf("Worker %d started", id)

	for {
		select {
		case job, ok := <-p.jobs:
			if !ok {
				p.logf("Worker %d stopping - job channel closed", id)
				return
			}
			p.processJob(job, id)
		case <-p.ctx.Done():
			p.logf("Worker %d stopping - context cancelled", id)
			return
		}
	}
}

// processJob executes a single job with retry logic
func (p *Pool) processJob(job *Job, workerID int) {
	atomic.AddInt64(&p.stats.ActiveJobs, 1)
	defer atomic.AddInt64(&p.stats.ActiveJobs, -1)

	job.SetStatus(JobRunning)
	job.Attempts++

	p.logf("Worker %d executing job %s (attempt %d): %s",
		workerID, job.ID, job.Attempts, job.Description)

	// Execute the job
	err := job.Execute(p.ctx)

	if err != nil {
		job.SetError(err)
		p.logf("Worker %d job %s failed (attempt %d): %v",
			workerID, job.ID, job.Attempts, err)

		// Check if we should retry
		if job.Attempts < job.MaxRetries {
			job.SetStatus(JobRetrying)
			atomic.AddInt64(&p.stats.RetryJobs, 1)
			p.scheduleRetry(job)
			return
		}

		// Job failed permanently
		job.SetStatus(JobFailed)
		atomic.AddInt64(&p.stats.FailedJobs, 1)
	} else {
		// Job completed successfully
		job.SetStatus(JobCompleted)
		atomic.AddInt64(&p.stats.CompletedJobs, 1)
		p.logf("Worker %d completed job %s", workerID, job.ID)
	}

	// Send result
	select {
	case p.results <- job:
	case <-p.ctx.Done():
		return
	default:
		// If results channel is full or closed, log and continue
		p.logf("Worker %d unable to send result for job %s - channel unavailable", workerID, job.ID)
		return
	}
}

// scheduleRetry schedules a job for retry with exponential backoff
func (p *Pool) scheduleRetry(job *Job) {
	delay := p.calculateRetryDelay(job.Attempts)

	// Check if we're being rate limited
	if atomic.LoadInt64(&p.rateLimited) > 0 {
		delay = p.config.RateLimitDelay
		p.logf("Rate limited detected, using rate limit delay: %v", delay)
	}

	p.logf("Scheduling retry for job %s in %v", job.ID, delay)

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
			// Check if pool is stopped before attempting retry
			p.stopMutex.Lock()
			stopped := p.stopped
			p.stopMutex.Unlock()

			if stopped {
				p.logf("Job %s retry cancelled - pool is stopped", job.ID)
				return
			}

			// Reset job status and resubmit
			job.SetStatus(JobPending)
			select {
			case p.jobs <- job:
				p.logf("Job %s resubmitted for retry", job.ID)
			case <-p.ctx.Done():
				p.logf("Job %s retry cancelled - context done", job.ID)
				return
			default:
				// Channel is likely closed, log and give up
				p.logf("Job %s retry failed - unable to resubmit", job.ID)
				return
			}
		case <-p.ctx.Done():
			p.logf("Job %s retry cancelled - context cancelled", job.ID)
			return
		}
	}()
}

// calculateRetryDelay calculates delay with exponential backoff
func (p *Pool) calculateRetryDelay(attempt int) time.Duration {
	delay := float64(p.config.RetryDelay)
	for i := 1; i < attempt; i++ {
		delay *= p.config.BackoffFactor
	}

	result := time.Duration(delay)
	if result > p.config.MaxRetryDelay {
		result = p.config.MaxRetryDelay
	}

	return result
}

// resultProcessor handles completed jobs
func (p *Pool) resultProcessor() {
	defer p.wg.Done()
	p.logf("Result processor started")

	for {
		select {
		case result, ok := <-p.results:
			if !ok {
				p.logf("Result processor stopping - results channel closed")
				return
			}
			p.handleResult(result)
		case <-p.ctx.Done():
			p.logf("Result processor stopping - context cancelled")
			return
		}
	}
}

// handleResult processes a completed job result
func (p *Pool) handleResult(job *Job) {
	switch job.GetStatus() {
	case JobCompleted:
		p.logf("Job %s completed successfully", job.ID)
	case JobFailed:
		p.logf("Job %s failed permanently after %d attempts: %v",
			job.ID, job.Attempts, job.GetError())
	}
}

// SetRateLimited marks the pool as being rate limited
func (p *Pool) SetRateLimited(limited bool) {
	if limited {
		atomic.StoreInt64(&p.rateLimited, 1)
		p.logf("Rate limiting detected - switching to rate limit delay")
	} else {
		atomic.StoreInt64(&p.rateLimited, 0)
		p.logf("Rate limiting cleared")
	}
}

// IsRateLimited returns true if the pool is currently rate limited
func (p *Pool) IsRateLimited() bool {
	return atomic.LoadInt64(&p.rateLimited) > 0
}

// GetStuckJobs returns jobs that have been running for longer than the specified duration
func (p *Pool) GetStuckJobs(threshold time.Duration) []*Job {
	var stuckJobs []*Job

	p.jobMap.Range(func(_, value interface{}) bool {
		job := value.(*Job)
		if job.GetStatus() == JobRunning {
			// Check how long this job has been running
			if time.Since(job.CreatedAt) > threshold {
				stuckJobs = append(stuckJobs, job)
			}
		}
		return true
	})

	return stuckJobs
}

// ForceKillStuckJobs attempts to cancel jobs that have been stuck for too long
func (p *Pool) ForceKillStuckJobs(threshold time.Duration) int {
	stuckJobs := p.GetStuckJobs(threshold)

	for _, job := range stuckJobs {
		p.logf("Force killing stuck job %s (running for %v)", job.ID, time.Since(job.CreatedAt))
		job.SetStatus(JobFailed)
		job.SetError(fmt.Errorf("job forcefully terminated due to timeout after %v", time.Since(job.CreatedAt)))

		// Send to results channel
		select {
		case p.results <- job:
		case <-p.ctx.Done():
			return len(stuckJobs)
		default:
			// If results channel is full, continue anyway
		}
	}

	return len(stuckJobs)
}

// logf logs a message if verbose logging is enabled
func (p *Pool) logf(format string, args ...interface{}) {
	if p.config.LogVerbose {
		log.Printf("[POOL] "+format, args...)
	}
}
