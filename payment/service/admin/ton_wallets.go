package admin

import (
	"context"
	"database/sql"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	paymentton "github.com/elum2b/services/payment/adapters/ton"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

func (a *Admin) SaveTONWallet(ctx context.Context, params TONWalletUpsertParams) error {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	network, err := paymentton.NormalizeNetwork(params.Network)
	if err != nil {
		return err
	}
	walletAddress, err := paymentton.NormalizeWalletAddress(params.WalletAddress, network)
	if err != nil {
		return err
	}
	return a.repository.UpsertTONWallet(mergedCtx, paymentsqlc.UpsertTONWalletParams{
		WorkspaceID:   params.WorkspaceID,
		Network:       network,
		WalletAddress: walletAddress,
		NetworkConfigUrl: sqlwrap.NullFromPtr(params.NetworkConfigURL, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		IsEnabled: params.IsEnabled,
	})
}

func (a *Admin) DeleteTONWallet(ctx context.Context, workspaceID string) (int64, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	return a.repository.DeleteTONWallet(mergedCtx, workspaceID)
}

func (a *Admin) GetTONWallet(ctx context.Context, workspaceID string) (TONWalletModel, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	return a.repository.AdminGetTONWallet(mergedCtx, workspaceID)
}
