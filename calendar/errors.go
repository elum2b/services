package calendar

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceNil                  = serviceerrors.New(serviceerrors.CodeNotReady, "calendar service is nil")
	ErrServiceRunning              = serviceerrors.New(serviceerrors.CodeConflict, "calendar service is already running")
	ErrDatabaseConfigRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "calendar database user and name are required")
	ErrCallbackHandlerNil          = serviceerrors.New(serviceerrors.CodeInvalidFields, "calendar callback handler is nil")
	ErrCallbacksRegistrationClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "calendar callbacks must be registered before Run")
	ErrCallbacksNotConfigured      = serviceerrors.New(serviceerrors.CodeNotReady, "calendar callback store is not configured")
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
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "calendar operation failed", err)
}
