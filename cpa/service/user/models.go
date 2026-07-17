package user

import (
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/cpa/model"
)

type Identity = services.Identity

type OfferModel struct {
	ID          string           `json:"id"`
	Payload     json.RawMessage  `json:"payload"`
	CodeMode    string           `json:"code_mode"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	StartAt     *time.Time       `json:"start_at,omitempty"`
	EndAt       *time.Time       `json:"end_at,omitempty"`
	Rewards     []RewardModel    `json:"rewards,omitempty"`
	Assignment  *AssignmentModel `json:"assignment,omitempty"`
}

type RewardModel struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type AssignmentModel struct {
	ID          uint64                 `json:"id"`
	CPAID       string                 `json:"cpa_id"`
	Code        string                 `json:"code"`
	CodeMode    string                 `json:"code_mode"`
	Status      model.AssignmentStatus `json:"status"`
	IssuedAt    time.Time              `json:"issued_at"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
}
