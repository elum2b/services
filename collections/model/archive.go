package model

import (
	"context"
	"io"
	"time"
)

type ArchiveObject struct {
	Service     string
	WorkspaceID string
	Type        string
	FileName    string
}
type ArchiveInfo struct {
	Key       string
	CreatedAt time.Time
}
type Archive interface {
	Store(context.Context, ArchiveObject, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

// ArchiveLister permits orphan object cleanup; implement it for custom stores.
type ArchiveLister interface {
	List(context.Context) ([]ArchiveInfo, error)
}
