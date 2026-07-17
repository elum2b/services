package repository

import (
	"context"
	"time"

	sqlc "github.com/elum2b/services/payment/sqlc"
)

type PaymentStats struct {
	ProductsTotal    uint64
	ActiveProducts   uint64
	VisibleProducts  uint64
	OrdersTotal      uint64
	PendingOrders    uint64
	FulfilledOrders  uint64
	RefundedOrders   uint64
	FailedOrders     uint64
	CanceledOrders   uint64
	PurchaseCount    uint64
	PurchaseQuantity uint64
	UniqueBuyers     uint64
	Assets           []PaymentAssetStats
}

type PaymentProductStats struct {
	ProductID        string
	OrdersTotal      uint64
	PendingOrders    uint64
	FulfilledOrders  uint64
	RefundedOrders   uint64
	FailedOrders     uint64
	CanceledOrders   uint64
	PurchaseCount    uint64
	PurchaseQuantity uint64
	UniqueBuyers     uint64
	Assets           []PaymentAssetStats
}

type PaymentAssetStats struct {
	AssetCode         string
	PurchaseCount     uint64
	PurchaseQuantity  uint64
	GrossAmountMinor  uint64
	RefundCount       uint64
	RefundAmountMinor uint64
}

type PaymentDailyStats struct {
	Date              time.Time
	ProductID         string
	AssetCode         string
	PurchaseCount     uint64
	PurchaseQuantity  uint64
	UniqueBuyers      uint64
	GrossAmountMinor  uint64
	RefundCount       uint64
	RefundAmountMinor uint64
}

type PaymentDailyOverview struct {
	Date                 time.Time
	ProductsTotal        uint64
	ActiveProducts       uint64
	VisibleProducts      uint64
	OrdersCreated        uint64
	DraftOrders          uint64
	PendingPaymentOrders uint64
	PaidOrders           uint64
	FulfilledOrders      uint64
	CanceledOrders       uint64
	ExpiredOrders        uint64
	RefundedOrders       uint64
	ChargebackedOrders   uint64
	FailedOrders         uint64
	PurchaseCount        uint64
	PurchaseQuantity     uint64
	UniqueBuyers         uint64
	RefundCount          uint64
}

func statUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func (r *PaymentRepository) GetPaymentStats(ctx context.Context, workspaceID string) (PaymentStats, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return PaymentStats{}, err
	}

	row, err := r.q.AdminGetPaymentStats(ctx, sqlc.AdminGetPaymentStatsParams{
		WorkspaceID: workspaceID, WorkspaceID_2: workspaceID, WorkspaceID_3: workspaceID,
	})
	if err != nil {
		return PaymentStats{}, err
	}
	assets, err := r.listPaymentAssetStats(ctx, workspaceID, "")
	if err != nil {
		return PaymentStats{}, err
	}
	return PaymentStats{
		ProductsTotal: uint64(row.ProductsTotal), ActiveProducts: uint64(row.ActiveProducts),
		VisibleProducts: uint64(row.VisibleProducts), OrdersTotal: uint64(row.OrdersTotal),
		PendingOrders: uint64(row.PendingOrders), FulfilledOrders: uint64(row.FulfilledOrders),
		RefundedOrders: uint64(row.RefundedOrders), FailedOrders: uint64(row.FailedOrders),
		CanceledOrders: uint64(row.CanceledOrders), PurchaseCount: uint64(row.PurchaseCount),
		PurchaseQuantity: uint64(row.PurchaseQuantity), UniqueBuyers: uint64(row.UniqueBuyers),
		Assets: assets,
	}, nil
}

func (r *PaymentRepository) GetPaymentProductStats(
	ctx context.Context,
	workspaceID, productID string,
) (PaymentProductStats, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return PaymentProductStats{}, err
	}

	row, err := r.q.AdminGetPaymentProductStats(ctx, sqlc.AdminGetPaymentProductStatsParams{
		WorkspaceID: workspaceID, ProductID: productID,
		WorkspaceID_2: workspaceID, ProductID_2: productID,
		WorkspaceID_3: workspaceID, ID: productID,
	})
	if err != nil {
		return PaymentProductStats{}, err
	}
	assets, err := r.listPaymentAssetStats(ctx, workspaceID, productID)
	if err != nil {
		return PaymentProductStats{}, err
	}
	return PaymentProductStats{
		ProductID: row.ProductID, OrdersTotal: uint64(row.OrdersTotal),
		PendingOrders: uint64(row.PendingOrders), FulfilledOrders: uint64(row.FulfilledOrders),
		RefundedOrders: uint64(row.RefundedOrders), FailedOrders: uint64(row.FailedOrders),
		CanceledOrders: uint64(row.CanceledOrders), PurchaseCount: uint64(row.PurchaseCount),
		PurchaseQuantity: uint64(row.PurchaseQuantity), UniqueBuyers: uint64(row.UniqueBuyers),
		Assets: assets,
	}, nil
}

func (r *PaymentRepository) listPaymentAssetStats(
	ctx context.Context,
	workspaceID, productID string,
) ([]PaymentAssetStats, error) {
	rows, err := r.q.AdminListPaymentAssetStats(ctx, sqlc.AdminListPaymentAssetStatsParams{
		WorkspaceID: workspaceID,
		Column2:     productID,
		ProductID:   productID,
	})
	if err != nil {
		return nil, err
	}
	result := make([]PaymentAssetStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, PaymentAssetStats{
			AssetCode: row.AssetCode, PurchaseCount: uint64(row.PurchaseCount),
			PurchaseQuantity: uint64(row.PurchaseQuantity), GrossAmountMinor: uint64(row.GrossAmountMinor),
			RefundCount: uint64(row.RefundCount), RefundAmountMinor: uint64(row.RefundAmountMinor),
		})
	}
	return result, nil
}

func (r *PaymentRepository) ListPaymentDailyStats(
	ctx context.Context,
	workspaceID, productID string,
	from, until time.Time,
) ([]PaymentDailyStats, error) {
	rows, err := r.q.AdminListPaymentDailyStats(ctx, sqlc.AdminListPaymentDailyStatsParams{
		WorkspaceID: workspaceID,
		ProductID:   productID,
		StatsDate:   from,
		StatsDate_2: until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]PaymentDailyStats, 0, len(rows))
	for _, row := range rows {
		result = append(result, PaymentDailyStats{
			Date: row.StatsDate, ProductID: row.ProductID, AssetCode: row.AssetCode,
			PurchaseCount: statUint64(row.PurchaseCount), PurchaseQuantity: statUint64(row.PurchaseQuantity),
			UniqueBuyers: statUint64(row.UniqueBuyers), GrossAmountMinor: statUint64(row.GrossAmountMinor),
			RefundCount: statUint64(row.RefundCount), RefundAmountMinor: statUint64(row.RefundAmountMinor),
		})
	}
	return result, nil
}

func (r *PaymentRepository) ListPaymentDailyOverview(
	ctx context.Context,
	workspaceID string,
	from, until time.Time,
) ([]PaymentDailyOverview, error) {
	rows, err := r.q.AdminListPaymentDailyOverview(ctx, sqlc.AdminListPaymentDailyOverviewParams{
		WorkspaceID:   workspaceID,
		StatsDate:     from,
		StatsDate_2:   until,
		WorkspaceID_2: workspaceID,
		WorkspaceID_3: workspaceID,
		WorkspaceID_4: workspaceID,
		Column7:       from,
		Column8:       until,
	})
	if err != nil {
		return nil, err
	}
	result := make([]PaymentDailyOverview, 0, len(rows))
	for _, row := range rows {
		result = append(result, mapStoredDailyOverview(row))
	}
	return result, nil
}

func (r *PaymentRepository) RefreshPaymentDailyStats(
	ctx context.Context,
	workspaceID string,
	from, until time.Time,
) error {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return err
	}
	if from.IsZero() || until.IsZero() || from.After(until) {
		return ErrInvalidDateRange
	}

	if err := r.q.RefreshPaymentDailyStats(ctx, sqlc.RefreshPaymentDailyStatsParams{
		WorkspaceID:  workspaceID,
		OccurredAt:   from,
		OccurredAt_2: until,
	}); err != nil {
		return err
	}
	return r.q.RefreshPaymentDailyOverview(ctx, sqlc.RefreshPaymentDailyOverviewParams{
		WorkspaceID: workspaceID,
		OccurredAt:  from, OccurredAt_2: until,
		OccurredAt_3: from, OccurredAt_4: until,
		OccurredAt_5: from, OccurredAt_6: until,
		OccurredAt_7: from, OccurredAt_8: until,
	})
}

func mapStoredDailyOverview(row sqlc.PaymentStatsDailyOverview) PaymentDailyOverview {
	return PaymentDailyOverview{
		Date:          row.StatsDate,
		ProductsTotal: statUint64(row.ProductsTotal), ActiveProducts: statUint64(row.ActiveProducts),
		VisibleProducts: statUint64(row.VisibleProducts), OrdersCreated: statUint64(row.OrdersCreated),
		DraftOrders: statUint64(row.DraftOrders), PendingPaymentOrders: statUint64(row.PendingPaymentOrders),
		PaidOrders: statUint64(row.PaidOrders), FulfilledOrders: statUint64(row.FulfilledOrders),
		CanceledOrders: statUint64(row.CanceledOrders), ExpiredOrders: statUint64(row.ExpiredOrders),
		RefundedOrders: statUint64(row.RefundedOrders), ChargebackedOrders: statUint64(row.ChargebackedOrders),
		FailedOrders: statUint64(row.FailedOrders), PurchaseCount: statUint64(row.PurchaseCount),
		PurchaseQuantity: statUint64(row.PurchaseQuantity), UniqueBuyers: statUint64(row.UniqueBuyers),
		RefundCount: statUint64(row.RefundCount),
	}
}
