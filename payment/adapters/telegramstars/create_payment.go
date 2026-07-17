package telegramstars

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	json "github.com/goccy/go-json"
)

func (a *TelegramStars) CreatePayment(ctx context.Context, params CreatePaymentParams) (*CreatePaymentResponse, error) {
	if a == nil || a.repository == nil {
		return nil, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if params.IdempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	client := NewClient(params.Credentials)
	if err := client.requireCredentials(); err != nil {
		return nil, err
	}
	fingerprint, err := telegramStarsRequestFingerprint(params)
	if err != nil {
		return nil, err
	}

	local, err := a.repository.CreateProviderAttempt(ctx, repository.ProviderAttemptCreateParams{
		Order: repository.OrderCreateParams{
			WorkspaceID:    params.WorkspaceID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			InternalUserID: params.InternalUserID,
			ProductID:      params.ProductID,
			Quantity:       params.Quantity,
			AssetCode:      AssetCode,
			Locale:         normalizeLocale(params.Locale),
			ReservedUntil:  params.ReservedUntil,
			ExpiresAt:      params.ExpiresAt,
		},
		ProviderCode:       ProviderCode,
		IdempotencyKey:     params.IdempotencyKey,
		RequestFingerprint: fingerprint,
	})
	if err != nil {
		return nil, err
	}
	order := local.Order
	if local.AlreadyExists && local.Attempt.Status != string(paymentsqlc.PaymentAttemptStatusCreated) {
		return telegramStarsExistingPaymentResponse(local, params.SubscriptionPeriod)
	}

	payload := order.PublicID
	title := normalizeTitle(params.Title, order.ProductID)
	description := normalizeDescription(params.Description, order.PublicID)
	subscriptionPeriod := normalizeSubscriptionPeriod(params.SubscriptionPeriod)

	invoiceLink, err := client.CreateInvoiceLink(ctx, createInvoiceLinkRequest{
		Title:              title,
		Description:        description,
		Payload:            payload,
		ProviderToken:      "",
		Currency:           AssetCode,
		Prices:             []LabeledPrice{{Label: title, Amount: order.PayableAmountMinor}},
		SubscriptionPeriod: subscriptionPeriod,
	})
	if err != nil {
		if isDefinitiveAPIError(err) {
			if failErr := a.repository.FailProviderAttempt(
				ctx,
				order.WorkspaceID,
				local.Attempt.ID,
				ProviderCode,
			); failErr != nil {
				return nil, fmt.Errorf("%w: fail local attempt: %v", err, failErr)
			}
		}
		return nil, err
	}
	if strings.TrimSpace(invoiceLink) == "" {
		if failErr := a.repository.FailProviderAttempt(
			ctx,
			order.WorkspaceID,
			local.Attempt.ID,
			ProviderCode,
		); failErr != nil {
			return nil, fmt.Errorf("%w: fail local attempt: %v", ErrCreateInvoiceLinkEmpty, failErr)
		}
		return nil, ErrCreateInvoiceLinkEmpty
	}

	attempt, err := a.repository.BindProviderAttempt(ctx, repository.ProviderAttemptBindParams{
		WorkspaceID:        order.WorkspaceID,
		AttemptID:          local.Attempt.ID,
		ProviderCode:       ProviderCode,
		RequestFingerprint: fingerprint,
		ProviderPaymentID:  payload,
		ProviderInvoiceID:  &payload,
		ConfirmationURL:    &invoiceLink,
		ExpiresAt:          params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &CreatePaymentResponse{
		OrderID:            order.ID,
		OrderPublicID:      order.PublicID,
		AttemptID:          attempt.ID,
		InvoiceLink:        invoiceLink,
		AmountMinor:        attempt.AmountMinor,
		AssetCode:          attempt.AssetCode,
		SubscriptionPeriod: subscriptionPeriod,
	}, nil
}

func telegramStarsExistingPaymentResponse(
	local repository.ProviderAttemptCreateResult,
	subscriptionPeriod int,
) (*CreatePaymentResponse, error) {
	if local.Attempt.Status == string(paymentsqlc.PaymentAttemptStatusFailed) ||
		local.Attempt.ProviderPaymentID == nil || local.Attempt.ConfirmationURL == nil {
		return nil, ErrPaymentAttemptState
	}

	return &CreatePaymentResponse{
		OrderID:            local.Order.ID,
		OrderPublicID:      local.Order.PublicID,
		AttemptID:          local.Attempt.ID,
		InvoiceLink:        *local.Attempt.ConfirmationURL,
		AmountMinor:        local.Attempt.AmountMinor,
		AssetCode:          local.Attempt.AssetCode,
		SubscriptionPeriod: normalizeSubscriptionPeriod(subscriptionPeriod),
	}, nil
}

func telegramStarsRequestFingerprint(params CreatePaymentParams) (string, error) {
	raw, err := json.Marshal(struct {
		WorkspaceID        string
		AppID              int64
		PlatformID         int64
		PlatformUserID     string
		InternalUserID     *int64
		ProductID          string
		Quantity           uint64
		Locale             string
		Title              string
		Description        string
		SubscriptionPeriod int
		ExpiresAt          *time.Time
		ReservedUntil      *time.Time
	}{
		WorkspaceID:        params.WorkspaceID,
		AppID:              params.AppID,
		PlatformID:         params.PlatformID,
		PlatformUserID:     params.PlatformUserID,
		InternalUserID:     params.InternalUserID,
		ProductID:          params.ProductID,
		Quantity:           params.Quantity,
		Locale:             normalizeLocale(params.Locale),
		Title:              strings.TrimSpace(params.Title),
		Description:        strings.TrimSpace(params.Description),
		SubscriptionPeriod: normalizeSubscriptionPeriod(params.SubscriptionPeriod),
		ExpiresAt:          params.ExpiresAt,
		ReservedUntil:      params.ReservedUntil,
	})
	if err != nil {
		return "", err
	}

	return sha256Hex(raw), nil
}
