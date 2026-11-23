package vkma

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/elum-utils/sign/vkmashop"

	"github.com/elum2b/services/payment/repository"
)

func (a *VKMA) GetItemForWorkspace(
	ctx context.Context,
	workspaceID string,
	params vkmashop.Params,
) (*ItemResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	ctx = mergedCtx

	return a.getProduct(ctx, workspaceID, params)
}

func (a *VKMA) GetSubscriptionForWorkspace(
	ctx context.Context,
	workspaceID string,
	params vkmashop.Params,
) (*ItemResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	ctx = mergedCtx

	return a.getProduct(ctx, workspaceID, params)
}

func (a *VKMA) getProduct(
	ctx context.Context,
	workspaceID string,
	params vkmashop.Params,
) (*ItemResponse, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	ctx = mergedCtx

	return a.getItem(ctx, repository.ProductGetParams{
		WorkspaceID:    workspaceID,
		AppID:          int64(params.AppID),
		PlatformID:     PlatformID,
		PlatformUserID: strconv.Itoa(params.UserID),
		ProductID:      productID(params),
		AssetCode:      AssetCode,
		Locale:         normalizeLocale(params.Lang),
	})
}

func (a *VKMA) getItem(
	ctx context.Context,
	params repository.ProductGetParams,
) (*ItemResponse, error) {
	product, err := a.repository.GetProduct(ctx, params)
	if err != nil {
		return nil, err
	}

	if params.AssetCode == OKAssetCode && product.Price.PayableAmountMinor == 0 {
		return nil, sql.ErrNoRows
	}

	expiration := uint64(0)
	if params.AssetCode != OKAssetCode && product.PeriodSeconds.Valid && product.PeriodSeconds.Int64 > 0 {
		expiration = uint64(product.PeriodSeconds.Int64)
	}

	return &ItemResponse{
		Title:      product.Title,
		PhotoURL:   product.ImageURL.String,
		Price:      product.Price.PayableAmountMinor,
		ItemID:     product.ID,
		Expiration: expiration,
	}, nil
}
