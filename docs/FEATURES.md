<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2025 The Linux Foundation -->

# git-bulk Features and Implementation Details

This document consolidates information about implemented features, SSH authentication, and recent enhancements to the git-bulk tool.

## Table of Contents

- [SSH Authentication Implementation](#ssh-authentication-implementation)
- [Gerrit SSH Repository Enumeration](#gerrit-ssh-repository-enumeration)
- [Archive Filtering Feature](#archive-filtering-feature)
- [Recent Improvements](#recent-improvements)

## SSH Authentication Implementation

### ✅ Completed Features

#### 1. Core SSH Authentication Module (`/internal/ssh/auth.go`)

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

#### 2. Provider SSH Integration

- **GitHub Provider** (`/internal/provider/github.go`):
  - SSH authentication support with `git@github.com` format
  - Port 22 standard SSH connection
  - SSH connection testing
- **GitLab Provider** (`/internal/provider/gitlab.go`):
  - SSH authentication support with `git@gitlab.com` format
  - Port 22 standard SSH connection
  - Host detection for GitLab instances
- **Gerrit Provider** (`/internal/provider/gerrit.go`):
  - SSH authentication support with custom port (29418)
  - SSH URL format: `ssh://host:29418/repo`
  - Username-based SSH authentication

#### 3. SSH Provider Interface (`/internal/provider/provider.go`)

```go
type SSHProvider interface {
    SSHClientConfig() (*ssh.ClientConfig, error)
    TestSSHConnection() error
    SupportsSSH() bool
}
```

#### 4. Clone Manager SSH Integration (`/internal/clone/manager.go`)

- SSH wrapper integration for git clone operations
- Automatic SSH URL selection when `--ssh` flag is used
- Environment variable management for git commands

### SSH Usage Examples

```bash
# Use SSH for cloning (auto-detects SSH keys/agent)
git-bulk clone github myorg --ssh

# Use SSH with specific key file
git-bulk clone gitlab myorg --ssh --ssh-key ~/.ssh/id_rsa

# Use SSH agent (works with hardware tokens)
git-bulk clone gerrit gerrit.example.com --ssh --ssh-agent
```

### SSH Authentication Methods Priority

1. **SSH Agent** (if `SSH_AUTH_SOCK` environment variable exists)
2. **SSH Key Files** (in order of preference):
   - `~/.ssh/id_ed25519`
   - `~/.ssh/id_rsa`
   - `~/.ssh/id_ecdsa`
   - `~/.ssh/id_dsa`
3. **SSH Config** (host-specific configurations from `~/.ssh/config`)

## Gerrit SSH Repository Enumeration

### Overview

Successfully implemented SSH-based repository enumeration as a fallback for Gerrit when HTTP/HTTPS API endpoints fail due to
protection services like Cloudflare.

### Implementation Details

#### Core Functionality

The implementation adds SSH-based repository enumeration to the Gerrit provider in `/internal/provider/gerrit.go`.
When HTTP-based repository enumeration fails and SSH credentials are available, the system automatically falls back to SSH
enumeration using Gerrit's native SSH commands.

#### Key Components

1. **Enhanced `getProjects` Method**:
   - **Primary**: Attempts HTTP authenticated endpoints (`projects/?d`, `projects/`)
   - **Secondary**: Falls back to unauthenticated endpoints for authorization issues
   - **Tertiary**: Uses SSH enumeration when HTTP methods fail and SSH is available

2. **SSH Project Listing (`trySSHProjectListing`)**:
   - Connects to Gerrit SSH port (default 29418)
   - Executes `gerrit ls-projects --format json --description` command
   - Falls back to basic `gerrit ls-projects` if JSON format is not supported
   - Parses output into `GerritProject` objects for compatibility

3. **SSH Output Parsing (`parseSSHProjectListOutput`)**:
   - Handles both JSON and plain text output formats
   - Creates compatible `GerritProject` objects
   - Maintains consistency with HTTP API responses

#### SSH Connection Flow

1. **Configuration**: Uses existing SSH infrastructure from `/internal/ssh/auth.go`
2. **Connection**: Establishes SSH connection to Gerrit server (port 29418)
3. **Authentication**: Uses SSH keys, SSH agent, or password authentication
4. **Command Execution**: Runs `gerrit ls-projects` with appropriate flags
5. **Output Processing**: Parses command output into repository objects

#### Error Handling and Fallbacks

- **HTTP First**: Always attempts HTTP methods first for better performance
- **SSH Fallback**: Only triggers SSH when HTTP fails and SSH is available
- **Graceful Degradation**: Maintains compatibility with existing workflows

### Usage Example

```bash
# Gerrit with SSH fallback (automatic when HTTP fails)
git-bulk clone gerrit gerrit.example.com --ssh --ssh-user myusername

# Force SSH enumeration
git-bulk clone gerrit gerrit.example.com --ssh --force-ssh-enum
```

## Archive Filtering Feature

### Feature Overview

Archive filtering functionality skips archived/read-only repositories by default with a new `--clone-archived` flag to optionally include them.

### Implementation Summary

#### Key Changes Made

1. **CLI Flag Addition**: Added `--clone-archived` flag to include archived repositories
2. **Configuration Support**: Added `CloneArchived bool` field to Config structs
3. **Archive Detection**: Implemented `isRepositoryArchived()` method supporting:
   - GitHub/GitLab: `archived` field set to "true"
   - Gerrit: `state` field set to "READ_ONLY" or "HIDDEN"
4. **Filtering Logic**: Implemented `filterRepositories()` method to separate active from archived repos
5. **Integration**: Updated `CloneRepositories()` and `CloneAll()` methods to apply filtering
6. **Result Handling**: Enhanced to include skipped archived repositories with `StatusSkipped`

#### Architecture

- **Default Behavior**: Skip archived repositories (`--clone-archived=false`)
- **Optional Inclusion**: Include archived repositories with `--clone-archived` flag
- **Multi-Provider Support**: Works with GitHub, GitLab, and Gerrit
- **Status Tracking**: Skipped repositories are properly tracked in results

### Testing Results

#### Test Coverage

- ✅ GitHub archived repositories (`archived: "true"`)
- ✅ GitLab archived repositories (`archived: "true"`)
- ✅ Gerrit read-only repositories (`state: "READ_ONLY"`)
- ✅ Gerrit hidden repositories (`state: "HIDDEN"`)
- ✅ Active repositories (should not be filtered)
- ✅ Repositories with no metadata (should not be filtered)
- ✅ Mixed metadata scenarios

#### CLI Validation

- ✅ `--clone-archived` flag appears in help output
- ✅ Flag is properly defined and accessible

### Usage Examples

```bash
# Default: skip archived repositories
git-bulk clone github myorg

# Include archived repositories
git-bulk clone github myorg --clone-archived

# Verbose output showing filtered repositories
git-bulk clone gitlab myorg --verbose
```

## Recent Improvements

### ✅ Completed Tasks

#### 1. Fixed Hanging GitLab Test Issue

- **Problem**: GitLab tests were hanging when trying to fetch from large organizations
- **Solution**: Added pagination limits (maxPages = 50) to `listGroupProjects` and `listUserProjects` functions
- **Files Modified**: `internal/provider/gitlab.go`
- **Result**: Tests now complete successfully and respect timeout limits

#### 2. Enhanced GitLab Error Handling

- **Problem**: Poor error messages when authentication was required
- **Solution**: Improved error handling in `ListRepositories` to provide helpful messages about authentication requirements for groups vs users
- **Files Modified**: `internal/provider/gitlab.go`
- **Result**: Better user experience with clearer error messages

#### 3. Verified Timeout Handling

- **Test**: Confirmed that operations respect the --timeout flag and context cancellation
- **Result**: ✅ Timeout handling works correctly - tested with --timeout=10s

#### 4. Fixed MIT License Reference in README

- **Problem**: README.md incorrectly referenced "MIT License"
- **Solution**: Changed to "Apache-2.0 License - see LICENSE file for details."
- **Files Modified**: `README.md` (line 267)
- **Result**: ✅ License reference is now correct

#### 5. Implemented --source and --target Flag Infrastructure

- **Problem**: README examples showed --source and --target flags that didn't exist
- **Solution**: Added flag infrastructure with proper validation
- **Features Added**:
  - `--source` flag for specifying source organization
  - `--target` flag for specifying target organization for forking
  - `--sync` flag for updating existing forks
  - Comprehensive validation logic
- **Files Modified**: `cmd/git-bulk/main.go`
- **Result**: ✅ Flags are now available and properly validated

#### 6. Updated README Documentation

- **Problem**: Examples in README were misleading about incomplete features
- **Solution**: Marked fork functionality as "Planned Feature" with clear notice
- **Result**: ✅ Documentation now accurately reflects current capabilities

### Performance Improvements

- **Pagination Control**: Added configurable limits to prevent hanging on large organizations
- **Timeout Handling**: Enhanced timeout management for network operations
- **Rate Limiting**: Improved rate limit detection and handling
- **Memory Management**: Better resource cleanup and management

### Error Handling Enhancements

- **Authentication Errors**: Clearer messages for auth failures
- **Network Errors**: Better handling of network timeouts and connectivity issues
- **Validation Errors**: More descriptive error messages for configuration issues
- **Graceful Degradation**: Fallback mechanisms for various failure scenarios

---

## Contributing

When contributing new features or improvements, please:

1. Add appropriate tests to the `/tests` directory
2. Update this documentation with feature descriptions
3. Ensure all pre-commit checks pass
4. Follow the existing code patterns and error handling approaches

For more information, see the main [README.md](../README.md) file.
