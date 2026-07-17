package vkma

import (
	"context"

	"github.com/elum2b/services/internal/utils/contextutil"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"

	"github.com/elum2b/services/payment/repository"
)

const (
	ProviderCode = "vkma"
	AssetCode    = "VOTE"
	PlatformID   = int64(1)
)

type VKMA struct {
	repository *repository.PaymentRepository
	rootCtx    context.Context
}

func New(ctx context.Context, db *sqlwrap.Client) *VKMA {
	return NewWithOptions(ctx, db, repository.Options{})
}

func NewWithOptions(ctx context.Context, db *sqlwrap.Client, options repository.Options) *VKMA {
	repo, err := repository.NewPreparedPaymentRepositoryWithOptions(context.Background(), db, options)
	if err == nil {
		return &VKMA{repository: repo, rootCtx: contextutil.Normalize(ctx)}
	}
	return &VKMA{
		repository: repository.NewPaymentRepositoryWithOptions(db, options),
		rootCtx:    contextutil.Normalize(ctx),
	}
}

func (a *VKMA) Close() error {
	if a == nil || a.repository == nil {
		return nil
	}
	return a.repository.Close()
}

func (a *VKMA) withContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return contextutil.Merge(a.rootCtx, ctx)
}

type ItemResponse struct {
	Title      string `json:"title"`
	PhotoURL   string `json:"photo_url,omitempty"`
	Price      uint64 `json:"price"`
	ItemID     string `json:"item_id"`
	Expiration uint64 `json:"expiration"`
}

type ChargeableResponse struct {
	AppOrderID uint64 `json:"app_order_id"`
	OrderID    int    `json:"order_id"`
}

type SubscriptionStatusResponse struct {
	AppOrderID     *uint64 `json:"app_order_id,omitempty"`
	SubscriptionID int     `json:"subscription_id"`
	Status         string  `json:"status"`
}
