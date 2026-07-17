package platega

import (
	"errors"
	"fmt"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrNotInitialized            = serviceerrors.New(serviceerrors.CodeNotReady, "platega adapter is not initialized")
	ErrCredentialsRequired       = serviceerrors.New(serviceerrors.CodeInvalidFields, "platega merchant id and secret are required")
	ErrWebhookCredentialsInvalid = serviceerrors.New(serviceerrors.CodeUnauthorized, "platega callback credentials are invalid")
	ErrTransactionIDRequired     = serviceerrors.New(serviceerrors.CodeInvalidFields, "platega transaction id is required")
	ErrTransactionResponseEmpty  = serviceerrors.New(serviceerrors.CodeInternalError, "platega create transaction response has empty transaction id")
	ErrRefundUnsupported         = serviceerrors.New(serviceerrors.CodeUnsupported, "platega refund API is not configured")
	ErrIdempotencyKeyRequired    = serviceerrors.New(serviceerrors.CodeInvalidFields, "platega idempotency key is required")
	ErrTransactionStateUnknown   = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "platega transaction creation state is unknown")
	ErrPaymentAttemptState       = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "platega payment attempt cannot be reused")
	ErrAmountInvalid             = serviceerrors.New(serviceerrors.CodeInvalidFields, "platega amount must be an exact RUB value with at most two decimal places")
	ErrExportResponseInvalid     = serviceerrors.New(serviceerrors.CodeInternalError, "platega transaction export response is invalid")
	ErrExportResponseTooLarge    = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "platega transaction export response is too large")
	ErrExportURLUnsafe           = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "platega transaction export URL is unsafe")
)

type apiError struct {
	status int
	err    error
}

func (e *apiError) Error() string { return e.err.Error() }
func (e *apiError) Unwrap() error { return e.err }

func wrapAPIError(action string, status int, body string) error {
	return &apiError{status: status, err: serviceerrors.Wrap(
		serviceerrors.CodeUnavailable,
		fmt.Sprintf("platega %s failed with status %d", action, status),
		errors.New(body),
	)}
}

func isDefinitiveAPIError(err error) bool {
	var target *apiError
	return errors.As(err, &target) && target.status >= 400 && target.status < 500
}
