package payment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	services "github.com/elum2b/services"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	"github.com/elum2b/services/payment/repository"
)

type BalanceBinding = repository.BalanceBindingKey
type BalanceChargeRequest = repository.BalanceChargeRequest
type BalanceChargeResult = repository.BalanceChargeResult
type BalanceChargeOutcome = repository.BalanceChargeOutcome
type BalanceChargeHandler = repository.BalanceChargeHandler
type BalancePurchase = repository.BalancePurchase
type BalancePurchaseStatus = repository.BalancePurchaseStatus
type PurchaseWithBalanceParams = repository.PurchaseWithBalanceParams
type GetBalancePurchaseParams = repository.GetBalancePurchaseParams
type BalanceDeliveryParams = repository.BalanceDeliveryParams
type BalanceDelivery = repository.BalanceDelivery

const (
	AssetKindInternalCurrency = repository.AssetKindInternalCurrency
	BalanceChargeSucceeded    = repository.BalanceChargeSucceeded
	BalanceChargeRejected     = repository.BalanceChargeRejected
	BalancePurchasePending    = repository.BalancePurchasePending
	BalancePurchaseProcessing = repository.BalancePurchaseProcessing
	BalancePurchaseSucceeded  = repository.BalancePurchaseSucceeded
	BalancePurchaseRejected   = repository.BalancePurchaseRejected
	balanceChargeTimeout      = 30 * time.Second
)

var (
	ErrBalanceRequestInvalid    = repository.ErrBalanceRequestInvalid
	ErrBalanceRequestConflict   = repository.ErrBalanceRequestConflict
	ErrBalanceUnsupported       = repository.ErrBalanceUnsupported
	ErrBalanceUnavailable       = repository.ErrBalanceUnavailable
	ErrBalanceInProgress        = repository.ErrBalanceInProgress
	ErrBalanceOperationConflict = repository.ErrBalanceOperationConflict
	ErrBalanceResultInvalid     = repository.ErrBalanceResultInvalid
)

func validateBalanceOptions(options Options) error {
	for name, handler := range options.BalanceHandlers {
		if !repository.ValidBalanceKey(name, 128) || handler == nil {
			return ErrBalanceRequestInvalid
		}
	}
	for key, handler := range options.BalanceBindings {
		if services.ValidateWorkspaceID(key.WorkspaceID) != nil || key.AppID <= 0 ||
			!repository.ValidBalanceKey(key.AssetCode, 32) {
			return ErrBalanceRequestInvalid
		}
		if options.BalanceHandlers[handler] == nil {
			return ErrBalanceUnavailable
		}
	}
	return nil
}

func (a *Payment) startBalanceWorker() {
	if len(a.balanceHandlers) == 0 {
		return
	}
	store := callbackutil.NewWithTable(a.client.DB(), repository.BalanceCallbackTable)
	worker := callbackutil.WithWorkerID("payment-balance:" + uuid.NewString())
	a.goroutines.Go("payment.balance", func() {
		for a.rootCtx.Err() == nil {
			_ = store.On(a.rootCtx, a.deliverBalanceCharge, worker, callbackutil.WithSourceService("payment"))
			// Retry leasing after temporary database errors.
			timer := time.NewTimer(time.Second)
			select {
			case <-a.rootCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	})
}

func (a *Payment) deliverBalanceCharge(event callbackutil.Context) error {
	if event.EventType != repository.BalanceChargeEvent {
		return ErrBalanceResultInvalid
	}
	request, handler, err := a.pricing.BeginBalanceCharge(event, event.EventID, a.balanceHandlers)
	if err != nil {
		return err
	}
	if request != nil {
		ctx, cancel := context.WithTimeout(event, balanceChargeTimeout)
		defer cancel()
		result, err := handler(ctx, *request)
		if err != nil {
			if errors.Is(err, ErrBalanceOperationConflict) {
				if flagErr := a.pricing.FlagBalanceDeliveryConflict(event, event.EventID); flagErr != nil {
					return flagErr
				}
			}
			return err
		}
		if err := a.pricing.FinishBalanceCharge(event, event.EventID, result); err != nil {
			return err
		}
	}
	err = event.Successful()
	if errors.Is(err, callbackutil.ErrNotLeased) {
		// Another worker acquired the lease; it will see the persisted result.
		return nil
	}
	return err
}
