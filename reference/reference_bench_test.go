package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"testing"
	"time"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/reference/repository"
	"github.com/elum2b/services/reference/service/admin"
	resourceservice "github.com/elum2b/services/reference/service/resource"
	"github.com/elum2b/services/reference/service/user"
	resourcestorage "github.com/elum2b/services/reference/storage"
)

var (
	referenceBenchmarkWorkspace = testsupport.WorkspaceID(
		"reference-benchmark",
	)
	referenceBenchmarkImportWorkspace = testsupport.WorkspaceID(
		"reference-benchmark-import",
	)
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
			WorkspaceID: referenceBenchmarkWorkspace,
			Key:         key,
			Type:        itemType,
			Payload: json.RawMessage(
				fmt.Sprintf(`{"position":%d}`, index),
			),
			IsActive: true,
		}); err != nil {
			b.Fatal(err)
		}

		if err := service.Admin.UpsertLocalization(
			ctx,
			admin.SaveLocalizationParams{
				WorkspaceID: referenceBenchmarkWorkspace,
				ItemKey:     key,
				Locale:      "ru",
				Title:       "Item " + key,
				Description: "Benchmark item",
			},
		); err != nil {
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
				WorkspaceID: referenceBenchmarkWorkspace,
				Key:         "item.0500",
				Locale:      "ru",
			})
			benchError(b, err)
		}
	})
	b.Run("User.Resolve/100", func(b *testing.B) {
		for range b.N {
			_, err := service.User.Resolve(ctx, user.ResolveParams{
				WorkspaceID: referenceBenchmarkWorkspace,
				Keys:        resolveKeys,
				Locale:      "ru",
			})
			benchError(b, err)
		}
	})
	b.Run("User.List/100", func(b *testing.B) {
		for range b.N {
			_, err := service.User.List(
				ctx,
				user.ListParams{
					WorkspaceID: referenceBenchmarkWorkspace,
					Locale:      "ru",
					Page:        user.Page{Limit: 100},
				},
			)
			benchError(b, err)
		}
	})
	b.Run("Admin.GetItem", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetItem(
				ctx,
				referenceBenchmarkWorkspace,
				"item.0500",
			)
			benchError(b, err)
		}
	})
	b.Run("Admin.ListItems/100", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListItems(ctx, admin.ItemListParams{
				WorkspaceID:    referenceBenchmarkWorkspace,
				OnlyNotDeleted: true,
				Page:           admin.Page{Limit: 100},
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

		if err := serviceAdmin.UpsertLocalization(
			ctx,
			admin.SaveLocalizationParams{
				WorkspaceID: referenceBenchmarkWorkspace,
				ItemKey:     "item.0500",
				Locale:      "ru",
				Title:       "Item item.0500",
				Description: "Benchmark item",
			},
		); err != nil {
			b.Fatal(err)
		}
	}

	b.Run("no_cache", func(b *testing.B) {
		service := newReferenceTestServiceWithOptions(
			b,
			"reference_bench_get_no_cache",
			Options{},
		)
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
		service := newReferenceTestServiceWithOptions(
			b,
			"reference_bench_get_l1_cache",
			Options{
				CacheEnabled: true,
				CacheSize:    10000,
				CacheL1Delay: time.Minute,
			},
		)
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
			WorkspaceID: referenceBenchmarkImportWorkspace,
			Key:         key,
			Type:        repository.ItemTypeQuantity,
			Payload: json.RawMessage(
				fmt.Sprintf(`{"position":%d}`, index),
			),
			IsActive: true,
		}); err != nil {
			b.Fatal(err)
		}

		if err := service.Admin.UpsertLocalization(
			ctx,
			admin.SaveLocalizationParams{
				WorkspaceID: referenceBenchmarkImportWorkspace,
				ItemKey:     key,
				Locale:      "ru",
				Title:       "Item " + key,
				Description: "Benchmark item",
			},
		); err != nil {
			b.Fatal(err)
		}
	}

	pkg, err := service.Admin.Export(
		ctx,
		referenceBenchmarkImportWorkspace,
		admin.ExportRequest{},
	)
	benchError(b, err)
	b.ReportAllocs()
	b.Run("Export", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Export(
				ctx,
				referenceBenchmarkImportWorkspace,
				admin.ExportRequest{},
			)
			benchError(b, err)
		}
	})
	b.Run("Import/update", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.Import(
				ctx,
				referenceBenchmarkImportWorkspace,
				admin.ImportRequest{
					Package:          pkg,
					ConflictStrategy: repository.ImportConflictUpdate,
				},
			)
			benchError(b, err)
		}
	})
}

func BenchmarkReferenceResourceMethods(b *testing.B) {
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("reference-resource-benchmark")
	file := referencePNG(b, color.NRGBA{R: 255, G: 64, B: 32, A: 255})

	newService := func(b *testing.B, database string, options Options) *Reference {
		b.Helper()

		options.ResourceStorage = resourcestorage.Config{Directory: b.TempDir()}

		return newReferenceTestServiceWithOptions(b, database, options)
	}
	create := func(b *testing.B, service *Reference, key string) {
		b.Helper()

		_, err := service.Resource.Create(ctx, resourceservice.SaveParams{
			WorkspaceID: workspaceID,
			Key:         key,
			Type:        "image",
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
			File:        file,
		})
		benchError(b, err)
	}

	b.Run("Resource.Get/l1_cache_warm", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_get_l1", Options{
			CacheEnabled: true,
			CacheL1Delay: time.Minute,
			CacheL2Delay: -1,
		})
		create(b, service, "resource")
		_, err := service.Resource.Get(ctx, resourceservice.GetParams{
			WorkspaceID: workspaceID,
			Key:         "resource",
		})
		benchError(b, err)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_, err := service.Resource.Get(ctx, resourceservice.GetParams{
				WorkspaceID: workspaceID,
				Key:         "resource",
			})
			benchError(b, err)
		}
	})

	b.Run("Resource.Get/l2_cache_warm", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_get_l2", Options{
			Cache:        newReferenceSharedCache(),
			CacheEnabled: true,
			CacheL1Delay: -1,
			CacheL2Delay: time.Minute,
		})
		create(b, service, "resource")
		_, err := service.Resource.Get(ctx, resourceservice.GetParams{
			WorkspaceID: workspaceID,
			Key:         "resource",
		})
		benchError(b, err)
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_, err := service.Resource.Get(ctx, resourceservice.GetParams{
				WorkspaceID: workspaceID,
				Key:         "resource",
			})
			benchError(b, err)
		}
	})

	b.Run("Resource.List/1", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_list", Options{})
		create(b, service, "resource")
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_, err := service.Resource.List(ctx, resourceservice.ListParams{
				WorkspaceID: workspaceID,
				Limit:       1,
			})
			benchError(b, err)
		}
	})

	b.Run("Resource.ListItemResources/1", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_item_list", Options{})
		create(b, service, "resource")
		if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: workspaceID,
			Key:         "item",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
		}); err != nil {
			b.Fatal(err)
		}
		benchError(b, service.Resource.Attach(ctx, workspaceID, "item", "resource", 1))
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_, err := service.Resource.ListItemResources(ctx, workspaceID, "item")
			benchError(b, err)
		}
	})

	b.Run("Resource.Create", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_create", Options{})
		b.ReportAllocs()
		b.ResetTimer()

		for index := range b.N {
			create(b, service, fmt.Sprintf("resource.%d", index))
		}
	})

	b.Run("Resource.Update", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_update", Options{})
		create(b, service, "resource")
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			_, err := service.Resource.Update(ctx, resourceservice.SaveParams{
				WorkspaceID: workspaceID,
				Key:         "resource",
				Type:        "image",
				Payload:     json.RawMessage(`{}`),
				IsActive:    true,
				File:        file,
			})
			benchError(b, err)
		}
	})

	b.Run("Resource.AttachDetach", func(b *testing.B) {
		service := newService(b, "reference_bench_resource_attach_detach", Options{})
		create(b, service, "resource")
		if err := service.Admin.CreateItem(ctx, admin.SaveItemParams{
			WorkspaceID: workspaceID,
			Key:         "item",
			Type:        repository.ItemTypeQuantity,
			Payload:     json.RawMessage(`{}`),
			IsActive:    true,
		}); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()

		for range b.N {
			benchError(b, service.Resource.Attach(ctx, workspaceID, "item", "resource", 1))
			_, err := service.Resource.Detach(ctx, workspaceID, "item", "resource")
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
