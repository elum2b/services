package jobs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/utils/goroutine"
)

type Manager struct {
	store         *store
	archive       Archive
	handler       Handler
	retention     time.Duration
	idleDelay     time.Duration
	cleanupPeriod time.Duration
	workers       *goroutine.Manager
	rootCtx       context.Context
	cancel        context.CancelFunc
	startMu       sync.Mutex
	started       bool
}

func New(db *sql.DB, archive Archive, handler Handler, options Options) (*Manager, error) {
	if db == nil {
		return nil, errors.New("importexport jobs: nil db")
	}
	if !isPostgresDB(db) {
		return nil, errors.New("importexport jobs: PostgreSQL is required")
	}
	if archive == nil {
		return nil, errors.New("importexport jobs: archive is required")
	}
	if handler == nil {
		return nil, errors.New("importexport jobs: handler is required")
	}
	if options.TableName == "" {
		options.TableName = DefaultTable
	}
	if options.WorkerID == "" {
		options.WorkerID = "importexport-worker-" + newToken()
	}
	if options.IdleDelay <= 0 {
		options.IdleDelay = DefaultIdleDelay
	}
	if options.LeaseTimeout <= 0 {
		options.LeaseTimeout = DefaultLeaseTimeout
	}
	if options.Retention <= 0 {
		options.Retention = DefaultRetention
	}
	if options.CleanupPeriod <= 0 {
		options.CleanupPeriod = DefaultCleanupPeriod
	}
	rootCtx, cancel := context.WithCancel(context.Background())
	return &Manager{store: &store{db: db, table: normalizeTableName(options.TableName), history: normalizeTableName(options.TableName) + "_history", workerID: options.WorkerID, leaseTime: options.LeaseTimeout}, archive: archive, handler: handler, retention: options.Retention, idleDelay: options.IdleDelay, cleanupPeriod: options.CleanupPeriod, workers: goroutine.New(), rootCtx: rootCtx, cancel: cancel}, nil
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
	if m.workers != nil {
		m.workers.Close()
	}
}

func (m *Manager) QueueExport(ctx context.Context, params QueueExportParams) (Job, error) {
	if err := validateIdentity(params.Service, params.WorkspaceID); err != nil {
		return Job{}, err
	}
	return m.store.queueWithOptions(ctx, params.Service, params.WorkspaceID, TypeExport, params.FileName, params.Options, "")
}

func (m *Manager) QueueImport(ctx context.Context, params QueueImportParams) (Job, error) {
	if err := validateIdentity(params.Service, params.WorkspaceID); err != nil {
		return Job{}, err
	}
	if params.Dump == nil {
		return Job{}, errors.New("importexport jobs: import dump is required")
	}
	key, err := m.archive.Store(ctx, ArchiveObject{Service: params.Service, WorkspaceID: params.WorkspaceID, Type: TypeImport, FileName: params.FileName}, params.Dump)
	if err != nil {
		return Job{}, fmt.Errorf("store import dump: %w", err)
	}
	job, err := m.store.queueWithOptions(ctx, params.Service, params.WorkspaceID, TypeImport, params.FileName, params.Options, key)
	if err != nil {
		_ = m.archive.Delete(ctx, key)
		return Job{}, err
	}
	return job, nil
}

func (m *Manager) Status(ctx context.Context, params StatusParams) (Job, error) {
	if err := validateIdentity(params.Service, params.WorkspaceID); err != nil {
		return Job{}, err
	}
	return m.store.get(ctx, params.Service, params.WorkspaceID, params.ID)
}

func (m *Manager) History(ctx context.Context, params HistoryParams) ([]Job, error) {
	if err := validateIdentity(params.Service, params.WorkspaceID); err != nil {
		return nil, err
	}
	if params.Limit <= 0 {
		params.Limit = DefaultHistoryLimit
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	return m.store.list(ctx, params)
}

func (m *Manager) JobHistory(ctx context.Context, params JobHistoryParams) ([]HistoryEntry, error) {
	if err := validateIdentity(params.Service, params.WorkspaceID); err != nil {
		return nil, err
	}
	if _, err := m.store.get(ctx, params.Service, params.WorkspaceID, params.ID); err != nil {
		return nil, err
	}
	if params.Limit <= 0 {
		params.Limit = DefaultHistoryLimit
	}
	if params.Offset < 0 {
		params.Offset = 0
	}
	return m.store.historyFor(ctx, params.ID, params.Limit, params.Offset)
}

func (m *Manager) Download(ctx context.Context, params DownloadParams) (io.ReadCloser, Job, error) {
	job, err := m.Status(ctx, StatusParams(params))
	if err != nil {
		return nil, Job{}, err
	}
	if job.ArchiveKey == "" {
		return nil, job, ErrArchiveNotReady
	}
	dump, err := m.archive.Open(ctx, job.ArchiveKey)
	if err != nil {
		return nil, job, fmt.Errorf("open job archive: %w", err)
	}
	return dump, job, nil
}

// Start runs the queue worker and periodic archive cleanup.
func (m *Manager) Start(ctx context.Context) bool {
	if m == nil || m.workers == nil {
		return false
	}
	m.startMu.Lock()
	defer m.startMu.Unlock()
	if m.started {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.rootCtx, cancel)
	if !m.workers.Go("importexport.jobs", func() {
		defer stop()
		defer cancel()
		_ = m.Run(workerCtx)
	}) {
		stop()
		cancel()
		return false
	}
	started := m.workers.Go("importexport.jobs.cleanup", func() {
		defer stop()
		defer cancel()
		m.cleanupLoop(workerCtx)
	})
	if !started {
		stop()
		cancel()
		return false
	}
	m.started = true
	return true
}

// Run processes jobs until ctx is canceled. A transient database or handler
// failure is recorded on its job; database polling failures are retried.
func (m *Manager) Run(ctx context.Context) error {
	if m == nil {
		return errors.New("importexport jobs: nil manager")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		job, err := m.store.lease(ctx)
		if err != nil {
			if !wait(ctx, m.idleDelay) {
				return ctx.Err()
			}
			continue
		}
		if job.ID == 0 {
			if !wait(ctx, m.idleDelay) {
				return ctx.Err()
			}
			continue
		}
		m.handle(ctx, job)
	}
}

func (m *Manager) handle(ctx context.Context, job Job) {
	var err error
	switch job.Type {
	case TypeExport:
		err = m.handleExport(ctx, job)
	case TypeImport:
		err = m.handleImport(ctx, job)
	default:
		err = fmt.Errorf("unsupported job type %q", job.Type)
	}
	if err != nil {
		_ = m.store.fail(ctx, job.ID, job.LeaseToken, err.Error(), m.retention)
	}
}

func (m *Manager) handleExport(ctx context.Context, job Job) error {
	dump, err := m.handler.Export(ctx, job)
	if err != nil {
		return err
	}
	if dump == nil {
		return errors.New("importexport jobs: export handler returned a nil dump")
	}
	defer dump.Close()
	key, err := m.archive.Store(ctx, ArchiveObject{Service: job.Service, WorkspaceID: job.WorkspaceID, Type: TypeExport, FileName: job.FileName}, dump)
	if err != nil {
		return fmt.Errorf("store export dump: %w", err)
	}
	if err := m.store.complete(ctx, job.ID, job.LeaseToken, key, m.retention); err != nil {
		_ = m.archive.Delete(ctx, key)
		return err
	}
	return nil
}

func (m *Manager) handleImport(ctx context.Context, job Job) error {
	if job.ArchiveKey == "" {
		return ErrArchiveNotReady
	}
	dump, err := m.archive.Open(ctx, job.ArchiveKey)
	if err != nil {
		return fmt.Errorf("open import dump: %w", err)
	}
	defer dump.Close()
	if err := m.handler.Import(ctx, job, dump); err != nil {
		return err
	}
	return m.store.complete(ctx, job.ID, job.LeaseToken, job.ArchiveKey, m.retention)
}

// Cleanup deletes expired dumps only. Completed and failed job records remain.
func (m *Manager) Cleanup(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = DefaultCleanupLimit
	}
	jobs, err := m.store.claimExpiredArchives(ctx, limit)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, job := range jobs {
		if err := m.archive.Delete(ctx, job.ArchiveKey); err != nil {
			_ = m.store.releaseArchiveClaim(ctx, job.ID, job.ArchiveKey, job.LeaseToken)
			return deleted, err
		}
		if err := m.store.clearArchive(ctx, job.ID, job.ArchiveKey, job.LeaseToken); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func (m *Manager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cleanupPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, _ = m.Cleanup(ctx, DefaultCleanupLimit)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func validateIdentity(service, workspaceID string) error {
	if strings.TrimSpace(service) == "" {
		return errors.New("importexport jobs: service is required")
	}
	if err := services.ValidateWorkspaceID(workspaceID); err != nil {
		return err
	}
	return nil
}
func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
