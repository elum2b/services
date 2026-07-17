package apiflow

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	StrategyRoundRobin = "round_robin"
	StrategyRandom     = "random"

	DefaultRatePerSecond = 30
)

var ErrNoTokens = errors.New("apiflow: no tokens")

type Options struct {
	RatePerSecond int
}

type TokenSet struct {
	Tokens   []string
	Strategy string
}

type Flow struct {
	interval time.Duration

	mu       sync.Mutex
	sets     map[string]*atomic.Uint64
	limiters map[string]*tokenLimiter
}

type tokenLimiter struct {
	mu   sync.Mutex
	next time.Time
}

func New(options Options) *Flow {
	rate := options.RatePerSecond
	if rate <= 0 {
		rate = DefaultRatePerSecond
	}
	return &Flow{
		interval: time.Second / time.Duration(rate),
		sets:     make(map[string]*atomic.Uint64),
		limiters: make(map[string]*tokenLimiter),
	}
}

func (f *Flow) Acquire(ctx context.Context, set TokenSet) (string, error) {
	tokens := normalizeTokens(set.Tokens)
	if len(tokens) == 0 {
		return "", ErrNoTokens
	}
	if f == nil {
		f = New(Options{})
	}
	token := f.selectToken(tokens, set.Strategy)
	if err := f.wait(ctx, token); err != nil {
		return "", err
	}
	return token, nil
}

func (f *Flow) selectToken(tokens []string, strategy string) string {
	if len(tokens) == 1 {
		return tokens[0]
	}
	switch normalizeStrategy(strategy) {
	case StrategyRandom:
		return tokens[rand.IntN(len(tokens))]
	default:
		counter := f.counter(tokens)
		index := counter.Add(1) - 1
		return tokens[index%uint64(len(tokens))]
	}
}

func (f *Flow) wait(ctx context.Context, token string) error {
	limiter := f.limiter(token)
	delay := limiter.reserve(f.interval)
	if delay <= 0 {
		return nil
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

func (f *Flow) counter(tokens []string) *atomic.Uint64 {
	key := strings.Join(tokens, "\x00")
	f.mu.Lock()
	defer f.mu.Unlock()
	counter := f.sets[key]
	if counter == nil {
		counter = &atomic.Uint64{}
		f.sets[key] = counter
	}
	return counter
}

func (f *Flow) limiter(token string) *tokenLimiter {
	f.mu.Lock()
	defer f.mu.Unlock()
	limiter := f.limiters[token]
	if limiter == nil {
		limiter = &tokenLimiter{}
		f.limiters[token] = limiter
	}
	return limiter
}

func (l *tokenLimiter) reserve(interval time.Duration) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.next.IsZero() || !l.next.After(now) {
		l.next = now.Add(interval)
		return 0
	}
	delay := l.next.Sub(now)
	l.next = l.next.Add(interval)
	return delay
}

func normalizeTokens(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func normalizeStrategy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "random", "rand":
		return StrategyRandom
	default:
		return StrategyRoundRobin
	}
}
