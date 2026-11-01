package sqlc

import _ "embed"

var (
	//go:embed streak_count_migration.sql
	StreakCountMigrationSQL string

	//go:embed schema.sql
	SchemaSQL string

	//go:embed trigger.sql
	TriggerSQL string

	//go:embed event.sql
	EventSQL string
)
