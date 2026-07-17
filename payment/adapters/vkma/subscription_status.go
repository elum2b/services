package vkma

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"

	"github.com/elum-utils/sign/vkmashop"
)

func (a *VKMA) Active(ctx context.Context, workspaceID string, params vkmashop.Params) (*SubscriptionStatusResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.updateSubscriptionStatus(ctx, workspaceID, params, "active", nil)
}

func (a *VKMA) Canceled(ctx context.Context, workspaceID string, params vkmashop.Params) (*SubscriptionStatusResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.updateSubscriptionStatus(ctx, workspaceID, params, "canceled", utils.Ref(time.Now()))
}

func (a *VKMA) Refunded(ctx context.Context, workspaceID string, params vkmashop.Params) (*SubscriptionStatusResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.updateSubscriptionStatus(ctx, workspaceID, params, "refunded", utils.Ref(time.Now()))
}

func (a *VKMA) updateSubscriptionStatus(
	ctx context.Context,
	workspaceID string,
	params vkmashop.Params,
	status string,
	endedAt *time.Time,
) (*SubscriptionStatusResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	rows, err := a.repository.UpdateSubscriptionStatusByProvider(ctx, repository.SubscriptionStatusUpdateParams{
		WorkspaceID:            workspaceID,
		ProviderCode:           ProviderCode,
		ProviderSubscriptionID: strconv.Itoa(params.SubscriptionID),
		Status:                 status,
		CancelReason:           nonEmptyStringPtr(string(params.CancelReason)),
		EndedAt:                endedAt,
	})
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}

	if _, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      ProviderCode,
		ProviderEventID:   eventID(params),
		ProviderPaymentID: positiveString(params.OrderID),
		EventType:         string(params.NotificationType),
		EventStatus:       utils.Ref(string(params.Status)),
		PayloadHash:       payloadHash(params),
		SignatureValid:    utils.Ref(true),
	}); err != nil {
		return nil, err
	}

	return &SubscriptionStatusResponse{
		SubscriptionID: params.SubscriptionID,
		Status:         status,
	}, nil
}

func nonEmptyStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
