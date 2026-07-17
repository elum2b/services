package admin

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRepositoryNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"calendar admin repository is not configured",
	)
	ErrCalendarIDRequired    = serviceerrors.New(serviceerrors.CodeInvalidFields, "calendar admin id is required")
	ErrCalendarScopeRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin workspace and type are required",
	)
	ErrCalendarTimezoneInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin timezone is invalid",
	)
	ErrCalendarRangeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin start_at must be before end_at",
	)
	ErrCalendarModeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin mode is invalid",
	)
	ErrCalendarIntervalTypeInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin interval type is invalid",
	)
	ErrCalendarIntervalUnitInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin interval unit is invalid",
	)
	ErrCalendarEndBehaviorInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin end behavior is invalid",
	)
	ErrCalendarNumberOutOfRange = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin numeric value is out of database range",
	)
	ErrStepCreateInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin step scope and positive position are required",
	)
	ErrStepUpdateInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin step id and positive position are required",
	)
	ErrRewardIDRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin reward id is required",
	)
	ErrRewardRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin reward scope, key, quantity and position are required",
	)
	ErrRewardQuantityUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin quantity reward must not have duration unit",
	)
	ErrRewardDurationUnit = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin duration reward requires a valid duration unit",
	)
	ErrRewardTypeUnsupported = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin reward type must be quantity or duration",
	)
	ErrLocalizationRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"calendar admin localization scope, locale and title are required",
	)
)
