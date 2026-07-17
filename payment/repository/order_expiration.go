package repository

import (
	"context"
	"time"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (r *PaymentRepository) ExpireStaleOrders(
	ctx context.Context,
	now time.Time,
	maxAge time.Duration,
	batchSize int32,
	protectUnboundPlatega bool,
) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if maxAge <= 0 {
		maxAge = time.Hour
	}
	if batchSize <= 0 {
		batchSize = 100
	}

	expired := 0
	err := r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		orders, err := txRepo.q.LockStalePaymentOrders(ctx, paymentsqlc.LockStalePaymentOrdersParams{
			ProtectUnboundPlatega: protectUnboundPlatega,
			NowAt:                 now,
			CreatedBefore:         now.Add(-maxAge),
			BatchSize:             batchSize,
		})
		if err != nil {
			return err
		}

		for _, order := range orders {
			if _, err := txRepo.q.ExpireActivePaymentAttemptsForOrder(
				ctx,
				paymentsqlc.ExpireActivePaymentAttemptsForOrderParams{
					WorkspaceID: order.WorkspaceID,
					OrderID:     order.ID,
				},
			); err != nil {
				return err
			}

			if err := txRepo.releaseOrderLimits(ctx, order); err != nil {
				return err
			}

			if order.PurchaseKeyID.Valid {
				rows, err := txRepo.q.ReleasePurchaseKeyReservation(ctx, order.PurchaseKeyID.Int64)
				if err != nil {
					return err
				}
				if rows != 1 {
					return ErrOrderStateInvalid
				}
			}

			rows, err := txRepo.q.AdminUpdateOrderStatus(ctx, paymentsqlc.AdminUpdateOrderStatusParams{
				Status:      paymentsqlc.PaymentOrderStatusExpired,
				Column2:     string(paymentsqlc.PaymentOrderStatusExpired),
				Column3:     string(paymentsqlc.PaymentOrderStatusExpired),
				Column4:     string(paymentsqlc.PaymentOrderStatusExpired),
				WorkspaceID: order.WorkspaceID,
				ID:          order.ID,
			})
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrOrderStateInvalid
			}

			expired++
		}

		return nil
	})

	return expired, err
}
