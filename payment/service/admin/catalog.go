package admin

import (
	"context"
	"database/sql"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *Admin) ListProductGroups(ctx context.Context, params ProductGroupListParams) ([]ProductGroupModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListProductGroups(ctx, paymentsqlc.AdminListProductGroupsParams{
		WorkspaceID: params.WorkspaceID,
		Limit:       limit,
		Offset:      offset,
	})
}

func (a *Admin) GetProductGroup(ctx context.Context, workspaceID string, code string) (ProductGroupModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetProductGroup(ctx, paymentsqlc.AdminGetProductGroupParams{
		WorkspaceID: workspaceID,
		Code:        code,
	})
}

func (a *Admin) UpsertProductGroup(ctx context.Context, params ProductGroupUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpsertProductGroup(ctx, repository.ProductGroupUpsertParams{
		WorkspaceID:    params.WorkspaceID,
		Code:           params.Code,
		TitleKey:       params.TitleKey,
		DescriptionKey: params.DescriptionKey,
		Position:       params.Position,
		IsActive:       params.IsActive,
	})
}

func (a *Admin) DeleteProductGroup(ctx context.Context, workspaceID string, code string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.DeleteProductGroup(ctx, workspaceID, code)
}

func (a *Admin) ListLocalizations(ctx context.Context, params LocalizationListParams) ([]LocalizationModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListLocalizations(ctx, paymentsqlc.AdminListLocalizationsParams{
		WorkspaceID: params.WorkspaceID,
		Column2:     params.Locale,
		Locale:      params.Locale,
		Limit:       limit,
		Offset:      offset,
	})
}

func (a *Admin) GetLocalization(
	ctx context.Context,
	workspaceID string,
	locale string,
	key string,
) (LocalizationModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetLocalization(ctx, paymentsqlc.AdminGetLocalizationParams{
		WorkspaceID:     workspaceID,
		Locale:          locale,
		LocalizationKey: key,
	})
}

func (a *Admin) UpsertLocalization(ctx context.Context, params LocalizationUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpsertLocalization(ctx, repository.LocalizationUpsertParams{
		WorkspaceID:     params.WorkspaceID,
		Locale:          params.Locale,
		LocalizationKey: params.LocalizationKey,
		Value:           params.Value,
	})
}

func (a *Admin) DeleteLocalization(ctx context.Context, workspaceID string, locale string, key string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.DeleteLocalization(ctx, workspaceID, locale, key)
}

func (a *Admin) ListProducts(ctx context.Context, params ProductListParams) ([]ProductModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListProducts(ctx, paymentsqlc.AdminListProductsParams{
		WorkspaceID:  params.WorkspaceID,
		Column2:      params.GroupCode,
		GroupCode:    sql.NullString{String: params.GroupCode, Valid: params.GroupCode != ""},
		Column4:      params.QuantityMode,
		QuantityMode: paymentsqlc.PaymentProductQuantityMode(params.QuantityMode),
		Limit:        limit,
		Offset:       offset,
	})
}

func (a *Admin) GetProduct(ctx context.Context, workspaceID string, id string) (ProductModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetProduct(ctx, paymentsqlc.AdminGetProductParams{WorkspaceID: workspaceID, ID: id})
}

func (a *Admin) UpsertProduct(ctx context.Context, params ProductUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpsertProduct(ctx, repository.ProductUpsertParams{
		WorkspaceID:          params.WorkspaceID,
		ID:                   params.ID,
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

func (a *Admin) DeleteProduct(ctx context.Context, workspaceID string, id string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.DeleteProduct(ctx, workspaceID, id)
}

func (a *Admin) ListProductItems(ctx context.Context, params ProductItemListParams) ([]ProductItemModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListProductItems(ctx, paymentsqlc.AdminListProductItemsParams{
		WorkspaceID: params.WorkspaceID,
		Column2:     params.ProductID,
		ProductID:   params.ProductID,
		Column4:     params.ItemID,
		ItemID:      params.ItemID,
		Limit:       limit,
		Offset:      offset,
	})
}

func (a *Admin) UpsertProductItem(ctx context.Context, params ProductItemUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	if params.Quantity <= 0 {
		return repository.ErrInvalidItemQuantity
	}
	return a.repository.UpsertProductItem(ctx, repository.ProductItemUpsertParams{
		WorkspaceID:  params.WorkspaceID,
		ProductID:    params.ProductID,
		ItemID:       params.ItemID,
		RewardType:   params.RewardType,
		Quantity:     params.Quantity,
		Scale:        uint16(params.Scale),
		DurationUnit: params.DurationUnit,
	})
}

func (a *Admin) DeleteProductItem(
	ctx context.Context,
	workspaceID string,
	productID string,
	itemID string,
) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.DeleteProductItem(ctx, workspaceID, productID, itemID)
}

func (a *Admin) ListPrices(ctx context.Context, params PriceListParams) ([]PriceModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListPrices(ctx, paymentsqlc.AdminListPricesParams{
		WorkspaceID: params.WorkspaceID,
		Column2:     params.ProductID,
		ProductID:   params.ProductID,
		Column4:     params.AssetCode,
		AssetCode:   params.AssetCode,
		Limit:       limit,
		Offset:      offset,
	})
}

func (a *Admin) GetPrice(ctx context.Context, workspaceID string, id uint64) (PriceModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetPrice(ctx, paymentsqlc.AdminGetPriceParams{WorkspaceID: workspaceID, ID: int64(id)})
}

func (a *Admin) CreatePrice(ctx context.Context, params ProductPriceCreateParams) (uint64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.CreateProductPrice(ctx, repository.ProductPriceCreateParams{
		WorkspaceID:         params.WorkspaceID,
		ProductID:           params.ProductID,
		AssetCode:           params.AssetCode,
		ListAmountMinor:     params.ListAmountMinor,
		DiscountAmountMinor: params.DiscountAmountMinor,
		IsPromotion:         params.IsPromotion,
		StartsAt:            params.StartsAt,
		EndsAt:              params.EndsAt,
	})
}

func (a *Admin) UpdatePrice(ctx context.Context, params ProductPriceUpdateParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpdateProductPrice(ctx, repository.ProductPriceUpdateParams{
		ID:                  params.ID,
		WorkspaceID:         params.WorkspaceID,
		AssetCode:           params.AssetCode,
		ListAmountMinor:     params.ListAmountMinor,
		DiscountAmountMinor: params.DiscountAmountMinor,
		IsPromotion:         params.IsPromotion,
		StartsAt:            params.StartsAt,
		EndsAt:              params.EndsAt,
	})
}

func (a *Admin) DeletePrice(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.DeleteProductPrice(ctx, workspaceID, id)
}

func (a *Admin) ListProductLimitCounters(
	ctx context.Context,
	params ProductLimitCounterListParams,
) ([]ProductLimitCounterModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListProductLimitCounters(ctx, paymentsqlc.AdminListProductLimitCountersParams{
		WorkspaceID:    params.WorkspaceID,
		Column2:        params.ProductID,
		ProductID:      params.ProductID,
		Column4:        params.PlatformID,
		PlatformID:     params.PlatformID,
		Column6:        params.PlatformUserID,
		PlatformUserID: params.PlatformUserID,
		Limit:          limit,
		Offset:         offset,
	})
}

func (a *Admin) DeleteProductLimitCounter(ctx context.Context, params ProductLimitCounterDeleteParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminDeleteProductLimitCounter(ctx, paymentsqlc.AdminDeleteProductLimitCounterParams{
		WorkspaceID:    params.WorkspaceID,
		PlatformID:     params.PlatformID,
		ProductID:      params.ProductID,
		CounterScope:   paymentsqlc.PaymentProductLimitCounterCounterScope(params.CounterScope),
		PlatformUserID: params.PlatformUserID,
		WindowStart:    params.WindowStart,
		WindowEnd:      params.WindowEnd,
	})
}
