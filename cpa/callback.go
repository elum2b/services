package cpa

import (
	"context"
	"time"

	json "github.com/goccy/go-json"

	services "github.com/elum2b/services"
	"github.com/elum2b/services/cpa/model"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

const (
	CallbackEventIssued    = "cpa.issued"
	CallbackEventCompleted = "cpa.completed"
)

type CallbackReward = services.Reward

type CallbackPayload struct {
	AssignmentID   uint64                    `json:"assignment_id"`
	WorkspaceID    string                    `json:"workspace_id"`
	CPAID          string                    `json:"cpa_id"`
	AppID          int64                     `json:"app_id"`
	PlatformID     int64                     `json:"platform_id"`
	PlatformUserID string                    `json:"platform_user_id"`
	Code           string                    `json:"code"`
	CodeMode       string                    `json:"code_mode"`
	Status         model.AssignmentEventType `json:"status"`
	Rewards        []CallbackReward          `json:"rewards,omitempty"`
}

type Context struct {
	callbackutil.Context
	Payload   *services.RewardPayload
	Issued    *CallbackPayload
	Completed *CallbackPayload
}

type CallbackHandler func(Context) error
type CallbackOption = callbackutil.Option
type callbackRegistration struct {
	ctx     context.Context
	handler CallbackHandler
	options []CallbackOption
}

var ErrCallbackAlreadyMarked = callbackutil.ErrAlreadyMarked

func WithCallbackWorkerID(workerID string) CallbackOption {
	return callbackutil.WithWorkerID(workerID)
}

func WithCallbackBatchSize(batchSize int32) CallbackOption {
	return callbackutil.WithBatchSize(batchSize)
}

func WithCallbackLeaseTimeout(timeout time.Duration) CallbackOption {
	return callbackutil.WithLeaseTimeout(timeout)
}

func WithCallbackIdleDelay(delay time.Duration) CallbackOption {
	return callbackutil.WithIdleDelay(delay)
}

func (c *CPA) OnCallback(ctx context.Context, handler CallbackHandler, opts ...CallbackOption) error {
	if handler == nil {
		return ErrCallbackHandlerNil
	}
	if c == nil {
		return ErrServiceNil
	}
	c.lifecycleMu.Lock()
	if c.running {
		c.lifecycleMu.Unlock()
		return ErrCallbacksRegistrationClosed
	}
	if c.callbacks != nil {
		c.lifecycleMu.Unlock()
		return c.runCallback(ctx, handler, opts...)
	}
	c.callbacksToRun = append(c.callbacksToRun, callbackRegistration{
		ctx:     ctx,
		handler: handler,
		options: append([]CallbackOption(nil), opts...),
	})
	c.lifecycleMu.Unlock()
	return nil
}

func (c *CPA) runCallback(ctx context.Context, handler CallbackHandler, opts ...CallbackOption) error {
	if c == nil || c.callbacks == nil {
		return ErrCallbacksNotConfigured
	}
	runCtx, cancel := c.bindContext(ctx)
	defer cancel()
	opts = append(opts, callbackutil.WithSourceService("cpa"))
	return c.callbacks.On(runCtx, func(callbackCtx callbackutil.Context) error {
		value := Context{Context: callbackCtx}
		var payload CallbackPayload
		if err := json.Unmarshal(callbackCtx.Payload, &payload); err != nil {
			return serviceerrors.Wrap(serviceerrors.CodeInternalError, "cpa callback payload decode failed", err)
		}
		value.Payload = &services.RewardPayload{
			Identity: services.Identity{
				WorkspaceID:    payload.WorkspaceID,
				AppID:          payload.AppID,
				PlatformID:     payload.PlatformID,
				PlatformUserID: payload.PlatformUserID,
			},
			Rewards: payload.Rewards,
		}
		switch callbackCtx.EventType {
		case CallbackEventIssued:
			value.Issued = &payload
		case CallbackEventCompleted:
			value.Completed = &payload
		}
		return handler(value)
	}, opts...)
}
