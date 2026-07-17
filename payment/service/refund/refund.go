package refund

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/repository"
)

type ProviderRefundFunc func(context.Context, ProviderRefundParams) (ProviderRefundResult, error)

type ProviderRefundOrder struct {
	ID                  uint64
	WorkspaceID         string
	AppID               int64
	PlatformID          int64
	PlatformUserID      string
	PayerPlatformID     *int64
	PayerPlatformUserID *string
	ProductID           string
}

type ProviderRefundAttempt struct {
	ID                uint64
	ProviderCode      string
	AssetCode         string
	AmountMinor       uint64
	ProviderPaymentID *string
	ProviderChargeID  *string
}

type ProviderRefundParams struct {
	Order          ProviderRefundOrder
	Attempt        ProviderRefundAttempt
	RefundID       uint64
	AmountMinor    uint64
	Reason         string
	ProviderParams any
}

type ProviderRefundResult struct {
	ProviderRefundID string
	Status           string
}

type Refund struct {
	repository *repository.PaymentRepository
	providers  map[string]ProviderRefundFunc
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client, providers map[string]ProviderRefundFunc) *Refund {
	return NewWithOptions(ctx, db, providers, repository.Options{})
}

func NewWithOptions(
	ctx context.Context,
	db *sqlwrap.Client,
	providers map[string]ProviderRefundFunc,
	options repository.Options,
) *Refund {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err == nil {
		return &Refund{
			repository: repo,
			providers:  providers,
			rootCtx:    contextutil.Normalize(ctx),
		}
	}
	return &Refund{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		providers:  providers,
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (a *Refund) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *Refund) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}
