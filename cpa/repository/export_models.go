package repository

import (
	"time"

	json "github.com/goccy/go-json"
)

const (
	ExportFormat         = "cpa.export.v1"
	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Now time.Time `json:"now,omitempty"`
}

type ExportPackage struct {
	Format    string        `json:"format"`
	Service   string        `json:"service"`
	CreatedAt time.Time     `json:"created_at"`
	Offers    []ExportOffer `json:"offers"`
}

type ExportOffer struct {
	ID                string                `json:"id"`
	Payload           json.RawMessage       `json:"payload"`
	Target            json.RawMessage       `json:"target,omitempty"`
	CodeMode          string                `json:"code_mode"`
	CodeSource        *string               `json:"code_source,omitempty"`
	SharedCode        *string               `json:"shared_code,omitempty"`
	GeneratedLength   *int16                `json:"generated_length,omitempty"`
	GeneratedAlphabet *string               `json:"generated_alphabet,omitempty"`
	IsActive          bool                  `json:"is_active"`
	StartAt           *time.Time            `json:"start_at,omitempty"`
	EndAt             *time.Time            `json:"end_at,omitempty"`
	Localization      map[string]ExportText `json:"localization,omitempty"`
	Rewards           []ExportReward        `json:"rewards,omitempty"`
}

type ExportText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ExportReward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
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
	Offers        uint64 `json:"offers"`
	Localizations uint64 `json:"localizations"`
	Rewards       uint64 `json:"rewards"`
}

type ImportConflict struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type ImportResult struct {
	Imported ImportCounts `json:"imported"`
	Skipped  ImportCounts `json:"skipped"`
}
