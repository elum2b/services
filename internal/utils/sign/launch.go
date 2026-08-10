// Package sign verifies signed Mini App launch payloads without coupling them
// to a workspace, database, or HTTP transport.
package sign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/elum-utils/sign/vkma"
	json "github.com/goccy/go-json"
)

type Provider string

const (
	ProviderVKMA Provider = "vkma"
	ProviderTMA  Provider = "tma"
)

var ErrInvalidLaunch = errors.New("invalid signed application launch")

type Launch struct {
	PlatformUserID string
	IssuedAt       time.Time
}

func Verify(
	provider Provider,
	raw, secret string,
	appID int64,
) (Launch, error) {

	if appID <= 0 || strings.TrimSpace(raw) == "" || secret == "" {
		return Launch{}, ErrInvalidLaunch
	}

	switch provider {
	case ProviderVKMA:
		return verifyVKMA(raw, secret, appID)
	case ProviderTMA:
		return verifyTMA(raw, secret, appID)
	default:
		return Launch{}, ErrInvalidLaunch
	}

}

func verifyVKMA(raw, secret string, appID int64) (Launch, error) {

	params, ok := vkma.Verify(
		raw,
		map[string]string{strconv.FormatInt(appID, 10): secret},
	)
	if !ok || int64(params.VkAppID) != appID || params.VkUserID <= 0 {
		return Launch{}, ErrInvalidLaunch
	}

	issuedAt, err := parseUnixTimestamp(params.VkTs)
	if err != nil {
		return Launch{}, ErrInvalidLaunch
	}

	return Launch{
		PlatformUserID: strconv.Itoa(params.VkUserID),
		IssuedAt:       issuedAt,
	}, nil

}

func verifyTMA(raw, secret string, appID int64) (Launch, error) {

	values, err := url.ParseQuery(raw)
	if err != nil {
		return Launch{}, ErrInvalidLaunch
	}

	hash, ok := oneValue(values, "hash")
	if !ok || hash == "" {
		return Launch{}, ErrInvalidLaunch
	}

	pairs := make([]string, 0, len(values)-1)
	for key, values := range values {
		if len(values) != 1 {
			return Launch{}, ErrInvalidLaunch
		}
		if key != "hash" {
			pairs = append(pairs, key+"="+values[0])
		}
	}
	sort.Strings(pairs)

	secretKey := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretKey.Write([]byte(secret))
	mac := hmac.New(sha256.New, secretKey.Sum(nil))
	_, _ = mac.Write([]byte(strings.Join(pairs, "\n")))
	expected := mac.Sum(nil)
	provided, err := hex.DecodeString(hash)
	if err != nil || !hmac.Equal(expected, provided) {
		return Launch{}, ErrInvalidLaunch
	}

	issuedRaw, ok := oneValue(values, "auth_date")
	if !ok {
		return Launch{}, ErrInvalidLaunch
	}
	issuedAt, err := parseUnixTimestamp(issuedRaw)
	if err != nil {
		return Launch{}, ErrInvalidLaunch
	}

	userRaw, ok := oneValue(values, "user")
	if !ok {
		return Launch{}, ErrInvalidLaunch
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(
		[]byte(userRaw),
		&user,
	); err != nil ||
		user.ID <= 0 {
		return Launch{}, ErrInvalidLaunch
	}

	return Launch{
		PlatformUserID: strconv.FormatInt(user.ID, 10),
		IssuedAt:       issuedAt,
	}, nil

}

func oneValue(values url.Values, key string) (string, bool) {

	value, ok := values[key]
	returnValue := ""
	if ok && len(value) == 1 {
		returnValue = value[0]
	}
	return returnValue, ok && len(value) == 1

}

func parseUnixTimestamp(value string) (time.Time, error) {

	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, fmt.Errorf("invalid timestamp")
	}
	return time.Unix(seconds, 0).UTC(), nil

}
