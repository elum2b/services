package repository

import (
	json "github.com/goccy/go-json"
	"time"

	services "github.com/elum2b/services"
)

const (
	StatusSuccess        = "success"
	StatusAlreadyApplied = "already_applied"
	StatusNotFound       = "not_found"
	StatusInactive       = "inactive"
	StatusNotStarted     = "not_started"
	StatusExpired        = "expired"
	StatusLimitReached   = "limit_reached"
)

type Promo struct {
	ID              uint64
	WorkspaceID     string
	Code            string
	Payload         json.RawMessage
	Target          json.RawMessage
	MaxActivations  uint64
	ActivationCount uint64
	IsActive        bool
	StartAt         *time.Time
	EndAt           *time.Time
	DeletedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Localization struct {
	WorkspaceID string
	PromoID     uint64
	Locale      string
	Title       string
	Description string
}

type Reward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type Identity struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	Platform       string
	PlatformUserID string
	IsPremium      bool
	Sex            string
	Country        string
}

func (i Identity) Validate() error {
	return (services.Identity{
		WorkspaceID:    i.WorkspaceID,
		AppID:          i.AppID,
		PlatformID:     i.PlatformID,
		Platform:       i.Platform,
		PlatformUserID: i.PlatformUserID,
		IsPremium:      i.IsPremium,
		Sex:            i.Sex,
		Country:        i.Country,
	}).Validate()
}

type Redemption struct {
	ID             uint64
	WorkspaceID    string
	PromoID        uint64
	AppID          int64
	PlatformID     int64
	PlatformUserID string
	RedeemedAt     time.Time
}

type ApplyResult struct {
	Status       string
	Promo        Promo
	Localization *Localization
	Rewards      []Reward
	Redemption   *Redemption
}

type Stats struct {
	ActivationCount      uint64
	MaxActivations       uint64
	RemainingActivations *uint64
}

type DailyStats struct {
	Date            time.Time
	RedemptionCount uint64
	UniqueUsers     uint64
}
