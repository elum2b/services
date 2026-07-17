package admin

import (
	"context"
)

func (a *Admin) ExportManifest(ctx context.Context) (ExportManifest, error) {
	if a == nil || a.repository == nil {
		return ExportManifest{}, ErrRepositoryNotConfigured
	}
	return a.repository.ExportManifest(), nil
}

func (a *Admin) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if a == nil || a.repository == nil {
		return ExportPackage{}, ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Export(mergedCtx, workspaceID, req)
}
