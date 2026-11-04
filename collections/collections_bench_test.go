package collections

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elum2b/services/collections/model"
	"github.com/elum2b/services/collections/service/admin"
)

func BenchmarkCollections(b *testing.B) {
	var sequence atomic.Uint64
	e := setup(b, Options{CacheEnabled: true, CacheL1Delay: time.Minute, CacheL2Delay: time.Minute})
	ctx := context.Background()
	c := model.Collection{
		Key:    "colors",
		Active: true,
	}
	for n := range 1000 {
		c.Entries = append(c.Entries, model.Entry{
			ItemKey:  fmt.Sprintf("item-%04d", n),
			Active:   true,
			Position: int32(n),
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
	must(b, err)
	first := e.set("seed", "item-0000", true)
	_, err = e.s.Internal.SetOwned(ctx, first)
	must(b, err)
	read := model.ReadParams{Identity: e.i}
	for _, name := range []string{"hit", "miss"} {
		b.Run("User.HasItem/"+name, func(b *testing.B) {
			key := "item-0000"
			if name == "miss" {
				key = "absent"
			}
			b.ReportAllocs()
			b.ReportMetric(1, "sql/op")
			for range b.N {
				result, err := e.s.User.HasItem(ctx, model.HasItemParams{
					ReadParams: read,
					ItemRef: model.ItemRef{
						CollectionKey: "colors",
						ItemKey:       key,
					},
				})
				must(b, err)
				if result.Owned != (name == "hit") {
					b.Fatal(result)
				}
			}
		})
	}
	for _, size := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("User.HasItems/%d", size), func(b *testing.B) {
			refs := make([]model.ItemRef, size)
			for n := range refs {
				refs[n] = model.ItemRef{
					CollectionKey: "colors",
					ItemKey:       c.Entries[n].ItemKey,
				}
			}
			b.ReportAllocs()
			b.ReportMetric(1, "sql/op")
			b.ResetTimer()
			for range b.N {
				states, err := e.s.User.HasItems(ctx, model.HasItemsParams{
					ReadParams: read,
					Items:      refs,
				})
				must(b, err)
				if len(states) != size {
					b.Fatal(len(states))
				}
			}
		})
	}
	b.Run("User.GetCollection", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.User.GetCollection(ctx, model.GetCollectionParams{
				ReadParams:    read,
				CollectionKey: "colors",
			})
			must(b, err)
		}
	})
	b.Run("User.ListCollections", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.User.ListCollections(ctx, read)
			must(b, err)
		}
	})
	b.Run("User.ListOwned", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.User.ListOwned(ctx, model.ListOwnedParams{ReadParams: read})
			must(b, err)
		}
	})
	b.Run("Admin.GetStats", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.Admin.GetStats(ctx, model.StatsParams{WorkspaceID: e.w})
			must(b, err)
		}
	})
	for _, branch := range []string{"change", "noop", "replay", "conflict"} {
		b.Run("Internal.SetOwned/"+branch, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				p := first
				switch branch {
				case "change":
					p.OperationID = fmt.Sprintf("%s-%d", b.Name(), sequence.Add(1))
					p.Identity.PlatformUserID = p.OperationID
				case "noop":
					p.OperationID = fmt.Sprintf("%s-%d", b.Name(), sequence.Add(1))
				case "conflict":
					p.Owned = false
				}
				_, err := e.s.Internal.SetOwned(ctx, p)
				if branch == "conflict" {
					if err == nil {
						b.Fatal("missing conflict")
					}
				} else {
					must(b, err)
				}
			}
		})
	}
	for _, size := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("Internal.SetOwnedBatch/%d", size), func(b *testing.B) {
			changes := make([]model.OwnershipChange, size)
			for n := range changes {
				changes[n] = model.OwnershipChange{
					ItemRef: model.ItemRef{
						CollectionKey: "colors",
						ItemKey:       c.Entries[n].ItemKey,
					},
					Owned: true,
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				i := e.i
				i.PlatformUserID = fmt.Sprintf("%s-%d", b.Name(), sequence.Add(1))
				_, err := e.s.Internal.SetOwnedBatch(ctx, model.SetOwnedBatchParams{
					Identity:    i,
					Source:      "bench",
					OperationID: "grant",
					Changes:     changes,
				})
				must(b, err)
			}
		})
	}

	for _, hit := range []bool{true, false} {
		b.Run(fmt.Sprintf("Admin.ListCatalog/cache_hit_%t", hit), func(b *testing.B) {
			p := model.CatalogParams{WorkspaceID: e.w}
			_, err := e.s.Admin.ListCatalog(ctx, p)
			must(b, err)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !hit {
					b.StopTimer()
					must(b, e.s.Admin.UpdateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "bench", Collection: model.Collection{Key: "colors", Active: true}}))
					b.StartTimer()
				}
				_, err := e.s.Admin.ListCatalog(ctx, p)
				must(b, err)
			}
		})
	}
	pkg, err := e.s.Admin.Export(ctx, admin.ExportParams{WorkspaceID: e.w})
	must(b, err)
	b.Run("Admin.Import", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.Admin.Import(ctx, admin.ImportParams{
				WorkspaceID: e.w,
				Request:     model.ImportRequest{ActorID: "operator", Package: pkg},
			})
			must(b, err)
		}
	})
	b.Run("Admin.Export", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			_, err := e.s.Admin.Export(ctx, admin.ExportParams{WorkspaceID: e.w})
			must(b, err)
		}
	})

	for _, shared := range []bool{false, true} {
		b.Run(fmt.Sprintf("Internal.SetOwnedParallel/shared_owner_%t", shared), func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					n := sequence.Add(1)
					p := e.set(fmt.Sprintf("parallel-%d", n), "item-0000", n%2 == 0)
					if !shared {
						p.Identity.PlatformUserID = p.OperationID
						p.Owned = true
					}
					_, err := e.s.Internal.SetOwned(ctx, p)
					if err != nil {
						b.Error(err)
					}
				}
			})
		})
	}
	b.Run("Admin.CreateCollection", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			must(b, e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{
				WorkspaceID: e.w,
				ActorID:     "bench",
				Collection: model.Collection{
					Key:    fmt.Sprintf("%s-%d", b.Name(), sequence.Add(1)),
					Active: true,
				},
			}))
		}
	})

}

func BenchmarkCollectionsLocaleRead(b *testing.B) {
	e := setup(b, Options{})
	ctx := context.Background()
	c := model.Collection{Key: "colors", Active: true, Localization: map[string]model.Text{}}
	for n := range 100 {
		c.Localization[fmt.Sprintf("l%02d", n)] = model.Text{Title: "title", Description: strings.Repeat("x", 65536)}
	}
	must(b, e.s.Admin.CreateCollection(ctx, model.SaveCollectionParams{WorkspaceID: e.w, ActorID: "operator", Collection: c}))
	read := model.ReadParams{Identity: e.i, Locale: "l00"}
	b.Run("ListCollections", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			rows, err := e.s.User.ListCollections(ctx, read)
			must(b, err)
			if len(rows) != 1 || len(rows[0].Text.Description) != 65536 {
				b.Fatal("wrong result")
			}
		}
	})
	b.Run("GetCollection", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			row, err := e.s.User.GetCollection(ctx, model.GetCollectionParams{ReadParams: read, CollectionKey: "colors"})
			must(b, err)
			if len(row.Text.Description) != 65536 {
				b.Fatal("wrong result")
			}
		}
	})
}
func BenchmarkCollectionsReorder(b *testing.B) {
	for _, size := range []int{20, 1001} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			e := setup(b, Options{})
			ctx := context.Background()
			c := model.Collection{Key: "colors", Active: true}
			keys := []string{}
			for n := range size {
				k := fmt.Sprint(n)
				keys = append(keys, k)
				c.Entries = append(c.Entries, model.Entry{ItemKey: k, Active: true})
			}
			_, err := e.s.Admin.Import(ctx, admin.ImportParams{WorkspaceID: e.w, Request: model.ImportRequest{ActorID: "operator", Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{c}}}})
			must(b, err)
			p := model.ReorderEntriesParams{WorkspaceID: e.w, ActorID: "operator", CollectionKey: "colors", ItemKeys: keys}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				must(b, e.s.Admin.ReorderEntries(ctx, p))
			}
		})
	}
}

func BenchmarkCollectionsPublishedCatalog(b *testing.B) {
	e := setup(b, Options{CacheEnabled: true})
	ctx := context.Background()
	// Keep the two planning scenarios deterministic in this isolated benchmark DB.
	_, err := e.db.ExecContext(ctx, "ALTER TABLE collections_collection SET (autovacuum_enabled=false); ALTER TABLE collections_entry SET (autovacuum_enabled=false); ALTER TABLE collections_ownership SET (autovacuum_enabled=false)")
	must(b, err)
	catalog := model.Collection{Key: "colors", Active: true}
	changes := make([]model.OwnershipChange, 0, 1000)
	for n := range 1000 {
		key := fmt.Sprintf("color-%04d", n)
		catalog.Entries = append(catalog.Entries, model.Entry{ItemKey: key, Active: true, Position: int32(n)})
		changes = append(changes, model.OwnershipChange{ItemRef: model.ItemRef{CollectionKey: "colors", ItemKey: key}, Owned: true})
	}
	_, err = e.s.Admin.Import(ctx, admin.ImportParams{
		WorkspaceID: e.w,
		Request: model.ImportRequest{
			ActorID: "operator",
			Package: model.ExportPackage{Format: model.ExportFormat, Service: "collections", Collections: []model.Collection{catalog}},
		},
	})
	must(b, err)
	_, err = e.s.Internal.SetOwnedBatch(ctx, model.SetOwnedBatchParams{Identity: e.i, Source: "bench", OperationID: "seed", Changes: changes})
	must(b, err)
	p := model.GetCollectionParams{
		ReadParams:    model.ReadParams{Identity: e.i, Page: model.Page{Limit: 1000}},
		CollectionKey: "colors",
	}
	var variants atomic.Uint64
	for _, branch := range []string{"cache_hit", "not_modified", "cache_miss"} {
		b.Run("GetCollection/"+branch, func(b *testing.B) {
			request := p
			variant := variants.Add(1)
			warm, err := e.s.User.GetCollection(ctx, request)
			must(b, err)
			if branch == "not_modified" {
				request.KnownVersion = warm.Version
			}
			b.ReportAllocs()
			b.ResetTimer()
			for n := range b.N {
				if branch == "cache_miss" {
					// A new locale creates a distinct uncached resource variant.
					request.Locale = fmt.Sprintf("miss-%d-%d", variant, n)
				}
				view, err := e.s.User.GetCollection(ctx, request)
				must(b, err)
				if view.NotModified != (branch == "not_modified") || (branch != "not_modified" && len(view.Entries) != 1000) {
					b.Fatal("incorrect catalog result")
				}
			}
			calls := 1
			if branch == "cache_miss" {
				calls = 5
			}
			b.ReportMetric(float64(calls), "sql/op")
		})
	}
	for _, analyzed := range []bool{false, true} {
		if analyzed {
			_, err := e.db.ExecContext(ctx, "ANALYZE collections_collection; ANALYZE collections_entry; ANALYZE collections_ownership")
			must(b, err)
		}
		b.Run(fmt.Sprintf("ListOwned/1000_analyzed_%t", analyzed), func(b *testing.B) {
			request := model.ListOwnedParams{ReadParams: p.ReadParams, CollectionKeys: []string{"colors"}}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				owned, err := e.s.User.ListOwned(ctx, request)
				must(b, err)
				if len(owned) != 1000 {
					b.Fatal("incorrect ownership result")
				}
			}
			b.ReportMetric(1, "sql/op")
		})
	}
}
