package auth

import (
	"context"
	"net/http"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/control/service/admin"
)

type Admin interface {
	CompleteAuth(ctx context.Context, params admin.AuthIdentityParams) (admin.AuthResult, error)
}

type Provider interface {
	Provider() string
	Resolve(ctx context.Context, request Request) (Identity, error)
}

type Request struct {
	Provider    string
	InviteToken string
	Code        string
	AccessToken string
	RedirectURI string
	State       string
	RawData     string
	IP          string
	UserAgent   string
	BindToIP    bool
	ExpiresAt   time.Time
}

type Identity struct {
	Provider    string
	Subject     string
	DisplayName string
	Payload     json.RawMessage
}

type ProfileFieldMapping struct {
	Subject     []string
	DisplayName []string
}

type OAuth2AuthParams struct {
	ClientID     string
	ClientSecret string
	InviteToken  string
	Code         string
	AccessToken  string
	RedirectURI  string
	IP           string
	UserAgent    string
	BindToIP     bool
	ExpiresAt    time.Time
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
	Mapping      ProfileFieldMapping
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type TelegramWebAppAuthParams struct {
	BotToken    string
	InitData    string
	InviteToken string
	IP          string
	UserAgent   string
	BindToIP    bool
	ExpiresAt   time.Time
	MaxAge      time.Duration
	Now         func() time.Time
}
