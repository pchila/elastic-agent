// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package migration

import (
	"fmt"

	"github.com/elastic/elastic-agent/internal/pkg/agent/application/upgrade"
)

type Migration interface {
	ID() string
	Upgrade(upgrade.UpdateMarker) error
	Rollback(upgrade.UpdateMarker) error
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
}

func (m Manager) RunMigrations(um UpdateMarkerReadWriter) error {
	upgdMarker, err := um.Read()
	if err != nil {
		return fmt.Errorf("reading the upgrade marker: %w", err)
	}
	if upgdMarker == nil {
		// no upgrade marker, nothing to do
		return nil
	}

	return m.performUpgrade(upgdMarker, um)
}

func (m Manager) RollbackMigrations(um UpdateMarkerReadWriter) error {
	upgdMarker, err := um.Read()
	if err != nil {
		return fmt.Errorf("reading the upgrade marker: %w", err)
	}
	if upgdMarker == nil {
		// no upgrade marker, nothing to do ---> is this correct in a rollback scenario?
		return nil
	}

	return m.performRollback(upgdMarker, um)
}

func (m Manager) performUpgrade(upgdMarker *upgrade.UpdateMarker, w UpdateMarkerWriter) error {
	for _, mig := range m.migrations {
		if err := mig.Upgrade(*upgdMarker); err != nil {
			return fmt.Errorf("running migration %s: %w", mig.ID(), err)
		}
		upgdMarker.AppliedMigrations = append(upgdMarker.AppliedMigrations, mig.ID())
		w.Write(upgdMarker)
	}
	return nil
}

func (m Manager) performRollback(upgdMarker *upgrade.UpdateMarker, w UpdateMarkerWriter) error {
	for i := len(upgdMarker.AppliedMigrations) - 1; i >= 0; i-- {
		migID := upgdMarker.AppliedMigrations[i]
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
		upgdMarker.AppliedMigrations = upgdMarker.AppliedMigrations[:len(upgdMarker.AppliedMigrations)-1]
		w.Write(upgdMarker)
	}
	return nil
}

func NewManager() *Manager {
	return new(Manager)
}
