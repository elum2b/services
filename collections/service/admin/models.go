package admin

import (
	"io"
	"time"

	"github.com/elum2b/services/collections/model"

	"github.com/elum2b/services/internal/utils/importexport/jobs"
)

type QueueArchiveExportParams struct {
	WorkspaceID string
	FileName    string
	ExportRequest
}
type QueueArchiveImportParams struct {
	WorkspaceID string
	FileName    string
	ImportRequest
	Archive io.Reader
}
type ArchiveJob struct {
	ID             int64      `json:"id"`
	Service        string     `json:"service"`
	WorkspaceID    string     `json:"workspace_id"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	FileName       string     `json:"file_name"`
	ArchiveExpires *time.Time `json:"archive_expires,omitempty"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
	Attempt        int        `json:"attempt"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
}

type ArchiveJobHistoryEntry = jobs.HistoryEntry

type Page = model.Page
type ExportRequest = model.ExportRequest
type ImportRequest = model.ImportRequest
