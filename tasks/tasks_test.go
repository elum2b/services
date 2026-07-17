package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	services "github.com/elum2b/services"
	"github.com/elum2b/services/internal/testsupport"
	"github.com/elum2b/services/internal/utils/apiflow"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/tasks/repository"
	taskruntime "github.com/elum2b/services/tasks/runtime"
	"github.com/elum2b/services/tasks/service/admin"
	"github.com/elum2b/services/tasks/service/integration"
	"github.com/elum2b/services/tasks/service/internalapi"
	"github.com/elum2b/services/tasks/service/user"
	"github.com/go-resty/resty/v2"
	json "github.com/goccy/go-json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTasksRecordRejectsAmountAboveBigInt(t *testing.T) {
	service := newTasksTestService(t)
	workspaceID := testsupport.WorkspaceID("record-amount-overflow")
	createStandaloneTask(t, service, workspaceID, "overflow", "overflow", 1, 1)

	_, err := service.Internal.Record(context.Background(), internalapi.RecordParams{
		Identity: internalapi.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "player",
		},
		ActionKey:        "overflow",
		Amount:           math.MaxUint64,
		Source:           "test",
		ExternalEventKey: "overflow-event",
	})
	if !errors.Is(err, repository.ErrRecordAmountOverflow) {
		t.Fatalf("record overflow error = %v, want ErrRecordAmountOverflow", err)
	}

	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open tasks database: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM task_progress_event
		WHERE workspace_id = $1 AND external_event_key = $2
	`, workspaceID, "overflow-event").Scan(&count); err != nil {
		t.Fatalf("count overflow events: %v", err)
	}
	if count != 0 {
		t.Fatalf("overflow event count = %d, want 0", count)
	}
}

func TestTasksRecordRejectsIntegrationAndCompositeActions(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("record-system-actions")
	identity := internalapi.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "player",
	}

	integrationTaskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "subscribe-protected",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "subscribe:protected",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "telegram",
	})
	complexIDs := createComplexTaskSet(t, service, workspaceID, complexTaskOptions{
		ParentKey: "complex-protected",
		Conditions: []complexConditionSeed{
			{
				Key:         "complex-protected-condition",
				ActionKey:   "allowed-action",
				TargetCount: 1,
			},
		},
		ParentRewardKey:      "stars",
		ParentRewardQuantity: 1,
	})

	for _, actionKey := range []string{
		"subscribe:protected",
		"complex.complex-protected",
	} {
		result, err := service.Internal.Record(ctx, internalapi.RecordParams{
			Identity:         identity,
			ActionKey:        actionKey,
			Amount:           1,
			Source:           "test",
			ExternalEventKey: "forbidden:" + actionKey,
		})
		if err != nil {
			t.Fatalf("record forbidden action %q: %v", actionKey, err)
		}
		if result.Status != repository.RecordStatusNoTasks || len(result.Tasks) != 0 {
			t.Fatalf("forbidden action %q progressed tasks: %+v", actionKey, result)
		}
	}

	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open tasks database: %v", err)
	}
	defer db.Close()

	var progressCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM task_progress
		WHERE workspace_id = $1 AND task_id IN ($2, $3)
	`, workspaceID, integrationTaskID, complexIDs.parentID).Scan(&progressCount); err != nil {
		t.Fatalf("count protected progress: %v", err)
	}
	if progressCount != 0 {
		t.Fatalf("protected task progress rows = %d, want 0", progressCount)
	}
}

func TestTasksAdminRejectsIncompatibleTaskAndActionKinds(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("task-action-compatibility")
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}

	_, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         "invalid-subscription",
		GroupKey:    "main",
		TaskKind:    repository.TaskKindInternal,
		ActionKey:   "subscribe:invalid",
		ActionKind:  repository.ActionKindChannelSubscribe,
		ClaimMode:   repository.ClaimModeManual,
		TargetCount: 1,
		ResetUnit:   repository.ResetNever,
		ResetEvery:  1,
		IsVisible:   true,
		IsActive:    true,
	})
	if err == nil || !strings.Contains(err.Error(), "is incompatible with") {
		t.Fatalf("incompatible task error = %v", err)
	}
}

func TestAdminValidateReward(t *testing.T) {
	service := newTasksTestService(t)
	workspaceID := testsupport.WorkspaceID("workspace-reward-validation")
	taskID := createRewardValidationTask(t, service, workspaceID)
	hour := "hour"

	if err := service.Admin.UpsertReward(context.Background(), workspaceID, taskID, admin.RewardModel{
		Key: "coin", Quantity: 1,
	}, 1); err != nil {
		t.Fatalf("default quantity reward: %v", err)
	}
	if err := service.Admin.UpsertReward(context.Background(), workspaceID, taskID, admin.RewardModel{
		Key: "energy", Type: "duration", Quantity: 3, Unit: &hour,
	}, 2); err != nil {
		t.Fatalf("duration reward: %v", err)
	}
	if err := service.Admin.UpsertReward(context.Background(), workspaceID, taskID, admin.RewardModel{
		Key: "bad_energy", Type: "duration", Quantity: 3,
	}, 3); err == nil {
		t.Fatal("duration reward without unit must fail")
	}
}

func createRewardValidationTask(t testing.TB, service *Tasks, workspaceID string) uint64 {
	t.Helper()
	if err := service.Admin.UpsertGroup(context.Background(), workspaceID, "daily", 1, true); err != nil {
		t.Fatalf("save reward validation group: %v", err)
	}
	taskID, err := service.Admin.SaveTask(context.Background(), admin.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         "reward_validation",
		GroupKey:    "daily",
		TaskKind:    repository.TaskKindInternal,
		ActionKey:   "reward.validation",
		ActionKind:  repository.ActionKindAppAction,
		ClaimMode:   repository.ClaimModeManual,
		TargetCount: 1,
		ResetUnit:   repository.ResetNever,
		ResetEvery:  1,
		Position:    1,
		IsVisible:   true,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("save reward validation task: %v", err)
	}
	return taskID
}

func TestTasksAPIFlowRoundRobin(t *testing.T) {
	flow := apiflow.New(apiflow.Options{RatePerSecond: 1000})
	set := apiflow.TokenSet{Tokens: []string{"a", "b"}}
	first, err := flow.Acquire(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	second, err := flow.Acquire(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	third, err := flow.Acquire(context.Background(), set)
	if err != nil {
		t.Fatal(err)
	}
	if first != "a" || second != "b" || third != "a" {
		t.Fatalf("unexpected order: %q %q %q", first, second, third)
	}
}

func TestTasksAPIFlowPerTokenRateLimit(t *testing.T) {
	flow := apiflow.New(apiflow.Options{RatePerSecond: 30})
	start := time.Now()
	if _, err := flow.Acquire(context.Background(), apiflow.TokenSet{Tokens: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Acquire(context.Background(), apiflow.TokenSet{Tokens: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("expected second acquire to wait, elapsed %s", elapsed)
	}
}

func TestTasksAPIFlowContextCanceled(t *testing.T) {
	flow := apiflow.New(apiflow.Options{RatePerSecond: 1})
	if _, err := flow.Acquire(context.Background(), apiflow.TokenSet{Tokens: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, err := flow.Acquire(ctx, apiflow.TokenSet{Tokens: []string{"a"}}); err == nil {
		t.Fatal("expected context timeout")
	}
}

func TestTasksChannelSubscriptionCheckerTelegram(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	usedTokens := make([]string, 0, 2)
	mux.HandleFunc("/botbad/getChatMember", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		usedTokens = append(usedTokens, "bad")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"member"}}`))
	})
	mux.HandleFunc("/botgood/getChatMember", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		usedTokens = append(usedTokens, "good")
		mu.Unlock()
		if got := r.URL.Query().Get("chat_id"); got != "@channel" {
			t.Fatalf("chat_id = %q", got)
		}
		if got := r.URL.Query().Get("user_id"); got != "1093776793" {
			t.Fatalf("user_id = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"member"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	checker := integration.NewChannelSubscriptionChecker(integration.ChannelSubscriptionCheckerOptions{
		Client:                resty.New(),
		TelegramBotAPIBaseURL: server.URL,
	})
	result, err := checker.CheckChannelSubscription(context.Background(), integration.ChannelSubscriptionCheckParams{
		Identity: integration.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "1093776793"},
		Provider: "tg",
		Task: integration.TaskContext{
			ActionKey: "telegram",
			IntegrationPayload: json.RawMessage(`{
				"chat_id":"@channel",
				"tokens":["bad","good"]
			}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed {
		t.Fatalf("expected completed result: %+v", result)
	}
	result, err = checker.CheckChannelSubscription(context.Background(), integration.ChannelSubscriptionCheckParams{
		Identity: integration.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "1093776793"},
		Provider: "tg",
		Task: integration.TaskContext{
			ActionKey: "telegram",
			IntegrationPayload: json.RawMessage(`{
				"chat_id":"@channel",
				"tokens":["bad","good"]
			}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed {
		t.Fatalf("expected completed result: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(usedTokens) != 2 || usedTokens[0] != "bad" || usedTokens[1] != "good" {
		t.Fatalf("unexpected token usage: %#v", usedTokens)
	}
}

func TestTasksChannelSubscriptionCheckerVK(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	usedTokens := make([]string, 0, 2)
	mux.HandleFunc("/groups.isMember", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("group_id"); got != "club123" {
			t.Fatalf("group_id = %q", got)
		}
		if got := r.URL.Query().Get("user_id"); got != "42" {
			t.Fatalf("user_id = %q", got)
		}
		mu.Lock()
		usedTokens = append(usedTokens, r.URL.Query().Get("access_token"))
		mu.Unlock()
		_, _ = w.Write([]byte(`{"response":1}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	checker := integration.NewChannelSubscriptionChecker(integration.ChannelSubscriptionCheckerOptions{
		Client:       resty.New(),
		VKAPIBaseURL: server.URL,
	})
	result, err := checker.CheckChannelSubscription(context.Background(), integration.ChannelSubscriptionCheckParams{
		Identity: integration.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "42"},
		Provider: "vk",
		Task: integration.TaskContext{
			IntegrationPayload: json.RawMessage(`{
				"group_id":"club123",
				"tokens":["vk-token-1","vk-token-2"]
			}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed {
		t.Fatalf("expected completed result: %+v", result)
	}
	result, err = checker.CheckChannelSubscription(context.Background(), integration.ChannelSubscriptionCheckParams{
		Identity: integration.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "42"},
		Provider: "vk",
		Task: integration.TaskContext{
			IntegrationPayload: json.RawMessage(`{
				"group_id":"club123",
				"tokens":["vk-token-1","vk-token-2"]
			}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Completed {
		t.Fatalf("expected completed result: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(usedTokens) != 2 || usedTokens[0] != "vk-token-1" || usedTokens[1] != "vk-token-2" {
		t.Fatalf("unexpected token usage: %#v", usedTokens)
	}
}

func TestTasksChannelBoostCheckerTelegram(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	usedTokens := make([]string, 0, 2)
	mux.HandleFunc("/botone/getUserChatBoosts", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		usedTokens = append(usedTokens, "one")
		mu.Unlock()
		if got := r.URL.Query().Get("chat_id"); got != "@boosted" {
			t.Fatalf("chat_id = %q", got)
		}
		if got := r.URL.Query().Get("user_id"); got != "1093776793" {
			t.Fatalf("user_id = %q", got)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"boosts":[{"boost_id":"b1"}]}}`))
	})
	mux.HandleFunc("/bottwo/getUserChatBoosts", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		usedTokens = append(usedTokens, "two")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"ok":true,"result":{"boosts":[{"boost_id":"b2"}]}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	checker := integration.NewChannelSubscriptionChecker(integration.ChannelSubscriptionCheckerOptions{
		Client:                resty.New(),
		TelegramBotAPIBaseURL: server.URL,
	})
	params := integration.ChannelBoostCheckParams{
		Identity: integration.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "1093776793"},
		Provider: "tg",
		Task: integration.TaskContext{
			ActionKey: "telegram",
			IntegrationPayload: json.RawMessage(`{
				"chat_id":"@boosted",
				"tokens":["one","two"]
			}`),
		},
	}
	first, err := checker.CheckChannelBoost(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	second, err := checker.CheckChannelBoost(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Completed || !second.Completed {
		t.Fatalf("expected completed boost results: %+v %+v", first, second)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(usedTokens) != 2 || usedTokens[0] != "one" || usedTokens[1] != "two" {
		t.Fatalf("unexpected token usage: %#v", usedTokens)
	}
}

func TestTasksComplexConditionsOutOfOrderAndClaim(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("complex-out-of-order"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	ids := createComplexTaskSet(t, service, identity.WorkspaceID, complexTaskOptions{
		ParentKey: "daily.combo",
		Conditions: []complexConditionSeed{
			{Key: "daily.send_message", ActionKey: "message.send", TargetCount: 1, RewardKey: "stars", RewardQuantity: 25},
			{Key: "daily.play_coin", ActionKey: "coin.play", TargetCount: 1},
		},
		ParentRewardKey:      "stars",
		ParentRewardQuantity: 100,
		ResetUnit:            repository.ResetNever,
	})

	first, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "coin.play", Amount: 1, Source: "test", ExternalEventKey: "coin-1", Now: now,
	})
	if err != nil {
		t.Fatalf("record first condition: %v", err)
	}
	if first.Status != repository.RecordStatusRecorded {
		t.Fatalf("unexpected first record: %+v", first)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: now})
	if err != nil {
		t.Fatalf("list after first: %v", err)
	}
	parent := findTask(t, list, "daily.combo")
	if parent.Progress == nil || parent.Progress.Status != repository.StatusOpen || parent.Progress.Progress != 1 {
		t.Fatalf("expected parent partial progress: %+v", parent.Progress)
	}
	if len(parent.Conditions) != 2 {
		t.Fatalf("expected nested conditions: %+v", parent.Conditions)
	}

	second, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "message.send", Amount: 1, Source: "test", ExternalEventKey: "message-1", Now: now,
	})
	if err != nil {
		t.Fatalf("record second condition: %v", err)
	}
	if second.Status != repository.RecordStatusRecorded {
		t.Fatalf("unexpected second record: %+v", second)
	}
	list, err = service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: now})
	if err != nil {
		t.Fatalf("list after second: %v", err)
	}
	parent = findTask(t, list, "daily.combo")
	if parent.ID != ids.parentID || parent.Progress == nil || parent.Progress.Status != repository.StatusReady || parent.Progress.Progress != 2 {
		t.Fatalf("expected parent ready: %+v", parent)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", ids.parentID), OperationID: "complex-claim", Now: now,
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim parent: %+v err=%v", claim, err)
	}
	if claim.Task == nil || len(claim.Task.Rewards) != 1 || claim.Task.Rewards[0].Quantity != 100 {
		t.Fatalf("unexpected parent reward: %+v", claim.Task)
	}
}

func TestTasksComplexParallelActionAndConditionReward(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 13, 0, 0, 0, time.UTC)
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("complex-parallel"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	if err := service.Admin.UpsertGroup(ctx, identity.WorkspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	standaloneID := saveTaskForComplexTest(t, service, identity.WorkspaceID, "standalone.send", "message.send", 1, repository.TaskKindInternal, repository.ActionKindAmountAction, repository.ResetNever, 10)
	if err := service.Admin.UpsertReward(ctx, identity.WorkspaceID, standaloneID, admin.RewardModel{Key: "stars", Quantity: 10}, 1); err != nil {
		t.Fatalf("standalone reward: %v", err)
	}
	ids := createComplexTaskSet(t, service, identity.WorkspaceID, complexTaskOptions{
		ParentKey: "combo.send",
		Conditions: []complexConditionSeed{
			{Key: "combo.condition.send", ActionKey: "message.send", TargetCount: 1, RewardKey: "stars", RewardQuantity: 15},
		},
		ParentRewardKey:      "stars",
		ParentRewardQuantity: 50,
		ResetUnit:            repository.ResetNever,
		StartPosition:        20,
	})

	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "message.send", Amount: 1, Source: "test", ExternalEventKey: "message-parallel", Now: now,
	})
	if err != nil {
		t.Fatalf("record parallel action: %v", err)
	}
	if recorded.Status != repository.RecordStatusRecorded || len(recorded.Tasks) != 2 {
		t.Fatalf("expected standalone and condition to progress: %+v", recorded)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: now})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	standalone := findTask(t, list, "standalone.send")
	condition := findTask(t, list, "combo.condition.send")
	parent := findTask(t, list, "combo.send")
	if standalone.Progress == nil || standalone.Progress.Status != repository.StatusReady {
		t.Fatalf("standalone must be ready: %+v", standalone.Progress)
	}
	if condition.Progress == nil || condition.Progress.Status != repository.StatusReady {
		t.Fatalf("condition must be ready: %+v", condition.Progress)
	}
	if parent.Progress == nil || parent.Progress.Status != repository.StatusReady {
		t.Fatalf("parent must be ready: %+v", parent.Progress)
	}

	conditionClaim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", ids.conditionIDs[0]), OperationID: "condition-claim", Now: now,
	})
	if err != nil || conditionClaim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim condition: %+v err=%v", conditionClaim, err)
	}
	parentClaim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", ids.parentID), OperationID: "parent-claim", Now: now,
	})
	if err != nil || parentClaim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim parent: %+v err=%v", parentClaim, err)
	}
	if parentClaim.Task == nil || len(parentClaim.Task.Rewards) != 1 || parentClaim.Task.Rewards[0].Quantity != 50 {
		t.Fatalf("unexpected parent claim reward: %+v", parentClaim.Task)
	}
}

func TestTasksComplexTargetsAndResetWindows(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	day1 := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)
	day2 := day1.Add(24 * time.Hour)
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("complex-window"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	ids := createComplexTaskSet(t, service, identity.WorkspaceID, complexTaskOptions{
		ParentKey: "daily.combo.window",
		Conditions: []complexConditionSeed{
			{Key: "daily.watch_ads", ActionKey: "ads.watch", TargetCount: 2},
		},
		ParentRewardKey:      "stars",
		ParentRewardQuantity: 25,
		ResetUnit:            repository.ResetDay,
	})

	one, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "ads.watch", Amount: 1, Source: "test", ExternalEventKey: "ads-1", Now: day1,
	})
	if err != nil || one.Status != repository.RecordStatusRecorded {
		t.Fatalf("record first ad: %+v err=%v", one, err)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: day1})
	if err != nil {
		t.Fatalf("list after one: %v", err)
	}
	parent := findTask(t, list, "daily.combo.window")
	if parent.Progress != nil {
		t.Fatalf("parent should not count incomplete condition: %+v", parent.Progress)
	}

	two, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "ads.watch", Amount: 1, Source: "test", ExternalEventKey: "ads-2", Now: day1,
	})
	if err != nil || two.Status != repository.RecordStatusRecorded {
		t.Fatalf("record second ad: %+v err=%v", two, err)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", ids.parentID), OperationID: "window-claim-day1", Now: day1,
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim day1: %+v err=%v", claim, err)
	}

	nextDayList, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: day2})
	if err != nil {
		t.Fatalf("list day2: %v", err)
	}
	nextDayParent := findTask(t, nextDayList, "daily.combo.window")
	nextDayCondition := findTask(t, nextDayList, "daily.watch_ads")
	if nextDayParent.Progress != nil || nextDayCondition.Progress != nil {
		t.Fatalf("daily window must reset parent=%+v condition=%+v", nextDayParent.Progress, nextDayCondition.Progress)
	}
}

func TestTasksComplexOptionalConditionDoesNotInflateRequiredProgress(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 14, 0, 0, 0, time.UTC)
	workspaceID := testsupport.WorkspaceID("complex-optional")
	identity := internalapi.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "player",
	}

	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}
	requiredID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"complex.required",
		"complex.required.event",
		1,
		repository.TaskKindInternal,
		repository.ActionKindAppAction,
		repository.ResetNever,
		2,
	)
	optionalID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"complex.optional",
		"complex.optional.event",
		1,
		repository.TaskKindInternal,
		repository.ActionKindAppAction,
		repository.ResetNever,
		3,
	)
	parentID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"complex.parent",
		"complex.parent.composite",
		1,
		repository.TaskKindComplex,
		repository.ActionKindComposite,
		repository.ResetNever,
		1,
	)

	for _, condition := range []admin.SaveComplexConditionParams{
		{
			WorkspaceID:     workspaceID,
			ParentTaskID:    parentID,
			ConditionTaskID: requiredID,
			RequiredStatus:  repository.ComplexRequiredStatusReady,
			Position:        1,
			IsRequired:      true,
		},
		{
			WorkspaceID:     workspaceID,
			ParentTaskID:    parentID,
			ConditionTaskID: optionalID,
			RequiredStatus:  repository.ComplexRequiredStatusReady,
			Position:        2,
			IsRequired:      false,
		},
	} {
		if err := service.Admin.UpsertComplexCondition(ctx, condition); err != nil {
			t.Fatalf("save complex condition: %v", err)
		}
	}

	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity:         identity,
		ActionKey:        "complex.required.event",
		Amount:           1,
		Source:           "test",
		ExternalEventKey: "complex-required-complete",
		Now:              now,
	}); err != nil {
		t.Fatalf("complete required condition: %v", err)
	}

	list, err := service.User.ListActive(ctx, user.ListActiveParams{
		Identity: user.Identity(identity),
		Locale:   "ru",
		Now:      now,
	})
	if err != nil {
		t.Fatalf("list complex tasks: %v", err)
	}

	parent := findTask(t, list, "complex.parent")
	if parent.TargetCount != 1 || len(parent.Conditions) != 1 ||
		parent.Conditions[0].ID != requiredID {
		t.Fatalf("public required conditions are inconsistent: %+v", parent)
	}
	if parent.Progress == nil || parent.Progress.Progress != 1 ||
		parent.Progress.Status != repository.StatusReady {
		t.Fatalf("parent progress = %+v, want ready 1/1", parent.Progress)
	}
	if optional := findTask(t, list, "complex.optional"); optional.ID != optionalID {
		t.Fatalf("optional task must remain independently visible: %+v", optional)
	}

}

func TestTasksComplexProgressPropagatesThroughDeepGraph(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	now := time.Date(2026, 7, 5, 15, 0, 0, 0, time.UTC)
	workspaceID := testsupport.WorkspaceID("complex-deep-graph")
	identity := internalapi.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "deep-player",
	}

	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}
	leafID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"deep.leaf",
		"deep.leaf.event",
		1,
		repository.TaskKindInternal,
		repository.ActionKindAppAction,
		repository.ResetNever,
		20,
	)

	conditionID := leafID
	var rootID uint64
	for depth := 1; depth <= 9; depth++ {
		parentID := saveTaskForComplexTest(
			t,
			service,
			workspaceID,
			fmt.Sprintf("deep.parent.%d", depth),
			fmt.Sprintf("deep.parent.%d.composite", depth),
			1,
			repository.TaskKindComplex,
			repository.ActionKindComposite,
			repository.ResetNever,
			int32(20-depth),
		)
		if err := service.Admin.UpsertComplexCondition(ctx, admin.SaveComplexConditionParams{
			WorkspaceID:     workspaceID,
			ParentTaskID:    parentID,
			ConditionTaskID: conditionID,
			RequiredStatus:  repository.ComplexRequiredStatusReady,
			Position:        1,
			IsRequired:      true,
		}); err != nil {
			t.Fatalf("link complex depth %d: %v", depth, err)
		}

		conditionID = parentID
		rootID = parentID
	}

	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity:         identity,
		ActionKey:        "deep.leaf.event",
		Amount:           1,
		Source:           "test",
		ExternalEventKey: "deep-leaf-complete",
		Now:              now,
	}); err != nil {
		t.Fatalf("complete deep leaf: %v", err)
	}

	list, err := service.User.ListActive(ctx, user.ListActiveParams{
		Identity: user.Identity(identity),
		Locale:   "ru",
		Now:      now,
	})
	if err != nil {
		t.Fatalf("list deep graph: %v", err)
	}

	root := findTask(t, list, "deep.parent.9")
	if root.ID != rootID || root.Progress == nil ||
		root.Progress.Progress != 1 || root.Progress.Status != repository.StatusReady {
		t.Fatalf("deep root was not refreshed: %+v", root)
	}

}

func TestTasksRejectsUnsupportedComplexConfigurations(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("complex-invalid-config")

	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}
	parentID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"not.complex.parent",
		"not.complex.parent.event",
		1,
		repository.TaskKindInternal,
		repository.ActionKindAppAction,
		repository.ResetNever,
		1,
	)
	conditionID := saveTaskForComplexTest(
		t,
		service,
		workspaceID,
		"invalid.condition",
		"invalid.condition.event",
		1,
		repository.TaskKindInternal,
		repository.ActionKindAppAction,
		repository.ResetNever,
		2,
	)

	if err := service.Admin.UpsertComplexCondition(ctx, admin.SaveComplexConditionParams{
		WorkspaceID:     workspaceID,
		ParentTaskID:    parentID,
		ConditionTaskID: conditionID,
		RequiredStatus:  repository.ComplexRequiredStatusReady,
		Position:        1,
		IsRequired:      true,
	}); err == nil {
		t.Fatal("non-complex parent must be rejected")
	}

	for _, task := range []admin.SaveTaskParams{
		{
			WorkspaceID: workspaceID,
			Key:         "invalid.complex.auto",
			GroupKey:    "main",
			TaskKind:    repository.TaskKindComplex,
			ActionKey:   "invalid.complex.auto",
			ActionKind:  repository.ActionKindComposite,
			ClaimMode:   repository.ClaimModeAuto,
			StartMode:   repository.StartModeNone,
			TargetCount: 1,
			ResetUnit:   repository.ResetNever,
			ResetEvery:  1,
		},
		{
			WorkspaceID: workspaceID,
			Key:         "invalid.integration.auto",
			GroupKey:    "main",
			TaskKind:    repository.TaskKindChannelSubscribe,
			ActionKey:   "invalid.integration.auto",
			ActionKind:  repository.ActionKindChannelSubscribe,
			ClaimMode:   repository.ClaimModeAuto,
			StartMode:   repository.StartModeNone,
			TargetCount: 1,
			ResetUnit:   repository.ResetNever,
			ResetEvery:  1,
		},
	} {
		if _, err := service.Admin.SaveTask(ctx, task); err == nil {
			t.Fatalf("unsupported auto task was accepted: %+v", task)
		}
	}

}

type complexTaskOptions struct {
	ParentKey            string
	Conditions           []complexConditionSeed
	ParentRewardKey      string
	ParentRewardQuantity int64
	ResetUnit            string
	StartPosition        int32
}

type complexConditionSeed struct {
	Key            string
	ActionKey      string
	TargetCount    uint64
	RewardKey      string
	RewardQuantity int64
}

type complexTaskIDs struct {
	parentID     uint64
	conditionIDs []uint64
}

func createComplexTaskSet(t testing.TB, service *Tasks, workspaceID string, options complexTaskOptions) complexTaskIDs {
	t.Helper()
	ctx := context.Background()
	if options.ResetUnit == "" {
		options.ResetUnit = repository.ResetNever
	}
	if options.StartPosition == 0 {
		options.StartPosition = 1
	}
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	parentID := saveTaskForComplexTest(t, service, workspaceID, options.ParentKey, "complex."+options.ParentKey, uint64(len(options.Conditions)), repository.TaskKindComplex, repository.ActionKindComposite, options.ResetUnit, options.StartPosition)
	if err := service.Admin.UpsertReward(ctx, workspaceID, parentID, admin.RewardModel{Key: options.ParentRewardKey, Quantity: options.ParentRewardQuantity}, 1); err != nil {
		t.Fatalf("parent reward: %v", err)
	}
	ids := complexTaskIDs{parentID: parentID, conditionIDs: make([]uint64, 0, len(options.Conditions))}
	for index, condition := range options.Conditions {
		conditionID := saveTaskForComplexTest(t, service, workspaceID, condition.Key, condition.ActionKey, condition.TargetCount, repository.TaskKindInternal, repository.ActionKindAmountAction, options.ResetUnit, options.StartPosition+int32(index)+1)
		if condition.RewardKey != "" {
			if err := service.Admin.UpsertReward(ctx, workspaceID, conditionID, admin.RewardModel{Key: condition.RewardKey, Quantity: condition.RewardQuantity}, 1); err != nil {
				t.Fatalf("condition reward %s: %v", condition.Key, err)
			}
		}
		if err := service.Admin.UpsertComplexCondition(ctx, admin.SaveComplexConditionParams{
			WorkspaceID:     workspaceID,
			ParentTaskID:    parentID,
			ConditionTaskID: conditionID,
			RequiredStatus:  repository.ComplexRequiredStatusReady,
			Position:        int32(index + 1),
			IsRequired:      true,
		}); err != nil {
			t.Fatalf("complex condition %s: %v", condition.Key, err)
		}
		ids.conditionIDs = append(ids.conditionIDs, conditionID)
	}
	return ids
}

func saveTaskForComplexTest(t testing.TB, service *Tasks, workspaceID, key, actionKey string, targetCount uint64, taskKind, actionKind, resetUnit string, position int32) uint64 {
	t.Helper()
	ctx := context.Background()
	id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         key,
		GroupKey:    "main",
		TaskKind:    taskKind,
		ActionKey:   actionKey,
		ActionKind:  actionKind,
		ClaimMode:   repository.ClaimModeManual,
		StartMode:   repository.StartModeNone,
		TargetCount: targetCount,
		ResetUnit:   resetUnit,
		ResetEvery:  1,
		Position:    position,
		IsVisible:   true,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("task %s: %v", key, err)
	}
	if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", key, key); err != nil {
		t.Fatalf("localization %s: %v", key, err)
	}
	return id
}

const (
	exportImportDB = "tasks_export_import_test"
)

func TestExportSectionsDefaultsToFullCatalog(t *testing.T) {
	manifest := (&repository.Repository{}).ExportManifest()
	sections := make(map[string]bool, len(manifest.Sections))
	for _, section := range manifest.Sections {
		sections[section.Key] = section.DefaultEnabled
	}
	for _, key := range []string{
		repository.ExportSectionGroups,
		repository.ExportSectionTasks,
		repository.ExportSectionSequences,
		repository.ExportSectionLocalization,
		repository.ExportSectionRewards,
		repository.ExportSectionTarget,
		repository.ExportSectionIntegration,
		repository.ExportSectionPartnerConfigs,
		repository.ExportSectionPartnerRewards,
	} {
		if !sections[key] {
			t.Fatalf("default export section %q disabled", key)
		}
	}
}

func TestValidateExportPackageRequiresSequencePair(t *testing.T) {
	repo := newExportImportRepository(t)
	sequenceKey := "chain"
	if _, err := repo.PreviewImport(context.Background(), "validation", repository.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "tasks",
		Groups: []repository.ExportGroup{{
			Key: "daily",
			Tasks: []repository.ExportTask{{
				Key:         "task",
				SequenceKey: &sequenceKey,
			}},
		}},
	}); err == nil {
		t.Fatal("sequence_key without sequence_position must fail")
	}
}

func TestRequireImportSecrets(t *testing.T) {
	repo := newExportImportRepository(t)
	embedded := "embedded-token"
	workspaceID := testsupport.WorkspaceID("secrets")
	embeddedWorkspaceID := testsupport.WorkspaceID("secrets-embedded")
	pkg := repository.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "tasks",
		Groups: []repository.ExportGroup{{
			Key: "daily",
			PartnerConfigs: []repository.ExportPartnerConfig{{
				Provider: "tgrass",
				Platform: "telegram",
				Secret:   &repository.ExportSecret{Mode: "required", Key: "tasks.partner.tgrass.daily.telegram.secret"},
			}},
		}},
	}
	preview, err := repo.PreviewImport(context.Background(), workspaceID, pkg)
	if err != nil {
		t.Fatalf("preview secrets: %v", err)
	}
	if len(preview.RequiredSecrets) != 1 || preview.RequiredSecrets[0].Key != "tasks.partner.tgrass.daily.telegram.secret" {
		t.Fatalf("bad required secrets: %+v", preview.RequiredSecrets)
	}
	_, err = repo.Import(context.Background(), workspaceID, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictFail,
		Secrets:          map[string]string{"tasks.partner.tgrass.daily.telegram.secret": "token"},
	})
	if err != nil {
		t.Fatalf("secret should satisfy import requirement: %v", err)
	}

	pkg.Groups[0].PartnerConfigs[0].Secret.Value = &embedded
	preview, err = repo.PreviewImport(context.Background(), embeddedWorkspaceID, pkg)
	if err != nil {
		t.Fatalf("preview embedded secrets: %v", err)
	}
	if len(preview.RequiredSecrets) != 0 {
		t.Fatalf("embedded secret must not be required: %+v", preview.RequiredSecrets)
	}
	_, err = repo.Import(context.Background(), embeddedWorkspaceID, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictFail,
	})
	if err != nil {
		t.Fatalf("embedded secret should satisfy import requirement: %v", err)
	}
	config, found, err := repo.GetPartnerConfig(
		context.Background(),
		embeddedWorkspaceID,
		"tgrass",
		"daily",
		"telegram",
	)
	if err != nil || !found || config.Secret == nil || *config.Secret != embedded {
		t.Fatalf("embedded secret was not imported: found=%t config=%+v err=%v", found, config, err)
	}
}

func TestExportImportFullCycle(t *testing.T) {
	repo := newExportImportRepository(t)
	ctx := context.Background()
	sourceWorkspace := testsupport.WorkspaceID("source")
	targetWorkspace := testsupport.WorkspaceID("target")
	seedExportSource(t, repo, sourceWorkspace)

	pkg, err := repo.Export(ctx, sourceWorkspace, repository.ExportRequest{Now: time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if pkg.Format != repository.ExportFormat || pkg.Service != "tasks" || len(pkg.Groups) != 1 || len(pkg.Sequences) != 1 {
		t.Fatalf("unexpected export package: %+v", pkg)
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	if strings.Contains(string(raw), "source-token") {
		t.Fatalf("export must not contain secret value: %s", raw)
	}
	withSecrets, err := repo.Export(ctx, sourceWorkspace, repository.ExportRequest{IncludeSecrets: true})
	if err != nil {
		t.Fatalf("export with secrets: %v", err)
	}
	if len(withSecrets.Groups) != 1 || len(withSecrets.Groups[0].PartnerConfigs) != 1 ||
		withSecrets.Groups[0].PartnerConfigs[0].Secret == nil ||
		withSecrets.Groups[0].PartnerConfigs[0].Secret.Value == nil ||
		*withSecrets.Groups[0].PartnerConfigs[0].Secret.Value != "source-token" ||
		withSecrets.Groups[0].PartnerConfigs[0].WebhookSecret == nil ||
		withSecrets.Groups[0].PartnerConfigs[0].WebhookSecret.Value == nil ||
		*withSecrets.Groups[0].PartnerConfigs[0].WebhookSecret.Value != "source-webhook-secret" {
		t.Fatalf("export with secrets missed values: %+v", withSecrets.Groups[0].PartnerConfigs)
	}

	preview, err := repo.PreviewImport(ctx, targetWorkspace, pkg)
	if err != nil {
		t.Fatalf("preview import: %v", err)
	}
	if preview.Counts.Groups != 1 ||
		preview.Counts.Sequences != 1 ||
		preview.Counts.Tasks != 1 ||
		preview.Counts.GroupLocalizations != 2 ||
		preview.Counts.TaskLocalizations != 2 ||
		preview.Counts.Rewards != 1 ||
		preview.Counts.PartnerConfigs != 1 ||
		preview.Counts.PartnerRewards != 1 {
		t.Fatalf("bad preview counts: %+v", preview.Counts)
	}
	if len(preview.RequiredSecrets) != 2 {
		t.Fatalf("required secrets = %+v, want 2", preview.RequiredSecrets)
	}
	if _, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictFail,
	}); err == nil {
		t.Fatal("import without required secret must fail")
	}

	secrets := exportImportSecretMap(preview.RequiredSecrets, "target-token")
	result, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictFail,
		Secrets:          secrets,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Imported.Tasks != 1 || result.Imported.Rewards != 1 || result.Imported.PartnerConfigs != 1 {
		t.Fatalf("bad import result: %+v", result)
	}

	imported, err := repo.Export(ctx, targetWorkspace, repository.ExportRequest{})
	if err != nil {
		t.Fatalf("export imported workspace: %v", err)
	}
	assertImportedCatalog(t, imported)
	config, found, err := repo.GetPartnerConfig(ctx, targetWorkspace, "tgrass", "daily", "telegram")
	if err != nil || !found || config.Secret == nil || *config.Secret != "target-token" {
		t.Fatalf("bad imported partner config: found=%t config=%+v err=%v", found, config, err)
	}
	if config.WebhookSecret == nil || *config.WebhookSecret != "target-token" {
		t.Fatalf("bad imported webhook secret: %+v", config)
	}

	conflictPreview, err := repo.PreviewImport(ctx, targetWorkspace, pkg)
	if err != nil {
		t.Fatalf("conflict preview: %v", err)
	}
	if len(conflictPreview.Conflicts) == 0 {
		t.Fatal("preview after import must report conflicts")
	}
	if _, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictFail,
		Secrets:          secrets,
	}); err == nil {
		t.Fatal("fail_on_conflict must reject existing catalog")
	}
	skipped, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictSkip,
		Secrets:          secrets,
	})
	if err != nil {
		t.Fatalf("skip existing import: %v", err)
	}
	if skipped.Skipped.Tasks != 1 || skipped.Skipped.Groups != 1 || skipped.Skipped.PartnerConfigs != 1 {
		t.Fatalf("bad skipped result: %+v", skipped)
	}

	pkg.Groups[0].Localization["ru"] = repository.ExportText{Title: "Обновленные", Description: "Обновленное описание"}
	pkg.Groups[0].Tasks[0].Rewards[0].Quantity = 777
	updatedSecrets := exportImportSecretMap(preview.RequiredSecrets, "updated-token")
	updated, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
		Secrets:          updatedSecrets,
	})
	if err != nil {
		t.Fatalf("update existing import: %v", err)
	}
	if updated.Imported.Tasks != 1 || updated.Imported.Rewards != 1 {
		t.Fatalf("bad update result: %+v", updated)
	}
	afterUpdate, err := repo.Export(ctx, targetWorkspace, repository.ExportRequest{})
	if err != nil {
		t.Fatalf("export after update: %v", err)
	}
	if afterUpdate.Groups[0].Localization["ru"].Title != "Обновленные" {
		t.Fatalf("group localization was not updated: %+v", afterUpdate.Groups[0].Localization)
	}
	if afterUpdate.Groups[0].Tasks[0].Rewards[0].Quantity != 777 {
		t.Fatalf("reward was not updated: %+v", afterUpdate.Groups[0].Tasks[0].Rewards[0])
	}

	pkg.Groups[0].Localization = nil
	pkg.Groups[0].Tasks[0].Localization = nil
	pkg.Groups[0].Tasks[0].Rewards = nil
	pkg.Groups[0].Tasks[0].Conditions = nil
	pkg.Groups[0].PartnerConfigs = nil
	pkg.Groups[0].PartnerRewardRules = nil
	if _, err := repo.Import(ctx, targetWorkspace, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("replace imported task aggregate: %v", err)
	}
	replaced, err := repo.Export(ctx, targetWorkspace, repository.ExportRequest{})
	if err != nil {
		t.Fatalf("export replaced task aggregate: %v", err)
	}
	if len(replaced.Groups) != 1 ||
		len(replaced.Groups[0].Localization) != 0 ||
		len(replaced.Groups[0].Tasks[0].Localization) != 0 ||
		len(replaced.Groups[0].Tasks[0].Rewards) != 0 ||
		len(replaced.Groups[0].PartnerConfigs) != 0 ||
		len(replaced.Groups[0].PartnerRewardRules) != 0 {
		t.Fatalf("update_existing kept removed task children: %+v", replaced.Groups)
	}
}

func TestExportSectionsAndInvalidImportFormats(t *testing.T) {
	repo := newExportImportRepository(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("sections")
	seedExportSource(t, repo, workspaceID)

	pkg, err := repo.Export(ctx, workspaceID, repository.ExportRequest{
		Sections: []string{repository.ExportSectionGroups, repository.ExportSectionTasks},
	})
	if err != nil {
		t.Fatalf("section export: %v", err)
	}
	if len(pkg.Sequences) != 0 {
		t.Fatalf("sequences must be omitted: %+v", pkg.Sequences)
	}
	if len(pkg.Groups) != 1 || len(pkg.Groups[0].Tasks) != 1 {
		t.Fatalf("tasks must be exported inside groups: %+v", pkg)
	}
	task := pkg.Groups[0].Tasks[0]
	if task.SequenceKey != nil || task.SequencePosition != nil {
		t.Fatalf("sequence fields must be omitted: %+v", task)
	}
	if len(task.Rewards) != 0 || len(task.Localization) != 0 || len(pkg.Groups[0].PartnerConfigs) != 0 {
		t.Fatalf("disabled sections leaked into export: %+v", pkg.Groups[0])
	}
	if len(task.Target) != 0 || len(task.Integration.Payload) != 0 || task.Integration.Provider != nil {
		t.Fatalf("target/integration must be omitted: %+v", task)
	}

	if _, err := repo.PreviewImport(ctx, workspaceID, repository.ExportPackage{Format: "tasks.export.v2", Service: "tasks"}); err == nil {
		t.Fatal("unsupported format must fail")
	}
	if _, err := repo.PreviewImport(ctx, workspaceID, repository.ExportPackage{Format: repository.ExportFormat, Service: "cpa"}); err == nil {
		t.Fatal("wrong service must fail")
	}
	position := uint32(1)
	if _, err := repo.PreviewImport(ctx, workspaceID, repository.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "tasks",
		Groups: []repository.ExportGroup{{
			Key: "broken",
			Tasks: []repository.ExportTask{{
				Key:              "broken_task",
				SequencePosition: &position,
			}},
		}},
	}); err == nil {
		t.Fatal("sequence_position without sequence_key must fail")
	}
}

func TestTasksImportRejectsConditionsOnNonComplexTask(t *testing.T) {

	repo := newExportImportRepository(t)
	parent := tasksImportTestTask("ordinary-parent")
	parent.Conditions = []repository.ExportCondition{
		{
			TaskKey:        "ordinary-child",
			RequiredStatus: repository.ComplexRequiredStatusReady,
			Position:       1,
			IsRequired:     true,
		},
	}
	pkg := repository.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "tasks",
		Groups: []repository.ExportGroup{
			{
				Key:      "main",
				Position: 1,
				IsActive: true,
				Tasks: []repository.ExportTask{
					parent,
					tasksImportTestTask("ordinary-child"),
				},
			},
		},
	}

	_, err := repo.PreviewImport(
		context.Background(),
		testsupport.WorkspaceID("import-non-complex-condition"),
		pkg,
	)
	if err == nil || !strings.Contains(err.Error(), `conditions require task_kind "complex"`) {
		t.Fatalf("non-complex conditions error = %v", err)
	}

}

func TestImportDailyTasksExampleAndExport(t *testing.T) {
	repo := newExportImportRepository(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("daily-example")

	raw, err := os.ReadFile(filepath.Join("examples", "daily_tasks_import.json"))
	if err != nil {
		t.Fatalf("read daily example: %v", err)
	}
	var req repository.ImportRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal daily example: %v", err)
	}
	pkg := req.Package
	if req.ConflictStrategy == "" {
		t.Fatal("daily example import request must contain conflict_strategy")
	}
	if len(req.Secrets) != 0 {
		t.Fatalf("daily example import request secrets = %d, want 0", len(req.Secrets))
	}

	preview, err := repo.PreviewImport(ctx, workspaceID, pkg)
	if err != nil {
		t.Fatalf("preview daily example: %v", err)
	}
	if preview.Counts.Groups != 2 ||
		preview.Counts.Sequences != 0 ||
		preview.Counts.Tasks != 25 ||
		preview.Counts.GroupLocalizations != 4 ||
		preview.Counts.TaskLocalizations != 50 ||
		preview.Counts.Rewards != 21 ||
		preview.Counts.Conditions != 4 ||
		preview.Counts.PartnerConfigs != 3 ||
		preview.Counts.PartnerRewards != 3 {
		t.Fatalf("bad daily preview counts: %+v", preview.Counts)
	}
	if len(preview.Conflicts) != 0 || len(preview.Warnings) != 0 || len(preview.RequiredSecrets) != 6 {
		t.Fatalf("daily preview should be clean: %+v", preview)
	}
	secrets := exportImportSecretMap(preview.RequiredSecrets, "example-secret")
	result, err := repo.Import(ctx, workspaceID, repository.ImportRequest{
		Package:          pkg,
		ConflictStrategy: req.ConflictStrategy,
		Secrets:          secrets,
	})
	if err != nil {
		t.Fatalf("import daily example: %v", err)
	}
	if result.Imported.Groups != 2 ||
		result.Imported.Tasks != 25 ||
		result.Imported.GroupLocalizations != 4 ||
		result.Imported.TaskLocalizations != 50 ||
		result.Imported.Rewards != 21 ||
		result.Imported.Conditions != 4 ||
		result.Imported.PartnerConfigs != 3 ||
		result.Imported.PartnerRewards != 3 {
		t.Fatalf("bad daily import result: %+v", result)
	}

	exported, err := repo.Export(ctx, workspaceID, repository.ExportRequest{})
	if err != nil {
		t.Fatalf("export daily example: %v", err)
	}
	assertDailyExampleExport(t, pkg, exported)
}

func seedExportSource(t *testing.T, repo *repository.Repository, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	if err := repo.UpsertGroup(ctx, workspaceID, "daily", 10, true); err != nil {
		t.Fatalf("upsert group: %v", err)
	}
	if err := repo.UpsertGroupLocalization(ctx, workspaceID, "daily", "ru", "Ежедневные", "Ежедневные задания"); err != nil {
		t.Fatalf("upsert group ru localization: %v", err)
	}
	if err := repo.UpsertGroupLocalization(ctx, workspaceID, "daily", "en", "Daily", "Daily tasks"); err != nil {
		t.Fatalf("upsert group en localization: %v", err)
	}
	if err := repo.UpsertSequence(ctx, workspaceID, "daily_chain", 10, true); err != nil {
		t.Fatalf("upsert sequence: %v", err)
	}
	position := uint32(1)
	provider := "http"
	taskID, err := repo.SaveTask(ctx, repository.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         "subscribe_tg",
		GroupKey:    "daily",
		SequenceKey: exportStrPtr("daily_chain"), SequencePosition: &position,
		TaskKind:            repository.TaskKindChannelSubscribe,
		ActionKey:           "telegram.subscribe",
		ActionKind:          repository.ActionKindChannelSubscribe,
		ClaimMode:           repository.ClaimModeManual,
		TargetCount:         1,
		ResetUnit:           repository.ResetNever,
		ResetEvery:          1,
		Position:            10,
		Payload:             json.RawMessage(`{"channel_url":"https://t.me/example"}`),
		Target:              json.RawMessage(`{"platform":["tma",12],"loc":["ru"]}`),
		IntegrationKind:     exportStrPtr("channel"),
		IntegrationProvider: &provider,
		IntegrationPayload:  json.RawMessage(`{"url":"https://partner.example/check","secret":"private"}`),
		ImageURL:            exportStrPtr("https://example.com/image.png"),
		IsVisible:           true,
		IsActive:            true,
	})
	if err != nil {
		t.Fatalf("save task: %v", err)
	}
	if err := repo.UpsertTaskLocalization(ctx, workspaceID, taskID, "ru", "Подпишись", "Подпишись на канал"); err != nil {
		t.Fatalf("upsert task ru localization: %v", err)
	}
	if err := repo.UpsertTaskLocalization(ctx, workspaceID, taskID, "en", "Subscribe", "Subscribe to channel"); err != nil {
		t.Fatalf("upsert task en localization: %v", err)
	}
	if err := repo.UpsertReward(ctx, workspaceID, taskID, repository.Reward{Key: "coin", Type: "quantity", Quantity: 100, Scale: 2}, 10); err != nil {
		t.Fatalf("upsert reward: %v", err)
	}
	secret := "source-token"
	webhookSecret := "source-webhook-secret"
	if err := repo.SavePartnerConfig(ctx, repository.SavePartnerConfigParams{
		WorkspaceID:   workspaceID,
		Provider:      "tgrass",
		GroupKey:      "daily",
		Platform:      "telegram",
		IsEnabled:     true,
		Secret:        &secret,
		WebhookSecret: &webhookSecret,
		Target:        json.RawMessage(`{"platform":"tma"}`),
		Settings:      json.RawMessage(`{"limit":5}`),
	}); err != nil {
		t.Fatalf("save partner config: %v", err)
	}
	if err := repo.SavePartnerRewardRule(ctx, repository.SavePartnerRewardRuleParams{
		WorkspaceID:  workspaceID,
		Provider:     "tgrass",
		GroupKey:     "daily",
		ExternalType: "*",
		Reward:       repository.Reward{Key: "coin", Type: "quantity", Quantity: 50, Scale: 2},
		Position:     10,
		IsEnabled:    true,
	}); err != nil {
		t.Fatalf("save partner reward rule: %v", err)
	}
}

func assertDailyExampleExport(t *testing.T, imported, exported repository.ExportPackage) {
	t.Helper()
	if exported.Format != repository.ExportFormat || exported.Service != "tasks" {
		t.Fatalf("bad exported header: %+v", exported)
	}
	if len(exported.Sequences) != 0 {
		t.Fatalf("daily tasks must be standalone, got sequences: %+v", exported.Sequences)
	}
	if len(imported.Groups) != 2 || len(exported.Groups) != 2 {
		t.Fatalf("bad group counts: imported=%d exported=%d", len(imported.Groups), len(exported.Groups))
	}
	importedByGroup := make(map[string]repository.ExportGroup, len(imported.Groups))
	exportedByGroup := make(map[string]repository.ExportGroup, len(exported.Groups))
	for _, group := range imported.Groups {
		importedByGroup[group.Key] = group
	}
	for _, group := range exported.Groups {
		exportedByGroup[group.Key] = group
	}
	expectedGroup := importedByGroup["daily"]
	actualGroup := exportedByGroup["daily"]
	if actualGroup.Key != expectedGroup.Key || actualGroup.Localization["ru"].Title != "Ежедневные задания" ||
		actualGroup.Localization["en"].Title != "Daily tasks" {
		t.Fatalf("bad exported daily group: %+v", actualGroup)
	}
	if len(actualGroup.Tasks) != 20 {
		t.Fatalf("exported tasks = %d, want 20", len(actualGroup.Tasks))
	}
	expectedByKey := make(map[string]repository.ExportTask, len(expectedGroup.Tasks))
	for _, task := range expectedGroup.Tasks {
		expectedByKey[task.Key] = task
	}
	for _, task := range actualGroup.Tasks {
		expected, ok := expectedByKey[task.Key]
		if !ok {
			t.Fatalf("unexpected exported task: %+v", task)
		}
		if task.SequenceKey != nil || task.SequencePosition != nil {
			t.Fatalf("daily task must not be sequential: %+v", task)
		}
		if len(task.Target) != 0 {
			t.Fatalf("daily task must not have target: key=%s target=%s", task.Key, task.Target)
		}
		if task.Localization["ru"].Title != expected.Localization["ru"].Title ||
			task.Localization["en"].Title != expected.Localization["en"].Title {
			t.Fatalf("bad localization for %s: %+v", task.Key, task.Localization)
		}
		if len(task.Rewards) != 1 || len(expected.Rewards) != 1 {
			t.Fatalf("bad rewards for %s: actual=%+v expected=%+v", task.Key, task.Rewards, expected.Rewards)
		}
		if task.Rewards[0].Key != "stars" ||
			task.Rewards[0].Type != "quantity" ||
			task.Rewards[0].Quantity != expected.Rewards[0].Quantity ||
			task.Rewards[0].Scale != 2 {
			t.Fatalf("bad reward for %s: actual=%+v expected=%+v", task.Key, task.Rewards[0], expected.Rewards[0])
		}
		if task.Reset.Unit != repository.ResetDay || task.Reset.Every != 1 {
			t.Fatalf("daily task must reset daily: key=%s reset=%+v", task.Key, task.Reset)
		}
	}

	complexGroup := exportedByGroup["complex"]
	if complexGroup.Key != "complex" || len(complexGroup.Tasks) != 5 {
		t.Fatalf("bad complex group: %+v", complexGroup)
	}
	var parent *repository.ExportTask
	for index := range complexGroup.Tasks {
		task := &complexGroup.Tasks[index]
		if task.Key == "complex.bear_gift" {
			parent = task
		}
	}
	if parent == nil {
		t.Fatal("complex parent task was not exported")
	}
	if parent.TaskKind != repository.TaskKindComplex || parent.ActionKind != repository.ActionKindComposite || len(parent.Conditions) != 4 {
		t.Fatalf("bad complex parent task: %+v", parent)
	}
	if len(parent.Rewards) != 1 || parent.Rewards[0].Key != "gift.bear" || parent.Rewards[0].Quantity != 1 {
		t.Fatalf("bad complex reward: %+v", parent.Rewards)
	}
	subscribeTask, ok := expectedByKey["daily.subscribe_channel"]
	if !ok || len(subscribeTask.Integration.Payload) == 0 || subscribeTask.Integration.Provider == nil {
		t.Fatalf("daily subscribe task must contain integration payload: %+v", subscribeTask)
	}
}

func assertImportedCatalog(t *testing.T, pkg repository.ExportPackage) {
	t.Helper()
	if len(pkg.Groups) != 1 || pkg.Groups[0].Key != "daily" {
		t.Fatalf("bad imported groups: %+v", pkg.Groups)
	}
	group := pkg.Groups[0]
	if group.Localization["ru"].Title != "Ежедневные" || group.Localization["en"].Title != "Daily" {
		t.Fatalf("bad group localization: %+v", group.Localization)
	}
	if len(group.Tasks) != 1 || group.Tasks[0].Key != "subscribe_tg" {
		t.Fatalf("bad imported tasks: %+v", group.Tasks)
	}
	task := group.Tasks[0]
	if task.SequenceKey == nil || *task.SequenceKey != "daily_chain" || task.SequencePosition == nil || *task.SequencePosition != 1 {
		t.Fatalf("bad task sequence fields: %+v", task)
	}
	if task.Localization["ru"].Title != "Подпишись" || len(task.Rewards) != 1 ||
		task.Rewards[0].Quantity != 100 || task.Rewards[0].Scale != 2 {
		t.Fatalf("bad task localized rewards: %+v", task)
	}
	if len(group.PartnerConfigs) != 1 || group.PartnerConfigs[0].Secret == nil {
		t.Fatalf("bad partner configs: %+v", group.PartnerConfigs)
	}
	if len(group.PartnerRewardRules) != 1 || group.PartnerRewardRules[0].Reward.Quantity != 50 ||
		group.PartnerRewardRules[0].Reward.Scale != 2 {
		t.Fatalf("bad partner rewards: %+v", group.PartnerRewardRules)
	}
}

func newExportImportRepository(t *testing.T) *repository.Repository {
	t.Helper()
	ctx := context.Background()
	adminDB, err := openExportImportPostgres("postgres")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}
	if err := recreateTasksDatabase(ctx, adminDB, exportImportDB); err != nil {
		t.Fatalf("recreate database: %v", err)
	}
	_ = adminDB.Close()
	db, err := openExportImportPostgres(exportImportDB)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	client, err := sqlwrap.New(db, sqlwrap.Options{CacheEnabled: true, CacheSize: 1000, CacheTTLCheck: time.Minute})
	if err != nil {
		t.Fatalf("sqlwrap: %v", err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = repo.Close()
		_ = client.Close()
	})
	return repo
}

func openExportImportPostgres(database string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		pgUser,
		pgPassword,
		pgHost,
		pgPort,
		database,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func exportStrPtr(value string) *string { return &value }

func exportImportSecretMap(required []repository.ExportSecret, value string) map[string]string {
	out := make(map[string]string, len(required))
	webhookSecrets := 0
	for _, secret := range required {
		if strings.HasSuffix(secret.Key, ".webhook_secret") {
			webhookSecrets++
		}
	}
	for index, secret := range required {
		secretValue := value
		if webhookSecrets > 1 && strings.HasSuffix(secret.Key, ".webhook_secret") {
			secretValue += "-" + strconv.Itoa(index+1)
		}
		out[secret.Key] = secretValue
	}
	return out
}

func TestTasksIntegrationChannelSubscriptionClaim(t *testing.T) {
	checker := &fakeChannelChecker{completed: true}
	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ChannelCheckers: map[string]integration.ChannelSubscriptionChecker{"telegram": checker},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-channel")
	identity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "telegram-user"}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "subscribe_telegram",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "subscribe:telegram:channel-a",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "telegram",
		PublicPayload: map[string]any{
			"channel": "@channel_a",
			"url":     "https://t.me/channel_a",
		},
		PrivatePayload: map[string]any{
			"bot_token": "secret",
			"channel":   "@channel_a",
		},
	})

	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	publicTask := findTask(t, list, "subscribe_telegram")
	if publicTask.TaskKind != repository.TaskKindChannelSubscribe {
		t.Fatalf("public task kind = %q", publicTask.TaskKind)
	}
	if string(publicTask.Payload) == "" || string(publicTask.Payload) == "null" {
		t.Fatalf("public payload not returned: %s", publicTask.Payload)
	}

	adminTask, err := service.Admin.GetTask(ctx, workspaceID, taskID)
	if err != nil {
		t.Fatalf("admin get task: %v", err)
	}
	if adminTask.IntegrationProvider == nil || *adminTask.IntegrationProvider != "telegram" {
		t.Fatalf("admin provider = %+v", adminTask.IntegrationProvider)
	}
	if len(adminTask.IntegrationPayload) == 0 || string(adminTask.IntegrationPayload) == "null" {
		t.Fatalf("admin private payload not returned: %s", adminTask.IntegrationPayload)
	}

	result, err := service.Integration.CheckChannelSubscription(ctx, integration.CheckChannelSubscriptionParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("check channel: %v", err)
	}
	if result.Status != repository.StatusReady || !result.Completed || result.Task == nil {
		t.Fatalf("unexpected channel result: %+v", result)
	}
	if checker.calls != 1 || checker.lastTask.IntegrationProvider == nil || *checker.lastTask.IntegrationProvider != "telegram" {
		t.Fatalf("checker did not receive private task context: calls=%d task=%+v", checker.calls, checker.lastTask)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: "telegram-claim-1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("claim reward: %v", err)
	}
	if claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("unexpected claim result: %+v", claim)
	}

	confirmed, err := service.Integration.ConfirmCompletion(ctx, integration.ConfirmCompletionParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: "subscribe_telegram", Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("confirm completion: %v", err)
	}
	if !confirmed.Completed || confirmed.Status != repository.StatusClaimed || confirmed.OperationID == nil || *confirmed.OperationID != "telegram-claim-1" {
		t.Fatalf("unexpected confirmation: %+v", confirmed)
	}
}

func TestTasksIntegrationCheckDispatchesByActionKind(t *testing.T) {
	checker := &fakeChannelChecker{completed: true}
	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ChannelCheckers: map[string]integration.ChannelSubscriptionChecker{"telegram": checker},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-generic-check")
	identity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "telegram-user"}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "generic_subscribe_telegram",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "subscribe:telegram:generic",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "telegram",
		PublicPayload: map[string]any{
			"channel": "@generic",
			"url":     "https://t.me/generic",
		},
		PrivatePayload: map[string]any{
			"chat_id": "@generic",
			"tokens":  []string{"secret"},
		},
	})

	result, err := service.Integration.Check(ctx, integration.CheckParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("generic check: %v", err)
	}
	if result.Status != repository.StatusReady || !result.Completed || result.Task == nil {
		t.Fatalf("unexpected generic result: %+v", result)
	}
	if checker.calls != 1 || checker.lastTask.ActionKind != repository.ActionKindChannelSubscribe {
		t.Fatalf("generic check did not dispatch to channel checker: calls=%d task=%+v", checker.calls, checker.lastTask)
	}
}

func TestTasksIntegrationCheckUsesStoredProvider(t *testing.T) {
	storedChecker := &fakeChannelChecker{completed: true}
	overrideChecker := &fakeChannelChecker{completed: false}
	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ChannelCheckers: map[string]integration.ChannelSubscriptionChecker{
				"stored":   storedChecker,
				"override": overrideChecker,
			},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("integration-stored-provider")
	identity := integration.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "player",
	}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "stored-provider-task",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "stored-provider-action",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "stored",
	})

	result, err := service.Integration.Check(ctx, integration.CheckParams{
		TaskRefParams: integration.TaskRefParams{
			Identity: identity,
			TaskRef:  strconv.FormatUint(taskID, 10),
			Now:      time.Now(),
		},
		Provider: "override",
	})
	if err != nil {
		t.Fatalf("check with provider override: %v", err)
	}
	if result.Status != repository.StatusReady || !result.Completed {
		t.Fatalf("stored provider result: %+v", result)
	}
	if storedChecker.calls != 1 || overrideChecker.calls != 0 {
		t.Fatalf(
			"checker calls stored=%d override=%d, want 1 and 0",
			storedChecker.calls,
			overrideChecker.calls,
		)
	}
}

func TestTasksIntegrationChannelBoostClaim(t *testing.T) {
	checker := &fakeChannelBoostChecker{completed: true}
	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ChannelBoostCheckers: map[string]integration.ChannelBoostChecker{"telegram": checker},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-channel-boost")
	identity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "telegram-user"}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "boost_telegram",
		TaskKind:    repository.TaskKindChannelBoost,
		ActionKey:   "tg",
		ActionKind:  repository.ActionKindChannelBoost,
		Provider:    "telegram",
		PublicPayload: map[string]any{
			"channel": "@boosted",
			"url":     "https://t.me/boosted?boost",
		},
		PrivatePayload: map[string]any{
			"chat_id": "@boosted",
			"tokens":  []string{"secret"},
		},
	})

	result, err := service.Integration.CheckChannelBoost(ctx, integration.CheckChannelBoostParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("check boost: %v", err)
	}
	if result.Status != repository.StatusReady || !result.Completed || result.Task == nil {
		t.Fatalf("unexpected boost result: %+v", result)
	}
	if checker.calls != 1 || checker.lastTask.ActionKind != repository.ActionKindChannelBoost {
		t.Fatalf("checker did not receive boost task context: calls=%d task=%+v", checker.calls, checker.lastTask)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: "boost-claim-1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("claim boost reward: %v", err)
	}
	if claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("unexpected boost claim result: %+v", claim)
	}
}

func TestTasksIntegrationExternalHTTPCheck(t *testing.T) {
	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedToken = r.Header.Get("X-Partner-Token")
		if r.Method != http.MethodPost || r.URL.Query().Get("user") != "external-user" {
			t.Fatalf("unexpected request: method=%s query=%s", r.Method, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "level": 5})
	}))
	defer server.Close()

	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ExternalCheckers: map[string]integration.ExternalTaskChecker{
				"http": integration.HTTPChecker{Client: server.Client()},
			},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-http")
	identity := integration.Identity{WorkspaceID: workspaceID, AppID: 7, PlatformID: 9, PlatformUserID: "external-user"}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "reach_level_5",
		TaskKind:    repository.TaskKindExternalCheck,
		ActionKey:   "external:level:5",
		ActionKind:  repository.ActionKindExternal,
		Provider:    "http",
		PublicPayload: map[string]any{
			"app_url": "https://partner.example/game",
		},
		PrivatePayload: integration.HTTPCheckPayload{
			Request: integration.HTTPCheckRequest{
				Method:  http.MethodPost,
				URL:     server.URL + "/check",
				Headers: map[string]string{"X-Partner-Token": "${token}"},
				Query:   map[string]string{"user": "${user}", "task": "${task_key}", "time": "${time_unix}"},
				Body:    json.RawMessage(`{"workspace":"${workspace}","app":"${app}"}`),
			},
			Success: integration.HTTPCheckSuccess{StatusCodes: []int{200}, JSONPath: "ok", Equals: true},
		},
	})

	result, err := service.Integration.CheckExternal(ctx, integration.CheckExternalParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
		Variables:     map[string]string{"token": "secret-token"},
	})
	if err != nil {
		t.Fatalf("check external: %v", err)
	}
	if result.Status != repository.StatusReady || !result.Completed {
		t.Fatalf("unexpected external result: %+v", result)
	}
	if receivedToken != "secret-token" {
		t.Fatalf("token header = %q", receivedToken)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: "external-claim-1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("claim external reward: %v", err)
	}
	if claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("unexpected external claim: %+v", claim)
	}
}

func TestTasksIntegrationExternalHTTPCheckRejectsDifferentJSONType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":"true"}`))
	}))
	defer server.Close()

	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ExternalCheckers: map[string]integration.ExternalTaskChecker{
				"http": integration.HTTPChecker{Client: server.Client()},
			},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-http-json-type")
	identity := integration.Identity{
		WorkspaceID:    workspaceID,
		AppID:          7,
		PlatformID:     9,
		PlatformUserID: "external-json-type-user",
	}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "external_json_type",
		TaskKind:    repository.TaskKindExternalCheck,
		ActionKey:   "external:json:type",
		ActionKind:  repository.ActionKindExternal,
		Provider:    "http",
		PrivatePayload: integration.HTTPCheckPayload{
			Request: integration.HTTPCheckRequest{
				Method: http.MethodGet,
				URL:    server.URL + "/check",
			},
			Success: integration.HTTPCheckSuccess{
				StatusCodes: []int{http.StatusOK},
				JSONPath:    "ok",
				Equals:      true,
			},
		},
	})

	result, err := service.Integration.CheckExternal(ctx, integration.CheckExternalParams{
		TaskRefParams: integration.TaskRefParams{
			Identity: identity,
			TaskRef:  strconv.FormatUint(taskID, 10),
			Now:      time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("check external task with mismatched JSON type: %v", err)
	}
	if result.Status != integration.StatusNotCompleted || result.Completed {
		t.Fatalf("mismatched JSON type result = %+v, want not completed", result)
	}
}

func TestTasksIntegrationNotCompleted(t *testing.T) {
	checker := &fakeExternalChecker{completed: false}
	service := newTasksTestService(t, Options{
		Integration: integration.Options{
			ExternalCheckers: map[string]integration.ExternalTaskChecker{"fake": checker},
		},
	})
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-integration-not-completed")
	identity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	taskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "external_not_done",
		TaskKind:    repository.TaskKindExternalCheck,
		ActionKey:   "external:not_done",
		ActionKind:  repository.ActionKindExternal,
		Provider:    "fake",
	})

	result, err := service.Integration.CheckExternal(ctx, integration.CheckExternalParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("check external not completed: %v", err)
	}
	if result.Status != integration.StatusNotCompleted || result.Completed {
		t.Fatalf("unexpected not completed result: %+v", result)
	}
	confirmed, err := service.Integration.ConfirmCompletion(ctx, integration.ConfirmCompletionParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("confirm after not completed: %v", err)
	}
	if confirmed.Completed || confirmed.Status != integration.StatusNotReady {
		t.Fatalf("unexpected not completed confirmation: %+v", confirmed)
	}
}

func TestTasksIntegrationChannelSubscriptionLivePlatforms(t *testing.T) {
	vkToken := os.Getenv("TASKS_LIVE_VK_TOKEN")
	vkGroupID := os.Getenv("TASKS_LIVE_VK_GROUP_ID")
	vkUserID := os.Getenv("TASKS_LIVE_VK_USER_ID")
	tgToken := os.Getenv("TASKS_LIVE_TG_TOKEN")
	tgChatID := os.Getenv("TASKS_LIVE_TG_CHAT_ID")
	tgUserID := os.Getenv("TASKS_LIVE_TG_USER_ID")
	if vkToken == "" || vkGroupID == "" || vkUserID == "" || tgToken == "" || tgChatID == "" || tgUserID == "" {
		t.Skip("set TASKS_LIVE_VK_TOKEN/GROUP_ID/USER_ID and TASKS_LIVE_TG_TOKEN/CHAT_ID/USER_ID")
	}

	service := newTasksTestService(t)
	ctx := context.Background()
	now := time.Now()
	workspaceID := testsupport.WorkspaceID(fmt.Sprintf("workspace-live-channel-%d", now.UnixNano()))

	vkIdentity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, Platform: "vk", PlatformUserID: vkUserID}
	tgIdentity := integration.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 2, Platform: "tg", PlatformUserID: tgUserID}

	vkTaskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "live_vk_subscribe",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "vk",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "vk",
		PublicPayload: map[string]any{
			"platform": "vk",
			"group_id": vkGroupID,
		},
		PrivatePayload: map[string]any{
			"platform":       "vk",
			"group_id":       vkGroupID,
			"tokens":         []string{vkToken},
			"token_strategy": "round_robin",
		},
	})
	tgTaskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "live_tg_subscribe",
		TaskKind:    repository.TaskKindChannelSubscribe,
		ActionKey:   "tg",
		ActionKind:  repository.ActionKindChannelSubscribe,
		Provider:    "tg",
		PublicPayload: map[string]any{
			"platform": "tg",
			"chat_id":  tgChatID,
		},
		PrivatePayload: map[string]any{
			"platform":       "tg",
			"chat_id":        tgChatID,
			"tokens":         []string{tgToken},
			"token_strategy": "round_robin",
		},
	})

	if _, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(vkIdentity), Locale: "ru", Now: now}); err != nil {
		t.Fatalf("list active vk: %v", err)
	}
	if _, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(tgIdentity), Locale: "ru", Now: now}); err != nil {
		t.Fatalf("list active tg: %v", err)
	}

	checkAndClaimLiveChannelTask(t, service, vkIdentity, vkTaskID, "vk-live-claim")
	checkAndClaimLiveChannelTask(t, service, tgIdentity, tgTaskID, "tg-live-claim")
	tgBoostTaskID := createIntegrationTask(t, service, integrationTaskSeed{
		WorkspaceID: workspaceID,
		Key:         "live_tg_boost",
		TaskKind:    repository.TaskKindChannelBoost,
		ActionKey:   "tg",
		ActionKind:  repository.ActionKindChannelBoost,
		Provider:    "tg",
		PublicPayload: map[string]any{
			"platform": "tg",
			"chat_id":  tgChatID,
		},
		PrivatePayload: map[string]any{
			"platform":       "tg",
			"chat_id":        tgChatID,
			"tokens":         []string{tgToken},
			"token_strategy": "round_robin",
		},
	})
	checkAndClaimLiveChannelBoostTask(t, service, tgIdentity, tgBoostTaskID, "tg-live-boost-claim")
}

func checkAndClaimLiveChannelTask(t *testing.T, service *Tasks, identity integration.Identity, taskID uint64, operationID string) {
	t.Helper()
	ctx := context.Background()
	result, err := service.Integration.CheckChannelSubscription(ctx, integration.CheckChannelSubscriptionParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("check %s: %v", operationID, err)
	}
	if result.Status != repository.StatusReady || !result.Completed {
		t.Fatalf("unexpected check %s: %+v", operationID, result)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: operationID, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("claim %s: %v", operationID, err)
	}
	if claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("unexpected claim %s: %+v", operationID, claim)
	}
}

func checkAndClaimLiveChannelBoostTask(t *testing.T, service *Tasks, identity integration.Identity, taskID uint64, operationID string) {
	t.Helper()
	ctx := context.Background()
	result, err := service.Integration.CheckChannelBoost(ctx, integration.CheckChannelBoostParams{
		TaskRefParams: integration.TaskRefParams{Identity: identity, TaskRef: fmt.Sprintf("%d", taskID), Now: time.Now()},
	})
	if err != nil {
		t.Fatalf("check %s: %v", operationID, err)
	}
	if result.Status != repository.StatusReady || !result.Completed {
		t.Fatalf("unexpected check %s: %+v", operationID, result)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: operationID, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("claim %s: %v", operationID, err)
	}
	if claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("unexpected claim %s: %+v", operationID, claim)
	}
}

type integrationTaskSeed struct {
	WorkspaceID    string
	Key            string
	TaskKind       string
	ActionKey      string
	ActionKind     string
	Provider       string
	PublicPayload  any
	PrivatePayload any
}

func createIntegrationTask(t testing.TB, service *Tasks, seed integrationTaskSeed) uint64 {
	t.Helper()
	ctx := context.Background()
	if err := service.Admin.UpsertGroup(ctx, seed.WorkspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	publicPayload := mustJSON(t, seed.PublicPayload)
	privatePayload := mustJSON(t, seed.PrivatePayload)
	id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: seed.WorkspaceID, Key: seed.Key, GroupKey: "main",
		TaskKind: seed.TaskKind, ActionKey: seed.ActionKey, ActionKind: seed.ActionKind,
		ClaimMode: repository.ClaimModeManual, TargetCount: 1,
		ResetUnit: repository.ResetNever, ResetEvery: 1, Position: 1,
		Payload: publicPayload, IntegrationProvider: strPtr(seed.Provider), IntegrationPayload: privatePayload,
		IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("integration task: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, seed.WorkspaceID, id, admin.RewardModel{Key: "coin", Quantity: 1}, 1); err != nil {
		t.Fatalf("reward: %v", err)
	}
	return id
}

func mustJSON(t testing.TB, value any) json.RawMessage {
	t.Helper()
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}

type fakeChannelChecker struct {
	completed bool
	calls     int
	lastTask  integration.TaskContext
}

func (f *fakeChannelChecker) CheckChannelSubscription(ctx context.Context, params integration.ChannelSubscriptionCheckParams) (integration.CheckResult, error) {
	f.calls++
	f.lastTask = params.Task
	return integration.CheckResult{Completed: f.completed, Payload: json.RawMessage(`{"source":"fake_channel"}`)}, nil
}

type fakeChannelBoostChecker struct {
	completed bool
	calls     int
	lastTask  integration.TaskContext
}

func (f *fakeChannelBoostChecker) CheckChannelBoost(ctx context.Context, params integration.ChannelBoostCheckParams) (integration.CheckResult, error) {
	f.calls++
	f.lastTask = params.Task
	return integration.CheckResult{Completed: f.completed, Payload: json.RawMessage(`{"source":"fake_channel_boost"}`)}, nil
}

type fakeExternalChecker struct {
	completed bool
}

func (f *fakeExternalChecker) CheckExternalTask(ctx context.Context, params integration.ExternalTaskCheckParams) (integration.CheckResult, error) {
	return integration.CheckResult{Completed: f.completed, Payload: json.RawMessage(`{"source":"fake_external"}`)}, nil
}

func TestTasksPartnerIssueAndIssuedStatsAreAtomic(t *testing.T) {
	_ = newTasksTestService(t)
	ctx := context.Background()
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open tasks database: %v", err)
	}
	client, err := sqlwrap.New(db, sqlwrap.Options{
		CacheEnabled:  true,
		CacheSize:     100,
		CacheTTLCheck: time.Minute,
	})
	if err != nil {
		_ = db.Close()
		t.Fatalf("create sql client: %v", err)
	}
	repo := repository.New(client)
	t.Cleanup(func() {
		_ = repo.Close()
		_ = client.Close()
	})

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE task_partner_stats_daily
		ADD CONSTRAINT task_partner_stats_daily_test_failure
		CHECK (issued_count = 0)
	`); err != nil {
		t.Fatalf("install stats failure constraint: %v", err)
	}

	workspaceID := testsupport.WorkspaceID("partner-issued-stats-atomic")
	params := repository.CreatePartnerIssueParams{
		Identity: repository.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     1,
			Platform:       "telegram",
			PlatformUserID: "player",
		},
		Provider:       "test",
		GroupKey:       "daily",
		Platform:       "telegram",
		ExternalID:     "offer-1",
		ExternalType:   "offer",
		IssueKey:       "atomic-issue",
		PublicPayload:  json.RawMessage(`{"title":"Offer"}`),
		PrivatePayload: json.RawMessage(`{}`),
		Now:            time.Now().UTC(),
	}
	if _, _, err := repo.CreatePartnerIssue(ctx, params); err == nil {
		t.Fatal("create partner issue succeeded while stats write was forced to fail")
	}

	for table, expected := range map[string]int{
		"task_partner_issue":       0,
		"task_partner_stats_event": 0,
	} {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE workspace_id = $1", table)
		if err := db.QueryRowContext(ctx, query, workspaceID).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != expected {
			t.Fatalf("%s rows = %d, want %d after rollback", table, count, expected)
		}
	}

	if _, err := db.ExecContext(ctx, `
		ALTER TABLE task_partner_stats_daily
		DROP CONSTRAINT task_partner_stats_daily_test_failure
	`); err != nil {
		t.Fatalf("remove stats failure constraint: %v", err)
	}

	issue, inserted, err := repo.CreatePartnerIssue(ctx, params)
	if err != nil {
		t.Fatalf("retry partner issue: %v", err)
	}
	if issue.ID == 0 || !inserted {
		t.Fatalf("retry partner issue result: issue=%+v inserted=%t", issue, inserted)
	}

	var issuedCount int64
	if err := db.QueryRowContext(ctx, `
		SELECT issued_count
		FROM task_partner_stats_daily
		WHERE workspace_id = $1 AND provider = $2 AND group_key = $3
	`, workspaceID, params.Provider, params.GroupKey).Scan(&issuedCount); err != nil {
		t.Fatalf("read issued stats: %v", err)
	}
	if issuedCount != 1 {
		t.Fatalf("issued count = %d, want 1", issuedCount)
	}
}

func TestTasksPartnerCallbackRevokesBeforeClaim(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-partner-revoke"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	createPartnerConfigAndReward(t, service, identity.WorkspaceID)

	items, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity, Provider: "fake", GroupKey: "daily", Platform: "telegram", Now: time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list partner: items=%+v err=%v", items, err)
	}

	revoked, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: identity.WorkspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "unsubscribed",
		Payload:     json.RawMessage(`{"source":"partner"}`),
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("revoke callback: %v", err)
	}
	if revoked.Status != repository.PartnerIssueStatusRevoked {
		t.Fatalf("revoke status = %q", revoked.Status)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: items[0].Key, OperationID: "claim-revoked",
	})
	if err != nil {
		t.Fatalf("claim revoked: %v", err)
	}
	if claim.Status != repository.ClaimStatusNotReady || claim.Task == nil ||
		claim.Task.Progress == nil || claim.Task.Progress.Status != repository.PartnerIssueStatusRevoked {
		t.Fatalf("unexpected revoked claim: %+v", claim)
	}

	stats := partnerDailyStats(t, service, identity.WorkspaceID)
	if stats.RevokedCount != 1 || stats.RevokedAfterClaimCount != 0 {
		t.Fatalf("unexpected revoke stats: %+v", stats)
	}

	again, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: identity.WorkspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "unsubscribed",
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("duplicate revoke callback: %v", err)
	}
	if again.Status != repository.PartnerIssueStatusRevoked {
		t.Fatalf("duplicate revoke status = %q", again.Status)
	}
	stats = partnerDailyStats(t, service, identity.WorkspaceID)
	if stats.RevokedCount != 1 || stats.RevokedAfterClaimCount != 0 {
		t.Fatalf("duplicate revoke changed stats: %+v", stats)
	}
}

func TestTasksPartnerCallbackRevokesAfterClaimAndEmitsCallback(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	now := time.Now().UTC()
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-partner-revoke-after-claim"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	createPartnerConfigAndReward(t, service, identity.WorkspaceID)

	items, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity, Provider: "fake", GroupKey: "daily", Platform: "telegram", Now: now,
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list partner: items=%+v err=%v", items, err)
	}
	check, err := service.User.CheckPartner(ctx, user.PartnerCheckParams{
		Identity: identity, IssueRef: items[0].Key, Now: now.Add(time.Minute),
	})
	if err != nil || !check.Completed {
		t.Fatalf("check partner: %+v err=%v", check, err)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: items[0].Key, OperationID: "claim-before-revoke",
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim partner: %+v err=%v", claim, err)
	}

	revoked, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: identity.WorkspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "unsubscribed",
		Payload:     json.RawMessage(`{"source":"partner"}`),
		Now:         now,
	})
	if err != nil {
		t.Fatalf("revoke callback: %v", err)
	}
	if revoked.Status != repository.PartnerIssueStatusRevokedAfterClaim {
		t.Fatalf("revoke status = %q", revoked.Status)
	}

	stats := partnerDailyStats(t, service, identity.WorkspaceID)
	if stats.ClaimedCount != 1 || stats.RevokedAfterClaimCount != 1 {
		t.Fatalf("unexpected revoke-after-claim stats: %+v", stats)
	}

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	seenRevoked := false
	err = service.OnCallback(workerCtx, func(callbackCtx Context) error {
		if callbackCtx.Claimed != nil {
			return callbackCtx.Successful()
		}
		if callbackCtx.Revoked == nil {
			return errors.New("expected revoked callback")
		}
		if callbackCtx.Revoked.TaskKey != items[0].Key ||
			callbackCtx.Revoked.OperationID != "claim-before-revoke" ||
			len(callbackCtx.Revoked.Rewards) != 1 || callbackCtx.Revoked.Rewards[0].Key != "stars" {
			return errors.New("bad revoked callback payload")
		}
		seenRevoked = true
		if err := callbackCtx.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	},
		WithCallbackWorkerID("tasks-complex-test-worker"),
		WithCallbackBatchSize(10),
		WithCallbackLeaseTimeout(time.Second),
		WithCallbackIdleDelay(10*time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callback: %v", err)
	}
	if !seenRevoked {
		t.Fatal("revoked callback was not delivered")
	}
}

func TestTasksPartnerCallbackRejectsIssueFromAnotherScope(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-partner-callback-scope")
	identity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "1093776793",
	}

	if err := service.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID: workspaceID,
		Provider:    "fake",
		GroupKey:    "weekly",
		Platform:    "telegram",
		IsEnabled:   true,
		Target:      json.RawMessage(`null`),
		Settings:    json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("save weekly partner config: %v", err)
	}

	items, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "fake",
		GroupKey: "weekly",
		Platform: "telegram",
		Now:      time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list weekly partner: items=%+v err=%v", items, err)
	}

	result, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: workspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "step_completed",
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("cross-scope callback: %v", err)
	}
	if result.Status != repository.ClaimStatusNotFound {
		t.Fatalf("cross-scope callback status = %q", result.Status)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     items[0].Key,
		OperationID: "cross-scope-claim",
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("claim untouched issue: %v", err)
	}
	if claim.Status != repository.ClaimStatusNotReady {
		t.Fatalf("cross-scope callback changed issue: %+v", claim)
	}
}

func TestTasksPartnerCallbackRequiresApplicationScopeWhenAmbiguous(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-partner-callback-application")
	createPartnerConfigAndReward(t, service, workspaceID)

	firstIdentity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "same-user",
	}
	secondIdentity := firstIdentity
	secondIdentity.AppID = 2

	firstItems, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: firstIdentity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      time.Now(),
	})
	if err != nil || len(firstItems) != 1 {
		t.Fatalf("list first application: items=%+v err=%v", firstItems, err)
	}
	secondItems, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: secondIdentity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      time.Now(),
	})
	if err != nil || len(secondItems) != 1 {
		t.Fatalf("list second application: items=%+v err=%v", secondItems, err)
	}

	ambiguous, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID:    workspaceID,
		Provider:       "fake",
		GroupKey:       "daily",
		Platform:       "telegram",
		ExternalID:     "offer-1",
		PlatformUserID: "same-user",
		Status:         "step_completed",
		Now:            time.Now(),
	})
	if err != nil {
		t.Fatalf("ambiguous callback: %v", err)
	}
	if ambiguous.Status != internalapi.PartnerCallbackStatusAmbiguous || ambiguous.Issue != nil {
		t.Fatalf("ambiguous callback result = %+v", ambiguous)
	}

	for _, item := range []struct {
		identity user.Identity
		task     user.TaskModel
	}{
		{identity: firstIdentity, task: firstItems[0]},
		{identity: secondIdentity, task: secondItems[0]},
	} {
		claim, err := service.User.Claim(ctx, user.ClaimParams{
			Identity:    item.identity,
			TaskRef:     item.task.Key,
			OperationID: "claim-before-scoped-callback",
			Now:         time.Now(),
		})
		if err != nil {
			t.Fatalf("claim untouched issue: %v", err)
		}
		if claim.Status != repository.ClaimStatusNotReady {
			t.Fatalf("ambiguous callback changed issue: %+v", claim)
		}
	}

	completed, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID:    workspaceID,
		Provider:       "fake",
		GroupKey:       "daily",
		Platform:       "telegram",
		ExternalID:     "offer-1",
		PlatformUserID: "same-user",
		AppID:          firstIdentity.AppID,
		PlatformID:     firstIdentity.PlatformID,
		Status:         "step_completed",
		Now:            time.Now(),
	})
	if err != nil || completed.Status != repository.PartnerIssueStatusCompleted {
		t.Fatalf("scoped callback: result=%+v err=%v", completed, err)
	}
	if completed.Issue == nil || completed.Issue.AppID != firstIdentity.AppID {
		t.Fatalf("scoped callback selected wrong issue: %+v", completed)
	}
}

func TestTasksPartnerIssueExpirationRespectsLimitedAndUnlimitedTasks(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	provider := deadlinePartnerProvider{base: base}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{"deadline": provider},
	})

	newIssue := func(t *testing.T, groupKey, platformUserID string) (user.Identity, user.TaskModel) {
		t.Helper()

		workspaceID := testsupport.WorkspaceID("partner-deadline-" + groupKey)
		createPartnerConfigAndRewardForProvider(t, service, workspaceID, "deadline", groupKey)
		identity := user.Identity{
			WorkspaceID:    workspaceID,
			AppID:          1,
			PlatformID:     2,
			Platform:       "telegram",
			PlatformUserID: platformUserID,
		}
		items, err := service.User.ListPartner(ctx, user.PartnerListParams{
			Identity: identity,
			Provider: "deadline",
			GroupKey: groupKey,
			Platform: "telegram",
			Now:      base,
		})
		if err != nil || len(items) != 1 {
			t.Fatalf("list %s issue: items=%+v err=%v", groupKey, items, err)
		}
		return identity, items[0]
	}

	t.Run("limited_late_callback_expires", func(t *testing.T) {
		identity, task := newIssue(t, "limited-expired", "limited-expired-user")
		result, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
			WorkspaceID: identity.WorkspaceID,
			Provider:    "deadline",
			GroupKey:    "limited-expired",
			Platform:    "telegram",
			IssueRef:    task.Key,
			Status:      "step_completed",
			Now:         base.Add(2 * time.Minute),
		})
		if err != nil || result.Status != repository.PartnerIssueStatusExpired {
			t.Fatalf("late callback result=%+v err=%v", result, err)
		}

		claim, err := service.User.Claim(ctx, user.ClaimParams{
			Identity:    identity,
			TaskRef:     task.Key,
			OperationID: "limited-expired-claim",
			Now:         base.Add(3 * time.Minute),
		})
		if err != nil || claim.Status != repository.ClaimStatusExpired {
			t.Fatalf("expired claim=%+v err=%v", claim, err)
		}
	})

	t.Run("limited_completion_can_be_claimed_later", func(t *testing.T) {
		identity, task := newIssue(t, "limited-completed", "limited-completed-user")
		result, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
			WorkspaceID: identity.WorkspaceID,
			Provider:    "deadline",
			GroupKey:    "limited-completed",
			Platform:    "telegram",
			IssueRef:    task.Key,
			Status:      "step_completed",
			Now:         base.Add(30 * time.Second),
		})
		if err != nil || result.Status != repository.PartnerIssueStatusCompleted {
			t.Fatalf("on-time callback result=%+v err=%v", result, err)
		}

		claim, err := service.User.Claim(ctx, user.ClaimParams{
			Identity:    identity,
			TaskRef:     task.Key,
			OperationID: "limited-completed-claim",
			Now:         base.Add(2 * time.Minute),
		})
		if err != nil || claim.Status != repository.ClaimStatusClaimed {
			t.Fatalf("late claim after on-time completion=%+v err=%v", claim, err)
		}
	})

	t.Run("unlimited_has_no_deadline", func(t *testing.T) {
		identity, task := newIssue(t, "unlimited", "unlimited-user")
		check, err := service.User.CheckPartner(ctx, user.PartnerCheckParams{
			Identity: identity,
			IssueRef: task.Key,
			Now:      base.Add(365 * 24 * time.Hour),
		})
		if err != nil || !check.Completed || check.Status != user.PartnerStatusReady {
			t.Fatalf("unlimited check=%+v err=%v", check, err)
		}

		claim, err := service.User.Claim(ctx, user.ClaimParams{
			Identity:    identity,
			TaskRef:     task.Key,
			OperationID: "unlimited-claim",
			Now:         base.Add(2 * 365 * 24 * time.Hour),
		})
		if err != nil || claim.Status != repository.ClaimStatusClaimed {
			t.Fatalf("unlimited claim=%+v err=%v", claim, err)
		}
	})
}

func TestTasksPartnerIssueKeepsRewardSnapshot(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("partner-reward-snapshot")
	identity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "reward-snapshot-user",
	}
	createPartnerConfigAndReward(t, service, workspaceID)

	issued, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      time.Now().UTC(),
	})
	if err != nil || len(issued) != 1 || len(issued[0].Rewards) != 1 {
		t.Fatalf("issue partner task: tasks=%+v err=%v", issued, err)
	}
	if issued[0].Rewards[0].Quantity != 25 {
		t.Fatalf("issued reward quantity = %d, want 25", issued[0].Rewards[0].Quantity)
	}

	if err := service.Admin.SavePartnerRewardRule(ctx, admin.SavePartnerRewardRuleParams{
		WorkspaceID:  workspaceID,
		Provider:     "fake",
		GroupKey:     "daily",
		ExternalType: "subscribe",
		Reward: admin.RewardModel{
			Key:      "stars",
			Type:     "quantity",
			Quantity: 100,
			Scale:    2,
		},
		IsEnabled: true,
	}); err != nil {
		t.Fatalf("update partner reward: %v", err)
	}

	listed, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      time.Now().UTC(),
	})
	if err != nil || len(listed) != 1 || len(listed[0].Rewards) != 1 {
		t.Fatalf("list issued partner task: tasks=%+v err=%v", listed, err)
	}
	if listed[0].Rewards[0].Quantity != 25 {
		t.Fatalf("listed reward quantity = %d, want issued snapshot 25", listed[0].Rewards[0].Quantity)
	}

	checked, err := service.User.CheckPartner(ctx, user.PartnerCheckParams{
		Identity: identity,
		IssueRef: issued[0].Key,
		Now:      time.Now().UTC(),
	})
	if err != nil || !checked.Completed || checked.Task == nil || len(checked.Task.Rewards) != 1 {
		t.Fatalf("check partner task: result=%+v err=%v", checked, err)
	}
	if checked.Task.Rewards[0].Quantity != 25 {
		t.Fatalf("checked reward quantity = %d, want issued snapshot 25", checked.Task.Rewards[0].Quantity)
	}

	claimed, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     issued[0].Key,
		OperationID: "partner-reward-snapshot-claim",
		Now:         time.Now().UTC(),
	})
	if err != nil || claimed.Status != repository.ClaimStatusClaimed ||
		claimed.Task == nil || len(claimed.Task.Rewards) != 1 {
		t.Fatalf("claim partner task: result=%+v err=%v", claimed, err)
	}
	if claimed.Task.Rewards[0].Quantity != 25 {
		t.Fatalf("claimed reward quantity = %d, want issued snapshot 25", claimed.Task.Rewards[0].Quantity)
	}
}

func TestTasksLimitedPartnerIssueCanBeReissuedInNextWindow(t *testing.T) {
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	provider := &windowPartnerProvider{
		expiresAt: base.Add(time.Minute),
		windowKey: "window-1",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{"window": provider},
	})
	workspaceID := testsupport.WorkspaceID("partner-next-window")
	createPartnerConfigAndRewardForProvider(t, service, workspaceID, "window", "daily")
	identity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "next-window-user",
	}

	first, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "window",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      base,
	})
	if err != nil || len(first) != 1 {
		t.Fatalf("list first window: tasks=%+v err=%v", first, err)
	}

	expired, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: workspaceID,
		Provider:    "window",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    first[0].Key,
		Status:      "step_completed",
		Now:         base.Add(2 * time.Minute),
	})
	if err != nil || expired.Status != repository.PartnerIssueStatusExpired {
		t.Fatalf("expire first window: result=%+v err=%v", expired, err)
	}

	provider.expiresAt = base.Add(3 * time.Minute)
	provider.windowKey = "window-2"
	second, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "window",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      base.Add(2 * time.Minute),
	})
	if err != nil || len(second) != 1 {
		t.Fatalf("list second window: tasks=%+v err=%v", second, err)
	}
	if second[0].Key == first[0].Key {
		t.Fatalf("next window reused expired issue %q", first[0].Key)
	}
	if second[0].Progress == nil || second[0].Progress.Status != repository.StatusOpen {
		t.Fatalf("second window task is not open: %+v", second[0])
	}
}

func TestTasksPartnerInternalRejectsInvalidWorkspace(t *testing.T) {
	service := newPartnerCallbackTestService(t)

	if _, err := service.Internal.OnPartnerCallback(context.Background(), internalapi.PartnerCallbackParams{
		WorkspaceID: "main",
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueID:     1,
		Status:      "step_completed",
	}); !errors.Is(err, services.ErrIdentityWorkspaceInvalid) {
		t.Fatalf("callback invalid workspace error = %v", err)
	}

	if _, err := service.Internal.HandlePartnerWebhook(context.Background(), internalapi.PartnerWebhookParams{
		WorkspaceID: "main",
		Secret:      "secret",
	}); !errors.Is(err, services.ErrIdentityWorkspaceInvalid) {
		t.Fatalf("webhook invalid workspace error = %v", err)
	}
}

type fakePartnerProvider struct{}

type controlledStartPartnerProvider struct {
	calls           atomic.Int32
	entered         chan struct{}
	release         <-chan struct{}
	externalClickID string
	actionURL       string
	onStart         func(user.PartnerStartProviderParams) error
}

type deadlinePartnerProvider struct {
	base time.Time
}

type windowPartnerProvider struct {
	expiresAt time.Time
	windowKey string
}

func (p deadlinePartnerProvider) ListPartnerTasks(
	_ context.Context,
	params user.PartnerListProviderParams,
) ([]user.PartnerExternalTask, error) {
	var expiresAt *time.Time
	if params.Config.GroupKey != "unlimited" {
		deadline := p.base.Add(time.Minute)
		expiresAt = &deadline
	}

	return []user.PartnerExternalTask{{
		ExternalID:     "offer-" + params.Config.GroupKey,
		ExternalType:   "subscribe",
		PublicPayload:  json.RawMessage(`{"provider":"deadline"}`),
		PrivatePayload: json.RawMessage(`{"provider":"deadline"}`),
		ExpiresAt:      expiresAt,
	}}, nil
}

func (deadlinePartnerProvider) CheckPartnerTask(
	context.Context,
	user.PartnerCheckProviderParams,
) (user.PartnerCheckResult, error) {
	return user.PartnerCheckResult{
		Completed: true,
		Status:    "completed",
	}, nil
}

func (p *windowPartnerProvider) ListPartnerTasks(
	context.Context,
	user.PartnerListProviderParams,
) ([]user.PartnerExternalTask, error) {
	return []user.PartnerExternalTask{
		{
			ExternalID:     "window-offer",
			ExternalType:   "subscribe",
			PublicPayload:  json.RawMessage(`{"provider":"window"}`),
			PrivatePayload: json.RawMessage(`{"provider":"window"}`),
			ExpiresAt:      &p.expiresAt,
			WindowKey:      p.windowKey,
		},
	}, nil
}

func (*windowPartnerProvider) CheckPartnerTask(
	context.Context,
	user.PartnerCheckProviderParams,
) (user.PartnerCheckResult, error) {
	return user.PartnerCheckResult{Completed: true, Status: "completed"}, nil
}

func (fakePartnerProvider) ListPartnerTasks(context.Context, user.PartnerListProviderParams) ([]user.PartnerExternalTask, error) {
	return []user.PartnerExternalTask{{
		ExternalID:     "offer-1",
		ExternalType:   "subscribe",
		PublicPayload:  json.RawMessage(`{"provider":"fake","link":"https://example.com"}`),
		PrivatePayload: json.RawMessage(`{"offer_id":"offer-1"}`),
	}}, nil
}

func (fakePartnerProvider) CheckPartnerTask(context.Context, user.PartnerCheckProviderParams) (user.PartnerCheckResult, error) {
	return user.PartnerCheckResult{
		Completed: true,
		Status:    "subscribed",
		Payload:   json.RawMessage(`{"provider":"fake","status":"subscribed"}`),
	}, nil
}

func (p *controlledStartPartnerProvider) ListPartnerTasks(
	context.Context,
	user.PartnerListProviderParams,
) ([]user.PartnerExternalTask, error) {

	return []user.PartnerExternalTask{
		{
			ExternalID:     "controlled-offer",
			ExternalType:   "subscribe",
			PublicPayload:  json.RawMessage(`{"provider":"controlled"}`),
			PrivatePayload: json.RawMessage(`{"offer_id":"controlled-offer"}`),
			StartMode:      repository.StartModeRequired,
		},
	}, nil

}

func (*controlledStartPartnerProvider) CheckPartnerTask(
	context.Context,
	user.PartnerCheckProviderParams,
) (user.PartnerCheckResult, error) {

	return user.PartnerCheckResult{
		Completed: true,
		Status:    repository.PartnerIssueStatusCompleted,
	}, nil

}

func (p *controlledStartPartnerProvider) StartPartnerTask(
	ctx context.Context,
	params user.PartnerStartProviderParams,
) (user.PartnerStartResult, error) {

	p.calls.Add(1)
	if p.entered != nil {
		select {
		case p.entered <- struct{}{}:
		default:
		}
	}
	if p.onStart != nil {
		if err := p.onStart(params); err != nil {
			return user.PartnerStartResult{}, err
		}
	}
	if p.release != nil {
		select {
		case <-ctx.Done():
			return user.PartnerStartResult{}, ctx.Err()
		case <-p.release:
		}
	}

	return user.PartnerStartResult{
		Started:         true,
		Status:          user.PartnerStatusStarted,
		ActionURL:       p.actionURL,
		ExternalClickID: p.externalClickID,
	}, nil

}

func TestTasksPartnerStartUsesDistributedLeaseAndPersistsActionURL(t *testing.T) {

	release := make(chan struct{})
	provider := &controlledStartPartnerProvider{
		entered:         make(chan struct{}, 2),
		release:         release,
		externalClickID: "distributed-click",
		actionURL:       "https://partner.example/action",
	}
	options := Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	}
	nodeA := newTasksTestService(t, options)
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open second tasks node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	nodeB, err := NewWithDatabase(context.Background(), db, tasksTestOptions(options))
	if err != nil {
		t.Fatalf("create second tasks node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	ctx := context.Background()
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-start-lease"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "lease-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		nodeA,
		identity.WorkspaceID,
		"controlled",
		"daily",
	)
	items, err := nodeA.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
		Now:      time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list controlled partner: items=%+v err=%v", items, err)
	}

	results := make([]user.PartnerStartOutput, 2)
	errorsByNode := make([]error, 2)
	startGate := make(chan struct{})
	var ready sync.WaitGroup
	var started sync.WaitGroup
	ready.Add(2)
	started.Add(2)
	for index, node := range []*Tasks{nodeA, nodeB} {
		index := index
		node := node
		go func() {
			defer started.Done()
			ready.Done()
			<-startGate
			results[index], errorsByNode[index] = node.User.StartPartner(
				ctx,
				user.PartnerStartParams{
					Identity: identity,
					IssueRef: items[0].Key,
					Now:      time.Now(),
				},
			)
		}()
	}
	ready.Wait()
	close(startGate)

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("partner provider start was not called")
	}
	time.Sleep(100 * time.Millisecond)
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls while lease is held = %d, want 1", calls)
	}
	close(release)
	started.Wait()

	for index := range results {
		if errorsByNode[index] != nil {
			t.Fatalf("node %d start error: %v", index, errorsByNode[index])
		}
		if !results[index].Started || results[index].ActionURL != provider.actionURL {
			t.Fatalf("node %d start result: %+v", index, results[index])
		}
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after concurrent start = %d, want 1", calls)
	}

	replayed, err := nodeB.User.StartPartner(ctx, user.PartnerStartParams{
		Identity: identity,
		IssueRef: items[0].Key,
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("replay partner start: %v", err)
	}
	if !replayed.Started || replayed.ActionURL != provider.actionURL {
		t.Fatalf("replayed start did not use persisted action URL: %+v", replayed)
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after replay = %d, want 1", calls)
	}

}

func TestTasksPartnerStartReleasesLeaseAfterRequestCancellation(t *testing.T) {

	release := make(chan struct{})
	provider := &controlledStartPartnerProvider{
		entered:         make(chan struct{}, 2),
		release:         release,
		externalClickID: "cancel-retry-click",
		actionURL:       "https://partner.example/cancel-retry",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	})
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-start-cancel-release"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "cancel-release-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		service,
		identity.WorkspaceID,
		"controlled",
		"daily",
	)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list controlled partner: items=%+v err=%v", items, err)
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, startErr := service.User.StartPartner(firstCtx, user.PartnerStartParams{
			Identity: identity,
			IssueRef: items[0].Key,
		})
		firstDone <- startErr
	}()

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("first partner start did not reach provider")
	}
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled partner start error = %v, want context.Canceled", err)
	}
	close(release)

	retryCtx, cancelRetry := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelRetry()
	retried, err := service.User.StartPartner(retryCtx, user.PartnerStartParams{
		Identity: identity,
		IssueRef: items[0].Key,
	})
	if err != nil {
		t.Fatalf("retry partner start after cancellation: %v", err)
	}
	if !retried.Started || retried.ActionURL != provider.actionURL {
		t.Fatalf("retry partner start result: %+v", retried)
	}
	if calls := provider.calls.Load(); calls != 2 {
		t.Fatalf("provider calls after canceled retry = %d, want 2", calls)
	}

}

func TestTasksStandaloneUserCloseCancelsPartnerStart(t *testing.T) {

	release := make(chan struct{})
	provider := &controlledStartPartnerProvider{
		entered:         make(chan struct{}, 1),
		release:         release,
		externalClickID: "standalone-close-click",
		actionURL:       "https://partner.example/standalone-close",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	})
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-standalone-close"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "standalone-close-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		service,
		identity.WorkspaceID,
		"controlled",
		"daily",
	)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list standalone close issue: items=%+v err=%v", items, err)
	}

	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open standalone user database: %v", err)
	}
	client, err := sqlwrap.New(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("create standalone user client: %v", err)
	}
	standalone := user.NewWithServiceOptions(context.Background(), client, user.Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	})
	t.Cleanup(func() {
		close(release)
		_ = standalone.Close()
		_ = client.Close()
		_ = db.Close()
	})

	startDone := make(chan error, 1)
	go func() {
		_, err := standalone.StartPartner(context.Background(), user.PartnerStartParams{
			Identity: identity,
			IssueRef: items[0].Key,
		})
		startDone <- err
	}()

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("standalone partner provider start was not called")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- standalone.Close()
	}()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close standalone user: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("standalone user close did not cancel partner start")
	}

	select {
	case err := <-startDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("partner start error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("partner start did not stop after standalone user close")
	}

}

func TestTasksPartnerStartRenewsLeaseDuringLongProviderCall(t *testing.T) {

	release := make(chan struct{})
	provider := &controlledStartPartnerProvider{
		entered:         make(chan struct{}, 2),
		release:         release,
		externalClickID: "long-start-click",
		actionURL:       "https://partner.example/long-start",
	}
	options := Options{
		PartnerStartLeaseDuration: 500 * time.Millisecond,
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	}
	nodeA := newTasksTestService(t, options)
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open second tasks node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	nodeB, err := NewWithDatabase(context.Background(), db, tasksTestOptions(options))
	if err != nil {
		t.Fatalf("create second tasks node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-start-lease-renewal"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "lease-renewal-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		nodeA,
		identity.WorkspaceID,
		"controlled",
		"daily",
	)
	items, err := nodeA.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list controlled partner: items=%+v err=%v", items, err)
	}

	results := make(chan user.PartnerStartOutput, 2)
	errorsByNode := make(chan error, 2)
	start := func(node *Tasks) {
		result, startErr := node.User.StartPartner(context.Background(), user.PartnerStartParams{
			Identity: identity,
			IssueRef: items[0].Key,
		})
		results <- result
		errorsByNode <- startErr
	}
	go start(nodeA)

	select {
	case <-provider.entered:
	case <-time.After(time.Second):
		t.Fatal("long partner start did not reach provider")
	}
	time.Sleep(3 * options.PartnerStartLeaseDuration)
	go start(nodeB)
	time.Sleep(options.PartnerStartLeaseDuration)
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after original lease duration = %d, want 1", calls)
	}

	close(release)
	for range 2 {
		if err := <-errorsByNode; err != nil {
			t.Fatalf("partner start with renewed lease: %v", err)
		}
		if result := <-results; !result.Started || result.ActionURL != provider.actionURL {
			t.Fatalf("partner start with renewed lease result: %+v", result)
		}
	}
	if calls := provider.calls.Load(); calls != 1 {
		t.Fatalf("provider calls after renewed lease completion = %d, want 1", calls)
	}

}

func TestTasksRequiredPartnerCallbackNeedsStartAndRevokedIssueCannotStart(t *testing.T) {

	provider := &controlledStartPartnerProvider{
		externalClickID: "state-click",
		actionURL:       "https://partner.example/state",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	})
	ctx := context.Background()

	prestartIdentity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-prestart-callback"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "prestart-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		service,
		prestartIdentity.WorkspaceID,
		"controlled",
		"daily",
	)
	prestartItems, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: prestartIdentity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(prestartItems) != 1 {
		t.Fatalf("list prestart issue: items=%+v err=%v", prestartItems, err)
	}

	prestartCallback, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: prestartIdentity.WorkspaceID,
		Provider:    "controlled",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    prestartItems[0].Key,
		Status:      "step_completed",
		Now:         time.Now(),
	})
	if err != nil {
		t.Fatalf("prestart callback: %v", err)
	}
	if prestartCallback.Status != repository.PartnerIssueStatusIssued {
		t.Fatalf("prestart callback status = %q, want issued", prestartCallback.Status)
	}

	revokedIdentity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-revoked-start"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "revoked-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		service,
		revokedIdentity.WorkspaceID,
		"controlled",
		"daily",
	)
	revokedItems, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: revokedIdentity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(revokedItems) != 1 {
		t.Fatalf("list revoked issue: items=%+v err=%v", revokedItems, err)
	}
	if _, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: revokedIdentity.WorkspaceID,
		Provider:    "controlled",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    revokedItems[0].Key,
		Status:      "unsubscribed",
		Now:         time.Now(),
	}); err != nil {
		t.Fatalf("revoke issue: %v", err)
	}

	startResult, err := service.User.StartPartner(ctx, user.PartnerStartParams{
		Identity: revokedIdentity,
		IssueRef: revokedItems[0].Key,
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("start revoked issue: %v", err)
	}
	if startResult.Status != repository.PartnerIssueStatusRevoked || startResult.Started {
		t.Fatalf("revoked start result: %+v", startResult)
	}
	if calls := provider.calls.Load(); calls != 0 {
		t.Fatalf("provider calls for rejected states = %d, want 0", calls)
	}

}

func TestTasksPartnerCallbackDuringStartCompletesAndPersistsStart(t *testing.T) {

	provider := &controlledStartPartnerProvider{
		externalClickID: "early-callback-click",
		actionURL:       "https://partner.example/early-callback",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"controlled": provider,
		},
	})
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-early-callback"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "early-callback-user",
	}
	createPartnerConfigAndRewardForProvider(
		t,
		service,
		identity.WorkspaceID,
		"controlled",
		"daily",
	)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity,
		Provider: "controlled",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list early callback issue: items=%+v err=%v", items, err)
	}

	provider.onStart = func(params user.PartnerStartProviderParams) error {
		callback, err := service.Internal.OnPartnerCallback(
			context.Background(),
			internalapi.PartnerCallbackParams{
				WorkspaceID: params.Issue.WorkspaceID,
				Provider:    params.Issue.Provider,
				GroupKey:    params.Issue.GroupKey,
				Platform:    params.Issue.Platform,
				IssueID:     params.Issue.ID,
				Status:      "step_completed",
				Now:         time.Now(),
			},
		)
		if err != nil {
			return err
		}
		if callback.Status != repository.PartnerIssueStatusCompleted {
			return fmt.Errorf("early callback status = %q", callback.Status)
		}

		return nil
	}

	started, err := service.User.StartPartner(context.Background(), user.PartnerStartParams{
		Identity: identity,
		IssueRef: items[0].Key,
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("start with early callback: %v", err)
	}
	if started.Status != user.PartnerStatusReady || !started.Started ||
		started.ActionURL != provider.actionURL {
		t.Fatalf("start with early callback result: %+v", started)
	}

}

func TestTasksPartnerClickIDIsScopedByProvider(t *testing.T) {

	firstProvider := &controlledStartPartnerProvider{
		externalClickID: "shared-provider-click",
		actionURL:       "https://first.example/action",
	}
	secondProvider := &controlledStartPartnerProvider{
		externalClickID: "shared-provider-click",
		actionURL:       "https://second.example/action",
	}
	service := newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{
			"first":  firstProvider,
			"second": secondProvider,
		},
	})
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-click-provider-scope"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "shared-click-user",
	}

	for _, provider := range []string{"first", "second"} {
		createPartnerConfigAndRewardForProvider(
			t,
			service,
			identity.WorkspaceID,
			provider,
			"daily",
		)
		items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
			Identity: identity,
			Provider: provider,
			GroupKey: "daily",
			Platform: "telegram",
		})
		if err != nil || len(items) != 1 {
			t.Fatalf("list %s issue: items=%+v err=%v", provider, items, err)
		}
		if _, err := service.User.StartPartner(context.Background(), user.PartnerStartParams{
			Identity: identity,
			IssueRef: items[0].Key,
			Now:      time.Now(),
		}); err != nil {
			t.Fatalf("start %s issue with shared click: %v", provider, err)
		}
	}

	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open tasks database: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM task_partner_issue
WHERE workspace_id = $1
  AND external_click_id = $2
`, identity.WorkspaceID, "shared-provider-click").Scan(&count); err != nil {
		t.Fatalf("count provider-scoped clicks: %v", err)
	}
	if count != 2 {
		t.Fatalf("provider-scoped click rows = %d, want 2", count)
	}

}

func newPartnerCallbackTestService(t testing.TB) *Tasks {
	t.Helper()
	return newTasksTestService(t, Options{
		PartnerProviders: map[string]user.PartnerProvider{"fake": fakePartnerProvider{}},
	})
}

func createPartnerConfigAndReward(t testing.TB, service *Tasks, workspaceID string) {
	t.Helper()
	createPartnerConfigAndRewardForProvider(t, service, workspaceID, "fake", "daily")
}

func createPartnerConfigAndRewardForProvider(
	t testing.TB,
	service *Tasks,
	workspaceID string,
	provider string,
	groupKey string,
) {
	t.Helper()
	if err := service.Admin.SavePartnerConfig(context.Background(), admin.PartnerConfigModel{
		WorkspaceID: workspaceID,
		Provider:    provider,
		GroupKey:    groupKey,
		Platform:    "telegram",
		IsEnabled:   true,
		Target:      json.RawMessage(`null`),
		Settings:    json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("save partner config: %v", err)
	}
	if err := service.Admin.SavePartnerRewardRule(context.Background(), admin.SavePartnerRewardRuleParams{
		WorkspaceID:  workspaceID,
		Provider:     provider,
		GroupKey:     groupKey,
		ExternalType: "subscribe",
		Reward:       admin.RewardModel{Key: "stars", Type: "quantity", Quantity: 25, Scale: 2},
		IsEnabled:    true,
	}); err != nil {
		t.Fatalf("save partner reward: %v", err)
	}
}

func partnerDailyStats(t testing.TB, service *Tasks, workspaceID string) admin.PartnerDailyStatsModel {
	t.Helper()
	now := time.Now()
	stats, err := service.Admin.ListPartnerDailyStats(
		context.Background(), workspaceID, "fake", "daily", now.Add(-24*time.Hour), now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("list partner stats: %v", err)
	}
	if len(stats) != 1 {
		result := admin.PartnerDailyStatsModel{Provider: "fake", GroupKey: "daily", ExternalType: "subscribe"}
		for _, item := range stats {
			result.IssuedCount += item.IssuedCount
			result.CompletedCount += item.CompletedCount
			result.ClaimedCount += item.ClaimedCount
			result.RevokedCount += item.RevokedCount
			result.RevokedAfterClaimCount += item.RevokedAfterClaimCount
			result.FailedCount += item.FailedCount
			result.FakeCount += item.FakeCount
			result.ExpiredCount += item.ExpiredCount
			result.UniqueIssuedUsers += item.UniqueIssuedUsers
			result.UniqueCompletedUsers += item.UniqueCompletedUsers
			result.UniqueClaimers += item.UniqueClaimers
		}
		return result
	}
	return stats[0]
}

func TestTgrassProviderListAndCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/offers", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Auth"); got != "token" {
			t.Fatalf("Auth header = %q", got)
		}
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"offers":[{"name":"Tech","link":"https://t.me/tech","subscribed":false,"type":"channel","channel_id":"-100","offer_id":1054}]
		}`))
	})
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"subscribed","is_fake":false}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	secret := "token"
	provider := user.TgrassProvider{BaseURL: server.URL}
	params := user.PartnerListProviderParams{
		Identity: user.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "123", IsPremium: true},
		Config:   repository.PartnerConfig{Provider: "tgrass", GroupKey: "tgrass", Platform: "telegram", Secret: &secret},
		Locale:   "ru",
		Limit:    1,
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ExternalID != "1054" || tasks[0].ExternalType != "channel" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue: repository.PartnerIssue{
			ExternalID: "1054", PrivatePayload: tasks[0].PrivatePayload,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Completed || check.Status != "subscribed" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestSubGramProviderListAndCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get-sponsors", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"warning",
			"additional":{"sponsors":[{"ads_id":"42","link":"https://t.me/s","resource_id":"-100","type":"channel","status":"unsubscribed","available_now":true,"button_text":"Join"}]}
		}`))
	})
	mux.HandleFunc("/get-user-subscriptions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"additional":{"sponsors":[{"link":"https://t.me/s","status":"subscribed"}]}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	secret := "token"
	provider := user.SubGramProvider{BaseURL: server.URL}
	params := user.PartnerListProviderParams{
		Identity: user.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "123"},
		Config:   repository.PartnerConfig{Provider: "subgram", GroupKey: "subgram", Secret: &secret, Settings: json.RawMessage(`{"action":"task"}`)},
		Locale:   "ru",
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ExternalID != "42:-100" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue:    repository.PartnerIssue{PrivatePayload: tasks[0].PrivatePayload},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Completed || check.Status != "subscribed" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestSubGramLuaProviderListAndCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get-sponsors", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Auth"); got != "token" {
			t.Fatalf("Auth header = %q", got)
		}
		_, _ = w.Write([]byte(`{
			"status":"warning",
			"additional":{"sponsors":[{"ads_id":"42","link":"https://t.me/s","resource_id":"-100","type":"channel","status":"unsubscribed","available_now":true,"button_text":"Join"}]}
		}`))
	})
	mux.HandleFunc("/get-user-subscriptions", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"additional":{"sponsors":[{"link":"https://t.me/s","status":"subscribed"}]}
		}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	secret := "token"
	provider, closeRuntime := newLuaProviderForScript(t, "subgram", taskruntime.SubGramScript)
	defer closeRuntime()
	params := user.PartnerListProviderParams{
		Identity: user.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "123"},
		Config: repository.PartnerConfig{
			Provider: "subgram", GroupKey: "subgram", Secret: &secret,
			Settings: json.RawMessage(`{"action":"task","base_url":"` + server.URL + `"}`),
		},
		Locale: "ru",
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ExternalID != "42:-100" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue:    repository.PartnerIssue{PrivatePayload: tasks[0].PrivatePayload},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Completed || check.Status != "subscribed" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestFlyerProviderListAndCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get_tasks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tasks":[{"signature":"sig","task_type":"subscribe channel","link":"https://t.me/c","title":"Channel"}]}`))
	})
	mux.HandleFunc("/check_task", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	secret := "key"
	provider := user.FlyerProvider{BaseURL: server.URL}
	params := user.PartnerListProviderParams{
		Identity: user.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "123"},
		Config:   repository.PartnerConfig{Provider: "flyer", GroupKey: "flyer", Platform: "telegram", Secret: &secret},
		Locale:   "ru",
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ExternalID != "sig" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue:    repository.PartnerIssue{PrivatePayload: tasks[0].PrivatePayload},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Completed || check.Status != "completed" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func TestFlyerLuaProviderListAndCheck(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/get_tasks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tasks":[{"signature":"sig","task_type":"subscribe channel","link":"https://t.me/c","title":"Channel"}]}`))
	})
	mux.HandleFunc("/check_task", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	secret := "key"
	provider, closeRuntime := newLuaProviderForScript(t, "flyer", taskruntime.FlyerScript)
	defer closeRuntime()
	params := user.PartnerListProviderParams{
		Identity: user.Identity{WorkspaceID: testsupport.WorkspaceID("w"), PlatformUserID: "123"},
		Config: repository.PartnerConfig{
			Provider: "flyer", GroupKey: "flyer", Platform: "telegram", Secret: &secret,
			Settings: json.RawMessage(`{"base_url":"` + server.URL + `"}`),
		},
		Locale: "ru",
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ExternalID != "sig" {
		t.Fatalf("unexpected tasks: %+v", tasks)
	}
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue:    repository.PartnerIssue{PrivatePayload: tasks[0].PrivatePayload},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !check.Completed || check.Status != "completed" {
		t.Fatalf("unexpected check: %+v", check)
	}
}

func newLuaProviderForScript(t testing.TB, provider string, source string) (user.LuaProvider, func()) {
	t.Helper()
	manager := taskruntime.New(context.Background(), taskruntime.Options{
		ScriptLoader: func(context.Context, string) (taskruntime.Script, bool, error) {
			return taskruntime.Script{Provider: provider, Source: source, Version: "test"}, true, nil
		},
	})
	return user.LuaProvider{Runtime: manager, Provider: provider}, func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("close lua runtime: %v", err)
		}
	}
}

func TestTasksIsReady(t *testing.T) {
	var nilService *Tasks
	if nilService.IsReady() {
		t.Fatal("nil tasks must not be ready")
	}
	service := New(DatabaseParams{})
	if service.IsReady() {
		t.Fatal("uninitialized tasks must not be ready")
	}
	initialized := newTasksTestService(t)
	if !initialized.IsReady() {
		t.Fatal("initialized tasks must be ready")
	}
	if err := initialized.Close(); err != nil {
		t.Fatalf("close initialized tasks: %v", err)
	}
	if initialized.IsReady() {
		t.Fatal("closed tasks must not be ready")
	}
}

func TestTasksRunBlocksUntilContextCanceled(t *testing.T) {
	newTasksTestService(t)
	service := New(DatabaseParams{
		User:     pgUser,
		Password: pgPassword,
		Database: tasksTestDB,
		Host:     pgHost,
		Port:     pgPort,
		Options:  tasksTestOptions(Options{}),
	})
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(runCtx)
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
			t.Fatal("tasks service did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := service.Run(context.Background()); !errors.Is(err, ErrServiceRunning) {
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
		t.Fatal("tasks Run did not stop after cancellation")
	}
}

func TestTasksAdminCatalogAndPartnerScriptSurface(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("admin-catalog-surface")
	ids := createComplexTaskSet(t, service, workspaceID, complexTaskOptions{
		ParentKey: "admin.complex",
		Conditions: []complexConditionSeed{
			{
				Key:            "admin.condition",
				ActionKey:      "admin.condition.event",
				TargetCount:    1,
				RewardKey:      "condition_coin",
				RewardQuantity: 1,
			},
		},
		ParentRewardKey:      "parent_coin",
		ParentRewardQuantity: 10,
		ResetUnit:            repository.ResetNever,
	})

	conditions, err := service.Admin.ListComplexConditions(ctx, workspaceID)
	if err != nil || len(conditions) != 1 ||
		conditions[0].ParentTaskID != ids.parentID ||
		conditions[0].ConditionTaskID != ids.conditionIDs[0] {
		t.Fatalf("list complex conditions: values=%+v err=%v", conditions, err)
	}
	if changed, err := service.Admin.DeleteReward(
		ctx,
		workspaceID,
		ids.conditionIDs[0],
		"condition_coin",
	); err != nil || changed != 1 {
		t.Fatalf("delete task reward: changed=%d err=%v", changed, err)
	}

	secret := "partner-secret"
	webhookSecret := "partner-webhook-secret"
	if err := service.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID:   workspaceID,
		Provider:      "admin-provider",
		GroupKey:      "main",
		Platform:      "telegram",
		IsEnabled:     true,
		Secret:        &secret,
		WebhookSecret: &webhookSecret,
		Settings:      json.RawMessage(`{"base_url":"https://partner.example"}`),
	}); err != nil {
		t.Fatalf("save partner config: %v", err)
	}
	configs, err := service.Admin.ListPartnerConfigs(ctx, workspaceID)
	if err != nil || len(configs) != 1 || configs[0].Provider != "admin-provider" ||
		configs[0].Secret == nil || *configs[0].Secret != secret {
		t.Fatalf("list partner configs: values=%+v err=%v", configs, err)
	}
	if err := service.Admin.SavePartnerRewardRule(ctx, admin.SavePartnerRewardRuleParams{
		WorkspaceID:  workspaceID,
		Provider:     "admin-provider",
		GroupKey:     "main",
		ExternalType: "subscribe",
		Reward: admin.RewardModel{
			Key:      "stars",
			Type:     "quantity",
			Quantity: 25,
			Scale:    2,
		},
		Position:  1,
		IsEnabled: true,
	}); err != nil {
		t.Fatalf("save partner reward: %v", err)
	}

	manifest, err := service.Admin.ExportManifest(ctx)
	if err != nil || manifest.Format != repository.ExportFormat || len(manifest.Sections) == 0 {
		t.Fatalf("export manifest: value=%+v err=%v", manifest, err)
	}
	pkg, err := service.Admin.Export(ctx, workspaceID, admin.ExportRequest{
		IncludeSecrets: true,
	})
	if err != nil || len(pkg.Groups) != 1 || len(pkg.Groups[0].Tasks) != 2 ||
		len(pkg.Groups[0].PartnerConfigs) != 1 ||
		len(pkg.Groups[0].PartnerRewardRules) != 1 {
		t.Fatalf("export package: value=%+v err=%v", pkg, err)
	}
	preview, err := service.Admin.PreviewImport(ctx, workspaceID, pkg)
	if err != nil || preview.Counts.Tasks != 2 || len(preview.Conflicts) == 0 {
		t.Fatalf("preview import: value=%+v err=%v", preview, err)
	}

	if changed, err := service.Admin.DeletePartnerRewardRule(
		ctx,
		workspaceID,
		"admin-provider",
		"main",
		"subscribe",
		"stars",
	); err != nil || changed != 1 {
		t.Fatalf("delete partner reward: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.DeleteComplexCondition(
		ctx,
		workspaceID,
		ids.parentID,
		ids.conditionIDs[0],
	); err != nil || changed != 1 {
		t.Fatalf("delete complex condition: changed=%d err=%v", changed, err)
	}
	conditions, err = service.Admin.ListComplexConditions(ctx, workspaceID)
	if err != nil || len(conditions) != 0 {
		t.Fatalf("complex conditions after delete: values=%+v err=%v", conditions, err)
	}

	script := internalapi.PartnerScriptModel{
		Provider:  "admin-provider",
		IsEnabled: true,
		Version:   "v1",
		Source:    "function list(event) return { tasks = {} } end",
	}
	if err := service.Internal.SavePartnerScript(ctx, script); err != nil {
		t.Fatalf("save partner script: %v", err)
	}
	loadedScript, found, err := service.Internal.GetPartnerScript(ctx, script.Provider)
	if err != nil || !found || loadedScript.Version != script.Version || loadedScript.Source != script.Source {
		t.Fatalf("get partner script: value=%+v found=%v err=%v", loadedScript, found, err)
	}
	scripts, err := service.Internal.ListPartnerScripts(ctx)
	if err != nil || len(scripts) != 1 || scripts[0].Provider != script.Provider {
		t.Fatalf("list partner scripts: values=%+v err=%v", scripts, err)
	}
	if _, found, err := service.Internal.GetPartnerScript(ctx, "missing-provider"); err != nil || found {
		t.Fatalf("missing partner script: found=%v err=%v", found, err)
	}

}

func TestTasksHTTPCheckerChannelSubscriptionContract(t *testing.T) {

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("user") != "channel-user" {
			t.Fatalf("channel user query = %q", request.URL.Query().Get("user"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"subscribed": true})
	}))
	defer server.Close()

	payload, err := json.Marshal(integration.HTTPCheckPayload{
		Request: integration.HTTPCheckRequest{
			URL:   server.URL,
			Query: map[string]string{"user": "${user}"},
		},
		Success: integration.HTTPCheckSuccess{
			StatusCodes: []int{http.StatusOK},
			JSONPath:    "subscribed",
			Equals:      true,
		},
	})
	if err != nil {
		t.Fatalf("encode checker payload: %v", err)
	}

	checker := integration.HTTPChecker{Client: server.Client()}
	result, err := checker.CheckChannelSubscription(
		context.Background(),
		integration.ChannelSubscriptionCheckParams{
			Identity: integration.Identity{
				WorkspaceID:    testsupport.WorkspaceID("http-channel-checker"),
				AppID:          1,
				PlatformID:     2,
				PlatformUserID: "channel-user",
			},
			Task: integration.TaskContext{
				ID:                 1,
				Key:                "channel-subscription",
				ActionKind:         repository.ActionKindChannelSubscribe,
				IntegrationPayload: payload,
			},
			Provider: "http",
		},
	)
	if err != nil || !result.Completed || result.Reason != "" {
		t.Fatalf("channel subscription check: result=%+v err=%v", result, err)
	}

}

func TestTasksCacheVersionsInvalidateOtherNode(t *testing.T) {
	cache := testsupport.NewCache()
	options := tasksTestOptions(Options{Cache: cache, CacheL2Delay: time.Minute})
	nodeA := newTasksTestService(t, options)
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open second tasks node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	nodeB, err := NewWithDatabase(context.Background(), db, options)
	if err != nil {
		t.Fatalf("create second tasks node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("cache-workspace")
	if err := nodeA.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create cached task group: %v", err)
	}
	taskID, err := nodeA.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         "cached-task",
		GroupKey:    "main",
		ActionKey:   "message.send",
		ActionKind:  repository.ActionKindAppAction,
		ClaimMode:   repository.ClaimModeManual,
		StartMode:   repository.StartModeNone,
		TargetCount: 1,
		ResetUnit:   repository.ResetNever,
		ResetEvery:  1,
		IsVisible:   true,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create cached task: %v", err)
	}
	if err := nodeA.Admin.UpsertTaskLocalization(ctx, workspaceID, taskID, "ru", "Old title", ""); err != nil {
		t.Fatalf("create cached task localization: %v", err)
	}
	secret := "old-secret"
	if err := nodeA.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID: workspaceID,
		Provider:    "test-provider",
		GroupKey:    "main",
		Platform:    "telegram",
		IsEnabled:   true,
		Secret:      &secret,
	}); err != nil {
		t.Fatalf("create cached partner config: %v", err)
	}
	assertTasksCacheRead(t, nodeB, "Old title", "old-secret")

	if err := nodeA.Admin.UpsertTaskLocalization(ctx, workspaceID, taskID, "ru", "New title", ""); err != nil {
		t.Fatalf("update cached task localization: %v", err)
	}
	secret = "new-secret"
	if err := nodeA.Admin.SavePartnerConfig(ctx, admin.PartnerConfigModel{
		WorkspaceID: workspaceID,
		Provider:    "test-provider",
		GroupKey:    "main",
		Platform:    "telegram",
		IsEnabled:   true,
		Secret:      &secret,
	}); err != nil {
		t.Fatalf("update cached partner config: %v", err)
	}
	assertTasksCacheRead(t, nodeB, "New title", "new-secret")
}

func TestTasksImportBatchesMoreThanPostgresParameterLimit(t *testing.T) {
	service := newTasksTestService(t)
	const taskCount = 4001
	values := make([]repository.ExportTask, 0, taskCount)
	for index := 0; index < taskCount; index++ {
		values = append(values, repository.ExportTask{
			Key:         fmt.Sprintf("large.task.%05d", index),
			TaskKind:    repository.TaskKindInternal,
			ActionKey:   "large.action",
			ActionKind:  repository.ActionKindAppAction,
			ClaimMode:   repository.ClaimModeManual,
			StartMode:   repository.StartModeNone,
			TargetCount: 1,
			Reset:       repository.ExportReset{Unit: repository.ResetNever, Every: 1},
			IsVisible:   true,
			IsActive:    true,
		})
	}
	result, err := service.Admin.Import(context.Background(), testsupport.WorkspaceID("large-workspace"), admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  repository.ExportFormat,
			Service: "tasks",
			Groups: []repository.ExportGroup{
				{Key: "main", IsActive: true, Tasks: values},
			},
		},
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("import large tasks package: %v", err)
	}
	if result.Imported.Tasks != taskCount {
		t.Fatalf("imported tasks = %d, want %d", result.Imported.Tasks, taskCount)
	}
}

func TestTasksImportSerializesWithAdminWrite(t *testing.T) {
	service := newTasksTestService(t)
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open tasks lock database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("concurrent-workspace")
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create tasks group before concurrent writes: %v", err)
	}

	transaction, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tasks lock transaction: %v", err)
	}
	t.Cleanup(func() { _ = transaction.Rollback() })
	if _, err := transaction.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		"tasks:"+workspaceID,
	); err != nil {
		t.Fatalf("lock tasks workspace: %v", err)
	}

	importResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.Import(ctx, workspaceID, admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "tasks",
				Groups: []repository.ExportGroup{
					{
						Key:      "main",
						IsActive: true,
						Tasks: []repository.ExportTask{
							tasksImportTestTask("import-task"),
						},
					},
				},
			},
			ConflictStrategy: repository.ImportConflictUpdate,
		})
		importResult <- err
	}()
	waitForTasksWorkspaceLock(t, db, 1)

	adminResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
			WorkspaceID: workspaceID,
			Key:         "admin-task",
			GroupKey:    "main",
			ActionKey:   "admin.action",
			ActionKind:  repository.ActionKindAppAction,
			ClaimMode:   repository.ClaimModeManual,
			StartMode:   repository.StartModeNone,
			TargetCount: 1,
			ResetUnit:   repository.ResetNever,
			ResetEvery:  1,
			IsVisible:   true,
			IsActive:    true,
		})
		adminResult <- err
	}()
	waitForTasksWorkspaceLock(t, db, 2)

	if err := transaction.Commit(); err != nil {
		t.Fatalf("release tasks workspace lock: %v", err)
	}
	if err := <-importResult; err != nil {
		t.Fatalf("concurrent tasks import: %v", err)
	}
	if err := <-adminResult; err != nil {
		t.Fatalf("concurrent tasks admin write: %v", err)
	}

	values, err := service.Admin.ListTasks(ctx, workspaceID, "main", 10, 0)
	if err != nil || len(values) != 2 {
		t.Fatalf("concurrent tasks result: values=%+v err=%v", values, err)
	}
}

func tasksImportTestTask(key string) repository.ExportTask {
	return repository.ExportTask{
		Key:         key,
		TaskKind:    repository.TaskKindInternal,
		ActionKey:   key + ".action",
		ActionKind:  repository.ActionKindAppAction,
		ClaimMode:   repository.ClaimModeManual,
		StartMode:   repository.StartModeNone,
		TargetCount: 1,
		Reset: repository.ExportReset{
			Unit:  repository.ResetNever,
			Every: 1,
		},
		IsVisible: true,
		IsActive:  true,
	}
}

func waitForTasksWorkspaceLock(t *testing.T, db interface {
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
			t.Fatalf("inspect tasks lock waiters: %v", err)
		}
		if waiting >= minimum {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tasks lock waiters = %d, want at least %d", waiting, minimum)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func assertTasksCacheRead(t *testing.T, service *Tasks, title string, secret string) {
	t.Helper()
	ctx := context.Background()
	values, err := service.User.ListActive(ctx, user.ListActiveParams{
		Identity: services.Identity{
			WorkspaceID:    testsupport.WorkspaceID("cache-workspace"),
			AppID:          1,
			PlatformID:     1,
			PlatformUserID: "cache-user",
		},
		Locale: "ru",
	})
	if err != nil || len(values) != 1 || len(values[0].Tasks) != 1 || values[0].Tasks[0].Title != title {
		t.Fatalf("tasks node returned stale catalog: values=%+v err=%v", values, err)
	}
	config, found, err := service.Admin.GetPartnerConfig(ctx, testsupport.WorkspaceID("cache-workspace"), "test-provider", "main", "telegram")
	if err != nil || !found || config.Secret == nil || *config.Secret != secret {
		t.Fatalf("tasks node returned stale partner config: config=%+v found=%v err=%v", config, found, err)
	}
}

func TestTasksRuntimeGetBonusFullFlow(t *testing.T) {
	var generatedClickID string
	var startRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/partner/offers":
			if r.Header.Get("X-Api-Key") != "secret" {
				http.Error(w, "bad key", http.StatusForbidden)
				return
			}
			_, _ = w.Write([]byte(`{"offers":[{"id":1,"title":"Offer","steps":[{"id":3,"title":"Registration","description":"Create account"}]}]}`))
		case "/v1/partner/click/generate":
			startRequests++
			var body struct {
				StepID  int64  `json:"step_id"`
				ClickID string `json:"click_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.StepID != 3 || body.ClickID == "" {
				http.Error(w, "bad click", http.StatusBadRequest)
				return
			}
			generatedClickID = body.ClickID
			_, _ = w.Write([]byte(`{"statusCode":200,"body":{"external_click_id":"` + body.ClickID + `","action_url":"https://advertiser.example/register","step":{"id":3,"title":"Registration"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTasksTestService(t, Options{
		Runtime: taskruntime.Options{
			Timeout: time.Second,
		},
	})
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-getbonus"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	saveRuntimePartnerConfig(t, service, identity.WorkspaceID, "getbonus", server.URL)

	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity, Provider: "getbonus", GroupKey: "daily", Platform: "telegram", Now: time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list getbonus: items=%+v err=%v", items, err)
	}
	if items[0].Payload == nil || !strings.Contains(string(items[0].Payload), "Registration") {
		t.Fatalf("bad getbonus payload: %s", string(items[0].Payload))
	}
	if items[0].StartMode != repository.StartModeRequired {
		t.Fatalf("getbonus must require start: %+v", items[0])
	}
	notStarted, err := service.User.CheckPartner(context.Background(), user.PartnerCheckParams{
		Identity: identity, IssueRef: items[0].Key, Now: time.Now(),
	})
	if err != nil || notStarted.Status != repository.ClaimStatusNotStarted {
		t.Fatalf("check before start: %+v err=%v", notStarted, err)
	}

	started, err := service.User.StartPartner(context.Background(), user.PartnerStartParams{
		Identity: identity, IssueRef: items[0].Key, Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("start getbonus: %v", err)
	}
	if !started.Started || started.ActionURL != "https://advertiser.example/register" || generatedClickID == "" {
		t.Fatalf("bad start result: %+v click=%q", started, generatedClickID)
	}

	startedAgain, err := service.User.StartPartner(context.Background(), user.PartnerStartParams{
		Identity: identity,
		IssueRef: items[0].Key,
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("repeat getbonus start: %v", err)
	}
	if !startedAgain.Started || startedAgain.ActionURL != started.ActionURL {
		t.Fatalf("repeat start result = %+v, want original %+v", startedAgain, started)
	}
	if startRequests != 1 {
		t.Fatalf("getbonus start requests = %d, want 1", startRequests)
	}

	completed, err := service.Internal.OnPartnerCallback(context.Background(), internalapi.PartnerCallbackParams{
		WorkspaceID:     identity.WorkspaceID,
		Provider:        "getbonus",
		ExternalClickID: generatedClickID,
		Status:          "step_completed",
		Payload:         json.RawMessage(`{"event":"step_completed"}`),
		Now:             time.Now(),
	})
	if err != nil || completed.Status != repository.PartnerIssueStatusCompleted {
		t.Fatalf("callback complete: %+v err=%v", completed, err)
	}

	claim, err := service.User.Claim(context.Background(), user.ClaimParams{
		Identity: identity, TaskRef: items[0].Key, OperationID: "getbonus-claim", Now: time.Now(),
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim getbonus: %+v err=%v", claim, err)
	}
}

func TestTasksRuntimeGetBonusUnifiedWebhook(t *testing.T) {
	var generatedClickID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/partner/offers":
			_, _ = w.Write([]byte(`{"offers":[{"id":1,"title":"Offer","steps":[{"id":3,"title":"Registration","description":"Create account"}]}]}`))
		case "/v1/partner/click/generate":
			var body struct {
				ClickID string `json:"click_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			generatedClickID = body.ClickID
			_, _ = w.Write([]byte(`{"statusCode":200,"body":{"external_click_id":"` + body.ClickID + `","action_url":"https://advertiser.example/register"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := newTasksTestService(t, Options{
		Runtime: taskruntime.Options{
			Timeout: time.Second,
		},
	})
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-getbonus-webhook"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	saveRuntimePartnerConfig(t, service, identity.WorkspaceID, "getbonus", server.URL)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity, Provider: "getbonus", GroupKey: "daily", Platform: "telegram", Now: time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list getbonus: items=%+v err=%v", items, err)
	}
	if _, err = service.User.StartPartner(context.Background(), user.PartnerStartParams{
		Identity: identity, IssueRef: items[0].Key, Now: time.Now(),
	}); err != nil {
		t.Fatalf("start getbonus: %v", err)
	}
	rejected, err := service.Internal.HandlePartnerWebhook(context.Background(), internalapi.PartnerWebhookParams{
		WorkspaceID: identity.WorkspaceID,
		Secret:      "webhook-secret-getbonus",
		Headers:     map[string]string{"X-Api-Key": "wrong-key"},
		Body: json.RawMessage(`{
			"event":"step_completed",
			"external_click_id":"` + generatedClickID + `",
			"offer_id":1,
			"step_id":3
		}`),
		Now: time.Now(),
	})
	if err != nil || rejected.Status != "invalid_api_key" {
		t.Fatalf("expected getbonus webhook api key rejection: %+v err=%v", rejected, err)
	}

	completed, err := service.Internal.HandlePartnerWebhook(context.Background(), internalapi.PartnerWebhookParams{
		WorkspaceID: identity.WorkspaceID,
		Secret:      "webhook-secret-getbonus",
		Headers:     map[string]string{"X-Api-Key": "getbonus-inbound-api-key"},
		Body: json.RawMessage(`{
			"event":"step_completed",
			"external_click_id":"` + generatedClickID + `",
			"offer_id":1,
			"step_id":3
		}`),
		Now: time.Now(),
	})
	if err != nil || completed.Status != repository.PartnerIssueStatusCompleted {
		t.Fatalf("handle getbonus webhook: %+v err=%v", completed, err)
	}
}

func TestTasksRuntimeTgrassUnifiedWebhookRevoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/offers":
			_, _ = w.Write([]byte(`{"status":"ok","offers":[{"link":"https://t.me/example","subscribed":false,"type":"channel","channel_id":"-100","offer_id":42,"name":"Example"}]}`))
		case "/check":
			_, _ = w.Write([]byte(`{"status":"subscribed","is_fake":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := newTasksTestService(t, Options{
		Runtime: taskruntime.Options{
			Timeout: time.Second,
		},
	})
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-tgrass-webhook"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	saveRuntimePartnerConfig(t, service, identity.WorkspaceID, "tgrass", server.URL)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity, Provider: "tgrass", GroupKey: "daily", Platform: "telegram", Now: time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list tgrass: items=%+v err=%v", items, err)
	}
	revoked, err := service.Internal.HandlePartnerWebhook(context.Background(), internalapi.PartnerWebhookParams{
		WorkspaceID: identity.WorkspaceID,
		Secret:      "webhook-secret-tgrass",
		Body:        json.RawMessage(`{"status":"unsubscribed","offer_link":"https://t.me/example","tg_user_id":"1093776793"}`),
		Now:         time.Now(),
	})
	if err != nil || revoked.Status != repository.PartnerIssueStatusRevoked {
		t.Fatalf("handle tgrass webhook: %+v err=%v", revoked, err)
	}
	check, err := service.User.CheckPartner(context.Background(), user.PartnerCheckParams{
		Identity: identity, IssueRef: items[0].Key, Now: time.Now(),
	})
	if err != nil || check.Status != repository.PartnerIssueStatusRevoked || check.Completed {
		t.Fatalf("check must not reopen revoked tgrass issue: %+v err=%v", check, err)
	}
	claim, err := service.User.Claim(context.Background(), user.ClaimParams{
		Identity: identity, TaskRef: items[0].Key, OperationID: "tgrass-revoked-claim", Now: time.Now(),
	})
	if err != nil || claim.Status != repository.ClaimStatusNotReady {
		t.Fatalf("claim revoked tgrass: %+v err=%v", claim, err)
	}
}

func TestTasksRuntimeSubGramBatchWebhookComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get-sponsors":
			_, _ = w.Write([]byte(`{
				"status":"warning",
				"additional":{"sponsors":[{"ads_id":"42","link":"https://t.me/s","resource_id":"-100","type":"channel","status":"unsubscribed","available_now":true,"button_text":"Join"}]}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	service := newTasksTestService(t, Options{
		Runtime: taskruntime.Options{
			Timeout: time.Second,
		},
	})
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-subgram-webhook"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793",
	}
	saveRuntimePartnerConfig(t, service, identity.WorkspaceID, "subgram", server.URL)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity, Provider: "subgram", GroupKey: "daily", Platform: "telegram", Now: time.Now(),
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list subgram: items=%+v err=%v", items, err)
	}
	completed, err := service.Internal.HandlePartnerWebhook(context.Background(), internalapi.PartnerWebhookParams{
		WorkspaceID: identity.WorkspaceID,
		Secret:      "webhook-secret-subgram",
		Body:        json.RawMessage(`{"webhooks":[{"status":"subscribed","ads_id":42,"resource_id":"-100","user_id":"1093776793"}]}`),
		Now:         time.Now(),
	})
	if err != nil || completed.Status != repository.PartnerIssueStatusCompleted {
		t.Fatalf("handle subgram webhook: %+v err=%v", completed, err)
	}
}

func TestTasksRuntimeDoesNotRestoreReplacedStatePool(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(requestStarted)
		<-releaseRequest
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	const oldSource = `
function list(event)
    http.request({ method = "GET", url = event.config.url })
    return { ok = true, version = "old" }
end
`
	const newSource = `
function list(event)
    return { ok = true, version = "new" }
end
`

	var scriptMu sync.RWMutex
	currentScript := taskruntime.Script{
		Provider: "pool-test",
		Source:   oldSource,
		Version:  "old",
	}
	manager := taskruntime.New(context.Background(), taskruntime.Options{
		Timeout:        5 * time.Second,
		StatePoolSize:  2,
		ScriptCacheTTL: time.Hour,
		ScriptLoader: func(context.Context, string) (taskruntime.Script, bool, error) {
			scriptMu.RLock()
			defer scriptMu.RUnlock()

			return currentScript, true, nil
		},
	})
	defer func() {
		if err := manager.Close(); err != nil {
			t.Fatalf("close runtime: %v", err)
		}
	}()

	if err := manager.WarmProvider(context.Background(), "pool-test"); err != nil {
		t.Fatalf("warm old provider: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := manager.Handle(context.Background(), "pool-test", taskruntime.Event{
			Action: "list",
			Config: map[string]any{"url": server.URL},
		})
		callDone <- err
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("old runtime call did not start")
	}

	scriptMu.Lock()
	currentScript = taskruntime.Script{
		Provider: "pool-test",
		Source:   newSource,
		Version:  "new",
	}
	scriptMu.Unlock()

	if err := manager.WarmProvider(context.Background(), "pool-test"); err != nil {
		t.Fatalf("warm new provider: %v", err)
	}
	close(releaseRequest)
	if err := <-callDone; err != nil {
		t.Fatalf("finish old runtime call: %v", err)
	}

	stats := manager.Stats()
	if stats.StatePools != 1 {
		t.Fatalf("state pools after replacement = %d, want 1", stats.StatePools)
	}
}

func BenchmarkTasksRuntimeTgrassProvider(b *testing.B) {
	httpClient := &http.Client{Transport: staticTgrassTransport{}}
	secret := "secret"
	identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("bench"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "1093776793"}
	config := repository.PartnerConfig{
		WorkspaceID: testsupport.WorkspaceID("bench"), Provider: "tgrass", GroupKey: "daily", Platform: "telegram",
		Secret: &secret, Settings: json.RawMessage(`{"base_url":"https://tgrass.local"}`),
	}
	goProvider := user.TgrassProvider{Client: httpClient, BaseURL: "https://tgrass.local"}
	luaProviders := map[string]user.LuaProvider{}
	for name, options := range map[string]taskruntime.Options{
		"lua_no_pool": {
			Timeout:       time.Second,
			StatePoolSize: -1,
		},
		"lua_no_pool_json": {
			Timeout:       time.Second,
			StatePoolSize: -1,
			JSONBoundary:  true,
		},
		"lua_pool": {
			Timeout: time.Second,
		},
		"lua_pool_json": {
			Timeout:      time.Second,
			JSONBoundary: true,
		},
	} {
		options.HTTPClient = httpClient
		options.ScriptLoader = func(context.Context, string) (taskruntime.Script, bool, error) {
			return taskruntime.Script{Provider: "tgrass", Source: taskruntime.TgrassScript, Version: "bench"}, true, nil
		}
		manager := taskruntime.New(context.Background(), options)
		defer func() { _ = manager.Close() }()
		luaProviders[name] = user.LuaProvider{Runtime: manager, Provider: "tgrass"}
	}

	b.Run("go_list_check", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			items, err := goProvider.ListPartnerTasks(context.Background(), user.PartnerListProviderParams{
				Identity: identity, Config: config, Locale: "ru", Now: time.Now(),
			})
			if err != nil || len(items) != 1 {
				b.Fatalf("go list: items=%+v err=%v", items, err)
			}
			_, err = goProvider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
				Identity: identity, Config: config, Issue: repository.PartnerIssue{
					ExternalID: items[0].ExternalID, PrivatePayload: items[0].PrivatePayload,
				},
			})
			if err != nil {
				b.Fatalf("go check: %v", err)
			}
		}
	})
	for name, provider := range luaProviders {
		provider := provider
		b.Run(name+"_list_check", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				items, err := provider.ListPartnerTasks(context.Background(), user.PartnerListProviderParams{
					Identity: identity, Config: config, Locale: "ru", Now: time.Now(),
				})
				if err != nil || len(items) != 1 {
					b.Fatalf("%s list: items=%+v err=%v", name, items, err)
				}
				_, err = provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
					Identity: identity, Config: config, Issue: repository.PartnerIssue{
						ExternalID: items[0].ExternalID, PrivatePayload: items[0].PrivatePayload,
					},
				})
				if err != nil {
					b.Fatalf("%s check: %v", name, err)
				}
			}
		})
	}
	b.Run("go_list_check_parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				items, err := goProvider.ListPartnerTasks(context.Background(), user.PartnerListProviderParams{
					Identity: identity, Config: config, Locale: "ru", Now: time.Now(),
				})
				if err != nil || len(items) != 1 {
					b.Fatalf("go list: items=%+v err=%v", items, err)
				}
				_, err = goProvider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
					Identity: identity, Config: config, Issue: repository.PartnerIssue{
						ExternalID: items[0].ExternalID, PrivatePayload: items[0].PrivatePayload,
					},
				})
				if err != nil {
					b.Fatalf("go check: %v", err)
				}
			}
		})
	})
	for name, provider := range luaProviders {
		provider := provider
		b.Run(name+"_list_check_parallel", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					items, err := provider.ListPartnerTasks(context.Background(), user.PartnerListProviderParams{
						Identity: identity, Config: config, Locale: "ru", Now: time.Now(),
					})
					if err != nil || len(items) != 1 {
						b.Fatalf("%s list: items=%+v err=%v", name, items, err)
					}
					_, err = provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
						Identity: identity, Config: config, Issue: repository.PartnerIssue{
							ExternalID: items[0].ExternalID, PrivatePayload: items[0].PrivatePayload,
						},
					})
					if err != nil {
						b.Fatalf("%s check: %v", name, err)
					}
				}
			})
		})
	}
}

func BenchmarkTasksRuntimePartnerServiceMethods(b *testing.B) {
	service := newTasksTestService(b, Options{
		Runtime: taskruntime.Options{
			HTTPClient: staticTgrassHTTPClient(),
			Timeout:    time.Second,
		},
	})
	ctx := context.Background()
	saveRuntimePartnerConfig(
		b,
		service,
		testsupport.WorkspaceID("bench-partner-service"),
		"tgrass",
		"https://tgrass.local",
	)

	b.ReportAllocs()
	b.Run("User.ListPartner/tgrass", func(b *testing.B) {
		identity := user.Identity{
			WorkspaceID: testsupport.WorkspaceID("bench-partner-service"), AppID: 1, PlatformID: 2, Platform: "telegram", PlatformUserID: "list",
		}
		for range b.N {
			_, err := service.User.ListPartner(ctx, user.PartnerListParams{
				Identity: identity, Provider: "tgrass", GroupKey: "daily", Platform: "telegram", Locale: "ru", Now: time.Now(),
			})
			benchError(b, err)
		}
	})
	b.Run("User.CheckPartner/tgrass_success", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			identity, issueRef := benchmarkTgrassIssue(b, service, ctx)
			b.StartTimer()
			_, err := service.User.CheckPartner(ctx, user.PartnerCheckParams{
				Identity: identity, IssueRef: issueRef, Now: time.Now(),
			})
			b.StopTimer()
			benchError(b, err)
		}
	})
	b.Run("Internal.HandlePartnerWebhook/tgrass_revoke", func(b *testing.B) {
		for range b.N {
			b.StopTimer()
			identity, _ := benchmarkTgrassIssue(b, service, ctx)
			b.StartTimer()
			_, err := service.Internal.HandlePartnerWebhook(ctx, internalapi.PartnerWebhookParams{
				WorkspaceID: testsupport.WorkspaceID("bench-partner-service"),
				Secret:      "webhook-secret-tgrass",
				Body:        json.RawMessage(`{"status":"unsubscribed","offer_link":"https://t.me/example","tg_user_id":"` + identity.PlatformUserID + `"}`),
				Now:         time.Now(),
			})
			b.StopTimer()
			benchError(b, err)
		}
	})
}

func benchmarkTgrassIssue(b *testing.B, service *Tasks, ctx context.Context) (user.Identity, string) {
	b.Helper()
	id := tasksBenchmarkUserID.Add(1)
	identity := user.Identity{
		WorkspaceID: testsupport.WorkspaceID("bench-partner-service"), AppID: 1, PlatformID: 2, Platform: "telegram",
		PlatformUserID: "bench-" + strconv.FormatUint(id, 10),
	}
	items, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity, Provider: "tgrass", GroupKey: "daily", Platform: "telegram", Locale: "ru", Now: time.Now(),
	})
	benchError(b, err)
	if len(items) != 1 {
		b.Fatalf("seed tgrass issue: items=%+v", items)
	}
	return identity, items[0].Key
}

func staticTgrassHTTPClient() *http.Client {
	return &http.Client{Transport: staticTgrassTransport{}}
}

type staticTgrassTransport struct{}

func (staticTgrassTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body string
	switch r.URL.Path {
	case "/offers":
		body = `{"status":"ok","offers":[{"link":"https://t.me/example","subscribed":false,"type":"channel","channel_id":"-100","offer_id":42,"name":"Example"}]}`
	case "/check":
		body = `{"status":"subscribed","is_fake":false}`
	default:
		body = `{}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    r,
	}, nil
}

func saveRuntimePartnerConfig(t testing.TB, service *Tasks, workspaceID, provider, baseURL string) {
	t.Helper()
	source := taskruntime.TgrassScript
	switch provider {
	case "getbonus":
		source = taskruntime.GetBonusScript
		source = strings.ReplaceAll(source, `local GETBONUS_WEBHOOK_API_KEY = "***"`, `local GETBONUS_WEBHOOK_API_KEY = "getbonus-inbound-api-key"`)
	case "subgram":
		source = taskruntime.SubGramScript
	case "flyer":
		source = taskruntime.FlyerScript
	}
	if err := service.Internal.SavePartnerScript(context.Background(), internalapi.PartnerScriptModel{
		Provider:  provider,
		IsEnabled: true,
		Version:   "test",
		Source:    source,
	}); err != nil {
		t.Fatalf("save runtime partner script: %v", err)
	}
	secret := "secret"
	webhookSecret := "webhook-secret-" + provider
	settings := json.RawMessage(`{"base_url":"` + baseURL + `"}`)
	if err := service.Admin.SavePartnerConfig(context.Background(), admin.PartnerConfigModel{
		WorkspaceID: workspaceID, Provider: provider, GroupKey: "daily", Platform: "telegram",
		IsEnabled: true, Secret: &secret, WebhookSecret: &webhookSecret, Target: json.RawMessage(`null`),
		Settings: settings,
	}); err != nil {
		t.Fatalf("save runtime partner config: %v", err)
	}
	if err := service.Admin.SavePartnerRewardRule(context.Background(), admin.SavePartnerRewardRuleParams{
		WorkspaceID: workspaceID, Provider: provider, GroupKey: "daily", ExternalType: "step:3",
		Reward: admin.RewardModel{Key: "stars", Type: "quantity", Quantity: 25, Scale: 2}, IsEnabled: true,
	}); err != nil {
		t.Fatalf("save runtime partner reward: %v", err)
	}
}

func TestTasksStatisticsFullCycle(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("stats-workspace")
	now := time.Now().UTC()

	createEarnChain(t, service, workspaceID, repository.ClaimModeManual)
	autoTaskID := createStatsAutoTask(t, service, workspaceID)
	identity := internalapi.Identity{
		WorkspaceID: workspaceID, AppID: 1, PlatformID: 2, PlatformUserID: "stats-player",
	}

	manual, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "stats", ExternalEventKey: "stats-manual", Now: now,
	})
	if err != nil {
		t.Fatalf("record manual progress: %v", err)
	}
	if manual.Consumed != 1000 || len(manual.Tasks) != 1 {
		t.Fatalf("unexpected manual record result: %+v", manual)
	}
	manualTaskID := manual.Tasks[0].Task.ID
	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity),
		TaskRef:  fmt.Sprint(manualTaskID), OperationID: "stats-manual-claim", Now: now,
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim manual task: %+v err=%v", claim, err)
	}

	auto, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "stats_auto", Amount: 500,
		Source: "stats", ExternalEventKey: "stats-auto", Now: now,
	})
	if err != nil {
		t.Fatalf("record auto progress: %v", err)
	}
	if len(auto.Tasks) != 1 || !auto.Tasks[0].Claimed || auto.Tasks[0].Task.ID != autoTaskID {
		t.Fatalf("unexpected auto record result: %+v", auto)
	}
	duplicate, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "stats_auto", Amount: 500,
		Source: "stats", ExternalEventKey: "stats-auto", Now: now,
	})
	if err != nil || duplicate.Status != repository.RecordStatusDuplicate {
		t.Fatalf("duplicate auto record: %+v err=%v", duplicate, err)
	}

	stats, err := service.Admin.GetStats(ctx, workspaceID)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TasksTotal != 3 || stats.ActiveTasks != 3 || stats.VisibleTasks != 3 ||
		stats.ProgressTotal != 2 || stats.ClaimedProgress != 2 ||
		stats.ProgressCreated != 2 || stats.ProgressAmount != 1500 ||
		stats.ReadyCount != 1 || stats.ClaimedCount != 2 ||
		stats.ManualClaimedCount != 1 || stats.AutoClaimedCount != 1 ||
		stats.UniqueParticipants != 1 || stats.UniqueClaimers != 1 {
		t.Fatalf("unexpected tasks stats: %+v", stats)
	}

	taskStats, err := service.Admin.GetTaskStats(ctx, workspaceID, manualTaskID)
	if err != nil {
		t.Fatalf("get single task stats: %v", err)
	}
	if taskStats.ProgressTotal != 1 || taskStats.ProgressAmount != 1000 ||
		taskStats.ReadyCount != 1 || taskStats.ClaimedCount != 1 ||
		taskStats.ManualClaimedCount != 1 || taskStats.AutoClaimedCount != 0 {
		t.Fatalf("unexpected single task stats: %+v", taskStats)
	}

	from, until := now.Add(-24*time.Hour), now.Add(24*time.Hour)
	if err := service.Admin.RefreshDailyStats(ctx, workspaceID, from, until); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}
	daily, err := service.Admin.ListDailyStats(ctx, workspaceID, manualTaskID, from, until)
	if err != nil {
		t.Fatalf("list daily stats: %v", err)
	}
	if len(daily) != 1 || daily[0].ProgressAmount != 1000 ||
		daily[0].ReadyCount != 1 || daily[0].ClaimedCount != 1 {
		t.Fatalf("unexpected daily task stats: %+v", daily)
	}
	overview, err := service.Admin.ListDailyOverview(ctx, workspaceID, from, until)
	if err != nil {
		t.Fatalf("list daily overview: %v", err)
	}
	if len(overview) != 1 || overview[0].TasksTotal != 3 ||
		overview[0].ProgressAmount != 1500 || overview[0].ClaimedCount != 2 ||
		overview[0].ManualClaimedCount != 1 || overview[0].AutoClaimedCount != 1 ||
		overview[0].UniqueParticipants != 1 || overview[0].UniqueClaimers != 1 {
		t.Fatalf("unexpected daily overview: %+v", overview)
	}
}

func BenchmarkTasksAdminStats(b *testing.B) {
	service := newTasksTestService(b)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("stats-benchmark")
	now := time.Now().UTC()
	createEarnChain(b, service, workspaceID, repository.ClaimModeManual)

	identity := internalapi.Identity{
		WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "bench-player",
	}
	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1000,
		Source: "bench", ExternalEventKey: "stats-benchmark", Now: now,
	})
	benchError(b, err)
	taskID := recorded.Tasks[0].Task.ID
	_, err = service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprint(taskID),
		OperationID: "stats-benchmark-claim", Now: now,
	})
	benchError(b, err)

	from, until := now.Add(-24*time.Hour), now.Add(24*time.Hour)
	benchError(b, service.Admin.RefreshDailyStats(ctx, workspaceID, from, until))
	b.ReportAllocs()
	b.Run("GetStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetStats(ctx, workspaceID)
			benchError(b, err)
		}
	})
	b.Run("GetTaskStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.GetTaskStats(ctx, workspaceID, taskID)
			benchError(b, err)
		}
	})
	b.Run("ListDailyStats", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListDailyStats(ctx, workspaceID, taskID, from, until)
			benchError(b, err)
		}
	})
	b.Run("ListDailyOverview", func(b *testing.B) {
		for range b.N {
			_, err := service.Admin.ListDailyOverview(ctx, workspaceID, from, until)
			benchError(b, err)
		}
	})
	b.Run("RefreshDailyStats", func(b *testing.B) {
		for range b.N {
			benchError(b, service.Admin.RefreshDailyStats(ctx, workspaceID, from, until))
		}
	})
}

func createStatsAutoTask(t testing.TB, service *Tasks, workspaceID string) uint64 {
	t.Helper()
	id, err := service.Admin.SaveTask(context.Background(), admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: "stats_auto", GroupKey: "main",
		ActionKey: "stats_auto", ActionKind: repository.ActionKindAmountAction,
		ClaimMode: repository.ClaimModeAuto, TargetCount: 500,
		ResetUnit: repository.ResetNever, ResetEvery: 1,
		Position: 3, IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("create stats auto task: %v", err)
	}
	if err := service.Admin.UpsertReward(
		context.Background(), workspaceID, id,
		admin.RewardModel{Key: "coin", Quantity: 1}, 1,
	); err != nil {
		t.Fatalf("create stats auto reward: %v", err)
	}
	return id
}

const (
	pgHost         = "localhost"
	pgPort         = 5432
	pgUser         = "postgres"
	pgPassword     = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	tasksTestDB    = "tasks_test"
	tasksTimeoutDB = "tasks_timeout_test"
)

func TestTasksManualSequenceCarryAndCallback(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := internalapi.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-a"), AppID: 1, PlatformID: 2, PlatformUserID: "player",
	}
	createEarnChain(t, service, identity.WorkspaceID, repository.ClaimModeManual)

	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "earn-1", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if recorded.Consumed != 1000 || recorded.Remaining != 500 || len(recorded.Tasks) != 1 {
		t.Fatalf("manual sequence must stop at ready task: %+v", recorded)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	first := findTask(t, list, "earn_1")
	second := findTask(t, list, "earn_2")
	if first.Progress == nil || first.Progress.Status != repository.StatusReady || first.Progress.Progress != 1000 {
		t.Fatalf("unexpected first progress: %+v", first.Progress)
	}
	if second.Progress != nil {
		t.Fatalf("second task must not receive carry before manual claim: %+v", second.Progress)
	}

	claim, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", first.ID), OperationID: "claim-1",
	})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim: %+v err=%v", claim, err)
	}
	again, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 500,
		Source: "game", ExternalEventKey: "earn-2", Now: time.Now(),
	})
	if err != nil {
		t.Fatalf("record second carry: %v", err)
	}
	if again.Consumed != 500 || again.Remaining != 0 || len(again.Tasks) != 1 || again.Tasks[0].Task.Key != "earn_2" {
		t.Fatalf("unexpected second progress: %+v", again)
	}

	workerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err = service.OnCallback(workerCtx, func(callbackCtx Context) error {
		if callbackCtx.Claimed == nil || callbackCtx.Claimed.TaskKey != "earn_1" ||
			len(callbackCtx.Claimed.Rewards) != 1 || callbackCtx.Claimed.Rewards[0].Key != "coin" {
			return errors.New("bad callback payload")
		}
		if err := callbackCtx.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	},
		WithCallbackWorkerID("tasks-test-worker"),
		WithCallbackBatchSize(10),
		WithCallbackLeaseTimeout(time.Second),
		WithCallbackIdleDelay(10*time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callback: %v", err)
	}
}

func TestTasksAutoClaimAndIdempotency(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-b"), AppID: 1, PlatformID: 1, PlatformUserID: "auto"}
	createEarnChain(t, service, identity.WorkspaceID, repository.ClaimModeAuto)
	first, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "auto-1",
	})
	if err != nil {
		t.Fatalf("auto record: %v", err)
	}
	if first.Consumed != 1000 || first.Remaining != 500 || len(first.Tasks) != 1 ||
		!first.Tasks[0].Claimed || first.Tasks[0].Task.Key != "earn_1" {
		t.Fatalf("unexpected auto result: %+v", first)
	}
	duplicate, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "auto-1",
	})
	if err != nil {
		t.Fatalf("duplicate record: %v", err)
	}
	if duplicate.Status != repository.RecordStatusDuplicate || duplicate.Consumed != 0 {
		t.Fatalf("unexpected duplicate: %+v", duplicate)
	}
	callbacks := countTaskCallbacks(t, tasksTestDB)
	if callbacks != 1 {
		t.Fatalf("auto claim callbacks = %d, want 1", callbacks)
	}
}

func TestTasksAutoClaimUsesTaskScopedOperationIDs(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-auto-operation-ids")
	identity := internalapi.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "auto-operation-user",
	}

	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}
	for position, key := range []string{"auto_first", "auto_second"} {
		if _, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
			WorkspaceID: workspaceID,
			Key:         key,
			GroupKey:    "main",
			ActionKey:   "shared.auto.event",
			ActionKind:  repository.ActionKindAppAction,
			ClaimMode:   repository.ClaimModeAuto,
			TargetCount: 1,
			ResetUnit:   repository.ResetNever,
			ResetEvery:  1,
			Position:    int32(position + 1),
			IsVisible:   true,
			IsActive:    true,
		}); err != nil {
			t.Fatalf("create task %s: %v", key, err)
		}
	}

	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity:         identity,
		ActionKey:        "shared.auto.event",
		Amount:           1,
		Source:           "test",
		ExternalEventKey: "one-event-for-two-tasks",
	})
	if err != nil {
		t.Fatalf("record shared event: %v", err)
	}
	if len(recorded.Tasks) != 2 || !recorded.Tasks[0].Claimed || !recorded.Tasks[1].Claimed {
		t.Fatalf("unexpected auto claim result: %+v", recorded)
	}

	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open callback database: %v", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(
		ctx,
		"SELECT payload FROM tasks_clb_event WHERE source_service = 'tasks' ORDER BY id",
	)
	if err != nil {
		t.Fatalf("select callbacks: %v", err)
	}
	defer func() { _ = rows.Close() }()

	operationIDs := make(map[string]struct{})
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan callback: %v", err)
		}

		var payload repository.CallbackPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode callback: %v", err)
		}
		operationIDs[payload.OperationID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate callbacks: %v", err)
	}
	if len(operationIDs) != 2 {
		t.Fatalf("unique operation ids = %d, want 2: %+v", len(operationIDs), operationIDs)
	}

}

func TestTasksStartModeRequiredBlocksRecordUntilStart(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-start-required")
	identity := user.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 2, PlatformUserID: "starter"}
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: "start_required", GroupKey: "main",
		ActionKey: "play", ActionKind: repository.ActionKindAppAction,
		ClaimMode: repository.ClaimModeManual, StartMode: repository.StartModeRequired,
		TargetCount: 1, ResetUnit: repository.ResetNever, ResetEvery: 1,
		Position: 1, IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: "coin", Quantity: 1}, 1); err != nil {
		t.Fatalf("reward: %v", err)
	}
	before, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: internalapi.Identity(identity), ActionKey: "play", Source: "game", ExternalEventKey: "before-start",
	})
	if err != nil {
		t.Fatalf("record before start: %v", err)
	}
	if before.Status != repository.RecordStatusNoTasks || len(before.Tasks) != 0 {
		t.Fatalf("record before start must be ignored: %+v", before)
	}
	started, err := service.User.StartTask(ctx, user.StartTaskParams{Identity: identity, TaskRef: "start_required"})
	if err != nil || !started.Started || started.Status != repository.StartStatusStarted {
		t.Fatalf("start: %+v err=%v", started, err)
	}
	after, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: internalapi.Identity(identity), ActionKey: "play", Source: "game", ExternalEventKey: "after-start",
	})
	if err != nil {
		t.Fatalf("record after start: %v", err)
	}
	if after.Status != repository.RecordStatusRecorded || len(after.Tasks) != 1 || after.Tasks[0].After != 1 {
		t.Fatalf("record after start: %+v", after)
	}
	claim, err := service.User.Claim(ctx, user.ClaimParams{Identity: identity, TaskRef: "start_required", OperationID: "claim-start-required"})
	if err != nil || claim.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim: %+v err=%v", claim, err)
	}
}

func TestTasksRecordBroadcastsToIndependentActiveBranches(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-broadcast"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	createStandaloneTask(t, service, identity.WorkspaceID, "earn_big", "earn_coin", 1000, 1)
	createStandaloneTask(t, service, identity.WorkspaceID, "earn_small", "earn_coin", 200, 2)
	createStandaloneTask(t, service, identity.WorkspaceID, "earn_mid", "earn_coin", 500, 3)

	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "broadcast-1",
	})
	if err != nil {
		t.Fatalf("broadcast record: %v", err)
	}
	if recorded.Status != repository.RecordStatusRecorded || recorded.Consumed != 1700 || recorded.Remaining != 500 || len(recorded.Tasks) != 3 {
		t.Fatalf("unexpected broadcast result: %+v", recorded)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	for _, item := range []struct {
		key      string
		progress uint64
	}{
		{key: "earn_big", progress: 1000},
		{key: "earn_small", progress: 200},
		{key: "earn_mid", progress: 500},
	} {
		task := findTask(t, list, item.key)
		if task.Progress == nil || task.Progress.Progress != item.progress || task.Progress.Status != repository.StatusReady {
			t.Fatalf("unexpected progress for %s: %+v", item.key, task.Progress)
		}
	}
}

func TestTasksRecordDoesNotSkipDifferentActiveActionInSequence(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-mixed"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	createMixedActionChain(t, service, identity.WorkspaceID)

	first, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "mixed-coin-1",
	})
	if err != nil {
		t.Fatalf("first coin record: %v", err)
	}
	if first.Consumed != 1000 || len(first.Tasks) != 1 || first.Tasks[0].Task.Key != "coin_1" || !first.Tasks[0].Claimed {
		t.Fatalf("unexpected first mixed result: %+v", first)
	}

	second, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "earn_coin", Amount: 1500,
		Source: "game", ExternalEventKey: "mixed-coin-2",
	})
	if err != nil {
		t.Fatalf("second coin record: %v", err)
	}
	if second.Status != repository.RecordStatusNoTasks || len(second.Tasks) != 0 {
		t.Fatalf("coin event must be blocked by active crystal task: %+v", second)
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if crystal := findTask(t, list, "crystal_1"); crystal.Progress != nil {
		t.Fatalf("crystal task must not get coin progress: %+v", crystal.Progress)
	}
	if coin := findTask(t, list, "coin_2"); coin.Progress != nil {
		t.Fatalf("later coin task must not be reached before crystal is done: %+v", coin.Progress)
	}
}

func TestTasksInvalidUsageIsolationAndRepeatedClaim(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := user.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-invalid"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	createEarnChain(t, service, identity.WorkspaceID, repository.ClaimModeManual)

	unknown, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: "missing", OperationID: "missing-op",
	})
	if err != nil {
		t.Fatalf("unknown claim: %v", err)
	}
	if unknown.Status != repository.ClaimStatusNotFound {
		t.Fatalf("unknown claim status = %q", unknown.Status)
	}

	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	first := findTask(t, list, "earn_1")
	notReady, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: fmt.Sprintf("%d", first.ID), OperationID: "too-early",
	})
	if err != nil {
		t.Fatalf("not ready claim: %v", err)
	}
	if notReady.Status != repository.ClaimStatusNotReady {
		t.Fatalf("not ready claim status = %q", notReady.Status)
	}

	otherWorkspace, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity{
		WorkspaceID: testsupport.WorkspaceID("workspace-other"), AppID: 1, PlatformID: 1, PlatformUserID: "player",
	}, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("other workspace list: %v", err)
	}
	if len(otherWorkspace) != 0 {
		t.Fatalf("workspace isolation failed: %+v", otherWorkspace)
	}

	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: internalapi.Identity(identity), ActionKey: "earn_coin", Amount: 1000,
		Source: "game", ExternalEventKey: "ready-once",
	}); err != nil {
		t.Fatalf("record ready: %v", err)
	}
	claimed, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: fmt.Sprintf("%d", first.ID), OperationID: "claim-once",
	})
	if err != nil || claimed.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim once: %+v err=%v", claimed, err)
	}
	repeated, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity, TaskRef: fmt.Sprintf("%d", first.ID), OperationID: "claim-once",
	})
	if err != nil || repeated.Status != repository.ClaimStatusAlreadyDone {
		t.Fatalf("repeated claim: %+v err=%v", repeated, err)
	}
	callbacks := countTaskCallbacks(t, tasksTestDB)
	if callbacks != 1 {
		t.Fatalf("manual repeated claim callbacks = %d, want 1", callbacks)
	}
	if _, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     fmt.Sprintf("%d", first.ID),
		OperationID: "claim-changed",
	}); !errors.Is(err, repository.ErrOperationIDConflict) {
		t.Fatalf("changed claim operation error = %v, want ErrOperationIDConflict", err)
	}
}

func TestTasksPartnerClaimOperationIDIsImmutable(t *testing.T) {

	service := newPartnerCallbackTestService(t)
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("partner-claim-operation-immutable"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "partner-operation-user",
	}
	createPartnerConfigAndReward(t, service, identity.WorkspaceID)
	items, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: identity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list partner task: items=%+v err=%v", items, err)
	}
	if _, err := service.Internal.OnPartnerCallback(context.Background(), internalapi.PartnerCallbackParams{
		WorkspaceID: identity.WorkspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "step_completed",
	}); err != nil {
		t.Fatalf("complete partner task: %v", err)
	}

	first, err := service.User.Claim(context.Background(), user.ClaimParams{
		Identity:    identity,
		TaskRef:     items[0].Key,
		OperationID: "partner-operation-original",
	})
	if err != nil || first.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim partner task: result=%+v err=%v", first, err)
	}

	repeated, err := service.User.Claim(context.Background(), user.ClaimParams{
		Identity:    identity,
		TaskRef:     items[0].Key,
		OperationID: "partner-operation-original",
	})
	if err != nil || repeated.Status != repository.ClaimStatusAlreadyDone {
		t.Fatalf("repeat partner claim: result=%+v err=%v", repeated, err)
	}

	if _, err := service.User.Claim(context.Background(), user.ClaimParams{
		Identity:    identity,
		TaskRef:     items[0].Key,
		OperationID: "partner-operation-changed",
	}); !errors.Is(err, repository.ErrOperationIDConflict) {
		t.Fatalf("changed partner operation error = %v, want ErrOperationIDConflict", err)
	}

}

func TestTasksClaimOperationIDCannotRewardTwoTasks(t *testing.T) {

	service := newTasksTestService(t)
	ctx := context.Background()
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("claim-operation-id"),
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "operation-user",
	}
	createStandaloneTask(t, service, identity.WorkspaceID, "operation-first", "operation.first", 1, 1)
	createStandaloneTask(t, service, identity.WorkspaceID, "operation-second", "operation.second", 1, 2)

	for _, actionKey := range []string{"operation.first", "operation.second"} {
		if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
			Identity:         internalapi.Identity(identity),
			ActionKey:        actionKey,
			Amount:           1,
			Source:           "test",
			ExternalEventKey: "ready-" + actionKey,
		}); err != nil {
			t.Fatalf("ready %s: %v", actionKey, err)
		}
	}

	const operationID = "single-reward-operation"
	first, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     "operation-first",
		OperationID: operationID,
	})
	if err != nil || first.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim first task: result=%+v err=%v", first, err)
	}

	if _, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     "operation-second",
		OperationID: operationID,
	}); !errors.Is(err, repository.ErrOperationIDConflict) {
		t.Fatalf("duplicate operation error = %v, want ErrOperationIDConflict", err)
	}

	list, err := service.User.ListActive(ctx, user.ListActiveParams{
		Identity: identity,
		Locale:   "ru",
		Now:      time.Now(),
	})
	if err != nil {
		t.Fatalf("list tasks after operation conflict: %v", err)
	}
	second := findTask(t, list, "operation-second")
	if second.Progress == nil || second.Progress.Status != repository.StatusReady {
		t.Fatalf("second task changed after operation conflict: %+v", second.Progress)
	}

	if _, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: identity,
		TaskRef:  "operation-second",
	}); !errors.Is(err, repository.ErrOperationIDRequired) {
		t.Fatalf("empty operation error = %v, want ErrOperationIDRequired", err)
	}

}

func TestTasksClaimOperationIDIsSharedByTaskAndPartnerRewards(t *testing.T) {

	service := newPartnerCallbackTestService(t)
	ctx := context.Background()
	identity := user.Identity{
		WorkspaceID:    testsupport.WorkspaceID("claim-operation-partner"),
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "operation-partner-user",
	}
	createStandaloneTask(t, service, identity.WorkspaceID, "ordinary-reward", "ordinary.reward", 1, 1)
	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity:         internalapi.Identity(identity),
		ActionKey:        "ordinary.reward",
		Amount:           1,
		Source:           "test",
		ExternalEventKey: "ordinary-reward-ready",
	}); err != nil {
		t.Fatalf("ready ordinary task: %v", err)
	}

	const operationID = "task-and-partner-operation"
	if result, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     "ordinary-reward",
		OperationID: operationID,
	}); err != nil || result.Status != repository.ClaimStatusClaimed {
		t.Fatalf("claim ordinary task: result=%+v err=%v", result, err)
	}

	createPartnerConfigAndReward(t, service, identity.WorkspaceID)
	items, err := service.User.ListPartner(ctx, user.PartnerListParams{
		Identity: identity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(items) != 1 {
		t.Fatalf("list partner reward: items=%+v err=%v", items, err)
	}
	if _, err := service.Internal.OnPartnerCallback(ctx, internalapi.PartnerCallbackParams{
		WorkspaceID: identity.WorkspaceID,
		Provider:    "fake",
		GroupKey:    "daily",
		Platform:    "telegram",
		IssueRef:    items[0].Key,
		Status:      "step_completed",
		Now:         time.Now(),
	}); err != nil {
		t.Fatalf("complete partner issue: %v", err)
	}

	if _, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    identity,
		TaskRef:     items[0].Key,
		OperationID: operationID,
	}); !errors.Is(err, repository.ErrOperationIDConflict) {
		t.Fatalf("cross-source operation error = %v, want ErrOperationIDConflict", err)
	}

}

func TestTasksConcurrentRecordSameUser(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	identity := internalapi.Identity{WorkspaceID: testsupport.WorkspaceID("workspace-race"), AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	createEarnChain(t, service, identity.WorkspaceID, repository.ClaimModeManual)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Internal.Record(ctx, internalapi.RecordParams{
				Identity: identity, ActionKey: "earn_coin", Amount: 600,
				Source: "game", ExternalEventKey: fmt.Sprintf("race-%d", i),
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent record: %v", err)
		}
	}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: user.Identity(identity), Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list after concurrent record: %v", err)
	}
	first := findTask(t, list, "earn_1")
	second := findTask(t, list, "earn_2")
	if first.Progress == nil || first.Progress.Progress != 1000 || first.Progress.Status != repository.StatusReady {
		t.Fatalf("first progress after concurrent record: %+v", first.Progress)
	}
	if second.Progress != nil {
		t.Fatalf("manual ready task must block carry under concurrency: %+v", second.Progress)
	}
	callbacks := countTaskCallbacks(t, tasksTestDB)
	if callbacks != 0 {
		t.Fatalf("callbacks before manual claim = %d, want 0", callbacks)
	}
}

func TestTasksRecordUsesDeltaAndPreservesRewardSnapshot(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-record-delta-snapshot")
	identity := internalapi.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     1,
		PlatformUserID: "player",
	}

	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("create group: %v", err)
	}

	taskID, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID,
		Key:         "delta_snapshot",
		GroupKey:    "main",
		TaskKind:    repository.TaskKindInternal,
		ActionKey:   "delta.snapshot",
		ActionKind:  repository.ActionKindAmountAction,
		ClaimMode:   repository.ClaimModeManual,
		TargetCount: 5,
		ResetUnit:   repository.ResetNever,
		ResetEvery:  1,
		Position:    1,
		IsVisible:   true,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := service.Admin.UpsertReward(
		ctx,
		workspaceID,
		taskID,
		admin.RewardModel{Key: "coin", Quantity: 10},
		1,
	); err != nil {
		t.Fatalf("create reward: %v", err)
	}

	for event := 1; event <= 4; event++ {
		_, err := service.Internal.Record(ctx, internalapi.RecordParams{
			Identity:         identity,
			ActionKey:        "delta.snapshot",
			Amount:           1,
			Source:           "test",
			ExternalEventKey: fmt.Sprintf("delta-%d", event),
		})
		if err != nil {
			t.Fatalf("record event %d: %v", event, err)
		}

		groups, err := service.User.ListActive(ctx, user.ListActiveParams{
			Identity: user.Identity(identity),
			Locale:   "ru",
			Now:      time.Now(),
		})
		if err != nil {
			t.Fatalf("list after event %d: %v", event, err)
		}
		progress := findTask(t, groups, "delta_snapshot").Progress
		if progress == nil || progress.Progress != uint64(event) || progress.Status != repository.StatusOpen {
			t.Fatalf("progress after event %d = %+v", event, progress)
		}
	}

	if err := service.Admin.UpsertReward(
		ctx,
		workspaceID,
		taskID,
		admin.RewardModel{Key: "coin", Quantity: 99},
		1,
	); err != nil {
		t.Fatalf("update catalog reward: %v", err)
	}

	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity:         identity,
		ActionKey:        "delta.snapshot",
		Amount:           1,
		Source:           "test",
		ExternalEventKey: "delta-5",
	}); err != nil {
		t.Fatalf("record final event: %v", err)
	}

	claimed, err := service.User.Claim(ctx, user.ClaimParams{
		Identity:    user.Identity(identity),
		TaskRef:     fmt.Sprint(taskID),
		OperationID: "delta-snapshot-claim",
	})
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if len(claimed.Task.Rewards) != 1 || claimed.Task.Rewards[0].Quantity != 10 {
		t.Fatalf("claim rewards = %+v, want snapshotted quantity 10", claimed.Task.Rewards)
	}
}

func TestTasksListActiveCatalogCacheInvalidation(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-cache")
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := service.Admin.UpsertGroupLocalization(ctx, workspaceID, "main", "ru", "Old group", "Old group description"); err != nil {
		t.Fatalf("group localization: %v", err)
	}
	id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: "cached_task", GroupKey: "main",
		ActionKey: "cache_action", ActionKind: repository.ActionKindAmountAction,
		ClaimMode: repository.ClaimModeManual, TargetCount: 1,
		ResetUnit: repository.ResetNever, ResetEvery: 1,
		Position: 1, IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", "Old title", "Old description"); err != nil {
		t.Fatalf("localization: %v", err)
	}

	identity := user.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "cache-user"}
	list, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("first list: %v", err)
	}
	if got := findTask(t, list, "cached_task").Title; got != "Old title" {
		t.Fatalf("initial title = %q", got)
	}
	if len(list) != 1 || list[0].Key != "main" || list[0].Title != "Old group" || list[0].Description != "Old group description" {
		t.Fatalf("initial group = %+v", list)
	}

	if err := service.Admin.UpsertGroupLocalization(ctx, workspaceID, "main", "ru", "New group", "New group description"); err != nil {
		t.Fatalf("group localization update: %v", err)
	}
	if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", "New title", "New description"); err != nil {
		t.Fatalf("localization update: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: "coin", Quantity: 7}, 1); err != nil {
		t.Fatalf("reward update: %v", err)
	}
	list, err = service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("second list: %v", err)
	}
	task := findTask(t, list, "cached_task")
	if task.Title != "New title" {
		t.Fatalf("updated title = %q", task.Title)
	}
	if len(task.Rewards) != 1 || task.Rewards[0].Key != "coin" || task.Rewards[0].Quantity != 7 {
		t.Fatalf("updated rewards = %+v", task.Rewards)
	}
	if len(list) != 1 || list[0].Title != "New group" || list[0].Description != "New group description" {
		t.Fatalf("updated group = %+v", list)
	}

	if _, err := service.Admin.DeleteTask(ctx, workspaceID, id); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	list, err = service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("third list: %v", err)
	}
	for _, group := range list {
		for _, task := range group.Tasks {
			if task.Key == "cached_task" {
				t.Fatalf("deleted task returned from cached list: %+v", task)
			}
		}
	}
}

func TestTasksListActiveFiltersByGroup(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-group-filter")
	identity := user.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "player"}

	createStandaloneTask(t, service, workspaceID, "main_task", "main_action", 1, 1)
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "bonus", 2, true); err != nil {
		t.Fatalf("bonus group: %v", err)
	}
	if err := service.Admin.UpsertGroupLocalization(ctx, workspaceID, "bonus", "ru", "Бонусные", "Бонусные задания"); err != nil {
		t.Fatalf("bonus group localization: %v", err)
	}
	bonusID, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: "bonus_task", GroupKey: "bonus",
		ActionKey: "bonus_action", ActionKind: repository.ActionKindAmountAction,
		ClaimMode: repository.ClaimModeManual, TargetCount: 1,
		ResetUnit: repository.ResetNever, ResetEvery: 1,
		Position: 1, IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("bonus task: %v", err)
	}
	if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, bonusID, "ru", "Bonus", "Bonus"); err != nil {
		t.Fatalf("bonus localization: %v", err)
	}

	all, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", Now: time.Now()})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all groups = %+v", all)
	}
	bonus, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", GroupKey: "bonus", Now: time.Now()})
	if err != nil {
		t.Fatalf("list bonus: %v", err)
	}
	if len(bonus) != 1 || bonus[0].Key != "bonus" || bonus[0].Title != "Бонусные" || len(bonus[0].Tasks) != 1 || bonus[0].Tasks[0].Key != "bonus_task" {
		t.Fatalf("filtered bonus groups = %+v", bonus)
	}
	missing, err := service.User.ListActive(ctx, user.ListActiveParams{Identity: identity, Locale: "ru", GroupKey: "missing", Now: time.Now()})
	if err != nil {
		t.Fatalf("list missing: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing group returned tasks: %+v", missing)
	}
}

func TestTasksRecordAndClaimCatalogCacheInvalidation(t *testing.T) {
	service := newTasksTestService(t)
	ctx := context.Background()
	workspaceID := testsupport.WorkspaceID("workspace-record-cache")
	identity := internalapi.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "player"}
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}

	missing, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "cached_action", Amount: 1,
		Source: "game", ExternalEventKey: "missing-before-create",
	})
	if err != nil {
		t.Fatalf("missing record: %v", err)
	}
	if missing.Status != repository.RecordStatusNoTasks {
		t.Fatalf("missing status = %s", missing.Status)
	}

	taskID, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: "cached_record_task", GroupKey: "main",
		ActionKey: "cached_action", ActionKind: repository.ActionKindAmountAction,
		ClaimMode: repository.ClaimModeManual, TargetCount: 1,
		ResetUnit: repository.ResetNever, ResetEvery: 1,
		Position: 1, IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("task: %v", err)
	}
	if err := service.Admin.UpsertReward(ctx, workspaceID, taskID, admin.RewardModel{Key: "coin", Quantity: 1}, 1); err != nil {
		t.Fatalf("reward: %v", err)
	}

	recorded, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: identity, ActionKey: "cached_action", Amount: 1,
		Source: "game", ExternalEventKey: "record-after-create",
	})
	if err != nil {
		t.Fatalf("record after create: %v", err)
	}
	if recorded.Status != repository.RecordStatusRecorded {
		t.Fatalf("recorded status = %s", recorded.Status)
	}

	claimed, err := service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(identity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: "claim-old-reward",
	})
	if err != nil {
		t.Fatalf("claim old reward: %v", err)
	}
	if len(claimed.Task.Rewards) != 1 || claimed.Task.Rewards[0].Quantity != 1 {
		t.Fatalf("old claim rewards = %+v", claimed.Task.Rewards)
	}

	secondIdentity := internalapi.Identity{WorkspaceID: workspaceID, AppID: 1, PlatformID: 1, PlatformUserID: "player-2"}
	if err := service.Admin.UpsertReward(ctx, workspaceID, taskID, admin.RewardModel{Key: "coin", Quantity: 7}, 1); err != nil {
		t.Fatalf("reward update: %v", err)
	}
	if _, err := service.Internal.Record(ctx, internalapi.RecordParams{
		Identity: secondIdentity, ActionKey: "cached_action", Amount: 1,
		Source: "game", ExternalEventKey: "record-after-reward-update",
	}); err != nil {
		t.Fatalf("record after reward update: %v", err)
	}
	claimed, err = service.User.Claim(ctx, user.ClaimParams{
		Identity: user.Identity(secondIdentity), TaskRef: fmt.Sprintf("%d", taskID), OperationID: "claim-new-reward",
	})
	if err != nil {
		t.Fatalf("claim new reward: %v", err)
	}
	if len(claimed.Task.Rewards) != 1 || claimed.Task.Rewards[0].Quantity != 7 {
		t.Fatalf("new claim rewards = %+v", claimed.Task.Rewards)
	}
}

func TestTasksQueryTimeout(t *testing.T) {
	ctx := context.Background()
	adminDB, err := openTasksPostgres("postgres")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}
	if err := recreateTasksDatabase(ctx, adminDB, tasksTimeoutDB); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	_ = adminDB.Close()

	db, err := openTasksPostgres(tasksTimeoutDB)
	if err != nil {
		t.Fatalf("open timeout db: %v", err)
	}
	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("create timeout sql client: %v", err)
	}
	service, err := NewWithDatabase(ctx, db, tasksTestOptions(Options{}))
	if err != nil {
		t.Fatalf("create tasks service: %v", err)
	}
	if err := service.Admin.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	timeoutService, err := NewWithDatabase(ctx, db, Options{QueryTimeout: time.Nanosecond})
	if err != nil {
		t.Fatalf("create timeout tasks service: %v", err)
	}
	t.Cleanup(func() {
		_ = timeoutService.Close()
		_ = service.Close()
		_ = client.Close()
	})

	err = timeoutService.Admin.UpsertGroup(ctx, testsupport.WorkspaceID("timeout-workspace"), "main", 1, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected query timeout, got %v", err)
	}
}

func createEarnChain(t testing.TB, service *Tasks, workspaceID, claimMode string) {
	t.Helper()
	ctx := context.Background()
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := service.Admin.UpsertSequence(ctx, workspaceID, "earn_chain", 1, true); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	for i := 1; i <= 2; i++ {
		pos := uint32(i)
		id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
			WorkspaceID: workspaceID, Key: fmt.Sprintf("earn_%d", i),
			GroupKey: "main", SequenceKey: strPtr("earn_chain"), SequencePosition: &pos,
			ActionKey: "earn_coin", ActionKind: repository.ActionKindAmountAction,
			ClaimMode: claimMode, TargetCount: 1000, ResetUnit: repository.ResetNever,
			ResetEvery: 1, Position: int32(i), IsVisible: true, IsActive: true,
		})
		if err != nil {
			t.Fatalf("task %d: %v", i, err)
		}
		if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", fmt.Sprintf("Earn %d", i), "Coins"); err != nil {
			t.Fatalf("localization: %v", err)
		}
		if err := service.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: "coin", Quantity: int64(i)}, 1); err != nil {
			t.Fatalf("reward: %v", err)
		}
	}
}

func createStandaloneTask(t testing.TB, service *Tasks, workspaceID, key, actionKey string, target uint64, position int32) {
	t.Helper()
	ctx := context.Background()
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
		WorkspaceID: workspaceID, Key: key, GroupKey: "main",
		ActionKey: actionKey, ActionKind: repository.ActionKindAmountAction,
		ClaimMode: repository.ClaimModeManual, TargetCount: target,
		ResetUnit: repository.ResetNever, ResetEvery: 1, Position: position,
		IsVisible: true, IsActive: true,
	})
	if err != nil {
		t.Fatalf("standalone task %s: %v", key, err)
	}
	if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", key, key); err != nil {
		t.Fatalf("standalone localization %s: %v", key, err)
	}
}

func createMixedActionChain(t testing.TB, service *Tasks, workspaceID string) {
	t.Helper()
	ctx := context.Background()
	if err := service.Admin.UpsertGroup(ctx, workspaceID, "main", 1, true); err != nil {
		t.Fatalf("group: %v", err)
	}
	if err := service.Admin.UpsertSequence(ctx, workspaceID, "mixed_chain", 1, true); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	definitions := []struct {
		key       string
		actionKey string
		claimMode string
		position  uint32
	}{
		{key: "coin_1", actionKey: "earn_coin", claimMode: repository.ClaimModeAuto, position: 1},
		{key: "crystal_1", actionKey: "earn_crystal", claimMode: repository.ClaimModeManual, position: 2},
		{key: "coin_2", actionKey: "earn_coin", claimMode: repository.ClaimModeManual, position: 3},
	}
	for _, definition := range definitions {
		pos := definition.position
		id, err := service.Admin.SaveTask(ctx, admin.SaveTaskParams{
			WorkspaceID: workspaceID, Key: definition.key, GroupKey: "main",
			SequenceKey: strPtr("mixed_chain"), SequencePosition: &pos,
			ActionKey: definition.actionKey, ActionKind: repository.ActionKindAmountAction,
			ClaimMode: definition.claimMode, TargetCount: 1000,
			ResetUnit: repository.ResetNever, ResetEvery: 1, Position: int32(definition.position),
			IsVisible: true, IsActive: true,
		})
		if err != nil {
			t.Fatalf("mixed task %s: %v", definition.key, err)
		}
		if err := service.Admin.UpsertTaskLocalization(ctx, workspaceID, id, "ru", definition.key, definition.key); err != nil {
			t.Fatalf("mixed localization %s: %v", definition.key, err)
		}
		if err := service.Admin.UpsertReward(ctx, workspaceID, id, admin.RewardModel{Key: definition.key, Quantity: 1}, int32(definition.position)); err != nil {
			t.Fatalf("mixed reward %s: %v", definition.key, err)
		}
	}
}

func findTask(t testing.TB, list []user.TaskGroupModel, key string) user.TaskModel {
	t.Helper()
	for _, group := range list {
		for _, task := range group.Tasks {
			if task.Key == key {
				return task
			}
		}
	}
	t.Fatalf("task %q not found in %+v", key, list)
	return user.TaskModel{}
}

func strPtr(value string) *string { return &value }

func newTasksTestService(t testing.TB, options ...Options) *Tasks {
	t.Helper()
	ctx := context.Background()
	adminDB, err := openTasksPostgres("postgres")
	if err != nil {
		t.Fatalf("open admin postgres: %v", err)
	}
	if err := recreateTasksDatabase(ctx, adminDB, tasksTestDB); err != nil {
		t.Fatalf("recreate database: %v", err)
	}
	_ = adminDB.Close()
	db, err := openTasksPostgres(tasksTestDB)
	if err != nil {
		t.Fatalf("open app postgres: %v", err)
	}
	client, err := sqlwrap.New(db, sqlwrap.Options{
		CacheEnabled:  true,
		CacheSize:     10000,
		CacheTTLCheck: time.Minute,
	})
	if err != nil {
		t.Fatalf("create sql client: %v", err)
	}
	serviceOptions := tasksTestOptions(Options{})
	if len(options) > 0 {
		serviceOptions = tasksTestOptions(options[0])
	}
	service, err := NewWithDatabase(ctx, db, serviceOptions)
	if err != nil {
		t.Fatalf("create tasks service: %v", err)
	}
	if err := service.Admin.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
		_ = client.Close()
	})
	return service
}

func tasksTestOptions(options Options) Options {
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

func TestPartnerIssueIsScopedByApplication(t *testing.T) {
	service := newPartnerCallbackTestService(t)
	workspaceID := testsupport.WorkspaceID("partner-issue-app-scope")
	createPartnerConfigAndReward(t, service, workspaceID)

	firstIdentity := user.Identity{
		WorkspaceID:    workspaceID,
		AppID:          1,
		PlatformID:     2,
		Platform:       "telegram",
		PlatformUserID: "same-user",
	}
	secondIdentity := firstIdentity
	secondIdentity.AppID = 2

	first, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: firstIdentity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(first) != 1 {
		t.Fatalf("first app partner list: %+v err=%v", first, err)
	}
	second, err := service.User.ListPartner(context.Background(), user.PartnerListParams{
		Identity: secondIdentity,
		Provider: "fake",
		GroupKey: "daily",
		Platform: "telegram",
	})
	if err != nil || len(second) != 1 {
		t.Fatalf("second app partner list: %+v err=%v", second, err)
	}
	if first[0].Key == second[0].Key {
		t.Fatalf("two apps received the same partner issue: %s", first[0].Key)
	}

	checked, err := service.User.CheckPartner(context.Background(), user.PartnerCheckParams{
		Identity: secondIdentity,
		IssueRef: second[0].Key,
	})
	if err != nil || checked.Status == user.PartnerStatusNotFound {
		t.Fatalf("second app cannot use its listed issue: %+v err=%v", checked, err)
	}
}

func openTasksPostgres(database string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		pgUser,
		pgPassword,
		pgHost,
		pgPort,
		database,
	)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func recreateTasksDatabase(ctx context.Context, adminDB *sql.DB, database string) error {
	if _, err := adminDB.ExecContext(
		ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		database,
	); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", database)); err != nil {
		return err
	}
	_, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", database))
	return err
}

func countTaskCallbacks(t testing.TB, database string) int {
	t.Helper()
	db, err := openTasksPostgres(database)
	if err != nil {
		t.Fatalf("open callback db: %v", err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM tasks_clb_event WHERE source_service = 'tasks'").Scan(&count); err != nil {
		t.Fatalf("count callbacks: %v", err)
	}
	return count
}

func TestTgrassProviderLiveManual(t *testing.T) {
	token := os.Getenv("TGRASS_TOKEN")
	userID := os.Getenv("TGRASS_USER_ID")
	if token == "" || userID == "" {
		t.Skip("set TGRASS_TOKEN and TGRASS_USER_ID to run live Tgrass check")
	}
	provider := user.TgrassProvider{Timeout: 15 * time.Second}
	params := user.PartnerListProviderParams{
		Identity: user.Identity{
			WorkspaceID:    testsupport.WorkspaceID("live"),
			Platform:       "tma",
			PlatformUserID: userID,
		},
		Config: repository.PartnerConfig{
			WorkspaceID: testsupport.WorkspaceID("live"),
			Provider:    "tgrass",
			GroupKey:    "tgrass",
			Platform:    "telegram",
			Secret:      &token,
		},
		Locale: "ru",
		Limit:  1,
		Now:    time.Now().UTC(),
	}
	tasks, err := provider.ListPartnerTasks(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		offerID := os.Getenv("TGRASS_OFFER_ID")
		if offerID == "" {
			t.Skip("Tgrass returned no available tasks")
		}
		tasks = []user.PartnerExternalTask{{
			ExternalID:     offerID,
			ExternalType:   "channel",
			PrivatePayload: []byte(`{"offer_id":` + offerID + `}`),
		}}
	}
	t.Logf("external_id=%s external_type=%s public_payload=%s", tasks[0].ExternalID, tasks[0].ExternalType, string(tasks[0].PublicPayload))
	check, err := provider.CheckPartnerTask(context.Background(), user.PartnerCheckProviderParams{
		Identity: params.Identity,
		Config:   params.Config,
		Issue: repository.PartnerIssue{
			ExternalID:     tasks[0].ExternalID,
			ExternalType:   tasks[0].ExternalType,
			PrivatePayload: tasks[0].PrivatePayload,
		},
		Now: params.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("check completed=%t status=%s payload=%s", check.Completed, check.Status, string(check.Payload))
}
