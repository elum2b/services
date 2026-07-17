package admin

import serviceerrors "github.com/elum2b/services/errors"

var (
	ErrRepositoryNotConfigured = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment admin repository is not configured",
	)
	ErrProductServiceNotInitialized = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment admin product service is not initialized",
	)
	ErrRefundServiceNotInitialized = serviceerrors.New(
		serviceerrors.CodeNotReady,
		"payment admin refund service is not initialized",
	)
)
