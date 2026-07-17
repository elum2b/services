package reference

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrServiceNil             = serviceerrors.New(serviceerrors.CodeNotReady, "reference service is nil")
	ErrServiceRunning         = serviceerrors.New(serviceerrors.CodeConflict, "reference service is already running")
	ErrDatabaseConfigRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "reference database user and name are required")
)

func wrapLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrServiceNil) ||
		errors.Is(err, ErrServiceRunning) ||
		errors.Is(err, ErrDatabaseConfigRequired) ||
		serviceerrors.IsStructured(err) {
		return err
	}
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "reference operation failed", err)
}
