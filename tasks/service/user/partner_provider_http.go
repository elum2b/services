package user

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"
)

const partnerMaxResponseBody = 1 << 20

type partnerHTTPClient struct {
	client  *http.Client
	timeout time.Duration
	baseURL string
}

func (c partnerHTTPClient) postJSON(
	ctx context.Context,
	path string,
	headers map[string]string,
	request any,
	response any,
) error {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(c.baseURL, "/")+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	client := c.client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, partnerMaxResponseBody))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("partner http status %d: %s", resp.StatusCode, string(raw))
	}
	if response == nil {
		return nil
	}
	return json.Unmarshal(raw, response)
}

func partnerSecret(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func partnerLimit(value int32, fallback int) int {
	if value > 0 {
		return int(value)
	}
	return fallback
}

func partnerString(values map[string]string, key string) string {
	if values == nil {
		return ""
	}
	return values[key]
}

func partnerBool(values map[string]string, key string) (bool, bool) {
	raw := partnerString(values, key)
	if raw == "" {
		return false, false
	}
	value, err := strconv.ParseBool(raw)
	return value, err == nil
}

func partnerInt64String(value string) any {
	if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
		return parsed
	}
	return value
}

func partnerRawObject(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return []byte("{}")
	}
	return value
}

func partnerMarshal(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return raw
}

func partnerConfigSetting(settings json.RawMessage, key string, fallback string) string {
	if len(settings) == 0 || string(settings) == "null" {
		return fallback
	}
	var data map[string]any
	if err := json.Unmarshal(settings, &data); err != nil {
		return fallback
	}
	value, ok := data[key]
	if !ok || value == nil {
		return fallback
	}
	return fmt.Sprint(value)
}
