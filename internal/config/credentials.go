// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package config provides credential management functionality for git-bulk operations.
// It handles loading and parsing credentials from various sources including environment
// variables and configuration files with support for both KEY=VALUE and INI-style sections.
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
	hostCredentials map[string]map[string]string // host-specific credentials from INI sections
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
		hostCredentials: make(map[string]map[string]string),
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
	defer func() {
		if err := file.Close(); err != nil {
			// Log the error but don't override the main function's return error
			fmt.Printf("Warning: failed to close credentials file: %v\n", err)
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	currentSection := ""

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Check for INI-style section headers [section]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = line[1 : len(line)-1]
			if c.hostCredentials[currentSection] == nil {
				c.hostCredentials[currentSection] = make(map[string]string)
			}
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

		// Store in appropriate location based on whether we're in a section
		if currentSection != "" {
			c.hostCredentials[currentSection][key] = value
		} else {
			c.fileCredentials[key] = value
		}
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

// GetHostCredential gets a credential value for a specific host from INI sections
func (c *CredentialsLoader) GetHostCredential(host, key string) string {
	// Check environment variable first (highest priority)
	if value := os.Getenv(key); value != "" {
		return value
	}

	// Check host-specific credentials
	if hostCreds, exists := c.hostCredentials[host]; exists {
		if value, exists := hostCreds[key]; exists {
			return value
		}
	}

	// Fall back to general credentials
	return c.GetCredential(key)
}

// GetGerritCredentials gets Gerrit credentials for a specific host
func (c *CredentialsLoader) GetGerritCredentials(host string) (username, password, token string) {
	// Try host-specific credentials first
	if hostCreds, exists := c.hostCredentials[host]; exists {
		if user, exists := hostCreds["USERNAME"]; exists {
			username = user
		}
		if pass, exists := hostCreds["PASSWORD"]; exists {
			password = pass
		}
		if tok, exists := hostCreds["TOKEN"]; exists {
			token = tok
		}
	}

	// Fall back to general Gerrit credentials if not found in host-specific
	if username == "" {
		username = c.GetCredential("GERRIT_USERNAME")
	}
	if password == "" {
		password = c.GetCredential("GERRIT_PASSWORD")
	}
	if token == "" {
		token = c.GetCredential("GERRIT_TOKEN")
	}

	return username, password, token
}

// GetSectionCredential gets a credential value from a specific section
func (c *CredentialsLoader) GetSectionCredential(section, key string) string {
	// Check environment variable first (highest priority)
	if value := os.Getenv(key); value != "" {
		return value
	}

	// Check section-specific credentials
	if sectionCreds, exists := c.hostCredentials[section]; exists {
		if value, exists := sectionCreds[key]; exists {
			return value
		}
	}

	// Fall back to general credentials
	return c.GetCredential(key)
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

// ListHostCredentials returns all host-specific credentials
func (c *CredentialsLoader) ListHostCredentials() map[string]map[string]bool {
	hostStatus := make(map[string]map[string]bool)
	
	for host, creds := range c.hostCredentials {
		hostStatus[host] = make(map[string]bool)
		for key := range creds {
			hostStatus[host][key] = creds[key] != ""
		}
	}
	
	return hostStatus
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