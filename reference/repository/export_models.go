package repository

import (
	json "github.com/goccy/go-json"
	"time"
)

const (
	ExportFormat         = "reference.export.v1"
	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Now            time.Time `json:"now,omitempty"`
	OnlyNotDeleted bool      `json:"only_not_deleted,omitempty"`
}

type ExportPackage struct {
	Format    string       `json:"format"`
	Service   string       `json:"service"`
	CreatedAt time.Time    `json:"created_at"`
	Items     []ExportItem `json:"items"`
}

type ExportItem struct {
	Key          string                `json:"key"`
	Type         string                `json:"type"`
	Payload      json.RawMessage       `json:"payload"`
	IsActive     bool                  `json:"is_active"`
	Deleted      bool                  `json:"deleted,omitempty"`
	Localization map[string]ExportText `json:"localization,omitempty"`
}

type ExportText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ImportRequest struct {
	Package          ExportPackage `json:"package"`
	ConflictStrategy string        `json:"conflict_strategy,omitempty"`
}

type ImportPreview struct {
	Format    string           `json:"format"`
	Service   string           `json:"service"`
	Counts    ImportCounts     `json:"counts"`
	Conflicts []ImportConflict `json:"conflicts,omitempty"`
}

type ImportCounts struct {
	Items         uint64 `json:"items"`
	Localizations uint64 `json:"localizations"`
}

type ImportConflict struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type ImportResult struct {
	Imported ImportCounts `json:"imported"`
	Skipped  ImportCounts `json:"skipped"`
}
