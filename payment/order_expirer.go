package payment

import (
	"context"
	"errors"
	"log"
	"time"
)

const (
	defaultOrderExpirationInterval = time.Minute
	defaultOrderExpirationAge      = time.Hour
	defaultOrderExpirationBatch    = int32(100)
)

func (a *Payment) startOrderExpirationWorker() {
	if a == nil || a.pricing == nil || a.rootCtx == nil || a.goroutines == nil {
		return
	}

	if a.orderExpirationInterval <= 0 {
		a.orderExpirationInterval = defaultOrderExpirationInterval
	}
	if a.orderExpirationAge <= 0 {
		a.orderExpirationAge = defaultOrderExpirationAge
	}
	if a.orderExpirationBatch <= 0 {
		a.orderExpirationBatch = defaultOrderExpirationBatch
	}

	a.goroutines.GoRestart(a.rootCtx, "payment.order_expirer", time.Second, func() {
		a.orderExpirationLoop()
	})
}

func (a *Payment) orderExpirationLoop() {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-a.rootCtx.Done():
			return
		case <-timer.C:
			if err := a.expireStaleOrders(a.rootCtx); err != nil &&
				!errors.Is(err, context.Canceled) &&
				!errors.Is(err, context.DeadlineExceeded) {
				log.Printf("payment order expiration: %v", err)
			}

			timer.Reset(a.orderExpirationInterval)
		}
	}
}

func (a *Payment) expireStaleOrders(ctx context.Context) error {
	for {
		count, err := a.pricing.ExpireStaleOrders(
			ctx,
			time.Now().UTC(),
			a.orderExpirationAge,
			a.orderExpirationBatch,
			a.plategaCredentials != nil,
		)
		if err != nil {
			return err
		}
		if count < int(a.orderExpirationBatch) {
			return nil
		}
	}
}
