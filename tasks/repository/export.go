package repository

import (
	"context"
	"time"

	json "github.com/goccy/go-json"

	tasksqlc "github.com/elum2b/services/tasks/sqlc"
)

func (r *Repository) ExportManifest() ExportManifest {
	return ExportManifest{
		Format: ExportFormat, Service: "tasks",
		Sections: []ExportManifestSection{
			{Key: ExportSectionGroups, Title: "Groups", Description: "Task groups and ordering.", DefaultEnabled: true},
			{Key: ExportSectionTasks, Title: "Tasks", Description: "Task definitions.", DefaultEnabled: true},
			{
				Key:            ExportSectionSequences,
				Title:          "Sequences",
				Description:    "Sequential execution chains.",
				DefaultEnabled: true,
			},
			{
				Key:            ExportSectionLocalization,
				Title:          "Localization",
				Description:    "Group and task texts.",
				DefaultEnabled: true,
			},
			{Key: ExportSectionRewards, Title: "Rewards", Description: "Task rewards.", DefaultEnabled: true},
			{
				Key:            ExportSectionTarget,
				Title:          "Target",
				Description:    "Task and partner visibility filters.",
				DefaultEnabled: true,
			},
			{
				Key:            ExportSectionIntegration,
				Title:          "Integration",
				Description:    "Private integration configuration without secret values.",
				DefaultEnabled: true,
			},
			{
				Key:            ExportSectionPartnerConfigs,
				Title:          "Partner configs",
				Description:    "Partner provider settings without secret values.",
				DefaultEnabled: true,
			},
			{
				Key:            ExportSectionPartnerRewards,
				Title:          "Partner rewards",
				Description:    "Partner reward rules.",
				DefaultEnabled: true,
			},
			{
				Key:            ExportSectionComplex,
				Title:          "Complex tasks",
				Description:    "Complex task conditions.",
				DefaultEnabled: true,
			},
		},
	}
}

func (r *Repository) Export(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return ExportPackage{}, err
	}

	var result ExportPackage
	err := r.WithTx(ctx, func(txRepo *Repository) error {
		if _, err := txRepo.executor.ExecContext(
			ctx,
			"SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY",
		); err != nil {
			return err
		}

		var err error
		result, err = txRepo.exportSnapshot(ctx, workspaceID, req)
		return err
	})
	return result, err
}

func (r *Repository) exportSnapshot(ctx context.Context, workspaceID string, req ExportRequest) (ExportPackage, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	sections := exportSections(req.Sections)
	groups, err := repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskGroup, error) {
		return r.q.AdminListGroups(ctx, workspaceID)
	})
	if err != nil {
		return ExportPackage{}, err
	}
	var groupLocalizations []tasksqlc.TaskGroupLocalization
	if sections[ExportSectionLocalization] {
		groupLocalizations, err = repositoryValue(
			ctx,
			r,
			func(ctx context.Context) ([]tasksqlc.TaskGroupLocalization, error) {
				return r.q.AdminListGroupLocalizations(ctx, workspaceID)
			},
		)
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var sequences []tasksqlc.TaskSequence
	if sections[ExportSectionSequences] {
		sequences, err = repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskSequence, error) {
			return r.q.AdminListSequences(ctx, workspaceID)
		})
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var taskRows []tasksqlc.TaskDefinition
	if sections[ExportSectionTasks] {
		taskRows, err = repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskDefinition, error) {
			return r.q.ExportListTasks(ctx, workspaceID)
		})
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var taskLocalizations []tasksqlc.TaskLocalization
	if sections[ExportSectionLocalization] {
		taskLocalizations, err = repositoryValue(
			ctx,
			r,
			func(ctx context.Context) ([]tasksqlc.TaskLocalization, error) {
				return r.q.AdminListTaskLocalizations(ctx, workspaceID)
			},
		)
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var rewardRows []tasksqlc.TaskReward
	if sections[ExportSectionRewards] {
		rewardRows, err = repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskReward, error) {
			return r.q.AdminListAllRewards(ctx, workspaceID)
		})
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var partnerConfigs []tasksqlc.TaskPartnerConfig
	if sections[ExportSectionPartnerConfigs] {
		partnerConfigs, err = repositoryValue(ctx, r, func(ctx context.Context) ([]tasksqlc.TaskPartnerConfig, error) {
			return r.q.AdminListPartnerConfigs(ctx, workspaceID)
		})
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var partnerRewards []tasksqlc.TaskPartnerRewardRule
	if sections[ExportSectionPartnerRewards] {
		partnerRewards, err = repositoryValue(
			ctx,
			r,
			func(ctx context.Context) ([]tasksqlc.TaskPartnerRewardRule, error) {
				return r.q.AdminListPartnerRewardRules(ctx, workspaceID)
			},
		)
		if err != nil {
			return ExportPackage{}, err
		}
	}
	var complexConditions []tasksqlc.TaskComplexCondition
	if sections[ExportSectionComplex] {
		complexConditions, err = repositoryValue(
			ctx,
			r,
			func(ctx context.Context) ([]tasksqlc.TaskComplexCondition, error) {
				return r.q.AdminListComplexConditions(ctx, workspaceID)
			},
		)
		if err != nil {
			return ExportPackage{}, err
		}
	}

	groupIndexByKey := make(map[string]int, len(groups))
	out := ExportPackage{
		Format: ExportFormat, Service: "tasks", CreatedAt: now.UTC(),
		Groups: make([]ExportGroup, 0, len(groups)), Sequences: make([]ExportSequence, 0, len(sequences)),
	}
	for _, group := range groups {
		exportGroup := ExportGroup{Key: group.Key, Position: group.Position, IsActive: group.IsActive}
		if sections[ExportSectionLocalization] {
			exportGroup.Localization = make(map[string]ExportText)
		}
		if sections[ExportSectionTasks] {
			exportGroup.Tasks = make([]ExportTask, 0)
		}
		out.Groups = append(out.Groups, exportGroup)
		groupIndexByKey[group.Key] = len(out.Groups) - 1
	}
	for _, item := range groupLocalizations {
		index, ok := groupIndexByKey[item.GroupKey]
		if !ok {
			continue
		}
		out.Groups[index].Localization[item.Locale] = ExportText{Title: item.Title, Description: item.Description}
	}
	for _, sequence := range sequences {
		out.Sequences = append(out.Sequences, ExportSequence{
			Key: sequence.Key, Position: sequence.Position, IsActive: sequence.IsActive,
		})
	}

	taskLocalizationsByID := make(map[uint64]map[string]ExportText)
	for _, item := range taskLocalizations {
		taskID := uint64(item.TaskID)
		if taskLocalizationsByID[taskID] == nil {
			taskLocalizationsByID[taskID] = make(map[string]ExportText)
		}
		taskLocalizationsByID[taskID][item.Locale] = ExportText{Title: item.Title, Description: item.Description}
	}
	rewardsByTaskID := make(map[uint64][]ExportReward)
	for _, reward := range rewardRows {
		taskID := uint64(reward.TaskID)
		rewardsByTaskID[taskID] = append(rewardsByTaskID[taskID], ExportReward{
			Key: reward.RewardKey, Type: string(reward.RewardType), Quantity: reward.Quantity,
			Scale: uint16(reward.Scale), Unit: taskDurationUnitPtr(reward.DurationUnit), Position: reward.Position,
		})
	}
	taskKeyByID := make(map[uint64]string, len(taskRows))
	for _, row := range taskRows {
		taskKeyByID[uint64(row.ID)] = row.Key
	}
	conditionsByParentID := make(map[uint64][]ExportCondition)
	for _, condition := range complexConditions {
		conditionTaskID := uint64(condition.ConditionTaskID)
		parentTaskID := uint64(condition.ParentTaskID)
		taskKey, ok := taskKeyByID[conditionTaskID]
		if !ok {
			continue
		}
		conditionsByParentID[parentTaskID] = append(conditionsByParentID[parentTaskID], ExportCondition{
			TaskKey:        taskKey,
			RequiredStatus: condition.RequiredStatus,
			Position:       condition.Position,
			IsRequired:     condition.IsRequired,
		})
	}
	for _, row := range taskRows {
		index, ok := groupIndexByKey[row.GroupKey]
		if !ok {
			continue
		}
		task := mapTask(row)
		sequenceKey := task.SequenceKey
		sequencePosition := task.SequencePosition
		if !sections[ExportSectionSequences] {
			sequenceKey = nil
			sequencePosition = nil
		}
		target := nullableRaw(task.Target)
		if !sections[ExportSectionTarget] {
			target = nil
		}
		integration := ExportIntegration{}
		if sections[ExportSectionIntegration] {
			integration = ExportIntegration{
				Kind: task.IntegrationKind, Provider: task.IntegrationProvider,
				Payload: nullableRaw(task.IntegrationPayload),
			}
		}
		out.Groups[index].Tasks = append(out.Groups[index].Tasks, ExportTask{
			Key: task.Key, SequenceKey: sequenceKey, SequencePosition: sequencePosition,
			TaskKind: task.TaskKind, ActionKey: task.ActionKey, ActionKind: task.ActionKind,
			ClaimMode: task.ClaimMode, StartMode: task.StartMode, TargetCount: task.TargetCount,
			Reset:    ExportReset{Unit: task.ResetUnit, Every: task.ResetEvery},
			Position: task.Position, Payload: nullableRaw(task.Payload), Target: target,
			Integration: integration,
			ImageURL:    task.ImageURL, IsVisible: task.IsVisible, IsActive: task.IsActive,
			StartAt: task.StartAt, EndAt: task.EndAt, Localization: taskLocalizationsByID[task.ID],
			Rewards: rewardsByTaskID[task.ID], Conditions: conditionsByParentID[task.ID],
		})
	}
	for _, row := range partnerConfigs {
		index, ok := groupIndexByKey[row.GroupKey]
		if !ok {
			continue
		}
		config := mapPartnerConfig(row)
		target := nullableRaw(config.Target)
		if !sections[ExportSectionTarget] {
			target = nil
		}
		out.Groups[index].PartnerConfigs = append(out.Groups[index].PartnerConfigs, ExportPartnerConfig{
			Provider: config.Provider, Platform: config.Platform, IsEnabled: config.IsEnabled,
			Secret: exportSecret(
				config,
				req.IncludeSecrets,
			), WebhookSecret: exportWebhookSecret(config, req.IncludeSecrets),
			Target: target, Settings: nullableRaw(config.Settings),
		})
	}
	for _, row := range partnerRewards {
		index, ok := groupIndexByKey[row.GroupKey]
		if !ok {
			continue
		}
		out.Groups[index].PartnerRewardRules = append(out.Groups[index].PartnerRewardRules, ExportPartnerRewardRule{
			Provider: row.Provider, ExternalType: row.ExternalType, Position: row.Position, IsEnabled: row.IsEnabled,
			Reward: ExportReward{
				Key: row.RewardKey, Type: string(row.RewardType), Quantity: row.Quantity,
				Scale: uint16(row.Scale), Unit: nullPartnerDurationUnit(row.DurationUnit), Position: row.Position,
			},
		})
	}
	return out, nil
}

func exportSections(values []string) map[string]bool {
	all := []string{
		ExportSectionGroups, ExportSectionSequences, ExportSectionTasks, ExportSectionLocalization,
		ExportSectionRewards, ExportSectionTarget, ExportSectionIntegration, ExportSectionPartnerConfigs,
		ExportSectionPartnerRewards, ExportSectionComplex,
	}
	out := make(map[string]bool, len(all))
	if len(values) == 0 {
		for _, value := range all {
			out[value] = true
		}
		return out
	}
	for _, value := range values {
		out[value] = true
	}
	out[ExportSectionGroups] = true
	return out
}

func exportSecret(config PartnerConfig, includeValue bool) *ExportSecret {
	if config.Secret == nil || *config.Secret == "" {
		return nil
	}
	secret := &ExportSecret{
		Mode: "required",
		Key:  partnerSecretImportKey(config.Provider, config.GroupKey, config.Platform),
	}
	if includeValue {
		secret.Value = config.Secret
	}
	return secret
}

func exportWebhookSecret(config PartnerConfig, includeValue bool) *ExportSecret {
	if config.WebhookSecret == nil || *config.WebhookSecret == "" {
		return nil
	}
	secret := &ExportSecret{
		Mode: "required",
		Key:  partnerWebhookSecretImportKey(config.Provider, config.GroupKey, config.Platform),
	}
	if includeValue {
		secret.Value = config.WebhookSecret
	}
	return secret
}

func nullableRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func partnerSecretImportKey(provider, groupKey, platform string) string {
	return "tasks.partner." + provider + "." + groupKey + "." + platform + ".secret"
}

func partnerWebhookSecretImportKey(provider, groupKey, platform string) string {
	return "tasks.partner." + provider + "." + groupKey + "." + platform + ".webhook_secret"
}
