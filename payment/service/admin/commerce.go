package admin

import (
	"context"
	"database/sql"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *Admin) ListPurchaseKeys(ctx context.Context, params PurchaseKeyListParams) ([]PurchaseKeyModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListPurchaseKeys(ctx, paymentsqlc.AdminListPurchaseKeysParams{
		WorkspaceID:    params.WorkspaceID,
		Column2:        params.ProductID,
		ProductID:      params.ProductID,
		Column4:        params.Status,
		Status:         paymentsqlc.PaymentPurchaseKeyStatus(params.Status),
		Column6:        params.PlatformID,
		PlatformID:     params.PlatformID,
		Column8:        params.PlatformUserID,
		PlatformUserID: params.PlatformUserID,
		Limit:          limit,
		Offset:         offset,
	})
}

func (a *Admin) GetPurchaseKey(ctx context.Context, workspaceID string, id uint64) (PurchaseKeyModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetPurchaseKey(
		ctx,
		paymentsqlc.AdminGetPurchaseKeyParams{WorkspaceID: workspaceID, ID: int64(id)},
	)
}

func (a *Admin) UpdatePurchaseKeyStatus(
	ctx context.Context,
	workspaceID string,
	id uint64,
	status string,
) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminUpdatePurchaseKeyStatus(ctx, paymentsqlc.AdminUpdatePurchaseKeyStatusParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
		Status:      paymentsqlc.PaymentPurchaseKeyStatus(status),
	})
}

func (a *Admin) ListOrders(ctx context.Context, params OrderListParams) ([]OrderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListOrders(ctx, paymentsqlc.AdminListOrdersParams{
		WorkspaceID:    params.WorkspaceID,
		Column2:        params.Status,
		Status:         paymentsqlc.PaymentOrderStatus(params.Status),
		Column4:        params.ProductID,
		ProductID:      params.ProductID,
		Column6:        params.PlatformID,
		PlatformID:     params.PlatformID,
		Column8:        params.PlatformUserID,
		PlatformUserID: params.PlatformUserID,
		Limit:          limit,
		Offset:         offset,
	})
}

func (a *Admin) GetOrder(ctx context.Context, params OrderRefParams) (OrderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetOrder(ctx, params.WorkspaceID, params.ID)
}

func (a *Admin) GetOrderByPublicID(ctx context.Context, params OrderPublicRefParams) (OrderModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetOrderByPublicID(ctx, params.WorkspaceID, params.PublicID)
}

func (a *Admin) UpdateOrderStatus(ctx context.Context, workspaceID string, id uint64, status string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpdateOrderStatus(ctx, workspaceID, id, status)
}

func (a *Admin) ListPaymentAttempts(ctx context.Context, params AttemptListParams) ([]PaymentAttemptModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListPaymentAttempts(ctx, paymentsqlc.AdminListPaymentAttemptsParams{
		WorkspaceID:  params.WorkspaceID,
		Column2:      int64(params.OrderID),
		OrderID:      int64(params.OrderID),
		Column4:      params.ProviderCode,
		ProviderCode: params.ProviderCode,
		Column6:      params.Status,
		Status:       paymentsqlc.PaymentAttemptStatus(params.Status),
		Limit:        limit,
		Offset:       offset,
	})
}

func (a *Admin) GetPaymentAttempt(ctx context.Context, params AttemptRefParams) (PaymentAttemptModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetPaymentAttempt(ctx, params.WorkspaceID, params.ID)
}

func (a *Admin) UpdatePaymentAttemptStatus(ctx context.Context, params AttemptStatusParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	_, err := a.repository.AdminUpdatePaymentAttemptStatus(
		ctx,
		params.WorkspaceID,
		params.ID,
		paymentsqlc.PaymentAttemptStatus(params.Status),
	)
	return err
}

func (a *Admin) ListPaymentEvents(ctx context.Context, params EventListParams) ([]PaymentEventModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListPaymentEvents(ctx, paymentsqlc.AdminListPaymentEventsParams{
		WorkspaceID:      params.WorkspaceID,
		WorkspaceID_2:    params.WorkspaceID,
		Column3:          params.ProviderCode,
		ProviderCode:     params.ProviderCode,
		Column5:          params.ProcessingStatus,
		ProcessingStatus: paymentsqlc.PaymentEventProcessingStatus(params.ProcessingStatus),
		Limit:            limit,
		Offset:           offset,
	})
}

func (a *Admin) GetPaymentEvent(ctx context.Context, params EventRefParams) (PaymentEventModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetPaymentEvent(ctx, params.WorkspaceID, params.ID)
}

func (a *Admin) UpdatePaymentEventProcessingStatus(ctx context.Context, params EventStatusParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	_, err := a.repository.AdminUpdatePaymentEventProcessingStatus(
		ctx,
		paymentsqlc.AdminUpdatePaymentEventStatusForWorkspaceParams{
			ProcessingStatus: paymentsqlc.PaymentEventProcessingStatus(params.Status),
			ProcessingError:  sql.NullString{String: params.Message, Valid: params.Message != ""},
			WorkspaceID:      params.WorkspaceID,
			ID:               int64(params.ID),
		},
	)
	return err
}

func (a *Admin) ListSubscriptions(ctx context.Context, params SubscriptionListParams) ([]SubscriptionModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListSubscriptions(ctx, paymentsqlc.AdminListSubscriptionsParams{
		WorkspaceID:    params.WorkspaceID,
		Column2:        params.ProviderCode,
		ProviderCode:   params.ProviderCode,
		Column4:        params.ProductID,
		ProductID:      params.ProductID,
		Column6:        params.Status,
		Status:         paymentsqlc.PaymentSubscriptionStatus(params.Status),
		Column8:        params.PlatformID,
		PlatformID:     params.PlatformID,
		Column10:       params.PlatformUserID,
		PlatformUserID: params.PlatformUserID,
		Limit:          limit,
		Offset:         offset,
	})
}

func (a *Admin) GetSubscription(ctx context.Context, workspaceID string, id uint64) (SubscriptionModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetSubscription(
		ctx,
		paymentsqlc.AdminGetSubscriptionParams{WorkspaceID: workspaceID, ID: int64(id)},
	)
}

func (a *Admin) GetSubscriptionByProviderID(
	ctx context.Context,
	params SubscriptionProviderRefParams,
) (SubscriptionModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetSubscriptionByProviderID(
		ctx,
		paymentsqlc.AdminGetSubscriptionByProviderIDForWorkspaceParams{
			WorkspaceID:            params.WorkspaceID,
			ProviderCode:           params.ProviderCode,
			ProviderSubscriptionID: params.ProviderSubscriptionID,
		},
	)
}

func (a *Admin) UpsertSubscription(ctx context.Context, params SubscriptionUpsertParams) (uint64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpsertSubscription(ctx, repository.SubscriptionUpsertParams{
		WorkspaceID:            params.WorkspaceID,
		ProviderCode:           params.ProviderCode,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
		AppID:                  params.AppID,
		PlatformID:             params.PlatformID,
		PlatformUserID:         params.PlatformUserID,
		InternalUserID:         params.InternalUserID,
		ProductID:              params.ProductID,
		OrderID:                params.OrderID,
		AttemptID:              params.AttemptID,
		Status:                 params.Status,
		CancelReason:           params.CancelReason,
		StartedAt:              params.StartedAt,
		EndedAt:                params.EndedAt,
	})
}

func (a *Admin) UpdateSubscriptionStatus(ctx context.Context, params SubscriptionStatusUpdateParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.UpdateSubscriptionStatus(ctx, repository.SubscriptionStatusUpdateParams{
		WorkspaceID:            params.WorkspaceID,
		ProviderCode:           params.ProviderCode,
		ProviderSubscriptionID: params.ProviderSubscriptionID,
		Status:                 params.Status,
		CancelReason:           params.CancelReason,
		EndedAt:                params.EndedAt,
	})
}

func (a *Admin) ListFulfillments(ctx context.Context, params FulfillmentListParams) ([]FulfillmentModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListFulfillments(ctx, paymentsqlc.AdminListFulfillmentsParams{
		WorkspaceID: params.WorkspaceID,
		Column2:     params.Status,
		Status:      paymentsqlc.PaymentFulfillmentStatus(params.Status),
		Column4:     int64(params.OrderID),
		OrderID:     int64(params.OrderID),
		Limit:       limit,
		Offset:      offset,
	})
}

func (a *Admin) GetFulfillment(ctx context.Context, params FulfillmentRefParams) (FulfillmentModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetFulfillment(ctx, params.WorkspaceID, params.ID)
}

func (a *Admin) UpdateFulfillmentStatus(ctx context.Context, params FulfillmentStatusParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminUpdateFulfillmentStatus(ctx, paymentsqlc.AdminUpdateFulfillmentStatusForWorkspaceParams{
		Status:      paymentsqlc.PaymentFulfillmentStatus(params.Status),
		Error:       sql.NullString{String: params.Message, Valid: params.Message != ""},
		WorkspaceID: params.WorkspaceID,
		ID:          int64(params.ID),
	})
}

func (a *Admin) ListFulfillmentItems(
	ctx context.Context,
	params FulfillmentItemListParams,
) ([]FulfillmentItemModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListFulfillmentItems(ctx, paymentsqlc.AdminListFulfillmentItemsParams{
		WorkspaceID:   params.WorkspaceID,
		Column2:       int64(params.FulfillmentID),
		FulfillmentID: int64(params.FulfillmentID),
		Limit:         limit,
		Offset:        offset,
	})
}

func (a *Admin) CreateRefund(ctx context.Context, params RefundCreateParams) (uint64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	status := params.Status
	if status == "" {
		status = string(paymentsqlc.PaymentRefundStatusCreated)
	}
	return a.repository.CreateRefund(ctx, repository.RefundCreateParams{
		WorkspaceID:      params.WorkspaceID,
		OrderID:          params.OrderID,
		AttemptID:        params.AttemptID,
		ProviderCode:     params.ProviderCode,
		ProviderRefundID: params.ProviderRefundID,
		AmountMinor:      params.AmountMinor,
		AssetCode:        params.AssetCode,
		Status:           status,
		Reason:           params.Reason,
	})
}

func (a *Admin) ListRefunds(ctx context.Context, params RefundListParams) ([]RefundModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	limit, offset := normalizePage(params.Page)
	return a.repository.AdminListRefunds(ctx, paymentsqlc.AdminListRefundsParams{
		WorkspaceID:  params.WorkspaceID,
		Column2:      int64(params.OrderID),
		OrderID:      int64(params.OrderID),
		Column4:      params.ProviderCode,
		ProviderCode: params.ProviderCode,
		Column6:      params.Status,
		Status:       paymentsqlc.PaymentRefundStatus(params.Status),
		Limit:        limit,
		Offset:       offset,
	})
}

func (a *Admin) GetRefund(ctx context.Context, params RefundRefParams) (RefundModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminGetRefund(ctx, params.WorkspaceID, params.ID)
}

func (a *Admin) UpdateRefundStatus(ctx context.Context, params RefundStatusParams) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	return a.repository.AdminUpdateRefundStatus(ctx, params.WorkspaceID, params.ID, params.Status, params.Reason)
}
