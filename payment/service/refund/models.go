package refund

type Params struct {
	WorkspaceID    string
	OrderID        uint64
	AttemptID      uint64
	IdempotencyKey string
	AmountMinor    uint64
	Reason         string
	ProviderParams any
}

type Result struct {
	RefundID         uint64  `json:"refund_id"`
	OrderID          uint64  `json:"order_id"`
	AttemptID        uint64  `json:"attempt_id"`
	ProviderCode     string  `json:"provider_code"`
	ProviderRefundID *string `json:"provider_refund_id,omitempty"`
	AmountMinor      uint64  `json:"amount_minor"`
	AssetCode        string  `json:"asset_code"`
	Status           string  `json:"status"`
}
