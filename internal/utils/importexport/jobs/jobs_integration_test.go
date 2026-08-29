package jobs

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	jobsTestPGHost     = "localhost"
	jobsTestPGPort     = 5432
	jobsTestPGUser     = "postgres"
	jobsTestPGPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
)

func openJobsTestPostgres(tb testing.TB) *sql.DB {
	tb.Helper()

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable",
		jobsTestPGHost,
		jobsTestPGPort,
		jobsTestPGUser,
		jobsTestPGPassword,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		tb.Fatal(err)
	}
	if err := db.PingContext(tb.Context()); err != nil {
		_ = db.Close()
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = db.Close() })

	return db
}

func openIntegrationStore(t *testing.T) (*sql.DB, *store) {
	t.Helper()
	db := openJobsTestPostgres(t)
	table := "importexport_jobs_test_" + newToken()[:12]
	if err := BootstrapTable(t.Context(), db, table); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + quoteIdentifier(table) + " CASCADE") })
	return db, &store{db: db, table: table, history: table + "_history", workerID: "worker-" + newToken(), leaseTime: time.Minute}
}

func TestPostgresLeaseFencingAndCleanupClaim(t *testing.T) {
	_, first := openIntegrationStore(t)
	second := &store{db: first.db, table: first.table, history: first.history, workerID: "worker-" + newToken(), leaseTime: time.Minute}
	job, err := first.queue(t.Context(), "service", "c2b604c6-6960-41a7-b330-5083ca633434", TypeExport, "dump.zip", "key")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := first.lease(t.Context())
	if err != nil || lease.LeaseToken == "" || lease.LockedUntil == nil {
		t.Fatalf("lease = %#v, err = %v", lease, err)
	}
	if lease.FileName != "dump.zip" {
		t.Fatalf("filename = %q", lease.FileName)
	}
	if err := second.complete(t.Context(), job.ID, lease.LeaseToken, "key", time.Hour); err != ErrNotLeased {
		t.Fatalf("other worker complete error = %v", err)
	}
	if err := first.complete(t.Context(), job.ID, "wrong-token", "key", time.Hour); err != ErrNotLeased {
		t.Fatalf("wrong token complete error = %v", err)
	}
	if _, err := first.db.ExecContext(t.Context(), "UPDATE "+quoteIdentifier(first.table)+" SET locked_until = now() - interval '1 second' WHERE id = $1", job.ID); err != nil {
		t.Fatal(err)
	}
	if err := first.complete(t.Context(), job.ID, lease.LeaseToken, "key", time.Hour); err != ErrNotLeased {
		t.Fatalf("expired lease complete error = %v", err)
	}
	lease, err = first.lease(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.complete(t.Context(), job.ID, lease.LeaseToken, "key", -time.Hour); err != nil {
		t.Fatal(err)
	}
	claims, err := first.claimExpiredArchives(t.Context(), 1)
	if err != nil || len(claims) != 1 || claims[0].LeaseToken == "" {
		t.Fatalf("claims = %#v, err = %v", claims, err)
	}
	otherClaims, err := second.claimExpiredArchives(t.Context(), 1)
	if err != nil || len(otherClaims) != 0 {
		t.Fatalf("other claims = %#v, err = %v", otherClaims, err)
	}
	if err := first.clearArchive(t.Context(), job.ID, "key", claims[0].LeaseToken); err != nil {
		t.Fatal(err)
	}
}
