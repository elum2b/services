package sql

import (
	"strings"
	"testing"
)

func TestPostgresDSNUsesTLSForRemoteHosts(t *testing.T) {

	dsn, err := PostgresDSN(PostgresParams{
		User:     "user",
		Password: "password",
		Database: "service",
		Host:     "db.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "sslmode=verify-full") {
		t.Fatalf("remote PostgreSQL DSN must verify TLS: %q", dsn)
	}

}

func TestPostgresDSNAllowsExplicitLocalDisable(t *testing.T) {

	dsn, err := PostgresDSN(PostgresParams{
		User:     "user",
		Password: "password",
		Database: "service",
		Host:     "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Fatalf("local PostgreSQL DSN = %q, want sslmode=disable", dsn)
	}

}

func TestPostgresDSNRejectsUnknownSSLMode(t *testing.T) {

	if _, err := PostgresDSN(
		PostgresParams{Host: "db.example.com", SSLMode: "unsafe"},
	); err == nil {
		t.Fatal("unknown SSL mode was accepted")
	}

}
