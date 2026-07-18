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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elum2b/services/control"
	controlmodel "github.com/elum2b/services/control/model"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/control/service/admin"
	"github.com/elum2b/services/control/service/internalapi"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	controlTestHost     = "localhost"
	controlTestPort     = 5432
	controlTestUser     = "postgres"
	controlTestPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	controlTestDatabase = "control_test"
)

var controlTestSecretEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

func TestControlInitializationAndInvitationOnlyRegistration(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()

	initialized, err := service.Admin.IsInitialized(ctx)
	if err != nil || initialized {
		t.Fatalf("initial state: initialized=%v err=%v", initialized, err)
	}

	owner := initializeControl(t, service, "owner")
	if owner.Account.ID == "" || owner.SessionToken == "" || !owner.Created {
		t.Fatalf("initialize result = %#v", owner)
	}

	if _, err := service.Admin.Initialize(ctx, authParams("other-initializer")); !errors.Is(
		err,
		repository.ErrAlreadyInitialized,
	) {
		t.Fatalf("second initialize error = %v", err)
	}

	if _, err := service.Admin.CompleteAuth(ctx, authParams("uninvited")); !errors.Is(
		err,
		repository.ErrInviteRequired,
	) {
		t.Fatalf("uninvited registration error = %v", err)
	}

	if _, err := service.Admin.CompleteAuth(ctx, authParams("owner")); err != nil {
		t.Fatalf("existing owner authentication: %v", err)
	}

}

func TestControlWorkspaceLimitCountsOwnershipOnly(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	creatorRole, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     "workspace_creator",
		Title:    "Workspace creator",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	if err := service.Admin.ReplaceGlobalRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:    owner.Account.ID,
			RoleID:     creatorRole.ID,
			MethodKeys: []string{"control.global.workspace.create"},
		},
	); err != nil {
		t.Fatalf("grant workspace create: %v", err)
	}

	_, globalToken, err := service.Admin.CreateGlobalInvite(ctx, admin.CreateInviteParams{
		ActorID: owner.Account.ID,
		RoleIDs: []string{creatorRole.ID},
	})
	if err != nil {
		t.Fatalf("create global invite: %v", err)
	}

	friendParams := authParams("friend")
	friendParams.InviteToken = globalToken
	friend, err := service.Admin.CompleteAuth(ctx, friendParams)
	if err != nil {
		t.Fatalf("register friend: %v", err)
	}

	ownerWorkspace := createWorkspace(t, service, owner.Account.ID, "owner-workspace")
	_, workspaceToken, err := service.Admin.CreateWorkspaceInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: ownerWorkspace.ID,
		},
	)
	if err != nil {
		t.Fatalf("create workspace invite: %v", err)
	}

	friendParams.InviteToken = workspaceToken
	if _, err := service.Admin.CompleteAuth(ctx, friendParams); err != nil {
		t.Fatalf("accept workspace invitation: %v", err)
	}

	friendWorkspace := createWorkspace(t, service, friend.Account.ID, "friend-workspace")
	if friendWorkspace.OwnerAccountID != friend.Account.ID {
		t.Fatalf("friend workspace owner = %q", friendWorkspace.OwnerAccountID)
	}

	_, err = service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: friend.Account.ID,
		ID:      uuid.NewString(),
		Slug:    "friend-second",
		Title:   "Friend second",
	})
	if !errors.Is(err, repository.ErrWorkspaceLimit) {
		t.Fatalf("second owned workspace error = %v", err)
	}

	workspaces, err := service.Admin.ListWorkspaces(ctx, friend.Account.ID, admin.Page{Limit: 10})
	if err != nil || len(workspaces) != 2 {
		t.Fatalf("friend memberships = %#v err=%v", workspaces, err)
	}

}

func TestControlWorkspaceEmployeeLimitReservesPendingInvites(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	workspace := createWorkspace(t, service, owner.Account.ID, "employees")

	inviteIDs := make([]string, 0, 10)
	for index := 0; index < 10; index++ {
		invite, _, err := service.Admin.CreateWorkspaceInvite(
			ctx,
			admin.CreateInviteParams{
				ActorID:     owner.Account.ID,
				WorkspaceID: workspace.ID,
			},
		)
		if err != nil {
			t.Fatalf("create invite %d: %v", index, err)
		}
		inviteIDs = append(inviteIDs, invite.ID)
	}

	if _, _, err := service.Admin.CreateWorkspaceInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
		},
	); !errors.Is(err, repository.ErrEmployeeLimit) {
		t.Fatalf("eleventh pending invite error = %v", err)
	}

	if _, err := service.Admin.RevokeInvite(ctx, owner.Account.ID, inviteIDs[0]); err != nil {
		t.Fatalf("revoke pending invite: %v", err)
	}
	if _, _, err := service.Admin.CreateWorkspaceInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
		},
	); err != nil {
		t.Fatalf("create invite after revoke: %v", err)
	}

}

func TestControlOneTimeInviteIsAtomic(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	_, token, err := service.Admin.CreateGlobalInvite(ctx, admin.CreateInviteParams{
		ActorID: owner.Account.ID,
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	for _, subject := range []string{"friend-a", "friend-b"} {
		subject := subject
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			params := authParams(subject)
			params.InviteToken = token
			_, err := service.Admin.CompleteAuth(ctx, params)
			errorsCh <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsCh)

	succeeded := 0
	rejected := 0
	for err := range errorsCh {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, repository.ErrInviteUnavailable):
			rejected++
		default:
			t.Fatalf("unexpected invite race error: %v", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("invite race: succeeded=%d rejected=%d", succeeded, rejected)
	}

}

func TestControlOwnershipTransferHonorsTargetLimit(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	friend := inviteAccount(t, service, owner.Account.ID, "friend")

	ownerWorkspace := createWorkspace(t, service, owner.Account.ID, "owner-workspace")
	friendWorkspace := createWorkspaceWithGlobalPermission(
		t,
		service,
		owner.Account.ID,
		friend.Account.ID,
		"friend-workspace",
	)
	inviteIntoWorkspace(t, service, owner.Account.ID, ownerWorkspace.ID, friend.Account.ID, "friend")

	err := service.Admin.TransferWorkspaceOwnership(
		ctx,
		owner.Account.ID,
		ownerWorkspace.ID,
		friend.Account.ID,
	)
	if !errors.Is(err, repository.ErrWorkspaceLimit) {
		t.Fatalf("transfer above target limit error = %v", err)
	}

	request, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		friend.Account.ID,
		2,
		"Need a second owned workspace",
	)
	if err != nil {
		t.Fatalf("request workspace limit: %v", err)
	}
	if _, err := service.Admin.ResolveLimitRequest(
		ctx,
		admin.ResolveLimitRequestParams{
			ActorID:       owner.Account.ID,
			RequestID:     request.ID,
			Approved:      true,
			ApprovedLimit: 2,
		},
	); err != nil {
		t.Fatalf("approve workspace limit: %v", err)
	}

	if err := service.Admin.TransferWorkspaceOwnership(
		ctx,
		owner.Account.ID,
		ownerWorkspace.ID,
		friend.Account.ID,
	); err != nil {
		t.Fatalf("transfer ownership: %v", err)
	}

	updated, err := service.Admin.GetWorkspace(ctx, ownerWorkspace.ID)
	if err != nil || updated.OwnerAccountID != friend.Account.ID {
		t.Fatalf("updated owner = %#v err=%v", updated, err)
	}
	workspaces, err := service.Admin.ListWorkspaces(ctx, owner.Account.ID, admin.Page{Limit: 10})
	if err != nil || len(workspaces) != 1 || workspaces[0].ID != ownerWorkspace.ID {
		t.Fatalf("former owner membership = %#v err=%v", workspaces, err)
	}
	if friendWorkspace.OwnerAccountID != friend.Account.ID {
		t.Fatalf("first friend workspace changed: %#v", friendWorkspace)
	}

}

func TestControlWorkspaceAccessIsScoped(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	member := inviteAccount(t, service, owner.Account.ID, "member")
	one := createWorkspace(t, service, owner.Account.ID, "one")

	role, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: one.ID,
		Code:        "editor",
		Title:       "Editor",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create workspace role: %v", err)
	}
	if err := service.Admin.ReplaceWorkspaceRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: one.ID,
			RoleID:      role.ID,
			MethodKeys:  []string{"control.workspace.update"},
		},
	); err != nil {
		t.Fatalf("grant workspace permission: %v", err)
	}
	inviteIntoWorkspaceWithRole(
		t,
		service,
		owner.Account.ID,
		one.ID,
		member.Account.ID,
		"member",
		role.ID,
	)

	allowed, err := service.Internal.CheckWorkspaceAccess(
		ctx,
		internalapi.WorkspaceAccessRequest{
			AccountID:   member.Account.ID,
			WorkspaceID: one.ID,
			MethodKey:   "control.workspace.update",
		},
	)
	if err != nil || !allowed {
		t.Fatalf("workspace access: allowed=%v err=%v", allowed, err)
	}
	globalAllowed, err := service.Internal.CheckGlobalAccess(
		ctx,
		internalapi.GlobalAccessRequest{
			AccountID: member.Account.ID,
			MethodKey: "control.global.workspace.create",
		},
	)
	if err != nil || globalAllowed {
		t.Fatalf("workspace role leaked globally: allowed=%v err=%v", globalAllowed, err)
	}

}

func TestControlRejectsWrongScopeAndManifestHijack(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	workspace := createWorkspace(t, service, owner.Account.ID, "scope")

	role, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "scope",
		Title:       "Scope",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}
	err = service.Admin.ReplaceWorkspaceRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
			RoleID:      role.ID,
			MethodKeys:  []string{"control.global.workspace.create"},
		},
	)
	if !errors.Is(err, repository.ErrMethodNotFound) {
		t.Fatalf("wrong-scope permission error = %v", err)
	}

	if err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
		{
			Key:      "tasks.task.custom_update",
			Service:  "tasks",
			GroupKey: "task",
			Position: 10,
		},
	}); err != nil {
		t.Fatalf("register manifest: %v", err)
	}
	err = service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
		{
			Key:      "tasks.task.custom_update",
			Service:  "cpa",
			GroupKey: "offer",
			Position: 10,
		},
	})
	if !errors.Is(err, repository.ErrMethodOwner) {
		t.Fatalf("manifest hijack error = %v", err)
	}

}

func TestControlAccessCatalogHasLocalizedGlobalAndWorkspaceScopes(t *testing.T) {

	service := newControlTestService(t)
	initializeControl(t, service, "owner")

	for _, scope := range []admin.AccessScope{admin.ScopeGlobal, admin.ScopeWorkspace} {
		items, err := service.Admin.ListAccess(context.Background(), "ru", scope)
		if err != nil {
			t.Fatalf("list %s access: %v", scope, err)
		}
		found := false
		for _, serviceItem := range items {
			for _, group := range serviceItem.Groups {
				for _, access := range group.Accesses {
					if access.Scope == scope && access.Title != "" && access.Desc != "" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("localized %s access was not found: %#v", scope, items)
		}
	}

}

func TestControlPlatformRemovalClearsAccessAndRequiresFreshGlobalInvite(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	member := inviteAccount(t, service, owner.Account.ID, "member")
	workspace := createWorkspace(t, service, owner.Account.ID, "removal")

	globalRole, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     "creator",
		Title:    "Creator",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	if err := service.Admin.ReplaceGlobalRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:    owner.Account.ID,
			RoleID:     globalRole.ID,
			MethodKeys: []string{"control.global.workspace.create"},
		},
	); err != nil {
		t.Fatalf("grant global permission: %v", err)
	}
	if err := service.Admin.AssignGlobalRole(ctx, admin.SetRoleMemberParams{
		ActorID:   owner.Account.ID,
		AccountID: member.Account.ID,
		RoleID:    globalRole.ID,
	}); err != nil {
		t.Fatalf("assign global role: %v", err)
	}

	workspaceRole, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "editor",
		Title:       "Editor",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create workspace role: %v", err)
	}
	if err := service.Admin.ReplaceWorkspaceRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
			RoleID:      workspaceRole.ID,
			MethodKeys:  []string{"control.workspace.update"},
		},
	); err != nil {
		t.Fatalf("grant workspace permission: %v", err)
	}
	inviteIntoWorkspaceWithRole(
		t,
		service,
		owner.Account.ID,
		workspace.ID,
		member.Account.ID,
		"member",
		workspaceRole.ID,
	)

	if _, err := service.Admin.RemovePlatformMember(
		ctx,
		owner.Account.ID,
		member.Account.ID,
	); err != nil {
		t.Fatalf("remove platform member: %v", err)
	}

	allowed, err := service.Internal.CheckWorkspaceAccess(
		ctx,
		internalapi.WorkspaceAccessRequest{
			AccountID:   member.Account.ID,
			WorkspaceID: workspace.ID,
			MethodKey:   "control.workspace.update",
		},
	)
	if err != nil || allowed {
		t.Fatalf("removed workspace access: allowed=%v err=%v", allowed, err)
	}
	if _, err := service.Admin.CompleteAuth(ctx, authParams("member")); !errors.Is(
		err,
		repository.ErrInviteRequired,
	) {
		t.Fatalf("removed member login error = %v", err)
	}

	_, token, err := service.Admin.CreateGlobalInvite(
		ctx,
		admin.CreateInviteParams{ActorID: owner.Account.ID},
	)
	if err != nil {
		t.Fatalf("create reactivation invite: %v", err)
	}
	params := authParams("member")
	params.InviteToken = token
	if _, err := service.Admin.CompleteAuth(ctx, params); err != nil {
		t.Fatalf("reactivate member: %v", err)
	}

	globalAllowed, err := service.Internal.CheckGlobalAccess(
		ctx,
		internalapi.GlobalAccessRequest{
			AccountID: member.Account.ID,
			MethodKey: "control.global.workspace.create",
		},
	)
	if err != nil || globalAllowed {
		t.Fatalf("old global role restored: allowed=%v err=%v", globalAllowed, err)
	}
	workspaces, err := service.Admin.ListWorkspaces(ctx, member.Account.ID, admin.Page{Limit: 10})
	if err != nil || len(workspaces) != 0 {
		t.Fatalf("old workspace membership restored: %#v err=%v", workspaces, err)
	}

}

func TestControlBackupCodeIsSingleUseAcrossConcurrentChallenges(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	_, backupCodes := enableControlTwoFactor(t, service, owner.Account.ID)

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open trigger database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE OR REPLACE FUNCTION control_test_delay_backup_update()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			PERFORM pg_sleep(0.2);
			RETURN NEW;
		END
		$$
	`); err != nil {
		t.Fatalf("create backup delay function: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER control_test_delay_backup_update
		BEFORE UPDATE OF backup_hashes ON control_two_factor
		FOR EACH ROW
		EXECUTE FUNCTION control_test_delay_backup_update()
	`); err != nil {
		t.Fatalf("create backup delay trigger: %v", err)
	}

	challenges := make([]string, 2)
	for index := range challenges {
		result, err := service.Admin.CompleteAuth(ctx, authParams("owner"))
		if err != nil {
			t.Fatalf("create challenge %d: %v", index, err)
		}
		if !result.TwoFactorRequired || result.TwoFactorChallenge == "" {
			t.Fatalf("challenge %d result = %#v", index, result)
		}
		challenges[index] = result.TwoFactorChallenge
	}

	start := make(chan struct{})
	results := make(chan error, len(challenges))
	for _, challenge := range challenges {
		challenge := challenge
		go func() {
			<-start
			_, err := service.Admin.CompleteTwoFactor(
				ctx,
				challenge,
				backupCodes[0],
				"127.0.0.1",
			)
			results <- err
		}()
	}
	close(start)

	succeeded := 0
	forbidden := 0
	for range challenges {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, repository.ErrForbidden):
			forbidden++
		default:
			t.Fatalf("complete challenge error = %v", err)
		}
	}
	if succeeded != 1 || forbidden != 1 {
		t.Fatalf("challenge results: succeeded=%d forbidden=%d", succeeded, forbidden)
	}

}

func TestControlPlatformRemovalRevokesSessionsAndDefersTwoFactorReactivation(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	member := inviteAccount(t, service, owner.Account.ID, "member")
	_, backupCodes := enableControlTwoFactor(t, service, member.Account.ID)

	if _, err := service.Admin.RemovePlatformMember(
		ctx,
		owner.Account.ID,
		member.Account.ID,
	); err != nil {
		t.Fatalf("remove platform member: %v", err)
	}
	if _, err := service.Admin.ValidateSession(
		ctx,
		member.SessionToken,
		"127.0.0.1",
	); err == nil {
		t.Fatal("removed member session remains valid")
	}

	_, inviteToken, err := service.Admin.CreateGlobalInvite(
		ctx,
		admin.CreateInviteParams{ActorID: owner.Account.ID},
	)
	if err != nil {
		t.Fatalf("create reactivation invite: %v", err)
	}
	params := authParams("member")
	params.InviteToken = inviteToken
	challenge, err := service.Admin.CompleteAuth(ctx, params)
	if err != nil {
		t.Fatalf("start member reactivation: %v", err)
	}
	if !challenge.TwoFactorRequired || challenge.TwoFactorChallenge == "" {
		t.Fatalf("reactivation challenge = %#v", challenge)
	}

	members, err := service.Admin.ListPlatformMembers(ctx, admin.Page{Limit: 100})
	if err != nil {
		t.Fatalf("list platform members: %v", err)
	}
	if status := platformMemberStatus(members, member.Account.ID); status != "removed" {
		t.Fatalf("member status before second factor = %q", status)
	}

	result, err := service.Admin.CompleteTwoFactor(
		ctx,
		challenge.TwoFactorChallenge,
		backupCodes[0],
		"127.0.0.1",
	)
	if err != nil {
		t.Fatalf("complete member reactivation: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("reactivation did not create a session")
	}
	if result.Session.CreatedAt.IsZero() || result.Session.LastUsedAt.IsZero() {
		t.Fatalf("reactivation session timestamps = %#v", result.Session)
	}
	if _, err := service.Admin.ValidateSession(
		ctx,
		result.SessionToken,
		"127.0.0.1",
	); err != nil {
		t.Fatalf("validate reactivation session: %v", err)
	}
	if _, err := service.Admin.ValidateSession(
		ctx,
		member.SessionToken,
		"127.0.0.1",
	); err == nil {
		t.Fatal("old session was restored after reactivation")
	}

}

func TestControlDeleteRolesRemovesPendingInviteReferences(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	globalRole, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     "temporary_global",
		Title:    "Temporary global",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	_, globalToken, err := service.Admin.CreateGlobalInvite(ctx, admin.CreateInviteParams{
		ActorID: owner.Account.ID,
		RoleIDs: []string{globalRole.ID},
	})
	if err != nil {
		t.Fatalf("create global invite: %v", err)
	}
	if affected, err := service.Admin.DeleteGlobalRole(
		ctx,
		owner.Account.ID,
		globalRole.ID,
	); err != nil || affected != 1 {
		t.Fatalf("delete global role: affected=%d err=%v", affected, err)
	}
	params := authParams("friend")
	params.InviteToken = globalToken
	friend, err := service.Admin.CompleteAuth(ctx, params)
	if err != nil {
		t.Fatalf("accept global invite after role deletion: %v", err)
	}

	workspace := createWorkspace(t, service, owner.Account.ID, "role-delete")
	workspaceRole, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "temporary_workspace",
		Title:       "Temporary workspace",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create workspace role: %v", err)
	}
	_, workspaceToken, err := service.Admin.CreateWorkspaceInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
			RoleIDs:     []string{workspaceRole.ID},
		},
	)
	if err != nil {
		t.Fatalf("create workspace invite: %v", err)
	}
	if affected, err := service.Admin.DeleteWorkspaceRole(
		ctx,
		owner.Account.ID,
		workspace.ID,
		workspaceRole.ID,
	); err != nil || affected != 1 {
		t.Fatalf("delete workspace role: affected=%d err=%v", affected, err)
	}
	params.InviteToken = workspaceToken
	if _, err := service.Admin.CompleteAuth(ctx, params); err != nil {
		t.Fatalf("accept workspace invite after role deletion: %v", err)
	}
	workspaces, err := service.Admin.ListWorkspaces(ctx, friend.Account.ID, admin.Page{Limit: 10})
	if err != nil || len(workspaces) != 1 || workspaces[0].ID != workspace.ID {
		t.Fatalf("friend workspaces = %#v err=%v", workspaces, err)
	}

}

func TestControlManifestValidatesNamespaceAndInvalidatesAccessCache(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	initializeControl(t, service, "owner")

	const methodKey = "tasks.task.cache_regression"
	before, err := service.Admin.ListAccess(ctx, "en", admin.ScopeWorkspace)
	if err != nil {
		t.Fatalf("prime access cache: %v", err)
	}
	if accessCatalogContains(before, methodKey) {
		t.Fatalf("test method %q already exists", methodKey)
	}

	if err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{
		{
			Key:      methodKey,
			Service:  "tasks",
			GroupKey: "task",
			Position: 999,
		},
	}); err != nil {
		t.Fatalf("register valid manifest: %v", err)
	}
	after, err := service.Admin.ListAccess(ctx, "en", admin.ScopeWorkspace)
	if err != nil {
		t.Fatalf("read invalidated access cache: %v", err)
	}
	if !accessCatalogContains(after, methodKey) {
		t.Fatalf("registered method %q is missing from access catalog", methodKey)
	}

	invalid := []internalapi.MethodManifest{
		{
			Key:      "control.global.injection",
			Service:  "tasks",
			GroupKey: "task",
		},
		{
			Key:      "tasks.unknown.action",
			Service:  "tasks",
			GroupKey: "unknown",
		},
	}
	for index, manifest := range invalid {
		err := service.Internal.RegisterManifest(ctx, []internalapi.MethodManifest{manifest})
		if !errors.Is(err, repository.ErrInvalidArgument) {
			t.Fatalf("invalid manifest %d error = %v", index, err)
		}
	}

}

func TestControlSecurityMutationsAreAudited(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	if err := service.Admin.BindIdentity(ctx, owner.Account.ID, admin.AuthIdentityParams{
		Provider:    "backup",
		Subject:     "owner-backup",
		DisplayName: "Owner backup",
	}); err != nil {
		t.Fatalf("bind identity: %v", err)
	}
	if _, err := service.Admin.UnbindIdentity(ctx, owner.Account.ID, "backup"); err != nil {
		t.Fatalf("unbind identity: %v", err)
	}
	extra, err := service.Admin.CompleteAuth(ctx, authParams("owner"))
	if err != nil {
		t.Fatalf("create extra session: %v", err)
	}
	if _, err := service.Admin.RevokeSession(
		ctx,
		owner.Account.ID,
		extra.Session.ID,
	); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	if _, err := service.Admin.RevokeAllSessions(ctx, owner.Account.ID, ""); err != nil {
		t.Fatalf("revoke all sessions: %v", err)
	}
	_, backupCodes := enableControlTwoFactor(t, service, owner.Account.ID)
	if _, err := service.Admin.DisableTwoFactor(
		ctx,
		owner.Account.ID,
		backupCodes[0],
	); err != nil {
		t.Fatalf("disable two factor: %v", err)
	}

	events, err := service.Admin.ListGlobalAudit(ctx, admin.Page{Limit: 100})
	if err != nil {
		t.Fatalf("list global audit: %v", err)
	}
	keys := make(map[string]bool, len(events))
	for _, event := range events {
		keys[event.MethodKey] = true
	}
	for _, key := range []string{
		"control.auth.identity.bind",
		"control.auth.identity.unbind",
		"control.auth.session.revoke",
		"control.auth.session.revoke_all",
		"control.auth.two_factor.begin",
		"control.auth.two_factor.confirm",
		"control.auth.two_factor.disable",
	} {
		if !keys[key] {
			t.Fatalf("audit event %q is missing", key)
		}
	}

}

func TestControlTOTPIsSingleUseAcrossChallenges(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	secret, _ := enableControlTwoFactor(t, service, owner.Account.ID)

	challenges := make([]string, 2)
	for index := range challenges {
		result, err := service.Admin.CompleteAuth(ctx, authParams("owner"))
		if err != nil {
			t.Fatalf("create challenge %d: %v", index, err)
		}
		challenges[index] = result.TwoFactorChallenge
	}

	nextCode := controlTestTOTP(secret, time.Now().Add(30*time.Second))
	if _, err := service.Admin.CompleteTwoFactor(
		ctx,
		challenges[0],
		nextCode,
		"127.0.0.1",
	); err != nil {
		t.Fatalf("complete first challenge: %v", err)
	}
	if _, err := service.Admin.CompleteTwoFactor(
		ctx,
		challenges[1],
		nextCode,
		"127.0.0.1",
	); !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("reused TOTP error = %v", err)
	}

}

func TestControlRemovalSerializesConcurrentAuthentication(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	member := inviteAccount(t, service, owner.Account.ID, "member")

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open trigger database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	delayFunction := fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION control_test_delay_member_session()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF NEW.account_id = '%s' THEN
				PERFORM pg_sleep(0.4);
			END IF;
			RETURN NEW;
		END
		$$
	`, member.Account.ID)
	if _, err := db.Exec(delayFunction); err != nil {
		t.Fatalf("create session delay function: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER control_test_delay_member_session
		BEFORE INSERT ON control_session
		FOR EACH ROW
		EXECUTE FUNCTION control_test_delay_member_session()
	`); err != nil {
		t.Fatalf("create session delay trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TRIGGER IF EXISTS control_test_delay_member_session ON control_session")
		_, _ = db.Exec("DROP FUNCTION IF EXISTS control_test_delay_member_session()")
	})

	type authCall struct {
		result admin.AuthResult
		err    error
	}
	authResult := make(chan authCall, 1)
	go func() {
		result, err := service.Admin.CompleteAuth(ctx, authParams("member"))
		authResult <- authCall{result: result, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	if _, err := service.Admin.RemovePlatformMember(
		ctx,
		owner.Account.ID,
		member.Account.ID,
	); err != nil {
		t.Fatalf("remove platform member: %v", err)
	}

	concurrentAuth := <-authResult
	if concurrentAuth.err != nil {
		t.Fatalf("concurrent authentication: %v", concurrentAuth.err)
	}

	if _, err := db.Exec("DROP TRIGGER IF EXISTS control_test_delay_member_session ON control_session"); err != nil {
		t.Fatalf("drop session delay trigger: %v", err)
	}

	_, inviteToken, err := service.Admin.CreateGlobalInvite(
		ctx,
		admin.CreateInviteParams{ActorID: owner.Account.ID},
	)
	if err != nil {
		t.Fatalf("create reactivation invite: %v", err)
	}
	params := authParams("member")
	params.InviteToken = inviteToken
	if _, err := service.Admin.CompleteAuth(ctx, params); err != nil {
		t.Fatalf("reactivate member: %v", err)
	}

	if _, err := service.Admin.ValidateSession(
		ctx,
		concurrentAuth.result.SessionToken,
		"127.0.0.1",
	); err == nil {
		t.Fatal("session committed during removal became valid after reactivation")
	}

}

func TestControlTwoFactorSetupSerializesBeginAndConfirm(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	setup, err := service.Admin.BeginTwoFactor(ctx, owner.Account.ID, "Elum control test")
	if err != nil {
		t.Fatalf("begin two factor: %v", err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open trigger database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE OR REPLACE FUNCTION control_test_delay_two_factor_confirm()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			PERFORM pg_sleep(0.4);
			RETURN NEW;
		END
		$$
	`); err != nil {
		t.Fatalf("create two-factor delay function: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER control_test_delay_two_factor_confirm
		BEFORE UPDATE OF backup_hashes ON control_two_factor
		FOR EACH ROW
		EXECUTE FUNCTION control_test_delay_two_factor_confirm()
	`); err != nil {
		t.Fatalf("create two-factor delay trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TRIGGER IF EXISTS control_test_delay_two_factor_confirm ON control_two_factor")
		_, _ = db.Exec("DROP FUNCTION IF EXISTS control_test_delay_two_factor_confirm()")
	})

	confirmResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.ConfirmTwoFactor(
			ctx,
			owner.Account.ID,
			controlTestTOTP(setup.Secret, time.Now()),
		)
		confirmResult <- err
	}()

	time.Sleep(100 * time.Millisecond)
	_, beginErr := service.Admin.BeginTwoFactor(ctx, owner.Account.ID, "Elum control test")
	if err := <-confirmResult; err != nil {
		t.Fatalf("confirm two factor: %v", err)
	}
	if !errors.Is(beginErr, repository.ErrTwoFactorEnabled) {
		t.Fatalf("concurrent begin error = %v", beginErr)
	}

	result, err := service.Admin.CompleteAuth(ctx, authParams("owner"))
	if err != nil {
		t.Fatalf("authenticate after confirmation: %v", err)
	}
	if !result.TwoFactorRequired || result.TwoFactorChallenge == "" {
		t.Fatalf("two factor was reset by concurrent begin: %#v", result)
	}

}

func TestControlRemovalRevokesPendingInvitesFromRemovedMember(t *testing.T) {

	t.Run("global", func(t *testing.T) {
		service := newControlTestService(t)
		ctx := context.Background()
		owner := initializeControl(t, service, "owner")
		moderator := inviteAccount(t, service, owner.Account.ID, "moderator")

		role, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
			ActorID:  owner.Account.ID,
			Code:     "global_inviter",
			Title:    "Global inviter",
			Position: 10,
		})
		if err != nil {
			t.Fatalf("create global inviter role: %v", err)
		}
		if err := service.Admin.ReplaceGlobalRolePermissions(
			ctx,
			admin.ReplaceRolePermissionsParams{
				ActorID:    owner.Account.ID,
				RoleID:     role.ID,
				MethodKeys: []string{"control.global.invite.create"},
			},
		); err != nil {
			t.Fatalf("grant global invite permission: %v", err)
		}
		if err := service.Admin.AssignGlobalRole(ctx, admin.SetRoleMemberParams{
			ActorID:   owner.Account.ID,
			AccountID: moderator.Account.ID,
			RoleID:    role.ID,
		}); err != nil {
			t.Fatalf("assign global inviter role: %v", err)
		}

		_, token, err := service.Admin.CreateGlobalInvite(
			ctx,
			admin.CreateInviteParams{ActorID: moderator.Account.ID},
		)
		if err != nil {
			t.Fatalf("create global invite: %v", err)
		}
		if _, err := service.Admin.RemovePlatformMember(
			ctx,
			owner.Account.ID,
			moderator.Account.ID,
		); err != nil {
			t.Fatalf("remove platform member: %v", err)
		}

		params := authParams("global-invite-target")
		params.InviteToken = token
		if _, err := service.Admin.CompleteAuth(ctx, params); !errors.Is(
			err,
			repository.ErrInviteUnavailable,
		) {
			t.Fatalf("removed member global invite error = %v", err)
		}
	})

	t.Run("workspace", func(t *testing.T) {
		service := newControlTestService(t)
		ctx := context.Background()
		owner := initializeControl(t, service, "owner")
		moderator := inviteAccount(t, service, owner.Account.ID, "moderator")
		inviteAccount(t, service, owner.Account.ID, "target")
		workspace := createWorkspace(t, service, owner.Account.ID, "invite-revocation")

		role, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
			Code:        "workspace_inviter",
			Title:       "Workspace inviter",
			Position:    10,
		})
		if err != nil {
			t.Fatalf("create workspace inviter role: %v", err)
		}
		if err := service.Admin.ReplaceWorkspaceRolePermissions(
			ctx,
			admin.ReplaceRolePermissionsParams{
				ActorID:     owner.Account.ID,
				WorkspaceID: workspace.ID,
				RoleID:      role.ID,
				MethodKeys:  []string{"control.workspace.invite.create"},
			},
		); err != nil {
			t.Fatalf("grant workspace invite permission: %v", err)
		}
		inviteIntoWorkspaceWithRole(
			t,
			service,
			owner.Account.ID,
			workspace.ID,
			moderator.Account.ID,
			"moderator",
			role.ID,
		)

		_, token, err := service.Admin.CreateWorkspaceInvite(
			ctx,
			admin.CreateInviteParams{
				ActorID:     moderator.Account.ID,
				WorkspaceID: workspace.ID,
			},
		)
		if err != nil {
			t.Fatalf("create workspace invite: %v", err)
		}
		if _, err := service.Admin.RemoveMember(
			ctx,
			owner.Account.ID,
			workspace.ID,
			moderator.Account.ID,
		); err != nil {
			t.Fatalf("remove workspace member: %v", err)
		}

		params := authParams("target")
		params.InviteToken = token
		if _, err := service.Admin.CompleteAuth(ctx, params); !errors.Is(
			err,
			repository.ErrInviteUnavailable,
		) {
			t.Fatalf("removed member workspace invite error = %v", err)
		}
	})

}

func TestControlInviteRolesAreNormalizedAndAuditTargetsInvite(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	globalRole, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     "invite_role",
		Title:    "Invite role",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	globalInvite, _, err := service.Admin.CreateGlobalInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID: owner.Account.ID,
			RoleIDs: []string{globalRole.ID, " " + globalRole.ID + " "},
		},
	)
	if err != nil {
		t.Fatalf("create global invite with duplicate roles: %v", err)
	}
	if len(globalInvite.RoleIDs) != 1 || globalInvite.RoleIDs[0] != globalRole.ID {
		t.Fatalf("global invite roles = %#v", globalInvite.RoleIDs)
	}

	globalAudit, err := service.Admin.ListGlobalAudit(ctx, admin.Page{Limit: 100})
	if err != nil {
		t.Fatalf("list global audit: %v", err)
	}
	if !auditContainsTarget(
		globalAudit,
		"control.global.invite.create",
		globalInvite.ID,
	) {
		t.Fatalf("global invite audit target %q is missing", globalInvite.ID)
	}

	workspace := createWorkspace(t, service, owner.Account.ID, "invite-audit")
	workspaceRole, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "invite_role",
		Title:       "Invite role",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create workspace role: %v", err)
	}
	workspaceInvite, _, err := service.Admin.CreateWorkspaceInvite(
		ctx,
		admin.CreateInviteParams{
			ActorID:     owner.Account.ID,
			WorkspaceID: workspace.ID,
			RoleIDs: []string{
				workspaceRole.ID,
				" " + workspaceRole.ID + " ",
			},
		},
	)
	if err != nil {
		t.Fatalf("create workspace invite with duplicate roles: %v", err)
	}
	if len(workspaceInvite.RoleIDs) != 1 || workspaceInvite.RoleIDs[0] != workspaceRole.ID {
		t.Fatalf("workspace invite roles = %#v", workspaceInvite.RoleIDs)
	}

	workspaceAudit, err := service.Admin.ListWorkspaceAudit(
		ctx,
		workspace.ID,
		admin.Page{Limit: 100},
	)
	if err != nil {
		t.Fatalf("list workspace audit: %v", err)
	}
	if !auditContainsTarget(
		workspaceAudit,
		"control.workspace.invite.create",
		workspaceInvite.ID,
	) {
		t.Fatalf("workspace invite audit target %q is missing", workspaceInvite.ID)
	}

}

func TestControlLimitCancellationUsesRequestScope(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	workspace := createWorkspace(t, service, owner.Account.ID, "limit-audit")

	employeeRequest, err := service.Admin.RequestEmployeeLimit(
		ctx,
		owner.Account.ID,
		workspace.ID,
		20,
		"Need more employees",
	)
	if err != nil {
		t.Fatalf("request employee limit: %v", err)
	}
	if _, err := service.Admin.CancelLimitRequest(
		ctx,
		owner.Account.ID,
		employeeRequest.ID,
	); err != nil {
		t.Fatalf("cancel employee limit request: %v", err)
	}

	workspaceAudit, err := service.Admin.ListWorkspaceAudit(
		ctx,
		workspace.ID,
		admin.Page{Limit: 100},
	)
	if err != nil {
		t.Fatalf("list workspace audit: %v", err)
	}
	if !auditContainsTarget(
		workspaceAudit,
		"control.workspace.employee_limit.cancel",
		employeeRequest.ID,
	) {
		t.Fatalf("employee limit cancellation audit %q is missing", employeeRequest.ID)
	}

	workspaceRequest, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		owner.Account.ID,
		2,
		"Need another workspace",
	)
	if err != nil {
		t.Fatalf("request workspace limit: %v", err)
	}
	if _, err := service.Admin.CancelLimitRequest(
		ctx,
		owner.Account.ID,
		workspaceRequest.ID,
	); err != nil {
		t.Fatalf("cancel workspace limit request: %v", err)
	}

	globalAudit, err := service.Admin.ListGlobalAudit(ctx, admin.Page{Limit: 100})
	if err != nil {
		t.Fatalf("list global audit: %v", err)
	}
	if !auditContainsTarget(
		globalAudit,
		"control.global.limit.cancel",
		workspaceRequest.ID,
	) {
		t.Fatalf("workspace limit cancellation audit %q is missing", workspaceRequest.ID)
	}

}

func TestControlUnbindIdentitySerializesAuthentication(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")

	if err := service.Admin.BindIdentity(ctx, owner.Account.ID, admin.AuthIdentityParams{
		Provider:    "backup",
		Subject:     "owner-backup",
		DisplayName: "Owner backup",
	}); err != nil {
		t.Fatalf("bind backup identity: %v", err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open trigger database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	delayFunction := fmt.Sprintf(`
		CREATE OR REPLACE FUNCTION control_test_delay_identity_delete()
		RETURNS trigger
		LANGUAGE plpgsql
		AS $$
		BEGIN
			IF OLD.account_id = '%s' AND OLD.provider = 'test' THEN
				PERFORM pg_sleep(0.4);
			END IF;
			RETURN OLD;
		END
		$$
	`, owner.Account.ID)
	if _, err := db.Exec(delayFunction); err != nil {
		t.Fatalf("create identity delay function: %v", err)
	}
	if _, err := db.Exec(`
		CREATE TRIGGER control_test_delay_identity_delete
		BEFORE DELETE ON control_identity
		FOR EACH ROW
		EXECUTE FUNCTION control_test_delay_identity_delete()
	`); err != nil {
		t.Fatalf("create identity delay trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TRIGGER IF EXISTS control_test_delay_identity_delete ON control_identity")
		_, _ = db.Exec("DROP FUNCTION IF EXISTS control_test_delay_identity_delete()")
	})

	unbound := make(chan error, 1)
	go func() {
		rows, err := service.Admin.UnbindIdentity(ctx, owner.Account.ID, "test")
		if err == nil && rows != 1 {
			err = fmt.Errorf("unbind rows = %d, want 1", rows)
		}
		unbound <- err
	}()

	time.Sleep(100 * time.Millisecond)
	_, authErr := service.Admin.CompleteAuth(ctx, authParams("owner"))
	if err := <-unbound; err != nil {
		t.Fatalf("unbind identity: %v", err)
	}
	if !errors.Is(authErr, repository.ErrInviteRequired) {
		t.Fatalf("authentication through revoked identity error = %v", authErr)
	}

}

func TestControlBindIdentityRejectsIdentityOwnedByAnotherAccount(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	member := inviteAccount(t, service, owner.Account.ID, "member")

	err := service.Admin.BindIdentity(ctx, member.Account.ID, admin.AuthIdentityParams{
		Provider:    "test",
		Subject:     "owner",
		DisplayName: "Owner identity",
	})
	if !errors.Is(err, repository.ErrForbidden) {
		t.Fatalf("bind identity owned by another account error = %v", err)
	}

}

func TestControlDuplicatePendingLimitRequestsReturnDomainError(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	workspace := createWorkspace(t, service, owner.Account.ID, "duplicate-limits")

	if _, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		owner.Account.ID,
		2,
		"Need another workspace",
	); err != nil {
		t.Fatalf("request workspace limit: %v", err)
	}
	if _, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		owner.Account.ID,
		3,
		"Need more workspaces",
	); !errors.Is(err, repository.ErrLimitRequest) {
		t.Fatalf("duplicate workspace limit request error = %v", err)
	}

	if _, err := service.Admin.RequestEmployeeLimit(
		ctx,
		owner.Account.ID,
		workspace.ID,
		20,
		"Need more employees",
	); err != nil {
		t.Fatalf("request employee limit: %v", err)
	}
	if _, err := service.Admin.RequestEmployeeLimit(
		ctx,
		owner.Account.ID,
		workspace.ID,
		30,
		"Need even more employees",
	); !errors.Is(err, repository.ErrLimitRequest) {
		t.Fatalf("duplicate employee limit request error = %v", err)
	}

}

func TestControlResolveLimitRequestLocksScopeBeforeRequest(t *testing.T) {
	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "owner")
	workspace := createWorkspace(t, service, owner.Account.ID, "limit-lock-order")

	db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
	if err != nil {
		t.Fatalf("open lock database: %v", err)
	}
	defer db.Close()

	accountRequest, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		owner.Account.ID,
		2,
		"account lock order",
	)
	if err != nil {
		t.Fatalf("request workspace limit: %v", err)
	}

	accountTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin account scope transaction: %v", err)
	}
	defer accountTx.Rollback()

	if _, err := accountTx.ExecContext(
		ctx,
		"SELECT account_id FROM control_platform_member WHERE account_id = $1 FOR UPDATE",
		owner.Account.ID,
	); err != nil {
		t.Fatalf("lock platform member: %v", err)
	}

	accountResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.ResolveLimitRequest(ctx, admin.ResolveLimitRequestParams{
			ActorID:       owner.Account.ID,
			RequestID:     accountRequest.ID,
			Approved:      true,
			ApprovedLimit: 2,
		})
		accountResult <- err
	}()

	waitForControlBlockedQuery(t, db, "control_platform_member")

	if _, err := accountTx.ExecContext(
		ctx,
		"SELECT id FROM control_limit_request WHERE id = $1 FOR UPDATE",
		accountRequest.ID,
	); err != nil {
		t.Fatalf("account limit request was locked before member: %v", err)
	}
	if err := accountTx.Commit(); err != nil {
		t.Fatalf("release account scope locks: %v", err)
	}
	if err := <-accountResult; err != nil {
		t.Fatalf("resolve account limit after releasing member: %v", err)
	}

	employeeRequest, err := service.Admin.RequestEmployeeLimit(
		ctx,
		owner.Account.ID,
		workspace.ID,
		20,
		"workspace lock order",
	)
	if err != nil {
		t.Fatalf("request employee limit: %v", err)
	}

	workspaceTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin workspace scope transaction: %v", err)
	}
	defer workspaceTx.Rollback()

	if _, err := workspaceTx.ExecContext(
		ctx,
		"SELECT id FROM control_workspace WHERE id = $1 FOR UPDATE",
		workspace.ID,
	); err != nil {
		t.Fatalf("lock workspace: %v", err)
	}

	employeeResult := make(chan error, 1)
	go func() {
		_, err := service.Admin.ResolveLimitRequest(ctx, admin.ResolveLimitRequestParams{
			ActorID:       owner.Account.ID,
			RequestID:     employeeRequest.ID,
			Approved:      true,
			ApprovedLimit: 20,
		})
		employeeResult <- err
	}()

	waitForControlBlockedQuery(t, db, "control_workspace")

	if _, err := workspaceTx.ExecContext(
		ctx,
		"SELECT id FROM control_limit_request WHERE id = $1 FOR UPDATE",
		employeeRequest.ID,
	); err != nil {
		t.Fatalf("employee limit request was locked before workspace: %v", err)
	}
	if err := workspaceTx.Commit(); err != nil {
		t.Fatalf("release workspace scope locks: %v", err)
	}
	if err := <-employeeResult; err != nil {
		t.Fatalf("resolve employee limit after releasing workspace: %v", err)
	}
}

func waitForControlBlockedQuery(t *testing.T, db *sql.DB, relation string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM pg_stat_activity
WHERE datname = current_database()
  AND wait_event_type = 'Lock'
  AND query LIKE '%' || $1 || '%'`, relation).Scan(&waiting); err != nil {
			t.Fatalf("inspect blocked control query: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no blocked control query for relation %s", relation)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestControlWorkspaceInviteUsesScopeFirstLockOrder(t *testing.T) {

	t.Run("revoke", func(t *testing.T) {
		service := newControlTestService(t)
		ctx := context.Background()
		owner := initializeControl(t, service, "owner")
		workspace := createWorkspace(t, service, owner.Account.ID, "revoke-lock-order")
		invite, _, err := service.Admin.CreateWorkspaceInvite(
			ctx,
			admin.CreateInviteParams{
				ActorID:     owner.Account.ID,
				WorkspaceID: workspace.ID,
			},
		)
		if err != nil {
			t.Fatalf("create workspace invite: %v", err)
		}

		db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
		if err != nil {
			t.Fatalf("open lock database: %v", err)
		}
		defer db.Close()

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin workspace lock: %v", err)
		}
		defer tx.Rollback()

		var lockedWorkspaceID string
		if err := tx.QueryRowContext(
			ctx,
			"SELECT id FROM control_workspace WHERE id = $1 FOR UPDATE",
			workspace.ID,
		).Scan(&lockedWorkspaceID); err != nil {
			t.Fatalf("lock workspace: %v", err)
		}

		opCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		revoked := make(chan error, 1)
		go func() {
			_, err := service.Admin.RevokeInvite(opCtx, owner.Account.ID, invite.ID)
			revoked <- err
		}()

		time.Sleep(100 * time.Millisecond)
		if _, err := tx.ExecContext(
			opCtx,
			"UPDATE control_invite SET created_at = created_at WHERE id = $1",
			invite.ID,
		); err != nil {
			t.Fatalf("touch invite while workspace is locked: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit workspace lock: %v", err)
		}
		if err := <-revoked; err != nil {
			t.Fatalf("revoke invite after workspace lock: %v", err)
		}
	})

	t.Run("accept", func(t *testing.T) {
		service := newControlTestService(t)
		ctx := context.Background()
		owner := initializeControl(t, service, "owner")
		inviteAccount(t, service, owner.Account.ID, "target")
		workspace := createWorkspace(t, service, owner.Account.ID, "accept-lock-order")
		invite, token, err := service.Admin.CreateWorkspaceInvite(
			ctx,
			admin.CreateInviteParams{
				ActorID:     owner.Account.ID,
				WorkspaceID: workspace.ID,
			},
		)
		if err != nil {
			t.Fatalf("create workspace invite: %v", err)
		}

		db, err := sql.Open("pgx", controlPostgresDSN(controlTestDatabase))
		if err != nil {
			t.Fatalf("open lock database: %v", err)
		}
		defer db.Close()

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin workspace lock: %v", err)
		}
		defer tx.Rollback()

		var lockedWorkspaceID string
		if err := tx.QueryRowContext(
			ctx,
			"SELECT id FROM control_workspace WHERE id = $1 FOR UPDATE",
			workspace.ID,
		).Scan(&lockedWorkspaceID); err != nil {
			t.Fatalf("lock workspace: %v", err)
		}

		opCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		authenticated := make(chan error, 1)
		go func() {
			params := authParams("target")
			params.InviteToken = token
			_, err := service.Admin.CompleteAuth(opCtx, params)
			authenticated <- err
		}()

		time.Sleep(100 * time.Millisecond)
		if _, err := tx.ExecContext(
			opCtx,
			"UPDATE control_invite SET created_at = created_at WHERE id = $1",
			invite.ID,
		); err != nil {
			t.Fatalf("touch invite while workspace is locked: %v", err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit workspace lock: %v", err)
		}
		if err := <-authenticated; err != nil {
			t.Fatalf("accept invite after workspace lock: %v", err)
		}
	})

}

func TestControlCreateMethodsReturnPersistedModelsAndDomainConflicts(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "persisted-create-owner")

	if owner.Session.CreatedAt.IsZero() || owner.Session.LastUsedAt.IsZero() {
		t.Fatalf("initialized session timestamps = %#v", owner.Session)
	}

	workspace := createWorkspace(t, service, owner.Account.ID, "persisted-create")
	if workspace.CreatedAt.IsZero() || workspace.UpdatedAt.IsZero() {
		t.Fatalf("created workspace timestamps = %#v", workspace)
	}

	globalRole, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     "persisted_global",
		Title:    "Persisted global",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create global role: %v", err)
	}
	if globalRole.CreatedAt.IsZero() || globalRole.UpdatedAt.IsZero() {
		t.Fatalf("created global role timestamps = %#v", globalRole)
	}

	workspaceRole, err := service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "persisted_workspace",
		Title:       "Persisted workspace",
		Position:    10,
	})
	if err != nil {
		t.Fatalf("create workspace role: %v", err)
	}
	if workspaceRole.CreatedAt.IsZero() || workspaceRole.UpdatedAt.IsZero() {
		t.Fatalf("created workspace role timestamps = %#v", workspaceRole)
	}

	_, err = service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Title:    "Missing code",
		Position: 20,
	})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("global role without code error = %v", err)
	}
	_, err = service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        "missing_title",
		Position:    20,
	})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("workspace role without title error = %v", err)
	}
	if _, err := service.Admin.UpdateWorkspace(ctx, admin.UpdateWorkspaceParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Title:       workspace.Title,
	}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("workspace update without slug error = %v", err)
	}
	if _, err := service.Admin.UpdateGlobalRole(ctx, admin.UpdateRoleParams{
		ActorID:  owner.Account.ID,
		ID:       globalRole.ID,
		Position: globalRole.Position,
	}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("global role update without title error = %v", err)
	}
	if _, err := service.Admin.UpdateWorkspaceRole(ctx, admin.UpdateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		ID:          workspaceRole.ID,
		Position:    workspaceRole.Position,
	}); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("workspace role update without title error = %v", err)
	}

	_, err = service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  owner.Account.ID,
		Code:     globalRole.Code,
		Title:    "Duplicate global",
		Position: 20,
	})
	if !errors.Is(err, repository.ErrAlreadyExists) {
		t.Fatalf("duplicate global role error = %v", err)
	}

	_, err = service.Admin.CreateWorkspaceRole(ctx, admin.CreateRoleParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: workspace.ID,
		Code:        workspaceRole.Code,
		Title:       "Duplicate workspace",
		Position:    20,
	})
	if !errors.Is(err, repository.ErrAlreadyExists) {
		t.Fatalf("duplicate workspace role error = %v", err)
	}

	request, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		owner.Account.ID,
		2,
		"test duplicate workspace slug",
	)
	if err != nil {
		t.Fatalf("request workspace limit: %v", err)
	}
	if _, err := service.Admin.ResolveLimitRequest(ctx, admin.ResolveLimitRequestParams{
		ActorID:       owner.Account.ID,
		RequestID:     request.ID,
		Approved:      true,
		ApprovedLimit: 2,
	}); err != nil {
		t.Fatalf("approve workspace limit: %v", err)
	}

	_, err = service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: owner.Account.ID,
		ID:      uuid.NewString(),
		Slug:    workspace.Slug,
		Title:   "Duplicate workspace",
	})
	if !errors.Is(err, repository.ErrAlreadyExists) {
		t.Fatalf("duplicate workspace error = %v", err)
	}

	second := createWorkspace(t, service, owner.Account.ID, "persisted-create-second")
	_, err = service.Admin.UpdateWorkspace(ctx, admin.UpdateWorkspaceParams{
		ActorID:     owner.Account.ID,
		WorkspaceID: second.ID,
		Slug:        workspace.Slug,
		Title:       second.Title,
	})
	if !errors.Is(err, repository.ErrAlreadyExists) {
		t.Fatalf("duplicate workspace update error = %v", err)
	}

}

func TestControlScopeDeactivationCancelsPendingLimitRequests(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "limit-cleanup-owner")
	member := inviteAccount(t, service, owner.Account.ID, "limit-cleanup-member")

	accountRequest, err := service.Admin.RequestWorkspaceLimit(
		ctx,
		member.Account.ID,
		2,
		"account limit cleanup",
	)
	if err != nil {
		t.Fatalf("request account workspace limit: %v", err)
	}
	if _, err := service.Admin.RemovePlatformMember(
		ctx,
		owner.Account.ID,
		member.Account.ID,
	); err != nil {
		t.Fatalf("remove platform member: %v", err)
	}

	workspace := createWorkspace(t, service, owner.Account.ID, "limit-cleanup-workspace")
	workspaceRequest, err := service.Admin.RequestEmployeeLimit(
		ctx,
		owner.Account.ID,
		workspace.ID,
		11,
		"workspace limit cleanup",
	)
	if err != nil {
		t.Fatalf("request employee limit: %v", err)
	}
	if _, err := service.Admin.ArchiveWorkspace(ctx, owner.Account.ID, workspace.ID); err != nil {
		t.Fatalf("archive workspace: %v", err)
	}

	requests, err := service.Admin.ListLimitRequests(
		ctx,
		owner.Account.ID,
		"cancelled",
		admin.Page{Limit: 100},
	)
	if err != nil {
		t.Fatalf("list cancelled limit requests: %v", err)
	}
	assertLimitRequestStatus(t, requests, accountRequest.ID, "cancelled")
	assertLimitRequestStatus(t, requests, workspaceRequest.ID, "cancelled")

	if _, err := service.Admin.ResolveLimitRequest(ctx, admin.ResolveLimitRequestParams{
		ActorID:       owner.Account.ID,
		RequestID:     accountRequest.ID,
		Approved:      true,
		ApprovedLimit: 2,
	}); !errors.Is(err, repository.ErrLimitRequest) {
		t.Fatalf("resolve cancelled account limit request error = %v", err)
	}
	if _, err := service.Admin.ResolveLimitRequest(ctx, admin.ResolveLimitRequestParams{
		ActorID:       owner.Account.ID,
		RequestID:     workspaceRequest.ID,
		Approved:      true,
		ApprovedLimit: 11,
	}); !errors.Is(err, repository.ErrLimitRequest) {
		t.Fatalf("resolve cancelled workspace limit request error = %v", err)
	}

}

func TestControlTypedStatusRejectsUnknownValues(t *testing.T) {

	service := newControlTestService(t)
	ctx := context.Background()
	owner := initializeControl(t, service, "typed-status-owner")

	if _, err := service.Admin.ListLimitRequests(
		ctx,
		owner.Account.ID,
		controlmodel.LimitRequestStatus("unknown"),
		admin.Page{Limit: 10},
	); !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("unknown limit request status error = %v", err)
	}

	err := service.Internal.AppendAudit(ctx, internalapi.AuditEventParams{
		Scope:      internalapi.ScopeGlobal,
		ActorID:    owner.Account.ID,
		MethodKey:  "control.test.invalid_audit_result",
		TargetType: "test",
		TargetID:   "invalid-result",
		Result:     controlmodel.AuditResult("unknown"),
	})
	if !errors.Is(err, repository.ErrInvalidArgument) {
		t.Fatalf("unknown audit result error = %v", err)
	}

}

func assertLimitRequestStatus(
	t testing.TB,
	requests []admin.LimitRequestModel,
	requestID string,
	want controlmodel.LimitRequestStatus,
) {

	t.Helper()

	for _, request := range requests {
		if request.ID != requestID {
			continue
		}
		if request.Status != want {
			t.Fatalf("limit request %q status = %q, want %q", requestID, request.Status, want)
		}

		return
	}

	t.Fatalf("limit request %q is missing", requestID)

}

func auditContainsTarget(
	events []admin.AuditEventModel,
	methodKey string,
	targetID string,
) bool {

	for _, event := range events {
		if event.MethodKey == methodKey && event.TargetID == targetID {
			return true
		}
	}

	return false

}

func newControlTestService(t testing.TB) *control.Control {

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
	client, err := sqlwrap.New(db)
	if err != nil {
		t.Fatalf("new sql client: %v", err)
	}
	repo := repository.New(client)
	if err := repo.Bootstrap(context.Background()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	service, err := control.NewWithDatabase(
		context.Background(),
		db,
		control.Options{
			CacheEnabled:        true,
			CacheL1Delay:        time.Minute,
			SecretEncryptionKey: controlTestSecretEncryptionKey,
		},
	)
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

func initializeControl(
	t testing.TB,
	service *control.Control,
	subject string,
) admin.AuthResult {

	t.Helper()

	result, err := service.Admin.Initialize(context.Background(), authParams(subject))
	if err != nil {
		t.Fatalf("initialize control: %v", err)
	}

	return result

}

func inviteAccount(
	t testing.TB,
	service *control.Control,
	ownerID string,
	subject string,
) admin.AuthResult {

	t.Helper()

	_, token, err := service.Admin.CreateGlobalInvite(
		context.Background(),
		admin.CreateInviteParams{ActorID: ownerID},
	)
	if err != nil {
		t.Fatalf("create global invite: %v", err)
	}
	params := authParams(subject)
	params.InviteToken = token
	result, err := service.Admin.CompleteAuth(context.Background(), params)
	if err != nil {
		t.Fatalf("accept global invite: %v", err)
	}

	return result

}

func createWorkspace(
	t testing.TB,
	service *control.Control,
	actorID string,
	slug string,
) admin.WorkspaceModel {

	t.Helper()

	workspace, err := service.Admin.CreateWorkspace(
		context.Background(),
		admin.CreateWorkspaceParams{
			ActorID: actorID,
			ID:      uuid.NewString(),
			Slug:    slug,
			Title:   slug,
		},
	)
	if err != nil {
		t.Fatalf("create workspace %q: %v", slug, err)
	}

	return workspace

}

func createWorkspaceWithGlobalPermission(
	t testing.TB,
	service *control.Control,
	ownerID string,
	accountID string,
	slug string,
) admin.WorkspaceModel {

	t.Helper()
	ctx := context.Background()

	role, err := service.Admin.CreateGlobalRole(ctx, admin.CreateRoleParams{
		ActorID:  ownerID,
		Code:     "creator_" + slug,
		Title:    "Creator",
		Position: 10,
	})
	if err != nil {
		t.Fatalf("create creator role: %v", err)
	}
	if err := service.Admin.ReplaceGlobalRolePermissions(
		ctx,
		admin.ReplaceRolePermissionsParams{
			ActorID:    ownerID,
			RoleID:     role.ID,
			MethodKeys: []string{"control.global.workspace.create"},
		},
	); err != nil {
		t.Fatalf("grant create workspace: %v", err)
	}
	if err := service.Admin.AssignGlobalRole(ctx, admin.SetRoleMemberParams{
		ActorID:   ownerID,
		AccountID: accountID,
		RoleID:    role.ID,
	}); err != nil {
		t.Fatalf("assign creator role: %v", err)
	}

	return createWorkspace(t, service, accountID, slug)

}

func inviteIntoWorkspace(
	t testing.TB,
	service *control.Control,
	ownerID string,
	workspaceID string,
	accountID string,
	subject string,
) {

	t.Helper()

	inviteIntoWorkspaceWithRole(
		t,
		service,
		ownerID,
		workspaceID,
		accountID,
		subject,
		"",
	)

}

func inviteIntoWorkspaceWithRole(
	t testing.TB,
	service *control.Control,
	ownerID string,
	workspaceID string,
	accountID string,
	subject string,
	roleID string,
) {

	t.Helper()

	roleIDs := []string(nil)
	if roleID != "" {
		roleIDs = []string{roleID}
	}
	_, token, err := service.Admin.CreateWorkspaceInvite(
		context.Background(),
		admin.CreateInviteParams{
			ActorID:     ownerID,
			WorkspaceID: workspaceID,
			RoleIDs:     roleIDs,
		},
	)
	if err != nil {
		t.Fatalf("create workspace invite: %v", err)
	}
	params := authParams(subject)
	params.InviteToken = token
	result, err := service.Admin.CompleteAuth(context.Background(), params)
	if err != nil {
		t.Fatalf("accept workspace invite: %v", err)
	}
	if result.Account.ID != accountID {
		t.Fatalf("workspace invite account = %q, want %q", result.Account.ID, accountID)
	}

}

func authParams(subject string) admin.AuthIdentityParams {

	return admin.AuthIdentityParams{
		Provider:    "test",
		Subject:     subject,
		DisplayName: subject,
		IP:          "127.0.0.1",
		UserAgent:   "control-test",
		ExpiresAt:   time.Now().Add(time.Hour),
	}

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

func enableControlTwoFactor(
	t testing.TB,
	service *control.Control,
	accountID string,
) (string, []string) {

	t.Helper()

	setup, err := service.Admin.BeginTwoFactor(
		context.Background(),
		accountID,
		"Elum control test",
	)
	if err != nil {
		t.Fatalf("begin two factor: %v", err)
	}
	backupCodes, err := service.Admin.ConfirmTwoFactor(
		context.Background(),
		accountID,
		controlTestTOTP(setup.Secret, time.Now()),
	)
	if err != nil {
		t.Fatalf("confirm two factor: %v", err)
	}
	if len(backupCodes) == 0 {
		t.Fatal("two factor backup codes are empty")
	}

	return setup.Secret, backupCodes

}

func platformMemberStatus(
	items []admin.PlatformMemberModel,
	accountID string,
) controlmodel.MembershipStatus {

	for _, item := range items {
		if item.AccountID == accountID {
			return item.Status
		}
	}

	return controlmodel.MembershipStatus("")

}

func accessCatalogContains(items []admin.AccessGroupModel, methodKey string) bool {

	for _, service := range items {
		for _, group := range service.Groups {
			for _, access := range group.Accesses {
				if access.Key == methodKey {
					return true
				}
			}
		}
	}

	return false

}

func controlTestTOTP(secret string, now time.Time) string {

	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).
		DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}

	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))

	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 0x0f)
	value := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])

	return fmt.Sprintf("%06d", value%1_000_000)

}
