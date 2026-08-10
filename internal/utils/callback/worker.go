package callback

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	serviceerrors "github.com/elum2b/services/errors"
)

const (
	defaultWorkerID      = "callback-worker"
	defaultBatchSize     = int32(10)
	maxRouteConcurrency  = int32(4)
	maxWorkerConcurrency = int32(256)
	defaultLeaseTimeout  = time.Minute
	defaultIdleDelay     = time.Second
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

	// Lease one event at a time so the scheduler can reserve its route before
	// another event for the same route is considered. WithBatchSize remains a
	// compatibility option, but route-aware leasing deliberately caps it at one.
	leaseLimit := options.batchSize
	if leaseLimit <= 0 || leaseLimit > 1 {
		leaseLimit = 1
	}

	scheduler := newRouteScheduler()
	completions := make(chan callbackCompletion, maxWorkerConcurrency)

	var waitGroup sync.WaitGroup

	defer waitGroup.Wait()

	for {
		if err := drainCallbackCompletions(scheduler, completions); err != nil {
			return err
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		if scheduler.total >= maxWorkerConcurrency {
			completion := <-completions
			scheduler.release(completion.route)

			if completion.err != nil {
				return completion.err
			}

			continue
		}

		events, err := s.LeaseEvents(ctx, LeaseParams{
			SourceService:       options.sourceService,
			WorkerID:            options.workerID,
			Limit:               leaseLimit,
			LeaseTimeout:        options.leaseTimeout,
			ExcludedRoutingKeys: scheduler.saturatedRoutes(),
		})
		if err != nil {
			return err
		}

		if len(events) == 0 {
			if err := waitForCallbackWork(
				ctx,
				options.idleDelay,
				scheduler,
				completions,
			); err != nil {
				return err
			}

			continue
		}

		for _, event := range events {
			route := callbackRouteKey(event)
			if !scheduler.acquire(route) {
				return errors.New(
					"callback: leased an event for a saturated route",
				)
			}

			waitGroup.Add(1)

			go func() {
				defer waitGroup.Done()

				completions <- callbackCompletion{
					route: route,
					err: s.handleEvent(
						ctx,
						event,
						options.workerID,
						handler,
					),
				}
			}()
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

func (s *Store) handleEvent(
	ctx context.Context,
	event storedEvent,
	workerID string,
	handler Handler,
) error {
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
			Error:    serviceerrors.PublicMessage(err),
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

func callbackRouteKey(event storedEvent) string {
	if event.RoutingKey != "" {
		return event.RoutingKey
	}

	return event.WorkspaceID
}

type callbackCompletion struct {
	route string
	err   error
}

type routeScheduler struct {
	active map[string]int32
	total  int32
}

func newRouteScheduler() *routeScheduler {
	return &routeScheduler{active: make(map[string]int32)}
}

func (s *routeScheduler) acquire(route string) bool {
	if s.active[route] >= maxRouteConcurrency ||
		s.total >= maxWorkerConcurrency {
		return false
	}

	s.active[route]++

	s.total++

	return true
}

func (s *routeScheduler) release(route string) {
	if s.active[route] <= 0 {
		return
	}

	s.active[route]--

	s.total--

	if s.active[route] == 0 {
		delete(s.active, route)
	}
}

func (s *routeScheduler) saturatedRoutes() []string {
	routes := make([]string, 0, len(s.active))

	for route, active := range s.active {
		if active >= maxRouteConcurrency {
			routes = append(routes, route)
		}
	}

	sort.Strings(routes)

	return routes
}

func drainCallbackCompletions(
	scheduler *routeScheduler,
	completions <-chan callbackCompletion,
) error {
	for {
		select {
		case completion := <-completions:
			scheduler.release(completion.route)

			if completion.err != nil {
				return completion.err
			}
		default:
			return nil
		}
	}
}

func waitForCallbackWork(
	ctx context.Context,
	delay time.Duration,
	scheduler *routeScheduler,
	completions <-chan callbackCompletion,
) error {
	if delay <= 0 {
		delay = defaultIdleDelay
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case completion := <-completions:
		scheduler.release(completion.route)

		return completion.err
	case <-timer.C:
		return nil
	}
}
