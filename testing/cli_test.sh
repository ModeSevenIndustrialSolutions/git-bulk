#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2025 The Linux Foundation

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

# Function to print test results
print_result() {
    local test_name="$1"
    local result="$2"
    local message="$3"

    TESTS_RUN=$((TESTS_RUN + 1))

    if [ "$result" = "PASS" ]; then
        echo -e "${GREEN}✓ PASS${NC}: $test_name"
        TESTS_PASSED=$((TESTS_PASSED + 1))
    else
        echo -e "${RED}✗ FAIL${NC}: $test_name - $message"
        TESTS_FAILED=$((TESTS_FAILED + 1))
    fi
}

# Function to run a command and capture output
run_command() {
    local cmd="$1"
    local expected_exit_code="${2:-0}"

    echo "Running: $cmd"
    if output=$(eval "$cmd" 2>&1); then
        actual_exit_code=0
    else
        actual_exit_code=$?
    fi

    if [ "$actual_exit_code" = "$expected_exit_code" ]; then
        return 0
    else
        echo "Expected exit code $expected_exit_code, got $actual_exit_code"
        echo "Output: $output"
        return 1
    fi
}

echo "Building git-bulk CLI..."
cd "$(dirname "$0")/.."
go build -o testing/git-bulk ./cmd/git-bulk/

CLI="./testing/git-bulk"

echo -e "\n${YELLOW}=== CLI Help and Version Tests ===${NC}"

# Test 1: Help command
if run_command "$CLI --help"; then
    print_result "Help command" "PASS"
else
    print_result "Help command" "FAIL" "Help command failed"
fi

# Test 2: Version command
if run_command "$CLI --version"; then
    print_result "Version command" "PASS"
else
    print_result "Version command" "FAIL" "Version command failed"
fi

# Test 3: Invalid flag
if run_command "$CLI --invalid-flag" 1; then
    print_result "Invalid flag handling" "PASS"
else
    print_result "Invalid flag handling" "FAIL" "Should exit with code 1 for invalid flags"
fi

echo -e "\n${YELLOW}=== GitHub Provider Tests ===${NC}"

# Test 4: List GitHub repositories (dry run)
if run_command "$CLI clone github.com/octocat --dry-run"; then
    print_result "GitHub clone dry run" "PASS"
else
    print_result "GitHub clone dry run" "FAIL" "GitHub dry run failed"
fi

# Test 5: GitHub organization listing (requires token)
if [ -n "$GITHUB_TOKEN" ]; then
    if run_command "GITHUB_TOKEN=$GITHUB_TOKEN $CLI clone github.com/octocat --dry-run --verbose"; then
        print_result "GitHub with token" "PASS"
    else
        print_result "GitHub with token" "FAIL" "GitHub with token failed"
    fi
else
    echo -e "${YELLOW}⚠ SKIP${NC}: GitHub with token (GITHUB_TOKEN not set)"
fi

echo -e "\n${YELLOW}=== GitLab Provider Tests ===${NC}"

# Test 6: GitLab clone dry run
if run_command "$CLI clone gitlab.com/gitlab-org --dry-run"; then
    print_result "GitLab clone dry run" "PASS"
else
    print_result "GitLab clone dry run" "FAIL" "GitLab dry run failed"
fi

echo -e "\n${YELLOW}=== Gerrit Provider Tests ===${NC}"

# Test 7: Gerrit clone dry run
if run_command "$CLI clone https://gerrit.googlesource.com --dry-run"; then
    print_result "Gerrit clone dry run" "PASS"
else
    print_result "Gerrit clone dry run" "FAIL" "Gerrit dry run failed"
fi

echo -e "\n${YELLOW}=== Error Handling Tests ===${NC}"

# Test 8: Invalid source
if run_command "$CLI clone invalid-source" 1; then
    print_result "Invalid source handling" "PASS"
else
    print_result "Invalid source handling" "FAIL" "Should fail for invalid source"
fi

# Test 9: Missing source argument
if run_command "$CLI clone" 1; then
    print_result "Missing source argument" "PASS"
else
    print_result "Missing source argument" "FAIL" "Should fail when source is missing"
fi

echo -e "\n${YELLOW}=== Configuration Tests ===${NC}"

# Test 10: Worker configuration
if run_command "$CLI clone github.com/octocat --dry-run --workers 2 --max-retries 1"; then
    print_result "Worker configuration" "PASS"
else
    print_result "Worker configuration" "FAIL" "Worker configuration failed"
fi

# Test 11: Output directory
TEMP_DIR=$(mktemp -d)
if run_command "$CLI clone github.com/octocat --dry-run --output $TEMP_DIR"; then
    print_result "Output directory" "PASS"
else
    print_result "Output directory" "FAIL" "Output directory configuration failed"
fi
rm -rf "$TEMP_DIR"

# Test 12: SSH clone
if run_command "$CLI clone github.com/octocat --dry-run --ssh"; then
    print_result "SSH clone option" "PASS"
else
    print_result "SSH clone option" "FAIL" "SSH clone option failed"
fi

echo -e "\n${YELLOW}=== Integration Tests ===${NC}"

# Test 13: Actual clone (small public repo)
TEMP_DIR=$(mktemp -d)
if run_command "$CLI clone github.com/octocat/Hello-World --output $TEMP_DIR --workers 1"; then
    if [ -d "$TEMP_DIR/Hello-World" ]; then
        print_result "Actual GitHub clone" "PASS"
    else
        print_result "Actual GitHub clone" "FAIL" "Repository not cloned"
    fi
else
    print_result "Actual GitHub clone" "FAIL" "Clone command failed"
fi
rm -rf "$TEMP_DIR"

# Test 14: Multiple repositories
TEMP_DIR=$(mktemp -d)
if run_command "$CLI clone github.com/octocat --output $TEMP_DIR --workers 2 --max-repos 2"; then
    print_result "Multiple repositories clone" "PASS"
else
    print_result "Multiple repositories clone" "FAIL" "Multiple repos clone failed"
fi
rm -rf "$TEMP_DIR"

echo -e "\n${YELLOW}=== Performance Tests ===${NC}"

# Test 15: Concurrent workers
if run_command "$CLI clone github.com/octocat --dry-run --workers 5 --timeout 30s"; then
    print_result "Concurrent workers" "PASS"
else
    print_result "Concurrent workers" "FAIL" "Concurrent workers failed"
fi

# Test 16: Timeout handling
if run_command "$CLI clone github.com/octocat --dry-run --timeout 1ms" 1; then
    print_result "Timeout handling" "PASS"
else
    print_result "Timeout handling" "FAIL" "Should timeout with very short timeout"
fi

echo -e "\n${YELLOW}=== Summary ===${NC}"
echo "Tests run: $TESTS_RUN"
echo -e "Tests passed: ${GREEN}$TESTS_PASSED${NC}"
echo -e "Tests failed: ${RED}$TESTS_FAILED${NC}"

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "\n${GREEN}All tests passed!${NC}"
    exit 0
else
    echo -e "\n${RED}Some tests failed!${NC}"
    exit 1
fi
