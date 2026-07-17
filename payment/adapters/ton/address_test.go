package ton

import (
	"context"
	"testing"
)

func TestNormalizeWalletAddressAcceptsFriendlyAndRaw(t *testing.T) {
	raw := "0:0000000000000000000000000000000000000000000000000000000000000000"
	mainnet, err := NormalizeWalletAddress(raw, NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize raw mainnet: %v", err)
	}
	if mainnet != "EQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAM9c" {
		t.Fatalf("unexpected normalized mainnet address: %s", mainnet)
	}

	friendly, err := NormalizeWalletAddress("UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ", NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize friendly mainnet: %v", err)
	}
	if friendly != "UQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAJKZ" {
		t.Fatalf("unexpected normalized friendly address: %s", friendly)
	}

	testnet, err := NormalizeWalletAddress(raw, NetworkTestnet)
	if err != nil {
		t.Fatalf("normalize raw testnet: %v", err)
	}
	if testnet == mainnet {
		t.Fatalf("expected testnet flag to change normalized address")
	}
}

func TestNormalizeWalletAddressRejectsInvalid(t *testing.T) {
	if _, err := NormalizeWalletAddress("EQ_WORKSPACE_WALLET", NetworkMainnet); err == nil {
		t.Fatal("expected invalid wallet address to be rejected")
	}
}

func TestCreateTransactionAcceptsRawDestination(t *testing.T) {
	api := &TON{rootCtx: context.Background()}
	raw := "0:0000000000000000000000000000000000000000000000000000000000000000"
	expected, err := NormalizeWalletAddress(raw, NetworkMainnet)
	if err != nil {
		t.Fatalf("normalize raw destination: %v", err)
	}
	tx, err := api.CreateTransaction(context.Background(), CreateTransactionParams{
		AssetCode:   AssetTON,
		Network:     NetworkMainnet,
		Destination: raw,
		AmountMinor: 1,
		Comment:     "test",
	})
	if err != nil {
		t.Fatalf("create ton transaction: %v", err)
	}
	if tx.Address != expected {
		t.Fatalf("transaction address = %s, want %s", tx.Address, expected)
	}
}
