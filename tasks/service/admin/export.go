package admin

import "context"

func (a *Admin) ExportManifest(ctx context.Context) (ExportManifest, error) {
	if a == nil || a.repository == nil {
		return ExportManifest{}, ErrRepositoryNotConfigured
	}

	return a.repository.ExportManifest(), nil
}
