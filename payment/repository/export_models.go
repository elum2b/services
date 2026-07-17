package repository

import (
	json "github.com/goccy/go-json"
	"time"
)

const (
	ExportFormat         = "payment.export.v1"
	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Now time.Time `json:"now,omitempty"`
}

type ExportPackage struct {
	Format     string               `json:"format"`
	Service    string               `json:"service"`
	CreatedAt  time.Time            `json:"created_at"`
	Groups     []ExportProductGroup `json:"groups,omitempty"`
	Products   []ExportProduct      `json:"products,omitempty"`
	TONWallets []ExportTONWallet    `json:"ton_wallets,omitempty"`
}

type ExportProductGroup struct {
	Code           string                `json:"key"`
	TitleKey       *string               `json:"-"`
	DescriptionKey *string               `json:"-"`
	Position       int32                 `json:"position"`
	IsActive       bool                  `json:"is_active"`
	Localization   map[string]ExportText `json:"localization,omitempty"`
	Products       []ExportProduct       `json:"products,omitempty"`
}

type ExportText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ExportProduct struct {
	ID                   string                `json:"key"`
	GroupCode            *string               `json:"group_code,omitempty"`
	TitleKey             string                `json:"-"`
	DescriptionKey       *string               `json:"-"`
	Target               json.RawMessage       `json:"target,omitempty"`
	ImageURL             *string               `json:"image_url,omitempty"`
	LinkURL              *string               `json:"link_url,omitempty"`
	SizeLabel            *string               `json:"size_label,omitempty"`
	PeriodSeconds        *int64                `json:"period_seconds,omitempty"`
	TrialDurationSeconds *int64                `json:"trial_duration_seconds,omitempty"`
	QuantityMode         string                `json:"quantity_mode"`
	Position             int32                 `json:"position"`
	GlobalLimit          int32                 `json:"global_limit"`
	GlobalInterval       string                `json:"global_interval"`
	GlobalIntervalCount  int32                 `json:"global_interval_count"`
	UserLimit            int32                 `json:"user_limit"`
	UserInterval         string                `json:"user_interval"`
	UserIntervalCount    int32                 `json:"user_interval_count"`
	AvailableFrom        time.Time             `json:"available_from"`
	AvailableUntil       time.Time             `json:"available_until"`
	IsVisible            bool                  `json:"is_visible"`
	IsClosed             bool                  `json:"is_closed"`
	Localization         map[string]ExportText `json:"localization,omitempty"`
	Items                []ExportProductItem   `json:"items,omitempty"`
	Prices               []ExportPrice         `json:"prices,omitempty"`
}

type ExportProductItem struct {
	ItemID       string  `json:"item_id"`
	RewardType   string  `json:"reward_type"`
	Quantity     int64   `json:"quantity"`
	Scale        uint16  `json:"scale"`
	DurationUnit *string `json:"duration_unit,omitempty"`
}

type ExportPrice struct {
	ID                           uint64    `json:"id,omitempty"`
	AssetCode                    string    `json:"asset_code"`
	ListAmountMinor              uint64    `json:"list_amount_minor"`
	DiscountAmountMinor          uint64    `json:"discount_amount_minor"`
	PricingMode                  string    `json:"pricing_mode"`
	ReferenceAssetCode           *string   `json:"reference_asset_code,omitempty"`
	ReferenceListAmountMinor     *uint64   `json:"reference_list_amount_minor,omitempty"`
	ReferenceDiscountAmountMinor *uint64   `json:"reference_discount_amount_minor,omitempty"`
	Coefficient                  *string   `json:"coefficient,omitempty"`
	IsPromotion                  bool      `json:"is_promotion"`
	StartsAt                     time.Time `json:"starts_at"`
	EndsAt                       time.Time `json:"ends_at"`
}

type ExportTONWallet struct {
	Network          string  `json:"network"`
	WalletAddress    string  `json:"wallet_address"`
	NetworkConfigURL *string `json:"network_config_url,omitempty"`
	IsEnabled        bool    `json:"is_enabled"`
}

type ImportRequest struct {
	Package          ExportPackage `json:"package"`
	ConflictStrategy string        `json:"conflict_strategy,omitempty"`
}

type ImportPreview struct {
	Format    string           `json:"format"`
	Service   string           `json:"service"`
	Counts    ImportCounts     `json:"counts"`
	Conflicts []ImportConflict `json:"conflicts,omitempty"`
}

type ImportCounts struct {
	Groups        uint64 `json:"groups"`
	Products      uint64 `json:"products"`
	ProductItems  uint64 `json:"product_items"`
	Prices        uint64 `json:"prices"`
	Localizations uint64 `json:"localizations"`
	TONWallets    uint64 `json:"ton_wallets"`
}

type ImportConflict struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type ImportResult struct {
	Imported ImportCounts `json:"imported"`
	Skipped  ImportCounts `json:"skipped"`
}
