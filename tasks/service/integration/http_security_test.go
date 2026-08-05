package integration

import (
	"context"
	"net/url"
	"testing"
)

func TestValidateHTTPCheckURLRejectsUnsafeTargets(t *testing.T) {

	for _, raw := range []string{
		"http://example.com/check",
		"https://127.0.0.1/check",
		"https://10.0.0.1/check",
		"https://169.254.169.254/latest/meta-data",
		"https://user:password@example.com/check",
		"file:///etc/passwd",
	} {
		value, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := validateHTTPCheckURL(context.Background(), value, false); err == nil {
			t.Fatalf("unsafe URL %q was accepted", raw)
		}
	}

}

func TestValidateHTTPCheckURLAllowsExplicitPrivateHost(t *testing.T) {

	value, err := url.Parse("http://127.0.0.1/check")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHTTPCheckURL(context.Background(), value, true); err != nil {
		t.Fatalf("explicit private URL rejected: %v", err)
	}

}
