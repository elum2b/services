package repository

import (
	"context"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (r *PaymentRepository) GetAsset(ctx context.Context, code string) (paymentsqlc.PaymentAsset, error) {
	key := paymentCacheKey("asset", code)
	return queryPaymentCache(
		ctx,
		r,
		paymentGlobalCacheScope,
		key,
		func(ctx context.Context) (paymentsqlc.PaymentAsset, error) {
			return r.q.GetAsset(ctx, code)
		},
	)
}

func (r *PaymentRepository) GetAssetByChainContract(
	ctx context.Context,
	params paymentsqlc.GetAssetByChainContractParams,
) (AdminAssetModel, error) {
	key := paymentCacheKey("asset_chain_contract", params.Chain, params.Network, params.ContractAddress)
	row, err := queryPaymentCache(
		ctx,
		r,
		paymentGlobalCacheScope,
		key,
		func(ctx context.Context) (paymentsqlc.PaymentAsset, error) {
			return r.q.GetAssetByChainContract(ctx, params)
		},
	)
	return mapAdminResult(row, err, mapAdminAsset)
}

func (r *PaymentRepository) GetProviderCursor(
	ctx context.Context,
	params paymentsqlc.GetProviderCursorParams,
) (AdminProviderCursorModel, error) {
	row, err := r.q.GetProviderCursor(ctx, params)
	return mapAdminResult(row, err, mapAdminProviderCursor)
}

func (r *PaymentRepository) UpsertProviderCursor(
	ctx context.Context,
	params paymentsqlc.UpsertProviderCursorParams,
) (int64, error) {
	return r.q.UpsertProviderCursor(ctx, params)
}

func (r *PaymentRepository) GetProviderTransactionByExternalID(
	ctx context.Context,
	params paymentsqlc.GetProviderTransactionByExternalIDParams,
) (AdminProviderTransactionModel, error) {
	row, err := r.q.GetProviderTransactionByExternalID(ctx, params)
	return mapAdminResult(row, err, mapAdminProviderTransaction)
}

func (r *PaymentRepository) CreateProviderTransaction(
	ctx context.Context,
	params paymentsqlc.CreateProviderTransactionParams,
) (uint64, error) {
	id, err := r.q.CreateProviderTransaction(ctx, params)
	return uint64(id), err
}

func (r *PaymentRepository) StoreProviderTransaction(
	ctx context.Context,
	transaction paymentsqlc.CreateProviderTransactionParams,
	cursor paymentsqlc.UpsertProviderCursorParams,
) (uint64, error) {
	var id uint64
	err := r.WithTx(ctx, func(tx *PaymentRepository) error {
		var err error
		createdID, err := tx.CreateProviderTransaction(ctx, transaction)
		if err != nil {
			return err
		}
		id = uint64(createdID)
		_, err = tx.UpsertProviderCursor(ctx, cursor)
		return err
	})
	return id, err
}

func (r *PaymentRepository) RecoverFailedProviderTransaction(
	ctx context.Context,
	transaction paymentsqlc.RecoverProviderTransactionParams,
	cursor paymentsqlc.UpsertProviderCursorParams,
) (bool, error) {

	var recovered bool
	err := r.WithTx(ctx, func(tx *PaymentRepository) error {
		updated, err := tx.q.RecoverProviderTransaction(ctx, transaction)
		if err != nil {
			return err
		}

		recovered = updated == 1
		_, err = tx.UpsertProviderCursor(ctx, cursor)

		return err
	})

	return recovered, err

}

func (r *PaymentRepository) AdminListProviderCursors(
	ctx context.Context,
	params paymentsqlc.AdminListProviderCursorsParams,
) ([]AdminProviderCursorModel, error) {
	rows, err := r.q.AdminListProviderCursors(ctx, params)
	return mapAdminSlice(rows, mapAdminProviderCursor), err
}

func (r *PaymentRepository) AdminListProviderTransactions(
	ctx context.Context,
	params paymentsqlc.AdminListProviderTransactionsParams,
) ([]AdminProviderTransactionModel, error) {
	rows, err := r.q.AdminListProviderTransactions(ctx, params)
	return mapAdminSlice(rows, mapAdminProviderTransaction), err
}

func (r *PaymentRepository) AdminGetProviderTransaction(
	ctx context.Context,
	params paymentsqlc.AdminGetProviderTransactionParams,
) (AdminProviderTransactionModel, error) {
	row, err := r.q.AdminGetProviderTransaction(ctx, params)
	return mapAdminResult(row, err, mapAdminProviderTransaction)
}

func (r *PaymentRepository) AdminUpdateProviderTransactionStatus(
	ctx context.Context,
	params paymentsqlc.AdminUpdateProviderTransactionStatusParams,
) (int64, error) {
	return r.q.AdminUpdateProviderTransactionStatus(ctx, params)
}
