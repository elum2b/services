package user

import (
	"context"
	"strings"
	"time"

	"github.com/elum2b/services/calendar/repository"
)

type RecordParams struct {
	Identity    Identity
	CalendarRef string
	OperationID string
	Now         time.Time
}

func (u *User) Record(ctx context.Context, params RecordParams) (RecordResult, error) {
	mergedCtx, cancel := u.withContext(ctx)
	defer cancel()

	if err := params.Identity.Validate(); err != nil {
		return RecordResult{}, err
	}
	if strings.TrimSpace(params.CalendarRef) == "" || strings.TrimSpace(params.OperationID) == "" {
		return RecordResult{}, ErrRecordParamsRequired
	}

	value, err := u.repository.Record(mergedCtx, repository.RecordParams{
		Identity: repositoryIdentity(params.Identity), CalendarRef: params.CalendarRef,
		OperationID: params.OperationID, Now: params.Now,
	})
	if err != nil {
		return RecordResult{}, err
	}

	result := mapRecord(value)
	hideFutureRewardSteps(&result)

	return result, nil
}
