package admin

import (
	"context"

	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/user"
)

type CompleteParams struct {
	Identity user.Identity
	CPAID    string
}

type CompleteResult struct {
	Assignment  user.AssignmentModel `json:"assignment"`
	Rewards     []user.RewardModel   `json:"rewards,omitempty"`
	AlreadyDone bool                 `json:"already_done"`
}

func (a *Admin) Complete(ctx context.Context, params CompleteParams) (CompleteResult, error) {

	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()

	result, err := a.repository.Complete(mergedCtx, repository.UserScope{
		WorkspaceID:    params.Identity.WorkspaceID,
		CPAID:          params.CPAID,
		AppID:          params.Identity.AppID,
		PlatformID:     params.Identity.PlatformID,
		PlatformUserID: params.Identity.PlatformUserID,
	})
	if err != nil {
		return CompleteResult{}, err
	}

	assignment := user.AssignmentModel{
		ID:          result.Assignment.ID,
		CPAID:       result.Assignment.CPAID,
		Code:        result.Assignment.Code,
		CodeMode:    result.Assignment.CodeMode,
		Status:      result.Assignment.Status,
		IssuedAt:    result.Assignment.IssuedAt,
		CompletedAt: result.Assignment.CompletedAt,
	}

	rewards := mapOffer(repository.Offer{}, nil, result.Rewards).Rewards
	return CompleteResult{
		Assignment:  assignment,
		Rewards:     rewards,
		AlreadyDone: result.AlreadyDone,
	}, nil

}
