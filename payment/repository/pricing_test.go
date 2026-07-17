package repository

import (
	"errors"
	"math/big"
	"testing"
)

func TestConvertReferenceAmount(t *testing.T) {
	tests := []struct {
		name            string
		referenceAmount uint64
		targetScale     uint16
		rate            string
		coefficient     string
		want            uint64
	}{
		{
			name:            "one USDT at two USDT per TON",
			referenceAmount: 1_000_000,
			targetScale:     9,
			rate:            "2",
			coefficient:     "1",
			want:            500_000_000,
		},
		{
			name:            "coefficient",
			referenceAmount: 1_000_000,
			targetScale:     9,
			rate:            "4",
			coefficient:     "1.25",
			want:            312_500_000,
		},
		{
			name:            "micro USDT to nano TON",
			referenceAmount: 15_000,
			targetScale:     9,
			rate:            "1.53",
			coefficient:     "1",
			want:            9_803_922,
		},
		{
			name:            "rounds minor unit upward",
			referenceAmount: 1,
			targetScale:     0,
			rate:            "3",
			coefficient:     "1",
			want:            1,
		},
		{
			name:            "zero amount remains zero",
			referenceAmount: 0,
			targetScale:     9,
			rate:            "2",
			coefficient:     "1",
			want:            0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertReferenceAmount(
				tt.referenceAmount,
				tt.targetScale,
				mustUSDTMinor(t, tt.rate),
				tt.coefficient,
			)
			if err != nil {
				t.Fatalf("convert amount: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected amount: got %d want %d", got, tt.want)
			}
		})
	}
}

func TestConvertReferenceAmountRejectsInvalidDecimals(t *testing.T) {
	_, err := convertReferenceAmount(1, 0, 0, "1")
	if !errors.Is(err, ErrInvalidAssetRate) {
		t.Fatalf("expected invalid rate error, got %v", err)
	}

	_, err = convertReferenceAmount(1, 0, 1_000_000, "-1")
	if !errors.Is(err, ErrInvalidPrice) {
		t.Fatalf("expected invalid price error, got %v", err)
	}
}

func mustUSDTMinor(t *testing.T, value string) uint64 {
	t.Helper()
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		t.Fatalf("invalid test rate %q", value)
	}
	rat.Mul(rat, big.NewRat(1_000_000, 1))
	if !rat.IsInt() || !rat.Num().IsUint64() {
		t.Fatalf("test rate is not exact micro-USDT: %q", value)
	}
	return rat.Num().Uint64()
}
