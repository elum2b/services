package product

import "context"

func (a *Product) DeleteLocalization(
	ctx context.Context,
	workspaceID string,
	locale string,
	localizationKey string,
) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.DeleteLocalization(ctx, workspaceID, locale, localizationKey)
}
