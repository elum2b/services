package product

import (
	json "github.com/goccy/go-json"
	"time"

	services "github.com/elum2b/services"
)

type UpsertParams struct {
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

type ListParams struct {
	Identity  services.Identity
	GroupCode string
	AssetCode string
	Locale    string
}

type ProductModel struct {
	ID                   string        `json:"id"`
	LinkURL              *string       `json:"link_url,omitempty"`
	SizeLabel            *string       `json:"size_label,omitempty"`
	GroupCode            *string       `json:"group_code,omitempty"`
	Title                string        `json:"title"`
	Description          string        `json:"description"`
	ImageURL             *string       `json:"image_url,omitempty"`
	PeriodSeconds        *uint64       `json:"period_seconds,omitempty"`
	TrialDurationSeconds *uint64       `json:"trial_duration_seconds,omitempty"`
	QuantityMode         string        `json:"quantity_mode"`
	Price                Price         `json:"price"`
	Limit                Limit         `json:"limit"`
	Items                []ProductItem `json:"items,omitempty"`
}

type ProductPreview struct {
	ID                   string        `json:"id"`
	LinkURL              *string       `json:"link_url,omitempty"`
	SizeLabel            *string       `json:"size_label,omitempty"`
	GroupCode            *string       `json:"group_code,omitempty"`
	Title                string        `json:"title"`
	Description          string        `json:"description"`
	ImageURL             *string       `json:"image_url,omitempty"`
	PeriodSeconds        *uint64       `json:"period_seconds,omitempty"`
	TrialDurationSeconds *uint64       `json:"trial_duration_seconds,omitempty"`
	QuantityMode         string        `json:"quantity_mode"`
	Limit                Limit         `json:"limit"`
	Items                []ProductItem `json:"items,omitempty"`
}

type Price struct {
	ID                  uint64 `json:"id"`
	AssetCode           string `json:"asset_code"`
	ListAmountMinor     uint64 `json:"list_amount_minor"`
	DiscountAmountMinor uint64 `json:"discount_amount_minor"`
	PayableAmountMinor  uint64 `json:"payable_amount_minor"`
}

type PriceOption struct {
	PriceID             uint64   `json:"price_id"`
	ProductID           string   `json:"product_id"`
	AssetCode           string   `json:"asset_code"`
	AssetTitle          string   `json:"asset_title"`
	AssetKind           string   `json:"asset_kind"`
	Scale               uint16   `json:"scale"`
	Chain               *string  `json:"chain,omitempty"`
	Network             *string  `json:"network,omitempty"`
	ContractAddress     *string  `json:"contract_address,omitempty"`
	ListAmountMinor     uint64   `json:"list_amount_minor"`
	DiscountAmountMinor uint64   `json:"discount_amount_minor"`
	PayableAmountMinor  uint64   `json:"payable_amount_minor"`
	ProviderCodes       []string `json:"provider_codes"`
}

type Limit struct {
	Global LimitRule `json:"global"`
	User   LimitRule `json:"user"`
}

type LimitRule struct {
	Limit         int32      `json:"limit"`
	Interval      string     `json:"interval"`
	IntervalCount int32      `json:"interval_count"`
	LockUntil     *time.Time `json:"lock_until,omitempty"`
}

type ProductItem struct {
	ID           string  `json:"id"`
	RewardType   string  `json:"reward_type"`
	Quantity     int64   `json:"quantity"`
	Scale        uint16  `json:"scale"`
	DurationUnit *string `json:"duration_unit,omitempty"`
}
