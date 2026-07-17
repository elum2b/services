package asset

import (
	"time"

	"github.com/elum2b/services/payment/repository"
)

const USDTAssetCode = repository.USDTAssetCode

type UpsertParams struct {
	Code            string
	Title           string
	AssetKind       string
	Scale           uint16
	Chain           *string
	Network         *string
	ContractAddress *string
	IsActive        bool
}

type Model = repository.AdminAssetModel
type ProviderModel = repository.AdminProviderAssetModel

type ProviderUpsertParams struct {
	ProviderCode    string
	AssetCode       string
	MinAmountMinor  *int64
	MaxAmountMinor  *int64
	MerchantAccount *string
	IsActive        bool
}

type USDTPriceModel struct {
	AssetCode          string    `json:"asset_code"`
	AssetTitle         string    `json:"asset_title"`
	Scale              uint16    `json:"scale"`
	ReferenceAssetCode string    `json:"reference_asset_code"`
	USDTPerAssetMinor  uint64    `json:"usdt_per_asset_minor"`
	Source             string    `json:"source"`
	ObservedAt         time.Time `json:"observed_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}
