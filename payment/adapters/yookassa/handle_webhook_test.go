package yookassa

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/elum2b/services/payment/repository"
)

func TestHandleWebhookRejectsInvalidSignatureBeforePayloadParsing(t *testing.T) {
	adapter := &YooKassa{repository: &repository.PaymentRepository{}}

	_, err := adapter.HandleWebhook(context.Background(), WebhookRequest{
		WorkspaceID:    "00000000-0000-0000-0000-000000000000",
		Raw:            []byte(`not-json`),
		SignatureValid: false,
	})
	if err == nil {
		t.Fatal("expected invalid signature to fail")
	}
	if !errors.Is(err, ErrWebhookSignatureInvalid) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseRubAmountRejectsOverflow(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    uint64
		wantErr bool
	}{
		{
			name:  "maximum int64 minor amount",
			value: "92233720368547758.07",
			want:  uint64(math.MaxInt64),
		},
		{
			name:    "one minor unit above int64",
			value:   "92233720368547758.08",
			wantErr: true,
		},
		{
			name:    "uint64 wraparound",
			value:   "184467440737095517.16",
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRubAmount(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("parseRubAmount(%q) succeeded with %d", test.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRubAmount(%q): %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("parseRubAmount(%q) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
