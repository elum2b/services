package ton

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/elum2b/services/payment/repository"
)

func (a *TON) CreatePayment(ctx context.Context, params CreatePaymentParams) (*CreatePaymentResponse, error) {

	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	assetCode := normalizeAsset(params.AssetCode)

	asset, err := a.repository.GetAsset(ctx, assetCode)
	if err != nil {
		return nil, err
	}

	if asset.Chain.Valid && asset.Chain.String != "" && !strings.EqualFold(asset.Chain.String, "ton") {
		return nil, ErrAssetChainMismatch
	}

	wallet, err := a.repository.GetEnabledTONWalletForWorkspace(ctx, params.WorkspaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrWalletNotConfigured
		}
		return nil, err
	}

	network, err := validateNetwork(wallet.Network)
	if err != nil {
		return nil, err
	}

	if asset.Network.Valid && asset.Network.String != "" && normalizeNetwork(asset.Network.String) != network {
		return nil, ErrAssetNetworkMismatch
	}

	walletAddress := strings.TrimSpace(wallet.WalletAddress)
	if walletAddress == "" {
		return nil, ErrWalletNotConfigured
	}
	walletAddress, err = NormalizeWalletAddress(walletAddress, network)
	if err != nil {
		return nil, err
	}

	order, err := a.repository.CreateOrder(ctx, repository.OrderCreateParams{
		WorkspaceID:    params.WorkspaceID,
		AppID:          params.AppID,
		PlatformID:     params.PlatformID,
		PlatformUserID: params.PlatformUserID,
		InternalUserID: params.InternalUserID,
		ProductID:      params.ProductID,
		Quantity:       params.Quantity,
		AssetCode:      assetCode,
		Locale:         normalizeLocale(params.Locale),
		ReservedUntil:  params.ReservedUntil,
		ExpiresAt:      params.ExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	comment := order.PublicID
	attempt, err := a.repository.CreateAttempt(ctx, repository.AttemptCreateParams{
		OrderID:           order.ID,
		ProviderCode:      ProviderCode,
		ProviderPaymentID: utils.Ref(comment),
		IdempotencyKey:    utils.Ref(fmt.Sprintf("%s:%s", ProviderCode, comment)),
	})
	if err != nil {
		return nil, err
	}

	return &CreatePaymentResponse{
		OrderID:        order.ID,
		OrderPublicID:  order.PublicID,
		AttemptID:      attempt.ID,
		WalletAddress:  walletAddress,
		Network:        network,
		AssetCode:      attempt.AssetCode,
		AmountMinor:    attempt.AmountMinor,
		Comment:        comment,
		Decimals:       uint16(asset.Scale),
		ProviderStatus: attempt.Status,
	}, nil
}

func normalizeAsset(assetCode string) string {
	assetCode = strings.ToUpper(strings.TrimSpace(assetCode))
	if assetCode == "" {
		return AssetTON
	}
	return assetCode
}
