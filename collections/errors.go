package collections

import (
	"errors"

	"github.com/elum2b/services/collections/repository"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "collections service is closed")
	ErrServiceNil    = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"collections service is nil",
	)
	ErrServiceRunning = serviceerrors.New(
		serviceerrors.CodeConflict,
		"collections service is already running",
	)
	ErrDatabaseConfigRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"collections database user and name are required",
	)
	ErrCallbackHandlerNil = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"collections callback handler is nil",
	)
	ErrCallbacksRegistrationClosed = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"collections callbacks must be registered before Run",
	)
	ErrCallbacksNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"collections callback store is not configured",
	)
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

	return serviceerrors.Wrap(
		serviceerrors.CodeInternalError,
		"collections operation failed",
		err,
	)
}

var (
	ErrInvalidArgument = repository.ErrInvalid
	ErrNotFound        = repository.ErrNotFound
	ErrConflict        = repository.ErrConflict
	ErrForbidden       = repository.ErrForbidden
)
