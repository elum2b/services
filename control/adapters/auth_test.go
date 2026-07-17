package auth

import (
	"context"
	"testing"
	"time"

	json "github.com/goccy/go-json"

	"github.com/elum2b/services/control/service/admin"
)

type mockAdmin struct {
	params admin.AuthIdentityParams
	called bool
}

func (m *mockAdmin) CompleteAuth(_ context.Context, params admin.AuthIdentityParams) (admin.AuthResult, error) {
	m.params = params
	m.called = true
	return admin.AuthResult{SessionToken: "session-token"}, nil
}

type staticProvider struct {
	identity Identity
}

func (p staticProvider) Provider() string { return p.identity.Provider }

func (p staticProvider) Resolve(context.Context, Request) (Identity, error) {
	return p.identity, nil
}

func TestAuthenticateCompletesControlAuth(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"provider":"test"}`)
	mock := &mockAdmin{}
	adapter, err := New(mock, staticProvider{identity: Identity{
		Provider: "Test", Subject: "42", DisplayName: "Admin", Payload: payload,
	}})
	if err != nil {
		t.Fatalf("new auth adapter: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	result, err := adapter.Authenticate(context.Background(), Request{
		Provider: "test", IP: "127.0.0.1", UserAgent: "ua", BindToIP: true, ExpiresAt: expiresAt,
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if result.SessionToken != "session-token" {
		t.Fatalf("unexpected token: %q", result.SessionToken)
	}
	if !mock.called {
		t.Fatal("control admin was not called")
	}
	if mock.params.Provider != "test" || mock.params.Subject != "42" || mock.params.DisplayName != "Admin" {
		t.Fatalf("unexpected identity params: %+v", mock.params)
	}
	if mock.params.IP != "127.0.0.1" || mock.params.UserAgent != "ua" || !mock.params.BindToIP || !mock.params.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected session params: %+v", mock.params)
	}
	if string(mock.params.Payload) != string(payload) {
		t.Fatalf("unexpected payload: %s", mock.params.Payload)
	}
}
