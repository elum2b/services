package admin

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRepositoryNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"reference admin repository is not configured",
	)
	ErrTypeChangeNotConfirmed = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"reference admin dangerous type change is not confirmed",
	)
	ErrLocalizationRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"reference admin localization scope, locale and title are required",
	)
	ErrItemScopeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"reference admin workspace and valid key are required",
	)
	ErrItemPayloadInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"reference admin payload must be valid JSON",
	)
	ErrItemTypeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"reference admin type must be quantity or duration",
	)
	ErrItemTypeFilterInvalid = serviceerrors.New(serviceerrors.CodeInvalidFields, "reference admin invalid item type")
)
