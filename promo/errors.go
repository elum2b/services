package promo

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceNil                  = serviceerrors.New(serviceerrors.CodeNotReady, "promo service is nil")
	ErrServiceRunning              = serviceerrors.New(serviceerrors.CodeConflict, "promo service is already running")
	ErrDatabaseConfigRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "promo database user and name are required")
	ErrCallbackHandlerNil          = serviceerrors.New(serviceerrors.CodeInvalidFields, "promo callback handler is nil")
	ErrCallbacksRegistrationClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "promo callbacks must be registered before Run")
	ErrCallbacksNotConfigured      = serviceerrors.New(serviceerrors.CodeNotReady, "promo callback store is not configured")
)

func wrapLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrServiceNil) ||
		errors.Is(err, ErrServiceRunning) ||
		errors.Is(err, ErrDatabaseConfigRequired) ||
		errors.Is(err, ErrCallbackHandlerNil) ||
		errors.Is(err, ErrCallbacksRegistrationClosed) ||
		errors.Is(err, ErrCallbacksNotConfigured) ||
		serviceerrors.IsStructured(err) {
		return err
	}
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "promo operation failed", err)
}
