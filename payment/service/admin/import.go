package admin

import "context"

func (a *Admin) PreviewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	if a == nil || a.repository == nil {
		return ImportPreview{}, ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.PreviewImport(mergedCtx, workspaceID, pkg)
}

func (a *Admin) Import(ctx context.Context, workspaceID string, req ImportRequest) (ImportResult, error) {
	if a == nil || a.repository == nil {
		return ImportResult{}, ErrRepositoryNotConfigured
	}
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.Import(mergedCtx, workspaceID, req)
}
