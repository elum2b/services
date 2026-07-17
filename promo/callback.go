package promo

import (
	"context"
	json "github.com/goccy/go-json"
	"time"

	services "github.com/elum2b/services"
	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"
)

const CallbackEventApplied = "promo.applied"

type CallbackReward = services.Reward

type CallbackPayload struct {
	RedemptionID   uint64           `json:"redemption_id"`
	WorkspaceID    string           `json:"workspace_id"`
	PromoID        uint64           `json:"promo_id"`
	Code           string           `json:"code"`
	AppID          int64            `json:"app_id"`
	PlatformID     int64            `json:"platform_id"`
	PlatformUserID string           `json:"platform_user_id"`
	Rewards        []CallbackReward `json:"rewards"`
}

type Context struct {
	callbackutil.Context
	Payload *services.RewardPayload
	Applied *CallbackPayload
}

type CallbackHandler func(Context) error
type CallbackOption = callbackutil.Option
type callbackRegistration struct {
	ctx     context.Context
	handler CallbackHandler
	options []CallbackOption
}

func WithCallbackWorkerID(value string) CallbackOption { return callbackutil.WithWorkerID(value) }
func WithCallbackBatchSize(value int32) CallbackOption { return callbackutil.WithBatchSize(value) }
func WithCallbackLeaseTimeout(value time.Duration) CallbackOption {
	return callbackutil.WithLeaseTimeout(value)
}
func WithCallbackIdleDelay(value time.Duration) CallbackOption {
	return callbackutil.WithIdleDelay(value)
}

func (p *Promo) OnCallback(ctx context.Context, handler CallbackHandler, opts ...CallbackOption) error {
	if handler == nil {
		return ErrCallbackHandlerNil
	}
	if p == nil {
		return ErrServiceNil
	}
	p.lifecycleMu.Lock()
	if p.running {
		p.lifecycleMu.Unlock()
		return ErrCallbacksRegistrationClosed
	}
	if p.callbacks != nil {
		p.lifecycleMu.Unlock()
		return p.runCallback(ctx, handler, opts...)
	}
	p.callbacksToRun = append(p.callbacksToRun, callbackRegistration{
		ctx: ctx, handler: handler, options: append([]CallbackOption(nil), opts...),
	})
	p.lifecycleMu.Unlock()
	return nil
}

func (p *Promo) runCallback(ctx context.Context, handler CallbackHandler, opts ...CallbackOption) error {
	if p == nil || p.callbacks == nil {
		return ErrCallbacksNotConfigured
	}
	runCtx, cancel := p.bindContext(ctx)
	defer cancel()
	opts = append(opts, callbackutil.WithSourceService("promo"))
	return p.callbacks.On(runCtx, func(callbackCtx callbackutil.Context) error {
		var payload CallbackPayload
		if err := json.Unmarshal(callbackCtx.Payload, &payload); err != nil {
			return serviceerrors.Wrap(serviceerrors.CodeInternalError, "promo callback payload decode failed", err)
		}
		return handler(Context{
			Context: callbackCtx,
			Payload: &services.RewardPayload{
				Identity: services.Identity{
					WorkspaceID: payload.WorkspaceID,
					AppID:       payload.AppID, PlatformID: payload.PlatformID,
					PlatformUserID: payload.PlatformUserID,
				},
				Rewards: payload.Rewards,
			},
			Applied: &payload,
		})
	}, opts...)
}
