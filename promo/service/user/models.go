package user

import (
	json "github.com/goccy/go-json"
	"time"

	services "github.com/elum2b/services"
)

type Identity = services.Identity

type RewardModel struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type PromoModel struct {
	ID              uint64          `json:"id"`
	Code            string          `json:"code"`
	Payload         json.RawMessage `json:"payload"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	MaxActivations  uint64          `json:"max_activations"`
	ActivationCount uint64          `json:"activation_count"`
	IsActive        bool            `json:"is_active"`
	StartAt         *time.Time      `json:"start_at,omitempty"`
	EndAt           *time.Time      `json:"end_at,omitempty"`
	Rewards         []RewardModel   `json:"rewards,omitempty"`
}

type RedemptionModel struct {
	ID         uint64    `json:"id"`
	RedeemedAt time.Time `json:"redeemed_at"`
}
