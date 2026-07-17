package product

import (
	"context"

	"github.com/elum2b/services/payment/repository"
)

type UpsertGroupParams struct {
	WorkspaceID    string
	Code           string
	TitleKey       *string
	DescriptionKey *string
	Position       int32
	IsActive       bool
}

func (a *Product) UpsertGroup(ctx context.Context, params UpsertGroupParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx

	return a.repository.UpsertProductGroup(ctx, repository.ProductGroupUpsertParams{
		Code:           params.Code,
		WorkspaceID:    params.WorkspaceID,
		TitleKey:       params.TitleKey,
		DescriptionKey: params.DescriptionKey,
		Position:       params.Position,
		IsActive:       params.IsActive,
	})
}
