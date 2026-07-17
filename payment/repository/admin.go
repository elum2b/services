package repository

import (
	"context"
	"strings"

	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

var (
	ErrAttemptStateInvalid = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"payment attempt state is invalid",
	)
	ErrFulfillmentStateInvalid = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"payment fulfillment state is invalid",
	)
	ErrAttemptStatusInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment attempt status is invalid",
	)
	ErrFulfillmentStatusInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment fulfillment status is invalid",
	)
)

func (r *PaymentRepository) AdminGetProvider(ctx context.Context, code string) (AdminProviderModel, error) {
	row, err := r.q.AdminGetProvider(ctx, code)
	return mapAdminResult(row, err, mapAdminProvider)
}

type ProviderUpsertParams struct {
	Code             string
	Title            string
	ProviderKind     string
	SupportsCreate   bool
	SupportsRedirect bool
	SupportsWebhook  bool
	SupportsRefund   bool
	IsActive         bool
}

func (r *PaymentRepository) UpsertProvider(ctx context.Context, params ProviderUpsertParams) error {
	if strings.TrimSpace(params.Code) == "" || strings.TrimSpace(params.Title) == "" ||
		!validProviderKind(params.ProviderKind) {
		return ErrInvalidProvider
	}
	if err := r.q.AdminUpsertProvider(ctx, paymentsqlc.AdminUpsertProviderParams{
		Code:             params.Code,
		Title:            params.Title,
		ProviderKind:     paymentsqlc.PaymentProviderProviderKind(params.ProviderKind),
		SupportsCreate:   params.SupportsCreate,
		SupportsRedirect: params.SupportsRedirect,
		SupportsWebhook:  params.SupportsWebhook,
		SupportsRefund:   params.SupportsRefund,
		IsActive:         params.IsActive,
	}); err != nil {
		return err
	}
	return r.invalidateAllCache()
}

func (r *PaymentRepository) AdminDeleteProvider(ctx context.Context, code string) (int64, error) {
	rows, err := r.q.AdminDeleteProvider(ctx, code)
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateAllCache()
}

func (r *PaymentRepository) AdminGetAsset(ctx context.Context, code string) (AdminAssetModel, error) {
	row, err := r.q.AdminGetAsset(ctx, code)
	return mapAdminResult(row, err, mapAdminAsset)
}

func (r *PaymentRepository) AdminUpsertAsset(ctx context.Context, params paymentsqlc.UpsertAssetParams) error {
	return r.UpsertAsset(ctx, AssetUpsertParams{
		Code:            params.Code,
		Title:           params.Title,
		AssetKind:       string(params.AssetKind),
		Scale:           uint16(params.Scale),
		Chain:           sqlwrap.NullStringPtr(params.Chain),
		Network:         sqlwrap.NullStringPtr(params.Network),
		ContractAddress: sqlwrap.NullStringPtr(params.ContractAddress),
		IsActive:        params.IsActive,
	})
}

func (r *PaymentRepository) AdminDeleteAsset(ctx context.Context, code string) (int64, error) {
	var rows int64
	err := r.inTransaction(ctx, func(tx *PaymentRepository) error {
		if _, err := tx.q.DeleteAssetRatesForAsset(ctx, paymentsqlc.DeleteAssetRatesForAssetParams{
			AssetCode:          code,
			ReferenceAssetCode: code,
		}); err != nil {
			return err
		}
		deleted, err := tx.q.DeleteAsset(ctx, code)
		if err != nil {
			return err
		}
		rows = deleted
		return nil
	})
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateAllCache()
}

func (r *PaymentRepository) AdminListProviderAssets(
	ctx context.Context,
	params paymentsqlc.AdminListProviderAssetsParams,
) ([]AdminProviderAssetModel, error) {
	rows, err := r.q.AdminListProviderAssets(ctx, params)
	return mapAdminSlice(rows, mapAdminProviderAsset), err
}

func (r *PaymentRepository) AdminUpsertProviderAsset(
	ctx context.Context,
	params paymentsqlc.UpsertProviderAssetParams,
) error {
	return r.UpsertProviderAsset(ctx, ProviderAssetUpsertParams{
		ProviderCode:    params.ProviderCode,
		AssetCode:       params.AssetCode,
		MinAmountMinor:  nullInt64Ptr(params.MinAmountMinor),
		MaxAmountMinor:  nullInt64Ptr(params.MaxAmountMinor),
		MerchantAccount: sqlwrap.NullStringPtr(params.MerchantAccount),
		IsActive:        params.IsActive,
	})
}

func validProviderKind(value string) bool {
	switch value {
	case string(paymentsqlc.PaymentProviderProviderKindPlatformInternal),
		string(paymentsqlc.PaymentProviderProviderKindFiatGateway),
		string(paymentsqlc.PaymentProviderProviderKindCryptoChain):
		return true
	default:
		return false
	}
}

func (r *PaymentRepository) AdminDeleteProviderAsset(
	ctx context.Context,
	params paymentsqlc.DeleteProviderAssetParams,
) (int64, error) {
	rows, err := r.q.DeleteProviderAsset(ctx, params)
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateAllCache()
}

func (r *PaymentRepository) AdminListProductGroups(
	ctx context.Context,
	params paymentsqlc.AdminListProductGroupsParams,
) ([]AdminProductGroupModel, error) {
	rows, err := r.q.AdminListProductGroups(ctx, params)
	return mapAdminSlice(rows, mapAdminProductGroup), err
}

func (r *PaymentRepository) AdminGetProductGroup(
	ctx context.Context,
	params paymentsqlc.AdminGetProductGroupParams,
) (AdminProductGroupModel, error) {
	row, err := r.q.AdminGetProductGroup(ctx, params)
	return mapAdminResult(row, err, mapAdminProductGroup)
}

func (r *PaymentRepository) AdminListLocalizations(
	ctx context.Context,
	params paymentsqlc.AdminListLocalizationsParams,
) ([]AdminLocalizationModel, error) {
	rows, err := r.q.AdminListLocalizations(ctx, params)
	return mapAdminSlice(rows, mapAdminLocalization), err
}

func (r *PaymentRepository) AdminGetLocalization(
	ctx context.Context,
	params paymentsqlc.AdminGetLocalizationParams,
) (AdminLocalizationModel, error) {
	row, err := r.q.AdminGetLocalization(ctx, params)
	return mapAdminResult(row, err, mapAdminLocalization)
}

func (r *PaymentRepository) AdminListProducts(
	ctx context.Context,
	params paymentsqlc.AdminListProductsParams,
) ([]AdminProductModel, error) {
	rows, err := r.q.AdminListProducts(ctx, params)
	return mapAdminSlice(rows, mapAdminProduct), err
}

func (r *PaymentRepository) AdminGetProduct(
	ctx context.Context,
	params paymentsqlc.AdminGetProductParams,
) (AdminProductModel, error) {
	row, err := r.q.AdminGetProduct(ctx, params)
	return mapAdminResult(row, err, mapAdminProduct)
}

func (r *PaymentRepository) AdminListProductItems(
	ctx context.Context,
	params paymentsqlc.AdminListProductItemsParams,
) ([]AdminProductItemModel, error) {
	rows, err := r.q.AdminListProductItems(ctx, params)
	return mapAdminSlice(rows, mapAdminProductItem), err
}

func (r *PaymentRepository) AdminListPrices(
	ctx context.Context,
	params paymentsqlc.AdminListPricesParams,
) ([]AdminPriceModel, error) {
	rows, err := r.q.AdminListPrices(ctx, params)
	return mapAdminSlice(rows, mapAdminPrice), err
}

func (r *PaymentRepository) AdminGetPrice(
	ctx context.Context,
	params paymentsqlc.AdminGetPriceParams,
) (AdminPriceModel, error) {
	row, err := r.q.AdminGetPrice(ctx, params)
	return mapAdminResult(row, err, mapAdminPrice)
}

func (r *PaymentRepository) AdminGetAssetRate(
	ctx context.Context,
	params paymentsqlc.AdminGetAssetRateParams,
) (AdminAssetRateModel, error) {
	row, err := r.q.AdminGetAssetRate(ctx, params)
	return mapAdminResult(row, err, mapAdminAssetRate)
}

func (r *PaymentRepository) AdminListAssetRates(
	ctx context.Context,
	params paymentsqlc.AdminListAssetRatesParams,
) ([]AdminAssetRateModel, error) {
	rows, err := r.q.AdminListAssetRates(ctx, params)
	return mapAdminSlice(rows, mapAdminAssetRate), err
}

func (r *PaymentRepository) AdminListProductLimitCounters(
	ctx context.Context,
	params paymentsqlc.AdminListProductLimitCountersParams,
) ([]AdminProductLimitCounterModel, error) {
	rows, err := r.q.AdminListProductLimitCounters(ctx, params)
	return mapAdminSlice(rows, mapAdminProductLimitCounter), err
}

func (r *PaymentRepository) AdminDeleteProductLimitCounter(
	ctx context.Context,
	params paymentsqlc.AdminDeleteProductLimitCounterParams,
) (int64, error) {
	return r.q.AdminDeleteProductLimitCounter(ctx, params)
}

func (r *PaymentRepository) AdminListPurchaseKeys(
	ctx context.Context,
	params paymentsqlc.AdminListPurchaseKeysParams,
) ([]AdminPurchaseKeyModel, error) {
	rows, err := r.q.AdminListPurchaseKeys(ctx, params)
	return mapAdminSlice(rows, mapAdminPurchaseKey), err
}

func (r *PaymentRepository) AdminGetPurchaseKey(
	ctx context.Context,
	params paymentsqlc.AdminGetPurchaseKeyParams,
) (AdminPurchaseKeyModel, error) {
	row, err := r.q.AdminGetPurchaseKey(ctx, params)
	return mapAdminResult(row, err, mapAdminPurchaseKey)
}

func (r *PaymentRepository) AdminUpdatePurchaseKeyStatus(
	ctx context.Context,
	params paymentsqlc.AdminUpdatePurchaseKeyStatusParams,
) (int64, error) {
	return r.q.AdminUpdatePurchaseKeyStatus(ctx, params)
}

func (r *PaymentRepository) AdminListOrders(
	ctx context.Context,
	params paymentsqlc.AdminListOrdersParams,
) ([]AdminOrderModel, error) {
	rows, err := r.q.AdminListOrders(ctx, params)
	return mapAdminSlice(rows, mapAdminOrder), err
}

func (r *PaymentRepository) AdminGetOrder(
	ctx context.Context,
	workspaceID string,
	id uint64,
) (AdminOrderModel, error) {
	row, err := r.q.AdminGetOrderForWorkspace(ctx, paymentsqlc.AdminGetOrderForWorkspaceParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
	})
	return mapAdminResult(row, err, mapAdminOrder)
}

func (r *PaymentRepository) AdminGetOrderByPublicID(
	ctx context.Context,
	workspaceID string,
	publicID string,
) (AdminOrderModel, error) {
	row, err := r.q.AdminGetOrderByPublicIDForWorkspace(ctx, paymentsqlc.AdminGetOrderByPublicIDForWorkspaceParams{
		WorkspaceID: workspaceID,
		PublicID:    publicID,
	})
	return mapAdminResult(row, err, mapAdminOrder)
}

func (r *PaymentRepository) AdminListPaymentAttempts(
	ctx context.Context,
	params paymentsqlc.AdminListPaymentAttemptsParams,
) ([]AdminPaymentAttemptModel, error) {
	rows, err := r.q.AdminListPaymentAttempts(ctx, params)
	return mapAdminSlice(rows, mapAdminPaymentAttempt), err
}

func (r *PaymentRepository) AdminGetPaymentAttempt(
	ctx context.Context,
	workspaceID string,
	id uint64,
) (AdminPaymentAttemptModel, error) {
	row, err := r.q.AdminGetPaymentAttemptForWorkspace(ctx, paymentsqlc.AdminGetPaymentAttemptForWorkspaceParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
	})
	return mapAdminResult(row, err, mapAdminPaymentAttempt)
}

func (r *PaymentRepository) AdminUpdatePaymentAttemptStatus(
	ctx context.Context,
	workspaceID string,
	id uint64,
	status paymentsqlc.PaymentAttemptStatus,
) (int64, error) {
	if !validPaymentAttemptStatus(status) {
		return 0, ErrAttemptStatusInvalid
	}

	rows, err := r.q.AdminUpdatePaymentAttemptStatusForWorkspace(
		ctx,
		paymentsqlc.AdminUpdatePaymentAttemptStatusForWorkspaceParams{
			Status:      status,
			WorkspaceID: workspaceID,
			ID:          int64(id),
		},
	)
	if err != nil || rows != 0 {
		return rows, err
	}

	_, err = r.q.AdminGetPaymentAttemptForWorkspace(
		ctx,
		paymentsqlc.AdminGetPaymentAttemptForWorkspaceParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		},
	)
	if err != nil {
		return 0, err
	}

	return 0, ErrAttemptStateInvalid
}

func (r *PaymentRepository) AdminListPaymentEvents(
	ctx context.Context,
	params paymentsqlc.AdminListPaymentEventsParams,
) ([]AdminPaymentEventModel, error) {
	rows, err := r.q.AdminListPaymentEvents(ctx, params)
	return mapAdminSlice(rows, mapAdminPaymentEvent), err
}

func (r *PaymentRepository) AdminGetPaymentEvent(
	ctx context.Context,
	workspaceID string,
	id uint64,
) (AdminPaymentEventModel, error) {
	row, err := r.q.AdminGetPaymentEventForWorkspace(ctx, paymentsqlc.AdminGetPaymentEventForWorkspaceParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
	})
	return mapAdminResult(row, err, mapAdminPaymentEvent)
}

func (r *PaymentRepository) AdminUpdatePaymentEventProcessingStatus(
	ctx context.Context,
	params paymentsqlc.AdminUpdatePaymentEventStatusForWorkspaceParams,
) (int64, error) {
	return r.q.AdminUpdatePaymentEventStatusForWorkspace(ctx, params)
}

func (r *PaymentRepository) AdminListSubscriptions(
	ctx context.Context,
	params paymentsqlc.AdminListSubscriptionsParams,
) ([]AdminSubscriptionModel, error) {
	rows, err := r.q.AdminListSubscriptions(ctx, params)
	return mapAdminSlice(rows, mapAdminSubscription), err
}

func (r *PaymentRepository) AdminGetSubscription(
	ctx context.Context,
	params paymentsqlc.AdminGetSubscriptionParams,
) (AdminSubscriptionModel, error) {
	row, err := r.q.AdminGetSubscription(ctx, params)
	return mapAdminResult(row, err, mapAdminSubscription)
}

func (r *PaymentRepository) AdminGetSubscriptionByProviderID(
	ctx context.Context,
	params paymentsqlc.AdminGetSubscriptionByProviderIDForWorkspaceParams,
) (AdminSubscriptionModel, error) {
	row, err := r.q.AdminGetSubscriptionByProviderIDForWorkspace(ctx, params)
	return mapAdminResult(row, err, mapAdminSubscription)
}

func (r *PaymentRepository) AdminUpsertSubscription(
	ctx context.Context,
	params paymentsqlc.UpsertPaymentSubscriptionParams,
) (uint64, error) {
	id, err := r.q.UpsertPaymentSubscription(ctx, params)
	return uint64(id), err
}

func (r *PaymentRepository) AdminListFulfillments(
	ctx context.Context,
	params paymentsqlc.AdminListFulfillmentsParams,
) ([]AdminFulfillmentModel, error) {
	rows, err := r.q.AdminListFulfillments(ctx, params)
	return mapAdminSlice(rows, mapAdminFulfillment), err
}

func (r *PaymentRepository) AdminGetFulfillment(
	ctx context.Context,
	workspaceID string,
	id uint64,
) (AdminFulfillmentModel, error) {
	row, err := r.q.AdminGetFulfillmentForWorkspace(ctx, paymentsqlc.AdminGetFulfillmentForWorkspaceParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
	})
	return mapAdminResult(row, err, mapAdminFulfillment)
}

func (r *PaymentRepository) AdminUpdateFulfillmentStatus(
	ctx context.Context,
	params paymentsqlc.AdminUpdateFulfillmentStatusForWorkspaceParams,
) (int64, error) {
	if !validPaymentFulfillmentStatus(params.Status) {
		return 0, ErrFulfillmentStatusInvalid
	}

	rows, err := r.q.AdminUpdateFulfillmentStatusForWorkspace(ctx, params)
	if err != nil || rows != 0 {
		return rows, err
	}

	_, err = r.q.AdminGetFulfillmentForWorkspace(
		ctx,
		paymentsqlc.AdminGetFulfillmentForWorkspaceParams{
			WorkspaceID: params.WorkspaceID,
			ID:          params.ID,
		},
	)
	if err != nil {
		return 0, err
	}

	return 0, ErrFulfillmentStateInvalid
}

func validPaymentAttemptStatus(status paymentsqlc.PaymentAttemptStatus) bool {
	switch status {
	case paymentsqlc.PaymentAttemptStatusCreated,
		paymentsqlc.PaymentAttemptStatusPending,
		paymentsqlc.PaymentAttemptStatusRequiresAction,
		paymentsqlc.PaymentAttemptStatusWaitingCapture,
		paymentsqlc.PaymentAttemptStatusSucceeded,
		paymentsqlc.PaymentAttemptStatusCanceled,
		paymentsqlc.PaymentAttemptStatusExpired,
		paymentsqlc.PaymentAttemptStatusRefunded,
		paymentsqlc.PaymentAttemptStatusChargebacked,
		paymentsqlc.PaymentAttemptStatusFailed:
		return true
	default:
		return false
	}
}

func validPaymentFulfillmentStatus(status paymentsqlc.PaymentFulfillmentStatus) bool {
	switch status {
	case paymentsqlc.PaymentFulfillmentStatusPending,
		paymentsqlc.PaymentFulfillmentStatusSucceeded,
		paymentsqlc.PaymentFulfillmentStatusRevoked,
		paymentsqlc.PaymentFulfillmentStatusFailed:
		return true
	default:
		return false
	}
}

func (r *PaymentRepository) AdminListFulfillmentItems(
	ctx context.Context,
	params paymentsqlc.AdminListFulfillmentItemsParams,
) ([]AdminFulfillmentItemModel, error) {
	rows, err := r.q.AdminListFulfillmentItems(ctx, params)
	return mapAdminSlice(rows, mapAdminFulfillmentItem), err
}

func (r *PaymentRepository) AdminListRefunds(
	ctx context.Context,
	params paymentsqlc.AdminListRefundsParams,
) ([]AdminRefundModel, error) {
	rows, err := r.q.AdminListRefunds(ctx, params)
	return mapAdminSlice(rows, mapAdminRefund), err
}

func (r *PaymentRepository) AdminGetRefund(
	ctx context.Context,
	workspaceID string,
	id uint64,
) (AdminRefundModel, error) {
	row, err := r.q.AdminGetRefundForWorkspace(ctx, paymentsqlc.AdminGetRefundForWorkspaceParams{
		WorkspaceID: workspaceID,
		ID:          int64(id),
	})
	return mapAdminResult(row, err, mapAdminRefund)
}
