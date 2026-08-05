package utils

import "testing"

func TestRefPreservesValue(t *testing.T) {

	value := "subscription-id"
	ref := Ref(value)
	if ref == nil || *ref != value {
		t.Fatalf("Ref value = %v, want %q", ref, value)
	}

}
