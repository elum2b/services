package payment

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/elum2b/services/payment/adapters/platega"
)

const (
	defaultPlategaReconcileInterval     = 5 * time.Minute
	defaultPlategaReconcileMinAge       = 30 * time.Second
	defaultPlategaReconcileMissingAfter = 30 * time.Minute
	defaultPlategaReconcileBatch        = int32(1000)
)

func (a *Payment) startPlategaReconciliationWorker() {
	if a == nil || a.rootCtx == nil || a.goroutines == nil || a.Adapters == nil ||
		a.Adapters.Platega == nil || a.plategaCredentials == nil {
		return
	}
	if a.plategaReconcileInterval <= 0 {
		a.plategaReconcileInterval = defaultPlategaReconcileInterval
	}
	if a.plategaReconcileMinAge <= 0 {
		a.plategaReconcileMinAge = defaultPlategaReconcileMinAge
	}
	if a.plategaReconcileMissingAfter <= 0 {
		a.plategaReconcileMissingAfter = defaultPlategaReconcileMissingAfter
	}
	if a.plategaReconcileBatch <= 0 {
		a.plategaReconcileBatch = defaultPlategaReconcileBatch
	}

	a.goroutines.GoRestart(a.rootCtx, "payment.platega_reconciler", time.Second, func() {
		a.plategaReconciliationLoop()
	})
}

func (a *Payment) plategaReconciliationLoop() {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-a.rootCtx.Done():
			return
		case <-timer.C:
			if _, err := a.reconcilePlatega(a.rootCtx); err != nil &&
				!errors.Is(err, context.Canceled) &&
				!errors.Is(err, context.DeadlineExceeded) {
				log.Printf("payment platega reconciliation: %v", err)
			}
			timer.Reset(a.plategaReconcileInterval)
		}
	}
}

func (a *Payment) reconcilePlatega(ctx context.Context) (platega.ReconcileResult, error) {
	now := time.Now().UTC()

	return a.Adapters.Platega.ReconcilePending(ctx, platega.ReconcileParams{
		ResolveCredentials: a.plategaCredentials,
		CreatedTo:          now.Add(-a.plategaReconcileMinAge),
		Limit:              a.plategaReconcileBatch,
		MissingAfter:       a.plategaReconcileMissingAfter,
	})
}
