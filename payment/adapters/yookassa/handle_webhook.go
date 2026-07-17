package yookassa

import (
	"context"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
	json "github.com/goccy/go-json"
)

func (a *YooKassa) HandleWebhook(ctx context.Context, request WebhookRequest) (*WebhookResult, error) {

	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	if !request.SignatureValid {
		return nil, ErrWebhookSignatureInvalid
	}

	var webhook webhookPayload
	if err := json.Unmarshal(request.Raw, &webhook); err != nil {
		return nil, err
	}

	return a.handlePayload(ctx, request.WorkspaceID, webhook, request.Raw, request.SignatureValid)

}

func (a *YooKassa) handlePayload(
	ctx context.Context,
	workspaceID string,
	webhook webhookPayload,
	raw []byte,
	signatureValid bool,
) (*WebhookResult, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	if webhook.Object.ID == "" {
		return nil, ErrPaymentIDRequired
	}

	attempt, err := a.repository.GetAttemptByProviderPaymentID(
		ctx,
		workspaceID,
		ProviderCode,
		webhook.Object.ID,
	)
	if err != nil {
		return nil, err
	}

	eventID := webhookEventID(webhook)
	eventDBID, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      ProviderCode,
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(attempt.OrderID)),
		ProviderEventID:   utils.Ref(eventID),
		ProviderPaymentID: utils.Ref(webhook.Object.ID),
		EventType:         webhook.Event,
		EventStatus:       utils.Ref(webhook.Object.Status),
		PayloadHash:       sha256Hex(raw),
		SignatureValid:    utils.Ref(signatureValid),
	})
	if err != nil && !isDuplicateEntry(err) {
		return nil, err
	}

	result := &WebhookResult{
		OrderID:   attempt.OrderID,
		AttemptID: attempt.ID,
		EventID:   eventDBID,
		Status:    webhook.Object.Status,
	}

	if webhook.Event == "payment.canceled" && webhook.Object.Status == "canceled" {
		err := a.repository.FinalizeProviderAttempt(ctx, repository.ProviderAttemptTerminalParams{
			WorkspaceID:       workspaceID,
			AttemptID:         attempt.ID,
			ProviderCode:      ProviderCode,
			ProviderPaymentID: webhook.Object.ID,
			Status:            repository.ProviderAttemptTerminalCanceled,
		})
		return result, err
	}

	if webhook.Event != "payment.succeeded" || webhook.Object.Status != "succeeded" || !webhook.Object.Paid {
		return result, nil
	}

	amountMinor, err := parseRubAmount(webhook.Object.Amount.Value)
	if err != nil {
		return nil, err
	}

	completed, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       attempt.WorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(webhook.Object.ID),
		AmountMinor:       amountMinor,
		AssetCode:         webhook.Object.Amount.Currency,
	})
	if err != nil {
		return nil, err
	}

	result.AlreadyDone = completed.AlreadyDone
	result.FulfilledID = uint64Ptr(completed.FulfillmentID)

	return result, nil

}

func uint64Ptr(value *int64) *uint64 {
	if value == nil {
		return nil
	}
	v := uint64(*value)
	return utils.Ref(v)
}
