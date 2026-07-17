package promo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/admin"
	"github.com/elum2b/services/promo/service/user"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

var (
	promoBenchWorkspace      = testsupport.WorkspaceID("promo-benchmark-workspace")
	promoBenchWriteWorkspace = testsupport.WorkspaceID("promo-benchmark-write-workspace")
)

const (
	promoBenchPromos = 100
	promoBenchUsers  = 1_000
)

type promoBenchmarkEnv struct {
	ctx       context.Context
	api       *Promo
	ids       []uint64
	codes     []string
	writeID   uint64
	writeCode string
}

var promoBenchSequence atomic.Uint64

func BenchmarkPromoServiceMethods(b *testing.B) {
	env := setupPromoBenchmark(b)
	identity := promoBenchmarkIdentity(0)
	appliedIdentity := promoBenchmarkIdentity(1)
	code := env.codes[0]
	promoID := env.ids[0]
	writeID := env.writeID
	writeCode := env.writeCode
	importPackage := promoBenchmarkImportPackage()

	first, err := env.api.User.Apply(env.ctx, user.ApplyParams{
		Identity: appliedIdentity,
		Code:     code,
		Locale:   "ru",
	})
	promoBenchNoError(b, err)
	if first.Status != repository.StatusSuccess {
		b.Fatalf("seed apply status = %s", first.Status)
	}

	b.ReportAllocs()

	b.Run("User.Apply/success", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.User.Apply(env.ctx, user.ApplyParams{
				Identity: promoBenchmarkUniqueIdentity("apply", i),
				Code:     env.codes[i%len(env.codes)],
				Locale:   "ru",
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("User.Apply/already_applied", func(b *testing.B) {
		for range b.N {
			_, err := env.api.User.Apply(env.ctx, user.ApplyParams{
				Identity: appliedIdentity,
				Code:     code,
				Locale:   "ru",
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.GetPromo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.GetPromo(
				env.ctx,
				promoBenchWorkspace,
				env.ids[i%len(env.ids)],
			)
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.ListPromos", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.ListPromos(env.ctx, promoBenchWorkspace, admin.Page{
				Limit: 100,
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.CreatePromo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.CreatePromo(env.ctx, admin.SavePromoParams{
				WorkspaceID: promoBenchWriteWorkspace,
				Code:        promoBenchmarkRunValue("create", i),
				Payload:     json.RawMessage(`{"benchmark":true}`),
				IsActive:    true,
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.UpdatePromo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.UpdatePromo(env.ctx, admin.SavePromoParams{
				ID:             writeID,
				WorkspaceID:    promoBenchWriteWorkspace,
				Code:           writeCode,
				Payload:        json.RawMessage(`{"benchmark":true,"updated":true}`),
				MaxActivations: 0,
				IsActive:       true,
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.UpsertLocalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := env.api.Admin.UpsertLocalization(env.ctx, admin.SaveLocalizationParams{
				WorkspaceID: promoBenchWriteWorkspace,
				PromoID:     writeID,
				Locale:      "bench-" + strconv.Itoa(i),
				Title:       "Benchmark promo",
				Description: "Benchmark promo description",
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.ListLocalizations", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.ListLocalizations(
				env.ctx,
				promoBenchWorkspace,
				env.ids[i%len(env.ids)],
			)
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.UpsertReward", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := env.api.Admin.UpsertReward(env.ctx, admin.SaveRewardParams{
				WorkspaceID: promoBenchWriteWorkspace,
				PromoID:     writeID,
				Key:         promoBenchmarkRunValue("reward", i),
				Quantity:    1,
				Scale:       2,
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.ListRewards", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.ListRewards(
				env.ctx,
				promoBenchWorkspace,
				env.ids[i%len(env.ids)],
			)
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.GetStats", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.GetStats(env.ctx, promoBenchWorkspace, promoID)
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.ListRedemptions", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.ListRedemptions(env.ctx, promoBenchWorkspace, promoID, admin.Page{
				Limit: 100,
			})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.RefreshDailyStats", func(b *testing.B) {
		from := time.Now().Add(-24 * time.Hour)
		until := time.Now().Add(24 * time.Hour)
		for range b.N {
			err := env.api.Admin.RefreshDailyStats(env.ctx, promoBenchWorkspace, from, until)
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.Export", func(b *testing.B) {
		for range b.N {
			_, err := env.api.Admin.Export(env.ctx, promoBenchWorkspace, admin.ExportRequest{})
			promoBenchNoError(b, err)
		}
	})

	b.Run("Admin.Import/update_existing", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := env.api.Admin.Import(env.ctx, testsupport.WorkspaceID(
				promoBenchmarkRunValue("import_workspace", i),
			), admin.ImportRequest{
				Package:          importPackage,
				ConflictStrategy: repository.ImportConflictUpdate,
			})
			promoBenchNoError(b, err)
		}
	})

	_ = identity
}

func setupPromoBenchmark(b *testing.B) promoBenchmarkEnv {
	b.Helper()
	api := newPromoTestService(b)
	env := promoBenchmarkEnv{
		ctx:   context.Background(),
		api:   api,
		ids:   make([]uint64, 0, promoBenchPromos),
		codes: make([]string, 0, promoBenchPromos),
	}
	seedPromoBenchmark(b, &env)
	return env
}

func seedPromoBenchmark(b *testing.B, env *promoBenchmarkEnv) {
	b.Helper()
	writeID, err := env.api.Admin.CreatePromo(env.ctx, admin.SavePromoParams{
		WorkspaceID: promoBenchWriteWorkspace,
		Code:        "WRITE_BENCH_PROMO",
		Payload:     json.RawMessage(`{"benchmark":true}`),
		IsActive:    true,
	})
	promoBenchNoError(b, err)
	env.writeID = writeID
	env.writeCode = "WRITE_BENCH_PROMO"
	for i := 0; i < promoBenchPromos; i++ {
		code := promoBenchmarkCode(i)
		id, err := env.api.Admin.CreatePromo(env.ctx, admin.SavePromoParams{
			WorkspaceID: promoBenchWorkspace,
			Code:        code,
			Payload:     json.RawMessage(`{"benchmark":true}`),
			IsActive:    true,
		})
		promoBenchNoError(b, err)
		env.ids = append(env.ids, id)
		env.codes = append(env.codes, code)
		promoBenchNoError(b, env.api.Admin.UpsertLocalization(env.ctx, admin.SaveLocalizationParams{
			WorkspaceID: promoBenchWorkspace,
			PromoID:     id,
			Locale:      "ru",
			Title:       "Benchmark promo",
			Description: "Benchmark promo description",
		}))
		promoBenchNoError(b, env.api.Admin.UpsertReward(env.ctx, admin.SaveRewardParams{
			WorkspaceID: promoBenchWorkspace,
			PromoID:     id,
			Key:         "stars",
			Quantity:    100,
			Scale:       2,
		}))
	}
	for i := 0; i < promoBenchUsers; i++ {
		_, err := env.api.User.Apply(env.ctx, user.ApplyParams{
			Identity: promoBenchmarkIdentity(i),
			Code:     env.codes[i%len(env.codes)],
			Locale:   "ru",
		})
		promoBenchNoError(b, err)
	}
}

func promoBenchmarkImportPackage() admin.ExportPackage {
	return admin.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "promo",
		Promos: []admin.ExportPromo{
			{
				Code:           "IMPORT_BENCH_1",
				Payload:        json.RawMessage(`{"benchmark":true}`),
				MaxActivations: 0,
				IsActive:       true,
				Localization: map[string]admin.ExportText{
					"ru": {
						Title:       "Import benchmark",
						Description: "Import benchmark description",
					},
				},
				Rewards: []admin.ExportReward{
					{
						Key:      "stars",
						Type:     "quantity",
						Quantity: 100,
						Scale:    2,
					},
				},
			},
		},
	}
}

func promoBenchmarkIdentity(index int) user.Identity {
	return user.Identity{
		WorkspaceID:    promoBenchWorkspace,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "bench-user-" + strconv.Itoa(index),
	}
}

func promoBenchmarkUniqueIdentity(prefix string, index int) user.Identity {
	sequence := promoBenchSequence.Add(1)
	return user.Identity{
		WorkspaceID:    promoBenchWorkspace,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: fmt.Sprintf("bench-%s-%d-%d", prefix, index, sequence),
	}
}

func promoBenchmarkCode(index int) string {
	return fmt.Sprintf("BENCH_PROMO_%04d", index)
}

func promoBenchmarkRunValue(prefix string, index int) string {
	sequence := promoBenchSequence.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, index, sequence)
}

func promoBenchNoError(b testing.TB, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}
