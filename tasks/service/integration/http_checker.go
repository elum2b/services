package integration

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const maxHTTPCheckResponse = 1 << 20

type HTTPChecker struct {
	Client  *http.Client
	Timeout time.Duration
}

type HTTPCheckPayload struct {
	Request HTTPCheckRequest `json:"request"`
	Success HTTPCheckSuccess `json:"success"`
}

type HTTPCheckRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Query   map[string]string `json:"query"`
	Body    json.RawMessage   `json:"body"`
}

type HTTPCheckSuccess struct {
	StatusCodes  []int  `json:"status_codes"`
	JSONPath     string `json:"json_path"`
	Equals       any    `json:"equals"`
	BodyContains string `json:"body_contains"`
}

func (h HTTPChecker) CheckExternalTask(ctx context.Context, params ExternalTaskCheckParams) (CheckResult, error) {
	return h.check(ctx, params.Identity, params.Task, params.Provider, params.Variables, params.OccurredAt)
}

func (h HTTPChecker) CheckChannelSubscription(
	ctx context.Context,
	params ChannelSubscriptionCheckParams,
) (CheckResult, error) {
	return h.check(ctx, params.Identity, params.Task, params.Provider, params.Variables, params.OccurredAt)
}

func (h HTTPChecker) check(
	ctx context.Context,
	identity Identity,
	task TaskContext,
	provider string,
	variables map[string]string,
	now time.Time,
) (CheckResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if h.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.Timeout)
		defer cancel()
	}
	var config HTTPCheckPayload
	if err := json.Unmarshal(task.IntegrationPayload, &config); err != nil {
		return CheckResult{}, err
	}
	values := templateValues(identity, task, provider, variables, now)
	req, err := buildHTTPCheckRequest(ctx, config.Request, values)
	if err != nil {
		return CheckResult{}, err
	}
	client := h.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return CheckResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPCheckResponse))
	if err != nil {
		return CheckResult{}, err
	}
	completed, reason := matchHTTPCheckSuccess(resp.StatusCode, body, config.Success)
	payload, _ := json.Marshal(map[string]any{
		"provider":    provider,
		"status_code": resp.StatusCode,
		"completed":   completed,
		"reason":      reason,
	})
	return CheckResult{Completed: completed, Reason: reason, Payload: payload}, nil
}

func buildHTTPCheckRequest(
	ctx context.Context,
	config HTTPCheckRequest,
	values map[string]string,
) (*http.Request, error) {
	method := strings.TrimSpace(config.Method)
	if method == "" {
		method = http.MethodGet
	}
	rawURL := renderTemplate(config.URL, values)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	for key, value := range config.Query {
		query.Set(renderTemplate(key, values), renderTemplate(value, values))
	}
	parsed.RawQuery = query.Encode()
	var body io.Reader
	if len(config.Body) > 0 {
		body = bytes.NewReader([]byte(renderTemplate(string(config.Body), values)))
	}
	req, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	for key, value := range config.Headers {
		req.Header.Set(renderTemplate(key, values), renderTemplate(value, values))
	}
	if len(config.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func matchHTTPCheckSuccess(statusCode int, body []byte, success HTTPCheckSuccess) (bool, string) {
	if len(success.StatusCodes) == 0 {
		if statusCode < 200 || statusCode >= 300 {
			return false, "status_code"
		}
	} else {
		ok := false
		for _, expected := range success.StatusCodes {
			if statusCode == expected {
				ok = true
				break
			}
		}
		if !ok {
			return false, "status_code"
		}
	}
	if success.BodyContains != "" && !strings.Contains(string(body), success.BodyContains) {
		return false, "body_contains"
	}
	if success.JSONPath != "" {
		var data any
		if err := json.Unmarshal(body, &data); err != nil {
			return false, "json"
		}
		value, ok := lookupJSONPath(data, success.JSONPath)
		if !ok {
			return false, "json_path"
		}
		if success.Equals != nil && !jsonValuesEqual(value, success.Equals) {
			return false, "json_equals"
		}
	}
	return true, ""
}

func templateValues(
	identity Identity,
	task TaskContext,
	provider string,
	variables map[string]string,
	now time.Time,
) map[string]string {
	values := map[string]string{
		"workspace":    identity.WorkspaceID,
		"workspace_id": identity.WorkspaceID,
		"app":          strconv.FormatInt(identity.AppID, 10),
		"app_id":       strconv.FormatInt(identity.AppID, 10),
		"platform":     strconv.FormatInt(identity.PlatformID, 10),
		"platform_id":  strconv.FormatInt(identity.PlatformID, 10),
		"user":         identity.PlatformUserID,
		"user_id":      identity.PlatformUserID,
		"task":         strconv.FormatUint(task.ID, 10),
		"task_id":      strconv.FormatUint(task.ID, 10),
		"task_key":     task.Key,
		"action_key":   task.ActionKey,
		"provider":     provider,
		"time":         now.UTC().Format(time.RFC3339),
		"time_rfc3339": now.UTC().Format(time.RFC3339),
		"time_unix":    strconv.FormatInt(now.UTC().Unix(), 10),
		"time_unix_ms": strconv.FormatInt(now.UTC().UnixMilli(), 10),
	}
	maps.Copy(values, variables)
	return values
}

func renderTemplate(value string, values map[string]string) string {
	return osExpandManual(value, func(name string) string {
		if value, ok := values[name]; ok {
			return value
		}
		return ""
	})
}

func osExpandManual(value string, mapping func(string) string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '$' && i+2 < len(value) && value[i+1] == '{' {
			end := strings.IndexByte(value[i+2:], '}')
			if end >= 0 {
				name := value[i+2 : i+2+end]
				out.WriteString(mapping(name))
				i += end + 2
				continue
			}
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func lookupJSONPath(value any, path string) (any, bool) {
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, ".")
	current := value
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func jsonValuesEqual(left any, right any) bool {
	return reflect.DeepEqual(left, right)
}
