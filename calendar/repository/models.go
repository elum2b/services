package repository

import (
	json "github.com/goccy/go-json"
	"time"

	services "github.com/elum2b/services"
)

const (
	ModeInterval        = "interval"
	ModeSequential      = "sequential"
	ModeSequentialReset = "sequential_reset"

	IntervalCalendar = "calendar"
	IntervalFloating = "floating"

	EndRestart    = "restart"
	EndRepeatLast = "repeat_last"
	EndStop       = "stop"

	StatusGranted      = "granted"
	StatusNotFound     = "not_found"
	StatusInactive     = "inactive"
	StatusNotStarted   = "not_started"
	StatusExpired      = "expired"
	StatusNotAvailable = "not_available"
	StatusCompleted    = "completed"
	StatusNoSteps      = "no_steps"
)

type Identity struct {
	WorkspaceID    string
	AppID          int64
	PlatformID     int64
	PlatformUserID string
}

func (i Identity) Validate() error {
	return (services.Identity{
		WorkspaceID:    i.WorkspaceID,
		AppID:          i.AppID,
		PlatformID:     i.PlatformID,
		PlatformUserID: i.PlatformUserID,
	}).Validate()
}

type Calendar struct {
	ID                  string
	WorkspaceID         string
	Type                string
	Mode                string
	IntervalType        string
	IntervalUnit        string
	IntervalCount       uint32
	ResetAfterIntervals uint32
	EndBehavior         string
	Timezone            string
	HideFutureRewards   bool
	IsActive            bool
	StartAt             *time.Time
	EndAt               *time.Time
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Localization        *Localization
	Steps               []Step
}

type Localization struct {
	WorkspaceID string
	CalendarID  string
	Locale      string
	Title       string
	Description string
}

type Step struct {
	ID       uint64
	Position uint32
	Rewards  []Reward
}

type Reward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type Progress struct {
	CurrentPosition   uint32
	ClaimCount        uint64
	LastClaimPosition *uint32
	LastClaimAt       *time.Time
	NextClaimAt       *time.Time
	IsCompleted       bool
	ResetCount        uint64
	LastWasReset      bool
}

type RecordParams struct {
	Identity    Identity
	CalendarRef string
	OperationID string
	Now         time.Time
}

type RecordResult struct {
	OperationRowID uint64
	OperationID    string
	Granted        bool
	Status         string
	Calendar       Calendar
	Position       *uint32
	Rewards        []Reward
	Progress       Progress
	OccurredAt     time.Time
}

type Operation struct {
	ID              uint64
	Identity        Identity
	CalendarID      string
	OperationID     string
	Granted         bool
	Status          string
	Position        *uint32
	Rewards         json.RawMessage
	CurrentPosition uint32
	ClaimCount      uint64
	OccurredAt      time.Time
}

type Stats struct {
	OperationCount uint64
	GrantCount     uint64
	UniqueUsers    uint64
}

type DailyStats struct {
	Date           time.Time
	OperationCount uint64
	GrantCount     uint64
	UniqueUsers    uint64
}
