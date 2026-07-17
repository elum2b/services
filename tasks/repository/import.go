package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	json "github.com/goccy/go-json"

	importexport "github.com/elum2b/services/internal/utils/importexport"
	"github.com/elum2b/services/internal/utils/target"
	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func (r *Repository) PreviewImport(ctx context.Context, workspaceID string, pkg ExportPackage) (ImportPreview, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return ImportPreview{}, err
	}

	if err := validateExportPackage(pkg); err != nil {
		return ImportPreview{}, err
	}
	preview := ImportPreview{Format: pkg.Format, Service: pkg.Service}
	preview.Counts = countPackage(pkg)
	existing, err := r.importExistingKeys(ctx, workspaceID)
	if err != nil {
		return ImportPreview{}, err
	}
	seenSequences := make(map[string]struct{})
	for _, sequence := range pkg.Sequences {
		seenSequences[sequence.Key] = struct{}{}
		if existing.sequences[sequence.Key] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "sequence", Key: sequence.Key})
		}
	}
	for _, group := range pkg.Groups {
		if existing.groups[group.Key] {
			preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "group", Key: group.Key})
		}
		for _, config := range group.PartnerConfigs {
			for _, secret := range []*ExportSecret{config.Secret, config.WebhookSecret} {
				if secret != nil && !secretHasEmbeddedValue(secret) {
					preview.RequiredSecrets = append(preview.RequiredSecrets, *secret)
				}
			}
			key := partnerConfigImportKey(config.Provider, group.Key, config.Platform)
			if existing.partnerConfigs[key] {
				preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "partner_config", Key: key})
			}
		}
		for _, rule := range group.PartnerRewardRules {
			key := partnerRewardImportKey(rule.Provider, group.Key, rule.ExternalType, rule.Reward.Key)
			if existing.partnerRewards[key] {
				preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "partner_reward_rule", Key: key})
			}
		}
		positions := make(map[string]map[uint32]string)
		for _, task := range group.Tasks {
			if existing.tasks[task.Key] {
				preview.Conflicts = append(preview.Conflicts, ImportConflict{Type: "task", Key: task.Key})
			}
			if task.SequenceKey == nil {
				continue
			}
			if _, ok := seenSequences[*task.SequenceKey]; !ok && !existing.sequences[*task.SequenceKey] {
				preview.Warnings = append(
					preview.Warnings,
					"sequence is referenced but not present: "+*task.SequenceKey,
				)
			}
			if task.SequencePosition != nil {
				if positions[*task.SequenceKey] == nil {
					positions[*task.SequenceKey] = make(map[uint32]string)
				}
				if prev := positions[*task.SequenceKey][*task.SequencePosition]; prev != "" {
					preview.Warnings = append(
						preview.Warnings,
						fmt.Sprintf(
							"duplicate sequence position %s:%d used by %s and %s",
							*task.SequenceKey,
							*task.SequencePosition,
							prev,
							task.Key,
						),
					)
				}
				positions[*task.SequenceKey][*task.SequencePosition] = task.Key
			}
		}
	}
	return preview, nil
}

func (r *Repository) Import(ctx context.Context, workspaceID string, req ImportRequest) (ImportResult, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
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
	if err := requireImportSecrets(req.Package, req.Secrets); err != nil {
		return ImportResult{}, err
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

		return txRepo.importBulk(ctx, workspaceID, req, strategy, preview, &result)
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, r.invalidateTaskCache(ctx, workspaceID)
}

func (r *Repository) importBulk(
	ctx context.Context,
	workspaceID string,
	req ImportRequest,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	if err := r.importSequencesBulk(
		ctx,
		workspaceID,
		req.Package.Sequences,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}
	if err := r.replaceImportedGroupChildren(
		ctx,
		workspaceID,
		req.Package.Groups,
		strategy,
		preview,
	); err != nil {
		return err
	}
	if err := r.importGroupsBulk(
		ctx,
		workspaceID,
		req.Package.Groups,
		strategy,
		preview,
		result,
	); err != nil {
		return err
	}
	taskIDs, err := r.importTasksBulk(
		ctx,
		workspaceID,
		req.Package.Groups,
		strategy,
		preview,
		result,
	)
	if err != nil {
		return err
	}
	if err := r.replaceImportedTaskChildren(
		ctx,
		workspaceID,
		req.Package.Groups,
		taskIDs,
		strategy,
		preview,
	); err != nil {
		return err
	}
	if err := r.importTaskLocalizationsBulk(ctx, workspaceID, req.Package.Groups, taskIDs, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importRewardsBulk(ctx, workspaceID, req.Package.Groups, taskIDs, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importComplexConditionsBulk(ctx, workspaceID, req.Package.Groups, taskIDs, strategy, preview, result); err != nil {
		return err
	}
	if err := r.importPartnerConfigsBulk(ctx, workspaceID, req.Package.Groups, strategy, req.Secrets, preview, result); err != nil {
		return err
	}
	return r.importPartnerRewardRulesBulk(ctx, workspaceID, req.Package.Groups, strategy, preview, result)
}

func (r *Repository) replaceImportedGroupChildren(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	strategy string,
	preview ImportPreview,
) error {
	if strategy != ImportConflictUpdate {
		return nil
	}

	groupKeys := make([]string, 0, len(groups))
	for _, group := range groups {
		if previewHasConflict(preview, "group", group.Key) {
			groupKeys = append(groupKeys, group.Key)
		}
	}
	if len(groupKeys) == 0 {
		return nil
	}

	for _, table := range []string{
		"task_group_localization",
		"task_partner_config",
		"task_partner_reward_rule",
	} {
		if _, err := r.executor.ExecContext(
			ctx,
			"DELETE FROM "+table+" WHERE workspace_id = $1 AND group_key = ANY($2::text[])",
			workspaceID,
			groupKeys,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) replaceImportedTaskChildren(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	taskIDs map[string]uint64,
	strategy string,
	preview ImportPreview,
) error {
	if strategy != ImportConflictUpdate {
		return nil
	}

	taskIDValues := make([]int64, 0)
	for _, group := range groups {
		for _, task := range group.Tasks {
			if previewHasConflict(preview, "task", task.Key) {
				taskIDValues = append(taskIDValues, int64(taskIDs[task.Key]))
			}
		}
	}

	if len(taskIDValues) == 0 {
		return nil
	}
	for _, spec := range []struct {
		table  string
		column string
	}{
		{table: "task_complex_condition", column: "parent_task_id"},
		{table: "task_localization", column: "task_id"},
		{table: "task_reward", column: "task_id"},
	} {
		if _, err := r.executor.ExecContext(
			ctx,
			"DELETE FROM "+spec.table+" WHERE workspace_id = $1 AND "+spec.column+" = ANY($2::bigint[])",
			workspaceID,
			taskIDValues,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) importSequencesBulk(
	ctx context.Context,
	workspaceID string,
	sequences []ExportSequence,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0, len(sequences))
	for _, sequence := range sequences {
		exists := previewHasConflict(preview, "sequence", sequence.Key)
		if exists && strategy == ImportConflictSkip {
			result.Skipped.Sequences++
			continue
		}
		rows = append(rows, []any{workspaceID, sequence.Key, sequence.Position, sequence.IsActive})
		result.Imported.Sequences++
	}
	return r.execImportBulk(
		ctx,
		"task_sequence",
		[]string{"workspace_id", "key", "position", "is_active"},
		rows,
		"ON CONFLICT (workspace_id, key) DO UPDATE SET position = EXCLUDED.position, is_active = EXCLUDED.is_active, deleted_at = NULL, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importGroupsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	groupRows := make([][]any, 0, len(groups))
	localizationRows := make([][]any, 0, len(groups)*2)
	for _, group := range groups {
		exists := previewHasConflict(preview, "group", group.Key)
		if exists && strategy == ImportConflictSkip {
			result.Skipped.Groups++
			continue
		}
		groupRows = append(groupRows, []any{workspaceID, group.Key, group.Position, group.IsActive})
		result.Imported.Groups++
		for locale, text := range group.Localization {
			localizationRows = append(
				localizationRows,
				[]any{workspaceID, group.Key, locale, text.Title, text.Description},
			)
			result.Imported.GroupLocalizations++
		}
	}
	if err := r.execImportBulk(ctx, "task_group",
		[]string{"workspace_id", "key", "position", "is_active"},
		groupRows,
		"ON CONFLICT (workspace_id, key) DO UPDATE SET position = EXCLUDED.position, is_active = EXCLUDED.is_active, deleted_at = NULL, updated_at = now()",
		strategy,
	); err != nil {
		return err
	}
	return r.execImportBulk(
		ctx,
		"task_group_localization",
		[]string{"workspace_id", "group_key", "locale", "title", "description"},
		localizationRows,
		"ON CONFLICT (workspace_id, group_key, locale) DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importTasksBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) (map[string]uint64, error) {
	rows := make([][]any, 0)
	needed := make(map[string]struct{})
	for _, group := range groups {
		for _, task := range group.Tasks {
			exists := previewHasConflict(preview, "task", task.Key)
			if exists && strategy == ImportConflictSkip {
				result.Skipped.Tasks++
				continue
			}
			needed[task.Key] = struct{}{}
			rows = append(rows, []any{
				workspaceID,
				task.Key,
				group.Key,
				nullString(task.SequenceKey),
				nullInt32FromUint32(task.SequencePosition),
				defaultString(task.TaskKind, TaskKindInternal),
				task.ActionKey,
				task.ActionKind,
				defaultString(task.ClaimMode, ClaimModeManual),
				defaultString(task.StartMode, StartModeNone),
				task.TargetCount,
				defaultString(task.Reset.Unit, ResetNever),
				defaultUint32(task.Reset.Every, 1),
				task.Position,
				defaultJSON(task.Payload, "{}"),
				defaultJSON(task.Target, "null"),
				nullString(task.Integration.Kind),
				nullString(task.Integration.Provider),
				defaultJSON(task.Integration.Payload, "null"),
				nullString(task.ImageURL),
				task.IsVisible,
				task.IsActive,
				nullTime(task.StartAt),
				nullTime(task.EndAt),
			})
			result.Imported.Tasks++
		}
	}
	if err := r.execImportBulk(ctx, "task_definition",
		[]string{
			"workspace_id", "key", "group_key", "sequence_key", "sequence_position",
			"task_kind", "action_key", "action_kind", "claim_mode", "start_mode", "target_count",
			"reset_unit", "reset_every", "position", "payload", "target",
			"integration_kind", "integration_provider", "integration_payload", "image_url",
			"is_visible", "is_active", "start_at", "end_at",
		},
		rows,
		"ON CONFLICT (workspace_id, key) DO UPDATE SET "+
			"group_key = EXCLUDED.group_key, sequence_key = EXCLUDED.sequence_key, sequence_position = EXCLUDED.sequence_position, "+
			"task_kind = EXCLUDED.task_kind, action_key = EXCLUDED.action_key, action_kind = EXCLUDED.action_kind, "+
			"claim_mode = EXCLUDED.claim_mode, start_mode = EXCLUDED.start_mode, target_count = EXCLUDED.target_count, reset_unit = EXCLUDED.reset_unit, "+
			"reset_every = EXCLUDED.reset_every, position = EXCLUDED.position, payload = EXCLUDED.payload, target = EXCLUDED.target, "+
			"integration_kind = EXCLUDED.integration_kind, integration_provider = EXCLUDED.integration_provider, "+
			"integration_payload = EXCLUDED.integration_payload, image_url = EXCLUDED.image_url, is_visible = EXCLUDED.is_visible, "+
			"is_active = EXCLUDED.is_active, start_at = EXCLUDED.start_at, end_at = EXCLUDED.end_at, deleted_at = NULL, updated_at = now()",
		strategy,
	); err != nil {
		return nil, err
	}
	if len(needed) == 0 {
		return nil, nil
	}
	taskRows, err := r.q.ExportListTasks(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	resultIDs := make(map[string]uint64, len(needed))
	for _, task := range taskRows {
		if _, ok := needed[task.Key]; ok {
			resultIDs[task.Key] = uint64(task.ID)
		}
	}
	return resultIDs, nil
}

func (r *Repository) importTaskLocalizationsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	taskIDs map[string]uint64,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, group := range groups {
		for _, task := range group.Tasks {
			if previewHasConflict(preview, "task", task.Key) && strategy == ImportConflictSkip {
				continue
			}
			taskID, ok := taskIDs[task.Key]
			if !ok {
				continue
			}
			for locale, text := range task.Localization {
				rows = append(rows, []any{workspaceID, taskID, locale, text.Title, text.Description})
				result.Imported.TaskLocalizations++
			}
		}
	}
	return r.execImportBulk(
		ctx,
		"task_localization",
		[]string{"workspace_id", "task_id", "locale", "title", "description"},
		rows,
		"ON CONFLICT (workspace_id, task_id, locale) DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importRewardsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	taskIDs map[string]uint64,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, group := range groups {
		for _, task := range group.Tasks {
			if previewHasConflict(preview, "task", task.Key) && strategy == ImportConflictSkip {
				continue
			}
			taskID, ok := taskIDs[task.Key]
			if !ok {
				continue
			}
			for _, reward := range task.Rewards {
				rows = append(rows, []any{
					workspaceID, taskID, reward.Key, defaultString(reward.Type, "quantity"),
					reward.Quantity, reward.Scale, nullRewardDurationUnit(reward.Unit), reward.Position,
				})
				result.Imported.Rewards++
			}
		}
	}
	return r.execImportBulk(
		ctx,
		"task_reward",
		[]string{
			"workspace_id",
			"task_id",
			"reward_key",
			"reward_type",
			"quantity",
			"scale",
			"duration_unit",
			"position",
		},
		rows,
		"ON CONFLICT (workspace_id, task_id, reward_key) DO UPDATE SET "+
			"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, "+
			"duration_unit = EXCLUDED.duration_unit, position = EXCLUDED.position, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importComplexConditionsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	taskIDs map[string]uint64,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, group := range groups {
		for _, task := range group.Tasks {
			if previewHasConflict(preview, "task", task.Key) && strategy == ImportConflictSkip {
				continue
			}
			parentID, ok := taskIDs[task.Key]
			if !ok {
				continue
			}
			for _, condition := range task.Conditions {
				conditionID, ok := taskIDs[condition.TaskKey]
				if !ok {
					continue
				}
				rows = append(rows, []any{
					workspaceID,
					parentID,
					conditionID,
					defaultString(condition.RequiredStatus, ComplexRequiredStatusReady),
					condition.Position,
					condition.IsRequired,
				})
				result.Imported.Conditions++
			}
		}
	}
	return r.execImportBulk(ctx, "task_complex_condition",
		[]string{"workspace_id", "parent_task_id", "condition_task_id", "required_status", "position", "is_required"},
		rows,
		"ON CONFLICT (workspace_id, parent_task_id, condition_task_id) DO UPDATE SET "+
			"required_status = EXCLUDED.required_status, position = EXCLUDED.position, "+
			"is_required = EXCLUDED.is_required, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importPartnerConfigsBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	strategy string,
	secrets map[string]string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, group := range groups {
		for _, config := range group.PartnerConfigs {
			key := partnerConfigImportKey(config.Provider, group.Key, config.Platform)
			exists := previewHasConflict(preview, "partner_config", key)
			if exists && strategy == ImportConflictSkip {
				result.Skipped.PartnerConfigs++
				continue
			}
			secret := importSecretValue(config.Secret, secrets)
			webhookSecret := importSecretValue(config.WebhookSecret, secrets)
			rows = append(rows, []any{
				workspaceID, config.Provider, group.Key, config.Platform, config.IsEnabled,
				secret, webhookSecret, defaultJSON(config.Target, "null"), defaultJSON(config.Settings, "null"),
			})
			result.Imported.PartnerConfigs++
		}
	}
	return r.execImportBulk(
		ctx,
		"task_partner_config",
		[]string{
			"workspace_id",
			"provider",
			"group_key",
			"platform",
			"is_enabled",
			"secret",
			"webhook_secret",
			"target",
			"settings",
		},
		rows,
		"ON CONFLICT (workspace_id, provider, group_key, platform) DO UPDATE SET "+
			"is_enabled = EXCLUDED.is_enabled, secret = EXCLUDED.secret, webhook_secret = EXCLUDED.webhook_secret, "+
			"target = EXCLUDED.target, settings = EXCLUDED.settings, updated_at = now()",
		strategy,
	)
}

func (r *Repository) importPartnerRewardRulesBulk(
	ctx context.Context,
	workspaceID string,
	groups []ExportGroup,
	strategy string,
	preview ImportPreview,
	result *ImportResult,
) error {
	rows := make([][]any, 0)
	for _, group := range groups {
		for _, rule := range group.PartnerRewardRules {
			externalType := defaultString(rule.ExternalType, "*")
			key := partnerRewardImportKey(rule.Provider, group.Key, externalType, rule.Reward.Key)
			exists := previewHasConflict(preview, "partner_reward_rule", key)
			if exists && strategy == ImportConflictSkip {
				result.Skipped.PartnerRewards++
				continue
			}
			rewardType := defaultString(rule.Reward.Type, "quantity")
			rows = append(rows, []any{
				workspaceID, rule.Provider, group.Key, externalType,
				rule.Reward.Key, rewardType, rule.Reward.Quantity, rule.Reward.Scale,
				nullPartnerRewardDurationUnit(rule.Reward.Unit), rule.Position, rule.IsEnabled,
			})
			result.Imported.PartnerRewards++
		}
	}
	return r.execImportBulk(ctx, "task_partner_reward_rule",
		[]string{
			"workspace_id", "provider", "group_key", "external_type", "reward_key",
			"reward_type", "quantity", "scale", "duration_unit", "position", "is_enabled",
		},
		rows,
		"ON CONFLICT (workspace_id, provider, group_key, external_type, reward_key) DO UPDATE SET "+
			"reward_type = EXCLUDED.reward_type, quantity = EXCLUDED.quantity, scale = EXCLUDED.scale, "+
			"duration_unit = EXCLUDED.duration_unit, position = EXCLUDED.position, is_enabled = EXCLUDED.is_enabled, updated_at = now()",
		strategy,
	)
}

func (r *Repository) execImportBulk(
	ctx context.Context,
	table string,
	columns []string,
	rows [][]any,
	conflictUpdate string,
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
				importConflictClause(conflictUpdate, strategy),
			)
			return repositoryExec(ctx, r, func(ctx context.Context) error {
				_, err := r.executor.ExecContext(ctx, query, args...)
				return err
			})
		},
	)
}

func importConflictClause(updateClause, strategy string) string {
	switch strategy {
	case ImportConflictSkip:
		index := strings.Index(updateClause, " DO UPDATE")
		if index < 0 {
			return ""
		}
		return updateClause[:index] + " DO NOTHING"
	case ImportConflictUpdate:
		return updateClause
	default:
		return ""
	}
}

func compileImportBulkUpsert(table string, columns []string, rows [][]any, conflictUpdate string) (string, []any) {
	var builder strings.Builder
	builder.Grow(len(rows) * len(columns) * 4)
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
	if conflictUpdate != "" {
		builder.WriteByte(' ')
		builder.WriteString(conflictUpdate)
	}
	return builder.String(), args
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func defaultUint32(value uint32, fallback uint32) uint32 {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultJSON(value []byte, fallback string) string {
	if len(value) == 0 {
		return fallback
	}
	return string(value)
}

func nullRewardDurationUnit(value *string) sql.NullString {
	return sql.NullString{
		String: taskStringValue(value),
		Valid:  value != nil,
	}
}

func nullPartnerRewardDurationUnit(value *string) sql.NullString {
	return sql.NullString{
		String: taskStringValue(value),
		Valid:  value != nil,
	}
}

type importExistingKeys struct {
	groups         map[string]bool
	sequences      map[string]bool
	tasks          map[string]bool
	partnerConfigs map[string]bool
	partnerRewards map[string]bool
}

func (r *Repository) importExistingKeys(ctx context.Context, workspaceID string) (importExistingKeys, error) {
	out := importExistingKeys{
		groups: make(map[string]bool), sequences: make(map[string]bool), tasks: make(map[string]bool),
		partnerConfigs: make(map[string]bool), partnerRewards: make(map[string]bool),
	}
	groups, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskGroup, error) {
		return r.q.AdminListGroups(ctx, workspaceID)
	})
	if err != nil {
		return out, err
	}
	for _, group := range groups {
		out.groups[group.Key] = true
	}
	sequences, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskSequence, error) {
		return r.q.AdminListSequences(ctx, workspaceID)
	})
	if err != nil {
		return out, err
	}
	for _, sequence := range sequences {
		out.sequences[sequence.Key] = true
	}
	tasks, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskDefinition, error) {
		return r.q.ExportListTasks(ctx, workspaceID)
	})
	if err != nil {
		return out, err
	}
	for _, task := range tasks {
		out.tasks[task.Key] = true
	}
	configs, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerConfig, error) {
		return r.q.AdminListPartnerConfigs(ctx, workspaceID)
	})
	if err != nil {
		return out, err
	}
	for _, config := range configs {
		out.partnerConfigs[partnerConfigImportKey(config.Provider, config.GroupKey, config.Platform)] = true
	}
	rewards, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerRewardRule, error) {
		return r.q.AdminListPartnerRewardRules(ctx, workspaceID)
	})
	if err != nil {
		return out, err
	}
	for _, reward := range rewards {
		out.partnerRewards[partnerRewardImportKey(reward.Provider, reward.GroupKey, reward.ExternalType, reward.RewardKey)] = true
	}
	return out, nil
}

func countPackage(pkg ExportPackage) ImportCounts {
	out := ImportCounts{Groups: len(pkg.Groups), Sequences: len(pkg.Sequences)}
	for _, group := range pkg.Groups {
		out.GroupLocalizations += len(group.Localization)
		out.Tasks += len(group.Tasks)
		out.PartnerConfigs += len(group.PartnerConfigs)
		out.PartnerRewards += len(group.PartnerRewardRules)
		for _, task := range group.Tasks {
			out.TaskLocalizations += len(task.Localization)
			out.Rewards += len(task.Rewards)
			out.Conditions += len(task.Conditions)
		}
	}
	return out
}

func validateExportPackage(pkg ExportPackage) error {
	if pkg.Format != ExportFormat {
		return fmt.Errorf("unsupported tasks export format: %s", pkg.Format)
	}
	if pkg.Service != "tasks" {
		return fmt.Errorf("unsupported export service: %s", pkg.Service)
	}
	sequenceKeys := make(map[string]int, len(pkg.Sequences))
	for index, sequence := range pkg.Sequences {
		if strings.TrimSpace(sequence.Key) == "" {
			return fmt.Errorf("tasks import sequences[%d].key is required", index)
		}
		if previous, exists := sequenceKeys[sequence.Key]; exists {
			return fmt.Errorf("tasks import sequences[%d].key duplicates sequences[%d].key", index, previous)
		}
		sequenceKeys[sequence.Key] = index
	}

	groupKeys := make(map[string]int, len(pkg.Groups))
	taskPaths := make(map[string]string)
	conditionRefs := make(map[string][]string)
	for groupIndex, group := range pkg.Groups {
		groupPath := fmt.Sprintf("tasks import groups[%d]", groupIndex)
		if strings.TrimSpace(group.Key) == "" {
			return fmt.Errorf("%s.key is required", groupPath)
		}
		if previous, exists := groupKeys[group.Key]; exists {
			return fmt.Errorf("%s.key duplicates groups[%d].key", groupPath, previous)
		}
		groupKeys[group.Key] = groupIndex

		for locale, text := range group.Localization {
			if strings.TrimSpace(locale) == "" || strings.TrimSpace(text.Title) == "" {
				return fmt.Errorf("%s.localization requires locale and title", groupPath)
			}
		}

		for configIndex, config := range group.PartnerConfigs {
			if strings.TrimSpace(config.Provider) == "" || strings.TrimSpace(config.Platform) == "" {
				return fmt.Errorf("%s.partner_configs[%d] requires provider and platform", groupPath, configIndex)
			}
			if err := target.Validate(config.Target); err != nil {
				return fmt.Errorf("%s.partner_configs[%d].target: %w", groupPath, configIndex, err)
			}
			if len(config.Settings) > 0 && !json.Valid(config.Settings) {
				return fmt.Errorf("%s.partner_configs[%d].settings must be valid JSON", groupPath, configIndex)
			}
		}

		for ruleIndex, rule := range group.PartnerRewardRules {
			if strings.TrimSpace(rule.Provider) == "" {
				return fmt.Errorf("%s.partner_reward_rules[%d].provider is required", groupPath, ruleIndex)
			}
			if err := validateRewardDefinition(rule.Reward); err != nil {
				return fmt.Errorf("%s.partner_reward_rules[%d].reward: %w", groupPath, ruleIndex, err)
			}
		}

		sequencePositions := make(map[string]map[uint32]string)
		for taskIndex, task := range group.Tasks {
			taskPath := fmt.Sprintf("%s.tasks[%d]", groupPath, taskIndex)
			if previous, exists := taskPaths[task.Key]; exists {
				return fmt.Errorf("%s.key duplicates %s.key", taskPath, previous)
			}
			taskPaths[task.Key] = taskPath

			params := normalizeSaveTaskParams(SaveTaskParams{
				WorkspaceID:         "00000000-0000-0000-0000-000000000000",
				Key:                 task.Key,
				GroupKey:            group.Key,
				SequenceKey:         task.SequenceKey,
				SequencePosition:    task.SequencePosition,
				TaskKind:            task.TaskKind,
				ActionKey:           task.ActionKey,
				ActionKind:          task.ActionKind,
				ClaimMode:           task.ClaimMode,
				StartMode:           task.StartMode,
				TargetCount:         task.TargetCount,
				ResetUnit:           task.Reset.Unit,
				ResetEvery:          task.Reset.Every,
				Position:            task.Position,
				Payload:             task.Payload,
				Target:              task.Target,
				IntegrationKind:     task.Integration.Kind,
				IntegrationProvider: task.Integration.Provider,
				IntegrationPayload:  task.Integration.Payload,
				ImageURL:            task.ImageURL,
				IsVisible:           task.IsVisible,
				IsActive:            task.IsActive,
				StartAt:             task.StartAt,
				EndAt:               task.EndAt,
			})
			if err := validateSaveTask(params); err != nil {
				return fmt.Errorf("%s: %w", taskPath, err)
			}

			if task.SequenceKey != nil && task.SequencePosition != nil {
				if sequencePositions[*task.SequenceKey] == nil {
					sequencePositions[*task.SequenceKey] = make(map[uint32]string)
				}
				if previous := sequencePositions[*task.SequenceKey][*task.SequencePosition]; previous != "" {
					return fmt.Errorf("%s sequence position duplicates task %s", taskPath, previous)
				}
				sequencePositions[*task.SequenceKey][*task.SequencePosition] = task.Key
			}

			for locale, text := range task.Localization {
				if strings.TrimSpace(locale) == "" || strings.TrimSpace(text.Title) == "" {
					return fmt.Errorf("%s.localization requires locale and title", taskPath)
				}
			}

			rewardKeys := make(map[string]int, len(task.Rewards))
			for rewardIndex, reward := range task.Rewards {
				if previous, exists := rewardKeys[reward.Key]; exists {
					return fmt.Errorf("%s.rewards[%d].key duplicates rewards[%d].key", taskPath, rewardIndex, previous)
				}
				rewardKeys[reward.Key] = rewardIndex
				if err := validateRewardDefinition(reward); err != nil {
					return fmt.Errorf("%s.rewards[%d]: %w", taskPath, rewardIndex, err)
				}
			}

			conditionKeys := make(map[string]int, len(task.Conditions))
			if len(task.Conditions) > 0 && task.TaskKind != TaskKindComplex {
				return fmt.Errorf("%s.conditions require task_kind %q", taskPath, TaskKindComplex)
			}
			for conditionIndex, condition := range task.Conditions {
				if strings.TrimSpace(condition.TaskKey) == "" || condition.TaskKey == task.Key {
					return fmt.Errorf("%s.conditions[%d].task_key is invalid", taskPath, conditionIndex)
				}
				if previous, exists := conditionKeys[condition.TaskKey]; exists {
					return fmt.Errorf("%s.conditions[%d] duplicates conditions[%d]", taskPath, conditionIndex, previous)
				}
				conditionKeys[condition.TaskKey] = conditionIndex
				if condition.RequiredStatus != "" &&
					condition.RequiredStatus != ComplexRequiredStatusReady &&
					condition.RequiredStatus != ComplexRequiredStatusClaimed {
					return fmt.Errorf("%s.conditions[%d].required_status is unsupported", taskPath, conditionIndex)
				}
				conditionRefs[task.Key] = append(conditionRefs[task.Key], condition.TaskKey)
			}
		}
	}

	for parent, children := range conditionRefs {
		for _, child := range children {
			if _, exists := taskPaths[child]; !exists {
				return fmt.Errorf("tasks import task %s references missing condition task %s", parent, child)
			}
		}
	}
	if hasDirectedCycle(conditionRefs) {
		return fmt.Errorf("tasks import complex conditions contain a cycle")
	}

	return nil
}

func requireImportSecrets(pkg ExportPackage, secrets map[string]string) error {
	for _, group := range pkg.Groups {
		for _, config := range group.PartnerConfigs {
			for _, secret := range []*ExportSecret{config.Secret, config.WebhookSecret} {
				if secret == nil {
					continue
				}
				if importSecretValue(secret, secrets).Valid {
					continue
				}
				if secret.Key == "" {
					return fmt.Errorf("required import secret is missing")
				}
				if secrets == nil || secrets[secret.Key] == "" {
					return fmt.Errorf("required import secret is missing: %s", secret.Key)
				}
			}
		}
	}
	return nil
}

func importSecretValue(secret *ExportSecret, secrets map[string]string) sql.NullString {
	if secret == nil {
		return sql.NullString{}
	}
	if secrets != nil && secret.Key != "" {
		if value := secrets[secret.Key]; value != "" {
			return sql.NullString{String: value, Valid: true}
		}
	}
	if secret.Value != nil && *secret.Value != "" {
		return sql.NullString{String: *secret.Value, Valid: true}
	}
	return sql.NullString{}
}

func secretHasEmbeddedValue(secret *ExportSecret) bool {
	return secret != nil && secret.Value != nil && *secret.Value != ""
}

func previewHasConflict(preview ImportPreview, kind, key string) bool {
	for _, conflict := range preview.Conflicts {
		if conflict.Type == kind && conflict.Key == key {
			return true
		}
	}
	return false
}

func partnerConfigImportKey(provider, groupKey, platform string) string {
	return provider + ":" + groupKey + ":" + platform
}

func partnerRewardImportKey(provider, groupKey, externalType, rewardKey string) string {
	if externalType == "" {
		externalType = "*"
	}
	return provider + ":" + groupKey + ":" + externalType + ":" + rewardKey
}
