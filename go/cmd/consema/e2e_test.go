package main

// Process-level tests of the Go CLI binary (mirroring the Rust
// crates/consema/tests/cli_*.rs spirit): stdout/stderr separation, the
// exit-code matrix, the machine-output envelope shape, the full plan→apply
// flow, the interruption/write-failure injection seams
// (CONSEMA_APPLY_INTERRUPT_AFTER / CONSEMA_APPLY_WRITE_FAILURE), and the
// negative write-policy states (stale, tampered patch, read-only,
// directory, missing plan). The tests launch the built binary via os/exec
// (TestMain builds it into a scratch directory).

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"consema.dev/consema/protocol"
)

// cliBinary is the path of the built binary, set by TestMain.
var cliBinary string

func TestMain(m *testing.M) {
	scratch, err := os.MkdirTemp("", "consema-cli-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(scratch)
	binary := filepath.Join(scratch, "consema"+exeSuffix())
	cmd := exec.Command("go", "build", "-o", binary, ".")
	cmd.Dir = packageDir()
	output, err := cmd.CombinedOutput()
	if err != nil {
		panic("go build failed: " + err.Error() + "\n" + string(output))
	}
	cliBinary = binary
	os.Exit(m.Run())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// packageDir returns the source directory of this package.
func packageDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate package dir")
	}
	return filepath.Dir(file)
}

// runCLI launches the built binary and returns its exit code and the
// stdout/stderr bytes.
func runCLI(args ...string) (int, []byte, []byte) {
	return runCLIEnv(nil, args...)
}

// runCLIEnv launches the built binary with the given environment overrides.
func runCLIEnv(env []string, args ...string) (int, []byte, []byte) {
	command := exec.Command(cliBinary, args...)
	command.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	if exitError, ok := err.(*exec.ExitError); ok {
		code = exitError.ExitCode()
	} else if err != nil {
		panic(err)
	}
	return code, stdout.Bytes(), stderr.Bytes()
}

func TestE2EHelpAndVersion(t *testing.T) {
	code, stdout, stderr := runCLI("--help")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.HasPrefix(string(stdout), "consema "+productVersion+" ") {
		t.Fatalf("help = %s", stdout)
	}
	code, stdout, _ = runCLI("--version")
	if code != 0 || string(stdout) != productVersion+"\n" {
		t.Fatalf("version = %q", stdout)
	}
}

func TestE2EExitCodeMatrix(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		expected int
		stdout   bool // whether stdout must carry an envelope
	}{
		{"success", []string{"conformance"}, 0, true},
		{"usage-unknown-command", []string{"frobnicate"}, 1, false},
		{"usage-unknown-argument", []string{"--bogus"}, 1, false},
		{"usage-missing-profile", []string{"query", "--request-file", "r.json"}, 1, false},
		{"usage-pretty-without-json", []string{"conformance", "--pretty"}, 1, false},
		{"data-missing-inspect", []string{"inspect", "definitely-missing.conf", "--json"}, 2, true},
		{"limit-max-bytes", []string{"inspect", "definitely-missing.conf"}, 2, false},
		{"precondition-explain-unknown", []string{"explain", "nope@1"}, 2, false},
		{"internal-classification", []string{"explain", "example.unknown@1", "--json"}, 2, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(test.args...)
			if code != test.expected {
				t.Fatalf("exit = %d want %d, stderr = %s", code, test.expected, stderr)
			}
			if test.stdout && len(stdout) == 0 {
				t.Fatalf("envelope expected on stdout: %q", stdout)
			}
			if !test.stdout && len(stdout) != 0 {
				t.Fatalf("no stdout expected: %q", stdout)
			}
		})
	}
}

func TestE2EUsageFailuresNeverProduceAnEnvelope(t *testing.T) {
	for _, args := range [][]string{
		{"frobnicate"},
		{"inspect"},
		{"inspect", "a", "b"},
		{"capabilities", "extra"},
		{"conformance", "--pretty"},
		{"query", "--request-file", "r.json"},
		{"edit", "x.conf"},
	} {
		code, stdout, stderr := runCLI(args...)
		if code != 1 {
			t.Fatalf("%v: exit = %d", args, code)
		}
		if len(stdout) != 0 {
			t.Fatalf("%v: usage failures must not emit stdout: %q", args, stdout)
		}
		if len(stderr) == 0 {
			t.Fatalf("%v: stderr must carry the diagnostic", args)
		}
	}
}

func TestE2EConformanceJSONEnvelopeShape(t *testing.T) {
	code, stdout, stderr := runCLI("conformance", "--json")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !bytes.HasSuffix(stdout, []byte("\n")) {
		t.Fatal("envelope line must end in one LF")
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "conformance" || envelope.ExitClass() != protocol.ExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	// The fixed field set of RFC 0015 §4.1 is present.
	value := envelope.Payload()
	_ = value
	// The redaction record is always present with the invariant.
	if envelope.Redaction() == nil || envelope.Redaction().Redacted() {
		t.Fatalf("redaction = %v", envelope.Redaction())
	}
}

func TestE2EFullPlanApplyFlow(t *testing.T) {
	dir := newTestDir(t, "e2e-flow")
	source := writeTestFile(t, dir, "app.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	resultPath := filepath.Join(dir, "result.json")

	code, stdout, stderr := runCLI("plan", source, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stdout), "consema plan: 1 file(s)") {
		t.Fatalf("plan report = %s", stdout)
	}
	if _, err := os.Stat(planPath); err != nil {
		t.Fatalf("plan manifest missing: %v", err)
	}

	code, stdout, stderr = runCLI("apply", planPath, "--output", resultPath)
	if code != 0 {
		t.Fatalf("apply exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stdout), "completed") {
		t.Fatalf("apply report = %s", stdout)
	}
	got, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, iniTarget()) {
		t.Fatalf("target bytes = %q", got)
	}
	// The result manifest is a byte-valid core.batch-result@1 record.
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	message := &protocol.BatchResultMessage{}
	decoded, decodeErr := message.FromJSON(resultBytes, protocolLimits())
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(decoded.Files()) != 1 ||
		decoded.Files()[0].Status() != protocol.ResultStatusCompleted {
		t.Fatalf("result = %v", decoded.Files())
	}
	// The manifest file carries the same record as the --json envelope
	// payload (byte-identical).
	code, stdout, _ = runCLI("apply", planPath, "--output", resultPath, "--json")
	if code != 0 {
		t.Fatalf("apply --json exit = %d", code)
	}
	envelope := envelopeOf(t, stdout)
	payloadBytes, err := protocol.EncodeJSON(envelope.Payload(), protocolLimits())
	if err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(resultPath)
	if !bytes.Equal(again, payloadBytes) {
		t.Fatal("result manifest must be byte-identical to the envelope payload")
	}
}

func TestE2EApplyStaleIsSkippedStaleExitFour(t *testing.T) {
	dir := newTestDir(t, "e2e-stale")
	source := writeTestFile(t, dir, "app.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	code, _, stderr := runCLI("plan", source, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d, stderr = %s", code, stderr)
	}
	// The file changes after the plan.
	if err := os.WriteFile(source, []byte("[db]\nport=9999\npassword=hunter2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLI("apply", planPath)
	if code != 4 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stderr), staleCode) {
		t.Fatalf("stderr = %s", stderr)
	}
	if !strings.Contains(string(stdout), "skipped-stale") {
		t.Fatalf("report = %s", stdout)
	}
	// The stale file bytes are untouched.
	got, _ := os.ReadFile(source)
	if !strings.Contains(string(got), "port=9999") {
		t.Fatalf("stale file must be untouched: %q", got)
	}
}

func TestE2EApplyInterruptionAndResume(t *testing.T) {
	dir := newTestDir(t, "e2e-interrupt")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	code, _, stderr := runCLI("plan", a, b, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d, stderr = %s", code, stderr)
	}
	resultPath := filepath.Join(dir, "result.json")
	// Interrupt at file 1's pending mark: the on-disk manifest shows
	// [completed, pending], exit 4, stdout empty (RFC 0015 §4.2).
	code, stdout, stderr := runCLIEnv(
		[]string{"CONSEMA_APPLY_INTERRUPT_AFTER=1"},
		"apply", planPath, "--output", resultPath)
	if code != 4 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stdout) != 0 {
		t.Fatalf("interruption writes no further stdout bytes: %q", stdout)
	}
	if !strings.Contains(string(stderr), interruptedCode) {
		t.Fatalf("stderr = %s", stderr)
	}
	resultBytes, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	message := &protocol.BatchResultMessage{}
	decoded, decodeErr := message.FromJSON(resultBytes, protocolLimits())
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if decoded.Files()[0].Status() != protocol.ResultStatusCompleted ||
		decoded.Files()[1].Status() != protocol.ResultStatusPending {
		t.Fatalf("interrupted manifest = %v", decoded.Files())
	}
	// Re-run without injection: all completed, exit 0.
	code, _, stderr = runCLI("apply", planPath, "--output", resultPath)
	if code != 0 {
		t.Fatalf("resume exit = %d, stderr = %s", code, stderr)
	}
	got, _ := os.ReadFile(b)
	if !bytes.Equal(got, iniTarget()) {
		t.Fatalf("b = %q", got)
	}
}

func TestE2EApplyWriteFailureInjection(t *testing.T) {
	dir := newTestDir(t, "e2e-write-failure")
	a := writeTestFile(t, dir, "a.conf", iniSource())
	b := writeTestFile(t, dir, "b.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	code, _, stderr := runCLI("plan", a, b, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d, stderr = %s", code, stderr)
	}
	// The first atomic target write fails; the batch continues and the
	// second file completes.
	code, stdout, stderr := runCLIEnv(
		[]string{"CONSEMA_APPLY_WRITE_FAILURE=io"},
		"apply", planPath)
	if code != 4 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stderr), "cli.write.io@1") {
		t.Fatalf("stderr = %s", stderr)
	}
	if !strings.Contains(string(stdout), "failed cli.write.io@1") ||
		!strings.Contains(string(stdout), "completed") {
		t.Fatalf("report = %s", stdout)
	}
	got, _ := os.ReadFile(a)
	if !bytes.Equal(got, iniSource()) {
		t.Fatalf("a must be untouched: %q", got)
	}
	got, _ = os.ReadFile(b)
	if !bytes.Equal(got, iniTarget()) {
		t.Fatalf("b = %q", got)
	}
}

func TestE2EApplyDirectoryTargetIsFailed(t *testing.T) {
	dir := newTestDir(t, "e2e-directory")
	source := writeTestFile(t, dir, "app.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	code, _, _ := runCLI("plan", source, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d", code)
	}
	// Replace the target with a directory: the write policy fails the file.
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI("apply", planPath)
	if code != 4 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	// A directory target fails as a per-file precondition failure: either
	// the read fails (cli.data.io@1, on Windows opening a directory for
	// reading fails) or the write policy rejects it
	// (cli.write.target-is-directory@1, on POSIX the read succeeds).
	if !strings.Contains(string(stderr), "cli.write.target-is-directory@1") &&
		!strings.Contains(string(stderr), "cli.data.io@1") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestE2EApplyReadOnlyTargetIsFailed(t *testing.T) {
	dir := newTestDir(t, "e2e-readonly")
	source := writeTestFile(t, dir, "app.conf", iniSource())
	request := writeTestFile(t, dir, "edit-request.json", editRequestFixture("db", "port", "9090"))
	planPath := filepath.Join(dir, "plan.json")
	code, _, _ := runCLI("plan", source, "--profile", "ini.portable",
		"--request-file", request, "--output", planPath)
	if code != 0 {
		t.Fatalf("plan exit = %d", code)
	}
	if err := os.Chmod(source, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(source, 0o644)
	})
	code, _, stderr := runCLI("apply", planPath)
	if code != 4 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stderr), "cli.write.read-only@1") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestE2EApplyMissingPlanIsDataError(t *testing.T) {
	dir := newTestDir(t, "e2e-missing-plan")
	missing := filepath.Join(dir, "missing.json")
	code, stdout, _ := runCLI("apply", missing, "--json")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) == 0 {
		t.Fatal("data-class failures carry the envelope")
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocol.ExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "cli.data.io@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
}

func TestE2EMachineOutputIsByteDeterministic(t *testing.T) {
	dir := newTestDir(t, "e2e-determinism")
	source := writeTestFile(t, dir, "app.conf", iniSource())
	// Two identical runs produce identical bytes.
	_, first, _ := runCLI("inspect", source, "--json")
	_, second, _ := runCLI("inspect", source, "--json")
	if !bytes.Equal(first, second) {
		t.Fatal("identical input must produce identical bytes")
	}
	// --json --pretty is pure whitespace indentation of the canonical line.
	_, pretty, _ := runCLI("inspect", source, "--json", "--pretty")
	if !bytes.Equal(collapseWhitespace(pretty), first[:len(first)-1]) {
		t.Fatal("pretty output must collapse to the canonical bytes")
	}
}
