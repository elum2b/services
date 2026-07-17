package yookassa

import "time"

type PaymentMethodType string

const (
	PaymentMethodBankCard    PaymentMethodType = "bank_card"
	PaymentMethodSberbank    PaymentMethodType = "sberbank"
	PaymentMethodSBP         PaymentMethodType = "sbp"
	PaymentMethodTinkoffBank PaymentMethodType = "tinkoff_bank"
	PaymentMethodYooMoney    PaymentMethodType = "yoo_money"
)

type CreatePaymentParams struct {
	Credentials       Credentials
	WorkspaceID       string
	AppID             int64
	PlatformID        int64
	PlatformUserID    string
	InternalUserID    *int64
	ProductID         string
	Quantity          uint64
	Locale            string
	ReturnURL         string
	Description       string
	IdempotencyKey    string
	PaymentMethodType PaymentMethodType
	Receipt           *Receipt
	Capture           *bool
	ExpiresAt         *time.Time
	ReservedUntil     *time.Time
	ConfirmationURL   string
}

type CreatePaymentResponse struct {
	OrderID           uint64            `json:"order_id"`
	OrderPublicID     string            `json:"order_public_id"`
	AttemptID         uint64            `json:"attempt_id"`
	PaymentID         string            `json:"payment_id"`
	Status            string            `json:"status"`
	ConfirmationURL   string            `json:"confirmation_url,omitempty"`
	AmountMinor       uint64            `json:"amount_minor"`
	AssetCode         string            `json:"asset_code"`
	PaymentMethodType PaymentMethodType `json:"payment_method_type,omitempty"`
}

type WebhookResult struct {
	OrderID     uint64  `json:"order_id"`
	AttemptID   uint64  `json:"attempt_id"`
	EventID     uint64  `json:"event_id,omitempty"`
	Status      string  `json:"status"`
	AlreadyDone bool    `json:"already_done"`
	FulfilledID *uint64 `json:"fulfillment_id,omitempty"`
}

type WebhookRequest struct {
	WorkspaceID    string
	Raw            []byte
	SignatureValid bool
}

type RefundParams struct {
	Credentials    Credentials
	PaymentID      string
	AmountMinor    uint64
	AssetCode      string
	Description    string
	IdempotencyKey string
}

type SyncPaymentParams struct {
	Credentials Credentials
	WorkspaceID string
	PaymentID   string
}

type RefundResult struct {
	ProviderRefundID string `json:"provider_refund_id"`
	Status           string `json:"status"`
}

type Receipt struct {
	Customer ReceiptCustomer `json:"customer"`
	Items    []ReceiptItem   `json:"items"`
}

type ReceiptCustomer struct {
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type ReceiptItem struct {
	Description    string            `json:"description"`
	Quantity       string            `json:"quantity"`
	Amount         Amount            `json:"amount"`
	VATCode        int               `json:"vat_code"`
	PaymentMode    string            `json:"payment_mode,omitempty"`
	PaymentSubject string            `json:"payment_subject,omitempty"`
	Measure        string            `json:"measure,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type Amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type createPaymentRequest struct {
	Amount            Amount                 `json:"amount"`
	Capture           bool                   `json:"capture"`
	Confirmation      yookassaConfirmation   `json:"confirmation"`
	PaymentMethodData *yookassaPaymentMethod `json:"payment_method_data,omitempty"`
	Receipt           *Receipt               `json:"receipt,omitempty"`
	Description       string                 `json:"description,omitempty"`
	Metadata          map[string]string      `json:"metadata,omitempty"`
}

type yookassaConfirmation struct {
	Type            string `json:"type"`
	ReturnURL       string `json:"return_url,omitempty"`
	ConfirmationURL string `json:"confirmation_url,omitempty"`
}

type yookassaPaymentMethod struct {
	Type PaymentMethodType `json:"type"`
}

type paymentAPIResponse struct {
	ID           string               `json:"id"`
	Status       string               `json:"status"`
	Paid         bool                 `json:"paid"`
	Amount       Amount               `json:"amount"`
	Confirmation yookassaConfirmation `json:"confirmation"`
}

type createRefundRequest struct {
	PaymentID   string `json:"payment_id"`
	Amount      Amount `json:"amount"`
	Description string `json:"description,omitempty"`
}

type refundAPIResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	PaymentID string `json:"payment_id"`
	Amount    Amount `json:"amount"`
}

type webhookPayload struct {
	Type   string               `json:"type"`
	Event  string               `json:"event"`
	Object webhookPaymentObject `json:"object"`
}

type webhookPaymentObject struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Paid   bool   `json:"paid"`
	Amount Amount `json:"amount"`
}
