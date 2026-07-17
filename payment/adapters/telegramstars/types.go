package telegramstars

import "time"

type CreatePaymentParams struct {
	Credentials        Credentials
	WorkspaceID        string
	AppID              int64
	PlatformID         int64
	PlatformUserID     string
	InternalUserID     *int64
	ProductID          string
	Quantity           uint64
	Locale             string
	Title              string
	Description        string
	IdempotencyKey     string
	SubscriptionPeriod int
	ExpiresAt          *time.Time
	ReservedUntil      *time.Time
}

type CreatePaymentResponse struct {
	OrderID            uint64 `json:"order_id"`
	OrderPublicID      string `json:"order_public_id"`
	AttemptID          uint64 `json:"attempt_id"`
	InvoiceLink        string `json:"invoice_link"`
	AmountMinor        uint64 `json:"amount_minor"`
	AssetCode          string `json:"asset_code"`
	SubscriptionPeriod int    `json:"subscription_period,omitempty"`
}

type PreCheckoutQuery struct {
	Credentials    Credentials `json:"-"`
	WorkspaceID    string      `json:"-"`
	ID             string      `json:"id"`
	FromID         int64       `json:"from_id"`
	Currency       string      `json:"currency"`
	TotalAmount    uint64      `json:"total_amount"`
	InvoicePayload string      `json:"invoice_payload"`
}

type PreCheckoutResult struct {
	AttemptID uint64 `json:"attempt_id,omitempty"`
	OrderID   uint64 `json:"order_id,omitempty"`
	Accepted  bool   `json:"accepted"`
}

type SuccessfulPayment struct {
	WorkspaceID                string `json:"-"`
	Currency                   string `json:"currency"`
	TotalAmount                uint64 `json:"total_amount"`
	InvoicePayload             string `json:"invoice_payload"`
	TelegramPaymentChargeID    string `json:"telegram_payment_charge_id"`
	SubscriptionExpirationDate int64  `json:"subscription_expiration_date,omitempty"`
	IsRecurring                bool   `json:"is_recurring,omitempty"`
	IsFirstRecurring           bool   `json:"is_first_recurring,omitempty"`
}

type SuccessfulPaymentResult struct {
	OrderID       uint64  `json:"order_id"`
	AttemptID     uint64  `json:"attempt_id"`
	EventID       uint64  `json:"event_id,omitempty"`
	RenewalID     *uint64 `json:"renewal_id,omitempty"`
	AlreadyDone   bool    `json:"already_done"`
	FulfillmentID *uint64 `json:"fulfillment_id,omitempty"`
}

type RefundParams struct {
	Credentials             Credentials
	UserID                  int64
	TelegramPaymentChargeID string
}

type RefundResult struct {
	ProviderRefundID string `json:"provider_refund_id,omitempty"`
	Status           string `json:"status"`
}

type EditSubscriptionParams struct {
	Credentials             Credentials
	UserID                  int64
	TelegramPaymentChargeID string
	IsCanceled              bool
}

type LabeledPrice struct {
	Label  string `json:"label"`
	Amount uint64 `json:"amount"`
}

type createInvoiceLinkRequest struct {
	Title              string         `json:"title"`
	Description        string         `json:"description"`
	Payload            string         `json:"payload"`
	ProviderToken      string         `json:"provider_token"`
	Currency           string         `json:"currency"`
	Prices             []LabeledPrice `json:"prices"`
	SubscriptionPeriod int            `json:"subscription_period,omitempty"`
}

type answerPreCheckoutQueryRequest struct {
	PreCheckoutQueryID string `json:"pre_checkout_query_id"`
	OK                 bool   `json:"ok"`
	ErrorMessage       string `json:"error_message,omitempty"`
}

type refundStarPaymentRequest struct {
	UserID                  int64  `json:"user_id"`
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
}

type editUserStarSubscriptionRequest struct {
	UserID                  int64  `json:"user_id"`
	TelegramPaymentChargeID string `json:"telegram_payment_charge_id"`
	IsCanceled              bool   `json:"is_canceled"`
}

type botAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}
