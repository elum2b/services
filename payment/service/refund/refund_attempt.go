package refund

import (
	"context"
	"database/sql"
	"errors"

	"github.com/elum2b/services/payment/repository"
)

func (a *Refund) refundAttempt(
	ctx context.Context,
	order repository.Order,
	attemptID uint64,
) (repository.Attempt, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	if attemptID != 0 {
		attempt, err := a.repository.GetAttempt(ctx, attemptID)
		if err != nil {
			return repository.Attempt{}, err
		}
		if attempt.OrderID != order.ID {
			return repository.Attempt{}, sql.ErrNoRows
		}
		return attempt, nil
	}

	attempt, err := a.repository.GetRefundAttempt(ctx, order.WorkspaceID, order.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.Attempt{}, ErrAttemptRequired
	}
	return attempt, err
}
