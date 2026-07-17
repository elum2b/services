package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

type UpsertLocalizationParams struct {
	WorkspaceID     string
	Locale          string
	LocalizationKey string
	Value           string
}

func (a *Product) UpsertLocalization(ctx context.Context, params UpsertLocalizationParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertLocalization(ctx, repository.LocalizationUpsertParams{
		Locale:          params.Locale,
		WorkspaceID:     params.WorkspaceID,
		LocalizationKey: params.LocalizationKey,
		Value:           params.Value,
	})
}
