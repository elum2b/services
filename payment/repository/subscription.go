package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	utils "github.com/elum2b/services/internal/utils"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

type SubscriptionUpsertParams struct {
	WorkspaceID            string
	ProviderCode           string
	ProviderSubscriptionID string
	AppID                  int64
	PlatformID             int64
	PlatformUserID         string
	InternalUserID         *int64
	ProductID              string
	OrderID                *int64
	AttemptID              *int64
	Status                 string
	CancelReason           *string
	StartedAt              time.Time
	EndedAt                *time.Time
}

type SubscriptionStatusUpdateParams struct {
	WorkspaceID            string
	ProviderCode           string
	ProviderSubscriptionID string
	Status                 string
	CancelReason           *string
	EndedAt                *time.Time
}

type SubscriptionIsActiveParams struct {
	WorkspaceID    string
	PlatformID     int64
	PlatformUserID string
	ProductID      string
	ProviderCode   string
	Now            time.Time
}

type SubscriptionRenewalParams struct {
	WorkspaceID            string
	AttemptID              uint64
	ProviderCode           string
	ProviderPaymentID      string
	ProviderSubscriptionID string
	ProviderChargeID       string
	AmountMinor            uint64
	AssetCode              string
	PeriodEnd              time.Time
}

type SubscriptionRenewalResult struct {
	OrderID        uint64
	AttemptID      uint64
	SubscriptionID uint64
	RenewalID      uint64
	AlreadyDone    bool
}

type paymentSubscriptionRenewedCallbackPayload struct {
	RenewalID              uint64                  `json:"renewal_id"`
	SubscriptionID         uint64                  `json:"subscription_id"`
	OrderID                uint64                  `json:"order_id"`
	AttemptID              uint64                  `json:"attempt_id"`
	WorkspaceID            string                  `json:"workspace_id"`
	AppID                  int64                   `json:"app_id"`
	PlatformID             int64                   `json:"platform_id"`
	PlatformUserID         string                  `json:"platform_user_id"`
	ProductID              string                  `json:"product_id"`
	Quantity               uint64                  `json:"quantity"`
	ProviderCode           string                  `json:"provider_code"`
	ProviderPaymentID      string                  `json:"provider_payment_id"`
	ProviderSubscriptionID string                  `json:"provider_subscription_id"`
	ProviderChargeID       string                  `json:"provider_charge_id"`
	AssetCode              string                  `json:"asset_code"`
	AmountMinor            uint64                  `json:"amount_minor"`
	PeriodEnd              time.Time               `json:"period_end"`
	Rewards                []paymentCallbackReward `json:"rewards"`
}

func (r *PaymentRepository) UpsertSubscription(ctx context.Context, params SubscriptionUpsertParams) (uint64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}
	startedAt := params.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	status := params.Status
	if status == "" {
		status = string(paymentsqlc.PaymentSubscriptionStatusActive)
	}

	id, err := r.q.UpsertPaymentSubscription(ctx, paymentsqlc.UpsertPaymentSubscriptionParams{
		ProviderCode:           params.ProviderCode,
		WorkspaceID:            workspaceID,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
		AppID:                  params.AppID,
		PlatformID:             params.PlatformID,
		PlatformUserID:         params.PlatformUserID,
		InternalUserID: sqlwrap.NullFromPtr(params.InternalUserID, func(value int64) sql.NullInt64 {
			return sql.NullInt64{Int64: value, Valid: true}
		}),
		ProductID: params.ProductID,
		OrderID: sqlwrap.NullFromPtr(params.OrderID, func(value int64) sql.NullInt64 {
			return sql.NullInt64{Int64: value, Valid: true}
		}),
		AttemptID: sqlwrap.NullFromPtr(params.AttemptID, func(value int64) sql.NullInt64 {
			return sql.NullInt64{Int64: value, Valid: true}
		}),
		Status: paymentsqlc.PaymentSubscriptionStatus(status),
		CancelReason: sqlwrap.NullFromPtr(params.CancelReason, func(value string) sql.NullString {
			return sql.NullString{String: value, Valid: true}
		}),
		StartedAt: startedAt,
		EndedAt: sqlwrap.NullFromPtr(params.EndedAt, func(value time.Time) sql.NullTime {
			return sql.NullTime{Time: value, Valid: true}
		}),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrPaymentMismatch
		}

		return 0, err
	}

	return uint64(id), nil
}

func (r *PaymentRepository) RecordSubscriptionRenewal(
	ctx context.Context,
	params SubscriptionRenewalParams,
) (SubscriptionRenewalResult, error) {

	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return SubscriptionRenewalResult{}, err
	}

	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	params.ProviderSubscriptionID = strings.TrimSpace(params.ProviderSubscriptionID)
	params.ProviderChargeID = strings.TrimSpace(params.ProviderChargeID)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.AttemptID == 0 || params.AttemptID > math.MaxInt64 ||
		params.ProviderCode == "" || params.ProviderPaymentID == "" ||
		params.ProviderSubscriptionID == "" || params.ProviderChargeID == "" ||
		params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 ||
		params.AssetCode == "" || params.PeriodEnd.IsZero() {
		return SubscriptionRenewalResult{}, ErrAttemptFieldsInvalid
	}

	var result SubscriptionRenewalResult
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}

		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}

		if order.WorkspaceID != workspaceID ||
			order.Status != paymentsqlc.PaymentOrderStatusFulfilled ||
			attempt.Status != paymentsqlc.PaymentAttemptStatusSucceeded ||
			attempt.ProviderCode != params.ProviderCode ||
			attempt.AssetCode != params.AssetCode ||
			uint64(attempt.AmountMinor) != params.AmountMinor ||
			!sameProviderPaymentID(
				attempt.ProviderPaymentID,
				sql.NullString{
					String: params.ProviderPaymentID,
					Valid:  true,
				},
			) ||
			!attempt.ProviderChargeID.Valid ||
			attempt.ProviderChargeID.String != params.ProviderSubscriptionID {
			return ErrPaymentMismatch
		}

		result.OrderID = uint64(order.ID)
		result.AttemptID = uint64(attempt.ID)

		existing, lookupErr := txRepo.q.GetPaymentSubscriptionRenewalByChargeID(
			ctx,
			paymentsqlc.GetPaymentSubscriptionRenewalByChargeIDParams{
				WorkspaceID:      order.WorkspaceID,
				ProviderCode:     params.ProviderCode,
				ProviderChargeID: params.ProviderChargeID,
			},
		)
		if lookupErr == nil {
			if existing.OrderID != order.ID ||
				existing.AttemptID != attempt.ID ||
				existing.ProviderSubscriptionID != params.ProviderSubscriptionID ||
				existing.ProviderChargeID != params.ProviderChargeID ||
				existing.AmountMinor != int64(params.AmountMinor) ||
				existing.AssetCode != params.AssetCode ||
				!existing.PeriodEnd.Equal(params.PeriodEnd) {
				return ErrPaymentMismatch
			}

			result.SubscriptionID = uint64(existing.SubscriptionID)
			result.RenewalID = uint64(existing.ID)
			result.AlreadyDone = true

			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}

		subscriptionID, err := txRepo.UpsertSubscription(ctx, SubscriptionUpsertParams{
			WorkspaceID:            order.WorkspaceID,
			ProviderCode:           params.ProviderCode,
			ProviderSubscriptionID: params.ProviderSubscriptionID,
			AppID:                  order.AppID,
			PlatformID:             order.PlatformID,
			PlatformUserID:         order.PlatformUserID,
			InternalUserID:         nullInt64Ptr(order.InternalUserID),
			ProductID:              order.ProductID,
			OrderID:                utils.Ref(order.ID),
			AttemptID:              utils.Ref(attempt.ID),
			Status:                 string(paymentsqlc.PaymentSubscriptionStatusActive),
			StartedAt:              time.Now().UTC(),
			EndedAt:                utils.Ref(params.PeriodEnd),
		})
		if err != nil {
			return err
		}
		result.SubscriptionID = subscriptionID

		renewalID, err := txRepo.q.CreatePaymentSubscriptionRenewal(
			ctx,
			paymentsqlc.CreatePaymentSubscriptionRenewalParams{
				WorkspaceID:            order.WorkspaceID,
				SubscriptionID:         int64(subscriptionID),
				OrderID:                order.ID,
				AttemptID:              attempt.ID,
				ProviderCode:           params.ProviderCode,
				ProviderSubscriptionID: params.ProviderSubscriptionID,
				ProviderChargeID:       params.ProviderChargeID,
				AmountMinor:            int64(params.AmountMinor),
				AssetCode:              params.AssetCode,
				PeriodEnd:              params.PeriodEnd,
			},
		)
		if errors.Is(err, sql.ErrNoRows) {
			existing, lookupErr := txRepo.q.GetPaymentSubscriptionRenewalByChargeID(
				ctx,
				paymentsqlc.GetPaymentSubscriptionRenewalByChargeIDParams{
					WorkspaceID:      order.WorkspaceID,
					ProviderCode:     params.ProviderCode,
					ProviderChargeID: params.ProviderChargeID,
				},
			)
			if errors.Is(lookupErr, sql.ErrNoRows) {
				existing, lookupErr = txRepo.q.GetPaymentSubscriptionRenewal(
					ctx,
					paymentsqlc.GetPaymentSubscriptionRenewalParams{
						WorkspaceID:            order.WorkspaceID,
						ProviderCode:           params.ProviderCode,
						ProviderSubscriptionID: params.ProviderSubscriptionID,
						PeriodEnd:              params.PeriodEnd,
					},
				)
			}
			if lookupErr != nil {
				return lookupErr
			}
			if existing.SubscriptionID != int64(subscriptionID) ||
				existing.OrderID != order.ID || existing.AttemptID != attempt.ID ||
				existing.ProviderSubscriptionID != params.ProviderSubscriptionID ||
				existing.ProviderChargeID != params.ProviderChargeID ||
				existing.AmountMinor != int64(params.AmountMinor) ||
				existing.AssetCode != params.AssetCode ||
				!existing.PeriodEnd.Equal(params.PeriodEnd) {
				return ErrPaymentMismatch
			}

			result.RenewalID = uint64(existing.ID)
			result.AlreadyDone = true

			return nil
		}
		if err != nil {
			return err
		}
		result.RenewalID = uint64(renewalID)

		items, err := txRepo.q.GetFulfillmentItemsForOrder(ctx, order.ID)
		if err != nil {
			return err
		}

		return txRepo.enqueuePaymentSubscriptionRenewedCallback(
			ctx,
			order,
			attempt,
			result,
			params,
			items,
		)
	})

	return result, err

}

func (r *PaymentRepository) enqueuePaymentSubscriptionRenewedCallback(
	ctx context.Context,
	order paymentsqlc.PaymentOrder,
	attempt paymentsqlc.PaymentAttempt,
	result SubscriptionRenewalResult,
	params SubscriptionRenewalParams,
	items []paymentsqlc.GetFulfillmentItemsForOrderRow,
) error {

	payload := paymentSubscriptionRenewedCallbackPayload{
		RenewalID:              result.RenewalID,
		SubscriptionID:         result.SubscriptionID,
		OrderID:                result.OrderID,
		AttemptID:              result.AttemptID,
		WorkspaceID:            order.WorkspaceID,
		AppID:                  order.AppID,
		PlatformID:             order.PlatformID,
		PlatformUserID:         order.PlatformUserID,
		ProductID:              order.ProductID,
		Quantity:               uint64(order.Quantity),
		ProviderCode:           params.ProviderCode,
		ProviderPaymentID:      params.ProviderPaymentID,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
		ProviderChargeID:       params.ProviderChargeID,
		AssetCode:              params.AssetCode,
		AmountMinor:            params.AmountMinor,
		PeriodEnd:              params.PeriodEnd,
		Rewards:                make([]paymentCallbackReward, 0, len(items)),
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

	eventKey := fmt.Sprintf(
		"payment.subscription.renewed:%d:%d",
		result.SubscriptionID,
		params.PeriodEnd.Unix(),
	)
	_, err = r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:        order.WorkspaceID,
		SourceService:      "payment",
		EventType:          "payment.subscription.renewed",
		EventKey:           eventKey,
		IdempotencyKey:     eventKey,
		Payload:            raw,
		PayloadContentType: callbackutil.JSONContentType,
	})

	return err

}

func (r *PaymentRepository) UpdateSubscriptionStatus(
	ctx context.Context,
	params SubscriptionStatusUpdateParams,
) (int64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}

	return r.q.UpdatePaymentSubscriptionStatusForWorkspace(ctx, paymentsqlc.UpdatePaymentSubscriptionStatusForWorkspaceParams{
		Status: paymentsqlc.PaymentSubscriptionStatus(params.Status),
		CancelReason: sqlwrap.NullFromPtr(params.CancelReason, func(value string) sql.NullString {
			return sql.NullString{String: value, Valid: true}
		}),
		EndedAt: sqlwrap.NullFromPtr(params.EndedAt, func(value time.Time) sql.NullTime {
			return sql.NullTime{Time: value, Valid: true}
		}),
		WorkspaceID:            workspaceID,
		ProviderCode:           params.ProviderCode,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
	})
}

func (r *PaymentRepository) UpdateSubscriptionStatusByProvider(
	ctx context.Context,
	params SubscriptionStatusUpdateParams,
) (int64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}

	return r.q.UpdatePaymentSubscriptionStatusByProvider(ctx, paymentsqlc.UpdatePaymentSubscriptionStatusByProviderParams{
		Status: paymentsqlc.PaymentSubscriptionStatus(params.Status),
		CancelReason: sqlwrap.NullFromPtr(params.CancelReason, func(value string) sql.NullString {
			return sql.NullString{String: value, Valid: true}
		}),
		EndedAt: sqlwrap.NullFromPtr(params.EndedAt, func(value time.Time) sql.NullTime {
			return sql.NullTime{Time: value, Valid: true}
		}),
		WorkspaceID:            workspaceID,
		ProviderCode:           params.ProviderCode,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
	})
}

func (r *PaymentRepository) IsSubscriptionActive(ctx context.Context, params SubscriptionIsActiveParams) (bool, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return false, err
	}
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	endedAt := sql.NullTime{Time: now, Valid: true}
	var count int64
	if params.ProductID != "" && params.ProviderCode != "" {
		count, err = r.q.CountActivePaymentSubscriptionsForProductProvider(
			ctx,
			paymentsqlc.CountActivePaymentSubscriptionsForProductProviderParams{
				PlatformID:     params.PlatformID,
				PlatformUserID: params.PlatformUserID,
				WorkspaceID:    workspaceID,
				ProductID:      params.ProductID,
				ProviderCode:   params.ProviderCode,
				EndedAt:        endedAt,
			},
		)
	} else if params.ProductID != "" {
		count, err = r.q.CountActivePaymentSubscriptionsForProduct(ctx, paymentsqlc.CountActivePaymentSubscriptionsForProductParams{
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			WorkspaceID:    workspaceID,
			ProductID:      params.ProductID,
			EndedAt:        endedAt,
		})
	} else if params.ProviderCode != "" {
		count, err = r.q.CountActivePaymentSubscriptionsForProvider(ctx, paymentsqlc.CountActivePaymentSubscriptionsForProviderParams{
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			WorkspaceID:    workspaceID,
			ProviderCode:   params.ProviderCode,
			EndedAt:        endedAt,
		})
	} else {
		count, err = r.q.CountActivePaymentSubscriptionsAll(ctx, paymentsqlc.CountActivePaymentSubscriptionsAllParams{
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			WorkspaceID:    workspaceID,
			EndedAt:        endedAt,
		})
	}
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
