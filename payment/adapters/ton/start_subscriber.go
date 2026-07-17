package ton

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	services "github.com/elum2b/services"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

type SubscriberParams struct {
	WorkspaceID      string
	Network          string
	NetworkConfigURL string
	WalletAddress    string
}

func (a *TON) StartSubscriber(ctx context.Context, params SubscriberParams) (*Sub, error) {
	params.NetworkConfigURL = strings.TrimSpace(params.NetworkConfigURL)
	params.WalletAddress = strings.TrimSpace(params.WalletAddress)
	if err := services.ValidateWorkspaceID(params.WorkspaceID); err != nil {
		return nil, err
	}
	if params.WalletAddress == "" {
		return nil, ErrWalletAddressRequired
	}
	network, err := validateNetwork(params.Network)
	if err != nil {
		return nil, err
	}
	params.WalletAddress, err = NormalizeWalletAddress(params.WalletAddress, network)
	if err != nil {
		return nil, err
	}
	if params.NetworkConfigURL == "" {
		params.NetworkConfigURL = defaultNetworkConfigURL(network)
	}
	lastLT := uint64(0)
	cursor, err := a.repository.GetProviderCursor(ctx, paymentsqlc.GetProviderCursorParams{
		WorkspaceID:  params.WorkspaceID,
		ProviderCode: ProviderCode,
		Network:      network,
		SourceKey:    params.WalletAddress,
	})
	if err == nil {
		lastLT = uint64(cursor.CursorSequence)
	} else if err != sql.ErrNoRows {
		return nil, err
	}

	runCtx, cancel := a.bindContext(ctx)
	sub, err := NewSub(runCtx, cancel, params.WalletAddress, params.NetworkConfigURL, lastLT)
	if err != nil {
		cancel()
		return nil, err
	}
	sub.onClose = func() {
		a.unregisterSubscriber(sub)
	}
	a.registerSubscriber(sub)

	sub.OnTON(func(tx *RootTON) error {
		amount, err := strconv.ParseUint(tx.Amount, 10, 64)
		if err != nil {
			return err
		}
		_, err = a.ProcessTransfer(runCtx, IncomingTransfer{
			WorkspaceID:        params.WorkspaceID,
			Network:            network,
			WalletAddress:      params.WalletAddress,
			AssetCode:          AssetTON,
			TxHash:             tx.Body.TxHash,
			LogicalTime:        tx.CreatedLT,
			SourceAddress:      tx.SrcAddr,
			DestinationAddress: tx.DstAddr,
			AmountMinor:        amount,
			Comment:            tx.Body.Message,
		})
		return err
	})

	sub.OnJetton(func(tx *RootJetton) error {
		amount, err := strconv.ParseUint(tx.Body.Amount, 10, 64)
		if err != nil {
			return err
		}
		masterAddress, err := sub.JettonMasterAddress(runCtx, tx.SrcAddr)
		if err != nil {
			return err
		}
		asset, err := a.ResolveJettonAsset(runCtx, network, masterAddress)
		if err != nil {
			return err
		}
		_, err = a.ProcessTransfer(runCtx, IncomingTransfer{
			WorkspaceID:        params.WorkspaceID,
			Network:            network,
			WalletAddress:      params.WalletAddress,
			AssetCode:          asset.Code,
			TxHash:             tx.Body.TxHash,
			LogicalTime:        tx.CreatedLT,
			SourceAddress:      tx.SrcAddr,
			DestinationAddress: tx.DstAddr,
			AmountMinor:        amount,
			Comment:            tx.Body.Message,
			JettonSender:       tx.Body.Sender,
		})
		return err
	})
	sub.Start()

	return sub, nil
}
