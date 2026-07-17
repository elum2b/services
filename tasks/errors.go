package tasks

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceNil                  = serviceerrors.New(serviceerrors.CodeNotReady, "tasks service is nil")
	ErrServiceRunning              = serviceerrors.New(serviceerrors.CodeConflict, "tasks service is already running")
	ErrDatabaseConfigRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "tasks database user and name are required")
	ErrCallbackHandlerNil          = serviceerrors.New(serviceerrors.CodeInvalidFields, "tasks callback handler is nil")
	ErrCallbacksRegistrationClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "tasks callbacks must be registered before Run")
	ErrCallbacksNotConfigured      = serviceerrors.New(serviceerrors.CodeNotReady, "tasks callback store is not configured")
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
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "tasks operation failed", err)
}
