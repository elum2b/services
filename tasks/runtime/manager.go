package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/elum2b/services/internal/utils/contextutil"
	json "github.com/goccy/go-json"
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

const (
	defaultTimeout          = 1500 * time.Millisecond
	defaultScriptCacheTTL   = time.Second
	defaultMaxMemory        = 4 << 20
	defaultMaxHTTPRequests  = 3
	defaultMaxResponseBytes = 1 << 20
	defaultStatePoolSize    = 64
)

var ErrClosed = errors.New("tasks runtime is closed")

var (
	jsonWrapperOnce  sync.Once
	jsonWrapperProto *lua.FunctionProto
	jsonWrapperErr   error
)

type Manager struct {
	options Options

	mu        sync.RWMutex
	cache     map[string]*lua.FunctionProto
	providers map[string]providerState
	pools     map[string]chan *runtimeState
	closed    bool
	active    sync.WaitGroup
	http      *httpClient
	rootCtx   context.Context
	cancel    context.CancelFunc
}

type providerState struct {
	script    Script
	loaded    bool
	checkedAt time.Time
}

func New(ctx context.Context, options Options) *Manager {
	if options.Timeout <= 0 {
		options.Timeout = defaultTimeout
	}
	if options.ScriptCacheTTL <= 0 {
		options.ScriptCacheTTL = defaultScriptCacheTTL
	}
	if options.MaxMemory <= 0 {
		options.MaxMemory = defaultMaxMemory
	}
	if options.MaxHTTPRequests <= 0 {
		options.MaxHTTPRequests = defaultMaxHTTPRequests
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = defaultMaxResponseBytes
	}
	if options.StatePoolSize == 0 {
		options.StatePoolSize = defaultStatePoolSize
	}
	rootCtx, cancel := context.WithCancel(ctx)
	return &Manager{
		options:   options,
		cache:     make(map[string]*lua.FunctionProto),
		providers: make(map[string]providerState),
		pools:     make(map[string]chan *runtimeState),
		http:      &httpClient{client: options.HTTPClient, maxResponseBytes: options.MaxResponseBytes},
		rootCtx:   rootCtx,
		cancel:    cancel,
	}
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.active.Wait()
	m.closePools()
	return nil
}

func (m *Manager) Stats() Stats {
	if m == nil {
		return Stats{}
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return Stats{
		Providers:       len(m.providers),
		CompiledScripts: len(m.cache),
		StatePools:      len(m.pools),
	}
}

func (m *Manager) WarmProviders(ctx context.Context, providers []string) error {
	if m == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		provider = strings.TrimSpace(provider)
		if provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		if err := m.WarmProvider(ctx, provider); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) WarmProvider(ctx context.Context, provider string) error {
	if m == nil {
		return nil
	}
	script, ok, err := m.loadScript(ctx, provider, true)
	if err != nil || !ok {
		return err
	}
	if strings.TrimSpace(script.Source) == "" {
		return nil
	}
	proto, err := m.compile(script)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, m.options.Timeout)
	defer cancel()
	state, pooled, err := m.acquireState(runCtx, script, proto)
	if err != nil {
		return err
	}
	m.releaseState(script, state, pooled)
	return nil
}

func (m *Manager) Handle(ctx context.Context, provider string, event Event) (Result, error) {
	if m == nil {
		return nil, ErrClosed
	}
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, ErrClosed
	}
	m.active.Add(1)
	m.mu.RUnlock()
	defer m.active.Done()

	script, ok, err := m.loadScript(ctx, provider, false)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(script.Source) == "" {
		return nil, fmt.Errorf("tasks runtime script %q not configured", provider)
	}
	if event.Provider == "" {
		event.Provider = provider
	}
	proto, err := m.compile(script)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, m.options.Timeout)
	defer cancel()
	runCtx, runCancel := contextutil.Merge(m.rootCtx, runCtx)
	defer runCancel()
	state, pooled, err := m.acquireState(runCtx, script, proto)
	if err != nil {
		return nil, err
	}
	defer m.releaseState(script, state, pooled)
	out, err := m.call(state.L, provider, event)
	if err != nil {
		state.discard = true
		return nil, err
	}
	return out, nil
}

func (m *Manager) loadScript(ctx context.Context, provider string, force bool) (Script, bool, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return Script{}, false, nil
	}
	now := time.Now()
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return Script{}, false, ErrClosed
	}
	state := m.providers[provider]
	if !force && state.checkedAt.Add(m.options.ScriptCacheTTL).After(now) {
		m.mu.RUnlock()
		return state.script, state.loaded, nil
	}
	m.mu.RUnlock()
	if m.options.ScriptLoader == nil {
		return Script{}, false, nil
	}
	script, found, err := m.options.ScriptLoader(ctx, provider)
	if err != nil {
		return Script{}, false, err
	}
	if found && script.Provider == "" {
		script.Provider = provider
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Script{}, false, ErrClosed
	}
	previous := m.providers[provider]
	if previous.loaded {
		oldKey := scriptCacheKey(previous.script)
		newKey := ""
		if found {
			newKey = scriptCacheKey(script)
		}
		if oldKey != newKey {
			m.deleteScriptStateLocked(oldKey)
		}
	}
	if !found {
		delete(m.providers, provider)
		return Script{}, false, nil
	}
	m.providers[provider] = providerState{script: script, loaded: true, checkedAt: now}
	return script, true, nil
}

func (m *Manager) call(L *lua.LState, provider string, event Event) (Result, error) {
	action, err := runtimeActionName(event.Action)
	if err != nil {
		return nil, err
	}
	if m.options.JSONBoundary {
		raw, err := json.Marshal(event)
		if err != nil {
			return nil, err
		}
		handle := L.GetGlobal("__runtime_call_json")
		if handle == lua.LNil {
			return nil, fmt.Errorf("tasks runtime script %q has no json handler", provider)
		}
		if err := L.CallByParam(lua.P{Fn: handle, NRet: 1, Protect: true}, lua.LString(action), lua.LString(raw)); err != nil {
			return nil, fmt.Errorf("tasks runtime %s %q failed: %w", action, provider, err)
		}
		value := L.Get(-1)
		L.Pop(1)
		var out map[string]any
		if err := json.Unmarshal([]byte(value.String()), &out); err != nil {
			return nil, fmt.Errorf("tasks runtime %s %q returned bad json: %w", action, provider, err)
		}
		return Result(out), nil
	}
	handle := L.GetGlobal(action)
	if handle == lua.LNil {
		return nil, fmt.Errorf("tasks runtime script %q has no %s(event)", provider, action)
	}
	input := goToLua(L, eventToMap(event))
	if err := L.CallByParam(lua.P{Fn: handle, NRet: 1, Protect: true}, input); err != nil {
		return nil, fmt.Errorf("tasks runtime %s %q failed: %w", action, provider, err)
	}
	value := L.Get(-1)
	L.Pop(1)
	out, ok := luaToGo(value).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tasks runtime %s %q returned non-object", action, provider)
	}
	return Result(out), nil
}

func runtimeActionName(action string) (string, error) {
	switch action {
	case "list", "start", "check", "callback":
		return action, nil
	default:
		return "", fmt.Errorf("tasks runtime unsupported action %q", action)
	}
}

type runtimeState struct {
	L         *lua.LState
	httpCalls int
	discard   bool
}

func (m *Manager) acquireState(ctx context.Context, script Script, proto *lua.FunctionProto) (*runtimeState, bool, error) {
	key := scriptCacheKey(script)
	if m.options.StatePoolSize > 0 {
		pool := m.pool(key)
		select {
		case state := <-pool:
			state.httpCalls = 0
			state.discard = false
			state.L.SetContext(ctx)
			state.L.SetTop(0)
			return state, true, nil
		default:
		}
		state, err := m.newState(ctx, script, proto)
		return state, true, err
	}
	state, err := m.newState(ctx, script, proto)
	return state, false, err
}

func (m *Manager) releaseState(script Script, state *runtimeState, pooled bool) {
	if state == nil {
		return
	}
	if !pooled || state.discard {
		state.L.Close()
		return
	}
	state.L.RemoveContext()
	state.L.SetTop(0)
	key := scriptCacheKey(script)
	pool, current := m.currentPool(script.Provider, key)
	if !current {
		state.L.Close()
		return
	}
	select {
	case pool <- state:
	default:
		state.L.Close()
	}
}

func (m *Manager) currentPool(provider string, key string) (chan *runtimeState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.providers[provider]
	if !ok || !current.loaded || scriptCacheKey(current.script) != key {
		return nil, false
	}

	pool := m.pools[key]
	if pool == nil {
		pool = make(chan *runtimeState, m.options.StatePoolSize)
		m.pools[key] = pool
	}

	return pool, true
}

func (m *Manager) pool(key string) chan *runtimeState {
	m.mu.Lock()
	defer m.mu.Unlock()
	pool := m.pools[key]
	if pool == nil {
		pool = make(chan *runtimeState, m.options.StatePoolSize)
		m.pools[key] = pool
	}
	return pool
}

func (m *Manager) closePools() {
	m.mu.Lock()
	pools := m.pools
	m.pools = make(map[string]chan *runtimeState)
	m.mu.Unlock()
	for _, pool := range pools {
		closeRuntimePool(pool)
	}
}

func (m *Manager) deleteScriptStateLocked(key string) {
	if key == "" {
		return
	}
	delete(m.cache, key)
	pool := m.pools[key]
	delete(m.pools, key)
	closeRuntimePool(pool)
}

func closeRuntimePool(pool chan *runtimeState) {
	for {
		select {
		case state := <-pool:
			if state != nil {
				state.L.Close()
			}
		default:
			return
		}
	}
}

func (m *Manager) newState(ctx context.Context, script Script, proto *lua.FunctionProto) (*runtimeState, error) {
	state := &runtimeState{}
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	state.L = L
	L.SetContext(ctx)
	L.SetMx(m.options.MaxMemory)
	openSafeLibs(L)
	registerJSON(L)
	registerUUID(L)
	registerTime(L)
	registerHTTP(L, m.http, &state.httpCalls, m.options.MaxHTTPRequests)
	if err := L.CallByParam(lua.P{Fn: L.NewFunctionFromProto(proto), NRet: 0, Protect: true}); err != nil {
		L.Close()
		return nil, fmt.Errorf("tasks runtime load %q failed: %w", script.Provider, err)
	}
	if m.options.JSONBoundary {
		wrapper, err := compileJSONWrapper()
		if err != nil {
			L.Close()
			return nil, err
		}
		if err := L.CallByParam(lua.P{Fn: L.NewFunctionFromProto(wrapper), NRet: 0, Protect: true}); err != nil {
			L.Close()
			return nil, fmt.Errorf("tasks runtime json wrapper %q failed: %w", script.Provider, err)
		}
	}
	return state, nil
}

func (m *Manager) compile(script Script) (*lua.FunctionProto, error) {
	key := scriptCacheKey(script)
	m.mu.RLock()
	proto := m.cache[key]
	m.mu.RUnlock()
	if proto != nil {
		return proto, nil
	}
	chunk, err := parse.Parse(strings.NewReader(script.Source), script.Provider)
	if err != nil {
		return nil, fmt.Errorf("tasks runtime parse %q failed: %w", script.Provider, err)
	}
	proto, err = lua.Compile(chunk, script.Provider)
	if err != nil {
		return nil, fmt.Errorf("tasks runtime compile %q failed: %w", script.Provider, err)
	}
	m.mu.Lock()
	if existing := m.cache[key]; existing != nil {
		m.mu.Unlock()
		return existing, nil
	}
	m.cache[key] = proto
	m.mu.Unlock()
	return proto, nil
}

func scriptCacheKey(script Script) string {
	hash := sha256.Sum256([]byte(script.Provider + "\x00" + script.Version + "\x00" + script.Source))
	return hex.EncodeToString(hash[:])
}

func compileJSONWrapper() (*lua.FunctionProto, error) {
	jsonWrapperOnce.Do(func() {
		const source = `
function __runtime_call_json(action, raw)
  local fn = _G[action]
  if fn == nil then
    error("missing runtime action: " .. tostring(action))
  end
  return json.encode(fn(json.decode(raw)))
end
`
		chunk, err := parse.Parse(strings.NewReader(source), "__runtime_json")
		if err != nil {
			jsonWrapperErr = err
			return
		}
		jsonWrapperProto, jsonWrapperErr = lua.Compile(chunk, "__runtime_json")
	})
	return jsonWrapperProto, jsonWrapperErr
}

func openSafeLibs(L *lua.LState) {
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		_ = L.PCall(1, 0, nil)
	}
	for _, name := range []string{"dofile", "loadfile", "collectgarbage", "require", "module"} {
		L.SetGlobal(name, lua.LNil)
	}
}

func eventToMap(event Event) map[string]any {
	raw, _ := json.Marshal(event)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}
