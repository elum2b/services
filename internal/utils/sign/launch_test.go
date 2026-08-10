package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestVerifyTMAUsesTheSecretProvidedForEachApplication(t *testing.T) {
	now := time.Now().UTC()
	first := signedTMA("first-bot-token", 1, now)
	second := signedTMA("second-bot-token", 2, now)

	firstResult, err := Verify(ProviderTMA, first, "first-bot-token", 100)
	if err != nil || firstResult.PlatformUserID != "1" {
		t.Fatalf("first application result = %#v, %v", firstResult, err)
	}

	secondResult, err := Verify(ProviderTMA, second, "second-bot-token", 101)
	if err != nil || secondResult.PlatformUserID != "2" {
		t.Fatalf("second application result = %#v, %v", secondResult, err)
	}

	if _, err := Verify(
		ProviderTMA,
		second,
		"first-bot-token",
		101,
	); err == nil {
		t.Fatal(
			"second application was verified by another application's secret",
		)
	}
}

func signedTMA(secret string, userID int64, issuedAt time.Time) string {
	values := url.Values{
		"auth_date": {strconv.FormatInt(issuedAt.Unix(), 10)},
		"user":      {`{"id":` + strconv.FormatInt(userID, 10) + `}`},
	}
	macKey := hmac.New(sha256.New, []byte("WebAppData"))

	_, _ = macKey.Write([]byte(secret))

	mac := hmac.New(sha256.New, macKey.Sum(nil))

	_, _ = mac.Write(
		[]byte(
			"auth_date=" + values.Get(
				"auth_date",
			) + "\nuser=" + values.Get(
				"user",
			),
		),
	)
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))

	return values.Encode()
}
