package ton

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xssnick/tonutils-go/address"
	"github.com/xssnick/tonutils-go/liteclient"
	tonclient "github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/jetton"
	"github.com/xssnick/tonutils-go/tvm/cell"

	serviceerrors "github.com/elum2b/services/errors"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const (
	transactionKindTONNative             = "ton_native"
	transactionKindJettonTransfer        = "jetton_transfer"
	jettonTransferOpCode          uint64 = 0x0f8a7ea5
	jettonForwardAmountNano       uint64 = 1
	jettonGasAmountNano           uint64 = 50_000_000
)

func TransactionTON(destination string, amount *int, message ...string) *Transaction {
	msg := ""
	if len(message) > 0 {
		msg = message[0]
	}
	if amount == nil {
		zero := 0
		amount = &zero
	}

	return &Transaction{
		Kind:    transactionKindTONNative,
		Address: destination,
		Amount:  fmt.Sprintf("%v", *amount),
		Payload: base64.StdEncoding.EncodeToString(cell.BeginCell().
			MustStoreUInt(0, 32).
			MustStoreStringSnake(msg).
			EndCell().
			ToBOC()),
	}
}

func TonkeeperLink(tx *Transaction) string {
	if tx == nil || tx.Address == "" || tx.Amount == "" {
		return ""
	}

	values := url.Values{}
	values.Set("amount", tx.Amount)
	if tx.Payload != "" {
		values.Set("bin", tx.Payload)
	}

	return "https://app.tonkeeper.com/transfer/" + tx.Address + "?" + values.Encode()
}

func (a *TON) CreateTransaction(ctx context.Context, params CreateTransactionParams) (*Transaction, error) {
	mergedCtx, cancel := a.withContext(ctx)
	defer cancel()
	ctx = mergedCtx

	assetCode := normalizeAsset(params.AssetCode)
	network, err := validateNetwork(params.Network)
	if err != nil {
		return nil, err
	}

	destination := strings.TrimSpace(params.Destination)
	destination, err = normalizeTONAddress(destination, network, ErrDestinationRequired, ErrDestinationAddressInvalid)
	if err != nil {
		return nil, err
	}

	if assetCode == AssetTON {
		return transactionTONMinor(destination, params.AmountMinor, params.Comment)
	}

	asset, err := a.repository.GetAsset(ctx, assetCode)
	if err != nil {
		return nil, err
	}
	if asset.Chain.Valid && asset.Chain.String != "" && !strings.EqualFold(asset.Chain.String, "ton") {
		return nil, ErrAssetChainMismatch
	}
	if asset.Network.Valid && asset.Network.String != "" && normalizeNetwork(asset.Network.String) != network {
		return nil, ErrAssetNetworkMismatch
	}

	sourceWallet := strings.TrimSpace(params.SourceWallet)
	sourceWallet, err = normalizeTONAddress(sourceWallet, network, ErrSourceWalletRequired, ErrWalletAddressInvalid)
	if err != nil {
		return nil, err
	}

	responseDestination := strings.TrimSpace(params.ResponseDestination)
	if responseDestination == "" {
		responseDestination = sourceWallet
	} else {
		responseDestination, err = normalizeTONAddress(responseDestination, network, ErrResponseAddressInvalid, ErrResponseAddressInvalid)
		if err != nil {
			return nil, err
		}
	}

	return transactionJetton(ctx, network, params.NetworkConfigURL, sourceWallet, destination, responseDestination, params.AmountMinor, params.Comment, asset)
}

func transactionTONMinor(destination string, amountMinor uint64, comment string) (*Transaction, error) {
	if amountMinor > math.MaxInt64 {
		return nil, ErrAmountOverflow
	}
	amount := int(amountMinor)
	return TransactionTON(destination, &amount, comment), nil
}

func transactionJetton(ctx context.Context, network string, networkConfigURL string, sourceWallet string, destination string, responseDestination string, amountMinor uint64, comment string, asset paymentsqlc.PaymentAsset) (*Transaction, error) {
	if !asset.ContractAddress.Valid || strings.TrimSpace(asset.ContractAddress.String) == "" {
		return nil, ErrJettonMasterAddressRequired
	}

	recipient, err := address.ParseAddr(destination)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInvalidFields, ErrDestinationAddressInvalid.Message(), err)
	}
	response, err := address.ParseAddr(responseDestination)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInvalidFields, ErrResponseAddressInvalid.Message(), err)
	}

	jettonWalletAddress, err := resolveJettonWalletAddress(ctx, network, networkConfigURL, sourceWallet, asset.ContractAddress.String)
	if err != nil {
		return nil, err
	}

	forwardPayload := cell.BeginCell().
		MustStoreUInt(0, 32).
		MustStoreStringSnake(comment).
		EndCell()

	payload := cell.BeginCell().
		MustStoreUInt(jettonTransferOpCode, 32).
		MustStoreUInt(0, 64).
		MustStoreBigCoins(new(big.Int).SetUint64(amountMinor)).
		MustStoreAddr(recipient).
		MustStoreAddr(response).
		MustStoreBoolBit(false).
		MustStoreCoins(jettonForwardAmountNano).
		MustStoreBoolBit(true).
		MustStoreRef(forwardPayload).
		EndCell()

	return &Transaction{
		Kind:    transactionKindJettonTransfer,
		Address: jettonWalletAddress,
		Amount:  strconv.FormatUint(jettonGasAmountNano, 10),
		Payload: base64.StdEncoding.EncodeToString(payload.ToBOC()),
	}, nil
}

func resolveJettonWalletAddress(ctx context.Context, network string, networkConfigURL string, ownerWallet string, masterAddress string) (string, error) {
	configURL := strings.TrimSpace(networkConfigURL)
	if configURL == "" {
		configURL = defaultNetworkConfigURL(network)
	}

	client := liteclient.NewConnectionPool()
	defer client.Stop()

	stickyCtx := client.StickyContext(ctx)
	cfg, err := liteclient.GetConfigFromUrl(stickyCtx, configURL)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnavailable, ErrNetworkConfigLoadFailed.Message(), err)
	}
	if err := client.AddConnectionsFromConfig(stickyCtx, cfg); err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnavailable, ErrLiteConnectionsFailed.Message(), err)
	}

	master, err := parseTONAddress(masterAddress)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInvalidFields, ErrMasterAddressInvalid.Message(), err)
	}
	owner, err := address.ParseAddr(ownerWallet)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInvalidFields, ErrOwnerWalletInvalid.Message(), err)
	}

	api := tonclient.NewAPIClient(client, tonclient.ProofCheckPolicyFast).WithRetryTimeout(0, 5*time.Second)
	api.SetTrustedBlockFromConfig(cfg)
	masterClient := jetton.NewJettonMasterClient(api, master)
	wallet, err := masterClient.GetJettonWallet(stickyCtx, owner)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnavailable, ErrRecipientWalletResolveFailed.Message(), err)
	}
	return wallet.Address().String(), nil
}

func defaultNetworkConfigURL(network string) string {
	switch normalizeNetwork(network) {
	case NetworkTestnet:
		return NetworkConfigURLTestnet
	default:
		return NetworkConfigURLMainnet
	}
}
