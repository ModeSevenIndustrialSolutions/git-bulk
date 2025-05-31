// SPDX-FileCopyrightText: 2024 Matt Mode Seven Industrial Solutions
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/modesevenindustrialsolutions/go-bulk-git/internal/provider"
)

func main() {
	manager := provider.NewProviderManager(&provider.Config{})

	githubProvider, err := provider.NewGitHubProvider("", "")
	if err != nil {
		fmt.Printf("Failed to create GitHub provider: %v\n", err)
	} else {
		manager.RegisterProvider("github", githubProvider)
		fmt.Printf("Successfully registered GitHub provider\n")
	}

	prov, sourceInfo, err := manager.GetProviderForSource("octocat")
	if err != nil {
		fmt.Printf("Failed to get provider: %v\n", err)
	} else {
		fmt.Printf("Success: Provider=%s, Org=%s\n", prov.Name(), sourceInfo.Organization)
	}
}
