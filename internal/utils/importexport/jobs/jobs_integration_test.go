package jobs

import (
	"context"
	"database/sql"
	"errors"
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

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			"DROP TABLE IF EXISTS "+quoteIdentifier(table)+" CASCADE",
		)
	})

	return db, &store{
		db:        db,
		table:     table,
		history:   table + "_history",
		service:   "service",
		workerID:  "worker-" + newToken(),
		leaseTime: time.Minute,
	}
}

func TestClaimImportReceiptIsIdempotentAndTransactional(t *testing.T) {
	db, _ := openIntegrationStore(t)
	ctx := t.Context()
	workspaceID := "c2b604c6-6960-41a7-b330-5083ca633434"
	jobID := time.Now().UnixNano()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := ClaimImportReceipt(ctx, tx, jobID, "promo", workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	if !claimed {
		t.Fatal("first receipt claim was not recorded")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	claimed, err = ClaimImportReceipt(ctx, db, jobID, "promo", workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	if !claimed {
		t.Fatal("rolled back receipt prevented a new claim")
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			"DELETE FROM importexport_job_import_receipt WHERE job_id = $1 AND service = $2 AND workspace_id = $3",
			jobID,
			"promo",
			workspaceID,
		)
	})

	claimed, err = ClaimImportReceipt(ctx, db, jobID, "promo", workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	if claimed {
		t.Fatal("duplicate receipt claim was accepted")
	}
}

func TestPostgresLeaseFencingAndCleanupClaim(t *testing.T) {
	_, first := openIntegrationStore(t)
	second := &store{
		db:        first.db,
		table:     first.table,
		history:   first.history,
		workerID:  "worker-" + newToken(),
		leaseTime: time.Minute,
	}

	job, err := first.queue(
		t.Context(),
		"service",
		"c2b604c6-6960-41a7-b330-5083ca633434",
		TypeExport,
		"dump.zip",
		"key",
	)
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

	if err := second.complete(
		t.Context(),
		job.ID,
		lease.LeaseToken,
		"key",
		time.Hour,
	); !errors.Is(err, ErrNotLeased) {
		t.Fatalf("other worker complete error = %v", err)
	}

	if err := first.complete(
		t.Context(),
		job.ID,
		"wrong-token",
		"key",
		time.Hour,
	); !errors.Is(err, ErrNotLeased) {
		t.Fatalf("wrong token complete error = %v", err)
	}

	if _, err := first.db.ExecContext(
		t.Context(),
		"UPDATE "+quoteIdentifier(
			first.table,
		)+" SET locked_until = now() - interval '1 second' WHERE id = $1",
		job.ID,
	); err != nil {
		t.Fatal(err)
	}

	if err := first.complete(
		t.Context(),
		job.ID,
		lease.LeaseToken,
		"key",
		time.Hour,
	); !errors.Is(err, ErrNotLeased) {
		t.Fatalf("expired lease complete error = %v", err)
	}

	lease, err = first.lease(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err := first.complete(
		t.Context(),
		job.ID,
		lease.LeaseToken,
		"key",
		-time.Hour,
	); err != nil {
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

	if err := first.clearArchive(
		t.Context(),
		job.ID,
		"key",
		claims[0].LeaseToken,
	); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresLeaseIsScopedToService(t *testing.T) {
	_, first := openIntegrationStore(t)
	second := &store{
		db:        first.db,
		table:     first.table,
		history:   first.history,
		service:   "second",
		workerID:  "worker-" + newToken(),
		leaseTime: time.Minute,
	}

	first.service = "first"

	for _, service := range []string{"first", "second"} {
		if _, err := first.queue(
			t.Context(),
			service,
			"c2b604c6-6960-41a7-b330-5083ca633434",
			TypeExport,
			"dump.zip",
			"",
		); err != nil {
			t.Fatal(err)
		}
	}

	lease, err := second.lease(t.Context())
	if err != nil || lease.Service != "second" {
		t.Fatalf("second service lease = %#v, err = %v", lease, err)
	}

	lease, err = first.lease(t.Context())
	if err != nil || lease.Service != "first" {
		t.Fatalf("first service lease = %#v, err = %v", lease, err)
	}
}

func TestDownloadRejectsCompletedImport(t *testing.T) {
	_, store := openIntegrationStore(t)

	job, err := store.queue(
		t.Context(),
		"service",
		"c2b604c6-6960-41a7-b330-5083ca633434",
		TypeImport,
		"dump.zip",
		"upload",
	)
	if err != nil {
		t.Fatal(err)
	}

	lease, err := store.lease(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err := store.complete(
		t.Context(),
		job.ID,
		lease.LeaseToken,
		"upload",
		time.Hour,
	); err != nil {
		t.Fatal(err)
	}

	manager := &Manager{store: store}
	if _, _, err := manager.Download(
		t.Context(),
		DownloadParams{
			Service:     "service",
			WorkspaceID: job.WorkspaceID,
			ID:          job.ID,
		},
	); !errors.Is(
		err,
		ErrArchiveNotReady,
	) {
		t.Fatalf("completed import download error = %v", err)
	}
}
