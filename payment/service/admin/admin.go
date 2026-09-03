package admin

import (
	"context"
	"database/sql"
	"errors"

	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/internal/utils/contextutil"
	"github.com/elum2b/services/internal/utils/importexport/jobs"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/product"
	"github.com/elum2b/services/payment/service/refund"
)

type Admin struct {
	repository *repository.PaymentRepository
	callbacks  *callbackutil.Store
	products   *product.Product
	refunds    *refund.Refund
	rootCtx    context.Context
	jobs       *jobs.Manager
}

func New(ctx context.Context, db *sqlwrap.Client) *Admin {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
) *Admin {
	return NewWithServices(ctx, db, options, nil, nil)
}

func NewWithServices(
	ctx context.Context,
	db *sqlwrap.Client,
	options repository.Options,
	products *product.Product,
	refunds *refund.Refund,
) *Admin {
	return &Admin{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		callbacks: callbackutil.NewWithTable(
			db.DB(),
			callbackutil.PaymentTable,
		),
		products: products,
		refunds:  refunds,
		rootCtx:  contextutil.Normalize(ctx),
	}
}

func (a *Admin) Close() error {
	if a == nil {
		return nil
	}

	var err error

	if a.jobs != nil {
		a.jobs.Close()
	}

	if a.repository != nil {
		err = errors.Join(err, a.repository.Close())
	}

	if a.callbacks != nil {
		err = errors.Join(err, a.callbacks.Close())
	}

	return err
}

func (a *Admin) configureArchiveJobs(manager *jobs.Manager) { a.jobs = manager }

// ConfigureArchiveJobs attaches the persistent async ZIP queue to this Admin.
// The caller must bootstrap the jobs table before invoking it.
func (a *Admin) ConfigureArchiveJobs(db *sql.DB, archive jobs.Archive) error {
	manager, err := jobs.New(
		db,
		archive,
		archiveJobHandler{admin: a},
		jobs.Options{Service: "payment"},
	)
	if err != nil {
		return err
	}

	a.configureArchiveJobs(manager)

	return nil
}

func (a *Admin) StartArchiveJobs(ctx context.Context) bool {
	return a != nil && a.jobs != nil && a.jobs.Start(ctx)
}

func (a *Admin) withContext(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}
