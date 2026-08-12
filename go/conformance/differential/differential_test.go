package differential

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
	"consema.dev/consema/protocol"
)

// ---------------------------------------------------------------------------
// Cross-language PVCE/PGCE byte-parity harness (milestone 0.14.0 G0.5;
// docs/go-implementation-plan.md §4.4; roadmap §16.1 hard gate: "Rust 与 Go
// 的 PVCE/PGCE bytes 完全一致").
//
// The harness never imports or calls Rust (RFC 0016 §1.1 cgo ban): the
// checked-in case set (cases.json, the shared conformance/differential/
// directory of the consema repository — single authority,
// docs/five-language-ci-design.md §3.5) is encoded by both sides, and the
// Rust encoder's bytes are compared as files. Orchestration:
// scripts/go-verify-byte-parity.ps1 drives the Rust example
// (crates/consema-conformance/examples/emit_parity_bytes.rs) into a
// directory of `<case-id>.hex` files, then runs this test with
// CONSEMA_DIFFERENTIAL_RUST_DIR set to that directory. Without the variable
// the byte-parity test skips (documented skip, never silent) and only the
// case-file integrity checks run.
// ---------------------------------------------------------------------------

// casesDirEnv names the shared differential case directory (the directory
// that contains cases.json directly — for the byte-parity harness that is
// conformance/differential of the consema repository).
const casesDirEnv = "CONSEMA_DIFFERENTIAL_CASES_DIR"

// resolveCasesDir locates the shared differential case directory: the
// CONSEMA_DIFFERENTIAL_CASES_DIR environment variable, or — like the Kotlin
// runner's resolveRepoRoot probe (Runner.kt:447-460) — the nearest ancestor
// of the package directory that carries a `conformance/differential`
// directory, either in this checkout or in a sibling consema checkout
// (consema and consema-go side by side). Without either the harness skips
// (documented skip, never silent).
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
	t.Skipf("%s is not set and no conformance/differential case set is reachable from this package: run scripts/go-verify-byte-parity.ps1 (which sets %s) or set %s to the shared case directory", casesDirEnv, casesDirEnv, casesDirEnv)
	return ""
}

// loadCaseJSON reads the checked-in byte-parity case file from the shared
// differential directory.
func loadCaseJSON(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(resolveCasesDir(t), "cases.json"))
	if err != nil {
		t.Fatalf("cannot read cases.json: %v", err)
	}
	return data
}

const caseFileManifest = "consema.differential.byte-parity@1"

// expectedCaseCount is the exact size of the checked-in input set (frozen
// test data; measured from cases.json: 68 cases). The integrity test fails
// if the checked-in file's case count drifts from it.
const expectedCaseCount = 68

// rustDirEnv names the directory of Rust encoder hex files.
const rustDirEnv = "CONSEMA_DIFFERENTIAL_RUST_DIR"

// allKindNames is the closed fifteen-kind vocabulary used by the case file's
// "kinds" metadata (the core Kind names of RFC 0016 §4.1).
var allKindNames = []string{
	"Null", "Boolean", "String", "Integer", "Decimal",
	"BinaryFloat32", "BinaryFloat64", "Bytes", "Date", "Time",
	"LocalDateTime", "OffsetDateTime", "Array", "Object", "EntryMapping",
}

// fileCase is one entry of cases.json.
type fileCase struct {
	ID    string     `json:"id"`
	Codec string     `json:"codec"` // "pvce" or "pgce"
	Value string     `json:"value,omitempty"`
	Graph *graphDesc `json:"graph,omitempty"`
	Kinds []string   `json:"kinds"`
}

// graphDesc is the neutral PortableGraph descriptor of cases.json (the same
// shape as conformance/vectors/portable-graph-v1.json inputs): roots are
// node indices, node records mirror the vector node objects.
type graphDesc struct {
	Roots []int      `json:"roots"`
	Nodes []nodeDesc `json:"nodes"`
}

type nodeDesc struct {
	Kind    string        `json:"kind"` // "Scalar", "Sequence", "Mapping"
	Tag     string        `json:"tag"`
	Content string        `json:"content,omitempty"`
	Items   []int         `json:"items,omitempty"`
	Entries []mappingDesc `json:"entries,omitempty"`
}

type mappingDesc struct {
	Key   int `json:"key"`
	Value int `json:"value"`
}

// loadCaseFile parses and validates the checked-in case set: manifest id,
// case count lower bound, unique ids, known codecs, decodable PVCE values,
// buildable PGCE graphs, and fifteen-kind coverage.
func loadCaseFile(t *testing.T) []fileCase {
	t.Helper()
	var file struct {
		Manifest string     `json:"manifest"`
		Cases    []fileCase `json:"cases"`
	}
	if err := json.Unmarshal(loadCaseJSON(t), &file); err != nil {
		t.Fatalf("cases.json is not valid JSON: %v", err)
	}
	if file.Manifest != caseFileManifest {
		t.Fatalf("cases.json manifest = %q, want %q", file.Manifest, caseFileManifest)
	}
	if len(file.Cases) != expectedCaseCount {
		t.Fatalf("cases.json has %d cases, want %d (the differential input set)", len(file.Cases), expectedCaseCount)
	}
	seen := make(map[string]bool, len(file.Cases))
	kinds := make(map[string]bool)
	for _, c := range file.Cases {
		if c.ID == "" {
			t.Fatal("case with an empty id")
		}
		if seen[c.ID] {
			t.Fatalf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		switch c.Codec {
		case "pvce":
			if c.Value == "" {
				t.Fatalf("case %s: pvce case without a value", c.ID)
			}
			// The strict canonicality check (parse + re-encode) keeps the
			// file's transport JSON honest; the Rust side must accept the
			// same text.
			if _, err := protocol.DecodeJSON([]byte(c.Value), protocol.DefaultProtocolLimits()); err != nil {
				t.Fatalf("case %s: value is not canonical transport JSON: %v", c.ID, err)
			}
		case "pgce":
			if c.Graph == nil {
				t.Fatalf("case %s: pgce case without a graph", c.ID)
			}
			if _, err := buildGraph(c.Graph); err != nil {
				t.Fatalf("case %s: graph does not build: %v", c.ID, err)
			}
		default:
			t.Fatalf("case %s: unknown codec %q", c.ID, c.Codec)
		}
		for _, kind := range c.Kinds {
			kinds[kind] = true
		}
	}
	for _, kind := range allKindNames {
		if !kinds[kind] {
			t.Fatalf("case set does not cover kind %q (kinds metadata)", kind)
		}
	}
	return file.Cases
}

// TestCaseFileIntegrity validates the checked-in case set. It always runs
// (no Rust bytes needed), so `go test ./...` guards the file even without
// the orchestrator.
func TestCaseFileIntegrity(t *testing.T) {
	loadCaseFile(t)
}

// TestDifferentialByteParity compares the Go encoder's bytes with the Rust
// encoder's bytes (produced by scripts/go-verify-byte-parity.ps1) for every
// case, and checks the bidirectional direction: Rust bytes decode under the
// Go decoder and re-encode byte-identically.
func TestDifferentialByteParity(t *testing.T) {
	cases := loadCaseFile(t)
	rustDir := os.Getenv(rustDirEnv)
	if rustDir == "" {
		t.Skipf("%s is not set: run scripts/go-verify-byte-parity.ps1 to provision the Rust encoder bytes", rustDirEnv)
	}

	knownIDs := make(map[string]bool, len(cases))
	for _, c := range cases {
		knownIDs[c.ID] = true
	}
	entries, err := os.ReadDir(rustDir)
	if err != nil {
		t.Fatalf("cannot read the Rust byte directory %q: %v", rustDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".hex")
		if !knownIDs[id] {
			t.Fatalf("rust byte file %q does not correspond to any case (case file drift?)", entry.Name())
		}
	}

	var failures []string
	passed, pvceCount, pgceCount := 0, 0, 0
	for _, c := range cases {
		rustBytes := readHexFile(t, rustDir, c.ID)
		switch c.Codec {
		case "pvce":
			pvceCount++
			value, err := protocol.DecodeJSON([]byte(c.Value), protocol.DefaultProtocolLimits())
			if err != nil {
				// loadCaseFile already validated the value; reaching here is
				// a harness bug.
				t.Fatalf("case %s: value no longer decodes: %v", c.ID, err)
			}
			goBytes, err := core.EncodePVCE(value)
			if err != nil {
				t.Fatalf("case %s: Go EncodePVCE failed: %v", c.ID, err)
			}
			if !bytes.Equal(goBytes, rustBytes) {
				failures = append(failures, firstDiff(c.ID, "pvce", goBytes, rustBytes))
				continue
			}
			// Bidirectional: Rust bytes decode under the Go decoder and
			// re-encode byte-identically (roadmap §16.1 gate).
			decoded, err := core.DecodePVCE(rustBytes, core.DefaultDecodeLimits())
			if err != nil {
				failures = append(failures, fmt.Sprintf("case %s: Go cannot decode the Rust PVCE bytes: %v", c.ID, err))
				continue
			}
			reEncoded, err := core.EncodePVCE(decoded)
			if err != nil {
				failures = append(failures, fmt.Sprintf("case %s: Go re-encode of decoded Rust bytes failed: %v", c.ID, err))
				continue
			}
			if !bytes.Equal(reEncoded, rustBytes) {
				failures = append(failures, firstDiff(c.ID, "pvce-rust->go->re-encode", reEncoded, rustBytes))
				continue
			}
			passed++
		case "pgce":
			pgceCount++
			goGraph, err := buildGraph(c.Graph)
			if err != nil {
				t.Fatalf("case %s: graph no longer builds: %v", c.ID, err)
			}
			goBytes, err := graph.EncodePGCE(goGraph)
			if err != nil {
				t.Fatalf("case %s: Go EncodePGCE failed: %v", c.ID, err)
			}
			if !bytes.Equal(goBytes, rustBytes) {
				failures = append(failures, firstDiff(c.ID, "pgce", goBytes, rustBytes))
				continue
			}
			// Bidirectional: Rust bytes decode under the Go decoder, Equal
			// the original graph, and re-encode byte-identically.
			decoded, err := graph.DecodePGCE(rustBytes, graph.DefaultPGCELimits())
			if err != nil {
				failures = append(failures, fmt.Sprintf("case %s: Go cannot decode the Rust PGCE bytes: %v", c.ID, err))
				continue
			}
			if !graph.Equal(decoded, goGraph) {
				failures = append(failures, fmt.Sprintf("case %s: Go decode of Rust PGCE bytes is not Equal to the source graph", c.ID))
				continue
			}
			reEncoded, err := graph.EncodePGCE(decoded)
			if err != nil {
				failures = append(failures, fmt.Sprintf("case %s: Go re-encode of decoded Rust bytes failed: %v", c.ID, err))
				continue
			}
			if !bytes.Equal(reEncoded, rustBytes) {
				failures = append(failures, firstDiff(c.ID, "pgce-rust->go->re-encode", reEncoded, rustBytes))
				continue
			}
			passed++
		}
	}
	for _, failure := range failures {
		t.Error(failure)
	}
	t.Logf("byte parity: %d/%d equal (%d pvce, %d pgce)", passed, passed+len(failures), pvceCount, pgceCount)
}

// buildGraph constructs the graph of a neutral descriptor (the Go mirror of
// the Rust runner's graph_from_value).
func buildGraph(desc *graphDesc) (*graph.Graph, error) {
	builder := graph.NewBuilder(graph.DefaultLimits())
	ids := make([]graph.NodeID, len(desc.Nodes))
	for i := range desc.Nodes {
		id, err := builder.ReserveNode()
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	ref := func(index int) (graph.NodeID, error) {
		if index < 0 || index >= len(ids) {
			return graph.NodeID{}, fmt.Errorf("node reference %d out of range (0..%d)", index, len(ids)-1)
		}
		return ids[index], nil
	}
	for i, n := range desc.Nodes {
		switch n.Kind {
		case "Scalar":
			if err := builder.DefineScalar(ids[i], n.Tag, n.Content); err != nil {
				return nil, err
			}
		case "Sequence":
			items := make([]graph.NodeID, len(n.Items))
			for j, index := range n.Items {
				id, err := ref(index)
				if err != nil {
					return nil, err
				}
				items[j] = id
			}
			if err := builder.DefineSequence(ids[i], n.Tag, items); err != nil {
				return nil, err
			}
		case "Mapping":
			entries := make([]graph.MappingEntry, len(n.Entries))
			for j, entry := range n.Entries {
				key, err := ref(entry.Key)
				if err != nil {
					return nil, err
				}
				value, err := ref(entry.Value)
				if err != nil {
					return nil, err
				}
				entries[j] = graph.MappingEntry{Key: key, Value: value}
			}
			if err := builder.DefineMapping(ids[i], n.Tag, entries); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown node kind %q", n.Kind)
		}
	}
	for _, index := range desc.Roots {
		id, err := ref(index)
		if err != nil {
			return nil, err
		}
		if err := builder.PushRoot(id); err != nil {
			return nil, err
		}
	}
	return builder.Build()
}

// readHexFile reads one Rust byte file and decodes its hex.
func readHexFile(t *testing.T, dir, id string) []byte {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, id+".hex"))
	if err != nil {
		t.Fatalf("case %s: missing Rust byte file: %v (run scripts/go-verify-byte-parity.ps1)", id, err)
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil {
		t.Fatalf("case %s: Rust byte file is not valid hex: %v", id, err)
	}
	return decoded
}

// firstDiff reports a byte-level difference with the first differing offset
// and the full hex of both sides.
func firstDiff(id, direction string, goBytes, rustBytes []byte) string {
	index := 0
	for index < len(goBytes) && index < len(rustBytes) && goBytes[index] == rustBytes[index] {
		index++
	}
	return fmt.Sprintf(
		"case %s (%s): Go %d bytes, Rust %d bytes, first difference at offset %d\n  Go:   %x\n  Rust: %x",
		id, direction, len(goBytes), len(rustBytes), index, goBytes, rustBytes)
}
