package repository

import (
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
)

const (
	TaskKindInternal           = "internal"
	TaskKindChannelBoost       = "channel_boost"
	TaskKindChannelSubscribe   = "channel_subscribe"
	TaskKindExternalCheck      = "external_check"
	TaskKindExternalConfirming = "external_confirming"
	TaskKindComplex            = "complex"

	ActionKindAppAction         = "app_action"
	ActionKindAmountAction      = "amount_action"
	ActionKindChannelBoost      = "channel_boost"
	ActionKindChannelSubscribe  = "channel_subscribe"
	ActionKindAdvertisementView = "advertisement_view"
	ActionKindExternal          = "external"
	ActionKindComposite         = "composite"

	ClaimModeManual   = "manual"
	ClaimModeAuto     = "auto"
	StartModeNone     = "none"
	StartModeRequired = "required"

	ResetNever  = "never"
	ResetSecond = "second"
	ResetMinute = "minute"
	ResetHour   = "hour"
	ResetDay    = "day"
	ResetYear   = "year"

	StatusOpen    = "open"
	StatusReady   = "ready"
	StatusClaimed = "claimed"

	ComplexRequiredStatusReady   = "ready"
	ComplexRequiredStatusClaimed = "claimed"

	RecordStatusRecorded   = "recorded"
	RecordStatusDuplicate  = "duplicate"
	RecordStatusNoTasks    = "no_tasks"
	StartStatusStarted     = "started"
	ClaimStatusClaimed     = "claimed"
	ClaimStatusAlreadyDone = "already_claimed"
	ClaimStatusNotReady    = "not_ready"
	ClaimStatusNotFound    = "not_found"
	ClaimStatusNotStarted  = "not_started"
	ClaimStatusExpired     = "expired"

	CallbackEventClaimed = "task.claimed"
	CallbackEventRevoked = "task.partner.revoked"

	TaskKindPartner = "partner"

	PartnerIssueStatusIssued            = "issued"
	PartnerIssueStatusCompleted         = "completed"
	PartnerIssueStatusClaimed           = "claimed"
	PartnerIssueStatusExpired           = "expired"
	PartnerIssueStatusRevoked           = "revoked"
	PartnerIssueStatusRevokedAfterClaim = "revoked_after_claim"

	PartnerStatsEventIssued            = "issued"
	PartnerStatsEventCompleted         = "completed"
	PartnerStatsEventClaimed           = "claimed"
	PartnerStatsEventRevoked           = "revoked"
	PartnerStatsEventRevokedAfterClaim = "revoked_after_claim"
	PartnerStatsEventFailed            = "failed"
	PartnerStatsEventFake              = "fake"
	PartnerStatsEventExpired           = "expired"

	PartnerIssueKeyPrefix = "partner_issue:"

	ExportFormat = "tasks.export.v1"

	ExportSectionGroups         = "groups"
	ExportSectionSequences      = "sequences"
	ExportSectionTasks          = "tasks"
	ExportSectionLocalization   = "localization"
	ExportSectionRewards        = "rewards"
	ExportSectionTarget         = "target"
	ExportSectionIntegration    = "integration"
	ExportSectionPartnerConfigs = "partner_configs"
	ExportSectionPartnerRewards = "partner_rewards"
	ExportSectionComplex        = "complex"

	ImportConflictFail   = "fail_on_conflict"
	ImportConflictSkip   = "skip_existing"
	ImportConflictUpdate = "update_existing"
)

type ExportRequest struct {
	Sections       []string  `json:"sections,omitempty"`
	IncludeSecrets bool      `json:"include_secrets,omitempty"`
	Now            time.Time `json:"-"`
}

type ExportManifest struct {
	Format   string                  `json:"format"`
	Service  string                  `json:"service"`
	Sections []ExportManifestSection `json:"sections"`
}

type ExportManifestSection struct {
	Key            string `json:"key"`
	Title          string `json:"title"`
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"default_enabled"`
}

type ExportPackage struct {
	Format    string           `json:"format"`
	Service   string           `json:"service"`
	CreatedAt time.Time        `json:"created_at"`
	Groups    []ExportGroup    `json:"groups"`
	Sequences []ExportSequence `json:"sequences,omitempty"`
}

type ExportGroup struct {
	Key                string                    `json:"key"`
	Position           int32                     `json:"position"`
	IsActive           bool                      `json:"is_active"`
	Localization       map[string]ExportText     `json:"localization,omitempty"`
	Tasks              []ExportTask              `json:"tasks,omitempty"`
	PartnerConfigs     []ExportPartnerConfig     `json:"partner_configs,omitempty"`
	PartnerRewardRules []ExportPartnerRewardRule `json:"partner_reward_rules,omitempty"`
}

type ExportText struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ExportSequence struct {
	Key      string `json:"key"`
	Position int32  `json:"position"`
	IsActive bool   `json:"is_active"`
}

type ExportTask struct {
	Key              string                `json:"key"`
	SequenceKey      *string               `json:"sequence_key,omitempty"`
	SequencePosition *uint32               `json:"sequence_position,omitempty"`
	TaskKind         string                `json:"task_kind"`
	ActionKey        string                `json:"action_key"`
	ActionKind       string                `json:"action_kind"`
	ClaimMode        string                `json:"claim_mode"`
	StartMode        string                `json:"start_mode"`
	TargetCount      uint64                `json:"target_count"`
	Reset            ExportReset           `json:"reset"`
	Position         int32                 `json:"position"`
	Payload          json.RawMessage       `json:"payload,omitempty"`
	Target           json.RawMessage       `json:"target,omitempty"`
	Integration      ExportIntegration     `json:"integration"`
	ImageURL         *string               `json:"image_url,omitempty"`
	IsVisible        bool                  `json:"is_visible"`
	IsActive         bool                  `json:"is_active"`
	StartAt          *time.Time            `json:"start_at,omitempty"`
	EndAt            *time.Time            `json:"end_at,omitempty"`
	Localization     map[string]ExportText `json:"localization,omitempty"`
	Rewards          []ExportReward        `json:"rewards,omitempty"`
	Conditions       []ExportCondition     `json:"conditions,omitempty"`
}

type ExportCondition struct {
	TaskKey        string `json:"task_key"`
	RequiredStatus string `json:"required_status"`
	Position       int32  `json:"position"`
	IsRequired     bool   `json:"is_required"`
}

type ExportReset struct {
	Unit  string `json:"unit"`
	Every uint32 `json:"every"`
}

type ExportIntegration struct {
	Kind     *string         `json:"kind,omitempty"`
	Provider *string         `json:"provider,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
}

type ExportReward struct {
	Key      string  `json:"key"`
	Type     string  `json:"type"`
	Quantity int64   `json:"quantity"`
	Scale    uint16  `json:"scale"`
	Unit     *string `json:"unit,omitempty"`
	Position int32   `json:"position"`
}

type ExportPartnerConfig struct {
	Provider      string          `json:"provider"`
	Platform      string          `json:"platform"`
	IsEnabled     bool            `json:"is_enabled"`
	Secret        *ExportSecret   `json:"secret,omitempty"`
	WebhookSecret *ExportSecret   `json:"webhook_secret,omitempty"`
	Target        json.RawMessage `json:"target,omitempty"`
	Settings      json.RawMessage `json:"settings,omitempty"`
}

type ExportSecret struct {
	Mode  string  `json:"mode"`
	Key   string  `json:"key"`
	Value *string `json:"value,omitempty"`
}

type ExportPartnerRewardRule struct {
	Provider     string       `json:"provider"`
	ExternalType string       `json:"external_type"`
	Reward       ExportReward `json:"reward"`
	Position     int32        `json:"position"`
	IsEnabled    bool         `json:"is_enabled"`
}

type ImportRequest struct {
	Package          ExportPackage     `json:"package"`
	ConflictStrategy string            `json:"conflict_strategy"`
	Secrets          map[string]string `json:"secrets,omitempty"`
}

type ImportPreview struct {
	Format          string           `json:"format"`
	Service         string           `json:"service"`
	Counts          ImportCounts     `json:"counts"`
	Conflicts       []ImportConflict `json:"conflicts,omitempty"`
	Warnings        []string         `json:"warnings,omitempty"`
	RequiredSecrets []ExportSecret   `json:"required_secrets,omitempty"`
}

type ImportCounts struct {
	Groups             int `json:"groups"`
	Sequences          int `json:"sequences"`
	Tasks              int `json:"tasks"`
	Conditions         int `json:"conditions"`
	TaskLocalizations  int `json:"task_localizations"`
	GroupLocalizations int `json:"group_localizations"`
	Rewards            int `json:"rewards"`
	PartnerConfigs     int `json:"partner_configs"`
	PartnerRewards     int `json:"partner_rewards"`
}

type ImportConflict struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

type ImportResult struct {
	Imported ImportCounts `json:"imported"`
	Skipped  ImportCounts `json:"skipped"`
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

type Task struct {
	ID                  uint64
	WorkspaceID         string
	Key                 string
	GroupKey            string
	GroupTitle          string
	GroupDesc           string
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
	DeletedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Localization        *Localization
	Rewards             []Reward
	Progress            *Progress
	Conditions          []ActiveTask
}

type ActiveTask struct {
	ID          uint64          `json:"id"`
	Key         string          `json:"key"`
	GroupKey    string          `json:"group_key"`
	GroupTitle  string          `json:"-"                     msgpack:"group_title"`
	GroupDesc   string          `json:"-"                     msgpack:"group_description"`
	TaskKind    string          `json:"task_kind"`
	ActionKey   string          `json:"action_key"`
	ActionKind  string          `json:"action_kind"`
	ClaimMode   string          `json:"claim_mode"`
	StartMode   string          `json:"start_mode"`
	TargetCount uint64          `json:"target_count"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	ImageURL    *string         `json:"image_url,omitempty"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Rewards     []Reward        `json:"rewards"`
	Progress    *ActiveProgress `json:"progress,omitempty"`
	Conditions  []ActiveTask    `json:"conditions,omitempty"`
	StartAt     *time.Time      `json:"-"                     msgpack:"start_at"`
	EndAt       *time.Time      `json:"-"                     msgpack:"end_at"`
	Target      json.RawMessage `json:"-"                     msgpack:"target"`
}

type ComplexCondition struct {
	WorkspaceID     string
	ParentTaskID    uint64
	ConditionTaskID uint64
	RequiredStatus  string
	Position        int32
	IsRequired      bool
	TaskKey         string
}

type SaveComplexConditionParams struct {
	WorkspaceID     string
	ParentTaskID    uint64
	ConditionTaskID uint64
	RequiredStatus  string
	Position        int32
	IsRequired      bool
}

type ActiveProgress struct {
	Progress      uint64     `json:"progress"`
	Status        string     `json:"status"`
	PeriodStartAt time.Time  `json:"period_start_at"`
	PeriodEndAt   time.Time  `json:"period_end_at"`
	ReadyAt       *time.Time `json:"ready_at,omitempty"`
	ClaimedAt     *time.Time `json:"claimed_at,omitempty"`
}

type Localization struct {
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

type Progress struct {
	ID            uint64
	Progress      uint64
	Status        string
	PeriodStartAt time.Time
	PeriodEndAt   time.Time
	ReadyAt       *time.Time
	ClaimedAt     *time.Time
	OperationID   *string
	Rewards       []Reward
}

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

type RecordParams struct {
	Identity         Identity
	ActionKey        string
	Amount           uint64
	Source           string
	ExternalEventKey string
	Payload          json.RawMessage
	Now              time.Time
}

type RecordResult struct {
	Status    string
	Consumed  uint64
	Remaining uint64
	Tasks     []TaskResult
}

type MarkIntegrationTaskReadyParams struct {
	Identity         Identity
	Task             Task
	Source           string
	ExternalEventKey string
	Payload          json.RawMessage
	Now              time.Time
}

type MarkIntegrationTaskReadyResult struct {
	Status string
	Task   Task
}

type TaskResult struct {
	Task     Task
	Before   uint64
	After    uint64
	Consumed uint64
	Claimed  bool
}

type ClaimParams struct {
	Identity    Identity
	TaskRef     string
	OperationID string
	Now         time.Time
}

type ClaimResult struct {
	Status string
	Task   *Task
}

type StartTaskParams struct {
	Identity Identity
	TaskRef  string
	Now      time.Time
}

type StartTaskResult struct {
	Status string
	Task   *Task
}

type PartnerConfig struct {
	WorkspaceID   string
	Provider      string
	GroupKey      string
	Platform      string
	IsEnabled     bool
	Secret        *string
	WebhookSecret *string
	Target        json.RawMessage
	Settings      json.RawMessage
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type SavePartnerConfigParams struct {
	WorkspaceID   string
	Provider      string
	GroupKey      string
	Platform      string
	IsEnabled     bool
	Secret        *string
	WebhookSecret *string
	Target        json.RawMessage
	Settings      json.RawMessage
}

type PartnerScript struct {
	Provider  string
	IsEnabled bool
	Version   string
	Source    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SavePartnerScriptParams struct {
	Provider  string
	IsEnabled bool
	Version   string
	Source    string
}

type PartnerRewardRule struct {
	WorkspaceID  string
	Provider     string
	GroupKey     string
	ExternalType string
	Reward       Reward
	Position     int32
	IsEnabled    bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SavePartnerRewardRuleParams struct {
	WorkspaceID  string
	Provider     string
	GroupKey     string
	ExternalType string
	Reward       Reward
	Position     int32
	IsEnabled    bool
}

type PartnerIssue struct {
	ID              uint64
	WorkspaceID     string
	Provider        string
	GroupKey        string
	Platform        string
	ExternalID      string
	ExternalType    string
	ExternalClickID *string
	StartMode       string
	IssueKey        string
	AppID           int64
	PlatformID      int64
	PlatformUserID  string
	PublicPayload   json.RawMessage
	PrivatePayload  json.RawMessage
	Rewards         []Reward
	Status          string
	IssuedAt        time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	ClaimedAt       *time.Time
	ExpiresAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type PartnerIssueScope struct {
	WorkspaceID string
	Provider    string
	GroupKey    string
	Platform    string
}

func (s PartnerIssueScope) matches(issue PartnerIssue) bool {
	return s.WorkspaceID == issue.WorkspaceID &&
		s.Provider == issue.Provider &&
		s.GroupKey == issue.GroupKey &&
		s.Platform == issue.Platform
}

type CreatePartnerIssueParams struct {
	Identity        Identity
	Provider        string
	GroupKey        string
	Platform        string
	ExternalID      string
	ExternalType    string
	ExternalClickID *string
	StartMode       string
	IssueKey        string
	PublicPayload   json.RawMessage
	PrivatePayload  json.RawMessage
	Rewards         []Reward
	ExpiresAt       *time.Time
	Now             time.Time
}

type PartnerClaimResult struct {
	Status      string
	Issue       PartnerIssue
	Rewards     []Reward
	OperationID string
}

type PartnerStatsDaily struct {
	Date                   time.Time
	Provider               string
	GroupKey               string
	ExternalType           string
	IssuedCount            uint64
	CompletedCount         uint64
	ClaimedCount           uint64
	RevokedCount           uint64
	RevokedAfterClaimCount uint64
	FailedCount            uint64
	FakeCount              uint64
	ExpiredCount           uint64
	UniqueIssuedUsers      uint64
	UniqueCompletedUsers   uint64
	UniqueClaimers         uint64
}
