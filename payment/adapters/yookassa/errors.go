package yookassa

import (
	"errors"
	"fmt"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrNotInitialized          = serviceerrors.New(serviceerrors.CodeNotReady, "yookassa adapter is not initialized")
	ErrCredentialsRequired     = serviceerrors.New(serviceerrors.CodeInvalidFields, "yookassa shop id and secret key are required")
	ErrWebhookSignatureInvalid = serviceerrors.New(serviceerrors.CodeUnauthorized, "yookassa webhook signature is invalid")
	ErrPaymentIDRequired       = serviceerrors.New(serviceerrors.CodeInvalidFields, "yookassa payment id is required")
	ErrCreatePaymentEmptyID    = serviceerrors.New(serviceerrors.CodeInternalError, "yookassa create payment response has empty id")
	ErrIdempotencyKeyRequired  = serviceerrors.New(serviceerrors.CodeInvalidFields, "yookassa idempotency key is required")
	ErrPaymentAttemptState     = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "yookassa payment attempt cannot be reused")
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
		fmt.Sprintf("yookassa %s failed with status %d", action, status),
		errors.New(body),
	)}
}

func isDefinitiveAPIError(err error) bool {
	var target *apiError
	return errors.As(err, &target) && target.status >= 400 && target.status < 500
}
