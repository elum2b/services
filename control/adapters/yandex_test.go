package auth

import "testing"

func TestYandexConstructorUsesStableProviderKey(t *testing.T) {
	t.Parallel()
	provider, err := NewYandex(OAuth2ProviderConfig{
		ClientID: "client", ClientSecret: "secret", UserInfoURL: "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new yandex provider: %v", err)
	}
	if provider.Provider() != ProviderYandex {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
