package admin

import (
	"context"

	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/user"
)

type UpsertRewardParams struct {
	WorkspaceID string
	CPAID       string
	Key         string
	Type        string
	Quantity    int64
	Scale       uint16
	Unit        *string
}

func (a *Admin) UpsertReward(ctx context.Context, params UpsertRewardParams) error {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.UpsertReward(mergedCtx, repository.Reward{
		WorkspaceID: params.WorkspaceID,
		CPAID:       params.CPAID,
		Key:         params.Key,
		Type:        params.Type,
		Quantity:    params.Quantity,
		Scale:       params.Scale,
		Unit:        params.Unit,
	})

}

func (a *Admin) ListRewards(ctx context.Context, workspaceID, cpaID string) ([]user.RewardModel, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	values, err := a.repository.ListRewards(mergedCtx, workspaceID, cpaID)
	if err != nil {
		return nil, err
	}

	result := mapOffer(repository.Offer{}, nil, values)
	return result.Rewards, nil

}

func (a *Admin) DeleteReward(ctx context.Context, workspaceID, cpaID, rewardKey string) (int64, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	return a.repository.DeleteReward(mergedCtx, workspaceID, cpaID, rewardKey)

}
