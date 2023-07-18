// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build !windows

package cmd

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/internal/pkg/cli"
	agtversion "github.com/elastic/elastic-agent/version"
)

func TestInstallPath(t *testing.T) {
	tests := map[string]string{
		"single_level": "/opt",
		"multi_level":  "/Library/Agent",
	}

	for name, basePath := range tests {
		t.Run(name, func(t *testing.T) {
			p := paths.InstallPath(basePath)
			require.Equal(t, basePath+"/Elastic/Agent", p)
		})
	}
}

func TestInvalidBasePath(t *testing.T) {
	tests := map[string]struct {
		basePath      string
		expectedError string
	}{
		"relative_path_1": {
			basePath:      "relative/path",
			expectedError: `base path [relative/path] is not absolute`,
		},
		"relative_path_2": {
			basePath:      "./relative/path",
			expectedError: `base path [./relative/path] is not absolute`,
		},
		"empty_path": {
			basePath:      "",
			expectedError: `base path [] is not absolute`,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pkgVersionPath, err := agtversion.GetAgentPackageVersionFilePath()
			require.NoError(t, err)
			err = os.WriteFile(pkgVersionPath, []byte(agtversion.GetDefaultVersion()), os.ModePerm&0o777)
			require.NoError(t, err)
			streams := cli.NewIOStreams()
			cmd := cobra.Command{}
			cmd.Flags().String(flagInstallBasePath, test.basePath, "")

			err = installCmd(streams, &cmd)

			if test.expectedError == "" {
				require.NoError(t, err)
			} else {
				require.Equal(t, test.expectedError, err.Error())
			}
		})
	}
}
