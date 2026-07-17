package integration

import (
	"context"
	json "github.com/goccy/go-json"
	"time"

	"github.com/elum2b/services/tasks/repository"
)

const (
	StatusClaimed       = repository.ClaimStatusClaimed
	StatusAlreadyDone   = repository.ClaimStatusAlreadyDone
	StatusNotReady      = repository.ClaimStatusNotReady
	StatusNotFound      = repository.ClaimStatusNotFound
	StatusNotCompleted  = "not_completed"
	StatusNoChecker     = "no_checker"
	StatusInvalidTask   = "invalid_task"
	StatusCheckRejected = "check_rejected"
)

type Identity = repository.Identity
type TaskModel = repository.ActiveTask

type CheckResult struct {
	Completed bool            `json:"completed"`
	Reason    string          `json:"reason,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type Result struct {
	Status    string          `json:"status"`
	Completed bool            `json:"completed"`
	Task      *TaskModel      `json:"task,omitempty"`
	Check     json.RawMessage `json:"check,omitempty"`
}

type ConfirmCompletionResult struct {
	Status      string     `json:"status"`
	Completed   bool       `json:"completed"`
	TaskID      uint64     `json:"task_id,omitempty"`
	TaskKey     string     `json:"task_key,omitempty"`
	ClaimedAt   *time.Time `json:"claimed_at,omitempty"`
	OperationID *string    `json:"operation_id,omitempty"`
}

type TaskRefParams struct {
	Identity Identity
	TaskRef  string
	Now      time.Time
}

type CheckParams struct {
	TaskRefParams
	Provider  string
	Variables map[string]string
}

type CheckChannelSubscriptionParams struct {
	TaskRefParams
	Provider  string
	Variables map[string]string
}

type CheckChannelBoostParams struct {
	TaskRefParams
	Provider  string
	Variables map[string]string
}

type CheckExternalParams struct {
	TaskRefParams
	Provider  string
	Variables map[string]string
}

type ConfirmCompletionParams struct {
	TaskRefParams
}

type ChannelSubscriptionCheckParams struct {
	Identity   Identity
	Task       TaskContext
	Provider   string
	Variables  map[string]string
	OccurredAt time.Time
}

type ChannelBoostCheckParams struct {
	Identity   Identity
	Task       TaskContext
	Provider   string
	Variables  map[string]string
	OccurredAt time.Time
}

type ExternalTaskCheckParams struct {
	Identity   Identity
	Task       TaskContext
	Provider   string
	Variables  map[string]string
	OccurredAt time.Time
}

type TaskContext struct {
	ID                  uint64
	Key                 string
	TaskKind            string
	ActionKey           string
	ActionKind          string
	Payload             json.RawMessage
	IntegrationKind     *string
	IntegrationProvider *string
	IntegrationPayload  json.RawMessage
}

type ChannelSubscriptionChecker interface {
	CheckChannelSubscription(ctx context.Context, params ChannelSubscriptionCheckParams) (CheckResult, error)
}

type ChannelBoostChecker interface {
	CheckChannelBoost(ctx context.Context, params ChannelBoostCheckParams) (CheckResult, error)
}

type ExternalTaskChecker interface {
	CheckExternalTask(ctx context.Context, params ExternalTaskCheckParams) (CheckResult, error)
}
