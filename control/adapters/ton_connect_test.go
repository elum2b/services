package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"

	"github.com/xssnick/tonutils-go/tlb"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

func TestTONConnectReturnsAuthIdentityParams(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	stateInit, err := wallet.GetStateInit(publicKey, wallet.V4R2, wallet.DefaultSubwallet)
	if err != nil {
		t.Fatalf("state init: %v", err)
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("state init cell: %v", err)
	}
	addr := stateInit.CalcAddress(0)
	proof := TONConnectProof{
		Timestamp: time.Now().Unix(),
		Domain:    TONConnectDomain{LengthBytes: uint32(len("example.com")), Value: "example.com"},
		Payload:   "nonce-1",
	}
	proof.Signature = signTONConnectProof(t, privateKey, addr.Workchain(), addr.Data(), proof)
	params, err := TONConnect(context.Background(), TONConnectAuthParams{
		Address:         addr.StringRaw(),
		Network:         "-239",
		WalletStateInit: base64.StdEncoding.EncodeToString(stateCell.ToBOC()),
		Proof:           proof,
		ExpectedPayload: "nonce-1",
		ExpectedDomain:  "example.com",
		ExpectedNetwork: "-239",
		IP:              "127.0.0.1",
		UserAgent:       "ua",
		BindToIP:        true,
	})
	if err != nil {
		t.Fatalf("ton connect: %v", err)
	}
	if params.Provider != ProviderTONConnect {
		t.Fatalf("unexpected provider: %q", params.Provider)
	}
	if params.Subject != "-239:"+addr.StringRaw() {
		t.Fatalf("unexpected subject: %q", params.Subject)
	}
	if params.DisplayName != addr.StringRaw() || params.IP != "127.0.0.1" || params.UserAgent != "ua" || !params.BindToIP {
		t.Fatalf("unexpected params: %+v", params)
	}
}

func TestTONConnectRejectsWrongPayload(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	stateInit, err := wallet.GetStateInit(publicKey, wallet.V4R2, wallet.DefaultSubwallet)
	if err != nil {
		t.Fatalf("state init: %v", err)
	}
	stateCell, err := tlb.ToCell(stateInit)
	if err != nil {
		t.Fatalf("state init cell: %v", err)
	}
	addr := stateInit.CalcAddress(0)
	proof := TONConnectProof{
		Timestamp: time.Now().Unix(),
		Domain:    TONConnectDomain{LengthBytes: uint32(len("example.com")), Value: "example.com"},
		Payload:   "nonce-1",
	}
	proof.Signature = signTONConnectProof(t, privateKey, addr.Workchain(), addr.Data(), proof)
	_, err = TONConnect(context.Background(), TONConnectAuthParams{
		Address:         addr.StringRaw(),
		WalletStateInit: base64.StdEncoding.EncodeToString(stateCell.ToBOC()),
		Proof:           proof,
		ExpectedPayload: "nonce-2",
		ExpectedDomain:  "example.com",
	})
	if err == nil {
		t.Fatal("expected payload mismatch error")
	}
}

func signTONConnectProof(t *testing.T, privateKey ed25519.PrivateKey, workchain int32, hash []byte, proof TONConnectProof) string {
	t.Helper()
	var msg bytes.Buffer
	msg.WriteString("ton-proof-item-v2/")
	if err := binary.Write(&msg, binary.BigEndian, workchain); err != nil {
		t.Fatalf("write workchain: %v", err)
	}
	msg.Write(hash)
	if err := binary.Write(&msg, binary.LittleEndian, proof.Domain.LengthBytes); err != nil {
		t.Fatalf("write domain length: %v", err)
	}
	msg.WriteString(proof.Domain.Value)
	if err := binary.Write(&msg, binary.LittleEndian, proof.Timestamp); err != nil {
		t.Fatalf("write timestamp: %v", err)
	}
	msg.WriteString(proof.Payload)
	messageHash := sha256.Sum256(msg.Bytes())
	var full bytes.Buffer
	full.Write([]byte{0xff, 0xff})
	full.WriteString("ton-connect")
	full.Write(messageHash[:])
	fullHash := sha256.Sum256(full.Bytes())
	return base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, fullHash[:]))
}
