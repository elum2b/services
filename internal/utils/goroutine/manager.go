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
}

func New() *Manager {
	return &Manager{}
}

func (m *Manager) Go(name string, fn func()) bool {
	if m == nil {
		m = New()
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

func (m *Manager) GoRestart(ctx context.Context, name string, delay time.Duration, fn func()) bool {
	if m == nil {
		m = New()
	}
	if delay <= 0 {
		delay = defaultRestartDelay
	}
	return m.Go(name, func() {
		for {
			if ctx != nil && ctx.Err() != nil {
				return
			}
			panicked := runRecovering(name, fn)
			if !panicked {
				return
			}
			if !waitContext(ctx, delay) {
				return
			}
		}
	})
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed.Store(true)
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
