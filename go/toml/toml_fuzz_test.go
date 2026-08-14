package toml

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4「release-candidate fuzz clean-run」). Discipline
// mirrors the Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2)
// and the Go core/graph/protocol targets of 0.14.0 (G0.5): resource limits
// are fixed at the production defaults, limit failures are passes, and
// property assertions detect closure violations. TOML 1.0 forms only
// complete valid documents: a successful parse must render byte-exactly
// and always report the Complete status with no diagnostics.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the TOML 1.0 parser under the
// production default limits. The parser must never panic and must never
// bypass the limit semantics: a successful parse renders the source
// byte-exactly, is Complete, and carries no diagnostics.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("a = 1\nb = [2, 3]\n[c]\nd = 4\n"),
		[]byte("[[products]]\nname = 'one'\n[[products]]\nname = 'two'\n"),
		[]byte("key = \"unterminated"),
		[]byte("key = ["),
		[]byte("key = [[[[[[[[1]]]]]]]]"),
		[]byte("a.b.c = { x = [1, 2, 3] }"),
		[]byte("value = 999999999999999999999999999999999999"),
		[]byte("time = 23:59:60"),
		[]byte("value = [nan, inf, -inf, -0.0]"),
		[]byte("ключ = 'значение'"),
		[]byte("\xef\xbb\xbfkey = 1"),
		[]byte("\xff"),
		[]byte(""),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := document.DefaultParseLimits()
		doc, failure := Parse(data, Toml10V1, limits)
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		if !bytes.Equal(doc.Render(), data) {
			t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
				len(data), len(doc.Render()), data, doc.Render())
		}
		if doc.FormationStatus() != document.FormationStatusComplete {
			t.Fatalf("TOML formed a non-complete document: %v", doc.FormationStatus())
		}
		if len(doc.Diagnostics()) != 0 {
			t.Fatalf("complete TOML document carries %d diagnostics", len(doc.Diagnostics()))
		}
	})
}
