package sqlc

import _ "embed"

//go:embed schema.sql
var SchemaSQL string

//go:embed catalog.sql
var CatalogSQL string
