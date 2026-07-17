package yookassa

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	json "github.com/goccy/go-json"
)

const yookassaIdempotencyTTL = 24 * time.Hour

func (a *YooKassa) CreatePayment(ctx context.Context, params CreatePaymentParams) (*CreatePaymentResponse, error) {

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
	fingerprint, err := yookassaRequestFingerprint(params)
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
		return yookassaExistingPaymentResponse(local, params.PaymentMethodType)
	}
	if local.AlreadyExists && time.Since(local.Attempt.CreatedAt) >= yookassaIdempotencyTTL {
		return nil, ErrPaymentAttemptState
	}

	description := params.Description
	if description == "" {
		description = fmt.Sprintf("Payment order %s", order.PublicID)
	}
	capture := true
	if params.Capture != nil {
		capture = *params.Capture
	}

	payment, err := client.CreatePayment(ctx, createPaymentRequest{
		Amount: Amount{
			Value:    formatRubMinor(order.PayableAmountMinor),
			Currency: AssetCode,
		},
		Capture:           capture,
		Description:       description,
		PaymentMethodData: paymentMethodData(params.PaymentMethodType),
		Receipt:           params.Receipt,
		Confirmation: yookassaConfirmation{
			Type:      "redirect",
			ReturnURL: params.ReturnURL,
		},
		Metadata: map[string]string{
			"order_id":        fmt.Sprintf("%d", order.ID),
			"order_public_id": order.PublicID,
			"workspace_id":    order.WorkspaceID,
			"product_id":      order.ProductID,
		},
	}, params.IdempotencyKey)
	if err != nil {
		if isDefinitiveAPIError(err) {
			if failErr := a.repository.FailProviderAttempt(ctx, order.WorkspaceID, local.Attempt.ID, ProviderCode); failErr != nil {
				return nil, fmt.Errorf("%w: fail local attempt: %v", err, failErr)
			}
		}
		return nil, err
	}
	if payment.ID == "" {
		return nil, ErrCreatePaymentEmptyID
	}

	attempt, err := a.repository.BindProviderAttempt(ctx, repository.ProviderAttemptBindParams{
		WorkspaceID:        order.WorkspaceID,
		AttemptID:          local.Attempt.ID,
		ProviderCode:       ProviderCode,
		RequestFingerprint: fingerprint,
		ProviderPaymentID:  payment.ID,
		ConfirmationURL:    nilIfEmpty(payment.Confirmation.ConfirmationURL),
	})
	if err != nil {
		return nil, err
	}

	return &CreatePaymentResponse{
		OrderID:           order.ID,
		OrderPublicID:     order.PublicID,
		AttemptID:         attempt.ID,
		PaymentID:         payment.ID,
		Status:            payment.Status,
		ConfirmationURL:   payment.Confirmation.ConfirmationURL,
		AmountMinor:       attempt.AmountMinor,
		AssetCode:         attempt.AssetCode,
		PaymentMethodType: params.PaymentMethodType,
	}, nil

}

func yookassaExistingPaymentResponse(
	local repository.ProviderAttemptCreateResult,
	paymentMethod PaymentMethodType,
) (*CreatePaymentResponse, error) {
	if local.Attempt.Status == string(paymentsqlc.PaymentAttemptStatusFailed) ||
		local.Attempt.ProviderPaymentID == nil {
		return nil, ErrPaymentAttemptState
	}

	return &CreatePaymentResponse{
		OrderID:           local.Order.ID,
		OrderPublicID:     local.Order.PublicID,
		AttemptID:         local.Attempt.ID,
		PaymentID:         *local.Attempt.ProviderPaymentID,
		Status:            local.Attempt.Status,
		ConfirmationURL:   valueOrEmpty(local.Attempt.ConfirmationURL),
		AmountMinor:       local.Attempt.AmountMinor,
		AssetCode:         local.Attempt.AssetCode,
		PaymentMethodType: paymentMethod,
	}, nil
}

func yookassaRequestFingerprint(params CreatePaymentParams) (string, error) {
	raw, err := json.Marshal(struct {
		WorkspaceID       string
		AppID             int64
		PlatformID        int64
		PlatformUserID    string
		InternalUserID    *int64
		ProductID         string
		Quantity          uint64
		Locale            string
		ReturnURL         string
		Description       string
		PaymentMethodType PaymentMethodType
		Receipt           *Receipt
		Capture           *bool
		ExpiresAt         *time.Time
		ReservedUntil     *time.Time
	}{
		WorkspaceID:       params.WorkspaceID,
		AppID:             params.AppID,
		PlatformID:        params.PlatformID,
		PlatformUserID:    params.PlatformUserID,
		InternalUserID:    params.InternalUserID,
		ProductID:         params.ProductID,
		Quantity:          params.Quantity,
		Locale:            normalizeLocale(params.Locale),
		ReturnURL:         params.ReturnURL,
		Description:       params.Description,
		PaymentMethodType: params.PaymentMethodType,
		Receipt:           params.Receipt,
		Capture:           params.Capture,
		ExpiresAt:         params.ExpiresAt,
		ReservedUntil:     params.ReservedUntil,
	})
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
