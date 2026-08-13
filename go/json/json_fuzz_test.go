package json

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4:1908 release-candidate fuzz clean-run, §22.4:1911
// "XML/YAML/HCL/binary plist 专项 threat tests"; G114 line re-verification
// 2026-08-13). Discipline mirrors the
// Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2) and the Go
// core/graph/protocol targets of 0.14.0 (G0.5): resource limits are fixed at
// the production defaults, limit failures are passes, and property
// assertions detect closure violations. A successful parse must render
// byte-exactly (the parser must never fabricate, drop, or normalize bytes),
// the closed formation-status contract must hold, and a Recovered document
// must publish at least one diagnostic within the diagnostics budget.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the JSON family parser under all three
// profiles (strict, JSONC, JSON5) with the production default limits. The
// parser must never panic and must never bypass the limit semantics: a
// successful parse renders the source byte-exactly, its status is one of
// the two closed values, diagnostics stay within the MaxDiagnostics budget,
// and a Recovered document carries at least one diagnostic.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		// Strict goldens.
		[]byte(`null`),
		[]byte(`{"a":1,"b":[2,3],"c":{"d":4}}`),
		[]byte(`[1,2,3]`),
		[]byte(`"строка é"`),
		[]byte(`{"a":1,}`), // strict recovery: one diagnostic
		// JSONC seeds (comments, trailing commas).
		[]byte("// comment\n{\"a\":1,}"),
		[]byte("/* block */ [1,2,3,]"),
		// JSON5 seeds (unquoted keys, hex, Infinity, trailing commas).
		[]byte(`{a:1,hex:0x2A,b:'str'}`),
		[]byte(`{a:+Infinity,b:-NaN,c:1e999999999999999999999999999999}`),
		[]byte(`{a:'\uD800'}`),
		[]byte(`{π:1}`),
		// Malformed and truncation-prone shapes.
		[]byte(`[`),
		[]byte(`{"a":`),
		[]byte(`[[[[[[[[[[[[[[[[[[[[`),
		[]byte(`[,,,,,,,,]`),
		[]byte("\xef\xbb\xbfnull"),
		[]byte("\xff"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, profile := range []JsonProfile{
			JsonProfileStrictV1, JsonProfileJsoncBoundedV1, JsonProfileJson5StandardV1,
		} {
			limits := document.DefaultParseLimits()
			doc, failure := Parse(context.Background(), data, profile, limits)
			if failure != nil {
				continue // fatal formation (incl. limit failures) is a pass
			}
			if !bytes.Equal(doc.Render(), data) {
				t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
					len(data), len(doc.Render()), data, doc.Render())
			}
			switch doc.FormationStatus() {
			case document.FormationStatusComplete:
				// Complete under strict JSON may still carry recovery
				// diagnostics; nothing further is asserted here.
			case document.FormationStatusRecovered:
				if len(doc.Diagnostics()) == 0 {
					t.Fatalf("recovered document without diagnostics (profile %d)", profile)
				}
			default:
				t.Fatalf("unexpected formation status %v", doc.FormationStatus())
			}
			if len(doc.Diagnostics()) > limits.MaxDiagnostics {
				t.Fatalf("diagnostics %d exceed MaxDiagnostics %d", len(doc.Diagnostics()), limits.MaxDiagnostics)
			}
		}
	})
}

// Assert that the seed corpus is deterministic: every seed that parses under
// any profile renders byte-exactly (exercised by the fuzz target's seed
// pass; this test pins the same property under plain `go test`).
func TestFuzzSeedRenderClosure(t *testing.T) {
	seeds := [][]byte{
		[]byte(`{"a":1,"b":[2,3],"c":{"d":4}}`),
		[]byte("// comment\n{\"a\":1,}"),
		[]byte(`{a:1,hex:0x2A,b:'str'}`),
	}
	for _, seed := range seeds {
		for _, profile := range []JsonProfile{
			JsonProfileStrictV1, JsonProfileJsoncBoundedV1, JsonProfileJson5StandardV1,
		} {
			doc, failure := Parse(context.Background(), seed, profile, document.DefaultParseLimits())
			if failure != nil {
				continue
			}
			if !bytes.Equal(doc.Render(), seed) {
				t.Fatalf("seed render closure violated under %d", profile)
			}
		}
	}
}
