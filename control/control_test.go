package control_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"github.com/elum2b/services/control"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/control/service/admin"
	"github.com/elum2b/services/control/service/internalapi"
	"github.com/elum2b/services/internal/testsupport"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	json "github.com/goccy/go-json"
	_ "github.com/jackc/pgx/v5/stdlib"
	"testing"
	"time"
)

const (
	controlTestHost     = "localhost"
	controlTestPort     = 5432
	controlTestUser     = "postgres"
	controlTestPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	controlTestDatabase = "control_test"
)

var controlTestSecretEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

func TestControlRunBlocksUntilContextCanceled(t *testing.T) {
	newControlTestService(t)
	service := control.New(control.DatabaseParams{
		User:     controlTestUser,
		Password: controlTestPassword,
		Database: controlTestDatabase,
		Host:     controlTestHost,
		Port:     controlTestPort,
		Options: control.Options{
			SecretEncryptionKey: controlTestSecretEncryptionKey,
		},
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
			t.Fatal("control service did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := service.Run(context.Background()); !errors.Is(err, control.ErrServiceRunning) {
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
		t.Fatal("control Run did not stop after cancellation")
	}
}

func TestControlWorkspaceAccessAndInvite(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	for _, accountID := range []string{"owner", "moderator", "member", "invitee"} {
		if _, err := service.Admin.CreateAccount(ctx, accountID, accountID); err != nil {
			t.Fatalf("create account %s: %v", accountID, err)
		}
	}
	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ID:      testsupport.WorkspaceID("workspace"),
		ActorID: "owner",
		Slug:    "workspace",
		Title:   "Workspace",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if allowed, err := service.Internal.CheckAccess(ctx, internalapi.AccessRequest{AccountID: "owner", WorkspaceID: workspace.ID, MethodKey: "unknown.method"}); err != nil || allowed {
		t.Fatalf("unregistered method must be denied: allowed=%v err=%v", allowed, err)
	}
	moderator, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{ActorID: "owner", ID: "moderator-role", WorkspaceID: workspace.ID, Code: "moderator", Title: "Moderator", Position: 10})
	if err != nil {
		t.Fatalf("create moderator role: %v", err)
	}
	member, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{ActorID: "owner", ID: "member-role", WorkspaceID: workspace.ID, Code: "member", Title: "Member", Position: 20})
	if err != nil {
		t.Fatalf("create member role: %v", err)
	}
	invite, token, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{ActorID: "owner", WorkspaceID: workspace.ID, RoleIDs: []string{member.ID}})
	if err != nil || invite.ID == "" || token == "" {
		t.Fatalf("create invite: invite=%#v token=%q err=%v", invite, token, err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "member", token); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if err := service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{ActorID: "owner", WorkspaceID: workspace.ID, AccountID: "invitee", RoleID: member.ID}); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("non-member must not receive role: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "moderator", token); err != nil {
		t.Fatalf("accept moderator invite: %v", err)
	}
	if err := service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{ActorID: "owner", WorkspaceID: workspace.ID, AccountID: "moderator", RoleID: moderator.ID}); err != nil {
		t.Fatalf("assign moderator role: %v", err)
	}
	if allowed, err := service.Internal.CheckAccess(ctx, internalapi.AccessRequest{AccountID: "moderator", WorkspaceID: workspace.ID, MethodKey: "control.role_member.set"}); err != nil || allowed {
		t.Fatalf("permission must be denied before grant: allowed=%v err=%v", allowed, err)
	}
	if err := service.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{ActorID: "owner", WorkspaceID: workspace.ID, RoleID: moderator.ID, MethodKey: "control.role_member.set", Enabled: true}); err != nil {
		t.Fatalf("set permission: %v", err)
	}
	allowed, err := service.Internal.CheckAccess(ctx, internalapi.AccessRequest{AccountID: "moderator", WorkspaceID: workspace.ID, MethodKey: "control.role_member.set"})
	if err != nil || !allowed {
		t.Fatalf("moderator access: allowed=%v err=%v", allowed, err)
	}
	authorizedMethods, err := service.Internal.GetAuthorizedMethods(ctx, "moderator", workspace.ID)
	if err != nil || len(authorizedMethods) != 1 || authorizedMethods[0].Key != "control.role_member.set" {
		t.Fatalf("authorized methods: methods=%#v err=%v", authorizedMethods, err)
	}
	if err := service.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{ActorID: "owner", WorkspaceID: workspace.ID, RoleID: moderator.ID, MethodKey: "control.role_member.set", Enabled: false}); err != nil {
		t.Fatalf("remove permission: %v", err)
	}
	authorizedMethods, err = service.Internal.GetAuthorizedMethods(ctx, "moderator", workspace.ID)
	if err != nil || len(authorizedMethods) != 0 {
		t.Fatalf("authorization cache invalidation: methods=%#v err=%v", authorizedMethods, err)
	}
	if err := service.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{ActorID: "owner", WorkspaceID: workspace.ID, RoleID: moderator.ID, MethodKey: "control.role_member.set", Enabled: true}); err != nil {
		t.Fatalf("restore permission: %v", err)
	}
	if err := service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{ActorID: "moderator", WorkspaceID: workspace.ID, AccountID: "member", RoleID: moderator.ID}); !errors.Is(err, repository.ErrRoleHierarchy) {
		t.Fatalf("moderator must not grant equal role: %v", err)
	}
	if err := service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{ActorID: "moderator", WorkspaceID: workspace.ID, AccountID: "member", RoleID: member.ID}); err != nil {
		t.Fatalf("moderator may grant lower role: %v", err)
	}
}

func TestControlAccessCacheVersionInvalidatesOtherNode(t *testing.T) {
	cache := testsupport.NewCache()
	options := control.Options{
		Cache:               cache,
		CacheEnabled:        true,
		CacheL1Delay:        time.Minute,
		CacheL2Delay:        time.Minute,
		SecretEncryptionKey: controlTestSecretEncryptionKey,
	}
	nodeA := newControlTestServiceWithOptions(t, options)
	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open second control node database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	nodeB, err := control.NewWithDatabase(context.Background(), db, options)
	if err != nil {
		t.Fatalf("create second control node: %v", err)
	}
	t.Cleanup(func() { _ = nodeB.Close() })

	ctx := context.Background()
	ownerID := "cache-owner"
	memberID := "cache-member"
	if _, err := nodeA.Admin.CreateAccount(ctx, ownerID, ownerID); err != nil {
		t.Fatalf("create cache owner: %v", err)
	}
	if _, err := nodeA.Admin.CreateAccount(ctx, memberID, memberID); err != nil {
		t.Fatalf("create cache member: %v", err)
	}
	workspace, err := nodeA.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ID:      testsupport.WorkspaceID("control-cache-workspace"),
		ActorID: ownerID,
		Slug:    "control-cache-workspace",
		Title:   "Control cache workspace",
	})
	if err != nil {
		t.Fatalf("create cache workspace: %v", err)
	}
	role, err := nodeA.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     ownerID,
		ID:          "control-cache-role",
		WorkspaceID: workspace.ID,
		Code:        "cache-role",
		Title:       "Cache role",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create cache role: %v", err)
	}
	invite, token, err := nodeA.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     ownerID,
		WorkspaceID: workspace.ID,
		RoleIDs:     []string{role.ID},
	})
	if err != nil || invite.ID == "" || token == "" {
		t.Fatalf("create cache invite: invite=%#v token=%q err=%v", invite, token, err)
	}
	if _, err := nodeA.Admin.AcceptInvite(ctx, memberID, token); err != nil {
		t.Fatalf("accept cache invite: %v", err)
	}

	request := internalapi.AccessRequest{
		AccountID:   memberID,
		WorkspaceID: workspace.ID,
		MethodKey:   "control.role.update",
	}
	allowed, err := nodeB.Internal.CheckAccess(ctx, request)
	if err != nil || allowed {
		t.Fatalf("access before grant allowed=%v err=%v", allowed, err)
	}
	if err := nodeA.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{
		ActorID:     ownerID,
		WorkspaceID: workspace.ID,
		RoleID:      role.ID,
		MethodKey:   request.MethodKey,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("grant cached permission: %v", err)
	}
	allowed, err = nodeB.Internal.CheckAccess(ctx, request)
	if err != nil || !allowed {
		t.Fatalf("access after cross-node grant allowed=%v err=%v", allowed, err)
	}
	if err := nodeA.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{
		ActorID:     ownerID,
		WorkspaceID: workspace.ID,
		RoleID:      role.ID,
		MethodKey:   request.MethodKey,
		Enabled:     false,
	}); err != nil {
		t.Fatalf("revoke cached permission: %v", err)
	}
	allowed, err = nodeB.Internal.CheckAccess(ctx, request)
	if err != nil || allowed {
		t.Fatalf("access after cross-node revoke allowed=%v err=%v", allowed, err)
	}
}

func TestControlAuthSessionAndIdentityLifecycle(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	initial, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:    "primary",
		Subject:     "auth-lifecycle",
		DisplayName: "Auth lifecycle",
		IP:          "127.0.0.1",
		UserAgent:   "control-test",
		BindToIP:    true,
	})
	if err != nil {
		t.Fatalf("complete initial auth: %v", err)
	}
	if initial.SessionToken == "" || initial.Account.ID == "" || initial.Session.ID == "" {
		t.Fatalf("incomplete auth result: %+v", initial)
	}

	account, err := service.Admin.GetAccount(ctx, initial.Account.ID)
	if err != nil || account.DisplayName != "Auth lifecycle" {
		t.Fatalf("get account: value=%+v err=%v", account, err)
	}
	validated, err := service.Admin.ValidateSession(ctx, initial.SessionToken, "127.0.0.1")
	if err != nil || validated.ID != initial.Session.ID {
		t.Fatalf("validate session: value=%+v err=%v", validated, err)
	}
	if _, err := service.Admin.ValidateSession(ctx, initial.SessionToken, "127.0.0.2"); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("bound session accepted another IP: %v", err)
	}
	if _, err := service.Admin.ValidateSession(ctx, "invalid-token", "127.0.0.1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("invalid token error = %v", err)
	}

	if err := service.Admin.BindIdentity(ctx, initial.Account.ID, admin.AuthIdentityParams{
		Provider:    "secondary",
		Subject:     "secondary-subject",
		DisplayName: "Secondary",
	}); err != nil {
		t.Fatalf("bind secondary identity: %v", err)
	}
	identities, err := service.Admin.ListIdentities(ctx, initial.Account.ID)
	if err != nil || len(identities) != 2 {
		t.Fatalf("list bound identities: values=%+v err=%v", identities, err)
	}
	if changed, err := service.Admin.UnbindIdentity(ctx, initial.Account.ID, "secondary"); err != nil || changed != 1 {
		t.Fatalf("unbind identity: changed=%d err=%v", changed, err)
	}

	second, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:  "primary",
		Subject:   "auth-lifecycle",
		IP:        "127.0.0.1",
		BindToIP:  true,
		ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	sessions, err := service.Admin.ListSessions(ctx, initial.Account.ID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("list sessions: values=%+v err=%v", sessions, err)
	}
	if changed, err := service.Admin.RevokeSession(
		ctx,
		initial.Account.ID,
		second.Session.ID,
	); err != nil || changed != 1 {
		t.Fatalf("revoke session: changed=%d err=%v", changed, err)
	}
	if _, err := service.Admin.ValidateSession(ctx, second.SessionToken, "127.0.0.1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("revoked session validation error = %v", err)
	}

	third, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider: "primary",
		Subject:  "auth-lifecycle",
	})
	if err != nil {
		t.Fatalf("create third session: %v", err)
	}
	if changed, err := service.Admin.RevokeAllSessions(
		ctx,
		initial.Account.ID,
		initial.Session.ID,
	); err != nil || changed != 1 {
		t.Fatalf("revoke all sessions: changed=%d err=%v", changed, err)
	}
	if _, err := service.Admin.ValidateSession(ctx, third.SessionToken, ""); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("bulk-revoked session validation error = %v", err)
	}
	if _, err := service.Admin.ValidateSession(ctx, initial.SessionToken, "127.0.0.1"); err != nil {
		t.Fatalf("excepted session was revoked: %v", err)
	}

	setup, err := service.Admin.BeginTwoFactor(ctx, initial.Account.ID, "Control Test")
	if err != nil {
		t.Fatalf("begin two factor: %v", err)
	}
	recoveryCodes, err := service.Admin.ConfirmTwoFactor(
		ctx,
		initial.Account.ID,
		controlTestTOTP(setup.Secret, time.Now()),
	)
	if err != nil || len(recoveryCodes) == 0 {
		t.Fatalf("confirm two factor: codes=%+v err=%v", recoveryCodes, err)
	}
	if changed, err := service.Admin.DisableTwoFactor(
		ctx,
		initial.Account.ID,
		recoveryCodes[0],
	); err != nil || changed != 1 {
		t.Fatalf("disable two factor: changed=%d err=%v", changed, err)
	}

}

func TestControlWorkspaceAdministrationLifecycle(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	for _, accountID := range []string{"workspace-owner", "workspace-member"} {
		if _, err := service.Admin.CreateAccount(ctx, accountID, accountID); err != nil {
			t.Fatalf("create account %s: %v", accountID, err)
		}
	}
	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: "workspace-owner",
		ID:      testsupport.WorkspaceID("administration-lifecycle"),
		Slug:    "administration-lifecycle",
		Title:   "Administration lifecycle",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	loadedWorkspace, err := service.Admin.GetWorkspace(ctx, workspace.ID)
	if err != nil || loadedWorkspace.ID != workspace.ID {
		t.Fatalf("get workspace: value=%+v err=%v", loadedWorkspace, err)
	}
	workspaces, err := service.Admin.ListWorkspaces(
		ctx,
		"workspace-owner",
		admin.Page{Limit: 10},
	)
	if err != nil || len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("list workspaces: values=%+v err=%v", workspaces, err)
	}
	if _, err := service.Admin.GetWorkspace(ctx, "invalid"); err == nil {
		t.Fatal("invalid workspace ID was accepted")
	}

	memberRole, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     "workspace-owner",
		ID:          "administration-member-role",
		WorkspaceID: workspace.ID,
		Code:        "administration_member",
		Title:       "Member",
		Position:    20,
	})
	if err != nil {
		t.Fatalf("create member role: %v", err)
	}
	deleteRole, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     "workspace-owner",
		ID:          "administration-delete-role",
		WorkspaceID: workspace.ID,
		Code:        "administration_delete",
		Title:       "Delete me",
		Position:    30,
	})
	if err != nil {
		t.Fatalf("create removable role: %v", err)
	}
	roles, err := service.Admin.ListRoles(ctx, workspace.ID)
	if err != nil || len(roles) != 3 {
		t.Fatalf("list roles: values=%+v err=%v", roles, err)
	}
	if changed, err := service.Admin.UpdateRole(ctx, admin.UpdateRoleParams{
		ActorID:     "workspace-owner",
		ID:          memberRole.ID,
		WorkspaceID: workspace.ID,
		Title:       "Updated member",
		Description: "Updated role",
		Position:    20,
	}); err != nil || changed != 1 {
		t.Fatalf("update role: changed=%d err=%v", changed, err)
	}

	invite, token, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     "workspace-owner",
		WorkspaceID: workspace.ID,
		RoleIDs:     []string{memberRole.ID},
	})
	if err != nil {
		t.Fatalf("create member invite: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "workspace-member", token); err != nil {
		t.Fatalf("accept member invite: %v", err)
	}
	members, err := service.Admin.ListMembers(ctx, workspace.ID, admin.Page{Limit: 10})
	if err != nil || len(members) != 2 {
		t.Fatalf("list members: values=%+v err=%v", members, err)
	}

	if err := service.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{
		ActorID:     "workspace-owner",
		WorkspaceID: workspace.ID,
		RoleID:      memberRole.ID,
		MethodKey:   "control.workspace.update",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("set role permission: %v", err)
	}
	permissions, err := service.Admin.ListRolePermissions(ctx, workspace.ID, memberRole.ID)
	if err != nil || len(permissions) != 1 || permissions[0] != "control.workspace.update" {
		t.Fatalf("list role permissions: values=%+v err=%v", permissions, err)
	}
	if changed, err := service.Admin.ClearRolePermissions(
		ctx,
		"workspace-owner",
		workspace.ID,
		memberRole.ID,
	); err != nil || changed != 1 {
		t.Fatalf("clear role permissions: changed=%d err=%v", changed, err)
	}

	methods, err := service.Admin.ListMethods(ctx)
	if err != nil || len(methods) == 0 {
		t.Fatalf("list methods: values=%+v err=%v", methods, err)
	}
	access, err := service.Admin.ListAccess(ctx, "ru")
	if err != nil || len(access) == 0 || len(access[0].Groups) == 0 {
		t.Fatalf("list access catalog: values=%+v err=%v", access, err)
	}

	if changed, err := service.Admin.RemoveRoleMember(ctx, admin.SetRoleMemberParams{
		ActorID:     "workspace-owner",
		WorkspaceID: workspace.ID,
		AccountID:   "workspace-member",
		RoleID:      memberRole.ID,
	}); err != nil || changed != 1 {
		t.Fatalf("remove role member: changed=%d err=%v", changed, err)
	}
	if changed, err := service.Admin.RemoveMember(
		ctx,
		"workspace-owner",
		workspace.ID,
		"workspace-member",
	); err != nil || changed != 1 {
		t.Fatalf("remove member: changed=%d err=%v", changed, err)
	}

	revokable, _, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     "workspace-owner",
		WorkspaceID: workspace.ID,
	})
	if err != nil {
		t.Fatalf("create revokable invite: %v", err)
	}
	if changed, err := service.Admin.RevokeInvite(
		ctx,
		"workspace-owner",
		workspace.ID,
		revokable.ID,
	); err != nil || changed != 1 {
		t.Fatalf("revoke invite: changed=%d err=%v", changed, err)
	}
	if invite.ID == revokable.ID {
		t.Fatal("invites unexpectedly share an ID")
	}
	if changed, err := service.Admin.DeleteRole(
		ctx,
		"workspace-owner",
		workspace.ID,
		deleteRole.ID,
	); err != nil || changed != 1 {
		t.Fatalf("delete role: changed=%d err=%v", changed, err)
	}

}

func TestControlRepeatedInviteAcceptanceDoesNotConsumeUse(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	for _, accountID := range []string{"invite-owner", "first-invitee", "second-invitee"} {
		if _, err := service.Admin.CreateAccount(ctx, accountID, accountID); err != nil {
			t.Fatalf("create account %s: %v", accountID, err)
		}
	}

	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: "invite-owner",
		ID:      testsupport.WorkspaceID("control-repeat-invite"),
		Slug:    "control-repeat-invite",
		Title:   "Repeated invite",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	role, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     "invite-owner",
		ID:          "repeat-invite-role",
		WorkspaceID: workspace.ID,
		Code:        "repeat_invite",
		Title:       "Repeat invite",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	_, token, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     "invite-owner",
		WorkspaceID: workspace.ID,
		RoleIDs:     []string{role.ID},
		MaxUses:     uint32Pointer(2),
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "first-invitee", token); err != nil {
		t.Fatalf("first acceptance: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "first-invitee", token); err != nil {
		t.Fatalf("repeated acceptance: %v", err)
	}

	invites, err := service.Admin.ListInvites(ctx, workspace.ID, admin.Page{Limit: 10})
	if err != nil || len(invites) != 1 {
		t.Fatalf("list invites: items=%+v err=%v", invites, err)
	}
	if invites[0].UsedCount == nil || *invites[0].UsedCount != 1 {
		t.Fatalf("used count after repeated acceptance = %+v", invites[0].UsedCount)
	}

	if _, err := service.Admin.AcceptInvite(ctx, "second-invitee", token); err != nil {
		t.Fatalf("second unique account must retain invite use: %v", err)
	}
	invites, err = service.Admin.ListInvites(ctx, workspace.ID, admin.Page{Limit: 10})
	if err != nil || len(invites) != 1 {
		t.Fatalf("list exhausted invite: items=%+v err=%v", invites, err)
	}
	if invites[0].UsedCount == nil || *invites[0].UsedCount != 2 {
		t.Fatalf("used count after unique acceptances = %+v", invites[0].UsedCount)
	}
}

func TestControlAcceptedInviteRemainsIdempotentAfterExpiration(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	for _, accountID := range []string{"expired-invite-owner", "accepted-invitee", "fresh-invitee"} {
		if _, err := service.Admin.CreateAccount(ctx, accountID, accountID); err != nil {
			t.Fatalf("create account %s: %v", accountID, err)
		}
	}

	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: "expired-invite-owner",
		ID:      testsupport.WorkspaceID("control-expired-invite"),
		Slug:    "control-expired-invite",
		Title:   "Expired invite",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	invite, token, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     "expired-invite-owner",
		WorkspaceID: workspace.ID,
		MaxUses:     uint32Pointer(2),
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "accepted-invitee", token); err != nil {
		t.Fatalf("accept active invite: %v", err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open control database: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(
		ctx,
		"UPDATE control_workspace_invite SET expires_at = now() - interval '1 second' WHERE id = $1",
		invite.ID,
	); err != nil {
		t.Fatalf("expire invite: %v", err)
	}

	if _, err := service.Admin.AcceptInvite(ctx, "accepted-invitee", token); err != nil {
		t.Fatalf("repeat accepted invite after expiration: %v", err)
	}
	if _, err := service.Admin.AcceptInvite(ctx, "fresh-invitee", token); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("fresh account expired invite error = %v", err)
	}
}

func TestControlRejectsInvalidInviteMaxUses(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()

	if _, err := service.Admin.CreateAccount(ctx, "invite-limit-owner", "owner"); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ID:      testsupport.WorkspaceID("invite-limit-workspace"),
		ActorID: "invite-limit-owner",
		Slug:    "invite-limit-workspace",
		Title:   "Invite limits",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	for _, maxUses := range []uint32{0, math.MaxUint32} {
		_, _, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
			ActorID:     "invite-limit-owner",
			WorkspaceID: workspace.ID,
			MaxUses:     &maxUses,
		})
		if !errors.Is(err, repository.ErrInviteMaxUses) {
			t.Fatalf("max_uses=%d error = %v", maxUses, err)
		}
	}
}

func TestControlManualAuditIsInternalOnly(t *testing.T) {
	if _, exposed := reflect.TypeOf((*admin.Admin)(nil)).MethodByName("AppendAudit"); exposed {
		t.Fatal("Admin must not expose manual audit writes")
	}

	service := newControlTestService(t)
	ctx := context.Background()
	if _, err := service.Admin.CreateAccount(ctx, "audit-owner", "owner"); err != nil {
		t.Fatalf("create owner: %v", err)
	}
	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ID:      testsupport.WorkspaceID("audit-workspace"),
		ActorID: "audit-owner",
		Slug:    "audit-workspace",
		Title:   "Audit",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if err := service.Internal.AppendAudit(ctx, internalapi.AuditEventParams{
		WorkspaceID: workspace.ID,
		ActorID:     "audit-owner",
		MethodKey:   "tasks.task.create",
		TargetType:  "task",
		TargetID:    "daily.message",
		Result:      "succeeded",
	}); err != nil {
		t.Fatalf("append internal audit: %v", err)
	}

	events, err := service.Admin.ListAudit(ctx, workspace.ID, admin.Page{Limit: 100})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, event := range events {
		if event.MethodKey == "tasks.task.create" && event.TargetID == "daily.message" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("internal audit event not found: %+v", events)
	}
}

func TestControlAdminMutationWritesAuditInTransaction(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()

	if _, err := service.Admin.CreateAccount(ctx, "audit-owner", "Audit owner"); err != nil {
		t.Fatalf("create account: %v", err)
	}

	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ID:      testsupport.WorkspaceID("audit-workspace"),
		ActorID: "audit-owner",
		Slug:    "audit-workspace",
		Title:   "Audit workspace",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := service.Admin.UpdateWorkspace(ctx, admin.UpdateWorkspaceParams{
		ActorID:     "audit-owner",
		WorkspaceID: workspace.ID,
		Slug:        "audit-workspace",
		Title:       "Updated audit workspace",
		Status:      "active",
	}); err != nil {
		t.Fatalf("update workspace: %v", err)
	}

	events, err := service.Admin.ListAudit(ctx, workspace.ID, admin.Page{Limit: 20})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}

	keys := make(map[string]bool, len(events))
	for _, event := range events {
		keys[event.MethodKey] = true
	}
	if !keys["control.workspace.create"] || !keys["control.workspace.update"] {
		t.Fatalf("automatic audit events = %#v", events)
	}
}

func TestControlTwoFactorSecretIsEncrypted(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	accountID := "two-factor-account"

	if _, err := service.Admin.CreateAccount(ctx, accountID, "Two Factor"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	setup, err := service.Admin.BeginTwoFactor(ctx, accountID, "Control Test")
	if err != nil {
		t.Fatalf("begin two factor: %v", err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open verification database: %v", err)
	}
	defer db.Close()

	var stored string
	if err := db.QueryRowContext(
		ctx,
		"SELECT secret FROM control_two_factor WHERE account_id = $1",
		accountID,
	).Scan(&stored); err != nil {
		t.Fatalf("read stored two factor secret: %v", err)
	}
	if stored == setup.Secret || !strings.HasPrefix(stored, "v1:") {
		t.Fatalf("two factor secret is not encrypted: %q", stored)
	}

	if _, err := service.Admin.ConfirmTwoFactor(ctx, accountID, controlTestTOTP(setup.Secret, time.Now())); err != nil {
		t.Fatalf("confirm encrypted two factor: %v", err)
	}
}

func TestControlTwoFactorPreservesRequestedSessionExpiration(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	provider := "two-factor-expiration"
	subject := "two-factor-expiration-account"

	initial, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:    provider,
		Subject:     subject,
		DisplayName: "Two Factor Expiration",
	})
	if err != nil {
		t.Fatalf("create authenticated account: %v", err)
	}

	setup, err := service.Admin.BeginTwoFactor(ctx, initial.Account.ID, "Control Test")
	if err != nil {
		t.Fatalf("begin two factor: %v", err)
	}
	if _, err := service.Admin.ConfirmTwoFactor(
		ctx,
		initial.Account.ID,
		controlTestTOTP(setup.Secret, time.Now()),
	); err != nil {
		t.Fatalf("confirm two factor: %v", err)
	}

	wantedExpiration := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	challenge, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:    provider,
		Subject:     subject,
		DisplayName: "Two Factor Expiration",
		ExpiresAt:   wantedExpiration,
	})
	if err != nil {
		t.Fatalf("create two factor challenge: %v", err)
	}
	if !challenge.TwoFactorRequired || challenge.TwoFactorChallenge == "" {
		t.Fatalf("expected two factor challenge: %+v", challenge)
	}

	completed, err := service.Admin.CompleteTwoFactor(
		ctx,
		challenge.TwoFactorChallenge,
		controlTestTOTP(setup.Secret, time.Now()),
		"",
	)
	if err != nil {
		t.Fatalf("complete two factor: %v", err)
	}
	if !completed.Session.ExpiresAt.Equal(wantedExpiration) {
		t.Fatalf(
			"session expiration = %s, want %s",
			completed.Session.ExpiresAt,
			wantedExpiration,
		)
	}
}

func TestControlListIdentitiesDoesNotExposePrivatePayload(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	payload := json.RawMessage(`{"access_token":"private-token","proof":"private-proof"}`)

	auth, err := service.Admin.CompleteAuth(ctx, admin.AuthIdentityParams{
		Provider:    "test-provider",
		Subject:     "test-subject",
		DisplayName: "Test account",
		Payload:     payload,
	})
	if err != nil {
		t.Fatalf("complete auth: %v", err)
	}

	identities, err := service.Admin.ListIdentities(ctx, auth.Account.ID)
	if err != nil || len(identities) != 1 {
		t.Fatalf("list identities: identities=%+v err=%v", identities, err)
	}
	raw, err := json.Marshal(identities)
	if err != nil {
		t.Fatalf("marshal identities: %v", err)
	}
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "private-proof") {
		t.Fatalf("private identity payload leaked: %s", raw)
	}
}

func TestControlMutationRechecksAuthorizationAfterLock(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	for _, accountID := range []string{"race-owner", "race-actor", "race-target"} {
		if _, err := service.Admin.CreateAccount(ctx, accountID, accountID); err != nil {
			t.Fatalf("create account %s: %v", accountID, err)
		}
	}

	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: "race-owner",
		ID:      testsupport.WorkspaceID("control-authorization-race"),
		Slug:    "control-authorization-race",
		Title:   "Authorization race",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	actorRole, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     "race-owner",
		ID:          "race-actor-role",
		WorkspaceID: workspace.ID,
		Code:        "race_actor",
		Title:       "Race actor",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create actor role: %v", err)
	}
	targetRole, err := service.Admin.CreateRole(ctx, admin.CreateRoleParams{
		ActorID:     "race-owner",
		ID:          "race-target-role",
		WorkspaceID: workspace.ID,
		Code:        "race_target",
		Title:       "Race target",
		Position:    20,
	})
	if err != nil {
		t.Fatalf("create target role: %v", err)
	}

	invite, token, err := service.Admin.CreateInvite(ctx, admin.CreateInviteParams{
		ActorID:     "race-owner",
		WorkspaceID: workspace.ID,
		RoleIDs:     []string{targetRole.ID},
		MaxUses:     uint32Pointer(2),
	})
	if err != nil || invite.ID == "" {
		t.Fatalf("create member invite: %v", err)
	}
	for _, accountID := range []string{"race-actor", "race-target"} {
		if _, err := service.Admin.AcceptInvite(ctx, accountID, token); err != nil {
			t.Fatalf("accept invite for %s: %v", accountID, err)
		}
	}
	if err := service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{
		ActorID:     "race-owner",
		WorkspaceID: workspace.ID,
		AccountID:   "race-actor",
		RoleID:      actorRole.ID,
	}); err != nil {
		t.Fatalf("assign actor role: %v", err)
	}
	if err := service.Admin.SetRolePermission(ctx, admin.SetRolePermissionParams{
		ActorID:     "race-owner",
		WorkspaceID: workspace.ID,
		RoleID:      actorRole.ID,
		MethodKey:   "control.role_member.set",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("grant actor permission: %v", err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open race database: %v", err)
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin authorization transaction: %v", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended('control:authorization:' || $1::text, 0))",
		workspace.ID,
	); err != nil {
		t.Fatalf("lock workspace authorization: %v", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM control_role_member WHERE role_id = $1 AND account_id = $2",
		actorRole.ID,
		"race-actor",
	); err != nil {
		t.Fatalf("remove actor role: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	result := make(chan error, 1)
	go func() {
		defer wg.Done()
		result <- service.Admin.SetRoleMember(ctx, admin.SetRoleMemberParams{
			ActorID:     "race-actor",
			WorkspaceID: workspace.ID,
			AccountID:   "race-target",
			RoleID:      targetRole.ID,
		})
	}()

	select {
	case err := <-result:
		t.Fatalf("mutation returned before authorization lock commit: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit actor demotion: %v", err)
	}
	wg.Wait()
	err = <-result
	if !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("mutation after actor demotion error = %v", err)
	}
}

func TestControlRegisterManifestIsAtomic(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()

	if err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
		{
			Key:      "atomic.owner",
			Service:  "owner-a",
			GroupKey: "test",
		},
	}); err != nil {
		t.Fatalf("register owner method: %v", err)
	}

	err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
		{
			Key:      "atomic.must_rollback",
			Service:  "owner-b",
			GroupKey: "test",
		},
		{
			Key:      "atomic.owner",
			Service:  "owner-b",
			GroupKey: "test",
		},
	})
	if !errors.Is(err, repository.ErrMethodOwner) {
		t.Fatalf("register conflicting manifest error = %v, want ErrMethodOwner", err)
	}
	if _, err := service.Admin.GetMethod(ctx, "atomic.must_rollback"); !errors.Is(err, repository.ErrMethodNotFound) {
		t.Fatalf("partial manifest row survived rollback: %v", err)
	}
}

func TestControlRegisterManifestSerializesConflictingOwners(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	start := make(chan struct{})
	type result struct {
		service string
		err     error
	}
	results := make(chan result, 2)

	for _, serviceName := range []string{"concurrent-owner-a", "concurrent-owner-b"} {
		serviceName := serviceName
		go func() {
			<-start
			err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
				{
					Key:      "concurrent.owner",
					Service:  serviceName,
					GroupKey: "test",
				},
			})
			results <- result{service: serviceName, err: err}
		}()
	}

	close(start)
	first := <-results
	second := <-results
	winners := make([]string, 0, 1)
	losers := 0
	for _, value := range []result{first, second} {
		switch {
		case value.err == nil:
			winners = append(winners, value.service)
		case errors.Is(value.err, repository.ErrMethodOwner):
			losers++
		default:
			t.Fatalf("unexpected concurrent manifest error for %s: %v", value.service, value.err)
		}
	}
	if len(winners) != 1 || losers != 1 {
		t.Fatalf("concurrent manifest results: winners=%v losers=%d", winners, losers)
	}

	method, err := service.Admin.GetMethod(ctx, "concurrent.owner")
	if err != nil {
		t.Fatalf("get concurrent manifest method: %v", err)
	}
	if method.Service != winners[0] {
		t.Fatalf("stored method owner = %q, want %q", method.Service, winners[0])
	}
}

func newControlTestService(t testing.TB) *control.Control {
	return newControlTestServiceWithOptions(t, control.Options{
		SecretEncryptionKey: controlTestSecretEncryptionKey,
	})
}

func newControlTestServiceWithOptions(t testing.TB, options control.Options) *control.Control {
	t.Helper()
	adminDB, err := sql.Open("pgx", controlPostgresDSN("postgres"))
	if err != nil {
		t.Fatalf("open postgres admin: %v", err)
	}
	defer adminDB.Close()
	if _, err := adminDB.Exec(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		controlTestDatabase,
	); err != nil {
		t.Fatalf("terminate test database connections: %v", err)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", controlTestDatabase)); err != nil {
		t.Fatalf("drop test database: %v", err)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", controlTestDatabase)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	db.SetConnMaxLifetime(time.Minute)
	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("new sql client: %v", err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	service, err := control.NewWithDatabase(context.Background(), db, options)
	if err != nil {
		t.Fatalf("new control: %v", err)
	}
	t.Cleanup(func() {
		_ = service.Close()
		_ = repo.Close()
		_ = client.Close()
	})
	return service
}

func controlPostgresDSN(database string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		controlTestUser,
		controlTestPassword,
		controlTestHost,
		controlTestPort,
		database,
	)
}

func uint32Pointer(value uint32) *uint32 {
	return &value
}

func controlTestTOTP(secret string, now time.Time) string {
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	counter := uint64(now.Unix() / 30)
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	return fmt.Sprintf("%06d", value%1_000_000)
}
