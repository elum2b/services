package sqlc

import _ "embed"

var (
	//go:embed schema.sql
	SchemaSQL string

	//go:embed event.sql
	EventSQL string

	//go:embed trigger.sql
	TriggerSQL string
)
