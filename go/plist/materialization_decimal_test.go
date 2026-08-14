package plist

import (
	"math"
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

// Wave-4 R42 regression tests (2026-08-15): decimalToFloat64 must convert
// in a single correctly-rounded pass and fail atomically on out-of-range
// decimals — the two-step coefficient-then-power-of-ten rounding
// double-rounded (7.038531e-26 came out 1 ULP wrong) and the old
// |exponent| > 308 clamping silently fabricated a wrong finite double
// (1e1000 became 1e308, 1e-1000 became 1e-308).
func TestDecimalToFloat64SinglePassRounding(t *testing.T) {
	decimal := core.NewDecimal(big.NewInt(7038531), big.NewInt(-32))
	value, ok := decimalToFloat64(&decimal)
	if !ok {
		t.Fatal("7.038531e-26 must convert")
	}
	if bits := math.Float64bits(value); bits != 0x3ab5c87fb0000000 {
		t.Fatalf("7.038531e-26 = %016x, want 0x3ab5c87fb0000000 (correctly rounded; the two-step rounding gave 0x3bf4c629e5b4c000)", bits)
	}
	// Negative spellings keep the sign.
	negative := core.NewDecimal(big.NewInt(-7038531), big.NewInt(-32))
	value, ok = decimalToFloat64(&negative)
	if !ok || !math.Signbit(value) {
		t.Fatalf("-7.038531e-26 = %v (ok=%v), want a negative double", value, ok)
	}
}

func TestDecimalToFloat64OutOfRangeFailsAtomically(t *testing.T) {
	// |exponent| > 308 fails even when ParseFloat could produce a value
	// (1e-1000 would underflow to 0); in-range exponent but out-of-range
	// value fails too (9e308 overflows to +Inf). A failure must never
	// fabricate a clamped finite double.
	cases := []struct {
		coefficient, exponent int64
	}{
		{1, 1000},  // 1e1000
		{1, -1000}, // 1e-1000
		{1, -309},  // 1e-309 (denormal-representable, but |exp| > 308)
		{9, 308},   // 9e308 overflows binary64
		{1, -308},  // 1e-308 is within range: must convert
	}
	for _, tc := range cases {
		decimal := core.NewDecimal(big.NewInt(tc.coefficient), big.NewInt(tc.exponent))
		value, ok := decimalToFloat64(&decimal)
		if tc.exponent == -308 {
			if !ok {
				t.Errorf("1e-308 must convert (denormal), got ok=false")
			}
			continue
		}
		if ok {
			t.Errorf("decimal %de%d must fail atomically, got %v (ok=true)", tc.coefficient, tc.exponent, value)
		}
	}
}
