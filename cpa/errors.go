package cpa

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceNil                  = serviceerrors.New(serviceerrors.CodeNotReady, "cpa service is nil")
	ErrServiceRunning              = serviceerrors.New(serviceerrors.CodeConflict, "cpa service is already running")
	ErrDatabaseUserRequired        = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa database user is required")
	ErrDatabaseNameRequired        = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa database name is required")
	ErrCallbackHandlerNil          = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa callback handler is nil")
	ErrCallbacksRegistrationClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "cpa callbacks must be registered before Run")
	ErrCallbacksNotConfigured      = serviceerrors.New(serviceerrors.CodeNotReady, "cpa callback store is not configured")
)

func wrapLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrServiceNil) ||
		errors.Is(err, ErrServiceRunning) ||
		errors.Is(err, ErrDatabaseUserRequired) ||
		errors.Is(err, ErrDatabaseNameRequired) ||
		errors.Is(err, ErrCallbackHandlerNil) ||
		errors.Is(err, ErrCallbacksRegistrationClosed) ||
		errors.Is(err, ErrCallbacksNotConfigured) ||
		serviceerrors.IsStructured(err) {
		return err
	}
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa operation failed", err)
}
