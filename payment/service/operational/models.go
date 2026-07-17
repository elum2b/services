package operational

import (
	"time"

	"github.com/elum2b/services/payment/repository"
	"github.com/elum2b/services/payment/service/asset"
	"github.com/elum2b/services/payment/service/checkout"
)

const AssetRateSourceDexScreener = repository.AssetRateSourceDexScreener

type CreateEventParams = checkout.CreateEventParams
type CompleteAttemptParams = checkout.CompleteAttemptParams
type CompleteAttemptResult = checkout.CompleteAttemptResult
type AssetUpsertParams = asset.UpsertParams
type ProviderAssetUpsertParams = asset.ProviderUpsertParams

type ProviderUpsertParams struct {
	Code             string
	Title            string
	ProviderKind     string
	SupportsCreate   bool
	SupportsRedirect bool
	SupportsWebhook  bool
	SupportsRefund   bool
	IsActive         bool
}

type UpdateAssetRateParams struct {
	AssetCode              string
	ReferenceAssetCode     string
	ReferencePerAssetMinor uint64
	Source                 string
	ObservedAt             time.Time
}

type UpdateAssetRateResult struct {
	UpdatedPrices      uint64 `json:"updated_prices"`
	AffectedProducts   uint64 `json:"affected_products"`
	AffectedWorkspaces uint64 `json:"affected_workspaces"`
}

type ConfigureAssetRateAutoUpdateParams struct {
	AssetCode          string
	ReferenceAssetCode string
	Enabled            bool
	Source             string
	SourceChainID      string
	SourceTokenAddress *string
}
