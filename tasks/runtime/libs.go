package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	json "github.com/goccy/go-json"
	"github.com/google/uuid"
	lua "github.com/yuin/gopher-lua"
)

type httpClient struct {
	client           *http.Client
	maxResponseBytes int64
}

func registerJSON(L *lua.LState) {
	module := L.NewTable()
	L.SetFuncs(module, map[string]lua.LGFunction{
		"decode": func(L *lua.LState) int {
			raw := L.CheckString(1)
			var value any
			if err := json.Unmarshal([]byte(raw), &value); err != nil {
				L.RaiseError("json.decode failed: %v", err)
				return 0
			}
			L.Push(goToLua(L, normalizeJSON(value)))
			return 1
		},
		"encode": func(L *lua.LState) int {
			raw, err := json.Marshal(luaToGo(L.CheckAny(1)))
			if err != nil {
				L.RaiseError("json.encode failed: %v", err)
				return 0
			}
			L.Push(lua.LString(raw))
			return 1
		},
	})
	L.SetGlobal("json", module)
}

func registerUUID(L *lua.LState) {
	module := L.NewTable()
	L.SetFuncs(module, map[string]lua.LGFunction{
		"new": func(L *lua.LState) int {
			L.Push(lua.LString(uuid.NewString()))
			return 1
		},
	})
	L.SetGlobal("uuid", module)
}

func registerTime(L *lua.LState) {
	module := L.NewTable()
	L.SetFuncs(module, map[string]lua.LGFunction{
		"now": func(L *lua.LState) int {
			L.Push(lua.LString(time.Now().UTC().Format(time.RFC3339Nano)))
			return 1
		},
		"unix": func(L *lua.LState) int {
			L.Push(lua.LNumber(time.Now().UTC().Unix()))
			return 1
		},
	})
	L.SetGlobal("time", module)
}

func registerHTTP(L *lua.LState, client *httpClient, calls *int, maxCalls int) {
	module := L.NewTable()
	L.SetFuncs(module, map[string]lua.LGFunction{
		"request": func(L *lua.LState) int {
			*calls = *calls + 1
			if *calls > maxCalls {
				L.RaiseError("http request limit exceeded")
				return 0
			}
			response, err := client.request(L.Context(), luaToGo(L.CheckTable(1)))
			if err != nil {
				L.RaiseError("http.request failed: %v", err)
				return 0
			}
			L.Push(goToLua(L, response))
			return 1
		},
	})
	L.SetGlobal("http", module)
}

func (c *httpClient) request(ctx context.Context, value any) (map[string]any, error) {
	params, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("params must be object")
	}
	method := strings.ToUpper(stringValue(params["method"]))
	if method == "" {
		method = http.MethodGet
	}
	rawURL := stringValue(params["url"])
	if rawURL == "" {
		return nil, fmt.Errorf("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if query, ok := params["query"].(map[string]any); ok {
		q := parsed.Query()
		for key, value := range query {
			q.Set(key, stringValue(value))
		}
		parsed.RawQuery = q.Encode()
	}
	var body io.Reader
	if rawBody, ok := params["body"]; ok && rawBody != nil {
		switch v := rawBody.(type) {
		case string:
			body = strings.NewReader(v)
		default:
			encoded, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			body = bytes.NewReader(encoded)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	if headers, ok := params["headers"].(map[string]any); ok {
		for key, value := range headers {
			req.Header.Set(key, stringValue(value))
		}
	}
	if req.Body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	limit := c.maxResponseBytes
	if limit <= 0 {
		limit = defaultMaxResponseBytes
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("response body exceeds %d bytes", limit)
	}
	headers := make(map[string]any, len(resp.Header))
	for key, values := range resp.Header {
		if len(values) == 1 {
			headers[key] = values[0]
			continue
		}
		items := make([]any, 0, len(values))
		for _, value := range values {
			items = append(items, value)
		}
		headers[key] = items
	}
	return map[string]any{
		"status":  int64(resp.StatusCode),
		"headers": headers,
		"body":    string(raw),
	}, nil
}

func normalizeJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = normalizeJSON(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, normalizeJSON(item))
		}
		return out
	case float64:
		if v == float64(int64(v)) {
			return int64(v)
		}
	}
	return value
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
