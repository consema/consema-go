package properties

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4「release-candidate fuzz clean-run」). Discipline
// mirrors the Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2)
// and the Go core/graph/protocol targets of 0.14.0 (G0.5): resource limits
// are fixed at the production defaults, limit failures are passes, and
// property assertions detect closure violations. A successful parse must
// render byte-exactly and a Recovered document must publish diagnostics
// within the diagnostics budget.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the Java Properties reader under the
// production default limits. The parser must never panic and must never
// bypass the limit semantics.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("a=1\nb=two\n"),
		[]byte("a=one\\ttwo\n# comment\n"),
		[]byte("a=1\na=2\n"),
		[]byte("a=" + `\u` + "12\n"), // malformed unicode escape recovers
		[]byte("a=\\u0041\n"),
		[]byte(""),
		[]byte("="),
		[]byte("a"),
		[]byte("a=b c d\\ e\n"),
		[]byte("\xff"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultPropertiesParseLimits()
		doc, failure := ParseReader(data, document.Utf8Encoding(), limits)
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		if !bytes.Equal(doc.Render(), data) {
			t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
				len(data), len(doc.Render()), data, doc.Render())
		}
		if doc.FormationStatus() == document.FormationStatusRecovered &&
			len(doc.Diagnostics()) == 0 {
			t.Fatalf("recovered document without diagnostics")
		}
		if len(doc.Diagnostics()) > limits.Common.MaxDiagnostics {
			t.Fatalf("diagnostics %d exceed MaxDiagnostics %d",
				len(doc.Diagnostics()), limits.Common.MaxDiagnostics)
		}
	})
}
