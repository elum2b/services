// Package archiveadapter isolates shared queue implementation types from the API.
package archiveadapter

import (
	"context"
	"github.com/elum2b/services/collections/model"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	"io"
)

type queueArchive struct{ model.Archive }

func (a queueArchive) Store(ctx context.Context, o jobs.ArchiveObject, r io.Reader) (string, error) {
	return a.Archive.Store(ctx, model.ArchiveObject{Service: o.Service, WorkspaceID: o.WorkspaceID, Type: o.Type, FileName: o.FileName}, r)
}

type queueListingArchive struct {
	queueArchive
	lister model.ArchiveLister
}

func (a queueListingArchive) List(ctx context.Context) ([]jobs.ArchiveInfo, error) {
	rows, err := a.lister.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]jobs.ArchiveInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, jobs.ArchiveInfo{Key: row.Key, CreatedAt: row.CreatedAt})
	}
	return out, nil
}
func ToQueue(a model.Archive) jobs.Archive {
	base := queueArchive{a}
	if lister, ok := a.(model.ArchiveLister); ok {
		return queueListingArchive{queueArchive: base, lister: lister}
	}
	return base
}

type publicArchive struct{ jobs.Archive }

func (a publicArchive) Store(ctx context.Context, o model.ArchiveObject, r io.Reader) (string, error) {
	return a.Archive.Store(ctx, jobs.ArchiveObject{Service: o.Service, WorkspaceID: o.WorkspaceID, Type: o.Type, FileName: o.FileName}, r)
}

type publicListingArchive struct {
	publicArchive
	lister jobs.ArchiveLister
}

func (a publicListingArchive) List(ctx context.Context) ([]model.ArchiveInfo, error) {
	rows, err := a.lister.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]model.ArchiveInfo, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.ArchiveInfo{Key: row.Key, CreatedAt: row.CreatedAt})
	}
	return out, nil
}
func FromQueue(a jobs.Archive) model.Archive {
	base := publicArchive{a}
	if lister, ok := a.(jobs.ArchiveLister); ok {
		return publicListingArchive{publicArchive: base, lister: lister}
	}
	return base
}
