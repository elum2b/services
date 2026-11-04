package collections

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elum2b/services/collections/model"
	"github.com/elum2b/services/collections/repository"
	"github.com/elum2b/services/collections/service/admin"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/lib/pq"
)

func testDSN() string {
	if dsn := os.Getenv("COLLECTIONS_TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	dsn, err := sqlwrap.PostgresDSN(sqlwrap.PostgresParams{
		Host:     "localhost",
		Port:     5432,
		User:     "postgres",
		Password: "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU",
		Database: "postgres",
	})
	if err != nil {
		panic(err)
	}
	return dsn
}

type testEnv struct {
	s    *Collections
	db   *sql.DB
	w    string
	i    services.Identity
	opts Options
}

func setup(t testing.TB, opts Options) *testEnv {
	t.Helper()
	ctx := context.Background()
	dsn := testDSN()
	base, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "collections_test_" + uuid.NewString()[:8]
	if _, err := base.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		base.Close()
		t.Fatalf("create isolated test database: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatal(err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if opts.QueryTimeout <= 0 {
		opts.QueryTimeout = 5 * time.Second
	}
	opts.ArchiveDirectory = t.TempDir()
	s, err := NewWithDatabase(ctx, db, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Close()
		client.Close()
		db.Close()
		_, _ = base.ExecContext(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		base.Close()
	})
	w := testsupport.WorkspaceID(name)
	return &testEnv{
		s:  s,
		db: db,
		w:  w,
		i: services.Identity{
			WorkspaceID:    w,
			AppID:          1,
			PlatformID:     2,
			PlatformUserID: "owner",
		},
		opts: opts,
	}
}
func (e *testEnv) seed(t testing.TB, keys ...string) {
	t.Helper()
	ctx := context.Background()
	if err := e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{
		WorkspaceID: e.w,
		ActorID:     "operator",
		Collection: model.Collection{
			Key:          "colors",
			Active:       true,
			Localization: map[string]model.Text{"ru": {Title: "Цвета"}},
		},
	}); err != nil {
		t.Fatal(err)
	}
	for n, key := range keys {
		if err := e.s.Admin.AddEntry(ctx, model.EntryParams{
			WorkspaceID:   e.w,
			ActorID:       "operator",
			CollectionKey: "colors",
			Entry: model.Entry{
				ItemKey:  key,
				Position: int32(n),
				Active:   true,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
}
func (e *testEnv) set(id, key string, owned bool) model.SetOwnedParams {
	return model.SetOwnedParams{
		Identity:    e.i,
		Source:      "game",
		OperationID: id,
		OwnershipChange: model.OwnershipChange{
			ItemRef: model.ItemRef{
				CollectionKey: "colors",
				ItemKey:       key,
			},
			Owned: owned,
		},
	}
}
func (e *testEnv) has(t testing.TB, key string) model.ItemState {
	t.Helper()
	s, err := e.s.User.HasItem(context.Background(), model.HasItemParams{
		ReadParams: model.ReadParams{Identity: e.i},
		ItemRef: model.ItemRef{
			CollectionKey: "colors",
			ItemKey:       key,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestOwnershipLifecycleAndVisibility(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	if s := e.has(t, "red"); s.Owned || s.Revision != 0 {
		t.Fatal(s)
	}
	p := e.set("grant", "red", true)
	receipt, err := e.s.Internal.SetOwned(ctx, p)
	must(t, err)
	if len(receipt.Results) != 1 || !receipt.Results[0].Changed || receipt.Results[0].Revision != 1 {
		t.Fatal(receipt)
	}
	replay, err := e.s.Internal.SetOwned(ctx, p)
	must(t, err)
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("receipt changed")
	}
	p.Owned = false
	_, err = e.s.Internal.SetOwned(ctx, p)
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("payload reuse = %v", err)
	}
	noop, err := e.s.Internal.SetOwned(ctx, e.set("noop", "red", true))
	must(t, err)
	if noop.Results[0].Changed || noop.Results[0].Revision != 1 {
		t.Fatal(noop)
	}
	remove := model.EntryParams{
		WorkspaceID:   e.w,
		ActorID:       "operator",
		CollectionKey: "colors",
		Entry: model.Entry{
			ItemKey: "red",
			Active:  true,
		},
	}
	must(t, e.s.Admin.RemoveEntry(ctx, remove))
	if s := e.has(t, "red"); s.Owned || s.Revision != 1 {
		t.Fatal(s)
	}
	replay, err = e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("hidden replay changed")
	}
	_, err = e.s.Internal.SetOwned(ctx, e.set("hidden-grant", "red", true))
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	must(t, e.s.Admin.AddEntry(ctx, remove))
	if s := e.has(t, "red"); !s.Owned || s.Revision != 1 {
		t.Fatal(s)
	}
	must(t, e.s.Admin.ArchiveCollection(ctx, model.ArchiveCollectionParams{
		WorkspaceID: e.w,
		ActorID:     "operator",
		Key:         "colors",
	}))
	if e.has(t, "red").Owned {
		t.Fatal("archived right is active")
	}
	revoke, err := e.s.Internal.SetOwned(ctx, e.set("late-revoke", "red", false))
	must(t, err)
	if revoke.Results[0].Revision != 2 {
		t.Fatal(revoke)
	}
	must(t, e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{
		WorkspaceID: e.w,
		ActorID:     "operator",
		Collection: model.Collection{
			Key:    "colors",
			Active: true,
		},
	}))
	if s := e.has(t, "red"); s.Owned || s.Revision != 2 {
		t.Fatal(s)
	}
	stats, err := e.s.Admin.GetStats(ctx, model.StatsParams{WorkspaceID: e.w})
	must(t, err)
	if stats.Grants != 1 || stats.Revokes != 1 || stats.StoredOwned != 0 {
		t.Fatal(stats)
	}
	events, err := e.s.Admin.ListCallbackEvents(ctx, model.CallbackParams{WorkspaceID: e.w})
	must(t, err)
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	journal, err := e.s.Admin.ListJournal(ctx, model.JournalParams{WorkspaceID: e.w})
	must(t, err)
	if len(journal) != 3 {
		t.Fatalf("journal = %d", len(journal))
	}
	now := time.Now().UTC()
	must(t, e.s.Admin.RefreshDailyStats(ctx, model.DailyParams{
		WorkspaceID: e.w,
		From:        now,
		Until:       now,
	}))
	daily, err := e.s.Admin.ListDailyStats(ctx, model.DailyParams{
		WorkspaceID: e.w,
		From:        now.Add(-24 * time.Hour),
		Until:       now.Add(24 * time.Hour),
	})
	must(t, err)
	if len(daily) != 1 || daily[0].Grants != 1 || daily[0].Revokes != 1 {
		t.Fatal(daily)
	}
}
func TestAtomicBatchAndRevision(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	batch := model.SetOwnedBatchParams{
		Identity:    e.i,
		Source:      "game",
		OperationID: "batch",
		Changes:     []model.OwnershipChange{e.set("", "red", true).OwnershipChange, e.set("", "missing", true).OwnershipChange},
	}
	_, err := e.s.Internal.SetOwnedBatch(ctx, batch)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	if e.has(t, "red").Owned {
		t.Fatal("partial batch")
	}
	batch.Changes[1].ItemKey = "blue"
	receipt, err := e.s.Internal.SetOwnedBatch(ctx, batch)
	must(t, err)
	if len(receipt.Results) != 2 {
		t.Fatal(receipt)
	}
	replay, err := e.s.Internal.SetOwnedBatch(ctx, batch)
	must(t, err)
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("batch replay")
	}
	stale := int64(0)
	p := e.set("stale", "red", false)
	p.ExpectedRevision = &stale
	_, err = e.s.Internal.SetOwned(ctx, p)
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	batch.OperationID = "duplicates"
	batch.Changes[1] = batch.Changes[0]
	_, err = e.s.Internal.SetOwnedBatch(ctx, batch)
	if !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	corrected, err := e.s.Admin.CorrectOwned(ctx, model.CorrectOwnedParams{
		ActorID: "operator",
		Reason:  "correction",
		SetOwnedBatchParams: model.SetOwnedBatchParams{
			Identity:    e.i,
			OperationID: "correct",
			Changes:     []model.OwnershipChange{e.set("", "red", false).OwnershipChange},
		},
	})
	must(t, err)
	if corrected.Source != "admin" {
		t.Fatal(corrected)
	}
	p = e.set("spoof", "red", true)
	p.Source = "admin"
	_, err = e.s.Internal.SetOwned(ctx, p)
	if !errors.Is(err, repository.ErrForbidden) {
		t.Fatal(err)
	}
}
func TestConcurrentOwnership(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	const n = 12
	errs := make(chan error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := e.s.Internal.SetOwned(ctx, e.set("same", "red", true))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	if s := e.has(t, "red"); !s.Owned || s.Revision != 1 {
		t.Fatal(s)
	}
	// Two distinct writes with the same expected revision: exactly one commits.
	outcomes := make(chan error, 2)
	rev := int64(1)
	for j := range 2 {
		go func() {
			p := e.set(fmt.Sprintf("cas-%d", j), "red", false)
			p.ExpectedRevision = &rev
			_, err := e.s.Internal.SetOwned(ctx, p)
			outcomes <- err
		}()
	}
	success, conflict := 0, 0
	for range 2 {
		err := <-outcomes
		if err == nil {
			success++
		} else if errors.Is(err, repository.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
	stats, err := e.s.Admin.GetStats(ctx, model.StatsParams{WorkspaceID: e.w})
	must(t, err)
	if stats.Grants != 1 || stats.Revokes != 1 {
		t.Fatal(stats)
	}
}
func TestUserReadsScopeAndAdminBoundary(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	_, err := e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	read := model.ReadParams{
		Identity: e.i,
		Locale:   "ru",
	}
	list, err := e.s.User.ListCollections(ctx, read)
	must(t, err)
	if len(list) != 1 || list[0].Text.Title != "Цвета" {
		t.Fatal(list)
	}
	view, err := e.s.User.GetCollection(ctx, model.GetCollectionParams{
		ReadParams:    read,
		CollectionKey: "colors",
	})
	must(t, err)
	if len(view.Entries) != 2 || view.Entries[0].ItemKey != "red" || view.Entries[1].ItemKey != "blue" {
		t.Fatal(view)
	}
	owned, err := e.s.User.ListOwned(ctx, model.ListOwnedParams{ReadParams: read})
	must(t, err)
	if len(owned) != 1 {
		t.Fatal(owned)
	}
	other := e.i
	other.PlatformUserID = "visitor"
	read.Identity = other
	read.Subject = &e.i
	view, err = e.s.User.GetCollection(ctx, model.GetCollectionParams{
		ReadParams:    read,
		CollectionKey: "colors",
	})
	must(t, err)
	if len(view.Entries) != 2 {
		t.Fatal("catalog depends on ownership")
	}
	owned, err = e.s.User.ListOwned(ctx, model.ListOwnedParams{ReadParams: read, CollectionKeys: []string{"colors"}})
	must(t, err)
	if len(owned) != 1 || owned[0].ItemKey != "red" {
		t.Fatal("target not used")
	}
	wrong := e.i
	wrong.AppID++
	read.Subject = &wrong
	_, err = e.s.User.ListOwned(ctx, model.ListOwnedParams{ReadParams: read})
	if !errors.Is(err, repository.ErrForbidden) {
		t.Fatal(err)
	}
	for _, change := range []func(*services.Identity){func(i *services.Identity) { i.WorkspaceID = testsupport.WorkspaceID("other") }, func(i *services.Identity) { i.AppID++ }, func(i *services.Identity) { i.PlatformID++ }, func(i *services.Identity) { i.PlatformUserID = "other" }} {
		i := e.i
		change(&i)
		s, err := e.s.User.HasItem(ctx, model.HasItemParams{
			ReadParams: model.ReadParams{Identity: i},
			ItemRef: model.ItemRef{
				CollectionKey: "colors",
				ItemKey:       "red",
			},
		})
		must(t, err)
		if s.Owned {
			t.Fatal("scope leak")
		}
	}
	must(t, e.s.Admin.ReorderEntries(ctx, model.ReorderEntriesParams{
		WorkspaceID:   e.w,
		ActorID:       "operator",
		CollectionKey: "colors",
		ItemKeys:      []string{"blue", "red"},
	}))
	catalog, err := e.s.Admin.GetCollection(ctx, model.CatalogParams{
		WorkspaceID: e.w,
		Key:         "colors",
	})
	must(t, err)
	if catalog.Entries[0].ItemKey != "blue" {
		t.Fatal(catalog)
	}
	for _, name := range []string{"SetOwned", "SetOwnedBatch", "CreateCollection", "UpdateCollection", "CorrectOwned", "Import", "ListJournal"} {
		if _, ok := reflect.TypeOf(e.s.User).MethodByName(name); ok {
			t.Fatalf("User exposes %s", name)
		}
	}
	for _, name := range []string{"SetOwned", "SetOwnedBatch"} {
		if _, ok := reflect.TypeOf(e.s.Admin).MethodByName(name); ok {
			t.Fatalf("Admin exposes trusted %s", name)
		}
	}
}
func TestImportMergeRetainsOwnership(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	_, err := e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	exported, err := e.s.Admin.Export(ctx, admin.ExportParams{WorkspaceID: e.w})
	must(t, err)
	pkg := exported
	pkg.Collections[0].Entries = nil
	p := admin.ImportParams{
		WorkspaceID: e.w,
		Request:     model.ImportRequest{ActorID: "operator", Package: pkg},
	}
	preview, err := e.s.Admin.PreviewImport(ctx, p)
	must(t, err)
	if len(preview.Conflicts) != 1 {
		t.Fatal(preview)
	}
	p.Request.ConflictStrategy = model.ConflictFail
	_, err = e.s.Admin.Import(ctx, p)
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	p.Request.ConflictStrategy = model.ConflictSkip
	out, err := e.s.Admin.Import(ctx, p)
	must(t, err)
	if out.Skipped != 1 || !e.has(t, "red").Owned {
		t.Fatal(out)
	}
	p.Request.ConflictStrategy = ""
	_, err = e.s.Admin.Import(ctx, p)
	must(t, err)
	if s := e.has(t, "red"); s.Owned || s.Revision != 1 {
		t.Fatal(s)
	}
	p.Request.Package = exported
	p.Request.Package.Collections[0].Entries = []model.Entry{{
		ItemKey: "red",
		Active:  true,
	}}
	_, err = e.s.Admin.Import(ctx, p)
	must(t, err)
	if !e.has(t, "red").Owned {
		t.Fatal("ownership lost on re-add")
	}
	for _, key := range []string{"A", "B", "C"} {
		must(t, e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{
			WorkspaceID: e.w,
			ActorID:     "operator",
			Collection: model.Collection{
				Key:    key,
				Active: true,
			},
		}))
	}
	pkg = model.ExportPackage{
		Format:  model.ExportFormat,
		Service: "collections",
	}
	for _, key := range []string{"B", "C", "D"} {
		pkg.Collections = append(pkg.Collections, model.Collection{
			Key:    key,
			Active: false,
		})
	}
	_, err = e.s.Admin.Import(ctx, admin.ImportParams{
		WorkspaceID: e.w,
		Request:     model.ImportRequest{ActorID: "operator", Package: pkg},
	})
	must(t, err)
	rows, err := e.s.Admin.ListCatalog(ctx, model.CatalogParams{WorkspaceID: e.w})
	must(t, err)
	if len(rows) != 5 {
		t.Fatal(rows)
	}
	// More than the PostgreSQL parameter limit: written in bounded bulk batches.
	huge := model.Collection{
		Key:    "large",
		Active: true,
	}
	for n := range 11000 {
		huge.Entries = append(huge.Entries, model.Entry{
			ItemKey: fmt.Sprint(n),
			Active:  true,
		})
	}
	_, err = e.s.Admin.Import(ctx, admin.ImportParams{
		WorkspaceID: e.w,
		Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{
			Format:      model.ExportFormat,
			Service:     "collections",
			Collections: []model.Collection{huge},
		}},
	})
	must(t, err)
	huge.Entries = append(huge.Entries, huge.Entries[0])
	_, err = e.s.Admin.Import(ctx, admin.ImportParams{
		WorkspaceID: e.w,
		Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{
			Format:      model.ExportFormat,
			Service:     "collections",
			Collections: []model.Collection{huge},
		}},
	})
	if !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestCallbackDeliveryAndAdminStore(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	receipt, err := e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	events, err := e.s.Admin.ListCallbackEvents(ctx, model.CallbackParams{WorkspaceID: e.w})
	must(t, err)
	if len(events) != 1 {
		t.Fatal(events)
	}
	id := events[0].ID
	p := model.CallbackParams{
		WorkspaceID: e.w,
		ID:          id,
	}
	event, err := e.s.Admin.GetCallbackEvent(ctx, p)
	must(t, err)
	var payload model.ChangeEvent
	must(t, json.Unmarshal(event.Payload, &payload))
	if payload.ReceiptID != receipt.ID || !payload.Owned {
		t.Fatal(payload)
	}
	_, err = e.s.Admin.RetryCallbackEventNow(ctx, p)
	must(t, err)
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	received := make(chan model.ChangeEvent, 1)
	done := make(chan error, 1)
	go func() {
		done <- e.s.OnCallback(runCtx, func(c Context) error { received <- *c.Ownership; err := c.Successful(); cancel(); return err }, WithCallbackIdleDelay(5*time.Millisecond))
	}()
	select {
	case got := <-received:
		if got.ReceiptID != receipt.ID {
			t.Fatal(got)
		}
	case <-runCtx.Done():
		t.Fatal("no delivery")
	}
	<-done
	_, err = e.s.Internal.SetOwned(ctx, e.set("revoke", "red", false))
	must(t, err)
	pending, err := e.s.Admin.ListCallbackEvents(ctx, model.CallbackParams{
		WorkspaceID: e.w,
		Status:      "pending",
	})
	must(t, err)
	if len(pending) != 1 {
		t.Fatal("missing revoke event")
	}
	_, err = e.s.Admin.MarkCallbackEventReject(ctx, model.CallbackParams{
		WorkspaceID: e.w,
		ID:          pending[0].ID,
		Reason:      "test",
	})
	must(t, err)
	changed, err := e.s.Admin.RetryCallbackEventNow(ctx, model.CallbackParams{
		WorkspaceID: e.w,
		ID:          pending[0].ID,
	})
	must(t, err)
	if changed != 0 {
		t.Fatal("terminal rejection revived")
	}
	_, err = e.s.Admin.MarkCallbackEventOK(ctx, p)
	must(t, err)
	_, err = e.s.Admin.ResetExpiredCallbackProcessing(ctx, p)
	must(t, err)
	_, err = e.s.Admin.GetCallbackEvent(ctx, model.CallbackParams{
		WorkspaceID: testsupport.WorkspaceID("other"),
		ID:          id,
	})
	if err == nil {
		t.Fatal("callback scope leak")
	}
}
func TestArchiveJobsRoundTrip(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !e.s.Admin.StartArchiveJobs(ctx) {
		t.Fatal("worker not started")
	}
	queued, err := e.s.Admin.QueueArchiveExport(ctx, admin.QueueArchiveExportParams{
		WorkspaceID: e.w,
		FileName:    "collections.zip",
	})
	must(t, err)
	wait := func(w string, id int64) admin.ArchiveJob {
		t.Helper()
		for {
			job, err := e.s.Admin.ArchiveJob(ctx, admin.ArchiveStatusParams{
				WorkspaceID: w,
				ID:          id,
			})
			must(t, err)
			if job.Status == "completed" {
				return job
			}
			if job.Status == "failed" {
				t.Fatal(job.Error)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	wait(e.w, queued.ID)
	stream, _, err := e.s.Admin.DownloadArchive(ctx, admin.ArchiveStatusParams{
		WorkspaceID: e.w,
		ID:          queued.ID,
	})
	must(t, err)
	defer stream.Close()
	target := testsupport.WorkspaceID("archive-target")
	imported, err := e.s.Admin.QueueArchiveImport(ctx, admin.QueueArchiveImportParams{ImportRequest: model.ImportRequest{ActorID: "operator"},
		WorkspaceID: target,
		FileName:    "collections.zip",
		Archive:     stream,
	})
	must(t, err)
	wait(target, imported.ID)
	var actor string
	must(t, e.db.QueryRow("SELECT actor_id FROM collections_catalog_audit WHERE workspace_id=$1 AND method='import_catalog'", target).Scan(&actor))
	if actor != "operator" {
		t.Fatal("async import lost operator attribution")
	}

	catalog, err := e.s.Admin.GetCollection(ctx, model.CatalogParams{
		WorkspaceID: target,
		Key:         "colors",
	})
	must(t, err)
	if len(catalog.Entries) != 1 {
		t.Fatal(catalog)
	}
	history, err := e.s.Admin.ArchiveHistory(ctx, admin.ArchiveHistoryParams{WorkspaceID: e.w})
	must(t, err)
	if len(history) == 0 {
		t.Fatal("no archive history")
	}
	_, err = e.s.Admin.ArchiveJobHistory(ctx, admin.ArchiveHistoryParams{
		WorkspaceID: e.w,
		ID:          queued.ID,
	})
	must(t, err)
	_, _, err = e.s.Admin.DownloadArchive(ctx, admin.ArchiveStatusParams{
		WorkspaceID: target,
		ID:          imported.ID,
	})
	if err == nil {
		t.Fatal("import downloadable")
	}
	_, err = e.s.Admin.ArchiveJob(ctx, admin.ArchiveStatusParams{
		WorkspaceID: target,
		ID:          queued.ID,
	})
	if err == nil {
		t.Fatal("archive scope leak")
	}
}

type sharedTestCache struct {
	mu      sync.Mutex
	values  map[string][]byte
	expires map[string]time.Time
	fail    bool
}

func (c *sharedTestCache) GetWithTTL(k string) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return nil, 0, errors.New("cache offline")
	}
	ttl := time.Until(c.expires[k])
	if ttl <= 0 {
		return nil, 0, nil
	}
	return append([]byte(nil), c.values[k]...), ttl, nil
}
func (c *sharedTestCache) Set(k string, v []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("cache offline")
	}
	c.values[k] = append([]byte(nil), v...)
	c.expires[k] = time.Now().Add(ttl)
	return nil
}
func (c *sharedTestCache) Delete(k string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.values, k)
	return nil
}
func (c *sharedTestCache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = map[string][]byte{}
	c.expires = map[string]time.Time{}
	return nil
}
func (c *sharedTestCache) Close() error { return nil }
func TestTwoNodesAndCommittedCacheFailure(t *testing.T) {
	cache := &sharedTestCache{
		values:  map[string][]byte{},
		expires: map[string]time.Time{},
	}
	opts := Options{
		CacheEnabled:             true,
		Cache:                    cache,
		Mutex:                    sqlwrap.NewMutex(),
		CacheL1Delay:             time.Minute,
		CacheL2Delay:             time.Minute,
		OnCacheInvalidationError: func(error) { panic("diagnostic callback") },
	}
	e := setup(t, opts)
	e.seed(t, "red")
	ctx := context.Background()
	node, err := NewWithDatabase(ctx, e.db, e.opts)
	must(t, err)
	defer node.Close()
	read := model.CatalogParams{WorkspaceID: e.w}
	before, err := node.Admin.ListCatalog(ctx, read)
	must(t, err)
	if len(before) != 1 || !before[0].Active {
		t.Fatal(before)
	}
	must(t, e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: model.Collection{Key: "colors", Active: true, Position: 42}}))
	after, err := node.Admin.ListCatalog(ctx, read)
	must(t, err)
	if len(after) != 1 || after[0].Position != 42 {
		t.Fatal("second node catalog stale")
	}
	_, err = e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	owned, err := node.User.HasItem(ctx, model.HasItemParams{
		ReadParams: model.ReadParams{Identity: e.i},
		ItemRef: model.ItemRef{
			CollectionKey: "colors",
			ItemKey:       "red",
		},
	})
	must(t, err)
	if !owned.Owned {
		t.Fatal("grant stale")
	}
	must(t, e.s.Admin.RemoveEntry(ctx, model.EntryParams{
		WorkspaceID:   e.w,
		ActorID:       "operator",
		CollectionKey: "colors",
		Entry:         model.Entry{ItemKey: "red"},
	}))
	owned, err = node.User.HasItem(ctx, model.HasItemParams{
		ReadParams: model.ReadParams{Identity: e.i},
		ItemRef: model.ItemRef{
			CollectionKey: "colors",
			ItemKey:       "red",
		},
	})
	must(t, err)
	if owned.Owned {
		t.Fatal("authorization uses stale catalog")
	}
	cache.mu.Lock()
	cache.fail = true
	cache.mu.Unlock()
	must(t, e.s.Admin.AddEntry(ctx, model.EntryParams{
		WorkspaceID:   e.w,
		ActorID:       "operator",
		CollectionKey: "colors",
		Entry: model.Entry{
			ItemKey: "green",
			Active:  true,
		},
	}))
	// Inspect committed state directly; cache outage must not change write result.
	var count int
	must(t, e.db.QueryRow("SELECT count(*) FROM collections_entry WHERE workspace_id=$1 AND item_key='green'", e.w).Scan(&count))
	if count != 1 {
		t.Fatal("commit lost")
	}
}
func TestCatalogLockAndDeadline(t *testing.T) {
	e := setup(t, Options{QueryTimeout: 100 * time.Millisecond})
	e.seed(t, "red")
	ctx := context.Background()
	tx, err := e.db.Begin()
	must(t, err)
	defer tx.Rollback()
	_, err = tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('collections:catalog:' || $1::text,0))", e.w)
	must(t, err)
	_, err = e.s.Internal.SetOwned(ctx, e.set("blocked", "red", true))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write = %v", err)
	}
	must(t, tx.Rollback())
	if e.has(t, "red").Owned {
		t.Fatal("canceled write persisted")
	}
	journal, err := e.s.Admin.ListJournal(ctx, model.JournalParams{WorkspaceID: e.w})
	must(t, err)
	if len(journal) != 0 {
		t.Fatal("canceled receipt persisted")
	}
}
func TestEmbeddingAndShellLifecycle(t *testing.T) {
	shell := New()
	if shell.IsReady() || shell.User == nil || shell.Admin == nil || shell.Internal == nil {
		t.Fatal("invalid shell")
	}
	_, err := shell.User.HasItem(context.Background(), model.HasItemParams{
		ReadParams: model.ReadParams{Identity: services.Identity{
			WorkspaceID:    testsupport.WorkspaceID("shell"),
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "user",
		}},
		ItemRef: model.ItemRef{
			CollectionKey: "c",
			ItemKey:       "i",
		},
	})
	if !errors.Is(err, sqlwrap.ErrServiceNotReady) {
		t.Fatal(err)
	}
	must(t, shell.Close())
	must(t, shell.Close())
	if err := shell.OnCallback(context.Background(), nil); err == nil {
		t.Fatal("nil callback accepted")
	}
	e := setup(t, Options{})
	e.seed(t, "red")
	must(t, e.s.Close())
	must(t, e.db.Ping())
	if e.s.IsReady() {
		t.Fatal("closed service ready")
	}
	_, err = e.s.Internal.SetOwned(context.Background(), e.set("after-close", "red", true))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestAccessManifest(t *testing.T) {
	keys := map[string]bool{}
	for _, a := range AccessManifest() {
		if a.Service != "collections" || a.TitleRU == "" || a.TitleEN == "" || a.DescriptionRU == "" || a.DescriptionEN == "" || keys[a.Key] {
			t.Fatal(a)
		}
		keys[a.Key] = true
	}
	if keys["collections.set_owned"] {
		t.Fatal("trusted method is an admin access")
	}
}

func TestLargeOwnershipBatch(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	c := model.Collection{
		Key:    "colors",
		Active: true,
	}
	changes := []model.OwnershipChange{}
	for n := range model.MaxBatch {
		key := fmt.Sprintf("item-%04d", n)
		c.Entries = append(c.Entries, model.Entry{
			ItemKey: key,
			Active:  true,
		})
		changes = append(changes, model.OwnershipChange{
			ItemRef: model.ItemRef{
				CollectionKey: "colors",
				ItemKey:       key,
			},
			Owned: true,
		})
	}
	_, err := e.s.Admin.Import(ctx, admin.ImportParams{
		WorkspaceID: e.w,
		Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{
			Format:      model.ExportFormat,
			Service:     "collections",
			Collections: []model.Collection{c},
		}},
	})
	must(t, err)
	receipt, err := e.s.Internal.SetOwnedBatch(ctx, model.SetOwnedBatchParams{
		Identity:    e.i,
		Source:      "game",
		OperationID: "large",
		Changes:     changes,
	})
	must(t, err)
	if len(receipt.Results) != model.MaxBatch {
		t.Fatal(len(receipt.Results))
	}
	states, err := e.s.User.HasItems(ctx, model.HasItemsParams{
		ReadParams: model.ReadParams{Identity: e.i},
		Items: func() []model.ItemRef {
			out := []model.ItemRef{}
			for _, c := range changes {
				out = append(out, c.ItemRef)
			}
			return out
		}(),
	})
	must(t, err)
	for n, state := range states {
		if !state.Owned || state.ItemKey != changes[n].ItemKey {
			t.Fatal("batch mapping mismatch")
		}
	}
	var count int
	must(t, e.db.QueryRow("SELECT count(*) FROM collections_clb_event WHERE workspace_id=$1", e.w).Scan(&count))
	if count != model.MaxBatch {
		t.Fatal(count)
	}
}
func TestInvalidPublicInputs(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	cases := map[string]func() error{
		"create":          func() error { return e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{}) },
		"update":          func() error { return e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{}) },
		"archive":         func() error { return e.s.Admin.ArchiveCollection(ctx, model.ArchiveCollectionParams{}) },
		"add":             func() error { return e.s.Admin.AddEntry(ctx, model.EntryParams{}) },
		"remove":          func() error { return e.s.Admin.RemoveEntry(ctx, model.EntryParams{}) },
		"reorder":         func() error { return e.s.Admin.ReorderEntries(ctx, model.ReorderEntriesParams{}) },
		"catalog":         func() error { _, err := e.s.Admin.ListCatalog(ctx, model.CatalogParams{}); return err },
		"get_catalog":     func() error { _, err := e.s.Admin.GetCollection(ctx, model.CatalogParams{}); return err },
		"correct":         func() error { _, err := e.s.Admin.CorrectOwned(ctx, model.CorrectOwnedParams{}); return err },
		"journal":         func() error { _, err := e.s.Admin.ListJournal(ctx, model.JournalParams{}); return err },
		"stats":           func() error { _, err := e.s.Admin.GetStats(ctx, model.StatsParams{}); return err },
		"refresh":         func() error { return e.s.Admin.RefreshDailyStats(ctx, model.DailyParams{}) },
		"daily":           func() error { _, err := e.s.Admin.ListDailyStats(ctx, model.DailyParams{}); return err },
		"has":             func() error { _, err := e.s.User.HasItem(ctx, model.HasItemParams{}); return err },
		"has_many":        func() error { _, err := e.s.User.HasItems(ctx, model.HasItemsParams{}); return err },
		"owned":           func() error { _, err := e.s.User.ListOwned(ctx, model.ListOwnedParams{}); return err },
		"collections":     func() error { _, err := e.s.User.ListCollections(ctx, model.ReadParams{}); return err },
		"collection":      func() error { _, err := e.s.User.GetCollection(ctx, model.GetCollectionParams{}); return err },
		"set":             func() error { _, err := e.s.Internal.SetOwned(ctx, model.SetOwnedParams{}); return err },
		"set_batch":       func() error { _, err := e.s.Internal.SetOwnedBatch(ctx, model.SetOwnedBatchParams{}); return err },
		"export":          func() error { _, err := e.s.Admin.Export(ctx, admin.ExportParams{}); return err },
		"preview":         func() error { _, err := e.s.Admin.PreviewImport(ctx, admin.ImportParams{}); return err },
		"import":          func() error { _, err := e.s.Admin.Import(ctx, admin.ImportParams{}); return err },
		"callback_list":   func() error { _, err := e.s.Admin.ListCallbackEvents(ctx, model.CallbackParams{}); return err },
		"callback_get":    func() error { _, err := e.s.Admin.GetCallbackEvent(ctx, model.CallbackParams{}); return err },
		"callback_retry":  func() error { _, err := e.s.Admin.RetryCallbackEventNow(ctx, model.CallbackParams{}); return err },
		"callback_ok":     func() error { _, err := e.s.Admin.MarkCallbackEventOK(ctx, model.CallbackParams{}); return err },
		"callback_reject": func() error { _, err := e.s.Admin.MarkCallbackEventReject(ctx, model.CallbackParams{}); return err },
		"callback_reset": func() error {
			_, err := e.s.Admin.ResetExpiredCallbackProcessing(ctx, model.CallbackParams{})
			return err
		},
		"archive_status":      func() error { _, err := e.s.Admin.ArchiveJob(ctx, admin.ArchiveStatusParams{}); return err },
		"archive_history":     func() error { _, err := e.s.Admin.ArchiveHistory(ctx, admin.ArchiveHistoryParams{}); return err },
		"archive_job_history": func() error { _, err := e.s.Admin.ArchiveJobHistory(ctx, admin.ArchiveHistoryParams{}); return err },
		"archive_download": func() error {
			stream, _, err := e.s.Admin.DownloadArchive(ctx, admin.ArchiveStatusParams{})
			if stream != nil {
				stream.Close()
			}
			return err
		},
		"queue_export": func() error {
			_, err := e.s.Admin.QueueArchiveExport(ctx, admin.QueueArchiveExportParams{})
			return err
		},
		"queue_import": func() error {
			_, err := e.s.Admin.QueueArchiveImport(ctx, admin.QueueArchiveImportParams{})
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestRunLifecycle(t *testing.T) {
	e := setup(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dsn, err := url.Parse(testDSN())
	must(t, err)
	var name string
	must(t, e.db.QueryRow("SELECT current_database()").Scan(&name))
	password, _ := dsn.User.Password()
	shell := New()
	must(t, shell.OnCallback(ctx, func(c Context) error { return c.Successful() }, WithCallbackIdleDelay(10*time.Millisecond)))
	done := make(chan error, 1)
	go func() {
		done <- shell.Run(ctx, DatabaseParams{User: dsn.User.Username(), Password: password, Host: dsn.Hostname(), Port: 5432, Database: name, SSLMode: "disable", Options: Options{ArchiveDirectory: t.TempDir()}})
	}()
	deadline := time.After(5 * time.Second)
	for !shell.IsReady() {
		select {
		case err := <-done:
			t.Fatalf("Run failed: %v", err)
		case <-deadline:
			t.Fatal("Run did not initialize")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := shell.Run(ctx, DatabaseParams{}); !errors.Is(err, ErrServiceRunning) {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	if shell.IsReady() {
		t.Fatal("shutdown shell remains ready")
	}
	must(t, e.db.Ping())
}

type queryTrace struct{ count atomic.Int64 }

func (q *queryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	sql := strings.ToLower(strings.TrimSpace(data.SQL))
	if sql != "begin" && sql != "commit" && sql != "rollback" {
		q.count.Add(1)
	}
	return ctx
}
func (q *queryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestBulkSQLRoundTrips(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	var name string
	must(t, e.db.QueryRow("SELECT current_database()").Scan(&name))
	u, err := url.Parse(testDSN())
	must(t, err)
	u.Path = "/" + name
	cfg, err := pgx.ParseConfig(u.String())
	must(t, err)
	trace := &queryTrace{}
	cfg.Tracer = trace
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	node, err := NewWithDatabase(ctx, db, e.opts)
	must(t, err)
	defer node.Close()
	c := model.Collection{Key: "colors", Active: true}
	changes := []model.OwnershipChange{}
	refs := []model.ItemRef{}
	for n := range 1000 {
		key := fmt.Sprint(n)
		c.Entries = append(c.Entries, model.Entry{ItemKey: key, Active: true})
		ref := model.ItemRef{CollectionKey: "colors", ItemKey: key}
		changes = append(changes, model.OwnershipChange{ItemRef: ref, Owned: true})
		refs = append(refs, ref)
	}
	_, err = node.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{c}}}})
	must(t, err)
	for _, size := range []int{1, 1000} {
		trace.count.Store(0)
		_, err := node.User.HasItems(ctx, model.HasItemsParams{ReadParams: model.ReadParams{Identity: e.i}, Items: refs[:size]})
		must(t, err)
		if got := trace.count.Load(); got != 1 {
			t.Fatalf("HasItems(%d)=%d SQL calls", size, got)
		}
		i := e.i
		i.PlatformUserID = fmt.Sprint(size)
		p := model.SetOwnedBatchParams{Identity: i, Source: "trace", OperationID: "first", Changes: changes[:size]}
		trace.count.Store(0)
		_, err = node.Internal.SetOwnedBatch(ctx, p)
		must(t, err)
		if got := trace.count.Load(); got != 8 {
			t.Fatalf("SetOwnedBatch(%d)=%d SQL calls", size, got)
		}
		trace.count.Store(0)
		_, err = node.Internal.SetOwnedBatch(ctx, p)
		must(t, err)
		if got := trace.count.Load(); got != 3 {
			t.Fatalf("replay=%d SQL calls", got)
		}
	}
}
func generatedQuery(t testing.TB, name string) string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "sqlc/query.sql.go", nil, 0)
	must(t, err)
	var out string
	ast.Inspect(f, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if ok && len(v.Names) == 1 && v.Names[0].Name == name {
			lit, ok := v.Values[0].(*ast.BasicLit)
			if ok {
				out, err = strconv.Unquote(lit.Value)
				must(t, err)
			}
		}
		return true
	})
	if out == "" {
		t.Fatalf("missing query %s", name)
	}
	return out
}
func TestExplainHotQueries(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	_, err := e.s.Internal.SetOwned(context.Background(), e.set("grant", "red", true))
	must(t, err)
	for _, tc := range []struct {
		name string
		args []any
	}{
		{"getPublishedCollection", []any{e.w, "colors"}},
		{"hasItems", []any{pq.Array([]string{"colors"}), pq.Array([]string{"red"}), e.w, e.i.AppID, e.i.PlatformID, e.i.PlatformUserID}},
		{"getReceipt", []any{e.w, e.i.AppID, e.i.PlatformID, e.i.PlatformUserID, "game", "grant"}},
		{"listOwned", []any{e.w, e.i.AppID, e.i.PlatformID, e.i.PlatformUserID, int32(100), int32(0), pq.Array([]string{})}},
	} {
		var plan []byte
		must(t, e.db.QueryRow("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+generatedQuery(t, tc.name), tc.args...).Scan(&plan))
		if !json.Valid(plan) {
			t.Fatal("invalid explain plan")
		}
		t.Logf("%s EXPLAIN: %s", tc.name, plan)
	}
}

func TestCallbackDomainValidation(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	_, err := e.s.Admin.ListCallbackEvents(ctx, model.CallbackParams{WorkspaceID: e.w, Status: "unknown"})
	if !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	_, err = e.s.Admin.GetCallbackEvent(ctx, model.CallbackParams{WorkspaceID: e.w, ID: ^uint64(0)})
	if !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	_, err = e.s.Admin.MarkCallbackEventReject(ctx, model.CallbackParams{WorkspaceID: e.w, ID: 1, Reason: " "})
	if !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
}

func testRunParams(t testing.TB, e *testEnv) DatabaseParams {
	t.Helper()
	u, err := url.Parse(testDSN())
	must(t, err)
	var name string
	must(t, e.db.QueryRow("SELECT current_database()").Scan(&name))
	password, _ := u.User.Password()
	port := 5432
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		must(t, err)
	}
	return DatabaseParams{User: u.User.Username(), Password: password, Host: u.Hostname(), Port: port, Database: name, SSLMode: "disable", Options: Options{QueryTimeout: 5 * time.Second, ArchiveDirectory: t.TempDir()}}
}
func TestRegressionCloseDuringStartup(t *testing.T) {
	e := setup(t, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := e.db.BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "LOCK TABLE importexport_job IN ACCESS EXCLUSIVE MODE")
	must(t, err)
	shell := New()
	params := testRunParams(t, e)
	done := make(chan error, 1)
	go func() { done <- shell.Run(ctx, params) }()
	for {
		var waiting int
		must(t, e.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%importexport_job%'").Scan(&waiting))
		if waiting > 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("startup lock timeout")
		case <-time.After(time.Millisecond):
		}
	}
	closed := make(chan error, 1)
	go func() { closed <- shell.Close() }()
	select {
	case err := <-closed:
		must(t, err)
	case <-time.After(time.Second):
		tx.Rollback()
		t.Fatal("Close did not cancel startup")
	}
	must(t, tx.Rollback())
	must(t, <-done)
	if shell.IsReady() {
		t.Fatal("closed shell revived")
	}
	if err := shell.Run(ctx, params); !errors.Is(err, ErrServiceClosed) {
		t.Fatalf("restart of closed shell: %v", err)
	}
	if err := shell.OnCallback(ctx, func(Context) error { return nil }); !errors.Is(err, ErrServiceClosed) {
		t.Fatal(err)
	}
}
func TestRegressionCloseWaitsEmbeddingCallback(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	_, err := e.s.Internal.SetOwned(context.Background(), e.set("grant", "red", true))
	must(t, err)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	exited := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		exited <- e.s.OnCallback(ctx, func(c Context) error { close(entered); <-c.Done(); <-release; return c.Err() }, WithCallbackIdleDelay(time.Millisecond))
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("handler not entered")
	}
	closed := make(chan error, 1)
	go func() { closed <- e.s.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before handler cleanup: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case err := <-closed:
		must(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close did not join handler")
	}
	<-exited
}
func TestRegressionConstructorDeadline(t *testing.T) {
	e := setup(t, Options{})
	e.db.SetMaxOpenConns(1)
	conn, err := e.db.Conn(context.Background())
	must(t, err)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	opts := e.opts
	opts.MaxConnections = 1
	result := make(chan error, 1)
	go func() {
		s, err := NewWithDatabase(ctx, e.db, opts)
		if s != nil {
			s.Close()
		}
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		conn.Close()
		t.Fatal("constructor ignored deadline")
	}
}

type selectedLocaleTraceKey struct{}
type selectedLocaleTrace struct{ rows atomic.Int64 }

func (q *selectedLocaleTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "Localizations") {
		return context.WithValue(ctx, selectedLocaleTraceKey{}, true)
	}
	return ctx
}
func (q *selectedLocaleTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if ctx.Value(selectedLocaleTraceKey{}) != nil {
		q.rows.Store(data.CommandTag.RowsAffected())
	}
}
func tracedDB(t testing.TB, e *testEnv, tracer pgx.QueryTracer) *sql.DB {
	t.Helper()
	var name string
	must(t, e.db.QueryRow("SELECT current_database()").Scan(&name))
	u, err := url.Parse(testDSN())
	must(t, err)
	u.Path = "/" + name
	cfg, err := pgx.ParseConfig(u.String())
	must(t, err)
	cfg.Tracer = tracer
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestRegressionUserLocaleRead(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	c := model.Collection{Key: "colors", Active: true, Localization: map[string]model.Text{}}
	for n := range 100 {
		c.Localization[fmt.Sprintf("l%02d", n)] = model.Text{Title: fmt.Sprint(n), Description: strings.Repeat("x", 65536)}
	}
	must(t, e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: c}))
	trace := &selectedLocaleTrace{}
	node, err := NewWithDatabase(ctx, tracedDB(t, e, trace), e.opts)
	must(t, err)
	defer node.Close()
	rows, err := node.User.ListCollections(ctx, model.ReadParams{Identity: e.i, Locale: "l05"})
	must(t, err)
	if len(rows) != 1 || rows[0].Text.Title != "5" || trace.rows.Load() != 1 {
		t.Fatal("list loaded unrelated locales")
	}
	trace.rows.Store(0)
	view, err := node.User.GetCollection(ctx, model.GetCollectionParams{ReadParams: model.ReadParams{Identity: e.i, Locale: "l07"}, CollectionKey: "colors"})
	must(t, err)
	if view.Text.Title != "7" || trace.rows.Load() != 1 {
		t.Fatal("get loaded unrelated locales")
	}
}
func TestRegressionBulkReorder(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	trace := &queryTrace{}
	node, err := NewWithDatabase(ctx, tracedDB(t, e, trace), e.opts)
	must(t, err)
	defer node.Close()
	for _, size := range []int{20, 1001} {
		c := model.Collection{Key: "colors", Active: true}
		keys := []string{}
		for n := range size {
			k := fmt.Sprintf("k%04d", n)
			keys = append(keys, k)
			c.Entries = append(c.Entries, model.Entry{ItemKey: k, Active: true, Position: int32(n)})
		}
		_, err := node.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{c}}}})
		must(t, err)
		for left, right := 0, len(keys)-1; left < right; left, right = left+1, right-1 {
			keys[left], keys[right] = keys[right], keys[left]
		}
		p := model.ReorderEntriesParams{WorkspaceID: e.w, ActorID: "operator", CollectionKey: "colors", ItemKeys: keys}
		trace.count.Store(0)
		must(t, node.Admin.ReorderEntries(ctx, p))
		if got := trace.count.Load(); got != 6 {
			t.Fatalf("reorder(%d) uses %d SQL calls", size, got)
		}
		page, err := node.Admin.GetCollection(ctx, model.CatalogParams{WorkspaceID: e.w, Key: "colors", Page: model.Page{Limit: 1}})
		must(t, err)
		if page.Entries[0].ItemKey != keys[0] {
			t.Fatal("wrong ordering")
		}
		bad := append([]string(nil), keys...)
		bad[len(bad)-1] = "not-in-collection"
		p.ItemKeys = bad
		if err := node.Admin.ReorderEntries(ctx, p); !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
		page, err = node.Admin.GetCollection(ctx, model.CatalogParams{WorkspaceID: e.w, Key: "colors", Page: model.Page{Limit: 1}})
		must(t, err)
		if page.Entries[0].ItemKey != keys[0] {
			t.Fatal("partial reorder persisted")
		}
	}
}
func TestRegressionImportAttribution(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	p := admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{{Key: "colors", Active: false}}}}}
	if _, err := e.s.Admin.Import(ctx, p); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	var active bool
	must(t, e.db.QueryRow("SELECT active FROM collections_collection WHERE workspace_id=$1", e.w).Scan(&active))
	if !active {
		t.Fatal("invalid import changed catalog")
	}
	p.Request.ActorID = "import-operator"
	_, err := e.s.Admin.Import(ctx, p)
	must(t, err)
	var actor string
	var payload []byte
	must(t, e.db.QueryRow("SELECT actor_id,payload FROM collections_catalog_audit WHERE workspace_id=$1 AND method='import_catalog'", e.w).Scan(&actor, &payload))
	var metadata struct {
		PackageHash string `json:"package_hash"`
	}
	must(t, json.Unmarshal(payload, &metadata))
	if actor != "import-operator" || len(metadata.PackageHash) != 64 {
		t.Fatal("missing import attribution")
	}
}
func TestRegressionCallbackNotFound(t *testing.T) {
	e := setup(t, Options{})
	_, err := e.s.Admin.GetCallbackEvent(context.Background(), model.CallbackParams{WorkspaceID: e.w, ID: 12345})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("callback error %v", err)
	}
}

type publicMemoryArchive struct {
	mu      sync.Mutex
	data    map[string][]byte
	objects []ArchiveObject
	listed  atomic.Int64
}

func (a *publicMemoryArchive) Store(ctx context.Context, o ArchiveObject, r io.Reader) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	key := uuid.NewString()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data[key] = data
	a.objects = append(a.objects, o)
	return key, nil
}
func (a *publicMemoryArchive) Open(_ context.Context, key string) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, ok := a.data[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}
func (a *publicMemoryArchive) Delete(_ context.Context, key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.data, key)
	return nil
}
func (a *publicMemoryArchive) List(context.Context) ([]ArchiveInfo, error) {
	a.listed.Add(1)
	a.mu.Lock()
	defer a.mu.Unlock()
	out := []ArchiveInfo{}
	for key := range a.data {
		out = append(out, ArchiveInfo{Key: key, CreatedAt: time.Now()})
	}
	return out, nil
}
func TestRegressionPublicArchive(t *testing.T) {
	archive := &publicMemoryArchive{data: map[string][]byte{}}
	var _ Archive = archive
	var _ ArchiveLister = archive
	e := setup(t, Options{Archive: archive})
	e.seed(t, "red")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !e.s.Admin.StartArchiveJobs(ctx) {
		t.Fatal("archive workers failed")
	}
	queued, err := e.s.Admin.QueueArchiveExport(ctx, admin.QueueArchiveExportParams{WorkspaceID: e.w, FileName: "export.zip"})
	must(t, err)
	for {
		status, err := e.s.Admin.ArchiveJob(ctx, admin.ArchiveStatusParams{WorkspaceID: e.w, ID: queued.ID})
		must(t, err)
		if status.Status == "completed" {
			break
		}
		if status.Status == "failed" {
			t.Fatal(status.Error)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	stream, _, err := e.s.Admin.DownloadArchive(ctx, admin.ArchiveStatusParams{WorkspaceID: e.w, ID: queued.ID})
	must(t, err)
	defer stream.Close()
	data, err := io.ReadAll(stream)
	must(t, err)
	if len(data) == 0 {
		t.Fatal("empty custom archive")
	}
	archive.mu.Lock()
	objects := append([]ArchiveObject(nil), archive.objects...)
	archive.mu.Unlock()
	if len(objects) != 1 || objects[0].Service != "collections" || objects[0].WorkspaceID != e.w {
		t.Fatal(objects)
	}
	for archive.listed.Load() == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("ArchiveLister was not preserved")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRegressionImportAuditRollback(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	_, err := e.db.Exec(`CREATE FUNCTION reject_import_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.method='import_catalog' THEN RAISE EXCEPTION 'audit unavailable'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_import_audit BEFORE INSERT ON collections_catalog_audit FOR EACH ROW EXECUTE FUNCTION reject_import_audit();`)
	must(t, err)
	_, err = e.s.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{{Key: "colors", Active: false}}}}})
	if err == nil {
		t.Fatal("import ignored mandatory audit failure")
	}
	row, err := e.s.Admin.GetCollection(ctx, model.CatalogParams{WorkspaceID: e.w, Key: "colors"})
	must(t, err)
	if !row.Active || len(row.Entries) != 1 || row.Entries[0].ItemKey != "red" {
		t.Fatal("audit failure left partial import")
	}
}
func TestRegressionLifecycleConcurrentClose(t *testing.T) {
	e := setup(t, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shell := New()
	done := make(chan error, 1)
	params := testRunParams(t, e)
	go func() { done <- shell.Run(ctx, params) }()
	deadline := time.After(5 * time.Second)
	for !shell.IsReady() {
		select {
		case <-deadline:
			t.Fatal("startup timeout")
		case <-time.After(time.Millisecond):
		}
	}
	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() { errs <- shell.Close() }()
	}
	for range n {
		must(t, <-errs)
	}
	must(t, <-done)
	if shell.IsReady() {
		t.Fatal("closed service ready")
	}
}

func TestPublishedCatalogSeparatesOwnership(t *testing.T) {
	cache := &sharedTestCache{values: map[string][]byte{}, expires: map[string]time.Time{}}
	e := setup(t, Options{CacheEnabled: true, Cache: cache, Mutex: sqlwrap.NewMutex()})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	trace := &queryTrace{}
	node, err := NewWithDatabase(ctx, tracedDB(t, e, trace), e.opts)
	must(t, err)
	defer node.Close()
	p := model.GetCollectionParams{
		ReadParams:    model.ReadParams{Identity: e.i, Locale: "ru"},
		CollectionKey: "colors",
	}
	trace.count.Store(0)
	catalog, err := node.User.GetCollection(ctx, p)
	must(t, err)
	if catalog.Version == "" || len(catalog.Entries) != 2 || catalog.Text.Title != "Цвета" || trace.count.Load() != 5 {
		t.Fatalf("cold catalog=%+v SQL=%d", catalog, trace.count.Load())
	}
	encoded, err := json.Marshal(catalog)
	must(t, err)
	for _, field := range []string{`"owned"`, `"revision"`, `"changed_at"`} {
		if bytes.Contains(encoded, []byte(field)) {
			t.Fatalf("user state in shared catalog: %s", field)
		}
	}
	// An independent node loads the same page from L2, using one DB revision read.
	node2, err := NewWithDatabase(ctx, tracedDB(t, e, trace), e.opts)
	must(t, err)
	defer node2.Close()
	trace.count.Store(0)
	l2, err := node2.User.GetCollection(ctx, p)
	must(t, err)
	if !reflect.DeepEqual(catalog, l2) || trace.count.Load() != 1 {
		t.Fatalf("L2 miss: SQL=%d", trace.count.Load())
	}
	cache.mu.Lock()
	longTTL := false
	for key, value := range cache.values {
		if bytes.Contains(value, []byte(catalog.Version)) && time.Until(cache.expires[key]) > 23*time.Hour {
			longTTL = true
		}
	}
	cache.mu.Unlock()
	if !longTTL {
		t.Fatal("catalog did not use the default 24h TTL")
	}
	// Detach response slices from the shared L1 value.
	l2.Entries[0].ItemKey = "mutated-by-consumer"
	visitor := e.i
	visitor.PlatformUserID = "visitor"
	p.Identity = visitor
	trace.count.Store(0)
	other, err := node2.User.GetCollection(ctx, p)
	must(t, err)
	if !reflect.DeepEqual(catalog, other) || trace.count.Load() != 1 {
		t.Fatal("catalog depends on caller or leaked a mutated slice")
	}
	for n, owned := range []bool{true, false} {
		_, err := e.s.Internal.SetOwned(ctx, e.set(fmt.Sprint("change-", n), "red", owned))
		must(t, err)
		p.KnownVersion = catalog.Version
		trace.count.Store(0)
		unchanged, err := node2.User.GetCollection(ctx, p)
		must(t, err)
		if !unchanged.NotModified || unchanged.Version != catalog.Version || len(unchanged.Entries) != 0 || trace.count.Load() != 1 {
			t.Fatal("ownership invalidated the static catalog")
		}
		for _, subject := range []*services.Identity{nil, &e.i} {
			trace.count.Store(0)
			states, err := node2.User.ListOwned(ctx, model.ListOwnedParams{
				ReadParams:     model.ReadParams{Identity: visitor, Subject: subject},
				CollectionKeys: []string{"colors"},
			})
			must(t, err)
			want := 0
			if subject != nil && owned {
				want = 1
			}
			if trace.count.Load() != 1 || len(states) != want || (want == 1 && (states[0].ItemKey != "red" || !states[0].Owned)) {
				t.Fatal("ListOwned returned stale or foreign ownership", states)
			}
		}
		if e.has(t, "red").Owned != owned {
			t.Fatal("HasItem stale")
		}
	}
}

func TestPublishedCatalogVersionScopeAndVisibility(t *testing.T) {
	e := setup(t, Options{CacheEnabled: true})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	otherWorkspace := testsupport.WorkspaceID("other-catalog")
	for _, c := range []model.SaveCollectionParams{
		{WorkspaceID: e.w, ActorID: "operator", Collection: model.Collection{Key: "shapes", Active: true}},
		{WorkspaceID: otherWorkspace, ActorID: "operator", Collection: model.Collection{Key: "colors", Active: true}},
	} {
		must(t, e.s.Admin.CreateCollection(ctx, c))
	}
	p := model.GetCollectionParams{ReadParams: model.ReadParams{Identity: e.i, Locale: "ru"}, CollectionKey: "colors"}
	before, err := e.s.User.GetCollection(ctx, p)
	must(t, err)
	p.KnownVersion = before.Version
	for name, change := range map[string]func(*model.GetCollectionParams){
		"locale":     func(p *model.GetCollectionParams) { p.Locale = "en" },
		"collection": func(p *model.GetCollectionParams) { p.CollectionKey = "shapes" },
		"workspace":  func(p *model.GetCollectionParams) { p.Identity.WorkspaceID = otherWorkspace },
		"limit":      func(p *model.GetCollectionParams) { p.Page.Limit = 1 },
		"offset":     func(p *model.GetCollectionParams) { p.Page.Offset = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			other := p
			change(&other)
			view, err := e.s.User.GetCollection(ctx, other)
			must(t, err)
			if view.NotModified || view.Version == before.Version {
				t.Fatal("version not bound to resource variant")
			}
		})
	}
	for _, version := range []string{"bad", strings.Repeat("x", 64)} {
		invalid := p
		invalid.KnownVersion = version
		_, err := e.s.User.GetCollection(ctx, invalid)
		if !errors.Is(err, ErrInvalidArgument) {
			t.Fatal(err)
		}
	}
	for _, change := range []func(*model.GetCollectionParams){
		func(p *model.GetCollectionParams) { p.CollectionKey = "missing" },
		func(p *model.GetCollectionParams) { p.Identity.WorkspaceID = testsupport.WorkspaceID("missing") },
	} {
		missing := p
		change(&missing)
		_, err := e.s.User.GetCollection(ctx, missing)
		if !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
	}
	wrong := e.i
	wrong.AppID++
	invalid := p
	invalid.Subject = &wrong
	_, err = e.s.User.GetCollection(ctx, invalid)
	if !errors.Is(err, ErrForbidden) {
		t.Fatal("conditional read bypassed scope", err)
	}
	_, err = e.s.Internal.SetOwned(ctx, e.set("grant", "red", true))
	must(t, err)
	must(t, e.s.Admin.ArchiveCollection(ctx, model.ArchiveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Key: "colors"}))
	_, err = e.s.User.GetCollection(ctx, p)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("conditional read exposed archived collection", err)
	}
	must(t, e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: model.Collection{Key: "colors", Active: true}}))
	after, err := e.s.User.GetCollection(ctx, p)
	must(t, err)
	if after.NotModified || after.Version == before.Version || !e.has(t, "red").Owned {
		t.Fatal("unarchive lost catalog version or ownership")
	}
	must(t, e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: model.Collection{Key: "colors", Active: false}}))
	_, err = e.s.User.GetCollection(ctx, p)
	if !errors.Is(err, ErrNotFound) {
		t.Fatal("inactive collection visible", err)
	}
}

func TestPublishedCatalogMutationVersionsAndCacheOutage(t *testing.T) {
	cache := &sharedTestCache{values: map[string][]byte{}, expires: map[string]time.Time{}}
	e := setup(t, Options{CacheEnabled: true, Cache: cache, Mutex: sqlwrap.NewMutex()})
	e.seed(t, "red", "blue")
	ctx := context.Background()
	node, err := NewWithDatabase(ctx, e.db, e.opts)
	must(t, err)
	defer node.Close()
	p := model.GetCollectionParams{ReadParams: model.ReadParams{Identity: e.i}, CollectionKey: "colors"}
	before, err := node.User.GetCollection(ctx, p)
	must(t, err)
	cache.mu.Lock()
	cache.fail = true
	cache.mu.Unlock()
	entry := model.EntryParams{WorkspaceID: e.w, ActorID: "operator", CollectionKey: "colors", Entry: model.Entry{ItemKey: "green", Active: true, Position: 2}}
	mutations := []struct {
		name string
		call func() error
	}{
		{"add", func() error { return e.s.Admin.AddEntry(ctx, entry) }},
		{"hide", func() error { entry.Entry.Active = false; return e.s.Admin.AddEntry(ctx, entry) }},
		{"remove", func() error { return e.s.Admin.RemoveEntry(ctx, entry) }},
		{"readd", func() error { entry.Entry.Active = true; return e.s.Admin.AddEntry(ctx, entry) }},
		{"reorder", func() error {
			return e.s.Admin.ReorderEntries(ctx, model.ReorderEntriesParams{WorkspaceID: e.w, ActorID: "operator", CollectionKey: "colors", ItemKeys: []string{"green", "red", "blue"}})
		}},
		{"update", func() error {
			return e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: model.Collection{Key: "colors", Active: true, Position: 8}})
		}},
		{"import", func() error {
			_, err := e.s.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{{Key: "colors", Active: true, Entries: []model.Entry{{ItemKey: "red", Active: true}}}}}}})
			return err
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			must(t, mutation.call())
			p.KnownVersion = before.Version
			after, err := node.User.GetCollection(ctx, p)
			must(t, err)
			if after.NotModified || after.Version == before.Version {
				t.Fatal("second node served stale catalog during cache outage")
			}
			before = after
		})
	}
	// A failed mutation must not publish a new version or leave partial data.
	p.KnownVersion = before.Version
	_, err = e.db.ExecContext(ctx, `CREATE FUNCTION reject_catalog_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_catalog_audit BEFORE INSERT ON collections_catalog_audit FOR EACH ROW EXECUTE FUNCTION reject_catalog_audit();`)
	must(t, err)
	if err := e.s.Admin.AddEntry(ctx, entry); err == nil {
		t.Fatal("ignored audit failure")
	}
	after, err := node.User.GetCollection(ctx, p)
	must(t, err)
	if !after.NotModified || after.Version != before.Version {
		t.Fatal("rolled back mutation changed catalog version")
	}
	// Starting a new service cannot reset the durable version.
	restarted, err := NewWithDatabase(ctx, e.db, e.opts)
	must(t, err)
	defer restarted.Close()
	must(t, restarted.Admin.Bootstrap(ctx))
	after, err = restarted.User.GetCollection(ctx, p)
	must(t, err)
	if !after.NotModified {
		t.Fatal("restart reset catalog version")
	}
}

type catalogSnapshotTraceKey struct{}
type catalogSnapshotTrace struct {
	versions atomic.Int64
	reached  chan struct{}
	release  chan struct{}
}

func (q *catalogSnapshotTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "GetPublishedCollection") {
		return context.WithValue(ctx, catalogSnapshotTraceKey{}, true)
	}
	return ctx
}
func (q *catalogSnapshotTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(catalogSnapshotTraceKey{}) != nil && q.versions.Add(1) == 2 {
		close(q.reached)
		<-q.release
	}
}
func TestPublishedCatalogConcurrentSnapshot(t *testing.T) {
	e := setup(t, Options{CacheEnabled: true})
	e.seed(t, "red")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := model.GetCollectionParams{ReadParams: model.ReadParams{Identity: e.i}, CollectionKey: "colors"}
	before, err := e.s.User.GetCollection(ctx, p)
	must(t, err)
	trace := &catalogSnapshotTrace{reached: make(chan struct{}), release: make(chan struct{})}
	node, err := NewWithDatabase(ctx, tracedDB(t, e, trace), e.opts)
	must(t, err)
	defer node.Close()
	var result model.CollectionView
	var readErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		result, readErr = node.User.GetCollection(ctx, p)
	}()
	var release sync.Once
	defer release.Do(func() { close(trace.release) })
	select {
	case <-trace.reached:
	case <-ctx.Done():
		t.Fatal("reader did not reach snapshot")
	}
	must(t, e.s.Admin.RemoveEntry(ctx, model.EntryParams{WorkspaceID: e.w, ActorID: "operator", CollectionKey: "colors", Entry: model.Entry{ItemKey: "red"}}))
	release.Do(func() { close(trace.release) })
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("reader did not finish")
	}
	must(t, readErr)
	if !reflect.DeepEqual(result, before) {
		t.Fatal("catalog data and version came from different snapshots", result)
	}
	fresh, err := node.User.GetCollection(ctx, p)
	must(t, err)
	if fresh.Version == before.Version || len(fresh.Entries) != 0 {
		t.Fatal("snapshot cache resurrected a removed entry", fresh)
	}
}

func TestPublishedCatalogImportVersionTransactions(t *testing.T) {
	e := setup(t, Options{})
	e.seed(t, "red")
	ctx := context.Background()
	version := func() int64 {
		var revision int64
		must(t, e.db.QueryRowContext(ctx, "SELECT revision FROM collections_catalog_version WHERE workspace_id=$1", e.w).Scan(&revision))
		return revision
	}
	p := admin.ImportParams{
		WorkspaceID: e.w,
		Request: model.ImportRequest{
			ActorID: "operator",
			Package: model.ExportPackage{
				Format:  model.ExportFormat,
				Service: "collections",
				Collections: []model.Collection{
					{Key: "colors", Active: true},
					{Key: "shapes", Active: true},
				},
			},
		},
	}
	before := version()
	_, err := e.s.Admin.Import(ctx, p)
	must(t, err)
	if version() != before+1 {
		t.Fatal("multi-collection import did not bump exactly once")
	}
	before = version()
	p.Request.ConflictStrategy = model.ConflictSkip
	result, err := e.s.Admin.Import(ctx, p)
	must(t, err)
	if result.Skipped != 2 || version() != before {
		t.Fatal("skip-only import changed version")
	}
	p.Request.ConflictStrategy = model.ConflictFail
	_, err = e.s.Admin.Import(ctx, p)
	if !errors.Is(err, ErrConflict) || version() != before {
		t.Fatal("failed import changed version", err)
	}
	_, err = e.db.ExecContext(ctx, `CREATE FUNCTION reject_version_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit unavailable'; END $$; CREATE TRIGGER reject_version_audit BEFORE INSERT ON collections_catalog_audit FOR EACH ROW EXECUTE FUNCTION reject_version_audit();`)
	must(t, err)
	p.Request.ConflictStrategy = model.ConflictUpdate
	_, err = e.s.Admin.Import(ctx, p)
	if err == nil || version() != before {
		t.Fatal("rolled back import changed version", err)
	}
}

func TestExplainPublishedCatalogLargePage(t *testing.T) {
	e := setup(t, Options{})
	ctx := context.Background()
	c := model.Collection{Key: "colors", Active: true}
	changes := []model.OwnershipChange{}
	for n := range 1000 {
		key := fmt.Sprintf("color-%04d", n)
		c.Entries = append(c.Entries, model.Entry{ItemKey: key, Active: true, Position: int32(n)})
		changes = append(changes, model.OwnershipChange{ItemRef: model.ItemRef{CollectionKey: "colors", ItemKey: key}, Owned: true})
	}
	_, err := e.s.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{c}}}})
	must(t, err)
	_, err = e.s.Internal.SetOwnedBatch(ctx, model.SetOwnedBatchParams{Identity: e.i, Source: "explain", OperationID: "seed", Changes: changes})
	must(t, err)
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			_, err := e.db.ExecContext(ctx, "ANALYZE collections_collection; ANALYZE collections_entry; ANALYZE collections_ownership; ANALYZE collections_catalog_version")
			must(t, err)
		}
		for _, tc := range []struct {
			name string
			args []any
		}{
			{"getPublishedCollection", []any{e.w, "colors"}},
			{"listPublishedEntries", []any{e.w, "colors", int32(1000), int32(0)}},
			{"listOwned", []any{e.w, e.i.AppID, e.i.PlatformID, e.i.PlatformUserID, int32(1000), int32(0), pq.Array([]string{"colors"})}},
		} {
			rows, err := e.db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+generatedQuery(t, tc.name), tc.args...)
			must(t, err)
			var plan []string
			for rows.Next() {
				var line string
				must(t, rows.Scan(&line))
				plan = append(plan, line)
			}
			must(t, rows.Err())
			must(t, rows.Close())
			t.Logf("%s analyzed=%t:\n%s", tc.name, analyzed, strings.Join(plan, "\n"))
		}
	}
}
