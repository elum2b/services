package control_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/elum2b/services/control"
	"github.com/elum2b/services/control/repository"
	"github.com/elum2b/services/control/service/admin"
	"github.com/elum2b/services/control/service/internalapi"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const controlBenchmarkDatabase = "control_bench"

func BenchmarkControlServiceMethods(b *testing.B) {

	bench := newControlBenchmark(b)
	defer bench.close()

	b.Run("Admin.CompleteAuth/existing", func(b *testing.B) {
		params := authParams("benchmark-owner")
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := bench.service.Admin.CompleteAuth(bench.ctx, params); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Admin.ValidateSession", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := bench.service.Admin.ValidateSession(
				bench.ctx,
				bench.owner.SessionToken,
				"127.0.0.1",
			); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Admin.ListWorkspaces", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := bench.service.Admin.ListWorkspaces(
				bench.ctx,
				bench.owner.Account.ID,
				admin.Page{Limit: 20},
			); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Internal.CheckGlobalAccess", func(b *testing.B) {
		request := internalapi.GlobalAccessRequest{
			AccountID: bench.owner.Account.ID,
			MethodKey: "control.global.workspace.create",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := bench.service.Internal.CheckGlobalAccess(bench.ctx, request); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Internal.CheckWorkspaceAccess", func(b *testing.B) {
		request := internalapi.WorkspaceAccessRequest{
			AccountID:   bench.owner.Account.ID,
			WorkspaceID: bench.workspace.ID,
			MethodKey:   "control.workspace.update",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			if _, err := bench.service.Internal.CheckWorkspaceAccess(bench.ctx, request); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("Admin.CreateWorkspaceInvite", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			b.StartTimer()
			invite, _, err := bench.service.Admin.CreateWorkspaceInvite(
				bench.ctx,
				admin.CreateInviteParams{
					ActorID:     bench.owner.Account.ID,
					WorkspaceID: bench.workspace.ID,
				},
			)
			b.StopTimer()
			if err != nil {
				b.Fatal(err)
			}
			if _, err := bench.service.Admin.RevokeInvite(
				bench.ctx,
				bench.owner.Account.ID,
				invite.ID,
			); err != nil {
				b.Fatal(err)
			}
		}
	})
}

type controlBenchmark struct {
	ctx       context.Context
	service   *control.Control
	db        *sql.DB
	client    *sqlwrap.Client
	repo      *repository.Repository
	owner     admin.AuthResult
	workspace admin.WorkspaceModel
}

func newControlBenchmark(b *testing.B) *controlBenchmark {

	b.Helper()
	ctx := context.Background()
	adminDB, err := sql.Open("pgx", controlPostgresDSN("postgres"))
	if err != nil {
		b.Fatal(err)
	}
	defer adminDB.Close()
	if _, err := adminDB.Exec(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		controlBenchmarkDatabase,
	); err != nil {
		b.Fatal(err)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", controlBenchmarkDatabase)); err != nil {
		b.Fatal(err)
	}
	if _, err := adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", controlBenchmarkDatabase)); err != nil {
		b.Fatal(err)
	}

	db, err := sql.Open("pgx", controlPostgresDSN(controlBenchmarkDatabase))
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
	service, err := control.NewWithDatabase(
		ctx,
		db,
		control.Options{SecretEncryptionKey: controlTestSecretEncryptionKey},
	)
	if err != nil {
		b.Fatal(err)
	}
	owner, err := service.Admin.Initialize(ctx, authParams("benchmark-owner"))
	if err != nil {
		b.Fatal(err)
	}
	workspace, err := service.Admin.CreateWorkspace(ctx, admin.CreateWorkspaceParams{
		ActorID: owner.Account.ID,
		ID:      uuid.NewString(),
		Slug:    "benchmark",
		Title:   "Benchmark",
	})
	if err != nil {
		b.Fatal(err)
	}

	return &controlBenchmark{
		ctx:       ctx,
		service:   service,
		db:        db,
		client:    client,
		repo:      repo,
		owner:     owner,
		workspace: workspace,
	}
}

func (b *controlBenchmark) close() {

	_ = b.service.Close()
	_ = b.repo.Close()
	_ = b.client.Close()
	_ = b.db.Close()

}

var _ = time.Second
