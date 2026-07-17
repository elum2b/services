package payment

import (
	"errors"

	serviceerrors "github.com/elum2b/services/errors"
)

var (
	// ErrServiceNil means the payment service receiver is nil.
	ErrServiceNil = serviceerrors.New(serviceerrors.CodeNotReady, "payment service is nil")
	// ErrServiceRunning means Run was called while the service is already active.
	ErrServiceRunning = serviceerrors.New(serviceerrors.CodeConflict, "payment service is already running")
	// ErrDatabaseUserRequired means database credentials are incomplete.
	ErrDatabaseUserRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment database user is required")
	// ErrDatabaseNameRequired means database credentials are incomplete.
	ErrDatabaseNameRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment database name is required")
	// ErrCallbackHandlerNil means callback registration was attempted with a nil handler.
	ErrCallbackHandlerNil = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment callback handler is nil")
	// ErrCallbacksRegistrationClosed means callbacks must be registered before the service enters Run.
	ErrCallbacksRegistrationClosed = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "payment callbacks must be registered before Run")
	// ErrCallbacksNotConfigured means callback processing was requested before the callback store was initialized.
	ErrCallbacksNotConfigured = serviceerrors.New(serviceerrors.CodeNotReady, "payment callback store is not configured")
	// ErrOpenDatabase means the service could not open the payment database connection.
	ErrOpenDatabase = serviceerrors.New(serviceerrors.CodeUnavailable, "payment database connection failed")
	// ErrCreateClient means the sql client wrapper could not be initialized.
	ErrCreateClient = serviceerrors.New(serviceerrors.CodeInternalError, "payment sql client initialization failed")
	// ErrBootstrapFailed means bootstrap could not create the schema or supporting objects.
	ErrBootstrapFailed = serviceerrors.New(serviceerrors.CodeInternalError, "payment bootstrap failed")
	// ErrDexScreenerClientRequired means the rate loader was called without an HTTP client.
	ErrDexScreenerClientRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment dexscreener HTTP client is nil")
	// ErrDexScreenerAddressesRequired means there were no token addresses to request.
	ErrDexScreenerAddressesRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment dexscreener source token addresses are required")
	// ErrDexScreenerBatchTooLarge means a single DexScreener batch exceeded the supported size.
	ErrDexScreenerBatchTooLarge = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment dexscreener batch exceeds 30 token addresses")
	// ErrUSDPriceInvalid means a fetched USD price cannot be parsed or is non-positive.
	ErrUSDPriceInvalid = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment USD price is invalid")
	// ErrUSDPriceOverflow means a fetched USD price exceeds the supported minor-unit range.
	ErrUSDPriceOverflow = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment USD price exceeds supported range")
	// ErrTelegramStarsRefundCredentialsRequired means refund provider parameters are missing required Telegram Stars credentials.
	ErrTelegramStarsRefundCredentialsRequired = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund telegram_stars credentials are required")
	ErrTelegramStarsFullRefundOnly            = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "payment refund telegram_stars supports full refunds only")
	ErrTelegramStarsChargeIDRequired          = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund telegram_stars provider charge id is required")
	ErrTelegramStarsPlatformUserIDInvalid     = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund telegram_stars platform user id must be int64")
	ErrYooKassaRefundCredentialsRequired      = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund yookassa credentials are required")
	ErrYooKassaPaymentIDRequired              = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund yookassa provider payment id is required")
	ErrPlategaRefundParamsRequired            = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund platega parameters are required")
	ErrTONRefundParamsRequired                = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment refund ton parameters are required")
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
	return serviceerrors.Wrap(serviceerrors.CodeInternalError, "payment operation failed", err)
}
