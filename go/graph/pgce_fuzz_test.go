package graph

import (
	"bytes"
	"testing"
)

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.14.0 G0.5; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.1; roadmap §16.1 "Go fuzz targets"). Discipline mirrors the Rust fuzz
// targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2): resource limits are
// fixed at the production defaults (DefaultPGCELimits), limit failures are
// passes, and property assertions detect encode/decode asymmetry.
// ---------------------------------------------------------------------------

// FuzzPGCE feeds arbitrary bytes to the strict PGCE/1 decoder under the
// production default limits. The decoder must never panic and must never
// bypass the limit semantics: a successful decode re-encodes to exactly the
// input bytes (the decoder's own re-encode check plus this assertion are
// defense-in-depth), and the decoded graph must fit the default encode
// limits.
func FuzzPGCE(f *testing.F) {
	seeds := [][]byte{
		// Golden vectors pinned from the Rust codec (README "Golden-bytes
		// provenance"): empty graph and the scalar "x" stream.
		{0x50, 0x47, 0x43, 0x45, 0x01, 0x00, 0x00},
		{0x50, 0x47, 0x43, 0x45, 0x01, 0x01, 0x01, 0x00, 0x20, 0x15, 0x74, 0x61, 0x67, 0x3a, 0x79, 0x61, 0x6d, 0x6c, 0x2e, 0x6f, 0x72, 0x67, 0x2c, 0x32, 0x30, 0x30, 0x32, 0x3a, 0x73, 0x74, 0x72, 0x01, 0x78},
		// A non-minimal varint (decoder must reject).
		{0x50, 0x47, 0x43, 0x45, 0x01, 0x80, 0x00, 0x00},
		// A truncated stream (decoder must fail cleanly).
		{0x50, 0x47, 0x43},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultPGCELimits()
		decoded, err := DecodePGCE(data, limits)
		if err != nil {
			return // strict rejection is a pass; limit failures are a pass
		}
		encoded, err := EncodePGCE(decoded)
		if err != nil {
			t.Fatalf("decode succeeded but re-encode failed: %v", err)
		}
		if !bytes.Equal(encoded, data) {
			t.Fatalf(
				"decode→encode fixed point violated (decoded %d bytes, re-encoded %d):\ninput:  %x\noutput: %x",
				len(data), len(encoded), data, encoded)
		}
		if _, err := EncodePGCEBounded(decoded, limits); err != nil {
			t.Fatalf("decoded graph exceeds the default encode limits: %v", err)
		}
	})
}

// FuzzPGCEEncodeDecode generates arbitrary valid PortableGraphs (sharing and
// cycles included) and asserts the round-trip identity: decode(encode(g)) is
// Equal to g, and re-encoding the decoded graph is byte-stable. Any
// encode/decode asymmetry fails here.
func FuzzPGCEEncodeDecode(f *testing.F) {
	f.Add(uint64(1), []byte("seed"))
	f.Add(uint64(0), []byte{})
	f.Add(uint64(0xdeadbeef), []byte("consema"))
	f.Add(uint64(2026), []byte("0.14.0 G0.5"))
	f.Fuzz(func(t *testing.T, seed uint64, blob []byte) {
		value := GenGraph(seed, blob)
		encoded, err := EncodePGCE(value)
		if err != nil {
			t.Fatalf("EncodePGCE failed for a generated graph: %v", err)
		}
		decoded, err := DecodePGCE(encoded, DefaultPGCELimits())
		if err != nil {
			t.Fatalf("decode(encode(x)) failed: %v", err)
		}
		if !Equal(decoded, value) {
			t.Fatalf("decode(encode(x)) != x")
		}
		reEncoded, err := EncodePGCE(decoded)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		if !bytes.Equal(reEncoded, encoded) {
			t.Fatalf("re-encode is not byte-stable:\nfirst:  %x\nsecond: %x", encoded, reEncoded)
		}
	})
}
