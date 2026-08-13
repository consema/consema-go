package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

func TestCapabilitiesHumanReportListsTheFacadeInventory(t *testing.T) {
	code, stdout, stderr := runCLIUnit("capabilities")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	for _, expected := range []string{
		"families (8):", "profiles (16):", "ini.portable@1 (family ini)",
		"plist.binary@1 (family plist)", "query domains (21):", "error codes (187):",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report misses %q:\n%s", expected, text)
		}
	}
}

func TestCapabilitiesJSONPayloadMatchesTheRFCRecord(t *testing.T) {
	code, stdout, stderr := runCLIUnit("capabilities", "--json")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "capabilities" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	entries := envelope.Payload().(*core.Object).Entries()
	if entries[0].Key != "schema" ||
		string(entries[0].Value.(core.String)) != "cli.capabilities@1" {
		t.Fatalf("payload = %v", entries[0])
	}
	field := func(key string) *core.Array {
		for _, entry := range entries {
			if entry.Key == key {
				return entry.Value.(*core.Array)
			}
		}
		t.Fatalf("missing field %s", key)
		return nil
	}
	if len(field("families").Items()) != 8 {
		t.Fatal("families != 8")
	}
	if len(field("profiles").Items()) != 16 {
		t.Fatal("profiles != 16")
	}
	if len(field("query_domains").Items()) != 21 {
		t.Fatal("query domains != 21")
	}
	if len(field("operations").Items()) != 16 {
		t.Fatal("operation registries != 16")
	}
	if len(field("error_codes").Items()) != 187 {
		t.Fatal("error codes != 187")
	}
}

// queryRequestBytes builds one strict cli.request@1 wrapper with a bare
// Input query definition (the shared-vector surface of the portable domain).
func queryRequestBytes(t *testing.T, sourceHex, profileID string) []byte {
	t.Helper()
	input, err := core.NewObject(core.Entry{Key: "kind", Value: core.String("Input")})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.query-definition@1")},
		core.Entry{Key: "domain_id", Value: core.String(portableQueryDomain)},
		core.Entry{Key: "domain_version", Value: coreInteger(1)},
		core.Entry{Key: "selection", Value: core.String("All")},
		core.Entry{Key: "expression", Value: input},
	)
	if err != nil {
		t.Fatal(err)
	}
	source, err := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("bytes")},
		core.Entry{Key: "bytes", Value: core.String(sourceHex)},
	)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := core.NewObject(
		core.Entry{Key: "id", Value: core.String(profileID)},
		core.Entry{Key: "version", Value: coreInteger(1)},
	)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(requestSchema)},
		core.Entry{Key: "source", Value: source},
		core.Entry{Key: "profile", Value: profile},
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

func TestQueryJSONSuccessRoundTrips(t *testing.T) {
	request := queryRequestBytes(t, "5b312c322c335d", "json.strict")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "query" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	// Byte-determinism: re-encoding reproduces the stdout bytes.
	again, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(stdout[:len(stdout)-1]) {
		t.Fatal("stdout envelope must be byte-deterministic")
	}
	// The payload decodes through the typed decoder.
	result := &protocol.QueryResultMessage{}
	result, err = result.FromValue(envelope.Payload())
	if err != nil {
		t.Fatalf("query-result record: %v", err)
	}
	if result.Completion().Status() != protocol.CompletionSuccess {
		t.Fatalf("completion = %v", result.Completion().Status())
	}
	if len(result.Matches()) != 1 {
		t.Fatalf("matches = %d", len(result.Matches()))
	}
}

func TestQueryJSONRedactsMatchingKeyValuesAndShowSecretsRecovers(t *testing.T) {
	// RFC 0015 §11.1/§11.3 (P1 audit): machine output redacts values under
	// matching key names — the payload carries `$REDACTED$` and the
	// envelope's redaction facts are real (`redacted == (count > 0)`);
	// `--show-secrets` is the sole opt-out and restores the plaintext with
	// zero facts (RFC 0015 §11.4).
	//
	// The Go portable domain implements only the bare Input operator
	// (query_exec.go:188-192), so the match carries the whole document
	// object; the envelope redaction walks the payload tree and replaces the
	// password entry inside the match value (the Rust bin runs the same
	// assertion with ObjectEntry matches in query_cmd.rs).
	request := queryRequestBytes(t,
		"7b2270617373776f7264223a2268756e74657232222c22686f7374223a2264622e696e7465726e616c227d",
		"json.strict")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	if !envelope.Redaction().Redacted() || envelope.Redaction().Count() != 1 {
		t.Fatalf("redaction facts = redacted %v count %d",
			envelope.Redaction().Redacted(), envelope.Redaction().Count())
	}
	result := &protocol.QueryResultMessage{}
	result, err := result.FromValue(envelope.Payload())
	if err != nil {
		t.Fatalf("query-result record: %v", err)
	}
	if len(result.Matches()) != 1 {
		t.Fatalf("matches = %d", len(result.Matches()))
	}
	object, ok := result.Matches()[0].Value.(*core.Object)
	if !ok {
		t.Fatalf("match value = %T", result.Matches()[0].Value)
	}
	entries := object.Entries()
	if entries[0].Key != "password" || entries[0].Value != core.String(placeholder) {
		t.Fatalf("password entry = %v, want %s", entries[0], placeholder)
	}
	if entries[1].Key != "host" || entries[1].Value != core.String("db.internal") {
		t.Fatalf("host entry = %v", entries[1])
	}
	// --show-secrets restores the plaintext and zeroes the facts.
	code, stdout, _ = runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json", "--show-secrets"}, request)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	envelope = envelopeOf(t, stdout)
	if envelope.Redaction().Redacted() || envelope.Redaction().Count() != 0 {
		t.Fatalf("--show-secrets facts = redacted %v count %d",
			envelope.Redaction().Redacted(), envelope.Redaction().Count())
	}
	result, err = result.FromValue(envelope.Payload())
	if err != nil {
		t.Fatalf("query-result record: %v", err)
	}
	object = result.Matches()[0].Value.(*core.Object)
	if object.Entries()[0].Value != core.String("hunter2") {
		t.Fatalf("password value = %v, want hunter2", object.Entries()[0].Value)
	}
}

func TestQueryHumanReportRedactsMatchingKeyValues(t *testing.T) {
	// RFC 0015 §11.1: the human report redacts the value of every match
	// whose key name matches, via redactText; --show-secrets reveals.
	// The Go portable domain cannot produce ObjectEntry matches end-to-end
	// (query_exec.go:188-192), so the report is exercised at the unit level
	// with one synthetic record; the Rust bin runs the same report
	// end-to-end in query_cmd.rs.
	root := protocol.RootValuePath()
	passwordPath := root.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "password"})
	hostPath := root.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "host"})
	matches := []protocol.ProtocolQueryMatch{
		{Kind: "ObjectEntry", Key: core.String("password"), ValuePath: passwordPath,
			Value: core.String("hunter2")},
		{Kind: "ObjectEntry", Key: core.String("host"), ValuePath: hostPath,
			Value: core.String("db.internal")},
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 2, 2, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := protocol.NewQueryResultMessage(protocol.DomainPortableValueV1(),
		protocol.RoleObjectEntry, matches, completion, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	policy := conservativePolicy()
	if err := writeQueryReport(result, &policy, &buf); err != nil {
		t.Fatal(err)
	}
	text := buf.String()
	if !strings.Contains(text, placeholder) {
		t.Fatalf("report misses the placeholder:\n%s", text)
	}
	if strings.Contains(text, "hunter2") {
		t.Fatalf("the secret value is visible:\n%s", text)
	}
	if !strings.Contains(text, "db.internal") {
		t.Fatalf("non-matching values must stay:\n%s", text)
	}
	// --show-secrets is the sole opt-out.
	buf.Reset()
	shown := showSecretsPolicy()
	if err := writeQueryReport(result, &shown, &buf); err != nil {
		t.Fatal(err)
	}
	text = buf.String()
	if !strings.Contains(text, "hunter2") {
		t.Fatalf("--show-secrets must reveal:\n%s", text)
	}
	if strings.Contains(text, placeholder) {
		t.Fatalf("--show-secrets must not redact:\n%s", text)
	}
}

func TestQueryUnknownProfileIsUsageExitOneWithoutEnvelope(t *testing.T) {
	request := queryRequestBytes(t, "5b312c322c335d", "json.strict")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.bogus", "--json"}, request)
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("usage failures never emit an envelope")
	}
	if !strings.Contains(stderrText(stderr), "unknown profile 'json.bogus'") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestQueryRequestTransportAndWrapperNegativesRejected(t *testing.T) {
	// PVCE transport is accepted.
	value, err := protocol.DecodeJSON(queryRequestBytes(t, "5b312c322c335d", "json.strict"),
		protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	pvce, err := protocol.EncodePVCE(value, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json"}, pvce)
	if code != 0 {
		t.Fatalf("PVCE request must decode, exit = %d", code)
	}
	// Malformed bytes -> data error.
	code, _, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json"}, []byte("not-a-request"))
	if code != 2 || !strings.Contains(stderrText(stderr), "cli.data.invalid-request@1") {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
}

func TestQueryRecoveredSourceIsADataError(t *testing.T) {
	// "[section\nvalue=1\n" — an unterminated section header recovers under
	// ini.portable.
	request := queryRequestBytes(t, "5b73656374696f6e0a76616c75653d310a", "ini.portable")
	code, _, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "ini.portable", "--json"}, request)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "Recovered") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestQueryNativeDomainIsRejectedClearly(t *testing.T) {
	// Rewrite the domain id to a native domain.
	request := queryRequestBytes(t, "7b7d", "json.strict")
	value, err := protocol.DecodeJSON(request, protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	object := value.(*core.Object)
	entries := object.Entries()
	var payload core.Value
	for _, entry := range entries {
		if entry.Key == "payload" {
			payloadObject := entry.Value.(*core.Object)
			fields := payloadObject.Entries()
			rewritten := make([]core.Entry, 0, len(fields))
			for _, field := range fields {
				if field.Key == "domain_id" {
					rewritten = append(rewritten, core.Entry{Key: field.Key,
						Value: core.String("json.native-semantic-query")})
				} else {
					rewritten = append(rewritten, field)
				}
			}
			payload, _ = core.NewObject(rewritten...)
		}
	}
	_ = object
	_ = payload
	// Rebuild the whole wrapper with the native domain payload.
	input, _ := core.NewObject(core.Entry{Key: "kind", Value: core.String("Input")})
	nativePayload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.query-definition@1")},
		core.Entry{Key: "domain_id", Value: core.String("json.native-semantic-query")},
		core.Entry{Key: "domain_version", Value: coreInteger(1)},
		core.Entry{Key: "selection", Value: core.String("All")},
		core.Entry{Key: "expression", Value: input},
	)
	source, _ := core.NewObject(
		core.Entry{Key: "kind", Value: core.String("bytes")},
		core.Entry{Key: "bytes", Value: core.String("7b7d")},
	)
	profile, _ := core.NewObject(
		core.Entry{Key: "id", Value: core.String("json.strict")},
		core.Entry{Key: "version", Value: coreInteger(1)},
	)
	wrapper, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String(requestSchema)},
		core.Entry{Key: "source", Value: source},
		core.Entry{Key: "profile", Value: profile},
		core.Entry{Key: "payload", Value: nativePayload},
	)
	nativeRequest, _ := protocol.EncodeJSON(wrapper, protocolLimits())
	code, _, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict", "--json"}, nativeRequest)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	// G121 (adversarial audit 2026-08-13): the milestone phrasing was
	// removed from the message; the rejection and its code are unchanged.
	if !strings.Contains(stderrText(stderr), "is not wired") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestQueryHumanReportRendersTheSameResults(t *testing.T) {
	request := queryRequestBytes(t, "5b312c322c335d", "json.strict")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"query", "--profile", "json.strict"}, request)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "match 0: $ = [1, 2, 3]") {
		t.Fatalf("report = %s", text)
	}
}

func TestExplainContractByInferredKind(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "core.cli-output@1")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "consema explain contract core.cli-output@1") ||
		!strings.Contains(text, "kind: contract") ||
		!strings.Contains(text, "stability: Stable") {
		t.Fatalf("report = %s", text)
	}
}

func TestExplainErrorCodeWithExplicitKind(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "error-code", "cli.data.io@1")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "kind: error-code") ||
		!strings.Contains(text, "category: Encoding") ||
		!strings.Contains(text, "code: cli.data.io@1") {
		t.Fatalf("report = %s", text)
	}
}

func TestExplainProfileReportsTheProfileDescriptor(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "profile", "ini.portable@1")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "kind: profile") ||
		!strings.Contains(text, "profile_id: ini.portable") ||
		!strings.Contains(text, "format_family_id: ini") {
		t.Fatalf("report = %s", text)
	}
}

func TestExplainUnknownIDIsADataError(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "example.unknown@1", "--json")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "cli.data.invalid-request@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if !strings.Contains(stderrText(stderr), "(code cli.data.invalid-request@1)") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExplainCapabilityKindIsReservedWithADataError(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "capability", "core.query.ordered-results@1")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("human-mode failures write zero stdout bytes (RFC 0015 §3.3)")
	}
	if !strings.Contains(stderrText(stderr), "(code cli.data.invalid-request@1)") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExplainKindWithoutIDIsUsage(t *testing.T) {
	code, stdout, stderr := runCLIUnit("explain", "contract")
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("usage failures never produce an envelope")
	}
	if !strings.Contains(stderrText(stderr), "missing required argument") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExplainIDWithoutVersionIsADataError(t *testing.T) {
	code, stdout, _ := runCLIUnit("explain", "core.cli-output")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("human-mode failures write zero stdout bytes")
	}
}

func TestConformanceHumanReportPasses(t *testing.T) {
	code, stdout, stderr := runCLIUnit("conformance")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	text := string(stdout)
	for _, expected := range []string{
		"consema conformance (consema.cli.conformance@1)",
		"[PASS] cli.envelope@1",
		"[PASS] cli.exit-code@1",
		"[PASS] cli.redaction@1",
		"3 passed, 0 failed",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report misses %q:\n%s", expected, text)
		}
	}
}

func TestConformanceJSONEmitsAByteValidEnvelopeLoop(t *testing.T) {
	code, stdout, stderr := runCLIUnit("conformance", "--json")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "conformance" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	// The byte loop closes: re-encoding reproduces the exact stdout bytes.
	again, err := envelope.ToJSON(protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(stdout[:len(stdout)-1]) {
		t.Fatal("stdout envelope bytes must be byte-deterministic")
	}
}

func TestConformanceFailureIsReportedAsInternalExitFive(t *testing.T) {
	broken := func() (string, error) {
		return "cli.envelope@1", &emitError{"injected failure"}
	}
	parsed, err := parseArgs([]string{"conformance", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := runConformanceWithChecks(parsed, []selfCheck{broken}, &stdout, &stderr)
	if code != 5 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr.String(), "self-check failed: cli.envelope@1") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	envelope := envelopeOf(t, []byte(stdout.String()))
	if envelope.ExitClass() != protocol.ExitInternal {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "cli.internal.unclassified@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
}

func TestHelpAndVersionExitZeroOnStdout(t *testing.T) {
	code, stdout, stderr := runCLIUnit("--help")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.HasPrefix(string(stdout), "consema "+productVersion+" ") ||
		!strings.Contains(string(stdout), "Commands (RFC 0015 §6.1)") {
		t.Fatalf("help = %s", stdout)
	}
	code, stdout, stderr = runCLIUnit("--version")
	if code != 0 || len(stderr) != 0 || string(stdout) != productVersion+"\n" {
		t.Fatalf("version = %q, exit = %d", stdout, code)
	}
}

func TestUnknownCommandAndArgumentsExitUsageWithNoStdout(t *testing.T) {
	for _, args := range [][]string{{"frobnicate"}, {"--bogus"}, {"ins"}, {}} {
		code, stdout, stderr := runCLIUnit(args...)
		if code != 1 {
			t.Fatalf("%v: exit = %d", args, code)
		}
		if len(stdout) != 0 {
			t.Fatalf("%v must not emit stdout bytes: %s", args, stdout)
		}
		if !strings.Contains(stderrText(stderr), "consema: error:") {
			t.Fatalf("%v: stderr = %s", args, stderr)
		}
	}
}

func TestApplyMissingPlanIsADataError(t *testing.T) {
	dir := newTestDir(t, "apply-missing")
	missing := filepath.Join(dir, "missing-plan.json")
	code, stdout, stderr := runCLIUnit("apply", missing, "--json")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) == 0 {
		t.Fatal("data-class failures carry the envelope")
	}
	if !strings.Contains(stderrText(stderr), "cli.data.io@1") ||
		!strings.Contains(stderrText(stderr), missing) {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestMalformedArgumentsMapToUsageExitOne(t *testing.T) {
	for _, args := range [][]string{
		{"query", "--request-file", "r.json"},
		{"convert", "x.json"},
		{"conformance", "--pretty"},
		{"conformance", "--output"},
		{"inspect"},
		{"inspect", "a", "b"},
	} {
		code, _, stderr := runCLIUnit(args...)
		if code != 1 {
			t.Fatalf("%v: exit = %d", args, code)
		}
		if !strings.Contains(stderrText(stderr), "(code cli.usage.") {
			t.Fatalf("%v: stderr = %s", args, stderr)
		}
	}
}

var _ = os.Getpid
