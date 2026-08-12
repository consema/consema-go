package main

// `consema conformance`: the embedded protocol self-check subset of RFC
// 0015 §16.4.
//
// The release artifact executes a small self-check subset — envelope
// round-trip, exit classification, and the redaction contract — with **no
// repository fixtures**; the full language-neutral `consema.cli.conformance@1`
// suite stays repository-level. The subset mirrors the library-side vector
// semantics case for case: the checks here are the embedded counterparts of
// the `cli.envelope@1`, `cli.exit-code@1`, and `cli.redaction@1` vector
// capabilities.
//
// All checks pass -> exit 0. Any check fails -> the envelope carries
// `internal` (RFC 0015 §5.1) with `cli.internal.unclassified@1` and the
// process exits 5.

import (
	"fmt"
	"io"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// conformanceSuite is the frozen conformance suite id of RFC 0015
// §6.2/§16.1.
const conformanceSuite = "consema.cli.conformance@1"

// selfCheck is one deterministic embedded self-check: the passing check id,
// or the failing check id and a human message.
type selfCheck func() (string, error)

// selfChecks is the embedded self-check subset, in deterministic order
// (RFC 0015 §16.4).
var selfChecks = []selfCheck{
	checkEnvelopeRoundTrip,
	checkExitClassification,
	checkRedactionContract,
}

// runConformance runs the embedded self-check subset and reports.
func runConformance(parsed *ParsedArgs, stdout, stderr io.Writer) uint8 {
	return runConformanceWithChecks(parsed, selfChecks, stdout, stderr)
}

// runConformanceWithChecks runs one deterministic self-check list against
// the writers and returns the frozen process exit code (shared by the unit
// tests for failure injection).
func runConformanceWithChecks(parsed *ParsedArgs, checks []selfCheck,
	stdout, stderr io.Writer) uint8 {
	var passedIDs []string
	var failedItems []conformanceFailure
	for _, check := range checks {
		if id, err := check(); err == nil {
			passedIDs = append(passedIDs, id)
		} else {
			failedItems = append(failedItems, conformanceFailure{ID: id, Message: err.Error()})
		}
	}
	exitClass := protocol.ExitSuccess
	var diagnostics []*protocol.Diagnostic
	if len(failedItems) > 0 {
		exitClass = protocol.ClassifyErrorCode("cli.internal.unclassified@1")
		diagnostics = []*protocol.Diagnostic{internalConformanceDiagnostic()}
	}
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandConformance,
		exitClass, productVersion, conformancePayload(passedIDs, failedItems),
		diagnostics, noRedaction())
	if err != nil {
		return internalFailure("conformance",
			"conformance envelope construction failed: "+err.Error(), stderr)
	}
	var writeErr error
	if parsed.json {
		writeErr = emitEnvelope(envelope, parsed.pretty, stdout)
	} else {
		writeErr = writeConformanceReport(passedIDs, failedItems, stdout)
	}
	if writeErr != nil {
		return internalFailure("conformance", writeErr.Error(), stderr)
	}
	for _, failure := range failedItems {
		fmt.Fprintf(stderr,
			"consema: error: conformance self-check failed: %s: %s "+
				"(code cli.internal.unclassified@1)\n", failure.ID, failure.Message)
	}
	return exitClass.ExitCode()
}

// conformanceFailure is one failing self-check item.
type conformanceFailure struct {
	// ID is the failing check id.
	ID string
	// Message is the human failure message.
	Message string
}

// conformancePayload builds the frozen `cli.conformance@1` payload record
// of RFC 0015 §6.2.
func conformancePayload(passed []string, failed []conformanceFailure) core.Value {
	passedValues := make([]core.Value, 0, len(passed))
	for _, id := range passed {
		passedValues = append(passedValues, core.String(id))
	}
	failedValues := make([]core.Value, 0, len(failed))
	for _, failure := range failed {
		entry, _ := core.NewObject(
			core.Entry{Key: "id", Value: core.String(failure.ID)},
			core.Entry{Key: "message", Value: core.String(failure.Message)},
		)
		failedValues = append(failedValues, entry)
	}
	payload, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("cli.conformance@1")},
		core.Entry{Key: "suite", Value: core.String(conformanceSuite)},
		core.Entry{Key: "passed", Value: core.NewArray(passedValues...)},
		core.Entry{Key: "failed", Value: core.NewArray(failedValues...)},
	)
	return payload
}

// internalConformanceDiagnostic builds the `cli.internal.unclassified@1`
// diagnostic carried by a failed conformance envelope.
func internalConformanceDiagnostic() *protocol.Diagnostic {
	diagnostic, _ := protocol.NewDiagnostic("cli.internal.unclassified@1",
		protocol.CategorySemantic, protocol.SeverityError, nil, nil,
		map[string]string{"command": "conformance"}, nil, nil, 0,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	return diagnostic
}

// writeConformanceReport writes the deterministic human conformance report
// (RFC 0015 §16.4 "plus a human report").
func writeConformanceReport(passed []string, failed []conformanceFailure,
	stdout io.Writer) error {
	var text string
	text += fmt.Sprintf("consema conformance (%s)\n", conformanceSuite)
	for _, id := range passed {
		text += fmt.Sprintf("  [PASS] %s\n", id)
	}
	for _, failure := range failed {
		text += fmt.Sprintf("  [FAIL] %s\n", failure.ID)
	}
	text += fmt.Sprintf("  %d passed, %d failed\n", len(passed), len(failed))
	_, err := io.WriteString(stdout, text)
	return err
}

// checkEnvelopeRoundTrip implements self-check `cli.envelope@1`: the fixed
// `core.cli-output@1` envelope round-trips on both transports and is
// byte-deterministic (RFC 0015 §3.3).
func checkEnvelopeRoundTrip() (string, error) {
	const check = "cli.envelope@1"
	limits := protocol.DefaultProtocolLimits()
	envelope, err := protocol.NewCliOutputMessage(protocol.CommandConformance,
		protocol.ExitSuccess, productVersion, conformancePayload(nil, nil),
		nil, noRedaction())
	if err != nil {
		return check, err
	}
	jsonBytes, err := envelope.ToJSON(limits)
	if err != nil {
		return check, err
	}
	decoded := &protocol.CliOutputMessage{}
	decoded, err = decoded.FromJSON(jsonBytes, limits)
	if err != nil {
		return check, err
	}
	jsonAgain, err := decoded.ToJSON(limits)
	if err != nil {
		return check, err
	}
	if string(jsonAgain) != string(jsonBytes) {
		return check, fmt.Errorf("envelope JSON is not byte-deterministic")
	}
	pvceBytes, err := envelope.ToPVCE(limits)
	if err != nil {
		return check, err
	}
	pvceDecoded := &protocol.CliOutputMessage{}
	pvceDecoded, err = pvceDecoded.FromPVCE(pvceBytes, limits)
	if err != nil {
		return check, err
	}
	pvceAgain, err := pvceDecoded.ToPVCE(limits)
	if err != nil {
		return check, err
	}
	if string(pvceAgain) != string(pvceBytes) {
		return check, fmt.Errorf("envelope PVCE is not byte-deterministic")
	}
	return check, nil
}

// checkExitClassification implements self-check `cli.exit-code@1`: the
// closed class-to-code table and one representative per error family
// classify per RFC 0015 §5.1/§5.2.
func checkExitClassification() (string, error) {
	const check = "cli.exit-code@1"
	table := []struct {
		class    protocol.ExitClass
		expected uint8
	}{
		{protocol.ExitSuccess, 0},
		{protocol.ExitUsage, 1},
		{protocol.ExitData, 2},
		{protocol.ExitLimit, 3},
		{protocol.ExitPrecondition, 4},
		{protocol.ExitInternal, 5},
	}
	for _, row := range table {
		actual := protocol.Classify(row.class)
		if actual != row.expected {
			return check, fmt.Errorf("%s maps to %d instead of %d",
				row.class.Name(), actual, row.expected)
		}
	}
	families := []struct {
		code     string
		expected protocol.ExitClass
	}{
		{"cli.usage.unknown-command@1", protocol.ExitUsage},
		{"cli.data.io@1", protocol.ExitData},
		{"cli.limit.file-size@1", protocol.ExitLimit},
		{"cli.write.io@1", protocol.ExitPrecondition},
		{"cli.internal.unclassified@1", protocol.ExitInternal},
	}
	for _, row := range families {
		actual := protocol.ClassifyErrorCode(row.code)
		if actual != row.expected {
			return check, fmt.Errorf("%s classifies as %s instead of %s",
				row.code, actual.Name(), row.expected.Name())
		}
	}
	return check, nil
}

// checkRedactionContract implements self-check `cli.redaction@1`: the
// presentation-only redaction contract of RFC 0015 §11 — the frozen
// key-name pattern set, the `$REDACTED$` placeholder and facts, the
// `redacted == (count > 0)` record invariant, `--show-secrets` as the sole
// opt-out, and the hard-gate-3 boundary that byte payloads under
// non-matching keys survive untouched.
func checkRedactionContract() (string, error) {
	const check = "cli.redaction@1"
	payload, _ := core.NewObject(
		core.Entry{Key: "host", Value: core.String("db.internal")},
		core.Entry{Key: "password", Value: core.String("hunter2")},
		core.Entry{Key: "api_key", Value: core.String("k-1234")},
		core.Entry{Key: "original", Value: core.NewBytes([]byte{0x6f, 0x6c, 0x64})},
	)
	policy := conservativePolicy()
	redacted, facts := redactValue(&policy, payload)
	if facts.count != 2 || len(facts.keys) != 2 ||
		facts.keys[0] != "password" || facts.keys[1] != "api_key" {
		return check, fmt.Errorf("redaction facts mismatch: %v", facts.keys)
	}
	if _, err := protocol.NewRedaction(facts.count > 0, facts.count); err != nil {
		return check, fmt.Errorf("redaction record invariant broken")
	}
	entries := redacted.(*core.Object).Entries()
	if entries[1].Value != core.String(placeholder) ||
		entries[2].Value != core.String(placeholder) ||
		entries[0].Value != core.String("db.internal") {
		return check, fmt.Errorf("placeholder replacement mismatch")
	}
	// Hard gate 3 / RFC 0015 §11.4: byte payloads under non-matching keys
	// are preserved exactly (redaction never touches precondition facts).
	bytes, ok := entries[3].Value.(core.Bytes)
	if !ok || string(bytes) != string([]byte{0x6f, 0x6c, 0x64}) {
		return check, fmt.Errorf("byte payload changed by redaction")
	}
	// --show-secrets is the sole opt-out: the tree is returned untouched.
	shownPolicy := showSecretsPolicy()
	shown, shownFacts := redactValue(&shownPolicy, payload)
	if !core.Equal(shown, payload) || shownFacts.count != 0 {
		return check, fmt.Errorf("--show-secrets must return the value untouched")
	}
	return check, nil
}
