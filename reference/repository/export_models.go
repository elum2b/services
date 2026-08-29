package repository

import (
	"time"

	json "github.com/goccy/go-json"
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

// ArchivePackage is the media portion of a Reference ZIP export. Package is
// kept separate so the established JSON ExportPackage format remains intact.
type ArchivePackage struct {
	Package   ExportPackage        `json:"package"`
	Resources []ExportResource     `json:"resources,omitempty"`
	Links     []ExportResourceLink `json:"links,omitempty"`
}

type ExportResource struct {
	Key            string          `json:"key"`
	Type           string          `json:"type"`
	Payload        json.RawMessage `json:"payload"`
	IsActive       bool            `json:"is_active"`
	Format         string          `json:"format"`
	ContentType    string          `json:"content_type"`
	SHA256         string          `json:"sha256"`
	MediaVersion   string          `json:"media_version"`
	Size           int64           `json:"size"`
	Width          int             `json:"width"`
	Height         int             `json:"height"`
	PlaceholderRef string          `json:"-"`
}

type ExportResourceLink struct {
	ItemKey     string `json:"item_key"`
	ResourceKey string `json:"resource_key"`
	Position    int32  `json:"position"`
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
