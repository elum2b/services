package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/sqlc"
)

type ProviderPurchaseParams struct {
	Order             OrderCreateParams
	ProviderCode      string
	ProviderPaymentID string
	AmountMinor       uint64
	EventType         string
	PayloadHash       string
}

// ApplyProviderPurchase records an authenticated one-unit purchase from a
// provider that supplies its payment ID and amount in the confirmation.
func (r *PaymentRepository) ApplyProviderPurchase(
	ctx context.Context,
	params ProviderPurchaseParams,
) (CompleteAttemptResult, error) {
	workspaceID, err := requireWorkspaceID(params.Order.WorkspaceID)
	if err != nil {
		return CompleteAttemptResult{}, err
	}

	if params.ProviderCode == "" || params.ProviderCode == BalanceProviderCode ||
		params.ProviderPaymentID == "" || len(params.ProviderPaymentID) > 128 ||
		params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 ||
		params.Order.Quantity > 1 || !validRequestFingerprint(params.PayloadHash) {
		return CompleteAttemptResult{}, ErrAttemptFieldsInvalid
	}

	var result CompleteAttemptResult
	err = r.inTransaction(ctx, func(ctx context.Context, tx *PaymentRepository) error {
		if err := tx.q.LockPaymentProviderIdempotency(ctx, sqlc.LockPaymentProviderIdempotencyParams{
			WorkspaceID:    workspaceID,
			ProviderCode:   params.ProviderCode,
			IdempotencyKey: params.ProviderPaymentID,
		}); err != nil {
			return err
		}

		attempt, err := tx.GetAttemptByProviderPaymentID(
			ctx, workspaceID, params.ProviderCode, params.ProviderPaymentID,
		)
		switch {
		case err == nil:
			order, err := tx.GetOrder(ctx, attempt.OrderID)
			if err != nil {
				return err
			}
			if order.AppID != params.Order.AppID ||
				order.PlatformID != params.Order.PlatformID ||
				order.PlatformUserID != params.Order.PlatformUserID ||
				order.ProductID != params.Order.ProductID || order.Quantity != 1 {
				return ErrPaymentMismatch
			}
		case errors.Is(err, sql.ErrNoRows):
			order, err := tx.createOrder(ctx, params.Order, nil, &params.AmountMinor)
			if err != nil {
				return err
			}
			attempt, err = tx.CreateAttempt(ctx, AttemptCreateParams{
				OrderID:           order.ID,
				ProviderCode:      params.ProviderCode,
				ProviderPaymentID: &params.ProviderPaymentID,
				IdempotencyKey:    &params.ProviderPaymentID,
			})
			if err != nil {
				return err
			}
			if _, err := tx.CreateEvent(ctx, EventCreateParams{
				WorkspaceID:       workspaceID,
				ProviderCode:      params.ProviderCode,
				AttemptID:         utils.Ref(int64(attempt.ID)),
				OrderID:           utils.Ref(int64(order.ID)),
				ProviderEventID:   &params.ProviderPaymentID,
				ProviderPaymentID: &params.ProviderPaymentID,
				EventType:         params.EventType,
				EventStatus:       utils.Ref("chargeable"),
				PayloadHash:       params.PayloadHash,
				SignatureValid:    utils.Ref(true),
			}); err != nil {
				return err
			}
		default:
			return err
		}

		result, err = tx.CompleteAttempt(ctx, CompleteAttemptParams{
			WorkspaceID:       workspaceID,
			AttemptID:         attempt.ID,
			ProviderCode:      params.ProviderCode,
			ProviderPaymentID: &params.ProviderPaymentID,
			AmountMinor:       params.AmountMinor,
			AssetCode:         params.Order.AssetCode,
		})
		if err == nil && result.FulfillmentID == nil {
			return ErrOrderStateInvalid
		}
		return err
	})
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	return result, nil
}
