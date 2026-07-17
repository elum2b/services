package auth

import "testing"

func TestVKIDConstructorUsesStableProviderKey(t *testing.T) {
	t.Parallel()
	provider, err := NewVKID(OAuth2ProviderConfig{
		ClientID: "client", ClientSecret: "secret", UserInfoURL: "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new vk id provider: %v", err)
	}
	if provider.Provider() != ProviderVKID {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
