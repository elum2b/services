package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscordExchangesCodeAndLoadsCurrentUser(t *testing.T) {

	t.Parallel()

	var tokenRequestSeen bool

	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			switch r.URL.Path {
			case "/token":
				tokenRequestSeen = true

				if r.Method != http.MethodPost {
					t.Fatalf("unexpected token method: %s", r.Method)
				}

				if err := r.ParseForm(); err != nil {
					t.Fatalf("parse token form: %v", err)
				}

				if r.Form.Get("code") != "code-1" ||
					r.Form.Get("client_id") != "client" ||
					r.Form.Get("client_secret") != "secret" ||
					r.Form.Get("scope") != "identify" {
					t.Fatalf("unexpected token form: %v", r.Form)
				}

				_, _ = w.Write(
					[]byte(`{"access_token":"access-1","token_type":"Bearer"}`),
				)
			case "/userinfo":
				if r.Header.Get("Authorization") != "Bearer access-1" {
					t.Fatalf(
						"unexpected authorization header: %q",
						r.Header.Get("Authorization"),
					)
				}

				_, _ = w.Write(
					[]byte(`{"id":"123","global_name":"Root Admin","username":"root"}`),
				)
			default:
				http.NotFound(w, r)
			}
		}),
	)
	defer server.Close()

	params, err := Discord(context.Background(), OAuth2AuthParams{
		ClientID:     "client",
		ClientSecret: "secret",
		Code:         "code-1",
		TokenURL:     server.URL + "/token",
		UserInfoURL:  server.URL + "/userinfo",
		HTTPClient:   server.Client(),
	})
	if err != nil {
		t.Fatalf("discord auth: %v", err)
	}

	if !tokenRequestSeen {
		t.Fatal("expected token request")
	}

	if params.Provider != ProviderDiscord || params.Subject != "123" ||
		params.DisplayName != "Root Admin" {
		t.Fatalf("unexpected auth params: %+v", params)
	}
}

func TestDiscordConstructorUsesStableProviderKey(t *testing.T) {

	t.Parallel()

	provider, err := NewDiscord(OAuth2ProviderConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		UserInfoURL:  "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new discord provider: %v", err)
	}

	if provider.Provider() != ProviderDiscord {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
