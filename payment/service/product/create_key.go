package product

import (
	"context"
	"time"

	"github.com/elum2b/services/payment/repository"
)

type CreateKeyParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	InternalUserID *int64
	ProductID      string
	MaxUses        int32
	ExpiresAt      *time.Time
}

func (a *Product) CreateKey(ctx context.Context, params CreateKeyParams) (string, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.CreateProductPurchaseKey(ctx, repository.ProductCreateKeyParams{
		AppID:          params.AppID,
		WorkspaceID:    params.WorkspaceID,
		PlatformID:     params.PlatformID,
		PlatformUserID: params.PlatformUserID,
		InternalUserID: params.InternalUserID,
		ProductID:      params.ProductID,
		MaxUses:        params.MaxUses,
		ExpiresAt:      params.ExpiresAt,
	})
}
