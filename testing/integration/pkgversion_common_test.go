// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

//go:build integration

package integration

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
	"gotest.tools/assert"

	"github.com/stretchr/testify/require"

	integrationtest "github.com/elastic/elastic-agent/pkg/testing"
	"github.com/elastic/elastic-agent/version"
)

// testAgentPackageVersion returns a func that can be used with t.Run() to execute the version check as a subtest
func testAgentPackageVersion(ctx context.Context, f *integrationtest.Fixture, binaryOnly bool) func(*testing.T) {
	return func(t *testing.T) {
		// find package version files
		pkgVersionFiles := findPkgVersionFiles(t, f.WorkDir())
		if len(pkgVersionFiles) == 0 {
			t.Skip("No package version files detected, skipping")
		}

		// Read the package version file content
		pkgVersionBytes, err := os.ReadFile(pkgVersionFiles[0])
		require.NoError(t, err, "package version file is not readable")
		pkgVersion := strings.TrimSpace(string(pkgVersionBytes))
		t.Logf("package version file content: %q", pkgVersion)

		if pkgVersion == "" {
			t.Skip("elastic agent has been packaged without specifying a package version")
		}

		// check the version returned by the running agent
		actualVersionBytes := getAgentVersion(t, f, context.Background(), binaryOnly)

		actualVersion := unmarshalVersionOutput(t, actualVersionBytes, "binary")
		assert.Equal(t, pkgVersion, actualVersion, "binary version does not match package version")

		if !binaryOnly {
			// check the daemon version
			actualVersion = unmarshalVersionOutput(t, actualVersionBytes, "daemon")
			assert.Equal(t, pkgVersion, actualVersion, "daemon version does not match package version")
		}
	}
}

// getAgentVersion retrieves the agent version yaml output via CLI
func getAgentVersion(t *testing.T, f *integrationtest.Fixture, ctx context.Context, binaryOnly bool) []byte {
	args := []string{"version", "--yaml"}
	if binaryOnly {
		args = append(args, "--binary-only")
	}
	actualVersionBytes, err := f.Exec(ctx, args)
	require.NoError(t, err, "error executing 'version' command. Output %q", string(actualVersionBytes))
	return actualVersionBytes
}

// unmarshalVersionOutput retrieves the version string for binary or daemon from "version" subcommand yaml output
func unmarshalVersionOutput(t *testing.T, cmdOutput []byte, binaryOrDaemonKey string) string {
	versionCmdOutput := map[string]any{}
	err := yaml.Unmarshal(cmdOutput, &versionCmdOutput)
	require.NoError(t, err, "error parsing 'version' command output")
	require.Contains(t, versionCmdOutput, binaryOrDaemonKey)
	return versionCmdOutput[binaryOrDaemonKey].(map[any]any)["version"].(string)
}

// findPkgVersionFiles scans recursively a root directory and returns all the package version files encountered
func findPkgVersionFiles(t *testing.T, rootDir string) []string {
	t.Helper()
	//find the package version file
	installFS := os.DirFS(rootDir)
	matches := []string{}
	err := fs.WalkDir(installFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.Name() == version.PackageVersionFileName {
			matches = append(matches, path)
		}
		return nil
	})
	require.NoError(t, err)

	t.Logf("package version files found: %v", matches)
	return matches
}
