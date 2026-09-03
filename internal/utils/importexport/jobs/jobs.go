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
	DefaultTable                   = "importexport_job"
	DefaultWorkerID                = "importexport-worker"
	DefaultIdleDelay               = time.Second
	DefaultLeaseTimeout            = 10 * time.Minute
	DefaultRetention               = 24 * time.Hour
	DefaultCleanupPeriod           = time.Hour
	DefaultHistoryLimit            = int32(100)
	DefaultCleanupLimit            = int32(100)
	DefaultMaxUploadBytes          = int64(100 << 20)
	DefaultMaxZIPEntries           = 1000
	DefaultMaxZIPUncompressedBytes = int64(500 << 20)
	DefaultMaxZIPCompressionRatio  = 100
	DefaultMaxAttempts             = 3
	DefaultRetryBackoff            = time.Second
	DefaultHandlerTimeout          = time.Hour
	StatusQueued                   = "queued"
	StatusProcessing               = "processing"
	StatusCompleted                = "completed"
	StatusFailed                   = "failed"
	TypeExport                     = "export"
	TypeImport                     = "import"
)

var (
	ErrActiveJob = errors.New(
		"importexport jobs: active job already exists",
	)
	ErrJobNotFound     = errors.New("importexport jobs: job not found")
	ErrArchiveNotReady = errors.New(
		"importexport jobs: archive is not available",
	)
	ErrNotLeased = errors.New(
		"importexport jobs: job is not leased by worker",
	)
	ErrArchiveTooLarge = errors.New(
		"importexport jobs: archive exceeds upload limit",
	)
	ErrInvalidZIP = errors.New("importexport jobs: invalid ZIP archive")
)

// Archive owns durable dump storage. The manager calls Delete after retention
// expires but never deletes the persistent job or its history.
type Archive interface {
	Store(context.Context, ArchiveObject, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// ArchiveLister is optional. Archives that implement it can be swept for
// objects left behind when a process stops between storing an archive and
// persisting its job.
type ArchiveLister interface {
	List(context.Context) ([]ArchiveInfo, error)
}

type ArchiveInfo struct {
	Key       string
	CreatedAt time.Time
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
	Attempt        int
	NextAttemptAt  *time.Time
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
	// ManifestName requires exactly one named entry in the uploaded ZIP.
	// Archive services use this to enforce their single-manifest contract.
	ManifestName string
	Options      json.RawMessage
	Dump         io.Reader
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
	// Service identifies the service whose jobs this manager may process.
	Service        string
	TableName      string
	WorkerID       string
	IdleDelay      time.Duration
	LeaseTimeout   time.Duration
	Retention      time.Duration
	CleanupPeriod  time.Duration
	MaxUploadBytes int64
	ZIPLimits      ZIPLimits
	MaxAttempts    int
	RetryBackoff   time.Duration
	HandlerTimeout time.Duration
}

// ZIPLimits bound ZIP metadata before a manifest consumer opens an entry.
// A zero field uses the package default.
type ZIPLimits struct {
	MaxEntries           int
	MaxCompressedBytes   int64
	MaxUncompressedBytes int64
	MaxCompressionRatio  int
}
