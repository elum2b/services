package telegramstars

import (
	"context"
	"fmt"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
)

func (a *TelegramStars) HandleSuccessfulPayment(ctx context.Context, payment SuccessfulPayment) (*SuccessfulPaymentResult, error) {
	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if payment.InvoicePayload == "" {
		return nil, ErrInvoicePayloadRequired
	}
	if payment.TelegramPaymentChargeID == "" {
		return nil, ErrTelegramPaymentChargeIDRequired
	}

	attempt, err := a.repository.GetAttemptByProviderPaymentID(
		ctx,
		payment.WorkspaceID,
		ProviderCode,
		payment.InvoicePayload,
	)
	if err != nil {
		return nil, err
	}

	eventID := fmt.Sprintf(
		"successful_payment:%s:%t",
		payment.TelegramPaymentChargeID,
		payment.IsFirstRecurring,
	)
	eventDBID, err := a.repository.CreateEvent(ctx, repository.EventCreateParams{
		WorkspaceID:       payment.WorkspaceID,
		ProviderCode:      ProviderCode,
		AttemptID:         utils.Ref(int64(attempt.ID)),
		OrderID:           utils.Ref(int64(attempt.OrderID)),
		ProviderEventID:   utils.Ref(eventID),
		ProviderPaymentID: utils.Ref(payment.InvoicePayload),
		EventType:         "successful_payment",
		EventStatus:       utils.Ref("succeeded"),
		PayloadHash:       sha256Hex([]byte(eventID + ":" + payment.InvoicePayload)),
		SignatureValid:    nil,
	})
	if err != nil && !isDuplicateEntry(err) {
		return nil, err
	}

	if payment.IsRecurring && !payment.IsFirstRecurring {
		if payment.SubscriptionExpirationDate <= 0 {
			return nil, ErrRecurringExpirationRequired
		}
		if attempt.ProviderChargeID == nil || *attempt.ProviderChargeID == "" {
			return nil, repository.ErrPaymentMismatch
		}

		periodEnd := time.Unix(payment.SubscriptionExpirationDate, 0).UTC()
		renewed, err := a.repository.RecordSubscriptionRenewal(
			ctx,
			repository.SubscriptionRenewalParams{
				WorkspaceID:            attempt.WorkspaceID,
				AttemptID:              attempt.ID,
				ProviderCode:           ProviderCode,
				ProviderPaymentID:      payment.InvoicePayload,
				ProviderSubscriptionID: *attempt.ProviderChargeID,
				ProviderChargeID:       payment.TelegramPaymentChargeID,
				AmountMinor:            payment.TotalAmount,
				AssetCode:              payment.Currency,
				PeriodEnd:              periodEnd,
			},
		)
		if err != nil {
			return nil, err
		}

		return &SuccessfulPaymentResult{
			OrderID:     renewed.OrderID,
			AttemptID:   renewed.AttemptID,
			EventID:     eventDBID,
			RenewalID:   utils.Ref(renewed.RenewalID),
			AlreadyDone: renewed.AlreadyDone,
		}, nil
	}

	updated, err := a.repository.SetAttemptProviderChargeID(
		ctx,
		attempt.ID,
		ProviderCode,
		payment.TelegramPaymentChargeID,
	)
	if err != nil {
		return nil, err
	}
	if updated != 1 {
		return nil, repository.ErrPaymentMismatch
	}

	completed, err := a.repository.CompleteAttempt(ctx, repository.CompleteAttemptParams{
		WorkspaceID:       attempt.WorkspaceID,
		AttemptID:         attempt.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(payment.InvoicePayload),
		AmountMinor:       payment.TotalAmount,
		AssetCode:         payment.Currency,
	})
	if err != nil {
		return nil, err
	}

	if payment.SubscriptionExpirationDate > 0 {
		order, err := a.repository.GetOrder(ctx, completed.OrderID)
		if err != nil {
			return nil, err
		}
		if _, err := a.repository.UpsertSubscription(ctx, repository.SubscriptionUpsertParams{
			WorkspaceID:            order.WorkspaceID,
			ProviderCode:           ProviderCode,
			ProviderSubscriptionID: payment.TelegramPaymentChargeID,
			AppID:                  order.AppID,
			PlatformID:             order.PlatformID,
			PlatformUserID:         order.PlatformUserID,
			InternalUserID:         order.InternalUserID,
			ProductID:              order.ProductID,
			OrderID:                utils.Ref(int64(order.ID)),
			AttemptID:              utils.Ref(int64(attempt.ID)),
			Status:                 "active",
			StartedAt:              time.Now(),
			EndedAt:                utils.Ref(time.Unix(payment.SubscriptionExpirationDate, 0)),
		}); err != nil {
			return nil, err
		}
	}

	return &SuccessfulPaymentResult{
		OrderID:       completed.OrderID,
		AttemptID:     completed.AttemptID,
		EventID:       eventDBID,
		AlreadyDone:   completed.AlreadyDone,
		FulfillmentID: uint64Ptr(completed.FulfillmentID),
	}, nil
}
