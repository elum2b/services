package sqlc

import _ "embed"

//go:embed schema.sql
var SchemaSQL string

//go:embed trigger.sql
var TriggerSQL string

//go:embed event.sql
var EventSQL string
