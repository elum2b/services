package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubExchangesCodeAndLoadsUserInfo(t *testing.T) {
	t.Parallel()
	var tokenRequestSeen bool
	var userInfoRequestSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenRequestSeen = true
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected token method: %s", r.Method)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse token form: %v", err)
			}
			if r.Form.Get("code") != "code-1" || r.Form.Get("client_id") != "client" || r.Form.Get("client_secret") != "secret" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-1","token_type":"Bearer"}`))
		case "/userinfo":
			userInfoRequestSeen = true
			if r.Header.Get("Authorization") != "Bearer access-1" {
				t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
			}
			_, _ = w.Write([]byte(`{"id":123,"login":"root"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	params, err := GitHub(context.Background(), OAuth2AuthParams{
		ClientID: "client", ClientSecret: "secret", Code: "code-1",
		TokenURL: server.URL + "/token", UserInfoURL: server.URL + "/userinfo", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("github auth: %v", err)
	}
	if !tokenRequestSeen || !userInfoRequestSeen {
		t.Fatalf("expected token and userinfo requests, token=%v userinfo=%v", tokenRequestSeen, userInfoRequestSeen)
	}
	if params.Provider != ProviderGitHub || params.Subject != "123" || params.DisplayName != "root" {
		t.Fatalf("unexpected auth params: %+v", params)
	}
}

func TestGitHubConstructorUsesStableProviderKey(t *testing.T) {
	t.Parallel()
	provider, err := NewGitHub(OAuth2ProviderConfig{
		ClientID: "client", ClientSecret: "secret", UserInfoURL: "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new github provider: %v", err)
	}
	if provider.Provider() != ProviderGitHub {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
