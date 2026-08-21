package rive

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	if err := Validate([]byte("RIVE\x00\x01")); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsInvalidSignature(t *testing.T) {
	if err := Validate([]byte("RIFF")); !errors.Is(err, ErrNotRive) {
		t.Fatalf("Validate() error = %v", err)
	}
}
