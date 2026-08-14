package plist

// ---------------------------------------------------------------------------
// Go native fuzz targets (milestone 0.19.0 G5.4; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
// §2.6; roadmap §22.4:1903 release-candidate fuzz clean-run, §22.4:1908
// "XML/YAML/HCL/binary plist 专项 threat tests"). Discipline mirrors the
// Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2) and the Go
// core/graph/protocol targets of 0.14.0 (G0.5): resource limits are fixed at
// the production defaults, limit failures are passes, and property
// assertions detect closure violations.
//
// The binary face is the plist-specific high-value surface (SECURITY.md;
// RFC 0013 §5): object tables, offset tables, and trailers are adversarial
// inputs, and the out-of-bounds/cycle/overflow recovery must never panic,
// never fabricate, and never fake a Complete document. A successful parse
// of either profile must render byte-exactly.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"encoding/hex"
	"testing"

	"consema.dev/consema/document"
)

// FuzzParseXML feeds arbitrary bytes to the `plist.xml@1` parser under the
// production default limits.
func FuzzParseXML(f *testing.F) {
	seeds := [][]byte{
		[]byte(`<plist version="1.0"><string>ok</string></plist>`),
		[]byte(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><string>ok</string></plist>`),
		[]byte(`<plist version="1.0"><dict><key>name</key><string>Consema</string><key>payload</key><data>AQID</data><key>tags</key><array><string>a</string><dict/></array></dict></plist>`),
		[]byte("<plist version=\"1.0\"><string>中文 &amp; 文</string></plist>"),
		[]byte(`<plist version="1.0"><string>&unknown;</string></plist>`),
		[]byte(`<plist version="1.0"><dict><key>a</key></dict></plist>`),
		[]byte(`<plist version="1.0"><string>`),
		[]byte(`<!DOCTYPE plist SYSTEM "http://x/"><plist version="1.0"/>`),
		[]byte(`<plist version="1.0"><integer>12a</integer></plist>`),
		[]byte(`<plist version="1.0"><date>2024-02-30T00:00:00Z</date></plist>`),
		[]byte("\xff"),
		[]byte(""),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultPlistParseLimits()
		doc, failure := Parse(data, PlistProfileXmlV1, PlistEncodingProfileDefault(), limits)
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		assertParseClosure(t, data, doc)
	})
}

// FuzzParseBinary feeds arbitrary bytes to the `plist.binary@1` parser
// under the production default limits. The seeds are the frozen bplist
// corpora of the Rust hardening suite (plist_hardening.rs) plus the
// recovered-binary shapes; the trailer/offset/object-table accounting must
// never panic and must never report a limit breach as a Complete document.
func FuzzParseBinary(f *testing.F) {
	hexSeeds := []string{
		"62706c697374303050080000000000000101000000000000000100000000000000000000000000000009",
		"62706c6973743030d1010251611001080b0d000000000000010100000000000000030000000000000000000000000000000f",
		"62706c6973743030a3010102d103045178516b5176080c0f11130000000000000101000000000000000500000000000000000000000000000015",
		"62706c6973743030d201010203516b10011002080d0f110000000000000101000000000000000400000000000000000000000000000013",
		"62706c6973743030a2010210015162080b0d000000000000010100000000000000030000000000000000000000000000000f",
		// Recovered binary shapes (bad header/version, self-referencing
		// tables, unproven top object).
		"62706c697374303000080000000000000101000000000000000100000000000000000000000000000009",
		"62706c697374303050500805000000000000010100000000000000020000000000000000000000000000000a",
		"62706c69737430305150080000000000000101000000000000000100000000000000000000000000000009",
	}
	for _, hexSeed := range hexSeeds {
		seed, err := hex.DecodeString(hexSeed)
		if err != nil {
			panic("plist: bad fuzz seed hex")
		}
		f.Add(seed)
	}
	f.Add([]byte("bplist00"))
	f.Add([]byte(""))
	f.Add([]byte{0xff, 0xfe, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		limits := DefaultPlistParseLimits()
		doc, failure := Parse(data, PlistProfileBinaryV1, PlistEncodingProfileDefault(), limits)
		if failure != nil {
			return // fatal formation (incl. limit failures) is a pass
		}
		assertParseClosure(t, data, doc)
	})
}

// assertParseClosure pins the parse-closure invariants shared by both
// plist profiles: byte-exact render, one of the two closed statuses, and a
// Recovered document that publishes at least one diagnostic within the
// budget.
func assertParseClosure(t *testing.T, data []byte, doc *Document) {
	t.Helper()
	if !bytes.Equal(doc.Render(), data) {
		t.Fatalf("render closure violated (%d bytes, rendered %d):\ninput:  %x\noutput: %x",
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
	if len(doc.Diagnostics()) > DefaultPlistParseLimits().Common.MaxDiagnostics {
		t.Fatalf("diagnostics %d exceed MaxDiagnostics", len(doc.Diagnostics()))
	}
}
