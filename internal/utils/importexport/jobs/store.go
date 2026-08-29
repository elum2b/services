package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
)

var tableNameExpression = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type store struct {
	db        *sql.DB
	table     string
	history   string
	workerID  string
	leaseTime time.Duration

	queryTemplatesOnce sync.Once
	queryTemplates     storeQueryTemplates
}

type storeQueryTemplates struct {
	queue, get, historyFor, list, lease, complete, fail string
	claimExpiredArchives                                string
	clearArchive, releaseArchiveClaim, addHistoryTx     string
}

func (s *store) queries() *storeQueryTemplates {
	s.queryTemplatesOnce.Do(func() {
		table := quoteIdentifier(s.table)
		history := quoteIdentifier(s.history)

		s.queryTemplates = storeQueryTemplates{
			queue: fmt.Sprintf(`WITH inserted AS (
	    INSERT INTO %s (service, workspace_id, type, file_name, options, archive_key)
	    VALUES ($1, $2, $3, $4, $5::jsonb, NULLIF($6, '')) RETURNING *
), history AS (
    INSERT INTO %s (job_id, status, message) SELECT id, 'queued', '' FROM inserted
)
SELECT %s FROM inserted`, table, history, jobColumns),
			get: fmt.Sprintf(
				`SELECT %s FROM %s WHERE id = $1 AND service = $2 AND workspace_id = $3`,
				jobColumns,
				table,
			),
			historyFor: fmt.Sprintf(
				`SELECT id, job_id, status, message, created_at FROM %s WHERE job_id = $1 ORDER BY id LIMIT $2 OFFSET $3`,
				history,
			),
			list: fmt.Sprintf(
				`SELECT %s FROM %s WHERE service = $1 AND workspace_id = $2 ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`,
				jobColumns,
				table,
			),
			lease: fmt.Sprintf(`WITH due AS (
    SELECT id FROM %s WHERE status IN ('queued', 'processing')
      AND (locked_until IS NULL OR locked_until <= now()) ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE %s AS job SET status = 'processing', locked_by = $1, lease_token = $2,
    locked_until = now() + ($3 * interval '1 microsecond'), started_at = COALESCE(started_at, now()), updated_at = now()
FROM due WHERE job.id = due.id RETURNING %s`, table, table, qualifiedJobColumns),
			complete: fmt.Sprintf(
				`UPDATE %s SET status = 'completed', archive_key = NULLIF($1, ''), archive_expires_at = now() + ($2 * interval '1 microsecond'), error = NULL, locked_by = NULL, lease_token = NULL, locked_until = NULL, finished_at = now(), updated_at = now() WHERE id = $3 AND status = 'processing' AND locked_by = $4 AND lease_token = $5 AND locked_until > now()`,
				table,
			),
			fail: fmt.Sprintf(
				`UPDATE %s SET status = 'failed', error = $1, archive_expires_at = CASE WHEN archive_key IS NULL THEN NULL ELSE now() + ($2 * interval '1 microsecond') END, locked_by = NULL, lease_token = NULL, locked_until = NULL, finished_at = now(), updated_at = now() WHERE id = $3 AND status = 'processing' AND locked_by = $4 AND lease_token = $5 AND locked_until > now()`,
				table,
			),
			claimExpiredArchives: fmt.Sprintf(`WITH due AS (
 SELECT id FROM %s WHERE archive_key IS NOT NULL AND archive_expires_at <= now()
   AND (archive_claimed_until IS NULL OR archive_claimed_until <= now()) ORDER BY archive_expires_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
)
UPDATE %s AS job SET archive_claim_token = $2, archive_claimed_until = now() + ($3 * interval '1 microsecond') FROM due WHERE job.id = due.id RETURNING %s`, table, table, qualifiedJobColumns),
			clearArchive: fmt.Sprintf(
				`UPDATE %s SET archive_key = NULL, archive_expires_at = NULL, archive_claim_token = NULL, archive_claimed_until = NULL, updated_at = now() WHERE id = $1 AND archive_key = $2 AND archive_claim_token = $3`,
				table,
			),
			releaseArchiveClaim: fmt.Sprintf(
				`UPDATE %s SET archive_claim_token = NULL, archive_claimed_until = NULL, updated_at = now() WHERE id = $1 AND archive_key = $2 AND archive_claim_token = $3`,
				table,
			),
			addHistoryTx: fmt.Sprintf(
				`INSERT INTO %s (job_id, status, message) VALUES ($1, $2, $3)`,
				history,
			),
		}
	})

	return &s.queryTemplates
}

func Bootstrap(ctx context.Context, db *sql.DB) error {
	return BootstrapTable(ctx, db, DefaultTable)
}

func BootstrapTable(ctx context.Context, db *sql.DB, tableName string) error {
	if db == nil {
		return errors.New("importexport jobs: nil db")
	}

	if !isPostgresDB(db) {
		return errors.New("importexport jobs: PostgreSQL is required")
	}

	tableName = normalizeTableName(tableName)

	historyTable := tableName + "_history"
	table := quoteIdentifier(tableName)
	history := quoteIdentifier(historyTable)
	statements := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id BIGSERIAL PRIMARY KEY, service VARCHAR(64) NOT NULL, workspace_id VARCHAR(36) NOT NULL,

 type VARCHAR(16) NOT NULL, status VARCHAR(16) NOT NULL DEFAULT 'queued', file_name TEXT NOT NULL DEFAULT '', options JSONB NOT NULL DEFAULT '{}'::jsonb, archive_key TEXT NULL,
	archive_expires_at TIMESTAMPTZ NULL, error TEXT NULL, locked_by VARCHAR(128) NULL,
	lease_token TEXT NULL, locked_until TIMESTAMPTZ NULL, archive_claim_token TEXT NULL, archive_claimed_until TIMESTAMPTZ NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
started_at TIMESTAMPTZ NULL, finished_at TIMESTAMPTZ NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
CONSTRAINT %s_type_chk CHECK (type IN ('export', 'import')),
CONSTRAINT %s_status_chk CHECK (status IN ('queued', 'processing', 'completed', 'failed'))
)`, table, tableName, tableName),
		fmt.Sprintf(
			`CREATE UNIQUE INDEX IF NOT EXISTS %s_active_workspace_uq ON %s (service, workspace_id) WHERE status IN ('queued', 'processing')`,
			tableName,
			table,
		),
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_due_idx ON %s (status, locked_until, id)`,
			tableName,
			table,
		),
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_history_idx ON %s (service, workspace_id, created_at DESC, id DESC)`,
			tableName,
			table,
		),
		fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s (id BIGSERIAL PRIMARY KEY, job_id BIGINT NOT NULL REFERENCES %s(id) ON DELETE CASCADE, status VARCHAR(16) NOT NULL, message TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now())`,
			history,
			table,
		),
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_job_idx ON %s (job_id, id)`,
			historyTable,
			history,
		),
	}

	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("importexport jobs schema: %w", err)
		}
	}

	// Keep existing installations compatible with the current schema.
	for _, statement := range []string{
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS file_name TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS options JSONB NOT NULL DEFAULT '{}'::jsonb`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS lease_token TEXT NULL`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS archive_claim_token TEXT NULL`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS archive_claimed_until TIMESTAMPTZ NULL`, table),
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("importexport jobs schema migration: %w", err)
		}
	}

	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf(
			`CREATE INDEX IF NOT EXISTS %s_archive_cleanup_idx ON %s (archive_expires_at, archive_claimed_until, id) WHERE archive_key IS NOT NULL`,
			tableName,
			table,
		),
	); err != nil {
		return fmt.Errorf("importexport jobs schema: %w", err)
	}

	return nil
}

func (s *store) queue(
	ctx context.Context,
	service, workspaceID, kind, fileName, archiveKey string,
) (Job, error) {
	return s.queueWithOptions(
		ctx,
		service,
		workspaceID,
		kind,
		fileName,
		nil,
		archiveKey,
	)
}

func (s *store) queueWithOptions(
	ctx context.Context,
	service, workspaceID, kind, fileName string,
	options []byte,
	archiveKey string,
) (Job, error) {
	if len(options) == 0 {
		options = []byte("{}")
	}

	if !json.Valid(options) {
		return Job{}, errors.New(
			"importexport jobs: options must be valid JSON",
		)
	}

	var job Job

	err := s.db.QueryRowContext(ctx, s.queries().queue, service, workspaceID, kind, fileName, options, archiveKey).
		Scan(jobDestinations(&job)...)
	if err != nil {
		if isUniqueViolation(err) {
			return Job{}, ErrActiveJob
		}

		return Job{}, err
	}

	return job, nil
}

func (s *store) get(
	ctx context.Context,
	service, workspaceID string,
	id int64,
) (Job, error) {
	var job Job

	err := s.db.QueryRowContext(ctx, s.queries().get, id, service, workspaceID).
		Scan(jobDestinations(&job)...)

	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrJobNotFound
	}

	return job, err
}

func (s *store) historyFor(
	ctx context.Context,
	jobID int64,
	limit, offset int32,
) ([]HistoryEntry, error) {
	rows, err := s.db.QueryContext(
		ctx,
		s.queries().historyFor,
		jobID,
		limit,
		offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := make([]HistoryEntry, 0)

	for rows.Next() {
		var entry HistoryEntry

		if err := rows.Scan(
			&entry.ID,
			&entry.JobID,
			&entry.Status,
			&entry.Message,
			&entry.CreatedAt,
		); err != nil {
			return nil, err
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

func (s *store) list(ctx context.Context, params HistoryParams) ([]Job, error) {
	rows, err := s.db.QueryContext(
		ctx,
		s.queries().list,
		params.Service,
		params.WorkspaceID,
		params.Limit,
		params.Offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]Job, 0)

	for rows.Next() {
		var job Job

		if err := rows.Scan(jobDestinations(&job)...); err != nil {
			return nil, err
		}

		jobs = append(jobs, job)
	}

	return jobs, rows.Err()
}

func (s *store) lease(ctx context.Context) (Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback()

	var job Job

	leaseToken := newToken()

	err = tx.QueryRowContext(ctx, s.queries().lease, s.workerID, leaseToken, s.leaseTime.Microseconds()).
		Scan(jobDestinations(&job)...)

	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, nil
	}

	if err != nil {
		return Job{}, err
	}

	if err := s.addHistoryTx(
		ctx,
		tx,
		job.ID,
		StatusProcessing,
		"",
	); err != nil {
		return Job{}, err
	}

	if err := tx.Commit(); err != nil {
		return Job{}, err
	}

	job.LeaseToken = leaseToken

	return job, nil
}

func (s *store) complete(
	ctx context.Context,
	jobID int64,
	leaseToken, archiveKey string,
	retention time.Duration,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(
		ctx,
		s.queries().complete,
		archiveKey,
		retention.Microseconds(),
		jobID,
		s.workerID,
		leaseToken,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrNotLeased
	}

	if err := s.addHistoryTx(ctx, tx, jobID, StatusCompleted, ""); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *store) fail(
	ctx context.Context,
	jobID int64,
	leaseToken string,
	message string,
	retention time.Duration,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(
		ctx,
		s.queries().fail,
		message,
		retention.Microseconds(),
		jobID,
		s.workerID,
		leaseToken,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ErrNotLeased
	}

	if err := s.addHistoryTx(
		ctx,
		tx,
		jobID,
		StatusFailed,
		message,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *store) claimExpiredArchives(
	ctx context.Context,
	limit int32,
) ([]Job, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	claimToken := newToken()

	rows, err := tx.QueryContext(
		ctx,
		s.queries().claimExpiredArchives,
		limit,
		claimToken,
		s.leaseTime.Microseconds(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]Job, 0)

	for rows.Next() {
		var job Job

		if err := rows.Scan(jobDestinations(&job)...); err != nil {
			return nil, err
		}

		jobs = append(jobs, job)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	for index := range jobs {
		jobs[index].LeaseToken = claimToken
	}

	return jobs, nil
}

func (s *store) clearArchive(
	ctx context.Context,
	id int64,
	key, claimToken string,
) error {
	result, err := s.db.ExecContext(
		ctx,
		s.queries().clearArchive,
		id,
		key,
		claimToken,
	)
	if err != nil {
		return err
	}

	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return ErrNotLeased
	}

	return nil
}

func (s *store) releaseArchiveClaim(
	ctx context.Context,
	id int64,
	key, claimToken string,
) error {
	_, err := s.db.ExecContext(
		ctx,
		s.queries().releaseArchiveClaim,
		id,
		key,
		claimToken,
	)

	return err
}

func (s *store) addHistoryTx(
	ctx context.Context,
	tx *sql.Tx,
	jobID int64,
	status, message string,
) error {
	_, err := tx.ExecContext(
		ctx,
		s.queries().addHistoryTx,
		jobID,
		status,
		message,
	)

	return err
}

const jobColumns = "id, service, workspace_id, type, status, file_name, options, COALESCE(archive_key, ''), archive_expires_at, COALESCE(error, ''), COALESCE(locked_by, ''), COALESCE(lease_token, ''), locked_until, created_at, started_at, finished_at, updated_at"

const qualifiedJobColumns = "job.id, job.service, job.workspace_id, job.type, job.status, job.file_name, job.options, COALESCE(job.archive_key, ''), job.archive_expires_at, COALESCE(job.error, ''), COALESCE(job.locked_by, ''), COALESCE(job.lease_token, ''), job.locked_until, job.created_at, job.started_at, job.finished_at, job.updated_at"

func jobDestinations(job *Job) []any {
	return []any{
		&job.ID,
		&job.Service,
		&job.WorkspaceID,
		&job.Type,
		&job.Status,
		&job.FileName,
		&job.Options,
		&job.ArchiveKey,
		&job.ArchiveExpires,
		&job.Error,
		&job.LockedBy,
		&job.LeaseToken,
		&job.LockedUntil,
		&job.CreatedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&job.UpdatedAt,
	}
}
func normalizeTableName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if !tableNameExpression.MatchString(value) || len(value) > 40 {
		return DefaultTable
	}

	return value
}
func quoteIdentifier(value string) string { return `"` + value + `"` }
func isPostgresDB(db *sql.DB) bool {
	if db == nil {
		return false
	}

	name := fmt.Sprintf("%T", db.Driver())

	return strings.Contains(name, "pgx") || strings.Contains(name, "pq") ||
		strings.Contains(name, "stdlib")
}
func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "duplicate key") ||
		strings.Contains(err.Error(), "SQLSTATE 23505")
}
