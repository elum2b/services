package subscription

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Subscription) IsActive(ctx context.Context, params IsActiveParams) (bool, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	if err := params.Identity.Validate(); err != nil {
		return false, err
	}

	return a.repository.IsSubscriptionActive(mergedCtx, repository.SubscriptionIsActiveParams{
		WorkspaceID:    params.Identity.WorkspaceID,
		PlatformID:     params.Identity.PlatformID,
		PlatformUserID: params.Identity.PlatformUserID,
		ProductID:      params.ProductID,
		ProviderCode:   params.ProviderCode,
	})
}
