package yookassa

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/jackc/pgx/v5/pgconn"
)

func formatRubMinor(amountMinor uint64) string {
	return fmt.Sprintf("%d.%02d", amountMinor/100, amountMinor%100)
}

func paymentMethodData(method PaymentMethodType) *yookassaPaymentMethod {
	if method == "" {
		return nil
	}
	return &yookassaPaymentMethod{Type: method}
}

func parseRubAmount(value string) (uint64, error) {
	whole, fraction, ok := strings.Cut(value, ".")
	if !ok {
		fraction = "00"
	}
	if len(fraction) == 1 {
		fraction += "0"
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("yookassa: invalid RUB amount precision: %s", value)
	}
	major, err := strconv.ParseUint(whole, 10, 64)
	if err != nil {
		return 0, err
	}
	minor, err := strconv.ParseUint(fraction, 10, 64)
	if err != nil {
		return 0, err
	}
	maxAmountMinor := uint64(math.MaxInt64)
	if major > (maxAmountMinor-minor)/100 {
		return 0, fmt.Errorf("yookassa: RUB amount is out of range: %s", value)
	}
	return major*100 + minor, nil
}

func webhookEventID(webhook webhookPayload) string {
	return strings.Join([]string{webhook.Event, webhook.Object.ID, webhook.Object.Status}, ":")
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return "ru"
	}
	return locale
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
