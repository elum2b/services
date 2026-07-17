package repository

import (
	"context"
	"fmt"
	"time"

	promosqlc "github.com/elum2b/services/promo/sqlc"
)

func (r *Repository) GetRedemption(ctx context.Context, identity Identity, promoID uint64) (*Redemption, error) {
	row, err := r.q.GetRedemption(ctx, promosqlc.GetRedemptionParams{
		WorkspaceID:    identity.WorkspaceID,
		PromoID:        int64(promoID),
		AppID:          identity.AppID,
		PlatformID:     identity.PlatformID,
		PlatformUserID: identity.PlatformUserID,
	})
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := mapRedemption(row)
	return &value, nil
}

func (r *Repository) ListRedemptions(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	limit, offset int32,
) ([]Redemption, error) {
	limit, offset = normalizePage(limit, offset)
	rows, err := r.q.AdminListRedemptions(ctx, promosqlc.AdminListRedemptionsParams{
		WorkspaceID: workspaceID,
		PromoID:     int64(promoID),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, err
	}
	result := make([]Redemption, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapRedemption(row))
	}
	return result, nil
}

func (r *Repository) GetStats(ctx context.Context, workspaceID string, promoID uint64) (Stats, error) {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return Stats{}, err
	}

	row, err := r.q.AdminGetStats(ctx, promosqlc.AdminGetStatsParams{
		WorkspaceID: workspaceID,
		ID:          int64(promoID),
	})
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		ActivationCount:      uint64(row.ActivationCount),
		MaxActivations:       uint64(row.MaxActivations),
		RemainingActivations: remainingActivations(row.RemainingActivations),
	}, nil
}

func (r *Repository) ListDailyStats(
	ctx context.Context,
	workspaceID string,
	promoID uint64,
	from, until time.Time,
) ([]DailyStats, error) {
	rows, err := r.q.AdminListDailyStats(ctx, promosqlc.AdminListDailyStatsParams{
		WorkspaceID: workspaceID,
		PromoID:     int64(promoID),
		StatsDate:   from,
		StatsDate_2: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]DailyStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, DailyStats{
			Date:            row.StatsDate,
			RedemptionCount: uint64(row.RedemptionCount),
			UniqueUsers:     uint64(row.UniqueUsers),
		})
	}
	return result, nil
}

func (r *Repository) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	if err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return fmt.Errorf("promo stats workspace or date range is invalid")
	}

	return r.q.RefreshDailyStats(ctx, promosqlc.RefreshDailyStatsParams{
		WorkspaceID:  workspaceID,
		OccurredAt:   from,
		OccurredAt_2: until,
	})
}

func remainingActivations(value int64) *uint64 {
	if value < 0 {
		return nil
	}
	converted := uint64(value)
	return &converted
}
