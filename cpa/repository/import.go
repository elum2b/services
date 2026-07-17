package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
	importexport "github.com/elum2b/services/internal/utils/importexport"
)

type ImportValidationError struct {
	OfferIndex int    `json:"offer_index"`
	Field      string `json:"field"`
	Cause      error
}

func (e *ImportValidationError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("cpa import offers[%d].%s: %v", e.OfferIndex, e.Field, e.Cause)
}

func (e *ImportValidationError) Code() string {
	return serviceerrors.CodeInvalidFields
}

func (e *ImportValidationError) Message() string {
	if e == nil {
		return ""
	}
	return e.Error()
}

func (e *ImportValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (r *Repository) PreviewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	if err := validateExportPackage(workspaceID, pkg); err != nil {
		return ImportPreview{}, err
	}
	return r.previewImport(ctx, workspaceID, pkg)
}

func (r *Repository) previewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	preview := ImportPreview{
		Format:  pkg.Format,
		Service: pkg.Service,
		Counts:  countPackage(pkg),
	}
	existing, err := r.importExistingOfferKeys(ctx, workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	for _, offer := range pkg.Offers {
		if existing[offer.ID] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{
				Type: "offer",
				Key:  offer.ID,
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

		preview, err := txRepo.previewImport(ctx, workspaceID, req.Package)
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
	r.invalidateCPACache(workspaceID, exportOfferIDs(req.Package.Offers)...)
	return result, nil
}

func (r *Repository) importBulk(ctx context.Context, workspaceID string, pkg ExportPackage, strategy string, preview ImportPreview, result *ImportResult) error {
	if err := r.importOffersBulk(ctx, workspaceID, pkg.Offers, strategy, preview, result); err != nil {
		return err
	}
	if strategy == ImportConflictUpdate {
		if err := r.replaceImportedOfferChildren(ctx, workspaceID, preview); err != nil {
			return err
		}
	}
	if err := r.importLocalizationsBulk(ctx, workspaceID, pkg.Offers, strategy, preview, result); err != nil {
		return err
	}
	return r.importRewardsBulk(ctx, workspaceID, pkg.Offers, strategy, preview, result)
}

func (r *Repository) replaceImportedOfferChildren(ctx context.Context, workspaceID string, preview ImportPreview) error {
	offerIDs := make([]string, 0, len(preview.Conflicts))
	for _, conflict := range preview.Conflicts {
		if conflict.Type == "offer" {
			offerIDs = append(offerIDs, conflict.Key)
		}
	}
	if len(offerIDs) == 0 {
		return nil
	}

	return importexport.ForEachBatch(
		len(offerIDs),
		1,
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			query, args := compileImportChildrenDelete("cpa_localization", workspaceID, offerIDs[start:end])
			if _, err := r.executor.ExecContext(ctx, query, args...); err != nil {
				return err
			}

			query, args = compileImportChildrenDelete("cpa_reward", workspaceID, offerIDs[start:end])
			_, err := r.executor.ExecContext(ctx, query, args...)
			return err
		},
	)
}

func compileImportChildrenDelete(table, workspaceID string, offerIDs []string) (string, []any) {
	var builder strings.Builder
	builder.WriteString("DELETE FROM ")
	builder.WriteString(table)
	builder.WriteString(" WHERE workspace_id = $1 AND cpa_id IN (")

	args := make([]any, 0, len(offerIDs)+1)
	args = append(args, workspaceID)
	for index, offerID := range offerIDs {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(fmt.Sprintf("$%d", index+2))
		args = append(args, offerID)
	}
	builder.WriteByte(')')
	return builder.String(), args
}

func (r *Repository) importOffersBulk(ctx context.Context, workspaceID string, offers []ExportOffer, strategy string, preview ImportPreview, result *ImportResult) error {
	rows := make([][]any, 0, len(offers))
	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) && strategy == ImportConflictSkip {
			result.Skipped.Offers++
			continue
		}
		params := exportOfferParams(workspaceID, offer)
		NormalizeOffer(&params)
		rows = append(rows, []any{
			params.WorkspaceID,
			params.ID,
			defaultJSON(params.Payload, "{}"),
			defaultJSON(params.Target, "null"),
			params.CodeMode,
			nullCodeSourceString(params.CodeSource),
			nullString(params.SharedCode),
			nullInt16(params.GeneratedLength),
			nullString(params.GeneratedAlphabet),
			params.IsActive,
			nullTime(params.StartAt),
			nullTime(params.EndAt),
		})
		result.Imported.Offers++
	}
	return r.execImportBulk(
		ctx,
		"cpa_offer",
		[]string{
			"workspace_id", "id", "payload", "target", "code_mode", "code_source", "shared_code",
			"generated_length", "generated_alphabet", "is_active", "start_at", "end_at",
		},
		rows,
		"payload = EXCLUDED.payload, target = EXCLUDED.target, code_mode = EXCLUDED.code_mode, "+
			"code_source = EXCLUDED.code_source, shared_code = EXCLUDED.shared_code, generated_length = EXCLUDED.generated_length, "+
			"generated_alphabet = EXCLUDED.generated_alphabet, is_active = EXCLUDED.is_active, start_at = EXCLUDED.start_at, "+
			"end_at = EXCLUDED.end_at, updated_at = now()",
	)
}

func (r *Repository) importLocalizationsBulk(ctx context.Context, workspaceID string, offers []ExportOffer, strategy string, preview ImportPreview, result *ImportResult) error {
	rows := make([][]any, 0)
	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) && strategy == ImportConflictSkip {
			continue
		}
		for locale, text := range offer.Localization {
			rows = append(rows, []any{workspaceID, offer.ID, locale, text.Title, text.Description})
			result.Imported.Localizations++
		}
	}
	return r.execImportBulk(
		ctx,
		"cpa_localization",
		[]string{"workspace_id", "cpa_id", "locale", "title", "description"},
		rows,
		"title = EXCLUDED.title, description = EXCLUDED.description, updated_at = now()",
	)
}

func (r *Repository) importRewardsBulk(ctx context.Context, workspaceID string, offers []ExportOffer, strategy string, preview ImportPreview, result *ImportResult) error {
	rows := make([][]any, 0)
	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) && strategy == ImportConflictSkip {
			continue
		}
		for _, reward := range offer.Rewards {
			rows = append(rows, []any{
				workspaceID, offer.ID, reward.Key, defaultString(reward.Type, "quantity"),
				reward.Quantity, reward.Scale, nullString(reward.Unit),
			})
			result.Imported.Rewards++
		}
	}
	return r.execImportBulk(
		ctx,
		"cpa_reward",
		[]string{"workspace_id", "cpa_id", "reward_key", "reward_type", "quantity", "scale", "duration_unit"},
		rows,
		"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, "+
			"duration_unit = EXCLUDED.duration_unit, updated_at = now()",
	)
}

func (r *Repository) execImportBulk(ctx context.Context, table string, columns []string, rows [][]any, duplicateUpdate string) error {
	if len(rows) == 0 {
		return nil
	}
	return importexport.ForEachBatch(
		len(rows),
		len(columns),
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			query, args := compileImportBulkUpsert(table, columns, rows[start:end], duplicateUpdate)
			if _, err := r.executor.ExecContext(ctx, query, args...); err != nil {
				return err
			}
			return nil
		},
	)
}

func compileImportBulkUpsert(table string, columns []string, rows [][]any, duplicateUpdate string) (string, []any) {
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
	if duplicateUpdate != "" {
		builder.WriteString(" ON CONFLICT ")
		builder.WriteString(importConflictTarget(table))
		builder.WriteString(" DO UPDATE SET ")
		builder.WriteString(duplicateUpdate)
	}
	return builder.String(), args
}

func importConflictTarget(table string) string {
	switch table {
	case "cpa_offer":
		return "(workspace_id, id)"
	case "cpa_localization":
		return "(workspace_id, cpa_id, locale)"
	case "cpa_reward":
		return "(workspace_id, cpa_id, reward_key)"
	default:
		return ""
	}
}

func validateExportPackage(workspaceID string, pkg ExportPackage) error {
	if err := requireWorkspace(workspaceID); err != nil {
		return err
	}
	if pkg.Format != ExportFormat {
		return fmt.Errorf("unsupported export format: %s", pkg.Format)
	}
	if pkg.Service != "cpa" {
		return fmt.Errorf("unsupported export service: %s", pkg.Service)
	}
	offerIndexes := make(map[string]int, len(pkg.Offers))
	for offerIndex, offer := range pkg.Offers {
		if err := ValidateOffer(exportOfferParams(workspaceID, offer)); err != nil {
			return importValidationError(offerIndex, "", err)
		}
		if previousIndex, exists := offerIndexes[offer.ID]; exists {
			return importValidationError(
				offerIndex,
				"id",
				fmt.Errorf("duplicates offers[%d].id", previousIndex),
			)
		}
		offerIndexes[offer.ID] = offerIndex

		for locale, text := range offer.Localization {
			err := ValidateLocalization(Localization{
				WorkspaceID: workspaceID,
				CPAID:       offer.ID,
				Locale:      locale,
				Title:       text.Title,
				Description: text.Description,
			})
			if err != nil {
				return importValidationError(
					offerIndex,
					fmt.Sprintf("localizations.%s", locale),
					err,
				)
			}
		}

		rewardIndexes := make(map[string]int, len(offer.Rewards))
		for rewardIndex, reward := range offer.Rewards {
			if previousIndex, exists := rewardIndexes[reward.Key]; exists {
				return importValidationError(
					offerIndex,
					fmt.Sprintf("rewards[%d].key", rewardIndex),
					fmt.Errorf("duplicates rewards[%d].key", previousIndex),
				)
			}
			rewardIndexes[reward.Key] = rewardIndex
			err := ValidateReward(Reward{
				WorkspaceID: workspaceID,
				CPAID:       offer.ID,
				Key:         reward.Key,
				Type:        reward.Type,
				Quantity:    reward.Quantity,
				Scale:       reward.Scale,
				Unit:        reward.Unit,
			})
			if err != nil {
				return importValidationError(
					offerIndex,
					fmt.Sprintf("rewards[%d]", rewardIndex),
					err,
				)
			}
		}
	}
	return nil
}

func importValidationError(offerIndex int, prefix string, cause error) *ImportValidationError {
	field := prefix
	var validationErr *FieldValidationError
	if errors.As(cause, &validationErr) {
		if field != "" {
			field += "."
		}
		field += validationErr.Field
	}
	return &ImportValidationError{
		OfferIndex: offerIndex,
		Field:      field,
		Cause:      cause,
	}
}

func exportOfferParams(workspaceID string, offer ExportOffer) UpsertOfferParams {
	return UpsertOfferParams{
		WorkspaceID:       workspaceID,
		ID:                offer.ID,
		Payload:           offer.Payload,
		Target:            offer.Target,
		CodeMode:          offer.CodeMode,
		CodeSource:        offer.CodeSource,
		SharedCode:        offer.SharedCode,
		GeneratedLength:   offer.GeneratedLength,
		GeneratedAlphabet: offer.GeneratedAlphabet,
		IsActive:          offer.IsActive,
		StartAt:           offer.StartAt,
		EndAt:             offer.EndAt,
	}
}

func countPackage(pkg ExportPackage) ImportCounts {
	var counts ImportCounts
	counts.Offers = uint64(len(pkg.Offers))
	for _, offer := range pkg.Offers {
		counts.Localizations += uint64(len(offer.Localization))
		counts.Rewards += uint64(len(offer.Rewards))
	}
	return counts
}

func (r *Repository) importExistingOfferKeys(ctx context.Context, workspaceID string) (map[string]bool, error) {
	ids, err := r.q.AdminListOfferIDs(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}

func exportOfferIDs(offers []ExportOffer) []string {
	ids := make([]string, 0, len(offers))
	for _, offer := range offers {
		if offer.ID != "" {
			ids = append(ids, offer.ID)
		}
	}
	return ids
}

func previewHasConflict(preview ImportPreview, kind, key string) bool {
	for _, conflict := range preview.Conflicts {
		if conflict.Type == kind && conflict.Key == key {
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

func nullCodeSourceString(value *string) sql.NullString {
	return nullString(value)
}

func nullInt16(value *int16) sql.NullInt16 {
	if value == nil {
		return sql.NullInt16{}
	}
	return sql.NullInt16{Int16: *value, Valid: true}
}

func nullTime(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}
