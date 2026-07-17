package auth

import "testing"

func TestGitLabConstructorUsesStableProviderKey(t *testing.T) {
	t.Parallel()
	provider, err := NewGitLab(OAuth2ProviderConfig{
		ClientID: "client", ClientSecret: "secret", UserInfoURL: "https://example.test/userinfo",
	})
	if err != nil {
		t.Fatalf("new gitlab provider: %v", err)
	}
	if provider.Provider() != ProviderGitLab {
		t.Fatalf("unexpected provider key: %q", provider.Provider())
	}
}
