package core

import (
	"bytes"
	"testing"
)

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.14.0 G0.5; docs/go-implementation-plan.md
// §2.1; roadmap §16.1 "Go fuzz targets").
//
// Discipline mirrors the Rust fuzz targets of 0.13.0 (docs/fuzz-evidence-
// 0.13.0.md §2): resource limits are fixed at the production defaults
// (DefaultDecodeLimits/DefaultEncodeLimits), limit failures are passes, and
// property assertions detect encode/decode asymmetry.
// ---------------------------------------------------------------------------

// FuzzPVCE feeds arbitrary bytes to the strict PVCE/1 decoder under the
// production default limits. The decoder must never panic, must never hang
// (the fuzz engine bounds wall time), and must never bypass the limit
// semantics: a successful decode re-encodes to exactly the input bytes
// (decode→encode fixed point — a decoder that accepted a non-canonical
// stream would fail here), and the decoded value must fit within the same
// default limits when re-encoded.
func FuzzPVCE(f *testing.F) {
	seeds := [][]byte{
		// Golden vectors pinned from the Rust codec (README "Golden-bytes
		// provenance"): Null, Integer(-256), {"a": Integer(1)}.
		{0x50, 0x56, 0x43, 0x45, 0x01, 0x00, 0x00},
		{0x50, 0x56, 0x43, 0x45, 0x01, 0x10, 0x04, 0x02, 0x02, 0x01, 0x00},
		{0x50, 0x56, 0x43, 0x45, 0x01, 0x41, 0x0a, 0x01, 0x20, 0x02, 0x01, 0x61, 0x10, 0x03, 0x01, 0x01},
		// Integer boundaries: 2^1016-1 (127-byte magnitude) and its
		// 128-byte magnitude double, positive and negative.
		bytes.Join([][]byte{
			{0x50, 0x56, 0x43, 0x45, 0x01, 0x10, 0x81, 0x01, 0x01, 0x7f},
			bytes.Repeat([]byte{0xff}, 127),
		}, nil),
		bytes.Join([][]byte{
			{0x50, 0x56, 0x43, 0x45, 0x01, 0x10, 0x83, 0x01, 0x02, 0x80, 0x01},
			bytes.Repeat([]byte{0xff}, 128),
		}, nil),
		// A non-minimal varint (decoder must reject).
		{0x50, 0x56, 0x43, 0x45, 0x01, 0x80, 0x00, 0x00},
		// A truncated stream (decoder must fail cleanly).
		{0x50, 0x56, 0x43},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultDecodeLimits()
		value, err := DecodePVCE(data, limits)
		if err != nil {
			return // strict rejection is a pass; limit failures are a pass
		}
		encoded, err := EncodePVCE(value)
		if err != nil {
			t.Fatalf("decode succeeded but re-encode failed: %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf(
				"decode→encode fixed point violated (decoded %d bytes, re-encoded %d):\ninput:  %x\noutput: %x",
				len(data), len(encoded), data, encoded)
		}
		if _, err := EncodePVCEBounded(value, DefaultEncodeLimits()); err != nil {
			t.Fatalf("decoded value exceeds the default encode limits: %v", err)
		}
	})
}

// FuzzPVCEEncodeDecode generates arbitrary legal fifteen-kind values and
// asserts the round-trip identity: decode(encode(x)) == x, Equal holds, and
// re-encoding the decoded value is byte-stable. Any encode/decode asymmetry
// (a value that encodes but cannot decode, or that changes in transit)
// fails here.
func FuzzPVCEEncodeDecode(f *testing.F) {
	f.Add(uint64(1), []byte("seed"))
	f.Add(uint64(0), []byte{})
	f.Add(uint64(0xdeadbeef), []byte("consema"))
	f.Add(uint64(2026), []byte("0.14.0 G0.5"))
	f.Fuzz(func(t *testing.T, seed uint64, blob []byte) {
		value := GenValue(seed, blob)
		encoded, err := EncodePVCE(value)
		if err != nil {
			t.Fatalf("EncodePVCE failed for a generated value: %v", err)
		}
		decoded, err := DecodePVCE(encoded, DefaultDecodeLimits())
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v", err)
		}
		if !Equal(decoded, value) {
			t.Fatalf("decode(encode(x)) != x: got %v, want %v", decoded, value)
		}
		reEncoded, err := EncodePVCE(decoded)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		if !bytes.Equal(reEncoded, encoded) {
			t.Fatalf("re-encode is not byte-stable:\nfirst:  %x\nsecond: %x", encoded, reEncoded)
		}
	})
}
