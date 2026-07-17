package platega

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	json "github.com/goccy/go-json"
)

func (a *Platega) CreatePayment(ctx context.Context, params CreatePaymentParams) (*CreatePaymentResponse, error) {

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
	fingerprint, err := plategaRequestFingerprint(params)
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
	if local.AlreadyExists {
		if local.Attempt.Status == string(paymentsqlc.PaymentAttemptStatusCreated) {
			return nil, ErrTransactionStateUnknown
		}
		return plategaExistingPaymentResponse(local, params.PaymentMethod)
	}

	description := params.Description
	if description == "" {
		description = fmt.Sprintf("Payment order %s", order.PublicID)
	}

	var method *PaymentMethod
	if params.PaymentMethod != PaymentMethodAny {
		method = &params.PaymentMethod
	}
	transaction, err := client.CreateTransaction(ctx, createTransactionRequest{
		PaymentMethod: method,
		PaymentDetails: paymentDetails{
			Amount:   rubMajorFromMinor(order.PayableAmountMinor),
			Currency: AssetCode,
		},
		Description: description,
		ReturnURL:   params.ReturnURL,
		FailedURL:   params.FailedURL,
		Payload:     order.PublicID,
	})
	if err != nil {
		if isDefinitiveAPIError(err) {
			if failErr := a.repository.FailProviderAttempt(ctx, order.WorkspaceID, local.Attempt.ID, ProviderCode); failErr != nil {
				return nil, fmt.Errorf("%w: fail local attempt: %v", err, failErr)
			}
		}
		return nil, err
	}
	if transaction.TransactionID == "" {
		return nil, ErrTransactionResponseEmpty
	}

	paymentURL := transaction.URL
	if paymentURL == "" {
		paymentURL = transaction.Redirect
	}

	attempt, err := a.repository.BindProviderAttempt(ctx, repository.ProviderAttemptBindParams{
		WorkspaceID:        order.WorkspaceID,
		AttemptID:          local.Attempt.ID,
		ProviderCode:       ProviderCode,
		RequestFingerprint: fingerprint,
		ProviderPaymentID:  transaction.TransactionID,
		ProviderInvoiceID:  &order.PublicID,
		ConfirmationURL:    nilIfEmpty(paymentURL),
		ReturnURL:          nilIfEmpty(params.ReturnURL),
		ExpiresAt:          params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return &CreatePaymentResponse{
		OrderID:        order.ID,
		OrderPublicID:  order.PublicID,
		AttemptID:      attempt.ID,
		TransactionID:  transaction.TransactionID,
		Status:         transaction.Status,
		PaymentURL:     paymentURL,
		RedirectURL:    transaction.Redirect,
		ReturnURL:      transaction.ReturnURL,
		ExpiresIn:      transaction.ExpiresIn,
		AmountMinor:    attempt.AmountMinor,
		AssetCode:      attempt.AssetCode,
		PaymentMethod:  params.PaymentMethod,
		ProviderMethod: transaction.PaymentMethod,
	}, nil

}

func plategaExistingPaymentResponse(
	local repository.ProviderAttemptCreateResult,
	paymentMethod PaymentMethod,
) (*CreatePaymentResponse, error) {
	if local.Attempt.Status == string(paymentsqlc.PaymentAttemptStatusFailed) ||
		local.Attempt.ProviderPaymentID == nil {
		return nil, ErrPaymentAttemptState
	}

	return &CreatePaymentResponse{
		OrderID:       local.Order.ID,
		OrderPublicID: local.Order.PublicID,
		AttemptID:     local.Attempt.ID,
		TransactionID: *local.Attempt.ProviderPaymentID,
		Status:        providerStatusFromAttempt(local.Attempt.Status),
		PaymentURL:    plategaValueOrEmpty(local.Attempt.ConfirmationURL),
		ReturnURL:     plategaValueOrEmpty(local.Attempt.ReturnURL),
		AmountMinor:   local.Attempt.AmountMinor,
		AssetCode:     local.Attempt.AssetCode,
		PaymentMethod: paymentMethod,
	}, nil
}

func providerStatusFromAttempt(status string) Status {
	switch paymentsqlc.PaymentAttemptStatus(status) {
	case paymentsqlc.PaymentAttemptStatusPending,
		paymentsqlc.PaymentAttemptStatusRequiresAction,
		paymentsqlc.PaymentAttemptStatusWaitingCapture:
		return StatusPending
	case paymentsqlc.PaymentAttemptStatusSucceeded:
		return StatusConfirmed
	case paymentsqlc.PaymentAttemptStatusCanceled:
		return StatusCanceled
	case paymentsqlc.PaymentAttemptStatusExpired:
		return StatusExpired
	case paymentsqlc.PaymentAttemptStatusFailed:
		return StatusFailed
	case paymentsqlc.PaymentAttemptStatusRefunded:
		return StatusRefunded
	case paymentsqlc.PaymentAttemptStatusChargebacked:
		return StatusChargebacked
	default:
		return Status(status)
	}
}

func plategaRequestFingerprint(params CreatePaymentParams) (string, error) {
	raw, err := json.Marshal(struct {
		WorkspaceID    string
		AppID          int64
		PlatformID     int64
		PlatformUserID string
		InternalUserID *int64
		ProductID      string
		Quantity       uint64
		Locale         string
		Description    string
		ReturnURL      string
		FailedURL      string
		PaymentMethod  PaymentMethod
		ExpiresAt      *time.Time
		ReservedUntil  *time.Time
	}{
		WorkspaceID:    params.WorkspaceID,
		AppID:          params.AppID,
		PlatformID:     params.PlatformID,
		PlatformUserID: params.PlatformUserID,
		InternalUserID: params.InternalUserID,
		ProductID:      params.ProductID,
		Quantity:       params.Quantity,
		Locale:         normalizeLocale(params.Locale),
		Description:    params.Description,
		ReturnURL:      params.ReturnURL,
		FailedURL:      params.FailedURL,
		PaymentMethod:  params.PaymentMethod,
		ExpiresAt:      params.ExpiresAt,
		ReservedUntil:  params.ReservedUntil,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func plategaValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (a *Platega) GetH2H(ctx context.Context, params GetH2HParams) (H2HResponse, error) {

	if a == nil {
		return H2HResponse{}, ErrNotInitialized
	}

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return NewClient(params.Credentials).GetH2H(ctx, params.TransactionID)

}
