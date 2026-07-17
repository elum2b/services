package telegramstars

import (
	"context"
	"database/sql"
	"errors"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
)

func (a *TelegramStars) HandlePreCheckoutQuery(ctx context.Context, query PreCheckoutQuery) (*PreCheckoutResult, error) {
	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	client := NewClient(query.Credentials)

	attempt, err := a.repository.ValidatePendingAttempt(ctx, repository.PendingAttemptValidationParams{
		WorkspaceID:       query.WorkspaceID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: query.InvoicePayload,
		AmountMinor:       query.TotalAmount,
		AssetCode:         query.Currency,
		Now:               time.Now().UTC(),
	})
	if err != nil {
		answerErr := client.AnswerPreCheckoutQuery(ctx, query.ID, false, "Payment order was not found")
		if answerErr != nil {
			return nil, answerErr
		}
		if !errors.Is(err, sql.ErrNoRows) &&
			!errors.Is(err, repository.ErrPaymentMismatch) &&
			!errors.Is(err, repository.ErrOrderStateInvalid) &&
			!errors.Is(err, repository.ErrAttemptFieldsInvalid) {
			return nil, err
		}
		return &PreCheckoutResult{Accepted: false}, nil
	}

	eventID := query.ID
	if _, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       query.WorkspaceID,
		ProviderCode:      ProviderCode,
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(attempt.OrderID)),
		ProviderEventID:   refIfNotEmpty(eventID),
		ProviderPaymentID: refIfNotEmpty(query.InvoicePayload),
		EventType:         "pre_checkout_query",
		EventStatus:       utils.Ref("received"),
		PayloadHash:       sha256Hex([]byte(eventID + ":" + query.InvoicePayload)),
		SignatureValid:    nil,
	}); err != nil && !isDuplicateEntry(err) {
		return nil, err
	}

	if err := client.AnswerPreCheckoutQuery(ctx, query.ID, true, ""); err != nil {
		return nil, err
	}
	return &PreCheckoutResult{AttemptID: attempt.ID, OrderID: attempt.OrderID, Accepted: true}, nil
}
