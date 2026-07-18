package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	json "github.com/goccy/go-json"

	utils "github.com/elum2b/services/internal/utils"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	sqlc "github.com/elum2b/services/payment/sqlc"
)

type RefundCreateParams struct {
	WorkspaceID      string
	OrderID          uint64
	AttemptID        uint64
	ProviderCode     string
	ProviderRefundID *string
	AmountMinor      uint64
	AssetCode        string
	Status           string
	Reason           *string
}

type IdempotentRefundCreateParams struct {
	WorkspaceID    string
	OrderID        uint64
	AttemptID      uint64
	ProviderCode   string
	IdempotencyKey string
	AmountMinor    uint64
	AssetCode      string
	Reason         *string
}

type IdempotentRefund struct {
	ID               uint64
	Status           string
	ProviderRefundID *string
}

type RefundFinalizeParams struct {
	WorkspaceID      string
	RefundID         uint64
	ProviderRefundID string
	Status           string
	Reason           string
}

type ProviderRefundParams struct {
	WorkspaceID       string
	ProviderCode      string
	ProviderPaymentID string
	ProviderRefundID  string
	Reason            *string
	Event             EventCreateParams
}

type ProviderRefundResult struct {
	RefundID    uint64
	OrderID     uint64
	AttemptID   uint64
	AlreadyDone bool
}

type paymentRefundedCallbackPayload struct {
	OrderID           uint64                  `json:"order_id"`
	AttemptID         uint64                  `json:"attempt_id"`
	FulfillmentID     uint64                  `json:"fulfillment_id"`
	RefundID          uint64                  `json:"refund_id"`
	WorkspaceID       string                  `json:"workspace_id"`
	AppID             int64                   `json:"app_id"`
	PlatformID        int64                   `json:"platform_id"`
	PlatformUserID    string                  `json:"platform_user_id"`
	ProductID         string                  `json:"product_id"`
	Quantity          uint64                  `json:"quantity"`
	ProviderCode      string                  `json:"provider_code"`
	ProviderPaymentID string                  `json:"provider_payment_id,omitempty"`
	AssetCode         string                  `json:"asset_code"`
	AmountMinor       uint64                  `json:"amount_minor"`
	Reason            string                  `json:"reason,omitempty"`
	Rewards           []paymentCallbackReward `json:"rewards"`
}

func (r *PaymentRepository) ApplyProviderRefund(
	ctx context.Context,
	params ProviderRefundParams,
) (ProviderRefundResult, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return ProviderRefundResult{}, err
	}
	params.WorkspaceID = workspaceID
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	params.ProviderRefundID = strings.TrimSpace(params.ProviderRefundID)
	if params.ProviderCode == "" || params.ProviderPaymentID == "" || params.ProviderRefundID == "" {
		return ProviderRefundResult{}, ErrAttemptFieldsInvalid
	}

	var result ProviderRefundResult

	err = r.inTransaction(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttemptByProviderPaymentID(
			ctx,
			sqlc.LockPaymentAttemptByProviderPaymentIDParams{
				WorkspaceID:  params.WorkspaceID,
				ProviderCode: params.ProviderCode,
				ProviderPaymentID: sql.NullString{
					String: params.ProviderPaymentID,
					Valid:  params.ProviderPaymentID != "",
				},
			},
		)
		if err != nil {
			return err
		}

		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}
		if order.WorkspaceID != params.WorkspaceID {
			return sql.ErrNoRows
		}

		result.OrderID = uint64(order.ID)
		result.AttemptID = uint64(attempt.ID)
		if order.Status == sqlc.PaymentOrderStatusRefunded {
			existing, err := txRepo.q.GetSucceededRefundForOrder(
				ctx,
				sqlc.GetSucceededRefundForOrderParams{
					WorkspaceID: workspaceID,
					OrderID:     order.ID,
				},
			)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrOrderStateInvalid
			}
			if err != nil {
				return err
			}
			if existing.AttemptID != attempt.ID ||
				existing.ProviderCode != params.ProviderCode ||
				!existing.ProviderRefundID.Valid ||
				existing.ProviderRefundID.String != params.ProviderRefundID ||
				existing.AmountMinor != attempt.AmountMinor ||
				existing.AssetCode != attempt.AssetCode {
				return ErrPaymentMismatch
			}

			result.RefundID = uint64(existing.ID)
			result.AlreadyDone = true

			return nil
		}
		if order.Status != sqlc.PaymentOrderStatusPaid &&
			order.Status != sqlc.PaymentOrderStatusFulfilled {
			return ErrOrderStateInvalid
		}

		refundID, err := txRepo.q.AdminCreateRefund(ctx, sqlc.AdminCreateRefundParams{
			WorkspaceID:      order.WorkspaceID,
			OrderID:          order.ID,
			AttemptID:        attempt.ID,
			ProviderCode:     params.ProviderCode,
			ProviderRefundID: sql.NullString{String: params.ProviderRefundID, Valid: params.ProviderRefundID != ""},
			AmountMinor:      attempt.AmountMinor,
			AssetCode:        attempt.AssetCode,
			Status:           sqlc.PaymentRefundStatusSucceeded,
			Reason: sqlwrap.NullFromPtr(params.Reason, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPaymentMismatch
			}

			return err
		}
		result.RefundID = uint64(refundID)

		if err := txRepo.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
			Status: sqlc.PaymentAttemptStatusRefunded,
			ID:     attempt.ID,
		}); err != nil {
			return err
		}
		if rows, err := txRepo.q.MarkOrderRefunded(ctx, order.ID); err != nil {
			return err
		} else if rows == 0 {
			return ErrOrderStateInvalid
		}
		if _, err := txRepo.q.MarkFulfillmentRevokedForOrder(ctx, order.ID); err != nil {
			return err
		}
		fulfillment, err := txRepo.q.GetFulfillmentForOrder(ctx, order.ID)
		if err != nil {
			return err
		}
		if _, err := txRepo.q.DecrementProductLimitCountersForRefund(ctx, order.ID); err != nil {
			return err
		}

		event := params.Event
		event.WorkspaceID = order.WorkspaceID
		event.AttemptID = utils.Ref(int64(attempt.ID))
		event.OrderID = utils.Ref(int64(order.ID))
		if _, err := txRepo.CreateEvent(ctx, event); err != nil {
			return err
		}
		return txRepo.enqueuePaymentRefundedCallback(
			ctx,
			order,
			attempt,
			uint64(fulfillment.ID),
			result.RefundID,
			params.Reason,
		)
	})

	return result, err
}

func (r *PaymentRepository) enqueuePaymentRefundedCallback(
	ctx context.Context,
	order sqlc.PaymentOrder,
	attempt sqlc.PaymentAttempt,
	fulfillmentID uint64,
	refundID uint64,
	reason *string,
) error {
	items, err := r.q.GetFulfillmentItemsForOrder(ctx, order.ID)
	if err != nil {
		return err
	}
	payload := paymentRefundedCallbackPayload{
		OrderID:        uint64(order.ID),
		AttemptID:      uint64(attempt.ID),
		FulfillmentID:  fulfillmentID,
		RefundID:       refundID,
		WorkspaceID:    order.WorkspaceID,
		AppID:          order.AppID,
		PlatformID:     order.PlatformID,
		PlatformUserID: order.PlatformUserID,
		ProductID:      order.ProductID,
		Quantity:       uint64(order.Quantity),
		ProviderCode:   attempt.ProviderCode,
		AssetCode:      attempt.AssetCode,
		AmountMinor:    uint64(attempt.AmountMinor),
		Rewards:        make([]paymentCallbackReward, 0, len(items)),
	}
	for _, item := range items {
		payload.Rewards = append(payload.Rewards, paymentCallbackReward{
			Key: item.ItemID, Type: string(item.RewardType), Quantity: item.Quantity,
			Scale: uint16(item.Scale),
			Unit:  orderDurationUnitPtr(item.DurationUnit),
		})
	}
	if attempt.ProviderPaymentID.Valid {
		payload.ProviderPaymentID = attempt.ProviderPaymentID.String
	}
	if reason != nil {
		payload.Reason = *reason
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("payment.order.refunded:%d", order.ID)
	_, err = r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:        order.WorkspaceID,
		SourceService:      "payment",
		EventType:          "payment.order.refunded",
		EventKey:           eventKey,
		IdempotencyKey:     eventKey,
		Payload:            raw,
		PayloadContentType: callbackutil.JSONContentType,
	})
	return err
}

func (r *PaymentRepository) GetAttempt(ctx context.Context, id uint64) (Attempt, error) {
	attempt, err := r.q.AdminGetPaymentAttempt(ctx, int64(id))
	if err != nil {
		return Attempt{}, err
	}
	return mapAttempt(attempt), nil
}

func (r *PaymentRepository) GetRefundAttempt(ctx context.Context, workspaceID string, orderID uint64) (Attempt, error) {
	attempts, err := r.q.AdminListPaymentAttempts(ctx, sqlc.AdminListPaymentAttemptsParams{
		WorkspaceID: workspaceID,
		Column2:     int64(orderID),
		OrderID:     int64(orderID),
		Column6:     string(sqlc.PaymentAttemptStatusSucceeded),
		Status:      sqlc.PaymentAttemptStatusSucceeded,
		Limit:       1,
	})
	if err != nil {
		return Attempt{}, err
	}
	if len(attempts) == 0 {
		return Attempt{}, sql.ErrNoRows
	}
	return mapAttempt(attempts[0]), nil
}

func (r *PaymentRepository) CreateRefund(ctx context.Context, params RefundCreateParams) (uint64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}
	if params.OrderID == 0 || params.OrderID > math.MaxInt64 ||
		params.AttemptID == 0 || params.AttemptID > math.MaxInt64 ||
		params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 ||
		strings.TrimSpace(params.ProviderCode) == "" || strings.TrimSpace(params.AssetCode) == "" {
		return 0, ErrAttemptFieldsInvalid
	}
	status := params.Status
	if status == "" {
		status = string(sqlc.PaymentRefundStatusCreated)
	}
	if !validRefundStatus(status) {
		return 0, ErrOrderStateInvalid
	}
	if status == string(sqlc.PaymentRefundStatusSucceeded) {
		return 0, ErrOrderStateInvalid
	}

	var refundID uint64
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}
		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}
		if order.WorkspaceID != workspaceID || uint64(attempt.OrderID) != params.OrderID ||
			attempt.ProviderCode != params.ProviderCode || attempt.AssetCode != params.AssetCode ||
			params.AmountMinor > uint64(attempt.AmountMinor) {
			return ErrPaymentMismatch
		}
		if params.ProviderRefundID != nil && *params.ProviderRefundID != "" {
			existing, err := txRepo.q.GetRefundByProviderRefundID(
				ctx,
				sqlc.GetRefundByProviderRefundIDParams{
					WorkspaceID:  workspaceID,
					ProviderCode: params.ProviderCode,
					ProviderRefundID: sql.NullString{
						String: *params.ProviderRefundID,
						Valid:  true,
					},
				},
			)
			if err == nil {
				if existing.OrderID != order.ID || existing.AttemptID != attempt.ID ||
					existing.AmountMinor != int64(params.AmountMinor) || existing.AssetCode != params.AssetCode {
					return ErrPaymentMismatch
				}
				refundID = uint64(existing.ID)
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		if refundReservesAmount(status) {
			reserved, err := txRepo.q.SumReservedRefundAmountForAttempt(ctx, attempt.ID)
			if err != nil {
				return err
			}
			if reserved > attempt.AmountMinor-int64(params.AmountMinor) {
				return ErrPaymentMismatch
			}
		}

		id, err := txRepo.q.AdminCreateRefund(ctx, sqlc.AdminCreateRefundParams{
			WorkspaceID:  workspaceID,
			OrderID:      int64(params.OrderID),
			AttemptID:    int64(params.AttemptID),
			ProviderCode: params.ProviderCode,
			ProviderRefundID: sqlwrap.NullFromPtr(
				params.ProviderRefundID,
				func(v string) sql.NullString {
					return sql.NullString{
						String: v,
						Valid:  true,
					}
				},
			),
			AmountMinor: int64(params.AmountMinor),
			AssetCode:   params.AssetCode,
			Status:      sqlc.PaymentRefundStatus(status),
			Reason: sqlwrap.NullFromPtr(
				params.Reason,
				func(v string) sql.NullString {
					return sql.NullString{
						String: v,
						Valid:  true,
					}
				},
			),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrPaymentMismatch
			}

			return err
		}
		refundID = uint64(id)

		return nil
	})

	return refundID, err
}

func (r *PaymentRepository) CreateIdempotentRefund(
	ctx context.Context,
	params IdempotentRefundCreateParams,
) (IdempotentRefund, error) {

	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return IdempotentRefund{}, err
	}

	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.OrderID == 0 || params.OrderID > math.MaxInt64 ||
		params.AttemptID == 0 || params.AttemptID > math.MaxInt64 ||
		params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 ||
		params.ProviderCode == "" || params.IdempotencyKey == "" ||
		len(params.IdempotencyKey) > 128 || params.AssetCode == "" {
		return IdempotentRefund{}, ErrAttemptFieldsInvalid
	}

	var result IdempotentRefund
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}

		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}

		if order.WorkspaceID != workspaceID || uint64(order.ID) != params.OrderID ||
			attempt.ProviderCode != params.ProviderCode || attempt.AssetCode != params.AssetCode ||
			params.AmountMinor > uint64(attempt.AmountMinor) {
			return ErrPaymentMismatch
		}

		existing, err := txRepo.q.GetRefundByIdempotencyKey(
			ctx,
			sqlc.GetRefundByIdempotencyKeyParams{
				WorkspaceID: workspaceID,
				IdempotencyKey: sql.NullString{
					String: params.IdempotencyKey,
					Valid:  true,
				},
			},
		)
		if err == nil {
			if existing.OrderID != order.ID || existing.AttemptID != attempt.ID ||
				existing.ProviderCode != params.ProviderCode ||
				existing.AmountMinor != int64(params.AmountMinor) ||
				existing.AssetCode != params.AssetCode ||
				!sameRefundReason(existing.Reason, params.Reason) {
				return ErrPaymentMismatch
			}

			result = IdempotentRefund{
				ID:               uint64(existing.ID),
				Status:           string(existing.Status),
				ProviderRefundID: sqlwrap.NullStringPtr(existing.ProviderRefundID),
			}

			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		reserved, err := txRepo.q.SumReservedRefundAmountForAttempt(ctx, attempt.ID)
		if err != nil {
			return err
		}
		if reserved > attempt.AmountMinor-int64(params.AmountMinor) {
			return ErrPaymentMismatch
		}

		created, err := txRepo.q.CreateIdempotentRefund(ctx, sqlc.CreateIdempotentRefundParams{
			WorkspaceID:  workspaceID,
			OrderID:      int64(params.OrderID),
			AttemptID:    int64(params.AttemptID),
			ProviderCode: params.ProviderCode,
			IdempotencyKey: sql.NullString{
				String: params.IdempotencyKey,
				Valid:  true,
			},
			AmountMinor: int64(params.AmountMinor),
			AssetCode:   params.AssetCode,
			Status:      sqlc.PaymentRefundStatusPending,
			Reason: sqlwrap.NullFromPtr(
				params.Reason,
				func(value string) sql.NullString {
					return sql.NullString{
						String: value,
						Valid:  true,
					}
				},
			),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || isUniqueViolation(err) {
				return ErrPaymentMismatch
			}

			return err
		}

		result = IdempotentRefund{
			ID:               uint64(created.ID),
			Status:           string(created.Status),
			ProviderRefundID: sqlwrap.NullStringPtr(created.ProviderRefundID),
		}

		return nil
	})

	return result, err

}

func sameRefundReason(stored sql.NullString, received *string) bool {

	if received == nil {
		return !stored.Valid
	}

	return stored.Valid && stored.String == *received

}

func (r *PaymentRepository) FinalizeRefund(ctx context.Context, params RefundFinalizeParams) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	if params.RefundID == 0 || params.RefundID > math.MaxInt64 || !validRefundStatus(params.Status) {
		return ErrOrderStateInvalid
	}

	return r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		refundSnapshot, err := txRepo.q.AdminGetRefund(ctx, int64(params.RefundID))
		if err != nil {
			return err
		}

		attempt, err := txRepo.q.LockPaymentAttempt(ctx, refundSnapshot.AttemptID)
		if err != nil {
			return err
		}

		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}

		refund, err := txRepo.q.LockPaymentRefund(ctx, int64(params.RefundID))
		if err != nil {
			return err
		}
		if order.WorkspaceID != workspaceID ||
			refund.WorkspaceID != workspaceID ||
			attempt.OrderID != order.ID ||
			refund.AttemptID != attempt.ID ||
			refund.OrderID != order.ID {
			return sql.ErrNoRows
		}
		targetStatus := sqlc.PaymentRefundStatus(params.Status)
		if !validRefundStatusTransition(refund.Status, targetStatus) {
			return ErrOrderStateInvalid
		}

		if params.ProviderRefundID != "" {
			if _, err := txRepo.q.AdminSetRefundProviderID(ctx, sqlc.AdminSetRefundProviderIDParams{
				ID: int64(params.RefundID),
				ProviderRefundID: sql.NullString{
					String: params.ProviderRefundID,
					Valid:  true,
				},
			}); err != nil {
				return err
			}
		}
		if _, err := txRepo.q.AdminUpdateRefundStatus(ctx, sqlc.AdminUpdateRefundStatusParams{
			ID:     int64(params.RefundID),
			Status: sqlc.PaymentRefundStatus(params.Status),
			Reason: sql.NullString{
				String: params.Reason,
				Valid:  params.Reason != "",
			},
		}); err != nil {
			return err
		}
		if params.Status != string(sqlc.PaymentRefundStatusSucceeded) {
			return nil
		}

		succeeded, err := txRepo.q.SumSucceededRefundAmountForAttempt(ctx, attempt.ID)
		if err != nil {
			return err
		}
		if succeeded > attempt.AmountMinor {
			return ErrPaymentMismatch
		}
		if succeeded < attempt.AmountMinor || order.Status == sqlc.PaymentOrderStatusRefunded {
			return nil
		}
		if order.Status != sqlc.PaymentOrderStatusPaid && order.Status != sqlc.PaymentOrderStatusFulfilled {
			return ErrOrderStateInvalid
		}

		if err := txRepo.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
			Status: sqlc.PaymentAttemptStatusRefunded,
			ID:     attempt.ID,
		}); err != nil {
			return err
		}
		if rows, err := txRepo.q.MarkOrderRefunded(ctx, order.ID); err != nil {
			return err
		} else if rows != 1 {
			return ErrOrderStateInvalid
		}
		if rows, err := txRepo.q.MarkFulfillmentRevokedForOrder(ctx, order.ID); err != nil {
			return err
		} else if rows != 1 {
			return ErrOrderStateInvalid
		}
		fulfillment, err := txRepo.q.GetFulfillmentForOrder(ctx, order.ID)
		if err != nil {
			return err
		}
		if _, err := txRepo.q.DecrementProductLimitCountersForRefund(ctx, order.ID); err != nil {
			return err
		}

		return txRepo.enqueuePaymentRefundedCallback(
			ctx,
			order,
			attempt,
			uint64(fulfillment.ID),
			params.RefundID,
			nilIfEmpty(params.Reason),
		)
	})
}

func refundReservesAmount(status string) bool {
	switch sqlc.PaymentRefundStatus(status) {
	case sqlc.PaymentRefundStatusCreated,
		sqlc.PaymentRefundStatusPending:
		return true
	default:
		return false
	}
}

func validRefundStatusTransition(current, target sqlc.PaymentRefundStatus) bool {
	if current == target {
		return true
	}

	switch current {
	case sqlc.PaymentRefundStatusCreated:
		return target == sqlc.PaymentRefundStatusPending ||
			target == sqlc.PaymentRefundStatusSucceeded ||
			target == sqlc.PaymentRefundStatusCanceled ||
			target == sqlc.PaymentRefundStatusFailed
	case sqlc.PaymentRefundStatusPending:
		return target == sqlc.PaymentRefundStatusSucceeded ||
			target == sqlc.PaymentRefundStatusCanceled ||
			target == sqlc.PaymentRefundStatusFailed
	default:
		return false
	}
}

func (r *PaymentRepository) AdminUpdateRefundStatus(
	ctx context.Context,
	workspaceID string,
	id uint64,
	status string,
	reason string,
) (int64, error) {
	if id == 0 || id > math.MaxInt64 || !validRefundStatus(status) {
		return 0, ErrOrderStateInvalid
	}
	if err := r.FinalizeRefund(ctx, RefundFinalizeParams{
		WorkspaceID: workspaceID,
		RefundID:    id,
		Status:      status,
		Reason:      reason,
	}); err != nil {
		return 0, err
	}

	return 1, nil
}

func validRefundStatus(status string) bool {
	switch sqlc.PaymentRefundStatus(status) {
	case sqlc.PaymentRefundStatusCreated,
		sqlc.PaymentRefundStatusPending,
		sqlc.PaymentRefundStatusSucceeded,
		sqlc.PaymentRefundStatusCanceled,
		sqlc.PaymentRefundStatusFailed:
		return true
	default:
		return false
	}
}

func (r *PaymentRepository) UpdateRefundStatus(
	ctx context.Context,
	id uint64,
	status string,
	reason string,
) (int64, error) {
	return r.q.AdminUpdateRefundStatus(ctx, sqlc.AdminUpdateRefundStatusParams{
		ID:     int64(id),
		Status: sqlc.PaymentRefundStatus(status),
		Reason: sqlwrap.NullFromPtr(nilIfEmpty(reason), func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
	})
}

func (r *PaymentRepository) SetRefundProviderID(
	ctx context.Context,
	id uint64,
	providerRefundID string,
) (int64, error) {
	return r.q.AdminSetRefundProviderID(ctx, sqlc.AdminSetRefundProviderIDParams{
		ID: int64(id),
		ProviderRefundID: sqlwrap.NullFromPtr(nilIfEmpty(providerRefundID), func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
	})
}

func (r *PaymentRepository) UpdateAttemptStatus(ctx context.Context, id uint64, status string) error {
	return r.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
		ID:     int64(id),
		Status: sqlc.PaymentAttemptStatus(status),
	})
}

func (r *PaymentRepository) UpdateOrderStatus(
	ctx context.Context,
	workspaceID string,
	id uint64,
	status string,
) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}

	var rows int64
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		order, err := txRepo.q.LockPaymentOrder(ctx, int64(id))
		if err != nil {
			return err
		}
		if order.WorkspaceID != workspaceID {
			return sql.ErrNoRows
		}
		targetStatus := sqlc.PaymentOrderStatus(status)
		if !validOrderStatusTransition(order.Status, targetStatus) {
			return ErrOrderStateInvalid
		}

		shouldRelease := (targetStatus == sqlc.PaymentOrderStatusCanceled ||
			targetStatus == sqlc.PaymentOrderStatusExpired ||
			targetStatus == sqlc.PaymentOrderStatusFailed) &&
			(order.Status == sqlc.PaymentOrderStatusDraft ||
				order.Status == sqlc.PaymentOrderStatusPendingPayment)
		if shouldRelease {
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
		}

		rows, err = txRepo.q.AdminUpdateOrderStatus(ctx, sqlc.AdminUpdateOrderStatusParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
			Status:      sqlc.PaymentOrderStatus(status),
			Column2:     status,
			Column3:     status,
			Column4:     status,
		})
		return err
	})

	return rows, err
}

func validOrderStatusTransition(current, target sqlc.PaymentOrderStatus) bool {
	if current == target {
		return true
	}

	switch current {
	case sqlc.PaymentOrderStatusDraft:
		return target == sqlc.PaymentOrderStatusPendingPayment ||
			target == sqlc.PaymentOrderStatusCanceled ||
			target == sqlc.PaymentOrderStatusExpired ||
			target == sqlc.PaymentOrderStatusFailed
	case sqlc.PaymentOrderStatusPendingPayment:
		return target == sqlc.PaymentOrderStatusCanceled ||
			target == sqlc.PaymentOrderStatusExpired ||
			target == sqlc.PaymentOrderStatusFailed
	default:
		return false
	}
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return utils.Ref(value)
}
