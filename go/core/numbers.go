package core

import (
	"fmt"
	"math"
	"math/big"
)

// Integer is a canonical arbitrary-precision integer (RFC 0016 §4.1; the
// BigInteger wrapper). The wrapped value is always canonical: zero has sign
// 0 and an empty magnitude; a non-zero magnitude is minimal big-endian with
// no leading zero octets (math/big invariants).
type Integer struct {
	value *big.Int
}

// NewInteger wraps value, copying it; nil is treated as zero. The returned
// Integer is independent of the caller's big.Int.
func NewInteger(value *big.Int) Integer {
	if value == nil {
		value = new(big.Int)
	}
	return Integer{value: new(big.Int).Set(value)}
}

// Int returns a copy of the integer value.
func (i Integer) Int() *big.Int {
	return new(big.Int).Set(i.safeValue())
}

// Signum returns -1, 0, or 1.
func (i Integer) Signum() int {
	return i.safeValue().Sign()
}

// String returns the base-ten representation.
func (i Integer) String() string {
	return i.safeValue().String()
}

// Kind implements Value.
func (Integer) Kind() Kind { return KindInteger }

func (Integer) isValue() {}

// safeValue returns the wrapped integer, treating the zero value as zero.
func (i Integer) safeValue() *big.Int {
	if i.value == nil {
		return new(big.Int)
	}
	return i.value
}

// magnitude returns the minimal big-endian magnitude octets.
func (i Integer) magnitude() []byte {
	return i.safeValue().Bytes()
}

// Decimal is a canonical exact finite decimal, coefficient × 10^exponent
// (RFC 0016 §4.1; no float round-trip). The canonical form mirrors the Rust
// Decimal::new normalization (consema-rs/consema-core/src/value.rs): a
// zero coefficient has exponent zero, and trailing decimal zeros of the
// coefficient are stripped into the exponent (10 × 10^0 → 1 × 10^1).
type Decimal struct {
	coefficient *big.Int
	exponent    *big.Int
}

// NewDecimal builds a canonical decimal, copying both operands (nil means
// zero). The zero decimal is 0 × 10^0 regardless of the given exponent.
func NewDecimal(coefficient, exponent *big.Int) Decimal {
	if coefficient == nil {
		coefficient = new(big.Int)
	}
	if exponent == nil {
		exponent = new(big.Int)
	}
	c := new(big.Int).Set(coefficient)
	e := new(big.Int).Set(exponent)
	if c.Sign() == 0 {
		return Decimal{coefficient: c, exponent: new(big.Int)}
	}
	ten := big.NewInt(10)
	one := big.NewInt(1)
	for {
		var quotient, remainder big.Int
		quotient.QuoRem(c, ten, &remainder)
		if remainder.Sign() != 0 {
			break
		}
		c = &quotient
		e.Add(e, one)
	}
	return Decimal{coefficient: c, exponent: e}
}

// Coefficient returns a copy of the canonical coefficient.
func (d Decimal) Coefficient() *big.Int {
	return new(big.Int).Set(d.safeCoefficient())
}

// Exponent returns a copy of the canonical exponent.
func (d Decimal) Exponent() *big.Int {
	return new(big.Int).Set(d.safeExponent())
}

// String returns a human-presentation form; it is not a language-neutral
// fact and never participates in conformance comparison.
func (d Decimal) String() string {
	return fmt.Sprintf("coefficient=%s exponent=%s", d.safeCoefficient(), d.safeExponent())
}

// Kind implements Value.
func (Decimal) Kind() Kind { return KindDecimal }

func (Decimal) isValue() {}

// safeCoefficient returns the coefficient, treating the zero value as zero.
func (d Decimal) safeCoefficient() *big.Int {
	if d.coefficient == nil {
		return new(big.Int)
	}
	return d.coefficient
}

// safeExponent returns the exponent, treating the zero value as zero.
func (d Decimal) safeExponent() *big.Int {
	if d.exponent == nil {
		return new(big.Int)
	}
	return d.exponent
}

// BinaryFloat64 is an exact IEEE-754 binary64 datum (RFC 0016 §4.1). The
// identity of a BinaryFloat64 is its 64-bit pattern: NaN payloads and the
// sign of zero are preserved exactly, and PVCE/1 encodes the bits
// big-endian.
type BinaryFloat64 uint64

// NewBinaryFloat64 wraps the exact bit pattern.
func NewBinaryFloat64(bits uint64) BinaryFloat64 { return BinaryFloat64(bits) }

// FromFloat64 wraps the exact bit pattern of f.
func FromFloat64(f float64) BinaryFloat64 { return BinaryFloat64(math.Float64bits(f)) }

// Bits returns the exact bit pattern.
func (b BinaryFloat64) Bits() uint64 { return uint64(b) }

// Float64 converts back to a float64 without changing the bit pattern.
func (b BinaryFloat64) Float64() float64 { return math.Float64frombits(uint64(b)) }

// Kind implements Value.
func (BinaryFloat64) Kind() Kind { return KindBinaryFloat64 }

func (BinaryFloat64) isValue() {}

// BinaryFloat32 is an exact IEEE-754 binary32 datum (配置内容统一处理标准与
// Rust 参考实现.md §10.3; the Rust BinaryFloat32,
// consema-rs/consema-core/src/value.rs). The identity of a BinaryFloat32
// is its 32-bit pattern: NaN payloads and the sign of zero are preserved
// exactly, and PVCE/1 encodes the bits big-endian. BinaryFloat32 and
// BinaryFloat64 are always different kinds.
type BinaryFloat32 uint32

// NewBinaryFloat32 wraps the exact bit pattern.
func NewBinaryFloat32(bits uint32) BinaryFloat32 { return BinaryFloat32(bits) }

// FromFloat32 wraps the exact bit pattern of f.
func FromFloat32(f float32) BinaryFloat32 { return BinaryFloat32(math.Float32bits(f)) }

// Bits returns the exact bit pattern.
func (b BinaryFloat32) Bits() uint32 { return uint32(b) }

// Float32 converts back to a float32 without changing the bit pattern.
func (b BinaryFloat32) Float32() float32 { return math.Float32frombits(uint32(b)) }

// Kind implements Value.
func (BinaryFloat32) Kind() Kind { return KindBinaryFloat32 }

func (BinaryFloat32) isValue() {}
