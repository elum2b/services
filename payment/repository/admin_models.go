package repository

import (
	"time"

	paymentsqlc "github.com/elum2b/services/payment/sqlc"
	json "github.com/goccy/go-json"
)

type NullableString struct {
	String string `json:"value"`
	Valid  bool   `json:"valid"`
}

type NullableInt64 struct {
	Int64 int64 `json:"value"`
	Valid bool  `json:"valid"`
}

type NullableTime struct {
	Time  time.Time `json:"value"`
	Valid bool      `json:"valid"`
}

type NullableBool struct {
	Bool  bool `json:"value"`
	Valid bool `json:"valid"`
}

func (value NullableString) MarshalJSON() ([]byte, error) {
	if !value.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(value.String)
}

func (value NullableInt64) MarshalJSON() ([]byte, error) {
	if !value.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(value.Int64)
}

func (value NullableTime) MarshalJSON() ([]byte, error) {
	if !value.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(value.Time)
}

func (value NullableBool) MarshalJSON() ([]byte, error) {
	if !value.Valid {
		return []byte("null"), nil
	}
	return json.Marshal(value.Bool)
}

type AdminProviderModel struct {
	Code             string    `json:"code"`
	Title            string    `json:"title"`
	ProviderKind     string    `json:"provider_kind"`
	SupportsCreate   bool      `json:"supports_create"`
	SupportsRedirect bool      `json:"supports_redirect"`
	SupportsWebhook  bool      `json:"supports_webhook"`
	SupportsRefund   bool      `json:"supports_refund"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type AdminAssetModel struct {
	Code            string         `json:"code"`
	Title           string         `json:"title"`
	AssetKind       string         `json:"asset_kind"`
	Scale           int16          `json:"scale"`
	Chain           NullableString `json:"chain"`
	Network         NullableString `json:"network"`
	ContractAddress NullableString `json:"contract_address"`
	IsActive        bool           `json:"is_active"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AdminProviderAssetModel struct {
	ProviderCode    string         `json:"provider_code"`
	AssetCode       string         `json:"asset_code"`
	MinAmountMinor  NullableInt64  `json:"min_amount_minor"`
	MaxAmountMinor  NullableInt64  `json:"max_amount_minor"`
	MerchantAccount NullableString `json:"merchant_account"`
	IsActive        bool           `json:"is_active"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

type AdminAssetRateModel struct {
	AssetCode              string         `json:"asset_code"`
	ReferenceAssetCode     string         `json:"reference_asset_code"`
	ReferencePerAssetMinor int64          `json:"reference_per_asset_minor"`
	Source                 string         `json:"source"`
	ObservedAt             time.Time      `json:"observed_at"`
	AutoUpdateEnabled      bool           `json:"auto_update_enabled"`
	AutoUpdateSource       NullableString `json:"auto_update_source"`
	SourceChainID          NullableString `json:"source_chain_id"`
	SourceTokenAddress     NullableString `json:"source_token_address"`
	LastAttemptAt          NullableTime   `json:"last_attempt_at"`
	LastError              NullableString `json:"last_error"`
	LeaseOwner             NullableString `json:"lease_owner"`
	LeaseUntil             NullableTime   `json:"lease_until"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type AdminProductGroupModel struct {
	WorkspaceID    string         `json:"workspace_id"`
	Code           string         `json:"code"`
	TitleKey       NullableString `json:"title_key"`
	DescriptionKey NullableString `json:"description_key"`
	Position       int32          `json:"position"`
	IsActive       bool           `json:"is_active"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type AdminLocalizationModel struct {
	ID              int64     `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	Locale          string    `json:"locale"`
	LocalizationKey string    `json:"localization_key"`
	Value           string    `json:"value"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AdminProductModel struct {
	WorkspaceID          string          `json:"workspace_id"`
	ID                   string          `json:"id"`
	GroupCode            NullableString  `json:"group_code"`
	TitleKey             string          `json:"title_key"`
	DescriptionKey       NullableString  `json:"description_key"`
	Target               json.RawMessage `json:"target"`
	ImageUrl             NullableString  `json:"image_url"`
	LinkUrl              NullableString  `json:"link_url"`
	SizeLabel            NullableString  `json:"size_label"`
	PeriodSeconds        NullableInt64   `json:"period_seconds"`
	TrialDurationSeconds NullableInt64   `json:"trial_duration_seconds"`
	QuantityMode         string          `json:"quantity_mode"`
	Position             int32           `json:"position"`
	GlobalLimit          int32           `json:"global_limit"`
	GlobalInterval       string          `json:"global_interval"`
	GlobalIntervalCount  int32           `json:"global_interval_count"`
	UserLimit            int32           `json:"user_limit"`
	UserInterval         string          `json:"user_interval"`
	UserIntervalCount    int32           `json:"user_interval_count"`
	AvailableFrom        time.Time       `json:"available_from"`
	AvailableUntil       time.Time       `json:"available_until"`
	IsVisible            bool            `json:"is_visible"`
	IsClosed             bool            `json:"is_closed"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

type AdminProductItemModel struct {
	ID           int64          `json:"id"`
	WorkspaceID  string         `json:"workspace_id"`
	ProductID    string         `json:"product_id"`
	ItemID       string         `json:"item_id"`
	RewardType   string         `json:"reward_type"`
	Quantity     int64          `json:"quantity"`
	Scale        int16          `json:"scale"`
	DurationUnit NullableString `json:"duration_unit"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type AdminPriceModel struct {
	ID                           int64          `json:"id"`
	WorkspaceID                  string         `json:"workspace_id"`
	ProductID                    string         `json:"product_id"`
	AssetCode                    string         `json:"asset_code"`
	ListAmountMinor              int64          `json:"list_amount_minor"`
	DiscountAmountMinor          int64          `json:"discount_amount_minor"`
	PricingMode                  string         `json:"pricing_mode"`
	ReferenceAssetCode           NullableString `json:"reference_asset_code"`
	ReferenceListAmountMinor     NullableInt64  `json:"reference_list_amount_minor"`
	ReferenceDiscountAmountMinor NullableInt64  `json:"reference_discount_amount_minor"`
	Coefficient                  NullableString `json:"coefficient"`
	IsPromotion                  bool           `json:"is_promotion"`
	StartsAt                     time.Time      `json:"starts_at"`
	EndsAt                       time.Time      `json:"ends_at"`
	CreatedAt                    time.Time      `json:"created_at"`
	UpdatedAt                    time.Time      `json:"updated_at"`
}

type AdminProductLimitCounterModel struct {
	WorkspaceID    string    `json:"workspace_id"`
	PlatformID     int64     `json:"platform_id"`
	ProductID      string    `json:"product_id"`
	CounterScope   string    `json:"counter_scope"`
	PlatformUserID string    `json:"platform_user_id"`
	WindowStart    time.Time `json:"window_start"`
	WindowEnd      time.Time `json:"window_end"`
	PaidCount      int64     `json:"paid_count"`
	ReservedCount  int64     `json:"reserved_count"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AdminPurchaseKeyModel struct {
	ID             int64         `json:"id"`
	WorkspaceID    string        `json:"workspace_id"`
	KeyHash        string        `json:"key_hash"`
	AppID          int64         `json:"app_id"`
	PlatformID     int64         `json:"platform_id"`
	PlatformUserID string        `json:"platform_user_id"`
	InternalUserID NullableInt64 `json:"internal_user_id"`
	ProductID      string        `json:"product_id"`
	Status         string        `json:"status"`
	MaxUses        int32         `json:"max_uses"`
	UsedCount      int32         `json:"used_count"`
	ReservedCount  int32         `json:"reserved_count"`
	ExpiresAt      NullableTime  `json:"expires_at"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type AdminOrderModel struct {
	ID                          int64          `json:"id"`
	PublicID                    string         `json:"public_id"`
	WorkspaceID                 string         `json:"workspace_id"`
	AppID                       int64          `json:"app_id"`
	PlatformID                  int64          `json:"platform_id"`
	PlatformUserID              string         `json:"platform_user_id"`
	InternalUserID              NullableInt64  `json:"internal_user_id"`
	PayerPlatformID             NullableInt64  `json:"payer_platform_id"`
	PayerPlatformUserID         NullableString `json:"payer_platform_user_id"`
	PayerInternalUserID         NullableInt64  `json:"payer_internal_user_id"`
	PurchaseKeyID               NullableInt64  `json:"purchase_key_id"`
	ProductID                   string         `json:"product_id"`
	Quantity                    int64          `json:"quantity"`
	PriceID                     int64          `json:"price_id"`
	AssetCode                   string         `json:"asset_code"`
	Locale                      string         `json:"locale"`
	ListAmountMinor             int64          `json:"list_amount_minor"`
	DiscountAmountMinor         int64          `json:"discount_amount_minor"`
	PayableAmountMinor          int64          `json:"payable_amount_minor"`
	Status                      string         `json:"status"`
	ReservedUntil               NullableTime   `json:"reserved_until"`
	GlobalLimitSnapshot         int32          `json:"global_limit_snapshot"`
	GlobalIntervalSnapshot      string         `json:"global_interval_snapshot"`
	GlobalIntervalCountSnapshot int32          `json:"global_interval_count_snapshot"`
	GlobalWindowStartSnapshot   NullableTime   `json:"global_window_start_snapshot"`
	GlobalWindowEndSnapshot     NullableTime   `json:"global_window_end_snapshot"`
	UserLimitSnapshot           int32          `json:"user_limit_snapshot"`
	UserIntervalSnapshot        string         `json:"user_interval_snapshot"`
	UserIntervalCountSnapshot   int32          `json:"user_interval_count_snapshot"`
	UserWindowStartSnapshot     NullableTime   `json:"user_window_start_snapshot"`
	UserWindowEndSnapshot       NullableTime   `json:"user_window_end_snapshot"`
	PaidAt                      NullableTime   `json:"paid_at"`
	FulfilledAt                 NullableTime   `json:"fulfilled_at"`
	CanceledAt                  NullableTime   `json:"canceled_at"`
	ExpiresAt                   NullableTime   `json:"expires_at"`
	CreatedAt                   time.Time      `json:"created_at"`
	UpdatedAt                   time.Time      `json:"updated_at"`
}

type AdminPaymentAttemptModel struct {
	ID                     int64          `json:"id"`
	OrderID                int64          `json:"order_id"`
	ProviderCode           string         `json:"provider_code"`
	AssetCode              string         `json:"asset_code"`
	AmountMinor            int64          `json:"amount_minor"`
	Status                 string         `json:"status"`
	ProviderPaymentID      NullableString `json:"provider_payment_id"`
	ProviderInvoiceID      NullableString `json:"provider_invoice_id"`
	ProviderChargeID       NullableString `json:"provider_charge_id"`
	ProviderSubscriptionID NullableString `json:"provider_subscription_id"`
	IdempotencyKey         NullableString `json:"idempotency_key"`
	ConfirmationUrl        NullableString `json:"confirmation_url"`
	ReturnUrl              NullableString `json:"return_url"`
	ExpiresAt              NullableTime   `json:"expires_at"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type AdminPaymentEventModel struct {
	ID                int64          `json:"id"`
	ProviderCode      string         `json:"provider_code"`
	AttemptID         NullableInt64  `json:"attempt_id"`
	OrderID           NullableInt64  `json:"order_id"`
	ProviderEventID   NullableString `json:"provider_event_id"`
	ProviderPaymentID NullableString `json:"provider_payment_id"`
	EventType         string         `json:"event_type"`
	EventStatus       NullableString `json:"event_status"`
	PayloadHash       string         `json:"payload_hash"`
	SignatureValid    NullableBool   `json:"signature_valid"`
	ProcessingStatus  string         `json:"processing_status"`
	ProcessingError   NullableString `json:"processing_error"`
	ReceivedAt        time.Time      `json:"received_at"`
	ProcessedAt       NullableTime   `json:"processed_at"`
}

type AdminSubscriptionModel struct {
	ID                     int64          `json:"id"`
	WorkspaceID            string         `json:"workspace_id"`
	ProviderCode           string         `json:"provider_code"`
	ProviderSubscriptionID string         `json:"provider_subscription_id"`
	AppID                  int64          `json:"app_id"`
	PlatformID             int64          `json:"platform_id"`
	PlatformUserID         string         `json:"platform_user_id"`
	InternalUserID         NullableInt64  `json:"internal_user_id"`
	ProductID              string         `json:"product_id"`
	OrderID                NullableInt64  `json:"order_id"`
	AttemptID              NullableInt64  `json:"attempt_id"`
	Status                 string         `json:"status"`
	CancelReason           NullableString `json:"cancel_reason"`
	StartedAt              time.Time      `json:"started_at"`
	EndedAt                NullableTime   `json:"ended_at"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
}

type AdminFulfillmentModel struct {
	ID             int64          `json:"id"`
	OrderID        int64          `json:"order_id"`
	AttemptID      int64          `json:"attempt_id"`
	InternalUserID NullableInt64  `json:"internal_user_id"`
	Status         string         `json:"status"`
	Error          NullableString `json:"error"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	FulfilledAt    NullableTime   `json:"fulfilled_at"`
	RevokedAt      NullableTime   `json:"revoked_at"`
}

type AdminFulfillmentItemModel struct {
	ID            int64          `json:"id"`
	FulfillmentID int64          `json:"fulfillment_id"`
	WorkspaceID   string         `json:"workspace_id"`
	ItemID        string         `json:"item_id"`
	RewardType    string         `json:"reward_type"`
	Quantity      int64          `json:"quantity"`
	Scale         int16          `json:"scale"`
	DurationUnit  NullableString `json:"duration_unit"`
	CreatedAt     time.Time      `json:"created_at"`
}

type AdminRefundModel struct {
	ID               int64          `json:"id"`
	OrderID          int64          `json:"order_id"`
	AttemptID        int64          `json:"attempt_id"`
	ProviderCode     string         `json:"provider_code"`
	IdempotencyKey   NullableString `json:"idempotency_key"`
	ProviderRefundID NullableString `json:"provider_refund_id"`
	AmountMinor      int64          `json:"amount_minor"`
	AssetCode        string         `json:"asset_code"`
	Status           string         `json:"status"`
	Reason           NullableString `json:"reason"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

type AdminProviderCursorModel struct {
	WorkspaceID    string    `json:"workspace_id"`
	ProviderCode   string    `json:"provider_code"`
	Network        string    `json:"network"`
	SourceKey      string    `json:"source_key"`
	CursorValue    string    `json:"cursor_value"`
	CursorSequence int64     `json:"cursor_sequence"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type AdminProviderTransactionModel struct {
	ID                    int64          `json:"id"`
	WorkspaceID           string         `json:"workspace_id"`
	ProviderCode          string         `json:"provider_code"`
	Network               string         `json:"network"`
	SourceKey             string         `json:"source_key"`
	AssetCode             string         `json:"asset_code"`
	ExternalTransactionID string         `json:"external_transaction_id"`
	SequenceNumber        int64          `json:"sequence_number"`
	SourceAddress         string         `json:"source_address"`
	DestinationAddress    string         `json:"destination_address"`
	AmountMinor           int64          `json:"amount_minor"`
	PaymentReference      string         `json:"payment_reference"`
	SenderReference       NullableString `json:"sender_reference"`
	OrderID               NullableInt64  `json:"order_id"`
	AttemptID             NullableInt64  `json:"attempt_id"`
	Status                string         `json:"status"`
	Error                 NullableString `json:"error"`
	OccurredAt            time.Time      `json:"occurred_at"`
	CreatedAt             time.Time      `json:"created_at"`
}

type AdminTONWalletModel struct {
	WorkspaceID      string         `json:"workspace_id"`
	Network          string         `json:"network"`
	WalletAddress    string         `json:"wallet_address"`
	NetworkConfigUrl NullableString `json:"network_config_url"`
	IsEnabled        bool           `json:"is_enabled"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func mapAdminProvider(row paymentsqlc.PaymentProvider) AdminProviderModel {
	return AdminProviderModel{
		Code:             row.Code,
		Title:            row.Title,
		ProviderKind:     string(row.ProviderKind),
		SupportsCreate:   row.SupportsCreate,
		SupportsRedirect: row.SupportsRedirect,
		SupportsWebhook:  row.SupportsWebhook,
		SupportsRefund:   row.SupportsRefund,
		IsActive:         row.IsActive,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func mapAdminAsset(row paymentsqlc.PaymentAsset) AdminAssetModel {
	return AdminAssetModel{
		Code:            row.Code,
		Title:           row.Title,
		AssetKind:       string(row.AssetKind),
		Scale:           row.Scale,
		Chain:           NullableString(row.Chain),
		Network:         NullableString(row.Network),
		ContractAddress: NullableString(row.ContractAddress),
		IsActive:        row.IsActive,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func mapAdminProviderAsset(row paymentsqlc.PaymentProviderAsset) AdminProviderAssetModel {
	return AdminProviderAssetModel{
		ProviderCode:    row.ProviderCode,
		AssetCode:       row.AssetCode,
		MinAmountMinor:  NullableInt64(row.MinAmountMinor),
		MaxAmountMinor:  NullableInt64(row.MaxAmountMinor),
		MerchantAccount: NullableString(row.MerchantAccount),
		IsActive:        row.IsActive,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func mapAdminAssetRate(row paymentsqlc.PaymentAssetRate) AdminAssetRateModel {
	return AdminAssetRateModel{
		AssetCode:              row.AssetCode,
		ReferenceAssetCode:     row.ReferenceAssetCode,
		ReferencePerAssetMinor: row.ReferencePerAssetMinor,
		Source:                 row.Source,
		ObservedAt:             row.ObservedAt,
		AutoUpdateEnabled:      row.AutoUpdateEnabled,
		AutoUpdateSource:       NullableString(row.AutoUpdateSource),
		SourceChainID:          NullableString(row.SourceChainID),
		SourceTokenAddress:     NullableString(row.SourceTokenAddress),
		LastAttemptAt:          NullableTime(row.LastAttemptAt),
		LastError:              NullableString(row.LastError),
		LeaseOwner:             NullableString(row.LeaseOwner),
		LeaseUntil:             NullableTime(row.LeaseUntil),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

func mapAdminProductGroup(row paymentsqlc.PaymentProductGroup) AdminProductGroupModel {
	return AdminProductGroupModel{
		WorkspaceID:    row.WorkspaceID,
		Code:           row.Code,
		TitleKey:       NullableString(row.TitleKey),
		DescriptionKey: NullableString(row.DescriptionKey),
		Position:       row.Position,
		IsActive:       row.IsActive,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func mapAdminLocalization(row paymentsqlc.PaymentLocalization) AdminLocalizationModel {
	return AdminLocalizationModel(row)
}

func mapAdminProduct(row paymentsqlc.PaymentProduct) AdminProductModel {
	var target json.RawMessage
	if row.Target.Valid {
		target = append(target, row.Target.RawMessage...)
	}
	return AdminProductModel{
		WorkspaceID:          row.WorkspaceID,
		ID:                   row.ID,
		GroupCode:            NullableString(row.GroupCode),
		TitleKey:             row.TitleKey,
		DescriptionKey:       NullableString(row.DescriptionKey),
		Target:               target,
		ImageUrl:             NullableString(row.ImageUrl),
		LinkUrl:              NullableString(row.LinkUrl),
		SizeLabel:            NullableString(row.SizeLabel),
		PeriodSeconds:        NullableInt64(row.PeriodSeconds),
		TrialDurationSeconds: NullableInt64(row.TrialDurationSeconds),
		QuantityMode:         string(row.QuantityMode),
		Position:             row.Position,
		GlobalLimit:          row.GlobalLimit,
		GlobalInterval:       string(row.GlobalInterval),
		GlobalIntervalCount:  row.GlobalIntervalCount,
		UserLimit:            row.UserLimit,
		UserInterval:         string(row.UserInterval),
		UserIntervalCount:    row.UserIntervalCount,
		AvailableFrom:        row.AvailableFrom,
		AvailableUntil:       row.AvailableUntil,
		IsVisible:            row.IsVisible,
		IsClosed:             row.IsClosed,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func mapAdminProductItem(row paymentsqlc.PaymentProductItem) AdminProductItemModel {
	unit := NullableString{}
	if row.DurationUnit.Valid {
		unit = NullableString{String: string(row.DurationUnit.PaymentProductItemDurationUnit), Valid: true}
	}
	return AdminProductItemModel{
		ID:           row.ID,
		WorkspaceID:  row.WorkspaceID,
		ProductID:    row.ProductID,
		ItemID:       row.ItemID,
		RewardType:   string(row.RewardType),
		Quantity:     row.Quantity,
		Scale:        row.Scale,
		DurationUnit: unit,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func mapAdminPrice(row paymentsqlc.PaymentPrice) AdminPriceModel {
	return AdminPriceModel{
		ID:                           row.ID,
		WorkspaceID:                  row.WorkspaceID,
		ProductID:                    row.ProductID,
		AssetCode:                    row.AssetCode,
		ListAmountMinor:              row.ListAmountMinor,
		DiscountAmountMinor:          row.DiscountAmountMinor,
		PricingMode:                  string(row.PricingMode),
		ReferenceAssetCode:           NullableString(row.ReferenceAssetCode),
		ReferenceListAmountMinor:     NullableInt64(row.ReferenceListAmountMinor),
		ReferenceDiscountAmountMinor: NullableInt64(row.ReferenceDiscountAmountMinor),
		Coefficient:                  NullableString(row.Coefficient),
		IsPromotion:                  row.IsPromotion,
		StartsAt:                     row.StartsAt,
		EndsAt:                       row.EndsAt,
		CreatedAt:                    row.CreatedAt,
		UpdatedAt:                    row.UpdatedAt,
	}
}

func mapAdminProductLimitCounter(row paymentsqlc.PaymentProductLimitCounter) AdminProductLimitCounterModel {
	return AdminProductLimitCounterModel{
		WorkspaceID:    row.WorkspaceID,
		PlatformID:     row.PlatformID,
		ProductID:      row.ProductID,
		CounterScope:   string(row.CounterScope),
		PlatformUserID: row.PlatformUserID,
		WindowStart:    row.WindowStart,
		WindowEnd:      row.WindowEnd,
		PaidCount:      row.PaidCount,
		ReservedCount:  row.ReservedCount,
		UpdatedAt:      row.UpdatedAt,
	}
}

func mapAdminPurchaseKey(row paymentsqlc.PaymentPurchaseKey) AdminPurchaseKeyModel {
	return AdminPurchaseKeyModel{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		KeyHash:        row.KeyHash,
		AppID:          row.AppID,
		PlatformID:     row.PlatformID,
		PlatformUserID: row.PlatformUserID,
		InternalUserID: NullableInt64(row.InternalUserID),
		ProductID:      row.ProductID,
		Status:         string(row.Status),
		MaxUses:        row.MaxUses,
		UsedCount:      row.UsedCount,
		ReservedCount:  row.ReservedCount,
		ExpiresAt:      NullableTime(row.ExpiresAt),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func mapAdminOrder(row paymentsqlc.PaymentOrder) AdminOrderModel {
	return AdminOrderModel{
		ID:                          row.ID,
		PublicID:                    row.PublicID,
		WorkspaceID:                 row.WorkspaceID,
		AppID:                       row.AppID,
		PlatformID:                  row.PlatformID,
		PlatformUserID:              row.PlatformUserID,
		InternalUserID:              NullableInt64(row.InternalUserID),
		PayerPlatformID:             NullableInt64(row.PayerPlatformID),
		PayerPlatformUserID:         NullableString(row.PayerPlatformUserID),
		PayerInternalUserID:         NullableInt64(row.PayerInternalUserID),
		PurchaseKeyID:               NullableInt64(row.PurchaseKeyID),
		ProductID:                   row.ProductID,
		Quantity:                    row.Quantity,
		PriceID:                     row.PriceID,
		AssetCode:                   row.AssetCode,
		Locale:                      row.Locale,
		ListAmountMinor:             row.ListAmountMinor,
		DiscountAmountMinor:         row.DiscountAmountMinor,
		PayableAmountMinor:          row.PayableAmountMinor,
		Status:                      string(row.Status),
		ReservedUntil:               NullableTime(row.ReservedUntil),
		GlobalLimitSnapshot:         row.GlobalLimitSnapshot,
		GlobalIntervalSnapshot:      row.GlobalIntervalSnapshot,
		GlobalIntervalCountSnapshot: row.GlobalIntervalCountSnapshot,
		GlobalWindowStartSnapshot:   NullableTime(row.GlobalWindowStartSnapshot),
		GlobalWindowEndSnapshot:     NullableTime(row.GlobalWindowEndSnapshot),
		UserLimitSnapshot:           row.UserLimitSnapshot,
		UserIntervalSnapshot:        row.UserIntervalSnapshot,
		UserIntervalCountSnapshot:   row.UserIntervalCountSnapshot,
		UserWindowStartSnapshot:     NullableTime(row.UserWindowStartSnapshot),
		UserWindowEndSnapshot:       NullableTime(row.UserWindowEndSnapshot),
		PaidAt:                      NullableTime(row.PaidAt),
		FulfilledAt:                 NullableTime(row.FulfilledAt),
		CanceledAt:                  NullableTime(row.CanceledAt),
		ExpiresAt:                   NullableTime(row.ExpiresAt),
		CreatedAt:                   row.CreatedAt,
		UpdatedAt:                   row.UpdatedAt,
	}
}

func mapAdminPaymentAttempt(row paymentsqlc.PaymentAttempt) AdminPaymentAttemptModel {
	return AdminPaymentAttemptModel{
		ID:                     row.ID,
		OrderID:                row.OrderID,
		ProviderCode:           row.ProviderCode,
		AssetCode:              row.AssetCode,
		AmountMinor:            row.AmountMinor,
		Status:                 string(row.Status),
		ProviderPaymentID:      NullableString(row.ProviderPaymentID),
		ProviderInvoiceID:      NullableString(row.ProviderInvoiceID),
		ProviderChargeID:       NullableString(row.ProviderChargeID),
		ProviderSubscriptionID: NullableString(row.ProviderSubscriptionID),
		IdempotencyKey:         NullableString(row.IdempotencyKey),
		ConfirmationUrl:        NullableString(row.ConfirmationUrl),
		ReturnUrl:              NullableString(row.ReturnUrl),
		ExpiresAt:              NullableTime(row.ExpiresAt),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

func mapAdminPaymentEvent(row paymentsqlc.PaymentEvent) AdminPaymentEventModel {
	return AdminPaymentEventModel{
		ID:                row.ID,
		ProviderCode:      row.ProviderCode,
		AttemptID:         NullableInt64(row.AttemptID),
		OrderID:           NullableInt64(row.OrderID),
		ProviderEventID:   NullableString(row.ProviderEventID),
		ProviderPaymentID: NullableString(row.ProviderPaymentID),
		EventType:         row.EventType,
		EventStatus:       NullableString(row.EventStatus),
		PayloadHash:       row.PayloadHash,
		SignatureValid:    NullableBool(row.SignatureValid),
		ProcessingStatus:  string(row.ProcessingStatus),
		ProcessingError:   NullableString(row.ProcessingError),
		ReceivedAt:        row.ReceivedAt,
		ProcessedAt:       NullableTime(row.ProcessedAt),
	}
}

func mapAdminSubscription(row paymentsqlc.PaymentSubscription) AdminSubscriptionModel {
	return AdminSubscriptionModel{
		ID:                     row.ID,
		WorkspaceID:            row.WorkspaceID,
		ProviderCode:           row.ProviderCode,
		ProviderSubscriptionID: row.ProviderSubscriptionID,
		AppID:                  row.AppID,
		PlatformID:             row.PlatformID,
		PlatformUserID:         row.PlatformUserID,
		InternalUserID:         NullableInt64(row.InternalUserID),
		ProductID:              row.ProductID,
		OrderID:                NullableInt64(row.OrderID),
		AttemptID:              NullableInt64(row.AttemptID),
		Status:                 string(row.Status),
		CancelReason:           NullableString(row.CancelReason),
		StartedAt:              row.StartedAt,
		EndedAt:                NullableTime(row.EndedAt),
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
}

func mapAdminFulfillment(row paymentsqlc.PaymentFulfillment) AdminFulfillmentModel {
	return AdminFulfillmentModel{
		ID:             row.ID,
		OrderID:        row.OrderID,
		AttemptID:      row.AttemptID,
		InternalUserID: NullableInt64(row.InternalUserID),
		Status:         string(row.Status),
		Error:          NullableString(row.Error),
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
		FulfilledAt:    NullableTime(row.FulfilledAt),
		RevokedAt:      NullableTime(row.RevokedAt),
	}
}

func mapAdminFulfillmentItem(row paymentsqlc.PaymentFulfillmentItem) AdminFulfillmentItemModel {
	unit := NullableString{}
	if row.DurationUnit.Valid {
		unit = NullableString{String: string(row.DurationUnit.PaymentFulfillmentItemDurationUnit), Valid: true}
	}
	return AdminFulfillmentItemModel{
		ID:            row.ID,
		FulfillmentID: row.FulfillmentID,
		WorkspaceID:   row.WorkspaceID,
		ItemID:        row.ItemID,
		RewardType:    string(row.RewardType),
		Quantity:      row.Quantity,
		Scale:         row.Scale,
		DurationUnit:  unit,
		CreatedAt:     row.CreatedAt,
	}
}

func mapAdminRefund(row paymentsqlc.PaymentRefund) AdminRefundModel {
	return AdminRefundModel{
		ID:               row.ID,
		OrderID:          row.OrderID,
		AttemptID:        row.AttemptID,
		ProviderCode:     row.ProviderCode,
		IdempotencyKey:   NullableString(row.IdempotencyKey),
		ProviderRefundID: NullableString(row.ProviderRefundID),
		AmountMinor:      row.AmountMinor,
		AssetCode:        row.AssetCode,
		Status:           string(row.Status),
		Reason:           NullableString(row.Reason),
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func mapAdminProviderCursor(row paymentsqlc.PaymentProviderCursor) AdminProviderCursorModel {
	return AdminProviderCursorModel(row)
}

func mapAdminProviderTransaction(row paymentsqlc.PaymentProviderTransaction) AdminProviderTransactionModel {
	return AdminProviderTransactionModel{
		ID:                    row.ID,
		WorkspaceID:           row.WorkspaceID,
		ProviderCode:          row.ProviderCode,
		Network:               row.Network,
		SourceKey:             row.SourceKey,
		AssetCode:             row.AssetCode,
		ExternalTransactionID: row.ExternalTransactionID,
		SequenceNumber:        row.SequenceNumber,
		SourceAddress:         row.SourceAddress,
		DestinationAddress:    row.DestinationAddress,
		AmountMinor:           row.AmountMinor,
		PaymentReference:      row.PaymentReference,
		SenderReference:       NullableString(row.SenderReference),
		OrderID:               NullableInt64(row.OrderID),
		AttemptID:             NullableInt64(row.AttemptID),
		Status:                string(row.Status),
		Error:                 NullableString(row.Error),
		OccurredAt:            row.OccurredAt,
		CreatedAt:             row.CreatedAt,
	}
}

func mapAdminTONWallet(row paymentsqlc.PaymentTonWallet) AdminTONWalletModel {
	return AdminTONWalletModel{
		WorkspaceID:      row.WorkspaceID,
		Network:          row.Network,
		WalletAddress:    row.WalletAddress,
		NetworkConfigUrl: NullableString(row.NetworkConfigUrl),
		IsEnabled:        row.IsEnabled,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
}

func mapAdminSlice[S any, D any](rows []S, mapper func(S) D) []D {
	result := make([]D, len(rows))
	for index, row := range rows {
		result[index] = mapper(row)
	}
	return result
}

func mapAdminResult[S any, D any](row S, err error, mapper func(S) D) (D, error) {
	if err != nil {
		var zero D
		return zero, err
	}
	return mapper(row), nil
}
