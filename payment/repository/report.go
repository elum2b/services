package repository

import (
	"context"
	"database/sql"
	"math"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (r *PaymentRepository) GetPaymentReport(
	ctx context.Context,
	params PaymentReportParams,
) ([]PaymentReportModel, PaymentReportStatsModel, error) {

	if _, err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return nil, PaymentReportStatsModel{}, err
	}
	if params.MinAmountMinor > math.MaxInt64 ||
		params.MaxAmountMinor > math.MaxInt64 ||
		(params.MaxAmountMinor > 0 && params.MinAmountMinor > params.MaxAmountMinor) {
		return nil, PaymentReportStatsModel{}, ErrPaymentReportInvalid
	}

	queryCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.db.DB().BeginTx(queryCtx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return nil, PaymentReportStatsModel{}, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	q := r.q.WithTx(tx)
	identityAppID, identityPlatformID, identityPlatformUserID := reportIdentity(params)
	createdFrom := sqlwrap.NullTimeFromPtr(params.CreatedFrom)
	createdUntil := sqlwrap.NullTimeFromPtr(params.CreatedUntil)

	rows, err := q.AdminListPaymentReport(queryCtx, paymentsqlc.AdminListPaymentReportParams{
		SortField:              params.Sort,
		SortDirection:          params.Direction,
		PageOffset:             params.Offset,
		PageLimit:              params.Limit,
		WorkspaceID:            params.WorkspaceID,
		AppID:                  params.AppID,
		PlatformID:             params.PlatformID,
		PlatformUserID:         params.PlatformUserID,
		Status:                 params.Status,
		ProductID:              params.ProductID,
		ProviderCode:           params.ProviderCode,
		AssetCode:              params.AssetCode,
		CreatedFrom:            createdFrom,
		CreatedUntil:           createdUntil,
		MinAmountMinor:         int64(params.MinAmountMinor),
		MaxAmountMinor:         int64(params.MaxAmountMinor),
		IdentityAppID:          identityAppID,
		IdentityRole:           params.IdentityRole,
		IdentityPlatformID:     identityPlatformID,
		IdentityPlatformUserID: identityPlatformUserID,
	})
	if err != nil {
		return nil, PaymentReportStatsModel{}, err
	}

	statRows, err := q.AdminGetPaymentReportStats(queryCtx, paymentsqlc.AdminGetPaymentReportStatsParams{
		WorkspaceID:            params.WorkspaceID,
		AppID:                  params.AppID,
		PlatformID:             params.PlatformID,
		PlatformUserID:         params.PlatformUserID,
		Status:                 params.Status,
		ProductID:              params.ProductID,
		ProviderCode:           params.ProviderCode,
		AssetCode:              params.AssetCode,
		CreatedFrom:            createdFrom,
		CreatedUntil:           createdUntil,
		MinAmountMinor:         int64(params.MinAmountMinor),
		MaxAmountMinor:         int64(params.MaxAmountMinor),
		IdentityAppID:          identityAppID,
		IdentityRole:           params.IdentityRole,
		IdentityPlatformID:     identityPlatformID,
		IdentityPlatformUserID: identityPlatformUserID,
	})
	if err != nil {
		return nil, PaymentReportStatsModel{}, err
	}

	if err := tx.Commit(); err != nil {
		return nil, PaymentReportStatsModel{}, err
	}

	return mapPaymentReportRows(rows), mapPaymentReportStats(statRows), nil
}

func reportIdentity(params PaymentReportParams) (int64, int64, string) {
	if params.Identity == nil {
		return 0, 0, ""
	}

	return params.Identity.AppID, params.Identity.PlatformID, params.Identity.PlatformUserID
}

func mapPaymentReportRows(rows []paymentsqlc.AdminListPaymentReportRow) []PaymentReportModel {
	result := make([]PaymentReportModel, 0, len(rows))
	for _, row := range rows {
		result = append(result, PaymentReportModel{
			ID:                  uint64(row.ID),
			PublicID:            row.PublicID,
			WorkspaceID:         row.WorkspaceID,
			AppID:               row.AppID,
			RecipientPlatformID: row.RecipientPlatformID,
			RecipientUserID:     row.RecipientUserID,
			InitiatorPlatformID: row.InitiatorPlatformID,
			InitiatorUserID:     row.InitiatorUserID,
			ProductID:           row.ProductID,
			Quantity:            uint64(row.Quantity),
			AssetCode:           row.AssetCode,
			ListAmountMinor:     uint64(row.ListAmountMinor),
			DiscountAmountMinor: uint64(row.DiscountAmountMinor),
			PayableAmountMinor:  uint64(row.PayableAmountMinor),
			RefundCount:         uint64(row.RefundCount),
			RefundAmountMinor:   uint64(row.RefundAmountMinor),
			ProviderCode:        row.ProviderCode,
			Status:              string(row.Status),
			PaidAt:              sqlwrap.NullTimePtr(row.PaidAt),
			FulfilledAt:         sqlwrap.NullTimePtr(row.FulfilledAt),
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		})
	}

	return result
}

func mapPaymentReportStats(rows []paymentsqlc.AdminGetPaymentReportStatsRow) PaymentReportStatsModel {
	result := PaymentReportStatsModel{
		Assets: make([]PaymentReportAssetStatsModel, 0, len(rows)),
	}
	for _, row := range rows {
		result.TotalOrders += uint64(row.OrderCount)
		result.DraftOrders += uint64(row.DraftOrders)
		result.PendingPaymentOrders += uint64(row.PendingPaymentOrders)
		result.PendingOrders += uint64(row.PendingOrders)
		result.PaidOrders += uint64(row.PaidOrders)
		result.FulfilledOrders += uint64(row.FulfilledOrders)
		result.CanceledOrders += uint64(row.CanceledOrders)
		result.ExpiredOrders += uint64(row.ExpiredOrders)
		result.RefundedOrders += uint64(row.RefundedOrders)
		result.ChargebackedOrders += uint64(row.ChargebackedOrders)
		result.FailedOrders += uint64(row.FailedOrders)
		result.PurchaseCount += uint64(row.PurchaseCount)
		result.PurchaseQuantity += uint64(row.PurchaseQuantity)
		// The query calculates this global distinct count in a scalar subquery,
		// so it is repeated for every asset row rather than scoped to that asset.
		result.UniqueBuyers = uint64(row.UniqueBuyers)

		refundAmount := uint64(row.RefundAmountMinor)
		grossAmount := uint64(row.GrossAmountMinor)
		netAmount := uint64(0)
		if grossAmount > refundAmount {
			netAmount = grossAmount - refundAmount
		}
		result.Assets = append(result.Assets, PaymentReportAssetStatsModel{
			AssetCode:         row.AssetCode,
			OrderCount:        uint64(row.OrderCount),
			PurchaseCount:     uint64(row.PurchaseCount),
			PurchaseQuantity:  uint64(row.PurchaseQuantity),
			GrossAmountMinor:  grossAmount,
			RefundCount:       uint64(row.RefundCount),
			RefundAmountMinor: refundAmount,
			NetAmountMinor:    netAmount,
		})
	}

	return result
}
