package auth

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/control/service/admin"
	serviceerrors "github.com/elum2b/services/errors"

	"github.com/xssnick/tonutils-go/address"
	tonclient "github.com/xssnick/tonutils-go/ton"
	"github.com/xssnick/tonutils-go/ton/wallet"
)

const ProviderTONConnect = "ton_connect"

const defaultTONConnectMaxAge = 5 * time.Minute

type TONConnectAuthParams struct {
	Address           string
	Network           string
	PublicKey         string
	WalletStateInit   string
	Proof             TONConnectProof
	ExpectedPayload   string
	PayloadSecret     string
	ExpectedDomain    string
	ExpectedNetwork   string
	AllowNativeDomain bool
	Client            tonclient.APIClientWrapped
	IP                string
	UserAgent         string
	BindToIP          bool
	ExpiresAt         time.Time
	MaxAge            time.Duration
}

type TONConnectProof struct {
	Timestamp int64            `json:"timestamp"`
	Domain    TONConnectDomain `json:"domain"`
	Signature string           `json:"signature"`
	Payload   string           `json:"payload"`
}

type TONConnectDomain struct {
	LengthBytes uint32 `json:"lengthBytes"`
	Value       string `json:"value"`
}

func TONConnectPayload(secret string, ttl time.Duration) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrPayloadRequired
	}
	return wallet.GeneratePayload(secret, ttl)
}

func TONConnect(ctx context.Context, params TONConnectAuthParams) (admin.AuthIdentityParams, error) {
	addr, err := parseTONAddress(params.Address)
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	if err := validateTONNetwork(params.Network, params.ExpectedNetwork); err != nil {
		return admin.AuthIdentityParams{}, err
	}
	expectedDomain := strings.TrimSpace(params.ExpectedDomain)
	if expectedDomain == "" {
		return admin.AuthIdentityParams{}, ErrDomainRequired
	}
	if err := validateTONDomain(params.Proof.Domain, expectedDomain, params.AllowNativeDomain); err != nil {
		return admin.AuthIdentityParams{}, err
	}
	signature, err := decodeTONBase64(params.Proof.Signature)
	if err != nil {
		return admin.AuthIdentityParams{}, serviceerrors.Wrap(serviceerrors.CodeUnauthorized, "ton connect signature is invalid", err)
	}
	stateInit, err := decodeOptionalTONBase64(params.WalletStateInit)
	if err != nil {
		return admin.AuthIdentityParams{}, serviceerrors.Wrap(serviceerrors.CodeUnauthorized, "ton connect state init is invalid", err)
	}
	if len(stateInit) == 0 && params.Client == nil {
		return admin.AuthIdentityParams{}, ErrStateInitOrClientRequired
	}
	proof := wallet.TonConnectProof{Timestamp: params.Proof.Timestamp, Signature: signature, Payload: params.Proof.Payload}
	proof.Domain.LengthBytes = params.Proof.Domain.LengthBytes
	proof.Domain.Value = params.Proof.Domain.Value
	maxAge := params.MaxAge
	if maxAge <= 0 {
		maxAge = defaultTONConnectMaxAge
	}
	verifier := wallet.NewTonConnectVerifier(expectedDomain, maxAge, params.Client)
	if strings.TrimSpace(params.PayloadSecret) != "" {
		err = verifier.VerifyProofHandlePayload(ctx, addr, proof, stateInit, wallet.CheckPayload, params.PayloadSecret)
	} else {
		expectedPayload := strings.TrimSpace(params.ExpectedPayload)
		if expectedPayload == "" {
			return admin.AuthIdentityParams{}, ErrPayloadRequired
		}
		err = verifier.VerifyProof(ctx, addr, proof, expectedPayload, stateInit)
	}
	if err != nil {
		return admin.AuthIdentityParams{}, serviceerrors.Wrap(serviceerrors.CodeUnauthorized, "ton connect proof is invalid", err)
	}
	payload, err := json.Marshal(map[string]any{
		"address":      addr.StringRaw(),
		"network":      strings.TrimSpace(params.Network),
		"public_key":   strings.TrimSpace(params.PublicKey),
		"domain":       params.Proof.Domain,
		"timestamp":    params.Proof.Timestamp,
		"payload":      params.Proof.Payload,
		"state_init":   strings.TrimSpace(params.WalletStateInit) != "",
		"payload_mode": tonPayloadMode(params),
	})
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	return admin.AuthIdentityParams{
		Provider:    ProviderTONConnect,
		Subject:     tonSubject(params.Network, addr),
		DisplayName: addr.StringRaw(),
		Payload:     payload,
		IP:          params.IP,
		UserAgent:   params.UserAgent,
		BindToIP:    params.BindToIP,
		ExpiresAt:   params.ExpiresAt,
	}, nil
}

func parseTONAddress(raw string) (*address.Address, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrAddressRequired
	}
	if strings.Contains(raw, ":") {
		addr, err := address.ParseRawAddr(raw)
		if err == nil {
			return addr, nil
		}
	}
	addr, err := address.ParseAddr(raw)
	if err != nil {
		return nil, serviceerrors.Wrap(serviceerrors.CodeInvalidFields, "ton wallet address is invalid", err)
	}
	return addr, nil
}

func validateTONNetwork(actual, expected string) error {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return nil
	}
	if actual != expected {
		return ErrInvalidNetwork
	}
	return nil
}

func validateTONDomain(domain TONConnectDomain, expected string, allowNative bool) error {
	value := strings.TrimSpace(domain.Value)
	if value == "" || !strings.EqualFold(value, expected) {
		return ErrInvalidDomain
	}
	if int(domain.LengthBytes) != len([]byte(domain.Value)) {
		return ErrInvalidDomain
	}
	if allowNative {
		return nil
	}
	if !validExternalTONDomain(value) {
		return ErrInvalidDomain
	}
	return nil
}

func validExternalTONDomain(value string) bool {
	if strings.ContainsAny(value, "/:@?#[]") {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}

func decodeOptionalTONBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	return decodeTONBase64(value)
}

func decodeTONBase64(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, ErrInvalidSignature
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(value); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(value)
}

func tonSubject(network string, addr *address.Address) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return addr.StringRaw()
	}
	return network + ":" + addr.StringRaw()
}

func tonPayloadMode(params TONConnectAuthParams) string {
	if strings.TrimSpace(params.PayloadSecret) != "" {
		return "signed_payload"
	}
	return "expected_payload"
}
