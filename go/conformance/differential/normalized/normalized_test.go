package normalized

// The Go test driver of the cross-language normalized-result differential
// harness (milestone 0.15.0 G1.5; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.4).
//
// TestCaseFileIntegrity runs whenever the provisioned case set is
// reachable (manifest id, case count, unique ids, schema validity), so
// `go test ./...` protects the input set when the data is present; on a
// clean clone without provisioned conformance data it skips with the
// documented skip in resolveCasesDir (wave-4 R49, 2026-08-15 — the old
// "always runs" wording was false; the skip is never silent). Third-path
// note (wave-4 R35): the five family packages (go/json, go/toml,
// go/yaml, go/properties, go/pilot) hard-read ../../conformance with no
// skip guard and fail loudly on a clean clone.
//
// TestNormalizedDifferential skips without the environment variable
// (documented skip, never silent) and runs only when
// scripts/go-verify-normalized-differential.ps1 provisioned the Rust
// evidence directory: the Go SDK executes the same input set, the two
// normalized results are compared field by field, and every divergence is
// reported as case id + field + both values.
//
// Since milestone 0.19.0 G5.2 the harness is bidirectional (roadmap §16.6
// 「`0.19.0`：双语言一致性与产品 Beta」「Rust/Go bidirectional
// differential runs」; https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.6):
// TestEmitGoNormalizedResults
// emits the Go-side evidence files for the same input set (one
// `<case-id>.txt` per case, the same line-oriented key=value format the
// forward direction reads), and the Rust example's consume mode compares
// them with its own results. TestEmitFormatConsistency always runs and
// proves the emitted files round-trip through the forward reader.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// casesDirEnv names the shared differential case directory (the directory
// that contains cases.json directly — here that is
// conformance/differential/normalized of the consema repository).
const casesDirEnv = "CONSEMA_DIFFERENTIAL_CASES_DIR"

// resolveCasesDir locates the shared differential case directory: the
// CONSEMA_DIFFERENTIAL_CASES_DIR environment variable (the
// conformance/differential root; each harness joins its own subdirectory),
// or — like the Kotlin runner's resolveRepoRoot probe
// (https://github.com/consema/consema-kt/blob/main/kotlin/src/main/kotlin/consema/conformance/Runner.kt
// 的 resolveRepoRoot) —
// the nearest ancestor of the package directory that carries a
// `conformance/differential` directory, either in this checkout or in a
// sibling consema checkout (consema and consema-go side by side). Without
// either the harness skips (documented skip, never silent).
func resolveCasesDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv(casesDirEnv); dir != "" {
		return dir
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot determine the working directory: %v", err)
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		for _, candidate := range []string{
			filepath.Join(dir, "conformance", "differential"),
			filepath.Join(dir, "consema", "conformance", "differential"),
		} {
			if _, err := os.Stat(filepath.Join(candidate, "cases.json")); err == nil {
				return candidate
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	t.Skipf("%s is not set and no conformance/differential case set is reachable from this package: run scripts/go-verify-normalized-differential.ps1 (which sets %s) or set %s to the shared case directory", casesDirEnv, casesDirEnv, casesDirEnv)
	return ""
}

// loadCaseJSON reads the provisioned normalized case file from the shared
// differential directory.
func loadCaseJSON(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(resolveCasesDir(t), "normalized", "cases.json"))
	if err != nil {
		t.Fatalf("cannot read normalized/cases.json: %v", err)
	}
	return data
}

// expectedCaseCount is the exact size of the provisioned input set (frozen
// test data; measured from cases.json: 108 cases). The integrity test fails
// if the provisioned file's case count drifts from it.
const expectedCaseCount = 108

// loadCaseFile parses and validates the provisioned case set.
func loadCaseFile(t *testing.T) []fileCase {
	t.Helper()
	var file struct {
		Manifest string     `json:"manifest"`
		Cases    []fileCase `json:"cases"`
	}
	if err := json.Unmarshal(loadCaseJSON(t), &file); err != nil {
		t.Fatalf("cases.json is not valid JSON: %v", err)
	}
	if file.Manifest != CaseFileManifest {
		t.Fatalf("cases.json manifest = %q, want %q", file.Manifest, CaseFileManifest)
	}
	if len(file.Cases) != expectedCaseCount {
		t.Fatalf("cases.json has %d cases, want %d (the differential input set)", len(file.Cases), expectedCaseCount)
	}
	seen := make(map[string]bool, len(file.Cases))
	for _, c := range file.Cases {
		if c.ID == "" {
			t.Fatal("case with an empty id")
		}
		if seen[c.ID] {
			t.Fatalf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		switch c.Kind {
		case "document":
			switch c.Format {
			case "json", "toml", "yaml", "ini", "properties":
			default:
				t.Fatalf("case %s: unknown format %q", c.ID, c.Format)
			}
			if _, err := parseDocumentProfile(&c); err != nil {
				t.Fatalf("case %s: %v", c.ID, err)
			}
			if len(c.Steps) == 0 {
				t.Fatalf("case %s: document case without steps", c.ID)
			}
			for _, step := range c.Steps {
				switch step.Op {
				case "parse", "query-native", "query-syntax", "project", "materialize", "edit":
				default:
					t.Fatalf("case %s: unknown step op %q", c.ID, step.Op)
				}
				switch step.Op {
				case "query-native", "query-syntax":
					if step.Domain == "" {
						t.Fatalf("case %s: query step without a domain", c.ID)
					}
				case "project":
					if step.Target == "" {
						t.Fatalf("case %s: project step without a target", c.ID)
					}
				case "materialize":
					if step.TargetProfile == "" || step.Style == "" {
						t.Fatalf("case %s: materialize step without target_profile/style", c.ID)
					}
				case "edit":
					if len(step.Operations) == 0 {
						t.Fatalf("case %s: edit step without operations", c.ID)
					}
					for _, op := range step.Operations {
						if op.Operation == "" {
							t.Fatalf("case %s: edit operation without a name", c.ID)
						}
						if op.Target == nil || op.Target.Kind == "" {
							t.Fatalf("case %s: edit operation %s without a target", c.ID, op.Operation)
						}
					}
				}
			}
		case "source":
			if c.Input == nil {
				t.Fatalf("case %s: source case without input", c.ID)
			}
			if c.Request == nil || c.Request.ProfileDefault == "" {
				t.Fatalf("case %s: source case without request.profile_default", c.ID)
			}
		default:
			t.Fatalf("case %s: unknown kind %q", c.ID, c.Kind)
		}
	}
	return file.Cases
}

// TestCaseFileIntegrity validates the provisioned case set. It runs with
// no Rust evidence needed, but only when the case set is reachable — on a
// clean clone it skips with the documented skip in resolveCasesDir
// (wave-4 R49, 2026-08-15). When the data is present, `go test ./...`
// guards the input set even without the orchestrator.
func TestCaseFileIntegrity(t *testing.T) {
	loadCaseFile(t)
}

// TestNormalizedDifferential computes the Go normalized results for the
// whole input set and compares them field by field against the Rust
// evidence files (produced by scripts/go-verify-normalized-differential.ps1
// into the directory named by CONSEMA_DIFFERENTIAL_NORMALIZED_RUST_DIR).
func TestNormalizedDifferential(t *testing.T) {
	cases := loadCaseFile(t)
	rustDir := os.Getenv(RustDirEnv)
	if rustDir == "" {
		t.Skipf("%s is not set: run scripts/go-verify-normalized-differential.ps1 to provision the Rust evidence files", RustDirEnv)
	}

	knownIDs := make(map[string]bool, len(cases))
	for _, c := range cases {
		knownIDs[c.ID] = true
	}
	entries, err := os.ReadDir(rustDir)
	if err != nil {
		t.Fatalf("cannot read the Rust evidence directory %q: %v", rustDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".txt")
		if !knownIDs[id] {
			t.Fatalf("rust evidence file %q does not correspond to any case (case file drift?)", entry.Name())
		}
	}

	passed := 0
	var failures []string
	for _, c := range cases {
		goLines, err := runCase(&c)
		if err != nil {
			t.Fatalf("case %s: harness bug: %v", c.ID, err)
		}
		rustLines := readEvidenceFile(t, rustDir, c.ID)
		fieldFailures := compareFacts(c.ID, goLines, rustLines)
		if len(fieldFailures) == 0 {
			passed++
			continue
		}
		failures = append(failures, fieldFailures...)
	}
	for _, failure := range failures {
		t.Error(failure)
	}
	t.Logf("normalized-result differential: %d/%d equal", passed, passed+len(failures))
}

// runCase dispatches one case to its face runner.
func runCase(c *fileCase) ([]string, error) {
	switch c.Kind {
	case "document":
		return runDocumentCase(c)
	case "source":
		return runSourceCase(c)
	}
	return nil, fmt.Errorf("unknown case kind %q", c.Kind)
}

// compareFacts compares the two fact line sets field by field.
func compareFacts(id string, goLines, rustLines []string) []string {
	goFacts := make(map[string]string, len(goLines))
	rustFacts := make(map[string]string, len(rustLines))
	for _, line := range goLines {
		key, value, ok := splitFact(line)
		if !ok {
			return []string{fmt.Sprintf("case %s: Go side emitted malformed fact line %q", id, line)}
		}
		if _, exists := goFacts[key]; exists {
			return []string{fmt.Sprintf("case %s: Go side emitted duplicate fact key %q", id, key)}
		}
		goFacts[key] = value
	}
	for _, line := range rustLines {
		key, value, ok := splitFact(line)
		if !ok {
			return []string{fmt.Sprintf("case %s: Rust side emitted malformed fact line %q", id, line)}
		}
		if _, exists := rustFacts[key]; exists {
			return []string{fmt.Sprintf("case %s: Rust side emitted duplicate fact key %q", id, key)}
		}
		rustFacts[key] = value
	}
	var failures []string
	for key, goValue := range goFacts {
		rustValue, present := rustFacts[key]
		if !present {
			failures = append(failures, fmt.Sprintf("case %s: field %s: Rust side has no such field (Go value %q)", id, key, goValue))
			continue
		}
		if goValue != rustValue {
			failures = append(failures, fmt.Sprintf("case %s: field %s differs\n  Go:   %q\n  Rust: %q", id, key, goValue, rustValue))
		}
	}
	for key := range rustFacts {
		if _, present := goFacts[key]; !present {
			failures = append(failures, fmt.Sprintf("case %s: field %s: Go side has no such field (Rust value %q)", id, key, rustFacts[key]))
		}
	}
	return failures
}

// splitFact splits one key=value line.
func splitFact(line string) (string, string, bool) {
	index := strings.IndexByte(line, '=')
	if index < 0 {
		return "", "", false
	}
	return line[:index], line[index+1:], true
}

// readEvidenceFile reads one Rust evidence file.
func readEvidenceFile(t *testing.T, dir, id string) []string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, id+".txt"))
	if err != nil {
		t.Fatalf("case %s: missing Rust evidence file: %v (run scripts/go-verify-normalized-differential.ps1)", id, err)
	}
	return splitEvidenceLines(string(text))
}

// splitEvidenceLines splits one evidence file into fact lines (the shared
// reader of both directions; the Rust example's consume mode mirrors it).
func splitEvidenceLines(text string) []string {
	content := strings.TrimRight(text, "\r\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

// emitFactsToDir writes the Go-side evidence files: one `<case-id>.txt` per
// case, every fact line as `key=value\n` (byte-identical in shape to the
// Rust emitter's files, so the Rust consume mode can read them with the
// same reader). Returns the number of emitted cases.
func emitFactsToDir(cases []fileCase, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	var emitted int
	for _, c := range cases {
		lines, err := runCase(&c)
		if err != nil {
			return emitted, fmt.Errorf("case %s: harness bug: %w", c.ID, err)
		}
		var content strings.Builder
		for _, line := range lines {
			content.WriteString(line)
			content.WriteByte('\n')
		}
		if err := os.WriteFile(filepath.Join(dir, c.ID+".txt"), []byte(content.String()), 0o644); err != nil {
			return emitted, fmt.Errorf("case %s: %w", c.ID, err)
		}
		emitted++
	}
	return emitted, nil
}

// TestEmitGoNormalizedResults emits the Go-side normalized results for the
// whole input set into the directory named by GoDirEnv (one `<case-id>.txt`
// per case). It is the reverse direction of the bidirectional differential
// (milestone 0.19.0 G5.2): the Rust example's consume mode reads this
// directory and compares it with its own results. It skips without the
// environment variable (documented skip, never silent) and runs only when
// scripts/go-verify-normalized-differential.ps1 provisioned the directory.
func TestEmitGoNormalizedResults(t *testing.T) {
	cases := loadCaseFile(t)
	goDir := os.Getenv(GoDirEnv)
	if goDir == "" {
		t.Skipf("%s is not set: run scripts/go-verify-normalized-differential.ps1 to provision the Go evidence files", GoDirEnv)
	}
	emitted, err := emitFactsToDir(cases, goDir)
	if err != nil {
		t.Fatalf("cannot emit the Go evidence files: %v", err)
	}
	t.Logf("emitted %d Go normalized results into %s", emitted, goDir)
}

// TestEmitFormatConsistency proves the Go emitter writes the same format
// the forward direction reads: the emitted files round-trip through the
// forward reader (splitEvidenceLines) and compare equal field by field with
// the computed facts. It runs whenever the case set is reachable (it
// skips with the documented skip in resolveCasesDir when the data is
// missing — wave-4 R49, 2026-08-15), so `go test ./...` guards the
// reverse-direction file format whenever the data is present.
func TestEmitFormatConsistency(t *testing.T) {
	cases := loadCaseFile(t)
	dir := t.TempDir()
	emitted, err := emitFactsToDir(cases, dir)
	if err != nil {
		t.Fatalf("cannot emit the Go evidence files: %v", err)
	}
	if emitted != len(cases) {
		t.Fatalf("emitted %d cases, want %d", emitted, len(cases))
	}
	for _, c := range cases {
		lines := readEvidenceFile(t, dir, c.ID)
		computed, err := runCase(&c)
		if err != nil {
			t.Fatalf("case %s: harness bug: %v", c.ID, err)
		}
		for _, failure := range compareFacts(c.ID, computed, lines) {
			t.Error(failure)
		}
	}
	t.Logf("emitted format round-trips through the forward reader for %d/%d cases", emitted, len(cases))
}
