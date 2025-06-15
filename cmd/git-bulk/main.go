// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/clone"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/config"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/provider"
	sshauth "github.com/ModeSevenIndustrialSolutions/git-bulk/internal/ssh"
	"github.com/ModeSevenIndustrialSolutions/git-bulk/internal/worker"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type Config struct {
	OutputDir                string
	Workers                  int
	MaxRetries               int
	RetryDelay               time.Duration
	QueueSize                int
	Timeout                  time.Duration
	CloneTimeout             time.Duration // Individual clone operation timeout
	NetworkTimeout           time.Duration // Network timeout for git operations
	UseSSH                   bool
	DryRun                   bool
	Verbose                  bool
	MaxRepos                 int
	CloneArchived            bool // Whether to clone archived/read-only repositories
	DisableCredentialHelpers bool // Disable git credential helpers to prevent password prompts
	GitHubToken              string
	GitLabToken              string
	GerritUser               string
	GerritPass               string
	GerritToken              string
	CredentialsFile          string
	SourceOrg                string
	TargetOrg                string
	SyncMode                 bool
}

func main() {
	var cfg Config

	rootCmd := &cobra.Command{
		Use:   "git-bulk",
		Short: "Bulk Git repository operations tool",
		Long: `git-bulk is a tool for performing bulk operations on Git repositories
across multiple hosting providers including GitHub, GitLab, and Gerrit.`,
		Version: fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
	}

	cloneCmd := &cobra.Command{
		Use:   "clone [<source>]",
		Short: "Clone repositories from a Git hosting provider",
		Long: `Clone repositories from GitHub, GitLab, or Gerrit.

Examples:
  git-bulk clone github.com/myorg                              # Clone all repos from GitHub org
  git-bulk clone gitlab.com/mygroup                            # Clone all repos from GitLab group
  git-bulk clone https://gerrit.example.com                    # Clone all repos from Gerrit
  git-bulk clone git@github.com:myorg/myrepo.git               # Clone specific repo
  git-bulk clone github.com/myorg --dry-run                    # Show what would be cloned
  git-bulk clone github.com/myorg --ssh                        # Use SSH for cloning
  git-bulk clone github.com/myorg --max-repos 10               # Limit to 10 repositories
  git-bulk clone --source github.com/sourceorg --target github.com/targetorg      # Same-provider fork
  git-bulk clone --source github.com/sourceorg --target gitlab.com/targetgroup   # Cross-provider fork
  git-bulk clone --source gerrit.example.com --target github.com/targetorg       # Gerrit to GitHub`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var source string
			if len(args) > 0 {
				source = args[0]
			}
			return runClone(cfg, source)
		},
	}

	sshCmd := &cobra.Command{
		Use:   "ssh-setup",
		Short: "Validate SSH authentication setup",
		Long: `Validate that SSH authentication is properly configured for Git operations.

This command checks:
- SSH agent availability and connection
- SSH key files in ~/.ssh/
- Connectivity to common Git hosting providers
- Proper SSH configuration

Examples:
  git-bulk ssh-setup                 # Basic SSH setup validation
  git-bulk ssh-setup --verbose       # Detailed SSH setup information`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSSHSetup(cfg)
		},
	}

	// Global flags
	rootCmd.PersistentFlags().BoolVarP(&cfg.Verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().DurationVar(&cfg.Timeout, "timeout", 30*time.Minute, "Overall operation timeout")

	// Clone command flags
	cloneCmd.Flags().StringVarP(&cfg.OutputDir, "output", "o", ".", "Output directory for cloned repositories")
	cloneCmd.Flags().IntVarP(&cfg.Workers, "workers", "w", 4, "Number of concurrent workers")
	cloneCmd.Flags().IntVar(&cfg.MaxRetries, "max-retries", 3, "Maximum number of retries per repository")
	cloneCmd.Flags().DurationVar(&cfg.RetryDelay, "retry-delay", 5*time.Second, "Delay between retries")
	cloneCmd.Flags().IntVar(&cfg.QueueSize, "queue-size", 100, "Worker queue size")
	cloneCmd.Flags().DurationVar(&cfg.CloneTimeout, "clone-timeout", 30*time.Minute, "Timeout for individual clone operations")
	cloneCmd.Flags().DurationVar(&cfg.NetworkTimeout, "network-timeout", 5*time.Minute, "Network timeout for git operations")
	cloneCmd.Flags().BoolVar(&cfg.UseSSH, "ssh", false, "Use SSH for cloning")
	cloneCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Show what would be done without actually doing it")
	cloneCmd.Flags().IntVar(&cfg.MaxRepos, "max-repos", 0, "Maximum number of repositories to process (0 = unlimited)")
	cloneCmd.Flags().BoolVar(&cfg.CloneArchived, "clone-archived", false, "Include archived/read-only repositories in cloning (by default they are skipped)")
	cloneCmd.Flags().BoolVar(&cfg.DisableCredentialHelpers, "disable-credential-helpers", true, "Disable git credential helpers to prevent password prompts (recommended for bulk operations)")

	// Authentication flags
	cloneCmd.Flags().StringVar(&cfg.GitHubToken, "github-token", "", "GitHub personal access token (or set GITHUB_TOKEN)")
	cloneCmd.Flags().StringVar(&cfg.GitLabToken, "gitlab-token", "", "GitLab personal access token (or set GITLAB_TOKEN)")
	cloneCmd.Flags().StringVar(&cfg.GerritUser, "gerrit-user", "", "Gerrit username (or set GERRIT_USERNAME)")
	cloneCmd.Flags().StringVar(&cfg.GerritPass, "gerrit-pass", "", "Gerrit password (or set GERRIT_PASSWORD)")
	cloneCmd.Flags().StringVar(&cfg.GerritToken, "gerrit-token", "", "Gerrit token (or set GERRIT_TOKEN)")
	cloneCmd.Flags().StringVar(&cfg.CredentialsFile, "credentials-file", "", "Path to credentials file (default: auto-detect)")

	// Fork/sync flags
	cloneCmd.Flags().StringVar(&cfg.SourceOrg, "source", "", "Source organization/user to fork from")
	cloneCmd.Flags().StringVar(&cfg.TargetOrg, "target", "", "Target organization to fork to (requires --source)")
	cloneCmd.Flags().BoolVar(&cfg.SyncMode, "sync", false, "Update existing forks (use with --target)")

	rootCmd.AddCommand(cloneCmd, sshCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

// isValidSourceFormat performs basic validation of source format to catch obvious errors early
func isValidSourceFormat(source string) bool {
	// Reject obviously invalid sources
	if source == "" || source == "invalid-source" || len(source) < 3 {
		return false
	}

	// Must contain either a dot (domain) or slash (path)
	if !strings.Contains(source, ".") && !strings.Contains(source, "/") {
		return false
	}

	// Reject sources with only special characters
	if strings.Trim(source, ".-_/") == "" {
		return false
	}

	return true
}

func runClone(cfg Config, source string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	// Validate input arguments
	if cfg.SourceOrg != "" && cfg.TargetOrg == "" {
		return fmt.Errorf("--target is required when using --source")
	}
	if cfg.TargetOrg != "" && cfg.SourceOrg == "" {
		return fmt.Errorf("--source is required when using --target")
	}
	if cfg.SyncMode && cfg.TargetOrg == "" {
		return fmt.Errorf("--sync can only be used with --target")
	}
	if cfg.SourceOrg != "" && source != "" {
		return fmt.Errorf("cannot specify both <source> argument and --source flag")
	}
	if cfg.SourceOrg == "" && source == "" {
		return fmt.Errorf("must specify either <source> argument or --source flag")
	}

	// Early validation of source format to prevent unnecessary network operations
	if source != "" && !isValidSourceFormat(source) {
		return fmt.Errorf("invalid source format: %s", source)
	}

	// Use source flag if provided, otherwise use positional argument
	if cfg.SourceOrg != "" {
		source = cfg.SourceOrg
	}

	// Fork mode vs regular clone mode
	if cfg.TargetOrg != "" {
		return runForkMode(ctx, cfg, source)
	}

	return runRegularClone(ctx, cfg, source)
}

func runForkMode(ctx context.Context, cfg Config, source string) error {
	fmt.Printf("Fork mode: %s -> %s\n", source, cfg.TargetOrg)
	if cfg.SyncMode {
		fmt.Println("Sync mode enabled: will update existing forks")
	}

	// Setup credentials and providers
	sourceProvider, targetProvider, sourceInfo, targetInfo, err := setupForkProviders(cfg, source)
	if err != nil {
		return err
	}

	// Check if this is cross-provider forking
	isCrossProvider := sourceProvider.Name() != targetProvider.Name()
	if isCrossProvider {
		fmt.Printf("Cross-provider operation: %s -> %s\n", sourceProvider.Name(), targetProvider.Name())
	}

	// Get repositories to fork
	repos, err := getRepositoriesToFork(ctx, cfg, sourceProvider, sourceInfo)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Println("No repositories found.")
		return nil
	}

	if cfg.DryRun {
		printDryRunForks(repos, targetInfo.Organization)
		return nil
	}

	// Process forks
	forkResults := processForks(ctx, cfg, sourceProvider, targetProvider, repos, targetInfo.Organization, isCrossProvider)

	// Print summary
	printForkSummary(repos, forkResults, cfg.SyncMode)

	return nil
}

func runRegularClone(ctx context.Context, cfg Config, source string) error {

	// Early validation to prevent unnecessary API calls
	if source == "" || source == "invalid-source" || (!strings.Contains(source, ".") && !strings.Contains(source, "/")) {
		return fmt.Errorf("invalid source: %s", source)
	}
	var credentialsLoader *config.CredentialsLoader
	if cfg.CredentialsFile != "" {
		credentialsLoader = config.NewCredentialsLoader(cfg.CredentialsFile)
	} else {
		// Auto-detect credentials file
		if credentialsPath := config.FindCredentialsFile(); credentialsPath != "" {
			credentialsLoader = config.NewCredentialsLoader(credentialsPath)
			if cfg.Verbose {
				fmt.Printf("Using credentials file: %s\n", credentialsPath)
			}
		} else {
			credentialsLoader = config.NewCredentialsLoader("")
		}
	}

	if err := credentialsLoader.LoadCredentials(); err != nil {
		return fmt.Errorf("failed to load credentials: %w", err)
	}

	// Get authentication from flags, credentials file, or environment (in that order)
	if cfg.GitHubToken == "" {
		cfg.GitHubToken = credentialsLoader.GetCredential("GITHUB_TOKEN")
	}
	if cfg.GitLabToken == "" {
		cfg.GitLabToken = credentialsLoader.GetCredential("GITLAB_TOKEN")
	}
	if cfg.GerritUser == "" {
		cfg.GerritUser = credentialsLoader.GetCredential("GERRIT_USERNAME")
	}
	if cfg.GerritPass == "" {
		cfg.GerritPass = credentialsLoader.GetCredential("GERRIT_PASSWORD")
	}
	if cfg.GerritToken == "" {
		cfg.GerritToken = credentialsLoader.GetCredential("GERRIT_TOKEN")
	}

	// Show credential status in verbose mode
	if cfg.Verbose {
		credentials := credentialsLoader.ListCredentials()
		fmt.Println("Credential status:")
		for cred, available := range credentials {
			status := "❌"
			if available {
				status = "✅"
			}
			fmt.Printf("  %s %s\n", status, cred)
		}
		fmt.Println()
	}

	// Create provider manager
	providerConfig := &provider.Config{
		GitHubToken:    cfg.GitHubToken,
		GitLabToken:    cfg.GitLabToken,
		GerritUsername: cfg.GerritUser,
		GerritPassword: cfg.GerritPass,
		GerritToken:    cfg.GerritToken,
		EnableSSH:      cfg.UseSSH,
	}

	manager := provider.NewProviderManager(providerConfig)

	// Register providers - always register GitHub and GitLab for maximum compatibility
	if cfg.UseSSH {
		// Use SSH-enabled providers
		if githubProvider, err := provider.NewGitHubProviderWithSSH(cfg.GitHubToken, "", true); err == nil {
			manager.RegisterProvider("github", githubProvider)
		}

		if gitlabProvider, err := provider.NewGitLabProviderWithSSH(cfg.GitLabToken, "", true); err == nil {
			manager.RegisterProvider("gitlab", gitlabProvider)
		}
	} else {
		// Use standard HTTP providers
		if githubProvider, err := provider.NewGitHubProvider(cfg.GitHubToken, ""); err == nil {
			manager.RegisterProvider("github", githubProvider)
		}

		if gitlabProvider, err := provider.NewGitLabProvider(cfg.GitLabToken, ""); err == nil {
			manager.RegisterProvider("gitlab", gitlabProvider)
		}
	}

	// Don't pre-register Gerrit providers with empty URLs - let GetProviderForSource handle dynamic creation
	// This allows proper baseURL setting and SSH fallback to work correctly

	// Get provider for source
	prov, sourceInfo, err := manager.GetProviderForSource(source)
	if err != nil {
		return fmt.Errorf("failed to get provider for source %s: %w", source, err)
	}

	if cfg.Verbose {
		fmt.Printf("Using provider: %s\n", prov.Name())
		fmt.Printf("Host: %s\n", sourceInfo.Host)
		fmt.Printf("Organization: %s\n", sourceInfo.Organization)
		if sourceInfo.Path != "" {
			fmt.Printf("Path: %s\n", sourceInfo.Path)
		}
	}

	// List repositories
	fmt.Printf("Fetching repositories from %s...\n", sourceInfo.Host)
	repos, err := prov.ListRepositories(ctx, sourceInfo.Organization)
	if err != nil {
		return fmt.Errorf("failed to list repositories: %w", err)
	}

	if len(repos) == 0 {
		fmt.Println("No repositories found.")
		return nil
	}

	// Apply max repos limit
	if cfg.MaxRepos > 0 && len(repos) > cfg.MaxRepos {
		repos = repos[:cfg.MaxRepos]
		fmt.Printf("Limited to %d repositories.\n", cfg.MaxRepos)
	}

	fmt.Printf("Found %d repositories.\n", len(repos))

	if cfg.DryRun {
		fmt.Println("\nDry run - repositories that would be cloned:")
		for _, repo := range repos {
			cloneURL := repo.CloneURL
			if cfg.UseSSH && repo.SSHCloneURL != "" {
				cloneURL = repo.SSHCloneURL
			}
			fmt.Printf("  %s -> %s\n", repo.FullName, cloneURL)
		}
		return nil
	}

	// Show credential helper guidance in verbose mode for bulk operations
	if cfg.Verbose && !cfg.DryRun {
		if !cfg.DisableCredentialHelpers {
			fmt.Printf("⚠️  WARNING: Credential helpers are enabled. This may cause password prompts.\n")
			fmt.Printf("💡 RECOMMENDATION: Use --disable-credential-helpers flag to prevent prompts during bulk operations.\n")
		} else {
			fmt.Printf("✅ Credential helpers disabled - no password prompts expected.\n")
		}
		fmt.Println()
	}

	// Create clone manager
	workerConfig := &worker.Config{
		WorkerCount:   cfg.Workers,
		MaxRetries:    cfg.MaxRetries,
		RetryDelay:    cfg.RetryDelay,
		QueueSize:     cfg.QueueSize,
		LogVerbose:    cfg.Verbose,
		BackoffFactor: 2.0,
	}

	cloneConfig := &clone.Config{
		WorkerConfig:             workerConfig,
		OutputDir:                cfg.OutputDir,
		UseSSH:                   cfg.UseSSH,
		Verbose:                  cfg.Verbose,
		DryRun:                   cfg.DryRun,
		ContinueOnFail:           true,
		CloneTimeout:             cfg.CloneTimeout,
		NetworkTimeout:           cfg.NetworkTimeout,
		CloneArchived:            cfg.CloneArchived,
		DisableCredentialHelpers: cfg.DisableCredentialHelpers,
	}

	cloneManager := clone.NewManager(cloneConfig, prov, sourceInfo)

	// Ensure output directory exists
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Start cloning
	fmt.Printf("Cloning repositories to %s...\n", cfg.OutputDir)
	results, err := cloneManager.CloneRepositories(ctx, repos, cfg.OutputDir, false, cfg.UseSSH)
	if err != nil {
		return fmt.Errorf("failed to clone repositories: %w", err)
	}

	// Print results
	fmt.Println("\nClone results:")
	for _, result := range results {
		if result.Error != nil {
			// Format error for concise display
			errorMsg := formatErrorMessage(result.Error)
			fmt.Printf("❌ %s [%s]\n", result.Repository.FullName, errorMsg)
		} else {
			fmt.Printf("✅ %s\n", result.Repository.FullName)
		}
	}

	return nil
}

func runSSHSetup(cfg Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	fmt.Println("SSH Authentication Setup Validation")
	fmt.Println("====================================")

	// Validate SSH setup
	if err := sshauth.ValidateSSHSetup(ctx, cfg.Verbose); err != nil {
		fmt.Printf("❌ SSH setup validation failed: %v\n", err)
		return err
	}

	fmt.Println("✅ SSH authentication setup is working correctly!")

	if cfg.Verbose {
		fmt.Println("\nSSH Configuration Details:")

		// Show SSH agent status
		agentSocket := os.Getenv("SSH_AUTH_SOCK")
		if agentSocket != "" {
			fmt.Printf("  SSH Agent: %s\n", agentSocket)
		} else {
			fmt.Println("  SSH Agent: Not available")
		}

		// Show SSH key files
		homeDir, err := os.UserHomeDir()
		if err == nil {
			sshDir := filepath.Join(homeDir, ".ssh")
			if entries, err := os.ReadDir(sshDir); err == nil {
				fmt.Println("  SSH Keys:")
				for _, entry := range entries {
					if strings.HasPrefix(entry.Name(), "id_") && !strings.HasSuffix(entry.Name(), ".pub") {
						fmt.Printf("    - %s\n", entry.Name())
					}
				}
			}
		}
	}

	return nil
}

// cloneForkWithRemotes clones a forked repository locally and sets up proper remotes
func cloneForkWithRemotes(ctx context.Context, cfg Config, provider provider.Provider, sourceRepo *provider.Repository, targetOrg string) error {
	// Determine clone URLs for fork and upstream
	forkURL, upstreamURL := getCloneURLs(cfg, provider, sourceRepo, targetOrg)

	// Create local path
	localPath := filepath.Join(cfg.OutputDir, sourceRepo.Name)

	// Check if directory already exists
	if _, err := os.Stat(localPath); err == nil {
		if cfg.Verbose {
			fmt.Printf("Directory %s already exists, skipping clone\n", localPath)
		}
		return setupRemotes(localPath, forkURL, upstreamURL, cfg.Verbose)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the fork
	if err := runGitCommand(ctx, "clone", forkURL, localPath); err != nil {
		return fmt.Errorf("failed to clone fork: %w", err)
	}

	// Setup remotes
	return setupRemotes(localPath, forkURL, upstreamURL, cfg.Verbose)
}

// getCloneURLs determines the fork and upstream URLs based on configuration
func getCloneURLs(cfg Config, provider provider.Provider, sourceRepo *provider.Repository, targetOrg string) (string, string) {
	forkURL := sourceRepo.CloneURL
	upstreamURL := sourceRepo.CloneURL

	if cfg.UseSSH && sourceRepo.SSHCloneURL != "" {
		forkURL = sourceRepo.SSHCloneURL
		upstreamURL = sourceRepo.SSHCloneURL
	}

	// Adjust fork URL to point to target organization
	sourceOrg := strings.Split(sourceRepo.FullName, "/")[0]

	if provider.Name() == "github" || provider.Name() == "gitlab" {
		if cfg.UseSSH {
			forkURL = strings.Replace(forkURL, fmt.Sprintf(":%s/", sourceOrg), fmt.Sprintf(":%s/", targetOrg), 1)
		} else {
			forkURL = strings.Replace(forkURL, fmt.Sprintf("/%s/", sourceOrg), fmt.Sprintf("/%s/", targetOrg), 1)
		}
	}

	return forkURL, upstreamURL
}

// setupRemotes configures origin and upstream remotes for a forked repository
func setupRemotes(repoPath, originURL, upstreamURL string, verbose bool) error {
	// Change to repository directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			// Log error but don't fail the operation
			fmt.Printf("Warning: failed to restore original directory: %v\n", err)
		}
	}()

	if err := os.Chdir(repoPath); err != nil {
		return fmt.Errorf("failed to change to repository directory: %w", err)
	}

	// Set origin to point to the fork
	if err := runGitCommand(context.Background(), "remote", "set-url", "origin", originURL); err != nil {
		return fmt.Errorf("failed to set origin remote: %w", err)
	}

	// Add upstream remote pointing to original repository
	if err := runGitCommand(context.Background(), "remote", "add", "upstream", upstreamURL); err != nil {
		// If upstream already exists, update it
		if err := runGitCommand(context.Background(), "remote", "set-url", "upstream", upstreamURL); err != nil {
			return fmt.Errorf("failed to set upstream remote: %w", err)
		}
	}

	if verbose {
		fmt.Printf("Configured remotes:\n")
		fmt.Printf("  origin: %s\n", originURL)
		fmt.Printf("  upstream: %s\n", upstreamURL)
	}

	return nil
}

// runGitCommand executes a git command with the given arguments
func runGitCommand(ctx context.Context, args ...string) error {
	// Disable credential helpers to prevent password prompts during bulk operations
	// This prevents macOS keychain and other credential helpers from prompting
	gitArgs := []string{"-c", "credential.helper=", "-c", "core.askpass="}
	gitArgs = append(gitArgs, args...)

	cmd := exec.CommandContext(ctx, "git", gitArgs...)

	// Set environment variables to further prevent credential prompts
	cmd.Env = append(os.Environ(),
		"GIT_ASKPASS=",          // Disable askpass program
		"SSH_ASKPASS=",          // Disable SSH askpass program
		"GIT_TERMINAL_PROMPT=0", // Disable terminal prompting in Git 2.3+
	)

	cmd.Stdout = nil // Suppress output unless verbose
	cmd.Stderr = nil
	return cmd.Run()
}

// formatErrorMessage formats an error message for concise display
func formatErrorMessage(err error) string {
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

	// Extract first meaningful line, truncate if too long
	firstLine := strings.Split(errStr, "\n")[0]
	if len(firstLine) > 80 {
		firstLine = firstLine[:77] + "..."
	}

	return strings.TrimSpace(firstLine)
}

// setupForkProviders configures and validates providers for fork operations
func setupForkProviders(cfg Config, source string) (provider.Provider, provider.Provider, *provider.SourceInfo, *provider.SourceInfo, error) {
	// Load credentials
	credentialsLoader := loadCredentials(cfg)

	// Update config with loaded credentials
	updateConfigWithCredentials(&cfg, credentialsLoader)

	// Show credentials in verbose mode
	if cfg.Verbose {
		showCredentialStatus(credentialsLoader)
	}

	// Create and setup provider manager
	manager := createProviderManager(cfg)

	// Get providers
	sourceProvider, sourceInfo, err := manager.GetProviderForSource(source)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get provider for source %s: %w", source, err)
	}

	targetProvider, targetInfo, err := manager.GetProviderForSource(cfg.TargetOrg)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to get provider for target %s: %w", cfg.TargetOrg, err)
	}

	// Validate providers
	if err := validateForkProviders(sourceProvider, targetProvider); err != nil {
		return nil, nil, nil, nil, err
	}

	if cfg.Verbose {
		fmt.Printf("Using provider: %s\n", sourceProvider.Name())
		fmt.Printf("Source: %s\n", sourceInfo.Organization)
		fmt.Printf("Target: %s\n", targetInfo.Organization)
	}

	return sourceProvider, targetProvider, sourceInfo, targetInfo, nil
}

// loadCredentials loads credentials from file or environment
func loadCredentials(cfg Config) *config.CredentialsLoader {
	var credentialsLoader *config.CredentialsLoader
	if cfg.CredentialsFile != "" {
		credentialsLoader = config.NewCredentialsLoader(cfg.CredentialsFile)
	} else {
		if credentialsPath := config.FindCredentialsFile(); credentialsPath != "" {
			credentialsLoader = config.NewCredentialsLoader(credentialsPath)
			if cfg.Verbose {
				fmt.Printf("Using credentials file: %s\n", credentialsPath)
			}
		} else {
			credentialsLoader = config.NewCredentialsLoader("")
		}
	}

	if err := credentialsLoader.LoadCredentials(); err != nil {
		fmt.Printf("Warning: failed to load credentials: %v\n", err)
	}

	return credentialsLoader
}

// updateConfigWithCredentials updates the configuration with loaded credentials
func updateConfigWithCredentials(cfg *Config, credentialsLoader *config.CredentialsLoader) {
	if cfg.GitHubToken == "" {
		cfg.GitHubToken = credentialsLoader.GetCredential("GITHUB_TOKEN")
	}
	if cfg.GitLabToken == "" {
		cfg.GitLabToken = credentialsLoader.GetCredential("GITLAB_TOKEN")
	}
	if cfg.GerritUser == "" {
		cfg.GerritUser = credentialsLoader.GetCredential("GERRIT_USERNAME")
	}
	if cfg.GerritPass == "" {
		cfg.GerritPass = credentialsLoader.GetCredential("GERRIT_PASSWORD")
	}
	if cfg.GerritToken == "" {
		cfg.GerritToken = credentialsLoader.GetCredential("GERRIT_TOKEN")
	}
}

// showCredentialStatus displays credential availability in verbose mode
func showCredentialStatus(credentialsLoader *config.CredentialsLoader) {
	credentials := credentialsLoader.ListCredentials()
	fmt.Println("Credential status:")
	for cred, available := range credentials {
		status := "❌"
		if available {
			status = "✅"
		}
		fmt.Printf("  %s %s\n", status, cred)
	}
	fmt.Println()
}

// createProviderManager creates and configures a provider manager
func createProviderManager(cfg Config) *provider.Manager {
	providerConfig := &provider.Config{
		GitHubToken:    cfg.GitHubToken,
		GitLabToken:    cfg.GitLabToken,
		GerritUsername: cfg.GerritUser,
		GerritPassword: cfg.GerritPass,
		GerritToken:    cfg.GerritToken,
		EnableSSH:      cfg.UseSSH,
	}

	manager := provider.NewProviderManager(providerConfig)

	// Register providers
	registerProviders(manager, cfg)

	return manager
}

// registerProviders registers appropriate providers based on configuration
func registerProviders(manager *provider.Manager, cfg Config) {
	if cfg.UseSSH {
		if githubProvider, err := provider.NewGitHubProviderWithSSH(cfg.GitHubToken, "", true); err == nil {
			manager.RegisterProvider("github", githubProvider)
		}
		if gitlabProvider, err := provider.NewGitLabProviderWithSSH(cfg.GitLabToken, "", true); err == nil {
			manager.RegisterProvider("gitlab", gitlabProvider)
		}
	} else {
		if githubProvider, err := provider.NewGitHubProvider(cfg.GitHubToken, ""); err == nil {
			manager.RegisterProvider("github", githubProvider)
		}
		if gitlabProvider, err := provider.NewGitLabProvider(cfg.GitLabToken, ""); err == nil {
			manager.RegisterProvider("gitlab", gitlabProvider)
		}
	}
}

// validateForkProviders validates that forking is supported between providers
func validateForkProviders(sourceProvider, targetProvider provider.Provider) error {
	// Gerrit cannot be used as a target for cross-provider forking
	if targetProvider.Name() == "gerrit" {
		return fmt.Errorf("gerrit cannot be used as a target provider for forking operations")
	}

	// Gerrit can be used as a source, but only for cross-provider clone operations
	if sourceProvider.Name() == "gerrit" && targetProvider.Name() == "gerrit" {
		return fmt.Errorf("forking between gerrit instances is not supported")
	}

	return nil
}

// getRepositoriesToFork fetches and filters repositories for forking
func getRepositoriesToFork(ctx context.Context, cfg Config, sourceProvider provider.Provider, sourceInfo *provider.SourceInfo) ([]*provider.Repository, error) {
	fmt.Printf("Fetching repositories from %s...\n", sourceInfo.Organization)
	repos, err := sourceProvider.ListRepositories(ctx, sourceInfo.Organization)
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	// Apply max repos limit
	if cfg.MaxRepos > 0 && len(repos) > cfg.MaxRepos {
		repos = repos[:cfg.MaxRepos]
		fmt.Printf("Limited to %d repositories.\n", cfg.MaxRepos)
	}

	fmt.Printf("Found %d repositories to fork.\n", len(repos))
	return repos, nil
}

// printDryRunForks displays what would be forked in dry run mode
func printDryRunForks(repos []*provider.Repository, targetOrg string) {
	fmt.Println("\nDry run - repositories that would be forked:")
	for _, repo := range repos {
		fmt.Printf("  %s -> %s/%s\n", repo.FullName, targetOrg, repo.Name)
	}
}

// processForks processes multiple repositories for forking
func processForks(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repos []*provider.Repository, targetOrg string, isCrossProvider bool) map[string]string {
	// Use parallel processing instead of sequential
	return processForksInParallel(ctx, cfg, sourceProvider, targetProvider, repos, targetOrg, isCrossProvider)
}

// processForksInParallel processes multiple repositories for forking using the worker pool
func processForksInParallel(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repos []*provider.Repository, targetOrg string, isCrossProvider bool) map[string]string {
	fmt.Printf("🚀 Starting parallel fork processing for %d repositories...\n", len(repos))

	// Create worker pool for fork operations
	workerConfig := &worker.Config{
		WorkerCount:   cfg.Workers,
		MaxRetries:    cfg.MaxRetries,
		RetryDelay:    cfg.RetryDelay,
		QueueSize:     cfg.QueueSize,
		LogVerbose:    cfg.Verbose,
		BackoffFactor: 2.0,
	}

	pool := worker.NewPool(workerConfig)
	pool.Start()
	defer pool.Stop()

	// Channel to collect results
	results := make(chan *ForkResult, len(repos))
	forkResults := make(map[string]string)

	// Start result collector
	go func() {
		for result := range results {
			if result.Error != nil {
				fmt.Printf("❌ %s (%s)\n", result.Repository.FullName, result.Status)
			} else {
				status := "✅"
				switch result.Status {
				case "exists":
					status = "📁"
				case "synced":
					status = "🔄"
				}
				fmt.Printf("%s %s (%s)\n", status, result.Repository.FullName, result.Status)
			}
			forkResults[result.Repository.FullName] = result.Status
		}
	}()
	// Submit fork jobs
	submittedJobs := 0
	for i, repo := range repos {
		jobConfig := &ForkJobConfig{
			Config:          cfg,
			SourceProvider:  sourceProvider,
			TargetProvider:  targetProvider,
			Repository:      repo,
			TargetOrg:       targetOrg,
			IsCrossProvider: isCrossProvider,
			Results:         results,
			Index:           i + 1,
			Total:           len(repos),
		}

		job := &worker.Job{
			ID:          fmt.Sprintf("fork-%s", repo.ID),
			Description: fmt.Sprintf("Fork repository %s", repo.FullName),
			Execute:     createForkJobExecutor(ctx, jobConfig),
			MaxRetries:  cfg.MaxRetries,
		}

		if err := pool.Submit(job); err != nil {
			fmt.Printf("❌ Failed to submit fork job for %s: %v\n", repo.Name, err)
			results <- &ForkResult{
				Repository: repo,
				Status:     "submission failed",
				Error:      err,
			}
		} else {
			submittedJobs++
		}
	}

	fmt.Printf("📤 Submitted %d fork jobs to worker pool\n", submittedJobs)

	// Wait for all jobs to complete
	waitForForkCompletion(ctx, pool, submittedJobs, len(repos))

	// Close results channel
	close(results)

	return forkResults
}

// ForkJobConfig contains configuration for a fork job
type ForkJobConfig struct {
	Config          Config
	SourceProvider  provider.Provider
	TargetProvider  provider.Provider
	Repository      *provider.Repository
	TargetOrg       string
	IsCrossProvider bool
	Results         chan<- *ForkResult
	Index           int
	Total           int
}

// createForkJobExecutor creates the executor function for a fork job
func createForkJobExecutor(ctx context.Context, jobConfig *ForkJobConfig) func(context.Context) error {
	return func(jobCtx context.Context) error {
		startTime := time.Now()

		fmt.Printf("\n[%d/%d] Processing repository: %s\n", jobConfig.Index, jobConfig.Total, jobConfig.Repository.FullName)

		status := processSingleFork(ctx, jobConfig.Config, jobConfig.SourceProvider, jobConfig.TargetProvider, jobConfig.Repository, jobConfig.TargetOrg, jobConfig.IsCrossProvider)

		var err error
		if strings.Contains(status, "failed") {
			err = fmt.Errorf("fork operation failed: %s", status)
		}

		jobConfig.Results <- &ForkResult{
			Repository: jobConfig.Repository,
			Status:     status,
			Error:      err,
			Duration:   time.Since(startTime),
		}

		return err
	}
}

// waitForForkCompletion waits for all fork jobs to complete
func waitForForkCompletion(ctx context.Context, pool *worker.Pool, expectedJobs, totalRepos int) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastProgress := time.Now()
	lastCompletedCount := int64(0)

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n⚠️  Fork operation cancelled by timeout")
			return
		case <-ticker.C:
			stats := pool.GetStats()
			currentCompleted := stats.CompletedJobs + stats.FailedJobs

			// Check if all jobs are complete
			if stats.ActiveJobs == 0 && currentCompleted >= int64(expectedJobs) {
				fmt.Printf("\n🎉 All %d fork jobs completed\n", expectedJobs)
				return
			}

			// Show progress updates
			if currentCompleted > lastCompletedCount {
				lastProgress = time.Now()
				lastCompletedCount = currentCompleted

				if expectedJobs > 0 {
					percentage := float64(currentCompleted) / float64(expectedJobs) * 100
					fmt.Printf("📊 Fork Progress: %.1f%% (%d/%d completed, %d active, %d failed)\n",
						percentage, currentCompleted, expectedJobs, stats.ActiveJobs, stats.FailedJobs)
				}
			}

			// Timeout detection
			if time.Since(lastProgress) >= 5*time.Minute {
				fmt.Printf("\n⚠️  Warning: No progress for 5 minutes with %d active jobs\n", stats.ActiveJobs)
				if stats.ActiveJobs == 0 {
					fmt.Printf("❌ No active jobs but only %d/%d completed - operation may have stalled\n", currentCompleted, expectedJobs)
					return
				}
			}
		}
	}
}

// processSingleFork processes a single repository fork
func processSingleFork(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string, isCrossProvider bool) string {
	// For cross-provider operations, check existence using target provider
	// For same-provider operations, use source provider (original behavior)
	checkProvider := sourceProvider
	if isCrossProvider {
		checkProvider = targetProvider
	}

	// Check if repository already exists in target
	exists, err := checkProvider.RepositoryExists(ctx, targetOrg, repo.Name)
	if err != nil {
		fmt.Printf("❌ Failed to check if repository exists: %v\n", err)
		return "check failed"
	}

	if exists {
		return handleExistingRepository(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, isCrossProvider)
	}

	return createNewRepository(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, isCrossProvider)
}

// handleExistingRepository handles the case where a repository already exists
func handleExistingRepository(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string, isCrossProvider bool) string {
	fmt.Printf("Repository %s/%s already exists\n", targetOrg, repo.Name)

	if cfg.SyncMode {
		fmt.Printf("Syncing existing repository %s/%s...\n", targetOrg, repo.Name)

		if isCrossProvider {
			// For cross-provider sync, we need to pull from source and push to target
			return syncCrossProviderRepository(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg)
		} else {
			// Same-provider sync using the provider's sync method
			targetRepo := &provider.Repository{
				ID:       repo.ID,
				Name:     repo.Name,
				FullName: fmt.Sprintf("%s/%s", targetOrg, repo.Name),
			}

			if err := sourceProvider.SyncRepository(ctx, targetRepo); err != nil {
				fmt.Printf("❌ Failed to sync repository: %v\n", err)
				return "sync failed"
			}

			fmt.Printf("✅ Repository synced successfully\n")
			cloneRepositoryIfNeeded(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, isCrossProvider)
			return "synced"
		}
	}

	fmt.Printf("✅ Repository already exists (skipping)\n")
	cloneRepositoryIfNeeded(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, isCrossProvider)
	return "exists"
}

// createNewRepository creates a new repository (fork or cross-provider clone)
func createNewRepository(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string, isCrossProvider bool) string {
	if isCrossProvider {
		// Cross-provider: create new repo and push content
		return createCrossProviderRepository(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg)
	} else {
		// Same-provider: use native fork API
		return createNativeFork(ctx, cfg, sourceProvider, repo, targetOrg)
	}
}

// cloneForkIfNeeded clones the fork locally if needed
func cloneForkIfNeeded(ctx context.Context, cfg Config, sourceProvider provider.Provider, repo *provider.Repository, targetOrg string) {
	if err := cloneForkWithRemotes(ctx, cfg, sourceProvider, repo, targetOrg); err != nil {
		fmt.Printf("❌ Failed to clone fork locally: %v\n", err)
	} else {
		fmt.Printf("✅ Fork cloned locally with proper remotes\n")
	}
}

// cloneRepositoryIfNeeded clones the repository locally with proper remotes (cross-provider aware)
func cloneRepositoryIfNeeded(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string, isCrossProvider bool) {
	if isCrossProvider {
		if err := cloneCrossProviderWithRemotes(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg); err != nil {
			fmt.Printf("❌ Failed to clone repository locally: %v\n", err)
		} else {
			fmt.Printf("✅ Repository cloned locally with proper remotes\n")
		}
	} else {
		// Use existing same-provider logic
		cloneForkIfNeeded(ctx, cfg, sourceProvider, repo, targetOrg)
	}
}

// createNativeFork creates a fork using the provider's native fork API
func createNativeFork(ctx context.Context, cfg Config, sourceProvider provider.Provider, repo *provider.Repository, targetOrg string) string {
	fmt.Printf("Creating fork %s/%s...\n", targetOrg, repo.Name)

	fork, err := sourceProvider.CreateFork(ctx, repo, targetOrg)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "already forked") {
			fmt.Printf("✅ Fork already exists\n")
			cloneForkIfNeeded(ctx, cfg, sourceProvider, repo, targetOrg)
			return "exists"
		}

		fmt.Printf("❌ Failed to create fork: %v\n", err)
		return "failed"
	}

	fmt.Printf("✅ Fork created: %s\n", fork.FullName)
	cloneForkIfNeeded(ctx, cfg, sourceProvider, repo, targetOrg)
	return "created"
}

// createCrossProviderRepository creates a repository in target provider and copies content from source
func createCrossProviderRepository(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string) string {
	fmt.Printf("Creating cross-provider repository %s/%s...\n", targetOrg, repo.Name)

	// Create empty repository in target provider
	newRepo, err := targetProvider.CreateRepository(ctx, targetOrg, repo.Name, repo.Description, repo.Private)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			fmt.Printf("✅ Repository already exists\n")
			cloneRepositoryIfNeeded(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, true)
			return "exists"
		}

		fmt.Printf("❌ Failed to create repository: %v\n", err)
		return "failed"
	}

	fmt.Printf("✅ Repository created: %s\n", newRepo.FullName)

	// Clone source and push to target
	if err := cloneAndPushCrossProvider(ctx, cfg, sourceProvider, targetProvider, repo, newRepo, targetOrg); err != nil {
		fmt.Printf("❌ Failed to copy repository content: %v\n", err)
		return "content copy failed"
	}

	fmt.Printf("✅ Repository content copied successfully\n")
	return "created"
}

// syncCrossProviderRepository syncs a cross-provider repository
func syncCrossProviderRepository(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, repo *provider.Repository, targetOrg string) string {
	fmt.Printf("Cross-provider sync not yet implemented\n")
	cloneRepositoryIfNeeded(ctx, cfg, sourceProvider, targetProvider, repo, targetOrg, true)
	return "sync not implemented"
}

// cloneCrossProviderWithRemotes sets up cross-provider repository with proper remotes
func cloneCrossProviderWithRemotes(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, sourceRepo *provider.Repository, targetOrg string) error {
	// Determine clone URLs for target and source
	targetURL, sourceURL := getCrossProviderCloneURLs(cfg, sourceProvider, targetProvider, sourceRepo, targetOrg)

	// Create local path
	localPath := filepath.Join(cfg.OutputDir, sourceRepo.Name)

	// Check if directory already exists
	if _, err := os.Stat(localPath); err == nil {
		if cfg.Verbose {
			fmt.Printf("Directory %s already exists, skipping clone\n", localPath)
		}
		return setupRemotes(localPath, targetURL, sourceURL, cfg.Verbose)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the target repository (which should have the content)
	if err := runGitCommand(ctx, "clone", targetURL, localPath); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	// Setup remotes
	return setupRemotes(localPath, targetURL, sourceURL, cfg.Verbose)
}

// getCrossProviderCloneURLs gets clone URLs for cross-provider setup
func getCrossProviderCloneURLs(cfg Config, sourceProvider, targetProvider provider.Provider, sourceRepo *provider.Repository, targetOrg string) (string, string) {
	// Target URL (origin)
	targetURL := sourceRepo.CloneURL
	if cfg.UseSSH && sourceRepo.SSHCloneURL != "" {
		targetURL = sourceRepo.SSHCloneURL
	}

	// Adjust target URL to point to target organization and provider
	// This is a simplified approach - in a real implementation, you'd need
	// to construct the proper URLs based on the target provider's host
	if targetProvider.Name() == "github" {
		if cfg.UseSSH {
			targetURL = fmt.Sprintf("git@github.com:%s/%s.git", targetOrg, sourceRepo.Name)
		} else {
			targetURL = fmt.Sprintf("https://github.com/%s/%s.git", targetOrg, sourceRepo.Name)
		}
	} else if targetProvider.Name() == "gitlab" {
		if cfg.UseSSH {
			targetURL = fmt.Sprintf("git@gitlab.com:%s/%s.git", targetOrg, sourceRepo.Name)
		} else {
			targetURL = fmt.Sprintf("https://gitlab.com/%s/%s.git", targetOrg, sourceRepo.Name)
		}
	}

	// Source URL (upstream) - keep original
	sourceURL := sourceRepo.CloneURL
	if cfg.UseSSH && sourceRepo.SSHCloneURL != "" {
		sourceURL = sourceRepo.SSHCloneURL
	}

	return targetURL, sourceURL
}

// cloneAndPushCrossProvider handles the clone-and-push workflow for cross-provider operations
func cloneAndPushCrossProvider(ctx context.Context, cfg Config, sourceProvider, targetProvider provider.Provider, sourceRepo, targetRepo *provider.Repository, targetOrg string) error {
	// Create temporary directory for the operation
	tempDir := filepath.Join(cfg.OutputDir, ".tmp-"+sourceRepo.Name)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			// Log error but don't fail the operation
			fmt.Printf("Warning: failed to clean up temporary directory %s: %v\n", tempDir, err)
		}
	}() // Clean up

	// Clone source repository
	sourceURL := sourceRepo.CloneURL
	if cfg.UseSSH && sourceRepo.SSHCloneURL != "" {
		sourceURL = sourceRepo.SSHCloneURL
	}

	if err := runGitCommand(ctx, "clone", "--bare", sourceURL, tempDir); err != nil {
		return fmt.Errorf("failed to clone source repository: %w", err)
	}

	// Change to temp directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			// Log error but don't fail the operation
			fmt.Printf("Warning: failed to restore original directory: %v\n", err)
		}
	}()

	if err := os.Chdir(tempDir); err != nil {
		return fmt.Errorf("failed to change to temp directory: %w", err)
	}

	// Set up target remote
	targetURL := targetRepo.CloneURL
	if cfg.UseSSH && targetRepo.SSHCloneURL != "" {
		targetURL = targetRepo.SSHCloneURL
	}

	// Push all branches and tags to target
	if err := runGitCommand(ctx, "push", "--mirror", targetURL); err != nil {
		return fmt.Errorf("failed to push to target repository: %w", err)
	}

	return nil
}

// ForkTask represents a single fork operation task
type ForkTask struct {
	Repository      *provider.Repository
	SourceProvider  provider.Provider
	TargetProvider  provider.Provider
	TargetOrg       string
	IsCrossProvider bool
	Config          Config
}

// ForkResult represents the result of a fork operation
type ForkResult struct {
	Repository *provider.Repository
	Status     string
	Error      error
	Duration   time.Duration
}

// printForkSummary prints the final summary of fork operations
func printForkSummary(repos []*provider.Repository, forkResults map[string]string, syncMode bool) {
	fmt.Println("\n==================================================")
	fmt.Println("Fork Summary:")
	fmt.Println("==================================================")

	// Count results by status
	created := 0
	exists := 0
	synced := 0
	failed := 0

	for _, repo := range repos {
		status := forkResults[repo.FullName]
		switch status {
		case "created":
			created++
			fmt.Printf("✅ %s (created)\n", repo.FullName)
		case "exists":
			exists++
			fmt.Printf("📁 %s (already exists)\n", repo.FullName)
		case "synced":
			synced++
			fmt.Printf("🔄 %s (synced)\n", repo.FullName)
		default:
			failed++
			fmt.Printf("❌ %s (failed)\n", repo.FullName)
		}
	}

	fmt.Println("\nTotal:", len(repos), "repositories")
	if created > 0 {
		fmt.Printf("Created: %d\n", created)
	}
	if syncMode && synced > 0 {
		fmt.Printf("Synced: %d\n", synced)
	}
	fmt.Printf("Already exists: %d\n", exists)
	if failed > 0 {
		fmt.Printf("Failed: %d\n", failed)
	}
}
