package reference

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	resourceservice "github.com/elum2b/services/reference/service/resource"
	"github.com/elum2b/services/reference/service/user"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

func TestIsReady(t *testing.T) {
	var nilService *Reference

	if nilService.IsReady() {
		t.Fatal("nil reference must not be ready")
	}

	service := New()
	if service.IsReady() {
		t.Fatal("uninitialized reference must not be ready")
	}

	if _, err := service.User.Get(context.Background(), user.GetParams{
		WorkspaceID: "00000000-0000-0000-0000-000000000001",
		Key:         "item",
		Locale:      "ru",
	}); !errors.Is(err, sqlwrap.ErrServiceNotReady) {
		t.Fatalf("unready reference user error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	service.rootCtx, service.client, service.Admin, service.User = ctx, &sqlwrap.Client{}, &admin.Admin{}, &user.User{}

	if !service.IsReady() {
		t.Fatal("initialized reference must be ready")
	}

	cancel()

	if service.IsReady() {
		t.Fatal("closed reference must not be ready")
	}
}

func TestReferenceRunBlocksUntilContextCanceled(t *testing.T) {
	newReferenceTestService(t)

	params := DatabaseParams{
		User:     referenceTestPGUser,
		Password: referenceTestPGPassword,
		Database: referenceTestDB,
		Host:     referenceTestPGHost,
		Port:     referenceTestPGPort,
	}
	service := New()
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- service.Run(runCtx, params)
	}()

	deadline := time.Now().Add(5 * time.Second)

	for !service.IsReady() {
		select {
		case err := <-done:
			cancel()
			t.Fatalf("Run returned before readiness: %v", err)
		default:
		}

		if time.Now().After(deadline) {
			cancel()
			t.Fatal("reference service did not become ready")
		}

		time.Sleep(10 * time.Millisecond)
	}

	if err := service.Run(
		context.Background(),
		params,
	); !errors.Is(
		err,
		ErrServiceRunning,
	) {
		cancel()
		t.Fatalf("second Run error = %v, want ErrServiceRunning", err)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run after cancellation: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reference Run did not stop after cancellation")
	}
}

func TestReferenceCacheVersionInvalidatesOtherNode(t *testing.T) {
	workspaceID := testsupport.WorkspaceID("cache-workspace")
	cache := newReferenceSharedCache()
	options := Options{
		Cache:        cache,
		CacheEnabled: true,
		CacheL1Delay: time.Minute,
		CacheL2Delay: time.Minute,
	}
	nodeA := newReferenceTestServiceWithOptions(t, referenceTestDB, options)

	db, err := openReferencePostgres(referenceTestDB)
	if err != nil {
		t.Fatalf("open second reference node database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	nodeB, err := NewWithDatabase(
		context.Background(),
		db,
		referenceTestOptions(options),
	)
	if err != nil {
		t.Fatalf("create second reference node: %v", err)
	}

	t.Cleanup(func() { _ = nodeB.Close() })

	if err := nodeA.Admin.CreateItem(context.Background(), admin.SaveItemParams{
		WorkspaceID: workspaceID,
		Key:         "stars",
		Type:        repository.ItemTypeQuantity,
		Payload:     json.RawMessage(`{"version":1}`),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("create cached reference item: %v", err)
	}

	if err := nodeA.Admin.UpsertLocalization(
		context.Background(),
		admin.SaveLocalizationParams{
			WorkspaceID: workspaceID,
			ItemKey:     "stars",
			Locale:      "ru",
			Title:       "Old title",
		},
	); err != nil {
		t.Fatalf("create cached reference localization: %v", err)
	}

	warmReferenceReads(t, nodeB, workspaceID, "Old title", 1)

	if _, err := nodeA.Admin.UpdateItem(
		context.Background(),
		admin.UpdateItemParams{
			WorkspaceID: workspaceID,
			Key:         "stars",
			Payload:     json.RawMessage(`{"version":2}`),
			IsActive:    true,
		},
	); err != nil {
		t.Fatalf("update cached reference item: %v", err)
	}

	if err := nodeA.Admin.UpsertLocalization(
		context.Background(),
		admin.SaveLocalizationParams{
			WorkspaceID: workspaceID,
			ItemKey:     "stars",
			Locale:      "ru",
			Title:       "New title",
		},
	); err != nil {
		t.Fatalf("update cached reference localization: %v", err)
	}

	warmReferenceReads(t, nodeB, workspaceID, "New title", 2)
}

func TestReferenceResolveCachePreservesRequestedOrder(t *testing.T) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		Cache:        newReferenceSharedCache(),
		CacheEnabled: true,
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resolve-cache-order")

	for _, key := range []string{"alpha", "beta"} {
		if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: workspaceID,
			Key:         key,
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
		}); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}

	first, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceID,
		Keys:        []string{"alpha", "beta"},
	})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	second, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceID,
		Keys:        []string{"beta", "alpha"},
	})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}

	if len(first.Items) != 2 || first.Items[0].Key != "alpha" ||
		first.Items[1].Key != "beta" {
		t.Fatalf("first order: %+v", first.Items)
	}

	if len(second.Items) != 2 || second.Items[0].Key != "beta" ||
		second.Items[1].Key != "alpha" {
		t.Fatalf("cached second order: %+v", second.Items)
	}
}

func TestReferenceImportBatchesLargePackage(t *testing.T) {
	service := newReferenceTestService(t)

	const itemCount = 12001

	items := make([]repository.ExportItem, 0, itemCount)

	for index := 0; index < itemCount; index++ {
		items = append(items, repository.ExportItem{
			Key:      fmt.Sprintf("large.item.%05d", index),
			Type:     repository.ItemTypeQuantity,
			Payload:  json.RawMessage(`{}`),
			IsActive: true,
		})
	}

	result, err := repository.New(service.client).Import(
		context.Background(),
		testsupport.WorkspaceID("large-workspace"),
		admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "reference",
				Items:   items,
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		},
	)
	if err != nil {
		t.Fatalf("import large reference package: %v", err)
	}

	if result.Imported.Items != itemCount {
		t.Fatalf(
			"imported items = %d, want %d",
			result.Imported.Items,
			itemCount,
		)
	}
}

func TestReferenceImportSerializesWithAdminWrite(t *testing.T) {
	service := newReferenceTestService(t)

	db, err := openReferencePostgres(referenceTestDB)
	if err != nil {
		t.Fatalf("open reference lock database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("concurrent-workspace")

	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin reference lock transaction: %v", err)
	}

	t.Cleanup(func() { _ = transaction.Rollback() })

	if _, err := transaction.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"reference:"+workspaceID,
	); err != nil {
		t.Fatalf("lock reference workspace: %v", err)
	}

	importResult := make(chan error, 1)

	go func() {
		_, err := repository.New(service.client).
			Import(ctx, workspaceID, admin.ImportRequest{
				Package: admin.ExportPackage{
					Format:  repository.ExportFormat,
					Service: "reference",
					Items: []repository.ExportItem{
						{
							Key:      "import.item",
							Type:     repository.ItemTypeQuantity,
							Payload:  json.RawMessage(`{}`),
							IsActive: true,
						},
					},
				},
				ConflictStrategy: repository.ImportConflictUpdate,
			})
		importResult <- err
	}()

	waitForReferenceWorkspaceLock(t, db, 1)

	adminResult := make(chan error, 1)
	go func() {
		adminResult <- service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: workspaceID,
			Key:         "admin.item",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
		})
	}()

	waitForReferenceWorkspaceLock(t, db, 2)

	if err := transaction.Commit(); err != nil {
		t.Fatalf("release reference workspace lock: %v", err)
	}

	if err := <-importResult; err != nil {
		t.Fatalf("concurrent reference import: %v", err)
	}

	if err := <-adminResult; err != nil {
		t.Fatalf("concurrent reference admin write: %v", err)
	}

	items, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceID,
		Keys:        []string{"import.item", "admin.item"},
	})
	if err != nil || len(items.Items) != 2 {
		t.Fatalf("concurrent reference result: items=%+v err=%v", items, err)
	}
}

func TestReferenceResourceUpdatePreservesNewMediaVersion(t *testing.T) {
	service := newReferenceTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resource-media-version")
	repo := repository.New(service.client)

	resource := repository.Resource{
		WorkspaceID:    workspaceID,
		Key:            "banner",
		Type:           "image",
		Payload:        json.RawMessage(`{"color":"blue"}`),
		IsActive:       true,
		Format:         "png",
		ContentType:    "image/png",
		SHA256:         strings.Repeat("a", 64),
		MediaVersion:   "AbCdEfGh",
		Size:           1,
		Width:          1,
		Height:         1,
		OriginalRef:    "original",
		Preview61Ref:   "preview-61",
		Preview128Ref:  "preview-128",
		Preview256Ref:  "preview-256",
		Preview512Ref:  "preview-512",
		PlaceholderRef: "placeholder",
	}
	if err := repo.CreateResource(ctx, resource); err != nil {
		t.Fatalf("create resource: %v", err)
	}

	resource.MediaVersion = "HgFeDcBa"
	resource.SHA256 = strings.Repeat("b", 64)

	if _, err := repo.UpdateResource(ctx, resource); err != nil {
		t.Fatalf("update resource: %v", err)
	}

	updated, err := repo.GetResource(ctx, workspaceID, resource.Key)
	if err != nil {
		t.Fatalf("get updated resource: %v", err)
	}

	if updated.MediaVersion != resource.MediaVersion {
		t.Fatalf(
			"media version = %q, want %q",
			updated.MediaVersion,
			resource.MediaVersion,
		)
	}
}

func TestReferenceResourceLifecycleAndWorkspaceIsolation(t *testing.T) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceStorage: resourcestorage.Config{Directory: t.TempDir()},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resource-lifecycle")
	otherWorkspaceID := testsupport.WorkspaceID("resource-lifecycle-other")

	created, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "banner",
		Type:        "image",
		Payload:     json.RawMessage(`{"color":"red"}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{R: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	second, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "logo",
		Type:        "image",
		Payload:     json.RawMessage(`{"color":"green"}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{G: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("create second resource: %v", err)
	}

	listed, err := service.Resource.List(ctx, resourceservice.ListParams{
		WorkspaceID: workspaceID,
		Limit:       1,
	})
	if err != nil || len(listed) != 1 || listed[0].Key != created.Key {
		t.Fatalf("list resources: values=%+v err=%v", listed, err)
	}

	listed, err = service.Resource.List(ctx, resourceservice.ListParams{
		WorkspaceID: workspaceID,
		Limit:       1,
		Offset:      1,
	})
	if err != nil || len(listed) != 1 || listed[0].Key != second.Key {
		t.Fatalf("list resources page two: values=%+v err=%v", listed, err)
	}

	if _, err := service.Resource.Get(ctx, resourceservice.GetParams{
		WorkspaceID: otherWorkspaceID,
		Key:         created.Key,
	}); !errors.Is(err, repository.ErrItemNotFound) {
		t.Fatalf("resource crossed workspace boundary: %v", err)
	}

	updated, err := service.Resource.Update(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
		Type:        "image",
		Payload:     json.RawMessage(`{"color":"blue"}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{B: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("update resource: %v", err)
	}

	if updated.MediaVersion == created.MediaVersion {
		t.Fatal("resource update did not create a new media version")
	}

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceID,
		Key:         "item",
		Type:        repository.ItemTypeQuantity,
		Payload:     json.RawMessage(`{}`),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}

	if err := service.Resource.Attach(
		ctx,
		workspaceID,
		"item",
		updated.Key,
		10,
	); err != nil {
		t.Fatalf("attach resource: %v", err)
	}

	attached, err := service.Resource.ListItemResources(
		ctx,
		workspaceID,
		"item",
	)
	if err != nil || len(attached) != 1 || attached[0].Key != updated.Key {
		t.Fatalf("list attached resources: values=%+v err=%v", attached, err)
	}

	item, err := service.User.Get(
		ctx,
		user.GetParams{WorkspaceID: workspaceID, Key: "item"},
	)
	if err != nil || len(item.Resources) != 1 ||
		item.Resources[0].Key != updated.Key {
		t.Fatalf("user item resources: value=%+v err=%v", item, err)
	}

	detached, err := service.Resource.Detach(
		ctx,
		workspaceID,
		"item",
		updated.Key,
	)
	if err != nil || detached != 1 {
		t.Fatalf("detach resource: rows=%d err=%v", detached, err)
	}

	attached, err = service.Resource.ListItemResources(ctx, workspaceID, "item")
	if err != nil || len(attached) != 0 {
		t.Fatalf("resources after detach: values=%+v err=%v", attached, err)
	}

	if err := service.Resource.Attach(
		ctx,
		workspaceID,
		"item",
		updated.Key,
		10,
	); err != nil {
		t.Fatalf("reattach resource: %v", err)
	}

	if err := service.Resource.Delete(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         updated.Key,
	}); err != nil {
		t.Fatalf("soft delete resource: %v", err)
	}

	listed, err = service.Resource.List(
		ctx,
		resourceservice.ListParams{WorkspaceID: workspaceID},
	)
	if err != nil || len(listed) != 1 || listed[0].Key != second.Key {
		t.Fatalf("resources after delete: values=%+v err=%v", listed, err)
	}

	attached, err = service.Resource.ListItemResources(ctx, workspaceID, "item")
	if err != nil || len(attached) != 0 {
		t.Fatalf(
			"attached resources after delete: values=%+v err=%v",
			attached,
			err,
		)
	}

	if err := service.Resource.Delete(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         second.Key,
	}); err != nil {
		t.Fatalf("soft delete second resource: %v", err)
	}

	listed, err = service.Resource.List(
		ctx,
		resourceservice.ListParams{WorkspaceID: workspaceID},
	)
	if err != nil || len(listed) != 0 {
		t.Fatalf(
			"resources after second delete: values=%+v err=%v",
			listed,
			err,
		)
	}
}

func TestReferenceResourceCacheVersionInvalidatesOtherNode(t *testing.T) {
	cache := newReferenceSharedCache()
	options := Options{
		Cache:           cache,
		CacheEnabled:    true,
		CacheL1Delay:    time.Minute,
		CacheL2Delay:    time.Minute,
		ResourceStorage: resourcestorage.Config{Directory: t.TempDir()},
	}
	nodeA := newReferenceTestServiceWithOptions(t, referenceTestDB, options)

	db, err := openReferencePostgres(referenceTestDB)
	if err != nil {
		t.Fatalf("open second reference node database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	nodeB, err := NewWithDatabase(
		context.Background(),
		db,
		referenceTestOptions(options),
	)
	if err != nil {
		t.Fatalf("create second reference node: %v", err)
	}

	t.Cleanup(func() { _ = nodeB.Close() })

	workspaceID := testsupport.WorkspaceID("resource-cache-workspace")

	created, err := nodeA.Resource.Create(
		context.Background(),
		resourceservice.SaveParams{
			WorkspaceID: workspaceID,
			Key:         "banner",
			Type:        "image",
			Payload:     json.RawMessage(`{"color":"red"}`),
			IsActive:    true,
			File:        referencePNG(t, color.NRGBA{R: 255, A: 255}),
		},
	)
	if err != nil {
		t.Fatalf("create cached resource: %v", err)
	}

	warm, err := nodeB.Resource.Get(
		context.Background(),
		resourceservice.GetParams{
			WorkspaceID: workspaceID,
			Key:         created.Key,
		},
	)
	if err != nil {
		t.Fatalf("warm second node resource cache: %v", err)
	}

	updated, err := nodeA.Resource.Update(
		context.Background(),
		resourceservice.SaveParams{
			WorkspaceID: workspaceID,
			Key:         created.Key,
			Type:        "image",
			Payload:     json.RawMessage(`{"color":"blue"}`),
			IsActive:    true,
			File:        referencePNG(t, color.NRGBA{B: 255, A: 255}),
		},
	)
	if err != nil {
		t.Fatalf("update cached resource: %v", err)
	}

	got, err := nodeB.Resource.Get(
		context.Background(),
		resourceservice.GetParams{
			WorkspaceID: workspaceID,
			Key:         created.Key,
		},
	)
	if err != nil || got.MediaVersion != updated.MediaVersion ||
		got.MediaVersion == warm.MediaVersion {
		t.Fatalf(
			"second node returned stale resource: value=%+v err=%v",
			got,
			err,
		)
	}
}

func TestReferenceResourceGarbageCollectionPurgesStorageAndDatabase(
	t *testing.T,
) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceStorage: resourcestorage.Config{Directory: t.TempDir()},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resource-garbage-collection")

	created, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "banner",
		Type:        "image",
		Payload:     json.RawMessage(`{}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{R: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if err := service.Resource.Delete(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
	}); err != nil {
		t.Fatalf("soft delete resource: %v", err)
	}

	if purged, err := service.Resource.CollectGarbage(
		ctx,
		resourceservice.CollectGarbageParams{Limit: 10},
	); err != nil ||
		purged != 0 {
		t.Fatalf(
			"resource was purged before retention: purged=%d err=%v",
			purged,
			err,
		)
	}

	ageResourceMediaVersions(t, service, workspaceID, created.Key)

	purged, err := service.Resource.CollectGarbage(
		ctx,
		resourceservice.CollectGarbageParams{Limit: 10},
	)
	if err != nil || purged != 1 {
		t.Fatalf("collect garbage: purged=%d err=%v", purged, err)
	}

	if _, err := service.Resource.Get(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
	}); !errors.Is(err, repository.ErrItemNotFound) {
		t.Fatalf("purged resource remains in database: %v", err)
	}

	if _, err := service.storage.ReadVersion(
		ctx,
		workspaceID,
		created.Key,
		created.MediaVersion,
		"image.png",
		0,
	); err == nil {
		t.Fatal("purged resource media remains in storage")
	}
}

func TestReferenceResourceGarbageCollectionRemovesRetiredUpdateVersion(
	t *testing.T,
) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceStorage: resourcestorage.Config{Directory: t.TempDir()},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resource-garbage-update")

	created, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "banner",
		Type:        "image",
		Payload:     json.RawMessage(`{"color":"red"}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{R: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	updated, err := service.Resource.Update(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
		Type:        "image",
		Payload:     json.RawMessage(`{"color":"blue"}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{B: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("update resource: %v", err)
	}

	if purged, err := service.Resource.CollectGarbage(
		ctx,
		resourceservice.CollectGarbageParams{Limit: 10},
	); err != nil ||
		purged != 0 {
		t.Fatalf(
			"retired version was purged before retention: purged=%d err=%v",
			purged,
			err,
		)
	}

	ageResourceMediaVersions(t, service, workspaceID, created.Key)

	purged, err := service.Resource.CollectGarbage(
		ctx,
		resourceservice.CollectGarbageParams{Limit: 10},
	)
	if err != nil || purged != 1 {
		t.Fatalf("collect garbage: purged=%d err=%v", purged, err)
	}

	if _, err := service.storage.ReadVersion(
		ctx,
		workspaceID,
		created.Key,
		created.MediaVersion,
		"image.png",
		0,
	); err == nil {
		t.Fatal("retired media version remains in storage")
	}

	current, err := service.Resource.Get(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
	})
	if err != nil || current.MediaVersion != updated.MediaVersion {
		t.Fatalf("current resource after GC: value=%+v err=%v", current, err)
	}

	if _, err := service.storage.ReadVersion(
		ctx,
		workspaceID,
		created.Key,
		updated.MediaVersion,
		"image.png",
		0,
	); err != nil {
		t.Fatalf("current media version was removed: %v", err)
	}
}

func TestReferenceResourceGarbageCollectionWorkerHandlesDeleteTrigger(
	t *testing.T,
) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceGCInterval: time.Hour,
		ResourceStorage:    resourcestorage.Config{Directory: t.TempDir()},
	})
	service.startWorkers()

	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("resource-garbage-worker")

	created, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "banner",
		Type:        "image",
		Payload:     json.RawMessage(`{}`),
		IsActive:    true,
		File:        referencePNG(t, color.NRGBA{R: 255, A: 255}),
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if err := service.Resource.Delete(ctx, resourceservice.GetParams{
		WorkspaceID: workspaceID,
		Key:         created.Key,
	}); err != nil {
		t.Fatalf("soft delete resource: %v", err)
	}

	ageResourceMediaVersions(t, service, workspaceID, created.Key)

	service.gcTrigger <- struct{}{}

	deadline := time.Now().Add(3 * time.Second)

	for {
		_, err := service.Resource.Get(ctx, resourceservice.GetParams{
			WorkspaceID: workspaceID,
			Key:         created.Key,
		})
		if errors.Is(err, repository.ErrItemNotFound) {
			return
		}

		if err != nil {
			t.Fatalf("get resource during garbage collection: %v", err)
		}

		if time.Now().After(deadline) {
			t.Fatal("resource GC worker did not process delete trigger")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func waitForReferenceWorkspaceLock(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, minimum int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)

	for {
		var waiting int

		if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%pg_advisory_xact_lock%'`).Scan(&waiting); err != nil {
			t.Fatalf("inspect reference lock waiters: %v", err)
		}

		if waiting >= minimum {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf(
				"reference lock waiters = %d, want at least %d",
				waiting,
				minimum,
			)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func warmReferenceReads(
	t *testing.T,
	service *Reference,
	workspaceID, title string,
	version int,
) {
	t.Helper()

	ctx := context.Background()
	item, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceID,
		Key:         "stars",
		Locale:      "ru",
	})

	if err != nil || item.Localization == nil ||
		item.Localization.Title != title ||
		referencePayloadVersion(item.Payload) != version {
		t.Fatalf(
			"reference Get returned stale data: item=%+v err=%v",
			item,
			err,
		)
	}

	resolved, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceID,
		Keys:        []string{"stars"},
		Locale:      "ru",
	})
	if err != nil || len(resolved.Items) != 1 ||
		resolved.Items[0].Localization == nil ||
		resolved.Items[0].Localization.Title != title ||
		referencePayloadVersion(resolved.Items[0].Payload) != version {
		t.Fatalf(
			"reference Resolve returned stale data: result=%+v err=%v",
			resolved,
			err,
		)
	}

	adminItem, err := service.Admin.GetItem(ctx, workspaceID, "stars")
	if err != nil || adminItem.Localizations[0].Title != title ||
		referencePayloadVersion(adminItem.Payload) != version {
		t.Fatalf(
			"reference admin GetItem returned stale data: item=%+v err=%v",
			adminItem,
			err,
		)
	}
}

func referencePayloadVersion(payload json.RawMessage) int {
	var value struct {
		Version int `json:"version"`
	}

	if json.Unmarshal(payload, &value) != nil {
		return 0
	}

	return value.Version
}

func ageResourceMediaVersions(
	t testing.TB,
	service *Reference,
	workspaceID, resourceKey string,
) {
	t.Helper()

	result, err := service.client.DB().ExecContext(context.Background(), `
UPDATE reference_resource_media_version
SET retired_at = now() - INTERVAL '1 hour 1 second'
WHERE workspace_id = $1 AND resource_key = $2 AND retired_at IS NOT NULL`, workspaceID, resourceKey)
	if err != nil {
		t.Fatalf("age resource media versions: %v", err)
	}

	if rows, err := result.RowsAffected(); err != nil || rows == 0 {
		t.Fatalf("aged resource media versions = %d err=%v", rows, err)
	}
}

func referencePNG(t testing.TB, fill color.NRGBA) []byte {
	t.Helper()

	image := image.NewNRGBA(image.Rect(0, 0, 4, 4))

	for y := range 4 {
		for x := range 4 {
			image.SetNRGBA(x, y, fill)
		}
	}

	var result bytes.Buffer

	if err := png.Encode(&result, image); err != nil {
		t.Fatal(err)
	}

	return result.Bytes()
}

type referenceSharedCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type referenceSharedCache struct {
	mu      sync.Mutex
	entries map[string]referenceSharedCacheEntry
}

func newReferenceSharedCache() *referenceSharedCache {
	return &referenceSharedCache{
		entries: make(map[string]referenceSharedCacheEntry),
	}
}

func (c *referenceSharedCache) GetWithTTL(
	key string,
) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if !exists ||
		(!entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt)) {
		delete(c.entries, key)

		return nil, 0, nil
	}

	return append([]byte(nil), entry.value...), time.Until(entry.expiresAt), nil
}

func (c *referenceSharedCache) Set(
	key string,
	value []byte,
	expiration time.Duration,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := referenceSharedCacheEntry{value: append([]byte(nil), value...)}
	if expiration > 0 {
		entry.expiresAt = time.Now().Add(expiration)
	}

	c.entries[key] = entry

	return nil
}

func (c *referenceSharedCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)

	return nil
}

func (c *referenceSharedCache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	clear(c.entries)

	return nil
}

func (c *referenceSharedCache) Close() error { return nil }

var _ Storage = (*referenceSharedCache)(nil)

const (
	referenceTestPGHost     = "localhost"
	referenceTestPGPort     = 5432
	referenceTestPGUser     = "postgres"
	referenceTestPGPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	referenceTestDB         = "reference_test"
)

func TestReferenceFullLifecycle(t *testing.T) {
	service := newReferenceTestService(t)
	ctx := context.Background()
	workspaceA := testsupport.WorkspaceID("workspace-a")
	workspaceB := testsupport.WorkspaceID("workspace-b")

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceA,
		Key:         "Coin",
		Type:        repository.ItemTypeQuantity,
		Payload: json.RawMessage(
			`{"icon":"coin.png","decimals":0}`,
		),
		IsActive: true,
	}); err != nil {
		t.Fatalf("create quantity item: %v", err)
	}

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceA,
		Key:         "premium",
		Type:        repository.ItemTypeDuration,
		Payload:     json.RawMessage(`{"icon":"premium.png"}`),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("create duration item: %v", err)
	}

	for _, localization := range []admin.SaveLocalizationParams{
		{WorkspaceID: workspaceA, ItemKey: "coin", Locale: "ru", Title: "Монеты", Description: "Игровая валюта"},
		{WorkspaceID: workspaceA, ItemKey: "coin", Locale: "en", Title: "Coins", Description: "Game currency"},
		{WorkspaceID: workspaceA, ItemKey: "premium", Locale: "ru", Title: "Премиум", Description: "Премиум-доступ"},
	} {
		if err := service.Admin.UpsertLocalization(
			ctx,
			localization,
		); err != nil {
			t.Fatalf("upsert localization: %v", err)
		}
	}

	localization, err := service.Admin.GetLocalization(
		ctx,
		workspaceA,
		"coin",
		"ru",
	)
	if err != nil || localization.Title != "Монеты" {
		t.Fatalf("get localization: value=%+v err=%v", localization, err)
	}

	localizations, err := service.Admin.ListLocalizations(
		ctx,
		workspaceA,
		"coin",
	)
	if err != nil || len(localizations) != 2 {
		t.Fatalf("list localizations: values=%+v err=%v", localizations, err)
	}

	items, err := service.Admin.ListItems(ctx, admin.ItemListParams{
		WorkspaceID:    workspaceA,
		Type:           repository.ItemTypeQuantity,
		OnlyNotDeleted: true,
		Page:           admin.Page{Limit: 10},
	})
	if err != nil || len(items) != 1 || items[0].Key != "coin" {
		t.Fatalf("list quantity items: values=%+v err=%v", items, err)
	}

	if _, err := service.Admin.ListItems(ctx, admin.ItemListParams{
		WorkspaceID: workspaceA,
		Type:        "unknown",
	}); !errors.Is(err, admin.ErrItemTypeFilterInvalid) {
		t.Fatalf("invalid item type filter error = %v", err)
	}

	coin, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceA, Key: " COIN ", Locale: "ru",
	})
	if err != nil {
		t.Fatalf("get coin: %v", err)
	}

	if coin.Key != "coin" || coin.Type != repository.ItemTypeQuantity ||
		coin.Localization == nil || coin.Localization.Title != "Монеты" {
		t.Fatalf("unexpected coin: %#v", coin)
	}

	resolved, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceA,
		Keys:        []string{"premium", "missing", "coin", "PREMIUM"},
		Locale:      "ru",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if len(resolved.Items) != 2 || resolved.Items[0].Key != "premium" ||
		resolved.Items[1].Key != "coin" || len(resolved.MissingKeys) != 1 ||
		resolved.MissingKeys[0] != "missing" {
		t.Fatalf("unexpected resolve result: %#v", resolved)
	}

	if _, err := service.Admin.UpdateItem(ctx, admin.UpdateItemParams{
		WorkspaceID: workspaceA,
		Key:         "coin",
		Payload: json.RawMessage(
			`{"icon":"coin-v2.png","decimals":0}`,
		),
		IsActive: true,
	}); err != nil {
		t.Fatalf("update item: %v", err)
	}

	updated, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceA, Key: "coin", Locale: "ru",
	})
	if err != nil || !strings.Contains(string(updated.Payload), "coin-v2.png") {
		t.Fatalf("updated cached item: %#v err=%v", updated, err)
	}

	adminItem, err := service.Admin.GetItem(ctx, workspaceA, "coin")
	if err != nil || len(adminItem.Localizations) != 2 {
		t.Fatalf("admin item: %#v err=%v", adminItem, err)
	}

	if changed, err := service.Admin.DeleteLocalization(
		ctx,
		workspaceA,
		"coin",
		"en",
	); err != nil ||
		changed != 1 {
		t.Fatalf("delete localization: changed=%d err=%v", changed, err)
	}

	localizations, err = service.Admin.ListLocalizations(
		ctx,
		workspaceA,
		"coin",
	)
	if err != nil || len(localizations) != 1 ||
		localizations[0].Locale != "ru" {
		t.Fatalf(
			"localizations after delete: values=%+v err=%v",
			localizations,
			err,
		)
	}

	stats, err := service.Admin.GetStats(ctx, workspaceA)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.ItemsTotal != 2 || stats.ActiveItems != 2 ||
		stats.QuantityItems != 1 || stats.DurationItems != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceB, Key: "coin", Type: repository.ItemTypeDuration,
		Payload: json.RawMessage(`{"workspace":"b"}`), IsActive: true,
	}); err != nil {
		t.Fatalf("create isolated item: %v", err)
	}

	isolated, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceB, Key: "coin", Locale: "ru",
	})
	if err != nil || isolated.Type != repository.ItemTypeDuration {
		t.Fatalf("workspace isolation: %#v err=%v", isolated, err)
	}

	if _, err := service.Admin.SoftDeleteItem(
		ctx,
		workspaceA,
		"coin",
	); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceA, Key: "coin", Locale: "ru",
	}); !errors.Is(err, repository.ErrItemNotFound) {
		t.Fatalf("deleted item must be hidden, err=%v", err)
	}

	if _, err := service.Admin.RestoreItem(
		ctx,
		workspaceA,
		"coin",
		true,
	); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if _, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceA, Key: "coin", Locale: "ru",
	}); err != nil {
		t.Fatalf("restored item: %v", err)
	}
}

func TestReferenceImportExportCycle(t *testing.T) {
	service := newReferenceTestService(t)
	ctx := context.Background()
	exportWorkspaceID := testsupport.WorkspaceID("workspace-export")
	importWorkspaceID := testsupport.WorkspaceID("workspace-import")

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: exportWorkspaceID,
		Key:         "coin",
		Type:        repository.ItemTypeQuantity,
		Payload: json.RawMessage(
			`{"icon":"coin.png","scale":2}`,
		),
		IsActive: true,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}

	if err := service.Admin.UpsertLocalization(
		ctx,
		admin.SaveLocalizationParams{
			WorkspaceID: exportWorkspaceID, ItemKey: "coin", Locale: "ru",
			Title: "Монеты", Description: "Игровая валюта",
		},
	); err != nil {
		t.Fatalf("upsert localization: %v", err)
	}

	pkg, err := repository.New(service.client).Export(
		ctx,
		exportWorkspaceID,
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	preview, err := repository.New(service.client).
		PreviewImport(ctx, importWorkspaceID, pkg)
	if err != nil {
		t.Fatalf("preview import: %v", err)
	}

	if preview.Counts.Items != 1 || preview.Counts.Localizations != 1 ||
		len(preview.Conflicts) != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}

	result, err := repository.New(service.client).Import(
		ctx,
		importWorkspaceID,
		admin.ImportRequest{
			Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
		},
	)
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	if result.Imported.Items != 1 || result.Imported.Localizations != 1 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	imported, err := repository.New(service.client).Export(
		ctx,
		importWorkspaceID,
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export imported: %v", err)
	}

	if len(imported.Items) != 1 || imported.Items[0].Key != "coin" ||
		imported.Items[0].Localization["ru"].Title != "Монеты" ||
		!strings.Contains(string(imported.Items[0].Payload), "coin.png") {
		t.Fatalf("unexpected imported package: %+v", imported)
	}

	pkg.Items[0].Localization = nil
	if _, err := repository.New(service.client).Import(
		ctx,
		importWorkspaceID,
		admin.ImportRequest{
			Package:          pkg,
			ConflictStrategy: repository.ImportConflictUpdate,
		},
	); err != nil {
		t.Fatalf("replace imported item: %v", err)
	}

	replaced, err := repository.New(service.client).Export(
		ctx,
		importWorkspaceID,
		admin.ExportRequest{},
	)
	if err != nil {
		t.Fatalf("export replaced item: %v", err)
	}

	if len(replaced.Items) != 1 || len(replaced.Items[0].Localization) != 0 {
		t.Fatalf(
			"update_existing kept removed localizations: %+v",
			replaced.Items,
		)
	}
}

func TestReferenceImmutableKeyAndDangerousTypeChange(t *testing.T) {
	service := newReferenceTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace")

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceID,
		Key:         "fixed-key",
		Type:        repository.ItemTypeQuantity,
		Payload:     json.RawMessage(`{}`),
		IsActive:    true,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.Admin.UpsertLocalization(
		ctx,
		admin.SaveLocalizationParams{
			WorkspaceID: workspaceID,
			ItemKey:     "fixed-key",
			Locale:      "en",
			Title:       "Fixed",
		},
	); err != nil {
		t.Fatal(err)
	}

	params := admin.DangerousChangeTypeParams{
		WorkspaceID: workspaceID,
		Key:         "fixed-key",
		CurrentType: repository.ItemTypeQuantity,
		NewType:     repository.ItemTypeDuration,
	}
	if _, err := service.Admin.DangerousChangeType(
		ctx,
		params,
	); !errors.Is(
		err,
		admin.ErrTypeChangeNotConfirmed,
	) {
		t.Fatalf("unconfirmed change: %v", err)
	}

	params.Confirmation = admin.DangerousTypeConfirmation

	rows, err := service.Admin.DangerousChangeType(ctx, params)

	if err != nil || rows != 1 {
		t.Fatalf("dangerous type change: rows=%d err=%v", rows, err)
	}

	item, err := service.User.Get(ctx, user.GetParams{
		WorkspaceID: workspaceID, Key: "fixed-key", Locale: "en",
	})
	if err != nil || item.Type != repository.ItemTypeDuration ||
		item.Localization == nil || item.Localization.Title != "Fixed" {
		t.Fatalf("changed item: %#v err=%v", item, err)
	}

	params.CurrentType = repository.ItemTypeQuantity
	rows, err = service.Admin.DangerousChangeType(ctx, params)

	if err != nil || rows != 0 {
		t.Fatalf(
			"stale expected type must not update: rows=%d err=%v",
			rows,
			err,
		)
	}
}

func TestReferenceValidationAndContext(t *testing.T) {
	service := newReferenceTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace")
	cases := []admin.SaveItemParams{
		{
			WorkspaceID: "",
			Key:         "coin",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
		},
		{
			WorkspaceID: workspaceID,
			Key:         "bad key",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
		},
		{
			WorkspaceID: workspaceID,
			Key:         "coin",
			Type:        "unknown",
			Payload:     json.RawMessage(`{}`),
		},
		{
			WorkspaceID: workspaceID,
			Key:         "coin",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{`),
		},
	}

	for _, params := range cases {
		if err := service.Admin.CreateItem(ctx, params); err == nil {
			t.Fatalf("expected validation error for %#v", params)
		}
	}

	if _, err := service.User.Resolve(
		ctx,
		user.ResolveParams{WorkspaceID: workspaceID},
	); !errors.Is(
		err,
		user.ErrKeysRequired,
	) {
		t.Fatalf("empty resolve: %v", err)
	}

	tooMany := make([]string, 1001)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("item.%d", index)
	}

	if _, err := service.User.Resolve(ctx, user.ResolveParams{
		WorkspaceID: workspaceID, Keys: tooMany,
	}); !errors.Is(err, user.ErrTooManyKeys) {
		t.Fatalf("oversized resolve: %v", err)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := service.User.List(
		canceled,
		user.ListParams{
			WorkspaceID: workspaceID,
			Locale:      "en",
			Page:        user.Page{},
		},
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("canceled request: %v", err)
	}
}

func TestReferenceOpenBootstrapsSchema(t *testing.T) {
	const database = "reference_open_test"

	ctx := context.Background()

	adminDB, err := openReferencePostgres("")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}

	terminateReferenceConnections(ctx, t, adminDB, database)

	if _, err := adminDB.ExecContext(
		ctx,
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", database),
	); err != nil {
		t.Fatalf("drop database: %v", err)
	}

	if _, err := adminDB.ExecContext(
		ctx,
		fmt.Sprintf("CREATE DATABASE %s", database),
	); err != nil {
		t.Fatalf("create database: %v", err)
	}

	_ = adminDB.Close()

	t.Cleanup(func() {
		adminDB, err := openReferencePostgres("")
		if err == nil {
			terminateReferenceConnections(
				context.Background(),
				t,
				adminDB,
				database,
			)

			_, _ = adminDB.ExecContext(
				context.Background(),
				fmt.Sprintf("DROP DATABASE IF EXISTS %s", database),
			)
			_ = adminDB.Close()
		}
	})

	db, err := openReferencePostgres(database)
	if err != nil {
		t.Fatalf("open reference database: %v", err)
	}
	defer db.Close()

	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("create reference sql client: %v", err)
	}

	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap reference: %v", err)
	}

	defer repo.Close()

	service, err := NewWithDatabase(
		ctx,
		db,
		referenceTestOptions(Options{CacheSize: 100}),
	)
	if err != nil {
		t.Fatalf("create reference: %v", err)
	}
	defer service.Close()

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: testsupport.WorkspaceID(
			"workspace",
		),
		Key:      "coin",
		Type:     repository.ItemTypeQuantity,
		Payload:  json.RawMessage(`{}`),
		IsActive: true,
	}); err != nil {
		t.Fatalf("schema was not bootstrapped: %v", err)
	}
}

func TestReferenceArchiveJobsExportDownloadAndImport(t *testing.T) {
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceStorage: resourcestorage.Config{Directory: t.TempDir()},
	})
	ctx := context.Background()
	sourceWorkspace := testsupport.WorkspaceID("archive-jobs-source")

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: sourceWorkspace,
		Key:         "coin",
		Type:        repository.ItemTypeQuantity,
		Payload:     json.RawMessage(`{"value": 1}`),
		IsActive:    true,
	}); err != nil {
		t.Fatal(err)
	}

	export, err := service.Admin.QueueArchiveExport(
		ctx,
		admin.QueueArchiveExportParams{
			WorkspaceID: sourceWorkspace,
			FileName:    "reference.zip",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	export = waitForArchiveJob(t, service, sourceWorkspace, export.ID)
	if export.Status != "completed" {
		t.Fatalf("export status = %q: %s", export.Status, export.Error)
	}

	dump, _, err := service.Admin.DownloadArchive(
		ctx,
		sourceWorkspace,
		export.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	data, err := io.ReadAll(dump)

	_ = dump.Close()

	if err != nil || len(data) == 0 {
		t.Fatalf("download archive: bytes=%d err=%v", len(data), err)
	}

	destinationWorkspace := testsupport.WorkspaceID("archive-jobs-destination")

	imported, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID: destinationWorkspace,
			FileName:    "reference.zip",
			Archive:     bytes.NewReader(data),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	imported = waitForArchiveJob(t, service, destinationWorkspace, imported.ID)
	if imported.Status != "completed" {
		t.Fatalf("import status = %q: %s", imported.Status, imported.Error)
	}

	if _, err := service.User.Get(
		ctx,
		user.GetParams{
			WorkspaceID: destinationWorkspace,
			Key:         "coin",
			Locale:      "en",
		},
	); err != nil {
		t.Fatalf("imported item: %v", err)
	}

	history, err := service.Admin.ArchiveJobHistory(
		ctx,
		sourceWorkspace,
		export.ID,
		admin.Page{},
	)
	if err != nil || len(history) < 2 {
		t.Fatalf("archive history: entries=%d err=%v", len(history), err)
	}
}

func TestReferenceArchiveJobsMediaRoundTripInWorkspace(t *testing.T) {
	storageDirectory := t.TempDir()
	service := newReferenceTestServiceWithOptions(t, referenceTestDB, Options{
		ResourceStorage: resourcestorage.Config{Directory: storageDirectory},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("archive-jobs-media-round-trip")
	fixtures := filepath.Join("testdata", "archive-media")

	original, err := os.ReadFile(filepath.Join(fixtures, "original.json"))
	if err != nil {
		t.Fatalf("read original fixture: %v", err)
	}

	preview, err := os.ReadFile(filepath.Join(fixtures, "preview-512.webp"))
	if err != nil {
		t.Fatalf("read WebP fixture: %v", err)
	}

	placeholder, err := os.ReadFile(filepath.Join(fixtures, "placeholder.svg"))
	if err != nil {
		t.Fatalf("read placeholder fixture: %v", err)
	}

	if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
		WorkspaceID: workspaceID,
		Key:         "animation",
		Type:        repository.ItemTypeQuantity,
		Payload:     json.RawMessage(`{}`),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("create item: %v", err)
	}

	created, err := service.Resource.Create(ctx, resourceservice.SaveParams{
		WorkspaceID: workspaceID,
		Key:         "animation",
		Type:        "image",
		Payload:     json.RawMessage(`{"fixture":"lottie.json"}`),
		IsActive:    true,
		File:        original,
		FirstFrame:  preview,
	})
	if err != nil {
		t.Fatalf("create WebP resource: %v", err)
	}

	if created.Format != "lottie" {
		t.Fatalf("created resource format = %q, want lottie", created.Format)
	}

	if err := os.WriteFile(
		filepath.Join(
			storageDirectory,
			filepath.FromSlash(created.PlaceholderRef),
		),
		placeholder,
		0o640,
	); err != nil {
		t.Fatalf("replace generated placeholder fixture: %v", err)
	}

	if err := service.Resource.Attach(
		ctx,
		workspaceID,
		"animation",
		created.Key,
		0,
	); err != nil {
		t.Fatalf("attach resource: %v", err)
	}

	export, err := service.Admin.QueueArchiveExport(
		ctx,
		admin.QueueArchiveExportParams{
			WorkspaceID:  workspaceID,
			FileName:     "media-round-trip.zip",
			IncludeMedia: true,
		},
	)
	if err != nil {
		t.Fatalf("queue export: %v", err)
	}

	export = waitForArchiveJob(t, service, workspaceID, export.ID)
	if export.Status != "completed" {
		t.Fatalf("export status = %q: %s", export.Status, export.Error)
	}

	dump, _, err := service.Admin.DownloadArchive(ctx, workspaceID, export.ID)
	if err != nil {
		t.Fatalf("download export: %v", err)
	}

	archiveData, err := io.ReadAll(dump)
	closeErr := dump.Close()

	if err != nil || closeErr != nil {
		t.Fatalf("read export: err=%v close=%v", err, closeErr)
	}

	archive, err := zip.NewReader(
		bytes.NewReader(archiveData),
		int64(len(archiveData)),
	)
	if err != nil {
		t.Fatalf("open export ZIP: %v", err)
	}

	entries := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		entries[file.Name] = file
	}

	for _, name := range []string{
		"manifest.json",
		"media/animation/lottie.json",
		"media/animation/placeholder.svg",
		"media/animation/preview-512.webp",
	} {
		if entries[name] == nil {
			t.Fatalf("export ZIP is missing %q", name)
		}
	}

	archivedOriginal := readReferenceZIPEntry(
		t,
		entries["media/animation/lottie.json"],
	)
	if !bytes.Equal(archivedOriginal, original) {
		t.Fatal("exported original does not match the Lottie fixture")
	}

	if archivedPlaceholder := readReferenceZIPEntry(
		t,
		entries["media/animation/placeholder.svg"],
	); !bytes.Equal(
		archivedPlaceholder,
		placeholder,
	) {
		t.Fatal("exported placeholder does not match the SVG fixture")
	}

	archivedPreview := readReferenceZIPEntry(
		t,
		entries["media/animation/preview-512.webp"],
	)

	if _, err := service.Admin.SoftDeleteItem(
		ctx,
		workspaceID,
		"animation",
	); err != nil {
		t.Fatalf("delete item: %v", err)
	}

	if err := service.Resource.Delete(
		ctx,
		resourceservice.GetParams{WorkspaceID: workspaceID, Key: created.Key},
	); err != nil {
		t.Fatalf("delete resource: %v", err)
	}

	ageResourceMediaVersions(t, service, workspaceID, created.Key)

	if purged, err := service.Resource.CollectGarbage(
		ctx,
		resourceservice.CollectGarbageParams{Limit: 10},
	); err != nil ||
		purged != 1 {
		t.Fatalf("purge deleted resource: purged=%d err=%v", purged, err)
	}

	if _, err := service.Resource.Get(
		ctx,
		resourceservice.GetParams{WorkspaceID: workspaceID, Key: created.Key},
	); !errors.Is(
		err,
		repository.ErrItemNotFound,
	) {
		t.Fatalf("deleted resource remains in database: %v", err)
	}

	if _, err := service.storage.ReadVersion(
		ctx,
		workspaceID,
		created.Key,
		created.MediaVersion,
		"image.webp",
		0,
	); err == nil {
		t.Fatal("deleted resource media remains in storage")
	}

	imported, err := service.Admin.QueueArchiveImport(
		ctx,
		admin.QueueArchiveImportParams{
			WorkspaceID:      workspaceID,
			FileName:         "media-round-trip.zip",
			IncludeMedia:     true,
			ConflictStrategy: repository.ImportConflictUpdate,
			Archive:          bytes.NewReader(archiveData),
		},
	)
	if err != nil {
		t.Fatalf("queue import: %v", err)
	}

	imported = waitForArchiveJob(t, service, workspaceID, imported.ID)
	if imported.Status != "completed" {
		t.Fatalf("import status = %q: %s", imported.Status, imported.Error)
	}

	if _, err := service.User.Get(
		ctx,
		user.GetParams{
			WorkspaceID: workspaceID,
			Key:         "animation",
			Locale:      "en",
		},
	); err != nil {
		t.Fatalf("restored item: %v", err)
	}

	restored, err := service.Resource.Get(
		ctx,
		resourceservice.GetParams{WorkspaceID: workspaceID, Key: created.Key},
	)
	if err != nil {
		t.Fatalf("restored resource: %v", err)
	}

	if restored.MediaVersion != created.MediaVersion {
		t.Fatalf(
			"restored media version = %q, want %q",
			restored.MediaVersion,
			created.MediaVersion,
		)
	}

	links, err := service.Resource.ListItemResources(
		ctx,
		workspaceID,
		"animation",
	)
	if err != nil || len(links) != 1 || links[0].Key != created.Key {
		t.Fatalf("restored resource links = %+v, err = %v", links, err)
	}

	content, err := service.Resource.GetContent(
		ctx,
		resourceservice.ContentParams{
			WorkspaceID: workspaceID,
			Key:         restored.Key,
			Version:     restored.MediaVersion,
			Format:      restored.Format,
		},
	)
	if err != nil {
		t.Fatalf("read restored original: %v", err)
	}

	if !bytes.Equal(content.Data, original) {
		t.Fatal("restored original does not match the Lottie fixture")
	}

	restoredPreview, err := service.Resource.GetContent(
		ctx,
		resourceservice.ContentParams{
			WorkspaceID: workspaceID,
			Key:         restored.Key,
			Version:     restored.MediaVersion,
			Format:      restored.Format,
			Size:        512,
		},
	)
	if err != nil {
		t.Fatalf("read restored preview: %v", err)
	}

	if !bytes.Equal(restoredPreview.Data, archivedPreview) {
		t.Fatal("restored preview does not match the exported preview")
	}

	restoredPlaceholder, err := service.storage.Read(
		ctx,
		restored.PlaceholderRef,
	)
	if err != nil {
		t.Fatalf("read restored placeholder: %v", err)
	}

	if !bytes.Equal(restoredPlaceholder, placeholder) {
		t.Fatal("restored placeholder does not match the SVG fixture")
	}
}

func readReferenceZIPEntry(t testing.TB, file *zip.File) []byte {
	t.Helper()

	reader, err := file.Open()
	if err != nil {
		t.Fatalf("open ZIP entry %q: %v", file.Name, err)
	}

	data, err := io.ReadAll(reader)
	closeErr := reader.Close()

	if err != nil || closeErr != nil {
		t.Fatalf("read ZIP entry %q: err=%v close=%v", file.Name, err, closeErr)
	}

	return data
}

func waitForArchiveJob(
	t *testing.T,
	service *Reference,
	workspaceID string,
	id int64,
) admin.ArchiveJob {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.Admin.ArchiveJob(
			context.Background(),
			workspaceID,
			id,
		)
		if err != nil {
			t.Fatal(err)
		}

		if job.Status == "completed" || job.Status == "failed" {
			return job
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("archive job did not finish")

	return admin.ArchiveJob{}
}

func newReferenceTestService(t testing.TB) *Reference {
	return newReferenceTestServiceWithOptions(t, referenceTestDB, Options{})
}

func newReferenceTestServiceWithOptions(
	t testing.TB,
	database string,
	options Options,
) *Reference {
	t.Helper()

	ctx := context.Background()

	adminDB, err := openReferencePostgres("")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}

	terminateReferenceConnections(ctx, t, adminDB, database)

	if _, err := adminDB.ExecContext(
		ctx,
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", database),
	); err != nil {
		t.Fatalf("drop database: %v", err)
	}

	if _, err := adminDB.ExecContext(
		ctx,
		fmt.Sprintf("CREATE DATABASE %s", database),
	); err != nil {
		t.Fatalf("create database: %v", err)
	}

	_ = adminDB.Close()

	db, err := openReferencePostgres(database)
	if err != nil {
		t.Fatalf("open app postgres: %v", err)
	}

	client, err := sqlwrap.New(db, sqlwrap.Options{
		CacheEnabled: true, CacheSize: 10000, CacheTTLCheck: time.Minute,
	})
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}

	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap reference: %v", err)
	}

	service, err := NewWithDatabase(ctx, db, referenceTestOptions(options))
	if err != nil {
		t.Fatalf("create reference service: %v", err)
	}

	t.Cleanup(func() {
		_ = service.Close()
		_ = repo.Close()
		_ = client.Close()
	})

	return service
}

func referenceTestOptions(options Options) Options {
	options.CacheEnabled = true
	if options.CacheSize == 0 {
		options.CacheSize = 10000
	}

	if options.CacheTTLCheck == 0 {
		options.CacheTTLCheck = time.Minute
	}

	if options.CacheL1Delay == 0 {
		options.CacheL1Delay = time.Minute
	}

	return options
}

func terminateReferenceConnections(
	ctx context.Context,
	t testing.TB,
	db *sql.DB,
	database string,
) {
	t.Helper()

	_, err := db.ExecContext(ctx, `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1 AND pid <> pg_backend_pid()`, database)
	if err != nil {
		t.Fatalf("terminate postgres connections: %v", err)
	}
}

func openReferencePostgres(database string) (*sql.DB, error) {
	if database == "" {
		database = "postgres"
	}

	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		referenceTestPGHost,
		referenceTestPGPort,
		referenceTestPGUser,
		referenceTestPGPassword,
		database,
	)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}
