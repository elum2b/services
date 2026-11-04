package collections

import (
	"context"
	"time"

	"github.com/elum2b/services/collections/model"

	serviceerrors "github.com/elum2b/services/errors"
	callbackutil "github.com/elum2b/services/internal/utils/callback"

	json "github.com/goccy/go-json"
)

const CallbackEventOwnershipChanged = "collections.ownership_changed"

type CallbackPayload = model.ChangeEvent

type Context struct {
	callbackutil.Context
	Ownership *CallbackPayload
}

type CallbackHandler func(Context) error
type CallbackOption = callbackutil.Option
type callbackRegistration struct {
	ctx     context.Context
	handler CallbackHandler
	options []CallbackOption
}

func WithCallbackWorkerID(
	value string,
) CallbackOption {
	return callbackutil.WithWorkerID(value)
}

func WithCallbackBatchSize(
	value int32,
) CallbackOption {
	return callbackutil.WithBatchSize(value)
}
func WithCallbackLeaseTimeout(value time.Duration) CallbackOption {
	return callbackutil.WithLeaseTimeout(value)
}
func WithCallbackIdleDelay(value time.Duration) CallbackOption {
	return callbackutil.WithIdleDelay(value)
}

func (p *Collections) OnCallback(
	ctx context.Context,
	handler CallbackHandler,
	opts ...CallbackOption,
) error {
	if handler == nil {
		return ErrCallbackHandlerNil
	}

	if p == nil {
		return ErrServiceNil
	}

	p.lifecycleMu.Lock()

	if p.closed {
		p.lifecycleMu.Unlock()
		return ErrServiceClosed
	}

	if p.running {
		p.lifecycleMu.Unlock()

		return ErrCallbacksRegistrationClosed
	}

	if p.callbacks != nil && !p.client.IsUnavailable() {
		p.callbackRuns.Add(1)
		p.lifecycleMu.Unlock()
		defer p.callbackRuns.Done()
		return p.runCallback(ctx, handler, opts...)
	}

	p.callbacksToRun = append(p.callbacksToRun, callbackRegistration{
		ctx:     ctx,
		handler: handler,
		options: append([]CallbackOption(nil), opts...),
	})
	p.lifecycleMu.Unlock()

	return nil
}

func (p *Collections) runCallback(
	ctx context.Context,
	handler CallbackHandler,
	opts ...CallbackOption,
) error {
	if p == nil {
		return ErrCallbacksNotConfigured
	}
	p.lifecycleMu.Lock()
	callbacks := p.callbacks
	p.lifecycleMu.Unlock()
	if callbacks == nil {
		return ErrCallbacksNotConfigured
	}

	runCtx, cancel := p.bindContext(ctx)

	defer cancel()

	opts = append(opts, callbackutil.WithSourceService("collections"))

	return callbacks.On(runCtx, func(callbackCtx callbackutil.Context) error {
		var payload CallbackPayload

		if err := json.Unmarshal(callbackCtx.Payload, &payload); err != nil {
			return serviceerrors.Wrap(
				serviceerrors.CodeInternalError,
				"collections callback payload decode failed",
				err,
			)
		}

		return handler(Context{
			Context:   callbackCtx,
			Ownership: &payload,
		})
	}, opts...)
}
