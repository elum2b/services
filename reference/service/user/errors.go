package user

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrKeysRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "reference user at least one key is required")
	ErrTooManyKeys  = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"reference user no more than 1000 keys are allowed",
	)
)
