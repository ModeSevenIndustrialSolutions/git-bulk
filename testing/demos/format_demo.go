// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Test the formatErrorMessage function
func formatErrorMessage(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

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

	// Extract exit status if available
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := exitErr.ExitCode()
		if strings.Contains(errStr, "exists and is not empty") {
			return fmt.Sprintf("failed, exit status %d path exists and is not empty", exitCode)
		}
		return fmt.Sprintf("failed, exit status %d", exitCode)
	}

	firstLine := strings.Split(errStr, "\n")[0]
	if len(firstLine) > 80 {
		firstLine = firstLine[:77] + "..."
	}

	return strings.TrimSpace(firstLine)
}

func main() {
	testErrors := []error{
		errors.New("repository was not processed - possible causes: worker pool full (3 active), context timeout, or job submission failed"),
		errors.New("git clone failed: exit status 128\nOutput: fatal: destination path 'repo' already exists and is not empty"),
		errors.New("authentication failed for https://github.com/user/repo.git"),
		errors.New("repository not found: https://github.com/user/nonexistent.git"),
		errors.New("network error for https://github.com/user/repo.git: connection timeout"),
	}

	fmt.Println("Testing error formatting:")
	for i, err := range testErrors {
		formatted := formatErrorMessage(err)
		fmt.Printf("❌ example/repo%d [%s]\n", i+1, formatted)
	}
}
