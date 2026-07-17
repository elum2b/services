package admin

import (
	"github.com/elum2b/services/cpa/repository"
	serviceerrors "github.com/elum2b/services/errors"
)

var (
	ErrRepositoryNotConfigured      = serviceerrors.New(serviceerrors.CodeNotReady, "cpa admin repository is not configured")
	ErrCodeUploadModeUnsupported    = repository.ErrCodeUploadMode
	ErrCallbackEventIDRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa callback event id is required")
	ErrCallbackRejectReasonRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "cpa callback reject reason is required")
)
