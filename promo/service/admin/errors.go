package admin

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRepositoryNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"promo admin repository is not configured",
	)
	ErrLocalizationRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin locale and title are required",
	)
	ErrPromoIDRequired    = serviceerrors.New(serviceerrors.CodeInvalidFields, "promo admin promo id is required")
	ErrPromoScopeRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin workspace and code are required",
	)
	ErrPromoPayloadInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin payload must be valid JSON",
	)
	ErrPromoRangeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin start_at must be before end_at",
	)
	ErrPromoNumberOutOfRange = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin numeric value is out of database range",
	)
	ErrRewardRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin reward key and positive quantity are required",
	)
	ErrRewardQuantityUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin quantity reward must not have duration unit",
	)
	ErrRewardDurationUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin duration reward requires a valid duration unit",
	)
	ErrRewardTypeUnsupported = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"promo admin reward type must be quantity or duration",
	)
)
