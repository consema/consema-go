package normalized

// The Go test driver of the cross-language normalized-result differential
// harness (milestone 0.15.0 G1.5; docs/go-implementation-plan.md §4.4).
//
// TestCaseFileIntegrity always runs and guards the checked-in case set
// (manifest id, case count, unique ids, schema validity), so `go test
// ./...` protects the input set even without the orchestrator.
//
// TestNormalizedDifferential skips without the environment variable
// (documented skip, never silent) and runs only when
// scripts/go-verify-normalized-differential.ps1 provisioned the Rust
// evidence directory: the Go SDK executes the same input set, the two
// normalized results are compared field by field, and every divergence is
// reported as case id + field + both values.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed cases.json
var casesJSON []byte

// loadCaseFile parses and validates the checked-in case set.
func loadCaseFile(t *testing.T) []fileCase {
	t.Helper()
	var file struct {
		Manifest string     `json:"manifest"`
		Cases    []fileCase `json:"cases"`
	}
	if err := json.Unmarshal(casesJSON, &file); err != nil {
		t.Fatalf("cases.json is not valid JSON: %v", err)
	}
	if file.Manifest != CaseFileManifest {
		t.Fatalf("cases.json manifest = %q, want %q", file.Manifest, CaseFileManifest)
	}
	if len(file.Cases) < MinCaseCount {
		t.Fatalf("cases.json has %d cases, want >= %d (the differential input set)", len(file.Cases), MinCaseCount)
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

// TestCaseFileIntegrity validates the checked-in case set. It always runs,
// so `go test ./...` guards the input set even without the orchestrator.
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
	content := strings.TrimRight(string(text), "\r\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}
