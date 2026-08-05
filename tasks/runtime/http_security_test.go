package runtime

import (
	"context"
	"net/url"
	"testing"
)

func TestValidatePartnerURLRejectsUnsafeTargets(t *testing.T) {

	for _, raw := range []string{
		"http://example.com/path",
		"https://127.0.0.1/path",
		"https://10.0.0.1/path",
		"https://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	} {
		value, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if err := validatePartnerURL(context.Background(), value, false); err == nil {
			t.Fatalf("unsafe URL %q was accepted", raw)
		}
	}

}

func TestValidatePartnerURLAllowsExplicitTrustedPrivateNetwork(t *testing.T) {

	value, err := url.Parse("https://127.0.0.1/path")
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePartnerURL(context.Background(), value, true); err != nil {
		t.Fatalf("explicit trusted private URL: %v", err)
	}

}
