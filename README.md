<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2025 The Linux Foundation -->

# Go Bulk Git Tools

A comprehensive suite of Go command-line tools for bulk Git repository operations with support for GitHub, GitLab, and Gerrit.

## Features

- **Multi-platform Git hosting support**: GitHub, GitLab, and Gerrit
- **Intelligent thread pooling**: Configurable worker threads with automatic rate limiting detection
- **Exponential backoff**: Automatic retry with exponential backoff for failed operations
- **Rich CLI interface**: Built with urfave/cli for comprehensive help and shell completion
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
git-bulk clone --source github.com/myorg --directory ./repos

# Clone from GitLab
git-bulk clone --source gitlab.com/mygroup --directory ./repos

# Clone from Gerrit
git-bulk clone --source gerrit.example.com --directory ./repos
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

## Architecture

The tool is built around a modular thread pool architecture:

- **Worker Pool**: Manages concurrent operations with configurable thread count
- **Rate Limiting**: Automatically detects and handles API rate limits
- **Retry Logic**: Exponential backoff with configurable retry attempts
- **Job Management**: Persistent job state for manual retry of failed operations
- **Provider Abstraction**: Unified interface for different Git hosting providers

## Development

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
