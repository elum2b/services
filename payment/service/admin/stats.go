package admin

import (
	"context"
	"time"

	"github.com/elum2b/services/payment/repository"
)

func (a *Admin) GetStats(ctx context.Context, workspaceID string) (StatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetPaymentStats(mergedCtx, workspaceID)
	if err != nil {
		return StatsModel{}, err
	}
	return mapStats(value), nil
}

func (a *Admin) GetProductStats(ctx context.Context, workspaceID, productID string) (ProductStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	value, err := a.repository.GetPaymentProductStats(mergedCtx, workspaceID, productID)
	if err != nil {
		return ProductStatsModel{}, err
	}
	return mapProductStats(value), nil
}

func (a *Admin) ListDailyStats(
	ctx context.Context,
	workspaceID, productID string,
	from, until time.Time,
) ([]DailyStatsModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListPaymentDailyStats(mergedCtx, workspaceID, productID, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]DailyStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyStatsModel{
			Date: value.Date, ProductID: value.ProductID, AssetCode: value.AssetCode,
			PurchaseCount: value.PurchaseCount, PurchaseQuantity: value.PurchaseQuantity,
			UniqueBuyers: value.UniqueBuyers, GrossAmountMinor: value.GrossAmountMinor,
			RefundCount: value.RefundCount, RefundAmountMinor: value.RefundAmountMinor,
		})
	}
	return result, nil
}

func (a *Admin) ListDailyOverview(
	ctx context.Context,
	workspaceID string,
	from, until time.Time,
) ([]DailyOverviewModel, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	values, err := a.repository.ListPaymentDailyOverview(mergedCtx, workspaceID, from, until)
	if err != nil {
		return nil, err
	}
	result := make([]DailyOverviewModel, 0, len(values))
	for _, value := range values {
		result = append(result, DailyOverviewModel{
			Date:          value.Date,
			ProductsTotal: value.ProductsTotal, ActiveProducts: value.ActiveProducts,
			VisibleProducts: value.VisibleProducts, OrdersCreated: value.OrdersCreated,
			DraftOrders: value.DraftOrders, PendingPaymentOrders: value.PendingPaymentOrders,
			PaidOrders: value.PaidOrders, FulfilledOrders: value.FulfilledOrders,
			CanceledOrders: value.CanceledOrders, ExpiredOrders: value.ExpiredOrders,
			RefundedOrders: value.RefundedOrders, ChargebackedOrders: value.ChargebackedOrders,
			FailedOrders: value.FailedOrders, PurchaseCount: value.PurchaseCount,
			PurchaseQuantity: value.PurchaseQuantity, UniqueBuyers: value.UniqueBuyers,
			RefundCount: value.RefundCount,
		})
	}
	return result, nil
}

func (a *Admin) RefreshDailyStats(ctx context.Context, workspaceID string, from, until time.Time) error {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	return a.repository.RefreshPaymentDailyStats(mergedCtx, workspaceID, from, until)
}

func mapStats(value repository.PaymentStats) StatsModel {
	return StatsModel{
		ProductsTotal: value.ProductsTotal, ActiveProducts: value.ActiveProducts,
		VisibleProducts: value.VisibleProducts, OrdersTotal: value.OrdersTotal,
		PendingOrders: value.PendingOrders, FulfilledOrders: value.FulfilledOrders,
		RefundedOrders: value.RefundedOrders, FailedOrders: value.FailedOrders,
		CanceledOrders: value.CanceledOrders, PurchaseCount: value.PurchaseCount,
		PurchaseQuantity: value.PurchaseQuantity, UniqueBuyers: value.UniqueBuyers,
		Assets: mapAssetStats(value.Assets),
	}
}

func mapProductStats(value repository.PaymentProductStats) ProductStatsModel {
	return ProductStatsModel{
		ProductID: value.ProductID, OrdersTotal: value.OrdersTotal,
		PendingOrders: value.PendingOrders, FulfilledOrders: value.FulfilledOrders,
		RefundedOrders: value.RefundedOrders, FailedOrders: value.FailedOrders,
		CanceledOrders: value.CanceledOrders, PurchaseCount: value.PurchaseCount,
		PurchaseQuantity: value.PurchaseQuantity, UniqueBuyers: value.UniqueBuyers,
		Assets: mapAssetStats(value.Assets),
	}
}

func mapAssetStats(values []repository.PaymentAssetStats) []AssetStatsModel {
	result := make([]AssetStatsModel, 0, len(values))
	for _, value := range values {
		result = append(result, AssetStatsModel{
			AssetCode: value.AssetCode, PurchaseCount: value.PurchaseCount,
			PurchaseQuantity: value.PurchaseQuantity, GrossAmountMinor: value.GrossAmountMinor,
			RefundCount: value.RefundCount, RefundAmountMinor: value.RefundAmountMinor,
		})
	}
	return result
}
