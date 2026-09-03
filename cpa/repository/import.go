package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	serviceerrors "github.com/elum2b/services/errors"
	importexport "github.com/elum2b/services/internal/utils/importexport"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
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

	return fmt.Sprintf(
		"cpa import offers[%d].%s: %v",
		e.OfferIndex,
		e.Field,
		e.Cause,
	)
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

func (r *Repository) PreviewImport(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
) (ImportPreview, error) {
	if err := validateExportPackage(workspaceID, pkg); err != nil {
		return ImportPreview{}, err
	}

	return r.previewImport(ctx, workspaceID, pkg)
}

func (r *Repository) previewImport(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
) (ImportPreview, error) {
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

func (r *Repository) Import(
	ctx context.Context,
	workspaceID string,
	req ImportRequest,
) (ImportResult, error) {
	return r.importWithJob(ctx, workspaceID, req, 0)
}

// ImportJob applies an archive-job import at most once after its transaction commits.
func (r *Repository) ImportJob(
	ctx context.Context, workspaceID string, jobID int64, req ImportRequest,
) (ImportResult, error) {
	return r.importWithJob(ctx, workspaceID, req, jobID)
}

func (r *Repository) importWithJob(
	ctx context.Context, workspaceID string, req ImportRequest, jobID int64,
) (ImportResult, error) {
	if err := validateExportPackage(workspaceID, req.Package); err != nil {
		return ImportResult{}, err
	}

	strategy := req.ConflictStrategy
	if strategy == "" {
		strategy = ImportConflictFail
	}

	if strategy != ImportConflictFail && strategy != ImportConflictSkip &&
		strategy != ImportConflictUpdate {
		return ImportResult{}, fmt.Errorf(
			"unsupported import conflict strategy: %s",
			strategy,
		)
	}

	result := ImportResult{}
	alreadyApplied := false

	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if err := txRepo.lockWorkspaceMutation(ctx, workspaceID); err != nil {
			return err
		}

		if jobID != 0 {
			applied, err := jobs.ClaimImportReceipt(
				ctx,
				txRepo.executor,
				jobID,
				"cpa",
				workspaceID,
			)
			if err != nil || !applied {
				alreadyApplied = !applied
				return err
			}
		}

		preview, err := txRepo.previewImport(ctx, workspaceID, req.Package)
		if err != nil {
			return err
		}

		if strategy == ImportConflictFail && len(preview.Conflicts) > 0 {
			return fmt.Errorf(
				"import conflicts found: %d",
				len(preview.Conflicts),
			)
		}

		return txRepo.importBulk(
			ctx,
			workspaceID,
			req.Package,
			strategy,
			preview,
			&result,
		)
	})
	if err != nil {
		return ImportResult{}, err
	}

	r.invalidateCPACache(workspaceID, exportOfferIDs(req.Package.Offers)...)

	if alreadyApplied {
		return ImportResult{}, nil
	}

	return result, nil
}

func (r *Repository) importBulk(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	if err := r.importOffersBulk(
		ctx,
		workspaceID,
		pkg.Offers,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}

	if strategy == ImportConflictUpdate {
		if err := r.replaceImportedOfferChildren(
			ctx,
			workspaceID,
			preview,
		); err != nil {
			return err
		}
	}

	if err := r.importLocalizationsBulk(
		ctx,
		workspaceID,
		pkg.Offers,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}

	if err := r.importRewardsBulk(
		ctx,
		workspaceID,
		pkg.Offers,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}

	return r.importRuntime(ctx, workspaceID, pkg, strategy, preview)
}

func (r *Repository) replaceImportedOfferChildren(
	ctx context.Context,
	workspaceID string,
	preview ImportPreview,
) error {
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
			query, args := compileImportChildrenDelete(
				"cpa_localization",
				workspaceID,
				offerIDs[start:end],
			)
			if _, err := r.executor.ExecContext(
				ctx,
				query,
				args...); err != nil {
				return err
			}

			query, args = compileImportChildrenDelete(
				"cpa_reward",
				workspaceID,
				offerIDs[start:end],
			)

			_, err := r.executor.ExecContext(ctx, query, args...)

			return err
		},
	)
}

func compileImportChildrenDelete(
	table, workspaceID string,
	offerIDs []string,
) (string, []any) {
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

		fmt.Fprintf(&builder, "$%d", index+2)

		args = append(args, offerID)
	}

	builder.WriteByte(')')

	return builder.String(), args
}

func (r *Repository) importOffersBulk(
	ctx context.Context,
	workspaceID string,
	offers []ExportOffer,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(offers))
	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) &&
			strategy == ImportConflictSkip {
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
			"workspace_id",
			"id",
			"payload",
			"target",
			"code_mode",
			"code_source",
			"shared_code",
			"generated_length",
			"generated_alphabet",
			"is_active",
			"start_at",
			"end_at",
		},
		rows,
		"payload = EXCLUDED.payload, target = EXCLUDED.target, code_mode = EXCLUDED.code_mode, "+
			"code_source = EXCLUDED.code_source, shared_code = EXCLUDED.shared_code, generated_length = EXCLUDED.generated_length, "+
			"generated_alphabet = EXCLUDED.generated_alphabet, is_active = EXCLUDED.is_active, start_at = EXCLUDED.start_at, "+
			"end_at = EXCLUDED.end_at, updated_at = now()",
	)
}

func (r *Repository) importLocalizationsBulk(
	ctx context.Context,
	workspaceID string,
	offers []ExportOffer,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)

	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) &&
			strategy == ImportConflictSkip {
			continue
		}

		for locale, text := range offer.Localization {
			rows = append(
				rows,
				[]any{
					workspaceID,
					offer.ID,
					locale,
					text.Title,
					text.Description,
				},
			)
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

func (r *Repository) importRewardsBulk(
	ctx context.Context,
	workspaceID string,
	offers []ExportOffer,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)

	for _, offer := range offers {
		if previewHasConflict(preview, "offer", offer.ID) &&
			strategy == ImportConflictSkip {
			continue
		}

		for _, reward := range offer.Rewards {
			rows = append(rows, []any{
				workspaceID,
				offer.ID,
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
		"cpa_reward",
		[]string{
			"workspace_id",
			"cpa_id",
			"reward_key",
			"reward_type",
			"quantity",
			"scale",
			"duration_unit",
		},
		rows,
		"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, "+
			"duration_unit = EXCLUDED.duration_unit, updated_at = now()",
	)
}

func (r *Repository) execImportBulk(
	ctx context.Context,
	table string,
	columns []string,
	rows [][]any,
	duplicateUpdate string,
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
				duplicateUpdate,
			)
			if _, err := r.executor.ExecContext(
				ctx,
				query,
				args...); err != nil {
				return err
			}

			return nil
		},
	)
}

func compileImportBulkUpsert(
	table string,
	columns []string,
	rows [][]any,
	duplicateUpdate string,
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
			fmt.Fprint(&builder, len(args)+columnIndex+1)
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
		if err := ValidateOffer(
			exportOfferParams(workspaceID, offer),
		); err != nil {
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

	return validateRuntimePackage(workspaceID, pkg, offerIndexes)
}

func validateRuntimePackage(
	workspaceID string,
	pkg ExportPackage,
	offers map[string]int,
) error {
	codes := make(map[string]ExportCode, len(pkg.Codes))
	for index, code := range pkg.Codes {
		offerIndex, ok := offers[code.CPAID]
		if !ok || code.Code == "" ||
			(code.Source != CodeSourcePool && code.Source != CodeSourceGenerated) ||
			(code.Status != "available" && code.Status != "issued" && code.Status != "completed" && code.Status != "deleted") ||
			code.CreatedAt.IsZero() || code.UpdatedAt.IsZero() ||
			code.UpdatedAt.Before(code.CreatedAt) ||
			(code.Status == "deleted") != (code.DeletedAt != nil) ||
			(code.DeletedAt != nil && code.DeletedAt.Before(code.CreatedAt)) {
			return fmt.Errorf(
				"cpa import codes[%d]: invalid code record",
				index,
			)
		}

		offer := pkg.Offers[offerIndex]
		if offer.CodeMode != CodeModePersonal || offer.CodeSource == nil ||
			code.Source != *offer.CodeSource {
			return fmt.Errorf(
				"cpa import codes[%d]: incompatible offer code configuration",
				index,
			)
		}

		key := code.CPAID + "\x00" + code.Code
		if _, exists := codes[key]; exists {
			return fmt.Errorf("cpa import codes[%d]: duplicate code", index)
		}

		codes[key] = code
	}

	identities := make(map[string]bool, len(pkg.Assignments))
	codeAssignments := make(map[string]ExportAssignment, len(pkg.Assignments))

	for index, assignment := range pkg.Assignments {
		offerIndex, ok := offers[assignment.CPAID]
		if !ok {
			return fmt.Errorf(
				"cpa import assignments[%d]: unknown offer",
				index,
			)
		}

		if err := requireUserScope(
			UserScope{
				WorkspaceID:    workspaceID,
				CPAID:          assignment.CPAID,
				AppID:          assignment.AppID,
				PlatformID:     assignment.PlatformID,
				PlatformUserID: assignment.PlatformUserID,
			},
			true,
		); err != nil {
			return importValidationError(
				offerIndex,
				fmt.Sprintf("assignments[%d]", index),
				err,
			)
		}

		key := fmt.Sprintf(
			"%s\x00%d\x00%d\x00%s",
			assignment.CPAID,
			assignment.AppID,
			assignment.PlatformID,
			assignment.PlatformUserID,
		)
		if identities[key] {
			return fmt.Errorf(
				"cpa import assignments[%d]: duplicate identity",
				index,
			)
		}

		identities[key] = true

		if assignment.Code == "" ||
			(assignment.CodeMode != CodeModeShared && assignment.CodeMode != CodeModePersonal) ||
			(assignment.Status != "issued" && assignment.Status != "completed") ||
			assignment.IssuedAt.IsZero() {
			return fmt.Errorf(
				"cpa import assignments[%d]: invalid assignment",
				index,
			)
		}

		if err := validateRewardsSnapshot(
			workspaceID,
			assignment.CPAID,
			assignment.RewardsSnapshot,
		); err != nil {
			return importValidationError(
				offerIndex,
				fmt.Sprintf("assignments[%d].rewards_snapshot", index),
				err,
			)
		}

		offer := pkg.Offers[offerIndex]
		if assignment.CodeMode != offer.CodeMode {
			return fmt.Errorf(
				"cpa import assignments[%d]: code mode does not match offer",
				index,
			)
		}

		if assignment.CodeMode == CodeModeShared {
			if assignment.CodeRef != nil || offer.SharedCode == nil ||
				assignment.Code != *offer.SharedCode {
				return fmt.Errorf(
					"cpa import assignments[%d]: invalid shared code linkage",
					index,
				)
			}
		} else {
			if assignment.CodeRef == nil || *assignment.CodeRef == "" ||
				assignment.Code != *assignment.CodeRef {
				return fmt.Errorf(
					"cpa import assignments[%d]: invalid personal code linkage",
					index,
				)
			}

			codeKey := assignment.CPAID + "\x00" + *assignment.CodeRef

			code, exists := codes[codeKey]

			if !exists || code.Source != *offer.CodeSource {
				return fmt.Errorf(
					"cpa import assignments[%d]: code reference missing or incompatible",
					index,
				)
			}

			if _, exists := codeAssignments[codeKey]; exists {
				return fmt.Errorf(
					"cpa import assignments[%d]: code reference is reused",
					index,
				)
			}

			codeAssignments[codeKey] = assignment
		}

		events := map[string]time.Time{}

		for eventIndex, event := range assignment.Events {
			if (event.EventType != "issued" && event.EventType != "completed") ||
				event.OccurredAt.IsZero() {
				return fmt.Errorf(
					"cpa import assignments[%d].events[%d]: invalid event",
					index,
					eventIndex,
				)
			}

			if _, exists := events[event.EventType]; exists {
				return fmt.Errorf(
					"cpa import assignments[%d].events[%d]: invalid event",
					index,
					eventIndex,
				)
			}

			events[event.EventType] = event.OccurredAt
		}

		issuedAt, issued := events["issued"]
		completedAt, completed := events["completed"]

		if !issued || issuedAt.Before(assignment.IssuedAt) ||
			(assignment.Status == "issued" && (assignment.CompletedAt != nil || completed)) ||
			(assignment.Status == "completed" &&
				(assignment.CompletedAt == nil || !completed || assignment.CompletedAt.Before(assignment.IssuedAt) ||
					completedAt.Before(issuedAt) || completedAt.Before(*assignment.CompletedAt))) {
			return fmt.Errorf(
				"cpa import assignments[%d]: events do not preserve state",
				index,
			)
		}
	}

	for key, code := range codes {
		assignment, assigned := codeAssignments[key]
		if code.Status == "available" && assigned {
			return fmt.Errorf(
				"cpa import code %q: available code is assigned",
				code.Code,
			)
		}

		if (code.Status == "issued" || code.Status == "completed") &&
			(!assigned || code.Status != assignment.Status) {
			return fmt.Errorf(
				"cpa import code %q: status does not match assignment",
				code.Code,
			)
		}
	}

	return nil
}

func importValidationError(
	offerIndex int,
	prefix string,
	cause error,
) *ImportValidationError {
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

func exportOfferParams(
	workspaceID string,
	offer ExportOffer,
) UpsertOfferParams {
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

func (r *Repository) importExistingOfferKeys(
	ctx context.Context,
	workspaceID string,
) (map[string]bool, error) {
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

func validateRewardsSnapshot(
	workspaceID, cpaID string,
	raw json.RawMessage,
) error {
	var rewards []callbackReward

	if err := json.Unmarshal(raw, &rewards); err != nil || rewards == nil {
		return fmt.Errorf("invalid reward snapshot")
	}

	for _, reward := range rewards {
		if err := ValidateReward(Reward{
			WorkspaceID: workspaceID,
			CPAID:       cpaID,
			Key:         reward.Key,
			Type:        reward.Type,
			Quantity:    reward.Quantity,
			Scale:       reward.Scale,
			Unit:        reward.Unit,
		}); err != nil {
			return err
		}
	}

	return nil
}

// importRuntime never touches the callback outbox. Runtime rows are keyed by
// stable business values and target serial IDs are resolved inside this transaction.
func (r *Repository) importRuntime(
	ctx context.Context,
	workspaceID string,
	pkg ExportPackage,
	strategy string,
	preview ImportPreview,
) error {
	include := func(cpaID string) bool {
		return strategy != ImportConflictSkip ||
			!previewHasConflict(preview, "offer", cpaID)
	}
	identities := make(map[string]bool, len(pkg.Assignments))

	for _, assignment := range pkg.Assignments {
		if include(assignment.CPAID) {
			identities[fmt.Sprintf("%s\x00%d\x00%d\x00%s", assignment.CPAID, assignment.AppID, assignment.PlatformID, assignment.PlatformUserID)] = true
		}
	}

	for _, code := range pkg.Codes {
		if !include(code.CPAID) {
			continue
		}

		var (
			appID, platformID int64
			platformUserID    string
		)

		err := r.executor.QueryRowContext(ctx, `SELECT a.app_id, a.platform_id, a.platform_user_id FROM cpa_assignment a JOIN cpa_code c ON c.id = a.code_id WHERE c.workspace_id=$1 AND c.cpa_id=$2 AND c.code=$3`, workspaceID, code.CPAID, code.Code).
			Scan(&appID, &platformID, &platformUserID)

		if isNoRows(err) {
			continue
		}

		if err != nil {
			return err
		}

		key := fmt.Sprintf(
			"%s\x00%d\x00%d\x00%s",
			code.CPAID,
			appID,
			platformID,
			platformUserID,
		)
		if !identities[key] {
			return fmt.Errorf(
				"cpa import cannot replace code %q for offer %q: target assignment belongs to a different identity",
				code.Code,
				code.CPAID,
			)
		}
	}

	if strategy == ImportConflictUpdate {
		if err := r.replaceImportedOfferRuntime(
			ctx,
			workspaceID,
			preview,
		); err != nil {
			return err
		}
	}

	for _, code := range pkg.Codes {
		if include(code.CPAID) {
			if _, err := r.executor.ExecContext(
				ctx,
				`INSERT INTO cpa_code (workspace_id,cpa_id,code,source,status,created_at,updated_at,deleted_at) VALUES ($1,$2,$3,$4::cpa_code_source,$5::cpa_code_status,$6,$7,$8) ON CONFLICT (workspace_id,cpa_id,code) DO UPDATE SET source=EXCLUDED.source,status=EXCLUDED.status,created_at=EXCLUDED.created_at,updated_at=EXCLUDED.updated_at,deleted_at=EXCLUDED.deleted_at`,
				workspaceID,
				code.CPAID,
				code.Code,
				code.Source,
				code.Status,
				code.CreatedAt,
				code.UpdatedAt,
				nullTime(code.DeletedAt),
			); err != nil {
				return err
			}
		}
	}

	for _, assignment := range pkg.Assignments {
		if !include(assignment.CPAID) {
			continue
		}

		var (
			id     int64
			codeID sql.NullInt64
		)

		if assignment.CodeRef != nil {
			if err := r.executor.QueryRowContext(ctx, `SELECT id FROM cpa_code WHERE workspace_id=$1 AND cpa_id=$2 AND code=$3`, workspaceID, assignment.CPAID, *assignment.CodeRef).
				Scan(&codeID); err != nil {
				return err
			}
		}

		if err := r.executor.QueryRowContext(ctx, `INSERT INTO cpa_assignment (workspace_id,cpa_id,app_id,platform_id,platform_user_id,code_id,code,code_mode,rewards_snapshot,status,issued_at,completed_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8::cpa_code_mode,$9,$10::cpa_assignment_status,$11,$12,now()) RETURNING id`, workspaceID, assignment.CPAID, assignment.AppID, assignment.PlatformID, assignment.PlatformUserID, codeID, assignment.Code, assignment.CodeMode, assignment.RewardsSnapshot, assignment.Status, assignment.IssuedAt, nullTime(assignment.CompletedAt)).
			Scan(&id); err != nil {
			return err
		}

		for _, event := range assignment.Events {
			if _, err := r.executor.ExecContext(
				ctx,
				`INSERT INTO cpa_assignment_event (workspace_id,cpa_id,assignment_id,event_type,occurred_at) VALUES ($1,$2,$3,$4::cpa_assignment_event_type,$5)`,
				workspaceID,
				assignment.CPAID,
				id,
				event.EventType,
				event.OccurredAt,
			); err != nil {
				return err
			}
		}
	}

	if _, err := r.executor.ExecContext(
		ctx,
		`DELETE FROM cpa_stats_daily WHERE workspace_id=$1`,
		workspaceID,
	); err != nil {
		return err
	}

	_, err := r.executor.ExecContext(
		ctx,
		`INSERT INTO cpa_stats_daily (workspace_id,cpa_id,stats_date,issued_count,completed_count,unique_users) SELECT workspace_id,cpa_id,(occurred_at AT TIME ZONE 'UTC')::date,SUM((event_type='issued')::int)::bigint,SUM((event_type='completed')::int)::bigint,COUNT(DISTINCT assignment_id)::bigint FROM cpa_assignment_event WHERE workspace_id=$1 GROUP BY workspace_id,cpa_id,(occurred_at AT TIME ZONE 'UTC')::date`,
		workspaceID,
	)

	return err
}

// replaceImportedOfferRuntime makes update_existing an authoritative runtime
// snapshot, after collision checks have protected foreign user assignments.
func (r *Repository) replaceImportedOfferRuntime(
	ctx context.Context,
	workspaceID string,
	preview ImportPreview,
) error {
	offerIDs := make([]string, 0, len(preview.Conflicts))
	for _, conflict := range preview.Conflicts {
		if conflict.Type == "offer" {
			offerIDs = append(offerIDs, conflict.Key)
		}
	}

	return importexport.ForEachBatch(
		len(offerIDs),
		1,
		importexport.DefaultBatchLimits,
		func(start, end int) error {
			for _, table := range []string{
				"cpa_assignment_event",
				"cpa_assignment",
				"cpa_code",
			} {
				query, args := compileImportChildrenDelete(
					table,
					workspaceID,
					offerIDs[start:end],
				)
				if _, err := r.executor.ExecContext(
					ctx,
					query,
					args...); err != nil {
					return err
				}
			}

			return nil
		},
	)
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
