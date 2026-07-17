package repository

import (
	"context"
	"time"

	refsqlc "github.com/elum2b/services/reference/sqlc"
)

func (r *Repository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return ExportPackage{}, err
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var items []refsqlc.ListExportItemsRow
	var localizationRows []refsqlc.ReferenceLocalization
	if err := r.WithTx(ctx, func(txRepo *Repository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		var err error
		items, err = txRepo.q.ListExportItems(ctx, refsqlc.ListExportItemsParams{
			WorkspaceID: workspaceID,
			Column2:     req.OnlyNotDeleted,
		})
		if err != nil {
			return err
		}

		localizationRows, err = txRepo.q.ListExportLocalizations(ctx, workspaceID)
		return err
	}); err != nil {
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

func mapExportLocalizations(rows []refsqlc.ReferenceLocalization) map[string]map[string]ExportText {
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
