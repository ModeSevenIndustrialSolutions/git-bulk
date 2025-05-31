// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CredentialsLoader handles loading credentials from files and environment variables
type CredentialsLoader struct {
	credentialsPath string
	fileCredentials map[string]string
}

// NewCredentialsLoader creates a new credentials loader
func NewCredentialsLoader(credentialsPath string) *CredentialsLoader {
	if credentialsPath == "" {
		// Default to .credentials in the current directory
		credentialsPath = ".credentials"
	}

	return &CredentialsLoader{
		credentialsPath: credentialsPath,
		fileCredentials: make(map[string]string),
	}
}

// LoadCredentials loads credentials from the file if it exists
func (c *CredentialsLoader) LoadCredentials() error {
	// Check if credentials file exists
	if _, err := os.Stat(c.credentialsPath); os.IsNotExist(err) {
		// File doesn't exist, that's okay - we'll use environment variables only
		return nil
	}

	file, err := os.Open(c.credentialsPath)
	if err != nil {
		return fmt.Errorf("failed to open credentials file %s: %w", c.credentialsPath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid format in credentials file at line %d: %s", lineNumber, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}

		c.fileCredentials[key] = value
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading credentials file: %w", err)
	}

	return nil
}

// GetCredential gets a credential value, checking environment variables first, then the file
func (c *CredentialsLoader) GetCredential(key string) string {
	// Check environment variable first (highest priority)
	if value := os.Getenv(key); value != "" {
		return value
	}

	// Check file credentials
	if value, exists := c.fileCredentials[key]; exists {
		return value
	}

	return ""
}

// SetEnvironmentFromFile sets environment variables from the loaded credentials file
// This is useful for tools that expect environment variables
func (c *CredentialsLoader) SetEnvironmentFromFile() error {
	for key, value := range c.fileCredentials {
		// Only set if environment variable is not already set
		if os.Getenv(key) == "" {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("failed to set environment variable %s: %w", key, err)
			}
		}
	}
	return nil
}

// ListCredentials returns all available credentials (for debugging/status)
func (c *CredentialsLoader) ListCredentials() map[string]bool {
	credentials := make(map[string]bool)

	// Common credential keys to check
	keys := []string{
		"GITHUB_TOKEN",
		"GITLAB_TOKEN",
		"GERRIT_USERNAME",
		"GERRIT_PASSWORD",
		"GERRIT_TOKEN",
	}

	for _, key := range keys {
		credentials[key] = c.GetCredential(key) != ""
	}

	return credentials
}

// FindCredentialsFile looks for a credentials file in common locations
func FindCredentialsFile() string {
	locations := []string{
		".credentials",
		".env",
		filepath.Join(os.Getenv("HOME"), ".config", "git-bulk", "credentials"),
		filepath.Join(os.Getenv("HOME"), ".git-bulk-credentials"),
	}

	for _, location := range locations {
		if _, err := os.Stat(location); err == nil {
			return location
		}
	}

	return ""
}
