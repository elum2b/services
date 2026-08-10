package admin

import (
	"context"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/payment/repository"
)

func (a *Admin) GetPaymentReport(
	ctx context.Context,
	params PaymentReportParams,
) (PaymentReport, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()

	normalized, err := normalizePaymentReportParams(params)
	if err != nil {
		return PaymentReport{}, err
	}

	payments, stats, err := a.repository.GetPaymentReport(
		mergedCtx,
		repository.PaymentReportParams{
			WorkspaceID:    normalized.WorkspaceID,
			Identity:       normalized.Identity,
			IdentityRole:   string(normalized.IdentityRole),
			AppID:          normalized.AppID,
			PlatformID:     normalized.PlatformID,
			PlatformUserID: normalized.PlatformUserID,
			Status:         normalized.Status,
			ProductID:      normalized.ProductID,
			ProviderCode:   normalized.ProviderCode,
			AssetCode:      normalized.AssetCode,
			CreatedFrom:    normalized.CreatedFrom,
			CreatedUntil:   normalized.CreatedUntil,
			MinAmountMinor: normalized.MinAmountMinor,
			MaxAmountMinor: normalized.MaxAmountMinor,
			Sort:           string(normalized.Sort),
			Direction:      string(normalized.Direction),
			Limit:          normalized.Page.Limit,
			Offset:         normalized.Page.Offset,
		},
	)
	if err != nil {
		return PaymentReport{}, err
	}

	return PaymentReport{
		Payments: mapPaymentReport(payments),
		Stats:    mapPaymentReportStats(stats),
	}, nil
}

func normalizePaymentReportParams(
	params PaymentReportParams,
) (PaymentReportParams, error) {
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return PaymentReportParams{}, err
	}
	if params.Identity != nil {
		if err := params.Identity.Validate(); err != nil {
			return PaymentReportParams{}, err
		}
		if params.Identity.WorkspaceID != params.WorkspaceID {
			return PaymentReportParams{}, repository.ErrPaymentReportInvalid
		}
	}
	if params.AppID < 0 || params.PlatformID < 0 ||
		(params.PlatformUserID != "" && (params.AppID <= 0 || params.PlatformID <= 0)) {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}
	if params.CreatedFrom != nil && params.CreatedUntil != nil &&
		!params.CreatedFrom.Before(*params.CreatedUntil) {
		return PaymentReportParams{}, repository.ErrInvalidDateRange
	}
	if params.MaxAmountMinor > 0 &&
		params.MinAmountMinor > params.MaxAmountMinor {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}
	if params.Status != "" && !validReportOrderStatus(params.Status) {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}

	if params.IdentityRole == "" {
		params.IdentityRole = PaymentIdentityRoleEither
	}
	if !validPaymentIdentityRole(params.IdentityRole) {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}
	if params.Sort == "" {
		params.Sort = PaymentSortCreatedAt
	}
	if !validPaymentSort(params.Sort) {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}
	if params.Direction == "" {
		params.Direction = SortDescending
	}
	if params.Direction != SortAscending && params.Direction != SortDescending {
		return PaymentReportParams{}, repository.ErrPaymentReportInvalid
	}

	params.Page.Limit, params.Page.Offset = normalizePage(params.Page)
	return params, nil
}

func validPaymentIdentityRole(role PaymentIdentityRole) bool {
	switch role {
	case PaymentIdentityRoleRecipient,
		PaymentIdentityRoleInitiator,
		PaymentIdentityRoleEither:
		return true
	default:
		return false
	}
}

func validPaymentSort(sort PaymentSortField) bool {
	switch sort {
	case PaymentSortCreatedAt,
		PaymentSortPaidAt,
		PaymentSortFulfilledAt,
		PaymentSortAmount,
		PaymentSortRefundAmount,
		PaymentSortStatus,
		PaymentSortProvider,
		PaymentSortProductID,
		PaymentSortAppID,
		PaymentSortPlatformID,
		PaymentSortPlatformUserID:
		return true
	default:
		return false
	}
}

func validReportOrderStatus(status string) bool {
	switch status {
	case "draft",
		"pending_payment",
		"paid",
		"fulfilled",
		"canceled",
		"expired",
		"refunded",
		"chargebacked",
		"failed":
		return true
	default:
		return false
	}
}

func mapPaymentReport(rows []repository.PaymentReportModel) []PaymentModel {
	result := make([]PaymentModel, 0, len(rows))
	for _, row := range rows {
		result = append(result, PaymentModel{
			ID:       row.ID,
			PublicID: row.PublicID,
			Initiator: services.Identity{
				WorkspaceID:    row.WorkspaceID,
				AppID:          row.AppID,
				PlatformID:     row.InitiatorPlatformID,
				PlatformUserID: row.InitiatorUserID,
			},
			Recipient: services.Identity{
				WorkspaceID:    row.WorkspaceID,
				AppID:          row.AppID,
				PlatformID:     row.RecipientPlatformID,
				PlatformUserID: row.RecipientUserID,
			},
			ProductID:           row.ProductID,
			Quantity:            row.Quantity,
			AssetCode:           row.AssetCode,
			ListAmountMinor:     row.ListAmountMinor,
			DiscountAmountMinor: row.DiscountAmountMinor,
			PayableAmountMinor:  row.PayableAmountMinor,
			RefundCount:         row.RefundCount,
			RefundAmountMinor:   row.RefundAmountMinor,
			ProviderCode:        row.ProviderCode,
			Status:              row.Status,
			PaidAt:              row.PaidAt,
			FulfilledAt:         row.FulfilledAt,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		})
	}

	return result
}

func mapPaymentReportStats(
	stats repository.PaymentReportStatsModel,
) PaymentReportStats {
	result := PaymentReportStats{
		TotalOrders:          stats.TotalOrders,
		DraftOrders:          stats.DraftOrders,
		PendingPaymentOrders: stats.PendingPaymentOrders,
		PendingOrders:        stats.PendingOrders,
		PaidOrders:           stats.PaidOrders,
		FulfilledOrders:      stats.FulfilledOrders,
		CanceledOrders:       stats.CanceledOrders,
		ExpiredOrders:        stats.ExpiredOrders,
		RefundedOrders:       stats.RefundedOrders,
		ChargebackedOrders:   stats.ChargebackedOrders,
		FailedOrders:         stats.FailedOrders,
		PurchaseCount:        stats.PurchaseCount,
		PurchaseQuantity:     stats.PurchaseQuantity,
		UniqueBuyers:         stats.UniqueBuyers,
		Assets: make(
			[]PaymentReportAssetStats,
			0,
			len(stats.Assets),
		),
	}
	for _, asset := range stats.Assets {
		result.Assets = append(result.Assets, PaymentReportAssetStats{
			AssetCode:         asset.AssetCode,
			OrderCount:        asset.OrderCount,
			PurchaseCount:     asset.PurchaseCount,
			PurchaseQuantity:  asset.PurchaseQuantity,
			GrossAmountMinor:  asset.GrossAmountMinor,
			RefundCount:       asset.RefundCount,
			RefundAmountMinor: asset.RefundAmountMinor,
			NetAmountMinor:    asset.NetAmountMinor,
		})
	}

	return result
}
