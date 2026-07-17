package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/control/service/admin"
)

const ProviderTelegramWebApp = "telegram_webapp"

type TelegramWebAppConfig struct {
	Provider string
	BotToken string
	MaxAge   time.Duration
	Now      func() time.Time
}

type TelegramWebAppProvider struct {
	provider string
	botToken string
	maxAge   time.Duration
	now      func() time.Time
}

type telegramWebAppUser struct {
	ID           int64  `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
	IsPremium    bool   `json:"is_premium"`
}

func NewTelegramWebApp(config TelegramWebAppConfig) (*TelegramWebAppProvider, error) {
	provider := normalizeProvider(config.Provider)
	if provider == "" {
		provider = ProviderTelegramWebApp
	}
	if strings.TrimSpace(config.BotToken) == "" {
		return nil, ErrBotTokenRequired
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	return &TelegramWebAppProvider{provider: provider, botToken: strings.TrimSpace(config.BotToken), maxAge: config.MaxAge, now: now}, nil
}

func TelegramWebApp(ctx context.Context, params TelegramWebAppAuthParams) (admin.AuthIdentityParams, error) {
	provider, err := NewTelegramWebApp(TelegramWebAppConfig{BotToken: params.BotToken, MaxAge: params.MaxAge, Now: params.Now})
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	identity, err := provider.Resolve(ctx, Request{RawData: params.InitData})
	if err != nil {
		return admin.AuthIdentityParams{}, err
	}
	return identityAuthParams(identity, params.IP, params.UserAgent, params.BindToIP, params.ExpiresAt), nil
}

func (t *TelegramWebAppProvider) Provider() string { return t.provider }

func (t *TelegramWebAppProvider) Resolve(_ context.Context, request Request) (Identity, error) {
	if t == nil {
		return Identity{}, ErrProviderRequired
	}
	values, err := url.ParseQuery(strings.TrimSpace(request.RawData))
	if err != nil {
		return Identity{}, err
	}
	hash := values.Get("hash")
	if hash == "" {
		return Identity{}, ErrInvalidSignature
	}
	if !t.validSignature(values, hash) {
		return Identity{}, ErrInvalidSignature
	}
	if err := t.validateAge(values.Get("auth_date")); err != nil {
		return Identity{}, err
	}
	var user telegramWebAppUser
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil {
		return Identity{}, err
	}
	if user.ID == 0 {
		return Identity{}, ErrSubjectRequired
	}
	displayName := strings.TrimSpace(strings.Join([]string{user.FirstName, user.LastName}, " "))
	if displayName == "" {
		displayName = user.Username
	}
	payload, err := json.Marshal(map[string]any{
		"user":      user,
		"auth_date": values.Get("auth_date"),
		"query_id":  values.Get("query_id"),
	})
	if err != nil {
		return Identity{}, err
	}
	return Identity{Provider: t.provider, Subject: strconv.FormatInt(user.ID, 10), DisplayName: displayName, Payload: payload}, nil
}

func (t *TelegramWebAppProvider) validSignature(values url.Values, expected string) bool {
	parts := make([]string, 0, len(values))
	for key, item := range values {
		if key == "hash" || len(item) == 0 {
			continue
		}
		parts = append(parts, key+"="+item[0])
	}
	sort.Strings(parts)
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(t.botToken))
	secret := secretMAC.Sum(nil)
	dataMAC := hmac.New(sha256.New, secret)
	_, _ = dataMAC.Write([]byte(strings.Join(parts, "\n")))
	actual := hex.EncodeToString(dataMAC.Sum(nil))
	return hmac.Equal([]byte(actual), []byte(strings.ToLower(expected)))
}

func (t *TelegramWebAppProvider) validateAge(rawAuthDate string) error {
	if t.maxAge <= 0 {
		return nil
	}
	unix, err := strconv.ParseInt(rawAuthDate, 10, 64)
	if err != nil {
		return ErrAuthDataExpired
	}
	age := t.now().Sub(time.Unix(unix, 0))
	if age < 0 {
		age = -age
	}
	if age > t.maxAge {
		return ErrAuthDataExpired
	}
	return nil
}
