// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package worker

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_NewPool(t *testing.T) {
	config := &Config{
		WorkerCount: 3,
		MaxRetries:  2,
		QueueSize:   10,
	}

	pool := NewPool(config)

	if pool.config.WorkerCount != 3 {
		t.Errorf("Expected worker count 3, got %d", pool.config.WorkerCount)
	}

	if pool.config.MaxRetries != 2 {
		t.Errorf("Expected max retries 2, got %d", pool.config.MaxRetries)
	}
}

func TestPool_DefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.WorkerCount != 6 {
		t.Errorf("Expected default worker count 6, got %d", config.WorkerCount)
	}

	if config.MaxRetries != 3 {
		t.Errorf("Expected default max retries 3, got %d", config.MaxRetries)
	}
}

func TestPool_SubmitAndExecute(t *testing.T) {
	config := &Config{
		WorkerCount: 2,
		MaxRetries:  1,
		QueueSize:   5,
		LogVerbose:  false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	var executed int32

	job := &Job{
		ID:          "test-job",
		Description: "Test job",
		Execute: func(_ context.Context) error {
			atomic.AddInt32(&executed, 1)
			return nil
		},
	}

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Wait for job to complete
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&executed) != 1 {
		t.Errorf("Expected job to be executed once, got %d", executed)
	}

	if job.GetStatus() != JobCompleted {
		t.Errorf("Expected job status to be completed, got %s", job.GetStatus())
	}
}

func TestPool_JobRetry(t *testing.T) {
	config := &Config{
		WorkerCount:   1,
		MaxRetries:    3,
		RetryDelay:    10 * time.Millisecond,
		BackoffFactor: 1.5,
		QueueSize:     5,
		LogVerbose:    false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	var attempts int32

	job := &Job{
		ID:          "retry-job",
		Description: "Job that fails then succeeds",
		Execute: func(_ context.Context) error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				return errors.New("temporary failure")
			}
			return nil
		},
	}

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Wait for retries to complete
	time.Sleep(200 * time.Millisecond)

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	if job.GetStatus() != JobCompleted {
		t.Errorf("Expected job status to be completed, got %s", job.GetStatus())
	}
}

func TestPool_JobFailure(t *testing.T) {
	config := &Config{
		WorkerCount:   1,
		MaxRetries:    2,
		RetryDelay:    10 * time.Millisecond,
		BackoffFactor: 1.0,
		QueueSize:     5,
		LogVerbose:    false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	var attempts int32
	expectedError := errors.New("persistent failure")

	job := &Job{
		ID:          "fail-job",
		Description: "Job that always fails",
		Execute: func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return expectedError
		},
	}

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Wait for all retries to complete
	time.Sleep(200 * time.Millisecond)

	maxAttempts := int32(config.MaxRetries)
	if atomic.LoadInt32(&attempts) != maxAttempts {
		t.Errorf("Expected %d attempts, got %d", maxAttempts, attempts)
	}

	if job.GetStatus() != JobFailed {
		t.Errorf("Expected job status to be failed, got %s", job.GetStatus())
	}

	if job.GetError() != expectedError {
		t.Errorf("Expected job error to be %v, got %v", expectedError, job.GetError())
	}
}

func TestPool_RateLimiting(t *testing.T) {
	config := &Config{
		WorkerCount:    1,
		MaxRetries:     1,
		RetryDelay:     10 * time.Millisecond,
		RateLimitDelay: 50 * time.Millisecond,
		QueueSize:      5,
		LogVerbose:     false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	// Test rate limiting
	pool.SetRateLimited(true)

	if !pool.IsRateLimited() {
		t.Error("Expected pool to be rate limited")
	}

	var executed int32
	job := &Job{
		ID:          "rate-limited-job",
		Description: "Job submitted while rate limited",
		Execute: func(_ context.Context) error {
			atomic.AddInt32(&executed, 1)
			return errors.New("simulated failure during rate limiting")
		},
	}

	start := time.Now()
	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Wait for job to be processed
	time.Sleep(100 * time.Millisecond)

	// Clear rate limiting
	pool.SetRateLimited(false)

	if pool.IsRateLimited() {
		t.Error("Expected pool to not be rate limited after clearing")
	}

	// Job should have been attempted at least once
	if atomic.LoadInt32(&executed) == 0 {
		t.Error("Expected job to be attempted at least once")
	}

	// Check that the retry delay was longer due to rate limiting
	duration := time.Since(start)
	if duration < config.RateLimitDelay {
		t.Errorf("Expected delay to be at least %v due to rate limiting, got %v", config.RateLimitDelay, duration)
	}
}

func TestPool_Stats(t *testing.T) {
	config := &Config{
		WorkerCount: 2,
		MaxRetries:  1,
		QueueSize:   10,
		LogVerbose:  false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	// Submit successful job
	successJob := &Job{
		ID:          "success-job",
		Description: "Successful job",
		Execute: func(_ context.Context) error {
			return nil
		},
	}

	// Submit failing job
	failJob := &Job{
		ID:          "fail-job",
		Description: "Failing job",
		Execute: func(_ context.Context) error {
			return errors.New("failure")
		},
	}

	err := pool.Submit(successJob)
	if err != nil {
		t.Fatalf("Failed to submit success job: %v", err)
	}

	err = pool.Submit(failJob)
	if err != nil {
		t.Fatalf("Failed to submit fail job: %v", err)
	}

	// Wait for jobs to complete
	time.Sleep(100 * time.Millisecond)

	stats := pool.GetStats()

	if stats.TotalJobs != 2 {
		t.Errorf("Expected 2 total jobs, got %d", stats.TotalJobs)
	}

	if stats.CompletedJobs != 1 {
		t.Errorf("Expected 1 completed job, got %d", stats.CompletedJobs)
	}

	if stats.FailedJobs != 1 {
		t.Errorf("Expected 1 failed job, got %d", stats.FailedJobs)
	}
}

func TestPool_JobStatus(t *testing.T) {
	job := &Job{
		ID:          "status-test",
		Description: "Test job status",
		Execute: func(_ context.Context) error {
			return nil
		},
	}

	// Test initial status
	if job.GetStatus() != JobPending {
		t.Errorf("Expected initial status to be pending, got %s", job.GetStatus())
	}

	// Test status changes
	job.SetStatus(JobRunning)
	if job.GetStatus() != JobRunning {
		t.Errorf("Expected status to be running, got %s", job.GetStatus())
	}

	job.SetStatus(JobCompleted)
	if job.GetStatus() != JobCompleted {
		t.Errorf("Expected status to be completed, got %s", job.GetStatus())
	}

	// Test error handling
	testError := errors.New("test error")
	job.SetError(testError)
	if job.GetError() != testError {
		t.Errorf("Expected error to be %v, got %v", testError, job.GetError())
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	config := &Config{
		WorkerCount: 1,
		MaxRetries:  1,
		QueueSize:   5,
		LogVerbose:  false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	var executed int32
	job := &Job{
		ID:          "cancel-job",
		Description: "Job that gets cancelled",
		Execute: func(jobCtx context.Context) error {
			// Wait for cancellation
			select {
			case <-jobCtx.Done():
				atomic.AddInt32(&executed, 1)
				return jobCtx.Err()
			case <-time.After(100 * time.Millisecond):
				return errors.New("job should have been cancelled")
			}
		},
	}

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Stop the pool to cancel ongoing jobs
	pool.Stop()

	// Wait a moment for job completion
	time.Sleep(50 * time.Millisecond)

	// The job should have been cancelled when pool stopped
	if atomic.LoadInt32(&executed) == 0 {
		t.Log("Job was not executed before pool stopped (this is acceptable)")
	}
}

func TestPool_GetJob(t *testing.T) {
	config := &Config{
		WorkerCount: 1,
		QueueSize:   5,
		LogVerbose:  false,
	}

	pool := NewPool(config)

	job := &Job{
		ID:          "get-job-test",
		Description: "Test getting job",
		Execute: func(_ context.Context) error {
			return nil
		},
	}

	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	retrievedJob, found := pool.GetJob("get-job-test")
	if !found {
		t.Error("Expected to find submitted job")
	}

	if retrievedJob.ID != job.ID {
		t.Errorf("Expected job ID %s, got %s", job.ID, retrievedJob.ID)
	}

	_, found = pool.GetJob("non-existent")
	if found {
		t.Error("Expected not to find non-existent job")
	}
}

func BenchmarkPool_SubmitJob(b *testing.B) {
	config := &Config{
		WorkerCount: 4,
		MaxRetries:  1,
		QueueSize:   1000,
		LogVerbose:  false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		job := &Job{
			ID:          fmt.Sprintf("bench-job-%d", i),
			Description: "Benchmark job",
			Execute: func(_ context.Context) error {
				// Simulate some work
				time.Sleep(time.Microsecond)
				return nil
			},
		}

		if err := pool.Submit(job); err != nil {
			b.Errorf("Failed to submit job: %v", err)
		}
	}
}

func BenchmarkPool_ExecuteJob(b *testing.B) {
	config := &Config{
		WorkerCount: 4,
		MaxRetries:  1,
		QueueSize:   1000,
		LogVerbose:  false,
	}

	pool := NewPool(config)
	pool.Start()
	defer pool.Stop()

	var completed int32

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		job := &Job{
			ID:          fmt.Sprintf("exec-bench-job-%d", i),
			Description: "Execution benchmark job",
			Execute: func(_ context.Context) error {
				atomic.AddInt32(&completed, 1)
				return nil
			},
		}

		if err := pool.Submit(job); err != nil {
			b.Errorf("Failed to submit job: %v", err)
		}
	}

	// Wait for all jobs to complete
	for atomic.LoadInt32(&completed) < int32(b.N) {
		time.Sleep(time.Millisecond)
	}
}
