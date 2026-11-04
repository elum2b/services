// Package model defines the transport-independent Collections contract.
package model

import (
	"time"

	services "github.com/elum2b/services"
)

const MaxBatch = 1000
const MaxCatalogEntries = 100000
const ExportFormat = "collections.export.v1"

type ConflictStrategy string

const (
	ConflictUpdate ConflictStrategy = "update_existing"
	ConflictSkip   ConflictStrategy = "skip_existing"
	ConflictFail   ConflictStrategy = "fail_on_conflict"
)

type Text struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}
type Page struct {
	Limit  int32 `json:"limit"`
	Offset int32 `json:"offset"`
}
type Collection struct {
	Key          string          `json:"key"`
	Position     int32           `json:"position"`
	Active       bool            `json:"active"`
	Archived     bool            `json:"archived"`
	Localization map[string]Text `json:"localization,omitempty"`
	Entries      []Entry         `json:"entries,omitempty"`
}
type Entry struct {
	ItemKey  string `json:"item_key"`
	Position int32  `json:"position"`
	Active   bool   `json:"active"`
}
type ItemRef struct {
	CollectionKey string `json:"collection_key"`
	ItemKey       string `json:"item_key"`
}
type ItemState struct {
	ItemRef
	Owned     bool       `json:"owned"`
	Revision  int64      `json:"revision"`
	ChangedAt *time.Time `json:"changed_at,omitempty"`
}
type OwnershipChange struct {
	ItemRef
	Owned            bool   `json:"owned"`
	ExpectedRevision *int64 `json:"expected_revision,omitempty"`
}
type SetOwnedParams struct {
	Identity    services.Identity `json:"identity"`
	Source      string            `json:"source"`
	OperationID string            `json:"operation_id"`
	OwnershipChange
}
type SetOwnedBatchParams struct {
	Identity    services.Identity `json:"identity"`
	Source      string            `json:"source"`
	OperationID string            `json:"operation_id"`
	Changes     []OwnershipChange `json:"changes"`
}
type ChangeResult struct {
	ItemState
	Before  bool `json:"before"`
	Changed bool `json:"changed"`
}
type Receipt struct {
	ID          string         `json:"id"`
	OperationID string         `json:"operation_id"`
	Source      string         `json:"source"`
	Results     []ChangeResult `json:"results"`
	CreatedAt   time.Time      `json:"created_at"`
}
type ChangeEvent struct {
	services.Identity
	ReceiptID   string `json:"receipt_id"`
	OperationID string `json:"operation_id"`
	Source      string `json:"source"`
	ChangeResult
}
type ReadParams struct {
	// Identity is the authenticated caller; Subject is resolved by the trusted backend.
	Identity services.Identity
	Subject  *services.Identity
	Locale   string
	Page     Page
}
type HasItemParams struct {
	ReadParams
	ItemRef
}
type HasItemsParams struct {
	ReadParams
	Items []ItemRef
}
type GetCollectionParams struct {
	ReadParams
	CollectionKey string
	// KnownVersion is the opaque version previously returned for this exact page/locale.
	KnownVersion string
}
type ListOwnedParams struct {
	ReadParams
	CollectionKeys []string
}

// CollectionView is shared catalog data. It never contains user ownership.
type CollectionView struct {
	Key         string  `json:"key"`
	Position    int32   `json:"position"`
	Text        Text    `json:"text"`
	Version     string  `json:"version"`
	NotModified bool    `json:"not_modified,omitempty"`
	Entries     []Entry `json:"entries,omitempty"`
}
type CollectionSummary struct {
	Key      string `json:"key"`
	Position int32  `json:"position"`
	Text     Text   `json:"text"`
}
type CatalogParams struct {
	WorkspaceID string
	Key         string
	Page        Page
}
type SaveCollectionParams struct {
	WorkspaceID string
	ActorID     string
	Collection  Collection
}
type ArchiveCollectionParams struct {
	WorkspaceID string
	ActorID     string
	Key         string
}
type EntryParams struct {
	WorkspaceID   string
	ActorID       string
	CollectionKey string
	Entry         Entry
}
type ReorderEntriesParams struct {
	WorkspaceID   string
	ActorID       string
	CollectionKey string
	ItemKeys      []string
}
type CorrectOwnedParams struct {
	SetOwnedBatchParams
	ActorID string
	Reason  string
}
type JournalParams struct {
	WorkspaceID string
	Identity    *services.Identity
	Page        Page
}
type JournalEntry struct {
	Receipt  Receipt           `json:"receipt"`
	Identity services.Identity `json:"identity"`
	ActorID  string            `json:"actor_id,omitempty"`
	Reason   string            `json:"reason,omitempty"`
}
type StatsParams struct {
	WorkspaceID   string
	CollectionKey string
}
type Stats struct {
	StoredOwned    int64 `json:"stored_owned"`
	AvailableOwned int64 `json:"available_owned"`
	Grants         int64 `json:"grants"`
	Revokes        int64 `json:"revokes"`
}
type DailyParams struct {
	WorkspaceID string
	From        time.Time
	Until       time.Time
	Page        Page
}
type DailyStats struct {
	Date    time.Time `json:"date"`
	Grants  int64     `json:"grants"`
	Revokes int64     `json:"revokes"`
}
type ExportRequest struct{}
type ExportPackage struct {
	Format      string       `json:"format"`
	Service     string       `json:"service"`
	Collections []Collection `json:"collections"`
}
type ImportRequest struct {
	ActorID          string           `json:"actor_id"`
	Package          ExportPackage    `json:"package"`
	ConflictStrategy ConflictStrategy `json:"conflict_strategy,omitempty"`
}
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}
type ImportPreview struct {
	Conflicts []string `json:"conflicts"`
}

type CallbackStatus string

const (
	CallbackPending    CallbackStatus = "pending"
	CallbackProcessing CallbackStatus = "processing"
	CallbackOK         CallbackStatus = "ok"
	CallbackRejected   CallbackStatus = "reject"
)

type CallbackParams struct {
	WorkspaceID string
	ID          uint64
	Status      CallbackStatus
	Reason      string
	Page        Page
}
type CallbackEvent struct {
	ID           uint64         `json:"id"`
	WorkspaceID  string         `json:"workspace_id"`
	EventKey     string         `json:"event_key"`
	Status       CallbackStatus `json:"status"`
	Payload      []byte         `json:"payload"`
	AttemptCount uint32         `json:"attempt_count"`
}
