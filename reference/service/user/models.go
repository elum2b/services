package user

import (
	json "github.com/goccy/go-json"
	"time"
)

type Page struct {
	Limit  int32
	Offset int32
}

type LocalizationModel struct {
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ItemModel struct {
	Key          string             `json:"key"`
	Type         string             `json:"type"`
	Payload      json.RawMessage    `json:"payload"`
	Localization *LocalizationModel `json:"localization,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
}

type GetParams struct {
	WorkspaceID string
	Key         string
	Locale      string
}

type ResolveParams struct {
	WorkspaceID string
	Keys        []string
	Locale      string
}

type ListParams struct {
	WorkspaceID string
	Locale      string
	Page        Page
}

type ResolveResult struct {
	Items       []ItemModel `json:"items"`
	MissingKeys []string    `json:"missing_keys,omitempty"`
}
