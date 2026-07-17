package admin

import (
	"context"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

func (a *Admin) SavePartnerConfig(ctx context.Context, params PartnerConfigModel) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.SavePartnerConfig(mergedCtx, repository.SavePartnerConfigParams{
		WorkspaceID: params.WorkspaceID, Provider: params.Provider, GroupKey: params.GroupKey,
		Platform: params.Platform, IsEnabled: params.IsEnabled, Secret: params.Secret,
		WebhookSecret: params.WebhookSecret,
		Target:        params.Target, Settings: params.Settings,
	})
}

func (a *Admin) GetPartnerConfig(
	ctx context.Context,
	workspaceID, provider, groupKey, platform string,
) (PartnerConfigModel, bool, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	config, found, err := a.repository.GetPartnerConfig(mergedCtx, workspaceID, provider, groupKey, platform)
	if err != nil || !found {
		return PartnerConfigModel{}, found, err
	}
	return mapPartnerConfig(config), true, nil
}

func (a *Admin) ListPartnerConfigs(ctx context.Context, workspaceID string) ([]PartnerConfigModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	configs, err := a.repository.ListPartnerConfigs(mergedCtx, workspaceID)
	if err != nil {
		return nil, err
	}
	result := make([]PartnerConfigModel, 0, len(configs))
	for _, config := range configs {
		result = append(result, mapPartnerConfig(config))
	}
	return result, nil
}

func (a *Admin) SavePartnerRewardRule(ctx context.Context, params SavePartnerRewardRuleParams) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	rewardType, err := validateReward(params.Reward)
	if err != nil {
		return err
	}
	return a.repository.SavePartnerRewardRule(mergedCtx, repository.SavePartnerRewardRuleParams{
		WorkspaceID: params.WorkspaceID, Provider: params.Provider, GroupKey: params.GroupKey,
		ExternalType: params.ExternalType, Reward: repository.Reward{
			Key: params.Reward.Key, Type: rewardType, Quantity: params.Reward.Quantity,
			Scale: params.Reward.Scale, Unit: params.Reward.Unit,
		},
		Position: params.Position, IsEnabled: params.IsEnabled,
	})
}

func (a *Admin) DeletePartnerRewardRule(
	ctx context.Context,
	workspaceID, provider, groupKey, externalType, rewardKey string,
) (int64, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.DeletePartnerRewardRule(mergedCtx, workspaceID, provider, groupKey, externalType, rewardKey)
}

func (a *Admin) ListPartnerDailyStats(
	ctx context.Context,
	workspaceID, provider, groupKey string,
	from, until time.Time,
) ([]PartnerDailyStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListPartnerDailyStats(mergedCtx, workspaceID, provider, groupKey, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]PartnerDailyStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, PartnerDailyStatsModel{
			Date: value.Date, Provider: value.Provider, GroupKey: value.GroupKey, ExternalType: value.ExternalType,
			IssuedCount: value.IssuedCount, CompletedCount: value.CompletedCount, ClaimedCount: value.ClaimedCount,
			RevokedCount: value.RevokedCount, RevokedAfterClaimCount: value.RevokedAfterClaimCount,
			FailedCount: value.FailedCount, FakeCount: value.FakeCount, ExpiredCount: value.ExpiredCount,
			UniqueIssuedUsers: value.UniqueIssuedUsers, UniqueCompletedUsers: value.UniqueCompletedUsers,
			UniqueClaimers: value.UniqueClaimers,
		})
	}
	return result, nil
}

func mapPartnerConfig(config repository.PartnerConfig) PartnerConfigModel {
	return PartnerConfigModel{
		WorkspaceID: config.WorkspaceID, Provider: config.Provider, GroupKey: config.GroupKey,
		Platform: config.Platform, IsEnabled: config.IsEnabled, Secret: config.Secret, WebhookSecret: config.WebhookSecret,
		Target: config.Target, Settings: config.Settings, CreatedAt: config.CreatedAt, UpdatedAt: config.UpdatedAt,
	}
}
