package cpa_test

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/admin"
	"github.com/elum2b/services/cpa/service/user"
	"github.com/elum2b/services/internal/testsupport"
)

func BenchmarkCPA(b *testing.B) {
	b.Run("User.ListActive/cache_hit", benchmarkCPAUserListActiveCacheHit)
	b.Run("User.GetCode/new_assignment", benchmarkCPAUserGetCodeNewAssignment)
	b.Run("User.GetCode/new_assignment_parallel", benchmarkCPAUserGetCodeNewAssignmentParallel)
	b.Run("User.GetCode/existing_assignment", benchmarkCPAUserGetCodeExistingAssignment)
	b.Run("Admin.Complete/new_assignment", benchmarkCPAAdminCompleteNewAssignment)
	b.Run("Admin.GetOffer/cache_hit", benchmarkCPAAdminGetOfferCacheHit)
	b.Run("Admin.ListOffers/cache_hit", benchmarkCPAAdminListOffersCacheHit)
	b.Run("Admin.UpsertOffer", benchmarkCPAAdminUpsertOffer)
	b.Run("Admin.UpsertLocalization", benchmarkCPAAdminUpsertLocalization)
	b.Run("Admin.UpsertReward", benchmarkCPAAdminUpsertReward)
	b.Run("Admin.GetStats", benchmarkCPAAdminGetStats)
	b.Run("Admin.ListAssignments", benchmarkCPAAdminListAssignments)
	b.Run("Admin.RefreshDailyStats", benchmarkCPAAdminRefreshDailyStats)
	b.Run("Admin.Export", benchmarkCPAAdminExport)
	b.Run("Admin.Import", benchmarkCPAAdminImport)
}

func benchmarkCPAUserGetCodeNewAssignmentParallel(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "parallel_issue_offer", true)

	var sequence atomic.Uint64
	b.ResetTimer()
	b.RunParallel(func(worker *testing.PB) {
		for worker.Next() {
			index := sequence.Add(1)
			if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
				Identity: cpaTestIdentity(fmt.Sprintf("parallel-benchmark-user-%d", index)),
				CPAID:    "parallel_issue_offer",
			}); err != nil {
				b.Errorf("parallel issue code: %v", err)
				return
			}
		}
	})
}

func benchmarkCPAUserListActiveCacheHit(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	seedCPACatalog(b, env, 100)
	params := user.ListActiveParams{Identity: cpaTestIdentity("list-user"), Locale: "ru"}
	if _, err := env.Service.User.ListActive(env.Context, params); err != nil {
		b.Fatalf("warm list active cache: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.User.ListActive(env.Context, params); err != nil {
			b.Fatalf("list active: %v", err)
		}
	}
}

func benchmarkCPAUserGetCodeNewAssignment(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "issue_offer", true)
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
			Identity: cpaBenchmarkIdentity(index),
			CPAID:    "issue_offer",
		}); err != nil {
			b.Fatalf("issue code: %v", err)
		}
	}
}

func benchmarkCPAUserGetCodeExistingAssignment(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "existing_offer", true)
	identity := cpaTestIdentity("existing-user")
	if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    "existing_offer",
	}); err != nil {
		b.Fatalf("seed assignment: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
			Identity: identity,
			CPAID:    "existing_offer",
		}); err != nil {
			b.Fatalf("get existing assignment: %v", err)
		}
	}
}

func benchmarkCPAAdminCompleteNewAssignment(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "complete_offer", true)
	identities := make([]user.Identity, b.N)
	for index := range identities {
		identities[index] = cpaBenchmarkIdentity(index)
		if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
			Identity: identities[index],
			CPAID:    "complete_offer",
		}); err != nil {
			b.Fatalf("seed completion assignment: %v", err)
		}
	}
	b.ResetTimer()
	for index := range identities {
		if _, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
			Identity: identities[index],
			CPAID:    "complete_offer",
		}); err != nil {
			b.Fatalf("complete assignment: %v", err)
		}
	}
}

func benchmarkCPAAdminGetOfferCacheHit(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "get_offer", true)
	if _, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "get_offer"); err != nil {
		b.Fatalf("warm get offer cache: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "get_offer"); err != nil {
			b.Fatalf("get offer: %v", err)
		}
	}
}

func benchmarkCPAAdminListOffersCacheHit(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	seedCPACatalog(b, env, 100)
	page := admin.Page{Limit: 100}
	if _, err := env.Service.Admin.ListOffers(env.Context, cpaTestWorkspaceID, page); err != nil {
		b.Fatalf("warm list offers cache: %v", err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.Admin.ListOffers(env.Context, cpaTestWorkspaceID, page); err != nil {
			b.Fatalf("list offers: %v", err)
		}
	}
}

func benchmarkCPAAdminUpsertOffer(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		id := fmt.Sprintf("upsert_%d", index)
		if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
			WorkspaceID: cpaTestWorkspaceID,
			ID:          id,
			Payload:     json.RawMessage(`{"kind":"benchmark"}`),
			CodeMode:    repository.CodeModeShared,
			SharedCode:  stringPointer("CODE-" + id),
			IsActive:    true,
		}); err != nil {
			b.Fatalf("upsert offer: %v", err)
		}
	}
}

func benchmarkCPAAdminUpsertLocalization(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "localization_offer", true)
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		if err := env.Service.Admin.UpsertLocalization(env.Context, admin.UpsertLocalizationParams{
			WorkspaceID: cpaTestWorkspaceID,
			CPAID:       "localization_offer",
			Locale:      fmt.Sprintf("locale-%d", index),
			Title:       "Benchmark localization",
			Description: "Benchmark localization description",
		}); err != nil {
			b.Fatalf("upsert localization: %v", err)
		}
	}
}

func benchmarkCPAAdminUpsertReward(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "reward_offer", true)
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		if err := env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
			WorkspaceID: cpaTestWorkspaceID,
			CPAID:       "reward_offer",
			Key:         fmt.Sprintf("reward-%d", index),
			Quantity:    1,
		}); err != nil {
			b.Fatalf("upsert reward: %v", err)
		}
	}
}

func benchmarkCPAAdminGetStats(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "stats_offer", true)
	seedCPAAssignments(b, env, "stats_offer", 100)
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.Admin.GetStats(env.Context, cpaTestWorkspaceID, "stats_offer"); err != nil {
			b.Fatalf("get stats: %v", err)
		}
	}
}

func benchmarkCPAAdminListAssignments(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "assignments_offer", true)
	seedCPAAssignments(b, env, "assignments_offer", 100)
	params := admin.AssignmentListParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       "assignments_offer",
		Page:        admin.Page{Limit: 100},
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.Admin.ListAssignments(env.Context, params); err != nil {
			b.Fatalf("list assignments: %v", err)
		}
	}
}

func benchmarkCPAAdminRefreshDailyStats(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	upsertSharedOffer(b, env, "daily_stats_offer", true)
	seedCPAAssignments(b, env, "daily_stats_offer", 100)
	from := time.Now().UTC().Add(-time.Hour)
	until := from.Add(2 * time.Hour)
	b.ResetTimer()
	for b.Loop() {
		if err := env.Service.Admin.RefreshDailyStats(env.Context, cpaTestWorkspaceID, from, until); err != nil {
			b.Fatalf("refresh daily stats: %v", err)
		}
	}
}

func benchmarkCPAAdminExport(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	seedCPACatalog(b, env, 100)
	b.ResetTimer()
	for b.Loop() {
		if _, err := env.Service.Admin.Export(env.Context, cpaTestWorkspaceID, admin.ExportRequest{}); err != nil {
			b.Fatalf("export offers: %v", err)
		}
	}
}

func benchmarkCPAAdminImport(b *testing.B) {
	env := newCPATestEnvironment(b, testCPAOptions())
	seedCPACatalog(b, env, 100)
	pkg, err := env.Service.Admin.Export(env.Context, cpaTestWorkspaceID, admin.ExportRequest{})
	if err != nil {
		b.Fatalf("prepare import package: %v", err)
	}
	b.ResetTimer()
	for index := 0; b.Loop(); index++ {
		if _, err := env.Service.Admin.Import(
			env.Context,
			testsupport.WorkspaceID(fmt.Sprintf("import-%d", index)),
			admin.ImportRequest{
				Package:          pkg,
				ConflictStrategy: repository.ImportConflictUpdate,
			},
		); err != nil {
			b.Fatalf("import offers: %v", err)
		}
	}
}

func seedCPACatalog(tb testing.TB, env cpaTestEnvironment, count int) {
	tb.Helper()
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("catalog_%03d", index)
		upsertSharedOffer(tb, env, id, true)
		upsertLocalization(tb, env, id, "ru", "Benchmark offer")
		upsertReward(tb, env, id, "stars", 1, 0)
	}
}

func seedCPAAssignments(tb testing.TB, env cpaTestEnvironment, cpaID string, count int) {
	tb.Helper()
	for index := 0; index < count; index++ {
		if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
			Identity: cpaBenchmarkIdentity(index),
			CPAID:    cpaID,
		}); err != nil {
			tb.Fatalf("seed assignment %d: %v", index, err)
		}
	}
}

func cpaBenchmarkIdentity(index int) user.Identity {
	return user.Identity{
		WorkspaceID:    cpaTestWorkspaceID,
		AppID:          100,
		PlatformID:     200,
		PlatformUserID: fmt.Sprintf("benchmark-user-%d", index),
	}
}
