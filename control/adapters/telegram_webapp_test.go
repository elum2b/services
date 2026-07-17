package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestTelegramWebAppResolve(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	provider, err := NewTelegramWebApp(TelegramWebAppConfig{
		BotToken: "bot-token", MaxAge: time.Minute, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new telegram webapp provider: %v", err)
	}
	rawData := signedTelegramWebAppData(t, "bot-token", url.Values{
		"auth_date": []string{"1700000000"},
		"query_id":  []string{"query-1"},
		"user":      []string{`{"id":1093776793,"first_name":"Root","last_name":"Admin","username":"root"}`},
	})
	identity, err := provider.Resolve(context.Background(), Request{RawData: rawData})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if identity.Provider != ProviderTelegramWebApp || identity.Subject != "1093776793" || identity.DisplayName != "Root Admin" {
		t.Fatalf("unexpected identity: %+v", identity)
	}
}

func TestTelegramWebAppFunctionReturnsAuthIdentityParams(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	rawData := signedTelegramWebAppData(t, "bot-token", url.Values{
		"auth_date": []string{"1700000000"},
		"user":      []string{`{"id":1093776793,"first_name":"Root"}`},
	})
	params, err := TelegramWebApp(context.Background(), TelegramWebAppAuthParams{
		BotToken: "bot-token", InitData: rawData, IP: "127.0.0.1", UserAgent: "ua",
		MaxAge: time.Minute, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("telegram webapp auth: %v", err)
	}
	if params.Provider != ProviderTelegramWebApp || params.Subject != "1093776793" || params.DisplayName != "Root" {
		t.Fatalf("unexpected auth params: %+v", params)
	}
}

func TestTelegramWebAppRejectsInvalidSignature(t *testing.T) {
	t.Parallel()
	provider, err := NewTelegramWebApp(TelegramWebAppConfig{BotToken: "bot-token"})
	if err != nil {
		t.Fatalf("new telegram webapp provider: %v", err)
	}
	_, err = provider.Resolve(context.Background(), Request{RawData: `auth_date=1700000000&user=%7B%22id%22%3A1%7D&hash=bad`})
	if err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func signedTelegramWebAppData(t *testing.T, botToken string, values url.Values) string {
	t.Helper()
	parts := make([]string, 0, len(values))
	for key, item := range values {
		if key == "hash" || len(item) == 0 {
			continue
		}
		parts = append(parts, key+"="+item[0])
	}
	sort.Strings(parts)
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(botToken))
	dataMAC := hmac.New(sha256.New, secretMAC.Sum(nil))
	_, _ = dataMAC.Write([]byte(strings.Join(parts, "\n")))
	values.Set("hash", hex.EncodeToString(dataMAC.Sum(nil)))
	return values.Encode()
}
