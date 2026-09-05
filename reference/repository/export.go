package repository

import (
	"context"
	"time"

	refsqlc "github.com/elum2b/services/reference/sqlc"
)

func (r *Repository) Export(
	ctx context.Context,
	workspaceID string,
	req ExportRequest,
) (ExportPackage, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return ExportPackage{}, err
	}

	var result ExportPackage
	err := r.WithTxTimeout(ctx, 15*time.Minute, func(ctx context.Context, tx *Repository) error {
		if _, err := tx.executor.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
			return err
		}
		var err error
		result, err = tx.exportPackage(ctx, workspaceID, req)
		return err
	})
	return result, err
}

// ExportArchive captures the entire manifest in one PostgreSQL snapshot.
func (r *Repository) ExportArchive(ctx context.Context, workspaceID string, includeMedia bool) (ArchivePackage, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return ArchivePackage{}, err
	}
	var result ArchivePackage
	err := r.WithTxTimeout(ctx, 15*time.Minute, func(ctx context.Context, tx *Repository) error {
		if _, err := tx.executor.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY"); err != nil {
			return err
		}
		var err error
		result.Package, err = tx.exportPackage(ctx, workspaceID, ExportRequest{OnlyNotDeleted: true})
		if err != nil {
			return err
		}
		if includeMedia {
			result.Resources, result.Links, err = tx.ExportArchiveData(ctx, workspaceID)
		}
		return err
	})
	return result, err
}

func (r *Repository) exportPackage(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	items, err := r.q.ListExportItems(ctx, refsqlc.ListExportItemsParams{WorkspaceID: workspaceID, Column2: req.OnlyNotDeleted})
	if err != nil {
		return ExportPackage{}, err
	}
	localizationRows, err := r.q.ListExportLocalizations(ctx, workspaceID)
	if err != nil {
		return ExportPackage{}, err
	}
	localizations := mapExportLocalizations(localizationRows)
	out := ExportPackage{
		Format:    ExportFormat,
		Service:   "reference",
		CreatedAt: now.UTC(),
		Items:     make([]ExportItem, 0, len(items)),
	}

	for _, item := range items {
		value := ExportItem{
			Key:          item.Key,
			Type:         item.ItemType,
			Payload:      item.Payload,
			IsActive:     item.IsActive,
			Deleted:      item.DeletedAt.Valid,
			Localization: localizations[item.Key],
		}

		out.Items = append(out.Items, value)
	}

	return out, nil
}

func mapExportLocalizations(
	rows []refsqlc.ReferenceLocalization,
) map[string]map[string]ExportText {
	result := make(map[string]map[string]ExportText)
	for _, row := range rows {
		if result[row.ItemKey] == nil {
			result[row.ItemKey] = make(map[string]ExportText)
		}

		result[row.ItemKey][row.Locale] = ExportText{
			Title:       row.Title,
			Description: row.Description,
		}
	}

	return result
}
