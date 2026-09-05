# Воспроизведения аудита Reference — 2026-09-05

Исторические пробы до исправлений. На исправленном коде проверки дефектов должны перестать проходить; актуальные regression tests находятся в reference_test.go и service/admin/archive_test.go. Старые сигнатуры отдельных repository methods могли измениться.

Это диагностические пробы текущих дефектов, **не регрессионные тесты желаемого поведения**. После исправлений их проверки необходимо инвертировать и перенести в существующие test-файлы.

Пробы используют существующий helper `newReferenceTestServiceWithOptions`: он пересоздаёт только указанные тестовые базы `elum_reference_audit_probe` и `elum_reference_audit_cache`. Запускать только на изолированной локальной PostgreSQL с тестовой конфигурацией репозитория. Credential здесь не дублируется.

Для повторения сохраните два Go-блока по указанным путям, выполните команду, затем удалите только эти временные файлы:

```sh
GOCACHE=/private/tmp/cpa-audit-gocache go test ./reference ./reference/service/admin -run TestReferenceAudit -count=1 -v
```

Проба duplicates показывает ошибку прямого repository API; публичный User.Resolve защищён дедупликацией, поэтому она **исключена из публичных находок**. Экспорт проверяется детерминированным чередованием тех же двух repository-вызовов, которые использует ZIP handler; это не инструментированный end-to-end race всего worker.

## reference/audit_probe_test.go

```go
package reference

import (
	"context"
	"errors"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	resourcestorage "github.com/elum2b/services/reference/storage"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	resourceservice "github.com/elum2b/services/reference/service/resource"
)

// Audit probes assert the observed vulnerable behavior, not the desired contract.
func TestReferenceAuditProbes(t *testing.T) {
	s := newReferenceTestServiceWithOptions(t, "elum_reference_audit_probe", Options{})
	ctx := context.Background()
	ws := testsupport.WorkspaceID("reference-audit")
	repo := repository.New(s.client)
	defer repo.Close()
	pkg := repository.ExportPackage{Format: repository.ExportFormat, Service: "reference", Items: []repository.ExportItem{{Key: "item", Type: "quantity", Payload: []byte(`{}`), IsActive: true}}}
	if _, err := repo.Import(ctx, ws, repository.ImportRequest{Package: pkg}); err != nil {
		t.Fatal(err)
	}
	res := repository.Resource{WorkspaceID: ws, Key: "media", Type: "image", Payload: []byte(`{}`), IsActive: true, Format: "png", ContentType: "image/png", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", MediaVersion: "abcdefgh", Size: 1, Width: 1, Height: 1}
	if err := repo.CreateResource(ctx, res); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachResource(ctx, ws, "item", "media", 0); err != nil {
		t.Fatal(err)
	}
	t.Run("archive_snapshot_can_reference_missing_item", func(t *testing.T) {
		exported, err := repo.Export(ctx, ws, repository.ExportRequest{OnlyNotDeleted: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateItem(ctx, repository.SaveItemParams{WorkspaceID: ws, Key: "late", Type: "quantity", Payload: []byte(`{}`), IsActive: true}); err != nil {
			t.Fatal(err)
		}
		if err := repo.AttachResource(ctx, ws, "late", "media", 0); err != nil {
			t.Fatal(err)
		}
		_, links, err := repo.ExportArchiveData(ctx, ws)
		if err != nil {
			t.Fatal(err)
		}
		keys := map[string]bool{}
		for _, item := range exported.Items {
			keys[item.Key] = true
		}
		missing := false
		for _, link := range links {
			if !keys[link.ItemKey] {
				missing = true
			}
		}
		if !missing {
			t.Fatal("probe did not reproduce mixed snapshot")
		}
		if _, err := repo.DetachResource(ctx, ws, "late", "media"); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.SoftDeleteItem(ctx, ws, "late"); err != nil {
			t.Fatal(err)
		}
		t.Log("CONFIRMED: export stages produce a link to an item absent from package")
	})
	t.Run("resolve_duplicates_lose_resources", func(t *testing.T) {
		items, err := repo.Resolve(ctx, ws, []string{"item", "item"}, "en")
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || len(items[0].Resources) != 0 || len(items[1].Resources) != 0 {
			t.Fatalf("unexpected result: %#v", items)
		}
		single, err := repo.Resolve(ctx, ws, []string{"item"}, "en")
		if err != nil || len(single) != 1 || len(single[0].Resources) != 1 {
			t.Fatalf("single key control failed: %#v %v", single, err)
		}
		t.Log("CONFIRMED: duplicate keys lose resources while single key has one")
	})
	t.Run("empty_links_update_retains_old_attachment", func(t *testing.T) {
		_, err := repo.ImportArchiveWithMedia(ctx, ws, repository.ImportRequest{Package: pkg, ConflictStrategy: repository.ImportConflictUpdate}, nil, nil, 5*time.Second, func(context.Context, []*repository.Resource) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err = s.client.DB().QueryRowContext(ctx, `SELECT count(*) FROM reference_item_resource WHERE workspace_id=$1`, ws).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected reproduction with stale link, got %d", count)
		}
		t.Log("CONFIRMED: empty imported links leave old attachment")
	})
	t.Run("skip_existing_changes_link_position", func(t *testing.T) {
		_, err := repo.ImportArchiveWithMedia(ctx, ws, repository.ImportRequest{Package: pkg, ConflictStrategy: repository.ImportConflictSkip}, nil, []repository.ExportResourceLink{{ItemKey: "item", ResourceKey: "media", Position: 5}}, 5*time.Second, func(context.Context, []*repository.Resource) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		var position int
		if err = s.client.DB().QueryRowContext(ctx, `SELECT position FROM reference_item_resource WHERE workspace_id=$1`, ws).Scan(&position); err != nil {
			t.Fatal(err)
		}
		if position != 5 {
			t.Fatalf("expected mutated skipped link, got %d", position)
		}
		t.Log("CONFIRMED: skipped item attachment changed to position 5")
	})
	t.Run("gc_retired_version_detaches_active_resource", func(t *testing.T) {
		res.MediaVersion = "ijklmnop"
		if _, err := repo.UpdateResource(ctx, res); err != nil {
			t.Fatal(err)
		}
		if _, err := s.client.DB().ExecContext(ctx, `UPDATE reference_resource_media_version SET retired_at=now()-interval '2 hours' WHERE workspace_id=$1 AND media_version='abcdefgh'`, ws); err != nil {
			t.Fatal(err)
		}
		count, err := s.Resource.CollectGarbage(ctx, resourceservice.CollectGarbageParams{Limit: 100})
		if err != nil || count != 1 {
			t.Fatalf("gc: %d %v", count, err)
		}
		var links, active int
		if err := s.client.DB().QueryRowContext(ctx, `SELECT count(*) FROM reference_item_resource WHERE workspace_id=$1`, ws).Scan(&links); err != nil {
			t.Fatal(err)
		}
		if err := s.client.DB().QueryRowContext(ctx, `SELECT count(*) FROM reference_resource WHERE workspace_id=$1 AND deleted_at IS NULL AND media_version='ijklmnop'`, ws).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if links != 0 || active != 1 {
			t.Fatalf("expected live resource losing links: links=%d active=%d", links, active)
		}
		t.Log("CONFIRMED: GC of retired version deletes attachment of still-active current resource")
	})
	t.Run("transaction_timeout_does_not_cancel_query", func(t *testing.T) {
		_, err := s.client.DB().ExecContext(ctx, `CREATE FUNCTION reference_audit_delay() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.5); RETURN NEW; END $$`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.client.DB().ExecContext(ctx, `CREATE TRIGGER reference_audit_delay BEFORE INSERT ON reference_item FOR EACH ROW EXECUTE FUNCTION reference_audit_delay()`)
		if err != nil {
			t.Fatal(err)
		}
		defer s.client.DB().ExecContext(ctx, `DROP TRIGGER reference_audit_delay ON reference_item`)
		timed := repository.NewWithOptions(s.client, repository.Options{QueryTimeout: 50 * time.Millisecond})
		defer timed.Close()
		start := time.Now()
		err = timed.CreateItem(ctx, repository.SaveItemParams{WorkspaceID: ws, Key: "slow", Type: "quantity", Payload: []byte(`{}`), IsActive: true})
		elapsed := time.Since(start)
		if err == nil || elapsed < 400*time.Millisecond {
			t.Fatalf("expected timeout only after SQL completes: elapsed=%s err=%v", elapsed, err)
		}
		var count int
		if e := s.client.DB().QueryRowContext(ctx, `SELECT count(*) FROM reference_item WHERE workspace_id=$1 AND key='slow'`, ws).Scan(&count); e != nil {
			t.Fatal(e)
		}
		if count != 0 {
			t.Fatal("unexpected committed item")
		}
		t.Logf("CONFIRMED: 50ms timeout returns after %s; transaction rolls back", elapsed)
	})
	t.Run("queue_after_close", func(t *testing.T) {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		job, err := s.Admin.QueueArchiveExport(ctx, admin.QueueArchiveExportParams{WorkspaceID: ws, FileName: "closed.zip"})
		if err != nil {
			t.Fatalf("probe did not reproduce: %v", err)
		}
		if job.ID == 0 {
			t.Fatal("missing job")
		}
		t.Logf("CONFIRMED: closed service accepted job %d", job.ID)
	})
}

func TestReferenceAuditS3HTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	archive, err := newS3Archive(resourcestorage.Config{Endpoint: server.URL, Bucket: "audit", AccessKey: "fixture", SecretKey: "fixture", UsePathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = archive.Store(t.Context(), jobs.ArchiveObject{}, strings.NewReader("fixture zip bytes"))
	t.Logf("S3 HTTP result: requests=%d err=%v", requests.Load(), err)
	if err == nil {
		t.Log("S3 streaming failure hypothesis not reproduced")
	}
}

type auditFailCache struct {
	*referenceSharedCache
	fail atomic.Bool
}

func (c *auditFailCache) Set(key string, value []byte, expiration time.Duration) error {
	if c.fail.Load() {
		return errors.New("audit cache offline")
	}
	return c.referenceSharedCache.Set(key, value, expiration)
}
func TestReferenceAuditCommittedWriteReturnsCacheError(t *testing.T) {
	cache := &auditFailCache{referenceSharedCache: newReferenceSharedCache()}
	s := newReferenceTestServiceWithOptions(t, "elum_reference_audit_cache", Options{Cache: cache, CacheEnabled: true, CacheL1Delay: time.Minute, CacheL2Delay: time.Minute})
	cache.fail.Store(true)
	ws := testsupport.WorkspaceID("audit-cache")
	err := s.Admin.CreateItem(t.Context(), admin.SaveItemParams{WorkspaceID: ws, Key: "item", Type: "quantity", Payload: []byte(`{}`), IsActive: true})
	var count int
	if e := s.client.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM reference_item WHERE workspace_id=$1`, ws).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if err == nil || count != 1 {
		t.Fatalf("probe did not reproduce: count=%d err=%v", count, err)
	}
	t.Logf("CONFIRMED: committed row count=%d but API error=%v", count, err)
}
```

## reference/service/admin/audit_probe_test.go

```go
package admin

import (
	"context"
	"testing"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/storage"
)

type auditCleanupStore struct {
	storage.Store
	deleted int
}

func (s *auditCleanupStore) DeleteVersion(context.Context, string, string, string) error {
	s.deleted++
	return nil
}

func TestReferenceAuditCleanupUnknownDurability(t *testing.T) {
	client := sqlwrap.NewUnavailable()
	defer client.Close()
	repo := repository.New(client)
	store := &auditCleanupStore{}
	a := &Admin{repository: repo, store: store}
	a.cleanupArchiveVersions("00000000-0000-0000-0000-000000000001", []*repository.Resource{{Key: "media", MediaVersion: "abcdefgh"}})
	if store.deleted != 1 {
		t.Fatalf("probe did not reproduce: deletes=%d", store.deleted)
	}
	t.Log("CONFIRMED: failed durability read still authorizes DeleteVersion")
}
```

