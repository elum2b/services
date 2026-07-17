package refund

import (
	"context"
	"database/sql"
	"strings"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/payment/repository"
)

func (a *Refund) Execute(ctx context.Context, params Params) (*Result, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return nil, err
	}
	if params.IdempotencyKey == "" || params.IdempotencyKey != strings.TrimSpace(params.IdempotencyKey) ||
		len(params.IdempotencyKey) > 128 {
		return nil, ErrIdempotencyKeyRequired
	}

	order, err := a.repository.GetOrder(ctx, params.OrderID)
	if err != nil {
		return nil, err
	}
	if params.WorkspaceID != order.WorkspaceID {
		return nil, sql.ErrNoRows
	}

	attempt, err := a.refundAttempt(ctx, order, params.AttemptID)
	if err != nil {
		return nil, err
	}
	amount := params.AmountMinor
	if amount == 0 {
		amount = attempt.AmountMinor
	}
	if amount == 0 || amount > attempt.AmountMinor {
		return nil, ErrAmountInvalid
	}

	providerRefund, ok := a.providers[attempt.ProviderCode]
	if !ok || providerRefund == nil {
		return nil, ErrProviderUnsupported
	}

	refundState, err := a.repository.CreateIdempotentRefund(ctx, repository.IdempotentRefundCreateParams{
		WorkspaceID:    order.WorkspaceID,
		OrderID:        order.ID,
		AttemptID:      attempt.ID,
		ProviderCode:   attempt.ProviderCode,
		IdempotencyKey: params.IdempotencyKey,
		AmountMinor:    amount,
		AssetCode:      attempt.AssetCode,
		Reason:         refIfNotEmpty(params.Reason),
	})
	if err != nil {
		return nil, err
	}

	if refundState.Status == "succeeded" {
		return &Result{
			RefundID:         refundState.ID,
			OrderID:          order.ID,
			AttemptID:        attempt.ID,
			ProviderCode:     attempt.ProviderCode,
			ProviderRefundID: refundState.ProviderRefundID,
			AmountMinor:      amount,
			AssetCode:        attempt.AssetCode,
			Status:           refundState.Status,
		}, nil
	}
	if refundState.Status != "created" && refundState.Status != "pending" {
		return nil, repository.ErrOrderStateInvalid
	}

	providerResult, providerErr := providerRefund(ctx, ProviderRefundParams{
		Order: ProviderRefundOrder{
			ID:                  order.ID,
			WorkspaceID:         order.WorkspaceID,
			AppID:               order.AppID,
			PlatformID:          order.PlatformID,
			PlatformUserID:      order.PlatformUserID,
			PayerPlatformID:     order.PayerPlatformID,
			PayerPlatformUserID: order.PayerPlatformUserID,
			ProductID:           order.ProductID,
		},
		Attempt: ProviderRefundAttempt{
			ID:                attempt.ID,
			ProviderCode:      attempt.ProviderCode,
			AssetCode:         attempt.AssetCode,
			AmountMinor:       attempt.AmountMinor,
			ProviderPaymentID: attempt.ProviderPaymentID,
			ProviderChargeID:  attempt.ProviderChargeID,
		},
		RefundID:       refundState.ID,
		AmountMinor:    amount,
		Reason:         params.Reason,
		ProviderParams: params.ProviderParams,
	})
	if providerErr != nil {
		return nil, providerErr
	}

	status := providerResult.Status
	if status == "" {
		status = "succeeded"
	}
	if err := a.repository.FinalizeRefund(ctx, repository.RefundFinalizeParams{
		WorkspaceID:      order.WorkspaceID,
		RefundID:         refundState.ID,
		ProviderRefundID: providerResult.ProviderRefundID,
		Status:           status,
		Reason:           params.Reason,
	}); err != nil {
		return nil, err
	}

	return &Result{
		RefundID:         refundState.ID,
		OrderID:          order.ID,
		AttemptID:        attempt.ID,
		ProviderCode:     attempt.ProviderCode,
		ProviderRefundID: refIfNotEmpty(providerResult.ProviderRefundID),
		AmountMinor:      amount,
		AssetCode:        attempt.AssetCode,
		Status:           status,
	}, nil
}
