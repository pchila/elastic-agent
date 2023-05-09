// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.
package migration

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/migration/mocks"
	"github.com/elastic/elastic-agent/version"
)

//go:generate mockery --name UpdateMarkerReadWriter
//go:generate mockery --name Migration

func TestManagerUpgrade(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig1.EXPECT().ID().Return("mock-migration-1")
	mockMig1.EXPECT().Upgrade(mock.Anything).Return(nil)
	mockMig2 := mocks.NewMigration(t)
	mockMig2.EXPECT().ID().Return("mock-migration-2")
	mockMig2.EXPECT().Upgrade(mock.Anything).Return(nil)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2},
	}

	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	// simulate an upgrade from some other version
	marker := upgrade.UpdateMarker{Hash: version.Commit(), UpdatedOn: time.Now(), PrevVersion: "1.2.3", PrevHash: "abcdef"}
	umrw.EXPECT().Read().Return(&marker, nil)
	umrw.EXPECT().Write(mock.Anything).Return(nil).Times(2)
	err := um.RunMigrations(umrw)
	assert.NoError(t, err)
	assert.Equal(t, marker.AppliedMigrations, []string{"mock-migration-1", "mock-migration-2"})
}

func TestManagerRollback(t *testing.T) {
	rollbackOrder := []string{}

	mockMig1 := mocks.NewMigration(t)
	mockMig1.EXPECT().ID().Return("mock-migration-1")
	mockMig1.EXPECT().Rollback(mock.Anything).RunAndReturn(
		func(um upgrade.UpdateMarker) error {
			rollbackOrder = append(rollbackOrder, "mock-migration-1")
			assert.Equal(t, um.AppliedMigrations, []string{"mock-migration-1"})
			return nil
		},
	)
	mockMig2 := mocks.NewMigration(t)
	mockMig2.EXPECT().ID().Return("mock-migration-2")
	mockMig2.EXPECT().Rollback(mock.Anything).RunAndReturn(
		func(um upgrade.UpdateMarker) error {
			rollbackOrder = append(rollbackOrder, "mock-migration-2")
			assert.Equal(t, um.AppliedMigrations, []string{"mock-migration-1", "mock-migration-2"})
			return nil
		},
	)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2},
	}

	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	// simulate an upgrade from some other version
	marker := upgrade.UpdateMarker{Hash: version.Commit(), UpdatedOn: time.Now(), PrevVersion: "1.2.3", PrevHash: "abcdef", AppliedMigrations: []string{"mock-migration-1", "mock-migration-2"}}
	umrw.EXPECT().Read().Return(&marker, nil)
	umrw.EXPECT().Write(mock.Anything).Return(nil).Times(2)
	err := um.RollbackMigrations(umrw)
	assert.NoError(t, err)
	assert.Equal(t, marker.AppliedMigrations, []string{})
	assert.Equal(t, rollbackOrder, []string{"mock-migration-2", "mock-migration-1"})
}

func TestManagerNoMigrationIfNoUpgrade(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig2 := mocks.NewMigration(t)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2},
	}

	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	// there is no upgrade in progress so no marker and no error
	umrw.EXPECT().Read().Return(nil, nil)
	t.Run("migration", func(t *testing.T) {
		err := um.RunMigrations(umrw)
		assert.NoError(t, err)
	})

	t.Run("rollback", func(t *testing.T) {
		err := um.RollbackMigrations(umrw)
		assert.NoError(t, err)
	})

}

func TestManagerReturnErrorIfMigrationFails(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig1.EXPECT().ID().Return("mock-migration-1")
	mockMig1.EXPECT().Upgrade(mock.Anything).Return(nil)
	mockMig2 := mocks.NewMigration(t)
	mockMig2.EXPECT().ID().Return("mock-migration-2")
	mockMig2.EXPECT().Upgrade(mock.Anything).Return(errors.New("migration error"))
	mockMig3 := mocks.NewMigration(t)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2, mockMig3},
	}

	marker := upgrade.UpdateMarker{Hash: version.Commit(), UpdatedOn: time.Now(), PrevVersion: "1.2.3", PrevHash: "abcdef"}
	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	umrw.EXPECT().Read().Return(&marker, nil)
	// Manager writes the marker after the 1st successful migration
	umrw.EXPECT().Write(mock.Anything).Return(nil).Times(1)
	err := um.RunMigrations(umrw)
	assert.Error(t, err)
	// We only save the successful migrations
	assert.Equal(t, marker.AppliedMigrations, []string{"mock-migration-1"})
}

func TestManagerReturnErrorIfRollbackFails(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig1.EXPECT().ID().Return("mock-migration-1")
	mockMig2 := mocks.NewMigration(t)
	mockMig2.EXPECT().ID().Return("mock-migration-2")
	mockMig2.EXPECT().Rollback(mock.Anything).Return(errors.New("rollback error"))
	mockMig3 := mocks.NewMigration(t)
	mockMig3.EXPECT().ID().Return("mock-migration-3")
	mockMig3.EXPECT().Rollback(mock.Anything).Return(nil)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2, mockMig3},
	}

	marker := upgrade.UpdateMarker{Hash: version.Commit(), UpdatedOn: time.Now(), PrevVersion: "1.2.3", PrevHash: "abcdef", AppliedMigrations: []string{"mock-migration-1", "mock-migration-2", "mock-migration-3"}}
	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	umrw.EXPECT().Read().Return(&marker, nil)
	// Manager writes the marker after the 1st successful migration
	umrw.EXPECT().Write(mock.Anything).Return(nil).Times(1)
	err := um.RollbackMigrations(umrw)
	assert.Error(t, err)
	// We only remove the successful rollbacks
	assert.Equal(t, marker.AppliedMigrations, []string{"mock-migration-1", "mock-migration-2"})
}

func TestManagerReturnErrorIfUnknownMigration(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig1.EXPECT().ID().Return("mock-migration-1")
	mockMig2 := mocks.NewMigration(t)
	mockMig2.EXPECT().ID().Return("mock-migration-2")
	mockMig3 := mocks.NewMigration(t)
	mockMig3.EXPECT().ID().Return("mock-migration-3")
	mockMig3.EXPECT().Rollback(mock.Anything).Return(nil)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2, mockMig3},
	}

	marker := upgrade.UpdateMarker{Hash: version.Commit(), UpdatedOn: time.Now(), PrevVersion: "1.2.3", PrevHash: "abcdef", AppliedMigrations: []string{"mock-migration-1", "mock-migration-2", "unknown-migration", "mock-migration-3"}}
	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	umrw.EXPECT().Read().Return(&marker, nil)
	// Manager writes the marker after the 1st successful migration
	umrw.EXPECT().Write(mock.Anything).Return(nil).Times(1)
	err := um.RollbackMigrations(umrw)
	assert.Error(t, err)
	// We only remove the successful rollbacks
	assert.Equal(t, marker.AppliedMigrations, []string{"mock-migration-1", "mock-migration-2", "unknown-migration"})
}

func TestManagerErrorIfMarkerCannotBeRead(t *testing.T) {
	mockMig1 := mocks.NewMigration(t)
	mockMig2 := mocks.NewMigration(t)
	um := Manager{
		migrations: []Migration{mockMig1, mockMig2},
	}

	umrw := mocks.NewUpgradeMarkerReadWriter(t)
	// there is no upgrade in progress so no marker and no error
	umrw.EXPECT().Read().Return(nil, errors.New("error reading marker"))
	t.Run("migration", func(t *testing.T) {
		err := um.RunMigrations(umrw)
		assert.Error(t, err)
	})

	t.Run("rollback", func(t *testing.T) {
		err := um.RollbackMigrations(umrw)
		assert.Error(t, err)
	})
}
