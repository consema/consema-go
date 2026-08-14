package main

import (
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// projectRequestBytes builds one strict cli.request@1 wrapper with a
// core.projection-request@1 payload for the given target.
func projectRequestBytes(t *testing.T, sourceHex, profileID, targetID string) []byte {
	t.Helper()
	target, err := core.NewObject(
		core.Entry{Key: "id", Value: core.String(targetID)},
		core.Entry{Key: "version", Value: coreInteger(1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := core.NewObject(
		core.Entry{Key: "id", Value: core.String(exactOrRejectContract)},
		core.Entry{Key: "version", Value: coreInteger(1)},
		core.Entry{Key: "arguments", Value: emptyObject(t)},
	)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.projection-request@1")},
		core.Entry{Key: "target", Value: target},
		core.Entry{Key: "default_policy", Value: policy},
		core.Entry{Key: "rules", Value: core.NewArray()},
		core.Entry{Key: "limits", Value: emptyObject(t)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return requestWrapperBytes(t, sourceHex, profileID, payload)
}

// materializeRequestBytes builds one strict cli.request@1 wrapper with a
// core.materialization-request@2 payload for the given target profile and
// style.
func materializeRequestBytes(t *testing.T, sourceHex, profileID, targetProfile,
	styleID string) []byte {
	t.Helper()
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-request@2")},
		core.Entry{Key: "target_profile", Value: referenceValue(targetProfile, 1)},
		core.Entry{Key: "style", Value: referenceValue(styleID, 1)},
		core.Entry{Key: "encoding", Value: encodingRecord(t, "Utf8")},
		core.Entry{Key: "newline", Value: core.String("Lf")},
		core.Entry{Key: "mapping_policy", Value: core.String("UniqueStringEntriesToObject")},
		core.Entry{Key: "representability", Value: core.String("ExactOnly")},
		core.Entry{Key: "limits", Value: materializationLimitsRecord(t)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return requestWrapperBytes(t, sourceHex, profileID, payload)
}

// convertRequestBytes builds one cli.convert-request@1 record (source is
// the positional path).
func convertRequestBytes(t *testing.T, sourceHex, profileID, targetID,
	targetProfile, styleID string) []byte {
	t.Helper()
	projection, err := protocol.DecodeJSON(
		projectRequestBytes(t, sourceHex, profileID, targetID), protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	projectionObject := projection.(*core.Object)
	payloadObject := projectionObject.Entries()[3].Value.(*core.Object)
	materialization, err := protocol.DecodeJSON(
		materializeRequestBytesForConvert(t, targetProfile, styleID), protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	materializationObject := materialization.(*core.Object)
	matPayload := materializationObject.Entries()[3].Value
	request, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(convertRequestSchema)},
		core.Entry{Key: "projection_request", Value: payloadObject},
		core.Entry{Key: "materialization_request", Value: matPayload},
	)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := protocol.EncodeJSON(request, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

// materializeRequestBytesForConvert builds the materialization payload
// record directly (no source wrapper).
func materializeRequestBytesForConvert(t *testing.T, targetProfile,
	styleID string) []byte {
	t.Helper()
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.materialization-request@2")},
		core.Entry{Key: "target_profile", Value: referenceValue(targetProfile, 1)},
		core.Entry{Key: "style", Value: referenceValue(styleID, 1)},
		core.Entry{Key: "encoding", Value: encodingRecord(t, "Utf8")},
		core.Entry{Key: "newline", Value: core.String("Lf")},
		core.Entry{Key: "mapping_policy", Value: core.String("UniqueStringEntriesToObject")},
		core.Entry{Key: "representability", Value: core.String("ExactOnly")},
		core.Entry{Key: "limits", Value: materializationLimitsRecord(t)},
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(requestSchema)},
		core.Entry{Key: "source", Value: bytesSource(t, "7b7d")},
		core.Entry{Key: "profile", Value: referenceValue("json.strict", 1)},
		core.Entry{Key: "payload", Value: payload},
	)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := protocol.EncodeJSON(wrapper, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

// requestWrapperBytes builds one cli.request@1 wrapper around a payload.
func requestWrapperBytes(t *testing.T, sourceHex, profileID string,
	payload core.Value) []byte {
	t.Helper()
	wrapper, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(requestSchema)},
		core.Entry{Key: "source", Value: bytesSource(t, sourceHex)},
		core.Entry{Key: "profile", Value: referenceValue(profileID, 1)},
		core.Entry{Key: "payload", Value: payload},
	)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := protocol.EncodeJSON(wrapper, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	return bytes
}

func bytesSource(t *testing.T, hex string) core.Value {
	t.Helper()
	source, err := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("bytes")},
		core.Entry{Key: "bytes", Value: core.String(hex)},
	)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func emptyObject(t *testing.T) core.Value {
	t.Helper()
	value, err := core.NewObject()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func encodingRecord(t *testing.T, kind string) core.Value {
	t.Helper()
	value, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.source-encoding@1")},
		core.Entry{Key: "kind", Value: core.String(kind)},
		core.Entry{Key: "windows_code_page", Value: core.NullValue()},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func materializationLimitsRecord(t *testing.T) core.Value {
	t.Helper()
	limits := protocol.DefaultMaterializationLimits()
	value, err := core.NewObject(
		core.Entry{Key: "max_input_nodes", Value: coreInteger(uint64(limits.MaxInputNodes))},
		core.Entry{Key: "max_output_bytes", Value: coreInteger(uint64(limits.MaxOutputBytes))},
		core.Entry{Key: "max_depth", Value: coreInteger(uint64(limits.MaxDepth))},
		core.Entry{Key: "max_report_entries", Value: coreInteger(uint64(limits.MaxReportEntries))},
		core.Entry{Key: "max_provenance_entries", Value: coreInteger(uint64(limits.MaxProvenanceEntries))},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestProjectJSONSuccessRoundTripsAndIsByteDeterministic(t *testing.T) {
	request := projectRequestBytes(t, "7b2261223a312c2262223a327d", "json.strict",
		"json.projection.best-exact-core")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"project", "--profile", "json.strict", "--json"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "project" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	again, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(stdout[:len(stdout)-1]) {
		t.Fatal("stdout envelope must be byte-deterministic")
	}
	// Round-trip through the typed projection decoder.
	result := &protocol.ProjectionResultMessage{}
	result, err = result.FromValue(envelope.Payload())
	if err != nil {
		t.Fatalf("projection-result record: %v", err)
	}
	if result.Completion().Status() != protocol.CompletionSuccess {
		t.Fatalf("completion = %v", result.Completion().Status())
	}
	value, hasValue := result.Value()
	if !hasValue || value.(*core.Object).Entries()[0].Key != "a" {
		t.Fatalf("value = %v", value)
	}
	// Provenance is externalized with byte spans and no locators.
	provenance := result.Provenance()
	if len(provenance.Entries()) == 0 {
		t.Fatal("provenance must be externalized")
	}
	for _, entry := range provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.NodeLocator != nil {
				t.Fatal("provenance must carry no node locators")
			}
		}
	}
}

func TestProjectJSONDuplicateKeysFailAsDataError(t *testing.T) {
	request := projectRequestBytes(t, "7b2261223a312c2261223a327d", "json.strict",
		"json.projection.project-as-object")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"project", "--profile", "json.strict", "--json"}, request)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "json.projection.duplicate-keys@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestProjectUsageAndDataRejections(t *testing.T) {
	// Unknown --profile -> usage exit 1, no envelope.
	request := projectRequestBytes(t, "7b7d", "json.strict",
		"json.projection.best-exact-core")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"project", "--profile", "json.bogus", "--json"}, request)
	if code != 1 || len(stdout) != 0 {
		t.Fatalf("exit = %d, stdout = %s", code, stdout)
	}
	if !strings.Contains(stderrText(stderr), "unknown profile 'json.bogus'") {
		t.Fatalf("stderr = %s", stderr)
	}
	// A target outside the family -> data error.
	code, _, stderr = runRequestCommand(t,
		[]string{"project", "--profile", "json.strict", "--json"},
		projectRequestBytes(t, "7b7d", "json.strict", "toml.projection.best-exact-core"))
	if code != 2 || !strings.Contains(stderrText(stderr), "does not belong to the 'json' format family") {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	// An unknown target -> data error.
	code, _, stderr = runRequestCommand(t,
		[]string{"project", "--profile", "json.strict", "--json"},
		projectRequestBytes(t, "7b7d", "json.strict", "json.projection.frobnicate"))
	if code != 2 || !strings.Contains(stderrText(stderr), "not published by this build") {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	// The family gate: ini is not wired for project.
	code, _, stderr = runRequestCommand(t,
		[]string{"project", "--profile", "ini.portable", "--json"},
		projectRequestBytes(t, "6e616d653d6170690a", "ini.portable",
			"ini.projection.best-exact-entry-mapping"))
	if code != 2 || !strings.Contains(stderrText(stderr), "not wired for the 'ini' family") {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
}

func TestMaterializeJSONToTOMLRoundTripsAndIsByteDeterministic(t *testing.T) {
	request := materializeRequestBytes(t, "7b2261223a312c2262223a2278227d", "json.strict",
		"toml.1.0", "toml.canonical-document")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"materialize", "--profile", "json.strict", "--json"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "materialize" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	again, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(stdout[:len(stdout)-1]) {
		t.Fatal("stdout envelope must be byte-deterministic")
	}
	// The payload decodes through the typed v2 decoder.
	result := &protocol.MaterializationResultMessageV2{}
	result, err = result.FromValue(envelope.Payload())
	if err != nil {
		t.Fatalf("materialization-result@2 record: %v", err)
	}
	targetProfile := result.TargetProfile()
	if targetProfile.ID() != "toml.1.0" {
		t.Fatalf("target = %v", targetProfile.ID())
	}
}

func TestMaterializeHumanModeWritesRawBytesToStdout(t *testing.T) {
	request := materializeRequestBytes(t, "7b2261223a317d", "json.strict",
		"json.strict", "json.canonical-compact")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"materialize", "--profile", "json.strict"}, request)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stdout), "{\"a\":1}") {
		t.Fatalf("human mode must carry the raw target bytes: %s", stdout)
	}
}

func TestMaterializeOutputFlagIsUsageExitOne(t *testing.T) {
	// G089 (adversarial audit, 2026-08-14): --output is plan/apply-only and
	// is now rejected at parse time (previously it parsed and was refused at
	// runtime); the assertion runs through runCLIUnit.
	code, stdout, stderr := runCLIUnit("materialize", "--profile", "json.strict",
		"--output", "out.json")
	if code != 1 || len(stdout) != 0 {
		t.Fatalf("exit = %d, stdout = %s", code, stdout)
	}
	if !strings.Contains(stderrText(stderr), "--output") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestMaterializeUnrepresentableTargetIsADataError(t *testing.T) {
	// {"a":{"b":1}} materialized to ini.portable: the nested object cannot
	// be expressed by the INI value vocabulary.
	request := materializeRequestBytes(t, "7b2261223a7b2262223a317d7d", "json.strict",
		"ini.portable", "ini.portable-canonical")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"materialize", "--profile", "json.strict", "--json"}, request)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "core.materialization.unrepresentable@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestConvertJSONToTOMLRoundTripsAndIsByteDeterministic(t *testing.T) {
	dir := newTestDir(t, "convert-json-toml")
	source := writeTestFile(t, dir, "src.json", []byte("{\"a\":1,\"b\":\"x\"}"))
	request := convertRequestBytes(t, "7b2261223a312c2262223a2278227d", "json.strict",
		"json.projection.best-exact-core", "toml.1.0", "toml.canonical-document")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"convert", source, "--profile", "json.strict", "--json"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "convert" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	again, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(stdout[:len(stdout)-1]) {
		t.Fatal("stdout envelope must be byte-deterministic")
	}
	// The cli.convert@1 payload: schema/report/target.
	payload := envelope.Payload().(*core.Object)
	fields := payload.Entries()
	if string(fields[0].Value.(core.String)) != "cli.convert@1" {
		t.Fatalf("payload = %v", fields[0])
	}
	report := fields[1].Value.(*core.Object)
	reportFields := report.Entries()
	if string(reportFields[0].Value.(core.String)) != "core.conversion-report@1" {
		t.Fatalf("report = %v", reportFields[0])
	}
	// The target decodes as core.source-snapshot@2 and carries the
	// materialized document.
	target := fields[2].Value.(*core.Object)
	targetFields := target.Entries()
	if string(targetFields[0].Value.(core.String)) != "core.source-snapshot@2" {
		t.Fatalf("target = %v", targetFields[0])
	}
}

func TestConvertAtomicFailureIsADataErrorWithoutTargetBytes(t *testing.T) {
	dir := newTestDir(t, "convert-gate")
	source := writeTestFile(t, dir, "src.xml", []byte("<root>x</root>"))
	request := convertRequestBytes(t, "3c726f6f743e783c2f726f6f743e", "xml.1.0-safe",
		"xml.projection.element-tree", "json.strict", "json.canonical-compact")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"convert", source, "--profile", "xml.1.0-safe", "--json"}, request)
	if code != 2 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	// The failure form carries no report and no target (atomic failure).
	payload := envelope.Payload().(*core.Object)
	fields := payload.Entries()
	if _, isNull := fields[1].Value.(core.Null); !isNull {
		t.Fatal("failure form must carry a null report")
	}
	if _, isNull := fields[2].Value.(core.Null); !isNull {
		t.Fatal("failure form must carry a null target")
	}
}

func TestConvertOutputFlagIsUsageExitOne(t *testing.T) {
	dir := newTestDir(t, "convert-output")
	source := writeTestFile(t, dir, "src.json", []byte("{\"a\":1}"))
	// G089 (adversarial audit, 2026-08-14): --output is plan/apply-only and
	// is now rejected at parse time (previously it parsed and was refused at
	// runtime); the assertion runs through runCLIUnit.
	code, stdout, stderr := runCLIUnit("convert", source, "--profile", "json.strict",
		"--output", "out.json")
	if code != 1 || len(stdout) != 0 {
		t.Fatalf("exit = %d, stdout = %s", code, stdout)
	}
	if !strings.Contains(stderrText(stderr), "--output") {
		t.Fatalf("stderr = %s", stderr)
	}
}
