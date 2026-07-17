package yookassa

import (
	"context"

	json "github.com/goccy/go-json"
)

func (a *YooKassa) SyncPayment(ctx context.Context, params SyncPaymentParams) (*WebhookResult, error) {

	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	payment, err := NewClient(params.Credentials).GetPayment(ctx, params.PaymentID)
	if err != nil {
		return nil, err
	}

	webhook := webhookPayload{
		Type:  "poll",
		Event: "payment." + payment.Status,
		Object: webhookPaymentObject{
			ID:     payment.ID,
			Status: payment.Status,
			Paid:   payment.Paid,
			Amount: payment.Amount,
		},
	}

	raw, err := json.Marshal(webhook)
	if err != nil {
		return nil, err
	}

	return a.handlePayload(ctx, params.WorkspaceID, webhook, raw, false)

}
