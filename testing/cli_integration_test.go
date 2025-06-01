// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

//go:build integration
// +build integration

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	binaryName             = "git-bulk"
	testTimeout            = 30 * time.Second
	buildPath              = "../cmd/git-bulk"
	failedToBuildBinaryMsg = "Failed to build binary: %v"
	expectedExitCodeMsg    = "Expected exit code %d, got %d"
	stdoutMsg              = "Stdout: %s"
	stderrMsg              = "Stderr: %s"
	fullOutputMsg          = "Full output: %s"

	// CLI flags and arguments
	flagHelp     = "--help"
	flagDryRun   = "--dry-run"
	flagVerbose  = "--verbose"
	flagSSH      = "--ssh"
	flagWorkers  = "--workers"
	flagMaxRepos = "--max-repos"
	flagTimeout  = "--timeout"
	flagSource   = "--source"
	flagTarget   = "--target"
	flagSync     = "--sync"

	// Common test values
	githubOctocat           = "github.com/octocat"
	githubOctocatHelloWorld = "github.com/octocat/Hello-World"
	gitlabOrg               = "gitlab.com/gitlab-org"
	gerritOnap              = "https://gerrit.onap.org"
	dryRunOutput            = "DRY RUN"
	commandSSHSetup         = "ssh-setup"
	usingProviderMsg        = "Using provider:"
	sshAuthSetupMsg         = "SSH Authentication Setup"
	sshConfigDetailsMsg     = "SSH Configuration Details"
	reposWouldBeClonedMsg   = "repositories that would be cloned"
	credentialStatusMsg     = "Credential status:"
	hostMsg                 = "Host:"
	organizationMsg         = "Organization:"
)

type testCase struct {
	name         string
	args         []string
	wantExit     int
	wantContains []string
	timeout      time.Duration
	setupEnv     map[string]string
}

// TestCLIIntegration tests the CLI functionality
func TestCLIIntegration(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	// Run all test suites
	t.Run("Basic Commands", func(t *testing.T) {
		testBasicCommands(t)
	})

	t.Run("Clone Commands", func(t *testing.T) {
		testCloneCommands(t)
	})

	t.Run("SSH Setup", func(t *testing.T) {
		testSSHSetupCommands(t)
	})

	t.Run("Advanced Scenarios", func(t *testing.T) {
		testAdvancedScenarios(t)
	})

	t.Run("Error Cases", func(t *testing.T) {
		testErrorCases(t)
	})
}

func testBasicCommands(t *testing.T) {
	tests := []testCase{
		{
			name:         "Help command",
			args:         []string{flagHelp},
			wantExit:     0,
			wantContains: []string{"git-bulk", "clone", "Usage:"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Version command",
			args:         []string{"--version"},
			wantExit:     0,
			wantContains: []string{"git-bulk version"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Invalid command",
			args:         []string{"invalid-command"},
			wantExit:     1,
			wantContains: []string{"unknown command"},
			timeout:      5 * time.Second,
		},
	}

	runTestCases(t, tests)
}

func testCloneCommands(t *testing.T) {
	tests := []testCase{
		{
			name:         "Clone help",
			args:         []string{"clone", flagHelp},
			wantExit:     0,
			wantContains: []string{"Clone repositories", "source", "Examples:"},
			timeout:      5 * time.Second,
		},
		{
			name:         "Clone without source",
			args:         []string{"clone"},
			wantExit:     1,
			wantContains: []string{"must specify either <source> argument or --source flag"},
			timeout:      5 * time.Second,
		},
		{
			name:         "GitHub dry run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{usingProviderMsg, "github", dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitLab dry run",
			args:         []string{"clone", gitlabOrg, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Gerrit SSH dry run",
			args:         []string{"clone", gerritOnap, flagSSH, flagDryRun, flagMaxRepos, "5"},
			wantExit:     0,
			wantContains: []string{dryRunOutput, reposWouldBeClonedMsg},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testSSHSetupCommands(t *testing.T) {
	tests := []testCase{
		{
			name:         "SSH setup basic",
			args:         []string{commandSSHSetup},
			wantExit:     0,
			wantContains: []string{sshAuthSetupMsg},
			timeout:      testTimeout,
		},
		{
			name:         "SSH setup verbose",
			args:         []string{commandSSHSetup, flagVerbose},
			wantExit:     0,
			wantContains: []string{sshAuthSetupMsg},
			timeout:      testTimeout,
		},
		{
			name:         "SSH setup help",
			args:         []string{commandSSHSetup, flagHelp},
			wantExit:     0,
			wantContains: []string{"SSH agent availability", "Examples:"},
			timeout:      5 * time.Second,
		},
	}

	runTestCases(t, tests)
}

func testAdvancedScenarios(t *testing.T) {
	tests := []testCase{
		{
			name:         "Worker configuration",
			args:         []string{"clone", githubOctocat, flagDryRun, flagWorkers, "8"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "SSH option",
			args:         []string{"clone", githubOctocat, flagDryRun, flagSSH},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Max repos limit",
			args:         []string{"clone", githubOctocat, flagDryRun, flagMaxRepos, "5"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Timeout configuration",
			args:         []string{"clone", githubOctocat, flagDryRun, flagTimeout, "10s"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Fork mode dry run",
			args:         []string{"clone", flagSource, githubOctocat, flagTarget, "github.com/testorg", flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testErrorCases(t *testing.T) {
	tests := []testCase{
		{
			name:         "Invalid source format",
			args:         []string{"clone", "invalid-source", flagDryRun},
			wantExit:     1,
			wantContains: []string{"failed to get provider"},
			timeout:      testTimeout,
		},
		{
			name:         "Fork without target",
			args:         []string{"clone", flagSource, githubOctocat, flagDryRun},
			wantExit:     1,
			wantContains: []string{"--target is required when using --source"},
			timeout:      5 * time.Second,
		},
	}

	runTestCases(t, tests)
}

func runTestCases(t *testing.T, tests []testCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSingleTestCase(t, tt)
		})
	}
}

func runSingleTestCase(t *testing.T, tt testCase) {
	// Skip tests that require tokens if not available
	if shouldSkipTest(tt) {
		t.Skip("Required environment variable not set, skipping test")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
	defer cancel()

	cmd := setupCommand(ctx, tt)
	stdout, stderr := executeCommand(t, cmd)

	validateResults(t, tt, cmd, stdout, stderr)
}

func shouldSkipTest(tt testCase) bool {
	if tt.setupEnv != nil {
		if token := tt.setupEnv["GITHUB_TOKEN"]; token == "" {
			return true
		}
	}
	return false
}

func setupCommand(ctx context.Context, tt testCase) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "./"+binaryName, tt.args...)

	// Set up environment variables
	if tt.setupEnv != nil {
		env := os.Environ()
		for k, v := range tt.setupEnv {
			if v != "" {
				env = append(env, fmt.Sprintf("%s=%s", k, v))
			}
		}
		cmd.Env = env
	}

	return cmd
}

func executeCommand(t *testing.T, cmd *exec.Cmd) (*bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	return &stdout, &stderr
}

func validateResults(t *testing.T, tt testCase, cmd *exec.Cmd, stdout, stderr *bytes.Buffer) {
	err := cmd.Run()

	// Check exit code
	exitCode := getExitCode(t, err)

	if exitCode != tt.wantExit {
		t.Errorf(expectedExitCodeMsg, tt.wantExit, exitCode)
		t.Logf(stdoutMsg, stdout.String())
		t.Logf(stderrMsg, stderr.String())
	}

	// Check output contains expected strings
	validateOutputContains(t, tt, stdout, stderr)
}

func getExitCode(t *testing.T, err error) int {
	if err == nil {
		return 0
	}

	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.ExitCode()
	}

	t.Fatalf("Command failed with error: %v", err)
	return -1
}

func validateOutputContains(t *testing.T, tt testCase, stdout, stderr *bytes.Buffer) {
	output := stdout.String() + stderr.String()
	for _, want := range tt.wantContains {
		if !strings.Contains(output, want) {
			t.Errorf("Expected output to contain %q, but it didn't", want)
			t.Logf(fullOutputMsg, output)
		}
	}
}

// TestActualClone tests actual repository cloning (requires network)
func TestActualClone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping actual clone test in short mode")
	}

	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "git-bulk-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test cloning a small public repository
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./"+binaryName,
		"clone", githubOctocatHelloWorld,
		"--output", tempDir,
		"--workers", "1",
		"--verbose")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		t.Logf("Stdout: %s", stdout.String())
		t.Logf("Stderr: %s", stderr.String())
		t.Fatalf("Clone command failed: %v", err)
	}

	// Check if repository was cloned
	repoPath := filepath.Join(tempDir, "Hello-World")
	if _, err := os.Stat(repoPath); os.IsNotExist(err) {
		t.Errorf("Repository was not cloned to expected path: %s", repoPath)
	}

	// Check if it's a valid git repository
	gitDir := filepath.Join(repoPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Errorf("Cloned repository does not contain .git directory")
	}
}

// TestProviderDetection tests provider detection logic
func TestProviderDetection(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	tests := []struct {
		name     string
		source   string
		wantExit int
		provider string
	}{
		{
			name:     "GitHub URL",
			source:   "github.com/owner",
			wantExit: 0,
			provider: "github",
		},
		{
			name:     "GitLab URL",
			source:   "gitlab.com/group",
			wantExit: 0,
			provider: "gitlab",
		},
		{
			name:     "GitHub HTTPS URL",
			source:   "https://github.com/owner",
			wantExit: 0,
			provider: "github",
		},
		{
			name:     "Invalid URL",
			source:   "invalid.com/owner",
			wantExit: 1,
			provider: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runProviderDetectionTest(t, tt)
		})
	}
}

func runProviderDetectionTest(t *testing.T, tt struct {
	name     string
	source   string
	wantExit int
	provider string
}) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./"+binaryName, "clone", tt.source, flagDryRun, flagVerbose)
	stdout, stderr := executeCommand(t, cmd)

	err := cmd.Run()
	exitCode := getExitCode(t, err)

	if exitCode != tt.wantExit {
		t.Errorf(expectedExitCodeMsg, tt.wantExit, exitCode)
		t.Logf(stdoutMsg, stdout.String())
		t.Logf(stderrMsg, stderr.String())
	}

	if tt.provider != "" {
		output := stdout.String() + stderr.String()
		if !strings.Contains(output, tt.provider) {
			t.Errorf("Expected output to contain provider %q", tt.provider)
			t.Logf(fullOutputMsg, output)
		}
	}
}

// TestErrorHandling tests various error conditions
func TestErrorHandling(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	tests := []struct {
		name         string
		args         []string
		wantExit     int
		wantContains []string
	}{
		{
			name:         "Invalid worker count",
			args:         []string{"clone", githubOctocat, flagWorkers, "-1", flagDryRun},
			wantExit:     1,
			wantContains: []string{"invalid"},
		},
		{
			name:         "Invalid timeout",
			args:         []string{"clone", githubOctocat, flagTimeout, "invalid", flagDryRun},
			wantExit:     1,
			wantContains: []string{"invalid"},
		},
		{
			name:         "Very short timeout",
			args:         []string{"clone", githubOctocat, flagTimeout, "1ms", flagDryRun},
			wantExit:     1,
			wantContains: []string{"timeout", "context deadline exceeded"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorHandlingTest(t, tt)
		})
	}
}

func runErrorHandlingTest(t *testing.T, tt struct {
	name         string
	args         []string
	wantExit     int
	wantContains []string
}) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "./"+binaryName, tt.args...)
	stdout, stderr := executeCommand(t, cmd)

	err := cmd.Run()
	exitCode := getExitCode(t, err)

	if exitCode != tt.wantExit {
		t.Errorf(expectedExitCodeMsg, tt.wantExit, exitCode)
		t.Logf(stdoutMsg, stdout.String())
		t.Logf(stderrMsg, stderr.String())
	}

	output := stdout.String() + stderr.String()
	for _, want := range tt.wantContains {
		if !strings.Contains(strings.ToLower(output), strings.ToLower(want)) {
			t.Errorf("Expected output to contain %q, but it didn't", want)
			t.Logf(fullOutputMsg, output)
		}
	}
}

// TestComprehensiveDryRun tests all CLI commands with dry-run flag combinations
func TestComprehensiveDryRun(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	// Run comprehensive test suites
	t.Run("Clone Command Dry-Run Variations", func(t *testing.T) {
		testCloneDryRunVariations(t)
	})

	t.Run("SSH Command Dry-Run Tests", func(t *testing.T) {
		testSSHCommandVariations(t)
	})

	t.Run("Provider-Specific Dry-Run Tests", func(t *testing.T) {
		testProviderSpecificDryRuns(t)
	})

	t.Run("Flag Combination Dry-Run Tests", func(t *testing.T) {
		testFlagCombinationDryRuns(t)
	})

	t.Run("Output Format Dry-Run Tests", func(t *testing.T) {
		testOutputFormatDryRuns(t)
	})
}

func testCloneDryRunVariations(t *testing.T) {
	tests := []testCase{
		{
			name:         "Basic GitHub dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitHub dry-run with verbose",
			args:         []string{"clone", githubOctocat, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput, usingProviderMsg, "github"},
			timeout:      testTimeout,
		},
		{
			name:         "GitHub dry-run with worker limit",
			args:         []string{"clone", githubOctocat, flagDryRun, flagWorkers, "2"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitHub dry-run with repo limit",
			args:         []string{"clone", githubOctocat, flagDryRun, flagMaxRepos, "3"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitHub dry-run with timeout",
			args:         []string{"clone", githubOctocat, flagDryRun, flagTimeout, "15s"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitLab dry-run basic",
			args:         []string{"clone", gitlabOrg, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitLab dry-run with verbose",
			args:         []string{"clone", gitlabOrg, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput, usingProviderMsg},
			timeout:      testTimeout,
		},
		{
			name:         "Gerrit dry-run basic",
			args:         []string{"clone", gerritOnap, flagDryRun, flagMaxRepos, "2"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "HTTPS URL dry-run",
			args:         []string{"clone", "https://" + githubOctocat, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testSSHCommandVariations(t *testing.T) {
	tests := []testCase{
		{
			name:         "SSH setup dry-run",
			args:         []string{commandSSHSetup},
			wantExit:     0,
			wantContains: []string{"SSH Authentication Setup"},
			timeout:      testTimeout,
		},
		{
			name:         "SSH setup with verbose flag",
			args:         []string{commandSSHSetup, flagVerbose},
			wantExit:     0,
			wantContains: []string{sshAuthSetupMsg, sshConfigDetailsMsg},
			timeout:      testTimeout,
		},
		{
			name:         "Clone with SSH flag dry-run",
			args:         []string{"clone", githubOctocat, flagSSH, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Gerrit with SSH dry-run",
			args:         []string{"clone", gerritOnap, flagSSH, flagDryRun, flagMaxRepos, "3"},
			wantExit:     0,
			wantContains: []string{dryRunOutput, reposWouldBeClonedMsg},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testProviderSpecificDryRuns(t *testing.T) {
	tests := []testCase{
		{
			name:         "GitHub organization dry-run",
			args:         []string{"clone", "github.com/microsoft", flagDryRun, flagMaxRepos, "1"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "GitLab group dry-run",
			args:         []string{"clone", "gitlab.com/gitlab-org", flagDryRun, flagMaxRepos, "1"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Gerrit instance dry-run",
			args:         []string{"clone", "https://gerrit.googlesource.com", flagDryRun, flagMaxRepos, "1"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testFlagCombinationDryRuns(t *testing.T) {
	tests := []testCase{
		{
			name:         "Multiple flags dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagVerbose, flagWorkers, "1", flagMaxRepos, "2"},
			wantExit:     0,
			wantContains: []string{dryRunOutput, usingProviderMsg},
			timeout:      testTimeout,
		},
		{
			name:         "SSH with verbose dry-run",
			args:         []string{"clone", githubOctocat, flagSSH, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "All performance flags dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagWorkers, "3", flagTimeout, "20s", "--max-retries", "2"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Fork mode dry-run with all flags",
			args:         []string{"clone", flagSource, githubOctocat, flagTarget, "github.com/testorg", flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

func testOutputFormatDryRuns(t *testing.T) {
	tests := []testCase{
		{
			name:         "Verbose output dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput, usingProviderMsg, hostMsg, organizationMsg},
			timeout:      testTimeout,
		},
		{
			name:         "Quiet dry-run (no verbose)",
			args:         []string{"clone", githubOctocat, flagDryRun},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Credential status in verbose dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagVerbose},
			wantExit:     0,
			wantContains: []string{dryRunOutput, credentialStatusMsg},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}

// TestEdgeCaseDryRuns tests edge cases and boundary conditions with dry-run
func TestEdgeCaseDryRuns(t *testing.T) {
	// Build the binary first
	buildCmd := exec.Command("go", "build", "-o", binaryName, buildPath)
	if err := buildCmd.Run(); err != nil {
		t.Fatalf(failedToBuildBinaryMsg, err)
	}
	defer os.Remove(binaryName)

	tests := []testCase{
		{
			name:         "Zero max-repos dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagMaxRepos, "0"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Very high worker count dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagWorkers, "50"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Long timeout dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagTimeout, "5m"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
		{
			name:         "Single repo max limit dry-run",
			args:         []string{"clone", githubOctocat, flagDryRun, flagMaxRepos, "1"},
			wantExit:     0,
			wantContains: []string{dryRunOutput},
			timeout:      testTimeout,
		},
	}

	runTestCases(t, tests)
}
