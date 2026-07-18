package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	utils "github.com/elum2b/services/internal/utils"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/payment/sqlc"

	"github.com/google/uuid"
)

var (
	ErrProductLocked = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"payment product limit is locked",
	)
	ErrPaymentMismatch   = serviceerrors.New(serviceerrors.CodeConflict, "payment data mismatch")
	ErrOrderStateInvalid = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"payment order state is invalid",
	)
	ErrPaymentAmountOverflow = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment amount overflow")
	ErrProductQuantityFixed  = serviceerrors.New(
		serviceerrors.CodeFailedPrecondition,
		"payment product quantity is fixed",
	)
	ErrOrderExpirationInvalid = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment order expiration is invalid",
	)
	ErrOrderFieldsInvalid     = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment order fields are invalid")
	ErrAttemptFieldsInvalid   = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment attempt fields are invalid")
	ErrIdempotencyKeyRequired = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment idempotency key is required",
	)
)

type OrderCreateParams struct {
	WorkspaceID         string
	AppID               int64
	PlatformID          int64
	PlatformUserID      string
	InternalUserID      *int64
	PayerPlatformID     *int64
	PayerPlatformUserID *string
	PayerInternalUserID *int64
	PurchaseKeyID       *int64
	ProductID           string
	Quantity            uint64
	AssetCode           string
	Locale              string
	ReservedUntil       *time.Time
	ExpiresAt           *time.Time
}

type OrderCreateByKeyParams struct {
	Key                 string
	PayerPlatformID     *int64
	PayerPlatformUserID *string
	PayerInternalUserID *int64
	AssetCode           string
	Locale              string
	Quantity            uint64
	ReservedUntil       *time.Time
	ExpiresAt           *time.Time
	Now                 time.Time
}

type Order struct {
	ID                  uint64
	PublicID            string
	WorkspaceID         string
	AppID               int64
	PlatformID          int64
	PlatformUserID      string
	InternalUserID      *int64
	PayerPlatformID     *int64
	PayerPlatformUserID *string
	PayerInternalUserID *int64
	PurchaseKeyID       *int64
	ProductID           string
	Quantity            uint64
	PriceID             uint64
	AssetCode           string
	Locale              string
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	PayableAmountMinor  uint64
	Status              string
}

type AttemptCreateParams struct {
	OrderID                uint64
	ProviderCode           string
	Status                 string
	ProviderPaymentID      *string
	ProviderInvoiceID      *string
	ProviderChargeID       *string
	ProviderSubscriptionID *string
	IdempotencyKey         *string
	RequestFingerprint     string
	ConfirmationURL        *string
	ReturnURL              *string
	ExpiresAt              *time.Time
}

type Attempt struct {
	ID                     uint64
	WorkspaceID            string
	OrderID                uint64
	ProviderCode           string
	AssetCode              string
	AmountMinor            uint64
	Status                 string
	ProviderPaymentID      *string
	ProviderInvoiceID      *string
	ProviderChargeID       *string
	ProviderSubscriptionID *string
	IdempotencyKey         *string
	RequestFingerprint     string
	ConfirmationURL        *string
	ReturnURL              *string
	ExpiresAt              *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type PendingAttemptValidationParams struct {
	WorkspaceID       string
	ProviderCode      string
	ProviderPaymentID string
	AmountMinor       uint64
	AssetCode         string
	Now               time.Time
}

type ProviderAttemptCreateParams struct {
	Order              OrderCreateParams
	ProviderCode       string
	IdempotencyKey     string
	RequestFingerprint string
}

type ProviderAttemptCreateResult struct {
	Order         Order
	Attempt       Attempt
	AlreadyExists bool
}

type ProviderAttemptBindParams struct {
	WorkspaceID        string
	AttemptID          uint64
	ProviderCode       string
	RequestFingerprint string
	ProviderPaymentID  string
	ProviderInvoiceID  *string
	ConfirmationURL    *string
	ReturnURL          *string
	ExpiresAt          *time.Time
}

type ProviderAttemptRecoverParams struct {
	WorkspaceID       string
	OrderPublicID     string
	ProviderCode      string
	ProviderPaymentID string
	AmountMinor       uint64
	AssetCode         string
}

type ProviderAttemptForReconciliation struct {
	ID                uint64
	WorkspaceID       string
	OrderPublicID     string
	ProviderCode      string
	AssetCode         string
	AmountMinor       uint64
	Status            string
	ProviderPaymentID *string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EventCreateParams struct {
	WorkspaceID       string
	ProviderCode      string
	AttemptID         *int64
	OrderID           *int64
	ProviderEventID   *string
	ProviderPaymentID *string
	EventType         string
	EventStatus       *string
	PayloadHash       string
	SignatureValid    *bool
}

type CompleteAttemptParams struct {
	WorkspaceID       string
	AttemptID         uint64
	ProviderCode      string
	ProviderPaymentID *string
	AmountMinor       uint64
	AssetCode         string
}

type CompleteAttemptResult struct {
	OrderID       uint64
	AttemptID     uint64
	FulfillmentID *int64
	AlreadyDone   bool
}

type orderLimitSnapshot struct {
	limit         int32
	interval      string
	intervalCount int32
	windowStart   sql.NullTime
	windowEnd     sql.NullTime
}

type paymentFulfilledCallbackPayload struct {
	OrderID           uint64                  `json:"order_id"`
	AttemptID         uint64                  `json:"attempt_id"`
	FulfillmentID     uint64                  `json:"fulfillment_id"`
	WorkspaceID       string                  `json:"workspace_id"`
	AppID             int64                   `json:"app_id"`
	PlatformID        int64                   `json:"platform_id"`
	PlatformUserID    string                  `json:"platform_user_id"`
	ProductID         string                  `json:"product_id"`
	Quantity          uint64                  `json:"quantity"`
	ProviderCode      string                  `json:"provider_code"`
	ProviderPaymentID string                  `json:"provider_payment_id,omitempty"`
	AssetCode         string                  `json:"asset_code"`
	AmountMinor       uint64                  `json:"amount_minor"`
	Rewards           []paymentCallbackReward `json:"rewards"`
}

type paymentCallbackReward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

func (r *PaymentRepository) CreateOrder(ctx context.Context, params OrderCreateParams) (Order, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Order{}, err
	}
	if params.AppID <= 0 || params.PlatformID <= 0 ||
		strings.TrimSpace(params.PlatformUserID) == "" ||
		strings.TrimSpace(params.ProductID) == "" ||
		strings.TrimSpace(params.AssetCode) == "" {
		return Order{}, ErrOrderFieldsInvalid
	}
	now := time.Now().UTC()
	if (params.ReservedUntil != nil && !params.ReservedUntil.After(now)) ||
		(params.ExpiresAt != nil && !params.ExpiresAt.After(now)) ||
		(params.ReservedUntil != nil && params.ExpiresAt != nil && params.ReservedUntil.After(*params.ExpiresAt)) {
		return Order{}, ErrOrderExpirationInvalid
	}

	var order Order
	err = r.inTransaction(ctx, func(txRepo *PaymentRepository) error {
		product, err := txRepo.getCheckoutProduct(ctx, ProductGetParams{
			AppID:          params.AppID,
			WorkspaceID:    workspaceID,
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			ProductID:      params.ProductID,
			AssetCode:      params.AssetCode,
			Locale:         params.Locale,
			Now:            now,
		})
		if err != nil {
			return err
		}
		if product.Limit.Global.LockUntil.Valid || product.Limit.User.LockUntil.Valid {
			return ErrProductLocked
		}
		quantity := normalizeOrderQuantity(params.Quantity)
		if product.QuantityMode != string(sqlc.PaymentProductCacheQuantityModeFlexible) && quantity != 1 {
			return ErrProductQuantityFixed
		}
		if err := txRepo.ensureProductLimitAvailable(ctx, product, params.PlatformID, params.PlatformUserID, quantity); err != nil {
			return err
		}
		listAmountMinor, err := multiplyMinorAmount(product.Price.ListAmountMinor, quantity)
		if err != nil {
			return err
		}
		discountAmountMinor, err := multiplyMinorAmount(product.Price.DiscountAmountMinor, quantity)
		if err != nil {
			return err
		}
		payableAmountMinor, err := multiplyMinorAmount(product.Price.PayableAmountMinor, quantity)
		if err != nil {
			return err
		}
		if err := validateOrderItemSnapshots(product.Items, quantity); err != nil {
			return err
		}
		var reservationNow time.Time
		if limitRuleNeedsWindow(product.Limit.Global) || limitRuleNeedsWindow(product.Limit.User) {
			reservationNow, err = txRepo.databaseNow(ctx)
			if err != nil {
				return err
			}
		}
		globalLimit := newOrderLimitSnapshot(product.Limit.Global, reservationNow)
		userLimit := newOrderLimitSnapshot(product.Limit.User, reservationNow)

		publicID := uuid.NewString()
		id, err := txRepo.q.CreatePaymentOrder(ctx, sqlc.CreatePaymentOrderParams{
			PublicID:       publicID,
			WorkspaceID:    product.WorkspaceID,
			AppID:          params.AppID,
			PlatformID:     params.PlatformID,
			PlatformUserID: params.PlatformUserID,
			InternalUserID: sqlwrap.NullFromPtr(params.InternalUserID, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			PayerPlatformID: sqlwrap.NullFromPtr(params.PayerPlatformID, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			PayerPlatformUserID: sqlwrap.NullFromPtr(params.PayerPlatformUserID, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			PayerInternalUserID: sqlwrap.NullFromPtr(params.PayerInternalUserID, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			PurchaseKeyID: sqlwrap.NullFromPtr(params.PurchaseKeyID, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			ProductID:                   product.ID,
			Quantity:                    int64(quantity),
			PriceID:                     int64(product.Price.ID),
			AssetCode:                   product.Price.AssetCode,
			Locale:                      normalizedLocale(params.Locale),
			ListAmountMinor:             int64(listAmountMinor),
			DiscountAmountMinor:         int64(discountAmountMinor),
			PayableAmountMinor:          int64(payableAmountMinor),
			Status:                      sqlc.PaymentOrderStatusDraft,
			ReservedUntil:               sqlwrap.NullTimeFromPtr(params.ReservedUntil),
			GlobalLimitSnapshot:         globalLimit.limit,
			GlobalIntervalSnapshot:      globalLimit.interval,
			GlobalIntervalCountSnapshot: globalLimit.intervalCount,
			GlobalWindowStartSnapshot:   globalLimit.windowStart,
			GlobalWindowEndSnapshot:     globalLimit.windowEnd,
			UserLimitSnapshot:           userLimit.limit,
			UserIntervalSnapshot:        userLimit.interval,
			UserIntervalCountSnapshot:   userLimit.intervalCount,
			UserWindowStartSnapshot:     userLimit.windowStart,
			UserWindowEndSnapshot:       userLimit.windowEnd,
			ExpiresAt:                   sqlwrap.NullTimeFromPtr(params.ExpiresAt),
		})
		if err != nil {
			return err
		}
		orderID := uint64(id)
		if err := txRepo.reserveOrderLimit(
			ctx,
			product.WorkspaceID,
			params.PlatformID,
			params.PlatformUserID,
			product.ID,
			sqlc.PaymentProductLimitCounterCounterScopeGlobal,
			globalLimit,
			quantity,
		); err != nil {
			return err
		}
		if err := txRepo.reserveOrderLimit(
			ctx,
			product.WorkspaceID,
			params.PlatformID,
			params.PlatformUserID,
			product.ID,
			sqlc.PaymentProductLimitCounterCounterScopeUser,
			userLimit,
			quantity,
		); err != nil {
			return err
		}

		order = Order{
			ID:                  orderID,
			PublicID:            publicID,
			WorkspaceID:         product.WorkspaceID,
			AppID:               params.AppID,
			PlatformID:          params.PlatformID,
			PlatformUserID:      params.PlatformUserID,
			InternalUserID:      params.InternalUserID,
			PayerPlatformID:     params.PayerPlatformID,
			PayerPlatformUserID: params.PayerPlatformUserID,
			PayerInternalUserID: params.PayerInternalUserID,
			PurchaseKeyID:       params.PurchaseKeyID,
			ProductID:           product.ID,
			Quantity:            quantity,
			PriceID:             product.Price.ID,
			AssetCode:           product.Price.AssetCode,
			Locale:              normalizedLocale(params.Locale),
			ListAmountMinor:     listAmountMinor,
			DiscountAmountMinor: discountAmountMinor,
			PayableAmountMinor:  payableAmountMinor,
			Status:              string(sqlc.PaymentOrderStatusDraft),
		}
		return nil
	})
	return order, err
}

func limitRuleNeedsWindow(rule ProductLimitRule) bool {
	return rule.Limit > 0 && rule.Interval != "UNLIMITED"
}

func newOrderLimitSnapshot(rule ProductLimitRule, now time.Time) orderLimitSnapshot {
	snapshot := orderLimitSnapshot{
		limit:         rule.Limit,
		interval:      rule.Interval,
		intervalCount: rule.IntervalCount,
	}
	if rule.Limit <= 0 || rule.Interval == "UNLIMITED" {
		return snapshot
	}

	start, end, ok := limitWindow(rule.Interval, rule.IntervalCount, now)
	if !ok {
		return snapshot
	}
	snapshot.windowStart = sql.NullTime{Time: start, Valid: true}
	snapshot.windowEnd = sql.NullTime{Time: end, Valid: true}

	return snapshot
}

func (r *PaymentRepository) reserveOrderLimit(
	ctx context.Context,
	workspaceID string,
	platformID int64,
	platformUserID string,
	productID string,
	scope sqlc.PaymentProductLimitCounterCounterScope,
	snapshot orderLimitSnapshot,
	quantity uint64,
) error {
	if !snapshot.windowStart.Valid || !snapshot.windowEnd.Valid {
		return nil
	}
	if scope == sqlc.PaymentProductLimitCounterCounterScopeGlobal {
		platformID = 0
		platformUserID = ""
	}

	ensure := sqlc.EnsureProductLimitCounterParams{
		WorkspaceID:    workspaceID,
		PlatformID:     platformID,
		ProductID:      productID,
		CounterScope:   scope,
		PlatformUserID: platformUserID,
		WindowStart:    snapshot.windowStart.Time,
		WindowEnd:      snapshot.windowEnd.Time,
	}
	if _, err := r.q.EnsureProductLimitCounter(ctx, ensure); err != nil {
		return err
	}

	amount := int64(normalizeLimitAmount(quantity))
	rows, err := r.q.ReserveProductLimitCounter(ctx, sqlc.ReserveProductLimitCounterParams{
		ReservedCount:  amount,
		WorkspaceID:    workspaceID,
		PlatformID:     platformID,
		ProductID:      productID,
		CounterScope:   scope,
		PlatformUserID: platformUserID,
		WindowStart:    snapshot.windowStart.Time,
		WindowEnd:      snapshot.windowEnd.Time,
		PaidCount:      amount,
		PaidCount_2:    int64(snapshot.limit),
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrProductLocked
	}

	return nil
}

func (r *PaymentRepository) CreateOrderByKey(ctx context.Context, params OrderCreateByKeyParams) (Order, error) {
	var order Order
	err := r.inTransaction(ctx, func(txRepo *PaymentRepository) error {
		now := params.Now
		if now.IsZero() {
			now = time.Now()
		}

		key, err := txRepo.q.GetPurchaseKeyByHash(ctx, hashPurchaseKey(params.Key))
		if err != nil {
			return err
		}
		if !isPurchaseKeyUsable(key, now) {
			return sql.ErrNoRows
		}

		order, err = txRepo.CreateOrder(ctx, OrderCreateParams{
			AppID:               key.AppID,
			WorkspaceID:         key.WorkspaceID,
			PlatformID:          key.PlatformID,
			PlatformUserID:      key.PlatformUserID,
			InternalUserID:      nullInt64Ptr(key.InternalUserID),
			PayerPlatformID:     params.PayerPlatformID,
			PayerPlatformUserID: params.PayerPlatformUserID,
			PayerInternalUserID: params.PayerInternalUserID,
			ProductID:           key.ProductID,
			AssetCode:           params.AssetCode,
			Locale:              params.Locale,
			Quantity:            params.Quantity,
			ReservedUntil:       params.ReservedUntil,
			ExpiresAt:           params.ExpiresAt,
		})
		if err != nil {
			return err
		}

		reserved, err := txRepo.q.ReservePurchaseKeyUsage(ctx, key.ID)
		if err != nil {
			return err
		}
		if reserved != 1 {
			return sql.ErrNoRows
		}

		bound, err := txRepo.q.BindPaymentOrderPurchaseKey(ctx, sqlc.BindPaymentOrderPurchaseKeyParams{
			PurchaseKeyID: sql.NullInt64{
				Int64: key.ID,
				Valid: true,
			},
			ID:          int64(order.ID),
			WorkspaceID: key.WorkspaceID,
		})
		if err != nil {
			return err
		}
		if bound != 1 {
			return ErrOrderStateInvalid
		}

		order.PurchaseKeyID = utils.Ref(int64(key.ID))

		return nil
	})
	return order, err
}

func normalizeOrderQuantity(quantity uint64) uint64 {
	if quantity == 0 {
		return 1
	}
	return quantity
}

func multiplyMinorAmount(amount uint64, quantity uint64) (uint64, error) {
	quantity = normalizeOrderQuantity(quantity)
	if quantity > uint64(1<<63-1) {
		return 0, ErrPaymentAmountOverflow
	}
	if amount != 0 && quantity > uint64(math.MaxInt64)/amount {
		return 0, ErrPaymentAmountOverflow
	}
	return amount * quantity, nil
}

func validateOrderItemSnapshots(items []ProductItem, quantity uint64) error {
	quantity = normalizeOrderQuantity(quantity)
	for _, item := range items {
		if item.Quantity < 0 || item.Quantity != 0 && quantity > uint64(math.MaxInt64)/uint64(item.Quantity) {
			return ErrPaymentAmountOverflow
		}
	}
	return nil
}

func (r *PaymentRepository) ensureProductLimitAvailable(
	ctx context.Context,
	product Product,
	platformID int64,
	platformUserID string,
	quantity uint64,
) error {
	globalLock, err := r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     platformID,
		platformUserID: "",
		productID:      product.ID,
		limit:          product.Limit.Global.Limit,
		interval:       product.Limit.Global.Interval,
		intervalCount:  product.Limit.Global.IntervalCount,
		amount:         quantity,
	})
	if err != nil {
		return err
	}
	if globalLock.Valid {
		return ErrProductLocked
	}

	userLock, err := r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     platformID,
		platformUserID: platformUserID,
		productID:      product.ID,
		limit:          product.Limit.User.Limit,
		interval:       product.Limit.User.Interval,
		intervalCount:  product.Limit.User.IntervalCount,
		amount:         quantity,
	})
	if err != nil {
		return err
	}
	if userLock.Valid {
		return ErrProductLocked
	}
	return nil
}

func (r *PaymentRepository) GetOrder(ctx context.Context, id uint64) (Order, error) {
	order, err := r.q.GetPaymentOrder(ctx, int64(id))
	if err != nil {
		return Order{}, err
	}
	return mapOrder(order), nil
}

func (r *PaymentRepository) GetAttemptByProviderPaymentID(
	ctx context.Context,
	workspaceID string,
	providerCode string,
	providerPaymentID string,
) (Attempt, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return Attempt{}, err
	}

	attempt, err := r.q.GetPaymentAttemptByProviderPaymentID(ctx, sqlc.GetPaymentAttemptByProviderPaymentIDParams{
		WorkspaceID:       workspaceID,
		ProviderCode:      providerCode,
		ProviderPaymentID: sql.NullString{String: providerPaymentID, Valid: true},
	})
	if err != nil {
		return Attempt{}, err
	}
	return mapAttempt(attempt), nil
}

func (r *PaymentRepository) ValidatePendingAttempt(
	ctx context.Context,
	params PendingAttemptValidationParams,
) (Attempt, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Attempt{}, err
	}
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.ProviderCode == "" || params.ProviderPaymentID == "" ||
		params.AssetCode == "" || params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 {
		return Attempt{}, ErrAttemptFieldsInvalid
	}
	if params.Now.IsZero() {
		params.Now = time.Now().UTC()
	}

	var result Attempt
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttemptByProviderPaymentID(
			ctx,
			sqlc.LockPaymentAttemptByProviderPaymentIDParams{
				WorkspaceID:  workspaceID,
				ProviderCode: params.ProviderCode,
				ProviderPaymentID: sql.NullString{
					String: params.ProviderPaymentID,
					Valid:  true,
				},
			},
		)
		if err != nil {
			return err
		}
		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}
		if attempt.WorkspaceID != workspaceID || order.WorkspaceID != workspaceID ||
			attempt.ProviderCode != params.ProviderCode || attempt.AssetCode != params.AssetCode ||
			uint64(attempt.AmountMinor) != params.AmountMinor {
			return ErrPaymentMismatch
		}
		if attempt.Status != sqlc.PaymentAttemptStatusPending &&
			attempt.Status != sqlc.PaymentAttemptStatusRequiresAction &&
			attempt.Status != sqlc.PaymentAttemptStatusWaitingCapture {
			return ErrOrderStateInvalid
		}
		if order.Status != sqlc.PaymentOrderStatusDraft &&
			order.Status != sqlc.PaymentOrderStatusPendingPayment {
			return ErrOrderStateInvalid
		}
		if (attempt.ExpiresAt.Valid && !params.Now.Before(attempt.ExpiresAt.Time)) ||
			(order.ReservedUntil.Valid && !params.Now.Before(order.ReservedUntil.Time)) ||
			(order.ExpiresAt.Valid && !params.Now.Before(order.ExpiresAt.Time)) {
			return ErrOrderStateInvalid
		}

		rows, err := txRepo.q.TouchPendingProviderAttempt(ctx, sqlc.TouchPendingProviderAttemptParams{
			WorkspaceID:       workspaceID,
			ID:                attempt.ID,
			ProviderCode:      params.ProviderCode,
			ProviderPaymentID: attempt.ProviderPaymentID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrOrderStateInvalid
		}

		result = mapAttempt(attempt)
		result.UpdatedAt = params.Now
		return nil
	})

	return result, err
}

func (r *PaymentRepository) CreateProviderAttempt(
	ctx context.Context,
	params ProviderAttemptCreateParams,
) (ProviderAttemptCreateResult, error) {
	workspaceID, err := requireWorkspaceID(params.Order.WorkspaceID)
	if err != nil {
		return ProviderAttemptCreateResult{}, err
	}
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.IdempotencyKey = strings.TrimSpace(params.IdempotencyKey)
	if params.ProviderCode == "" || params.IdempotencyKey == "" || len(params.IdempotencyKey) > 128 ||
		!validRequestFingerprint(params.RequestFingerprint) {
		return ProviderAttemptCreateResult{}, ErrAttemptFieldsInvalid
	}
	params.Order.WorkspaceID = workspaceID

	var result ProviderAttemptCreateResult
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		lockParams := sqlc.LockPaymentProviderIdempotencyParams{
			WorkspaceID:    workspaceID,
			ProviderCode:   params.ProviderCode,
			IdempotencyKey: params.IdempotencyKey,
		}
		if err := txRepo.q.LockPaymentProviderIdempotency(ctx, lockParams); err != nil {
			return err
		}

		existing, err := txRepo.q.GetPaymentAttemptByIdempotencyKey(
			ctx,
			sqlc.GetPaymentAttemptByIdempotencyKeyParams{
				WorkspaceID:  lockParams.WorkspaceID,
				ProviderCode: lockParams.ProviderCode,
				IdempotencyKey: sql.NullString{
					String: lockParams.IdempotencyKey,
					Valid:  true,
				},
			},
		)
		if err == nil {
			if existing.RequestFingerprint != params.RequestFingerprint {
				return ErrPaymentMismatch
			}
			order, err := txRepo.q.GetPaymentOrder(ctx, existing.OrderID)
			if err != nil {
				return err
			}
			result = ProviderAttemptCreateResult{
				Order:         mapOrder(order),
				Attempt:       mapAttempt(existing),
				AlreadyExists: true,
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		order, err := txRepo.CreateOrder(ctx, params.Order)
		if err != nil {
			return err
		}
		attempt, err := txRepo.createAttempt(ctx, AttemptCreateParams{
			OrderID:            order.ID,
			ProviderCode:       params.ProviderCode,
			Status:             string(sqlc.PaymentAttemptStatusCreated),
			IdempotencyKey:     utils.Ref(params.IdempotencyKey),
			RequestFingerprint: params.RequestFingerprint,
		}, nil)
		if err != nil {
			return err
		}
		result = ProviderAttemptCreateResult{
			Order:   order,
			Attempt: attempt,
		}
		return nil
	})

	return result, err
}

func (r *PaymentRepository) BindProviderAttempt(
	ctx context.Context,
	params ProviderAttemptBindParams,
) (Attempt, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Attempt{}, err
	}
	if params.AttemptID == 0 || params.AttemptID > math.MaxInt64 || strings.TrimSpace(params.ProviderCode) == "" ||
		strings.TrimSpace(params.ProviderPaymentID) == "" || !validRequestFingerprint(params.RequestFingerprint) {
		return Attempt{}, ErrAttemptFieldsInvalid
	}

	var result Attempt
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		rows, err := txRepo.q.BindPaymentAttemptProviderResult(ctx, sqlc.BindPaymentAttemptProviderResultParams{
			ProviderPaymentID: sql.NullString{String: params.ProviderPaymentID, Valid: true},
			ProviderInvoiceID: sqlwrap.NullFromPtr(params.ProviderInvoiceID, func(value string) sql.NullString {
				return sql.NullString{String: value, Valid: true}
			}),
			ConfirmationUrl: sqlwrap.NullFromPtr(params.ConfirmationURL, func(value string) sql.NullString {
				return sql.NullString{String: value, Valid: true}
			}),
			ReturnUrl: sqlwrap.NullFromPtr(params.ReturnURL, func(value string) sql.NullString {
				return sql.NullString{String: value, Valid: true}
			}),
			ExpiresAt:          sqlwrap.NullTimeFromPtr(params.ExpiresAt),
			WorkspaceID:        workspaceID,
			ID:                 int64(params.AttemptID),
			ProviderCode:       strings.TrimSpace(params.ProviderCode),
			RequestFingerprint: params.RequestFingerprint,
		})
		if err != nil {
			if isUniqueViolation(err) {
				return ErrPaymentMismatch
			}

			return err
		}
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}
		if attempt.WorkspaceID != workspaceID || attempt.ProviderCode != params.ProviderCode ||
			attempt.RequestFingerprint != params.RequestFingerprint ||
			!attempt.ProviderPaymentID.Valid || attempt.ProviderPaymentID.String != params.ProviderPaymentID {
			return ErrPaymentMismatch
		}
		if rows == 0 && attempt.Status == sqlc.PaymentAttemptStatusCreated {
			return ErrOrderStateInvalid
		}
		result = mapAttempt(attempt)
		return nil
	})

	return result, err
}

func (r *PaymentRepository) RecoverProviderAttempt(
	ctx context.Context,
	params ProviderAttemptRecoverParams,
) (Attempt, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Attempt{}, err
	}
	params.OrderPublicID = strings.TrimSpace(params.OrderPublicID)
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.ProviderPaymentID = strings.TrimSpace(params.ProviderPaymentID)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.OrderPublicID == "" || params.ProviderCode == "" || params.ProviderPaymentID == "" ||
		params.AssetCode == "" || params.AmountMinor == 0 || params.AmountMinor > math.MaxInt64 {
		return Attempt{}, ErrAttemptFieldsInvalid
	}

	recovered, err := r.q.RecoverCreatedPaymentAttempt(ctx, sqlc.RecoverCreatedPaymentAttemptParams{
		ProviderPaymentID: params.ProviderPaymentID,
		OrderPublicID:     params.OrderPublicID,
		WorkspaceID:       workspaceID,
		ProviderCode:      params.ProviderCode,
		AmountMinor:       int64(params.AmountMinor),
		AssetCode:         params.AssetCode,
	})
	if err == nil {
		return mapAttempt(recovered), nil
	}
	if !errors.Is(err, sql.ErrNoRows) && !isUniqueViolation(err) {
		return Attempt{}, err
	}

	existing, lookupErr := r.GetAttemptByProviderPaymentID(
		ctx,
		workspaceID,
		params.ProviderCode,
		params.ProviderPaymentID,
	)
	if lookupErr != nil {
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return Attempt{}, ErrPaymentMismatch
		}
		return Attempt{}, lookupErr
	}
	order, lookupErr := r.GetOrder(ctx, existing.OrderID)
	if lookupErr != nil {
		return Attempt{}, lookupErr
	}
	if order.WorkspaceID != workspaceID || order.PublicID != params.OrderPublicID ||
		existing.AmountMinor != params.AmountMinor || existing.AssetCode != params.AssetCode {
		return Attempt{}, ErrPaymentMismatch
	}

	return existing, nil
}

func (r *PaymentRepository) ListProviderAttemptsForReconciliation(
	ctx context.Context,
	providerCode string,
	updatedTo time.Time,
	limit int32,
) ([]ProviderAttemptForReconciliation, error) {
	providerCode = strings.TrimSpace(providerCode)
	if providerCode == "" || updatedTo.IsZero() || limit <= 0 {
		return nil, ErrAttemptFieldsInvalid
	}

	rows, err := r.q.ListProviderAttemptsForReconciliation(ctx, sqlc.ListProviderAttemptsForReconciliationParams{
		ProviderCode: providerCode,
		EligibleTo:   updatedTo,
		RowLimit:     limit,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ProviderAttemptForReconciliation, 0, len(rows))
	for _, row := range rows {
		result = append(result, ProviderAttemptForReconciliation{
			ID:                uint64(row.ID),
			WorkspaceID:       row.WorkspaceID,
			OrderPublicID:     row.OrderPublicID,
			ProviderCode:      row.ProviderCode,
			AssetCode:         row.AssetCode,
			AmountMinor:       uint64(row.AmountMinor),
			Status:            string(row.Status),
			ProviderPaymentID: sqlwrap.NullStringPtr(row.ProviderPaymentID),
			CreatedAt:         row.CreatedAt,
			UpdatedAt:         row.UpdatedAt,
		})
	}

	return result, nil
}

func (r *PaymentRepository) TouchPendingProviderAttempt(
	ctx context.Context,
	workspaceID string,
	attemptID uint64,
	providerCode string,
	providerPaymentID string,
) error {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	providerCode = strings.TrimSpace(providerCode)
	providerPaymentID = strings.TrimSpace(providerPaymentID)
	if attemptID == 0 || attemptID > math.MaxInt64 || providerCode == "" || providerPaymentID == "" {
		return ErrAttemptFieldsInvalid
	}

	rows, err := r.q.TouchPendingProviderAttempt(ctx, sqlc.TouchPendingProviderAttemptParams{
		WorkspaceID:       workspaceID,
		ID:                int64(attemptID),
		ProviderCode:      providerCode,
		ProviderPaymentID: sql.NullString{String: providerPaymentID, Valid: true},
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrOrderStateInvalid
	}

	return nil
}

func (r *PaymentRepository) FailProviderAttempt(
	ctx context.Context,
	workspaceID string,
	attemptID uint64,
	providerCode string,
) error {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return err
	}
	if attemptID == 0 || attemptID > math.MaxInt64 || strings.TrimSpace(providerCode) == "" {
		return ErrAttemptFieldsInvalid
	}

	return r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(attemptID))
		if err != nil {
			return err
		}
		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}
		if attempt.WorkspaceID != workspaceID || order.WorkspaceID != workspaceID ||
			attempt.ProviderCode != providerCode {
			return ErrPaymentMismatch
		}
		if attempt.Status == sqlc.PaymentAttemptStatusFailed {
			return nil
		}
		if attempt.Status != sqlc.PaymentAttemptStatusCreated ||
			(order.Status != sqlc.PaymentOrderStatusDraft && order.Status != sqlc.PaymentOrderStatusPendingPayment) {
			return ErrOrderStateInvalid
		}
		rows, err := txRepo.q.FailCreatedPaymentAttempt(ctx, sqlc.FailCreatedPaymentAttemptParams{
			WorkspaceID:  workspaceID,
			ID:           int64(attemptID),
			ProviderCode: providerCode,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrOrderStateInvalid
		}
		if err := txRepo.releaseOrderLimits(ctx, order); err != nil {
			return err
		}
		if order.PurchaseKeyID.Valid {
			rows, err := txRepo.q.ReleasePurchaseKeyReservation(ctx, order.PurchaseKeyID.Int64)
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrOrderStateInvalid
			}
		}
		rows, err = txRepo.q.AdminUpdateOrderStatus(ctx, sqlc.AdminUpdateOrderStatusParams{
			Status:      sqlc.PaymentOrderStatusCanceled,
			Column2:     string(sqlc.PaymentOrderStatusCanceled),
			Column3:     string(sqlc.PaymentOrderStatusCanceled),
			Column4:     string(sqlc.PaymentOrderStatusCanceled),
			WorkspaceID: workspaceID,
			ID:          order.ID,
		})
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrOrderStateInvalid
		}
		return nil
	})
}

func validRequestFingerprint(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (r *PaymentRepository) CreateAttempt(ctx context.Context, params AttemptCreateParams) (Attempt, error) {
	return r.createAttempt(ctx, params, nil)
}

func (r *PaymentRepository) CreateUserAttempt(
	ctx context.Context,
	identity services.Identity,
	params AttemptCreateParams,
) (Attempt, error) {

	if err := identity.Validate(); err != nil {
		return Attempt{}, err
	}

	return r.createAttempt(ctx, params, &identity)

}

func (r *PaymentRepository) createAttempt(
	ctx context.Context,
	params AttemptCreateParams,
	identity *services.Identity,
) (Attempt, error) {

	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	if params.OrderID == 0 || params.ProviderCode == "" {
		return Attempt{}, ErrAttemptFieldsInvalid
	}

	var attempt Attempt
	err := r.inTransaction(ctx, func(txRepo *PaymentRepository) error {
		status := sqlc.PaymentAttemptStatus(params.Status)
		if status == "" {
			status = sqlc.PaymentAttemptStatusPending
		}
		createParams := sqlc.CreatePaymentAttemptFromOrderParams{
			ProviderCode: params.ProviderCode,
			Status:       status,
			ProviderPaymentID: sqlwrap.NullFromPtr(params.ProviderPaymentID, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			ProviderInvoiceID: sqlwrap.NullFromPtr(params.ProviderInvoiceID, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			ProviderChargeID: sqlwrap.NullFromPtr(params.ProviderChargeID, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			ProviderSubscriptionID: sqlwrap.NullFromPtr(params.ProviderSubscriptionID, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			IdempotencyKey: sqlwrap.NullFromPtr(params.IdempotencyKey, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			RequestFingerprint: params.RequestFingerprint,
			ConfirmationUrl: sqlwrap.NullFromPtr(params.ConfirmationURL, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			ReturnUrl: sqlwrap.NullFromPtr(params.ReturnURL, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			ExpiresAt: sqlwrap.NullTimeFromPtr(params.ExpiresAt),
			OrderID:   int64(params.OrderID),
		}

		var createdID int64
		var createdWorkspaceID string
		var createdAssetCode string
		var createdAmountMinor int64
		var err error
		if identity == nil {
			created, createErr := txRepo.q.CreatePaymentAttemptFromOrder(ctx, createParams)
			err = createErr
			createdID = created.ID
			createdWorkspaceID = created.WorkspaceID
			createdAssetCode = created.AssetCode
			createdAmountMinor = created.AmountMinor
		} else {
			created, createErr := txRepo.q.CreatePaymentAttemptFromOwnedOrder(
				ctx,
				sqlc.CreatePaymentAttemptFromOwnedOrderParams{
					ProviderCode:           createParams.ProviderCode,
					Status:                 createParams.Status,
					ProviderPaymentID:      createParams.ProviderPaymentID,
					ProviderInvoiceID:      createParams.ProviderInvoiceID,
					ProviderChargeID:       createParams.ProviderChargeID,
					ProviderSubscriptionID: createParams.ProviderSubscriptionID,
					IdempotencyKey:         createParams.IdempotencyKey,
					RequestFingerprint:     createParams.RequestFingerprint,
					ConfirmationUrl:        createParams.ConfirmationUrl,
					ReturnUrl:              createParams.ReturnUrl,
					ExpiresAt:              createParams.ExpiresAt,
					OrderID:                int64(params.OrderID),
					WorkspaceID:            identity.WorkspaceID,
					AppID:                  identity.AppID,
					PlatformID:             identity.PlatformID,
					PlatformUserID:         identity.PlatformUserID,
				},
			)
			err = createErr
			createdID = created.ID
			createdWorkspaceID = created.WorkspaceID
			createdAssetCode = created.AssetCode
			createdAmountMinor = created.AmountMinor
		}
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			order, orderErr := txRepo.q.GetPaymentOrder(ctx, int64(params.OrderID))
			if orderErr != nil {
				return orderErr
			}
			if identity != nil && !orderOwnedByIdentity(order, *identity) {
				return ErrPaymentMismatch
			}
			if order.Status != sqlc.PaymentOrderStatusDraft && order.Status != sqlc.PaymentOrderStatusPendingPayment {
				return ErrOrderStateInvalid
			}
			return err
		}
		if createdID == 0 {
			order, orderErr := txRepo.q.GetPaymentOrder(ctx, int64(params.OrderID))
			if orderErr != nil {
				return orderErr
			}
			if identity != nil && !orderOwnedByIdentity(order, *identity) {
				return ErrPaymentMismatch
			}
			if order.Status != sqlc.PaymentOrderStatusDraft && order.Status != sqlc.PaymentOrderStatusPendingPayment {
				return ErrOrderStateInvalid
			}
			return sql.ErrNoRows
		}

		if _, err := txRepo.q.MarkOrderPendingPayment(ctx, int64(params.OrderID)); err != nil {
			return err
		}

		attempt = Attempt{
			ID:                 uint64(createdID),
			WorkspaceID:        createdWorkspaceID,
			OrderID:            params.OrderID,
			ProviderCode:       params.ProviderCode,
			AssetCode:          createdAssetCode,
			AmountMinor:        uint64(createdAmountMinor),
			Status:             string(status),
			ProviderPaymentID:  params.ProviderPaymentID,
			IdempotencyKey:     params.IdempotencyKey,
			RequestFingerprint: params.RequestFingerprint,
			ConfirmationURL:    params.ConfirmationURL,
			ReturnURL:          params.ReturnURL,
			ExpiresAt:          params.ExpiresAt,
		}
		return nil
	})

	return attempt, err

}

func orderOwnedByIdentity(order sqlc.PaymentOrder, identity services.Identity) bool {
	if order.WorkspaceID != identity.WorkspaceID || order.AppID != identity.AppID {
		return false
	}

	if order.PlatformID == identity.PlatformID && order.PlatformUserID == identity.PlatformUserID {
		return true
	}

	return order.PayerPlatformID.Valid &&
		order.PayerPlatformID.Int64 == identity.PlatformID &&
		order.PayerPlatformUserID.Valid &&
		order.PayerPlatformUserID.String == identity.PlatformUserID
}

func (r *PaymentRepository) CreateEvent(ctx context.Context, params EventCreateParams) (uint64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}

	id, err := r.q.CreatePaymentEvent(ctx, sqlc.CreatePaymentEventParams{
		WorkspaceID:  workspaceID,
		ProviderCode: params.ProviderCode,
		AttemptID: sqlwrap.NullFromPtr(params.AttemptID, func(v int64) sql.NullInt64 {
			return sql.NullInt64{Int64: v, Valid: true}
		}),
		OrderID: sqlwrap.NullFromPtr(params.OrderID, func(v int64) sql.NullInt64 {
			return sql.NullInt64{Int64: v, Valid: true}
		}),
		ProviderEventID: sqlwrap.NullFromPtr(params.ProviderEventID, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		ProviderPaymentID: sqlwrap.NullFromPtr(params.ProviderPaymentID, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		EventType: params.EventType,
		EventStatus: sqlwrap.NullFromPtr(params.EventStatus, func(v string) sql.NullString {
			return sql.NullString{String: v, Valid: true}
		}),
		PayloadHash: params.PayloadHash,
		SignatureValid: sqlwrap.NullFromPtr(params.SignatureValid, func(v bool) sql.NullBool {
			return sql.NullBool{Bool: v, Valid: true}
		}),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrPaymentMismatch
		}
		if isUniqueViolation(err) && params.ProviderEventID != nil {
			existing, lookupErr := r.q.GetPaymentEventIdentity(ctx, sqlc.GetPaymentEventIdentityParams{
				WorkspaceID:  workspaceID,
				ProviderCode: params.ProviderCode,
				ProviderEventID: sql.NullString{
					String: *params.ProviderEventID,
					Valid:  true,
				},
			})
			if lookupErr == nil && existing.PayloadHash == params.PayloadHash {
				return uint64(existing.ID), nil
			}
			if lookupErr == nil || errors.Is(lookupErr, sql.ErrNoRows) {
				return 0, ErrPaymentMismatch
			}

			return 0, lookupErr
		}
		return 0, err
	}
	return uint64(id), nil
}

func (r *PaymentRepository) SetAttemptProviderChargeID(
	ctx context.Context,
	attemptID uint64,
	providerCode string,
	chargeID string,
) (int64, error) {
	return r.q.SetPaymentAttemptProviderChargeID(ctx, sqlc.SetPaymentAttemptProviderChargeIDParams{
		ProviderChargeID: sql.NullString{String: chargeID, Valid: chargeID != ""},
		ID:               int64(attemptID),
		ProviderCode:     providerCode,
		ProviderChargeID_2: sql.NullString{
			String: chargeID,
			Valid:  chargeID != "",
		},
	})
}

func (r *PaymentRepository) CompleteAttempt(
	ctx context.Context,
	params CompleteAttemptParams,
) (CompleteAttemptResult, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return CompleteAttemptResult{}, err
	}
	params.ProviderCode = strings.TrimSpace(params.ProviderCode)
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	if params.AttemptID == 0 || params.ProviderCode == "" || params.AssetCode == "" ||
		params.AmountMinor > math.MaxInt64 {
		return CompleteAttemptResult{}, ErrAttemptFieldsInvalid
	}

	fulfilled, err := r.getFulfilledAttemptResult(ctx, workspaceID, params.AttemptID)
	if err == nil {
		if fulfilled.ProviderCode != params.ProviderCode ||
			fulfilled.AssetCode != params.AssetCode ||
			uint64(fulfilled.AmountMinor) != params.AmountMinor ||
			!sameProviderPaymentID(
				fulfilled.ProviderPaymentID,
				sqlwrap.NullFromPtr(params.ProviderPaymentID, func(v string) sql.NullString {
					return sql.NullString{
						String: v,
						Valid:  true,
					}
				}),
			) {
			return CompleteAttemptResult{}, ErrPaymentMismatch
		}

		return CompleteAttemptResult{
			OrderID:       uint64(fulfilled.OrderID),
			AttemptID:     uint64(fulfilled.AttemptID),
			FulfillmentID: utils.Ref(fulfilled.FulfillmentID),
			AlreadyDone:   true,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CompleteAttemptResult{}, err
	}

	var result CompleteAttemptResult
	err = r.WithTx(ctx, func(txRepo *PaymentRepository) error {
		attempt, err := txRepo.q.LockPaymentAttempt(ctx, int64(params.AttemptID))
		if err != nil {
			return err
		}
		order, err := txRepo.q.LockPaymentOrder(ctx, attempt.OrderID)
		if err != nil {
			return err
		}

		if attempt.WorkspaceID != workspaceID || order.WorkspaceID != workspaceID {
			return ErrPaymentMismatch
		}

		result.OrderID = uint64(order.ID)
		result.AttemptID = uint64(attempt.ID)

		if attempt.ProviderCode != params.ProviderCode ||
			attempt.AssetCode != params.AssetCode ||
			uint64(attempt.AmountMinor) != params.AmountMinor ||
			!sameProviderPaymentID(
				attempt.ProviderPaymentID,
				sqlwrap.NullFromPtr(params.ProviderPaymentID, func(v string) sql.NullString {
					return sql.NullString{String: v, Valid: true}
				}),
			) {
			return ErrPaymentMismatch
		}

		if order.Status == sqlc.PaymentOrderStatusFulfilled {
			result.AlreadyDone = true
			fulfillment, err := txRepo.q.GetFulfillmentForOrder(ctx, order.ID)
			if err != nil {
				return err
			}
			result.FulfillmentID = utils.Ref(fulfillment.ID)
			return nil
		}

		if err := txRepo.q.UpdatePaymentAttemptStatus(ctx, sqlc.UpdatePaymentAttemptStatusParams{
			Status: sqlc.PaymentAttemptStatusSucceeded,
			ID:     attempt.ID,
		}); err != nil {
			return err
		}

		if order.Status != sqlc.PaymentOrderStatusDraft &&
			order.Status != sqlc.PaymentOrderStatusPendingPayment &&
			order.Status != sqlc.PaymentOrderStatusPaid {
			return ErrOrderStateInvalid
		}
		if err := txRepo.markOrderPaidAndConsumeLimits(ctx, order); err != nil {
			return err
		}

		fulfillmentID, err := txRepo.q.CompleteFulfillmentFromOrder(ctx, sqlc.CompleteFulfillmentFromOrderParams{
			OrderID:        order.ID,
			AttemptID:      attempt.ID,
			InternalUserID: order.InternalUserID,
			Status:         sqlc.PaymentFulfillmentStatusSucceeded,
		})
		if err != nil {
			return err
		}
		result.FulfillmentID = utils.Ref(fulfillmentID)

		items, err := txRepo.q.GetFulfillmentItemsForOrder(ctx, order.ID)
		if err != nil {
			return err
		}

		if order.PurchaseKeyID.Valid {
			rows, err := txRepo.q.ConsumePurchaseKeyReservation(ctx, order.PurchaseKeyID.Int64)
			if err != nil {
				return err
			}
			if rows != 1 {
				return ErrOrderStateInvalid
			}
		}

		if err := txRepo.enqueuePaymentFulfilledCallback(ctx, order, attempt, uint64(fulfillmentID), items); err != nil {
			return err
		}

		return nil
	})
	return result, err
}

func (r *PaymentRepository) getFulfilledAttemptResult(
	ctx context.Context,
	workspaceID string,
	attemptID uint64,
) (sqlc.GetFulfilledAttemptResultRow, error) {
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{
		Key:          paymentCacheKey("fulfilled_attempt", workspaceID, attemptID),
		Timeout:      r.timeout,
		CacheL1Delay: r.cacheL1,
		CacheL2Delay: r.cacheL2,
	}, func(ctx context.Context) (sqlc.GetFulfilledAttemptResultRow, error) {
		return r.q.GetFulfilledAttemptResult(ctx, sqlc.GetFulfilledAttemptResultParams{
			ID:          int64(attemptID),
			WorkspaceID: workspaceID,
		})
	})
}

func (r *PaymentRepository) enqueuePaymentFulfilledCallback(
	ctx context.Context,
	order sqlc.PaymentOrder,
	attempt sqlc.PaymentAttempt,
	fulfillmentID uint64,
	items []sqlc.GetFulfillmentItemsForOrderRow,
) error {
	payload := paymentFulfilledCallbackPayload{
		OrderID:        uint64(order.ID),
		AttemptID:      uint64(attempt.ID),
		FulfillmentID:  fulfillmentID,
		WorkspaceID:    order.WorkspaceID,
		AppID:          order.AppID,
		PlatformID:     order.PlatformID,
		PlatformUserID: order.PlatformUserID,
		ProductID:      order.ProductID,
		Quantity:       uint64(order.Quantity),
		ProviderCode:   attempt.ProviderCode,
		AssetCode:      attempt.AssetCode,
		AmountMinor:    uint64(attempt.AmountMinor),
		Rewards:        make([]paymentCallbackReward, 0, len(items)),
	}
	for _, item := range items {
		payload.Rewards = append(payload.Rewards, paymentCallbackReward{
			Key: item.ItemID, Type: string(item.RewardType), Quantity: item.Quantity,
			Scale: uint16(item.Scale),
			Unit:  orderDurationUnitPtr(item.DurationUnit),
		})
	}
	if attempt.ProviderPaymentID.Valid {
		payload.ProviderPaymentID = attempt.ProviderPaymentID.String
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventKey := fmt.Sprintf("payment.order.fulfilled:%d", order.ID)
	_, err = r.callbacks.CreateEvent(ctx, callbackutil.CreateParams{
		WorkspaceID:        order.WorkspaceID,
		SourceService:      "payment",
		EventType:          "payment.order.fulfilled",
		EventKey:           eventKey,
		IdempotencyKey:     eventKey,
		Payload:            raw,
		PayloadContentType: callbackutil.JSONContentType,
	})
	return err
}

func orderDurationUnitValue(value sqlc.NullPaymentOrderItemDurationUnit) string {
	if !value.Valid {
		return ""
	}
	return string(value.PaymentOrderItemDurationUnit)
}

func orderDurationUnitPtr(value sqlc.NullPaymentOrderItemDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.PaymentOrderItemDurationUnit)
	return &unit
}

func (r *PaymentRepository) markOrderPaidAndConsumeLimits(ctx context.Context, order sqlc.PaymentOrder) error {
	indexed, err := r.q.MarkOrderPaidAndIndex(ctx, order.ID)
	if err != nil {
		return err
	}
	if !indexed {
		return nil
	}

	if err := r.consumeOrderLimit(ctx, order, sqlc.PaymentProductLimitCounterCounterScopeGlobal, orderLimitSnapshot{
		limit:         order.GlobalLimitSnapshot,
		interval:      order.GlobalIntervalSnapshot,
		intervalCount: order.GlobalIntervalCountSnapshot,
		windowStart:   order.GlobalWindowStartSnapshot,
		windowEnd:     order.GlobalWindowEndSnapshot,
	}); err != nil {
		return err
	}

	return r.consumeOrderLimit(ctx, order, sqlc.PaymentProductLimitCounterCounterScopeUser, orderLimitSnapshot{
		limit:         order.UserLimitSnapshot,
		interval:      order.UserIntervalSnapshot,
		intervalCount: order.UserIntervalCountSnapshot,
		windowStart:   order.UserWindowStartSnapshot,
		windowEnd:     order.UserWindowEndSnapshot,
	})
}

func (r *PaymentRepository) consumeOrderLimit(
	ctx context.Context,
	order sqlc.PaymentOrder,
	scope sqlc.PaymentProductLimitCounterCounterScope,
	snapshot orderLimitSnapshot,
) error {
	if !snapshot.windowStart.Valid || !snapshot.windowEnd.Valid {
		return nil
	}
	platformUserID := ""
	platformID := int64(0)
	if scope == sqlc.PaymentProductLimitCounterCounterScopeUser {
		platformID = order.PlatformID
		platformUserID = order.PlatformUserID
	}

	amount := int64(normalizeLimitAmount(uint64(order.Quantity)))
	rows, err := r.q.ConsumeProductLimitReservation(ctx, sqlc.ConsumeProductLimitReservationParams{
		ReservedCount:   amount,
		PaidCount:       amount,
		WorkspaceID:     order.WorkspaceID,
		PlatformID:      platformID,
		ProductID:       order.ProductID,
		CounterScope:    scope,
		PlatformUserID:  platformUserID,
		WindowStart:     snapshot.windowStart.Time,
		WindowEnd:       snapshot.windowEnd.Time,
		ReservedCount_2: amount,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrOrderStateInvalid
	}

	return nil
}

func (r *PaymentRepository) releaseOrderLimits(ctx context.Context, order sqlc.PaymentOrder) error {
	if err := r.releaseOrderLimit(ctx, order, sqlc.PaymentProductLimitCounterCounterScopeGlobal, orderLimitSnapshot{
		limit:         order.GlobalLimitSnapshot,
		interval:      order.GlobalIntervalSnapshot,
		intervalCount: order.GlobalIntervalCountSnapshot,
		windowStart:   order.GlobalWindowStartSnapshot,
		windowEnd:     order.GlobalWindowEndSnapshot,
	}); err != nil {
		return err
	}

	return r.releaseOrderLimit(ctx, order, sqlc.PaymentProductLimitCounterCounterScopeUser, orderLimitSnapshot{
		limit:         order.UserLimitSnapshot,
		interval:      order.UserIntervalSnapshot,
		intervalCount: order.UserIntervalCountSnapshot,
		windowStart:   order.UserWindowStartSnapshot,
		windowEnd:     order.UserWindowEndSnapshot,
	})
}

func (r *PaymentRepository) releaseOrderLimit(
	ctx context.Context,
	order sqlc.PaymentOrder,
	scope sqlc.PaymentProductLimitCounterCounterScope,
	snapshot orderLimitSnapshot,
) error {
	if !snapshot.windowStart.Valid || !snapshot.windowEnd.Valid {
		return nil
	}
	platformUserID := ""
	platformID := int64(0)
	if scope == sqlc.PaymentProductLimitCounterCounterScopeUser {
		platformID = order.PlatformID
		platformUserID = order.PlatformUserID
	}

	amount := int64(normalizeLimitAmount(uint64(order.Quantity)))
	rows, err := r.q.ReleaseProductLimitReservation(ctx, sqlc.ReleaseProductLimitReservationParams{
		ReservedCount:   amount,
		WorkspaceID:     order.WorkspaceID,
		PlatformID:      platformID,
		ProductID:       order.ProductID,
		CounterScope:    scope,
		PlatformUserID:  platformUserID,
		WindowStart:     snapshot.windowStart.Time,
		WindowEnd:       snapshot.windowEnd.Time,
		ReservedCount_2: amount,
	})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrOrderStateInvalid
	}

	return nil
}

func sameProviderPaymentID(stored sql.NullString, received sql.NullString) bool {
	if stored.Valid != received.Valid {
		return false
	}
	if !stored.Valid {
		return true
	}
	return stored.String == received.String
}

func mapOrder(order sqlc.PaymentOrder) Order {
	return Order{
		ID:                  uint64(order.ID),
		PublicID:            order.PublicID,
		WorkspaceID:         order.WorkspaceID,
		AppID:               order.AppID,
		PlatformID:          order.PlatformID,
		PlatformUserID:      order.PlatformUserID,
		InternalUserID:      nullInt64Ptr(order.InternalUserID),
		PayerPlatformID:     nullInt64Ptr(order.PayerPlatformID),
		PayerPlatformUserID: sqlwrap.NullStringPtr(order.PayerPlatformUserID),
		PayerInternalUserID: nullInt64Ptr(order.PayerInternalUserID),
		PurchaseKeyID:       nullInt64Ptr(order.PurchaseKeyID),
		ProductID:           order.ProductID,
		Quantity:            uint64(order.Quantity),
		PriceID:             uint64(order.PriceID),
		AssetCode:           order.AssetCode,
		Locale:              order.Locale,
		ListAmountMinor:     uint64(order.ListAmountMinor),
		DiscountAmountMinor: uint64(order.DiscountAmountMinor),
		PayableAmountMinor:  uint64(order.PayableAmountMinor),
		Status:              string(order.Status),
	}
}

func mapAttempt(attempt sqlc.PaymentAttempt) Attempt {
	return Attempt{
		ID:                     uint64(attempt.ID),
		WorkspaceID:            attempt.WorkspaceID,
		OrderID:                uint64(attempt.OrderID),
		ProviderCode:           attempt.ProviderCode,
		AssetCode:              attempt.AssetCode,
		AmountMinor:            uint64(attempt.AmountMinor),
		Status:                 string(attempt.Status),
		ProviderPaymentID:      sqlwrap.NullStringPtr(attempt.ProviderPaymentID),
		ProviderInvoiceID:      sqlwrap.NullStringPtr(attempt.ProviderInvoiceID),
		ProviderChargeID:       sqlwrap.NullStringPtr(attempt.ProviderChargeID),
		ProviderSubscriptionID: sqlwrap.NullStringPtr(attempt.ProviderSubscriptionID),
		IdempotencyKey:         sqlwrap.NullStringPtr(attempt.IdempotencyKey),
		RequestFingerprint:     attempt.RequestFingerprint,
		ConfirmationURL:        sqlwrap.NullStringPtr(attempt.ConfirmationUrl),
		ReturnURL:              sqlwrap.NullStringPtr(attempt.ReturnUrl),
		ExpiresAt:              sqlwrap.NullTimePtr(attempt.ExpiresAt),
		CreatedAt:              attempt.CreatedAt,
		UpdatedAt:              attempt.UpdatedAt,
	}
}

func nullInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func normalizedLocale(locale string) string {
	if locale == "" {
		return "ru"
	}
	return locale
}
