package runtime

import (
	"context"
	"net/http"
	"time"
)

type Options struct {
	Enabled          bool
	ScriptLoader     ScriptLoader
	ScriptCacheTTL   time.Duration
	Timeout          time.Duration
	MaxMemory        int
	MaxHTTPRequests  int
	MaxResponseBytes int64
	HTTPClient       *http.Client
	JSONBoundary     bool
	StatePoolSize    int // 0 uses the default pool, negative disables pooling.
}

type Script struct {
	Provider string
	Source   string
	Version  string
}

type ScriptLoader func(ctx context.Context, provider string) (Script, bool, error)

type Event struct {
	Action    string         `json:"action"`
	Provider  string         `json:"provider,omitempty"`
	Identity  map[string]any `json:"identity,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
	Issue     map[string]any `json:"issue,omitempty"`
	Request   map[string]any `json:"request,omitempty"`
	Variables map[string]any `json:"variables,omitempty"`
	Locale    string         `json:"locale,omitempty"`
	Limit     int32          `json:"limit,omitempty"`
	Now       time.Time      `json:"now,omitempty"`
}

type Result map[string]any

type Stats struct {
	Providers       int
	CompiledScripts int
	StatePools      int
}
