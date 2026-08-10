package protocolexchange

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
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
// Cross-language protocol exchange harness (milestone 0.19.0 G5.3;
// docs/go-implementation-plan.md §2.6 and §4.4; roadmap §16.6 line 1549 and
// §22.2 line 1882: "protocol cross-encode/decode 100%").
//
// The harness never imports or calls Rust (RFC 0016 §1.1 cgo ban): the
// checked-in case set (cases.json, this directory) is decoded and re-encoded
// by both sides, and the other side's bytes are compared as files.
// Orchestration: scripts/go-verify-protocol-exchange.ps1 runs the Rust
// example (crates/consema-conformance/examples/emit_protocol_exchange.rs)
// over the case set into a directory, then runs this test with
// CONSEMA_EXCHANGE_RUST_DIR set to that directory and
// CONSEMA_EXCHANGE_GO_DIR set to a writable directory the Go side fills
// with its own encoder bytes (which the script then re-runs the Rust example
// over in --verify mode, closing the Go-encode -> Rust-decode direction).
//
// For every case:
//   - accept cases: both sides decode the canonical transport JSON with the
//     full typed record decoder, re-encode byte-identically on both
//     transports (canonical JSON and PVCE/1), and the cross-language bytes
//     are byte-equal; each side decodes the other side's bytes, the typed
//     record is equivalent (value-tree equality through the typed record
//     codec), and re-encoding is byte-identical.
//   - reject cases: both sides reject the same transport bytes with the same
//     registered error code (core.protocol.*@1). Error text never
//     participates in any comparison.
//
// The machine schema of the case file is the RFC 0015 protocol schema
// discriminator (core.cli-output@1, ...); it contains no Rust type names.
// Without the environment variables the exchange test skips (documented
// skip, never silent) and only the case-file integrity checks run.
// ---------------------------------------------------------------------------

//go:embed cases.json
var casesJSON []byte

const caseFileManifest = "consema.differential.protocol-exchange@1"

// minCaseCount is the task's lower bound for the input set ("至少 40 个
// case"). The integrity test fails if the checked-in file drops below it.
const minCaseCount = 40

// rustDirEnv names the directory of the Rust encoder's per-case files.
const rustDirEnv = "CONSEMA_EXCHANGE_RUST_DIR"

// goOutDirEnv names the directory the Go side writes its own encoder bytes
// into (consumed by the Rust example's --verify pass).
const goOutDirEnv = "CONSEMA_EXCHANGE_GO_DIR"

// allRecords is the closed record inventory of the exchange set. It is
// exactly the protocol record surface both implementations decode in full
// (go/protocol payload.go dispatch intersect crates/consema-protocol
// payload.rs dispatch). The six records validated in Go only at the envelope
// level (core.conversion-report@1, core.edit-plan@1,
// core.format-operation-registry@1, core.materialization-provenance-map@1,
// core.materialization-report@1, core.materialization-result@1) are outside
// the exchange set: negative cases would diverge because the Go side cannot
// reject what it does not yet decode (documented reachable-code difference,
// go/protocol/contract.go NewProtocolMessage note). No Rust type names
// appear anywhere in the case file.
var allRecords = []string{
	"core.batch-plan@1",
	"core.batch-result@1",
	"core.cancellation-request@1",
	"core.capability-declaration@1",
	"core.change-set@1",
	"core.cli-output@1",
	"core.completion@1",
	"core.diagnostic@1",
	"core.error-code-registry@1",
	"core.execution-policy@1",
	"core.graph-projection-result@1",
	"core.graph-provenance-map@1",
	"core.graph-query-result@1",
	"core.ini-query-result@1",
	"core.java-properties-query-result@1",
	"core.java-utf16-string@1",
	"core.materialization-request@2",
	"core.materialization-result@2",
	"core.portable-graph@1",
	"core.portable-value-json@1",
	"core.profile-descriptor@1",
	"core.projection-report@1",
	"core.projection-request@1",
	"core.projection-result@1",
	"core.provenance-map@1",
	"core.query-definition@1",
	"core.query-result@1",
	"core.registry-manifest@1",
	"core.source-encoding@1",
	"core.source-patch@2",
	"core.source-snapshot@2",
	"core.yaml-query-result@1",
}

// fileCase is one entry of cases.json.
type fileCase struct {
	ID       string `json:"id"`
	Record   string `json:"record"`
	JSON     string `json:"json"`
	Expected struct {
		ErrorCode string `json:"error_code"`
	} `json:"expected"`
}

// loadCaseFile parses and validates the checked-in case set: manifest id,
// case count lower bound, unique ids, known records, per-record positive and
// negative coverage, canonical transport JSON, and registered expected
// codes.
func loadCaseFile(t *testing.T) []fileCase {
	t.Helper()
	var file struct {
		Manifest string     `json:"manifest"`
		Cases    []fileCase `json:"cases"`
	}
	if err := json.Unmarshal(casesJSON, &file); err != nil {
		t.Fatalf("cases.json is not valid JSON: %v", err)
	}
	if file.Manifest != caseFileManifest {
		t.Fatalf("cases.json manifest = %q, want %q", file.Manifest, caseFileManifest)
	}
	if len(file.Cases) < minCaseCount {
		t.Fatalf("cases.json has %d cases, want >= %d", len(file.Cases), minCaseCount)
	}
	known := make(map[string]bool, len(allRecords))
	for _, record := range allRecords {
		known[record] = true
	}
	coverage := make(map[string][2]int) // record -> {accept, reject}
	seen := make(map[string]bool, len(file.Cases))
	limits := protocol.DefaultProtocolLimits()
	for _, c := range file.Cases {
		if c.ID == "" {
			t.Fatal("case with an empty id")
		}
		if seen[c.ID] {
			t.Fatalf("duplicate case id %q", c.ID)
		}
		seen[c.ID] = true
		if !known[c.Record] {
			t.Fatalf("case %s: record %q is not in the exchange inventory", c.ID, c.Record)
		}
		if c.Expected.ErrorCode != "" {
			if !protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1).Contains(c.Expected.ErrorCode) {
				t.Fatalf("case %s: expected code %q is not a registered protocol code", c.ID, c.Expected.ErrorCode)
			}
			coverage[c.Record] = [2]int{coverage[c.Record][0], coverage[c.Record][1] + 1}
			// The Go side must reject the case with exactly the expected
			// code, whether the transport or the typed record decoder
			// rejects it (transport-level rejections such as
			// non-canonical-json intentionally fail the canonicality check
			// below; record-level rejections must still parse canonically).
			if code := goRejectionCode(c, limits); code != c.Expected.ErrorCode {
				t.Fatalf("case %s: Go rejection code %q != expected %q", c.ID, code, c.Expected.ErrorCode)
			}
			continue
		}
		coverage[c.Record] = [2]int{coverage[c.Record][0] + 1, coverage[c.Record][1]}
		// The strict canonicality check (parse + re-encode) keeps the file's
		// transport JSON honest: the Rust side must accept the same text.
		value, err := protocol.DecodeJSON([]byte(c.JSON), limits)
		if err != nil {
			t.Fatalf("case %s: json is not canonical transport JSON: %v", c.ID, err)
		}
		// Accept cases must re-encode byte-identically through the typed
		// record codec on both transports.
		recordValue, err := decodeRecord(c.Record, value)
		if err != nil {
			t.Fatalf("case %s: Go typed record decode failed: %v", c.ID, err)
		}
		reEncoded, err := protocol.EncodeJSON(recordValue, limits)
		if err != nil {
			t.Fatalf("case %s: Go re-encode failed: %v", c.ID, err)
		}
		if !bytes.Equal(reEncoded, []byte(c.JSON)) {
			t.Fatalf("case %s: Go typed re-encode is not byte-identical to the case json", c.ID)
		}
		if _, err := protocol.EncodePVCE(recordValue, limits); err != nil {
			t.Fatalf("case %s: Go PVCE encode of the typed record failed: %v", c.ID, err)
		}
	}
	for _, record := range allRecords {
		counts := coverage[record]
		if counts[0] == 0 || counts[1] == 0 {
			t.Fatalf("record %s has no %s case in the exchange set",
				record, map[bool]string{true: "accept", false: "reject"}[counts[0] == 0])
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

// TestProtocolExchange verifies the bidirectional cross-language exchange:
// Go-encoded bytes match the Rust encoder's bytes, Rust bytes decode under
// the Go typed record codec to equivalent records and re-encode
// byte-identically, and rejection cases reject with the same registered code
// on both sides.
func TestProtocolExchange(t *testing.T) {
	cases := loadCaseFile(t)
	rustDir := os.Getenv(rustDirEnv)
	if rustDir == "" {
		t.Skipf("%s is not set: run scripts/go-verify-protocol-exchange.ps1 to provision the Rust side", rustDirEnv)
	}
	goOutDir := os.Getenv(goOutDirEnv)
	if goOutDir != "" {
		if err := os.MkdirAll(goOutDir, 0o755); err != nil {
			t.Fatalf("cannot create the Go output directory %q: %v", goOutDir, err)
		}
	}

	knownIDs := make(map[string]bool, len(cases))
	for _, c := range cases {
		knownIDs[c.ID] = true
	}
	entries, err := os.ReadDir(rustDir)
	if err != nil {
		t.Fatalf("cannot read the Rust exchange directory %q: %v", rustDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		base := name
		base = strings.TrimSuffix(base, ".json.hex")
		base = strings.TrimSuffix(base, ".pvce.hex")
		base = strings.TrimSuffix(base, ".error.txt")
		if base != name && !knownIDs[base] {
			t.Fatalf("rust file %q does not correspond to any case (case file drift?)", name)
		}
	}

	limits := protocol.DefaultProtocolLimits()
	var failures []string
	acceptCount, rejectCount := 0, 0
	acceptFailures, rejectFailures := 0, 0
	for _, c := range cases {
		if c.Expected.ErrorCode != "" {
			rejectCount++
			before := len(failures)
			failures = append(failures, verifyRejectCase(c, rustDir, goOutDir, limits)...)
			rejectFailures += len(failures) - before
			continue
		}
		acceptCount++
		before := len(failures)
		failures = append(failures, verifyAcceptCase(c, rustDir, goOutDir, limits)...)
		acceptFailures += len(failures) - before
	}
	for _, failure := range failures {
		t.Error(failure)
	}
	t.Logf("protocol exchange: %d/%d accept cases and %d/%d reject cases verified",
		acceptCount-acceptFailures, acceptCount,
		rejectCount-rejectFailures, rejectCount)
}

// verifyAcceptCase verifies one accept case end to end.
func verifyAcceptCase(c fileCase, rustDir, goOutDir string, limits protocol.ProtocolLimits) []string {
	var failures []string
	// Go side: decode the transport JSON, decode the typed record, re-encode.
	value, err := protocol.DecodeJSON([]byte(c.JSON), limits)
	if err != nil {
		return []string{fmt.Sprintf("case %s: case json no longer decodes: %v", c.ID, err)}
	}
	recordValue, err := decodeRecord(c.Record, value)
	if err != nil {
		return []string{fmt.Sprintf("case %s: Go typed record decode failed: %v", c.ID, err)}
	}
	goJSON, err := protocol.EncodeJSON(recordValue, limits)
	if err != nil {
		return []string{fmt.Sprintf("case %s: Go JSON encode failed: %v", c.ID, err)}
	}
	goPVCE, err := protocol.EncodePVCE(recordValue, limits)
	if err != nil {
		return []string{fmt.Sprintf("case %s: Go PVCE encode failed: %v", c.ID, err)}
	}
	if goOutDir != "" {
		writeHex(goOutDir, c.ID+".json", goJSON, &failures, c.ID)
		writeHex(goOutDir, c.ID+".pvce", goPVCE, &failures, c.ID)
	}

	// Rust encoder bytes must be byte-equal on both transports.
	rustJSON := readHexFile(rustDir, c.ID+".json", &failures, c.ID)
	rustPVCE := readHexFile(rustDir, c.ID+".pvce", &failures, c.ID)
	if len(failures) > 0 {
		return failures
	}
	if !bytes.Equal(goJSON, rustJSON) {
		failures = append(failures, firstDiff(c.ID, "json", goJSON, rustJSON))
	}
	if !bytes.Equal(goPVCE, rustPVCE) {
		failures = append(failures, firstDiff(c.ID, "pvce", goPVCE, rustPVCE))
	}

	// Rust encode -> Go decode: the Rust JSON bytes decode to an equivalent
	// typed record and re-encode byte-identically.
	rustValue, err := protocol.DecodeJSON(rustJSON, limits)
	if err != nil {
		failures = append(failures, fmt.Sprintf("case %s: Go cannot decode the Rust JSON bytes: %v", c.ID, err))
	} else if recordValue2, err := decodeRecord(c.Record, rustValue); err != nil {
		failures = append(failures, fmt.Sprintf("case %s: Go typed decode of the Rust JSON bytes failed: %v", c.ID, err))
	} else {
		if !core.Equal(recordValue2, recordValue) {
			failures = append(failures, fmt.Sprintf("case %s: Go typed decode of the Rust JSON is not equivalent to the case record", c.ID))
		}
		if reEncoded, err := protocol.EncodeJSON(recordValue2, limits); err != nil || !bytes.Equal(reEncoded, rustJSON) {
			failures = append(failures, fmt.Sprintf("case %s: Go JSON re-encode of the Rust bytes is not byte-identical: %v", c.ID, err))
		}
	}

	// Rust encode -> Go decode over the PVCE transport.
	rustValue2, err := protocol.DecodePVCE(rustPVCE, limits)
	if err != nil {
		failures = append(failures, fmt.Sprintf("case %s: Go cannot decode the Rust PVCE bytes: %v", c.ID, err))
	} else if recordValue3, err := decodeRecord(c.Record, rustValue2); err != nil {
		failures = append(failures, fmt.Sprintf("case %s: Go typed decode of the Rust PVCE bytes failed: %v", c.ID, err))
	} else {
		if !core.Equal(recordValue3, recordValue) {
			failures = append(failures, fmt.Sprintf("case %s: Go typed decode of the Rust PVCE is not equivalent to the case record", c.ID))
		}
		if reEncoded, err := protocol.EncodePVCE(recordValue3, limits); err != nil || !bytes.Equal(reEncoded, rustPVCE) {
			failures = append(failures, fmt.Sprintf("case %s: Go PVCE re-encode of the Rust bytes is not byte-identical: %v", c.ID, err))
		}
	}
	return failures
}

// goRejectionCode decodes one reject case on the Go side (transport then
// typed record decoder) and returns the registered rejection code.
func goRejectionCode(c fileCase, limits protocol.ProtocolLimits) string {
	value, err := protocol.DecodeJSON([]byte(c.JSON), limits)
	if err != nil {
		return protocolErrorCode(err)
	}
	if _, err := decodeRecord(c.Record, value); err != nil {
		return protocolErrorCode(err)
	}
	return ""
}

// verifyRejectCase verifies one reject case cross-language: the Go side
// rejects with exactly the expected code (re-verified here), and the Rust
// side must have recorded the same code.
func verifyRejectCase(c fileCase, rustDir, goOutDir string, limits protocol.ProtocolLimits) []string {
	var failures []string
	code := goRejectionCode(c, limits)
	if code != c.Expected.ErrorCode {
		return []string{fmt.Sprintf("case %s: Go rejection code %q != expected %q", c.ID, code, c.Expected.ErrorCode)}
	}
	// Record the Go rejection code so the Rust --verify pass can compare it
	// (the same file contract as the Rust emitter's error files).
	if goOutDir != "" {
		if err := os.WriteFile(filepath.Join(goOutDir, c.ID+".error.txt"), []byte(code+"\n"), 0o644); err != nil {
			return []string{fmt.Sprintf("case %s: cannot write the Go rejection file: %v", c.ID, err)}
		}
	}
	rustCode := readErrorFile(rustDir, c.ID, &failures, c.ID)
	if len(failures) > 0 {
		return failures
	}
	if rustCode != c.Expected.ErrorCode {
		failures = append(failures, fmt.Sprintf(
			"case %s: rejection codes diverge: Go %s, Rust %s (want %s)",
			c.ID, c.Expected.ErrorCode, rustCode, c.Expected.ErrorCode))
	}
	return failures
}

// decodeRecord dispatches one record schema to its full typed record decoder
// and returns the record's re-encodeable value tree. The dispatch mirrors
// the payload.rs/validate_registered_payload table; the typed decode
// re-validates every cross constraint. core.portable-value-json@1 has no
// record-level decoder: the transported value is the record.
func decodeRecord(record string, value core.Value) (core.Value, error) {
	switch record {
	case "core.cli-output@1":
		message := &protocol.CliOutputMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.batch-plan@1":
		message := &protocol.BatchPlanMessage{}
		decoded, err := message.FromValueWithRegistryAndPatchLimits(
			value, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7), protocol.DefaultSourcePatchLimits())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.batch-result@1":
		message := &protocol.BatchResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.cancellation-request@1":
		message := &protocol.CancellationRequest{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.capability-declaration@1":
		message := &protocol.CapabilityDeclaration{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.change-set@1":
		message := &protocol.ChangeSetMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.completion@1":
		message := &protocol.Completion{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.diagnostic@1":
		message := &protocol.Diagnostic{}
		decoded, err := message.FromValue(value, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.error-code-registry@1":
		if err := protocol.ValidateErrorCodeManifestValue(value); err != nil {
			return nil, err
		}
		return value, nil
	case "core.execution-policy@1":
		message := &protocol.ExecutionPolicy{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.graph-projection-result@1":
		message := &protocol.GraphProjectionResultMessage{}
		decoded, err := message.FromValueWithRegistry(value, graph.DefaultPGCELimits(),
			protocol.DefaultErrorCodeRegistry())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.graph-provenance-map@1":
		message := &protocol.GraphProvenanceMapMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.graph-query-result@1":
		message := &protocol.GraphQueryResultMessage{}
		decoded, err := message.FromValueWithRegistry(value, graph.DefaultPGCELimits(),
			protocol.DefaultErrorCodeRegistry())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.ini-query-result@1":
		message := &protocol.IniQueryResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.java-properties-query-result@1":
		message := &protocol.JavaPropertiesQueryResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.java-utf16-string@1":
		message := &protocol.JavaUtf16String{}
		decoded, err := message.FromValue(value, protocol.DefaultProtocolLimits())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.materialization-request@2":
		message := &protocol.MaterializationRequestMessageV2{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.materialization-result@2":
		message := &protocol.MaterializationResultMessageV2{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.portable-graph@1":
		message := &protocol.PortableGraphMessage{}
		decoded, err := message.FromValue(value, graph.DefaultPGCELimits())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.portable-value-json@1":
		return value, nil
	case "core.profile-descriptor@1":
		message := &protocol.ProfileDescriptor{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.projection-report@1":
		message := &protocol.ProjectionReportMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.projection-request@1":
		message := &protocol.ProjectionRequestMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.projection-result@1":
		message := &protocol.ProjectionResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.provenance-map@1":
		message := &protocol.ProvenanceMapMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.query-definition@1":
		// The registry dispatch mirrors the payload.rs mapping: any
		// QueryFailure becomes KindInvalidValue at "$.payload" (the same
		// mapping as validate_registered_payload and the Go payload.go).
		definition := &protocol.QueryDefinition{}
		decoded, failure := definition.FromProtocolValue(value)
		if failure != nil {
			return nil, &protocol.ProtocolError{
				Kind:   protocol.KindInvalidValue,
				Path:   "$.payload",
				Detail: "invalid query definition: " + failure.Error(),
			}
		}
		encoded, encodeFailure := decoded.ToProtocolValue()
		if encodeFailure != nil {
			return nil, &protocol.ProtocolError{
				Kind:   protocol.KindInvalidValue,
				Path:   "$.payload",
				Detail: "invalid query definition: " + encodeFailure.Error(),
			}
		}
		return encoded, nil
	case "core.query-result@1":
		message := &protocol.QueryResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.registry-manifest@1":
		message := &protocol.RegistryManifest{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.source-encoding@1":
		message := &protocol.SourceEncodingMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue(), nil
	case "core.source-patch@2":
		message := &protocol.SourcePatchMessageV2{}
		decoded, err := message.FromValue(value, protocol.DefaultSourcePatchLimits())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.source-snapshot@2":
		message := &protocol.SourceSnapshotMessageV2{}
		decoded, err := message.FromValue(value, protocol.DefaultSourceLimits())
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	case "core.yaml-query-result@1":
		message := &protocol.YamlQueryResultMessage{}
		decoded, err := message.FromValue(value)
		if err != nil {
			return nil, err
		}
		return decoded.ToValue()
	}
	return nil, fmt.Errorf("record %s is not in the exchange inventory", record)
}

// protocolErrorCode extracts the registered code of a transport or record
// rejection. All rejections in this harness surface as *ProtocolError; any
// other RFC 0016 §6 coded error is reported by its Code().
func protocolErrorCode(err error) string {
	var protocolError *protocol.ProtocolError
	if errors.As(err, &protocolError) {
		return protocolError.Code()
	}
	type coded interface{ Code() string }
	if coded, ok := err.(coded); ok {
		return coded.Code()
	}
	return ""
}

// writeHex writes one hex-encoded byte file into the Go output directory.
func writeHex(dir, name string, bytes []byte, failures *[]string, id string) {
	text := hex.EncodeToString(bytes) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".hex"), []byte(text), 0o644); err != nil {
		*failures = append(*failures, fmt.Sprintf("case %s: cannot write %s: %v", id, name, err))
	}
}

// readHexFile reads one Rust byte file and decodes its hex.
func readHexFile(dir, name string, failures *[]string, id string) []byte {
	text, err := os.ReadFile(filepath.Join(dir, name+".hex"))
	if err != nil {
		*failures = append(*failures, fmt.Sprintf("case %s: missing Rust byte file %s.hex: %v (run scripts/go-verify-protocol-exchange.ps1)", id, name, err))
		return nil
	}
	decoded, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil {
		*failures = append(*failures, fmt.Sprintf("case %s: Rust byte file %s.hex is not valid hex: %v", id, name, err))
		return nil
	}
	return decoded
}

// readErrorFile reads the Rust side's recorded rejection code of one reject
// case.
func readErrorFile(dir, id string, failures *[]string, caseID string) string {
	text, err := os.ReadFile(filepath.Join(dir, id+".error.txt"))
	if err != nil {
		*failures = append(*failures, fmt.Sprintf("case %s: missing Rust rejection file: %v", caseID, err))
		return ""
	}
	return strings.TrimSpace(string(text))
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
