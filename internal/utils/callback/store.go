package callback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	services "github.com/elum2b/services"
	callbacksqlc "github.com/elum2b/services/internal/utils/callback/sqlc"
)

const (
	DefaultSourceService = "service"
	JSONContentType      = "application/json"
	StatusProcessing     = "processing"

	DefaultTable  = "clb_event"
	PaymentTable  = "payment_clb_event"
	CPATable      = "cpa_clb_event"
	PromoTable    = "promo_clb_event"
	CalendarTable = "calendar_clb_event"
	TasksTable    = "tasks_clb_event"
)

var (
	ErrNotLeased        = errors.New("callback: event is not leased by worker")
	tableNameExpression = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct {
	db        *sql.DB
	executor  dbtx
	tableName string
	postgres  bool
}

type CreateParams struct {
	WorkspaceID        string
	SourceService      string
	EventType          string
	EventKey           string
	IdempotencyKey     string
	Payload            []byte
	PayloadContentType string
	NextAttemptAt      time.Time
}

type LeaseParams struct {
	SourceService string
	WorkerID      string
	Limit         int32
	LeaseTimeout  time.Duration
}

type FailParams struct {
	ID       uint64
	WorkerID string
	Error    string
	Attempt  uint32
	FailedAt time.Time
}

type AdminListEventsParams struct {
	WorkspaceID   string
	SourceService string
	EventType     string
	Status        string
	Limit         int32
	Offset        int32
}

type Event struct {
	ID                 uint64
	WorkspaceID        string
	SourceService      string
	EventType          string
	EventKey           string
	IdempotencyKey     string
	Payload            []byte
	PayloadContentType string
	Status             string
	AttemptCount       uint32
	NextAttemptAt      time.Time
	LockedBy           *string
	LockedUntil        *time.Time
	DeliveredAt        *time.Time
	RejectedAt         *time.Time
	LastError          *string
	RejectReason       *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func New(db *sql.DB) *Store {
	return NewWithTable(db, DefaultTable)
}

func NewWithTable(db *sql.DB, tableName string) *Store {
	tableName = normalizeTableName(tableName)
	return &Store{db: db, executor: db, tableName: tableName, postgres: isPostgresDB(db)}
}

func (s *Store) WithTx(tx *sql.Tx) *Store {
	if s == nil {
		return nil
	}
	return &Store{db: s.db, executor: tx, tableName: s.tableName, postgres: s.postgres}
}

func (s *Store) Close() error { return nil }

func Bootstrap(ctx context.Context, db *sql.DB) error {
	return BootstrapTable(ctx, db, DefaultTable)
}

func BootstrapTable(ctx context.Context, db *sql.DB, tableName string) error {
	if db == nil {
		return errors.New("callback: nil db")
	}
	tableName = normalizeTableName(tableName)
	if isPostgresDB(db) {
		return bootstrapPostgresTable(ctx, db, tableName)
	}
	return bootstrapMySQLTable(ctx, db, tableName)
}

func (s *Store) CreateEvent(ctx context.Context, params CreateParams) (uint64, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}
	sourceService := params.SourceService
	if sourceService == "" {
		sourceService = DefaultSourceService
	}
	contentType := params.PayloadContentType
	if contentType == "" {
		contentType = JSONContentType
	}
	nextAttemptAt := params.NextAttemptAt
	if nextAttemptAt.IsZero() {
		nextAttemptAt = time.Now().UTC()
	} else {
		nextAttemptAt = nextAttemptAt.UTC()
	}
	idempotencyKey := strings.TrimSpace(params.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = params.EventKey
	}
	if idempotencyKey == "" {
		return 0, errors.New("callback: idempotency key is required")
	}
	if !s.postgres {
		result, err := s.executor.ExecContext(ctx, fmt.Sprintf(`
INSERT INTO %s (
    workspace_id, source_service, event_type, event_key, idempotency_key,
    payload, payload_content_type, next_attempt_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = LAST_INSERT_ID(id)`, s.table()),
			workspaceID, sourceService, params.EventType, params.EventKey, idempotencyKey,
			params.Payload, contentType, nextAttemptAt,
		)
		if err != nil {
			return 0, err
		}
		id, err := result.LastInsertId()
		return uint64(id), err
	}
	var id int64
	err = s.executor.QueryRowContext(ctx, fmt.Sprintf(`
INSERT INTO %s (
    workspace_id, source_service, event_type, event_key, idempotency_key,
    payload, payload_content_type, next_attempt_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (idempotency_key) DO UPDATE SET
    idempotency_key = EXCLUDED.idempotency_key
RETURNING id`, s.table()),
		workspaceID, sourceService, params.EventType, params.EventKey, idempotencyKey,
		params.Payload, contentType, nextAttemptAt,
	).Scan(&id)
	return uint64(id), err
}

func (s *Store) GetEvent(ctx context.Context, workspaceID string, id uint64) (Event, error) {
	if err := s.validate(); err != nil {
		return Event{}, err
	}
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return Event{}, err
	}
	query := fmt.Sprintf(`
SELECT %s FROM %s WHERE workspace_id = ? AND id = ? LIMIT 1`, eventColumns, s.table())
	if s.postgres {
		query = rewriteQuestionPlaceholders(query)
	}
	row := s.executor.QueryRowContext(ctx, query, workspaceID, int64(id))
	value, err := scanEvent(row.Scan)
	if err != nil {
		return Event{}, err
	}
	return mapEvent(value), nil
}

func (s *Store) AdminListEvents(ctx context.Context, params AdminListEventsParams) ([]Event, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizePage(params.Limit, params.Offset)
	query := fmt.Sprintf(`
SELECT %s FROM %s
WHERE workspace_id = ?
  AND (? = '' OR source_service = ?)
  AND (? = '' OR event_type = ?)
  AND (? = '' OR status = ?)
ORDER BY created_at DESC, id DESC
LIMIT ? OFFSET ?`, eventColumns, s.table())
	if s.postgres {
		query = rewriteQuestionPlaceholders(query)
	}
	rows, err := s.executor.QueryContext(ctx, query,
		workspaceID,
		params.SourceService, params.SourceService,
		params.EventType, params.EventType,
		params.Status, params.Status,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		value, err := scanEvent(rows.Scan)
		if err != nil {
			return nil, err
		}
		result = append(result, mapEvent(value))
	}
	return result, rows.Err()
}

func (s *Store) AdminRetryEventNow(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	return s.execRows(ctx, `
SET status = 'pending', next_attempt_at = NOW(), locked_by = NULL,
    locked_until = NULL, last_error = NULL, updated_at = NOW()
WHERE workspace_id = ? AND id = ? AND status IN ('pending', 'processing')`, workspaceID, int64(id))
}

func (s *Store) AdminMarkEventOK(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	return s.execRows(ctx, `
SET status = 'ok', delivered_at = NOW(), locked_by = NULL,
    locked_until = NULL, last_error = NULL, updated_at = NOW()
WHERE workspace_id = ? AND id = ? AND status IN ('pending', 'processing')`, workspaceID, int64(id))
}

func (s *Store) AdminMarkEventReject(ctx context.Context, workspaceID string, id uint64, reason string) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	return s.execRows(ctx, `
SET status = 'reject', rejected_at = NOW(), reject_reason = ?,
    locked_by = NULL, locked_until = NULL, updated_at = NOW()
WHERE workspace_id = ? AND id = ? AND status IN ('pending', 'processing')`, nullableString(reason), workspaceID, int64(id))
}

func (s *Store) AdminResetExpiredProcessing(ctx context.Context, workspaceID string) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	return s.execRows(ctx, `
SET status = 'pending', locked_by = NULL, locked_until = NULL,
    next_attempt_at = NOW(), updated_at = NOW()
WHERE workspace_id = ? AND status = 'processing' AND locked_until IS NOT NULL AND locked_until <= NOW()`, workspaceID)
}

func (s *Store) LeaseEvents(ctx context.Context, params LeaseParams) ([]storedEvent, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	workerID := normalizeWorkerID(params.WorkerID)
	limit := params.Limit
	if limit <= 0 {
		limit = 1
	}
	leaseTimeout := params.LeaseTimeout
	if leaseTimeout <= 0 {
		leaseTimeout = time.Minute
	}

	var leased []storedEvent
	if err := s.withTx(ctx, func(txStore *Store) error {
		query := fmt.Sprintf(`
SELECT %s FROM %s
WHERE (? = '' OR source_service = ?)
  AND status IN ('pending', 'processing')
  AND next_attempt_at <= NOW()
  AND (locked_until IS NULL OR locked_until <= NOW())
ORDER BY next_attempt_at, id
LIMIT ?
FOR UPDATE SKIP LOCKED`, eventColumns, txStore.table())
		if txStore.postgres {
			query = rewriteQuestionPlaceholders(query)
		}
		rows, err := txStore.executor.QueryContext(ctx, query,
			params.SourceService, params.SourceService, limit,
		)
		if err != nil {
			return err
		}
		candidates := make([]storedEvent, 0, limit)
		for rows.Next() {
			row, err := scanEvent(rows.Scan)
			if err != nil {
				_ = rows.Close()
				return err
			}
			candidates = append(candidates, row)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}

		lockedBy := sql.NullString{String: workerID, Valid: true}
		lockedUntil := sql.NullTime{Time: time.Now().UTC().Add(leaseTimeout), Valid: true}
		for _, row := range candidates {
			affected, err := txStore.execRows(ctx, `
SET status = 'processing', locked_by = ?, locked_until = ?, updated_at = NOW()
WHERE id = ? AND status IN ('pending', 'processing')
  AND (locked_until IS NULL OR locked_until <= NOW())`,
				lockedBy, lockedUntil, row.ID,
			)
			if err != nil {
				return err
			}
			if affected == 0 {
				continue
			}
			row.Status = StatusProcessing
			row.LockedBy = lockedBy
			row.LockedUntil = lockedUntil
			leased = append(leased, row)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return leased, nil
}

func (s *Store) MarkOK(ctx context.Context, id uint64, workerID string) error {
	rows, err := s.execRows(ctx, `
SET status = 'ok', delivered_at = NOW(), locked_by = NULL,
    locked_until = NULL, last_error = NULL, updated_at = NOW()
WHERE id = ? AND status = 'processing' AND locked_by = ?`,
		int64(id), normalizeWorkerID(workerID),
	)
	return leasedResult(rows, err)
}

func (s *Store) MarkReject(ctx context.Context, id uint64, workerID string, reason string) error {
	rows, err := s.execRows(ctx, `
SET status = 'reject', rejected_at = NOW(), reject_reason = ?,
    locked_by = NULL, locked_until = NULL, updated_at = NOW()
WHERE id = ? AND status = 'processing' AND locked_by = ?`,
		nullableString(reason), int64(id), normalizeWorkerID(workerID),
	)
	return leasedResult(rows, err)
}

func (s *Store) MarkFailed(ctx context.Context, params FailParams) error {
	failedAt := params.FailedAt
	if failedAt.IsZero() {
		failedAt = time.Now().UTC()
	} else {
		failedAt = failedAt.UTC()
	}
	rows, err := s.execRows(ctx, `
SET status = 'pending', attempt_count = attempt_count + 1,
    next_attempt_at = ?, locked_by = NULL, locked_until = NULL,
    last_error = ?, updated_at = NOW()
WHERE id = ? AND status = 'processing' AND locked_by = ?`,
		failedAt.Add(RetryDelay(params.Attempt)), nullableString(params.Error),
		int64(params.ID), normalizeWorkerID(params.WorkerID),
	)
	return leasedResult(rows, err)
}

func RetryDelay(attempt uint32) time.Duration {
	switch attempt {
	case 0:
		return 5 * time.Second
	case 1:
		return 30 * time.Second
	case 2:
		return time.Minute
	case 3:
		return 5 * time.Minute
	case 4:
		return 10 * time.Minute
	case 5:
		return 30 * time.Minute
	default:
		return time.Hour
	}
}

func normalizeWorkerID(workerID string) string {
	if workerID == "" {
		return "default"
	}
	return workerID
}

func normalizePage(limit int32, offset int32) (int32, int32) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func requireWorkspaceID(value string) (string, error) {
	if err := services.ValidateWorkspaceID(value); err != nil {
		return "", err
	}
	return value, nil
}

func (s *Store) withTx(ctx context.Context, fn func(*Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txStore := s.WithTx(tx)
	if err := fn(txStore); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w: rollback: %v", err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

func (s *Store) execRows(ctx context.Context, update string, args ...any) (int64, error) {
	if err := s.validate(); err != nil {
		return 0, err
	}
	query := "UPDATE " + s.table() + " " + update
	if s.postgres {
		query = rewriteQuestionPlaceholders(query)
	}
	result, err := s.executor.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) validate() error {
	if s == nil || s.db == nil || s.executor == nil {
		return ErrStoreNotConfigured
	}
	if !tableNameExpression.MatchString(s.tableName) {
		return errors.New("callback: invalid table name")
	}
	return nil
}

func (s *Store) table() string {
	if s.postgres {
		return quotePostgresIdentifier(s.tableName)
	}
	return quoteIdentifier(s.tableName)
}

func normalizeTableName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if !tableNameExpression.MatchString(value) {
		return DefaultTable
	}
	return value
}

func quoteIdentifier(value string) string {
	return "`" + value + "`"
}

func quotePostgresIdentifier(value string) string {
	return `"` + value + `"`
}

func isPostgresDB(db *sql.DB) bool {
	if db == nil {
		return false
	}
	driverName := fmt.Sprintf("%T", db.Driver())
	return strings.Contains(driverName, "pgx") || strings.Contains(driverName, "pq") || strings.Contains(driverName, "stdlib")
}

func rewriteQuestionPlaceholders(query string) string {
	var builder strings.Builder
	builder.Grow(len(query) + 16)
	index := 1
	for _, r := range query {
		if r == '?' {
			builder.WriteByte('$')
			builder.WriteString(fmt.Sprint(index))
			index++
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func bootstrapMySQLTable(ctx context.Context, db *sql.DB, tableName string) error {
	table := quoteIdentifier(tableName)
	statement := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    source_service VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    payload LONGBLOB NOT NULL,
    payload_content_type VARCHAR(64) NOT NULL DEFAULT 'application/json',
    status ENUM('pending', 'processing', 'ok', 'reject') NOT NULL DEFAULT 'pending',
    attempt_count INT UNSIGNED NOT NULL DEFAULT 0,
    next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    locked_by VARCHAR(128) NULL,
    locked_until DATETIME NULL,
    delivered_at DATETIME NULL,
    rejected_at DATETIME NULL,
    last_error TEXT NULL,
    reject_reason TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY callback_event_key_uq (source_service, event_key),
    UNIQUE KEY callback_idempotency_key_uq (idempotency_key),
    KEY callback_due_idx (status, next_attempt_at, locked_until, id),
    KEY callback_type_idx (event_type, status, created_at)
)`, table)
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("callback schema statement failed for %s: %w", tableName, err)
	}
	return nil
}

func bootstrapPostgresTable(ctx context.Context, db *sql.DB, tableName string) error {
	table := quotePostgresIdentifier(tableName)
	statement := fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    id BIGSERIAL PRIMARY KEY,
    workspace_id VARCHAR(36) NOT NULL,
    source_service VARCHAR(64) NOT NULL,
    event_type VARCHAR(128) NOT NULL,
    event_key VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(191) NOT NULL,
    payload BYTEA NOT NULL,
    payload_content_type VARCHAR(64) NOT NULL DEFAULT 'application/json',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_by VARCHAR(128) NULL,
    locked_until TIMESTAMPTZ NULL,
    delivered_at TIMESTAMPTZ NULL,
    rejected_at TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    reject_reason TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT %s_status_chk CHECK (status IN ('pending', 'processing', 'ok', 'reject')),
    CONSTRAINT %s_event_key_uq UNIQUE (source_service, event_key),
    CONSTRAINT %s_idempotency_key_uq UNIQUE (idempotency_key)
)`, table, tableName, tableName, tableName)
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("callback schema statement failed for %s: %w", tableName, err)
	}
	if _, err := db.ExecContext(
		ctx,
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS workspace_id VARCHAR(36) NOT NULL DEFAULT ''`, table),
	); err != nil {
		return fmt.Errorf("callback workspace migration failed for %s: %w", tableName, err)
	}
	indexes := []string{
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_due_idx ON %s (status, next_attempt_at, locked_until, id)`, tableName, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s_workspace_type_idx ON %s (workspace_id, event_type, status, created_at)`, tableName, table),
	}
	for _, statement := range indexes {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("callback schema statement failed for %s: %w", tableName, err)
		}
	}
	return nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

func leasedResult(rows int64, err error) error {
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotLeased
	}
	return nil
}

const eventColumns = `
id, workspace_id, source_service, event_type, event_key, idempotency_key,
payload, payload_content_type, status, attempt_count, next_attempt_at,
locked_by, locked_until, delivered_at, rejected_at, last_error,
reject_reason, created_at, updated_at`

type scanFunc func(...any) error

type storedEvent struct {
	callbacksqlc.ClbEvent
}

func scanEvent(scan scanFunc) (storedEvent, error) {
	var value storedEvent
	err := scan(
		&value.ID,
		&value.WorkspaceID,
		&value.SourceService,
		&value.EventType,
		&value.EventKey,
		&value.IdempotencyKey,
		&value.Payload,
		&value.PayloadContentType,
		&value.Status,
		&value.AttemptCount,
		&value.NextAttemptAt,
		&value.LockedBy,
		&value.LockedUntil,
		&value.DeliveredAt,
		&value.RejectedAt,
		&value.LastError,
		&value.RejectReason,
		&value.CreatedAt,
		&value.UpdatedAt,
	)
	if err != nil {
		return storedEvent{}, err
	}
	return value, nil
}

func mapEvent(value storedEvent) Event {
	return Event{
		ID:                 uint64(value.ID),
		WorkspaceID:        value.WorkspaceID,
		SourceService:      value.SourceService,
		EventType:          value.EventType,
		EventKey:           value.EventKey,
		IdempotencyKey:     value.IdempotencyKey,
		Payload:            value.Payload,
		PayloadContentType: value.PayloadContentType,
		Status:             string(value.Status),
		AttemptCount:       uint32(value.AttemptCount),
		NextAttemptAt:      value.NextAttemptAt,
		LockedBy:           nullStringPtr(value.LockedBy),
		LockedUntil:        nullTimePtr(value.LockedUntil),
		DeliveredAt:        nullTimePtr(value.DeliveredAt),
		RejectedAt:         nullTimePtr(value.RejectedAt),
		LastError:          nullStringPtr(value.LastError),
		RejectReason:       nullStringPtr(value.RejectReason),
		CreatedAt:          value.CreatedAt,
		UpdatedAt:          value.UpdatedAt,
	}
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
