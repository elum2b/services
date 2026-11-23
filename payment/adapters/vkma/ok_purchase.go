package vkma

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/payment/repository"
)

const (
	OKAssetCode     = "OK"
	okPaymentMethod = "callbacks.payment"
)

type OKNotificationParams struct {
	WorkspaceID string
	AppID       int64
	PlatformID  int64
	// ApplicationKey optionally pins the signed OK application_key field.
	ApplicationKey string
	Secret         string
	RawQuery       string
}

// HandleOKNotification handles a one-time purchase on the separate OK GET
// callback. The caller supplies the trusted app mapping for that endpoint.
// A successful return is serialized as the JSON boolean true.
func (a *VKMA) HandleOKNotification(ctx context.Context, params OKNotificationParams) (bool, error) {
	fields, err := notificationFields(params.RawQuery)
	if err != nil {
		return false, err
	}
	providerID, amount, hash, err := verifyOKPurchase(params, fields)
	if err != nil {
		return false, err
	}

	ctx, cancel := a.withContext(ctx)
	defer cancel()

	_, err = a.repository.ApplyProviderPurchase(ctx, repository.ProviderPurchaseParams{
		Order: repository.OrderCreateParams{
			WorkspaceID:    params.WorkspaceID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
			PlatformUserID: fields.Get("uid"),
			ProductID:      fields.Get("product_code"),
			AssetCode:      OKAssetCode,
			Locale:         normalizeLocale(fields.Get("lang")),
		},
		ProviderCode:      ProviderCode,
		ProviderPaymentID: providerID,
		AmountMinor:       amount,
		EventType:         okPaymentMethod,
		PayloadHash:       hash,
	})
	return err == nil, err
}

func verifyOKPurchase(params OKNotificationParams, fields url.Values) (string, uint64, string, error) {
	applicationKey := fields.Get("application_key")
	signature, err := hex.DecodeString(fields.Get("sig"))
	if err != nil || len(signature) != md5.Size || params.Secret == "" ||
		applicationKey == "" || (params.ApplicationKey != "" && applicationKey != params.ApplicationKey) {
		return "", 0, "", ErrNotificationSignature
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key != "sig" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(key)
		canonical.WriteByte('=')
		canonical.WriteString(fields.Get(key))
	}
	payload := canonical.String()
	sum := md5.Sum([]byte(payload + params.Secret))
	if subtle.ConstantTimeCompare(signature, sum[:]) != 1 {
		return "", 0, "", ErrNotificationSignature
	}

	transactionID := fields.Get("transaction_id")
	providerID := "ok:" + strconv.FormatInt(params.AppID, 10) + ":" + transactionID
	amount, err := strconv.ParseUint(fields.Get("amount"), 10, 63)
	if err != nil || amount == 0 || amount > math.MaxInt32 ||
		params.AppID <= 0 || params.PlatformID <= 0 || params.PlatformID == PlatformID ||
		fields.Get("method") != okPaymentMethod || fields.Get("currency") != "" ||
		fields.Get("uid") == "" || fields.Get("product_code") == "" ||
		transactionID == "" || len(providerID) > 128 || fields.Get("transaction_time") == "" {
		return "", 0, "", ErrInvalidNotification
	}

	// Ordinary mobile purchases can carry other JSON attributes.
	extra := fields.Get("extra_attributes")
	if extra != "" && extra != "[]" {
		var attributes struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(extra), &attributes); err != nil || attributes.Action != "" {
			return "", 0, "", ErrInvalidNotification
		}
	}

	hash := sha256.Sum256([]byte(payload))
	return providerID, amount, hex.EncodeToString(hash[:]), nil
}
