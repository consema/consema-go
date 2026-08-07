package properties

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

func materializationMapping(t *testing.T, entries [][2]string) *core.EntryMapping {
	t.Helper()
	pairs := make([]core.EntryMappingEntry, 0, len(entries))
	for _, entry := range entries {
		pairs = append(pairs, core.EntryMappingEntry{
			Key: core.String(entry[0]), Value: core.String(entry[1]),
		})
	}
	mapping, err := core.NewEntryMapping(pairs...)
	if err != nil {
		t.Fatalf("EntryMapping construction failed")
	}
	return mapping
}

func TestReaderCanonicalEscapesStructureAndControls(t *testing.T) {
	value := materializationMapping(t, [][2]string{{" a#", "  v:=!\\\t\u0008值"}})
	result := Materialize(value, vectorMaterializationRequest(PropertiesReaderV1))
	if result.Complete == nil {
		t.Fatalf("Reader materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "\\ a\\#=\\ \\ v\\:\\=\\!\\\\\\t\\u0008值\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("render %q", string(result.Complete.Document.Render()))
	}
	if result.Complete.Fidelity != MaterializationFidelityExact {
		t.Fatalf("fidelity %s", result.Complete.Fidelity)
	}
	if len(result.Complete.Report.Events()) != 0 {
		t.Fatalf("events %d", len(result.Complete.Report.Events()))
	}
	projected := result.Complete.Document.Project(BestExactEntryMapping())
	if projected.Complete == nil || !core.Equal(projected.Complete.Value, value) {
		t.Fatalf("closure failed")
	}
	if len(result.Complete.Provenance.Entries()) != 4 {
		t.Fatalf("provenance entries %d", len(result.Complete.Provenance.Entries()))
	}
}

func TestLatin1CanonicalUsesUppercaseUtf16EscapesWithoutBom(t *testing.T) {
	value := materializationMapping(t, [][2]string{{"emoji😀", "café"}})
	result := Materialize(value, vectorMaterializationRequest(PropertiesLatin1V1).
		WithNewline(document.NewlineCrLf))
	if result.Complete == nil {
		t.Fatalf("Latin-1 materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := []byte("emoji\\uD83D\\uDE00=caf\\u00E9\\u007F\r\n")
	if string(result.Complete.Document.Render()) != string(expected) {
		t.Fatalf("render %q", string(result.Complete.Document.Render()))
	}
	facts := result.Complete.Document.Source().EncodingFacts()
	if !facts.Selected().Equal(document.Latin1Encoding()) || facts.Bom() != nil {
		t.Fatalf("encoding facts")
	}
}

func TestReaderUtf16AndStrictCodePagesAreExplicit(t *testing.T) {
	unicode := materializationMapping(t, [][2]string{{"名", "值"}})
	utf16 := Materialize(unicode, vectorMaterializationRequest(PropertiesReaderV1).
		WithEncoding(document.Utf16BeEncoding()).WithNewline(document.NewlineCrLf))
	if utf16.Complete == nil {
		t.Fatalf("UTF-16BE materialization failed")
	}
	render := utf16.Complete.Document.Render()
	if len(render) < 2 || render[0] != 0xFE || render[1] != 0xFF {
		t.Fatalf("UTF-16BE BOM missing")
	}
	if !utf16.Complete.Document.Source().EncodingFacts().Selected().Equal(
		document.Utf16BeEncoding()) {
		t.Fatalf("encoding facts")
	}

	cpPage, _ := document.WindowsCodePageFromNumber(1252)
	cpRequest := vectorMaterializationRequest(PropertiesReaderV1).
		WithEncoding(document.WindowsCodePageEncoding(cpPage))
	latin := materializationMapping(t, [][2]string{{"name", "café"}})
	cpResult := Materialize(latin, cpRequest)
	if cpResult.Complete == nil {
		t.Fatalf("CP1252 materialization failed")
	}
	if !containsByte(cpResult.Complete.Document.Render(), 0xE9) {
		t.Fatalf("CP1252 byte missing")
	}
	unrepresentable := Materialize(unicode, cpRequest)
	if unrepresentable.Complete != nil {
		t.Fatalf("unrepresentable 名 accepted by CP1252")
	}
}

func containsByte(bytes []byte, target byte) bool {
	for _, value := range bytes {
		if value == target {
			return true
		}
	}
	return false
}

func TestDuplicateEntryMappingAndUniqueObjectCloseExactly(t *testing.T) {
	duplicate := materializationMapping(t, [][2]string{{"a", "first"}, {"a", "last"}})
	result := Materialize(duplicate, vectorMaterializationRequest(PropertiesReaderV1))
	if result.Complete == nil {
		t.Fatalf("duplicate mapping failed")
	}
	if len(result.Complete.Document.Properties()) != 2 {
		t.Fatalf("duplicates collapsed")
	}

	object, err := core.NewObject(
		core.Entry{Key: "a", Value: core.String("one")},
		core.Entry{Key: "b", Value: core.String("two")},
	)
	if err != nil {
		t.Fatalf("object construction failed")
	}
	objectResult := Materialize(object, vectorMaterializationRequest(PropertiesReaderV1))
	if objectResult.Complete == nil {
		t.Fatalf("object materialization failed")
	}
	projected := objectResult.Complete.Document.Project(
		RequireObject(DuplicatePolicyRequireUnique))
	if projected.Complete == nil || !core.Equal(projected.Complete.Value, object) {
		t.Fatalf("object closure failed")
	}

	empty := materializationMapping(t, [][2]string{{"", ""}})
	tight := vectorMaterializationRequest(PropertiesReaderV1).WithLimits(document.MaterializationLimits{
		MaxInputNodes: 3, MaxOutputBytes: 2,
		MaxDepth: 256, MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000,
	})
	tightResult := Materialize(empty, tight)
	if tightResult.Complete == nil || string(tightResult.Complete.Document.Render()) != "=\n" {
		t.Fatalf("dense empty property %q",
			string(tightResult.Complete.Document.Render()))
	}
}

func TestInvalidRequestsShapesAndLimitsFailAtomically(t *testing.T) {
	value := materializationMapping(t, [][2]string{{"key", "value"}})
	if result := Materialize(value, vectorMaterializationRequest(PropertiesLatin1V1).
		WithEncoding(document.Utf8Encoding())); result.Complete != nil {
		t.Fatalf("Latin-1 accepted UTF-8")
	}
	if result := Materialize(value, vectorMaterializationRequest(PropertiesReaderV1).
		WithNewline(document.NewlineNone)); result.Complete != nil {
		t.Fatalf("None newline accepted")
	}
	if result := Materialize(core.String("scalar"),
		vectorMaterializationRequest(PropertiesReaderV1)); result.Complete != nil {
		t.Fatalf("scalar accepted")
	} else if result.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
		t.Fatalf("scalar code %s", result.Failed.Failure.Code())
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("java-properties.reader-canonical", 1))
	if result := Materialize(value, request); result.Complete != nil {
		t.Fatalf("wrong profile accepted")
	} else if result.Failed.Failure.Code() != "core.materialization.unsupported-profile@1" {
		t.Fatalf("profile code %s", result.Failed.Failure.Code())
	}
	for _, limits := range []document.MaterializationLimits{
		{MaxInputNodes: 1, MaxOutputBytes: 64 << 20, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000},
		{MaxInputNodes: 1_000_000, MaxOutputBytes: 2, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 2_000_000},
		{MaxInputNodes: 1_000_000, MaxOutputBytes: 64 << 20, MaxDepth: 256,
			MaxReportEntries: 100_000, MaxProvenanceEntries: 1},
	} {
		result := Materialize(value, vectorMaterializationRequest(PropertiesReaderV1).
			WithLimits(limits))
		if result.Complete != nil {
			t.Fatalf("limit accepted")
		}
		if result.Failed.Failure.Code() != "core.materialization.resource-limit@1" {
			t.Fatalf("limit code %s", result.Failed.Failure.Code())
		}
	}
}
