package jobs

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

const benchmarkWorkspaceID = "c2b604c6-6960-41a7-b330-5083ca633434"

func benchmarkStore(b *testing.B) (*sql.DB, *store) {
	b.Helper()

	db := openJobsTestPostgres(b)
	table := "importexport_jobs_bench_" + newToken()[:12]

	if err := BootstrapTable(b.Context(), db, table); err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		_, _ = db.ExecContext(
			b.Context(),
			"DROP TABLE IF EXISTS "+quoteIdentifier(table)+" CASCADE",
		)
	})

	return db, &store{
		db:        db,
		table:     table,
		history:   table + "_history",
		workerID:  "bench-" + newToken(),
		leaseTime: time.Minute,
	}
}

func benchmarkQueuedJob(b *testing.B, store *store, workspaceID string) Job {
	b.Helper()

	job, err := store.queue(
		b.Context(),
		"service",
		workspaceID,
		TypeExport,
		"dump.zip",
		"",
	)
	if err != nil {
		b.Fatal(err)
	}

	return job
}

func BenchmarkQueueExportStoreQueuePostgres(b *testing.B) {
	db, store := benchmarkStore(b)
	workspaceIDs := make([]string, b.N)

	for i := range workspaceIDs {
		workspaceIDs[i] = "c2b604c6-6960-41a7-b330-5083ca" + newToken()[:4]
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		workspaceID := workspaceIDs[i]
		if _, err := store.queue(
			b.Context(),
			"service",
			workspaceID,
			TypeExport,
			"dump.zip",
			"",
		); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()

		if _, err := db.ExecContext(
			b.Context(),
			"DELETE FROM "+quoteIdentifier(
				store.table,
			)+" WHERE workspace_id = $1",
			workspaceID,
		); err != nil {
			b.Fatal(err)
		}

		if i+1 < b.N {
			b.StartTimer()
		}
	}
}

func BenchmarkLeasePostgres(b *testing.B) {
	db, store := benchmarkStore(b)
	job := benchmarkQueuedJob(b, store, benchmarkWorkspaceID)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := store.lease(b.Context()); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()

		if _, err := db.ExecContext(
			b.Context(),
			"UPDATE "+quoteIdentifier(
				store.table,
			)+" SET status = 'queued', locked_by = NULL, lease_token = NULL, locked_until = NULL, started_at = NULL WHERE id = $1",
			job.ID,
		); err != nil {
			b.Fatal(err)
		}

		if _, err := db.ExecContext(
			b.Context(),
			"DELETE FROM "+quoteIdentifier(
				store.history,
			)+" WHERE job_id = $1 AND status = 'processing'",
			job.ID,
		); err != nil {
			b.Fatal(err)
		}

		if i+1 < b.N {
			b.StartTimer()
		}
	}
}

func BenchmarkStatusStoreGetPostgres(b *testing.B) {
	_, store := benchmarkStore(b)
	job := benchmarkQueuedJob(b, store, benchmarkWorkspaceID)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := store.get(
			b.Context(),
			"service",
			benchmarkWorkspaceID,
			job.ID,
		); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkHistoryStoreHistoryForPostgres(b *testing.B) {
	db, store := benchmarkStore(b)
	job := benchmarkQueuedJob(b, store, benchmarkWorkspaceID)

	for i := 0; i < 9; i++ {
		if _, err := db.ExecContext(
			b.Context(),
			"INSERT INTO "+quoteIdentifier(
				store.history,
			)+" (job_id, status, message) VALUES ($1, 'processing', '')",
			job.ID,
		); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := store.historyFor(b.Context(), job.ID, 10, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCleanupClaimPostgres(b *testing.B) {
	db, store := benchmarkStore(b)
	job := benchmarkQueuedJob(b, store, benchmarkWorkspaceID)

	if _, err := db.ExecContext(
		b.Context(),
		"UPDATE "+quoteIdentifier(
			store.table,
		)+" SET status = 'completed', archive_key = 'dump.zip', archive_expires_at = now() - interval '1 second' WHERE id = $1",
		job.ID,
	); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		claims, err := store.claimExpiredArchives(b.Context(), 1)
		if err != nil || len(claims) != 1 {
			b.Fatalf("claims = %#v, err = %v", claims, err)
		}

		b.StopTimer()

		if _, err := db.ExecContext(
			b.Context(),
			"UPDATE "+quoteIdentifier(
				store.table,
			)+" SET archive_claim_token = NULL, archive_claimed_until = NULL WHERE id = $1",
			job.ID,
		); err != nil {
			b.Fatal(err)
		}

		if i+1 < b.N {
			b.StartTimer()
		}
	}
}

func BenchmarkCleanupClearPostgres(b *testing.B) {
	db, store := benchmarkStore(b)
	job := benchmarkQueuedJob(b, store, benchmarkWorkspaceID)
	claimToken := newToken()
	prepare := func() {
		if _, err := db.ExecContext(
			b.Context(),
			"UPDATE "+quoteIdentifier(
				store.table,
			)+" SET status = 'completed', archive_key = 'dump.zip', archive_expires_at = now() - interval '1 second', archive_claim_token = $1, archive_claimed_until = now() + interval '1 minute' WHERE id = $2",
			claimToken,
			job.ID,
		); err != nil {
			b.Fatal(err)
		}
	}
	prepare()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := store.clearArchive(
			b.Context(),
			job.ID,
			"dump.zip",
			claimToken,
		); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()

		if i+1 < b.N {
			claimToken = newToken()

			prepare()
			b.StartTimer()
		}
	}
}

func BenchmarkQueueExportActiveJobContentionPostgres(b *testing.B) {
	_, store := benchmarkStore(b)
	benchmarkQueuedJob(b, store, benchmarkWorkspaceID)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := store.queue(
				b.Context(),
				"service",
				benchmarkWorkspaceID,
				TypeExport,
				"dump.zip",
				"",
			)
			if !errors.Is(err, ErrActiveJob) {
				b.Fatalf("queue error = %v, want %v", err, ErrActiveJob)
			}
		}
	})
}
