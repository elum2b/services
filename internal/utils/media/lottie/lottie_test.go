package lottie

import (
	"errors"
	"testing"
)

func TestValidate(t *testing.T) {
	meta, err := Validate(
		[]byte(`{"v":"5.12.0","w":512,"h":256,"ip":0,"op":30,"layers":[]}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Width != 512 || meta.Height != 256 || meta.Out != 30 {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestValidateRejectsExpressionsAndExternalAssets(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"v":"5.12.0","w":1,"h":1,"layers":[],"x":"alert(1)"}`),
		[]byte(`{"v":"5.12.0","w":1,"h":1,"layers":[],"assets":[{"p":"https://example.test/a.png"}]}`),
	} {
		if _, err := Validate(data); !errors.Is(err, ErrUnsafeContent) {
			t.Fatalf("Validate() error = %v", err)
		}
	}
}

func TestValidateRejectsNonLottie(t *testing.T) {
	if _, err := Validate([]byte(`{"w":10,"h":10}`)); err == nil {
		t.Fatal("Validate() accepted a non-Lottie document")
	}
}
