// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2025 The Linux Foundation

package main

import (
	"context"
	"fmt"
	"log"
	"os"
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
	OutputDir       string
	Workers         int
	MaxRetries      int
	RetryDelay      time.Duration
	QueueSize       int
	Timeout         time.Duration
	UseSSH          bool
	DryRun          bool
	Verbose         bool
	MaxRepos        int
	GitHubToken     string
	GitLabToken     string
	GerritUser      string
	GerritPass      string
	GerritToken     string
	CredentialsFile string
	SourceOrg       string
	TargetOrg       string
	SyncMode        bool
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
  git-bulk clone github.com/myorg                    # Clone all repos from GitHub org
  git-bulk clone gitlab.com/mygroup                  # Clone all repos from GitLab group
  git-bulk clone https://gerrit.example.com          # Clone all repos from Gerrit
  git-bulk clone git@github.com:myorg/myrepo.git     # Clone specific repo
  git-bulk clone github.com/myorg --dry-run          # Show what would be cloned
  git-bulk clone github.com/myorg --ssh              # Use SSH for cloning
  git-bulk clone github.com/myorg --max-repos 10     # Limit to 10 repositories
  git-bulk clone --source github.com/sourceorg --target github.com/targetorg  # Fork repositories`,
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
	cloneCmd.Flags().BoolVar(&cfg.UseSSH, "ssh", false, "Use SSH for cloning")
	cloneCmd.Flags().BoolVar(&cfg.DryRun, "dry-run", false, "Show what would be done without actually doing it")
	cloneCmd.Flags().IntVar(&cfg.MaxRepos, "max-repos", 0, "Maximum number of repositories to process (0 = unlimited)")

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

	// TODO: Implement fork functionality
	// This would involve:
	// 1. List repositories from source organization
	// 2. For each repository, check if fork already exists in target org
	// 3. If not exists, create fork
	// 4. If exists and sync mode is enabled, update the fork
	// 5. Clone the forked repository to local directory

	return fmt.Errorf("fork functionality not yet implemented")
}

func runRegularClone(ctx context.Context, cfg Config, source string) error {

	// Load credentials from file if available
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
		WorkerConfig:   workerConfig,
		OutputDir:      cfg.OutputDir,
		UseSSH:         cfg.UseSSH,
		Verbose:        cfg.Verbose,
		DryRun:         cfg.DryRun,
		ContinueOnFail: true,
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
		status := "✅"
		if result.Error != nil {
			status = "❌"
		}
		fmt.Printf("  %s: %s\n", status, result.Repository.FullName)
		if result.Error != nil {
			fmt.Printf("    Error: %v\n", result.Error)
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
