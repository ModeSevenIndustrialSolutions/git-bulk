// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

// Package ssh provides transparent SSH authentication support for Git operations
package ssh

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// SSHConfig holds SSH configuration and authentication methods
type SSHConfig struct {
	// SSH agent socket path (auto-detected if not provided)
	AgentSocket string

	// SSH key file paths (auto-detected if not provided)
	KeyFiles []string

	// SSH config file path (defaults to ~/.ssh/config)
	ConfigFile string

	// Host-specific configurations
	HostConfigs map[string]*HostConfig

	// Timeout for SSH operations
	Timeout time.Duration

	// Enable verbose logging
	Verbose bool
}

// HostConfig contains SSH configuration for a specific host
type HostConfig struct {
	Hostname     string
	Port         int
	User         string
	IdentityFile string
	ProxyCommand string

	// Additional SSH options
	Options map[string]string
}

// AuthMethod represents an SSH authentication method
type AuthMethod interface {
	Name() string
	Auth() (ssh.AuthMethod, error)
	Available() bool
}

// SSHAuthenticator manages SSH authentication methods
type SSHAuthenticator struct {
	config      *SSHConfig
	authMethods []AuthMethod
}

// DefaultConfig returns a default SSH configuration
func DefaultConfig() *SSHConfig {
	return &SSHConfig{
		Timeout: 30 * time.Second,
		Verbose: false,
	}
}

// NewSSHAuthenticator creates a new SSH authenticator with auto-detection
func NewSSHAuthenticator(config *SSHConfig) (*SSHAuthenticator, error) {
	if config == nil {
		config = &SSHConfig{
			Timeout: 30 * time.Second,
		}
	}

	auth := &SSHAuthenticator{
		config:      config,
		authMethods: make([]AuthMethod, 0),
	}

	// Auto-detect SSH configuration
	if err := auth.autoDetectConfig(); err != nil {
		return nil, fmt.Errorf("failed to auto-detect SSH configuration: %w", err)
	}

	// Initialize authentication methods
	if err := auth.initAuthMethods(); err != nil {
		return nil, fmt.Errorf("failed to initialize authentication methods: %w", err)
	}

	return auth, nil
}

// autoDetectConfig automatically detects SSH configuration
func (a *SSHAuthenticator) autoDetectConfig() error {
	// Detect SSH agent socket
	if a.config.AgentSocket == "" {
		a.config.AgentSocket = os.Getenv("SSH_AUTH_SOCK")
	}

	// Detect SSH config file
	if a.config.ConfigFile == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		a.config.ConfigFile = filepath.Join(homeDir, ".ssh", "config")
	}

	// Detect SSH key files
	if len(a.config.KeyFiles) == 0 {
		a.config.KeyFiles = a.findSSHKeys()
	}

	// Initialize host configs map
	if a.config.HostConfigs == nil {
		a.config.HostConfigs = make(map[string]*HostConfig)
	}

	return nil
}

// findSSHKeys finds common SSH key files
func (a *SSHAuthenticator) findSSHKeys() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	commonKeys := []string{
		"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
	}

	var foundKeys []string
	for _, key := range commonKeys {
		keyPath := filepath.Join(sshDir, key)
		if _, err := os.Stat(keyPath); err == nil {
			foundKeys = append(foundKeys, keyPath)
		}
	}

	return foundKeys
}

// initAuthMethods initializes available authentication methods in priority order
func (a *SSHAuthenticator) initAuthMethods() error {
	// 1. SSH Agent (highest priority - includes hardware tokens like Secretive)
	if a.config.AgentSocket != "" {
		agentAuth := &SSHAgentAuth{
			socketPath: a.config.AgentSocket,
			timeout:    a.config.Timeout,
			verbose:    a.config.Verbose,
		}
		a.authMethods = append(a.authMethods, agentAuth)
	}

	// 2. SSH Key files
	for _, keyFile := range a.config.KeyFiles {
		keyAuth := &SSHKeyAuth{
			keyFile: keyFile,
			timeout: a.config.Timeout,
			verbose: a.config.Verbose,
		}
		a.authMethods = append(a.authMethods, keyAuth)
	}

	return nil
}

// GetAuthMethods returns all available SSH authentication methods
func (a *SSHAuthenticator) GetAuthMethods() []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	for _, authMethod := range a.authMethods {
		if authMethod.Available() {
			if method, err := authMethod.Auth(); err == nil {
				methods = append(methods, method)
				if a.config.Verbose {
					fmt.Printf("SSH: Added auth method: %s\n", authMethod.Name())
				}
			} else if a.config.Verbose {
				fmt.Printf("SSH: Failed to initialize auth method %s: %v\n", authMethod.Name(), err)
			}
		}
	}

	return methods
}

// TestConnection tests SSH connection to a host
func (a *SSHAuthenticator) TestConnection(ctx context.Context, host string, port int) error {
	if port == 0 {
		port = 22
	}

	config := &ssh.ClientConfig{
		User:            "git", // Default for Git hosting providers
		Auth:            a.GetAuthMethods(),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // For testing only
		Timeout:         a.config.Timeout,
	}

	address := fmt.Sprintf("%s:%d", host, port)

	conn, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", address, err)
	}
	defer func() {
		_ = conn.Close() // Ignore close error for test connection
	}()

	if a.config.Verbose {
		fmt.Printf("SSH: Successfully connected to %s\n", address)
	}

	return nil
}

// SSHAgentAuth implements SSH agent authentication
type SSHAgentAuth struct {
	socketPath string
	timeout    time.Duration
	verbose    bool
}

func (a *SSHAgentAuth) Name() string {
	return "SSH Agent"
}

func (a *SSHAgentAuth) Available() bool {
	if a.socketPath == "" {
		return false
	}

	// Test if socket exists and is accessible
	conn, err := net.Dial("unix", a.socketPath)
	if err != nil {
		return false
	}
	_ = conn.Close() // Ignore close error for availability check
	return true
}

func (a *SSHAgentAuth) Auth() (ssh.AuthMethod, error) {
	conn, err := net.Dial("unix", a.socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH agent: %w", err)
	}

	agentClient := agent.NewClient(conn)
	return ssh.PublicKeysCallback(agentClient.Signers), nil
}

// SSHKeyAuth implements SSH key file authentication
type SSHKeyAuth struct {
	keyFile string
	timeout time.Duration
	verbose bool
}

func (a *SSHKeyAuth) Name() string {
	return fmt.Sprintf("SSH Key: %s", filepath.Base(a.keyFile))
}

func (a *SSHKeyAuth) Available() bool {
	_, err := os.Stat(a.keyFile)
	return err == nil
}

func (a *SSHKeyAuth) Auth() (ssh.AuthMethod, error) {
	key, err := os.ReadFile(a.keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read key file %s: %w", a.keyFile, err)
	}

	var signer ssh.Signer

	// Try parsing without passphrase first
	signer, err = ssh.ParsePrivateKey(key)
	if err != nil {
		// If it's an encrypted key, we'll need a passphrase
		if strings.Contains(err.Error(), "encrypted") {
			// For now, skip encrypted keys without ssh-agent
			// In a real implementation, you might want to prompt for passphrase
			return nil, fmt.Errorf("encrypted key file %s requires passphrase (use ssh-agent)", a.keyFile)
		}
		return nil, fmt.Errorf("failed to parse key file %s: %w", a.keyFile, err)
	}

	return ssh.PublicKeys(signer), nil
}

// GitSSHWrapper provides Git-specific SSH configuration
type GitSSHWrapper struct {
	authenticator *SSHAuthenticator
	tempDir       string
}

// NewGitSSHWrapper creates a new Git SSH wrapper
func NewGitSSHWrapper(config *SSHConfig) (*GitSSHWrapper, error) {
	auth, err := NewSSHAuthenticator(config)
	if err != nil {
		return nil, err
	}

	wrapper := &GitSSHWrapper{
		authenticator: auth,
	}

	return wrapper, nil
}

// SetupGitSSH configures Git to use SSH authentication
func (w *GitSSHWrapper) SetupGitSSH() error {
	// Create temporary directory for SSH wrapper script
	tempDir, err := os.MkdirTemp("", "git-bulk-ssh-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	w.tempDir = tempDir

	// Create SSH wrapper script
	wrapperScript := filepath.Join(tempDir, "ssh-wrapper.sh")
	scriptContent := w.generateSSHWrapperScript()

	if err := os.WriteFile(wrapperScript, []byte(scriptContent), 0755); err != nil {
		return fmt.Errorf("failed to write SSH wrapper script: %w", err)
	}

	// Set GIT_SSH environment variable
	if err := os.Setenv("GIT_SSH", wrapperScript); err != nil {
		return fmt.Errorf("failed to set GIT_SSH environment variable: %w", err)
	}

	return nil
}

// generateSSHWrapperScript generates a shell script that configures SSH for Git
func (w *GitSSHWrapper) generateSSHWrapperScript() string {
	script := `#!/bin/bash
# Auto-generated SSH wrapper for git-bulk

# Use SSH agent if available
if [ -n "$SSH_AUTH_SOCK" ] && [ -S "$SSH_AUTH_SOCK" ]; then
    export SSH_AUTH_SOCK="$SSH_AUTH_SOCK"
fi

# Common SSH options for Git operations
SSH_OPTS="-o BatchMode=yes"
SSH_OPTS="$SSH_OPTS -o StrictHostKeyChecking=no"
SSH_OPTS="$SSH_OPTS -o UserKnownHostsFile=/dev/null"
SSH_OPTS="$SSH_OPTS -o ConnectTimeout=30"

# Support for different Git hosting providers
case "$1" in
    *github.com*)
        SSH_OPTS="$SSH_OPTS -p 22"
        ;;
    *gitlab.com*)
        SSH_OPTS="$SSH_OPTS -p 22"
        ;;
    *gerrit*)
        # Gerrit typically uses port 29418
        SSH_OPTS="$SSH_OPTS -p 29418"
        ;;
esac

# Execute SSH with our options
exec ssh $SSH_OPTS "$@"
`
	return script
}

// Cleanup removes temporary files
func (w *GitSSHWrapper) Cleanup() error {
	if w.tempDir != "" {
		return os.RemoveAll(w.tempDir)
	}
	return nil
}

// ValidateSSHSetup validates that SSH authentication is working
func ValidateSSHSetup(ctx context.Context, verbose bool) error {
	config := &SSHConfig{
		Verbose: verbose,
		Timeout: 10 * time.Second,
	}

	auth, err := NewSSHAuthenticator(config)
	if err != nil {
		return fmt.Errorf("failed to create SSH authenticator: %w", err)
	}

	// Test common Git hosting providers
	hosts := []struct {
		name string
		host string
		port int
	}{
		{"GitHub", "github.com", 22},
		{"GitLab", "gitlab.com", 22},
	}

	for _, host := range hosts {
		if verbose {
			fmt.Printf("Testing SSH connection to %s...\n", host.name)
		}

		if err := auth.TestConnection(ctx, host.host, host.port); err != nil {
			if verbose {
				fmt.Printf("SSH connection to %s failed: %v\n", host.name, err)
			}
		} else {
			if verbose {
				fmt.Printf("SSH connection to %s successful\n", host.name)
			}
		}
	}

	return nil
}

// GetSSHCloneURL converts HTTPS URLs to SSH URLs for Git hosting providers
func GetSSHCloneURL(httpsURL string) string {
	// GitHub
	if strings.Contains(httpsURL, "github.com") {
		return strings.Replace(httpsURL, "https://github.com/", "git@github.com:", 1)
	}

	// GitLab
	if strings.Contains(httpsURL, "gitlab.com") {
		return strings.Replace(httpsURL, "https://gitlab.com/", "git@gitlab.com:", 1)
	}

	// Generic GitLab instances
	if strings.Contains(httpsURL, "gitlab") {
		// Extract host from HTTPS URL
		parts := strings.Split(httpsURL, "/")
		if len(parts) >= 4 {
			host := parts[2]
			path := strings.Join(parts[3:], "/")
			return fmt.Sprintf("git@%s:%s", host, path)
		}
	}

	// Gerrit (typically uses SSH URLs directly)
	if strings.Contains(httpsURL, "gerrit") {
		// Convert HTTPS Gerrit URL to SSH format
		// https://gerrit.example.com/repo.git -> ssh://gerrit.example.com:29418/repo.git
		parts := strings.Split(httpsURL, "/")
		if len(parts) >= 4 && strings.HasPrefix(httpsURL, "https://") {
			host := parts[2]
			path := strings.Join(parts[3:], "/")
			return fmt.Sprintf("ssh://%s:29418/%s", host, path)
		}
		return httpsURL
	}

	return httpsURL
}
