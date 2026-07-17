package repository

import (
	"context"
	"database/sql"
	"math"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	serviceerrors "github.com/elum2b/services/errors"
	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

var (
	ErrInvalidPrice        = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment price is invalid")
	ErrInvalidItemQuantity = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment item quantity must be positive",
	)
	ErrInvalidReward = serviceerrors.New(
		serviceerrors.CodeInvalidFields,
		"payment reward type or duration unit is invalid",
	)
	ErrInvalidProduct = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment product is invalid")
)

type ProductGroupUpsertParams struct {
	WorkspaceID    string
	Code           string
	TitleKey       *string
	DescriptionKey *string
	Position       int32
	IsActive       bool
}

type ProductUpsertParams struct {
	WorkspaceID          string
	ID                   string
	GroupCode            *string
	TitleKey             string
	DescriptionKey       *string
	Target               json.RawMessage
	ImageURL             *string
	LinkURL              *string
	SizeLabel            *string
	PeriodSeconds        *int64
	TrialDurationSeconds *int64
	QuantityMode         string
	Position             int32
	GlobalLimit          int32
	GlobalInterval       string
	GlobalIntervalCount  int32
	UserLimit            int32
	UserInterval         string
	UserIntervalCount    int32
	AvailableFrom        *time.Time
	AvailableUntil       *time.Time
	IsVisible            bool
	IsClosed             bool
}

type LocalizationUpsertParams struct {
	WorkspaceID     string
	Locale          string
	LocalizationKey string
	Value           string
}

type ProductItemUpsertParams struct {
	WorkspaceID  string
	ProductID    string
	ItemID       string
	RewardType   string
	Quantity     int64
	Scale        uint16
	DurationUnit *string
}

type ProductPriceCreateParams struct {
	WorkspaceID                  string
	ProductID                    string
	AssetCode                    string
	ListAmountMinor              uint64
	DiscountAmountMinor          uint64
	PricingMode                  string
	ReferenceAssetCode           *string
	ReferenceListAmountMinor     *uint64
	ReferenceDiscountAmountMinor *uint64
	Coefficient                  *string
	IsPromotion                  bool
	StartsAt                     *time.Time
	EndsAt                       *time.Time
}

type ProductPriceUpdateParams struct {
	ID                           uint64
	WorkspaceID                  string
	AssetCode                    string
	ListAmountMinor              uint64
	DiscountAmountMinor          uint64
	PricingMode                  string
	ReferenceAssetCode           *string
	ReferenceListAmountMinor     *uint64
	ReferenceDiscountAmountMinor *uint64
	Coefficient                  *string
	IsPromotion                  bool
	StartsAt                     *time.Time
	EndsAt                       *time.Time
}

func (r *PaymentRepository) UpsertProductGroup(ctx context.Context, params ProductGroupUpsertParams) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		return tx.q.UpsertProductGroup(ctx, paymentsqlc.UpsertProductGroupParams{
			WorkspaceID: workspaceID,
			Code:        params.Code,
			TitleKey: sqlwrap.NullFromPtr(params.TitleKey, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			DescriptionKey: sqlwrap.NullFromPtr(params.DescriptionKey, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			Position: params.Position,
			IsActive: params.IsActive,
		})
	})
	if err != nil {
		return err
	}

	return r.invalidateWorkspaceCache(workspaceID)
}

func (r *PaymentRepository) DeleteProductGroup(ctx context.Context, workspaceID string, code string) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		var err error
		rows, err = tx.q.DeleteProductGroup(ctx, paymentsqlc.DeleteProductGroupParams{
			WorkspaceID: workspaceID,
			Code:        code,
		})
		return err
	})
	if err != nil {
		return 0, err
	}

	return rows, r.invalidateWorkspaceCache(workspaceID)
}

func (r *PaymentRepository) UpsertProduct(ctx context.Context, params ProductUpsertParams) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	if err := target.Validate(params.Target); err != nil {
		return err
	}
	if strings.TrimSpace(params.ID) == "" || strings.TrimSpace(params.TitleKey) == "" ||
		(params.PeriodSeconds != nil && *params.PeriodSeconds < 0) ||
		(params.TrialDurationSeconds != nil && *params.TrialDurationSeconds < 0) ||
		params.GlobalLimit < 0 || params.GlobalIntervalCount < 0 ||
		params.UserLimit < 0 || params.UserIntervalCount < 0 {
		return ErrInvalidProduct
	}
	availableFrom := sqlwrap.ValueFromPtr(params.AvailableFrom)
	if availableFrom.IsZero() {
		availableFrom = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	availableUntil := sqlwrap.ValueFromPtr(params.AvailableUntil)
	if availableUntil.IsZero() {
		availableUntil = time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	globalInterval := params.GlobalInterval
	if globalInterval == "" {
		globalInterval = string(paymentsqlc.PaymentProductGlobalIntervalUNLIMITED)
	}

	userInterval := params.UserInterval
	if userInterval == "" {
		userInterval = string(paymentsqlc.PaymentProductUserIntervalUNLIMITED)
	}
	quantityMode := params.QuantityMode
	if quantityMode == "" {
		quantityMode = string(paymentsqlc.PaymentProductQuantityModeFixed)
	}
	target := params.Target
	if len(target) == 0 {
		target = []byte("null")
	}
	if (quantityMode != "fixed" && quantityMode != "flexible") ||
		!validPaymentInterval(globalInterval) ||
		!validPaymentInterval(userInterval) ||
		!availableFrom.Before(availableUntil) {
		return ErrInvalidProduct
	}

	return r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		if err := tx.q.UpsertProduct(ctx, paymentsqlc.UpsertProductParams{
			WorkspaceID: workspaceID,
			ID:          params.ID,
			GroupCode: sqlwrap.NullFromPtr(params.GroupCode, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			TitleKey: params.TitleKey,
			DescriptionKey: sqlwrap.NullFromPtr(params.DescriptionKey, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			Target: rawMessageParam(target),
			ImageUrl: sqlwrap.NullFromPtr(params.ImageURL, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			LinkUrl: sqlwrap.NullFromPtr(params.LinkURL, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			SizeLabel: sqlwrap.NullFromPtr(params.SizeLabel, func(v string) sql.NullString {
				return sql.NullString{String: v, Valid: true}
			}),
			PeriodSeconds: sqlwrap.NullFromPtr(params.PeriodSeconds, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			TrialDurationSeconds: sqlwrap.NullFromPtr(params.TrialDurationSeconds, func(v int64) sql.NullInt64 {
				return sql.NullInt64{Int64: v, Valid: true}
			}),
			QuantityMode:        paymentsqlc.PaymentProductQuantityMode(quantityMode),
			Position:            params.Position,
			GlobalLimit:         params.GlobalLimit,
			GlobalInterval:      paymentsqlc.PaymentProductGlobalInterval(globalInterval),
			GlobalIntervalCount: params.GlobalIntervalCount,
			UserLimit:           params.UserLimit,
			UserInterval:        paymentsqlc.PaymentProductUserInterval(userInterval),
			UserIntervalCount:   params.UserIntervalCount,
			AvailableFrom:       availableFrom,
			AvailableUntil:      availableUntil,
			IsVisible:           params.IsVisible,
			IsClosed:            params.IsClosed,
		}); err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, params.ID)
	})
}

func (r *PaymentRepository) DeleteProduct(ctx context.Context, workspaceID string, id string) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		var err error
		rows, err = tx.q.DeleteProduct(ctx, paymentsqlc.DeleteProductParams{
			WorkspaceID: workspaceID,
			ID:          id,
		})
		if err != nil {
			return err
		}
		_, err = tx.q.DeleteProductCache(ctx, paymentsqlc.DeleteProductCacheParams{
			WorkspaceID: workspaceID,
			ProductID:   id,
		})
		return err
	})
	if err != nil {
		return 0, err
	}
	return rows, r.invalidateWorkspaceCache(workspaceID)
}

func (r *PaymentRepository) UpsertLocalization(ctx context.Context, params LocalizationUpsertParams) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	return r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		if err := tx.q.UpsertLocalization(ctx, paymentsqlc.UpsertLocalizationParams{
			WorkspaceID:     workspaceID,
			Locale:          params.Locale,
			LocalizationKey: params.LocalizationKey,
			Value:           params.Value,
		}); err != nil {
			return err
		}
		return tx.RebuildWorkspaceProductCache(ctx, workspaceID)
	})
}

func (r *PaymentRepository) DeleteLocalization(
	ctx context.Context,
	workspaceID string,
	locale string,
	localizationKey string,
) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		var err error
		rows, err = tx.q.DeleteLocalization(ctx, paymentsqlc.DeleteLocalizationParams{
			Locale:          locale,
			LocalizationKey: localizationKey,
			WorkspaceID:     workspaceID,
		})
		if err != nil {
			return err
		}
		return tx.RebuildWorkspaceProductCache(ctx, workspaceID)
	})
	return rows, err
}

func (r *PaymentRepository) UpsertProductItem(ctx context.Context, params ProductItemUpsertParams) error {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return err
	}
	if params.Quantity <= 0 || params.Scale > math.MaxInt16 {
		return ErrInvalidItemQuantity
	}
	rewardType := params.RewardType
	if rewardType == "" {
		rewardType = "quantity"
	}
	if !validReward(rewardType, params.DurationUnit) {
		return ErrInvalidReward
	}
	return r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		if err := tx.q.UpsertProductItem(ctx, paymentsqlc.UpsertProductItemParams{
			WorkspaceID: workspaceID,
			ProductID:   params.ProductID,
			ItemID:      params.ItemID,
			RewardType:  paymentsqlc.PaymentProductItemRewardType(rewardType),
			Quantity:    params.Quantity,
			Scale:       int16(params.Scale),
			DurationUnit: paymentsqlc.NullPaymentProductItemDurationUnit{
				PaymentProductItemDurationUnit: paymentsqlc.PaymentProductItemDurationUnit(pointerString(params.DurationUnit)),
				Valid:                          params.DurationUnit != nil,
			},
		}); err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, params.ProductID)
	})
}

func validReward(rewardType string, unit *string) bool {
	if rewardType == "quantity" {
		return unit == nil
	}
	if rewardType != "duration" || unit == nil {
		return false
	}
	switch *unit {
	case "second", "minute", "hour", "day", "week", "month", "year":
		return true
	default:
		return false
	}
}

func pointerString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (r *PaymentRepository) DeleteProductItem(
	ctx context.Context,
	workspaceID string,
	productID string,
	itemID string,
) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		var err error
		rows, err = tx.q.DeleteProductItem(ctx, paymentsqlc.DeleteProductItemParams{
			ProductID:   productID,
			ItemID:      itemID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, productID)
	})
	return rows, err
}

func (r *PaymentRepository) CreateProductPrice(ctx context.Context, params ProductPriceCreateParams) (uint64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}
	startsAt := sqlwrap.ValueFromPtr(params.StartsAt)
	if startsAt.IsZero() {
		startsAt = time.Now().Add(-time.Minute)
	}

	endsAt := sqlwrap.ValueFromPtr(params.EndsAt)
	if endsAt.IsZero() {
		endsAt = time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if strings.TrimSpace(params.ProductID) == "" || strings.TrimSpace(params.AssetCode) == "" ||
		!startsAt.Before(endsAt) {
		return 0, ErrInvalidPrice
	}

	var id int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		amounts, err := tx.resolveProductPriceAmounts(ctx, workspaceID, productPriceInput{
			AssetCode: params.AssetCode, ListAmountMinor: params.ListAmountMinor,
			DiscountAmountMinor: params.DiscountAmountMinor, PricingMode: params.PricingMode,
			ReferenceAssetCode:           params.ReferenceAssetCode,
			ReferenceListAmountMinor:     params.ReferenceListAmountMinor,
			ReferenceDiscountAmountMinor: params.ReferenceDiscountAmountMinor,
			Coefficient:                  params.Coefficient,
		})
		if err != nil {
			return err
		}
		if amounts.dynamic {
			id, err = tx.createDynamicProductPrice(ctx, workspaceID, params, amounts, startsAt, endsAt)
		} else {
			id, err = tx.q.CreateProductPrice(ctx, paymentsqlc.CreateProductPriceParams{
				WorkspaceID: workspaceID, ProductID: params.ProductID, AssetCode: params.AssetCode,
				ListAmountMinor: int64(amounts.list), DiscountAmountMinor: int64(amounts.discount),
				IsPromotion: params.IsPromotion, StartsAt: startsAt, EndsAt: endsAt,
			})
		}
		if err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, params.ProductID)
	})
	if err != nil {
		return 0, err
	}

	return uint64(id), nil
}

func (r *PaymentRepository) UpdateProductPrice(ctx context.Context, params ProductPriceUpdateParams) (int64, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return 0, err
	}
	startsAt := sqlwrap.ValueFromPtr(params.StartsAt)
	if startsAt.IsZero() {
		startsAt = time.Now().Add(-time.Minute)
	}

	endsAt := sqlwrap.ValueFromPtr(params.EndsAt)
	if endsAt.IsZero() {
		endsAt = time.Date(2124, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if params.ID == 0 || params.ID > math.MaxInt64 || strings.TrimSpace(params.AssetCode) == "" ||
		!startsAt.Before(endsAt) {
		return 0, ErrInvalidPrice
	}

	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		productID, err := tx.q.GetProductPriceProductID(ctx, paymentsqlc.GetProductPriceProductIDParams{
			WorkspaceID: workspaceID,
			ID:          int64(params.ID),
		})
		if err != nil {
			return err
		}
		amounts, err := tx.resolveProductPriceAmounts(ctx, workspaceID, productPriceInput{
			AssetCode: params.AssetCode, ListAmountMinor: params.ListAmountMinor,
			DiscountAmountMinor: params.DiscountAmountMinor, PricingMode: params.PricingMode,
			ReferenceAssetCode:           params.ReferenceAssetCode,
			ReferenceListAmountMinor:     params.ReferenceListAmountMinor,
			ReferenceDiscountAmountMinor: params.ReferenceDiscountAmountMinor,
			Coefficient:                  params.Coefficient,
		})
		if err != nil {
			return err
		}
		if amounts.dynamic {
			rows, err = tx.updateDynamicProductPrice(ctx, workspaceID, params, amounts, startsAt, endsAt)
		} else {
			rows, err = tx.q.UpdateProductPrice(ctx, paymentsqlc.UpdateProductPriceParams{
				ID: int64(params.ID), WorkspaceID: workspaceID, AssetCode: params.AssetCode,
				ListAmountMinor: int64(amounts.list), DiscountAmountMinor: int64(amounts.discount),
				IsPromotion: params.IsPromotion, StartsAt: startsAt, EndsAt: endsAt,
			})
		}
		if err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, productID)
	})
	return rows, err
}

func (r *PaymentRepository) DeleteProductPrice(ctx context.Context, workspaceID string, id uint64) (int64, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return 0, err
	}
	if id == 0 || id > math.MaxInt64 {
		return 0, ErrInvalidPrice
	}
	var rows int64
	err = r.withWorkspaceMutation(ctx, workspaceID, func(tx *PaymentRepository) error {
		productID, err := tx.q.GetProductPriceProductID(ctx, paymentsqlc.GetProductPriceProductIDParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return err
		}
		rows, err = tx.q.DeleteProductPrice(ctx, paymentsqlc.DeleteProductPriceParams{
			WorkspaceID: workspaceID,
			ID:          int64(id),
		})
		if err != nil {
			return err
		}
		return tx.RebuildProductCache(ctx, workspaceID, productID)
	})
	return rows, err
}
