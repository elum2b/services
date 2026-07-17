package ton

import (
	"database/sql"
	"errors"
	"strings"

	serviceerrors "github.com/elum2b/services/errors"
	"github.com/jackc/pgx/v5/pgconn"
)

func normalizeLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if locale == "" {
		return "ru"
	}
	return locale
}

func normalizeNetwork(network string) string {
	network = strings.ToLower(strings.TrimSpace(network))
	if network == "" {
		return NetworkMainnet
	}
	return network
}

func validateNetwork(network string) (string, error) {
	network = normalizeNetwork(network)
	switch network {
	case NetworkMainnet, NetworkTestnet:
		return network, nil
	default:
		return "", ErrNetworkInvalid
	}
}

func NormalizeNetwork(network string) (string, error) {
	return validateNetwork(network)
}

func NormalizeWalletAddress(value string, network string) (string, error) {
	return normalizeTONAddress(value, network, ErrWalletAddressRequired, ErrWalletAddressInvalid)
}

func normalizeTONAddress(value string, network string, requiredErr *serviceerrors.Error, invalidErr *serviceerrors.Error) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", requiredErr
	}
	network, err := validateNetwork(network)
	if err != nil {
		return "", err
	}
	parsed, err := parseTONAddress(value)
	if err != nil {
		return "", serviceerrors.Wrap(serviceerrors.CodeInvalidFields, invalidErr.Message(), err)
	}
	return parsed.Testnet(network == NetworkTestnet).String(), nil
}

func nullInt64FromPtr(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullInt64FromUint64(value uint64) sql.NullInt64 {
	if value == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(value), Valid: true}
}

func uint64FromNull(value sql.NullInt64) uint64 {
	if !value.Valid || value.Int64 <= 0 {
		return 0
	}
	return uint64(value.Int64)
}

func isDuplicateEntry(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
