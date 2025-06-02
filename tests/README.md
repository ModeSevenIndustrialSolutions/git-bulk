<!--
SPDX-License-Identifier: Apache-2.0
SPDX-FileCopyrightText: 2025 The Linux Foundation
-->

# git-bulk Tests

This directory contains comprehensive testing for the git-bulk tool.

## Test Structure

- **Integration Tests**: `*_test.go` files with proper Go testing framework
- **Demo Applications**: `demos/` directory contains standalone demo programs
- **Test Utilities**: Shell scripts and utilities for test environment management
- **Test Data**: Sample configurations and test fixtures

## Running Tests

### Unit Tests

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

### Integration Tests

Integration tests require build tags and may need credentials:

```bash
# Run integration tests
go test -tags=integration ./tests/

# Run with verbose output
go test -tags=integration -v ./tests/

# Run specific integration test
go test -tags=integration -run TestCLIIntegration ./tests/
```

### Test Environment Management

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

## Demo Applications

The `demos/` directory contains standalone programs that demonstrate specific functionality:

- `format_demo.go` - Demonstrates error message formatting
- `comprehensive_integration_demo.go` - Shows worker pool and rate limiting features

Run demos with:

```bash
cd tests/demos
go run format_demo.go
go run comprehensive_integration_demo.go
```

## Test Data and Fixtures

- `git-bulk.license` - Sample license file for testing
- Shell scripts for various testing scenarios
- Sample configurations and credential files

## Contributing Tests

When adding new tests:

1. Unit tests go in the same directory as the code being tested
2. Integration tests go in this `tests/` directory
3. Use proper build tags for integration tests: `//go:build integration`
4. Add test utilities to `test-utils.sh` for common operations
5. Document any special setup requirements

## Test Requirements

Some tests may require:

- Valid GitHub/GitLab tokens (set via environment variables)
- SSH keys configured for git operations
- Network access to test against live services
- Sufficient disk space for cloning test repositories

See `test-credentials.sh` for credential setup guidance.
