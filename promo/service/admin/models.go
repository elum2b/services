package admin

import (
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/promo/repository"
	"github.com/elum2b/services/promo/service/user"
)

type Page struct {
	Limit  int32
	Offset int32
}

type PromoModel struct {
	ID              uint64              `json:"id"`
	Code            string              `json:"code"`
	Payload         json.RawMessage     `json:"payload"`
	Target          json.RawMessage     `json:"target,omitempty"`
	MaxActivations  uint64              `json:"max_activations"`
	ActivationCount uint64              `json:"activation_count"`
	IsActive        bool                `json:"is_active"`
	StartAt         *time.Time          `json:"start_at,omitempty"`
	EndAt           *time.Time          `json:"end_at,omitempty"`
	DeletedAt       *time.Time          `json:"deleted_at,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
	Localizations   []LocalizationModel `json:"localizations,omitempty"`
	Rewards         []user.RewardModel  `json:"rewards,omitempty"`
}

type LocalizationModel struct {
	Locale      string `json:"locale"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type RedemptionModel struct {
	ID             uint64    `json:"id"`
	AppID          int64     `json:"app_id"`
	PlatformID     int64     `json:"platform_id"`
	PlatformUserID string    `json:"platform_user_id"`
	RedeemedAt     time.Time `json:"redeemed_at"`
}

type StatsModel struct {
	ActivationCount      uint64  `json:"activation_count"`
	MaxActivations       uint64  `json:"max_activations"`
	RemainingActivations *uint64 `json:"remaining_activations,omitempty"`
}

type DailyStatsModel struct {
	Date            time.Time `json:"date"`
	RedemptionCount uint64    `json:"redemption_count"`
	UniqueUsers     uint64    `json:"unique_users"`
}

type ExportRequest = repository.ExportRequest
type ExportPackage = repository.ExportPackage
type ExportPromo = repository.ExportPromo
type ExportText = repository.ExportText
type ExportReward = repository.ExportReward
type ImportRequest = repository.ImportRequest
type ImportPreview = repository.ImportPreview
type ImportCounts = repository.ImportCounts
type ImportConflict = repository.ImportConflict
type ImportResult = repository.ImportResult
