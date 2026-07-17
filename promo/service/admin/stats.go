package admin

import (
	"context"
	"time"

	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/user"
)

func (a *Admin) GetStats(ctx context.Context, workspaceID string, promoID uint64) (StatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetStats(mergedCtx, workspaceID, promoID)
	if err != nil {
		return StatsModel{}, err
	}
	return StatsModel(value), nil
}

func (a *Admin) GetUserRedemption(
	ctx context.Context,
	identity user.Identity,
	promoID uint64,
) (*RedemptionModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetRedemption(mergedCtx, repository.Identity{
		WorkspaceID:    identity.WorkspaceID,
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		Platform:       identity.Platform,
		PlatformUserID: identity.PlatformUserID,
		IsPremium:      identity.IsPremium,
		Sex:            identity.Sex,
		Country:        identity.Country,
	}, promoID)
	if err != nil || value == nil {
		return nil, err
	}
	result := mapRedemption(*value)
	return &result, nil
}

func (a *Admin) ListRedemptions(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	page Page,
) ([]RedemptionModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	values, err := a.repository.ListRedemptions(mergedCtx, workspaceID, promoID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]RedemptionModel, 0, len(values))
	for _, value := range values {
		result = append(result, mapRedemption(value))
	}
	return result, nil
}

func (a *Admin) ListDailyStats(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	from, until time.Time,
) ([]DailyStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListDailyStats(mergedCtx, workspaceID, promoID, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]DailyStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyStatsModel(value))
	}
	return result, nil
}

func (a *Admin) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RefreshDailyStats(mergedCtx, workspaceID, from, until)
}

func mapRedemption(value repository.Redemption) RedemptionModel {
	return RedemptionModel{
		ID: value.ID, AppID: value.AppID, PlatformID: value.PlatformID,
		PlatformUserID: value.PlatformUserID, RedeemedAt: value.RedeemedAt,
	}
}
