package tasks

import (
	"context"
	"fmt"

	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
	"github.com/elum2b/services/tasks/service/admin"
	"github.com/elum2b/services/tasks/service/integration"
	"github.com/elum2b/services/tasks/service/internalapi"
	"github.com/elum2b/services/tasks/service/user"
	json "github.com/goccy/go-json"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

var tasksBenchmarkUserID atomic.Uint64

var (
	benchmarkTasksExampleImportResult admin.ImportResult
	benchmarkTasksExampleExportRaw    []byte
)

func BenchmarkTasksExampleDumpImport(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	raw := readDailyExampleDump(b)
	var req admin.ImportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		b.Fatalf("unmarshal daily example request: %v", err)
	}
	preview, err := service.Admin.PreviewImport(
		ctx,
		testsupport.WorkspaceID("daily-import-preview"),
		req.Package,
	)
	if err != nil {
		b.Fatalf("preview daily example: %v", err)
	}
	secrets := exportImportSecretMap(preview.RequiredSecrets, "benchmark-secret")
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var current admin.ImportRequest
		if err := json.Unmarshal(raw, &current); err != nil {
			b.Fatalf("unmarshal daily example: %v", err)
		}
		result, err := service.Admin.Import(ctx, benchmarkWorkspace("daily-import", i), admin.ImportRequest{
			Package:          current.Package,
			ConflictStrategy: repository.ImportConflictFail,
			Secrets:          secrets,
		})
		if err != nil {
			b.Fatalf("import daily example: %v", err)
		}
		benchmarkTasksExampleImportResult = result
	}
}

func BenchmarkTasksExampleDumpExport(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	raw := readDailyExampleDump(b)
	var req admin.ImportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		b.Fatalf("unmarshal daily example request: %v", err)
	}
	preview, err := service.Admin.PreviewImport(
		ctx,
		testsupport.WorkspaceID("daily-export-preview"),
		req.Package,
	)
	if err != nil {
		b.Fatalf("preview daily example: %v", err)
	}
	workspaceID := testsupport.WorkspaceID("daily-export-benchmark")
	if _, err := service.Admin.Import(ctx, workspaceID, admin.ImportRequest{
		Package:          req.Package,
		ConflictStrategy: repository.ImportConflictFail,
		Secrets:          exportImportSecretMap(preview.RequiredSecrets, "benchmark-secret"),
	}); err != nil {
		b.Fatalf("seed daily example: %v", err)
	}
	b.SetBytes(int64(len(raw)))
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		exported, err := service.Admin.Export(ctx, workspaceID, admin.ExportRequest{
			Now: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC),
		})
		if err != nil {
			b.Fatalf("export daily example: %v", err)
		}
		out, err := json.Marshal(exported)
		if err != nil {
			b.Fatalf("marshal daily example export: %v", err)
		}
		benchmarkTasksExampleExportRaw = out
	}
}

func BenchmarkTasksServiceMethods(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	createEarnChain(
		b,
		service,
		testsupport.WorkspaceID("bench-manual"),
		repository.ClaimModeManual,
	)
	createEarnChain(
		b,
		service,
		testsupport.WorkspaceID("bench-auto"),
		repository.ClaimModeAuto,
	)

	idempotentIdentity := internalapi.Identity{
		WorkspaceID: testsupport.WorkspaceID("bench-auto"), AppID: 1, PlatformID: 1, PlatformUserID: "same",
	}
	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: idempotentIdentity, ActionKey: "earn_coin", Amount: 1500,
		Source: "bench", ExternalEventKey: "same-event", Now: now,
	}); err != nil {
		b.Fatal(err)
	}
	manualList, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity{
		WorkspaceID: testsupport.WorkspaceID("bench-manual"), AppID: 1, PlatformID: 1, PlatformUserID: "claim-seed",
	}, Locale: "ru", Now: now})
	if err != nil {
		b.Fatal(err)
	}
	manualFirstID := findTask(b, manualList, "earn_1").ID

	b.ReportAllocs()
	b.Run("Internal.Record/idempotent", func(b *testing.B) {
		for range b.N {
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: idempotentIdentity, ActionKey: "earn_coin", Amount: 1500,
				Source: "bench", ExternalEventKey: "same-event", Now: now,
			})
			benchError(b, err)
		}
	})
	b.Run("Internal.Record/manual_ready", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: testsupport.WorkspaceID("bench-manual"), AppID: 1, PlatformID: 1,
					PlatformUserID: "manual-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "earn_coin", Amount: 1500,
				Source: "bench", ExternalEventKey: "manual-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})
	b.Run("Internal.Record/auto_claim", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: testsupport.WorkspaceID("bench-auto"), AppID: 1, PlatformID: 1,
					PlatformUserID: "auto-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "earn_coin", Amount: 1500,
				Source: "bench", ExternalEventKey: "auto-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})
	b.Run("User.Claim/success", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := user.Identity{
				WorkspaceID: testsupport.WorkspaceID("bench-manual"), AppID: 1, PlatformID: 1,
				PlatformUserID: "claim-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity(identity), ActionKey: "earn_coin", Amount: 1000,
				Source: "bench", ExternalEventKey: "claim-ready-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.User.Claim(ctx, user.ClaimParams{
				Identity: identity, TaskRef: fmt.Sprintf("%d", manualFirstID),
				OperationID: "claim-op-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})
	b.Run("User.Claim/idempotent", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("bench-manual"), AppID: 1, PlatformID: 1, PlatformUserID: "claimed"}
		_, err := service.Internal.Record(ctx, internalapi.RecordParams{
			Identity: internalapi.Identity(identity), ActionKey: "earn_coin", Amount: 1000,
			Source: "bench", ExternalEventKey: "claimed-ready", Now: now,
		})
		benchError(b, err)
		_, err = service.User.Claim(ctx, user.ClaimParams{
			Identity: identity, TaskRef: fmt.Sprintf("%d", manualFirstID), OperationID: "claimed-op", Now: now,
		})
		benchError(b, err)
		b.ResetTimer()
		for range b.N {
			_, err := service.User.Claim(ctx, user.ClaimParams{
				Identity: identity, TaskRef: fmt.Sprintf("%d", manualFirstID), OperationID: "claimed-op", Now: now,
			})
			benchError(b, err)
		}
	})
	b.Run("User.ListActive", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("bench-manual"), AppID: 1, PlatformID: 1, PlatformUserID: "list"}
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: now})
			benchError(b, err)
		}
	})
}

func BenchmarkTasksIntegration(b *testing.B) {
	service := newTasksTestService(b, Options{
		Integration: integration.Options{
			ExternalCheckers: map[string]integration.ExternalTaskChecker{
				"fake": &fakeExternalChecker{completed: true},
			},
		},
	})
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	taskID := createIntegrationTask(b, service, integrationTaskSeed{
		WorkspaceID: testsupport.WorkspaceID("bench-integration"),
		Key:         "external_claim",
		TaskKind:    repository.TaskKindExternalCheck,
		ActionKey:   "external:bench",
		ActionKind:  repository.ActionKindExternal,
		Provider:    "fake",
	})
	taskRef := fmt.Sprintf("%d", taskID)

	b.ReportAllocs()
	b.Run("User.ListActive/integration_task", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("bench-integration"), AppID: 1, PlatformID: 1, PlatformUserID: "list"}
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: now})
			benchError(b, err)
		}
	})
	b.Run("Integration.CheckExternal/ready", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Integration.CheckExternal(ctx, integration.CheckExternalParams{
				TaskRefParams: integration.TaskRefParams{
					Identity: integration.Identity{
						WorkspaceID: testsupport.WorkspaceID("bench-integration"), AppID: 1, PlatformID: 1,
						PlatformUserID: "check-" + strconv.FormatUint(id, 10),
					},
					TaskRef: taskRef,
					Now:     now,
				},
			})
			benchError(b, err)
		}
	})
	b.Run("User.Claim/integration_success", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := integration.Identity{
				WorkspaceID: testsupport.WorkspaceID("bench-integration"), AppID: 1, PlatformID: 1,
				PlatformUserID: "claim-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Integration.CheckExternal(ctx, integration.CheckExternalParams{
				TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: taskRef, Now: now},
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.User.Claim(ctx, user.ClaimParams{
				Identity: user.Identity(identity), TaskRef: taskRef,
				OperationID: "integration-op-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})
}

func BenchmarkTasksLargeWorkspace(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	seed := seedLargeTasksBenchmark(
		b,
		service,
		testsupport.WorkspaceID("bench-large"),
	)

	b.ReportAllocs()
	b.ReportMetric(float64(seed.totalTasks), "tasks")
	b.ReportMetric(float64(seed.sequences), "sequences")
	b.ReportMetric(float64(seed.targetStandalone), "target_standalone")
	b.ReportMetric(float64(seed.targetSequenceHeads), "target_sequence_heads")

	b.Run("Internal.Record/scan_only_no_match", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: testsupport.WorkspaceID("bench-large"), AppID: 1, PlatformID: 1,
					PlatformUserID: "scan-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "missing_action", Amount: 1, Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("Internal.Record/target_action_new_user", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: testsupport.WorkspaceID("bench-large"), AppID: 1, PlatformID: 1,
					PlatformUserID: "target-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "target_action", Amount: 1000,
				Source: "bench-large", ExternalEventKey: "target-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("User.ListActive/large_workspace", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("bench-large"), AppID: 1, PlatformID: 1, PlatformUserID: "list-large"}
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: now})
			benchError(b, err)
		}
	})

	b.Run("User.Claim/success_by_id_large_workspace", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := user.Identity{
				WorkspaceID: testsupport.WorkspaceID("bench-large"), AppID: 1, PlatformID: 1,
				PlatformUserID: "claim-large-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity(identity), ActionKey: "target_action", Amount: 1000,
				Source: "bench-large", ExternalEventKey: "claim-large-ready-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.User.Claim(ctx, user.ClaimParams{
				Identity: identity, TaskRef: fmt.Sprintf("%d", seed.claimTaskID),
				OperationID: "claim-large-op-" + strconv.FormatUint(id, 10), Now: now,
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

type largeTasksSeed struct {
	totalTasks          int
	sequences           int
	targetStandalone    int
	targetSequenceHeads int
	claimTaskID         uint64
}

func seedLargeTasksBenchmark(b testing.TB, tasksService *Tasks, workspaceID string) largeTasksSeed {
	b.Helper()
	ctx := context.Background()
	if err := tasksService.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		b.Fatalf("large group: %v", err)
	}

	actions := []string{
		"target_action", "earn_coin", "earn_crystal", "watch_ad", "open_app",
		"daily_login", "invite_friend", "play_match", "win_match", "spend_energy",
		"collect_bonus", "upgrade_card", "join_channel", "share_app", "finish_level",
		"spin_wheel", "buy_item", "craft_item", "send_gift", "claim_daily",
	}
	seed := largeTasksSeed{sequences: 100}
	const standaloneTasks = 1000
	const sequenceLength = 10
	for i := 0; i < standaloneTasks; i++ {
		action := actions[i%len(actions)]
		id, err := tasksService.Admin.SaveTask(ctx, admin.SaveTaskParams{
			WorkspaceID: workspaceID, Key: fmt.Sprintf("standalone_%04d", i),
			GroupKey: "main", ActionKey: action, ActionKind: repository.ActionKindAmountAction,
			ClaimMode: repository.ClaimModeManual, TargetCount: 1000,
			ResetUnit: repository.ResetNever, ResetEvery: 1,
			Position: int32(i + 1), IsVisible: true, IsActive: true,
		})
		if err != nil {
			b.Fatalf("large standalone %d: %v", i, err)
		}
		if action == "target_action" {
			seed.targetStandalone++
			if seed.claimTaskID == 0 {
				seed.claimTaskID = id
			}
		}
		if err := tasksService.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", fmt.Sprintf("Standalone %d", i), "Large benchmark task"); err != nil {
			b.Fatalf("large standalone localization %d: %v", i, err)
		}
		if err := tasksService.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: "coin", Quantity: 1}, 1); err != nil {
			b.Fatalf("large standalone reward %d: %v", i, err)
		}
	}
	for seq := 0; seq < seed.sequences; seq++ {
		sequenceKey := fmt.Sprintf("seq_%03d", seq)
		if err := tasksService.Admin.UpsertSequence(ctx, workspaceID, sequenceKey, int32(seq+1), true); err != nil {
			b.Fatalf("large sequence %d: %v", seq, err)
		}
		for pos := 1; pos <= sequenceLength; pos++ {
			sequencePosition := uint32(pos)
			action := actions[(seq+pos)%len(actions)]
			if pos == 1 && seq%4 == 0 {
				action = "target_action"
				seed.targetSequenceHeads++
			}
			id, err := tasksService.Admin.SaveTask(ctx, admin.SaveTaskParams{
				WorkspaceID: workspaceID, Key: fmt.Sprintf("sequence_%03d_%02d", seq, pos),
				GroupKey: "main", SequenceKey: strPtr(sequenceKey), SequencePosition: &sequencePosition,
				ActionKey: action, ActionKind: repository.ActionKindAmountAction,
				ClaimMode: repository.ClaimModeManual, TargetCount: 1000,
				ResetUnit: repository.ResetNever, ResetEvery: 1,
				Position: int32(standaloneTasks + seq*sequenceLength + pos), IsVisible: true, IsActive: true,
			})
			if err != nil {
				b.Fatalf("large sequence %d task %d: %v", seq, pos, err)
			}
			if err := tasksService.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", fmt.Sprintf("Sequence %d.%d", seq, pos), "Large benchmark sequence task"); err != nil {
				b.Fatalf("large sequence localization %d.%d: %v", seq, pos, err)
			}
			if err := tasksService.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: "coin", Quantity: 1}, 1); err != nil {
				b.Fatalf("large sequence reward %d.%d: %v", seq, pos, err)
			}
		}
	}
	seed.totalTasks = standaloneTasks + seed.sequences*sequenceLength
	return seed
}

func readDailyExampleDump(tb testing.TB) []byte {
	tb.Helper()
	raw, err := os.ReadFile(filepath.Join("examples", "daily_tasks_import.json"))
	if err != nil {
		tb.Fatalf("read daily example: %v", err)
	}
	return raw
}

func benchmarkWorkspace(prefix string, iteration int) string {
	return testsupport.WorkspaceID(prefix + "-" + strconv.Itoa(iteration))
}

func BenchmarkTasksComplex(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	workspaceID := testsupport.WorkspaceID("bench-complex")
	ids := createComplexTaskSet(b, service, workspaceID, complexTaskOptions{
		ParentKey: "bench.combo",
		Conditions: []complexConditionSeed{
			{Key: "bench.condition.message", ActionKey: "message.send", TargetCount: 1},
			{Key: "bench.condition.ads", ActionKey: "ads.watch", TargetCount: 2},
		},
		ParentRewardKey:      "stars",
		ParentRewardQuantity: 100,
		ResetUnit:            repository.ResetNever,
	})
	b.ReportAllocs()

	b.Run("User.ListActive/no_progress", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "list-empty"}
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: now})
			benchError(b, err)
		}
	})

	b.Run("User.ListActive/with_progress", func(b *testing.B) {
		identity := user.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "list-progress"}
		_, err := service.Internal.Record(ctx, internalapi.RecordParams{
			Identity: internalapi.Identity(identity), ActionKey: "ads.watch", Amount: 1,
			Source: "bench", ExternalEventKey: "list-progress-ads", Now: now,
		})
		benchError(b, err)
		b.ResetTimer()
		for range b.N {
			_, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: now})
			benchError(b, err)
		}
	})

	b.Run("Internal.Record/progress_partial_no_idempotency", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
					PlatformUserID: "partial-noidem-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "ads.watch", Amount: 1,
				Source: "bench", Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("Internal.Record/progress_partial_idempotent", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity{
					WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
					PlatformUserID: "partial-" + strconv.FormatUint(id, 10),
				},
				ActionKey: "ads.watch", Amount: 1,
				Source: "bench", ExternalEventKey: "partial-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("Internal.Record/condition_ready_parent_refresh", func(b *testing.B) {
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := internalapi.Identity{
				WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
				PlatformUserID: "ready-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: identity, ActionKey: "message.send", Amount: 1,
				Source: "bench", ExternalEventKey: "ready-message-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: identity, ActionKey: "ads.watch", Amount: 2,
				Source: "bench", ExternalEventKey: "ready-ads-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("User.Claim/condition_reward", func(b *testing.B) {
		conditionRef := fmt.Sprintf("%d", ids.conditionIDs[0])
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := user.Identity{
				WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
				PlatformUserID: "condition-claim-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity(identity), ActionKey: "message.send", Amount: 1,
				Source: "bench", ExternalEventKey: "condition-claim-message-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.User.Claim(ctx, user.ClaimParams{
				Identity: identity, TaskRef: conditionRef,
				OperationID: "condition-claim-op-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})

	b.Run("User.Claim/complex_reward", func(b *testing.B) {
		parentRef := fmt.Sprintf("%d", ids.parentID)
		for range b.N {
			id := tasksBenchmarkUserID.Add(1)
			identity := user.Identity{
				WorkspaceID: workspaceID, AppID: 1, PlatformID: 1,
				PlatformUserID: "claim-" + strconv.FormatUint(id, 10),
			}
			b.StopTimer()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity(identity), ActionKey: "message.send", Amount: 1,
				Source: "bench", ExternalEventKey: "claim-message-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			_, err = service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: internalapi.Identity(identity), ActionKey: "ads.watch", Amount: 2,
				Source: "bench", ExternalEventKey: "claim-ads-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
			b.StartTimer()
			_, err = service.User.Claim(ctx, user.ClaimParams{
				Identity: identity, TaskRef: parentRef,
				OperationID: "complex-claim-" + strconv.FormatUint(id, 10), Now: now,
			})
			benchError(b, err)
		}
	})
}

func BenchmarkTasksLifecycle(b *testing.B) {
	ctx := context.Background()
	adminDB, err := openTasksPostgres("postgres")
	if err != nil {
		b.Fatal(err)
	}
	if err := recreateTasksDatabase(ctx, adminDB, "tasks_bench_lifecycle"); err != nil {
		b.Fatal(err)
	}
	_ = adminDB.Close()

	db, err := openTasksPostgres("tasks_bench_lifecycle")
	if err != nil {
		b.Fatal(err)
	}
	client, err := sqlwrap.New(db)
	if err != nil {
		b.Fatal(err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		b.Fatal(err)
	}
	_ = repo.Close()
	b.Cleanup(func() { _ = client.Close() })

	b.ReportAllocs()
	b.Run("NewClose", func(b *testing.B) {
		for range b.N {
			service := New(DatabaseParams{})
			benchError(b, service.Close())
		}
	})
	b.Run("RunClose", func(b *testing.B) {
		params := DatabaseParams{
			User: pgUser, Password: pgPassword, Database: "tasks_bench_lifecycle",
			Host: "127.0.0.1", Port: pgPort,
		}
		for range b.N {
			runCtx, cancel := context.WithCancel(ctx)
			cancel()
			benchError(b, New(params).Run(runCtx))
		}
	})
}
