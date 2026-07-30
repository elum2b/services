package repository

import (
	"context"
	"database/sql"
	"errors"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	"github.com/elum2b/services/payment/tonconnect"
)

func (r *PaymentRepository) UpsertTONWallet(ctx context.Context, params paymentsqlc.UpsertTONWalletParams) error {
	if _, err := requireWorkspaceID(params.WorkspaceID); err != nil {
		return err
	}

	if err := r.q.UpsertTONWallet(ctx, params); err != nil {
		return err
	}

	return r.invalidateTONManifestCache(params.WorkspaceID)
}

func (r *PaymentRepository) DeleteTONWallet(ctx context.Context, workspaceID string) (int64, error) {
	if _, err := requireWorkspaceID(workspaceID); err != nil {
		return 0, err
	}

	rows, err := r.q.DeleteTONWallet(ctx, workspaceID)
	if err != nil {
		return 0, err
	}

	return rows, r.invalidateTONManifestCache(workspaceID)
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

	entry, err := queryPaymentVersionedCache(
		ctx,
		r,
		paymentTONManifestVersionScope(workspaceID),
		paymentCacheKey("ton_manifest", workspaceID),
		paymentTONManifestCacheTTL,
		paymentTONManifestCacheTTL,
		func(ctx context.Context) (tonManifestCacheEntry, error) {
			row, err := r.q.GetEnabledTONConnectManifest(ctx, workspaceID)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return tonManifestCacheEntry{}, nil
				}

				return tonManifestCacheEntry{}, err
			}

			return tonManifestCacheEntry{
				Found: true,
				Manifest: tonconnect.Manifest{
					URL:              row.ManifestAppUrl,
					Name:             row.ManifestName,
					IconURL:          row.ManifestIconUrl,
					TermsOfUseURL:    exportNullStringPtr(row.ManifestTermsOfUseUrl),
					PrivacyPolicyURL: exportNullStringPtr(row.ManifestPrivacyPolicyUrl),
				},
			}, nil
		},
	)
	if err != nil {
		return tonconnect.Manifest{}, err
	}
	if !entry.Found {
		return tonconnect.Manifest{}, sql.ErrNoRows
	}

	return cloneTONConnectManifest(entry.Manifest), nil
}

type tonManifestCacheEntry struct {
	Manifest tonconnect.Manifest `json:"manifest"`
	Found    bool                `json:"found"`
}

func cloneTONConnectManifest(manifest tonconnect.Manifest) tonconnect.Manifest {
	clone := manifest
	if manifest.TermsOfUseURL != nil {
		value := *manifest.TermsOfUseURL
		clone.TermsOfUseURL = &value
	}
	if manifest.PrivacyPolicyURL != nil {
		value := *manifest.PrivacyPolicyURL
		clone.PrivacyPolicyURL = &value
	}

	return clone
}
