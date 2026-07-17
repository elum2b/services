package repository

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	importexport "github.com/elum2b/services/internal/utils/importexport"
)

var referenceImportItemKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

func (r *Repository) PreviewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return ImportPreview{}, err
	}
	if err := validateExportPackage(pkg); err != nil {
		return ImportPreview{}, err
	}
	preview := ImportPreview{Format: pkg.Format, Service: pkg.Service, Counts: countPackage(pkg)}
	existing, err := r.importExistingItemKeys(ctx, workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, item := range pkg.Items {
		if existing[item.Key] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "item", Key: item.Key})
		}
	}
	return preview, nil
}

func (r *Repository) Import(ctx context.Context, workspaceID string, req ImportRequest) (ImportResult, error) {
	if err := requireWorkspace(workspaceID); err != nil {
		return ImportResult{}, err
	}
	if err := validateExportPackage(req.Package); err != nil {
		return ImportResult{}, err
	}
	strategy := req.ConflictStrategy
	if strategy == "" {
		strategy = ImportConflictFail
	}
	if strategy != ImportConflictFail && strategy != ImportConflictSkip && strategy != ImportConflictUpdate {
		return ImportResult{}, fmt.Errorf("unsupported import conflict strategy: %s", strategy)
	}
	result := ImportResult{}
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		preview, err := txRepo.PreviewImport(ctx, workspaceID, req.Package)
		if err != nil {
			return err
		}
		if strategy == ImportConflictFail && len(preview.Conflicts) > 0 {
			return fmt.Errorf("import conflicts found: %d", len(preview.Conflicts))
		}

		return txRepo.importBulk(ctx, workspaceID, req.Package, strategy, preview, &result)
	})
	if err != nil {
		return ImportResult{}, err
	}
	methods := append([]string{}, referenceItemMutationCacheMethods...)
	methods = append(methods, referenceLocalizationMutationCacheMethods...)
	return result, r.bumpReferenceCacheVersions(workspaceID, methods...)
}

func (r *Repository) lockWorkspaceMutation(ctx context.Context, workspaceID string) error {
	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"reference:"+workspaceID,
	)
	return err
}

func (r *Repository) withWorkspaceMutation(
	ctx context.Context,
	workspaceID string,
	fn func(*Repository) error,
) error {
	return r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}
		return fn(txRepo)
	})
}

func (r *Repository) importBulk(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	conflicts := importConflictSet(preview)
	if err := r.importItemsBulk(
		ctx,
		workspaceID,
		pkg.Items,
		strategy,
		conflicts,
		result,
	); err != nil {
		return err
	}
	if err := r.replaceImportedItemLocalizations(
		ctx,
		workspaceID,
		pkg.Items,
		strategy,
		conflicts,
	); err != nil {
		return err
	}
	return r.importLocalizationsBulk(
		ctx,
		workspaceID,
		pkg.Items,
		strategy,
		conflicts,
		result,
	)
}

func (r *Repository) replaceImportedItemLocalizations(
	ctx context.Context,
	workspaceID string,
	items []ExportItem,
	strategy string,
	conflicts map[string]struct{},
) error {
	if strategy != ImportConflictUpdate {
		return nil
	}

	keys := make([]string, 0, len(items))
	for _, item := range items {
		if hasImportConflict(conflicts, "item", item.Key) {
			keys = append(keys, item.Key)
		}
	}
	if len(keys) == 0 {
		return nil
	}

	_, err := r.executor.ExecContext(
		ctx,
		`DELETE FROM reference_localization
WHERE workspace_id = $1
  AND item_key = ANY($2::text[])`,
		workspaceID,
		keys,
	)
	return err
}

func (r *Repository) importItemsBulk(
	ctx context.Context,
	workspaceID string,
	items []ExportItem,
	strategy string,
	conflicts map[string]struct{},
	result *ImportResult,
) error {
	keys := make([]string, 0, len(items))
	types := make([]string, 0, len(items))
	payloads := make([]string, 0, len(items))
	active := make([]bool, 0, len(items))
	deletedAt := make([]sql.NullTime, 0, len(items))
	for _, item := range items {
		if hasImportConflict(conflicts, "item", item.Key) && strategy == ImportConflictSkip {
			result.Skipped.Items++
			continue
		}
		keys = append(keys, item.Key)
		types = append(types, defaultString(item.Type, ItemTypeQuantity))
		payloads = append(payloads, defaultJSON(item.Payload, "{}"))
		active = append(active, item.IsActive)
		deletedAt = append(deletedAt, nullableDeletedAt(item.Deleted))
		result.Imported.Items++
	}
	if len(keys) == 0 {
		return nil
	}
	query := `
INSERT INTO reference_item (
    workspace_id, key, item_type, payload, is_active, deleted_at
)
SELECT
    $1,
    value.key,
    value.item_type,
    value.payload::jsonb,
    value.is_active,
    value.deleted_at
FROM unnest(
    $2::text[],
    $3::text[],
    $4::text[],
    $5::boolean[],
    $6::timestamptz[]
) AS value(key, item_type, payload, is_active, deleted_at)
	` + referenceItemConflictClause(strategy)
	return importexport.ForEachBatch(
		len(keys),
		5,
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			_, err := r.executor.ExecContext(
				ctx,
				query,
				workspaceID,
				keys[start:end],
				types[start:end],
				payloads[start:end],
				active[start:end],
				deletedAt[start:end],
			)
			return err
		},
	)
}

func (r *Repository) importLocalizationsBulk(
	ctx context.Context,
	workspaceID string,
	items []ExportItem,
	strategy string,
	conflicts map[string]struct{},
	result *ImportResult,
) error {
	itemKeys := make([]string, 0)
	locales := make([]string, 0)
	titles := make([]string, 0)
	descriptions := make([]string, 0)
	for _, item := range items {
		if hasImportConflict(conflicts, "item", item.Key) && strategy == ImportConflictSkip {
			continue
		}
		for locale, text := range item.Localization {
			itemKeys = append(itemKeys, item.Key)
			locales = append(locales, locale)
			titles = append(titles, text.Title)
			descriptions = append(descriptions, text.Description)
			result.Imported.Localizations++
		}
	}
	if len(itemKeys) == 0 {
		return nil
	}
	query := `
INSERT INTO reference_localization (
    workspace_id, item_key, locale, title, description
)
SELECT
    $1,
    value.item_key,
    value.locale,
    value.title,
    value.description
FROM unnest(
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[]
) AS value(item_key, locale, title, description)
	` + referenceLocalizationConflictClause(strategy)
	return importexport.ForEachBatch(
		len(itemKeys),
		4,
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			_, err := r.executor.ExecContext(
				ctx,
				query,
				workspaceID,
				itemKeys[start:end],
				locales[start:end],
				titles[start:end],
				descriptions[start:end],
			)
			return err
		},
	)
}

func referenceItemConflictClause(strategy string) string {
	switch strategy {
	case ImportConflictSkip:
		return " ON CONFLICT (workspace_id, key) DO NOTHING"
	case ImportConflictUpdate:
		return ` ON CONFLICT (workspace_id, key) DO UPDATE SET
    item_type = EXCLUDED.item_type,
    payload = EXCLUDED.payload,
    is_active = EXCLUDED.is_active,
    deleted_at = EXCLUDED.deleted_at,
    updated_at = now()`
	default:
		return ""
	}
}

func referenceLocalizationConflictClause(strategy string) string {
	switch strategy {
	case ImportConflictSkip:
		return " ON CONFLICT (workspace_id, item_key, locale) DO NOTHING"
	case ImportConflictUpdate:
		return ` ON CONFLICT (workspace_id, item_key, locale) DO UPDATE SET
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    updated_at = now()`
	default:
		return ""
	}
}

func validateExportPackage(pkg ExportPackage) error {
	if pkg.Format != ExportFormat {
		return fmt.Errorf("unsupported export format: %s", pkg.Format)
	}
	if pkg.Service != "reference" {
		return fmt.Errorf("unsupported export service: %s", pkg.Service)
	}

	itemKeys := make(map[string]int, len(pkg.Items))
	for itemIndex, item := range pkg.Items {
		prefix := fmt.Sprintf("reference import items[%d]", itemIndex)
		if !referenceImportItemKeyPattern.MatchString(item.Key) {
			return fmt.Errorf("%s.key: invalid item key", prefix)
		}
		if previousIndex, exists := itemKeys[item.Key]; exists {
			return fmt.Errorf("%s.key: duplicates items[%d].key", prefix, previousIndex)
		}
		itemKeys[item.Key] = itemIndex

		itemType := defaultString(item.Type, ItemTypeQuantity)
		if itemType != ItemTypeQuantity && itemType != ItemTypeDuration {
			return fmt.Errorf("%s.type: unsupported value %q", prefix, itemType)
		}
		if len(item.Payload) == 0 || !json.Valid(item.Payload) {
			return fmt.Errorf("%s.payload: must be valid JSON", prefix)
		}

		for locale, text := range item.Localization {
			if strings.TrimSpace(locale) == "" {
				return fmt.Errorf("%s.localization: locale is required", prefix)
			}
			if strings.TrimSpace(text.Title) == "" {
				return fmt.Errorf("%s.localization.%s.title: title is required", prefix, locale)
			}
		}
	}

	return nil
}

func countPackage(pkg ExportPackage) ImportCounts {
	var counts ImportCounts
	counts.Items = uint64(len(pkg.Items))
	for _, item := range pkg.Items {
		counts.Localizations += uint64(len(item.Localization))
	}
	return counts
}

func (r *Repository) importExistingItemKeys(ctx context.Context, workspaceID string) (map[string]bool, error) {
	keys, err := r.q.ListImportItemKeys(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(keys))
	for _, key := range keys {
		result[key] = true
	}
	return result, nil
}

func importConflictSet(preview ImportPreview) map[string]struct{} {
	result := make(map[string]struct{}, len(preview.Conflicts))
	for _, conflict := range preview.Conflicts {
		result[importConflictKey(conflict.Type, conflict.Key)] = struct{}{}
	}
	return result
}

func hasImportConflict(conflicts map[string]struct{}, kind, key string) bool {
	_, ok := conflicts[importConflictKey(kind, key)]
	return ok
}

func importConflictKey(kind, key string) string {
	return kind + "\x00" + key
}

func defaultJSON(value []byte, fallback string) string {
	if len(value) == 0 {
		return fallback
	}
	return string(value)
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func nullableDeletedAt(deleted bool) sql.NullTime {
	if !deleted {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: time.Now().UTC(), Valid: true}
}
