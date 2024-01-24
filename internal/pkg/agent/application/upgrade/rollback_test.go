// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package upgrade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/paths"
	"github.com/elastic/elastic-agent/pkg/control/v2/client/mocks"
	"github.com/elastic/elastic-agent/pkg/core/logger"
)

type hookFunc func(t *testing.T, topDir string)

type agentVersion struct {
	version string
	hash    string
}

type agentInstall struct {
	version          agentVersion
	useVersionInPath bool
}

type setupAgentInstallations struct {
	installedAgents []agentInstall
	upgradeFrom     agentVersion
	upgradeTo       agentVersion
	currentAgent    agentVersion
}

var (
	version123Snapshot = agentVersion{
		version: "1.2.3-SNAPSHOT",
		hash:    "abcdef",
	}
	version456Snapshot = agentVersion{
		version: "4.5.6-SNAPSHOT",
		hash:    "ghijkl",
	}
)

func TestCleanup(t *testing.T) {
	type args struct {
		currentVersionedHome string
		currentHash          string
		removeMarker         bool
		keepLogs             bool
	}

	tests := []struct {
		name               string
		args               args
		agentInstallsSetup setupAgentInstallations
		additionalSetup    hookFunc
		wantErr            assert.ErrorAssertionFunc
		checkAfterCleanup  hookFunc
	}{
		{
			name: "cleanup without versionedHome (legacy upgrade process)",
			args: args{
				currentVersionedHome: "data/elastic-agent-ghijkl",
				currentHash:          "ghijkl",
				removeMarker:         true,
				keepLogs:             false,
			},
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: false,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: false,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterCleanup: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-ghijkl")
				checkFilesAfterCleanup(t, topDir, newAgentHome, oldAgentHome)
			},
		},
		{
			name: "cleanup with versionedHome (new upgrade process)",
			args: args{
				currentVersionedHome: "data/elastic-agent-4.5.6-SNAPSHOT-ghijkl",
				currentHash:          "ghijkl",
				removeMarker:         true,
				keepLogs:             false,
			},
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: true,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: true,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterCleanup: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-1.2.3-SNAPSHOT-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")
				checkFilesAfterCleanup(t, topDir, newAgentHome, oldAgentHome)
			},
		},
		{
			name: "cleanup with versionedHome only on the new agent (new upgrade process from an old agent upgraded with legacy process)",
			args: args{
				currentVersionedHome: "data/elastic-agent-4.5.6-SNAPSHOT-ghijkl",
				currentHash:          "ghijkl",
				removeMarker:         true,
				keepLogs:             false,
			},
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: false,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: true,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterCleanup: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")
				checkFilesAfterCleanup(t, topDir, newAgentHome, oldAgentHome)
			},
		},
		{
			name: "cleanup with versionedHome only on the new agent + extra old agent installs",
			args: args{
				currentVersionedHome: "data/elastic-agent-4.5.6-SNAPSHOT-ghijkl",
				currentHash:          "ghijkl",
				removeMarker:         true,
				keepLogs:             false,
			},
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version: agentVersion{
							version: "0.9.9",
							hash:    "aaaaaa",
						},
						useVersionInPath: false,
					},
					{
						version: agentVersion{
							version: "1.1.1",
							hash:    "aaabbb",
						},
						useVersionInPath: false,
					},
					{
						version:          version123Snapshot,
						useVersionInPath: false,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: true,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterCleanup: func(t *testing.T, topDir string) {
				newAgentHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")
				oldAgentHomes := []string{
					filepath.Join("data", "elastic-agent-abcdef"),
					filepath.Join("data", "elastic-agent-aaabbb"),
					filepath.Join("data", "elastic-agent-aaaaaa"),
				}

				checkFilesAfterCleanup(t, topDir, newAgentHome, oldAgentHomes...)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger, _ := logger.NewTesting(t.Name())
			testTop := t.TempDir()
			setupAgents(t, testLogger, testTop, tt.agentInstallsSetup)
			if tt.additionalSetup != nil {
				tt.additionalSetup(t, testTop)
			}
			marker, err := LoadMarker(paths.DataFrom(testTop))
			require.NoError(t, err, "error loading update marker")
			require.NotNil(t, marker, "loaded marker must not be nil")
			t.Logf("Loaded update marker %+v", marker)
			tt.wantErr(t, Cleanup(testLogger, testTop, marker.VersionedHome, marker.Hash, tt.args.removeMarker, tt.args.keepLogs), fmt.Sprintf("Cleanup(%v, %v, %v, %v)", marker.VersionedHome, marker.Hash, tt.args.removeMarker, tt.args.keepLogs))
			tt.checkAfterCleanup(t, testTop)
		})
	}
}

func TestRollback(t *testing.T) {
	tests := []struct {
		name               string
		agentInstallsSetup setupAgentInstallations
		additionalSetup    hookFunc
		wantErr            assert.ErrorAssertionFunc
		checkAfterRollback hookFunc
	}{
		{
			name: "rollback without versionedHome (legacy upgrade process)",
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: false,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: false,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterRollback: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-ghijkl")
				checkFilesAfterRollback(t, topDir, oldAgentHome, newAgentHome)
			},
		},
		{
			name: "rollback with versionedHome (new upgrade process)",
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: true,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: true,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterRollback: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-1.2.3-SNAPSHOT-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")
				checkFilesAfterRollback(t, topDir, oldAgentHome, newAgentHome)
			},
		},
		{
			name: "rollback with versionedHome only on the new agent (new upgrade process from an old agent upgraded with legacy process)",
			agentInstallsSetup: setupAgentInstallations{
				installedAgents: []agentInstall{
					{
						version:          version123Snapshot,
						useVersionInPath: false,
					},
					{
						version:          version456Snapshot,
						useVersionInPath: true,
					},
				},
				upgradeFrom:  version123Snapshot,
				upgradeTo:    version456Snapshot,
				currentAgent: version456Snapshot,
			},
			wantErr: assert.NoError,
			checkAfterRollback: func(t *testing.T, topDir string) {
				oldAgentHome := filepath.Join("data", "elastic-agent-abcdef")
				newAgentHome := filepath.Join("data", "elastic-agent-4.5.6-SNAPSHOT-ghijkl")
				checkFilesAfterRollback(t, topDir, oldAgentHome, newAgentHome)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLogger, _ := logger.NewTesting(t.Name())
			testTop := t.TempDir()
			setupAgents(t, testLogger, testTop, tt.agentInstallsSetup)
			if tt.additionalSetup != nil {
				tt.additionalSetup(t, testTop)
			}
			marker, err := LoadMarker(paths.DataFrom(testTop))
			require.NoError(t, err, "error loading update marker")
			require.NotNil(t, marker, "loaded marker must not be nil")
			t.Logf("Loaded update marker %+v", marker)

			// mock client
			mockClient := mocks.NewClient(t)
			mockClient.EXPECT().Connect(
				mock.AnythingOfType("*context.timerCtx"),
				mock.AnythingOfType("*grpc.funcDialOption"),
				mock.AnythingOfType("*grpc.funcDialOption"),
			).Return(nil)
			mockClient.EXPECT().Disconnect().Return()
			mockClient.EXPECT().Restart(mock.Anything).Return(nil).Once()

			ctx := context.TODO()
			tt.wantErr(t, Rollback(ctx, testLogger, mockClient, testTop, marker.PrevVersionedHome, marker.PrevHash), fmt.Sprintf("Rollback(%v, %v, %v, %v, %v, %v)", ctx, testLogger, mockClient, testTop, marker.PrevVersionedHome, marker.PrevHash))
			tt.checkAfterRollback(t, testTop)
		})
	}
}

// checkFilesAfterCleanup is a convenience function to check the file structure within topDir.
// *AgentHome paths must be the expected old and new agent paths relative to topDir (typically in the form of "data/elastic-agent-*")
func checkFilesAfterCleanup(t *testing.T, topDir, newAgentHome string, oldAgentHomes ...string) {
	t.Helper()
	// the old agent directories must not exist anymore
	for _, oldAgentHome := range oldAgentHomes {
		assert.NoDirExistsf(t, filepath.Join(topDir, oldAgentHome), "old agent directory %q should be deleted after cleanup", oldAgentHome)
	}

	// check the new agent home
	assert.DirExists(t, filepath.Join(topDir, newAgentHome), "new agent directory should exist after cleanup")
	agentExecutable := agentName
	if runtime.GOOS == "windows" {
		agentExecutable += ".exe"
	}
	symlinkPath := filepath.Join(topDir, agentExecutable)
	linkTarget, err := os.Readlink(symlinkPath)
	if assert.NoError(t, err, "unable to read symbolic link") {
		assert.Equal(t, paths.BinaryPath(newAgentHome, agentExecutable), linkTarget, "symbolic link should point to the new agent executable after cleanup")
	}

	// read the elastic agent placeholder via the symlink
	elasticAgentBytes, err := os.ReadFile(symlinkPath)
	if assert.NoError(t, err, "error reading elastic-agent content through the symlink") {
		assert.Equal(t, []byte("Placeholder for agent 4.5.6-SNAPSHOT"), elasticAgentBytes, "reading elastic-agent content through symbolic link should point to the new version")
	}

	assert.NoFileExists(t, filepath.Join(topDir, "data", markerFilename), "update marker should have been cleaned up")
}

// checkFilesAfterRollback is a convenience function to check the file structure within topDir.
// *AgentHome paths must be the expected old and new agent paths relative to topDir (typically in the form of "data/elastic-agent-*")
func checkFilesAfterRollback(t *testing.T, topDir, oldAgentHome, newAgentHome string) {
	t.Helper()
	// the new agent directory must still exist (for the logs)
	assert.DirExists(t, filepath.Join(topDir, newAgentHome), "new agent directory should still exist after rollback")
	assert.DirExists(t, filepath.Join(topDir, newAgentHome, "logs"), "new agent logs directory should still exist after rollback")
	// some things should have been removed from the new agent directory
	assert.NoDirExists(t, filepath.Join(topDir, newAgentHome, "components"), "new agent components directory should have been cleaned up in the rollback")
	assert.NoDirExists(t, filepath.Join(topDir, newAgentHome, "run"), "new agent run directory should have been cleaned up in the rollback")
	assert.NoFileExists(t, filepath.Join(topDir, newAgentHome, agentName), "new agent binary should have been cleaned up in the rollback")

	// check the old agent home
	assert.DirExists(t, filepath.Join(topDir, oldAgentHome), "old agent directory should exist after rollback")
	agentExecutable := agentName
	if runtime.GOOS == "windows" {
		agentExecutable += ".exe"
	}
	symlinkPath := filepath.Join(topDir, agentExecutable)
	linkTarget, err := os.Readlink(symlinkPath)
	if assert.NoError(t, err, "unable to read symbolic link") {
		// Due to the unique way the rollback process works we end up with an absolute path in the link
		expectedLinkTargetAfterRollback := paths.BinaryPath(filepath.Join(topDir, oldAgentHome), agentExecutable)
		assert.Equal(t, expectedLinkTargetAfterRollback, linkTarget, "symbolic link should point to the old agent executable after rollback")
	}

	// read the elastic agent placeholder via the symlink
	elasticAgentBytes, err := os.ReadFile(symlinkPath)
	if assert.NoError(t, err, "error reading elastic-agent content through the symlink") {
		assert.Equal(t, []byte("Placeholder for agent 1.2.3-SNAPSHOT"), elasticAgentBytes, "reading elastic-agent content through symbolic link should point to the old version")
	}

	assert.NoFileExists(t, filepath.Join(topDir, "data", markerFilename), "update marker should have been cleaned up")
}

// setupAgents create fake agent installs, update marker file and symlink pointing to one of the installations' elastic-agent placeholder
func setupAgents(t *testing.T, log *logger.Logger, topDir string, installations setupAgentInstallations) {

	var (
		oldAgentVersion       agentVersion
		oldAgentVersionedHome string
		newAgentVersion       agentVersion
		newAgentVersionedHome string
		useNewMarker          bool
	)
	for _, ia := range installations.installedAgents {
		versionedHome := createFakeAgentInstall(t, topDir, ia.version.version, ia.version.hash, ia.useVersionInPath)
		t.Logf("Created fake agent install for %+v located at %q", ia, versionedHome)
		if installations.upgradeFrom == ia.version {
			t.Logf("Setting version %+v as FROM version for the update marker", ia.version)
			oldAgentVersion = ia.version
			oldAgentVersionedHome = versionedHome
		}

		if installations.upgradeTo == ia.version {
			t.Logf("Setting version %+v as TO version for the update marker", ia.version)
			newAgentVersion = ia.version
			newAgentVersionedHome = versionedHome
			useNewMarker = ia.useVersionInPath
		}

		if installations.currentAgent == ia.version {
			t.Logf("Creating symlink pointing to %q", versionedHome)
			createLink(t, topDir, versionedHome)
		}
	}

	t.Logf("Creating upgrade marker from %+v located at %q to %+v located at %q", oldAgentVersion, oldAgentVersionedHome, newAgentVersion, newAgentVersionedHome)
	createUpdateMarker(t, log, topDir, newAgentVersion.version, newAgentVersion.hash, newAgentVersionedHome, oldAgentVersion.version, oldAgentVersion.hash, oldAgentVersionedHome, useNewMarker)
}

// createFakeAgentInstall will create a mock agent install within topDir, possibly using the version in the directory name, depending on useVersionInPath
// it MUST return the path to the created versionedHome relative to topDir, to mirror what step_unpack returns
func createFakeAgentInstall(t *testing.T, topDir, version, hash string, useVersionInPath bool) string {

	// create versioned home
	versionedHome := fmt.Sprintf("elastic-agent-%s", hash[:hashLen])
	if useVersionInPath {
		// use the version passed as parameter
		versionedHome = fmt.Sprintf("elastic-agent-%s-%s", version, hash[:hashLen])
	}
	relVersionedHomePath := filepath.Join("data", versionedHome)
	absVersionedHomePath := filepath.Join(topDir, relVersionedHomePath)

	// recalculate the binary path and launch a mkDirAll to account for MacOS weirdness
	// (the extra nesting of elastic agent binary within versionedHome)
	absVersioneHomeBinaryPath := paths.BinaryPath(absVersionedHomePath, "")
	err := os.MkdirAll(absVersioneHomeBinaryPath, 0o750)
	require.NoError(t, err, "error creating fake install versioned home directory (including binary path) %q", absVersioneHomeBinaryPath)

	// place a few directories in the fake install
	absComponentsDirPath := filepath.Join(absVersionedHomePath, "components")
	err = os.MkdirAll(absComponentsDirPath, 0o750)
	require.NoError(t, err, "error creating fake install components directory %q", absVersionedHomePath)

	absLogsDirPath := filepath.Join(absVersionedHomePath, "logs")
	err = os.MkdirAll(absLogsDirPath, 0o750)
	require.NoError(t, err, "error creating fake install logs directory %q", absLogsDirPath)

	absRunDirPath := filepath.Join(absVersionedHomePath, "run")
	err = os.MkdirAll(absRunDirPath, 0o750)
	require.NoError(t, err, "error creating fake install run directory %q", absRunDirPath)

	// put some placeholder for files
	agentExecutableName := agentName
	if runtime.GOOS == "windows" {
		agentExecutableName += ".exe"
	}
	err = os.WriteFile(paths.BinaryPath(absVersionedHomePath, agentExecutableName), []byte(fmt.Sprintf("Placeholder for agent %s", version)), 0o750)
	require.NoErrorf(t, err, "error writing elastic agent binary placeholder %q", agentExecutableName)
	err = os.WriteFile(filepath.Join(absLogsDirPath, "fakelog.ndjson"), []byte(fmt.Sprintf("Sample logs for agent %s", version)), 0o750)
	require.NoErrorf(t, err, "error writing fake log placeholder %q")

	// return the path relative to top exactly like the step_unpack does
	return relVersionedHomePath
}

func createLink(t *testing.T, topDir string, currentAgentVersionedHome string) {
	linkTarget := paths.BinaryPath(currentAgentVersionedHome, agentName)
	linkName := agentName
	if runtime.GOOS == "windows" {
		linkTarget += ".exe"
		linkName += ".exe"
	}
	err := os.Symlink(linkTarget, filepath.Join(topDir, linkName))
	require.NoError(t, err, "error creating test symlink to fake agent install")
}

func createUpdateMarker(t *testing.T, log *logger.Logger, topDir, newAgentVersion, newAgentHash, newAgentVersionedHome, oldAgentVersion, oldAgentHash, oldAgentVersionedHome string, useNewMarker bool) {

	if !useNewMarker {
		newAgentVersion = ""
		newAgentVersionedHome = ""
		oldAgentVersionedHome = ""
	}

	err := markUpgrade(log, paths.DataFrom(topDir), newAgentVersion, newAgentHash, newAgentVersionedHome, oldAgentVersion, oldAgentHash, oldAgentVersionedHome, nil, nil)
	require.NoError(t, err, "error writing fake update marker")
}
