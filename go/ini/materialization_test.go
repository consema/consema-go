package ini

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

// nestedEntryMapping builds one nested String EntryMapping.
func nestedEntryMapping(t *testing.T, sections []struct {
	name    string
	entries [][2]string
}) core.Value {
	t.Helper()
	outer := core.NewEntryMappingBuilder()
	for _, section := range sections {
		inner := core.NewEntryMappingBuilder()
		for _, pair := range section.entries {
			_ = inner.Push(core.String(pair[0]), core.String(pair[1]))
		}
		_ = outer.Push(core.String(section.name), inner.Build())
	}
	return outer.Build()
}

// materializationRequest builds the frozen request for one profile.
func materializationRequest(profile IniProfile) document.MaterializationRequest {
	switch {
	case profile == PortableV1:
		return document.NewMaterializationRequest(document.NewProfileId("ini.portable", 1),
			document.NewMaterializationStyleId("ini.portable-canonical", 1))
	case profile == WindowsV1:
		return document.NewMaterializationRequest(document.NewProfileId("ini.windows", 1),
			document.NewMaterializationStyleId("ini.windows-canonical", 1)).
			WithEncoding(document.Utf16LeEncoding()).
			WithNewline(document.NewlineCrLf)
	default:
		return document.NewMaterializationRequest(document.NewProfileId("ini.python-configparser", 1),
			document.NewMaterializationStyleId("ini.python-configparser-canonical", 1))
	}
}

func TestAllCanonicalProfilesCloseExactly(t *testing.T) {
	portable := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"main", [][2]string{{"key", "value"}, {"empty", ""}}}})
	result := Materialize(portable, materializationRequest(PortableV1))
	if result.Complete == nil {
		t.Fatalf("portable materialization failed: %s", result.Failed.Failure.Code())
	}
	if string(result.Complete.Document.Render()) != "[main]\nkey=value\nempty=\n" {
		t.Fatalf("portable render differed: %q", result.Complete.Document.Render())
	}

	windows := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"Main", [][2]string{{"quoted", " value "}, {"plain", "value"}}}})
	result = Materialize(windows, materializationRequest(WindowsV1))
	if result.Complete == nil {
		t.Fatalf("windows materialization failed: %s", result.Failed.Failure.Code())
	}
	decoded, _ := result.Complete.Document.Source().DecodedText()
	if decoded != "\uFEFF[Main]\r\nquoted=\" value \"\r\nplain=value\r\n" {
		t.Fatalf("windows decoded differed: %q", decoded)
	}
	if !result.Complete.Document.Source().EncodingFacts().Selected().Equal(document.Utf16LeEncoding()) {
		t.Fatalf("windows encoding fact differed")
	}
	if result.Complete.Document.Entries()[0].Value() != " value " {
		t.Fatalf("windows quoted value differed")
	}

	python := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"DEFAULT", [][2]string{{"raw", "%(name)s"}, {"multi", "first\n\nthird"}}}})
	result = Materialize(python, materializationRequest(PythonConfigParserV1))
	if result.Complete == nil {
		t.Fatalf("python materialization failed: %s", result.Failed.Failure.Code())
	}
	decoded, _ = result.Complete.Document.Source().DecodedText()
	if decoded != "[DEFAULT]\nraw = %(name)s\nmulti = first\n\n    third\n" {
		t.Fatalf("python decoded differed: %q", decoded)
	}
	multi := result.Complete.Document.Entries()[1]
	if multi.Value() != "first\n\nthird" {
		t.Fatalf("python multiline value differed")
	}
	multiOrigins := 0
	for _, entry := range result.Complete.Provenance.Entries() {
		if len(entry.Outputs) > 1 {
			multiOrigins++
		}
	}
	if multiOrigins == 0 {
		t.Fatalf("continuation provenance missing")
	}
}

func TestWindowsCodePageIsStrictAndDuplicateEntryMappingSurvives(t *testing.T) {
	value := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"name", "café"}, {"name", "two"}}}})
	page, _ := document.WindowsCodePageFromNumber(1252)
	request := materializationRequest(WindowsV1).
		WithEncoding(document.WindowsCodePageEncoding(page))
	result := Materialize(value, request)
	if result.Complete == nil {
		t.Fatalf("code-page materialization failed: %s", result.Failed.Failure.Code())
	}
	render := result.Complete.Document.Render()
	if len(render) == 0 || render[len(render)-1] != '\n' {
		t.Fatalf("render must end with a newline")
	}
	if !containsByte(render, 0xE9) {
		t.Fatalf("café must encode as 0xE9")
	}
	if len(result.Complete.Document.Entries()) != 2 {
		t.Fatalf("duplicate entry mapping must survive")
	}

	unrepresentable := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"name", "漢"}}}})
	result = Materialize(unrepresentable, request)
	if result.Complete != nil || result.Failed.Failure.Code() != "core.materialization.unsupported-encoding@1" {
		t.Fatalf("unrepresentable scalar must fail with unsupported-encoding")
	}
}

func containsByte(bytes []byte, target byte) bool {
	for _, byte := range bytes {
		if byte == target {
			return true
		}
	}
	return false
}

func TestPythonExplicitTextEncodingsAreRepresentabilityChecked(t *testing.T) {
	latin := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"name", "café"}}}})
	latinRequest := materializationRequest(PythonConfigParserV1).
		WithEncoding(document.Latin1Encoding())
	result := Materialize(latin, latinRequest)
	if result.Complete == nil {
		t.Fatalf("Latin-1 python materialization failed: %s", result.Failed.Failure.Code())
	}
	if !result.Complete.Document.Source().EncodingFacts().Selected().Equal(document.Latin1Encoding()) {
		t.Fatalf("Latin-1 encoding fact differed")
	}
	if !containsByte(result.Complete.Document.Render(), 0xE9) {
		t.Fatalf("Latin-1 render must contain 0xE9")
	}

	unicode := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"節", [][2]string{{"鍵", "値"}}}})
	utf16Request := materializationRequest(PythonConfigParserV1).
		WithEncoding(document.Utf16BeEncoding())
	result = Materialize(unicode, utf16Request)
	if result.Complete == nil {
		t.Fatalf("UTF-16BE python materialization failed: %s", result.Failed.Failure.Code())
	}
	render := result.Complete.Document.Render()
	if len(render) < 2 || render[0] != 0xFE || render[1] != 0xFF {
		t.Fatalf("UTF-16BE BOM missing")
	}
	if !result.Complete.Document.Source().EncodingFacts().Selected().Equal(document.Utf16BeEncoding()) {
		t.Fatalf("UTF-16BE encoding fact differed")
	}
	result = Materialize(unicode, latinRequest)
	if result.Complete != nil {
		t.Fatalf("unrepresentable scalar must fail")
	}
}

func TestObjectInputIsUniqueAndCannotFabricateWindowsCaseCollisions(t *testing.T) {
	inner := core.NewObjectBuilder()
	_ = inner.Insert("Name", core.String("one"))
	_ = inner.Insert("name", core.String("two"))
	outer := core.NewObjectBuilder()
	_ = outer.Insert("s", inner.Build())
	result := Materialize(outer.Build(), materializationRequest(WindowsV1))
	if result.Complete != nil {
		t.Fatalf("case-equivalent Object must fail")
	}
	uniqueInner := core.NewObjectBuilder()
	_ = uniqueInner.Insert("Name", core.String("one"))
	uniqueOuter := core.NewObjectBuilder()
	_ = uniqueOuter.Insert("s", uniqueInner.Build())
	result = Materialize(uniqueOuter.Build(), materializationRequest(WindowsV1))
	if result.Complete == nil {
		t.Fatalf("unique Object must succeed")
	}
}

func TestMalformedShapesUnrepresentableValuesAndLimitsFailAtomically(t *testing.T) {
	scalar := Materialize(core.String("x"), materializationRequest(PortableV1))
	if scalar.Complete != nil {
		t.Fatalf("scalar input must fail")
	}
	trailing := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"value", "line\n"}}}})
	result := Materialize(trailing, materializationRequest(PythonConfigParserV1))
	if result.Complete != nil {
		t.Fatalf("trailing empty python line must fail")
	}
	nonportable := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"key", "value;with-semicolon"}}}})
	result = Materialize(nonportable, materializationRequest(PortableV1))
	if result.Complete != nil {
		t.Fatalf("nonportable value must fail")
	}

	value := nestedEntryMapping(t, []struct {
		name    string
		entries [][2]string
	}{{"s", [][2]string{{"key", "value"}}}})
	limitCases := []document.MaterializationLimits{
		{MaxInputNodes: 1, MaxOutputBytes: 64 << 20, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000},
		{MaxInputNodes: 1_000_000, MaxOutputBytes: 2, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000},
		{MaxInputNodes: 1_000_000, MaxOutputBytes: 64 << 20, MaxDepth: 0,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000},
		{MaxInputNodes: 1_000_000, MaxOutputBytes: 64 << 20, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 1},
	}
	for _, limits := range limitCases {
		result = Materialize(value, materializationRequest(PortableV1).WithLimits(limits))
		if result.Complete != nil {
			t.Fatalf("limit %+v must fail", limits)
		}
		if result.Failed.Failure.Code() != "core.materialization.resource-limit@1" {
			t.Fatalf("limit code differed")
		}
	}
}
