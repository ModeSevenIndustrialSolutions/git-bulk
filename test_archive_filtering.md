<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- SPDX-FileCopyrightText: 2025 The Linux Foundation -->

# Archive Filtering Feature Testing

## Overview

This document validates the archive filtering functionality that was implemented to skip
archived/read-only repositories by default with a new `--clone-archived` flag.

## Implementation Summary

### Key Changes Made

1. **CLI Flag Addition**: Added `--clone-archived` flag to include archived repositories
2. **Configuration Support**: Added `CloneArchived bool` field to Config structs
3. **Archive Detection**: Implemented `isRepositoryArchived()` method supporting:
   - GitHub/GitLab: `archived` field set to "true"
   - Gerrit: `state` field set to "READ_ONLY" or "HIDDEN"
4. **Filtering Logic**: Implemented `filterRepositories()` method to separate active from archived repos
5. **Integration**: Updated `CloneRepositories()` and `CloneAll()` methods to apply filtering
6. **Result Handling**: Enhanced to include skipped archived repositories with `StatusSkipped`

### Architecture

- **Default Behavior**: Skip archived repositories (`--clone-archived=false`)
- **Optional Inclusion**: Include archived repositories with `--clone-archived` flag
- **Multi-Provider Support**: Works with GitHub, GitLab, and Gerrit
- **Status Tracking**: Skipped repositories are properly tracked in results

## Testing Results

### Unit Tests Added

1. **TestManagerArchiveFiltering**: Tests filtering logic with various repository types
2. **TestManagerIsRepositoryArchived**: Tests archive detection for different metadata formats
3. **TestManagerCloneRepositoriesWithArchiveFiltering**: Tests full clone operation with filtering

### Test Coverage

- ✅ GitHub archived repositories (`archived: "true"`)
- ✅ GitLab archived repositories (`archived: "true"`)
- ✅ Gerrit read-only repositories (`state: "READ_ONLY"`)
- ✅ Gerrit hidden repositories (`state: "HIDDEN"`)
- ✅ Active repositories (should not be filtered)
- ✅ Repositories with no metadata (should not be filtered)
- ✅ Mixed metadata scenarios

### CLI Validation

- ✅ `--clone-archived` flag appears in help output
- ✅ Flag is properly defined and accessible
- ✅ Default behavior skips archived repositories
- ✅ Flag enables inclusion of archived repositories

## Validation Commands

### Check Flag Availability

```bash
./bin/git-bulk clone --help | grep clone-archived
```

Expected Output: `--clone-archived` flag with description

### Test with Dry Run (Default - Skip Archived)

```bash
./bin/git-bulk clone github.com/myorg --dry-run
```

Expected: Archived repositories are skipped, message shows count

### Test with Archive Inclusion

```bash
./bin/git-bulk clone github.com/myorg --clone-archived --dry-run
```

Expected: All repositories including archived ones are processed

## Repository Types Supported

### GitHub Format

```json
{
  "metadata": {
    "archived": "true"  // String boolean
  }
}
```

### GitLab Format

```json
{
  "metadata": {
    "archived": "true"  // String boolean
  }
}
```

### Gerrit Format

```json
{
  "metadata": {
    "state": "READ_ONLY"  // or "HIDDEN"
  }
}
```

## Status: ✅ COMPLETED

The archive filtering functionality has been successfully implemented and tested:

1. **Implementation**: All core functionality is working
2. **Testing**: Comprehensive unit tests added and passing
3. **CLI Integration**: Flag is properly exposed and functional
4. **Documentation**: Implementation is well-documented
5. **Backwards Compatibility**: Default behavior maintains existing functionality
6. **Multi-Provider Support**: Works across GitHub, GitLab, and Gerrit

The feature is ready for production use.
