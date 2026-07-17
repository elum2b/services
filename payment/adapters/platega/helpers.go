package platega

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	utils "github.com/elum2b/services/internal/utils"
	json "github.com/goccy/go-json"
	"github.com/jackc/pgx/v5/pgconn"
)

func rubMajorFromMinor(amountMinor uint64) json.Number {
	return json.Number(fmt.Sprintf("%d.%02d", amountMinor/100, amountMinor%100))
}

func rubMinorFromMajor(amount json.Number) (uint64, error) {
	value := strings.TrimSpace(amount.String())
	if value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "eE+") {
		return 0, ErrAmountInvalid
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrAmountInvalid
	}
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, ErrAmountInvalid
	}

	fraction := uint64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, ErrAmountInvalid
		}
		fractionValue := parts[1]
		if len(fractionValue) == 1 {
			fractionValue += "0"
		}
		fraction, err = strconv.ParseUint(fractionValue, 10, 64)
		if err != nil {
			return 0, ErrAmountInvalid
		}
	}

	maxMinor := uint64(math.MaxInt64)
	if whole > maxMinor/100 ||
		(whole == maxMinor/100 && fraction > maxMinor%100) {
		return 0, ErrAmountInvalid
	}

	return whole*100 + fraction, nil
}

func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return "ru"
	}
	return locale
}

func validateHeaders(headers http.Header, credentials Credentials) bool {
	if credentials.MerchantID == "" || credentials.Secret == "" {
		return false
	}
	return constantTimeString(headers.Get("X-MerchantId"), credentials.MerchantID) &&
		constantTimeString(headers.Get("X-Secret"), credentials.Secret)
}

func constantTimeString(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := 0; i < len(left); i++ {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}

func webhookEventID(payload callbackPayload) string {
	return fmt.Sprintf("%s:%s:%d", payload.ID, payload.Status, payload.PaymentMethod)
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func nullInt64FromPtr(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nilIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return utils.Ref(value)
}

func isDuplicateEntry(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
