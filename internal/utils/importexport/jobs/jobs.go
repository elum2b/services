// Package jobs manages persistent, workspace-scoped import and export jobs.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	json "github.com/goccy/go-json"
)

const (
	DefaultTable         = "importexport_job"
	DefaultWorkerID      = "importexport-worker"
	DefaultIdleDelay     = time.Second
	DefaultLeaseTimeout  = 10 * time.Minute
	DefaultRetention     = 24 * time.Hour
	DefaultCleanupPeriod = time.Hour
	DefaultHistoryLimit  = int32(100)
	DefaultCleanupLimit  = int32(100)
	StatusQueued         = "queued"
	StatusProcessing     = "processing"
	StatusCompleted      = "completed"
	StatusFailed         = "failed"
	TypeExport           = "export"
	TypeImport           = "import"
)

var (
	ErrActiveJob       = errors.New("importexport jobs: active job already exists")
	ErrJobNotFound     = errors.New("importexport jobs: job not found")
	ErrArchiveNotReady = errors.New("importexport jobs: archive is not available")
	ErrNotLeased       = errors.New("importexport jobs: job is not leased by worker")
)

// Archive owns durable dump storage. The manager calls Delete after retention
// expires but never deletes the persistent job or its history.
type Archive interface {
	Store(context.Context, ArchiveObject, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type ArchiveObject struct {
	Service     string
	WorkspaceID string
	Type        string
	FileName    string
}

// Handler contains service-specific serialization and import application.
// Export must return a readable dump; the manager stores and later deletes it.
type Handler interface {
	Export(context.Context, Job) (io.ReadCloser, error)
	Import(context.Context, Job, io.Reader) error
}

type Job struct {
	ID             int64
	Service        string
	WorkspaceID    string
	Type           string
	Status         string
	FileName       string
	Options        json.RawMessage
	ArchiveKey     string
	ArchiveExpires *time.Time
	Error          string
	LockedBy       string
	LeaseToken     string
	LockedUntil    *time.Time
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	UpdatedAt      time.Time
}

func newToken() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		// crypto/rand failing is exceptional; time still prevents normal collisions.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

type HistoryEntry struct {
	ID        int64
	JobID     int64
	Status    string
	Message   string
	CreatedAt time.Time
}

type QueueExportParams struct {
	Service     string
	WorkspaceID string
	FileName    string
	Options     json.RawMessage
}

type QueueImportParams struct {
	Service     string
	WorkspaceID string
	FileName    string
	Options     json.RawMessage
	Dump        io.Reader
}

type HistoryParams struct {
	Service     string
	WorkspaceID string
	Limit       int32
	Offset      int32
}

type StatusParams struct {
	Service     string
	WorkspaceID string
	ID          int64
}

type JobHistoryParams struct {
	Service     string
	WorkspaceID string
	ID          int64
	Limit       int32
	Offset      int32
}

type DownloadParams struct {
	Service     string
	WorkspaceID string
	ID          int64
}

type Options struct {
	TableName     string
	WorkerID      string
	IdleDelay     time.Duration
	LeaseTimeout  time.Duration
	Retention     time.Duration
	CleanupPeriod time.Duration
}
