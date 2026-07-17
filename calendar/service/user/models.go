package user

import (
	"time"

	services "github.com/elum2b/services"
)

type Identity = services.Identity

type ListActiveParams struct {
	WorkspaceID string
	Locale      string
	Now         time.Time
}

type GetCalendarParams struct {
	Identity Identity
	Ref      string
	Locale   string
	Now      time.Time
}

type GetProgressParams struct {
	Identity   Identity
	CalendarID string
}

type RewardModel struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type StepModel struct {
	ID       uint64        `json:"id"`
	Position uint32        `json:"position"`
	Rewards  []RewardModel `json:"rewards,omitempty"`
}

type CalendarModel struct {
	ID                  string      `json:"id"`
	Type                string      `json:"type"`
	Mode                string      `json:"mode"`
	IntervalType        string      `json:"interval_type"`
	IntervalUnit        string      `json:"interval_unit"`
	IntervalCount       uint32      `json:"interval_count"`
	ResetAfterIntervals uint32      `json:"reset_after_intervals"`
	EndBehavior         string      `json:"end_behavior"`
	Timezone            string      `json:"timezone"`
	HideFutureRewards   bool        `json:"hide_future_rewards"`
	IsActive            bool        `json:"is_active"`
	StartAt             *time.Time  `json:"start_at,omitempty"`
	EndAt               *time.Time  `json:"end_at,omitempty"`
	Title               string      `json:"title"`
	Description         string      `json:"description"`
	Steps               []StepModel `json:"steps,omitempty"`
}

type ProgressModel struct {
	CurrentPosition   uint32     `json:"current_position"`
	ClaimCount        uint64     `json:"claim_count"`
	LastClaimPosition *uint32    `json:"last_claim_position,omitempty"`
	LastClaimAt       *time.Time `json:"last_claim_at,omitempty"`
	NextClaimAt       *time.Time `json:"next_claim_at,omitempty"`
	IsCompleted       bool       `json:"is_completed"`
	ResetCount        uint64     `json:"reset_count"`
	LastWasReset      bool       `json:"last_was_reset"`
}

type RecordResult struct {
	OperationRowID uint64        `json:"operation_row_id,omitempty"`
	OperationID    string        `json:"operation_id"`
	Granted        bool          `json:"granted"`
	Status         string        `json:"status"`
	Calendar       CalendarModel `json:"calendar"`
	Position       *uint32       `json:"position,omitempty"`
	Rewards        []RewardModel `json:"rewards,omitempty"`
	Progress       ProgressModel `json:"progress"`
	OccurredAt     time.Time     `json:"occurred_at"`
}

type ActiveCalendarModel struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Mode        string     `json:"mode"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	IsActive    bool       `json:"is_active"`
	StartAt     *time.Time `json:"start_at,omitempty"`
	EndAt       *time.Time `json:"end_at,omitempty"`
}
