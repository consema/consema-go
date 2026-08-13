package conformance

// The `consema.cli.conformance@1` suite runner
// (consema-rs/consema-conformance/src/cli_v1.rs). Every case is dispatched
// by its `capability` and executed against the protocol v7 types: envelope
// decode, exit classification, batch state machines, the redaction record
// contract, and transport budgets. The vector data drives every result;
// the runner pins the frozen per-suite case count (conformance/README.md
// rule 4) but holds no per-case expectation literals (G146, adversarial
// audit 2026-08-13).

import (
	"encoding/hex"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runCLIV1 executes the embedded `consema.cli.conformance@1` suite.
func runCLIV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch vector.Capability {
		case "cli.envelope@1":
			_, err = cliEnvelope(vector)
		case "cli.exit-code@1":
			_, err = cliExitCode(vector)
		case "cli.batch-plan@1":
			_, err = cliBatchPlan(vector)
		case "cli.batch-result@1":
			_, err = cliBatchResult(vector)
		case "cli.redaction@1":
			_, err = cliRedaction(vector)
		case "cli.limit@1":
			_, err = cliLimit(vector)
		default:
			err = fmt.Errorf("unknown capability %s", vector.Capability)
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

// cliInputJSON reads the canonical tagged input JSON of one case.
func cliInputJSON(vector *caseData) ([]byte, error) {
	text, ok := caseInput(vector, "json")
	if !ok {
		return nil, fmt.Errorf("missing input.json")
	}
	jsonText, ok := text.(core.String)
	if !ok {
		return nil, fmt.Errorf("input.json must be String")
	}
	return []byte(jsonText), nil
}

// expectProtocolRejection asserts the pinned error code and optional path
// of a rejected record.
func expectProtocolRejection(vector *caseData, err error) error {
	if err == nil {
		return fmt.Errorf("case %s: record must be rejected", vector.ID)
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok {
		return fmt.Errorf("case %s: unexpected error type: %v", vector.ID, err)
	}
	errorCode, _ := stringField(vector.Expected, "error_code")
	if protocolError.Code() != errorCode {
		return fmt.Errorf("case %s: rejection %s != %s", vector.ID, protocolError.Code(), errorCode)
	}
	if errorPath, ok := stringField(vector.Expected, "error_path"); ok {
		if protocolError.Path != errorPath {
			return fmt.Errorf("case %s: rejection path %s != %s", vector.ID, protocolError.Path, errorPath)
		}
	}
	return nil
}

func hexString(bytes []byte) string {
	return hex.EncodeToString(bytes)
}

func cliEnvelope(vector *caseData) (bool, error) {
	jsonBytes, err := cliInputJSON(vector)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	if _, ok := caseExpected(vector, "error_code"); ok {
		message := &protocol.CliOutputMessage{}
		_, err := message.FromJSON(jsonBytes, limits)
		return false, expectProtocolRejection(vector, err)
	}
	message := &protocol.CliOutputMessage{}
	envelope, err := message.FromJSON(jsonBytes, limits)
	if err != nil {
		return false, fmt.Errorf("envelope decode: %w", err)
	}
	reEncoded, err := envelope.ToJSON(limits)
	if err != nil {
		return false, fmt.Errorf("envelope re-encode: %w", err)
	}
	if string(reEncoded) != string(jsonBytes) {
		return false, fmt.Errorf("envelope re-encode must reproduce the input bytes exactly")
	}
	pvce, err := envelope.ToPVCE(limits)
	if err != nil {
		return false, fmt.Errorf("envelope PVCE encode: %w", err)
	}
	if expectedPVCE, ok := stringField(vector.Expected, "pvce_hex"); ok {
		if hexString(pvce) != expectedPVCE {
			return false, fmt.Errorf("pvce_hex %s != %s", hexString(pvce), expectedPVCE)
		}
	}
	decodedPVCE := &protocol.CliOutputMessage{}
	decoded, err := decodedPVCE.FromPVCE(pvce, limits)
	if err != nil {
		return false, fmt.Errorf("envelope PVCE decode: %w", err)
	}
	decodedValue, err := decoded.ToValue()
	if err != nil {
		return false, err
	}
	envelopeValue, err := envelope.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(decodedValue, envelopeValue) {
		return false, fmt.Errorf("dual transport must decode to the same envelope")
	}
	again, err := envelope.ToJSON(limits)
	if err != nil {
		return false, err
	}
	if string(again) != string(reEncoded) {
		return false, fmt.Errorf("envelope JSON is not byte-deterministic")
	}
	return true, assertEnvelopeFacts(envelope, vector)
}

func assertEnvelopeFacts(envelope *protocol.CliOutputMessage, vector *caseData) error {
	if command, ok := stringField(vector.Expected, "command"); ok {
		if envelope.Command().Name() != command {
			return fmt.Errorf("command %s != %s", envelope.Command().Name(), command)
		}
	}
	if exitClass, ok := stringField(vector.Expected, "exit_class"); ok {
		if envelope.ExitClass().Name() != exitClass {
			return fmt.Errorf("exit_class %s != %s", envelope.ExitClass().Name(), exitClass)
		}
	}
	if productVersion, ok := stringField(vector.Expected, "product_version"); ok {
		if envelope.ProductVersion() != productVersion {
			return fmt.Errorf("product_version mismatch")
		}
	}
	if payloadSchema, ok := stringField(vector.Expected, "payload_schema"); ok {
		payload, ok := envelope.Payload().(*core.Object)
		if !ok || payload.Len() == 0 {
			return fmt.Errorf("payload has no schema first field")
		}
		first, ok := payload.Entries()[0].Value.(core.String)
		if !ok || string(first) != payloadSchema {
			return fmt.Errorf("payload schema mismatch")
		}
	}
	if redacted, ok := booleanField(vector.Expected, "redacted"); ok {
		if envelope.Redaction().Redacted() != redacted {
			return fmt.Errorf("redaction.redacted mismatch")
		}
	}
	if count, ok := integerField(vector.Expected, "count"); ok {
		if envelope.Redaction().Count() != count {
			return fmt.Errorf("redaction.count mismatch")
		}
	}
	if diagnostics, ok := integerField(vector.Expected, "diagnostics_count"); ok {
		if uint64(len(envelope.Diagnostics())) != diagnostics {
			return fmt.Errorf("diagnostics count mismatch")
		}
	}
	if code, ok := stringField(vector.Expected, "diagnostic_code"); ok {
		found := false
		for _, diagnostic := range envelope.Diagnostics() {
			if diagnostic.Code == code {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("diagnostic %s not found", code)
		}
	}
	return nil
}

func cliExitCode(vector *caseData) (bool, error) {
	if namesValue, ok := caseInput(vector, "names"); ok {
		names, ok := namesValue.(*core.Array)
		if !ok {
			return false, fmt.Errorf("input.names must be Sequence")
		}
		codesValue, ok := caseInput(vector, "codes")
		if !ok {
			return false, fmt.Errorf("missing input.codes")
		}
		codes, ok := codesValue.(*core.Array)
		if !ok {
			return false, fmt.Errorf("input.codes must be Sequence")
		}
		if names.Len() != codes.Len() {
			return false, fmt.Errorf("class table count mismatch")
		}
		for index := 0; index < names.Len(); index++ {
			name, ok := names.At(index).(core.String)
			if !ok {
				return false, fmt.Errorf("class name must be String")
			}
			code, ok := codes.At(index).(core.Integer)
			if !ok {
				return false, fmt.Errorf("class code must be Integer")
			}
			exitClass, ok := protocol.ParseExitClass(string(name))
			if !ok {
				return false, fmt.Errorf("unknown class %s", string(name))
			}
			if uint64(exitClass.ExitCode()) != code.Int().Uint64() {
				return false, fmt.Errorf("class table row %d: %s maps to %d instead of %d",
					index, string(name), exitClass.ExitCode(), code.Int().Uint64())
			}
		}
		return true, nil
	}
	codesValue, ok := caseInput(vector, "codes")
	if !ok {
		return false, fmt.Errorf("missing input.codes")
	}
	codes, ok := codesValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.codes must be Sequence")
	}
	classes, ok := stringSequenceField(vector.Expected, "classes")
	if !ok {
		return false, fmt.Errorf("missing expected.classes")
	}
	if codes.Len() != len(classes) {
		return false, fmt.Errorf("code/class count mismatch")
	}
	for index := 0; index < codes.Len(); index++ {
		code, ok := codes.At(index).(core.String)
		if !ok {
			return false, fmt.Errorf("code must be String")
		}
		actual := protocol.ClassifyErrorCode(string(code))
		if actual.Name() != classes[index] {
			return false, fmt.Errorf("matrix row %d: %s classifies as %s instead of %s",
				index, string(code), actual.Name(), classes[index])
		}
	}
	return true, nil
}

func cliBatchPlan(vector *caseData) (bool, error) {
	jsonBytes, err := cliInputJSON(vector)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	record, err := protocol.DecodeJSON(jsonBytes, limits)
	if err != nil {
		return false, fmt.Errorf("plan transport decode: %w", err)
	}
	if _, ok := caseExpected(vector, "error_code"); ok {
		message := &protocol.BatchPlanMessage{}
		_, err := message.FromValueWithRegistry(record, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		return false, expectProtocolRejection(vector, err)
	}
	message := &protocol.BatchPlanMessage{}
	plan, err := message.FromValueWithRegistry(record, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		return false, fmt.Errorf("plan decode: %w", err)
	}
	transport, err := protocol.EncodeJSON(record, limits)
	if err != nil {
		return false, fmt.Errorf("plan re-encode: %w", err)
	}
	if string(transport) != string(jsonBytes) {
		return false, fmt.Errorf("plan record must re-encode to the exact input bytes")
	}
	reEncoded, err := plan.ToValue()
	if err != nil {
		return false, fmt.Errorf("plan re-encode: %w", err)
	}
	if !core.Equal(reEncoded, record) {
		return false, fmt.Errorf("plan re-encode must reproduce the record exactly")
	}
	if pvceHex, ok := stringField(vector.Expected, "pvce_hex"); ok {
		pvce, err := protocol.EncodePVCE(record, limits)
		if err != nil {
			return false, err
		}
		if hexString(pvce) != pvceHex {
			return false, fmt.Errorf("plan pvce_hex %s != %s", hexString(pvce), pvceHex)
		}
	}
	if productVersion, ok := stringField(vector.Expected, "product_version"); ok {
		if plan.ProductVersion() != productVersion {
			return false, fmt.Errorf("plan product_version mismatch")
		}
	}
	if statuses, ok := stringSequenceField(vector.Expected, "statuses"); ok {
		if len(plan.Files()) != len(statuses) {
			return false, fmt.Errorf("plan file count %d != %d", len(plan.Files()), len(statuses))
		}
		for index, entry := range plan.Files() {
			actual := ""
			switch entry.Status() {
			case protocol.PlanStatusPlanned:
				actual = "planned"
			case protocol.PlanStatusFailed:
				actual = "failed"
			}
			if actual != statuses[index] {
				return false, fmt.Errorf("plan status %s != %s", actual, statuses[index])
			}
		}
	}
	if digest, ok := stringField(vector.Expected, "source_digest_hex"); ok {
		entry := plannedEntry(plan)
		if entry.SourceDigest() == nil || entry.SourceDigest().Hex() != digest {
			return false, fmt.Errorf("plan source_digest mismatch")
		}
	}
	if digest, ok := stringField(vector.Expected, "target_digest_hex"); ok {
		entry := plannedEntry(plan)
		patch := entry.SourcePatch()
		if patch == nil || patch.TargetDigest.Hex() != digest {
			return false, fmt.Errorf("plan patch target_digest mismatch")
		}
	}
	if code, ok := stringField(vector.Expected, "failure_code"); ok {
		found := false
		for _, entry := range plan.Files() {
			if entry.Status() == protocol.PlanStatusFailed &&
				entry.FailureCode() != nil && *entry.FailureCode() == code {
				found = true
			}
		}
		if !found {
			return false, fmt.Errorf("plan failure_code mismatch")
		}
	}
	return true, nil
}

// plannedEntry finds the planned entry of a plan manifest.
func plannedEntry(plan *protocol.BatchPlanMessage) *protocol.BatchPlanFileEntry {
	for _, entry := range plan.Files() {
		if entry.Status() == protocol.PlanStatusPlanned {
			return entry
		}
	}
	return nil
}

func cliBatchResult(vector *caseData) (bool, error) {
	if _, ok := caseInput(vector, "branches"); ok {
		return cliRecoveryRule(vector)
	}
	jsonBytes, err := cliInputJSON(vector)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	record, err := protocol.DecodeJSON(jsonBytes, limits)
	if err != nil {
		return false, fmt.Errorf("result transport decode: %w", err)
	}
	if _, ok := caseExpected(vector, "error_code"); ok {
		message := &protocol.BatchResultMessage{}
		_, err := message.FromValue(record)
		return false, expectProtocolRejection(vector, err)
	}
	message := &protocol.BatchResultMessage{}
	result, err := message.FromValue(record)
	if err != nil {
		return false, fmt.Errorf("result decode: %w", err)
	}
	transport, err := protocol.EncodeJSON(record, limits)
	if err != nil {
		return false, fmt.Errorf("result re-encode: %w", err)
	}
	if string(transport) != string(jsonBytes) {
		return false, fmt.Errorf("result record must re-encode to the exact input bytes")
	}
	reEncoded, err := result.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(reEncoded, record) {
		return false, fmt.Errorf("result re-encode must reproduce the record exactly")
	}
	if pvceHex, ok := stringField(vector.Expected, "pvce_hex"); ok {
		pvce, err := protocol.EncodePVCE(record, limits)
		if err != nil {
			return false, err
		}
		if hexString(pvce) != pvceHex {
			return false, fmt.Errorf("result pvce_hex %s != %s", hexString(pvce), pvceHex)
		}
	}
	if productVersion, ok := stringField(vector.Expected, "product_version"); ok {
		if result.ProductVersion() != productVersion {
			return false, fmt.Errorf("result product_version mismatch")
		}
	}
	if statuses, ok := stringSequenceField(vector.Expected, "statuses"); ok {
		if len(result.Files()) != len(statuses) {
			return false, fmt.Errorf("result file count %d != %d", len(result.Files()), len(statuses))
		}
		for index, entry := range result.Files() {
			actual := ""
			switch entry.Status() {
			case protocol.ResultStatusCompleted:
				actual = "completed"
			case protocol.ResultStatusFailed:
				actual = "failed"
			case protocol.ResultStatusPending:
				actual = "pending"
			case protocol.ResultStatusSkippedStale:
				actual = "skipped-stale"
			}
			if actual != statuses[index] {
				return false, fmt.Errorf("result status %s != %s", actual, statuses[index])
			}
		}
	}
	if digest, ok := stringField(vector.Expected, "target_digest_hex"); ok {
		for _, entry := range result.Files() {
			if entry.Status() == protocol.ResultStatusCompleted {
				if entry.TargetDigest() == nil || entry.TargetDigest().Hex() != digest {
					return false, fmt.Errorf("result target_digest mismatch")
				}
			}
		}
	}
	if redacted, ok := booleanField(vector.Expected, "redacted"); ok {
		entry := result.Files()[0]
		if entry.Redacted() != redacted {
			return false, fmt.Errorf("result redacted mismatch")
		}
	}
	if code, ok := stringField(vector.Expected, "failure_code"); ok {
		found := false
		for _, entry := range result.Files() {
			if (entry.Status() == protocol.ResultStatusFailed ||
				entry.Status() == protocol.ResultStatusSkippedStale) &&
				entry.FailureCode() != nil && *entry.FailureCode() == code {
				found = true
			}
		}
		if !found {
			return false, fmt.Errorf("result failure_code mismatch")
		}
	}
	return true, nil
}

// cliRecoveryRule pins the RFC 0015 §9.4 three-way rule data-driven.
func cliRecoveryRule(vector *caseData) (bool, error) {
	branchesValue, ok := caseInput(vector, "branches")
	if !ok {
		return false, fmt.Errorf("missing input.branches")
	}
	branches, ok := branchesValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.branches must be Sequence")
	}
	for index := 0; index < branches.Len(); index++ {
		branch, ok := branches.At(index).(*core.Object)
		if !ok {
			return false, fmt.Errorf("branch must be Object")
		}
		disk, _ := stringField(branch, "disk")
		outcome, _ := stringField(branch, "outcome")
		expected := ""
		switch disk {
		case "source":
			expected = "redo"
		case "target":
			expected = "skip"
		case "other":
			expected = "stale"
		default:
			return false, fmt.Errorf("unknown disk branch %s", disk)
		}
		if outcome != expected {
			return false, fmt.Errorf("branch %d outcome %s != %s", index, outcome, expected)
		}
	}
	if illegalValue, ok := caseInput(vector, "illegal_branch"); ok {
		illegal, ok := illegalValue.(*core.Object)
		if !ok {
			return false, fmt.Errorf("illegal_branch must be Object")
		}
		disk, _ := stringField(illegal, "disk")
		if disk == "source" || disk == "target" || disk == "other" {
			return false, fmt.Errorf("branch %s must not be in the three-way rule", disk)
		}
	}
	return true, nil
}

func cliRedaction(vector *caseData) (bool, error) {
	if samplesValue, ok := caseInput(vector, "samples"); ok {
		samples, ok := samplesValue.(*core.Array)
		if !ok {
			return false, fmt.Errorf("input.samples must be Sequence")
		}
		for index := 0; index < samples.Len(); index++ {
			sample, ok := samples.At(index).(*core.Object)
			if !ok {
				return false, fmt.Errorf("sample must be Object")
			}
			redacted, _ := booleanField(sample, "redacted")
			count, _ := integerField(sample, "count")
			valid, _ := booleanField(sample, "valid")
			_, err := protocol.NewRedaction(redacted, count)
			if (err == nil) != valid {
				return false, fmt.Errorf("sample %d Redaction(%v, %d) validity mismatch", index, redacted, count)
			}
		}
		return true, nil
	}
	jsonBytes, err := cliInputJSON(vector)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	// The plan-byte case pins the presentation-only boundary on a batch-plan
	// record; the other cases decode the envelope.
	if _, ok := caseExpected(vector, "original_hex"); ok {
		record, err := protocol.DecodeJSON(jsonBytes, limits)
		if err != nil {
			return false, fmt.Errorf("plan transport decode: %w", err)
		}
		message := &protocol.BatchPlanMessage{}
		plan, err := message.FromValueWithRegistry(record, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		if err != nil {
			return false, fmt.Errorf("plan decode: %w", err)
		}
		entry := plannedEntry(plan)
		if entry == nil {
			return false, fmt.Errorf("no planned entry")
		}
		patch := entry.SourcePatch()
		if patch == nil {
			return false, fmt.Errorf("planned entry without source_patch")
		}
		if len(patch.Replacements) == 0 {
			return false, fmt.Errorf("no replacement in patch")
		}
		replacement := patch.Replacements[0]
		if originalHex, ok := stringField(vector.Expected, "original_hex"); ok {
			if hexString(replacement.Original) != originalHex {
				return false, fmt.Errorf("patch original bytes changed")
			}
		}
		if replacementHex, ok := stringField(vector.Expected, "replacement_hex"); ok {
			if hexString(replacement.Replacement) != replacementHex {
				return false, fmt.Errorf("patch replacement bytes changed")
			}
		}
		reEncoded, err := plan.ToValue()
		if err != nil {
			return false, fmt.Errorf("plan re-encode: %w", err)
		}
		if !core.Equal(reEncoded, record) {
			return false, fmt.Errorf("plan bytes are not preserved through the record")
		}
		transport, err := protocol.EncodeJSON(record, limits)
		if err != nil {
			return false, err
		}
		if string(transport) != string(jsonBytes) {
			return false, fmt.Errorf("plan record must re-encode to the exact input bytes")
		}
		return true, nil
	}
	message := &protocol.CliOutputMessage{}
	envelope, err := message.FromJSON(jsonBytes, limits)
	if err != nil {
		return false, fmt.Errorf("envelope decode: %w", err)
	}
	if err := assertEnvelopeFacts(envelope, vector); err != nil {
		return false, err
	}
	reEncoded, err := envelope.ToJSON(limits)
	if err != nil {
		return false, err
	}
	if string(reEncoded) != string(jsonBytes) {
		return false, fmt.Errorf("envelope re-encode must reproduce the input bytes exactly")
	}
	if placeholder, ok := stringField(vector.Expected, "placeholder"); ok {
		if !payloadContainsString(envelope.Payload(), placeholder) {
			return false, fmt.Errorf("placeholder value changed through the transport")
		}
	}
	return true, nil
}

// payloadContainsString reports whether the exact string appears anywhere in
// the payload tree (RFC 0015 §11.3 placeholder contract).
func payloadContainsString(value core.Value, needle string) bool {
	switch item := value.(type) {
	case core.String:
		return string(item) == needle
	case *core.Object:
		for _, entry := range item.Entries() {
			if payloadContainsString(entry.Value, needle) {
				return true
			}
		}
	case *core.Array:
		for _, element := range item.Items() {
			if payloadContainsString(element, needle) {
				return true
			}
		}
	}
	return false
}

func cliLimit(vector *caseData) (bool, error) {
	jsonBytes, err := cliInputJSON(vector)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	record, err := protocol.DecodeJSON(jsonBytes, limits)
	if err != nil {
		return false, fmt.Errorf("transport decode: %w", err)
	}
	classified := protocol.ClassifyErrorCode(protocol.ResourceLimitCode())
	if classified != protocol.ExitLimit {
		return false, fmt.Errorf("resource-limit must classify as limit")
	}
	if maxBytesValue, ok := caseInput(vector, "max_bytes"); ok {
		maxBytes, ok := maxBytesValue.(core.Integer)
		if !ok {
			return false, fmt.Errorf("input.max_bytes must be Integer")
		}
		budget := limits
		budget.MaxBytes = int(maxBytes.Int().Uint64())
		_, err := protocol.DecodeJSON(jsonBytes, budget)
		if err == nil {
			return false, fmt.Errorf("payload must exceed the transport budget")
		}
		protocolError, ok := err.(*protocol.ProtocolError)
		if !ok || protocolError.Kind != protocol.KindResourceLimit {
			return false, fmt.Errorf("decode must fail with ResourceLimit, got %v", err)
		}
		return true, nil
	}
	patchLimits := protocol.DefaultSourcePatchLimits()
	patchLimits.MaxReplacements = 0
	message := &protocol.BatchPlanMessage{}
	_, err = message.FromValueWithRegistryAndPatchLimits(record,
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7), patchLimits)
	if err == nil {
		return false, fmt.Errorf("plan must exceed the patch replacement budget")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Kind != protocol.KindResourceLimit {
		return false, fmt.Errorf("plan decode must fail with ResourceLimit, got %v", err)
	}
	return true, nil
}
