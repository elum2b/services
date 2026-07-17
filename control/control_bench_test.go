package control_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	control "github.com/elum2b/services/control"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/control/service/admin"
	"github.com/elum2b/services/control/service/internalapi"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"strings"
	"testing"
	"time"
)

const (
	controlBenchmarkHost     = "localhost"
	controlBenchmarkPort     = 5432
	controlBenchmarkUser     = "postgres"
	controlBenchmarkPassword = "RBTX0DXKbagvCy2XCAi4qHt0cjeSD6bU"
	controlBenchmarkDatabase = "control_bench"
)

// These benchmarks deliberately use a dedicated PostgreSQL database.
func BenchmarkAdmin(b *testing.B) {
	run := func(name string, fn func(*testing.B, *controlBenchmark)) {
		b.Run(name, func(b *testing.B) {
			b.StopTimer()
			bench := newControlBenchmark(b)
			defer bench.close()
			b.StartTimer()
			fn(b, bench)
		})
	}

	run("CompleteAuthNewIdentity", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			params := admin.AuthIdentityParams{Provider: "benchmark", Subject: unique("auth"), DisplayName: "Benchmark", ExpiresAt: time.Now().Add(time.Hour)}
			b.StartTimer()
			_, err := bench.admin.CompleteAuth(bench.ctx, params)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("CompleteAuthExistingIdentity", func(b *testing.B, bench *controlBenchmark) {
		b.StopTimer()
		params := admin.AuthIdentityParams{Provider: "benchmark", Subject: unique("existing-auth"), DisplayName: "Benchmark", ExpiresAt: time.Now().Add(time.Hour)}
		_, err := bench.admin.CompleteAuth(bench.ctx, params)
		mustBenchmark(b, err)
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			_, err := bench.admin.CompleteAuth(bench.ctx, params)
			mustBenchmark(b, err)
		}
		b.StopTimer()
	})

	run("CompleteTwoFactor", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			accountID, setup := bench.newTwoFactorAccount(b)
			identity := admin.AuthIdentityParams{Provider: "benchmark-2fa", Subject: unique("2fa"), ExpiresAt: time.Now().Add(time.Hour)}
			mustBenchmark(b, bench.admin.BindIdentity(bench.ctx, accountID, identity))
			auth, err := bench.admin.CompleteAuth(bench.ctx, identity)
			mustBenchmark(b, err)
			if auth.Account.ID != accountID || !auth.TwoFactorRequired {
				b.Fatal("expected two-factor challenge")
			}
			b.StartTimer()
			_, err = bench.admin.CompleteTwoFactor(bench.ctx, auth.TwoFactorChallenge, benchmarkTOTP(setup.Secret, time.Now()), "127.0.0.1")
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})

	run("GetAccount", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error { _, err := bench.admin.GetAccount(bench.ctx, bench.actorID); return err })
	})
	run("ListIdentities", func(b *testing.B, bench *controlBenchmark) {
		auth := bench.completeAuth(b, "identities")
		bench.benchmarkRead(b, func() error { _, err := bench.admin.ListIdentities(bench.ctx, auth.Account.ID); return err })
	})
	run("BindIdentity", func(b *testing.B, bench *controlBenchmark) {
		account := bench.newAccount(b, "bind")
		for i := 0; i < b.N; i++ {
			params := admin.AuthIdentityParams{Provider: unique("provider"), Subject: unique("subject"), DisplayName: "Benchmark"}
			b.StartTimer()
			err := bench.admin.BindIdentity(bench.ctx, account, params)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("UnbindIdentity", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			auth := bench.completeAuth(b, "unbind")
			provider := unique("secondary")
			mustBenchmark(b, bench.admin.BindIdentity(bench.ctx, auth.Account.ID, admin.AuthIdentityParams{Provider: provider, Subject: unique("subject")}))
			b.StartTimer()
			_, err := bench.admin.UnbindIdentity(bench.ctx, auth.Account.ID, provider)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ValidateSession", func(b *testing.B, bench *controlBenchmark) {
		auth := bench.completeAuth(b, "session")
		bench.benchmarkRead(b, func() error { _, err := bench.admin.ValidateSession(bench.ctx, auth.SessionToken, ""); return err })
	})
	run("ListSessions", func(b *testing.B, bench *controlBenchmark) {
		auth := bench.completeAuth(b, "sessions")
		bench.benchmarkRead(b, func() error { _, err := bench.admin.ListSessions(bench.ctx, auth.Account.ID); return err })
	})
	run("RevokeSession", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			auth := bench.completeAuth(b, "revoke-session")
			b.StartTimer()
			_, err := bench.admin.RevokeSession(bench.ctx, auth.Account.ID, auth.Session.ID)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("RevokeAllSessions", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			auth := bench.completeAuth(b, "revoke-all")
			bench.completeAuthForAccount(b, auth.Account.ID, unique("extra"))
			b.StartTimer()
			_, err := bench.admin.RevokeAllSessions(bench.ctx, auth.Account.ID, "")
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("BeginTwoFactor", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			account := bench.newAccount(b, "begin-2fa")
			b.StartTimer()
			_, err := bench.admin.BeginTwoFactor(bench.ctx, account, "Control benchmark")
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ConfirmTwoFactor", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			account := bench.newAccount(b, "confirm-2fa")
			setup, err := bench.admin.BeginTwoFactor(bench.ctx, account, "Control benchmark")
			mustBenchmark(b, err)
			b.StartTimer()
			_, err = bench.admin.ConfirmTwoFactor(bench.ctx, account, benchmarkTOTP(setup.Secret, time.Now()))
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("DisableTwoFactor", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			accountID, setup := bench.newTwoFactorAccount(b)
			b.StartTimer()
			_, err := bench.admin.DisableTwoFactor(bench.ctx, accountID, benchmarkTOTP(setup.Secret, time.Now()))
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})

	run("CreateWorkspace", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			id := uuid.NewString()
			b.StartTimer()
			_, err := bench.admin.CreateWorkspace(bench.ctx, admin.CreateWorkspaceParams{ID: id, ActorID: bench.actorID, Slug: id, Title: "Benchmark workspace"})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("GetWorkspace", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error { _, err := bench.admin.GetWorkspace(bench.ctx, bench.workspaceID); return err })
	})
	run("UpdateWorkspace", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			workspaceID := bench.newWorkspace(b)
			slug := unique("updated-workspace")
			b.StartTimer()
			_, err := bench.admin.UpdateWorkspace(bench.ctx, admin.UpdateWorkspaceParams{ActorID: bench.actorID, WorkspaceID: workspaceID, Slug: slug, Title: "Updated workspace", Status: "active"})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListWorkspaces", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error {
			_, err := bench.admin.ListWorkspaces(bench.ctx, bench.actorID, admin.Page{Limit: 100})
			return err
		})
	})
	run("ListMembers", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error {
			_, err := bench.admin.ListMembers(bench.ctx, bench.workspaceID, admin.Page{Limit: 100})
			return err
		})
	})
	run("RemoveMember", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			member := bench.newMember(b, nil)
			b.StartTimer()
			_, err := bench.admin.RemoveMember(bench.ctx, bench.actorID, bench.workspaceID, member)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("CreateInvite", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StartTimer()
			_, _, err := bench.admin.CreateInvite(bench.ctx, admin.CreateInviteParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("AcceptInvite", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			account := bench.newAccount(b, "invite-account")
			_, token, err := bench.admin.CreateInvite(bench.ctx, admin.CreateInviteParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID})
			mustBenchmark(b, err)
			b.StartTimer()
			_, err = bench.admin.AcceptInvite(bench.ctx, account, token)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListInvites", func(b *testing.B, bench *controlBenchmark) {
		_, _, err := bench.admin.CreateInvite(bench.ctx, admin.CreateInviteParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID})
		mustBenchmark(b, err)
		bench.benchmarkRead(b, func() error {
			_, err := bench.admin.ListInvites(bench.ctx, bench.workspaceID, admin.Page{Limit: 100})
			return err
		})
	})
	run("RevokeInvite", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			invite, _, err := bench.admin.CreateInvite(bench.ctx, admin.CreateInviteParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID})
			mustBenchmark(b, err)
			b.StartTimer()
			_, err = bench.admin.RevokeInvite(bench.ctx, bench.actorID, bench.workspaceID, invite.ID)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})

	run("CreateRole", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			params := bench.roleParams(unique("create-role"))
			b.StartTimer()
			_, err := bench.admin.CreateRole(bench.ctx, params)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListRoles", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error { _, err := bench.admin.ListRoles(bench.ctx, bench.workspaceID); return err })
	})
	run("UpdateRole", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role := bench.newRole(b)
			b.StartTimer()
			_, err := bench.admin.UpdateRole(bench.ctx, admin.UpdateRoleParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, ID: role.ID, Title: "Updated", Description: "Updated", Position: 10})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("DeleteRole", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role := bench.newRole(b)
			b.StartTimer()
			_, err := bench.admin.DeleteRole(bench.ctx, bench.actorID, bench.workspaceID, role.ID)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("SetRoleMember", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role, member := bench.newRole(b), bench.newMember(b, nil)
			b.StartTimer()
			err := bench.admin.SetRoleMember(bench.ctx, admin.SetRoleMemberParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, AccountID: member, RoleID: role.ID})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("RemoveRoleMember", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role, member := bench.newRole(b), bench.newMember(b, nil)
			mustBenchmark(b, bench.admin.SetRoleMember(bench.ctx, admin.SetRoleMemberParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, AccountID: member, RoleID: role.ID}))
			b.StartTimer()
			_, err := bench.admin.RemoveRoleMember(bench.ctx, admin.SetRoleMemberParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, AccountID: member, RoleID: role.ID})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("SetRolePermission", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role := bench.newRole(b)
			b.StartTimer()
			err := bench.admin.SetRolePermission(bench.ctx, admin.SetRolePermissionParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, RoleID: role.ID, MethodKey: bench.methodKey, Enabled: true})
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListRolePermissions", func(b *testing.B, bench *controlBenchmark) {
		role := bench.newRole(b)
		mustBenchmark(b, bench.admin.SetRolePermission(bench.ctx, admin.SetRolePermissionParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, RoleID: role.ID, MethodKey: bench.methodKey, Enabled: true}))
		bench.benchmarkRead(b, func() error {
			_, err := bench.admin.ListRolePermissions(bench.ctx, bench.workspaceID, role.ID)
			return err
		})
	})
	run("ClearRolePermissions", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			role := bench.newRole(b)
			mustBenchmark(b, bench.admin.SetRolePermission(bench.ctx, admin.SetRolePermissionParams{ActorID: bench.actorID, WorkspaceID: bench.workspaceID, RoleID: role.ID, MethodKey: bench.methodKey, Enabled: true}))
			b.StartTimer()
			_, err := bench.admin.ClearRolePermissions(bench.ctx, bench.actorID, bench.workspaceID, role.ID)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListMethods", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error { _, err := bench.admin.ListMethods(bench.ctx); return err })
	})
	run("GetMethod", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error { _, err := bench.admin.GetMethod(bench.ctx, bench.methodKey); return err })
	})
	run("AppendAudit", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			params := internalapi.AuditEventParams{WorkspaceID: bench.workspaceID, ActorID: bench.actorID, MethodKey: bench.methodKey, TargetType: "benchmark", TargetID: unique("audit"), Result: "succeeded", RequestID: unique("request")}
			b.StartTimer()
			err := bench.internal.AppendAudit(bench.ctx, params)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("ListAudit", func(b *testing.B, bench *controlBenchmark) {
		mustBenchmark(b, bench.internal.AppendAudit(bench.ctx, internalapi.AuditEventParams{WorkspaceID: bench.workspaceID, ActorID: bench.actorID, MethodKey: bench.methodKey, TargetType: "benchmark", TargetID: "list", Result: "succeeded"}))
		bench.benchmarkRead(b, func() error {
			_, err := bench.admin.ListAudit(bench.ctx, bench.workspaceID, admin.Page{Limit: 100})
			return err
		})
	})
}

func BenchmarkInternal(b *testing.B) {
	run := func(name string, fn func(*testing.B, *controlBenchmark)) {
		b.Run(name, func(b *testing.B) {
			b.StopTimer()
			bench := newControlBenchmark(b)
			defer bench.close()
			b.StartTimer()
			fn(b, bench)
		})
	}

	run("RegisterManifest", func(b *testing.B, bench *controlBenchmark) {
		for i := 0; i < b.N; i++ {
			values := []internalapi.MethodManifest{{Key: unique("manifest"), Service: "benchmark", GroupKey: "internal"}}
			b.StartTimer()
			err := bench.internal.RegisterManifest(bench.ctx, values)
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	run("CheckAccess", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error {
			_, err := bench.internal.CheckAccess(bench.ctx, internalapi.AccessRequest{AccountID: bench.actorID, WorkspaceID: bench.workspaceID, MethodKey: bench.methodKey})
			return err
		})
	})
	run("GetAuthorizedMethods", func(b *testing.B, bench *controlBenchmark) {
		bench.benchmarkRead(b, func() error {
			_, err := bench.internal.GetAuthorizedMethods(bench.ctx, bench.actorID, bench.workspaceID)
			return err
		})
	})
}

func BenchmarkControlLifecycle(b *testing.B) {
	bench := newControlBenchmark(b)
	defer bench.close()

	b.Run("NewWithDatabase", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StartTimer()
			instance, err := control.NewWithDatabase(context.Background(), bench.db, control.Options{MaxConnections: 32, CacheEnabled: true, CacheSize: 10_000})
			b.StopTimer()
			mustBenchmark(b, err)
			mustBenchmark(b, instance.Close())
		}
	})
	b.Run("IsReady", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if !bench.control.IsReady() {
				b.Fatal("control must be ready")
			}
		}
		b.StopTimer()
	})
	b.Run("Close", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StartTimer()
			instance, err := control.NewWithDatabase(context.Background(), bench.db, control.Options{MaxConnections: 32})
			mustBenchmark(b, err)
			err = instance.Close()
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
	b.Run("Run", func(b *testing.B) {
		params := control.DatabaseParams{User: controlBenchmarkUser, Password: controlBenchmarkPassword, Database: controlBenchmarkDatabase, Host: controlBenchmarkHost, Port: controlBenchmarkPort, Options: control.Options{MaxConnections: 32, CacheEnabled: true, CacheSize: 10_000}}
		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			instance := control.New(params)
			done := make(chan error, 1)
			b.StartTimer()
			go func() { done <- instance.Run(ctx) }()
			deadline := time.NewTimer(10 * time.Second)
			for !instance.IsReady() {
				select {
				case err := <-done:
					deadline.Stop()
					b.StopTimer()
					mustBenchmark(b, err)
					b.Fatal("control stopped before becoming ready")
				case <-deadline.C:
					b.StopTimer()
					b.Fatal("control Run did not become ready in 10 seconds")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			deadline.Stop()
			cancel()
			err := <-done
			b.StopTimer()
			mustBenchmark(b, err)
		}
	})
}

type controlBenchmark struct {
	ctx                  context.Context
	db                   *sql.DB
	client               *sqlwrap.Client
	control              *control.Control
	admin                *admin.Admin
	internal             *internalapi.Internal
	actorID, workspaceID string
	methodKey            string
}

func newControlBenchmark(b *testing.B) *controlBenchmark {
	b.Helper()
	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ensureControlBenchmarkDatabase(b, connectCtx)
	db, err := sql.Open("pgx", controlBenchmarkDSN(controlBenchmarkDatabase))
	if err != nil {
		b.Fatalf("open benchmark database: %v", err)
	}
	if err := db.PingContext(connectCtx); err != nil {
		_ = db.Close()
		b.Fatalf("ping benchmark database: %v", err)
	}
	client, err := sqlwrap.New(db, sqlwrap.Options{MaxConnections: 32, CacheEnabled: true, CacheSize: 10_000})
	if err != nil {
		_ = db.Close()
		b.Fatalf("create benchmark sql client: %v", err)
	}
	resetControlBenchmark(b, db)
	bootstrap := repository.New(client)
	if err := bootstrap.Bootstrap(context.Background()); err != nil {
		_ = client.Close()
		_ = db.Close()
		b.Fatalf("bootstrap benchmark schema: %v", err)
	}
	_ = bootstrap.Close()
	c, err := control.NewWithDatabase(context.Background(), db, control.Options{
		MaxConnections:      32,
		CacheEnabled:        true,
		CacheSize:           10_000,
		SecretEncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	if err != nil {
		_ = client.Close()
		_ = db.Close()
		b.Fatalf("create control: %v", err)
	}
	result := &controlBenchmark{ctx: context.Background(), db: db, client: client, control: c, admin: c.Admin, internal: c.Internal}
	result.actorID = result.newAccount(b, "owner")
	result.workspaceID = result.newWorkspace(b)
	result.methodKey = unique("method")
	mustBenchmark(b, result.internal.RegisterManifest(result.ctx, []internalapi.MethodManifest{
		{
			Key:      result.methodKey,
			Service:  "benchmark",
			GroupKey: "control",
		},
	}))
	return result
}

func ensureControlBenchmarkDatabase(b *testing.B, ctx context.Context) {
	b.Helper()
	adminDB, err := sql.Open("pgx", controlBenchmarkDSN("postgres"))
	if err != nil {
		b.Fatalf("open postgres admin database: %v", err)
	}
	defer adminDB.Close()
	var exists bool
	if err := adminDB.QueryRowContext(
		ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)",
		controlBenchmarkDatabase,
	).Scan(&exists); err != nil {
		b.Fatalf("check benchmark database: %v", err)
	}
	if exists {
		return
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", controlBenchmarkDatabase)); err != nil {
		b.Fatalf("create benchmark database: %v", err)
	}
}

func resetControlBenchmark(b *testing.B, db *sql.DB) {
	b.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		b.Fatalf("acquire benchmark database connection: %v", err)
	}
	defer conn.Close()
	statements := []string{
		`TRUNCATE TABLE
			control_audit_event,
			control_two_factor_challenge,
			control_two_factor,
			control_role_permission,
			control_workspace_invite_role,
			control_role_member,
			control_workspace_invite,
			control_workspace_member,
			control_role,
			control_method,
			control_workspace,
			control_identity,
			control_session,
			control_account
		RESTART IDENTITY CASCADE`,
	}
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			b.Fatalf("reset benchmark database: %v", err)
		}
	}
}

func controlBenchmarkDSN(database string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		controlBenchmarkUser,
		controlBenchmarkPassword,
		controlBenchmarkHost,
		controlBenchmarkPort,
		database,
	)
}

func (b *controlBenchmark) close() { _ = b.control.Close(); _ = b.client.Close(); _ = b.db.Close() }

func (b *controlBenchmark) benchmarkRead(tb *testing.B, fn func() error) {
	tb.Helper()
	tb.ResetTimer()
	for i := 0; i < tb.N; i++ {
		mustBenchmark(tb, fn())
	}
	tb.StopTimer()
}

func (b *controlBenchmark) newAccount(tb *testing.B, prefix string) string {
	tb.Helper()
	id := unique(prefix)
	_, err := b.admin.CreateAccount(b.ctx, id, "Benchmark "+prefix)
	mustBenchmark(tb, err)
	return id
}

func (b *controlBenchmark) newWorkspace(tb *testing.B) string {
	tb.Helper()
	id := uuid.NewString()
	_, err := b.admin.CreateWorkspace(b.ctx, admin.CreateWorkspaceParams{ID: id, ActorID: b.actorID, Slug: id, Title: "Benchmark workspace"})
	mustBenchmark(tb, err)
	return id
}

func (b *controlBenchmark) roleParams(code string) admin.CreateRoleParams {
	return admin.CreateRoleParams{ID: unique("role"), ActorID: b.actorID, WorkspaceID: b.workspaceID, Code: code, Title: "Benchmark role", Description: "Benchmark role", Position: 10}
}

func (b *controlBenchmark) newRole(tb *testing.B) admin.RoleModel {
	tb.Helper()
	role, err := b.admin.CreateRole(b.ctx, b.roleParams(unique("role-code")))
	mustBenchmark(tb, err)
	return role
}

func (b *controlBenchmark) newMember(tb *testing.B, roleIDs []string) string {
	tb.Helper()
	account := b.newAccount(tb, "member")
	_, token, err := b.admin.CreateInvite(b.ctx, admin.CreateInviteParams{ActorID: b.actorID, WorkspaceID: b.workspaceID, RoleIDs: roleIDs})
	mustBenchmark(tb, err)
	_, err = b.admin.AcceptInvite(b.ctx, account, token)
	mustBenchmark(tb, err)
	return account
}

func (b *controlBenchmark) completeAuth(tb *testing.B, prefix string) admin.AuthResult {
	tb.Helper()
	result, err := b.admin.CompleteAuth(b.ctx, admin.AuthIdentityParams{Provider: "benchmark-" + prefix, Subject: unique(prefix), DisplayName: "Benchmark", ExpiresAt: time.Now().Add(time.Hour)})
	mustBenchmark(tb, err)
	return result
}

func (b *controlBenchmark) completeAuthForAccount(tb *testing.B, accountID, subject string) {
	tb.Helper()
	identity := admin.AuthIdentityParams{Provider: unique("extra-provider"), Subject: subject, ExpiresAt: time.Now().Add(time.Hour)}
	mustBenchmark(tb, b.admin.BindIdentity(b.ctx, accountID, identity))
	_, err := b.admin.CompleteAuth(b.ctx, identity)
	mustBenchmark(tb, err)
}

func (b *controlBenchmark) newTwoFactorAccount(tb *testing.B) (string, admin.TwoFactorSetupModel) {
	tb.Helper()
	account := b.newAccount(tb, "two-factor")
	setup, err := b.admin.BeginTwoFactor(b.ctx, account, "Control benchmark")
	mustBenchmark(tb, err)
	_, err = b.admin.ConfirmTwoFactor(b.ctx, account, benchmarkTOTP(setup.Secret, time.Now()))
	mustBenchmark(tb, err)
	return account, setup
}

func mustBenchmark(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}

func unique(prefix string) string { return fmt.Sprintf("%s-%s", prefix, uuid.NewString()) }

func benchmarkTOTP(secret string, now time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(now.Unix()/30))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := int(sum[len(sum)-1] & 0x0f)
	value := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", value%1_000_000)
}
