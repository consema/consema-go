package hcl

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4「release-candidate fuzz clean-run」与「XML/YAML/HCL/
// binary plist 专项 threat tests」——G050，对抗审计 2026-08-14：改引节锚，
// 行号删除). Discipline mirrors the
// Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2) and the Go
// core/graph/protocol targets of 0.14.0 (G0.5): resource limits are fixed at
// the production defaults, limit failures are passes, and property
// assertions detect closure violations.
//
// The heredoc/template face is the HCL-specific high-value surface
// (SECURITY.md「hcl 对抗/边界覆盖」段——G057，对抗审计 2026-08-14：旧行号 :36
// 指向 plist 段，改引节锚): heredoc, template, and expression nesting are bounded
// by the frozen depth budgets (`hcl.limit.*@1`), and the parse of any
// adversarial input must never panic. A successful parse must render
// byte-exactly; a Recovered document publishes diagnostics within the
// budget.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the HCL parser under both profiles
// (native and tfvars) with the production default limits.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		[]byte("region = \"us-east-1\"\nserver \"web\" {\n  port = 8080\n}\ncount = 3\n"),
		[]byte("# comment\na = \"${x}\"\nb = [1, 2, {k = \"v\"}]\nc = <<EOT\nline ${y}\nEOT\n"),
		[]byte("a = \"中文 & 文\"\nb = 1e3\nc = true ? \"yes\" : \"no\"\n"),
		[]byte("a = \"bad \\q\"\nb = \"\\u0041\"\n"),
		[]byte("terraform {\n  required_version = \">= 1.5\"\n}\nvariable \"region\" {\n  type    = string\n  default = \"us-east-1\"\n}\n"),
		[]byte("a = \"%{ if x }${x}%{ endif }\"\nb = [for v in x : v if v > 1]\n"),
		[]byte("h = <<-EOT\n  indented\n  EOT\ni = foo.*.bar\nj = foo[*].baz\n"),
		// Heredoc and template threat shapes.
		[]byte("a = <<EOT\ncontent\n"),
		[]byte("a = \"${1 +\""),
		[]byte("a = \"${${${${1}}}}\"\n"),
		[]byte("a = (1\n"),
		[]byte("a = [1, 2\n"),
		[]byte("a = 1 @ 2\n"),
		[]byte("a = \"unterminated"),
		[]byte("a = 1 /* unterminated"),
		[]byte("a = 1\rb = 2\n"),
		[]byte("\xef\xbb\xbfa = 1\n"),
		[]byte("a = \xff\n"),
		[]byte(""),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, profile := range []HclProfile{HclProfileNativeV1, HclProfileTfvarsV1} {
			limits := DefaultHclParseLimits()
			doc, failure := Parse(context.Background(), data, profile,
				HclEncodingSelectionProfileDefault(), limits)
			if failure != nil {
				continue // fatal formation (incl. limit failures) is a pass
			}
			if !bytes.Equal(doc.Render(), data) {
				t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
					len(data), len(doc.Render()), data, doc.Render())
			}
			switch doc.FormationStatus() {
			case document.FormationStatusComplete:
			case document.FormationStatusRecovered:
				if len(doc.Diagnostics()) == 0 {
					t.Fatalf("recovered document without diagnostics (profile %d)", profile)
				}
			default:
				t.Fatalf("unexpected formation status %v", doc.FormationStatus())
			}
			if len(doc.Diagnostics()) > limits.Common.MaxDiagnostics {
				t.Fatalf("diagnostics %d exceed MaxDiagnostics %d",
					len(doc.Diagnostics()), limits.Common.MaxDiagnostics)
			}
		}
	})
}
