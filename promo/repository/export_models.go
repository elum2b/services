package repository

import (
	json "github.com/goccy/go-json"
	"time"
)

const (
	ExportFormat         = "promo.export.v1"
	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Now time.Time `json:"now"`
}

type ExportPackage struct {
	Format    string        `json:"format"`
	Service   string        `json:"service"`
	CreatedAt time.Time     `json:"created_at"`
	Promos    []ExportPromo `json:"promos"`
}

type ExportPromo struct {
	Code           string                `json:"code"`
	Payload        json.RawMessage       `json:"payload"`
	Target         json.RawMessage       `json:"target,omitempty"`
	MaxActivations uint64                `json:"max_activations"`
	IsActive       bool                  `json:"is_active"`
	StartAt        *time.Time            `json:"start_at,omitempty"`
	EndAt          *time.Time            `json:"end_at,omitempty"`
	Localization   map[string]ExportText `json:"localization,omitempty"`
	Rewards        []ExportReward        `json:"rewards,omitempty"`
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
	Promos        uint64 `json:"promos"`
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
