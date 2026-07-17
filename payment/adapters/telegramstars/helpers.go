package telegramstars

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	utils "github.com/elum2b/services/internal/utils"
	"github.com/jackc/pgx/v5/pgconn"
)

const telegramStarsSubscriptionPeriod = 30 * 24 * 60 * 60

func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return "ru"
	}
	return locale
}

func normalizeTitle(title string, fallback string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = fallback
	}
	return trimRunes(title, 32)
}

func normalizeDescription(description string, fallback string) string {
	description = strings.TrimSpace(description)
	if description == "" {
		description = "Payment order " + fallback
	}
	return trimRunes(description, 255)
}

func normalizeSubscriptionPeriod(period int) int {
	if period == 0 {
		return 0
	}
	return telegramStarsSubscriptionPeriod
}

func trimRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
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

func int64Null(value int64) sql.NullInt64 { return sql.NullInt64{Int64: value, Valid: true} }

func timeNull(value time.Time) sql.NullTime { return sql.NullTime{Time: value, Valid: true} }

func uint64Ptr(value *int64) *uint64 {
	if value == nil {
		return nil
	}
	v := uint64(*value)
	return utils.Ref(v)
}

func refIfNotEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return utils.Ref(value)
}

func isDuplicateEntry(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
