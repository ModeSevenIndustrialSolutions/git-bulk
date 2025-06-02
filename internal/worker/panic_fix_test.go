// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package worker

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestPoolRetryPanicFix tests that retries don't panic when pool is stopped
func TestPoolRetryPanicFix(t *testing.T) {
	config := &Config{
		WorkerCount: 1,
		MaxRetries:  3,
		RetryDelay:  100 * time.Millisecond,
		QueueSize:   5,
		LogVerbose:  true,
	}

	pool := NewPool(config)
	pool.Start()

	var attempts int32
	job := &Job{
		ID:          "retry-panic-test",
		Description: "Job that fails and retries during shutdown",
		Execute: func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return errors.New("simulated failure for retry testing")
		},
	}

	// Submit job that will fail and trigger retry
	err := pool.Submit(job)
	if err != nil {
		t.Fatalf("Failed to submit job: %v", err)
	}

	// Wait a bit for the job to start and fail
	time.Sleep(50 * time.Millisecond)

	// Stop the pool while retry might be scheduled
	pool.Stop()

	// Wait a bit more to see if any panics occur
	time.Sleep(200 * time.Millisecond)

	// If we reach here without panic, the fix is working
	t.Logf("Pool stopped successfully without panic. Job attempts: %d", atomic.LoadInt32(&attempts))
}
