package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runInspectWith(t *testing.T, dir string, args ...string) (uint8, []byte, []byte) {
	t.Helper()
	return runCLIUnit(args...)
}

func TestInspectReportsFileFactsAndAmbiguitySuccessfully(t *testing.T) {
	dir := newTestDir(t, "inspect")
	path := writeTestFile(t, dir, "app.conf", []byte("[section]\nvalue=1\n"))
	code, stdout, stderr := runCLIUnit("inspect", path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "consema inspect") ||
		!strings.Contains(text, "bytes: 18 bytes sha256:") ||
		!strings.Contains(text, "bom: none") ||
		!strings.Contains(text, "symlink: no") ||
		!strings.Contains(text, "markers: [section] line") ||
		!strings.Contains(text, "ambiguous: yes: [section] line is consistent with format families: ini, toml") {
		t.Fatalf("report = %s", text)
	}
}

func TestInspectJSONEmitsTheFrozenPayloadShape(t *testing.T) {
	dir := newTestDir(t, "inspect-json")
	path := writeTestFile(t, dir, "app.json", []byte("{\"a\":1}"))
	code, stdout, stderr := runCLIUnit("inspect", path, "--json")
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.Command() != "inspect" || envelope.ExitClass() != protocolExitSuccess {
		t.Fatalf("envelope = %v", envelope)
	}
	payload := envelope.Payload().(*coreObject)
	entries := payload.Entries()
	if entries[0].Key != "schema" || string(entries[0].Value.(coreString)) != "cli.inspect@1" {
		t.Fatalf("payload = %v", entries[0])
	}
}

func TestInspectUnreadableFileIsADataErrorWithEnvelope(t *testing.T) {
	missing := filepath.Join(newTestDir(t, "inspect-missing"), "missing.conf")
	code, stdout, stderr := runCLIUnit("inspect", missing, "--json")
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocolExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "cli.data.io@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if !strings.Contains(stderrText(stderr), "(code cli.data.io@1)") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestInspectUnknownProfileValueIsUsage(t *testing.T) {
	dir := newTestDir(t, "inspect-profile")
	path := writeTestFile(t, dir, "app.conf", []byte("value=1\n"))
	code, stdout, stderr := runCLIUnit("inspect", path, "--profile", "example.unknown")
	if code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if len(stdout) != 0 {
		t.Fatalf("usage failures never produce an envelope: %s", stdout)
	}
	if !strings.Contains(stderrText(stderr), "invalid --profile value") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestInspectParseFactsReportRecoveredFilesWithExitZero(t *testing.T) {
	dir := newTestDir(t, "inspect-recovered")
	path := writeTestFile(t, dir, "app.conf", []byte("[section]\nvalue=1\nbad line\n"))
	code, stdout, stderr := runCLIUnit("inspect", path, "--profile", "ini.portable")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if len(stderr) != 0 {
		t.Fatalf("stderr = %s", stderr)
	}
	text := string(stdout)
	if !strings.Contains(text, "parse (ini.portable@1): Recovered") ||
		!strings.Contains(text, "ini.sections: 1") {
		t.Fatalf("report = %s", text)
	}
}

func TestInspectLimitBudgetIsALimitError(t *testing.T) {
	dir := newTestDir(t, "inspect-limit")
	path := writeTestFile(t, dir, "app.conf", []byte("value=1\n"))
	code, stdout, _ := runCLIUnit("inspect", path, "--json", "--max-bytes", "4")
	if code != 3 {
		t.Fatalf("exit = %d", code)
	}
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocolExitLimit {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "cli.limit.file-size@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
}

func TestInspectXMLFatalIsADataErrorWithRegisteredFallback(t *testing.T) {
	dir := newTestDir(t, "inspect-xml")
	content := make([]byte, 0)
	for index := 0; index < 300; index++ {
		content = append(content, "<a>"...)
	}
	path := writeTestFile(t, dir, "deep.xml", content)
	code, stdout, stderr := runCLIUnit("inspect", path, "--profile", "xml.1.0-safe", "--json")
	if code != 2 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	// The envelope carries only registry-bound codes; the true format-local
	// code stays on stderr.
	envelope := envelopeOf(t, stdout)
	if envelope.ExitClass() != protocolExitData {
		t.Fatalf("class = %v", envelope.ExitClass())
	}
	diagnostics := envelope.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Code != "core.source.invalid-sequence@1" {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if !strings.Contains(stderrText(stderr), "xml.limit.depth@1") {
		t.Fatalf("stderr must keep the true format-local code: %s", stderr)
	}
}

func TestInspectJSONStructureCounts(t *testing.T) {
	dir := newTestDir(t, "inspect-json-counts")
	path := writeTestFile(t, dir, "app.json", []byte("{\"a\":1,\"b\":2}"))
	code, stdout, stderr := runCLIUnit("inspect", path, "--profile", "json.strict")
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(string(stdout), "json.object_members: 2") {
		t.Fatalf("report = %s", stdout)
	}
}

func TestInspectSymlinkFact(t *testing.T) {
	dir := newTestDir(t, "inspect-symlink")
	target := writeTestFile(t, dir, "target.conf", []byte("value=1\n"))
	link := filepath.Join(dir, "link.conf")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	code, stdout, _ := runCLIUnit("inspect", link)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(string(stdout), "symlink: yes") {
		t.Fatalf("report = %s", stdout)
	}
}
