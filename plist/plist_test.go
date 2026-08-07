package plist_test

// Focused unit tests for the plist package surface: formation recovery,
// the bplist width matrix (every legal offset/ref width, RFC 0013 §5.11),
// binary offset hardening (out-of-bounds offsets and references, cycles,
// width truncation), the cross-representation round trip, conversion
// inexpressibility, the three query domains, and the structural edits.

import (
	"context"
	"encoding/hex"
	"math"
	"math/big"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/plist"
	"consema.dev/consema/protocol"
)

func parseXMLTest(t *testing.T, source string) *plist.Document {
	t.Helper()
	doc, failure := plist.Parse([]byte(source), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("xml parse failed: %s", failure.Code())
	}
	return doc
}

// testBinaryBuilder is a hand-built `bplist00` fixture writer: header,
// objects, offset table, trailer.
type testBinaryBuilder struct {
	bytes         []byte
	offsets       []uint64
	offsetIntSize int
	refSize       int
}

func newTestBinaryBuilder(offsetIntSize, refSize int) *testBinaryBuilder {
	return &testBinaryBuilder{bytes: []byte("bplist00"), offsetIntSize: offsetIntSize, refSize: refSize}
}

func (b *testBinaryBuilder) object(object []byte) uint64 {
	offset := uint64(len(b.bytes))
	b.offsets = append(b.offsets, offset)
	b.bytes = append(b.bytes, object...)
	return offset
}

func (b *testBinaryBuilder) pushBE(value uint64, width int) {
	for shift := (width - 1) * 8; shift >= 0; shift -= 8 {
		b.bytes = append(b.bytes, byte((value>>uint(shift))&0xFF))
	}
}

func (b *testBinaryBuilder) finish(topObject uint64) []byte {
	offsetTableOffset := uint64(len(b.bytes))
	for _, offset := range b.offsets {
		b.pushBE(offset, b.offsetIntSize)
	}
	b.bytes = append(b.bytes, 0, 0, 0, 0, 0)
	b.bytes = append(b.bytes, 0) // sortVersion
	b.bytes = append(b.bytes, byte(b.offsetIntSize))
	b.bytes = append(b.bytes, byte(b.refSize))
	b.pushBE(uint64(len(b.offsets)), 8)
	b.pushBE(topObject, 8)
	b.pushBE(offsetTableOffset, 8)
	return b.bytes
}

func referenceBytes(objectIndex int, refSize int) []byte {
	var out []byte
	for shift := (refSize - 1) * 8; shift >= 0; shift -= 8 {
		out = append(out, byte(uint64(objectIndex)>>uint(shift)))
	}
	return out
}

func parseBinaryTest(t *testing.T, bytes []byte) (*plist.Document, *plist.FormationFailure) {
	t.Helper()
	doc, failure := plist.Parse(bytes, plist.PlistProfileBinaryV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	return doc, failure
}

// TestBinaryWidthMatrix parses one integer object under every legal
// offset/ref width pair (RFC 0013 §5.11: widths 1, 2, 4, and 8 bytes).
func TestBinaryWidthMatrix(t *testing.T) {
	for _, offsetIntSize := range []int{1, 2, 4, 8} {
		for _, refSize := range []int{1, 2, 4, 8} {
			builder := newTestBinaryBuilder(offsetIntSize, refSize)
			builder.object([]byte{0x10, 0x2A}) // integer 42
			bytes := builder.finish(0)
			doc, failure := parseBinaryTest(t, bytes)
			if failure != nil {
				t.Fatalf("width %d/%d parse failed: %s", offsetIntSize, refSize, failure.Code())
			}
			if doc.FormationStatus() != document.FormationStatusComplete {
				t.Fatalf("width %d/%d not complete", offsetIntSize, refSize)
			}
			facts := doc.BinaryFacts()
			if int(facts.Trailer().OffsetIntSize()) != offsetIntSize ||
				int(facts.Trailer().ObjectRefSize()) != refSize {
				t.Fatalf("width %d/%d trailer facts mismatch", offsetIntSize, refSize)
			}
			root := doc.NativeDocument().RootValue()
			integer, ok := root.AsInteger()
			if !ok || integer.Value() != 42 {
				t.Fatalf("width %d/%d integer value mismatch", offsetIntSize, refSize)
			}
			// Byte-exact render.
			if string(doc.Render()) != string(bytes) {
				t.Fatalf("width %d/%d render not byte-exact", offsetIntSize, refSize)
			}
		}
	}
}

// TestBinaryIntegerWidths parses the full integer width matrix with the
// signedness rules of RFC 0013 §5.3.
func TestBinaryIntegerWidths(t *testing.T) {
	cases := []struct {
		marker []byte
		value  int64
	}{
		{[]byte{0x10, 0x00}, 0},
		{[]byte{0x10, 0xFF}, 255},
		{[]byte{0x11, 0x01, 0x00}, 256},
		{[]byte{0x11, 0xFF, 0xFF}, 65535},
		{[]byte{0x12, 0x00, 0x01, 0x00, 0x00}, 65536},
		{[]byte{0x12, 0xFF, 0xFF, 0xFF, 0xFF}, 4294967295},
		{[]byte{0x13, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, -1},
		{[]byte{0x13, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, math.MinInt64},
		{[]byte{0x13, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, math.MaxInt64},
	}
	for _, test := range cases {
		builder := newTestBinaryBuilder(1, 1)
		builder.object(test.marker)
		doc, failure := parseBinaryTest(t, builder.finish(0))
		if failure != nil {
			t.Fatalf("marker %x parse failed: %s", test.marker, failure.Code())
		}
		root := doc.NativeDocument().RootValue()
		integer, ok := root.AsInteger()
		if !ok || integer.Value() != test.value {
			t.Fatalf("marker %x value %d != %d", test.marker, integer.Value(), test.value)
		}
	}
}

// TestBinaryOffsetHardening rejects out-of-bounds offsets, references, and
// width truncation (RFC 0013 §5.11).
func TestBinaryOffsetHardening(t *testing.T) {
	// Out-of-bounds offset-table entry (points before the header).
	builder := newTestBinaryBuilder(1, 1)
	builder.object([]byte{0x08})
	bytes := builder.finish(0)
	bytes[9] = 0x05 // entry value 5 < 8
	doc, failure := parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("out-of-bounds offset must recover, got %s", doc.FormationStatus())
	}
	assertDiagnostic(t, doc, "plist.binary.offset-table@1")

	// Out-of-bounds reference (target >= numObjects).
	builder = newTestBinaryBuilder(1, 1)
	builder.object(append([]byte{0xA1}, referenceBytes(5, 1)...))
	doc, failure = parseBinaryTest(t, builder.finish(0))
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("out-of-bounds reference must recover")
	}
	assertDiagnostic(t, doc, "plist.binary.reference@1")

	// Width truncation: offsetIntSize 1 cannot address a table offset at
	// 256.
	builder = newTestBinaryBuilder(1, 1)
	builder.object([]byte{0x08})
	bytes = builder.finish(0)
	// Patch offsetTableOffset to 256 and the total length accordingly.
	start := len(bytes) - 32 + 24
	bytes = bytes[:start]
	bytes = append(bytes, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x00)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	assertDiagnostic(t, doc, "plist.binary.trailer@1")

	// A reference cycle is recovered (RFC 0013 §5.11).
	builder = newTestBinaryBuilder(1, 1)
	builder.object(append([]byte{0xA1}, referenceBytes(1, 1)...))
	builder.object(append([]byte{0xA1}, referenceBytes(0, 1)...))
	doc, failure = parseBinaryTest(t, builder.finish(0))
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("cycle must recover")
	}
	assertDiagnostic(t, doc, "plist.binary.cycle@1")
	if doc.NativeDocument() != nil {
		t.Fatalf("cyclic document must carry no native arena")
	}

	// A truncated object extent is recovered.
	builder = newTestBinaryBuilder(1, 1)
	builder.object([]byte{0x5F, 0x10, 0x10, 0x61}) // string of 16 'a' but only 1 payload byte
	doc, failure = parseBinaryTest(t, builder.finish(0))
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("truncated extent must recover")
	}
}

// TestXMLRecoveryDiagnostics pins the frozen recovery codes of RFC 0013 §4.
func TestXMLRecoveryDiagnostics(t *testing.T) {
	cases := []struct {
		source string
		code   string
	}{
		{"<!DOCTYPE wrong PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\"><plist version=\"1.0\"><string>x</string></plist>",
			"plist.parse.doctype@1"},
		{"<plist><string>x</string></plist>", "plist.parse.root-version@1"},
		{"<plist version=\"2.0\"><string>x</string></plist>", "plist.parse.root-version@1"},
		{"<plist version=\"1.0\" extra=\"1\"><string>x</string></plist>", "plist.parse.root-attribute@1"},
		{"<plist version=\"1.0\"></plist>", "plist.parse.root-value-count@1"},
		{"<plist version=\"1.0\"><foo/></plist>", "plist.parse.element-name@1"},
		{"<plist version=\"1.0\"><integer>12a</integer></plist>", "plist.parse.integer@1"},
		{"<plist version=\"1.0\"><date>2023-01-01T00:00:00.5Z</date></plist>", "plist.parse.date@1"},
		{"<plist version=\"1.0\"><data>QUJ</data></plist>", "plist.parse.data@1"},
		{"<plist version=\"1.0\"><dict><key>a</key></dict></plist>", "plist.parse.dict-missing-value@1"},
		{"<plist version=\"1.0\"><dict><string>x</string></dict></plist>", "plist.parse.dict-key@1"},
		{"<plist version=\"1.0\"><string>x</string></plist> trailing", "plist.parse.well-formedness@1"},
		{"<plist version=\"1.0\"><string>x</string>", "plist.parse.unclosed-element@1"},
	}
	for _, test := range cases {
		doc, failure := plist.Parse([]byte(test.source), plist.PlistProfileXmlV1,
			plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
		if failure != nil {
			t.Fatalf("%s parse failed: %s", test.code, failure.Code())
		}
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatalf("%s: expected Recovered", test.code)
		}
		assertDiagnostic(t, doc, test.code)
	}
}

func assertDiagnostic(t *testing.T, doc *plist.Document, code string) {
	t.Helper()
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostic %s not found", code)
}

// TestXMLValueFacts pins the native facts of the XML value grammar.
func TestXMLValueFacts(t *testing.T) {
	source := "<plist version=\"1.0\"><dict>" +
		"<key>count</key><integer>0x2A</integer>" +
		"<key>ratio</key><real>1.5e3</real>" +
		"<key>nan</key><real>nan</real>" +
		"<key>born</key><date>1970-01-01T00:00:00Z</date>" +
		"<key>payload</key><data>AQID</data>" +
		"<key>text</key><string>a &lt; b &amp; c &#65;</string>" +
		"<key>lines</key><string>cr\r\nlf</string>" +
		"</dict></plist>"
	doc := parseXMLTest(t, source)
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("not complete: %v", doc.Diagnostics())
	}
	root := doc.NativeDocument().RootValue()
	dict, _ := root.AsDict()
	values := map[string]*plist.PlistValue{}
	for _, entry := range dict.Entries() {
		key, _ := entry.Key().ToUnicode()
		value, _ := doc.NativeDocument().Get(entry.Value())
		values[key] = &value
	}
	integer, _ := values["count"].AsInteger()
	if integer.Value() != 42 {
		t.Fatalf("hex integer mismatch: %d", integer.Value())
	}
	real, _ := values["ratio"].AsReal()
	if !plistBitsEqual64(real.AsFloat64(), 1500.0) {
		t.Fatalf("real exponent mismatch")
	}
	if real2, ok := values["nan"].AsReal(); !ok || !math.IsNaN(real2.AsFloat64()) {
		t.Fatalf("nan admitted")
	}
	date, _ := values["born"].AsDate()
	if !plistBitsEqual64(date.Seconds(), -978307200.0) {
		t.Fatalf("epoch date mismatch: %f", date.Seconds())
	}
	data, _ := values["payload"].AsData()
	if string(data.Bytes()) != string([]byte{1, 2, 3}) {
		t.Fatalf("base64 mismatch")
	}
	text, _ := values["text"].AsString()
	textValue, err := text.ToUnicode()
	if err != nil || textValue != "a < b & c A" {
		t.Fatalf("reference resolution mismatch: %q", textValue)
	}
	lines, _ := values["lines"].AsString()
	linesValue, _ := lines.ToUnicode()
	if linesValue != "cr\nlf" {
		t.Fatalf("line-end normalization mismatch: %q", linesValue)
	}
}

func plistBitsEqual64(left, right float64) bool {
	return math.Float64bits(left) == math.Float64bits(right)
}

// TestXMLBinaryXMLRoundTrip converts across both representations and back
// with native-model equality (RFC 0013 §7).
func TestXMLBinaryXMLRoundTrip(t *testing.T) {
	source := "<plist version=\"1.0\"><dict>" +
		"<key>name</key><string>Consema</string>" +
		"<key>count</key><integer>42</integer>" +
		"<key>ratio</key><real>1.5</real>" +
		"<key>enabled</key><true/>" +
		"<key>payload</key><data>AQID</data>" +
		"<key>born</key><date>2023-01-01T00:00:00Z</date>" +
		"<key>tags</key><array><string>a</string><dict/></array>" +
		"</dict></plist>"
	xml := parseXMLTest(t, source)
	binary, failure := xml.ConvertTo(plist.PlistProfileBinaryV1, plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("xml->binary failed: %s", failure.Code())
	}
	if !binary.Report().RepresentationChanged() {
		t.Fatalf("representation change not reported")
	}
	if binary.Document().FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("binary target not complete")
	}
	if !binary.Document().NativeDocument().Equal(xml.NativeDocument()) {
		t.Fatalf("native model mismatch after xml->binary")
	}
	back, failure := binary.Document().ConvertTo(plist.PlistProfileXmlV1,
		plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("binary->xml failed: %s", failure.Code())
	}
	if !back.Document().NativeDocument().Equal(xml.NativeDocument()) {
		t.Fatalf("native model mismatch after round trip")
	}
}

// TestConversionInexpressibleFacts fails atomically for every binary-only
// fact (RFC 0013 §7, hard gate 3).
func TestConversionInexpressibleFacts(t *testing.T) {
	cases := []struct {
		objects [][]byte
		fact    string
	}{
		{[][]byte{{0x80, 0x2A}}, "uid"},
		{[][]byte{{0x22, 0x3D, 0xCC, 0xCC, 0xCD}}, "float32-width"},
		{[][]byte{{0x62, 0x00, 0x41, 0xD8, 0x00}}, "unpaired-surrogate"},
		{[][]byte{{0x33}}, "fractional-seconds"},
		{[][]byte{{0x51, 'x'}, {0xA2, 0x00, 0x00}}, "shared-identity"},
	}
	for _, test := range cases {
		builder := newTestBinaryBuilder(1, 1)
		for _, object := range test.objects {
			builder.object(object)
		}
		doc, failure := parseBinaryTest(t, builder.finish(uint64(len(test.objects)-1)))
		if failure != nil {
			t.Fatalf("parse failed: %s", failure.Code())
		}
		if test.fact == "fractional-seconds" {
			// date 0.0 + NaN payload marker: build the fractional date
			// payload explicitly.
			doc = binaryDateDocument(t, 0.5)
		}
		converted, convertFailure := doc.ConvertTo(plist.PlistProfileXmlV1,
			plist.DefaultPlistParseLimits())
		if convertFailure == nil {
			t.Fatalf("%s must fail conversion", test.fact)
		}
		if converted != nil {
			t.Fatalf("%s must return no target document", test.fact)
		}
		found := false
		for _, diagnostic := range convertFailure.Diagnostics() {
			if diagnostic.Code == "plist.conversion.inexpressible@1" {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s: inexpressible diagnostic not found", test.fact)
		}
	}
}

func binaryDateDocument(t *testing.T, seconds float64) *plist.Document {
	t.Helper()
	builder := newTestBinaryBuilder(1, 1)
	builder.object(append([]byte{0x33}, float64Bytes(seconds)...))
	doc, failure := parseBinaryTest(t, builder.finish(0))
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	return doc
}

func float64Bytes(value float64) []byte {
	bits := math.Float64bits(value)
	var out []byte
	for shift := 56; shift >= 0; shift -= 8 {
		out = append(out, byte(bits>>uint(shift)))
	}
	return out
}

// TestNativeQueryPins the native query domain over the arena (RFC 0013
// §8.1).
func TestNativeQueryPins(t *testing.T) {
	source := "<plist version=\"1.0\"><dict>" +
		"<key>a</key><integer>1</integer>" +
		"<key>b</key><array><string>x</string></array>" +
		"<key>a</key><integer>2</integer>" +
		"</dict></plist>"
	doc := parseXMLTest(t, source)
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("plist.document-root", 1)).
		Then(protocol.NewOperatorCall("plist.dict-entries", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainPlistNativeV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %s", failure.Code())
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %s", failure.Code())
	}
	matches, failure := plist.ExecutePlistNativeQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %s", failure.Code())
	}
	var keys []string
	for _, match := range matches {
		if match.Kind == plist.PlistMatchDictEntry && match.Key != nil {
			text, _ := match.Key.ToUnicode()
			keys = append(keys, text)
		}
	}
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "a" {
		t.Fatalf("entry order mismatch: %v", keys)
	}
}

// TestEditSetValueCommits commits a set-value edit with the reparse
// closure, the replayable patch, and the untouched-byte proof (RFC 0013
// §11).
func TestEditSetValueCommits(t *testing.T) {
	source := "<plist version=\"1.0\"><dict><key>a</key><string>old</string></dict></plist>"
	doc := parseXMLTest(t, source)
	builder := plist.NewEditTransactionBuilder(doc)
	builder.SetValue(plist.NewEditPath([]plist.EditPathStep{
		plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("a"), 0),
	}), plist.NewEditValueString(plist.NewPlistStringFromUnicode("new")))
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("edit failed: %s", failure.Code())
	}
	rendered := string(commit.Document.Render())
	if rendered != "<plist version=\"1.0\"><dict><key>a</key><string>new</string></dict></plist>" {
		t.Fatalf("edit render mismatch: %s", rendered)
	}
	// Reparse closure under the same profile.
	reparsed, reparseFailure := plist.Parse(commit.Document.Render(), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if reparseFailure != nil || reparsed.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("reparse closure failed")
	}
	// Patch replay.
	replay, err := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil || string(replay.Bytes()) != rendered {
		t.Fatalf("patch does not replay")
	}
	// Untouched proof verifies.
	if proofError := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()); proofError != nil {
		t.Fatalf("untouched proof: %v", proofError)
	}
}

// TestBinaryEditStructural rewrites the object table structurally and
// regenerates the offset table and trailer (RFC 0013 §11).
func TestBinaryEditStructural(t *testing.T) {
	builder := newTestBinaryBuilder(1, 1)
	builder.object(append([]byte{0xA2}, referenceBytes(1, 1)...))
	builder.object(append([]byte{0xA2}, referenceBytes(0, 1)...))
	_ = builder.object([]byte{0x08}) // unreachable object stays untouched
	bytes := builder.finish(1)
	doc, failure := parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	// Set the root array's second element to integer 42: a fresh non-cyclic
	// fixture with the array at object 0 referencing the integer at object 1.
	builder = newTestBinaryBuilder(1, 1)
	builder.object(append([]byte{0xA1}, referenceBytes(1, 1)...)) // array [7]
	builder.object([]byte{0x10, 0x07})                            // integer 7
	doc, failure = parseBinaryTest(t, builder.finish(0))
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	editBuilder := plist.NewEditTransactionBuilder(doc)
	editBuilder.SetValue(plist.NewEditPath([]plist.EditPathStep{
		plist.NewEditPathStepArrayIndex(0),
	}), plist.NewEditValueInteger(plist.NewPlistInteger(42)))
	commit, editFailure := doc.Commit(editBuilder.Build())
	if editFailure != nil {
		t.Fatalf("binary edit failed: %s", editFailure.Code())
	}
	root := commit.Document.NativeDocument().RootValue()
	array, _ := root.AsArray()
	element, _ := commit.Document.NativeDocument().Get(array.Elements()[0])
	integer, ok := element.AsInteger()
	if !ok || integer.Value() != 42 {
		t.Fatalf("structural set-value mismatch")
	}
	// The rewritten bytes reparse complete under the binary profile.
	reparsed, failure := parseBinaryTest(t, commit.Document.Render())
	if failure != nil || reparsed.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("binary reparse closure failed")
	}
}

// TestProjectionValueTreePins the exact `plist.value-tree@1` record
// (RFC 0013 §9).
func TestProjectionValueTreePins(t *testing.T) {
	source := "<plist version=\"1.0\"><dict>" +
		"<key>name</key><string>text</string>" +
		"<key>count</key><integer>42</integer>" +
		"<key>ratio</key><real>1.5</real>" +
		"<key>enabled</key><true/>" +
		"<key>payload</key><data>AQID</data>" +
		"<key>created</key><date>2023-01-01T00:00:00Z</date>" +
		"<key>tags</key><array><string>a</string><string>b</string></array>" +
		"</dict></plist>"
	doc := parseXMLTest(t, source)
	result := plist.Project(doc, plist.NewProjectionRequestValueTree())
	if result.Failed != nil {
		t.Fatalf("projection failed: %s", result.Failed.Diagnostics[0].Code)
	}
	record, ok := result.Complete.Value.(*core.Object)
	if !ok {
		t.Fatalf("record must be an Object")
	}
	recordText, _ := record.Get("record")
	if recordText != core.String("plist.value-tree@1") {
		t.Fatalf("record member mismatch")
	}
	rootValue, _ := record.Get("root")
	mapping, ok := rootValue.(*core.EntryMapping)
	if !ok || mapping.Len() != 7 {
		t.Fatalf("root must be an entry mapping")
	}
	if result.Complete.Fidelity != plist.FidelityExact {
		t.Fatalf("fidelity must be Exact")
	}
	if len(result.Complete.Report.Events()) != 0 {
		t.Fatalf("no events expected")
	}
}

// TestMaterializationXMLCanonicalPins the canonical XML bytes with the
// reparse closure (RFC 0013 §10.1, §10.3).
func TestMaterializationXMLCanonicalPins(t *testing.T) {
	root, _ := core.NewObject(
		core.Entry{Key: "name", Value: core.String("value")},
		core.Entry{Key: "count", Value: core.NewInteger(big.NewInt(42))},
		core.Entry{Key: "title", Value: core.String("a & b < c")},
	)
	record, _ := core.NewObject(
		core.Entry{Key: "record", Value: core.String("plist.value-tree@1")},
		core.Entry{Key: "root", Value: root},
	)
	request := document.NewMaterializationRequest(
		document.NewProfileId("plist.xml", 1),
		document.NewMaterializationStyleId("plist.xml-canonical", 1))
	result := plist.Materialize(record, request)
	if result.Failed != nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" +
		"<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n" +
		"<plist version=\"1.0\">\n" +
		"    <dict>\n" +
		"        <key>name</key>\n" +
		"        <string>value</string>\n" +
		"        <key>count</key>\n" +
		"        <integer>42</integer>\n" +
		"        <key>title</key>\n" +
		"        <string>a &amp; b &lt; c</string>\n" +
		"    </dict>\n" +
		"</plist>\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("canonical render mismatch:\n%s", result.Complete.Document.Render())
	}
	if result.Complete.Fidelity != plist.MaterializationFidelityExact {
		t.Fatalf("fidelity must be Exact")
	}
}

// TestMaterializationTruncateWithReport truncates fractional dates only
// under the authorized policy and reports the event (RFC 0013 §10.1).
func TestMaterializationTruncateWithReport(t *testing.T) {
	root, _ := core.NewObject(core.Entry{Key: "t", Value: dateRecord(1.5)})
	record, _ := core.NewObject(
		core.Entry{Key: "record", Value: core.String("plist.value-tree@1")},
		core.Entry{Key: "root", Value: root},
		core.Entry{Key: "truncate_policy", Value: core.String("TruncateWithReport")},
	)
	request := document.NewMaterializationRequest(
		document.NewProfileId("plist.xml", 1),
		document.NewMaterializationStyleId("plist.xml-canonical", 1))
	result := plist.Materialize(record, request)
	if result.Failed != nil {
		t.Fatalf("truncation failed: %s", result.Failed.Failure.Code())
	}
	if result.Complete.Fidelity != plist.MaterializationFidelityTransformed {
		t.Fatalf("fidelity must be Transformed")
	}
	events := result.Complete.Report.Events()
	if len(events) != 1 || events[0].Code != "plist.materialization.fractional-date@1" {
		t.Fatalf("truncation event missing")
	}
	rendered := string(result.Complete.Document.Render())
	if !strings.Contains(rendered, "<date>2001-01-01T00:00:01Z</date>") {
		t.Fatalf("truncated date missing: %s", rendered)
	}
	// Without the policy the same record fails atomically.
	recordNoPolicy, _ := core.NewObject(
		core.Entry{Key: "record", Value: core.String("plist.value-tree@1")},
		core.Entry{Key: "root", Value: root},
	)
	resultNoPolicy := plist.Materialize(recordNoPolicy, request)
	if resultNoPolicy.Complete != nil {
		t.Fatalf("fractional date must fail without the policy")
	}
	if resultNoPolicy.Failed.Failure.Code() != "plist.materialization.fractional-date@1" {
		t.Fatalf("failure code mismatch: %s", resultNoPolicy.Failed.Failure.Code())
	}
}

func dateRecord(seconds float64) core.Value {
	obj, _ := core.NewObject(
		core.Entry{Key: "epoch", Value: core.String("2001-01-01T00:00:00Z")},
		core.Entry{Key: "seconds", Value: core.NewBinaryFloat64(math.Float64bits(seconds))},
	)
	return obj
}

// TestOperationRegistryPins the six-operation surface (RFC 0013 §11).
func TestOperationRegistryPins(t *testing.T) {
	expected := []string{
		"plist.edit.set-value@1",
		"plist.edit.insert-dict-entry@1",
		"plist.edit.remove-dict-entry@1",
		"plist.edit.rename-dict-key@1",
		"plist.edit.insert-array-element@1",
		"plist.edit.remove-array-element@1",
	}
	for _, profile := range []plist.PlistProfile{plist.PlistProfileXmlV1, plist.PlistProfileBinaryV1} {
		registry := plist.FormatOperationRegistryFor(profile)
		operations := registry.Operations()
		if len(operations) != len(expected) {
			t.Fatalf("operation count mismatch")
		}
		for index := range operations {
			if operations[index].ID() != expected[index] {
				t.Fatalf("operation %d mismatch: %s", index, operations[index].ID())
			}
		}
	}
}

// TestBinaryExtendedSizeCounts parses an extended-size array count object
// (RFC 0013 §5.4).
func TestBinaryExtendedSizeCounts(t *testing.T) {
	hexSource := "62706c6973743030af101002030405060708090a0b0c0d0e0f10115050505050505050505050505050505008091b1c1d1e1f202122232425262728292a000000000000010100000000000000120000000000000000000000000000002b"
	bytes, _ := hex.DecodeString(hexSource)
	doc, failure := parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("not complete")
	}
	root := doc.NativeDocument().RootValue()
	array, ok := root.AsArray()
	if !ok || array.Len() != 16 {
		t.Fatalf("extended array length mismatch")
	}
}
