package auth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/control/service/admin"
	serviceerrors "github.com/elum2b/services/errors"
)

const maxProviderResponseSize = 1 << 20

type OAuth2ProviderConfig struct {
	ClientID     string
	ClientSecret string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	Mapping      ProfileFieldMapping
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type OAuth2Config struct {
	Provider     string
	ClientID     string
	ClientSecret string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	Mapping      ProfileFieldMapping
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type OAuth2 struct {
	provider     string
	clientID     string
	clientSecret string
	tokenURL     string
	userInfoURL  string
	scopes       []string
	mapping      ProfileFieldMapping
	client       *http.Client
	timeout      time.Duration
}

func NewOAuth2(config OAuth2Config) (*OAuth2, error) {
	provider := normalizeProvider(config.Provider)
	if provider == "" {
		return nil, ErrProviderRequired
	}
	if strings.TrimSpace(config.ClientID) == "" {
		return nil, ErrClientIDRequired
	}
	if strings.TrimSpace(config.ClientSecret) == "" {
		return nil, ErrClientSecretRequired
	}
	if strings.TrimSpace(config.UserInfoURL) == "" {
		return nil, ErrEndpointRequired
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	mapping := config.Mapping
	if len(mapping.Subject) == 0 {
		mapping.Subject = []string{"sub", "id", "user.id"}
	}
	if len(mapping.DisplayName) == 0 {
		mapping.DisplayName = []string{"name", "display_name", "login", "username", "email"}
	}
	return &OAuth2{
		provider: provider, clientID: strings.TrimSpace(config.ClientID), clientSecret: strings.TrimSpace(config.ClientSecret),
		tokenURL: strings.TrimSpace(config.TokenURL), userInfoURL: strings.TrimSpace(config.UserInfoURL), scopes: config.Scopes,
		mapping: mapping, client: client, timeout: config.Timeout,
	}, nil
}

func (o *OAuth2) Provider() string { return o.provider }

func (o *OAuth2) Resolve(ctx context.Context, request Request) (Identity, error) {
	if o == nil {
		return Identity{}, ErrProviderRequired
	}
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	token := strings.TrimSpace(request.AccessToken)
	if token == "" {
		if strings.TrimSpace(request.Code) == "" {
			return Identity{}, ErrCodeRequired
		}
		if o.tokenURL == "" {
			return Identity{}, ErrEndpointRequired
		}
		exchanged, err := o.exchangeCode(ctx, request)
		if err != nil {
			return Identity{}, err
		}
		token = exchanged
	}
	profile, raw, err := o.fetchProfile(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	subject := firstMappedString(profile, o.mapping.Subject...)
	if subject == "" {
		return Identity{}, ErrSubjectRequired
	}
	displayName := firstMappedString(profile, o.mapping.DisplayName...)
	payload, err := json.Marshal(map[string]any{
		"profile": profile,
		"raw":     json.RawMessage(raw),
	})
	if err != nil {
		return Identity{}, err
	}
	return Identity{Provider: o.provider, Subject: subject, DisplayName: displayName, Payload: payload}, nil
}

func (o *OAuth2) exchangeCode(ctx context.Context, request Request) (string, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("client_id", o.clientID)
	values.Set("client_secret", o.clientSecret)
	values.Set("code", strings.TrimSpace(request.Code))
	if request.RedirectURI != "" {
		values.Set("redirect_uri", strings.TrimSpace(request.RedirectURI))
	}
	if len(o.scopes) > 0 {
		values.Set("scope", strings.Join(o.scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := o.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseSize))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", serviceerrors.Wrap(serviceerrors.CodeUnauthorized, "oauth token exchange failed", providerResponseError{statusCode: resp.StatusCode})
	}
	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", ErrTokenRequired
	}
	return payload.AccessToken, nil
}

func (o *OAuth2) fetchProfile(ctx context.Context, token string) (map[string]any, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.userInfoURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseSize))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, serviceerrors.Wrap(serviceerrors.CodeUnauthorized, "oauth userinfo failed", providerResponseError{statusCode: resp.StatusCode})
	}
	var profile map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&profile); err != nil {
		return nil, nil, err
	}
	return profile, body, nil
}

func newOAuth2Provider(provider string, config OAuth2ProviderConfig) (*OAuth2, error) {
	return NewOAuth2(OAuth2Config{
		Provider:     provider,
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		TokenURL:     config.TokenURL,
		UserInfoURL:  config.UserInfoURL,
		Scopes:       config.Scopes,
		Mapping:      config.Mapping,
		HTTPClient:   config.HTTPClient,
		Timeout:      config.Timeout,
	})
}

func oAuth2ProviderConfigFromAuthParams(params OAuth2AuthParams) OAuth2ProviderConfig {
	return OAuth2ProviderConfig{
		ClientID: params.ClientID, ClientSecret: params.ClientSecret, TokenURL: params.TokenURL, UserInfoURL: params.UserInfoURL,
		Scopes: params.Scopes, Mapping: params.Mapping, HTTPClient: params.HTTPClient, Timeout: params.Timeout,
	}
}

func identityAuthParams(identity Identity, ip, userAgent string, bindToIP bool, expiresAt time.Time) admin.AuthIdentityParams {
	return admin.AuthIdentityParams{
		Provider: identity.Provider, Subject: identity.Subject, DisplayName: identity.DisplayName, Payload: identity.Payload,
		IP: ip, UserAgent: userAgent, BindToIP: bindToIP, ExpiresAt: expiresAt,
	}
}

func firstMappedString(data map[string]any, paths ...string) string {
	for _, path := range paths {
		value, ok := lookupPath(data, path)
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case json.Number:
			return typed.String()
		case float64:
			return strconv.FormatInt(int64(typed), 10)
		case int64:
			return strconv.FormatInt(typed, 10)
		case int:
			return strconv.Itoa(typed)
		}
	}
	return ""
}

func lookupPath(data map[string]any, path string) (any, bool) {
	current := any(data)
	for _, part := range strings.Split(path, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
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

type providerResponseError struct {
	statusCode int
}

func (e providerResponseError) Error() string {
	return "provider responded with status " + strconv.Itoa(e.statusCode)
}
