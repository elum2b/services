package repository

import (
	json "github.com/goccy/go-json"
	"time"
)

const (
	ItemTypeQuantity = "quantity"
	ItemTypeDuration = "duration"
)

type Item struct {
	WorkspaceID   string
	Key           string
	Type          string
	Payload       json.RawMessage
	IsActive      bool
	DeletedAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Localization  *Localization
	Localizations []Localization
}

type Localization struct {
	WorkspaceID string
	ItemKey     string
	Locale      string
	Title       string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type SaveItemParams struct {
	WorkspaceID string
	Key         string
	Type        string
	Payload     json.RawMessage
	IsActive    bool
}

type DangerousChangeTypeParams struct {
	WorkspaceID string
	Key         string
	CurrentType string
	NewType     string
}

type ListItemsParams struct {
	WorkspaceID    string
	Type           string
	OnlyNotDeleted bool
	Limit          int32
	Offset         int32
}

type Stats struct {
	ItemsTotal      uint64
	ItemsNotDeleted uint64
	ActiveItems     uint64
	DeletedItems    uint64
	QuantityItems   uint64
	DurationItems   uint64
}
