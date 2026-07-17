package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
	paymentsqlc "github.com/elum2b/services/payment/sqlc"
)

const (
	PricingModeFixed   = "fixed"
	PricingModeDynamic = "dynamic"
	USDTAssetCode      = "USDT_TON"
)

var (
	ErrInvalidPricingMode = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment pricing mode is invalid")
	ErrInvalidAssetRate   = serviceerrors.New(serviceerrors.CodeInvalidFields, "payment asset rate is invalid")
	ErrAssetRateNotFound  = serviceerrors.New(serviceerrors.CodeNotFound, "payment asset rate not found")
)

type productPriceInput struct {
	AssetCode                    string
	ListAmountMinor              uint64
	DiscountAmountMinor          uint64
	PricingMode                  string
	ReferenceAssetCode           *string
	ReferenceListAmountMinor     *uint64
	ReferenceDiscountAmountMinor *uint64
	Coefficient                  *string
}

type resolvedProductPrice struct {
	dynamic           bool
	list              uint64
	discount          uint64
	referenceAsset    string
	referenceList     uint64
	referenceDiscount uint64
	coefficient       string
}

type AssetRateUpdateParams struct {
	AssetCode              string
	ReferenceAssetCode     string
	ReferencePerAssetMinor uint64
	Source                 string
	ObservedAt             time.Time
}

type AssetRateUpdateResult struct {
	UpdatedPrices      uint64
	AffectedProducts   uint64
	AffectedWorkspaces uint64
}

func (r *PaymentRepository) GetAssetUSDTPrice(
	ctx context.Context,
	assetCode string,
) (paymentsqlc.GetAssetUSDTPriceRow, error) {
	return r.q.GetAssetUSDTPrice(ctx, paymentsqlc.GetAssetUSDTPriceParams{
		AssetCode:          strings.TrimSpace(assetCode),
		ReferenceAssetCode: USDTAssetCode,
	})
}

func (r *PaymentRepository) ListAssetUSDTPrices(
	ctx context.Context,
) ([]paymentsqlc.ListAssetUSDTPricesRow, error) {
	return r.q.ListAssetUSDTPrices(ctx, USDTAssetCode)
}

func (r *PaymentRepository) resolveProductPriceAmounts(
	ctx context.Context,
	workspaceID string,
	input productPriceInput,
) (resolvedProductPrice, error) {
	mode := strings.ToLower(strings.TrimSpace(input.PricingMode))
	if mode == "" {
		mode = PricingModeFixed
	}
	if mode == PricingModeFixed {
		if input.ListAmountMinor > math.MaxInt64 || input.DiscountAmountMinor > input.ListAmountMinor {
			return resolvedProductPrice{}, ErrInvalidPrice
		}
		return resolvedProductPrice{
			list:     input.ListAmountMinor,
			discount: input.DiscountAmountMinor,
		}, nil
	}
	if mode != PricingModeDynamic {
		return resolvedProductPrice{}, ErrInvalidPricingMode
	}
	if input.ReferenceAssetCode == nil ||
		input.ReferenceListAmountMinor == nil ||
		input.ReferenceDiscountAmountMinor == nil ||
		input.Coefficient == nil {
		return resolvedProductPrice{}, ErrInvalidPrice
	}

	referenceAsset := strings.TrimSpace(*input.ReferenceAssetCode)
	coefficient := strings.TrimSpace(*input.Coefficient)
	if referenceAsset == "" || referenceAsset == input.AssetCode ||
		*input.ReferenceDiscountAmountMinor > *input.ReferenceListAmountMinor ||
		*input.ReferenceListAmountMinor > math.MaxInt64 ||
		*input.ReferenceDiscountAmountMinor > math.MaxInt64 {
		return resolvedProductPrice{}, ErrInvalidPrice
	}

	rate, err := r.q.GetAssetRateForPricing(ctx, paymentsqlc.GetAssetRateForPricingParams{
		AssetCode:          input.AssetCode,
		ReferenceAssetCode: referenceAsset,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return resolvedProductPrice{}, ErrAssetRateNotFound
	}
	if err != nil {
		return resolvedProductPrice{}, err
	}

	list, err := convertReferenceAmount(
		*input.ReferenceListAmountMinor,
		uint16(rate.TargetScale),
		uint64(rate.ReferencePerAssetMinor),
		coefficient,
	)
	if err != nil {
		return resolvedProductPrice{}, err
	}
	discount, err := convertReferenceAmount(
		*input.ReferenceDiscountAmountMinor,
		uint16(rate.TargetScale),
		uint64(rate.ReferencePerAssetMinor),
		coefficient,
	)
	if err != nil {
		return resolvedProductPrice{}, err
	}
	if discount > list {
		return resolvedProductPrice{}, ErrInvalidPrice
	}
	if list > math.MaxInt64 {
		return resolvedProductPrice{}, ErrInvalidPrice
	}

	return resolvedProductPrice{
		dynamic:           true,
		list:              list,
		discount:          discount,
		referenceAsset:    referenceAsset,
		referenceList:     *input.ReferenceListAmountMinor,
		referenceDiscount: *input.ReferenceDiscountAmountMinor,
		coefficient:       coefficient,
	}, nil
}

func (r *PaymentRepository) createDynamicProductPrice(
	ctx context.Context,
	workspaceID string,
	params ProductPriceCreateParams,
	amounts resolvedProductPrice,
	startsAt time.Time,
	endsAt time.Time,
) (int64, error) {
	return r.q.CreateDynamicProductPrice(ctx, paymentsqlc.CreateDynamicProductPriceParams{
		WorkspaceID:                  workspaceID,
		ProductID:                    params.ProductID,
		AssetCode:                    params.AssetCode,
		ListAmountMinor:              int64(amounts.list),
		DiscountAmountMinor:          int64(amounts.discount),
		ReferenceAssetCode:           sql.NullString{String: amounts.referenceAsset, Valid: true},
		ReferenceListAmountMinor:     sql.NullInt64{Int64: int64(amounts.referenceList), Valid: true},
		ReferenceDiscountAmountMinor: sql.NullInt64{Int64: int64(amounts.referenceDiscount), Valid: true},
		Coefficient:                  sql.NullString{String: amounts.coefficient, Valid: true},
		IsPromotion:                  params.IsPromotion,
		StartsAt:                     startsAt,
		EndsAt:                       endsAt,
	})
}

func (r *PaymentRepository) updateDynamicProductPrice(
	ctx context.Context,
	workspaceID string,
	params ProductPriceUpdateParams,
	amounts resolvedProductPrice,
	startsAt time.Time,
	endsAt time.Time,
) (int64, error) {
	return r.q.UpdateDynamicProductPrice(ctx, paymentsqlc.UpdateDynamicProductPriceParams{
		ID:                           int64(params.ID),
		WorkspaceID:                  workspaceID,
		AssetCode:                    params.AssetCode,
		ListAmountMinor:              int64(amounts.list),
		DiscountAmountMinor:          int64(amounts.discount),
		ReferenceAssetCode:           sql.NullString{String: amounts.referenceAsset, Valid: true},
		ReferenceListAmountMinor:     sql.NullInt64{Int64: int64(amounts.referenceList), Valid: true},
		ReferenceDiscountAmountMinor: sql.NullInt64{Int64: int64(amounts.referenceDiscount), Valid: true},
		Coefficient:                  sql.NullString{String: amounts.coefficient, Valid: true},
		IsPromotion:                  params.IsPromotion,
		StartsAt:                     startsAt,
		EndsAt:                       endsAt,
	})
}

func (r *PaymentRepository) UpdateAssetRate(
	ctx context.Context,
	params AssetRateUpdateParams,
) (AssetRateUpdateResult, error) {
	params.AssetCode = strings.TrimSpace(params.AssetCode)
	params.ReferenceAssetCode = strings.TrimSpace(params.ReferenceAssetCode)
	params.Source = strings.TrimSpace(params.Source)
	if params.AssetCode == "" || params.ReferenceAssetCode == "" ||
		params.AssetCode == params.ReferenceAssetCode || params.Source == "" ||
		params.ReferencePerAssetMinor == 0 || params.ReferencePerAssetMinor > math.MaxInt64 {
		return AssetRateUpdateResult{}, ErrInvalidAssetRate
	}
	if params.ObservedAt.IsZero() {
		params.ObservedAt = time.Now().UTC()
	}

	var result AssetRateUpdateResult
	affectedWorkspaces := make(map[string]struct{})
	err := r.inTransaction(ctx, func(tx *PaymentRepository) error {
		if err := tx.q.UpsertAssetRate(ctx, paymentsqlc.UpsertAssetRateParams{
			AssetCode:              params.AssetCode,
			ReferenceAssetCode:     params.ReferenceAssetCode,
			ReferencePerAssetMinor: int64(params.ReferencePerAssetMinor),
			Source:                 params.Source,
			ObservedAt:             params.ObservedAt,
		}); err != nil {
			return err
		}

		rate, err := tx.q.GetAssetRateForPricing(ctx, paymentsqlc.GetAssetRateForPricingParams{
			AssetCode:          params.AssetCode,
			ReferenceAssetCode: params.ReferenceAssetCode,
		})
		if err != nil {
			return err
		}
		prices, err := tx.q.ListDynamicPricesForRate(ctx, paymentsqlc.ListDynamicPricesForRateParams{
			AssetCode:          params.AssetCode,
			ReferenceAssetCode: sql.NullString{String: params.ReferenceAssetCode, Valid: true},
		})
		if err != nil {
			return err
		}

		products := make(map[string]struct{}, len(prices))
		for _, price := range prices {
			if !price.ReferenceListAmountMinor.Valid ||
				!price.ReferenceDiscountAmountMinor.Valid ||
				!price.Coefficient.Valid ||
				price.ReferenceListAmountMinor.Int64 < 0 ||
				price.ReferenceDiscountAmountMinor.Int64 < 0 {
				return ErrInvalidPrice
			}
			list, err := convertReferenceAmount(
				uint64(price.ReferenceListAmountMinor.Int64),
				uint16(rate.TargetScale),
				uint64(rate.ReferencePerAssetMinor),
				price.Coefficient.String,
			)
			if err != nil {
				return err
			}
			discount, err := convertReferenceAmount(
				uint64(price.ReferenceDiscountAmountMinor.Int64),
				uint16(rate.TargetScale),
				uint64(rate.ReferencePerAssetMinor),
				price.Coefficient.String,
			)
			if err != nil {
				return err
			}
			rows, err := tx.q.UpdateDynamicPriceAmounts(ctx, paymentsqlc.UpdateDynamicPriceAmountsParams{
				ListAmountMinor:     int64(list),
				DiscountAmountMinor: int64(discount),
				WorkspaceID:         price.WorkspaceID,
				ID:                  price.ID,
			})
			if err != nil {
				return err
			}
			result.UpdatedPrices += uint64(rows)
			products[price.WorkspaceID+"\x00"+price.ProductID] = struct{}{}
			affectedWorkspaces[price.WorkspaceID] = struct{}{}
		}

		for workspaceID := range affectedWorkspaces {
			if _, err := tx.q.DeleteWorkspaceProductCache(ctx, workspaceID); err != nil {
				return err
			}
			if err := tx.q.RebuildWorkspaceProductCache(ctx, paymentsqlc.RebuildWorkspaceProductCacheParams{
				WorkspaceID:   workspaceID,
				WorkspaceID_2: workspaceID,
			}); err != nil {
				return err
			}
		}
		result.AffectedProducts = uint64(len(products))
		result.AffectedWorkspaces = uint64(len(affectedWorkspaces))
		return nil
	})
	if err != nil {
		return AssetRateUpdateResult{}, err
	}
	for workspaceID := range affectedWorkspaces {
		if err := r.invalidateWorkspaceCache(workspaceID); err != nil {
			return AssetRateUpdateResult{}, err
		}
	}
	return result, nil
}

func convertReferenceAmount(
	referenceAmount uint64,
	targetScale uint16,
	referencePerAssetMinor uint64,
	coefficient string,
) (uint64, error) {
	if referencePerAssetMinor == 0 {
		return 0, ErrInvalidAssetRate
	}
	factor, err := positiveRat(coefficient)
	if err != nil {
		return 0, ErrInvalidPrice
	}

	value := new(big.Rat).SetInt(new(big.Int).SetUint64(referenceAmount))
	value.Mul(value, new(big.Rat).SetInt(pow10(targetScale)))
	value.Mul(value, factor)
	value.Quo(value, new(big.Rat).SetInt(new(big.Int).SetUint64(referencePerAssetMinor)))

	quotient, remainder := new(big.Int).QuoRem(value.Num(), value.Denom(), new(big.Int))
	if remainder.Sign() > 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if quotient.Sign() < 0 || !quotient.IsUint64() {
		return 0, ErrInvalidPrice
	}
	return quotient.Uint64(), nil
}

func positiveRat(value string) (*big.Rat, error) {
	rat, ok := new(big.Rat).SetString(strings.TrimSpace(value))
	if !ok || rat.Sign() <= 0 {
		return nil, ErrInvalidAssetRate
	}
	return rat, nil
}

func pow10(scale uint16) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(scale)), nil)
}
