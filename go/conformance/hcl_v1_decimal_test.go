package conformance

import (
	"math"
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

// Wave-4 R42 regression tests (2026-08-15): the HCL runner's
// decimalToFloat64 / decimalValueToFloat64 must convert in a single
// correctly-rounded pass and fail atomically on out-of-range decimals.
// The old two-step coefficient-then-power-of-ten rounding double-rounded
// (7.038531e-26 came out 1 ULP wrong), and |exponent| > 308 was silently
// clamped to ±308 with ok=true.
func TestHCLDecimalToFloat64SinglePassRounding(t *testing.T) {
	value, ok := decimalToFloat64("7.038531e-26")
	if !ok {
		t.Fatal("7.038531e-26 must convert")
	}
	if bits := math.Float64bits(value); bits != 0x3ab5c87fb0000000 {
		t.Fatalf("7.038531e-26 = %016x, want 0x3ab5c87fb0000000 (correctly rounded; the two-step rounding gave 0x3bf4c629e5b4c000)", bits)
	}
	decimal := core.NewDecimal(big.NewInt(7038531), big.NewInt(-32))
	value, ok = decimalValueToFloat64(&decimal)
	if !ok {
		t.Fatal("Decimal(7038531, -32) must convert")
	}
	if bits := math.Float64bits(value); bits != 0x3ab5c87fb0000000 {
		t.Fatalf("Decimal(7038531, -32) = %016x, want 0x3ab5c87fb0000000", bits)
	}
	// A plain fractional spelling without an exponent still converts.
	value, ok = decimalToFloat64("0.5")
	if !ok || value != 0.5 {
		t.Fatalf("0.5 = %v (ok=%v), want 0.5", value, ok)
	}
}

func TestHCLDecimalToFloat64OutOfRangeFailsAtomically(t *testing.T) {
	for _, text := range []string{
		"1e1000", "1e-1000", "1e-309", "9e308",
		"1e100000000000000000000", // exponent beyond int64
	} {
		if value, ok := decimalToFloat64(text); ok {
			t.Errorf("decimal %q must fail atomically, got %v (ok=true)", text, value)
		}
	}
	decimal := core.NewDecimal(big.NewInt(1), big.NewInt(1000))
	if value, ok := decimalValueToFloat64(&decimal); ok {
		t.Errorf("Decimal(1, 1000) must fail atomically, got %v (ok=true)", value)
	}
	// 1e-308 is within the |exp| <= 308 rule and converts (denormal).
	value, ok := decimalToFloat64("1e-308")
	if !ok {
		t.Fatal("1e-308 must convert (denormal)")
	}
	if math.Float64bits(value) != 0x000730d67819e8d2 {
		t.Fatalf("1e-308 = %016x, want 0x000730d67819e8d2", math.Float64bits(value))
	}
}
