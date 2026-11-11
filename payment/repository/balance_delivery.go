package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

// lockBalanceDelivery follows the same lock order as expiration and provider completion.
func (r *PaymentRepository) lockBalanceDelivery(ctx context.Context, eventID uint64) (paymentsqlc.PaymentBalancePurchase, paymentsqlc.PaymentOrder, error) {
	row, err := r.q.GetBalancePurchaseByEvent(ctx, int64(eventID))
	if err != nil {
		return row, paymentsqlc.PaymentOrder{}, err
	}
	if _, err := r.q.LockPaymentAttempt(ctx, row.AttemptID); err != nil {
		return row, paymentsqlc.PaymentOrder{}, err
	}
	order, err := r.q.LockPaymentOrder(ctx, row.OrderID)
	if err != nil {
		return row, order, err
	}
	row, err = r.q.GetBalancePurchaseByEvent(ctx, int64(eventID))
	return row, order, err
}

// BeginBalanceCharge returns nil when the event can be acknowledged without calling the game.
func (r *PaymentRepository) BeginBalanceCharge(ctx context.Context, eventID uint64, handlers map[string]BalanceChargeHandler) (*BalanceChargeRequest, BalanceChargeHandler, error) {
	var request *BalanceChargeRequest
	var handler BalanceChargeHandler
	err := r.WithTx(ctx, func(ctx context.Context, tx *PaymentRepository) error {
		row, order, err := tx.lockBalanceDelivery(ctx, eventID)
		if err != nil || row.Outcome != "" {
			return err
		}
		if order.Status != paymentsqlc.PaymentOrderStatusPendingPayment {
			return ErrOrderStateInvalid
		}
		handler = handlers[row.HandlerKey]
		if handler == nil {
			return ErrBalanceUnavailable
		}

		if !row.AdmittedAt.Valid {
			owned, err := tx.q.HasOwnedBalanceReservations(ctx, order.ID)
			if err != nil {
				return err
			}
			if !owned {
				return tx.rejectBalanceCharge(ctx, row, order, "limit_reservation_lost")
			}
		}
		if err := tx.q.RecordBalanceDelivery(ctx, order.ID); err != nil {
			return err
		}

		var saved BalanceChargeRequest
		if err := json.Unmarshal(row.Snapshot, &saved); err != nil {
			return err
		}
		request = &saved
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return request, handler, nil
}

func (r *PaymentRepository) FlagBalanceDeliveryConflict(ctx context.Context, eventID uint64) error {
	return r.q.FlagBalanceResultConflict(ctx, int64(eventID))
}

func validBalanceRejectionReason(reason string) bool {
	if len(reason) == 0 || len(reason) > 128 {
		return false
	}
	for _, c := range reason {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func (r *PaymentRepository) FinishBalanceCharge(ctx context.Context, eventID uint64, result BalanceChargeResult) error {
	if (result.Outcome != BalanceChargeSucceeded || result.Reason != "") &&
		(result.Outcome != BalanceChargeRejected || !validBalanceRejectionReason(result.Reason)) {
		if err := r.FlagBalanceDeliveryConflict(ctx, eventID); err != nil {
			return err
		}
		return ErrBalanceResultInvalid
	}

	var conflict bool
	err := r.WithTx(ctx, func(ctx context.Context, tx *PaymentRepository) error {
		row, order, err := tx.lockBalanceDelivery(ctx, eventID)
		if err != nil {
			return err
		}
		if !row.AdmittedAt.Valid {
			return ErrOrderStateInvalid
		}
		if row.Outcome != "" {
			if row.Outcome == string(result.Outcome) && row.Reason == result.Reason {
				return nil
			}
			conflict = true
			return tx.FlagBalanceDeliveryConflict(ctx, eventID)
		}
		if order.Status != paymentsqlc.PaymentOrderStatusPendingPayment {
			return ErrOrderStateInvalid
		}
		if result.Outcome == BalanceChargeRejected {
			return tx.rejectBalanceCharge(ctx, row, order, result.Reason)
		}
		if order.GlobalWindowStartSnapshot.Valid || order.UserWindowStartSnapshot.Valid {
			owned, err := tx.q.ConsumeOrderLimitReservations(ctx, order.ID)
			if err != nil {
				return err
			}
			if !owned {
				return ErrBalanceInProgress
			}
		}
		if err := tx.q.UpdatePaymentAttemptStatus(ctx, paymentsqlc.UpdatePaymentAttemptStatusParams{
			ID: row.AttemptID, Status: paymentsqlc.PaymentAttemptStatusSucceeded,
		}); err != nil {
			return err
		}
		if _, err := tx.q.MarkOrderPaidAndIndex(ctx, order.ID); err != nil {
			return err
		}
		if _, err := tx.q.CompleteFulfillmentFromOrder(ctx, paymentsqlc.CompleteFulfillmentFromOrderParams{
			OrderID: order.ID, AttemptID: row.AttemptID, Status: paymentsqlc.PaymentFulfillmentStatusSucceeded,
			InternalUserID: order.InternalUserID,
		}); err != nil {
			return err
		}
		// The game has already granted these items. There is no ordinary fulfillment callback.
		return tx.q.SetBalancePurchaseOutcome(ctx, paymentsqlc.SetBalancePurchaseOutcomeParams{
			OrderID: order.ID, Outcome: string(BalanceChargeSucceeded),
		})
	})
	if err == nil && conflict {
		return ErrBalanceResultInvalid
	}
	return err
}

func (r *PaymentRepository) rejectBalanceCharge(ctx context.Context, row paymentsqlc.PaymentBalancePurchase, order paymentsqlc.PaymentOrder, reason string) error {
	if err := r.releaseOrderLimits(ctx, order); err != nil {
		return err
	}
	if err := r.q.UpdatePaymentAttemptStatus(ctx, paymentsqlc.UpdatePaymentAttemptStatusParams{
		ID: row.AttemptID, Status: paymentsqlc.PaymentAttemptStatusFailed,
	}); err != nil {
		return err
	}
	status := string(paymentsqlc.PaymentOrderStatusFailed)
	if _, err := r.q.AdminUpdateOrderStatus(ctx, paymentsqlc.AdminUpdateOrderStatusParams{
		ID: order.ID, WorkspaceID: order.WorkspaceID, Status: paymentsqlc.PaymentOrderStatusFailed,
		Column2: status, Column3: status, Column4: status,
	}); err != nil {
		return err
	}
	return r.q.SetBalancePurchaseOutcome(ctx, paymentsqlc.SetBalancePurchaseOutcomeParams{
		OrderID: order.ID, Outcome: string(BalanceChargeRejected), Reason: reason,
	})
}

func (r *PaymentRepository) closeQueuedBalancePurchase(ctx context.Context, orderID int64, reason string, status paymentsqlc.PaymentAttemptStatus) error {
	id, err := r.q.CloseQueuedBalancePurchase(ctx, paymentsqlc.CloseQueuedBalancePurchaseParams{OrderID: orderID, Reason: reason})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.q.UpdatePaymentAttemptStatus(ctx, paymentsqlc.UpdatePaymentAttemptStatusParams{ID: id, Status: status})
}

type BalanceDeliveryParams struct {
	WorkspaceID string
	OrderID     uint64
}

type BalanceDelivery struct {
	BalancePurchase
	OperationKey   string     `json:"operation_key"`
	HandlerKey     string     `json:"handler_key"`
	ResultConflict bool       `json:"result_conflict"`
	AdmittedAt     *time.Time `json:"admitted_at,omitempty"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	AttemptCount   uint64     `json:"attempt_count"`
	LastError      *string    `json:"last_error,omitempty"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

func (r *PaymentRepository) getBalanceDeliveryRow(ctx context.Context, params BalanceDeliveryParams) (paymentsqlc.PaymentBalancePurchase, error) {
	if _, err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return paymentsqlc.PaymentBalancePurchase{}, err
	}
	if params.OrderID == 0 || params.OrderID > math.MaxInt64 {
		return paymentsqlc.PaymentBalancePurchase{}, ErrBalanceRequestInvalid
	}
	row, err := r.q.GetBalancePurchaseForOrder(ctx, int64(params.OrderID))
	if err == nil && row.WorkspaceID != params.WorkspaceID {
		return paymentsqlc.PaymentBalancePurchase{}, sql.ErrNoRows
	}
	return row, err
}

func (r *PaymentRepository) GetBalanceDelivery(ctx context.Context, params BalanceDeliveryParams) (BalanceDelivery, error) {
	row, err := r.getBalanceDeliveryRow(ctx, params)
	if err != nil {
		return BalanceDelivery{}, err
	}
	event, err := r.balanceCallbacks.GetEvent(ctx, row.WorkspaceID, uint64(row.EventID))
	if err != nil {
		return BalanceDelivery{}, err
	}
	delivery := BalanceDelivery{
		BalancePurchase: mapBalancePurchase(row), OperationKey: event.EventKey, HandlerKey: row.HandlerKey,
		ResultConflict: row.ResultConflict, AttemptCount: uint64(row.DeliveryAttemptCount), LastError: event.LastError,
		NextAttemptAt: event.NextAttemptAt, DeliveredAt: event.DeliveredAt,
	}
	if row.AdmittedAt.Valid {
		delivery.AdmittedAt = &row.AdmittedAt.Time
	}
	if row.LastDeliveryAt.Valid {
		delivery.LastDeliveryAt = &row.LastDeliveryAt.Time
	}
	return delivery, nil
}

func (r *PaymentRepository) RetryBalanceDelivery(ctx context.Context, params BalanceDeliveryParams) (bool, error) {
	row, err := r.getBalanceDeliveryRow(ctx, params)
	if err != nil {
		return false, err
	}
	if row.Outcome != "" {
		return false, nil
	}
	count, err := r.balanceCallbacks.RetryAvailableEventNow(ctx, row.WorkspaceID, uint64(row.EventID))
	return count == 1, err
}
