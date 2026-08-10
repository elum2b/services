package goroutine

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const defaultRestartDelay = time.Second

type Manager struct {
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed atomic.Bool
	ctx    context.Context
	cancel context.CancelFunc
}

func New() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, cancel: cancel}
}

func (m *Manager) Go(name string, fn func()) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		return false
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer recoverAndLog(name)
		fn()
	}()
	return true
}

func (m *Manager) GoRestart(
	ctx context.Context,
	name string,
	delay time.Duration,
	fn func(),
) bool {
	if m == nil {
		return false
	}
	if delay <= 0 {
		delay = defaultRestartDelay
	}
	if ctx == nil {
		ctx = context.Background()
	}
	restartCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	started := m.Go(name, func() {
		defer stop()
		defer cancel()
		for {
			if restartCtx.Err() != nil {
				return
			}
			panicked := runRecovering(name, fn)
			if !panicked {
				return
			}
			if !waitContext(restartCtx, delay) {
				return
			}
		}
	})
	if !started {
		stop()
		cancel()
	}
	return started
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed.Store(true)
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func runRecovering(name string, fn func()) (panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("goroutine %s recovered from panic: %v", name, recovered)
			panicked = true
		}
	}()
	fn()
	return false
}

func recoverAndLog(name string) {
	if recovered := recover(); recovered != nil {
		log.Printf("goroutine %s recovered from panic: %v", name, recovered)
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	if ctx == nil {
		time.Sleep(delay)
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
