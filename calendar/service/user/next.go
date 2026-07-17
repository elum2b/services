package user

import (
	"context"
	"time"
)

type NextParams struct {
	Identity    Identity
	CalendarRef string
	Locale      string
	Now         time.Time
}

func (u *User) Next(ctx context.Context, params NextParams) (RecordResult, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return RecordResult{}, err
	}

	value, err := u.repository.Next(
		mergedCtx, repositoryIdentity(params.Identity), params.CalendarRef, params.Locale, params.Now,
	)
	if err != nil {
		return RecordResult{}, err
	}
	result := mapRecord(value)
	hideFutureRewardSteps(&result)

	return result, nil
}
