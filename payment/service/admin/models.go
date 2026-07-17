package admin

import (
	"time"

	"github.com/elum2b/services/payment/repository"
	json "github.com/goccy/go-json"
)

type ExportRequest = repository.ExportRequest
type ExportPackage = repository.ExportPackage
type ExportProductGroup = repository.ExportProductGroup
type ExportText = repository.ExportText
type ExportProduct = repository.ExportProduct
type ExportProductItem = repository.ExportProductItem
type ExportPrice = repository.ExportPrice
type ExportTONWallet = repository.ExportTONWallet
type ImportRequest = repository.ImportRequest
type ImportPreview = repository.ImportPreview
type ImportCounts = repository.ImportCounts
type ImportConflict = repository.ImportConflict
type ImportResult = repository.ImportResult

type ProviderModel = repository.AdminProviderModel
type AssetModel = repository.AdminAssetModel
type ProviderAssetModel = repository.AdminProviderAssetModel
type AssetRateModel = repository.AdminAssetRateModel
type ProductGroupModel = repository.AdminProductGroupModel
type LocalizationModel = repository.AdminLocalizationModel
type ProductModel = repository.AdminProductModel
type ProductItemModel = repository.AdminProductItemModel
type PriceModel = repository.AdminPriceModel
type ProductLimitCounterModel = repository.AdminProductLimitCounterModel
type PurchaseKeyModel = repository.AdminPurchaseKeyModel
type OrderModel = repository.AdminOrderModel
type PaymentAttemptModel = repository.AdminPaymentAttemptModel
type PaymentEventModel = repository.AdminPaymentEventModel
type SubscriptionModel = repository.AdminSubscriptionModel
type FulfillmentModel = repository.AdminFulfillmentModel
type FulfillmentItemModel = repository.AdminFulfillmentItemModel
type RefundModel = repository.AdminRefundModel
type ProviderCursorModel = repository.AdminProviderCursorModel
type ProviderTransactionModel = repository.AdminProviderTransactionModel
type TONWalletModel = repository.AdminTONWalletModel

type PageParams struct {
	Limit  int32
	Offset int32
}

type StatsModel struct {
	ProductsTotal    uint64            `json:"products_total"`
	ActiveProducts   uint64            `json:"active_products"`
	VisibleProducts  uint64            `json:"visible_products"`
	OrdersTotal      uint64            `json:"orders_total"`
	PendingOrders    uint64            `json:"pending_orders"`
	FulfilledOrders  uint64            `json:"fulfilled_orders"`
	RefundedOrders   uint64            `json:"refunded_orders"`
	FailedOrders     uint64            `json:"failed_orders"`
	CanceledOrders   uint64            `json:"canceled_orders"`
	PurchaseCount    uint64            `json:"purchase_count"`
	PurchaseQuantity uint64            `json:"purchase_quantity"`
	UniqueBuyers     uint64            `json:"unique_buyers"`
	Assets           []AssetStatsModel `json:"assets"`
}

type ProductStatsModel struct {
	ProductID        string            `json:"product_id"`
	OrdersTotal      uint64            `json:"orders_total"`
	PendingOrders    uint64            `json:"pending_orders"`
	FulfilledOrders  uint64            `json:"fulfilled_orders"`
	RefundedOrders   uint64            `json:"refunded_orders"`
	FailedOrders     uint64            `json:"failed_orders"`
	CanceledOrders   uint64            `json:"canceled_orders"`
	PurchaseCount    uint64            `json:"purchase_count"`
	PurchaseQuantity uint64            `json:"purchase_quantity"`
	UniqueBuyers     uint64            `json:"unique_buyers"`
	Assets           []AssetStatsModel `json:"assets"`
}

type AssetStatsModel struct {
	AssetCode         string `json:"asset_code"`
	PurchaseCount     uint64 `json:"purchase_count"`
	PurchaseQuantity  uint64 `json:"purchase_quantity"`
	GrossAmountMinor  uint64 `json:"gross_amount_minor"`
	RefundCount       uint64 `json:"refund_count"`
	RefundAmountMinor uint64 `json:"refund_amount_minor"`
}

type DailyStatsModel struct {
	Date              time.Time `json:"date"`
	ProductID         string    `json:"product_id,omitempty"`
	AssetCode         string    `json:"asset_code"`
	PurchaseCount     uint64    `json:"purchase_count"`
	PurchaseQuantity  uint64    `json:"purchase_quantity"`
	UniqueBuyers      uint64    `json:"unique_buyers"`
	GrossAmountMinor  uint64    `json:"gross_amount_minor"`
	RefundCount       uint64    `json:"refund_count"`
	RefundAmountMinor uint64    `json:"refund_amount_minor"`
}

type DailyOverviewModel struct {
	Date                 time.Time `json:"date"`
	ProductsTotal        uint64    `json:"products_total"`
	ActiveProducts       uint64    `json:"active_products"`
	VisibleProducts      uint64    `json:"visible_products"`
	OrdersCreated        uint64    `json:"orders_created"`
	DraftOrders          uint64    `json:"draft_orders"`
	PendingPaymentOrders uint64    `json:"pending_payment_orders"`
	PaidOrders           uint64    `json:"paid_orders"`
	FulfilledOrders      uint64    `json:"fulfilled_orders"`
	CanceledOrders       uint64    `json:"canceled_orders"`
	ExpiredOrders        uint64    `json:"expired_orders"`
	RefundedOrders       uint64    `json:"refunded_orders"`
	ChargebackedOrders   uint64    `json:"chargebacked_orders"`
	FailedOrders         uint64    `json:"failed_orders"`
	PurchaseCount        uint64    `json:"purchase_count"`
	PurchaseQuantity     uint64    `json:"purchase_quantity"`
	UniqueBuyers         uint64    `json:"unique_buyers"`
	RefundCount          uint64    `json:"refund_count"`
}

type ProviderAssetListParams struct {
	ProviderCode string
	AssetCode    string
	Page         PageParams
}

type TONWalletUpsertParams struct {
	WorkspaceID      string
	Network          string
	WalletAddress    string
	NetworkConfigURL *string
	IsEnabled        bool
}

type ProductGroupUpsertParams struct {
	WorkspaceID    string
	Code           string
	TitleKey       *string
	DescriptionKey *string
	Position       int32
	IsActive       bool
}

type LocalizationUpsertParams struct {
	WorkspaceID     string
	Locale          string
	LocalizationKey string
	Value           string
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
	WorkspaceID         string
	ProductID           string
	AssetCode           string
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	IsPromotion         bool
	StartsAt            *time.Time
	EndsAt              *time.Time
}

type ProductPriceUpdateParams struct {
	ID                  uint64
	WorkspaceID         string
	AssetCode           string
	ListAmountMinor     uint64
	DiscountAmountMinor uint64
	IsPromotion         bool
	StartsAt            *time.Time
	EndsAt              *time.Time
}

type ProductGroupListParams struct {
	WorkspaceID string
	Page        PageParams
}

type LocalizationListParams struct {
	WorkspaceID string
	Locale      string
	Page        PageParams
}

type ProductListParams struct {
	WorkspaceID  string
	GroupCode    string
	QuantityMode string
	Page         PageParams
}

type ProductItemListParams struct {
	WorkspaceID string
	ProductID   string
	ItemID      string
	Page        PageParams
}

type PriceListParams struct {
	WorkspaceID string
	ProductID   string
	AssetCode   string
	Page        PageParams
}

type AssetRateListParams struct {
	AssetCode          string
	ReferenceAssetCode string
	Page               PageParams
}

type ProductLimitCounterListParams struct {
	WorkspaceID    string
	ProductID      string
	PlatformID     int64
	PlatformUserID string
	Page           PageParams
}

type ProductLimitCounterDeleteParams struct {
	WorkspaceID    string
	PlatformID     int64
	ProductID      string
	CounterScope   string
	PlatformUserID string
	WindowStart    time.Time
	WindowEnd      time.Time
}

type PurchaseKeyListParams struct {
	WorkspaceID    string
	ProductID      string
	Status         string
	PlatformID     int64
	PlatformUserID string
	Page           PageParams
}

type OrderListParams struct {
	WorkspaceID    string
	Status         string
	ProductID      string
	PlatformID     int64
	PlatformUserID string
	Page           PageParams
}

type OrderRefParams struct {
	WorkspaceID string
	ID          uint64
}

type OrderPublicRefParams struct {
	WorkspaceID string
	PublicID    string
}

type AttemptListParams struct {
	WorkspaceID  string
	OrderID      uint64
	ProviderCode string
	Status       string
	Page         PageParams
}

type AttemptRefParams struct {
	WorkspaceID string
	ID          uint64
}

type AttemptStatusParams struct {
	WorkspaceID string
	ID          uint64
	Status      string
}

type EventListParams struct {
	WorkspaceID      string
	ProviderCode     string
	ProcessingStatus string
	Page             PageParams
}

type EventRefParams struct {
	WorkspaceID string
	ID          uint64
}

type EventStatusParams struct {
	WorkspaceID string
	ID          uint64
	Status      string
	Message     string
}

type SubscriptionListParams struct {
	WorkspaceID    string
	ProviderCode   string
	ProductID      string
	Status         string
	PlatformID     int64
	PlatformUserID string
	Page           PageParams
}

type SubscriptionProviderRefParams struct {
	WorkspaceID            string
	ProviderCode           string
	ProviderSubscriptionID string
}

type SubscriptionUpsertParams struct {
	WorkspaceID            string
	ProviderCode           string
	ProviderSubscriptionID string
	AppID                  int64
	PlatformID             int64
	PlatformUserID         string
	InternalUserID         *int64
	ProductID              string
	OrderID                *int64
	AttemptID              *int64
	Status                 string
	CancelReason           *string
	StartedAt              time.Time
	EndedAt                *time.Time
}

type SubscriptionStatusUpdateParams struct {
	WorkspaceID            string
	ProviderCode           string
	ProviderSubscriptionID string
	Status                 string
	CancelReason           *string
	EndedAt                *time.Time
}

type FulfillmentListParams struct {
	WorkspaceID string
	Status      string
	OrderID     uint64
	Page        PageParams
}

type FulfillmentRefParams struct {
	WorkspaceID string
	ID          uint64
}

type FulfillmentStatusParams struct {
	WorkspaceID string
	ID          uint64
	Status      string
	Message     string
}

type FulfillmentItemListParams struct {
	WorkspaceID   string
	FulfillmentID uint64
	Page          PageParams
}

type RefundCreateParams struct {
	WorkspaceID      string
	OrderID          uint64
	AttemptID        uint64
	ProviderCode     string
	ProviderRefundID *string
	AmountMinor      uint64
	AssetCode        string
	Status           string
	Reason           *string
}

type RefundRefParams struct {
	WorkspaceID string
	ID          uint64
}

type RefundStatusParams struct {
	WorkspaceID string
	ID          uint64
	Status      string
	Reason      string
}

type RefundListParams struct {
	WorkspaceID  string
	OrderID      uint64
	ProviderCode string
	Status       string
	Page         PageParams
}

type ProviderCursorListParams struct {
	WorkspaceID  string
	ProviderCode string
	Network      string
	Page         PageParams
}

type ProviderCursorUpsertParams struct {
	WorkspaceID    string
	ProviderCode   string
	Network        string
	SourceKey      string
	CursorValue    string
	CursorSequence int64
}

type ProviderTransactionListParams struct {
	WorkspaceID  string
	ProviderCode string
	Network      string
	SourceKey    string
	Status       string
	Page         PageParams
}

type CallbackEventListParams struct {
	WorkspaceID   string
	SourceService string
	EventType     string
	Status        string
	Page          PageParams
}
