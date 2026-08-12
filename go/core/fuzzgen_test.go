package core

// ---------------------------------------------------------------------------
// Deterministic fifteen-kind value-space generator for the round-trip fuzz
// targets (milestone 0.14.0 G0.5; docs/go-implementation-plan.md §2.1).
//
// The generator lives in package core (test files) because it constructs
// core values: any package that imports core would create an import cycle
// with core's own fuzz tests (the same reason the Rust fuzz drivers live in
// the per-crate fuzz directories, docs/fuzz-evidence-0.13.0.md §2). The
// protocol package's transport fuzz target carries its own value generator
// for the same reason.
//
// The same (seed, blob) input always produces the same value, so a fuzz
// input fully determines its generated value. Every kind is reachable;
// containers stay within depth 6 and 4 entries, blobs within 32 bytes, so
// every generated value encodes under the production default limits.
// ---------------------------------------------------------------------------

import (
	"hash/fnv"
	"math/big"
	"math/rand"
	"strings"
)

// genValue is the deterministic generator state.
type genValue struct {
	r *rand.Rand
}

// newGenValue derives the RNG state from the seed and the blob's FNV-1a
// hash.
func newGenValue(seed uint64, blob []byte) *genValue {
	h := fnv.New64a()
	h.Write(blob)
	return &genValue{r: rand.New(rand.NewSource(int64(seed ^ h.Sum64())))}
}

// GenValue deterministically generates one legal fifteen-kind core value
// (RFC 0016 §4.1). It is exported within the test package for use by
// FuzzPVCEEncodeDecode; it is not SDK API.
func GenValue(seed uint64, blob []byte) Value {
	return newGenValue(seed, blob).value(0)
}

// value generates one value of any of the fifteen kinds.
func (g *genValue) value(depth int) Value {
	if depth >= 6 {
		return g.leaf()
	}
	switch g.r.Intn(15) {
	case 0:
		return NullValue()
	case 1:
		return Boolean(g.r.Intn(2) == 0)
	case 2:
		return String(g.utf8String(32))
	case 3:
		return g.integer()
	case 4:
		return g.decimal()
	case 5:
		return NewBinaryFloat32(g.r.Uint32())
	case 6:
		return NewBinaryFloat64(g.r.Uint64())
	case 7:
		return NewBytes(g.bytes(32))
	case 8:
		return g.date()
	case 9:
		return g.time()
	case 10:
		return NewLocalDateTime(g.date(), g.time())
	case 11:
		return g.offsetDateTime()
	case 12:
		return g.array(depth)
	case 13:
		return g.object(depth)
	default:
		return g.entryMapping(depth)
	}
}

// leaf generates a scalar-kind value (used to bound container nesting).
func (g *genValue) leaf() Value {
	switch g.r.Intn(11) {
	case 0:
		return NullValue()
	case 1:
		return Boolean(g.r.Intn(2) == 0)
	case 2:
		return String(g.utf8String(32))
	case 3:
		return g.integer()
	case 4:
		return g.decimal()
	case 5:
		return NewBinaryFloat32(g.r.Uint32())
	case 6:
		return NewBinaryFloat64(g.r.Uint64())
	case 7:
		return NewBytes(g.bytes(32))
	case 8:
		return g.date()
	case 9:
		return g.time()
	default:
		return g.offsetDateTime()
	}
}

// array generates an array of 0..3 items.
func (g *genValue) array(depth int) Value {
	items := make([]Value, g.r.Intn(4))
	for i := range items {
		items[i] = g.value(depth + 1)
	}
	return NewArray(items...)
}

// object generates an object of 0..3 entries with unique keys.
func (g *genValue) object(depth int) Value {
	builder := NewObjectBuilder()
	seen := make(map[string]struct{})
	for count := g.r.Intn(4); count > 0; count-- {
		key := g.utf8String(16)
		for {
			if _, exists := seen[key]; !exists {
				break
			}
			key = g.utf8String(16)
		}
		seen[key] = struct{}{}
		if err := builder.Insert(key, g.value(depth+1)); err != nil {
			panic("core: Insert failed for a unique generated key")
		}
	}
	return builder.Build()
}

// entryMapping generates an entry mapping of 0..3 associations (duplicates
// allowed, keys arbitrary).
func (g *genValue) entryMapping(depth int) Value {
	builder := NewEntryMappingBuilder()
	for count := g.r.Intn(4); count > 0; count-- {
		if err := builder.Push(g.value(depth+1), g.value(depth+1)); err != nil {
			panic("core: Push failed for generated values")
		}
	}
	return builder.Build()
}

// integer generates a canonical arbitrary integer, including the boundary
// values 0, ±1, 2^64-1, -2^64, and 128-bit magnitudes.
func (g *genValue) integer() Integer {
	switch g.r.Intn(8) {
	case 0:
		return NewInteger(big.NewInt(0))
	case 1:
		return NewInteger(big.NewInt(1))
	case 2:
		return NewInteger(big.NewInt(-1))
	case 3:
		return NewInteger(new(big.Int).SetUint64(g.r.Uint64()))
	case 4:
		return NewInteger(new(big.Int).Neg(new(big.Int).SetUint64(g.r.Uint64())))
	case 5:
		value := new(big.Int).SetUint64(g.r.Uint64())
		value.Lsh(value, 64)
		value.Or(value, new(big.Int).SetUint64(g.r.Uint64()))
		return NewInteger(value)
	case 6:
		return NewInteger(new(big.Int).Sub(
			new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1)))
	default:
		return NewInteger(new(big.Int).Neg(
			new(big.Int).Lsh(big.NewInt(1), 64)))
	}
}

// decimal generates a canonical decimal (NewDecimal normalizes, so any
// coefficient/exponent pair is legal), including extreme exponents.
func (g *genValue) decimal() Decimal {
	var exponent int64
	switch g.r.Intn(6) {
	case 0:
		exponent = int64(g.r.Intn(61) - 30)
	case 1:
		exponent = 100
	case 2:
		exponent = -100
	case 3:
		exponent = 1_000_000
	case 4:
		exponent = -1_000_000
	default:
		exponent = 0
	}
	return NewDecimal(g.integer().Int(), big.NewInt(exponent))
}

// fraction generates a decimal fraction in [0, 1) (Time's fractional second):
// zero, or coefficient with digits + exponent <= 0.
func (g *genValue) fraction() Decimal {
	if g.r.Intn(4) == 0 {
		return NewDecimal(big.NewInt(0), big.NewInt(0))
	}
	digits := 1 + g.r.Intn(9)
	coefficient := int64(g.r.Intn(9) + 1)
	for i := 1; i < digits; i++ {
		coefficient = coefficient*10 + int64(g.r.Intn(10))
	}
	return NewDecimal(big.NewInt(coefficient), big.NewInt(-int64(digits)))
}

// date generates a valid proleptic Gregorian date (day <= 28 is valid for
// every month, so NewDate cannot fail).
func (g *genValue) date() Date {
	year := int64(g.r.Intn(20001) - 10000)
	month := uint8(1 + g.r.Intn(12))
	day := uint8(1 + g.r.Intn(28))
	date, err := NewDate(big.NewInt(year), month, day)
	if err != nil {
		panic("core: NewDate rejected a generator-produced date")
	}
	return date
}

// time generates a valid wall-clock time.
func (g *genValue) time() Time {
	time, err := NewTime(
		uint8(g.r.Intn(24)),
		uint8(g.r.Intn(60)),
		uint8(g.r.Intn(60)),
		g.fraction(),
	)
	if err != nil {
		panic("core: NewTime rejected a generator-produced time")
	}
	return time
}

// offsetDateTime generates a valid offset date-time (|offset| < 24 h).
func (g *genValue) offsetDateTime() OffsetDateTime {
	offset := int32(g.r.Intn(2*86399+1) - 86399)
	value, err := NewOffsetDateTime(
		NewLocalDateTime(g.date(), g.time()), offset)
	if err != nil {
		panic("core: NewOffsetDateTime rejected a generator-produced value")
	}
	return value
}

// bytes generates a raw octet blob (any bytes; Bytes is not text).
func (g *genValue) bytes(max int) []byte {
	out := make([]byte, g.r.Intn(max+1))
	g.r.Read(out)
	return out
}

// utf8String generates a valid UTF-8 string (control characters are legal
// PortableValue string content; the protocol transport escapes them).
func (g *genValue) utf8String(max int) string {
	var builder strings.Builder
	for count := g.r.Intn(max + 1); count > 0; count-- {
		builder.WriteRune(g.rune())
	}
	return builder.String()
}

// rune generates one valid Unicode scalar value (never a surrogate).
func (g *genValue) rune() rune {
	switch g.r.Intn(6) {
	case 0:
		return rune(g.r.Intn(0x20)) // control characters are legal string content
	case 1:
		return rune(0x20 + g.r.Intn(0x5f)) // ASCII printable
	case 2:
		return rune(0x80 + g.r.Intn(0x780)) // two-byte UTF-8
	case 3:
		return rune(0x1000 + g.r.Intn(0xf000)) // three-byte UTF-8
	case 4:
		return rune(0x10000 + g.r.Intn(0x100000)) // four-byte UTF-8
	default:
		return '中'
	}
}
