package protocol

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.14.0 G0.5; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.1; roadmap §16.1 "Go fuzz targets"). Discipline mirrors the Rust fuzz
// targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2): resource limits are
// fixed at the production defaults (DefaultProtocolLimits), limit failures
// are passes, and property assertions detect encode/decode asymmetry.
//
// The round-trip target carries its own deterministic fifteen-kind value
// generator (test files only): a shared generator would create an import
// cycle, since the generator must construct core values (the same reason
// the Rust fuzz drivers live in the per-crate fuzz directories). The
// generator mirrors go/core's fuzzgen_test.go.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"hash/fnv"
	"math/big"
	"math/rand"
	"strings"
	"testing"

	"consema.dev/consema/core"
)

// FuzzCanonicalJSON feeds arbitrary bytes to the strict canonical tagged JSON
// transport decoder (`core.portable-value-json@1`) under the production
// default limits. The decoder must never panic and must never bypass the
// limit semantics: a successful decode re-encodes to exactly the input bytes
// (decode→encode fixed point — the strict JSON parser plus the canonicality
// re-encode check must agree), and the decoded value must fit the same
// default limits when re-encoded.
func FuzzCanonicalJSON(f *testing.F) {
	seeds := [][]byte{
		// Golden transport vectors pinned from the Rust encoder
		// (fifteen_transport_test.go; conformance/vectors/protocol-v1.json
		// "protocol.json.null-vector").
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Null"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"BinaryFloat32","bits":"7fc00001"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Bytes","hex":"0001feff"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Date","year":"-44","month":"3","day":"15"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Time","hour":"12","minute":"34","second":"56","fraction":{"type":"Decimal","coefficient":"125","exponent":"-3"}}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"OffsetDateTime","local":{"type":"LocalDateTime","date":{"type":"Date","year":"-44","month":"3","day":"15"},"time":{"type":"Time","hour":"12","minute":"34","second":"56","fraction":{"type":"Decimal","coefficient":"125","exponent":"-3"}}},"offset_seconds":"-90"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"EntryMapping","entries":[{"key":{"type":"String","value":"k"},"value":{"type":"Null"}}]}}`),
		// Rejection seeds: whitespace, alternate escape, unknown field.
		[]byte(`{"schema":"core.portable-value-json@1", "value":{"type":"Null"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"String","value":"a"}}`),
		[]byte(`{"schema":"core.portable-value-json@1","value":{"type":"Null"},"extra":1}`),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultProtocolLimits()
		value, err := DecodeJSON(data, limits)
		if err != nil {
			return // strict rejection is a pass; limit failures are a pass
		}
		encoded, err := EncodeJSON(value, limits)
		if err != nil {
			t.Fatalf("decode succeeded but re-encode failed: %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf(
				"decode→encode fixed point violated (decoded %d bytes, re-encoded %d):\ninput:  %s\noutput: %s",
				len(data), len(encoded), data, encoded)
		}
	})
}

// FuzzJSONEncodeDecode generates arbitrary legal fifteen-kind values and
// asserts the canonical JSON transport round-trip identity:
// decode(encode(x)) == x, Equal holds, and re-encoding the decoded value is
// byte-stable. Any encode/decode asymmetry of the transport (escaping,
// number normalization, hex casing, tagged forms) fails here.
func FuzzJSONEncodeDecode(f *testing.F) {
	f.Add(uint64(1), []byte("seed"))
	f.Add(uint64(0), []byte{})
	f.Add(uint64(0xdeadbeef), []byte("consema"))
	f.Add(uint64(2026), []byte("0.14.0 G0.5"))
	f.Fuzz(func(t *testing.T, seed uint64, blob []byte) {
		value := genTransportValue(seed, blob)
		limits := DefaultProtocolLimits()
		encoded, err := EncodeJSON(value, limits)
		if err != nil {
			t.Fatalf("EncodeJSON failed for a generated value: %v", err)
		}
		decoded, err := DecodeJSON(encoded, limits)
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v", err)
		}
		if !core.Equal(decoded, value) {
			t.Fatalf("decode(encode(x)) != x: got %v, want %v", decoded, value)
		}
		reEncoded, err := EncodeJSON(decoded, limits)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		if !bytes.Equal(reEncoded, encoded) {
			t.Fatalf("re-encode is not byte-stable:\nfirst:  %s\nsecond: %s", encoded, reEncoded)
		}
	})
}

// genTransportValue deterministically generates one legal fifteen-kind core
// value from the seed and blob (the same (seed, blob) always produces the
// same value). Every kind is reachable; containers stay within depth 6 and 4
// entries, blobs within 32 bytes, so every generated value encodes under
// DefaultProtocolLimits.
func genTransportValue(seed uint64, blob []byte) core.Value {
	h := fnv.New64a()
	h.Write(blob)
	g := &transportGen{r: rand.New(rand.NewSource(int64(seed ^ h.Sum64())))}
	return g.value(0)
}

// transportGen is the deterministic generator state.
type transportGen struct {
	r *rand.Rand
}

// value generates one value of any of the fifteen kinds.
func (g *transportGen) value(depth int) core.Value {
	if depth >= 6 {
		return g.leaf()
	}
	switch g.r.Intn(15) {
	case 0:
		return core.NullValue()
	case 1:
		return core.Boolean(g.r.Intn(2) == 0)
	case 2:
		return core.String(g.utf8String(32))
	case 3:
		return g.integer()
	case 4:
		return g.decimal()
	case 5:
		return core.NewBinaryFloat32(g.r.Uint32())
	case 6:
		return core.NewBinaryFloat64(g.r.Uint64())
	case 7:
		return core.NewBytes(g.bytes(32))
	case 8:
		return g.date()
	case 9:
		return g.time()
	case 10:
		return core.NewLocalDateTime(g.date(), g.time())
	case 11:
		return g.offsetDateTime()
	case 12:
		items := make([]core.Value, g.r.Intn(4))
		for i := range items {
			items[i] = g.value(depth + 1)
		}
		return core.NewArray(items...)
	case 13:
		builder := core.NewObjectBuilder()
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
				panic("protocol: Insert failed for a unique generated key")
			}
		}
		return builder.Build()
	default:
		builder := core.NewEntryMappingBuilder()
		for count := g.r.Intn(4); count > 0; count-- {
			if err := builder.Push(g.value(depth+1), g.value(depth+1)); err != nil {
				panic("protocol: Push failed for generated values")
			}
		}
		return builder.Build()
	}
}

// leaf generates a scalar-kind value (used to bound container nesting).
func (g *transportGen) leaf() core.Value {
	switch g.r.Intn(11) {
	case 0:
		return core.NullValue()
	case 1:
		return core.Boolean(g.r.Intn(2) == 0)
	case 2:
		return core.String(g.utf8String(32))
	case 3:
		return g.integer()
	case 4:
		return g.decimal()
	case 5:
		return core.NewBinaryFloat32(g.r.Uint32())
	case 6:
		return core.NewBinaryFloat64(g.r.Uint64())
	case 7:
		return core.NewBytes(g.bytes(32))
	case 8:
		return g.date()
	case 9:
		return g.time()
	default:
		return g.offsetDateTime()
	}
}

// integer generates a canonical arbitrary integer, including the boundary
// values 0, ±1, 2^64-1, -2^64, and 128-bit magnitudes.
func (g *transportGen) integer() core.Integer {
	switch g.r.Intn(8) {
	case 0:
		return core.NewInteger(big.NewInt(0))
	case 1:
		return core.NewInteger(big.NewInt(1))
	case 2:
		return core.NewInteger(big.NewInt(-1))
	case 3:
		return core.NewInteger(new(big.Int).SetUint64(g.r.Uint64()))
	case 4:
		return core.NewInteger(new(big.Int).Neg(new(big.Int).SetUint64(g.r.Uint64())))
	case 5:
		value := new(big.Int).SetUint64(g.r.Uint64())
		value.Lsh(value, 64)
		value.Or(value, new(big.Int).SetUint64(g.r.Uint64()))
		return core.NewInteger(value)
	case 6:
		return core.NewInteger(new(big.Int).Sub(
			new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1)))
	default:
		return core.NewInteger(new(big.Int).Neg(
			new(big.Int).Lsh(big.NewInt(1), 64)))
	}
}

// decimal generates a canonical decimal, including extreme exponents.
func (g *transportGen) decimal() core.Decimal {
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
	return core.NewDecimal(g.integer().Int(), big.NewInt(exponent))
}

// fraction generates a decimal fraction in [0, 1) (Time's fractional second).
func (g *transportGen) fraction() core.Decimal {
	if g.r.Intn(4) == 0 {
		return core.NewDecimal(big.NewInt(0), big.NewInt(0))
	}
	digits := 1 + g.r.Intn(9)
	coefficient := int64(g.r.Intn(9) + 1)
	for i := 1; i < digits; i++ {
		coefficient = coefficient*10 + int64(g.r.Intn(10))
	}
	return core.NewDecimal(big.NewInt(coefficient), big.NewInt(-int64(digits)))
}

// date generates a valid proleptic Gregorian date (day <= 28 is valid for
// every month, so NewDate cannot fail).
func (g *transportGen) date() core.Date {
	year := int64(g.r.Intn(20001) - 10000)
	month := uint8(1 + g.r.Intn(12))
	day := uint8(1 + g.r.Intn(28))
	date, err := core.NewDate(big.NewInt(year), month, day)
	if err != nil {
		panic("protocol: NewDate rejected a generator-produced date")
	}
	return date
}

// time generates a valid wall-clock time.
func (g *transportGen) time() core.Time {
	time, err := core.NewTime(
		uint8(g.r.Intn(24)),
		uint8(g.r.Intn(60)),
		uint8(g.r.Intn(60)),
		g.fraction(),
	)
	if err != nil {
		panic("protocol: NewTime rejected a generator-produced time")
	}
	return time
}

// offsetDateTime generates a valid offset date-time (|offset| < 24 h).
func (g *transportGen) offsetDateTime() core.OffsetDateTime {
	offset := int32(g.r.Intn(2*86399+1) - 86399)
	value, err := core.NewOffsetDateTime(
		core.NewLocalDateTime(g.date(), g.time()), offset)
	if err != nil {
		panic("protocol: NewOffsetDateTime rejected a generator-produced value")
	}
	return value
}

// bytes generates a raw octet blob.
func (g *transportGen) bytes(max int) []byte {
	out := make([]byte, g.r.Intn(max+1))
	g.r.Read(out)
	return out
}

// utf8String generates a valid UTF-8 string (control characters are legal
// PortableValue string content; the transport escapes them).
func (g *transportGen) utf8String(max int) string {
	var builder strings.Builder
	for count := g.r.Intn(max + 1); count > 0; count-- {
		builder.WriteRune(g.rune())
	}
	return builder.String()
}

// rune generates one valid Unicode scalar value (never a surrogate).
func (g *transportGen) rune() rune {
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
