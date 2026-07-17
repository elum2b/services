package platega

import (
	"context"
	"net/http"
	"time"

	json "github.com/goccy/go-json"
)

type CredentialsResolver func(context.Context, string) (Credentials, error)

type PaymentMethod int

const (
	PaymentMethodAny   PaymentMethod = 0
	PaymentMethodSBPQR PaymentMethod = 2
	PaymentMethodCard  PaymentMethod = 10
	PaymentMethodIntl  PaymentMethod = 12
)

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusConfirmed    Status = "CONFIRMED"
	StatusExpired      Status = "EXPIRED"
	StatusCanceled     Status = "CANCELED"
	StatusFailed       Status = "FAILED"
	StatusRefunded     Status = "REFUNDED"
	StatusChargebacked Status = "CHARGEBACKED"
)

type CreatePaymentParams struct {
	Credentials    Credentials
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	InternalUserID *int64
	ProductID      string
	Quantity       uint64
	Locale         string
	Description    string
	ReturnURL      string
	FailedURL      string
	PaymentMethod  PaymentMethod
	IdempotencyKey string
	ExpiresAt      *time.Time
	ReservedUntil  *time.Time
}

type GetH2HParams struct {
	Credentials   Credentials
	TransactionID string
}

type SyncPaymentParams struct {
	Credentials   Credentials
	WorkspaceID   string
	TransactionID string
}

type WebhookRequest struct {
	Credentials Credentials
	WorkspaceID string
	Raw         []byte
	Headers     http.Header
}

type CreatePaymentResponse struct {
	OrderID        uint64        `json:"order_id"`
	OrderPublicID  string        `json:"order_public_id"`
	AttemptID      uint64        `json:"attempt_id"`
	TransactionID  string        `json:"transaction_id"`
	Status         Status        `json:"status"`
	PaymentURL     string        `json:"payment_url,omitempty"`
	RedirectURL    string        `json:"redirect_url,omitempty"`
	ReturnURL      string        `json:"return_url,omitempty"`
	ExpiresIn      string        `json:"expires_in,omitempty"`
	AmountMinor    uint64        `json:"amount_minor"`
	AssetCode      string        `json:"asset_code"`
	PaymentMethod  PaymentMethod `json:"payment_method,omitempty"`
	ProviderMethod string        `json:"provider_method,omitempty"`
}

type WebhookResult struct {
	OrderID     uint64  `json:"order_id"`
	AttemptID   uint64  `json:"attempt_id"`
	EventID     uint64  `json:"event_id,omitempty"`
	Status      Status  `json:"status"`
	AlreadyDone bool    `json:"already_done"`
	FulfilledID *uint64 `json:"fulfillment_id,omitempty"`
}

type ReconcileParams struct {
	ResolveCredentials CredentialsResolver
	CreatedTo          time.Time
	Limit              int32
	MissingAfter       time.Duration
}

type ReconcileResult struct {
	Scanned   int
	Recovered int
	Completed int
	Released  int
}

type H2HResponse struct {
	Amount json.Number `json:"amount"`
	QR     string      `json:"qr"`
}

type paymentDetails struct {
	Amount   json.Number `json:"amount"`
	Currency string      `json:"currency"`
}

type createTransactionRequest struct {
	PaymentMethod  *PaymentMethod `json:"paymentMethod,omitempty"`
	PaymentDetails paymentDetails `json:"paymentDetails"`
	Description    string         `json:"description,omitempty"`
	ReturnURL      string         `json:"return,omitempty"`
	FailedURL      string         `json:"failedUrl,omitempty"`
	Payload        string         `json:"payload,omitempty"`
}

type createTransactionResponse struct {
	PaymentMethod  string  `json:"paymentMethod"`
	TransactionID  string  `json:"transactionId"`
	Redirect       string  `json:"redirect"`
	URL            string  `json:"url"`
	ReturnURL      string  `json:"return"`
	PaymentDetails string  `json:"paymentDetails"`
	Status         Status  `json:"status"`
	ExpiresIn      string  `json:"expiresIn"`
	MerchantID     string  `json:"merchantId"`
	Rate           float64 `json:"rate"`
	USDTRate       float64 `json:"usdtRate"`
}

type transactionStatusResponse struct {
	ID                string         `json:"id"`
	Status            Status         `json:"status"`
	PaymentDetails    paymentDetails `json:"paymentDetails"`
	MerchantName      string         `json:"merchantName"`
	MerchantID        string         `json:"mechantId"`
	Commission        float64        `json:"comission"`
	PaymentMethod     string         `json:"paymentMethod"`
	ExpiresIn         string         `json:"expiresIn"`
	ReturnURL         string         `json:"return"`
	QR                string         `json:"qr"`
	PayformSuccessURL string         `json:"payformSuccessUrl"`
	Payload           string         `json:"payload"`
	ExternalID        string         `json:"externalId"`
	Description       string         `json:"description"`
}

type callbackPayload struct {
	ID            string        `json:"id"`
	Amount        json.Number   `json:"amount"`
	Currency      string        `json:"currency"`
	Status        Status        `json:"status"`
	PaymentMethod PaymentMethod `json:"paymentMethod"`
}

type exportTransactionsRequest struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	TimeZoneID string    `json:"timeZoneId"`
}

type exportedTransaction struct {
	RecordID      string      `json:"recordId"`
	Amount        json.Number `json:"amount"`
	CurrencyCode  string      `json:"currencyCode"`
	Status        Status      `json:"status"`
	PaymentMethod string      `json:"paymentMethod"`
	Payload       string      `json:"payload"`
}
