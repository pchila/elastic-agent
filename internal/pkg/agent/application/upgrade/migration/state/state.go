// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package state

import "time"

const migrationStateFile = ".migrations-state"

type MigrationState struct {
	// MigrationStarted contains the timestamp of when the migration process started
	MigrationStarted time.Time `json:"migration_started" yaml:"migration_started"`
	// MigrationCompleted contains the timestamp of when the migration process completed successfully
	MigrationCompleted time.Time `json:"migration_completed" yaml:"migration_completed"`
	// PlannedMigration keeps track of which migrations have been detected for upgrade, by ID
	PlannedMigrations []string `json:"planned_migrations" yaml:"planned_migrations"`
	// AppliedMigrations keeps track of which migrations have been successfully applied during upgrade, by ID
	AppliedMigrations []string `json:"applied_migrations" yaml:"applied_migrations"`
}
