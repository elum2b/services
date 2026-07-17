package asset

import (
	"context"
)

func (a *Asset) List(ctx context.Context) ([]Model, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.ListAssets(ctx)
}
