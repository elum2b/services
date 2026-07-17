package admin

import (
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/reference/repository"
)

const DangerousTypeConfirmation = "CHANGE_REFERENCE_TYPE"

type Page struct {
	Limit  int32
	Offset int32
}

type SaveItemParams struct {
	WorkspaceID string
	Key         string
	Type        string
	Payload     json.RawMessage
	IsActive    bool
}

type UpdateItemParams struct {
	WorkspaceID string
	Key         string
	Payload     json.RawMessage
	IsActive    bool
}

type DangerousChangeTypeParams struct {
	WorkspaceID  string
	Key          string
	CurrentType  string
	NewType      string
	Confirmation string
}

type ItemListParams struct {
	WorkspaceID    string
	Type           string
	OnlyNotDeleted bool
	Page           Page
}

type LocalizationModel struct {
	Locale      string    `json:"locale"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ItemModel struct {
	Key           string              `json:"key"`
	Type          string              `json:"type"`
	Payload       json.RawMessage     `json:"payload"`
	IsActive      bool                `json:"is_active"`
	DeletedAt     *time.Time          `json:"deleted_at,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Localizations []LocalizationModel `json:"localizations,omitempty"`
}

type SaveLocalizationParams struct {
	WorkspaceID string
	ItemKey     string
	Locale      string
	Title       string
	Description string
}

type StatsModel struct {
	ItemsTotal      uint64 `json:"items_total"`
	ItemsNotDeleted uint64 `json:"items_not_deleted"`
	ActiveItems     uint64 `json:"active_items"`
	DeletedItems    uint64 `json:"deleted_items"`
	QuantityItems   uint64 `json:"quantity_items"`
	DurationItems   uint64 `json:"duration_items"`
}

type ExportRequest = repository.ExportRequest
type ExportPackage = repository.ExportPackage
type ExportItem = repository.ExportItem
type ExportText = repository.ExportText
type ImportRequest = repository.ImportRequest
type ImportPreview = repository.ImportPreview
type ImportCounts = repository.ImportCounts
type ImportConflict = repository.ImportConflict
type ImportResult = repository.ImportResult
