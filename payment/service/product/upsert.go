package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

func (a *Product) Upsert(ctx context.Context, params UpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertProduct(ctx, repository.ProductUpsertParams{
		ID:                   params.ID,
		WorkspaceID:          params.WorkspaceID,
		GroupCode:            params.GroupCode,
		TitleKey:             params.TitleKey,
		DescriptionKey:       params.DescriptionKey,
		Target:               params.Target,
		ImageURL:             params.ImageURL,
		LinkURL:              params.LinkURL,
		SizeLabel:            params.SizeLabel,
		PeriodSeconds:        params.PeriodSeconds,
		TrialDurationSeconds: params.TrialDurationSeconds,
		QuantityMode:         params.QuantityMode,
		Position:             params.Position,
		GlobalLimit:          params.GlobalLimit,
		GlobalInterval:       params.GlobalInterval,
		GlobalIntervalCount:  params.GlobalIntervalCount,
		UserLimit:            params.UserLimit,
		UserInterval:         params.UserInterval,
		UserIntervalCount:    params.UserIntervalCount,
		AvailableFrom:        params.AvailableFrom,
		AvailableUntil:       params.AvailableUntil,
		IsVisible:            params.IsVisible,
		IsClosed:             params.IsClosed,
	})
}
