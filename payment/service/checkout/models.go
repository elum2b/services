package checkout

import (
	"time"

	services "github.com/elum2b/services"
)

type CreateOrderParams struct {
	Identity       services.Identity
	InternalUserID *int64
	Payer          *services.Actor
	ProductID      string
	Quantity       uint64
	AssetCode      string
	Locale         string
	ReservedUntil  *time.Time
	ExpiresAt      *time.Time
}

type CreateOrderByKeyParams struct {
	Key           string
	Payer         *services.Actor
	AssetCode     string
	Quantity      uint64
	Locale        string
	ReservedUntil *time.Time
	ExpiresAt     *time.Time
}

type Order struct {
	ID                  uint64  `json:"id"`
	PublicID            string  `json:"public_id"`
	WorkspaceID         string  `json:"workspace_id"`
	AppID               int64   `json:"app_id"`
	PlatformID          int64   `json:"platform_id"`
	PlatformUserID      string  `json:"platform_user_id"`
	InternalUserID      *uint64 `json:"internal_user_id,omitempty"`
	PayerPlatformID     *uint64 `json:"payer_platform_id,omitempty"`
	PayerPlatformUserID *string `json:"payer_platform_user_id,omitempty"`
	PayerInternalUserID *uint64 `json:"payer_internal_user_id,omitempty"`
	ProductID           string  `json:"product_id"`
	Quantity            uint64  `json:"quantity"`
	PriceID             uint64  `json:"price_id"`
	AssetCode           string  `json:"asset_code"`
	Locale              string  `json:"locale"`
	ListAmountMinor     uint64  `json:"list_amount_minor"`
	DiscountAmountMinor uint64  `json:"discount_amount_minor"`
	PayableAmountMinor  uint64  `json:"payable_amount_minor"`
	Status              string  `json:"status"`
}

type CreateAttemptParams struct {
	Identity               services.Identity
	OrderID                uint64
	ProviderCode           string
	ProviderPaymentID      *string
	ProviderInvoiceID      *string
	ProviderChargeID       *string
	ProviderSubscriptionID *string
	IdempotencyKey         *string
	ConfirmationURL        *string
	ReturnURL              *string
	ExpiresAt              *time.Time
}

type Attempt struct {
	ID                uint64  `json:"id"`
	OrderID           uint64  `json:"order_id"`
	ProviderCode      string  `json:"provider_code"`
	AssetCode         string  `json:"asset_code"`
	AmountMinor       uint64  `json:"amount_minor"`
	Status            string  `json:"status"`
	ProviderPaymentID *string `json:"provider_payment_id,omitempty"`
}

type CreateEventParams struct {
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
	OrderID       uint64  `json:"order_id"`
	AttemptID     uint64  `json:"attempt_id"`
	FulfillmentID *uint64 `json:"fulfillment_id,omitempty"`
	AlreadyDone   bool    `json:"already_done"`
}
