package user

import "context"

type GetCodeParams struct {
	Identity Identity
	CPAID    string
}

type GetCodeResult struct {
	Assignment    AssignmentModel `json:"assignment"`
	Rewards       []RewardModel   `json:"rewards,omitempty"`
	AlreadyIssued bool            `json:"already_issued"`
}

func (u *User) GetCode(ctx context.Context, params GetCodeParams) (GetCodeResult, error) {

	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return GetCodeResult{}, err
	}

	result, err := u.repository.Issue(mergedCtx, scope(params.Identity, params.CPAID))
	if err != nil {
		return GetCodeResult{}, err
	}

	return GetCodeResult{
		Assignment:    mapAssignment(result.Assignment),
		Rewards:       mapRewards(result.Rewards),
		AlreadyIssued: result.AlreadyIssued,
	}, nil

}
