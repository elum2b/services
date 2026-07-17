package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	importexport "github.com/elum2b/services/internal/utils/importexport"
	"github.com/elum2b/services/internal/utils/target"
)

func (r *Repository) PreviewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	if err := validateExportPackage(workspaceID, pkg); err != nil {
		return ImportPreview{}, err
	}
	preview := ImportPreview{
		Format:  pkg.Format,
		Service: pkg.Service,
		Counts:  countPackage(pkg),
	}
	existing, err := r.importExistingPromoCodes(ctx, workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, promo := range pkg.Promos {
		key := normalizeCode(promo.Code)
		if existing[key] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{
				Type: "promo",
				Key:  promo.Code,
			})
		}
	}
	return preview, nil
}

func (r *Repository) Import(ctx context.Context, workspaceID string, req ImportRequest) (ImportResult, error) {
	if err := validateExportPackage(workspaceID, req.Package); err != nil {
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
	return result, r.invalidatePromoCache(workspaceID)
}

func (r *Repository) lockWorkspaceMutation(ctx context.Context, workspaceID string) error {
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}

	_, err := r.executor.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"promo:"+workspaceID,
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
	if err := r.importPromosBulk(
		ctx,
		workspaceID,
		pkg.Promos,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}
	ids, err := r.importPromoIDs(
		ctx,
		workspaceID,
		pkg.Promos,
		strategy,
		preview,
	)
	if err != nil {
		return err
	}
	if err := r.replaceImportedPromoChildren(
		ctx,
		workspaceID,
		pkg.Promos,
		ids,
		strategy,
		preview,
	); err != nil {
		return err
	}
	if err := r.importLocalizationsBulk(
		ctx,
		workspaceID,
		pkg.Promos,
		ids,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}
	return r.importRewardsBulk(
		ctx,
		workspaceID,
		pkg.Promos,
		ids,
		strategy,
		preview,
		result,
	)
}

func (r *Repository) replaceImportedPromoChildren(
	ctx context.Context,
	workspaceID string,
	promos []ExportPromo,
	ids map[string]uint64,
	strategy string,
	preview ImportPreview,
) error {
	if strategy != ImportConflictUpdate {
		return nil
	}

	promoIDs := make([]int64, 0, len(promos))
	for _, promo := range promos {
		if previewHasConflict(preview, "promo", promo.Code) {
			promoIDs = append(promoIDs, int64(ids[normalizeCode(promo.Code)]))
		}
	}
	if len(promoIDs) == 0 {
		return nil
	}

	if _, err := r.executor.ExecContext(
		ctx,
		`DELETE FROM promo_localization
WHERE workspace_id = $1
  AND promo_id = ANY($2::bigint[])`,
		workspaceID,
		promoIDs,
	); err != nil {
		return err
	}
	_, err := r.executor.ExecContext(
		ctx,
		`DELETE FROM promo_reward
WHERE workspace_id = $1
  AND promo_id = ANY($2::bigint[])`,
		workspaceID,
		promoIDs,
	)
	return err
}

func (r *Repository) importPromosBulk(
	ctx context.Context,
	workspaceID string,
	promos []ExportPromo,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(promos))
	for _, promo := range promos {
		if previewHasConflict(preview, "promo", promo.Code) && strategy == ImportConflictSkip {
			result.Skipped.Promos++
			continue
		}
		rows = append(rows, []any{
			workspaceID,
			promo.Code,
			normalizeCode(promo.Code),
			defaultJSON(promo.Payload, "{}"),
			defaultJSON(promo.Target, "null"),
			promo.MaxActivations,
			promo.IsActive,
			nullTime(promo.StartAt),
			nullTime(promo.EndAt),
		})
		result.Imported.Promos++
	}
	return r.execImportBulk(
		ctx,
		"promo_offer",
		[]string{
			"workspace_id",
			"code",
			"code_normalized",
			"payload",
			"target",
			"max_activations",
			"is_active",
			"start_at",
			"end_at",
		},
		rows,
		"(workspace_id, code_normalized)",
		"code = EXCLUDED.code, payload = EXCLUDED.payload, target = EXCLUDED.target, max_activations = EXCLUDED.max_activations, "+
			"is_active = EXCLUDED.is_active, start_at = EXCLUDED.start_at, end_at = EXCLUDED.end_at, deleted_at = NULL, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importPromoIDs(
	ctx context.Context,
	workspaceID string,
	promos []ExportPromo,
	strategy string,
	preview ImportPreview,
) (map[string]uint64, error) {
	needed := make(map[string]struct{}, len(promos))
	for _, promo := range promos {
		if previewHasConflict(preview, "promo", promo.Code) && strategy == ImportConflictSkip {
			continue
		}
		needed[normalizeCode(promo.Code)] = struct{}{}
	}
	rows, err := r.q.ListImportPromoIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]uint64, len(needed))
	for _, row := range rows {
		if _, ok := needed[row.CodeNormalized]; ok {
			ids[row.CodeNormalized] = uint64(row.ID)
		}
	}
	return ids, nil
}

func (r *Repository) importLocalizationsBulk(
	ctx context.Context,
	workspaceID string,
	promos []ExportPromo,
	ids map[string]uint64,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, promo := range promos {
		if previewHasConflict(preview, "promo", promo.Code) && strategy == ImportConflictSkip {
			continue
		}
		id := ids[normalizeCode(promo.Code)]
		for locale, text := range promo.Localization {
			rows = append(rows, []any{
				workspaceID,
				id,
				locale,
				text.Title,
				text.Description,
			})
			result.Imported.Localizations++
		}
	}
	return r.execImportBulk(ctx, "promo_localization",
		[]string{"workspace_id", "promo_id", "locale", "title", "description"},
		rows,
		"(workspace_id, promo_id, locale)",
		"title = EXCLUDED.title, description = EXCLUDED.description, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importRewardsBulk(
	ctx context.Context,
	workspaceID string,
	promos []ExportPromo,
	ids map[string]uint64,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, promo := range promos {
		if previewHasConflict(preview, "promo", promo.Code) && strategy == ImportConflictSkip {
			continue
		}
		id := ids[normalizeCode(promo.Code)]
		for _, reward := range promo.Rewards {
			rows = append(rows, []any{
				workspaceID,
				id,
				reward.Key,
				defaultString(reward.Type, "quantity"),
				reward.Quantity,
				reward.Scale,
				nullString(reward.Unit),
			})
			result.Imported.Rewards++
		}
	}
	return r.execImportBulk(
		ctx,
		"promo_reward",
		[]string{"workspace_id", "promo_id", "reward_key", "reward_type", "quantity", "scale", "duration_unit"},
		rows,
		"(workspace_id, promo_id, reward_key)",
		"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, duration_unit = EXCLUDED.duration_unit, updated_at = now()",
		strategy,
	)
}

func (r *Repository) execImportBulk(
	ctx context.Context,
	table string,
	columns []string,
	rows [][]any,
	conflictTarget string,
	duplicateUpdate string,
	strategy string,
) error {
	if len(rows) == 0 {
		return nil
	}
	return importexport.ForEachBatch(
		len(rows),
		len(columns),
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			query, args := compileImportBulkUpsert(
				table,
				columns,
				rows[start:end],
				conflictTarget,
				duplicateUpdate,
				strategy,
			)
			_, err := r.executor.ExecContext(ctx, query, args...)
			return err
		},
	)
}

func compileImportBulkUpsert(
	table string,
	columns []string,
	rows [][]any,
	conflictTarget string,
	duplicateUpdate string,
	strategy string,
) (string, []any) {
	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(table)
	builder.WriteString(" (")
	builder.WriteString(strings.Join(columns, ", "))
	builder.WriteString(") VALUES ")
	args := make([]any, 0, len(rows)*len(columns))
	for rowIndex, row := range rows {
		if rowIndex > 0 {
			builder.WriteString(", ")
		}
		builder.WriteByte('(')
		for columnIndex := range columns {
			if columnIndex > 0 {
				builder.WriteString(", ")
			}
			builder.WriteByte('$')
			builder.WriteString(fmt.Sprint(len(args) + columnIndex + 1))
		}
		builder.WriteByte(')')
		args = append(args, row...)
	}
	switch strategy {
	case ImportConflictSkip:
		builder.WriteString(" ON CONFLICT ")
		builder.WriteString(conflictTarget)
		builder.WriteString(" DO NOTHING")
	case ImportConflictUpdate:
		builder.WriteString(" ON CONFLICT ")
		builder.WriteString(conflictTarget)
		builder.WriteString(" DO UPDATE SET ")
		builder.WriteString(duplicateUpdate)
	}
	return builder.String(), args
}

func validateExportPackage(workspaceID string, pkg ExportPackage) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if pkg.Format != ExportFormat || pkg.Service != "promo" {
		return fmt.Errorf("unsupported export package: %s/%s", pkg.Service, pkg.Format)
	}

	promoCodes := make(map[string]int, len(pkg.Promos))
	for promoIndex, promo := range pkg.Promos {
		prefix := fmt.Sprintf("promo import promos[%d]", promoIndex)
		code := normalizeCode(promo.Code)
		if code == "" {
			return fmt.Errorf("%s.code: code is required", prefix)
		}
		if previousIndex, exists := promoCodes[code]; exists {
			return fmt.Errorf("%s.code: duplicates promos[%d].code", prefix, previousIndex)
		}
		promoCodes[code] = promoIndex

		if len(promo.Payload) == 0 || !json.Valid(promo.Payload) {
			return fmt.Errorf("%s.payload: must be valid JSON", prefix)
		}
		if promo.MaxActivations > math.MaxInt64 {
			return fmt.Errorf("%s.max_activations: numeric value is out of database range", prefix)
		}
		if err := target.Validate(promo.Target); err != nil {
			return fmt.Errorf("%s.target: %w", prefix, err)
		}
		if promo.StartAt != nil && promo.EndAt != nil && !promo.StartAt.Before(*promo.EndAt) {
			return fmt.Errorf("%s.start_at: must be before end_at", prefix)
		}

		for locale, text := range promo.Localization {
			if strings.TrimSpace(locale) == "" {
				return fmt.Errorf("%s.localization: locale is required", prefix)
			}
			if strings.TrimSpace(text.Title) == "" {
				return fmt.Errorf("%s.localization.%s.title: title is required", prefix, locale)
			}
		}

		rewardKeys := make(map[string]int, len(promo.Rewards))
		for rewardIndex, reward := range promo.Rewards {
			if strings.TrimSpace(reward.Key) == "" {
				return fmt.Errorf("%s.rewards[%d].key: key is required", prefix, rewardIndex)
			}
			if previousIndex, exists := rewardKeys[reward.Key]; exists {
				return fmt.Errorf(
					"%s.rewards[%d].key: duplicates rewards[%d].key",
					prefix,
					rewardIndex,
					previousIndex,
				)
			}
			rewardKeys[reward.Key] = rewardIndex
			if err := validateExportReward(reward); err != nil {
				return fmt.Errorf("%s.rewards[%d]: %w", prefix, rewardIndex, err)
			}
		}
	}
	return nil
}

func validateExportReward(reward ExportReward) error {
	if reward.Quantity <= 0 {
		return fmt.Errorf("quantity must be positive")
	}
	if reward.Scale > math.MaxInt16 {
		return fmt.Errorf("scale is out of database range")
	}

	switch defaultString(reward.Type, "quantity") {
	case "quantity":
		if reward.Unit != nil {
			return fmt.Errorf("quantity reward must not have duration unit")
		}
	case "duration":
		if reward.Unit == nil || !validPromoDurationUnit(*reward.Unit) {
			return fmt.Errorf("duration reward requires a valid duration unit")
		}
	default:
		return fmt.Errorf("type must be quantity or duration")
	}

	return nil
}

func validPromoDurationUnit(value string) bool {
	switch value {
	case "second", "minute", "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func countPackage(pkg ExportPackage) ImportCounts {
	var counts ImportCounts
	counts.Promos = uint64(len(pkg.Promos))
	for _, promo := range pkg.Promos {
		counts.Localizations += uint64(len(promo.Localization))
		counts.Rewards += uint64(len(promo.Rewards))
	}
	return counts
}

func (r *Repository) importExistingPromoCodes(ctx context.Context, workspaceID string) (map[string]bool, error) {
	rows, err := r.q.ListImportPromoCodes(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(rows))
	for _, code := range rows {
		result[code] = true
	}
	return result, nil
}

func previewHasConflict(preview ImportPreview, kind, key string) bool {
	normalized := normalizeCode(key)
	for _, conflict := range preview.Conflicts {
		if conflict.Type == kind && normalizeCode(conflict.Key) == normalized {
			return true
		}
	}
	return false
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

func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}
