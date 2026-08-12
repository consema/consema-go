package plist_test

// XML recovery-classification negatives transcribed from the Rust
// reference parser (consema-plist parser_xml.rs; RFC 0013 §4). Malformed
// values, references, and structure are Recovered with the frozen
// `plist.parse.*@1` codes — never a fatal failure, never a silent success
// — and the recovery semantics (entity text dropped, proven dict entries
// kept) match the reference implementation.

import (
	"testing"

	"consema.dev/consema/document"
	"consema.dev/consema/plist"
)

// parseXMLExpectRecovered parses one XML source and asserts it forms a
// Recovered document carrying the expected diagnostic code.
func parseXMLExpectRecovered(t *testing.T, source, code string) *plist.Document {
	t.Helper()
	doc, failure := plist.Parse([]byte(source), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("%s: parse failed fatally: %s", code, failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("%s: expected Recovered, got %s", code, doc.FormationStatus())
	}
	assertDiagnostic(t, doc, code)
	return doc
}

// TestXMLMalformedRecoveryClassification pins the frozen recovery
// classification of the value and structure grammar (parser_xml.rs
// integer_range_and_grammar_violations_are_recovered,
// real_grammar_violations_are_recovered,
// date_forms_and_calendar_edges, data_base64_violations_are_recovered,
// string_malformed_references_are_recovered,
// string_unknown_entity_is_recovered_without_partial_text,
// empty_scalars_are_recovered, true_false_forms_and_content_recovery,
// dict_pairing_recovery_matrix, key_outside_dict_is_recovered,
// scalar_child_elements_drop_the_scalar,
// unknown_elements_cover_whole_subtrees_as_error_regions).
func TestXMLMalformedRecoveryClassification(t *testing.T) {
	cases := []struct {
		source string
		code   string
	}{
		// Integer grammar violations: out-of-range, empty prefix, trailing
		// junk, embedded whitespace.
		{`<plist version="1.0"><integer>9223372036854775808</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>-9223372036854775809</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>0x</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>12x</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>1 2</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>++1</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer>0b101</integer></plist>`, "plist.parse.integer@1"},
		{`<plist version="1.0"><integer></integer></plist>`, "plist.parse.empty-value@1"},
		{`<plist version="1.0"><integer/></plist>`, "plist.parse.empty-value@1"},
		// Real grammar violations.
		{`<plist version="1.0"><real>5.</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>.5</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>abc</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>1e</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>1e+</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>1.5.2</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>0x1p2</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real>1 5</real></plist>`, "plist.parse.real@1"},
		{`<plist version="1.0"><real/></plist>`, "plist.parse.empty-value@1"},
		// Date calendar edges and grammar violations.
		{`<plist version="1.0"><date>1900-02-29T00:00:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2001-02-29T00:00:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-13-01T00:00:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01T24:00:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01T00:60:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01T00:00:60Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01T00:00:00.5Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01T00:00:00+01:00</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date>2020-01-01 00:00:00Z</date></plist>`, "plist.parse.date@1"},
		{`<plist version="1.0"><date/></plist>`, "plist.parse.empty-value@1"},
		// Base64 violations.
		{`<plist version="1.0"><data>YQ</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>YQ=</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>YQ===</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>YW=j</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>Y!!j</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>A</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data>===</data></plist>`, "plist.parse.data@1"},
		{`<plist version="1.0"><data/></plist>`, "plist.parse.empty-value@1"},
		// Malformed character references.
		{`<plist version="1.0"><string>a &#xZZ; b</string></plist>`, "plist.parse.reference@1"},
		{`<plist version="1.0"><string>a &#xD800; b</string></plist>`, "plist.parse.reference@1"},
		{`<plist version="1.0"><string>a &#xFFFE; b</string></plist>`, "plist.parse.reference@1"},
		{`<plist version="1.0"><string>a &#x0; b</string></plist>`, "plist.parse.reference@1"},
		{`<plist version="1.0"><string>a &b</string></plist>`, "plist.parse.reference@1"},
		{`<plist version="1.0"><string>&;x</string></plist>`, "plist.parse.reference@1"},
		// Unknown entity.
		{`<plist version="1.0"><string>before &bogus; after</string></plist>`, "plist.parse.entity@1"},
		// Empty scalars.
		{`<plist version="1.0"><real></real></plist>`, "plist.parse.empty-value@1"},
		{`<plist version="1.0"><date></date></plist>`, "plist.parse.empty-value@1"},
		// Boolean content.
		{`<plist version="1.0"><true>x</true></plist>`, "plist.parse.boolean-content@1"},
		{`<plist version="1.0"><false>x</false></plist>`, "plist.parse.boolean-content@1"},
		// Structure.
		{`<plist version="1.0"><array><key>a</key></array></plist>`, "plist.parse.key-outside-dict@1"},
		{`<plist version="1.0"><string>a<dict/>b</string></plist>`, "plist.parse.scalar-content@1"},
		{`<plist version="1.0"><dict><string>v</string><key>k</key><string>x</string></dict></plist>`, "plist.parse.dict-key@1"},
		{`<plist version="1.0"><dict><key>a</key><key>b</key><string>x</string></dict></plist>`, "plist.parse.dict-missing-value@1"},
		{`<plist version="1.0"><array><foo>text<bar/></foo><string>x</string></array></plist>`, "plist.parse.element-name@1"},
		{`<plist version="1.0"><dict></array></plist>`, "plist.parse.mismatched-end-tag@1"},
		{`<plist version="1.0">text<string>x</string></plist>`, "plist.parse.text-outside-value@1"},
		{`<plist version="1.0"><array><string>x</string></array>`, "plist.parse.unclosed-element@1"},
		{``, "plist.parse.missing-root@1"},
		// Prologue.
		{`<?xml version="1.1"?><plist version="1.0"><string>x</string></plist>`, "plist.parse.declaration-version@1"},
		{`<?xml version="1.0"?><?xml?><plist version="1.0"><dict/></plist>`, "plist.parse.pi-target@1"},
		{`<!DOCTYPE plist [<!ENTITY x "y">]><plist version="1.0"><string>x</string></plist>`, "plist.parse.doctype-subset@1"},
	}
	for _, test := range cases {
		parseXMLExpectRecovered(t, test.source, test.code)
	}
}

// TestXMLMismatchedAndExtraEndTagsAreRecovered pins the recovery of an end
// tag that mismatches the open root and of a stray end tag after the root
// closed (parser_xml.rs mismatched_and_extra_end_tags_are_recovered). The
// tokenizer error after the root closes must not overlap the previous
// close-tag piece: the error region is the last source byte (xmlparser
// jumps its stream to the end on error), so the lossless index coverage
// holds and the formation is Recovered, never a fatal coverage failure.
func TestXMLMismatchedAndExtraEndTagsAreRecovered(t *testing.T) {
	cases := []struct {
		source string
		codes  []string
	}{
		// `</dict>` mismatches the open `<plist>` and pops the root frame;
		// the trailing `</plist>` is then a tokenizer well-formedness error.
		{`<plist version="1.0"><string>x</string></dict></plist>`, []string{
			"plist.parse.mismatched-end-tag@1",
			"plist.parse.well-formedness@1",
		}},
		// The extra `</plist>` after the root closed is a tokenizer
		// well-formedness error; the empty root also reports its count.
		{`<plist version="1.0"></plist></plist>`, []string{
			"plist.parse.well-formedness@1",
		}},
		// Mismatch below the root: the plist frame closes normally.
		{`<plist version="1.0"><dict></array></plist>`, []string{
			"plist.parse.mismatched-end-tag@1",
		}},
	}
	for _, test := range cases {
		doc := parseXMLExpectRecovered(t, test.source, test.codes[0])
		for _, code := range test.codes[1:] {
			assertDiagnostic(t, doc, code)
		}
	}
}

// TestXMLRecoveryBoundaryDataExplicitEmpty pins the classification
// boundary of the data value: `<data/>` is Recovered (empty-value) while
// `<data></data>` is a Complete zero-length value (RFC 0013 §4.8).
func TestXMLRecoveryBoundaryDataExplicitEmpty(t *testing.T) {
	doc, failure := plist.Parse([]byte(`<plist version="1.0"><data/></plist>`),
		plist.PlistProfileXmlV1, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("self-closing data parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("self-closing data must be Recovered")
	}
	assertDiagnostic(t, doc, "plist.parse.empty-value@1")

	doc, failure = plist.Parse([]byte(`<plist version="1.0"><data></data></plist>`),
		plist.PlistProfileXmlV1, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("explicit empty data parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("explicit empty data must be Complete")
	}
	root := doc.NativeDocument().RootValue()
	data, ok := root.AsData()
	if !ok || len(data.Bytes()) != 0 {
		t.Fatalf("explicit empty data must be a zero-length data value")
	}
}

// TestXMLUnknownEntityRecoveryDropsText pins the reference semantics:
// an unknown entity recovers with `plist.parse.entity@1` and the entity
// text is dropped from the string value while the surrounding text
// survives (parser_xml.rs
// string_unknown_entity_is_recovered_without_partial_text).
func TestXMLUnknownEntityRecoveryDropsText(t *testing.T) {
	doc := parseXMLExpectRecovered(t,
		`<plist version="1.0"><string>before &bogus; after</string></plist>`,
		"plist.parse.entity@1")
	root := doc.NativeDocument().RootValue()
	text, _ := root.AsString()
	value, err := text.ToUnicode()
	if err != nil || value != "before  after" {
		t.Fatalf("entity text must be dropped: %q", value)
	}
}

// TestXMLDictPairingRecoveryMatrix pins the pairing semantics of the
// reference implementation: a value in key position is skipped (following
// pairs still work), a key while a value is expected drops the pending
// key, and a pending key at `</dict>` is dropped (parser_xml.rs
// dict_pairing_recovery_matrix).
func TestXMLDictPairingRecoveryMatrix(t *testing.T) {
	// Value element in key position: unpaired, following pairs still work.
	doc := parseXMLExpectRecovered(t,
		`<plist version="1.0"><dict><string>v</string><key>k</key><string>x</string></dict></plist>`,
		"plist.parse.dict-key@1")
	root := doc.NativeDocument().RootValue()
	dict, _ := root.AsDict()
	if dict.Len() != 1 {
		t.Fatalf("value-in-key: entries %d, want 1", dict.Len())
	}

	// Key while a value is expected: the pending key is dropped.
	doc = parseXMLExpectRecovered(t,
		`<plist version="1.0"><dict><key>a</key><key>b</key><string>x</string></dict></plist>`,
		"plist.parse.dict-missing-value@1")
	root = doc.NativeDocument().RootValue()
	dict, _ = root.AsDict()
	if dict.Len() != 1 {
		t.Fatalf("key-after-key: entries %d, want 1", dict.Len())
	}

	// Pending key at `</dict>`: dropped with a diagnostic.
	doc = parseXMLExpectRecovered(t,
		`<plist version="1.0"><dict><key>a</key></dict></plist>`,
		"plist.parse.dict-missing-value@1")
	root = doc.NativeDocument().RootValue()
	dict, _ = root.AsDict()
	if dict.Len() != 0 {
		t.Fatalf("pending key: entries %d, want 0", dict.Len())
	}
}
