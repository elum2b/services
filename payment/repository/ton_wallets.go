package repository

import (
	"context"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	"github.com/elum2b/services/payment/tonconnect"
)

func (r *PaymentRepository) UpsertTONWallet(ctx context.Context, params paymentsqlc.UpsertTONWalletParams) error {
	if _, err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}

	return r.q.UpsertTONWallet(ctx, params)
}

func (r *PaymentRepository) DeleteTONWallet(ctx context.Context, workspaceID string) (int64, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	return r.q.DeleteTONWallet(ctx, workspaceID)
}

func (r *PaymentRepository) AdminGetTONWallet(
	ctx context.Context,
	workspaceID string,
) (AdminTONWalletModel, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return AdminTONWalletModel{}, err
	}

	row, err := r.q.AdminGetTONWallet(ctx, workspaceID)
	return mapAdminResult(row, err, mapAdminTONWallet)
}

func (r *PaymentRepository) ListEnabledTONWallets(ctx context.Context) ([]paymentsqlc.PaymentTonWallet, error) {
	return r.q.ListEnabledTONWallets(ctx)
}

func (r *PaymentRepository) GetEnabledTONWalletForWorkspace(
	ctx context.Context,
	workspaceID string,
) (paymentsqlc.PaymentTonWallet, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return paymentsqlc.PaymentTonWallet{}, err
	}

	return r.q.GetEnabledTONWalletForWorkspace(ctx, workspaceID)
}

func (r *PaymentRepository) GetEnabledTONConnectManifest(
	ctx context.Context,
	workspaceID string,
) (tonconnect.Manifest, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return tonconnect.Manifest{}, err
	}

	row, err := r.q.GetEnabledTONConnectManifest(ctx, workspaceID)
	if err != nil {
		return tonconnect.Manifest{}, err
	}

	return tonconnect.Manifest{
		URL:              row.ManifestAppUrl,
		Name:             row.ManifestName,
		IconURL:          row.ManifestIconUrl,
		TermsOfUseURL:    exportNullStringPtr(row.ManifestTermsOfUseUrl),
		PrivacyPolicyURL: exportNullStringPtr(row.ManifestPrivacyPolicyUrl),
	}, nil
}
