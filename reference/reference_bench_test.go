package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	"github.com/elum2b/services/reference/service/user"
	"testing"
	"time"
)

var (
	referenceBenchmarkWorkspace       = testsupport.WorkspaceID("reference-benchmark")
	referenceBenchmarkImportWorkspace = testsupport.WorkspaceID("reference-benchmark-import")
)

func BenchmarkReferenceServiceMethods(b *testing.B) {
	service := newReferenceTestService(b)
	ctx := context.Background()
	for index := range 1000 {
		key := fmt.Sprintf("item.%04d", index)
		itemType := repository.ItemTypeQuantity
		if index%2 == 1 {
			itemType = repository.ItemTypeDuration
		}
		if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: referenceBenchmarkWorkspace, Key: key, Type: itemType,
			Payload: json.RawMessage(fmt.Sprintf(`{"position":%d}`, index)), IsActive: true,
		}); err != nil {
			b.Fatal(err)
		}
		if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
			WorkspaceID: referenceBenchmarkWorkspace, ItemKey: key, Locale: "ru",
			Title: "Item " + key, Description: "Benchmark item",
		}); err != nil {
			b.Fatal(err)
		}
	}
	resolveKeys := make([]string, 0, 100)
	for index := range 100 {
		resolveKeys = append(resolveKeys, fmt.Sprintf("item.%04d", index))
	}

	b.ReportAllocs()
	b.Run("User.Get", func(b *testing.B) {
		for range b.N {
			_, err := service.User.Get(ctx, user.GetParams{
				WorkspaceID: referenceBenchmarkWorkspace, Key: "item.0500", Locale: "ru",
			})
			benchError(b, err)
		}
	})
	b.Run("User.Resolve/100", func(b *testing.B) {
		for range b.N {
			_, err := service.User.Resolve(ctx, user.ResolveParams{
				WorkspaceID: referenceBenchmarkWorkspace, Keys: resolveKeys, Locale: "ru",
			})
			benchError(b, err)
		}
	})
	b.Run("User.List/100", func(b *testing.B) {
		for range b.N {
			_, err := service.User.List(ctx, user.ListParams{WorkspaceID: referenceBenchmarkWorkspace, Locale: "ru", Page: user.Page{Limit: 100}})
			benchError(b, err)
		}
	})
	b.Run("Admin.GetItem", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetItem(ctx, referenceBenchmarkWorkspace, "item.0500")
			benchError(b, err)
		}
	})
	b.Run("Admin.ListItems/100", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListItems(ctx, admin.ItemListParams{
				WorkspaceID: referenceBenchmarkWorkspace, OnlyNotDeleted: true, Page: admin.Page{Limit: 100},
			})
			benchError(b, err)
		}
	})
	b.Run("Admin.GetStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetStats(ctx, referenceBenchmarkWorkspace)
			benchError(b, err)
		}
	})
}

func BenchmarkReferenceUserGetCacheModes(b *testing.B) {
	ctx := context.Background()

	prepare := func(b *testing.B, serviceAdmin *admin.Admin) {
		b.Helper()
		if err := serviceAdmin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: referenceBenchmarkWorkspace,
			Key:         "item.0500",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{"position":500}`),
			IsActive:    true,
		}); err != nil {
			b.Fatal(err)
		}
		if err := serviceAdmin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
			WorkspaceID: referenceBenchmarkWorkspace,
			ItemKey:     "item.0500",
			Locale:      "ru",
			Title:       "Item item.0500",
			Description: "Benchmark item",
		}); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("no_cache", func(b *testing.B) {
		service := newReferenceTestServiceWithOptions(b, "reference_bench_get_no_cache", Options{})
		prepare(b, service.Admin)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, err := service.User.Get(ctx, user.GetParams{
				WorkspaceID: referenceBenchmarkWorkspace,
				Key:         "item.0500",
				Locale:      "ru",
			})
			benchError(b, err)
		}
	})

	b.Run("l1_cache_warm", func(b *testing.B) {
		service := newReferenceTestServiceWithOptions(b, "reference_bench_get_l1_cache", Options{
			CacheEnabled: true,
			CacheSize:    10000,
			CacheL1Delay: time.Minute,
		})
		prepare(b, service.Admin)
		_, err := service.User.Get(ctx, user.GetParams{
			WorkspaceID: referenceBenchmarkWorkspace,
			Key:         "item.0500",
			Locale:      "ru",
		})
		benchError(b, err)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, err := service.User.Get(ctx, user.GetParams{
				WorkspaceID: referenceBenchmarkWorkspace,
				Key:         "item.0500",
				Locale:      "ru",
			})
			benchError(b, err)
		}
	})
}

func BenchmarkReferenceImportExport(b *testing.B) {
	service := newReferenceTestService(b)
	ctx := context.Background()
	for index := range 1000 {
		key := fmt.Sprintf("export.%04d", index)
		if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: referenceBenchmarkImportWorkspace, Key: key, Type: repository.ItemTypeQuantity,
			Payload: json.RawMessage(fmt.Sprintf(`{"position":%d}`, index)), IsActive: true,
		}); err != nil {
			b.Fatal(err)
		}
		if err := service.Admin.UpsertLocalization(ctx, admin.SaveLocalizationParams{
			WorkspaceID: referenceBenchmarkImportWorkspace, ItemKey: key, Locale: "ru",
			Title: "Item " + key, Description: "Benchmark item",
		}); err != nil {
			b.Fatal(err)
		}
	}
	pkg, err := service.Admin.Export(ctx, referenceBenchmarkImportWorkspace, admin.ExportRequest{})
	benchError(b, err)
	b.ReportAllocs()
	b.Run("Export", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Export(ctx, referenceBenchmarkImportWorkspace, admin.ExportRequest{})
			benchError(b, err)
		}
	})
	b.Run("Import/update", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Import(ctx, referenceBenchmarkImportWorkspace, admin.ImportRequest{
				Package: pkg, ConflictStrategy: repository.ImportConflictUpdate,
			})
			benchError(b, err)
		}
	})
}

func benchError(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}
