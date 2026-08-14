package protocol

import (
	"testing"

	"consema.dev/consema/core"
)

func v7ErrorRegistry() ErrorCodeRegistry {
	return NewErrorCodeRegistry(ErrorRegistryV7)
}

func sampleDiagnostic(t *testing.T, registry ErrorCodeRegistry) *Diagnostic {
	t.Helper()
	location, err := NewSourceLocation("source:one", 10, 20)
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := NewDiagnostic(
		"ini.parse.malformed-line@1", CategorySyntax, SeverityError,
		location, []RelatedSourceLocation{
			{Role: "context", Location: SourceLocation{SourceID: "source:one", StartByte: 0, EndByte: 5}},
		},
		map[string]string{"line": "3"}, []string{"note:line"}, nil, 1, registry)
	if err != nil {
		t.Fatal(err)
	}
	return diagnostic
}

func TestDiagnosticConstructionValidation(t *testing.T) {
	registry := v7ErrorRegistry()
	// Unknown code is a protocol error (RFC 0011; diagnostic.rs).
	_, err := NewDiagnostic("example.unknown@1", CategorySyntax, SeverityError,
		nil, nil, nil, nil, nil, 0, registry)
	protocolErr, _ := err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Kind != KindInvalidValue || protocolErr.Path != "$.code" {
		t.Errorf("unknown code: got %v", err)
	}
	// A registered code with a contradicting category is rejected.
	_, err = NewDiagnostic("ini.parse.malformed-line@1", CategorySemantic, SeverityError,
		nil, nil, nil, nil, nil, 0, registry)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.category" {
		t.Errorf("category contradiction: got %v", err)
	}
	// The registry-default v1 constructor still rejects v7-only codes.
	_, err = NewDiagnostic("cli.limit.file-size@1", CategoryResource, SeverityError,
		nil, nil, nil, nil, nil, 0, DefaultErrorCodeRegistry())
	if err == nil {
		t.Error("v1 registry accepted a v7-only code")
	}
	// A valid diagnostic constructs.
	diagnostic := sampleDiagnostic(t, registry)
	if diagnostic.Code != "ini.parse.malformed-line@1" || diagnostic.Occurrence != 1 {
		t.Error("diagnostic fields wrong")
	}
}

func TestDiagnosticRoundTripsBothTransports(t *testing.T) {
	registry := v7ErrorRegistry()
	diagnostic := sampleDiagnostic(t, registry)
	value, err := diagnostic.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&Diagnostic{}).FromValue(value, registry)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Code != diagnostic.Code || decoded.Category != diagnostic.Category ||
		decoded.Severity != diagnostic.Severity || decoded.Occurrence != diagnostic.Occurrence {
		t.Error("value round-trip changed diagnostic facts")
	}
	if decoded.Primary == nil || decoded.Primary.SourceID != "source:one" ||
		decoded.Primary.StartByte != 10 || decoded.Primary.EndByte != 20 {
		t.Error("primary location lost")
	}
	if len(decoded.Related) != 1 || decoded.Related[0].Role != "context" {
		t.Error("related location lost")
	}
	if decoded.Arguments["line"] != "3" || len(decoded.Notes) != 1 {
		t.Error("arguments or notes lost")
	}
	limits := DefaultProtocolLimits()
	jsonBytes, err := EncodeJSON(value, limits)
	if err != nil {
		t.Fatal(err)
	}
	jsonValue, err := DecodeJSON(jsonBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := (&Diagnostic{}).FromValue(jsonValue, registry)
	if err != nil {
		t.Fatal(err)
	}
	if fromJSON.Code != diagnostic.Code {
		t.Error("JSON round-trip lost the code")
	}
	pvceBytes, err := EncodePVCE(value, limits)
	if err != nil {
		t.Fatal(err)
	}
	pvceValue, err := DecodePVCE(pvceBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	fromPVCE, err := (&Diagnostic{}).FromValue(pvceValue, registry)
	if err != nil {
		t.Fatal(err)
	}
	if fromPVCE.Code != diagnostic.Code {
		t.Error("PVCE round-trip lost the code")
	}
}

func TestDiagnosticWireSchemaIsStrict(t *testing.T) {
	registry := v7ErrorRegistry()
	diagnostic := sampleDiagnostic(t, registry)
	value, err := diagnostic.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	// An unknown field is rejected.
	object := value.(*core.Object)
	entries := object.Entries()
	tampered := make([]core.Entry, 0, len(entries)+1)
	tampered = append(tampered, entries[:1]...)
	tampered = append(tampered, core.Entry{Key: "extra", Value: core.NullValue()})
	tampered = append(tampered, entries[1:]...)
	bad, err := core.NewObject(tampered...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Diagnostic{}).FromValue(bad, registry)
	if err == nil || protocolCode(err) != "core.protocol.unknown-field@1" {
		t.Errorf("unknown field: got %v", err)
	}
	// Reordered fields are rejected.
	reordered := make([]core.Entry, 0, len(entries))
	reordered = append(reordered, entries[1:]...)
	reordered = append(reordered, entries[0])
	bad, err = core.NewObject(reordered...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Diagnostic{}).FromValue(bad, registry)
	if err == nil || protocolCode(err) != "core.protocol.schema-mismatch@1" {
		t.Errorf("reordered fields: got %v", err)
	}
	// A diagnostic carrying a fix is fully expressible: the wire replacement
	// field is a Bytes leaf carried with byte fidelity (diagnostic.rs
	// and 424-429). The encoder and decoder agree.
	withFix, err := NewDiagnostic("ini.parse.malformed-line@1", CategorySyntax, SeverityError,
		nil, nil, nil, nil, []FixProposal{
			{ID: "fix:quote", Applicability: ApplicabilityManual, Replacement: []byte("x")},
		}, 0, registry)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := withFix.ToValue()
	if err != nil {
		t.Fatalf("ToValue rejected a fix with replacement bytes: %v", err)
	}
	decoded, err := (&Diagnostic{}).FromValue(encoded, registry)
	if err != nil {
		t.Fatalf("FromValue rejected a Bytes fix: %v", err)
	}
	if len(decoded.Fixes) != 1 || string(decoded.Fixes[0].Replacement) != "x" ||
		decoded.Fixes[0].ID != "fix:quote" || decoded.Fixes[0].Applicability != ApplicabilityManual {
		t.Fatalf("fix round-trip changed the proposal: %+v", decoded.Fixes)
	}
	// Null replacement is a wrong-type error, mirroring the Rust as_bytes
	// rejection (diagnostic.rs).
	tampered = append([]core.Entry(nil), encoded.(*core.Object).Entries()...)
	fixArray, ok := tampered[8].Value.(*core.Array)
	if !ok || len(fixArray.Items()) != 1 {
		t.Fatal("fixes field is not a one-item Array")
	}
	fixObject, ok := fixArray.Items()[0].(*core.Object)
	if !ok {
		t.Fatal("fix item is not an Object")
	}
	fixEntries := fixObject.Entries()
	nullReplacement := make([]core.Entry, 0, len(fixEntries))
	for _, entry := range fixEntries {
		if entry.Key == "replacement" {
			nullReplacement = append(nullReplacement, core.Entry{Key: "replacement", Value: core.NullValue()})
			continue
		}
		nullReplacement = append(nullReplacement, entry)
	}
	badFix, err := core.NewObject(nullReplacement...)
	if err != nil {
		t.Fatal(err)
	}
	tampered[8] = core.Entry{Key: "fixes", Value: core.NewArray(badFix)}
	bad, err = core.NewObject(tampered...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Diagnostic{}).FromValue(bad, registry)
	if err == nil || protocolCode(err) != "core.protocol.wrong-type@1" {
		t.Errorf("null fix replacement: got %v, want core.protocol.wrong-type@1", err)
	}
}

func TestSourceLocationValidation(t *testing.T) {
	if _, err := NewSourceLocation("", 0, 1); err == nil {
		t.Error("empty source ID accepted")
	}
	if _, err := NewSourceLocation("s", 5, 3); err == nil {
		t.Error("inverted byte range accepted")
	}
	if _, err := NewSourceLocation("s", 3, 3); err != nil {
		t.Error("empty half-open range rejected")
	}
}
