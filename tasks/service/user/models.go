package user

import (
	"context"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/tasks/repository"
)

type Identity = services.Identity

type RewardModel = repository.Reward
type ProgressModel = repository.ActiveProgress
type TaskModel = repository.ActiveTask

type TaskGroupModel struct {
	Key         string      `json:"key"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Tasks       []TaskModel `json:"tasks"`
}

type ListActiveParams struct {
	Identity Identity
	Locale   string
	GroupKey string
	Now      time.Time
}

type ClaimParams struct {
	Identity    Identity
	TaskRef     string
	OperationID string
	Now         time.Time
}

type StartTaskParams struct {
	Identity Identity
	TaskRef  string
	Now      time.Time
}

type StartTaskResult struct {
	Status  string     `json:"status"`
	Started bool       `json:"started"`
	Task    *TaskModel `json:"task,omitempty"`
}

type ClaimResult struct {
	Status string     `json:"status"`
	Task   *TaskModel `json:"task,omitempty"`
}

type PartnerProvider interface {
	ListPartnerTasks(ctx context.Context, params PartnerListProviderParams) ([]PartnerExternalTask, error)
	CheckPartnerTask(ctx context.Context, params PartnerCheckProviderParams) (PartnerCheckResult, error)
}

type PartnerStarter interface {
	StartPartnerTask(ctx context.Context, params PartnerStartProviderParams) (PartnerStartResult, error)
}

type PartnerListParams struct {
	Identity  Identity
	Provider  string
	GroupKey  string
	Platform  string
	Locale    string
	Limit     int32
	Variables map[string]string
	Now       time.Time
}

type PartnerCheckParams struct {
	Identity  Identity
	IssueRef  string
	Variables map[string]string
	Now       time.Time
}

type PartnerStartParams struct {
	Identity  Identity
	IssueRef  string
	Variables map[string]string
	Now       time.Time
}

type PartnerListProviderParams struct {
	Identity  Identity
	Config    repository.PartnerConfig
	Locale    string
	Limit     int32
	Variables map[string]string
	Now       time.Time
}

type PartnerCheckProviderParams struct {
	Identity  Identity
	Config    repository.PartnerConfig
	Issue     repository.PartnerIssue
	Variables map[string]string
	Now       time.Time
}

type PartnerStartProviderParams struct {
	Identity  Identity
	Config    repository.PartnerConfig
	Issue     repository.PartnerIssue
	Variables map[string]string
	Now       time.Time
}

type PartnerExternalTask struct {
	ExternalID     string
	ExternalType   string
	PublicPayload  json.RawMessage
	PrivatePayload json.RawMessage
	ExpiresAt      *time.Time
	StartMode      string
	WindowKey      string
}

type PartnerCheckResult struct {
	Completed bool
	Status    string
	Payload   json.RawMessage
}

type PartnerStartResult struct {
	Started             bool
	Status              string
	ActionURL           string
	ExternalClickID     string
	PublicPayloadPatch  json.RawMessage
	PrivatePayloadPatch json.RawMessage
	Payload             json.RawMessage
}

type PartnerCheckOutput struct {
	Status    string     `json:"status"`
	Completed bool       `json:"completed"`
	Task      *TaskModel `json:"task,omitempty"`
}

type PartnerStartOutput struct {
	Status    string     `json:"status"`
	Started   bool       `json:"started"`
	ActionURL string     `json:"action_url,omitempty"`
	Task      *TaskModel `json:"task,omitempty"`
}
