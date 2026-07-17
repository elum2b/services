package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"

	json "github.com/goccy/go-json"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlc "github.com/elum2b/services/payment/sqlc"
)

type ProviderAttemptTerminalStatus string

const (
	ProviderAttemptTerminalCanceled ProviderAttemptTerminalStatus = "canceled"
	ProviderAttemptTerminalExpired  ProviderAttemptTerminalStatus = "expired"
	ProviderAttemptTerminalFailed   ProviderAttemptTerminalStatus = "failed"
)

type ProviderAttemptTerminalParams struct {
	WorkspaceID       string
	AttemptID         uint64
	ProviderCode      string
	ProviderPaymentID string
	Status            ProviderAttemptTerminalStatus
}

type ProviderChargebackParams struct {
	WorkspaceID       string
	ProviderCode      string
	ProviderPaymentID string
	AmountMinor       uint64
	AssetCode         string
	Reason            string
}

type ProviderChargebackResult struct {
	OrderID       uint64
	AttemptID     uint64
	AlreadyDone   bool
	FulfillmentID uint64
}

type paymentChargebackedCallbackPayload struct {
	OrderID           uint64                  `json:"order_id"`
	AttemptID         uint64                  `json:"attempt_id"`
	FulfillmentID     uint64                  `json:"fulfillment_id"`
	WorkspaceID       string                  `json:"workspace_id"`
	AppID             int64                   `json:"app_id"`
	PlatformID        int64                   `json:"platform_id"`
	PlatformUserID    string                  `json:"platform_user_id"`
	ProductID         string                  `json:"product_id"`
	Quantity          uint64                  `json:"quantity"`
	ProviderCode      string                  `json:"provider_code"`
	ProviderPaymentID string                  `json:"provider_payment_id"`
	AssetCode         string                  `json:"asset_code"`
	AmountMinor       uint64                  `json:"amount_minor"`
	Reason            string                  `json:"reason,omitempty"`
	Rewards           []paymentCallbackReward `json:"rewards"`
}

func (r *PaymentRepository) FinalizeProviderAttempt(
	ctx context.Context,
	params ProviderAttemptTerminalParams,
) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	if params.AttemptID == 0 || params.AttemptID > math.MaxInt64 ||
		params.ProviderCode == "" || params.ProviderPaymentID == "" {
		return ErrAttemptFieldsInvalid
	}

	attemptStatus, orderStatus, ok := providerTerminalStatuses(params.Status)
	if !ok {
		return ErrAttemptFieldsInvalid
	}

	return r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}

		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}
		if attempt.WorkspaceID != workspaceID || order.WorkspaceID != workspaceID ||
			attempt.ProviderCode != params.ProviderCode || !attempt.ProviderPaymentID.Valid ||
			attempt.ProviderPaymentID.String != params.ProviderPaymentID {
			return ErrPaymentMismatch
		}
		if attempt.Status == attemptStatus && order.Status == orderStatus {
			return nil
		}
		if !providerAttemptCanTerminate(attempt.Status) ||
			(order.Status != sqlc.PaymentOrderStatusDraft &&
				order.Status != sqlc.PaymentOrderStatusPendingPayment) {
			return ErrOrderStateInvalid
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

		if err := txRepo.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
			Status: attemptStatus,
			ID:     attempt.ID,
		}); err != nil {
			return err
		}

		rows, err := txRepo.q.AdminUpdateOrderStatus(ctx, sqlc.AdminUpdateOrderStatusParams{
			Status:      orderStatus,
			Column2:     string(orderStatus),
			Column3:     string(orderStatus),
			Column4:     string(orderStatus),
			WorkspaceID: workspaceID,
			ID:          order.ID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrOrderStateInvalid
		}

		return nil
	})
}

func (r *PaymentRepository) ApplyProviderChargeback(
	ctx context.Context,
	params ProviderChargebackParams,
) (ProviderChargebackResult, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return ProviderChargebackResult{}, err
	}
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.ProviderCode == "" || params.ProviderPaymentID == "" || params.AssetCode == "" ||
		params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 {
		return ProviderChargebackResult{}, ErrAttemptFieldsInvalid
	}

	var result ProviderChargebackResult
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttemptByProviderPaymentID(
			ctx,
			sqlc.LockPaymentAttemptByProviderPaymentIDParams{
				WorkspaceID:  workspaceID,
				ProviderCode: params.ProviderCode,
				ProviderPaymentID: sql.NullString{
					String: params.ProviderPaymentID,
					Valid:  true,
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
		if order.WorkspaceID != workspaceID {
			return ErrPaymentMismatch
		}
		if uint64(attempt.AmountMinor) != params.AmountMinor || attempt.AssetCode != params.AssetCode {
			return ErrPaymentMismatch
		}

		result.OrderID = uint64(order.ID)
		result.AttemptID = uint64(attempt.ID)
		if order.Status == sqlc.PaymentOrderStatusChargebacked &&
			attempt.Status == sqlc.PaymentAttemptStatusChargebacked {
			result.AlreadyDone = true

			fulfillment, err := txRepo.q.GetFulfillmentForOrder(ctx, order.ID)
			if err != nil {
				return err
			}
			result.FulfillmentID = uint64(fulfillment.ID)

			return nil
		}
		if (order.Status != sqlc.PaymentOrderStatusPaid &&
			order.Status != sqlc.PaymentOrderStatusFulfilled) ||
			attempt.Status != sqlc.PaymentAttemptStatusSucceeded {
			return ErrOrderStateInvalid
		}

		fulfillment, err := txRepo.q.GetFulfillmentForOrder(ctx, order.ID)
		if err != nil {
			return err
		}
		result.FulfillmentID = uint64(fulfillment.ID)

		if err := txRepo.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
			Status: sqlc.PaymentAttemptStatusChargebacked,
			ID:     attempt.ID,
		}); err != nil {
			return err
		}
		if rows, err := txRepo.q.MarkOrderChargebacked(ctx, order.ID); err != nil {
			return err
		} else if rows != 1 {
			return ErrOrderStateInvalid
		}
		if rows, err := txRepo.q.MarkFulfillmentRevokedForOrder(ctx, order.ID); err != nil {
			return err
		} else if rows != 1 {
			return ErrOrderStateInvalid
		}
		if _, err := txRepo.q.DecrementProductLimitCountersForRefund(ctx, order.ID); err != nil {
			return err
		}

		return txRepo.enqueuePaymentChargebackedCallback(
			ctx,
			order,
			attempt,
			result.FulfillmentID,
			params.Reason,
		)
	})

	return result, err
}

func (r *PaymentRepository) enqueuePaymentChargebackedCallback(
	ctx context.Context,
	order sqlc.PaymentOrder,
	attempt sqlc.PaymentAttempt,
	fulfillmentID uint64,
	reason string,
) error {
	items, err := r.q.GetFulfillmentItemsForOrder(ctx, order.ID)
	if err != nil {
		return err
	}

	payload := paymentChargebackedCallbackPayload{
		OrderID:           uint64(order.ID),
		AttemptID:         uint64(attempt.ID),
		FulfillmentID:     fulfillmentID,
		WorkspaceID:       order.WorkspaceID,
		AppID:             order.AppID,
		PlatformID:        order.PlatformID,
		PlatformUserID:    order.PlatformUserID,
		ProductID:         order.ProductID,
		Quantity:          uint64(order.Quantity),
		ProviderCode:      attempt.ProviderCode,
		ProviderPaymentID: attempt.ProviderPaymentID.String,
		AssetCode:         attempt.AssetCode,
		AmountMinor:       uint64(attempt.AmountMinor),
		Reason:            reason,
		Rewards:           make([]paymentCallbackReward, 0, len(items)),
	}
	for _, item := range items {
		payload.Rewards = append(payload.Rewards, paymentCallbackReward{
			Key:      item.ItemID,
			Type:     string(item.RewardType),
			Quantity: item.Quantity,
			Scale:    uint16(item.Scale),
			Unit:     orderDurationUnitPtr(item.DurationUnit),
		})
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("payment.order.chargebacked:%d", order.ID)
	_, err = r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:        order.WorkspaceID,
		SourceService:      "payment",
		EventType:          "payment.order.chargebacked",
		EventKey:           eventKey,
		IdempotencyKey:     eventKey,
		Payload:            raw,
		PayloadContentType: callbackutil.JSONContentType,
	})

	return err
}

func providerTerminalStatuses(
	status ProviderAttemptTerminalStatus,
) (sqlc.PaymentAttemptStatus, sqlc.PaymentOrderStatus, bool) {
	switch status {
	case ProviderAttemptTerminalCanceled:
		return sqlc.PaymentAttemptStatusCanceled, sqlc.PaymentOrderStatusCanceled, true
	case ProviderAttemptTerminalExpired:
		return sqlc.PaymentAttemptStatusExpired, sqlc.PaymentOrderStatusExpired, true
	case ProviderAttemptTerminalFailed:
		return sqlc.PaymentAttemptStatusFailed, sqlc.PaymentOrderStatusFailed, true
	default:
		return "", "", false
	}
}

func providerAttemptCanTerminate(status sqlc.PaymentAttemptStatus) bool {
	switch status {
	case sqlc.PaymentAttemptStatusCreated,
		sqlc.PaymentAttemptStatusPending,
		sqlc.PaymentAttemptStatusRequiresAction,
		sqlc.PaymentAttemptStatusWaitingCapture:
		return true
	default:
		return false
	}
}
