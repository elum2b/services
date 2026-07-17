package telegramstars

import (
	"errors"
	"fmt"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrNotInitialized                  = serviceerrors.New(serviceerrors.CodeNotReady, "telegram_stars adapter is not initialized")
	ErrBotTokenRequired                = serviceerrors.New(serviceerrors.CodeInvalidFields, "telegram_stars bot token is required")
	ErrIdempotencyKeyRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "telegram_stars idempotency key is required")
	ErrPaymentAttemptState             = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "telegram_stars payment attempt cannot be reused")
	ErrCreateInvoiceLinkEmpty          = serviceerrors.New(serviceerrors.CodeUnavailable, "telegram_stars create invoice link returned an empty result")
	ErrInvoicePayloadRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "telegram_stars invoice payload is required")
	ErrTelegramPaymentChargeIDRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "telegram_stars payment charge id is required")
	ErrRecurringExpirationRequired     = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"telegram_stars recurring payment expiration is required",
	)
)

type apiError struct {
	status int
	err    error
}

func (e *apiError) Error() string { return e.err.Error() }
func (e *apiError) Unwrap() error { return e.err }

func wrapAPIError(action string, status int, code int, description string, body string) error {
	return &apiError{status: status, err: serviceerrors.Wrap(
		serviceerrors.CodeUnavailable,
		fmt.Sprintf("telegram_stars %s failed with status %d code %d", action, status, code),
		errors.New(description+": "+body),
	)}
}

func isDefinitiveAPIError(err error) bool {
	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.status >= 400 && apiErr.status < 500 &&
		apiErr.status != 408 && apiErr.status != 429
}
