package sql

import "testing"

func TestCreateKey_StableAndDifferent(t *testing.T) {
	a := CreateKey("x", 1, true)
	b := CreateKey("x", 1, true)
	c := CreateKey("x", 2, true)

	if a != b {
		t.Fatalf("expected stable key, got %q and %q", a, b)
	}
	if a == c {
		t.Fatalf("expected different key for different parts, got %q", a)
	}
}
