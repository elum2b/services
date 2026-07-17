package user

import "context"

type GetStatusParams struct {
	Identity Identity
	CPAID    string
}

func (u *User) GetStatus(ctx context.Context, params GetStatusParams) (*AssignmentModel, error) {

	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return nil, err
	}

	value, err := u.repository.FindAssignment(mergedCtx, scope(params.Identity, params.CPAID))
	if err != nil || value == nil {
		return nil, err
	}

	result := mapAssignment(*value)
	return &result, nil

}
