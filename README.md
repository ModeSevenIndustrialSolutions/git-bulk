<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2025 The Linux Foundation -->

# Go Bulk Git Tools

A comprehensive suite of Go command-line tools for bulk Git repository operations with support for GitHub, GitLab, and Gerrit.

## Table of Contents

- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Usage](#usage)
  - [Clone Operations](#clone-operations)
  - [Fork Operations](#fork-operations)
  - [SSH Authentication](#ssh-authentication)
  - [Configuration](#configuration)
- [Advanced Features](#advanced-features)
  - [Cross-Provider Forking](#cross-provider-forking)
  - [Archive Filtering](#archive-filtering)
  - [Gerrit SSH Enumeration](#gerrit-ssh-enumeration)
- [Development](#development)
- [Testing](#testing)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Multi-platform Git hosting support**: GitHub, GitLab, and Gerrit
- **Cross-provider forking**: Fork/clone repositories between different Git hosting providers
- **SSH authentication**: Full SSH support with automatic key detection and hardware token support
- **Intelligent thread pooling**: Configurable worker threads with automatic rate limiting detection
- **Exponential backoff**: Automatic retry with exponential backoff for failed operations
- **Archive filtering**: Skip archived repositories by default with optional inclusion
- **Rich CLI interface**: Built with Cobra for comprehensive help and shell completion
- **Comprehensive testing**: Full test suite with coverage reporting
- **Modular design**: Reusable components for building additional tools
- **Password prompt prevention**: Automatic detection and prevention of credential helper prompts
- **Gerrit SSH enumeration**: SSH-based repository discovery for protected Gerrit instances

### SSH Authentication Implementation

#### Core Features

- **Auto-detection of SSH infrastructure**:
  - SSH agent socket path (`SSH_AUTH_SOCK`)
  - Common SSH key files (`~/.ssh/id_*`)
  - SSH config file (`~/.ssh/config`)
- **Multiple authentication methods**:
  - SSH Agent authentication (works with hardware tokens like Secretive)
  - SSH key file authentication with passphrase support
  - Host-specific SSH configurations
- **Git SSH wrapper generation**:
  - Automatic SSH wrapper script creation
  - Environment variable setup (`GIT_SSH`)
  - Provider-specific SSH options

#### Provider Integration

- **GitHub Provider**: SSH authentication with `git@github.com` format, port 22
- **GitLab Provider**: SSH authentication with `git@gitlab.com` format, port 22
- **Gerrit Provider**: SSH authentication with custom port (29418), format `ssh://host:29418/repo`

#### SSH Authentication Priority

1. **SSH Agent** (if `SSH_AUTH_SOCK` environment variable exists)
2. **SSH Key Files** (in order of preference):
   - `~/.ssh/id_ed25519`
   - `~/.ssh/id_rsa`
   - `~/.ssh/id_ecdsa`
   - `~/.ssh/id_dsa`
3. **SSH Config** (host-specific configurations from `~/.ssh/config`)

### Password Prompt Prevention

The tool implements comprehensive password prompt detection and prevention:

#### Detection Capabilities

- **Credential Helper Patterns**: Detects `osxkeychain`, `manager-core`, password prompts
- **SSH Fallback Scenarios**: Identifies when SSH auth fails and git falls back to HTTPS
- **Terminal Prompt Detection**: Recognizes various credential helper activation patterns

#### Prevention Mechanisms

- **Default Protection**: `--disable-credential-helpers=true` (default)
- **Git Command Enhancement**: Adds `-c credential.helper= -c core.askpass=` to git commands
- **Environment Variables**: Sets `GIT_ASKPASS="", SSH_ASKPASS="", GIT_TERMINAL_PROMPT=0`
- **Runtime Monitoring**: Analyzes git output for credential helper usage

#### User Guidance

Provides specific troubleshooting guidance when credential helpers are detected:

- Exact commands to prevent prompts
- SSH authentication setup guidance
- Configuration validation steps

## Installation

```bash
go install github.com/ModeSevenIndustrialSolutions/git-bulk/cmd/git-bulk@latest
```

Or build from source:

```bash
git clone https://github.com/ModeSevenIndustrialSolutions/git-bulk
cd git-bulk
make build

# Install to your Go bin path
make install

# Or install system-wide (requires sudo)
make install-system
```

## Quick Start

```bash
# Clone all repositories from a GitHub organization
git-bulk clone github.com/myorg --output ./repos

# Fork repositories from GitHub to GitLab (cross-provider)
git-bulk clone --source github.com/sourceorg --target gitlab.com/targetgroup

# Use SSH authentication
git-bulk clone github.com/myorg --ssh --output ./repos

# Validate SSH setup
git-bulk ssh-setup --verbose
```

## Usage

### Clone Operations

```bash
# Clone all repositories from a GitHub organization
git-bulk clone github.com/myorg --output ./repos

# Clone from GitLab
git-bulk clone gitlab.com/mygroup --output ./repos

# Clone from Gerrit
git-bulk clone https://gerrit.example.com --output ./repos

# Use SSH for cloning
git-bulk clone github.com/myorg --ssh --output ./repos

# Dry run to see what would be cloned
git-bulk clone github.com/myorg --dry-run --verbose

# Limit number of repositories
git-bulk clone github.com/myorg --max-repos 10 --output ./repos

# Include archived repositories (skipped by default)
git-bulk clone github.com/myorg --clone-archived --output ./repos

# Use custom credentials file
git-bulk clone github.com/myorg --credentials-file ./my-credentials --output ./repos
```

### Fork Operations

#### Same-Provider Forking

```bash
# GitHub to GitHub (native fork)
git-bulk clone --source github.com/sourceorg --target github.com/targetorg

# GitLab to GitLab (native fork)
git-bulk clone --source gitlab.com/sourcegroup --target gitlab.com/targetgroup

# Enable sync mode to update existing forks
git-bulk clone --source github.com/sourceorg --target github.com/targetorg --sync
```

#### Cross-Provider Forking

```bash
# GitHub to GitLab
git-bulk clone --source github.com/sourceorg --target gitlab.com/targetgroup

# GitLab to GitHub
git-bulk clone --source gitlab.com/sourcegroup --target github.com/targetorg

# Gerrit to GitHub
git-bulk clone --source gerrit.example.com --target github.com/targetorg

# Gerrit to GitLab
git-bulk clone --source gerrit.example.com --target gitlab.com/targetgroup
```

#### Fork Options

```bash
# Fork with SSH support
git-bulk clone --source github.com/sourceorg --target github.com/targetorg --ssh

# Fork with custom output directory
git-bulk clone -o ./forks --source github.com/sourceorg --target github.com/targetorg

# Dry run to see what would be forked
git-bulk clone --source github.com/sourceorg --target github.com/targetorg --dry-run

# Fork with verbose output
git-bulk clone --source github.com/sourceorg --target github.com/targetorg --verbose
```

> **Note:** Fork functionality supports both same-provider and cross-provider operations:
>
> **Same-Provider Forking** (GitHub→GitHub, GitLab→GitLab):
>
> - Uses native fork APIs for true repository forks
> - Supports `--sync` flag to update existing forks
> - Maintains fork relationship in the provider
>
> **Cross-Provider Forking** (GitHub→GitLab, GitLab→GitHub, Gerrit→GitHub/GitLab):
>
> - Creates new repositories and copies all content (branches, tags, history)
> - Sets up proper `origin` (target) and `upstream` (source) remotes
> - Does not maintain fork relationship (creates independent repositories)
> - Gerrit can be used as source but not as target
>
> **All fork operations:**
>
> - Create repositories in target organization if they don't exist
> - Skip existing repositories (unless `--sync` is used for same-provider)
> - Clone repositories locally with proper remote configuration

### SSH Authentication

The tool provides transparent SSH authentication support that integrates with your existing SSH infrastructure including ssh-agent,
GPG, and hardware security modules like Secretive (for Apple Silicon secure enclave).

#### SSH Setup and Validation

```bash
# Basic SSH setup validation
git-bulk ssh-setup

# Detailed SSH setup information
git-bulk ssh-setup --verbose
```

#### Using SSH for Operations

```bash
# Use SSH for all clone operations
git-bulk clone github.com/myorg --ssh --output ./repos

# SSH works with all supported providers
git-bulk clone gitlab.com/mygroup --ssh --output ./repos
git-bulk clone https://gerrit.example.com --ssh --output ./repos

# SSH with fork operations
git-bulk clone --source github.com/sourceorg --target gitlab.com/targetgroup --ssh
```

#### SSH Configuration

The tool automatically detects and uses:

- **SSH Agent**: Automatically detects `SSH_AUTH_SOCK` environment variable
- **SSH Keys**: Auto-discovers common SSH key files in `~/.ssh/` (id_rsa, id_ed25519, etc.)
- **SSH Config**: Reads `~/.ssh/config` for host-specific settings
- **Hardware Tokens**: Works with hardware security modules and secure enclaves

**SSH Provider Support:**

- **GitHub**: Uses standard SSH port 22 with `git@github.com`
- **GitLab**: Uses standard SSH port 22 with `git@gitlab.com`
- **Gerrit**: Uses SSH port 29418 with SSH URL format `ssh://host:29418/repo`

**SSH Authentication Priority:**

1. SSH Agent (if available and contains loaded keys)
2. SSH key files (with automatic passphrase detection)
3. Fallback to HTTPS authentication if SSH fails

#### Advanced SSH Configuration

For custom SSH configurations, the tool respects standard SSH config files:

```bash
# ~/.ssh/config example
Host my-gerrit
    HostName gerrit.company.com
    Port 29418
    User myusername
    IdentityFile ~/.ssh/id_ed25519_work
    ProxyCommand ssh gateway.company.com -W %h:%p

Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519_personal
```

### Configuration

#### Authentication Credentials

Set authentication tokens via environment variables:

```bash
export GITHUB_TOKEN="your_github_token"
export GITLAB_TOKEN="your_gitlab_token"
export GERRIT_USERNAME="your_gerrit_username"
export GERRIT_PASSWORD="your_gerrit_password"
```

#### Credentials File Support

You can also store credentials in a file instead of environment variables. The tool will automatically look for credential files
in the following locations:

1. `.credentials` (current directory)
2. `.env` (current directory)
3. `~/.config/git-bulk/credentials`
4. `~/.git-bulk-credentials`

**Credentials file format:**

```bash
# Git hosting provider tokens
GITHUB_TOKEN="ghp_your_github_token_here"
GITLAB_TOKEN="glpat-your_gitlab_token_here"

# Gerrit credentials
GERRIT_USERNAME="your_username"
GERRIT_PASSWORD="your_password"

# Comments and empty lines are ignored
```

**Priority order for credentials:**

1. Command-line flags (`--github-token`, `--gitlab-token`, etc.)
2. Environment variables (`GITHUB_TOKEN`, `GITLAB_TOKEN`, etc.)
3. Credentials file values

**Using a custom credentials file:**

```bash
git-bulk clone github.com/myorg --credentials-file /path/to/my/credentials
```

**View credential status:**

```bash
git-bulk clone github.com/myorg --dry-run --verbose
# Shows which credentials are available with ✅/❌ indicators
```

## Troubleshooting

### Password Prompts During Bulk Operations

If you're experiencing unexpected password prompts during bulk clone operations (especially on macOS), this is typically caused by
git credential helpers like `osxkeychain`. The tool now automatically prevents these prompts by default with comprehensive detection and
guidance.

#### 🔍 Automatic Detection and Guidance

git-bulk automatically detects when git attempts to use credential helpers or password authentication during bulk operations and
provides specific guidance:

```bash
⚠️  CREDENTIAL HELPER DETECTED: Git attempted password authentication
🔍 SSH authentication likely failed, git fell back to HTTPS authentication
💡 SSH FALLBACK DETECTED: Check SSH configuration and credentials

💡 SOLUTION: Use --disable-credential-helpers flag to prevent password prompts:
   git-bulk clone git@github.com:myorg/repo.git --disable-credential-helpers
📖 NOTE: Credential helpers are disabled by default since v1.0

🔧 SSH ALTERNATIVE: Fix SSH authentication setup:
   git-bulk ssh-setup --verbose
   ssh -T git@github.com  # Test SSH connectivity
```

#### 🛡️ Prevention Mechanisms

**Default Protection (Recommended):**

```bash
# Default behavior - credential helpers disabled
git-bulk clone github.com/myorg

# Equivalent to:
git-bulk clone github.com/myorg --disable-credential-helpers=true
```

**Manual Override (if needed):**

```bash
# If you need interactive authentication for some reason
git-bulk clone github.com/myorg --disable-credential-helpers=false
```

#### Root Cause Analysis

**Why This Happens:**

1. SSH authentication fails (invalid keys, ssh-agent issues, etc.)
2. Git automatically falls back to HTTPS authentication
3. Git credential helpers attempt to prompt for stored credentials
4. This causes unexpected password prompts during bulk operations

**Common Credential Helper Sources:**

- macOS Keychain (`credential.helper=osxkeychain`)
- Windows Credential Manager (`credential.helper=manager-core`)
- Git credential store (`credential.helper=store`)

#### Detection Patterns

The tool detects various credential helper scenarios:

- Password prompts: `"Password for"`, `"Username for"`
- Credential helpers: `"keychain"`, `"osxkeychain"`, `"manager-core"`
- SSH failures: `"Permission denied (publickey)"`, `"Host key verification failed"`
- Terminal prompts: `"terminal prompts disabled"`, `"could not read Username"`

#### Prevention Measures

1. **Use SSH authentication** (prevents fallback to HTTPS)
2. **Keep credential helpers disabled** (default behavior)
3. **Validate SSH setup regularly:**

   ```bash
   git-bulk ssh-setup --verbose
   ```

4. **Check your git configuration:**

   ```bash
   git config --global --list | grep credential
   ```

#### Technical Implementation

The tool implements multiple layers of protection:

- **Git command flags**: `-c credential.helper= -c core.askpass=`
- **Environment variables**: `GIT_ASKPASS="", SSH_ASKPASS="", GIT_TERMINAL_PROMPT=0`
- **Runtime detection**: Monitors git output for credential helper patterns
- **User guidance**: Provides specific troubleshooting steps when issues are detected

### SSH Authentication Details

The tool provides comprehensive SSH authentication support that integrates with your existing SSH infrastructure including
ssh-agent, GPG, and hardware security modules like Secretive (for Apple Silicon secure enclave).

#### SSH Configuration and Validation

```bash
# Basic SSH setup validation
git-bulk ssh-setup

# Detailed SSH setup information
git-bulk ssh-setup --verbose
```

#### SSH Operations Examples

```bash
# Use SSH for all clone operations
git-bulk clone github.com/myorg --ssh --output ./repos

# SSH works with all supported providers
git-bulk clone gitlab.com/mygroup --ssh --output ./repos
git-bulk clone https://gerrit.example.com --ssh --output ./repos

# SSH with fork operations
git-bulk clone --source github.com/sourceorg --target gitlab.com/targetgroup --ssh
```

#### SSH Configuration Details

The tool automatically detects and uses:

- **SSH Agent**: Automatically detects `SSH_AUTH_SOCK` environment variable
- **SSH Keys**: Auto-discovers common SSH key files in `~/.ssh/` (id_rsa, id_ed25519, etc.)
- **SSH Config**: Reads `~/.ssh/config` for host-specific settings
- **Hardware Tokens**: Works with hardware security modules and secure enclaves

**SSH Provider Support:**

- **GitHub**: Uses standard SSH port 22 with `git@github.com`
- **GitLab**: Uses standard SSH port 22 with `git@gitlab.com`
- **Gerrit**: Uses SSH port 29418 with SSH URL format `ssh://host:29418/repo`

**SSH Authentication Priority:**

1. SSH Agent (if available and contains loaded keys)
2. SSH key files (with automatic passphrase detection)
3. Fallback to HTTPS authentication if SSH fails

#### Advanced SSH Configuration Details

For custom SSH configurations, the tool respects standard SSH config files:

```bash
# ~/.ssh/config example
Host my-gerrit
    HostName gerrit.company.com
    Port 29418
    User myusername
    IdentityFile ~/.ssh/id_ed25519_work
    ProxyCommand ssh gateway.company.com -W %h:%p

Host github.com
    HostName github.com
    User git
    IdentityFile ~/.ssh/id_ed25519_personal
```

#### SSH Implementation Features

- **Auto-detection of SSH infrastructure**: SSH agent socket, key files, config files
- **Multiple authentication methods**: SSH Agent, key files with passphrase support
- **Git SSH wrapper generation**: Automatic SSH wrapper script creation
- **Provider-specific SSH options**: Optimized for each Git hosting provider
- **Connection testing**: Built-in SSH connectivity validation

#### SSH Troubleshooting

**Validate SSH setup:**

```bash
git-bulk ssh-setup --verbose
```

**Test SSH connectivity manually:**

```bash
ssh -T git@github.com
ssh -T git@gitlab.com
```

**Check SSH agent:**

```bash
ssh-add -l
```

**Common SSH Issues:**

- **Key not loaded**: Use `ssh-add ~/.ssh/id_ed25519` to load keys
- **Wrong permissions**: SSH keys should be `600`, `.ssh` directory should be `700`
- **Host key verification**: First connection may require host key acceptance
- **Hardware tokens**: Ensure hardware security modules are properly configured

### Common Issues

**Rate Limiting:**

- The tool automatically detects and handles rate limits
- Use `--verbose` to see rate limiting messages
- Consider using personal access tokens for higher rate limits

**Authentication Errors:**

- Verify tokens have correct permissions
- Check that tokens aren't expired
- Use `--verbose` to see detailed authentication status

**Network Timeouts:**

- Adjust timeout settings: `--timeout 60m --clone-timeout 10m`
- Check network connectivity to Git hosting providers

**Repository Not Found:**

- Verify organization/user names are correct
- Ensure you have access to private repositories
- Check if repositories exist and aren't archived (unless `--clone-archived` is used)

## Advanced Features

### Cross-Provider Repository Migration

The tool supports cross-provider "fork-like" functionality, allowing users to copy repositories from one Git hosting provider
to another while maintaining proper Git history and remote configuration.

#### Supported Cross-Provider Operations

- **GitHub → GitLab**: Clone GitHub repos and create new GitLab projects
- **GitLab → GitHub**: Clone GitLab projects and create new GitHub repos
- **Gerrit → GitHub**: Clone Gerrit projects and create new GitHub repos
- **Gerrit → GitLab**: Clone Gerrit projects and create new GitLab projects

#### Cross-Provider Workflow

1. **Repository Discovery**: List repositories from source provider
2. **Existence Check**: Check if repository exists in target provider
3. **Repository Creation**: Create empty repository in target provider
4. **Content Transfer**: Clone source repository (bare) and push to target
5. **Local Clone**: Clone target repository locally with proper remotes
6. **Remote Setup**: Configure `origin` (target) and `upstream` (source) remotes

#### Remote Configuration

**Same-Provider Operations:**

- `origin`: Points to fork in target organization
- `upstream`: Points to original repository in source organization

**Cross-Provider Operations:**

- `origin`: Points to new repository in target provider
- `upstream`: Points to original repository in source provider

Example for GitHub → GitLab:

```bash
# origin: git@gitlab.com:targetgroup/repo.git
# upstream: git@github.com:sourceorg/repo.git
```

#### Limitations

- **Gerrit as Target**: Gerrit cannot be used as a target (no CreateRepository support)
- **Cross-Provider Sync**: `--sync` only works for same-provider operations
- **Fork Relationships**: Cross-provider operations create independent repositories (not true forks)

### Archive Filtering

By default, the tool skips archived/read-only repositories to focus on active development. You can override this behavior
with the `--clone-archived` flag.

#### Archive Detection

- **GitHub/GitLab**: Repositories with `archived` field set to `true`
- **Gerrit**: Projects with `state` field set to `READ_ONLY` or `HIDDEN`

#### Archive Handling Usage

```bash
# Default: skip archived repositories
git-bulk clone github.com/myorg

# Include archived repositories
git-bulk clone github.com/myorg --clone-archived

# See which repositories are filtered
git-bulk clone github.com/myorg --verbose --dry-run
```

### Gerrit SSH Enumeration

For Gerrit instances protected by services like Cloudflare, the tool automatically falls back to SSH-based repository
enumeration when HTTP/HTTPS API endpoints fail.

**SSH Enumeration Features:**

- **Automatic Fallback**: Triggers when HTTP methods fail and SSH is available
- **Native Commands**: Uses Gerrit's `ls-projects` SSH command
- **JSON Support**: Attempts JSON format with description, falls back to plain text
- **Compatibility**: Maintains compatibility with HTTP API responses

**SSH Connection Flow:**

1. **HTTP First**: Always attempts HTTP methods first for better performance
2. **SSH Fallback**: Only triggers SSH when HTTP fails and SSH is available
3. **Command Execution**: Runs `gerrit ls-projects --format json --description`
4. **Output Processing**: Parses command output into repository objects
5. **Graceful Degradation**: Maintains compatibility with existing workflows

**Usage:**

```bash
# Automatic SSH fallback when HTTP fails
git-bulk clone gerrit.example.com --ssh

# With Gerrit username
git-bulk clone gerrit.example.com --ssh --gerrit-user myusername
```

**Implementation Details:**

- Uses existing SSH infrastructure from `/internal/ssh/auth.go`
- Establishes SSH connection to Gerrit server (port 29418)
- Runs native Gerrit commands for repository enumeration
- Gracefully handles both authenticated and anonymous access
- Maintains compatibility with HTTP API response format

## Architecture

The tool is built around a modular thread pool architecture:

- **Worker Pool**: Manages concurrent operations with configurable thread count
- **Rate Limiting**: Automatically detects and handles API rate limits
- **Retry Logic**: Exponential backoff with configurable retry attempts
- **Job Management**: Persistent job state for manual retry of failed operations
- **Provider Abstraction**: Unified interface for different Git hosting providers

### Provider Interface

The tool uses a unified provider interface that supports:

```go
type Provider interface {
    Name() string
    ParseSource(source string) (*SourceInfo, error)
    GetOrganization(ctx context.Context, orgName string) (*Organization, error)
    ListRepositories(ctx context.Context, orgName string) ([]*Repository, error)
    CreateFork(ctx context.Context, sourceRepo *Repository, targetOrg string) (*Repository, error)
    CreateRepository(ctx context.Context, orgName, repoName, description string, private bool) (*Repository, error)
    SyncRepository(ctx context.Context, repo *Repository) error
    RepositoryExists(ctx context.Context, orgName, repoName string) (bool, error)
}
```

## Development

### Recent Improvements

The tool has been recently enhanced with significant performance and reliability improvements:

#### GitHub Fork Processing Fix (v1.1.0)

**Problem**: GitHub returns 202 Accepted when fork creation is queued for processing, but the tool previously treated this as an error.

**Solution**: The GitHub provider now properly handles 202 Accepted responses with intelligent retry logic:

- **Progressive Backoff**: 10s base delay + 2s per attempt (max 1 minute)
- **Smart Waiting**: Up to 12 retry attempts (~2 minutes total wait time)
- **Fallback Handling**: Returns placeholder repository if fork doesn't become available
- **Transparent Operation**: Users see normal fork creation flow

**Implementation Details**:

```go
// Enhanced CreateFork method with 202 handling
if resp != nil && resp.StatusCode == http.StatusAccepted {
    return g.waitForForkAvailability(ctx, targetOrg, sourceRepoName, sourceRepo)
}
```

#### Parallel Processing Implementation (v1.1.0)

**Problem**: Fork operations were previously processed sequentially, causing performance bottlenecks for organizations with large numbers
of repositories.

**Solution**: Implemented parallel processing using the existing worker pool infrastructure:

- **Worker Pool Integration**: Uses configurable worker threads for parallel fork operations
- **Real-time Progress**: Live progress reporting with percentage completion
- **Job Management**: Structured job configuration with comprehensive error handling
- **Result Collection**: Enhanced status reporting with visual feedback
- **Timeout Detection**: 5-minute stall detection with graceful handling

**Performance Impact**: Organizations with 100+ repositories can expect 4-8x performance improvements depending on worker configuration.

**Technical Implementation**:

- `ForkJobConfig` struct for streamlined job parameters
- `processForksInParallel()` function using worker pool
- `waitForForkCompletion()` with progress monitoring
- Enhanced result collection and status reporting

### Dependencies

The project uses the following major dependencies:

- **CLI Framework**: `github.com/spf13/cobra` for command-line interface
- **GitLab API**: `gitlab.com/gitlab-org/api/client-go@v0.129.0` for GitLab integration
- **GitHub API**: `github.com/google/go-github/v53` for GitHub integration
- **Rate Limiting**: `golang.org/x/time@v0.11.0` for API rate limiting

### Running tests

```bash
make test

# Run tests with coverage
make test-coverage

# Run integration tests
make test-integration

# Run CLI tests
make cli-test
```

### Building

```bash
# Build for current platform
make build

# Build for multiple platforms
make build-all

# Clean build artifacts
make clean
```

### Development workflow

```bash
# Set up development environment
make dev-setup

# Full development cycle
make all

# Run linting and security checks
make lint
make security
```

## Testing

The project includes comprehensive testing:

- **Unit tests**: Located alongside source code in `internal/*/` directories
- **Integration tests**: Located in `tests/` directory
- **Cross-Provider Tests**: Validates fork functionality across different providers
- **SSH Tests**: Validates SSH authentication and connection handling
- **Archive Filtering Tests**: Validates repository filtering logic

### Test Structure

- **Unit Tests**: Located alongside source code in `internal/` directories
- **Integration Tests**: `tests/*_test.go` files with proper Go testing framework
- **Demo Applications**: `tests/demos/` directory contains standalone demo programs
- **Test Utilities**: Shell scripts and utilities for test environment management

### Running Tests

#### Unit Tests

Unit tests are located alongside the source code in the `internal/` directory:

```bash
# Run all unit tests
go test ./internal/...

# Run with coverage
go test -cover ./internal/...

# Run specific package tests
go test ./internal/clone/
go test ./internal/provider/
go test ./internal/worker/
```

#### Integration Tests

Integration tests require build tags and may need credentials:

```bash
# Run integration tests
go test -tags=integration ./tests/

# Run with verbose output
go test -tags=integration -v ./tests/

# Run specific integration test
go test -tags=integration -run TestCLIIntegration ./tests/
```

#### Test Environment Management

Use the test utilities script for environment setup:

```bash
# Set up test environment
./tests/test-utils.sh setup

# Run integration tests with environment setup
./tests/test-utils.sh integration

# Clean up after tests
./tests/test-utils.sh cleanup

# Validate test results
./tests/test-utils.sh validate ./test_output
```

#### Demo Applications

The `tests/demos/` directory contains standalone programs that demonstrate specific functionality:

- `format_demo.go` - Demonstrates error message formatting
- `comprehensive_integration_demo.go` - Shows worker pool and rate limiting features

Run demos with:

```bash
cd tests/demos
go run format_demo.go
go run comprehensive_integration_demo.go
```

### Test Coverage

- ✅ GitHub provider operations (clone, fork, sync)
- ✅ GitLab provider operations (clone, fork, sync)
- ✅ Gerrit provider operations (clone, SSH enumeration)
- ✅ Cross-provider forking (GitHub↔GitLab, Gerrit→GitHub/GitLab)
- ✅ SSH authentication (keys, agent, hardware tokens)
- ✅ Archive filtering (GitHub, GitLab, Gerrit)
- ✅ Rate limiting and error handling
- ✅ Credential management and validation

### Test Requirements

Some tests may require:

- Valid GitHub/GitLab tokens (set via environment variables)
- SSH keys configured for git operations
- Network access to test against live services
- Sufficient disk space for cloning test repositories

Run tests with:

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run integration tests
./tests/test-utils.sh integration
```

## Contributing

When contributing new features or improvements, please:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality (see `tests/` directory)
4. Ensure all tests pass
5. Update documentation as needed
6. Submit a pull request

### Development Guidelines

- Follow existing code patterns and error handling approaches
- Add comprehensive tests for new features
- Update documentation for user-facing changes
- Ensure backward compatibility when possible
- Follow Go best practices and conventions

### Pre-commit Checks

Before submitting changes, ensure:

```bash
# All tests pass
make test

# Code is properly formatted
make fmt

# Linting passes
make lint

# Security checks pass
make security
```

## License

Apache-2.0 License - see LICENSE file for details.
