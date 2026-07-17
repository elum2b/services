package ton

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/elum2b/services/payment/repository"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	"github.com/xssnick/tonutils-go/address"
)

type JettonAsset struct {
	Code            string
	Decimals        uint16
	ContractAddress string
}

func (a *TON) ResolveJettonAsset(ctx context.Context, network string, masterAddress string) (JettonAsset, error) {
	mergedCtx, paymentRequestCancel := a.withContext(ctx)
	defer paymentRequestCancel()
	ctx = mergedCtx
	network, err := validateNetwork(network)
	if err != nil {
		return JettonAsset{}, err
	}
	masterAddress = strings.TrimSpace(masterAddress)
	if masterAddress == "" {
		return JettonAsset{}, ErrJettonMasterAddressRequired
	}

	candidates := []string{masterAddress}
	if parsed, err := parseTONAddress(masterAddress); err == nil {
		candidates = appendUnique(candidates, parsed.String(), parsed.StringRaw())
	}

	for _, candidate := range candidates {
		asset, err := a.repository.GetAssetByChainContract(ctx, paymentsqlc.GetAssetByChainContractParams{
			Chain:           sql.NullString{String: "ton", Valid: true},
			Network:         sql.NullString{String: network, Valid: true},
			ContractAddress: sql.NullString{String: candidate, Valid: true},
		})
		if err == nil {
			return jettonAssetFromRow(asset), nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return JettonAsset{}, err
		}
	}

	// Friendly TON addresses can differ in bounce/testnet flags while still
	// referring to the same account. Compare their canonical raw addresses.
	master, err := parseTONAddress(masterAddress)
	if err == nil {
		assets, listErr := a.repository.ListAssets(ctx)
		if listErr != nil {
			return JettonAsset{}, listErr
		}
		masterRaw := master.StringRaw()
		for _, asset := range assets {
			if asset.AssetKind != string(paymentsqlc.PaymentAssetAssetKindCryptoJetton) ||
				!asset.Chain.Valid || !strings.EqualFold(asset.Chain.String, "ton") ||
				!asset.Network.Valid || normalizeNetwork(asset.Network.String) != network ||
				!asset.ContractAddress.Valid {
				continue
			}
			contract, parseErr := parseTONAddress(asset.ContractAddress.String)
			if parseErr == nil && contract.StringRaw() == masterRaw {
				return jettonAssetFromRow(asset), nil
			}
		}
	}

	return JettonAsset{}, ErrJettonAssetNotFound
}

func jettonAssetFromRow(asset repository.AdminAssetModel) JettonAsset {
	return JettonAsset{
		Code:            asset.Code,
		Decimals:        uint16(asset.Scale),
		ContractAddress: asset.ContractAddress.String,
	}
}

func parseTONAddress(value string) (*address.Address, error) {
	if parsed, err := address.ParseAddr(value); err == nil {
		return parsed, nil
	}
	return address.ParseRawAddr(value)
}

func appendUnique(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		found := slices.Contains(values, candidate)
		if !found {
			values = append(values, candidate)
		}
	}
	return values
}
