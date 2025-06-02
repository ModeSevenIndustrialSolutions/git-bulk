#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2025 The Linux Foundation

# git-bulk Test Utilities
# This script provides utilities for testing git-bulk functionality

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# Test environment setup
setup_test_env() {
    local test_dir="${1:-./test_output}"

    log_info "Setting up test environment in $test_dir"

    # Create test directory if it doesn't exist
    mkdir -p "$test_dir"

    # Clean up any previous test runs
    if [ -d "$test_dir" ] && [ "$(find "$test_dir" -mindepth 1 -maxdepth 1 | wc -l)" -gt 0 ]; then
        log_warning "Cleaning up existing test output in $test_dir"
        rm -rf "${test_dir:?}"/*
    fi

    log_success "Test environment ready at $test_dir"
}

# Cleanup test environment
cleanup_test_env() {
    local test_dir="${1:-./test_output}"

    if [ -d "$test_dir" ]; then
        log_info "Cleaning up test environment at $test_dir"
        rm -rf "$test_dir"
        log_success "Test environment cleaned up"
    fi
}

# Validate git repository
validate_git_repo() {
    local repo_path="$1"

    if [ ! -d "$repo_path" ]; then
        log_error "Repository path does not exist: $repo_path"
        return 1
    fi

    if [ ! -d "$repo_path/.git" ]; then
        log_error "Not a git repository: $repo_path"
        return 1
    fi

    # Check if repository has commits
    if ! git -C "$repo_path" rev-parse HEAD >/dev/null 2>&1; then
        log_error "Repository has no commits: $repo_path"
        return 1
    fi

    log_success "Valid git repository: $repo_path"
    return 0
}

# Validate directory structure after clone operations
validate_clone_results() {
    local output_dir="${1:-./test_output}"
    local expected_repos="${2:-1}"

    log_info "Validating clone results in $output_dir"

    if [ ! -d "$output_dir" ]; then
        log_error "Output directory does not exist: $output_dir"
        return 1
    fi

    # Count repositories
    local repo_count
    repo_count=$(find "$output_dir" -name ".git" -type d | wc -l | tr -d ' ')

    log_info "Found $repo_count repositories"

    if [ "$repo_count" -lt "$expected_repos" ]; then
        log_warning "Expected at least $expected_repos repositories, found $repo_count"
    fi

    # Validate each repository
    local validation_errors=0
    while IFS= read -r -d '' git_dir; do
        local repo_dir
        repo_dir=$(dirname "$git_dir")
        if ! validate_git_repo "$repo_dir"; then
            validation_errors=$((validation_errors + 1))
        fi
    done < <(find "$output_dir" -name ".git" -type d -print0)

    if [ "$validation_errors" -eq 0 ]; then
        log_success "All repositories validated successfully"
        return 0
    else
        log_error "$validation_errors repositories failed validation"
        return 1
    fi
}

# Test credentials helper
test_credentials() {
    log_info "Testing credential availability"

    # Check for GitHub token
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        log_success "GitHub token available"
    else
        log_warning "No GitHub token found in GITHUB_TOKEN environment variable"
    fi

    # Check for GitLab token
    if [ -n "${GITLAB_TOKEN:-}" ]; then
        log_success "GitLab token available"
    else
        log_warning "No GitLab token found in GITLAB_TOKEN environment variable"
    fi

    # Check for SSH keys
    if [ -f ~/.ssh/id_rsa ] || [ -f ~/.ssh/id_ed25519 ] || [ -f ~/.ssh/id_ecdsa ]; then
        log_success "SSH keys found"
    else
        log_warning "No SSH keys found in ~/.ssh/"
    fi

    # Check SSH agent
    if [ -n "${SSH_AUTH_SOCK:-}" ]; then
        log_success "SSH agent available"
    else
        log_warning "SSH agent not available"
    fi
}

# Run integration tests
run_integration_tests() {
    local test_dir="${1:-./test_output}"
    local provider="${2:-github}"
    local org="${3:-octocat}"

    log_info "Running integration tests for $provider/$org"

    setup_test_env "$test_dir"
    test_credentials

    # Run the actual clone operation
    log_info "Testing clone operation..."
    if ./bin/git-bulk clone "$provider" "$org" --output-dir "$test_dir" --verbose --timeout 30s; then
        log_success "Clone operation completed"
    else
        log_error "Clone operation failed"
        return 1
    fi

    # Validate results
    validate_clone_results "$test_dir"
}

# Main function for when script is run directly
main() {
    case "${1:-help}" in
        "setup")
            setup_test_env "${2:-./test_output}"
            ;;
        "cleanup")
            cleanup_test_env "${2:-./test_output}"
            ;;
        "validate")
            validate_clone_results "${2:-./test_output}" "${3:-1}"
            ;;
        "credentials")
            test_credentials
            ;;
        "integration")
            run_integration_tests "${2:-./test_output}" "${3:-github}" "${4:-octocat}"
            ;;
        "help"|*)
            echo "Usage: $0 {setup|cleanup|validate|credentials|integration|help}"
            echo ""
            echo "Commands:"
            echo "  setup [dir]                    - Set up test environment"
            echo "  cleanup [dir]                  - Clean up test environment"
            echo "  validate [dir] [min_repos]     - Validate clone results"
            echo "  credentials                    - Test credential availability"
            echo "  integration [dir] [provider] [org] - Run integration tests"
            echo "  help                           - Show this help message"
            ;;
    esac
}

# Run main function if script is executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
