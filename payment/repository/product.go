package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	json "github.com/goccy/go-json"
	"strings"
	"time"

	sqlwrap "github.com/elum2b/services/internal/utils/sql"
	"github.com/elum2b/services/internal/utils/target"
	"github.com/elum2b/services/payment/sqlc"
)

type ProductGetParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	Platform       string
	PlatformUserID string
	IsPremium      bool
	Sex            string
	Country        string
	ProductID      string
	AssetCode      string
	Locale         string
	Now            time.Time
}

type ProductGetByKeyParams struct {
	Key       string
	AssetCode string
	Locale    string
	Now       time.Time
}

type ProductListParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	Platform       string
	PlatformUserID string
	IsPremium      bool
	Sex            string
	Country        string
	GroupCode      string
	AssetCode      string
	Locale         string
	Now            time.Time
}

type ProductCreateKeyParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	InternalUserID *int64
	ProductID      string
	MaxUses        int32
	ExpiresAt      *time.Time
}

type Product struct {
	WorkspaceID          string
	ID                   string
	LinkURL              sql.NullString
	SizeLabel            sql.NullString
	GroupCode            sql.NullString
	Target               json.RawMessage
	Title                string
	Description          string
	ImageURL             sql.NullString
	PeriodSeconds        sql.NullInt64
	TrialDurationSeconds sql.NullInt64
	QuantityMode         string
	Price                ProductPrice
	Limit                ProductLimit
	Items                []ProductItem
}

type ProductPurchaseKey struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	InternalUserID sql.NullInt64
	ProductID      string
}

type ProductPreview struct {
	WorkspaceID          string
	ID                   string
	LinkURL              sql.NullString
	SizeLabel            sql.NullString
	GroupCode            sql.NullString
	Title                string
	Description          string
	ImageURL             sql.NullString
	PeriodSeconds        sql.NullInt64
	TrialDurationSeconds sql.NullInt64
	QuantityMode         string
	Limit                ProductLimit
	Items                []ProductItem
}

type ProductPriceOption struct {
	PriceID             uint64
	ProductID           string
	AssetCode           string
	AssetTitle          string
	AssetKind           string
	Scale               uint16
	Chain               sql.NullString
	Network             sql.NullString
	ContractAddress     sql.NullString
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	PayableAmountMinor  uint64
	ProviderCodes       []string
}

type ProductPrice struct {
	ID                  uint64
	AssetCode           string
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	PayableAmountMinor  uint64
}

type ProductLimit struct {
	Global ProductLimitRule
	User   ProductLimitRule
}

type ProductLimitRule struct {
	Limit         int32
	Interval      string
	IntervalCount int32
	LockUntil     sql.NullTime
}

type ProductItem struct {
	ID           string
	Quantity     int64
	Scale        uint16
	RewardType   string
	DurationUnit *string
}

func (r *PaymentRepository) GetProduct(ctx context.Context, params ProductGetParams) (Product, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Product{}, err
	}
	locale := params.Locale
	if locale == "" {
		locale = "ru"
	}

	now, err := r.catalogNow(ctx, params.Now)
	if err != nil {
		return Product{}, err
	}

	product, err := r.getProductCatalog(ctx, workspaceID, params.ProductID, params.AssetCode, locale, now)
	if err != nil {
		return Product{}, err
	}
	if !productTargetMatches(
		product.Target,
		params.IsPremium,
		params.Sex,
		params.Country,
		locale,
		params.Platform,
		params.PlatformID,
	) {
		return Product{}, sql.ErrNoRows
	}

	if err := r.attachProductLimitLocks(ctx, &product, params.PlatformID, params.PlatformUserID); err != nil {
		return Product{}, err
	}

	return product, nil
}

func (r *PaymentRepository) ListProducts(ctx context.Context, params ProductListParams) ([]Product, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return nil, err
	}
	locale := normalizedLocale(params.Locale)
	now, err := r.catalogNow(ctx, params.Now)
	if err != nil {
		return nil, err
	}

	key := paymentCacheKey("products_catalog", workspaceID, params.AssetCode, locale, params.GroupCode)
	rows, err := queryPaymentCache(
		ctx,
		r,
		workspaceID,
		key,
		func(ctx context.Context) ([]sqlc.ListProductsCatalogCacheRowsRow, error) {
			return r.q.ListProductsCatalogCacheRows(ctx, sqlc.ListProductsCatalogCacheRowsParams{
				WorkspaceID: workspaceID,
				AssetCode:   params.AssetCode,
				Locale:      locale,
				Column4:     params.GroupCode,
				GroupCode:   sql.NullString{String: params.GroupCode, Valid: params.GroupCode != ""},
			})
		},
	)
	if err != nil {
		return nil, err
	}

	products := mapProductsCatalogRows(rows, now)
	products = filterProductsByTarget(
		products,
		params.IsPremium,
		params.Sex,
		params.Country,
		locale,
		params.Platform,
		params.PlatformID,
	)
	if len(products) == 0 {
		return []Product{}, nil
	}
	if err := r.attachProductsLimitLocks(ctx, products, workspaceID, params.PlatformID, params.PlatformUserID, now); err != nil {
		return nil, err
	}
	return products, nil
}

func (r *PaymentRepository) getProductCatalog(
	ctx context.Context,
	workspaceID string,
	productID string,
	assetCode string,
	locale string,
	now time.Time,
) (Product, error) {
	key := paymentCacheKey("product_catalog", workspaceID, productID, assetCode, locale)
	rows, err := queryPaymentCache(
		ctx,
		r,
		workspaceID,
		key,
		func(ctx context.Context) ([]sqlc.ListProductCatalogCacheRowsRow, error) {
			return r.q.ListProductCatalogCacheRows(ctx, sqlc.ListProductCatalogCacheRowsParams{
				ProductID:   productID,
				WorkspaceID: workspaceID,
				AssetCode:   assetCode,
				Locale:      locale,
			})
		},
	)
	if err != nil {
		return Product{}, err
	}
	return mapProductCatalogRows(rows, now)
}

func (r *PaymentRepository) attachProductLimitLocks(
	ctx context.Context,
	product *Product,
	platformID int64,
	platformUserID string,
) error {
	var err error
	product.Limit.Global.LockUntil, err = r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     platformID,
		platformUserID: "",
		productID:      product.ID,
		limit:          product.Limit.Global.Limit,
		interval:       product.Limit.Global.Interval,
		intervalCount:  product.Limit.Global.IntervalCount,
	})
	if err != nil {
		return err
	}

	product.Limit.User.LockUntil, err = r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     platformID,
		platformUserID: platformUserID,
		productID:      product.ID,
		limit:          product.Limit.User.Limit,
		interval:       product.Limit.User.Interval,
		intervalCount:  product.Limit.User.IntervalCount,
	})
	if err != nil {
		return err
	}
	return nil
}

func (r *PaymentRepository) getCheckoutProduct(ctx context.Context, params ProductGetParams) (Product, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return Product{}, err
	}
	locale := normalizedLocale(params.Locale)

	now, err := r.catalogNow(ctx, params.Now)
	if err != nil {
		return Product{}, err
	}
	product, err := r.getProductCatalog(ctx, workspaceID, params.ProductID, params.AssetCode, locale, now)
	if err != nil {
		return Product{}, err
	}
	if !productTargetMatches(
		product.Target,
		params.IsPremium,
		params.Sex,
		params.Country,
		locale,
		params.Platform,
		params.PlatformID,
	) {
		return Product{}, sql.ErrNoRows
	}
	if err := r.attachProductLimitLocks(ctx, &product, params.PlatformID, params.PlatformUserID); err != nil {
		return Product{}, err
	}

	return product, nil
}

func (r *PaymentRepository) GetProductByKey(ctx context.Context, params ProductGetByKeyParams) (Product, error) {
	now := params.Now
	if now.IsZero() {
		now = time.Now()
	}

	key, err := r.q.GetPurchaseKeyByHash(ctx, hashPurchaseKey(params.Key))
	if err != nil {
		return Product{}, err
	}
	if !isPurchaseKeyUsable(key, now) {
		return Product{}, sql.ErrNoRows
	}

	return r.GetProduct(ctx, ProductGetParams{
		WorkspaceID:    key.WorkspaceID,
		AppID:          key.AppID,
		PlatformID:     key.PlatformID,
		PlatformUserID: key.PlatformUserID,
		ProductID:      key.ProductID,
		AssetCode:      params.AssetCode,
		Locale:         params.Locale,
		Now:            now,
	})
}

type ProductPreviewParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	ProductID      string
	Locale         string
	Now            time.Time
}

func (r *PaymentRepository) GetProductPreview(
	ctx context.Context,
	params ProductPreviewParams,
) (ProductPreview, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return ProductPreview{}, err
	}
	locale := params.Locale
	if locale == "" {
		locale = "ru"
	}

	now, err := r.catalogNow(ctx, params.Now)
	if err != nil {
		return ProductPreview{}, err
	}

	key := paymentCacheKey("product_preview_catalog", workspaceID, params.ProductID, locale)
	rows, err := queryPaymentCache(
		ctx,
		r,
		workspaceID,
		key,
		func(ctx context.Context) ([]sqlc.ListProductPreviewCatalogCacheRowsRow, error) {
			return r.q.ListProductPreviewCatalogCacheRows(ctx, sqlc.ListProductPreviewCatalogCacheRowsParams{
				ProductID:   params.ProductID,
				WorkspaceID: workspaceID,
				Locale:      locale,
			})
		},
	)
	if err != nil {
		return ProductPreview{}, err
	}
	product, err := mapProductPreviewCatalogRows(rows, now)
	if err != nil {
		return ProductPreview{}, err
	}

	product.Limit.Global.LockUntil, err = r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     params.PlatformID,
		platformUserID: "",
		productID:      product.ID,
		limit:          product.Limit.Global.Limit,
		interval:       product.Limit.Global.Interval,
		intervalCount:  product.Limit.Global.IntervalCount,
	})
	if err != nil {
		return ProductPreview{}, err
	}

	product.Limit.User.LockUntil, err = r.getProductLimitLock(ctx, productLimitQuery{
		workspaceID:    product.WorkspaceID,
		platformID:     params.PlatformID,
		platformUserID: params.PlatformUserID,
		productID:      product.ID,
		limit:          product.Limit.User.Limit,
		interval:       product.Limit.User.Interval,
		intervalCount:  product.Limit.User.IntervalCount,
	})
	if err != nil {
		return ProductPreview{}, err
	}

	return product, nil
}

func (r *PaymentRepository) ListProductPriceOptions(
	ctx context.Context,
	workspaceID string,
	productID string,
) ([]ProductPriceOption, error) {
	workspaceID, err := requireWorkspaceID(workspaceID)
	if err != nil {
		return nil, err
	}
	now, err := r.catalogNow(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	key := paymentCacheKey("product_price_options", workspaceID, productID)
	rows, err := queryPaymentCache(
		ctx,
		r,
		workspaceID,
		key,
		func(ctx context.Context) ([]sqlc.ListProductPriceOptionCatalogRowsRow, error) {
			return r.q.ListProductPriceOptionCatalogRows(ctx, sqlc.ListProductPriceOptionCatalogRowsParams{
				WorkspaceID: workspaceID,
				ProductID:   productID,
			})
		},
	)
	if err != nil {
		return nil, err
	}
	options := make([]ProductPriceOption, 0, len(rows))
	for _, row := range rows {
		if now.Before(row.StartsAt) || now.After(row.EndsAt) {
			continue
		}
		options = append(options, ProductPriceOption{
			PriceID:             uint64(row.PriceID),
			ProductID:           row.ProductID,
			AssetCode:           row.AssetCode,
			AssetTitle:          row.AssetTitle,
			AssetKind:           string(row.AssetKind),
			Scale:               uint16(row.Scale),
			Chain:               row.Chain,
			Network:             row.Network,
			ContractAddress:     row.ContractAddress,
			ListAmountMinor:     uint64(row.ListAmountMinor),
			DiscountAmountMinor: uint64(row.DiscountAmountMinor),
			PayableAmountMinor:  uint64(row.ListAmountMinor - row.DiscountAmountMinor),
			ProviderCodes:       splitProviderCodes(row.ProviderCodes),
		})
	}
	return options, nil
}

func (r *PaymentRepository) catalogNow(ctx context.Context, value time.Time) (time.Time, error) {
	if !value.IsZero() {
		return value, nil
	}
	return r.databaseNow(ctx)
}

func mapProductCatalogRows(rows []sqlc.ListProductCatalogCacheRowsRow, now time.Time) (Product, error) {
	if len(rows) == 0 {
		return Product{}, sql.ErrNoRows
	}

	selected, ok := selectProductCatalogPrice(rows, now)
	if !ok {
		return Product{}, sql.ErrNoRows
	}

	product := Product{
		WorkspaceID:          selected.WorkspaceID,
		ID:                   selected.ProductID,
		LinkURL:              selected.LinkUrl,
		SizeLabel:            selected.SizeLabel,
		GroupCode:            selected.GroupCode,
		Target:               nullRawMessage(selected.Target),
		Title:                selected.ProductTitle,
		Description:          selected.ProductDescription,
		ImageURL:             selected.ImageUrl,
		PeriodSeconds:        selected.PeriodSeconds,
		TrialDurationSeconds: selected.TrialDurationSeconds,
		QuantityMode:         string(selected.QuantityMode),
		Price: ProductPrice{
			ID:                  uint64(selected.PriceID),
			AssetCode:           selected.AssetCode,
			ListAmountMinor:     uint64(selected.ListAmountMinor),
			DiscountAmountMinor: uint64(selected.DiscountAmountMinor),
			PayableAmountMinor:  uint64(selected.ListAmountMinor - selected.DiscountAmountMinor),
		},
		Limit: ProductLimit{
			Global: ProductLimitRule{
				Limit:         selected.GlobalLimit,
				Interval:      string(selected.GlobalInterval),
				IntervalCount: selected.GlobalIntervalCount,
			},
			User: ProductLimitRule{
				Limit:         selected.UserLimit,
				Interval:      string(selected.UserInterval),
				IntervalCount: selected.UserIntervalCount,
			},
		},
		Items: make([]ProductItem, 0, len(rows)),
	}

	for _, row := range rows {
		if row.PriceID != selected.PriceID || row.ItemID == "" {
			continue
		}
		product.Items = append(product.Items, ProductItem{
			ID:           row.ItemID,
			Quantity:     row.ItemQuantity,
			Scale:        uint16(row.ItemScale),
			RewardType:   string(row.RewardType),
			DurationUnit: paymentCacheDurationUnitPtr(row.DurationUnit),
		})
	}

	return product, nil
}

func mapProductsCatalogRows(rows []sqlc.ListProductsCatalogCacheRowsRow, now time.Time) []Product {
	products := make([]Product, 0)
	for start := 0; start < len(rows); {
		end := start + 1
		for end < len(rows) && rows[end].ProductID == rows[start].ProductID {
			end++
		}
		if product, ok := mapProductsCatalogGroup(rows[start:end], now); ok {
			products = append(products, product)
		}
		start = end
	}
	return products
}

func mapProductsCatalogGroup(rows []sqlc.ListProductsCatalogCacheRowsRow, now time.Time) (Product, bool) {
	var selected sqlc.ListProductsCatalogCacheRowsRow
	found := false
	for _, row := range rows {
		if productCatalogRowActive(
			row.IsVisible,
			row.IsClosed,
			row.AvailableFrom,
			row.AvailableUntil,
			row.PriceStartsAt,
			row.PriceEndsAt,
			now,
		) {
			selected = row
			found = true
			break
		}
	}
	if !found {
		return Product{}, false
	}

	product := Product{
		WorkspaceID:          selected.WorkspaceID,
		ID:                   selected.ProductID,
		LinkURL:              selected.LinkUrl,
		SizeLabel:            selected.SizeLabel,
		GroupCode:            selected.GroupCode,
		Title:                selected.ProductTitle,
		Description:          selected.ProductDescription,
		ImageURL:             selected.ImageUrl,
		PeriodSeconds:        selected.PeriodSeconds,
		TrialDurationSeconds: selected.TrialDurationSeconds,
		QuantityMode:         string(selected.QuantityMode),
		Price: ProductPrice{
			ID:                  uint64(selected.PriceID),
			AssetCode:           selected.AssetCode,
			ListAmountMinor:     uint64(selected.ListAmountMinor),
			DiscountAmountMinor: uint64(selected.DiscountAmountMinor),
			PayableAmountMinor:  uint64(selected.ListAmountMinor - selected.DiscountAmountMinor),
		},
		Limit: ProductLimit{
			Global: ProductLimitRule{
				Limit:         selected.GlobalLimit,
				Interval:      string(selected.GlobalInterval),
				IntervalCount: selected.GlobalIntervalCount,
			},
			User: ProductLimitRule{
				Limit:         selected.UserLimit,
				Interval:      string(selected.UserInterval),
				IntervalCount: selected.UserIntervalCount,
			},
		},
		Items: make([]ProductItem, 0, len(rows)),
	}

	for _, row := range rows {
		if row.PriceID != selected.PriceID || row.ItemID == "" {
			continue
		}
		product.Items = append(product.Items, ProductItem{
			ID:           row.ItemID,
			Quantity:     row.ItemQuantity,
			Scale:        uint16(row.ItemScale),
			RewardType:   string(row.RewardType),
			DurationUnit: listProductsDurationUnitPtr(row.DurationUnit),
		})
	}
	return product, true
}

func filterProductsByTarget(
	products []Product,
	isPremium bool,
	sex, country, locale, platform string,
	platformID int64,
) []Product {
	filtered := products[:0]
	for _, product := range products {
		if productTargetMatches(product.Target, isPremium, sex, country, locale, platform, platformID) {
			product.Target = nil
			filtered = append(filtered, product)
		}
	}
	return filtered
}

func productTargetMatches(
	raw json.RawMessage,
	isPremium bool,
	sex, country, locale, platform string,
	platformID int64,
) bool {
	return target.Match(raw, target.Context{
		IsPremium:  isPremium,
		Sex:        sex,
		Country:    country,
		Locale:     locale,
		Platform:   platform,
		PlatformID: platformID,
	})
}

func listProductsDurationUnitPtr(value sqlc.NullPaymentProductCacheDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.PaymentProductCacheDurationUnit)
	return &unit
}

func (r *PaymentRepository) attachProductsLimitLocks(
	ctx context.Context,
	products []Product,
	workspaceID string,
	platformID int64,
	platformUserID string,
	now time.Time,
) error {
	rows, err := r.q.ListActiveProductLimitCounters(ctx, sqlc.ListActiveProductLimitCountersParams{
		WorkspaceID:    workspaceID,
		PlatformID:     platformID,
		PlatformUserID: platformUserID,
		WindowStart:    now,
		WindowEnd:      now,
	})
	if err != nil {
		return err
	}

	counts := make(map[string]uint64, len(rows))
	for _, row := range rows {
		counts[productLimitCounterKey(row.ProductID, string(row.CounterScope), row.PlatformUserID, row.WindowStart, row.WindowEnd)] = uint64(
			row.PaidCount,
		)
	}

	for index := range products {
		attachProductListLimitLock(&products[index].Limit.Global, products[index].ID, "global", "", now, counts)
		attachProductListLimitLock(&products[index].Limit.User, products[index].ID, "user", platformUserID, now, counts)
	}
	return nil
}

func attachProductListLimitLock(
	rule *ProductLimitRule,
	productID string,
	scope string,
	platformUserID string,
	now time.Time,
	counts map[string]uint64,
) {
	if rule.Limit <= 0 || rule.Interval == "UNLIMITED" {
		return
	}
	start, end, ok := limitWindow(rule.Interval, rule.IntervalCount, now)
	if !ok {
		return
	}
	if counts[productLimitCounterKey(productID, scope, platformUserID, start, end)] >= uint64(rule.Limit) {
		rule.LockUntil = sql.NullTime{Time: end, Valid: true}
	}
}

func productLimitCounterKey(
	productID string,
	scope string,
	platformUserID string,
	start time.Time,
	end time.Time,
) string {
	return productID + "\x00" + scope + "\x00" + platformUserID + "\x00" + start.Format(
		time.RFC3339Nano,
	) + "\x00" + end.Format(
		time.RFC3339Nano,
	)
}

func selectProductCatalogPrice(
	rows []sqlc.ListProductCatalogCacheRowsRow,
	now time.Time,
) (sqlc.ListProductCatalogCacheRowsRow, bool) {
	for _, row := range rows {
		if productCatalogRowActive(
			row.IsVisible,
			row.IsClosed,
			row.AvailableFrom,
			row.AvailableUntil,
			row.PriceStartsAt,
			row.PriceEndsAt,
			now,
		) {
			return row, true
		}
	}
	return sqlc.ListProductCatalogCacheRowsRow{}, false
}

func mapProductPreviewCatalogRows(
	rows []sqlc.ListProductPreviewCatalogCacheRowsRow,
	now time.Time,
) (ProductPreview, error) {
	if len(rows) == 0 {
		return ProductPreview{}, sql.ErrNoRows
	}

	selected, ok := selectProductPreviewCatalogPrice(rows, now)
	if !ok {
		return ProductPreview{}, sql.ErrNoRows
	}

	product := ProductPreview{
		WorkspaceID:          selected.WorkspaceID,
		ID:                   selected.ProductID,
		LinkURL:              selected.LinkUrl,
		SizeLabel:            selected.SizeLabel,
		GroupCode:            selected.GroupCode,
		Title:                selected.ProductTitle,
		Description:          selected.ProductDescription,
		ImageURL:             selected.ImageUrl,
		PeriodSeconds:        selected.PeriodSeconds,
		TrialDurationSeconds: selected.TrialDurationSeconds,
		QuantityMode:         string(selected.QuantityMode),
		Limit: ProductLimit{
			Global: ProductLimitRule{
				Limit:         selected.GlobalLimit,
				Interval:      string(selected.GlobalInterval),
				IntervalCount: selected.GlobalIntervalCount,
			},
			User: ProductLimitRule{
				Limit:         selected.UserLimit,
				Interval:      string(selected.UserInterval),
				IntervalCount: selected.UserIntervalCount,
			},
		},
		Items: make([]ProductItem, 0, len(rows)),
	}

	for _, row := range rows {
		if row.PriceID != selected.PriceID || row.ItemID == "" {
			continue
		}
		product.Items = append(product.Items, ProductItem{
			ID:           row.ItemID,
			Quantity:     row.ItemQuantity,
			Scale:        uint16(row.ItemScale),
			RewardType:   string(row.RewardType),
			DurationUnit: paymentCacheDurationUnitPtr(row.DurationUnit),
		})
	}

	return product, nil
}

func paymentCacheDurationUnitPtr(value sqlc.NullPaymentProductCacheDurationUnit) *string {
	if !value.Valid {
		return nil
	}
	unit := string(value.PaymentProductCacheDurationUnit)
	return &unit
}

func selectProductPreviewCatalogPrice(
	rows []sqlc.ListProductPreviewCatalogCacheRowsRow,
	now time.Time,
) (sqlc.ListProductPreviewCatalogCacheRowsRow, bool) {
	for _, row := range rows {
		if productCatalogRowActive(
			row.IsVisible,
			row.IsClosed,
			row.AvailableFrom,
			row.AvailableUntil,
			row.PriceStartsAt,
			row.PriceEndsAt,
			now,
		) {
			return row, true
		}
	}
	return sqlc.ListProductPreviewCatalogCacheRowsRow{}, false
}

func productCatalogRowActive(
	isVisible bool,
	isClosed bool,
	availableFrom time.Time,
	availableUntil time.Time,
	priceStartsAt time.Time,
	priceEndsAt time.Time,
	now time.Time,
) bool {
	return isVisible &&
		!isClosed &&
		!now.Before(availableFrom) &&
		!now.After(availableUntil) &&
		!now.Before(priceStartsAt) &&
		!now.After(priceEndsAt)
}

func (r *PaymentRepository) CreateProductPurchaseKey(
	ctx context.Context,
	params ProductCreateKeyParams,
) (string, error) {
	workspaceID, err := requireWorkspaceID(params.WorkspaceID)
	if err != nil {
		return "", err
	}
	maxUses := params.MaxUses
	if maxUses <= 0 {
		maxUses = 1
	}

	key, err := newPurchaseKey()
	if err != nil {
		return "", err
	}

	_, err = r.q.CreatePurchaseKey(ctx, sqlc.CreatePurchaseKeyParams{
		KeyHash:        hashPurchaseKey(key),
		WorkspaceID:    workspaceID,
		AppID:          params.AppID,
		PlatformID:     params.PlatformID,
		PlatformUserID: params.PlatformUserID,
		InternalUserID: sqlwrap.NullFromPtr(params.InternalUserID, func(v int64) sql.NullInt64 {
			return sql.NullInt64{Int64: v, Valid: true}
		}),
		ProductID: params.ProductID,
		MaxUses:   maxUses,
		ExpiresAt: sqlwrap.NullTimeFromPtr(params.ExpiresAt),
	})
	if err != nil {
		return "", err
	}

	return key, nil
}

func hashPurchaseKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func newPurchaseKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func isPurchaseKeyUsable(key sqlc.PaymentPurchaseKey, now time.Time) bool {
	if key.Status != sqlc.PaymentPurchaseKeyStatusActive {
		return false
	}
	if key.ExpiresAt.Valid && !key.ExpiresAt.Time.After(now) {
		return false
	}
	return key.UsedCount+key.ReservedCount < key.MaxUses
}

func splitProviderCodes(value []byte) []string {
	if len(value) == 0 {
		return nil
	}
	return strings.Split(string(value), ",")
}

type productLimitQuery struct {
	workspaceID    string
	platformID     int64
	platformUserID string
	productID      string
	limit          int32
	interval       string
	intervalCount  int32
	amount         uint64
}

func (r *PaymentRepository) getProductLimitLock(ctx context.Context, query productLimitQuery) (sql.NullTime, error) {
	if query.limit <= 0 || query.interval == "UNLIMITED" {
		return sql.NullTime{}, nil
	}

	now, err := r.databaseNow(ctx)
	if err != nil {
		return sql.NullTime{}, err
	}

	start, end, ok := limitWindow(query.interval, query.intervalCount, now)
	if !ok {
		return sql.NullTime{}, nil
	}

	scope := sqlc.PaymentProductLimitCounterCounterScopeGlobal
	platformUserID := ""
	if query.platformUserID == "" {
		scope = sqlc.PaymentProductLimitCounterCounterScopeGlobal
	} else {
		scope = sqlc.PaymentProductLimitCounterCounterScopeUser
		platformUserID = query.platformUserID
	}

	total, err := r.q.GetProductLimitCounterCount(ctx, sqlc.GetProductLimitCounterCountParams{
		WorkspaceID:    query.workspaceID,
		PlatformID:     limitCounterPlatformID(scope, query.platformID),
		ProductID:      query.productID,
		CounterScope:   scope,
		PlatformUserID: platformUserID,
		WindowStart:    start,
		WindowEnd:      end,
	})
	if err == sql.ErrNoRows {
		return sql.NullTime{}, nil
	}
	if err != nil {
		return sql.NullTime{}, err
	}
	amount := normalizeLimitAmount(query.amount)
	limit := uint64(query.limit)
	if amount <= limit && uint64(total) <= limit-amount {
		return sql.NullTime{}, nil
	}

	return sql.NullTime{Time: end, Valid: true}, nil
}

func normalizeLimitAmount(amount uint64) uint64 {
	if amount == 0 {
		return 1
	}
	return amount
}

func limitCounterPlatformID(
	scope sqlc.PaymentProductLimitCounterCounterScope,
	platformID int64,
) int64 {
	if scope == sqlc.PaymentProductLimitCounterCounterScopeGlobal {
		return 0
	}

	return platformID
}

func (r *PaymentRepository) databaseNow(ctx context.Context) (time.Time, error) {
	return sqlwrap.Query(ctx, r.db, sqlwrap.Params{Timeout: r.timeout}, func(ctx context.Context) (time.Time, error) {
		return r.q.DatabaseNow(ctx)
	})
}

func limitWindow(interval string, intervalCount int32, now time.Time) (time.Time, time.Time, bool) {
	count := int(intervalCount)
	if count <= 0 {
		count = 1
	}

	anchor := time.Date(2024, 1, 1, 0, 0, 0, 0, now.Location())
	switch interval {
	case "SECOND":
		return fixedLimitWindow(anchor, now, time.Duration(count)*time.Second)
	case "MINUTE":
		return fixedLimitWindow(anchor, now, time.Duration(count)*time.Minute)
	case "HOUR":
		return fixedLimitWindow(anchor, now, time.Duration(count)*time.Hour)
	case "DAY":
		return fixedLimitWindow(anchor, now, time.Duration(count)*24*time.Hour)
	case "WEEK":
		return fixedLimitWindow(anchor, now, time.Duration(count)*7*24*time.Hour)
	case "MONTH":
		start := monthLimitWindow(anchor, now, count)
		return start, start.AddDate(0, count, 0), true
	case "ONCE":
		end := anchor.AddDate(100, 0, 0)
		return anchor, end, true
	default:
		return time.Time{}, time.Time{}, false
	}
}

func fixedLimitWindow(anchor time.Time, now time.Time, duration time.Duration) (time.Time, time.Time, bool) {
	if duration <= 0 {
		return time.Time{}, time.Time{}, false
	}
	if now.Before(anchor) {
		return anchor, anchor.Add(duration), true
	}
	elapsed := now.Sub(anchor)
	start := anchor.Add(time.Duration(int64(elapsed/duration)) * duration)
	return start, start.Add(duration), true
}

func monthLimitWindow(anchor time.Time, now time.Time, count int) time.Time {
	if now.Before(anchor) {
		return anchor
	}
	months := (now.Year()-anchor.Year())*12 + int(now.Month()) - int(anchor.Month())
	bucket := months / count * count
	return anchor.AddDate(0, bucket, 0)
}
