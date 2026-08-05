package sql

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

type PostgresParams struct {
	User        string
	Password    string
	Database    string
	Host        string
	Port        int
	SSLMode     string
	SSLRootCert string
}

func PostgresDSN(params PostgresParams) (string, error) {

	host := strings.TrimSpace(params.Host)
	if host == "" {
		host = "localhost"
	}
	port := params.Port
	if port == 0 {
		port = 5432
	}

	sslMode := strings.ToLower(strings.TrimSpace(params.SSLMode))
	if sslMode == "" {
		if isLocalPostgresHost(host) {
			sslMode = "disable"
		} else {
			sslMode = "verify-full"
		}
	}
	if !validPostgresSSLMode(sslMode) {
		return "", fmt.Errorf("unsupported PostgreSQL SSL mode %q", params.SSLMode)
	}

	values := url.Values{"sslmode": []string{sslMode}}
	if rootCert := strings.TrimSpace(params.SSLRootCert); rootCert != "" {
		values.Set("sslrootcert", rootCert)
	}

	return (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(params.User, params.Password),
		Host:     net.JoinHostPort(host, fmt.Sprintf("%d", port)),
		Path:     params.Database,
		RawQuery: values.Encode(),
	}).String(), nil

}

func validPostgresSSLMode(value string) bool {

	switch value {
	case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}

}

func isLocalPostgresHost(host string) bool {

	host = strings.Trim(host, "[]")
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"

}
