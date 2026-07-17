package vkma

import (
	"context"
	"strconv"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"

	"github.com/elum-utils/sign/vkmashop"
)

func (a *VKMA) RefundOrderForWorkspace(ctx context.Context, workspaceID string, params vkmashop.Params) (*ChargeableResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	providerPaymentID := strconv.Itoa(params.OrderID)

	result, err := a.repository.ApplyProviderRefund(ctx, repository.ProviderRefundParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: providerPaymentID,
		ProviderRefundID:  providerPaymentID,
		Reason:            refIfNotEmpty(string(params.CancelReason)),
		Event: repository.EventCreateParams{
			WorkspaceID:       workspaceID,
			ProviderCode:      ProviderCode,
			ProviderEventID:   eventID(params),
			ProviderPaymentID: utils.Ref(providerPaymentID),
			EventType:         string(params.NotificationType),
			EventStatus:       utils.Ref(string(params.Status)),
			PayloadHash:       payloadHash(params),
			SignatureValid:    utils.Ref(true),
		},
	})
	if err != nil {
		return nil, err
	}

	return &ChargeableResponse{
		AppOrderID: result.OrderID,
		OrderID:    params.OrderID,
	}, nil
}
