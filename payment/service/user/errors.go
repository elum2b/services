package user

import serviceerrors "github.com/elum2b/services/errors"

var (
	// ErrServiceNotInitialized means the payment user facade was created without required dependencies.
	ErrServiceNotInitialized = serviceerrors.New(serviceerrors.CodeNotReady, "payment user service is not initialized")
	// ErrCheckoutNotInitialized means checkout operations are unavailable.
	ErrCheckoutNotInitialized = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment checkout service is not initialized",
	)
	// ErrAssetNotInitialized means asset operations are unavailable.
	ErrAssetNotInitialized = serviceerrors.New(serviceerrors.CodeNotReady, "payment asset service is not initialized")
	// ErrProductNotInitialized means product operations are unavailable.
	ErrProductNotInitialized = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment product service is not initialized",
	)
	// ErrSubscriptionNotInitialized means subscription operations are unavailable.
	ErrSubscriptionNotInitialized = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment subscription service is not initialized",
	)
)
