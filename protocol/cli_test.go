package protocol

import (
	"strings"
	"testing"

	"consema.dev/consema/core"
)

// The RFC 0015 §4.4 envelope example, transcribed byte-for-byte from the
// cli-v1 vector cli.envelope.rfc-canonical-bytes (conformance/vectors/
// cli-v1.json).
const rfcEnvelopeJSON = `{"schema":"core.portable-value-json@1","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.cli-output@1"}},{"key":"command","value":{"type":"String","value":"inspect"}},{"key":"exit_class","value":{"type":"String","value":"success"}},{"key":"product_version","value":{"type":"String","value":"0.12.0"}},{"key":"payload","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"cli.inspect@1"}},{"key":"path","value":{"type":"String","value":"app.conf"}},{"key":"bytes","value":{"type":"Object","entries":[{"key":"size","value":{"type":"Integer","value":"43"}},{"key":"digest","value":{"type":"Object","entries":[{"key":"algorithm","value":{"type":"String","value":"sha256"}},{"key":"hex","value":{"type":"String","value":"2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"}}]}}]}},{"key":"bom","value":{"type":"Null"}},{"key":"symlink","value":{"type":"Boolean","value":false}},{"key":"markers","value":{"type":"Sequence","items":[{"type":"String","value":"[section]"}]}},{"key":"candidates","value":{"type":"Sequence","items":[{"type":"Object","entries":[{"key":"profile","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"ini.portable"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"reason","value":{"type":"String","value":"leading [section] line"}}]}]}},{"key":"ambiguous","value":{"type":"Boolean","value":false}},{"key":"ambiguity_reasons","value":{"type":"Sequence","items":[]}},{"key":"parse","value":{"type":"Null"}}]}},{"key":"diagnostics","value":{"type":"Sequence","items":[]}},{"key":"redaction","value":{"type":"Object","entries":[{"key":"redacted","value":{"type":"Boolean","value":false}},{"key":"count","value":{"type":"Integer","value":"0"}}]}}]}}`

// The RFC 0015 §8.2 plan manifest example, transcribed byte-for-byte from
// the cli-v1 vector cli.batch-plan.rfc-normative-record. It carries Bytes
// replacement leaves, exercising the full-fidelity JSON path (doc.go).
const rfcPlanJSON = `{"schema":"core.portable-value-json@1","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.batch-plan@1"}},{"key":"product_version","value":{"type":"String","value":"0.12.0"}},{"key":"command","value":{"type":"String","value":"plan"}},{"key":"files","value":{"type":"Sequence","items":[{"type":"Object","entries":[{"key":"path","value":{"type":"String","value":"app.conf"}},{"key":"status","value":{"type":"String","value":"planned"}},{"key":"profile","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"ini.portable"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"source_digest","value":{"type":"Object","entries":[{"key":"algorithm","value":{"type":"String","value":"sha256"}},{"key":"hex","value":{"type":"String","value":"03903885bf416e0f6aa8cb034cc0bcf3009ed0ce39a615c3bd5437aac69a2af9"}}]}},{"key":"operations","value":{"type":"Sequence","items":[{"type":"Object","entries":[{"key":"operation","value":{"type":"Object","entries":[{"key":"id","value":{"type":"String","value":"ini.edit.set-entry-value"}},{"key":"version","value":{"type":"Integer","value":"1"}}]}},{"key":"summary","value":{"type":"Object","entries":[{"key":"name","value":{"type":"String","value":"password"}}]}}]}]}},{"key":"source_patch","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.source-patch@2"}},{"key":"base_digest","value":{"type":"Object","entries":[{"key":"algorithm","value":{"type":"String","value":"sha256"}},{"key":"hex","value":{"type":"String","value":"03903885bf416e0f6aa8cb034cc0bcf3009ed0ce39a615c3bd5437aac69a2af9"}}]}},{"key":"target_digest","value":{"type":"Object","entries":[{"key":"algorithm","value":{"type":"String","value":"sha256"}},{"key":"hex","value":{"type":"String","value":"77b7ea230beae079972e4223c68a0715afcb7e36c3bda4c439e43a4a22630bd6"}}]}},{"key":"encoding","value":{"type":"Object","entries":[{"key":"profile_default","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.source-encoding@1"}},{"key":"kind","value":{"type":"String","value":"Utf8"}},{"key":"windows_code_page","value":{"type":"Null"}}]}},{"key":"bom_policy","value":{"type":"String","value":"DetectUnicode"}},{"key":"bom","value":{"type":"Null"}},{"key":"declaration","value":{"type":"Null"}},{"key":"caller_override","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.source-encoding@1"}},{"key":"kind","value":{"type":"String","value":"Utf8"}},{"key":"windows_code_page","value":{"type":"Null"}}]}},{"key":"selected","value":{"type":"Object","entries":[{"key":"schema","value":{"type":"String","value":"core.source-encoding@1"}},{"key":"kind","value":{"type":"String","value":"Utf8"}},{"key":"windows_code_page","value":{"type":"Null"}}]}}]}},{"key":"replacements","value":{"type":"Sequence","items":[{"type":"Object","entries":[{"key":"old_start","value":{"type":"Integer","value":"16"}},{"key":"old_end","value":{"type":"Integer","value":"19"}},{"key":"original","value":{"type":"Bytes","hex":"6f6c64"}},{"key":"replacement","value":{"type":"Bytes","hex":"6e6577"}},{"key":"redact_original","value":{"type":"Boolean","value":false}},{"key":"redact_replacement","value":{"type":"Boolean","value":false}}]}]}},{"key":"metadata","value":{"type":"Object","entries":[]}}]}},{"key":"failure_code","value":{"type":"Null"}},{"key":"diagnostics","value":{"type":"Null"}}]}]}}]}}`

// The envelope's pinned PVCE/1 bytes from the same vector.
const rfcEnvelopePVCEHex = "505643450141980407200706736368656d61201211636f72652e636c692d6f75747075744031200807636f6d6d616e64200807696e7370656374200b0a657869745f636c6173732008077375636365737320100f70726f647563745f76657273696f6e200706302e31322e302008077061796c6f616441ee020a200706736368656d61200e0d636c692e696e73706563744031200504706174682009086170702e636f6e66200605627974657341770220050473697a65100301012b200706646967657374415f02200a09616c676f726974686d20070673686132353620040368657820414032633236623436623638666663363866663939623435336331643330343133343133343232643730363438336266613066393861356538383632363665376165200403626f6d000020080773796d6c696e6b01002008076d61726b657273400d01200a095b73656374696f6e5d200b0a63616e6469646174657340560141530220080770726f66696c654124022003026964200d0c696e692e706f727461626c6520080776657273696f6e1003010101200706726561736f6e2017166c656164696e67205b73656374696f6e5d206c696e65200a09616d626967756f75730100201211616d626967756974795f726561736f6e7340010020060570617273650000200c0b646961676e6f7374696373400100200a09726564616374696f6e411a0220090872656461637465640100200605636f756e7410020000"

func rfcInspectPayload(t *testing.T) core.Value {
	t.Helper()
	value, err := DecodeJSON([]byte(rfcEnvelopeJSON), DefaultProtocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	envelope, ok := value.(*core.Object)
	if !ok {
		t.Fatal("envelope is not an object")
	}
	payload, _ := envelope.Get("payload")
	return payload
}

func TestCLIEnvelopeVectorBytesMatchExactly(t *testing.T) {
	limits := DefaultProtocolLimits()
	// The RFC 0015 §4.4 envelope decodes, re-encodes byte-exactly, and
	// round-trips the CLI record.
	envelope, err := (&CliOutputMessage{}).FromJSON([]byte(rfcEnvelopeJSON), limits)
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Command() != CommandInspect || envelope.ExitClass() != ExitSuccess ||
		envelope.ProductVersion() != "0.12.0" {
		t.Error("envelope facts wrong")
	}
	if envelope.Redaction().Redacted() || envelope.Redaction().Count() != 0 {
		t.Error("redaction facts wrong")
	}
	encoded, err := envelope.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != rfcEnvelopeJSON {
		t.Error("envelope canonical bytes drifted from the vector")
	}
	// The PVCE/1 bytes match the vector pin exactly (cross-language byte
	// parity, roadmap §16.1).
	pvce, err := envelope.ToPVCE(limits)
	if err != nil {
		t.Fatal(err)
	}
	if hexEncode(pvce) != rfcEnvelopePVCEHex {
		t.Error("envelope PVCE bytes drifted from the vector pin")
	}
	decodedPVCE, err := envelope.FromPVCE(pvce, limits)
	if err != nil {
		t.Fatal(err)
	}
	if decodedPVCE.ProductVersion() != "0.12.0" {
		t.Error("PVCE round-trip lost facts")
	}
}

func hexEncode(bytes []byte) string {
	const hexDigits = "0123456789abcdef"
	text := make([]byte, 0, len(bytes)*2)
	for _, byte := range bytes {
		text = append(text, hexDigits[byte>>4], hexDigits[byte&0x0f])
	}
	return string(text)
}

func TestCLIEnvelopeRejectionRules(t *testing.T) {
	// The payload schema must be published by the command (cli.rs:1494-1565).
	payload := rfcInspectPayload(t)
	redaction, err := NewRedaction(false, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewCliOutputMessage(CommandInspect, ExitSuccess, "0.12.0",
		valueObject(core.Entry{Key: "schema", Value: core.String("cli.explain@1")}),
		nil, redaction)
	if err == nil || protocolCode(err) != "core.protocol.schema-mismatch@1" {
		t.Errorf("payload mismatch: got %v", err)
	}
	// The Query command accepts every RFC 0015 §6.1 query-result schema.
	for _, schema := range []string{
		"core.query-result@1", "core.ini-query-result@1",
		"core.java-properties-query-result@1", "core.yaml-query-result@1",
		"core.graph-query-result@1",
	} {
		if _, err := NewCliOutputMessage(CommandQuery, ExitSuccess, "0.12.0",
			valueObject(core.Entry{Key: "schema", Value: core.String(schema)}),
			nil, redaction); err != nil {
			t.Errorf("query schema %s rejected: %v", schema, err)
		}
	}
	// A non-object payload is rejected.
	_, err = NewCliOutputMessage(CommandQuery, ExitSuccess, "0.12.0",
		core.String("core.query-result@1"), nil, redaction)
	if err == nil || protocolCode(err) != "core.protocol.wrong-type@1" {
		t.Errorf("non-object payload: got %v", err)
	}
	// The schema must be the first payload field.
	reordered, err := core.NewObject(
		core.Entry{Key: "path", Value: core.String("x")},
		core.Entry{Key: "schema", Value: core.String("cli.inspect@1")},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewCliOutputMessage(CommandInspect, ExitSuccess, "0.12.0", reordered, nil, redaction)
	if err == nil || protocolCode(err) != "core.protocol.schema-mismatch@1" {
		t.Errorf("reordered payload: got %v", err)
	}
	// Invalid product versions (cli.rs:1568-1586).
	for _, version := range []string{"0.12", "0.12.0.1", "0.12.01", "00.1.0", "0.12.a", "0..0"} {
		_, err := NewCliOutputMessage(CommandInspect, ExitSuccess, version, payload, nil, redaction)
		protocolErr, _ := err.(*ProtocolError)
		if protocolErr == nil || protocolErr.Path != "$.product_version" {
			t.Errorf("version %q: got %v", version, err)
		}
	}
	// Redaction invariant (cli.rs:1581-1585).
	if _, err := NewRedaction(true, 0); err == nil {
		t.Error("redacted with count 0 accepted")
	}
	if _, err := NewRedaction(false, 3); err == nil {
		t.Error("not redacted with count 3 accepted")
	}
	if redacted, err := NewRedaction(true, 3); err != nil || redacted.Count() != 3 {
		t.Error("valid redaction rejected")
	}
}

func TestCLIEnvelopeDiagnosticsRegistryBinding(t *testing.T) {
	registry := v7ErrorRegistry()
	diagnostic, err := NewDiagnostic("cli.limit.file-size@1", CategoryResource, SeverityError,
		nil, nil, nil, nil, nil, 0, registry)
	if err != nil {
		t.Fatal(err)
	}
	redaction, err := NewRedaction(false, 0)
	if err != nil {
		t.Fatal(err)
	}
	message, err := NewCliOutputMessage(CommandInspect, ExitLimit, "0.12.0",
		rfcInspectPayload(t), []*Diagnostic{diagnostic}, redaction)
	if err != nil {
		t.Fatal(err)
	}
	value, err := message.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&CliOutputMessage{}).FromValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Diagnostics()) != 1 || decoded.Diagnostics()[0].Code != "cli.limit.file-size@1" {
		t.Error("diagnostic round-trip lost the code")
	}
	// An unregistered code is rejected by the envelope constructor. The
	// public diagnostic constructor already validates, so the tampered code
	// is injected directly into the record fields.
	valid, err := NewDiagnostic("ini.parse.malformed-line@1", CategorySyntax, SeverityError,
		nil, nil, nil, nil, nil, 0, registry)
	if err != nil {
		t.Fatal(err)
	}
	tampered := &Diagnostic{Code: "example.unknown@1", Category: valid.Category,
		Severity: valid.Severity, Arguments: map[string]string{}, Occurrence: 0}
	_, err = NewCliOutputMessage(CommandInspect, ExitSuccess, "0.12.0",
		rfcInspectPayload(t), []*Diagnostic{tampered}, redaction)
	if err == nil {
		t.Error("envelope accepted an unregistered diagnostic code")
	}
}

func TestBatchPlanVectorRoundTripsThroughJSON(t *testing.T) {
	limits := DefaultProtocolLimits()
	plan, err := (&BatchPlanMessage{}).FromJSON([]byte(rfcPlanJSON), limits)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ProductVersion() != "0.12.0" || len(plan.Files()) != 1 {
		t.Fatal("plan facts wrong")
	}
	entry := plan.Files()[0]
	if entry.Status() != PlanStatusPlanned || entry.Path() != "app.conf" {
		t.Error("entry facts wrong")
	}
	// The vector pins the source and target digests.
	if entry.SourceDigest().Hex() != "03903885bf416e0f6aa8cb034cc0bcf3009ed0ce39a615c3bd5437aac69a2af9" {
		t.Error("source digest wrong")
	}
	if entry.SourcePatch().TargetDigest.Hex() != "77b7ea230beae079972e4223c68a0715afcb7e36c3bda4c439e43a4a22630bd6" {
		t.Error("target digest wrong")
	}
	// The Bytes replacement leaves survive the tree codec.
	if len(entry.SourcePatch().Replacements) != 1 {
		t.Fatal("replacement lost")
	}
	replacement := entry.SourcePatch().Replacements[0]
	if string(replacement.Original) != "old" || string(replacement.Replacement) != "new" {
		t.Error("replacement bytes wrong")
	}
	// The re-encode is byte-exact.
	encoded, err := plan.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != rfcPlanJSON {
		t.Error("plan canonical bytes drifted from the vector")
	}
}

func TestBatchPlanCrossConstraintsAreEnforced(t *testing.T) {
	limits := DefaultProtocolLimits()
	registry := v7ErrorRegistry()
	profile, err := NewProfileReference("ini.portable", 1)
	if err != nil {
		t.Fatal(err)
	}
	baseDigest := DigestOf([]byte("base"))
	targetDigest := DigestOf([]byte("target"))
	operation, err := NewEditOperationSummary(
		NewFormatOperationId("ini.edit.set-entry-value", 1),
		map[string]string{"name": "password"})
	if err != nil {
		t.Fatal(err)
	}
	patch := &SourcePatch{
		BaseDigest:   baseDigest,
		TargetDigest: targetDigest,
		Encoding: EncodingFacts{
			ProfileDefault: &SourceEncoding{Kind: "Utf8"},
			BomPolicy:      "DetectUnicode",
			Selected:       &SourceEncoding{Kind: "Utf8"},
		},
		Metadata: map[string]string{},
	}
	// source_digest != base_digest is rejected at $.files[].source_digest
	// (cli.batch-plan.reject-source-digest-mismatch).
	_, err = NewBatchPlanFileEntry("app.conf", PlanStatusPlanned, profile,
		&targetDigest, []*EditOperationSummary{operation}, patch, nil, nil, registry)
	protocolErr, _ := err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[].source_digest" {
		t.Errorf("source digest mismatch: got %v", err)
	}
	// A planned entry cannot carry failure facts.
	failureCode := "ini.parse.malformed-line@1"
	_, err = NewBatchPlanFileEntry("app.conf", PlanStatusPlanned, profile,
		&baseDigest, []*EditOperationSummary{operation}, patch, &failureCode, nil, registry)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[]" {
		t.Errorf("planned with failure facts: got %v", err)
	}
	// A failed entry needs failure_code and diagnostics
	// (cli.batch-plan.reject-failed-without-diagnostics).
	_, err = NewBatchPlanFileEntry("broken.conf", PlanStatusFailed, nil, nil,
		nil, nil, nil, []*Diagnostic{}, registry)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[].failure_code" {
		t.Errorf("failed without code: got %v", err)
	}
	// Failed entries cannot carry planning facts
	// (cli.batch-plan.reject-failed-with-planning-facts).
	_, err = NewBatchPlanFileEntry("broken.conf", PlanStatusFailed, profile, nil,
		nil, nil, &failureCode, []*Diagnostic{}, registry)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[]" {
		t.Errorf("failed with planning facts: got %v", err)
	}
	// An empty or oversized path is rejected.
	_, err = NewBatchPlanFileEntry("", PlanStatusFailed, nil, nil, nil, nil,
		&failureCode, []*Diagnostic{}, registry)
	if err == nil {
		t.Error("empty path accepted")
	}
	// The manifest rejects a non-plan command
	// (cli.batch-plan.reject-command-fixed).
	plan, err := NewBatchPlanMessage("0.12.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := plan.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	fields := value.(*core.Object).Entries()
	replaced := make([]core.Entry, 0, len(fields))
	for _, field := range fields {
		replacement := field.Value
		if field.Key == "command" {
			replacement = core.String("apply")
		}
		replaced = append(replaced, core.Entry{Key: field.Key, Value: replacement})
	}
	badValue, err := core.NewObject(replaced...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&BatchPlanMessage{}).FromValue(badValue)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.command" {
		t.Errorf("command fixed: got %v", err)
	}
	// The value-level codec carries byte-bearing patches with full
	// fidelity (the cli-v1 vectors decode the plan records through
	// canonical JSON into the value model).
	patchWithReplacement := &SourcePatch{
		BaseDigest:   baseDigest,
		TargetDigest: targetDigest,
		Encoding: EncodingFacts{
			ProfileDefault: &SourceEncoding{Kind: "Utf8"},
			BomPolicy:      "DetectUnicode",
			Selected:       &SourceEncoding{Kind: "Utf8"},
		},
		Replacements: []SourceReplacement{{OldStart: 16, OldEnd: 19, Original: []byte("old"), Replacement: []byte("new")}},
		Metadata:     map[string]string{},
	}
	entry, err := NewBatchPlanFileEntry("app.conf", PlanStatusPlanned, profile,
		&baseDigest, []*EditOperationSummary{operation}, patchWithReplacement, nil, nil, registry)
	if err != nil {
		t.Fatal(err)
	}
	entryValue, err := planEntryValue(entry, 0)
	if err != nil {
		t.Fatalf("value codec rejected a byte-bearing patch: %v", err)
	}
	redecoded, err := parsePlanEntry(entryValue, 0, registry, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("value codec lost a byte-bearing patch: %v", err)
	}
	if len(redecoded.SourcePatch().Replacements) != 1 ||
		redecoded.SourcePatch().Replacements[0].OldStart != 16 ||
		string(redecoded.SourcePatch().Replacements[0].Original) != "old" {
		t.Error("value round-trip lost replacement facts")
	}
	// ... and the JSON tree codec round-trips it.
	planWithPatch, err := NewBatchPlanMessage("0.12.0", []*BatchPlanFileEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := planWithPatch.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&BatchPlanMessage{}).FromJSON(jsonBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Files()[0].SourcePatch().Replacements[0].OldStart != 16 {
		t.Error("JSON round-trip lost replacement facts")
	}
}

func TestBatchPlanVectorRejectionInputs(t *testing.T) {
	// The cli-v1 rejection vectors, rebuilt inputs with their pinned error
	// codes and paths. Each case builds the minimal canonical plan document
	// carrying the offending fact.
	planJSON := func(command, sourceDigestHex, sourcePatch string) string {
		entry := `{"key":"path","value":{"type":"String","value":"app.conf"}},` +
			`{"key":"status","value":{"type":"String","value":"planned"}},` +
			`{"key":"profile","value":{"type":"Object","entries":[` +
			`{"key":"id","value":{"type":"String","value":"ini.portable"}},` +
			`{"key":"version","value":{"type":"Integer","value":"1"}}]}},` +
			`{"key":"source_digest","value":{"type":"Object","entries":[` +
			`{"key":"algorithm","value":{"type":"String","value":"sha256"}},` +
			`{"key":"hex","value":{"type":"String","value":"` + sourceDigestHex + `"}}]}},` +
			`{"key":"operations","value":{"type":"Sequence","items":[]}},` +
			`{"key":"source_patch","value":` + sourcePatch + `},` +
			`{"key":"failure_code","value":{"type":"Null"}},` +
			`{"key":"diagnostics","value":{"type":"Null"}}`
		return `{"schema":"core.portable-value-json@1","value":{"type":"Object","entries":[` +
			`{"key":"schema","value":{"type":"String","value":"core.batch-plan@1"}},` +
			`{"key":"product_version","value":{"type":"String","value":"0.12.0"}},` +
			`{"key":"command","value":{"type":"String","value":"` + command + `"}},` +
			`{"key":"files","value":{"type":"Sequence","items":[{"type":"Object","entries":[` +
			entry + `]}]}}]}}`
	}
	patch := `{"type":"Object","entries":[` +
		`{"key":"schema","value":{"type":"String","value":"core.source-patch@2"}},` +
		`{"key":"base_digest","value":{"type":"Object","entries":[` +
		`{"key":"algorithm","value":{"type":"String","value":"sha256"}},` +
		`{"key":"hex","value":{"type":"String","value":"03903885bf416e0f6aa8cb034cc0bcf3009ed0ce39a615c3bd5437aac69a2af9"}}]}},` +
		`{"key":"target_digest","value":{"type":"Object","entries":[` +
		`{"key":"algorithm","value":{"type":"String","value":"sha256"}},` +
		`{"key":"hex","value":{"type":"String","value":"77b7ea230beae079972e4223c68a0715afcb7e36c3bda4c439e43a4a22630bd6"}}]}},` +
		`{"key":"encoding","value":{"type":"Object","entries":[` +
		`{"key":"profile_default","value":{"type":"Null"}},` +
		`{"key":"bom_policy","value":{"type":"String","value":"DetectUnicode"}},` +
		`{"key":"bom","value":{"type":"Null"}},` +
		`{"key":"declaration","value":{"type":"Null"}},` +
		`{"key":"caller_override","value":{"type":"Null"}},` +
		`{"key":"selected","value":{"type":"Object","entries":[` +
		`{"key":"schema","value":{"type":"String","value":"core.source-encoding@1"}},` +
		`{"key":"kind","value":{"type":"String","value":"Utf8"}},` +
		`{"key":"windows_code_page","value":{"type":"Null"}}]}}]}},` +
		`{"key":"replacements","value":{"type":"Sequence","items":[{"type":"Object","entries":[` +
		`{"key":"old_start","value":{"type":"Integer","value":"16"}},` +
		`{"key":"old_end","value":{"type":"Integer","value":"19"}},` +
		`{"key":"original","value":{"type":"Bytes","hex":"6f6c64"}},` +
		`{"key":"replacement","value":{"type":"Bytes","hex":"6e6577"}},` +
		`{"key":"redact_original","value":{"type":"Boolean","value":false}},` +
		`{"key":"redact_replacement","value":{"type":"Boolean","value":false}}]}]}},` +
		`{"key":"metadata","value":{"type":"Object","entries":[]}}]}`
	validDigest := "03903885bf416e0f6aa8cb034cc0bcf3009ed0ce39a615c3bd5437aac69a2af9"
	cases := []struct {
		id    string
		input string
		code  string
		path  string
	}{
		// The source_digest hex "0" fails the digest parse at the entry
		// path (cli.batch-plan.reject-source-digest-mismatch).
		{
			"reject-source-digest-mismatch",
			planJSON("plan", "0", patch),
			"core.protocol.invalid-value@1",
			"$.files[0].source_digest",
		},
		// The command member is fixed to "plan"
		// (cli.batch-plan.reject-command-fixed).
		{
			"reject-command-fixed",
			planJSON("apply", validDigest, patch),
			"core.protocol.invalid-value@1",
			"$.command",
		},
		// A planned entry whose source_patch is Null is a wrong type
		// (cli.batch-plan.reject-planned-without-patch).
		{
			"reject-planned-without-patch",
			planJSON("plan", validDigest, `{"type":"Null"}`),
			"core.protocol.wrong-type@1",
			"",
		},
	}
	limits := DefaultProtocolLimits()
	for _, testCase := range cases {
		t.Run(testCase.id, func(t *testing.T) {
			_, err := (&BatchPlanMessage{}).FromJSON([]byte(testCase.input), limits)
			if err == nil {
				t.Fatalf("expected %s", testCase.code)
			}
			if protocolCode(err) != testCase.code {
				t.Fatalf("code = %s, want %s (err: %v)", protocolCode(err), testCase.code, err)
			}
			if testCase.path != "" {
				protocolErr, _ := err.(*ProtocolError)
				if protocolErr == nil || protocolErr.Path != testCase.path {
					t.Fatalf("path = %v, want %s", err, testCase.path)
				}
			}
		})
	}
}

func TestBatchResultRoundTripsWithAllStatuses(t *testing.T) {
	target := DigestOf([]byte("target"))
	completed, err := NewBatchResultFileEntry("app.conf", ResultStatusCompleted,
		nil, &target, true)
	if err != nil {
		t.Fatal(err)
	}
	failureCode := "core.source.patch-original-mismatch@1"
	failed, err := NewBatchResultFileEntry("broken.conf", ResultStatusFailed,
		&failureCode, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	staleCode := "core.source.patch-base-mismatch@1"
	stale, err := NewBatchResultFileEntry("stale.conf", ResultStatusSkippedStale,
		&staleCode, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := NewBatchResultFileEntry("pending.conf", ResultStatusPending,
		nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewBatchResultMessage("0.12.0",
		[]*BatchResultFileEntry{completed, failed, stale, pending})
	if err != nil {
		t.Fatal(err)
	}
	limits := DefaultProtocolLimits()
	jsonBytes, err := result.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (&BatchResultMessage{}).FromJSON(jsonBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Files()) != 4 {
		t.Fatal("file count wrong")
	}
	if decoded.Files()[0].Status() != ResultStatusCompleted ||
		decoded.Files()[0].TargetDigest().Hex() != target.Hex() || !decoded.Files()[0].Redacted() {
		t.Error("completed entry facts wrong")
	}
	if decoded.Files()[1].FailureCode() == nil || *decoded.Files()[1].FailureCode() != failureCode {
		t.Error("failed entry facts wrong")
	}
	if decoded.Files()[2].Status() != ResultStatusSkippedStale {
		t.Error("skipped-stale entry facts wrong")
	}
	if decoded.Files()[3].Status() != ResultStatusPending {
		t.Error("pending entry facts wrong")
	}
	// The manifest's command member is fixed to "apply".
	value, err := result.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	command, _ := value.(*core.Object).Get("command")
	if command != core.String("apply") {
		t.Error("batch-result command is not fixed to apply")
	}
	pvceBytes, err := result.ToPVCE(limits)
	if err != nil {
		t.Fatal(err)
	}
	decodedPVCE, err := (&BatchResultMessage{}).FromPVCE(pvceBytes, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(decodedPVCE.Files()) != 4 {
		t.Error("PVCE round-trip lost files")
	}
}

func TestBatchResultStatusPresenceRulesAreEnforced(t *testing.T) {
	target := DigestOf([]byte("target"))
	// Completed requires a target digest and no failure code.
	_, err := NewBatchResultFileEntry("app.conf", ResultStatusCompleted, nil, nil, false)
	protocolErr, _ := err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[]" {
		t.Errorf("completed without digest: got %v", err)
	}
	failureCode := "cli.write.io@1"
	_, err = NewBatchResultFileEntry("app.conf", ResultStatusCompleted, &failureCode, &target, false)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[]" {
		t.Errorf("completed with failure code: got %v", err)
	}
	// Failed/skipped-stale require a failure code and no target digest.
	for _, status := range []BatchResultFileStatus{ResultStatusFailed, ResultStatusSkippedStale} {
		_, err := NewBatchResultFileEntry("x.conf", status, nil, nil, false)
		protocolErr, _ := err.(*ProtocolError)
		if protocolErr == nil || protocolErr.Path != "$.files[]" {
			t.Errorf("%s without code: got %v", status, err)
		}
		_, err = NewBatchResultFileEntry("x.conf", status, &failureCode, &target, false)
		protocolErr, _ = err.(*ProtocolError)
		if protocolErr == nil || protocolErr.Path != "$.files[]" {
			t.Errorf("%s with digest: got %v", status, err)
		}
	}
	// Pending carries neither field.
	_, err = NewBatchResultFileEntry("p.conf", ResultStatusPending, &failureCode, nil, false)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[]" {
		t.Errorf("pending with facts: got %v", err)
	}
	// An unknown status is rejected by the decoder.
	unknown, err := core.NewObject(
		core.Entry{Key: "path", Value: core.String("x.conf")},
		core.Entry{Key: "status", Value: core.String("committed")},
		core.Entry{Key: "failure_code", Value: core.NullValue()},
		core.Entry{Key: "target_digest", Value: core.NullValue()},
		core.Entry{Key: "redacted", Value: core.Boolean(false)},
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseResultEntry(unknown, "$.files[0]")
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.files[0].status" {
		t.Errorf("unknown status: got %v", err)
	}
	// A batch-result manifest with the wrong command is rejected.
	result, err := NewBatchResultMessage("0.12.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	value, err := result.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	fields := value.(*core.Object).Entries()
	replaced := make([]core.Entry, 0, len(fields))
	for _, field := range fields {
		replacement := field.Value
		if field.Key == "command" {
			replacement = core.String("plan")
		}
		replaced = append(replaced, core.Entry{Key: field.Key, Value: replacement})
	}
	badValue, err := core.NewObject(replaced...)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&BatchResultMessage{}).FromValue(badValue)
	protocolErr, _ = err.(*ProtocolError)
	if protocolErr == nil || protocolErr.Path != "$.command" {
		t.Errorf("result command fixed: got %v", err)
	}
}

func TestDigestParsingIsStrict(t *testing.T) {
	digest := DigestOf([]byte("x"))
	value := digestValue(digest)
	decoded, err := parseDigest(value, "$.digest")
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(digest) {
		t.Error("digest round-trip changed")
	}
	// A wrong algorithm is rejected.
	badAlgorithm, _ := core.NewObject(
		core.Entry{Key: "algorithm", Value: core.String("md5")},
		core.Entry{Key: "hex", Value: core.String(digest.Hex())},
	)
	if _, err := parseDigest(badAlgorithm, "$.digest"); err == nil {
		t.Error("non-sha256 digest accepted")
	}
	// Uppercase hex is rejected (canonical form is lowercase).
	upperHex, _ := core.NewObject(
		core.Entry{Key: "algorithm", Value: core.String("sha256")},
		core.Entry{Key: "hex", Value: core.String(strings.ToUpper(digest.Hex()))},
	)
	if _, err := parseDigest(upperHex, "$.digest"); err == nil {
		t.Error("uppercase hex accepted")
	}
}

func TestCLIEnvelopeWrapsAsProtocolMessage(t *testing.T) {
	// core.cli-output@1 is registered in v7 and must be envelopeable.
	limits := DefaultProtocolLimits()
	envelope, err := (&CliOutputMessage{}).FromJSON([]byte(rfcEnvelopeJSON), limits)
	if err != nil {
		t.Fatal(err)
	}
	value, err := envelope.ToValue()
	if err != nil {
		t.Fatal(err)
	}
	contract, err := NewContractId("core.cli-output", 1)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewContractRegistry(RegistryV7)
	message, err := NewProtocolMessage(contract, value, registry)
	if err != nil {
		t.Fatal(err)
	}
	jsonBytes, err := message.ToJSON(limits)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := message.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Contract().Schema() != "core.cli-output@1" {
		t.Error("cli-output envelope round-trip lost the contract")
	}
}
