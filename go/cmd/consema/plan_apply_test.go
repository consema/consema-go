package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"consema.dev/consema/protocol"
)

func planRequest() []byte {
	return encodeEditRequestBytes("db", "port", "9090")
}

func encodeEditRequestBytes(section, key, value string) []byte {
	// This helper exists to keep the plan tests free of the *testing.T
	// dependency of encodeEditRequest (the machine tests run against
	// already-built fixtures).
	request := editRequestFixture(section, key, value)
	return request
}

func TestEditDryRunEmitsAByteValidEditPayload(t *testing.T) {
	dir := newTestDir(t, "edit-ok")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--json"},
		planRequest())
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "edit" || envelope.ExitClass() != protocol.ExitSuccess {
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
	// The cli.edit@1 payload record: plan + change_set + committed.
	payload := envelope.Payload().(*objectType)
	fields := payload.Entries()
	if fields[0].Key != "schema" ||
		string(fields[0].Value.(stringType)) != "cli.edit@1" ||
		fields[1].Key != "plan" || fields[2].Key != "change_set" ||
		fields[3].Key != "committed" ||
		fields[3].Value.(boolType) != false {
		t.Fatalf("payload fields = %v", fields)
	}
	// The plan record carries the exact replacement facts.
	planObject := fields[1].Value.(*objectType)
	planFields := planObject.Entries()
	baseDigest := planFields[2].Value.(*objectType)
	digestEntries := baseDigest.Entries()
	if string(digestEntries[1].Value.(stringType)) !=
		protocol.DigestOf(iniSource()).Hex() {
		t.Fatal("base digest mismatch")
	}
	replacements := planFields[5].Value.(*arrayType)
	if len(replacements.Items()) != 1 {
		t.Fatalf("replacements = %d", len(replacements.Items()))
	}
}

func TestEditWriteFlagIsRefusedAsUsageWithoutEnvelope(t *testing.T) {
	dir := newTestDir(t, "edit-write")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--write", "--json"},
		planRequest())
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("usage failures never emit an envelope")
	}
	if !strings.Contains(stderrText(stderr), "--write") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestEditMissingTargetIsAPreconditionFailure(t *testing.T) {
	dir := newTestDir(t, "edit-missing")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--json"},
		encodeEditRequestBytes("db", "missing", "9090"))
	if code != 4 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "core.edit.target-not-found@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitPrecondition {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestEditUnknownOperationIDIsADataError(t *testing.T) {
	dir := newTestDir(t, "edit-unknown-op")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	request := editRequestWithUnknownOperation(t)
	code, _, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--json"}, request)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "not published by profile 'ini.portable'") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestEditHumanViewRendersOperationsAndRedacts(t *testing.T) {
	dir := newTestDir(t, "edit-human")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable"},
		encodeEditRequestBytes("db", "password", "hunter3"))
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	text := string(stdout)
	if !strings.Contains(text, "edit dry-run (ini.portable)") {
		t.Fatalf("report = %s", text)
	}
	if !strings.Contains(text, placeholder) {
		t.Fatal("the matching key name is redacted")
	}
	if strings.Contains(text, "password") || strings.Contains(text, "hunter3") {
		t.Fatalf("the key name and new value must be hidden: %s", text)
	}
	if !strings.Contains(stderrText(stderr), "redacted 2 value(s)") {
		t.Fatalf("stderr = %s", stderr)
	}
	// --show-secrets is the sole opt-out.
	code, stdout, stderr = runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--show-secrets"},
		encodeEditRequestBytes("db", "password", "hunter3"))
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if len(stderr) != 0 {
		t.Fatalf("no redaction notice under --show-secrets: %s", stderr)
	}
	if !strings.Contains(string(stdout), "password") ||
		!strings.Contains(string(stdout), "hunter3") {
		t.Fatalf("report = %s", stdout)
	}
}

func TestEditInvalidRedactKeysPatternIsUsage(t *testing.T) {
	dir := newTestDir(t, "edit-redact-keys")
	path := writeTestFile(t, dir, "app.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"edit", path, "--profile", "ini.portable", "--redact-keys", "ke[y]"},
		planRequest())
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatal("usage failures never emit an envelope")
	}
	if !strings.Contains(stderrText(stderr), "cli.usage.redaction-pattern@1") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestPlanMixedBatchExitsZeroWithPlannedAndFailedEntries(t *testing.T) {
	dir := newTestDir(t, "plan-mixed")
	good := writeTestFile(t, dir, "good.conf", iniSource())
	missing := writeTestFile(t, dir, "missing.conf", []byte("[db]\nhost=db.internal\n"))
	code, stdout, stderr := runRequestCommand(t,
		[]string{"plan", good, missing, "--profile", "ini.portable", "--json"},
		planRequest())
	if code != 0 {
		t.Fatalf("plan exits 0 with per-file failed: %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "plan" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	manifest := &protocol.BatchPlanMessage{}
	manifest, err := manifest.FromValue(envelope.Payload())
	if err != nil {
		t.Fatal(err)
	}
	files := manifest.Files()
	if len(files) != 2 {
		t.Fatalf("files = %d", len(files))
	}
	if files[0].Path() != good || files[0].Status() != protocol.PlanStatusPlanned {
		t.Fatalf("files[0] = %v", files[0])
	}
	if files[0].Profile() == nil || files[0].Profile().ID() != "ini.portable" {
		t.Fatalf("profile = %v", files[0].Profile())
	}
	// The cross constraint: source_digest == source_patch.base_digest.
	if !files[0].SourceDigest().Equal(files[0].SourcePatch().BaseDigest) {
		t.Fatal("source_digest must equal source_patch.base_digest")
	}
	if files[1].Status() != protocol.PlanStatusFailed ||
		files[1].FailureCode() == nil ||
		*files[1].FailureCode() != "core.edit.target-not-found@1" {
		t.Fatalf("files[1] = %v", files[1])
	}
	diagnostics := files[1].Diagnostics()
	if len(diagnostics) == 0 {
		t.Fatal("failed entries require diagnostics")
	}
	// The stderr carries the per-file failure line.
	if !strings.Contains(stderrText(stderr), "core.edit.target-not-found@1") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestPlanManifestBytesMatchTheOutputFile(t *testing.T) {
	dir := newTestDir(t, "plan-output")
	good := writeTestFile(t, dir, "good.conf", iniSource())
	output := filepath.Join(dir, "batch.plan.json")
	code, stdout, stderr := runRequestCommand(t,
		[]string{"plan", good, "--profile", "ini.portable", "--json", "--output", output},
		planRequest())
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	// The --output file carries the same record bytes as the envelope
	// payload.
	planPayload := envelope.Payload()
	payloadBytes, encodeErr := protocol.EncodeJSON(planPayload, protocolLimits())
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	fileBytes, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(fileBytes) != string(payloadBytes) {
		t.Fatal("the --output file must carry the same record bytes as the envelope payload")
	}
	decoded := &protocol.BatchPlanMessage{}
	decoded, decodeErr := decoded.FromJSON(fileBytes, protocolLimits())
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(decoded.Files()) != 1 ||
		decoded.Files()[0].Status() != protocol.PlanStatusPlanned {
		t.Fatal("file must be a byte-valid batch-plan record")
	}
}

func TestPlanBatchCountLimitIsALimitError(t *testing.T) {
	dir := newTestDir(t, "plan-limit")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"plan", a, b, "--profile", "ini.portable", "--max-files", "1", "--json"},
		planRequest())
	if code != 3 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "cli.limit.batch-count@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitLimit {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestPlanOutputWriteFailuresArePreconditionErrors(t *testing.T) {
	dir := newTestDir(t, "plan-write")
	good := writeTestFile(t, dir, "good.conf", iniSource())
	outputDir := filepath.Join(dir, "out-dir")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runRequestCommand(t,
		[]string{"plan", good, "--profile", "ini.portable", "--json", "--output", outputDir},
		planRequest())
	if code != 4 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "cli.write.target-is-directory@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitPrecondition {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestPlanRequestDecodeFailureIsADataError(t *testing.T) {
	dir := newTestDir(t, "plan-request")
	good := writeTestFile(t, dir, "good.conf", iniSource())
	code, stdout, stderr := runRequestCommand(t,
		[]string{"plan", good, "--profile", "ini.portable", "--json"},
		[]byte("not-a-request"))
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderrText(stderr), "cli.data.invalid-request@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
}

func TestApplyMachineHappyPath(t *testing.T) {
	dir := newTestDir(t, "apply-happy")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	plan := applyPlanOf(t, a, b, false)
	resultPath := filepath.Join(dir, "result.json")
	var stderr strings.Builder
	outcome, err := runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Interrupted || stderr.Len() != 0 {
		t.Fatalf("outcome = %+v, stderr = %s", outcome, stderr.String())
	}
	statuses := make([]string, 0, len(outcome.Entries))
	for _, entry := range outcome.Entries {
		statuses = append(statuses, string(entry.Status))
	}
	if len(statuses) != 2 || statuses[0] != "completed" || statuses[1] != "completed" {
		t.Fatalf("statuses = %v", statuses)
	}
	if got, _ := os.ReadFile(a); string(got) != string(iniTarget()) {
		t.Fatalf("a = %q", got)
	}
	if got, _ := os.ReadFile(b); string(got) != string(iniTarget()) {
		t.Fatalf("b = %q", got)
	}
	// The on-disk result manifest decodes as core.batch-result@1.
	bytes, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	message := &protocol.BatchResultMessage{}
	message, readErr = message.FromJSON(bytes, protocolLimits())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(message.Files()) != 2 {
		t.Fatalf("files = %d", len(message.Files()))
	}
	for _, entry := range message.Files() {
		if entry.Status() != protocol.ResultStatusCompleted || entry.FailureCode() != nil {
			t.Fatalf("entry = %v", entry)
		}
	}
}

func TestApplyMachineStaleSourceIsSkippedStaleWithoutRewrite(t *testing.T) {
	dir := newTestDir(t, "apply-stale")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	plan := applyPlanOf(t, a, b, false)
	// The file changes after the plan: digest differs from both source and
	// target digests.
	external := []byte("[db]\nport=8080\npassword=hunter3\n")
	if err := os.WriteFile(a, external, 0o644); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(dir, "result.json")
	var stderr strings.Builder
	outcome, err := runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusSkippedStale {
		t.Fatalf("status = %v", outcome.Entries[0].Status)
	}
	if outcome.Entries[0].FailureCode == nil ||
		*outcome.Entries[0].FailureCode != staleCode {
		t.Fatalf("failure = %v", outcome.Entries[0].FailureCode)
	}
	if outcome.Entries[1].Status != protocol.ResultStatusCompleted {
		t.Fatalf("status = %v", outcome.Entries[1].Status)
	}
	if got, _ := os.ReadFile(a); string(got) != string(external) {
		t.Fatalf("a must be untouched: %q", got)
	}
	if !strings.Contains(stderr.String(), staleCode) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestApplyMachineInterruptionPersistsPendingBeforeWriteAndResumeCompletes(t *testing.T) {
	dir := newTestDir(t, "apply-interrupt")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	plan := applyPlanOf(t, a, b, false)
	resultPath := filepath.Join(dir, "result.json")
	after := 1
	injections := applyInjections{interruptAfter: &after}
	var stderr strings.Builder
	outcome, err := runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&injections, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.Interrupted {
		t.Fatal("must be interrupted")
	}
	if !strings.Contains(stderr.String(), interruptedCode) {
		t.Fatalf("stderr = %s", stderr.String())
	}
	// The on-disk manifest must show [completed, pending].
	bytes, _ := os.ReadFile(resultPath)
	message := &protocol.BatchResultMessage{}
	message, decodeErr := message.FromJSON(bytes, protocolLimits())
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if message.Files()[0].Status() != protocol.ResultStatusCompleted {
		t.Fatalf("files[0] = %v", message.Files()[0].Status())
	}
	if message.Files()[1].Status() != protocol.ResultStatusPending {
		t.Fatalf("files[1] = %v", message.Files()[1].Status())
	}
	if message.Files()[1].FailureCode() != nil || message.Files()[1].TargetDigest() != nil {
		t.Fatal("pending entries carry neither failure_code nor target_digest")
	}
	// Re-run without injection: file 0 is skipped (disk == target), file 1
	// is redone (disk == source) — all completed, no pending.
	outcome, err = runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Interrupted {
		t.Fatal("resume must not be interrupted")
	}
	bytes, _ = os.ReadFile(resultPath)
	decoded, decodeErr := message.FromJSON(bytes, protocolLimits())
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	message = decoded
	for _, entry := range message.Files() {
		if entry.Status() != protocol.ResultStatusCompleted {
			t.Fatalf("entry = %v", entry.Status())
		}
	}
	if got, _ := os.ReadFile(b); string(got) != string(iniTarget()) {
		t.Fatalf("b = %q", got)
	}
}

func TestApplyMachineWriteFailureMarksFailedAndContinuesTheBatch(t *testing.T) {
	dir := newTestDir(t, "apply-write-failure")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	plan := applyPlanOf(t, a, b, false)
	resultPath := filepath.Join(dir, "result.json")
	injections := applyInjections{
		writeFailure: &writeFailureInjection{
			code:    "cli.write.io@1",
			message: "injected disk full",
		},
	}
	var stderr strings.Builder
	outcome, err := runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&injections, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusFailed ||
		outcome.Entries[0].FailureCode == nil ||
		*outcome.Entries[0].FailureCode != "cli.write.io@1" {
		t.Fatalf("entries[0] = %+v", outcome.Entries[0])
	}
	if outcome.Entries[1].Status != protocol.ResultStatusCompleted {
		t.Fatalf("entries[1] = %+v", outcome.Entries[1])
	}
	if got, _ := os.ReadFile(a); string(got) != string(iniSource()) {
		t.Fatalf("a must be untouched: %q", got)
	}
	if got, _ := os.ReadFile(b); string(got) != string(iniTarget()) {
		t.Fatalf("b = %q", got)
	}
}

func TestApplyMachinePlanFailedEntryIsReReportedFailed(t *testing.T) {
	dir := newTestDir(t, "apply-plan-failed")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	failureCode := "core.edit.target-not-found@1"
	failedEntry, err := protocol.NewBatchPlanFileEntry(b, protocol.PlanStatusFailed,
		nil, nil, nil, nil, &failureCode,
		[]*protocol.Diagnostic{diagnosticFor(failureCode)},
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		t.Fatal(err)
	}
	plan := applyPlanOf(t, a, "", false)
	// Rebuild the plan with the failed entry appended.
	entries := append(plan.Files(), failedEntry)
	plan, err = protocol.NewBatchPlanMessage(productVersion, entries)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(dir, "result.json")
	var stderr strings.Builder
	outcome, batchErr := runBatch(plan, resultPath, 1<<20,
		func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusCompleted {
		t.Fatalf("entries[0] = %v", outcome.Entries[0].Status)
	}
	if outcome.Entries[1].Status != protocol.ResultStatusFailed ||
		outcome.Entries[1].FailureCode == nil ||
		*outcome.Entries[1].FailureCode != failureCode {
		t.Fatalf("entries[1] = %+v", outcome.Entries[1])
	}
	if !strings.Contains(stderr.String(), failureCode) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestApplyMachineReadOnlyTargetIsFailedReadOnly(t *testing.T) {
	dir := newTestDir(t, "apply-readonly")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	plan := applyPlanOf(t, a, "", false)
	if err := os.Chmod(a, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(a, 0o644)
	})
	resultPath := filepath.Join(dir, "result.json")
	var stderr strings.Builder
	outcome, err := runBatch(plan, resultPath, 1<<20, func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusFailed ||
		outcome.Entries[0].FailureCode == nil ||
		*outcome.Entries[0].FailureCode != "cli.write.read-only@1" {
		t.Fatalf("entries[0] = %+v", outcome.Entries[0])
	}
	if !strings.Contains(stderr.String(), "cli.write.read-only@1") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestApplyMachineTamperedPatchIsFailedOriginalMismatch(t *testing.T) {
	dir := newTestDir(t, "apply-tamper")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	plan := applyPlanOf(t, a, "", true)
	resultPath := filepath.Join(dir, "result.json")
	var stderr strings.Builder
	outcome, batchErr := runBatch(plan, resultPath, 1<<20,
		func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusFailed ||
		outcome.Entries[0].FailureCode == nil ||
		*outcome.Entries[0].FailureCode != "core.source.patch-original-mismatch@1" {
		t.Fatalf("entries[0] = %+v", outcome.Entries[0])
	}
	if got, _ := os.ReadFile(a); string(got) != string(iniSource()) {
		t.Fatalf("a must be untouched: %q", got)
	}
	if !strings.Contains(stderr.String(), "core.source.patch-original-mismatch@1") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestApplyMachineResumeExternalModificationIsSkippedStale(t *testing.T) {
	dir := newTestDir(t, "apply-resume-stale")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	plan := applyPlanOf(t, a, b, false)
	resultPath := filepath.Join(dir, "result.json")
	after := 1
	injections := applyInjections{interruptAfter: &after}
	var stderr strings.Builder
	interrupted, batchErr := runBatch(plan, resultPath, 1<<20,
		func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&injections, &stderr)
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	if !interrupted.Interrupted {
		t.Fatal("must be interrupted")
	}
	// An external concurrent modification of the pending file makes the
	// re-run skip it as stale.
	external := []byte("[db]\nport=8080\npassword=hunter4\n")
	if err := os.WriteFile(b, external, 0o644); err != nil {
		t.Fatal(err)
	}
	outcome, batchErr := runBatch(plan, resultPath, 1<<20,
		func() *redactPolicy { p := conservativePolicy(); return &p }(),
		&applyInjections{}, &stderr)
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	if outcome.Entries[0].Status != protocol.ResultStatusCompleted {
		t.Fatalf("entries[0] = %v", outcome.Entries[0].Status)
	}
	if outcome.Entries[1].Status != protocol.ResultStatusSkippedStale ||
		outcome.Entries[1].FailureCode == nil ||
		*outcome.Entries[1].FailureCode != staleCode {
		t.Fatalf("entries[1] = %+v", outcome.Entries[1])
	}
	if got, _ := os.ReadFile(b); string(got) != string(external) {
		t.Fatalf("b must be untouched: %q", got)
	}
}

func TestEntryRedactedFlagFollowsThePolicyOnSummaryKeyNames(t *testing.T) {
	policy := conservativePolicy()
	plain := plannedIniEntry(t, "x.conf", false)
	// The frozen default policy matches nothing in the content-free summary
	// keys of the wired INI vocabulary.
	if entryRedacted(plain, &policy) {
		t.Fatal("default policy must not match the plain summary")
	}
	// An explicit glob matching a summary key flips the fact.
	matching, err := conservativePolicy().withExtraPatterns([]string{"value*"})
	if err != nil {
		t.Fatal(err)
	}
	if !entryRedacted(plain, &matching) {
		t.Fatal("explicit glob must flip the redacted fact")
	}
	// --show-secrets is the sole opt-out and disables matching entirely.
	secrets := showSecretsPolicy()
	if entryRedacted(plain, &secrets) {
		t.Fatal("--show-secrets must disable matching")
	}
}
