package user

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRecordParamsRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar user identity, calendar and operation are required",
	)
	ErrWorkspaceRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "calendar workspace is required")
)
