package admin

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRepositoryNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"tasks admin repository is not configured",
	)
	ErrRewardRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks admin reward key and positive quantity are required",
	)
	ErrRewardQuantityUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks admin quantity reward must not have duration unit",
	)
	ErrRewardDurationUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks admin duration reward requires a valid duration unit",
	)
	ErrRewardTypeUnsupported = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"tasks admin reward type must be quantity or duration",
	)
)
