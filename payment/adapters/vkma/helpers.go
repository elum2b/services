package vkma

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	json "github.com/goccy/go-json"
	"strconv"
	"strings"

	"github.com/elum-utils/sign/vkmashop"
	utils "github.com/elum2b/services/internal/utils"
)

func productID(params vkmashop.Params) string {
	if params.Item != "" {
		return params.Item
	}
	return params.ItemID
}

func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return "ru"
	}
	if len(locale) > 2 {
		return locale[:2]
	}
	return locale
}

func nullStringFromPositiveInt(value int) *string {
	if value <= 0 {
		return nil
	}
	v := strconv.Itoa(value)
	return utils.Ref(v)
}

func eventID(params vkmashop.Params) *string {
	value := fmt.Sprintf("%s:%s:%d:%d", params.NotificationType, params.Status, params.OrderID, params.SubscriptionID)
	return utils.Ref(value)
}

func uint64Ptr(value uint64) *uint64 { return utils.Ref(value) }

func int64Null(value int64) sql.NullInt64 {
	return sql.NullInt64{Int64: value, Valid: true}
}

func refIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return utils.Ref(value)
}

func positiveString(value int) *string {
	if value <= 0 {
		return nil
	}
	s := strconv.Itoa(value)
	return utils.Ref(s)
}

func payloadHash(params vkmashop.Params) string {
	sum := sha256.Sum256([]byte(payload(params)))
	return hex.EncodeToString(sum[:])
}

func payload(params vkmashop.Params) string {
	data, err := json.Marshal(params)
	if err != nil {
		return "{}"
	}
	return string(data)
}
