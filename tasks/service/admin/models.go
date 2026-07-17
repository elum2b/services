package admin

import (
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

type SaveTaskParams struct {
	ID                  uint64
	WorkspaceID         string
	Key                 string
	GroupKey            string
	SequenceKey         *string
	SequencePosition    *uint32
	TaskKind            string
	ActionKey           string
	ActionKind          string
	ClaimMode           string
	StartMode           string
	TargetCount         uint64
	ResetUnit           string
	ResetEvery          uint32
	Position            int32
	Payload             json.RawMessage
	Target              json.RawMessage
	IntegrationKind     *string
	IntegrationProvider *string
	IntegrationPayload  json.RawMessage
	ImageURL            *string
	IsVisible           bool
	IsActive            bool
	StartAt             *time.Time
	EndAt               *time.Time
}

type TaskModel struct {
	ID                  uint64          `json:"id"`
	Key                 string          `json:"key"`
	GroupKey            string          `json:"group_key"`
	SequenceKey         *string         `json:"sequence_key,omitempty"`
	SequencePosition    *uint32         `json:"sequence_position,omitempty"`
	TaskKind            string          `json:"task_kind"`
	ActionKey           string          `json:"action_key"`
	ActionKind          string          `json:"action_kind"`
	ClaimMode           string          `json:"claim_mode"`
	StartMode           string          `json:"start_mode"`
	TargetCount         uint64          `json:"target_count"`
	ResetUnit           string          `json:"reset_unit"`
	ResetEvery          uint32          `json:"reset_every"`
	Position            int32           `json:"position"`
	Payload             json.RawMessage `json:"payload,omitempty"`
	Target              json.RawMessage `json:"target,omitempty"`
	IntegrationKind     *string         `json:"integration_kind,omitempty"`
	IntegrationProvider *string         `json:"integration_provider,omitempty"`
	IntegrationPayload  json.RawMessage `json:"integration_payload,omitempty"`
	ImageURL            *string         `json:"image_url,omitempty"`
	IsVisible           bool            `json:"is_visible"`
	IsActive            bool            `json:"is_active"`
	StartAt             *time.Time      `json:"start_at,omitempty"`
	EndAt               *time.Time      `json:"end_at,omitempty"`
	DeletedAt           *time.Time      `json:"deleted_at,omitempty"`
}

type RewardModel struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
}

type ComplexConditionModel struct {
	WorkspaceID     string `json:"workspace_id,omitempty"`
	ParentTaskID    uint64 `json:"parent_task_id"`
	ConditionTaskID uint64 `json:"condition_task_id"`
	RequiredStatus  string `json:"required_status"`
	Position        int32  `json:"position"`
	IsRequired      bool   `json:"is_required"`
}

type StatsModel struct {
	TasksTotal         uint64 `json:"tasks_total"`
	ActiveTasks        uint64 `json:"active_tasks"`
	VisibleTasks       uint64 `json:"visible_tasks"`
	ProgressTotal      uint64 `json:"progress_total"`
	OpenProgress       uint64 `json:"open_progress"`
	ReadyProgress      uint64 `json:"ready_progress"`
	ClaimedProgress    uint64 `json:"claimed_progress"`
	ProgressCreated    uint64 `json:"progress_created"`
	ProgressAmount     uint64 `json:"progress_amount"`
	ReadyCount         uint64 `json:"ready_count"`
	ClaimedCount       uint64 `json:"claimed_count"`
	ManualClaimedCount uint64 `json:"manual_claimed_count"`
	AutoClaimedCount   uint64 `json:"auto_claimed_count"`
	UniqueParticipants uint64 `json:"unique_participants"`
	UniqueClaimers     uint64 `json:"unique_claimers"`
}

type TaskStatsModel struct {
	TaskID             uint64 `json:"task_id"`
	ProgressTotal      uint64 `json:"progress_total"`
	OpenProgress       uint64 `json:"open_progress"`
	ReadyProgress      uint64 `json:"ready_progress"`
	ClaimedProgress    uint64 `json:"claimed_progress"`
	ProgressCreated    uint64 `json:"progress_created"`
	ProgressAmount     uint64 `json:"progress_amount"`
	ReadyCount         uint64 `json:"ready_count"`
	ClaimedCount       uint64 `json:"claimed_count"`
	ManualClaimedCount uint64 `json:"manual_claimed_count"`
	AutoClaimedCount   uint64 `json:"auto_claimed_count"`
	UniqueParticipants uint64 `json:"unique_participants"`
	UniqueClaimers     uint64 `json:"unique_claimers"`
}

type DailyStatsModel struct {
	Date               time.Time `json:"date"`
	TaskID             uint64    `json:"task_id"`
	ProgressCreated    uint64    `json:"progress_created"`
	ProgressAmount     uint64    `json:"progress_amount"`
	ReadyCount         uint64    `json:"ready_count"`
	ClaimedCount       uint64    `json:"claimed_count"`
	ManualClaimedCount uint64    `json:"manual_claimed_count"`
	AutoClaimedCount   uint64    `json:"auto_claimed_count"`
	UniqueParticipants uint64    `json:"unique_participants"`
	UniqueClaimers     uint64    `json:"unique_claimers"`
}

type DailyOverviewModel struct {
	Date               time.Time `json:"date"`
	TasksTotal         uint64    `json:"tasks_total"`
	ActiveTasks        uint64    `json:"active_tasks"`
	VisibleTasks       uint64    `json:"visible_tasks"`
	ProgressCreated    uint64    `json:"progress_created"`
	ProgressAmount     uint64    `json:"progress_amount"`
	ReadyCount         uint64    `json:"ready_count"`
	ClaimedCount       uint64    `json:"claimed_count"`
	ManualClaimedCount uint64    `json:"manual_claimed_count"`
	AutoClaimedCount   uint64    `json:"auto_claimed_count"`
	UniqueParticipants uint64    `json:"unique_participants"`
	UniqueClaimers     uint64    `json:"unique_claimers"`
}

type PartnerConfigModel struct {
	WorkspaceID   string          `json:"workspace_id"`
	Provider      string          `json:"provider"`
	GroupKey      string          `json:"group_key"`
	Platform      string          `json:"platform"`
	IsEnabled     bool            `json:"is_enabled"`
	Secret        *string         `json:"secret,omitempty"`
	WebhookSecret *string         `json:"webhook_secret,omitempty"`
	Target        json.RawMessage `json:"target,omitempty"`
	Settings      json.RawMessage `json:"settings,omitempty"`
	CreatedAt     time.Time       `json:"created_at,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at,omitempty"`
}

type SavePartnerRewardRuleParams struct {
	WorkspaceID  string      `json:"workspace_id"`
	Provider     string      `json:"provider"`
	GroupKey     string      `json:"group_key"`
	ExternalType string      `json:"external_type"`
	Reward       RewardModel `json:"reward"`
	Position     int32       `json:"position"`
	IsEnabled    bool        `json:"is_enabled"`
}

type PartnerDailyStatsModel struct {
	Date                   time.Time `json:"date"`
	Provider               string    `json:"provider"`
	GroupKey               string    `json:"group_key"`
	ExternalType           string    `json:"external_type"`
	IssuedCount            uint64    `json:"issued_count"`
	CompletedCount         uint64    `json:"completed_count"`
	ClaimedCount           uint64    `json:"claimed_count"`
	RevokedCount           uint64    `json:"revoked_count"`
	RevokedAfterClaimCount uint64    `json:"revoked_after_claim_count"`
	FailedCount            uint64    `json:"failed_count"`
	FakeCount              uint64    `json:"fake_count"`
	ExpiredCount           uint64    `json:"expired_count"`
	UniqueIssuedUsers      uint64    `json:"unique_issued_users"`
	UniqueCompletedUsers   uint64    `json:"unique_completed_users"`
	UniqueClaimers         uint64    `json:"unique_claimers"`
}

type SaveComplexConditionParams = repository.SaveComplexConditionParams
type ExportPackage = repository.ExportPackage
type ExportRequest = repository.ExportRequest
type ExportManifest = repository.ExportManifest
type ExportManifestSection = repository.ExportManifestSection
type ExportGroup = repository.ExportGroup
type ExportText = repository.ExportText
type ExportSequence = repository.ExportSequence
type ExportTask = repository.ExportTask
type ExportReset = repository.ExportReset
type ExportIntegration = repository.ExportIntegration
type ExportReward = repository.ExportReward
type ExportPartnerConfig = repository.ExportPartnerConfig
type ExportSecret = repository.ExportSecret
type ExportPartnerRewardRule = repository.ExportPartnerRewardRule
type ImportRequest = repository.ImportRequest
type ImportPreview = repository.ImportPreview
type ImportCounts = repository.ImportCounts
type ImportConflict = repository.ImportConflict
type ImportResult = repository.ImportResult
