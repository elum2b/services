package callback

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultWorkerID     = "callback-worker"
	defaultBatchSize    = int32(10)
	defaultLeaseTimeout = time.Minute
	defaultIdleDelay    = time.Second
)

var (
	ErrAlreadyMarked      = errors.New("callback: event already marked")
	ErrStoreNotConfigured = errors.New("callback: store is not configured")
)

type Handler func(Context) error

type Option func(*options)

type options struct {
	sourceService string
	workerID      string
	batchSize     int32
	leaseTimeout  time.Duration
	idleDelay     time.Duration
}

type Context struct {
	context.Context

	EventID            uint64
	EventType          string
	EventKey           string
	IdempotencyKey     string
	Payload            []byte
	PayloadContentType string
	Attempt            uint32
	CreatedAt          time.Time

	store    *Store
	workerID string
	marked   *bool
}

func WithWorkerID(workerID string) Option {
	return func(options *options) {
		options.workerID = workerID
	}
}

func WithSourceService(sourceService string) Option {
	return func(options *options) {
		options.sourceService = sourceService
	}
}

func WithBatchSize(batchSize int32) Option {
	return func(options *options) {
		options.batchSize = batchSize
	}
}

func WithLeaseTimeout(timeout time.Duration) Option {
	return func(options *options) {
		options.leaseTimeout = timeout
	}
}

func WithIdleDelay(delay time.Duration) Option {
	return func(options *options) {
		options.idleDelay = delay
	}
}

func (s *Store) On(ctx context.Context, handler Handler, opts ...Option) error {
	if s == nil {
		return ErrStoreNotConfigured
	}
	if handler == nil {
		return errors.New("callback: handler is nil")
	}
	options := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := s.LeaseEvents(ctx, LeaseParams{
			SourceService: options.sourceService,
			WorkerID:      options.workerID,
			Limit:         options.batchSize,
			LeaseTimeout:  options.leaseTimeout,
		})
		if err != nil {
			return err
		}
		if len(events) == 0 {
			if err := sleepContext(ctx, options.idleDelay); err != nil {
				return err
			}
			continue
		}
		for _, event := range events {
			if err := s.handleEvent(ctx, event, options.workerID, handler); err != nil {
				return err
			}
		}
	}
}

func (ctx Context) Successful() error {
	if err := ctx.mark(); err != nil {
		return err
	}
	return ctx.store.MarkOK(ctx.Context, ctx.EventID, ctx.workerID)
}

func (ctx Context) Failed() error {
	return ctx.FailedWithError("")
}

func (ctx Context) FailedWithError(message string) error {
	if err := ctx.mark(); err != nil {
		return err
	}
	return ctx.store.MarkFailed(ctx.Context, FailParams{
		ID:       ctx.EventID,
		WorkerID: ctx.workerID,
		Error:    message,
		Attempt:  ctx.Attempt,
	})
}

func (ctx Context) Canceled() error {
	return ctx.CanceledWithReason("")
}

func (ctx Context) CanceledWithReason(reason string) error {
	if err := ctx.mark(); err != nil {
		return err
	}
	return ctx.store.MarkReject(ctx.Context, ctx.EventID, ctx.workerID, reason)
}

func (ctx Context) String() string {
	return fmt.Sprintf("callback event %d %s", ctx.EventID, ctx.EventType)
}

func (ctx Context) mark() error {
	if ctx.marked == nil {
		return errors.New("callback: context is not initialized")
	}
	if *ctx.marked {
		return ErrAlreadyMarked
	}
	*ctx.marked = true
	return nil
}

func (s *Store) handleEvent(ctx context.Context, event storedEvent, workerID string, handler Handler) error {
	marked := false
	callbackCtx := Context{
		Context:            ctx,
		EventID:            uint64(event.ID),
		EventType:          event.EventType,
		EventKey:           event.EventKey,
		IdempotencyKey:     event.IdempotencyKey,
		Payload:            event.Payload,
		PayloadContentType: event.PayloadContentType,
		Attempt:            uint32(event.AttemptCount),
		CreatedAt:          event.CreatedAt,
		store:              s,
		workerID:           workerID,
		marked:             &marked,
	}
	err := handler(callbackCtx)
	if marked {
		return err
	}
	if err != nil {
		return s.MarkFailed(ctx, FailParams{
			ID:       uint64(event.ID),
			WorkerID: workerID,
			Error:    err.Error(),
			Attempt:  uint32(event.AttemptCount),
		})
	}
	return s.MarkFailed(ctx, FailParams{
		ID:       uint64(event.ID),
		WorkerID: workerID,
		Error:    "callback handler returned without marking event",
		Attempt:  uint32(event.AttemptCount),
	})
}

func defaultOptions() options {
	return options{
		workerID:     defaultWorkerID,
		batchSize:    defaultBatchSize,
		leaseTimeout: defaultLeaseTimeout,
		idleDelay:    defaultIdleDelay,
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = defaultIdleDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
