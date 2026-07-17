package admin

import (
	"context"
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/calendar/service/user"
)

type OperationModel struct {
	ID              uint64             `json:"id"`
	AppID           int64              `json:"app_id"`
	PlatformID      int64              `json:"platform_id"`
	PlatformUserID  string             `json:"platform_user_id"`
	OperationID     string             `json:"operation_id"`
	Granted         bool               `json:"granted"`
	Status          string             `json:"status"`
	Position        *uint32            `json:"position,omitempty"`
	Rewards         []user.RewardModel `json:"rewards,omitempty"`
	CurrentPosition uint32             `json:"current_position"`
	ClaimCount      uint64             `json:"claim_count"`
	OccurredAt      time.Time          `json:"occurred_at"`
}

func (a *Admin) ListOperations(
	ctx context.Context,
	workspaceID, calendarID string,
	page Page,
) ([]OperationModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	limit, offset := normalizePage(page)
	values, err := a.repository.ListOperations(mergedCtx, workspaceID, calendarID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]OperationModel, 0, len(values))
	for _, value := range values {
		var rewards []user.RewardModel
		if err := json.Unmarshal(value.Rewards, &rewards); err != nil {
			return nil, err
		}
		result = append(result, OperationModel{
			ID: value.ID, AppID: value.Identity.AppID, PlatformID: value.Identity.PlatformID,
			PlatformUserID: value.Identity.PlatformUserID, OperationID: value.OperationID,
			Granted: value.Granted, Status: value.Status, Position: value.Position,
			Rewards: rewards, CurrentPosition: value.CurrentPosition,
			ClaimCount: value.ClaimCount, OccurredAt: value.OccurredAt,
		})
	}
	return result, nil
}

func (a *Admin) GetStats(ctx context.Context, workspaceID, calendarID string) (StatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetStats(mergedCtx, workspaceID, calendarID)
	if err != nil {
		return StatsModel{}, err
	}
	return StatsModel(value), nil
}

func (a *Admin) ListDailyStats(
	ctx context.Context,
	workspaceID, calendarID string,
	from, until time.Time,
) ([]DailyStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListDailyStats(mergedCtx, workspaceID, calendarID, from, until)
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
