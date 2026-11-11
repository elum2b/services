package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const (
	AssetKindInternalCurrency = "internal_currency"
	BalanceProviderCode       = "internal_balance"
	BalanceCallbackTable      = "payment_balance_clb_event"
	BalanceChargeEvent        = "payment.balance.charge_requested"
)

var (
	ErrBalanceRequestInvalid    = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment balance purchase parameters are invalid")
	ErrBalanceRequestConflict   = serviceerrors.New(serviceerrors.CodeConflict, "payment balance request id is already used for another purchase")
	ErrBalanceUnsupported       = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "payment method does not support this operation")
	ErrBalanceUnavailable       = serviceerrors.New(serviceerrors.CodeUnavailable, "payment balance handler or asset is unavailable")
	ErrBalanceInProgress        = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "payment balance debit is in progress")
	ErrBalanceOperationConflict = serviceerrors.New(serviceerrors.CodeConflict, "payment balance operation requires reconciliation")
	ErrBalanceResultInvalid     = serviceerrors.New(serviceerrors.CodeFailedPrecondition, "payment balance handler returned an invalid result")
)

type BalanceBindingKey struct {
	WorkspaceID string
	AppID       int64
	AssetCode   string
}

type PurchaseWithBalanceParams struct {
	Identity  services.Identity
	ProductID string
	AssetCode string
	Quantity  uint64
	RequestID string
	Locale    string
}

type GetBalancePurchaseParams struct {
	Identity  services.Identity
	OrderID   uint64
	RequestID string
}

type BalancePurchaseStatus string

const (
	BalancePurchasePending    BalancePurchaseStatus = "pending"
	BalancePurchaseProcessing BalancePurchaseStatus = "processing"
	BalancePurchaseSucceeded  BalancePurchaseStatus = "succeeded"
	BalancePurchaseRejected   BalancePurchaseStatus = "rejected"
)

type BalancePurchase struct {
	OrderID uint64                `json:"order_id"`
	Status  BalancePurchaseStatus `json:"status"`
	Reason  string                `json:"reason,omitempty"`
}

type BalanceChargeRequest struct {
	services.Identity
	OrderID      uint64            `json:"order_id"`
	AttemptID    uint64            `json:"attempt_id"`
	OperationKey string            `json:"operation_key"`
	ProductID    string            `json:"product_id"`
	AssetCode    string            `json:"asset_code"`
	Quantity     uint64            `json:"quantity"`
	AmountMinor  uint64            `json:"amount_minor"`
	Scale        uint16            `json:"scale"`
	Rewards      []services.Reward `json:"rewards"`
}

type BalanceChargeOutcome string

const (
	BalanceChargeSucceeded BalanceChargeOutcome = "succeeded"
	BalanceChargeRejected  BalanceChargeOutcome = "rejected"
)

type BalanceChargeResult struct {
	Outcome BalanceChargeOutcome `json:"outcome"`
	Reason  string               `json:"reason,omitempty"`
}

type BalanceChargeHandler func(context.Context, BalanceChargeRequest) (BalanceChargeResult, error)

type balanceChargeEvent struct {
	BalanceChargeRequest
	HandlerKey string `json:"handler_key"`
}

func ValidBalanceKey(value string, maxLen int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= maxLen &&
		utf8.ValidString(value) && !strings.ContainsRune(value, 0)
}

func balanceRequestScope(identity services.Identity, requestID string) paymentsqlc.GetBalancePurchaseByRequestParams {
	return paymentsqlc.GetBalancePurchaseByRequestParams{
		WorkspaceID: identity.WorkspaceID, AppID: identity.AppID, PlatformID: identity.PlatformID,
		PlatformUserID: identity.PlatformUserID, RequestID: requestID,
	}
}

func (r *PaymentRepository) PurchaseWithBalance(ctx context.Context, params PurchaseWithBalanceParams) (BalancePurchase, error) {
	identity := params.Identity
	if err := identity.Validate(); err != nil {
		return BalancePurchase{}, err
	}
	if !ValidBalanceKey(identity.PlatformUserID, 128) || !ValidBalanceKey(params.RequestID, 128) ||
		!ValidBalanceKey(params.ProductID, 64) || !ValidBalanceKey(params.AssetCode, 32) ||
		params.Quantity == 0 || params.Quantity > math.MaxInt64 {
		return BalancePurchase{}, ErrBalanceRequestInvalid
	}

	scope := balanceRequestScope(identity, params.RequestID)
	var purchase BalancePurchase
	err := r.inTransaction(ctx, func(ctx context.Context, tx *PaymentRepository) error {
		if err := tx.q.LockBalancePurchaseRequest(ctx, paymentsqlc.LockBalancePurchaseRequestParams(scope)); err != nil {
			return err
		}
		existing, err := tx.q.GetBalancePurchaseByRequest(ctx, scope)
		if err == nil {
			var saved BalanceChargeRequest
			if err := json.Unmarshal(existing.Snapshot, &saved); err != nil {
				return err
			}
			if saved.ProductID != params.ProductID || saved.AssetCode != params.AssetCode || saved.Quantity != params.Quantity {
				return ErrBalanceRequestConflict
			}
			purchase = mapBalancePurchase(existing)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		handlerKey := tx.balanceBindings[BalanceBindingKey{
			WorkspaceID: identity.WorkspaceID, AppID: identity.AppID, AssetCode: params.AssetCode,
		}]
		if handlerKey == "" {
			return ErrBalanceUnavailable
		}
		scale, err := tx.q.GetBalanceAssetForShare(ctx, params.AssetCode)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBalanceUnavailable
		}
		if err != nil {
			return err
		}

		order, err := tx.createOrder(ctx, OrderCreateParams{
			WorkspaceID: identity.WorkspaceID, AppID: identity.AppID, PlatformID: identity.PlatformID,
			PlatformUserID: identity.PlatformUserID, ProductID: params.ProductID, AssetCode: params.AssetCode,
			Quantity: params.Quantity, Locale: params.Locale,
		}, &identity)
		if err != nil {
			return err
		}

		request, err := tx.createBalanceAttempt(ctx, order, identity, uint16(scale))
		if err != nil {
			return err
		}
		snapshot, err := json.Marshal(balanceChargeEvent{BalanceChargeRequest: request, HandlerKey: handlerKey})
		if err != nil {
			return err
		}
		eventID, err := tx.balanceCallbacks.CreateEvent(ctx, callbackutil.CreateParams{
			WorkspaceID: identity.WorkspaceID, SourceService: "payment", EventType: BalanceChargeEvent,
			EventKey: request.OperationKey, IdempotencyKey: request.OperationKey,
			Payload: snapshot, PayloadContentType: callbackutil.JSONContentType,
		})
		if err != nil {
			return err
		}
		if err := tx.q.CreateBalancePurchase(ctx, paymentsqlc.CreateBalancePurchaseParams{
			OrderID: int64(order.ID), AttemptID: int64(request.AttemptID), WorkspaceID: identity.WorkspaceID,
			AppID: identity.AppID, PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
			RequestID: params.RequestID, HandlerKey: handlerKey, EventID: int64(eventID), Snapshot: snapshot,
		}); err != nil {
			return err
		}
		purchase = BalancePurchase{OrderID: order.ID, Status: BalancePurchasePending}
		return nil
	})
	if err != nil {
		return BalancePurchase{}, err
	}
	return purchase, nil
}

func (r *PaymentRepository) createBalanceAttempt(ctx context.Context, order Order, identity services.Identity, scale uint16) (BalanceChargeRequest, error) {
	key := "payment.balance:" + order.WorkspaceID + ":" + order.PublicID
	attemptID, err := r.q.CreatePaymentAttempt(ctx, paymentsqlc.CreatePaymentAttemptParams{
		OrderID: int64(order.ID), ProviderCode: BalanceProviderCode, AssetCode: order.AssetCode,
		AmountMinor: int64(order.PayableAmountMinor), Status: paymentsqlc.PaymentAttemptStatusPending,
		IdempotencyKey:    sql.NullString{String: key, Valid: true},
		ProviderPaymentID: sql.NullString{String: key, Valid: true},
	})
	if err != nil {
		return BalanceChargeRequest{}, err
	}
	rows, err := r.q.MarkOrderPendingPayment(ctx, int64(order.ID))
	if err != nil {
		return BalanceChargeRequest{}, err
	}
	if rows != 1 {
		return BalanceChargeRequest{}, ErrOrderStateInvalid
	}
	items, err := r.q.GetFulfillmentItemsForOrder(ctx, int64(order.ID))
	if err != nil {
		return BalanceChargeRequest{}, err
	}
	rewards := make([]services.Reward, 0, len(items))
	for _, item := range items {
		rewards = append(rewards, services.Reward{
			Key: item.ItemID, Type: string(item.RewardType), Quantity: item.Quantity,
			Scale: uint16(item.Scale), Unit: orderDurationUnitPtr(item.DurationUnit),
		})
	}
	return BalanceChargeRequest{
		Identity: identity, OrderID: order.ID, AttemptID: uint64(attemptID), OperationKey: key,
		ProductID: order.ProductID, AssetCode: order.AssetCode, Quantity: order.Quantity,
		AmountMinor: order.PayableAmountMinor, Scale: scale, Rewards: rewards,
	}, nil
}

func (r *PaymentRepository) GetBalancePurchase(ctx context.Context, params GetBalancePurchaseParams) (BalancePurchase, error) {
	identity := params.Identity
	if err := identity.Validate(); err != nil {
		return BalancePurchase{}, err
	}
	if !ValidBalanceKey(identity.PlatformUserID, 128) || (params.OrderID == 0) == (params.RequestID == "") ||
		params.OrderID > math.MaxInt64 || (params.RequestID != "" && !ValidBalanceKey(params.RequestID, 128)) {
		return BalancePurchase{}, ErrBalanceRequestInvalid
	}
	var row paymentsqlc.PaymentBalancePurchase
	var err error
	if params.OrderID != 0 {
		row, err = r.q.GetBalancePurchaseByOwner(ctx, paymentsqlc.GetBalancePurchaseByOwnerParams{
			OrderID: int64(params.OrderID), WorkspaceID: identity.WorkspaceID, AppID: identity.AppID,
			PlatformID: identity.PlatformID, PlatformUserID: identity.PlatformUserID,
		})
	} else {
		row, err = r.q.GetBalancePurchaseByRequest(ctx, balanceRequestScope(identity, params.RequestID))
	}
	if err != nil {
		return BalancePurchase{}, err
	}
	return mapBalancePurchase(row), nil
}

func mapBalancePurchase(row paymentsqlc.PaymentBalancePurchase) BalancePurchase {
	status := BalancePurchasePending
	if row.Outcome != "" {
		status = BalancePurchaseStatus(row.Outcome)
	} else if row.AdmittedAt.Valid {
		status = BalancePurchaseProcessing
	}
	return BalancePurchase{OrderID: uint64(row.OrderID), Status: status, Reason: row.Reason}
}
