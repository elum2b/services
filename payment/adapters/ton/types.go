package ton

import "time"

type CreatePaymentParams struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	InternalUserID *int64
	ProductID      string
	Quantity       uint64
	AssetCode      string
	Locale         string
	ExpiresAt      *time.Time
	ReservedUntil  *time.Time
}

type CreateTransactionParams struct {
	AssetCode           string
	Network             string
	NetworkConfigURL    string
	SourceWallet        string
	Destination         string
	ResponseDestination string
	AmountMinor         uint64
	Comment             string
}

type Transaction struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Amount  string `json:"amount"`
	Payload string `json:"payload,omitempty"`
}

type CreatePaymentResponse struct {
	OrderID        uint64 `json:"order_id"`
	OrderPublicID  string `json:"order_public_id"`
	AttemptID      uint64 `json:"attempt_id"`
	WalletAddress  string `json:"wallet_address"`
	Network        string `json:"network"`
	AssetCode      string `json:"asset_code"`
	AmountMinor    uint64 `json:"amount_minor"`
	Comment        string `json:"comment"`
	Decimals       uint16 `json:"decimals"`
	ProviderStatus string `json:"provider_status"`
}

type ProcessResult struct {
	OrderID     uint64 `json:"order_id"`
	AttemptID   uint64 `json:"attempt_id"`
	Transaction uint64 `json:"transaction_id"`
	AlreadyDone bool   `json:"already_done"`
	Ignored     bool   `json:"ignored"`
}

type IncomingTransfer struct {
	WorkspaceID        string
	Network            string
	WalletAddress      string
	AssetCode          string
	TxHash             string
	LogicalTime        uint64
	SourceAddress      string
	DestinationAddress string
	AmountMinor        uint64
	Comment            string
	JettonSender       string
	OccurredAt         time.Time
}
