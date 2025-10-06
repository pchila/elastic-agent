// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package install

import (
	"context"
	"testing"

	v1 "github.com/elastic/elastic-agent/pkg/api/v1"
)

func TestSyncRegistry(t *testing.T) {
	testcases := []struct {
		name                    string
		parallelOperationGroups [][]func(context.Context, *SyncInstallRegistry)
		expectedInstalls        [][]v1.AgentInstallDesc
	}{
		{
			name: "concurrent adds, modify, delete",
			parallelOperationGroups: [][]func(context.Context, *SyncInstallRegistry){
				// first group: add 3 installs
				{
					func(ctx context.Context, sir *SyncInstallRegistry) {
						sir.AddInstallDesc(v1.AgentInstallDesc{
							VersionedHome: "data/v1",
							Flavor:        "flavah 1",
							Active:        false,
						})
					},
					func(ctx context.Context, sir *SyncInstallRegistry) {
						sir.AddInstallDesc(v1.AgentInstallDesc{
							VersionedHome: "data/v2",
							Flavor:        "flavah 2",
							Active:        false,
						})
					},
					func(ctx context.Context, sir *SyncInstallRegistry) {
						sir.AddInstallDesc(v1.AgentInstallDesc{
							VersionedHome: "data/v3",
							Flavor:        "flavah 3",
							Active:        true,
						})
					},
				},
				// second group: modify 2 installs and delete one of those two
				{
					func(ctx context.Context, sir *SyncInstallRegistry) {
						sir.ModifyInstallDesc(func(desc *v1.AgentInstallDesc) error {
							if desc.VersionedHome == "data/v2" {
								desc.Flavor = "flavah two"
							} else if desc.VersionedHome == "data/v3" {
								desc.Flavor = "flavah three"
							}
							return nil
						})
					},
					func(ctx context.Context, sir *SyncInstallRegistry) {
						sir.RemoveAgentInstallDesc("data/v2")
					},
				},
			},
			expectedInstalls: [][]v1.AgentInstallDesc{
				// result after first group of operations
				{
					{
						VersionedHome: "data/v1",
						Flavor:        "flavah 1",
						Active:        false,
					},
					{
						VersionedHome: "data/v2",
						Flavor:        "flavah 2",
						Active:        false,
					},
					{
						VersionedHome: "data/v3",
						Flavor:        "flavah 3",
						Active:        true,
					},
				},
				// result after second group of operations
				{
					{
						VersionedHome: "data/v1",
						Flavor:        "flavah 1",
						Active:        false,
					},
					{
						VersionedHome: "data/v3",
						Flavor:        "flavah three",
						Active:        true,
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			installDescriptor := v1.NewInstallDescriptor()
			mockInstallRegistry :=

		})
	}
}
