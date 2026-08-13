package xml

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4:1903 release-candidate fuzz clean-run, §22.4:1908
// "XML/YAML/HCL/binary plist 专项 threat tests"). Discipline mirrors the
// Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2) and the Go
// core/graph/protocol targets of 0.14.0 (G0.5): resource limits are fixed at
// the production defaults, limit failures are passes, and property
// assertions detect closure violations.
//
// The entity face is the XML-specific high-value surface (SECURITY.md:32):
// billion-laughs, cyclic, and deep reference chains must never panic and
// must never fabricate bytes; the document-wide entity accounting
// (declaration/reference counts, expansion depth, expanded bytes, and the
// amplification ratio) bounds every expansion under the default limits.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParse feeds arbitrary bytes to the `xml.1.0-safe@1` parser under the
// production default limits. The parser must never panic and must never
// bypass the limit semantics: a successful parse renders the source
// byte-exactly, a Recovered document publishes diagnostics within the
// MaxDiagnostics budget, and a Complete document is never reported for an
// input the parser rejected.
func FuzzParse(f *testing.F) {
	seeds := [][]byte{
		// Well-formed corpora.
		[]byte(`<?xml version="1.0"?><root a="1"><child>t</child></root>`),
		[]byte(`<!DOCTYPE root [<!ENTITY e "hello">]><root>&e;</root>`),
		[]byte("<root>中文 &amp; 文</root>"),
		[]byte(`<p:root xmlns:p="urn:p"><p:child q:attr="x" xmlns:q="urn:q"/></p:root>`),
		[]byte(`<root>a<child/>b<![CDATA[c]]><!--d--><?pi e?>f</root>`),
		// Entity threat corpora: exponential billion laughs (small), cyclic
		// references, deep chains, linear amplification.
		[]byte(`<!DOCTYPE root [<!ENTITY a "x"><!ENTITY b "&a;&a;&a;">]><root>&b;</root>`),
		[]byte(`<!DOCTYPE root [<!ENTITY a "&b;"><!ENTITY b "&a;">]><root>&a;</root>`),
		[]byte(`<!DOCTYPE root [<!ENTITY e0 "x"><!ENTITY e1 "&e0;&e0;"><!ENTITY e2 "&e1;&e1;">]><root>&e2;</root>`),
		[]byte(`<!DOCTYPE root [<!ENTITY a "xxxxxxxxxxxxxxxxxxxx">]><root>&a;&a;&a;&a;&a;&a;</root>`),
		// Malformed and truncation-prone shapes.
		[]byte(``),
		[]byte(`<`),
		[]byte(`<root>`),
		[]byte(`<root>&unknown;</root>`),
		[]byte(`<!DOCTYPE root SYSTEM "http://x/"><root/>`),
		[]byte(`<!DOCTYPE root [<!ENTITY e SYSTEM "file:///etc/passwd">]><root>&e;</root>`),
		[]byte(`<root a="1"><![CDATA[unterminated`),
		[]byte("\xff"),
		[]byte("\xff\xfe"),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultXmlParseLimits()
		doc, failure := Parse(context.Background(), data, XmlProfileSafeV1,
			XmlEncodingProfileDefault(), limits)
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		if !bytes.Equal(doc.Render(), data) {
			t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %q\noutput: %q",
				len(data), len(doc.Render()), data, doc.Render())
		}
		switch doc.FormationStatus() {
		case document.FormationStatusComplete:
		case document.FormationStatusRecovered:
			if len(doc.Diagnostics()) == 0 {
				t.Fatalf("recovered document without diagnostics")
			}
		default:
			t.Fatalf("unexpected formation status %v", doc.FormationStatus())
		}
		if len(doc.Diagnostics()) > limits.Common.MaxDiagnostics {
			t.Fatalf("diagnostics %d exceed MaxDiagnostics %d",
				len(doc.Diagnostics()), limits.Common.MaxDiagnostics)
		}
	})
}
