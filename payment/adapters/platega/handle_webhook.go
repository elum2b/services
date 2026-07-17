package platega

import (
	"context"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
	json "github.com/goccy/go-json"
)

func (a *Platega) HandleWebhook(ctx context.Context, request WebhookRequest) (*WebhookResult, error) {

	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	signatureValid := validateHeaders(request.Headers, request.Credentials)
	if !signatureValid {
		return nil, ErrWebhookCredentialsInvalid
	}

	var payload callbackPayload
	if err := json.Unmarshal(request.Raw, &payload); err != nil {
		return nil, err
	}

	return a.handlePayload(
		ctx,
		request.Credentials,
		request.WorkspaceID,
		payload,
		request.Raw,
		signatureValid,
		nil,
	)

}

func (a *Platega) handlePayload(
	ctx context.Context,
	credentials Credentials,
	workspaceID string,
	payload callbackPayload,
	raw []byte,
	signatureValid bool,
	knownTransaction *transactionStatusResponse,
) (*WebhookResult, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if payload.ID == "" {
		return nil, ErrTransactionIDRequired
	}
	if !validTransactionStatus(payload.Status) {
		return nil, ErrTransactionStateUnknown
	}
	amountMinor := uint64(0)
	if payload.Status == StatusConfirmed || payload.Status == StatusChargebacked {
		var err error
		amountMinor, err = rubMinorFromMajor(payload.Amount)
		if err != nil || amountMinor == 0 {
			return nil, ErrAmountInvalid
		}
	}

	attempt, err := a.resolveAttempt(
		ctx,
		credentials,
		workspaceID,
		payload,
		knownTransaction,
	)
	if err != nil {
		return nil, err
	}

	eventID := webhookEventID(payload)
	eventDBID, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      ProviderCode,
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(attempt.OrderID)),
		ProviderEventID:   utils.Ref(eventID),
		ProviderPaymentID: utils.Ref(payload.ID),
		EventType:         "payment_status",
		EventStatus:       utils.Ref(string(payload.Status)),
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
		Status:    payload.Status,
	}
	switch payload.Status {
	case StatusPending:
		err := a.repository.TouchPendingProviderAttempt(
			ctx,
			workspaceID,
			attempt.ID,
			ProviderCode,
			payload.ID,
		)
		return result, err
	case StatusCanceled, StatusExpired, StatusFailed:
		err := a.repository.FinalizeProviderAttempt(ctx, repository.ProviderAttemptTerminalParams{
			WorkspaceID:       workspaceID,
			AttemptID:         attempt.ID,
			ProviderCode:      ProviderCode,
			ProviderPaymentID: payload.ID,
			Status:            terminalStatus(payload.Status),
		})
		return result, err
	case StatusChargebacked:
		chargeback, err := a.repository.ApplyProviderChargeback(ctx, repository.ProviderChargebackParams{
			WorkspaceID:       workspaceID,
			ProviderCode:      ProviderCode,
			ProviderPaymentID: payload.ID,
			AmountMinor:       amountMinor,
			AssetCode:         payload.Currency,
			Reason:            "provider_chargeback",
		})
		if err != nil {
			return nil, err
		}
		result.AlreadyDone = chargeback.AlreadyDone
		result.FulfilledID = utils.Ref(chargeback.FulfillmentID)
		return result, nil
	case StatusConfirmed:
		// Completion is handled below so the returned fulfillment remains identical
		// to all other provider completion paths.
	default:
		return result, nil
	}

	completed, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       attempt.WorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(payload.ID),
		AmountMinor:       amountMinor,
		AssetCode:         payload.Currency,
	})
	if err != nil {
		return nil, err
	}
	result.AlreadyDone = completed.AlreadyDone
	result.FulfilledID = uint64Ptr(completed.FulfillmentID)
	return result, nil
}

func validTransactionStatus(status Status) bool {
	switch status {
	case StatusPending,
		StatusConfirmed,
		StatusExpired,
		StatusCanceled,
		StatusFailed,
		StatusChargebacked:
		return true
	default:
		return false
	}
}

func terminalStatus(status Status) repository.ProviderAttemptTerminalStatus {
	switch status {
	case StatusCanceled:
		return repository.ProviderAttemptTerminalCanceled
	case StatusExpired:
		return repository.ProviderAttemptTerminalExpired
	case StatusFailed:
		return repository.ProviderAttemptTerminalFailed
	default:
		return ""
	}
}

func uint64Ptr(value *int64) *uint64 {
	if value == nil {
		return nil
	}
	v := uint64(*value)
	return utils.Ref(v)
}
