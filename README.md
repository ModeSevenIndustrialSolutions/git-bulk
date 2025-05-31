<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2025 The Linux Foundation -->

# Go Bulk Git Tools

A comprehensive suite of Go command-line tools for bulk Git repository operations with support for GitHub, GitLab, and Gerrit.

## Features

- **Multi-platform Git hosting support**: GitHub, GitLab, and Gerrit
- **Intelligent thread pooling**: Configurable worker threads with automatic rate limiting detection
- **Exponential backoff**: Automatic retry with exponential backoff for failed operations
- **Rich CLI interface**: Built with Cobra for comprehensive help and shell completion
- **Comprehensive testing**: Full test suite with coverage reporting
- **Modular design**: Reusable components for building additional tools

## Installation

```bash
go install github.com/modesevenindustrialsolutions/go-bulk-git/cmd/git-bulk@latest
```

Or build from source:

```bash
git clone https://github.com/modesevenindustrialsolutions/go-bulk-git
cd go-bulk-git
go build -o git-bulk ./cmd/git-bulk
```

## Usage

### Clone repositories from an organization

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

# Use custom credentials file
git-bulk clone github.com/myorg --credentials-file ./my-credentials --output ./repos
```

### Fork repositories to another organization

```bash
# Fork all repositories from source to target GitHub organization
git-bulk clone --source github.com/sourceorg --target github.com/targetorg

# Enable sync mode to update existing forks
git-bulk clone --source github.com/sourceorg --target github.com/targetorg --sync
```

### Configuration

Set authentication tokens via environment variables:

```bash
export GITHUB_TOKEN="your_github_token"
export GITLAB_TOKEN="your_gitlab_token"
export GERRIT_USERNAME="your_gerrit_username"
export GERRIT_PASSWORD="your_gerrit_password"
```

#### Credentials File Support

You can also store credentials in a file instead of environment variables. The tool will automatically look for credential
files in the following locations:

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

### SSH Authentication

The tool provides transparent SSH authentication support that integrates with your existing SSH infrastructure including ssh-agent,
GPG, and hardware security modules like Secretive (for Apple Silicon secure enclave).

#### SSH Setup and Validation

Validate your SSH authentication setup:

```bash
# Basic SSH setup validation
git-bulk ssh-setup

# Detailed SSH setup information
git-bulk ssh-setup --verbose
```

#### Using SSH for Cloning

```bash
# Use SSH for all clone operations
git-bulk clone github.com/myorg --ssh --output ./repos

# SSH works with all supported providers
git-bulk clone gitlab.com/mygroup --ssh --output ./repos
git-bulk clone https://gerrit.example.com --ssh --output ./repos
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

## Architecture

The tool is built around a modular thread pool architecture:

- **Worker Pool**: Manages concurrent operations with configurable thread count
- **Rate Limiting**: Automatically detects and handles API rate limits
- **Retry Logic**: Exponential backoff with configurable retry attempts
- **Job Management**: Persistent job state for manual retry of failed operations
- **Provider Abstraction**: Unified interface for different Git hosting providers

## Development

### Dependencies

The project uses the following major dependencies:

- **CLI Framework**: `github.com/spf13/cobra` for command-line interface
- **GitLab API**: `gitlab.com/gitlab-org/api/client-go@v0.129.0` for GitLab integration
- **GitHub API**: `github.com/google/go-github/v53` for GitHub integration
- **Rate Limiting**: `golang.org/x/time@v0.11.0` for API rate limiting

### Running tests

```bash
go test ./...
```

### Building

```bash
go build -o git-bulk ./cmd/git-bulk
```

### Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

MIT License - see LICENSE file for details.
