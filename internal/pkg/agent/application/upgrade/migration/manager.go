// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package migration

import (
	"fmt"
	"time"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade"
	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade/migration/state"
)

type Migration interface {
	ID() string
	IsApplicable(upgrade.UpdateMarker) bool
	Upgrade(upgrade.UpdateMarker) error
	Rollback(upgrade.UpdateMarker) error
}

type MigrationStateReader interface {
	Read() (*state.MigrationState, error)
}

type MigrationStateWriter interface {
	Write(*state.MigrationState) error
}

type MigrationStateReadWriter interface {
	MigrationStateReader
	MigrationStateWriter
}

type UpdateMarkerReader interface {
	Read() (*upgrade.UpdateMarker, error)
}

type UpdateMarkerWriter interface {
	Write(*upgrade.UpdateMarker) error
}

type UpdateMarkerReadWriter interface {
	UpdateMarkerReader
	UpdateMarkerWriter
}
type Manager struct {
	migrations []Migration
	msrw       MigrationStateReadWriter
}

func (m Manager) RunMigrations(um UpdateMarkerReader) error {
	upgdMarker, err := um.Read()
	if err != nil {
		return fmt.Errorf("reading the upgrade marker: %w", err)
	}
	if upgdMarker == nil {
		// no upgrade marker, nothing to do
		return nil
	}
	migState := new(state.MigrationState)
	migState.MigrationStarted = time.Now().UTC()
	err = m.performUpgrade(upgdMarker, migState)
	if err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	migState.MigrationCompleted = time.Now().UTC()
	return nil
}

func (m Manager) RollbackMigrations(um UpdateMarkerReader) error {
	upgdMarker, err := um.Read()
	if err != nil {
		return fmt.Errorf("reading the upgrade marker: %w", err)
	}
	if upgdMarker == nil {
		// no upgrade marker, nothing to do ---> is this correct in a rollback scenario?
		return nil
	}

	migrationState, err := m.msrw.Read()
	if err != nil {
		return fmt.Errorf("reading the migration state: %w", err)
	}

	if migrationState == nil {
		// no migration state, nothing to do
		return nil
	}

	return m.performRollback(upgdMarker, migrationState)
}

func (m Manager) performUpgrade(upgdMarker *upgrade.UpdateMarker, migState *state.MigrationState) error {
	for _, mig := range m.migrations {
		if !mig.IsApplicable(*upgdMarker) {
			continue
		}

		if err := mig.Upgrade(*upgdMarker); err != nil {
			return fmt.Errorf("running migration %s: %w", mig.ID(), err)
		}
		migState.AppliedMigrations = append(migState.AppliedMigrations, mig.ID())
		if err := m.msrw.Write(migState); err != nil {
			return fmt.Errorf("writing migration state: %w", err)
		}
	}
	return nil
}

func (m Manager) performRollback(upgdMarker *upgrade.UpdateMarker, migState *state.MigrationState) error {
	for i := len(migState.AppliedMigrations) - 1; i >= 0; i-- {
		migID := migState.AppliedMigrations[i]
		//FIXME we probably need a map to speed up things up a bit
		var mig Migration
		for _, m := range m.migrations {
			if m.ID() != migID {
				continue
			}
			mig = m
			break
		}

		if mig == nil {
			return fmt.Errorf("cannot rollback unknown migration %s", migID)
		}

		if err := mig.Rollback(*upgdMarker); err != nil {
			return fmt.Errorf("rolling back migration %s: %w", mig.ID(), err)
		}
		migState.AppliedMigrations = migState.AppliedMigrations[:len(migState.AppliedMigrations)-1]

		if err := m.msrw.Write(migState); err != nil {
			return fmt.Errorf("writing migration state: %w", err)
		}
	}
	return nil
}

func NewManager() *Manager {
	//TODO: populate migrations in order and inject MigrationStateReadWriter
	return new(Manager)
}
