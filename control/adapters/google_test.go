package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoogleReturnsAuthIdentityParams(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"sub":"sub-1","name":"Root Admin"}`))
	}))
	defer server.Close()
	params, err := Google(context.Background(), OAuth2AuthParams{
		ClientID: "client", ClientSecret: "secret", AccessToken: "access",
		UserInfoURL: server.URL, IP: "127.0.0.1", UserAgent: "ua", BindToIP: true,
	})
	if err != nil {
		t.Fatalf("google auth: %v", err)
	}
	if params.Provider != ProviderGoogle || params.Subject != "sub-1" || params.DisplayName != "Root Admin" {
		t.Fatalf("unexpected auth params: %+v", params)
	}
	if params.IP != "127.0.0.1" || params.UserAgent != "ua" || !params.BindToIP {
		t.Fatalf("unexpected session params: %+v", params)
	}
}

func TestGoogleConstructorUsesStableProviderKey(t *testing.T) {
	t.Parallel()
	provider, err := NewGoogle(OAuth2ProviderConfig{
		ClientID: "client", ClientSecret: "secret", UserInfoURL: "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new google provider: %v", err)
	}
	if provider.Provider() != ProviderGoogle {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
