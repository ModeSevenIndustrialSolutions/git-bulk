#!/usr/bin/env python3

# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2025 The Linux Foundation

import re

# Read the file
with open("internal/clone/manager_test.go", "r") as f:
    content = f.read()

# Pattern to match the Config struct ending with WorkerConfig
pattern = r"(manager := NewManager\(&Config\{\s*WorkerConfig: &worker\.Config\{[^}]*\},)\s*(\}, nil, nil\))"

# Replacement that adds timeout configs
replacement = r"\1\n\t\tCloneTimeout:   30 * time.Minute,\n\t\tNetworkTimeout: 5 * time.Minute,\n\t\2"

# Apply the replacement
new_content = re.sub(pattern, replacement, content, flags=re.MULTILINE | re.DOTALL)

# Write the file back
with open("internal/clone/manager_test.go", "w") as f:
    f.write(new_content)

print("Fixed manager_test.go configurations")
