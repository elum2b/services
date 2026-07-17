package cpa_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/elum2b/services/cpa"
	"github.com/elum2b/services/cpa/repository"
	"github.com/elum2b/services/cpa/service/admin"
	"github.com/elum2b/services/cpa/service/user"
	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
)

const (
	cpaTestPGHost        = "localhost"
	cpaTestPGPort        = 5432
	cpaTestPGUser        = "postgres"
	cpaTestPGPassword    = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	cpaTestWorkspaceID   = "00000000-0000-0000-0000-000000000001"
	cpaImportWorkspaceID = "00000000-0000-0000-0000-000000000002"
	cpaOtherWorkspaceID  = "00000000-0000-0000-0000-000000000003"
)

var cpaTestDatabaseSequence atomic.Uint64

type cpaTestEnvironment struct {
	Context  context.Context
	Database *sql.DB
	Name     string
	Service  *cpa.CPA
}

func TestCPA_NewWithDatabaseAppliesDefaultCache(t *testing.T) {
	cache := newCPATestCache()
	env := newCPATestEnvironment(t, cpa.Options{
		Cache:        cache,
		CacheEnabled: true,
	})

	upsertSharedOffer(t, env, "cache_offer", true)

	if _, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "cache_offer"); err != nil {
		t.Fatalf("get offer on cache miss: %v", err)
	}
	setsAfterFirstRead := cache.DataSetCalls()
	if setsAfterFirstRead == 0 {
		t.Fatal("service created with CacheEnabled must populate the configured cache")
	}
	if cache.DataLastTTL() <= 0 {
		t.Fatal("service created with CacheEnabled must apply a positive default cache TTL")
	}

	if _, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "cache_offer"); err != nil {
		t.Fatalf("get offer on cache hit: %v", err)
	}
	if got := cache.DataSetCalls(); got != setsAfterFirstRead {
		t.Fatalf("second read repopulated cache: got %d writes, want %d", got, setsAfterFirstRead)
	}
}

func TestCPA_PublicStatusContractsSerializeAsStrings(t *testing.T) {
	value := struct {
		Assignment user.AssignmentModel       `json:"assignment"`
		Code       admin.CodeModel            `json:"code"`
		Event      admin.AssignmentEventModel `json:"event"`
	}{
		Assignment: user.AssignmentModel{Status: cpa.AssignmentStatusIssued},
		Code:       admin.CodeModel{Status: cpa.CodeStatusAvailable},
		Event:      admin.AssignmentEventModel{EventType: cpa.AssignmentEventTypeCompleted},
	}

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal public status contracts: %v", err)
	}

	var result struct {
		Assignment struct {
			Status string `json:"status"`
		} `json:"assignment"`
		Code struct {
			Status string `json:"status"`
		} `json:"code"`
		Event struct {
			EventType string `json:"event_type"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode public status contracts: %v", err)
	}
	if result.Assignment.Status != "issued" ||
		result.Code.Status != "available" ||
		result.Event.EventType != "completed" {
		t.Fatalf("public status JSON = %s", raw)
	}
}

func TestCPA_CacheVersionsInvalidateReadsOnOtherNode(t *testing.T) {
	cache := newCPATestCache()
	options := cpa.Options{
		Cache:        cache,
		CacheEnabled: true,
	}
	env := newCPATestEnvironment(t, options)
	nodeB := newCPAAdditionalNode(t, env, options)

	upsertSharedOffer(t, env, "distributed_offer", true)
	upsertLocalization(t, env, "distributed_offer", "ru", "Old title")

	if _, err := nodeB.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "distributed_offer"); err != nil {
		t.Fatalf("warm offer cache on node B: %v", err)
	}
	if _, err := nodeB.Admin.ListOffers(env.Context, cpaTestWorkspaceID, admin.Page{Limit: 10}); err != nil {
		t.Fatalf("warm admin list cache on node B: %v", err)
	}
	if _, err := nodeB.User.ListActive(env.Context, user.ListActiveParams{
		Identity: cpaTestIdentity("user-1"),
		Locale:   "ru",
	}); err != nil {
		t.Fatalf("warm user list cache on node B: %v", err)
	}

	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          "distributed_offer",
		Payload:     json.RawMessage(`{"kind":"updated"}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("SHARED-distributed_offer"),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("update offer on node A: %v", err)
	}
	upsertLocalization(t, env, "distributed_offer", "ru", "New title")

	offer, err := nodeB.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "distributed_offer")
	if err != nil || payloadKind(t, offer.Payload) != "updated" {
		t.Fatalf("node B returned stale offer: offer=%+v err=%v", offer, err)
	}
	adminOffers, err := nodeB.Admin.ListOffers(env.Context, cpaTestWorkspaceID, admin.Page{Limit: 10})
	if err != nil || len(adminOffers) != 1 || payloadKind(t, adminOffers[0].Payload) != "updated" {
		t.Fatalf("node B returned stale admin list: offers=%+v err=%v", adminOffers, err)
	}
	userOffers, err := nodeB.User.ListActive(env.Context, user.ListActiveParams{
		Identity: cpaTestIdentity("user-1"),
		Locale:   "ru",
	})
	if err != nil || len(userOffers) != 1 || userOffers[0].Title != "New title" {
		t.Fatalf("node B returned stale user list: offers=%+v err=%v", userOffers, err)
	}
}

func TestCPA_UserGetCodeReusesExistingAssignment(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "shared_offer", true)
	upsertReward(t, env, "shared_offer", "stars", 25, 2)
	upsertLocalization(t, env, "shared_offer", "ru", "Shared offer")

	identity := cpaTestIdentity("user-1")
	first, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    "shared_offer",
	})
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	if first.AlreadyIssued || first.Assignment.Code != "SHARED-shared_offer" {
		t.Fatalf("unexpected first issue result: %+v", first)
	}

	second, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    "shared_offer",
	})
	if err != nil {
		t.Fatalf("read existing code: %v", err)
	}
	if !second.AlreadyIssued || second.Assignment.ID != first.Assignment.ID {
		t.Fatalf("assignment is not idempotent: first=%+v second=%+v", first, second)
	}

	status, err := env.Service.User.GetStatus(env.Context, user.GetStatusParams{
		Identity: identity,
		CPAID:    "shared_offer",
	})
	if err != nil || status == nil || status.ID != first.Assignment.ID {
		t.Fatalf("get status: status=%+v err=%v", status, err)
	}
}

func TestCPA_AssignmentKeepsRewardSnapshotAfterCatalogChanges(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "snapshot_offer", true)
	upsertReward(t, env, "snapshot_offer", "stars", 25, 2)

	identity := cpaTestIdentity("snapshot-user")
	issued := issueCode(t, env, identity, "snapshot_offer")
	if len(issued.Rewards) != 1 || issued.Rewards[0].Key != "stars" || issued.Rewards[0].Quantity != 25 {
		t.Fatalf("issued reward snapshot = %+v", issued.Rewards)
	}

	upsertReward(t, env, "snapshot_offer", "stars", 100, 2)
	upsertReward(t, env, "snapshot_offer", "bonus", 1, 0)

	repeated := issueCode(t, env, identity, "snapshot_offer")
	if len(repeated.Rewards) != 1 || repeated.Rewards[0].Key != "stars" || repeated.Rewards[0].Quantity != 25 {
		t.Fatalf("existing assignment used current rewards: %+v", repeated.Rewards)
	}

	offers, err := env.Service.User.ListActive(env.Context, user.ListActiveParams{
		Identity: identity,
		Locale:   "ru",
	})
	if err != nil || len(offers) != 1 || len(offers[0].Rewards) != 1 || offers[0].Rewards[0].Quantity != 25 {
		t.Fatalf("list active did not use assignment snapshot: offers=%+v err=%v", offers, err)
	}

	completed, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    "snapshot_offer",
	})
	if err != nil || len(completed.Rewards) != 1 || completed.Rewards[0].Quantity != 25 {
		t.Fatalf("completion reward snapshot: result=%+v err=%v", completed, err)
	}

	if _, err := env.Service.Admin.DeleteReward(env.Context, cpaTestWorkspaceID, "snapshot_offer", "stars"); err != nil {
		t.Fatalf("delete catalog reward: %v", err)
	}
	completedAgain, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    "snapshot_offer",
	})
	if err != nil || !completedAgain.AlreadyDone || len(completedAgain.Rewards) != 1 || completedAgain.Rewards[0].Quantity != 25 {
		t.Fatalf("idempotent completion changed rewards: result=%+v err=%v", completedAgain, err)
	}

	events, err := env.Service.Admin.ListCallbackEvents(env.Context, admin.CallbackEventListParams{
		WorkspaceID: cpaTestWorkspaceID,
		EventType:   cpa.CallbackEventCompleted,
		Page:        admin.Page{Limit: 10},
	})
	if err != nil || len(events) != 1 {
		t.Fatalf("list completed callbacks: events=%+v err=%v", events, err)
	}
	var payload cpa.CallbackPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("decode completed callback: %v", err)
	}
	if len(payload.Rewards) != 1 || payload.Rewards[0].Quantity != 25 {
		t.Fatalf("completed callback changed reward snapshot: %+v", payload.Rewards)
	}
}

func TestCPA_UserGetCodeRejectsInactiveOffer(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "inactive_offer", false)

	_, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: cpaTestIdentity("user-1"),
		CPAID:    "inactive_offer",
	})
	if err == nil {
		t.Fatal("inactive offer must not issue a code")
	}
}

func TestCPA_UserMethodsRejectInvalidIdentity(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	identityTests := []struct {
		name     string
		identity user.Identity
	}{
		{
			name: "empty identity",
		},
		{
			name: "non UUID workspace",
			identity: user.Identity{
				WorkspaceID:    "workspace",
				AppID:          100,
				PlatformID:     200,
				PlatformUserID: "user",
			},
		},
		{
			name: "missing app id",
			identity: user.Identity{
				WorkspaceID:    cpaTestWorkspaceID,
				PlatformID:     200,
				PlatformUserID: "user",
			},
		},
		{
			name: "missing platform id",
			identity: user.Identity{
				WorkspaceID:    cpaTestWorkspaceID,
				AppID:          100,
				PlatformUserID: "user",
			},
		},
		{
			name: "missing platform user id",
			identity: user.Identity{
				WorkspaceID: cpaTestWorkspaceID,
				AppID:       100,
				PlatformID:  200,
			},
		},
	}
	methodTests := []struct {
		name string
		call func(user.Identity) error
	}{
		{
			name: "list active",
			call: func(identity user.Identity) error {
				_, err := env.Service.User.ListActive(env.Context, user.ListActiveParams{
					Identity: identity,
					Locale:   "ru",
				})
				return err
			},
		},
		{
			name: "get code",
			call: func(identity user.Identity) error {
				_, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
					Identity: identity,
					CPAID:    "offer",
				})
				return err
			},
		},
		{
			name: "get status",
			call: func(identity user.Identity) error {
				_, err := env.Service.User.GetStatus(env.Context, user.GetStatusParams{
					Identity: identity,
					CPAID:    "offer",
				})
				return err
			},
		},
	}

	for _, identityTest := range identityTests {
		t.Run(identityTest.name, func(t *testing.T) {
			for _, methodTest := range methodTests {
				t.Run(methodTest.name, func(t *testing.T) {
					err := methodTest.call(identityTest.identity)
					if serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
						t.Fatalf("invalid identity error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
					}
				})
			}
		})
	}
}

func TestCPA_UserCodeMethodsRejectEmptyOfferID(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	identity := cpaTestIdentity("empty-offer-user")

	_, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{Identity: identity})
	if serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
		t.Fatalf("get code without offer id error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
	}

	_, err = env.Service.User.GetStatus(env.Context, user.GetStatusParams{Identity: identity})
	if serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
		t.Fatalf("get status without offer id error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
	}
}

func TestCPA_UserListActiveAppliesTarget(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "public_offer", true)
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          "premium_offer",
		Payload:     json.RawMessage(`{"kind":"premium"}`),
		Target:      json.RawMessage(`{"is_premium":true}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("PREMIUM"),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("upsert premium offer: %v", err)
	}

	offers, err := env.Service.User.ListActive(env.Context, user.ListActiveParams{
		Identity: cpaTestIdentity("user-1"),
		Locale:   "ru",
	})
	if err != nil {
		t.Fatalf("list active offers: %v", err)
	}
	if len(offers) != 1 || offers[0].ID != "public_offer" {
		t.Fatalf("targeted offer leaked to non-premium user: %+v", offers)
	}
}

func TestCPA_UserListActiveReevaluatesTimeWindowOnCacheHit(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	endAt := time.Now().UTC().Add(500 * time.Millisecond)
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          "expiring-offer",
		Payload:     json.RawMessage(`{}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("EXPIRING"),
		IsActive:    true,
		EndAt:       &endAt,
	}); err != nil {
		t.Fatalf("upsert expiring offer: %v", err)
	}

	params := user.ListActiveParams{
		Identity: cpaTestIdentity("expiring-user"),
		Locale:   "ru",
	}
	items, err := env.Service.User.ListActive(env.Context, params)
	if err != nil || len(items) != 1 {
		t.Fatalf("list active offer before expiration: items=%+v err=%v", items, err)
	}

	time.Sleep(750 * time.Millisecond)
	items, err = env.Service.User.ListActive(env.Context, params)
	if err != nil || len(items) != 0 {
		t.Fatalf("expired offer remained visible from cache: items=%+v err=%v", items, err)
	}
}

func TestCPA_UserGetCodeAllocatesDifferentPoolCodes(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	pool := repository.CodeSourcePool
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          "pool_offer",
		Payload:     json.RawMessage(`{}`),
		CodeMode:    repository.CodeModePersonal,
		CodeSource:  &pool,
		IsActive:    true,
	}); err != nil {
		t.Fatalf("upsert pool offer: %v", err)
	}
	added, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       "pool_offer",
		Codes:       []string{"POOL-1", "POOL-2", "POOL-2"},
	})
	if err != nil || added != 2 {
		t.Fatalf("add pool codes: added=%d err=%v", added, err)
	}

	first, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: cpaTestIdentity("user-1"),
		CPAID:    "pool_offer",
	})
	if err != nil {
		t.Fatalf("issue first pool code: %v", err)
	}
	second, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: cpaTestIdentity("user-2"),
		CPAID:    "pool_offer",
	})
	if err != nil {
		t.Fatalf("issue second pool code: %v", err)
	}
	if first.Assignment.Code == second.Assignment.Code {
		t.Fatalf("pool code was reused: %q", first.Assignment.Code)
	}
}

func TestCPA_AdminDeleteCodesKeepsIssuedAndCompletedAssignments(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())

	t.Run("issued assignment", func(t *testing.T) {
		upsertPoolOffer(t, env, "issued_offer")
		addPoolCode(t, env, "issued_offer", "ISSUED-1")
		identity := cpaTestIdentity("issued-user")
		issued := issueCode(t, env, identity, "issued_offer")

		deleted, err := env.Service.Admin.DeleteIssuedCodes(env.Context, cpaTestWorkspaceID, "issued_offer")
		if err != nil || deleted != 1 {
			t.Fatalf("delete issued code: deleted=%d err=%v", deleted, err)
		}
		repeated := issueCode(t, env, identity, "issued_offer")
		if !repeated.AlreadyIssued || repeated.Assignment.ID != issued.Assignment.ID || repeated.Assignment.Code != issued.Assignment.Code {
			t.Fatalf("issued assignment was not preserved: first=%+v repeated=%+v", issued, repeated)
		}
		assertAssignmentIsVisible(t, env, identity, "issued_offer", issued.Assignment.ID)
	})

	t.Run("completed assignment", func(t *testing.T) {
		upsertPoolOffer(t, env, "completed_offer")
		addPoolCode(t, env, "completed_offer", "COMPLETED-1")
		identity := cpaTestIdentity("completed-user")
		completed := issueCode(t, env, identity, "completed_offer")
		if _, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
			Identity: identity,
			CPAID:    "completed_offer",
		}); err != nil {
			t.Fatalf("complete assignment: %v", err)
		}

		deleted, err := env.Service.Admin.DeleteCompletedCodes(env.Context, cpaTestWorkspaceID, "completed_offer")
		if err != nil || deleted != 1 {
			t.Fatalf("delete completed code: deleted=%d err=%v", deleted, err)
		}
		repeated := issueCode(t, env, identity, "completed_offer")
		if !repeated.AlreadyIssued || repeated.Assignment.ID != completed.Assignment.ID || repeated.Assignment.Status != cpa.AssignmentStatusCompleted {
			t.Fatalf("completed assignment was not preserved: first=%+v repeated=%+v", completed, repeated)
		}
		assertAssignmentIsVisible(t, env, identity, "completed_offer", completed.Assignment.ID)
	})
}

func TestCPA_AdminDeleteOfferReturnsDomainErrorWhenAssignmentExists(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "used_offer", true)
	issueCode(t, env, cpaTestIdentity("used-offer-user"), "used_offer")

	deleted, err := env.Service.Admin.DeleteOffer(env.Context, cpaTestWorkspaceID, "used_offer")
	if deleted != 0 || !errors.Is(err, repository.ErrOfferInUse) {
		t.Fatalf("delete used offer: rows=%d err=%v, want ErrOfferInUse", deleted, err)
	}
}

func TestCPA_AdminAddCodesRechecksOfferModeInsideWorkspaceLock(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertPoolOffer(t, env, "mode_race_offer")

	tx, err := env.Database.BeginTx(env.Context, nil)
	if err != nil {
		t.Fatalf("begin mode switch transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(
		env.Context,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		cpaTestWorkspaceID,
	); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
			WorkspaceID: cpaTestWorkspaceID,
			CPAID:       "mode_race_offer",
			Codes:       []string{"MUST-NOT-BE-ADDED"},
		})
		result <- err
	}()

	select {
	case err := <-result:
		t.Fatalf("AddCodes bypassed workspace lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if _, err := tx.ExecContext(env.Context, `
UPDATE cpa_offer
SET code_mode = 'shared_code',
    code_source = NULL,
    shared_code = 'SHARED-AFTER-SWITCH',
    generated_length = NULL,
    generated_alphabet = NULL
WHERE workspace_id = $1 AND id = $2`, cpaTestWorkspaceID, "mode_race_offer"); err != nil {
		t.Fatalf("switch offer mode: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit mode switch: %v", err)
	}

	if err := <-result; !errors.Is(err, repository.ErrCodeUploadMode) {
		t.Fatalf("AddCodes after mode switch error = %v, want ErrCodeUploadMode", err)
	}
}

func TestCPA_NestedCatalogMutationsUseWorkspaceLock(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "locked_nested_offer", true)

	operations := []struct {
		name string
		call func() error
	}{
		{
			name: "upsert localization",
			call: func() error {
				return env.Service.Admin.UpsertLocalization(env.Context, admin.UpsertLocalizationParams{
					WorkspaceID: cpaTestWorkspaceID,
					CPAID:       "locked_nested_offer",
					Locale:      "ru",
					Title:       "Locked",
				})
			},
		},
		{
			name: "delete localization",
			call: func() error {
				_, err := env.Service.Admin.DeleteLocalization(env.Context, cpaTestWorkspaceID, "locked_nested_offer", "ru")
				return err
			},
		},
		{
			name: "upsert reward",
			call: func() error {
				return env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
					WorkspaceID: cpaTestWorkspaceID,
					CPAID:       "locked_nested_offer",
					Key:         "stars",
					Quantity:    1,
				})
			},
		},
		{
			name: "delete reward",
			call: func() error {
				_, err := env.Service.Admin.DeleteReward(env.Context, cpaTestWorkspaceID, "locked_nested_offer", "stars")
				return err
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			tx, err := env.Database.BeginTx(env.Context, nil)
			if err != nil {
				t.Fatalf("begin lock transaction: %v", err)
			}
			if _, err := tx.ExecContext(
				env.Context,
				"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
				cpaTestWorkspaceID,
			); err != nil {
				_ = tx.Rollback()
				t.Fatalf("lock workspace: %v", err)
			}

			result := make(chan error, 1)
			go func() { result <- operation.call() }()

			select {
			case err := <-result:
				_ = tx.Rollback()
				t.Fatalf("operation bypassed workspace lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}

			if err := tx.Rollback(); err != nil {
				t.Fatalf("release workspace lock: %v", err)
			}
			if err := <-result; err != nil {
				t.Fatalf("operation after lock release: %v", err)
			}
		})
	}
}

func TestCPA_UserGetCodeIsConcurrentAndIdempotent(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "concurrent_offer", true)

	const workers = 8
	identity := cpaTestIdentity("concurrent-user")
	results := make(chan user.GetCodeResult, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
				Identity: identity,
				CPAID:    "concurrent_offer",
			})
			results <- result
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent issue: %v", err)
		}
	}
	var assignmentID uint64
	for result := range results {
		if assignmentID == 0 {
			assignmentID = result.Assignment.ID
			continue
		}
		if result.Assignment.ID != assignmentID {
			t.Fatalf("concurrent requests created different assignments: %d and %d", assignmentID, result.Assignment.ID)
		}
	}
}

func TestCPA_UserGetCodeIssuesPopularOfferForDifferentUsersInParallel(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "parallel_offer", true)

	const workers = 16
	results := make(chan user.GetCodeResult, workers)
	errs := make(chan error, workers)
	var group sync.WaitGroup
	for index := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
				Identity: cpaTestIdentity(fmt.Sprintf("parallel-user-%d", index)),
				CPAID:    "parallel_offer",
			})
			results <- result
			errs <- err
		}()
	}
	group.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("parallel issue: %v", err)
		}
	}
	assignmentIDs := make(map[uint64]struct{}, workers)
	for result := range results {
		assignmentIDs[result.Assignment.ID] = struct{}{}
	}
	if len(assignmentIDs) != workers {
		t.Fatalf("parallel issues created %d assignments, want %d", len(assignmentIDs), workers)
	}
}

func TestCPA_AdminCompleteIsIdempotentAndUpdatesDailyStats(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "complete_offer", true)
	identity := cpaTestIdentity("user-1")
	if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    "complete_offer",
	}); err != nil {
		t.Fatalf("issue code: %v", err)
	}

	first, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    "complete_offer",
	})
	if err != nil || first.AlreadyDone || first.Assignment.Status != cpa.AssignmentStatusCompleted {
		t.Fatalf("complete assignment: result=%+v err=%v", first, err)
	}
	second, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    "complete_offer",
	})
	if err != nil || !second.AlreadyDone {
		t.Fatalf("repeat completion must be idempotent: result=%+v err=%v", second, err)
	}

	now := time.Now().UTC()
	if err := env.Service.Admin.RefreshDailyStats(env.Context, cpaTestWorkspaceID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}
	stats, err := env.Service.Admin.ListDailyStats(env.Context, cpaTestWorkspaceID, "complete_offer", now, now)
	if err != nil || len(stats) != 1 || stats[0].IssuedCount != 1 || stats[0].CompletedCount != 1 {
		t.Fatalf("unexpected daily stats: stats=%+v err=%v", stats, err)
	}
}

func TestCPA_DailyStatsAlwaysUseUTCDate(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "utc_stats_offer", true)
	issueCode(t, env, cpaTestIdentity("utc-stats-user"), "utc_stats_offer")

	eventTime := time.Date(2026, time.January, 2, 0, 30, 0, 0, time.UTC)
	if _, err := env.Database.ExecContext(env.Context, `
UPDATE cpa_assignment_event
SET occurred_at = $1
WHERE workspace_id = $2 AND cpa_id = $3`, eventTime, cpaTestWorkspaceID, "utc_stats_offer"); err != nil {
		t.Fatalf("move event near UTC midnight: %v", err)
	}

	env.Database.SetMaxOpenConns(1)
	if _, err := env.Database.ExecContext(env.Context, "SET TIME ZONE 'America/Los_Angeles'"); err != nil {
		t.Fatalf("set non-UTC session timezone: %v", err)
	}

	from := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	until := from.Add(24 * time.Hour)
	if err := env.Service.Admin.RefreshDailyStats(env.Context, cpaTestWorkspaceID, from, until); err != nil {
		t.Fatalf("refresh UTC daily stats: %v", err)
	}
	stats, err := env.Service.Admin.ListDailyStats(env.Context, cpaTestWorkspaceID, "utc_stats_offer", from, from)
	if err != nil || len(stats) != 1 || stats[0].Date.Day() != 2 || stats[0].IssuedCount != 1 {
		t.Fatalf("UTC daily stats: values=%+v err=%v", stats, err)
	}
}

func TestCPA_AdminUpsertOfferRejectsInvalidConfiguration(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	tests := []struct {
		name   string
		params admin.UpsertOfferParams
	}{
		{
			name: "invalid payload",
			params: admin.UpsertOfferParams{
				WorkspaceID: cpaTestWorkspaceID,
				ID:          "invalid_payload",
				Payload:     json.RawMessage(`{`),
				CodeMode:    repository.CodeModeShared,
				SharedCode:  stringPointer("CODE"),
			},
		},
		{
			name: "invalid target rules",
			params: admin.UpsertOfferParams{
				WorkspaceID: cpaTestWorkspaceID,
				ID:          "invalid_target",
				Payload:     json.RawMessage(`{}`),
				Target:      json.RawMessage(`{"sex":{}}`),
				CodeMode:    repository.CodeModeShared,
				SharedCode:  stringPointer("CODE"),
			},
		},
		{
			name: "missing shared code",
			params: admin.UpsertOfferParams{
				WorkspaceID: cpaTestWorkspaceID,
				ID:          "missing_shared_code",
				Payload:     json.RawMessage(`{}`),
				CodeMode:    repository.CodeModeShared,
			},
		},
		{
			name: "generated code needs alphabet",
			params: admin.UpsertOfferParams{
				WorkspaceID:     cpaTestWorkspaceID,
				ID:              "missing_alphabet",
				Payload:         json.RawMessage(`{}`),
				CodeMode:        repository.CodeModePersonal,
				CodeSource:      stringPointer(repository.CodeSourceGenerated),
				GeneratedLength: int16Pointer(8),
			},
		},
		{
			name: "generated alphabet needs distinct symbols",
			params: admin.UpsertOfferParams{
				WorkspaceID:       cpaTestWorkspaceID,
				ID:                "duplicate_alphabet",
				Payload:           json.RawMessage(`{}`),
				CodeMode:          repository.CodeModePersonal,
				CodeSource:        stringPointer(repository.CodeSourceGenerated),
				GeneratedLength:   int16Pointer(8),
				GeneratedAlphabet: stringPointer("aa"),
			},
		},
		{
			name: "generated code exceeds stored code length",
			params: admin.UpsertOfferParams{
				WorkspaceID:       cpaTestWorkspaceID,
				ID:                "long_generated_code",
				Payload:           json.RawMessage(`{}`),
				CodeMode:          repository.CodeModePersonal,
				CodeSource:        stringPointer(repository.CodeSourceGenerated),
				GeneratedLength:   int16Pointer(513),
				GeneratedAlphabet: stringPointer("ab"),
			},
		},
		{
			name: "shared code exceeds stored length",
			params: admin.UpsertOfferParams{
				WorkspaceID: cpaTestWorkspaceID,
				ID:          "long_shared_code",
				Payload:     json.RawMessage(`{}`),
				CodeMode:    repository.CodeModeShared,
				SharedCode:  stringPointer(strings.Repeat("x", 513)),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := env.Service.Admin.UpsertOffer(env.Context, test.params); err == nil {
				t.Fatal("invalid offer configuration must be rejected")
			}
		})
	}
}

func TestCPA_AdminUpsertRewardRejectsInvalidConfiguration(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "reward_offer", true)

	err := env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       "reward_offer",
		Key:         "stars",
		Type:        "quantity",
		Quantity:    0,
	})
	if err == nil {
		t.Fatal("zero reward quantity must be rejected")
	}

	if err := env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       "reward_offer",
		Key:         "max-scale",
		Quantity:    1,
		Scale:       ^uint16(0),
	}); err != nil {
		t.Fatalf("uint16 scale must fit the SQL contract: %v", err)
	}
}

func TestCPA_AdminMethodsRejectInvalidInput(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	validIdentity := cpaTestIdentity("invalid-input-user")
	now := time.Now().UTC()
	upsertPoolOffer(t, env, "invalid-input-pool")

	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "get offer without workspace",
			call: func() error {
				_, err := env.Service.Admin.GetOffer(env.Context, "", "offer")
				return err
			},
		},
		{
			name: "list offers without workspace",
			call: func() error {
				_, err := env.Service.Admin.ListOffers(env.Context, "", admin.Page{})
				return err
			},
		},
		{
			name: "delete offer without id",
			call: func() error {
				_, err := env.Service.Admin.DeleteOffer(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "add codes without offer id",
			call: func() error {
				_, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
					WorkspaceID: cpaTestWorkspaceID,
					Codes:       []string{"CODE"},
				})
				return err
			},
		},
		{
			name: "add empty code list",
			call: func() error {
				_, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
					WorkspaceID: cpaTestWorkspaceID,
					CPAID:       "invalid-input-pool",
				})
				return err
			},
		},
		{
			name: "add blank code",
			call: func() error {
				_, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
					WorkspaceID: cpaTestWorkspaceID,
					CPAID:       "invalid-input-pool",
					Codes:       []string{" "},
				})
				return err
			},
		},
		{
			name: "delete available codes without offer id",
			call: func() error {
				_, err := env.Service.Admin.DeleteAvailableCodes(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "delete issued codes without offer id",
			call: func() error {
				_, err := env.Service.Admin.DeleteIssuedCodes(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "delete completed codes without offer id",
			call: func() error {
				_, err := env.Service.Admin.DeleteCompletedCodes(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "complete without offer id",
			call: func() error {
				_, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
					Identity: validIdentity,
				})
				return err
			},
		},
		{
			name: "get user assignment without identity",
			call: func() error {
				_, err := env.Service.Admin.GetUserAssignment(env.Context, user.GetStatusParams{CPAID: "offer"})
				return err
			},
		},
		{
			name: "list assignments without offer id",
			call: func() error {
				_, err := env.Service.Admin.ListAssignments(env.Context, admin.AssignmentListParams{WorkspaceID: cpaTestWorkspaceID})
				return err
			},
		},
		{
			name: "list codes without offer id",
			call: func() error {
				_, err := env.Service.Admin.ListCodes(env.Context, admin.CodeListParams{WorkspaceID: cpaTestWorkspaceID})
				return err
			},
		},
		{
			name: "list assignment events without offer id",
			call: func() error {
				_, err := env.Service.Admin.ListAssignmentEvents(env.Context, admin.AssignmentEventListParams{WorkspaceID: cpaTestWorkspaceID})
				return err
			},
		},
		{
			name: "list localizations without offer id",
			call: func() error {
				_, err := env.Service.Admin.ListLocalizations(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "delete localization without locale",
			call: func() error {
				_, err := env.Service.Admin.DeleteLocalization(env.Context, cpaTestWorkspaceID, "offer", "")
				return err
			},
		},
		{
			name: "list rewards without offer id",
			call: func() error {
				_, err := env.Service.Admin.ListRewards(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "delete reward without key",
			call: func() error {
				_, err := env.Service.Admin.DeleteReward(env.Context, cpaTestWorkspaceID, "offer", "")
				return err
			},
		},
		{
			name: "get stats without offer id",
			call: func() error {
				_, err := env.Service.Admin.GetStats(env.Context, cpaTestWorkspaceID, "")
				return err
			},
		},
		{
			name: "list daily stats with inverted range",
			call: func() error {
				_, err := env.Service.Admin.ListDailyStats(env.Context, cpaTestWorkspaceID, "offer", now, now.Add(-time.Hour))
				return err
			},
		},
		{
			name: "refresh daily stats with empty range",
			call: func() error {
				return env.Service.Admin.RefreshDailyStats(env.Context, cpaTestWorkspaceID, time.Time{}, now)
			},
		},
		{
			name: "export without workspace",
			call: func() error {
				_, err := env.Service.Admin.Export(env.Context, "", admin.ExportRequest{})
				return err
			},
		},
		{
			name: "preview import without workspace",
			call: func() error {
				_, err := env.Service.Admin.PreviewImport(env.Context, "", admin.ExportPackage{
					Format:  repository.ExportFormat,
					Service: "cpa",
				})
				return err
			},
		},
		{
			name: "import without workspace",
			call: func() error {
				_, err := env.Service.Admin.Import(env.Context, "", admin.ImportRequest{
					Package: admin.ExportPackage{
						Format:  repository.ExportFormat,
						Service: "cpa",
					},
				})
				return err
			},
		},
		{
			name: "get callback event without id",
			call: func() error {
				_, err := env.Service.Admin.GetCallbackEvent(env.Context, cpaTestWorkspaceID, 0)
				return err
			},
		},
		{
			name: "retry callback event without id",
			call: func() error {
				_, err := env.Service.Admin.RetryCallbackEventNow(env.Context, cpaTestWorkspaceID, 0)
				return err
			},
		},
		{
			name: "mark callback event ok without id",
			call: func() error {
				_, err := env.Service.Admin.MarkCallbackEventOK(env.Context, cpaTestWorkspaceID, 0)
				return err
			},
		},
		{
			name: "reject callback event without reason",
			call: func() error {
				_, err := env.Service.Admin.MarkCallbackEventReject(env.Context, cpaTestWorkspaceID, 1, "")
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
				t.Fatalf("invalid input error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
			}
		})
	}
}

func TestCPA_AdminMethodsManageOfferState(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	poolOfferID := "admin-methods-pool"
	identity := cpaTestIdentity("admin-methods-user")

	upsertPoolOffer(t, env, poolOfferID)
	upsertLocalization(t, env, poolOfferID, "ru", "Pool offer")
	upsertReward(t, env, poolOfferID, "stars", 25, 2)

	localizations, err := env.Service.Admin.ListLocalizations(env.Context, cpaTestWorkspaceID, poolOfferID)
	if err != nil || len(localizations) != 1 || localizations[0].Title != "Pool offer" {
		t.Fatalf("list localizations: values=%+v err=%v", localizations, err)
	}
	rewards, err := env.Service.Admin.ListRewards(env.Context, cpaTestWorkspaceID, poolOfferID)
	if err != nil || len(rewards) != 1 || rewards[0].Key != "stars" {
		t.Fatalf("list rewards: values=%+v err=%v", rewards, err)
	}

	added, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       poolOfferID,
		Codes:       []string{"POOL-1", "POOL-2"},
	})
	if err != nil || added != 2 {
		t.Fatalf("add codes: added=%d err=%v", added, err)
	}

	codes, err := env.Service.Admin.ListCodes(env.Context, admin.CodeListParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       poolOfferID,
	})
	if err != nil || len(codes) != 2 {
		t.Fatalf("list codes: values=%+v err=%v", codes, err)
	}

	issued := issueCode(t, env, identity, poolOfferID)
	assignment, err := env.Service.Admin.GetUserAssignment(env.Context, user.GetStatusParams{
		Identity: identity,
		CPAID:    poolOfferID,
	})
	if err != nil || assignment == nil || assignment.ID != issued.Assignment.ID {
		t.Fatalf("get user assignment: assignment=%+v err=%v", assignment, err)
	}

	assignments, err := env.Service.Admin.ListAssignments(env.Context, admin.AssignmentListParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       poolOfferID,
	})
	if err != nil || len(assignments) != 1 || assignments[0].ID != issued.Assignment.ID {
		t.Fatalf("list assignments: values=%+v err=%v", assignments, err)
	}

	events, err := env.Service.Admin.ListAssignmentEvents(env.Context, admin.AssignmentEventListParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       poolOfferID,
		EventType:   cpa.AssignmentEventTypeIssued,
	})
	if err != nil || len(events) != 1 || events[0].AssignmentID != issued.Assignment.ID {
		t.Fatalf("list assignment events: values=%+v err=%v", events, err)
	}

	deletedAvailable, err := env.Service.Admin.DeleteAvailableCodes(env.Context, cpaTestWorkspaceID, poolOfferID)
	if err != nil || deletedAvailable != 1 {
		t.Fatalf("delete available codes: rows=%d err=%v", deletedAvailable, err)
	}
	deletedIssued, err := env.Service.Admin.DeleteIssuedCodes(env.Context, cpaTestWorkspaceID, poolOfferID)
	if err != nil || deletedIssued != 1 {
		t.Fatalf("delete issued codes: rows=%d err=%v", deletedIssued, err)
	}

	completed, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    poolOfferID,
	})
	if err != nil || completed.AlreadyDone || completed.Assignment.Status != cpa.AssignmentStatusCompleted {
		t.Fatalf("complete assignment: result=%+v err=%v", completed, err)
	}

	stats, err := env.Service.Admin.GetStats(env.Context, cpaTestWorkspaceID, poolOfferID)
	if err != nil || stats.AssignmentsTotal != 1 || stats.CompletedTotal != 1 {
		t.Fatalf("get stats: stats=%+v err=%v", stats, err)
	}

	now := time.Now().UTC()
	if err := env.Service.Admin.RefreshDailyStats(env.Context, cpaTestWorkspaceID, now.Add(-time.Hour), now.Add(time.Hour)); err != nil {
		t.Fatalf("refresh daily stats: %v", err)
	}
	dailyStats, err := env.Service.Admin.ListDailyStats(env.Context, cpaTestWorkspaceID, poolOfferID, now, now)
	if err != nil || len(dailyStats) != 1 || dailyStats[0].CompletedCount != 1 {
		t.Fatalf("list daily stats: values=%+v err=%v", dailyStats, err)
	}

	if deleted, err := env.Service.Admin.DeleteLocalization(env.Context, cpaTestWorkspaceID, poolOfferID, "ru"); err != nil || deleted != 1 {
		t.Fatalf("delete localization: rows=%d err=%v", deleted, err)
	}
	if deleted, err := env.Service.Admin.DeleteReward(env.Context, cpaTestWorkspaceID, poolOfferID, "stars"); err != nil || deleted != 1 {
		t.Fatalf("delete reward: rows=%d err=%v", deleted, err)
	}

	standaloneOfferID := "admin-methods-delete"
	upsertSharedOffer(t, env, standaloneOfferID, true)
	if deleted, err := env.Service.Admin.DeleteOffer(env.Context, cpaTestWorkspaceID, standaloneOfferID); err != nil || deleted != 1 {
		t.Fatalf("delete offer: rows=%d err=%v", deleted, err)
	}
}

func TestCPA_AdminCallbackMethodsManageCallbackEvents(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "callback-admin-offer", true)

	issueCode(t, env, cpaTestIdentity("callback-admin-user-1"), "callback-admin-offer")
	events, err := env.Service.Admin.ListCallbackEvents(env.Context, admin.CallbackEventListParams{
		WorkspaceID: cpaTestWorkspaceID,
		EventType:   cpa.CallbackEventIssued,
		Page:        admin.Page{Limit: 10},
	})
	if err != nil || len(events) != 1 {
		t.Fatalf("list callback events: values=%+v err=%v", events, err)
	}

	first, err := env.Service.Admin.GetCallbackEvent(env.Context, cpaTestWorkspaceID, events[0].ID)
	if err != nil || first.EventType != cpa.CallbackEventIssued {
		t.Fatalf("get callback event: event=%+v err=%v", first, err)
	}
	if _, err := env.Service.Admin.GetCallbackEvent(env.Context, cpaOtherWorkspaceID, first.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-workspace callback read error = %v, want sql.ErrNoRows", err)
	}
	if affected, err := env.Service.Admin.MarkCallbackEventOK(env.Context, cpaOtherWorkspaceID, first.ID); err != nil || affected != 0 {
		t.Fatalf("cross-workspace callback update: rows=%d err=%v", affected, err)
	}
	if affected, err := env.Service.Admin.RetryCallbackEventNow(env.Context, cpaTestWorkspaceID, first.ID); err != nil || affected != 1 {
		t.Fatalf("retry callback event: rows=%d err=%v", affected, err)
	}
	if affected, err := env.Service.Admin.MarkCallbackEventOK(env.Context, cpaTestWorkspaceID, first.ID); err != nil || affected != 1 {
		t.Fatalf("mark callback event ok: rows=%d err=%v", affected, err)
	}

	issueCode(t, env, cpaTestIdentity("callback-admin-user-2"), "callback-admin-offer")
	events, err = env.Service.Admin.ListCallbackEvents(env.Context, admin.CallbackEventListParams{
		WorkspaceID: cpaTestWorkspaceID,
		EventType:   cpa.CallbackEventIssued,
		Page:        admin.Page{Limit: 10},
	})
	if err != nil || len(events) != 2 {
		t.Fatalf("list callback events after second issue: values=%+v err=%v", events, err)
	}
	if affected, err := env.Service.Admin.MarkCallbackEventReject(env.Context, cpaTestWorkspaceID, events[0].ID, "test rejection"); err != nil || affected != 1 {
		t.Fatalf("reject callback event: rows=%d err=%v", affected, err)
	}
	if _, err := env.Service.Admin.ResetExpiredCallbackProcessing(env.Context, cpaTestWorkspaceID); err != nil {
		t.Fatalf("reset expired callback processing: %v", err)
	}
}

func TestCPA_AdminExportAndImportPreserveOffer(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "export_offer", true)
	upsertLocalization(t, env, "export_offer", "en", "Export offer")
	upsertReward(t, env, "export_offer", "stars", 25, 2)

	pkg, err := env.Service.Admin.Export(env.Context, cpaTestWorkspaceID, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export offers: %v", err)
	}
	rawPackage, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal export package: %v", err)
	}
	var exportObject map[string]json.RawMessage
	if err := json.Unmarshal(rawPackage, &exportObject); err != nil {
		t.Fatalf("decode export package: %v", err)
	}
	if _, exists := exportObject["items"]; exists {
		t.Fatal("CPA export must not duplicate reference items")
	}
	preview, err := env.Service.Admin.PreviewImport(env.Context, cpaImportWorkspaceID, pkg)
	if err != nil || preview.Counts.Offers != 1 || len(preview.Conflicts) != 0 {
		t.Fatalf("preview import: preview=%+v err=%v", preview, err)
	}
	if _, err := env.Service.Admin.Import(env.Context, cpaImportWorkspaceID, admin.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	}); err != nil {
		t.Fatalf("import offers: %v", err)
	}
	imported, err := env.Service.Admin.GetOffer(env.Context, cpaImportWorkspaceID, "export_offer")
	if err != nil {
		t.Fatalf("read imported offer: %v", err)
	}
	if len(imported.Localizations) != 1 || len(imported.Rewards) != 1 || imported.Rewards[0].Scale != 2 {
		t.Fatalf("import did not preserve nested data: %+v", imported)
	}
}

func TestCPA_AdminImportUpdateReplacesNestedOfferSnapshot(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaImportWorkspaceID,
		ID:          "replace_nested_offer",
		Payload:     json.RawMessage(`{"version":"old"}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("OLD"),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("seed imported offer: %v", err)
	}
	for locale, title := range map[string]string{"ru": "Старый", "en": "Old"} {
		if err := env.Service.Admin.UpsertLocalization(env.Context, admin.UpsertLocalizationParams{
			WorkspaceID: cpaImportWorkspaceID,
			CPAID:       "replace_nested_offer",
			Locale:      locale,
			Title:       title,
			Description: title,
		}); err != nil {
			t.Fatalf("seed localization %s: %v", locale, err)
		}
	}
	for key, quantity := range map[string]int64{"stars": 10, "obsolete": 99} {
		if err := env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
			WorkspaceID: cpaImportWorkspaceID,
			CPAID:       "replace_nested_offer",
			Key:         key,
			Quantity:    quantity,
		}); err != nil {
			t.Fatalf("seed reward %s: %v", key, err)
		}
	}

	_, err := env.Service.Admin.Import(env.Context, cpaImportWorkspaceID, admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  repository.ExportFormat,
			Service: "cpa",
			Offers: []admin.ExportOffer{
				{
					ID:         "replace_nested_offer",
					Payload:    json.RawMessage(`{"version":"new"}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("NEW"),
					IsActive:   true,
					Localization: map[string]admin.ExportText{
						"ru": {Title: "Новый", Description: "Новый"},
					},
					Rewards: []admin.ExportReward{
						{Key: "stars", Type: "quantity", Quantity: 25, Scale: 2},
					},
				},
			},
		},
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("update existing import: %v", err)
	}

	offer, err := env.Service.Admin.GetOffer(env.Context, cpaImportWorkspaceID, "replace_nested_offer")
	if err != nil {
		t.Fatalf("get replaced offer: %v", err)
	}
	if len(offer.Localizations) != 1 || offer.Localizations[0].Locale != "ru" {
		t.Fatalf("stale localization remained after import: %+v", offer.Localizations)
	}
	if len(offer.Rewards) != 1 || offer.Rewards[0].Key != "stars" || offer.Rewards[0].Quantity != 25 {
		t.Fatalf("stale reward remained after import: %+v", offer.Rewards)
	}
}

func TestCPA_AdminExportAndFailOnConflictInspectAllOffers(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	for index := 0; index <= 1000; index++ {
		upsertSharedOffer(t, env, fmt.Sprintf("offer-%04d", index), true)
	}

	pkg, err := env.Service.Admin.Export(env.Context, cpaTestWorkspaceID, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export all offers: %v", err)
	}
	if len(pkg.Offers) != 1001 {
		t.Fatalf("exported offers = %d, want 1001", len(pkg.Offers))
	}

	_, err = env.Service.Admin.Import(env.Context, cpaTestWorkspaceID, admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  repository.ExportFormat,
			Service: "cpa",
			Offers: []admin.ExportOffer{
				{
					ID:         "offer-0000",
					Payload:    json.RawMessage(`{}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("CONFLICT"),
				},
			},
		},
		ConflictStrategy: repository.ImportConflictFail,
	})
	if err == nil {
		t.Fatal("fail_on_conflict must find an offer beyond the first 1000 rows")
	}
}

func TestCPA_AdminImportFailOnConflictIsAtomicAgainstConcurrentOfferWrite(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	transaction, err := env.Database.BeginTx(env.Context, nil)
	if err != nil {
		t.Fatalf("begin competing offer transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(
		env.Context,
		"SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
		cpaTestWorkspaceID,
	); err != nil {
		t.Fatalf("lock workspace for competing write: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := env.Service.Admin.Import(env.Context, cpaTestWorkspaceID, admin.ImportRequest{
			Package: admin.ExportPackage{
				Format:  repository.ExportFormat,
				Service: "cpa",
				Offers:  []admin.ExportOffer{cpaTestExportOffer("concurrent-offer")},
			},
			ConflictStrategy: repository.ImportConflictFail,
		})
		result <- err
	}()

	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		err := env.Database.QueryRowContext(env.Context, `
SELECT EXISTS (
    SELECT 1
    FROM pg_stat_activity
    WHERE datname = current_database()
      AND wait_event_type = 'Lock'
      AND query LIKE '%pg_advisory_xact_lock%'
)`).Scan(&waiting)
		if err != nil {
			t.Fatalf("observe waiting import transaction: %v", err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("import did not wait for the workspace conflict lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := transaction.ExecContext(env.Context, `
INSERT INTO cpa_offer (
    workspace_id, id, payload, target, code_mode, shared_code, is_active
) VALUES ($1, $2, '{}', 'null', 'shared_code', $3, TRUE)`, cpaTestWorkspaceID, "concurrent-offer", "CONCURRENT"); err != nil {
		t.Fatalf("create concurrent offer: %v", err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatalf("commit concurrent offer: %v", err)
	}

	err = <-result
	if err == nil || !strings.Contains(err.Error(), "import conflicts found") {
		t.Fatalf("concurrent fail_on_conflict error = %v", err)
	}

	offer, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "concurrent-offer")
	if err != nil || offer.SharedCode == nil || *offer.SharedCode != "CONCURRENT" {
		t.Fatalf("concurrent offer was changed by failed import: offer=%+v err=%v", offer, err)
	}
}

func TestCPA_AdminImportRejectsUnsupportedPackage(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	_, err := env.Service.Admin.Import(env.Context, cpaTestWorkspaceID, admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  "unknown.export.v1",
			Service: "cpa",
		},
	})
	if err == nil {
		t.Fatal("import must reject an unsupported package format")
	}
}

func TestCPA_AdminImportRejectsInvalidOfferBeforeWrite(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	_, err := env.Service.Admin.Import(env.Context, cpaImportWorkspaceID, admin.ImportRequest{
		Package: admin.ExportPackage{
			Format:  repository.ExportFormat,
			Service: "cpa",
			Offers: []admin.ExportOffer{
				{
					ID:         "valid-offer",
					Payload:    json.RawMessage(`{}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("VALID"),
				},
				{
					ID:       "invalid-offer",
					Payload:  json.RawMessage(`{}`),
					CodeMode: repository.CodeModeShared,
				},
			},
		},
	})
	if serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
		t.Fatalf("invalid import error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
	}
	var validationErr *repository.ImportValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("import error must expose ImportValidationError: %v", err)
	}
	if validationErr.OfferIndex != 1 || validationErr.Field != "shared_code" {
		t.Fatalf("invalid import context = %+v, want offer[1].shared_code", validationErr)
	}
	if _, getErr := env.Service.Admin.GetOffer(env.Context, cpaImportWorkspaceID, "valid-offer"); getErr == nil {
		t.Fatal("invalid import must not write offers before validation completes")
	}
}

func TestCPA_AdminImportRejectsInvalidNestedDataBeforeWrite(t *testing.T) {
	tests := []struct {
		name        string
		offers      []admin.ExportOffer
		wantField   string
		absentOffer string
	}{
		{
			name: "empty localization title",
			offers: []admin.ExportOffer{
				cpaTestExportOffer("valid-first"),
				{
					ID:         "invalid-localization",
					Payload:    json.RawMessage(`{}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("INVALID-LOCALIZATION"),
					Localization: map[string]admin.ExportText{
						"ru": {Description: "Missing title"},
					},
				},
			},
			wantField:   "localizations.ru.title",
			absentOffer: "valid-first",
		},
		{
			name: "invalid reward quantity",
			offers: []admin.ExportOffer{
				cpaTestExportOffer("valid-first"),
				{
					ID:         "invalid-reward",
					Payload:    json.RawMessage(`{}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("INVALID-REWARD"),
					Rewards: []admin.ExportReward{
						{Key: "stars", Quantity: 0},
					},
				},
			},
			wantField:   "rewards[0].quantity",
			absentOffer: "valid-first",
		},
		{
			name: "duplicate offer id",
			offers: []admin.ExportOffer{
				cpaTestExportOffer("duplicate-offer"),
				cpaTestExportOffer("duplicate-offer"),
			},
			wantField:   "id",
			absentOffer: "duplicate-offer",
		},
		{
			name: "duplicate reward key",
			offers: []admin.ExportOffer{
				{
					ID:         "duplicate-reward",
					Payload:    json.RawMessage(`{}`),
					CodeMode:   repository.CodeModeShared,
					SharedCode: stringPointer("DUPLICATE-REWARD"),
					Rewards: []admin.ExportReward{
						{Key: "stars", Quantity: 1},
						{Key: "stars", Quantity: 2},
					},
				},
			},
			wantField:   "rewards[1].key",
			absentOffer: "duplicate-reward",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := newCPATestEnvironment(t, testCPAOptions())
			_, err := env.Service.Admin.Import(env.Context, cpaImportWorkspaceID, admin.ImportRequest{
				Package: admin.ExportPackage{
					Format:  repository.ExportFormat,
					Service: "cpa",
					Offers:  test.offers,
				},
			})
			if serviceerrors.CodeOf(err) != serviceerrors.CodeInvalidFields {
				t.Fatalf("invalid import error code = %q, want %q; err=%v", serviceerrors.CodeOf(err), serviceerrors.CodeInvalidFields, err)
			}
			var validationErr *repository.ImportValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("import error must expose ImportValidationError: %v", err)
			}
			if validationErr.Field != test.wantField {
				t.Fatalf("invalid import field = %q, want %q", validationErr.Field, test.wantField)
			}
			if _, getErr := env.Service.Admin.GetOffer(env.Context, cpaImportWorkspaceID, test.absentOffer); getErr == nil {
				t.Fatal("invalid import must not write any offer before validation completes")
			}
		})
	}
}

func TestCPA_AdminListAssignmentEventsFiltersByEventType(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "event-filter-offer", true)
	identity := cpaTestIdentity("event-filter-user")

	if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    "event-filter-offer",
	}); err != nil {
		t.Fatalf("issue code: %v", err)
	}
	if _, err := env.Service.Admin.Complete(env.Context, admin.CompleteParams{
		Identity: identity,
		CPAID:    "event-filter-offer",
	}); err != nil {
		t.Fatalf("complete assignment: %v", err)
	}

	events, err := env.Service.Admin.ListAssignmentEvents(env.Context, admin.AssignmentEventListParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       "event-filter-offer",
		EventType:   cpa.AssignmentEventTypeCompleted,
	})
	if err != nil {
		t.Fatalf("list completed events: %v", err)
	}
	if len(events) != 1 || events[0].EventType != cpa.AssignmentEventTypeCompleted {
		t.Fatalf("completed events = %+v, want one completed event", events)
	}
}

func TestCPA_AdminImportBatchesPackageBeyondPostgreSQLParameterLimit(t *testing.T) {
	const offerCount = 5500
	env := newCPATestEnvironment(t, testCPAOptions())
	pkg := admin.ExportPackage{
		Format:  repository.ExportFormat,
		Service: "cpa",
		Offers:  make([]admin.ExportOffer, 0, offerCount),
	}
	for index := 0; index < offerCount; index++ {
		pkg.Offers = append(pkg.Offers, admin.ExportOffer{
			ID:         fmt.Sprintf("large-offer-%05d", index),
			Payload:    json.RawMessage(`{}`),
			CodeMode:   repository.CodeModeShared,
			SharedCode: stringPointer(fmt.Sprintf("LARGE-%05d", index)),
			IsActive:   true,
		})
	}

	result, err := env.Service.Admin.Import(env.Context, cpaImportWorkspaceID, admin.ImportRequest{
		Package:          pkg,
		ConflictStrategy: repository.ImportConflictUpdate,
	})
	if err != nil {
		t.Fatalf("import %d offers: %v", offerCount, err)
	}
	if result.Imported.Offers != offerCount {
		t.Fatalf("imported offers = %d, want %d", result.Imported.Offers, offerCount)
	}
	exported, err := env.Service.Admin.Export(env.Context, cpaImportWorkspaceID, admin.ExportRequest{})
	if err != nil {
		t.Fatalf("export imported offers: %v", err)
	}
	if len(exported.Offers) != offerCount {
		t.Fatalf("exported offers = %d, want %d", len(exported.Offers), offerCount)
	}
}

func TestCPA_WriteSucceedsWhenCacheInvalidationFails(t *testing.T) {
	cache := &failingCPAVersionCache{cpaTestCache: newCPATestCache()}
	var (
		mu         sync.Mutex
		diagnostic error
	)
	env := newCPATestEnvironment(t, cpa.Options{
		Cache:        cache,
		CacheEnabled: true,
		OnCacheInvalidationError: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			diagnostic = err
		},
	})

	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          "cache-failure-offer",
		Payload:     json.RawMessage(`{}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("CACHE-FAILURE"),
		IsActive:    true,
	}); err != nil {
		t.Fatalf("write must not fail because cache invalidation failed: %v", err)
	}

	mu.Lock()
	err := diagnostic
	mu.Unlock()
	if err == nil {
		t.Fatal("cache invalidation failure must be reported through the diagnostic callback")
	}
	if _, err := env.Service.Admin.GetOffer(env.Context, cpaTestWorkspaceID, "cache-failure-offer"); err != nil {
		t.Fatalf("offer write was not committed: %v", err)
	}
}

func TestCPA_CallbackDeliversIssuedAssignment(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	upsertSharedOffer(t, env, "callback_offer", true)
	if _, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: cpaTestIdentity("callback-user"),
		CPAID:    "callback_offer",
	}); err != nil {
		t.Fatalf("issue callback offer: %v", err)
	}

	callbackCtx, cancel := context.WithTimeout(env.Context, 5*time.Second)
	defer cancel()
	err := env.Service.OnCallback(callbackCtx, func(value cpa.Context) error {
		if value.Issued == nil || value.Issued.CPAID != "callback_offer" {
			return errors.New("issued callback payload was not delivered")
		}
		if err := value.Successful(); err != nil {
			return err
		}
		cancel()
		return nil
	},
		cpa.WithCallbackWorkerID("cpa-test-worker"),
		cpa.WithCallbackBatchSize(10),
		cpa.WithCallbackLeaseTimeout(time.Second),
		cpa.WithCallbackIdleDelay(10*time.Millisecond),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("callback worker error = %v, want context canceled", err)
	}
}

func TestCPA_RunBlocksUntilContextCanceled(t *testing.T) {
	env := newCPATestEnvironment(t, testCPAOptions())
	service := cpa.New(cpa.DatabaseParams{
		User:     cpaTestPGUser,
		Password: cpaTestPGPassword,
		Database: env.Name,
		Host:     cpaTestPGHost,
		Port:     cpaTestPGPort,
		Options:  testCPAOptions(),
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
			t.Fatal("CPA service did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := service.Run(context.Background()); !errors.Is(err, cpa.ErrServiceRunning) {
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
		t.Fatal("CPA Run did not stop after cancellation")
	}
}

func TestCPA_IsReadyReflectsInitialization(t *testing.T) {
	if cpa.New(cpa.DatabaseParams{}).IsReady() {
		t.Fatal("service without a database must not be ready")
	}
	env := newCPATestEnvironment(t, testCPAOptions())
	if !env.Service.IsReady() {
		t.Fatal("service created from a database must be ready")
	}
}

func newCPATestEnvironment(tb testing.TB, options cpa.Options) cpaTestEnvironment {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	tb.Cleanup(cancel)

	adminDB, err := openCPATestPostgres("postgres")
	if err != nil {
		tb.Fatalf("open postgres admin connection: %v", err)
	}
	tb.Cleanup(func() { _ = adminDB.Close() })

	database := fmt.Sprintf("cpa_test_%d", cpaTestDatabaseSequence.Add(1))
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE "+database); err != nil {
		tb.Fatalf("create test database: %v", err)
	}
	tb.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", database)
		_, _ = adminDB.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+database)
	})

	appDB, err := openCPATestPostgres(database)
	if err != nil {
		tb.Fatalf("open test database: %v", err)
	}
	client, err := sqlwrap.New(appDB)
	if err != nil {
		_ = appDB.Close()
		tb.Fatalf("create bootstrap client: %v", err)
	}
	tb.Cleanup(func() { _ = client.Close() })

	bootstrap := repository.New(client)
	tb.Cleanup(func() { _ = bootstrap.Close() })
	if err := bootstrap.Bootstrap(ctx); err != nil {
		tb.Fatalf("bootstrap CPA schema: %v", err)
	}

	service, err := cpa.NewWithDatabase(ctx, appDB, options)
	if err != nil {
		tb.Fatalf("create CPA service: %v", err)
	}
	tb.Cleanup(func() { _ = service.Close() })
	return cpaTestEnvironment{
		Context:  ctx,
		Database: appDB,
		Name:     database,
		Service:  service,
	}
}

func newCPAAdditionalNode(tb testing.TB, env cpaTestEnvironment, options cpa.Options) *cpa.CPA {
	tb.Helper()
	service, err := cpa.NewWithDatabase(env.Context, env.Database, options)
	if err != nil {
		tb.Fatalf("create additional CPA node: %v", err)
	}
	tb.Cleanup(func() { _ = service.Close() })
	return service
}

func openCPATestPostgres(database string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cpaTestPGHost,
		cpaTestPGPort,
		cpaTestPGUser,
		cpaTestPGPassword,
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

func testCPAOptions() cpa.Options {
	return cpa.Options{CacheEnabled: true}
}

func cpaTestIdentity(platformUserID string) user.Identity {
	return user.Identity{
		WorkspaceID:    cpaTestWorkspaceID,
		AppID:          100,
		PlatformID:     200,
		PlatformUserID: platformUserID,
	}
}

func upsertSharedOffer(tb testing.TB, env cpaTestEnvironment, id string, active bool) {
	tb.Helper()
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          id,
		Payload:     json.RawMessage(`{"kind":"offer"}`),
		CodeMode:    repository.CodeModeShared,
		SharedCode:  stringPointer("SHARED-" + id),
		IsActive:    active,
	}); err != nil {
		tb.Fatalf("upsert shared offer %q: %v", id, err)
	}
}

func cpaTestExportOffer(id string) admin.ExportOffer {
	return admin.ExportOffer{
		ID:         id,
		Payload:    json.RawMessage(`{}`),
		CodeMode:   repository.CodeModeShared,
		SharedCode: stringPointer("EXPORT-" + id),
	}
}

func upsertPoolOffer(tb testing.TB, env cpaTestEnvironment, id string) {
	tb.Helper()
	pool := repository.CodeSourcePool
	if err := env.Service.Admin.UpsertOffer(env.Context, admin.UpsertOfferParams{
		WorkspaceID: cpaTestWorkspaceID,
		ID:          id,
		Payload:     json.RawMessage(`{"kind":"pool"}`),
		CodeMode:    repository.CodeModePersonal,
		CodeSource:  &pool,
		IsActive:    true,
	}); err != nil {
		tb.Fatalf("upsert pool offer %q: %v", id, err)
	}
}

func addPoolCode(tb testing.TB, env cpaTestEnvironment, cpaID, code string) {
	tb.Helper()
	added, err := env.Service.Admin.AddCodes(env.Context, admin.AddCodesParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       cpaID,
		Codes:       []string{code},
	})
	if err != nil || added != 1 {
		tb.Fatalf("add pool code: added=%d err=%v", added, err)
	}
}

func issueCode(tb testing.TB, env cpaTestEnvironment, identity user.Identity, cpaID string) user.GetCodeResult {
	tb.Helper()
	result, err := env.Service.User.GetCode(env.Context, user.GetCodeParams{
		Identity: identity,
		CPAID:    cpaID,
	})
	if err != nil {
		tb.Fatalf("issue code: %v", err)
	}
	return result
}

func assertAssignmentIsVisible(
	tb testing.TB,
	env cpaTestEnvironment,
	identity user.Identity,
	cpaID string,
	assignmentID uint64,
) {
	tb.Helper()
	status, err := env.Service.User.GetStatus(env.Context, user.GetStatusParams{
		Identity: identity,
		CPAID:    cpaID,
	})
	if err != nil || status == nil || status.ID != assignmentID {
		tb.Fatalf("assignment status disappeared: status=%+v err=%v", status, err)
	}
	offers, err := env.Service.User.ListActive(env.Context, user.ListActiveParams{
		Identity: identity,
		Locale:   "ru",
	})
	if err != nil {
		tb.Fatalf("list active offers: %v", err)
	}
	for _, offer := range offers {
		if offer.ID == cpaID && offer.Assignment != nil && offer.Assignment.ID == assignmentID {
			return
		}
	}
	tb.Fatalf("assignment %d is missing from active offers", assignmentID)
}

func upsertLocalization(tb testing.TB, env cpaTestEnvironment, cpaID, locale, title string) {
	tb.Helper()
	if err := env.Service.Admin.UpsertLocalization(env.Context, admin.UpsertLocalizationParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       cpaID,
		Locale:      locale,
		Title:       title,
		Description: title + " description",
	}); err != nil {
		tb.Fatalf("upsert localization: %v", err)
	}
}

func upsertReward(tb testing.TB, env cpaTestEnvironment, cpaID, key string, quantity int64, scale uint16) {
	tb.Helper()
	if err := env.Service.Admin.UpsertReward(env.Context, admin.UpsertRewardParams{
		WorkspaceID: cpaTestWorkspaceID,
		CPAID:       cpaID,
		Key:         key,
		Quantity:    quantity,
		Scale:       scale,
	}); err != nil {
		tb.Fatalf("upsert reward: %v", err)
	}
}

func stringPointer(value string) *string {
	return &value
}

func payloadKind(tb testing.TB, payload json.RawMessage) string {
	tb.Helper()
	var value struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		tb.Fatalf("decode offer payload: %v", err)
	}
	return value.Kind
}

func int16Pointer(value int16) *int16 {
	return &value
}

type cpaTestCacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type cpaTestCache struct {
	mu          sync.Mutex
	entries     map[string]cpaTestCacheEntry
	sets        int
	lastTTL     time.Duration
	dataSets    int
	dataLastTTL time.Duration
}

type failingCPAVersionCache struct {
	*cpaTestCache
}

func (c *failingCPAVersionCache) Set(key string, value []byte, expiration time.Duration) error {
	if strings.HasPrefix(key, "cache_version:") {
		return errors.New("cache version backend is unavailable")
	}
	return c.cpaTestCache.Set(key, value, expiration)
}

func newCPATestCache() *cpaTestCache {
	return &cpaTestCache{entries: make(map[string]cpaTestCacheEntry)}
}

func (c *cpaTestCache) GetWithTTL(key string) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, 0, nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return nil, 0, nil
	}
	return append([]byte(nil), entry.value...), time.Until(entry.expiresAt), nil
}

func (c *cpaTestCache) Set(key string, value []byte, expiration time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := cpaTestCacheEntry{value: append([]byte(nil), value...)}
	if expiration > 0 {
		entry.expiresAt = time.Now().Add(expiration)
	}
	c.entries[key] = entry
	c.sets++
	c.lastTTL = expiration
	if !strings.HasPrefix(key, "cache_version:") {
		c.dataSets++
		c.dataLastTTL = expiration
	}
	return nil
}

func (c *cpaTestCache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	return nil
}

func (c *cpaTestCache) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.entries)
	return nil
}

func (c *cpaTestCache) Close() error {
	return nil
}

func (c *cpaTestCache) SetCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sets
}

func (c *cpaTestCache) LastTTL() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastTTL
}

func (c *cpaTestCache) DataSetCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dataSets
}

func (c *cpaTestCache) DataLastTTL() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dataLastTTL
}
