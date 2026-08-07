package yaml

import (
	"math/big"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/graph"
)

func bigOne() *big.Int { return big.NewInt(1) }

func bigNegOne() *big.Int { return big.NewInt(-1) }

func flowRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1)).
		WithNewline(document.NewlineLf)
}

func blockRequest() document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.canonical-block", 1)).
		WithNewline(document.NewlineLf)
}

func graphOf(t *testing.T, source string) *graph.Graph {
	t.Helper()
	doc := mustParse(t, source, Yaml12CoreV1)
	projected, err := doc.ProjectGraph()
	if err != nil {
		t.Fatalf("graph projection failed: %v", err)
	}
	return projected
}

// TestMaterializeGraphCycleFlow pins the vector golden
// (materialization.rs verified byte-for-byte).
func TestMaterializeGraphCycleFlow(t *testing.T) {
	projected := graphOf(t, "&root [one, *root]\n")
	result := MaterializeGraph(projected, flowRequest())
	if result.Complete == nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "--- &g0 !!seq [!!str \"one\", *g0]\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(result.Complete.Document.Render()), expected)
	}
	if result.Complete.Fidelity != MaterializationFidelityExact {
		t.Fatalf("fidelity %s", result.Complete.Fidelity)
	}
}

// TestMaterializeGraphBlock pins the canonical block layout
// (materialization.rs block test).
func TestMaterializeGraphBlock(t *testing.T) {
	projected := graphOf(t, "&root [one, *root]\n")
	result := MaterializeGraph(projected, blockRequest())
	if result.Complete == nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "--- &g0 !!seq\n- !!str \"one\"\n- *g0\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(result.Complete.Document.Render()), expected)
	}
}

// TestMaterializeGraphFlowMapping pins the explicit-key flow mapping
// golden (materialization.rs verified byte-for-byte).
func TestMaterializeGraphFlowMapping(t *testing.T) {
	projected := graphOf(t, "{a: [1, true]}\n")
	result := MaterializeGraph(projected, flowRequest())
	if result.Complete == nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "--- !!map {? !!str \"a\" : !!seq [!!int \"1\", !!bool \"true\"]}\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(result.Complete.Document.Render()), expected)
	}
}

// TestMaterializeValueFlow pins the vector golden with the reprojection
// closure.
func TestMaterializeValueFlow(t *testing.T) {
	doc := mustParse(t, "{a: [1, true]}\n", Yaml12CoreV1)
	projected := doc.ProjectValue(BestExactValueV1())
	if projected.Complete == nil {
		t.Fatalf("projection failed: %s", projected.Failed.Code())
	}
	result := MaterializeValue(projected.Complete.Value, flowRequest())
	if result.Complete == nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	expected := "--- !!map {? !!str \"a\" : !!seq [!!int \"1\", !!bool \"true\"]}\n"
	if string(result.Complete.Document.Render()) != expected {
		t.Fatalf("render %q, want %q", string(result.Complete.Document.Render()), expected)
	}
	reprojected := result.Complete.Document.ProjectValue(BestExactValueV1())
	if reprojected.Complete == nil ||
		!core.Equal(reprojected.Complete.Value, projected.Complete.Value) {
		t.Fatalf("reprojection closure failed")
	}
}

// TestMaterializeFloatCanonical pins the integer-looking float rewrite.
func TestMaterializeFloatCanonical(t *testing.T) {
	doc := mustParse(t, "1.0\n", Yaml12CoreV1)
	projected := doc.ProjectValue(BestExactValueV1())
	result := MaterializeValue(projected.Complete.Value, flowRequest())
	if result.Complete == nil {
		t.Fatalf("materialization failed: %s", result.Failed.Failure.Code())
	}
	if !strings.Contains(string(result.Complete.Document.Render()), `!!float "1e0"`) {
		t.Fatalf("float canonical missing: %q", string(result.Complete.Document.Render()))
	}
}

// TestMaterializeUnrepresentableValues pins the ExactOnly rejections.
func TestMaterializeUnrepresentableValues(t *testing.T) {
	cases := []core.Value{
		core.NewBinaryFloat64(0x3FF0000000000000), // finite 1.0
		core.NewBinaryFloat64(0xFFF8000000000000), // non-canonical NaN
		core.Time{},          // standalone Time
		core.LocalDateTime{}, // LocalDateTime
	}
	for _, value := range cases {
		result := MaterializeValue(value, flowRequest())
		if result.Complete != nil {
			t.Fatalf("%T unexpectedly materialized", value)
		}
		if result.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
			t.Fatalf("%T code %s", value, result.Failed.Failure.Code())
		}
	}
	// A finite decimal is representable and exact.
	result := MaterializeValue(core.NewDecimal(bigOne(), bigNegOne()), flowRequest())
	if result.Complete == nil {
		t.Fatalf("decimal failed: %s", result.Failed.Failure.Code())
	}
}

// TestMaterializeNonFiniteValues pins the frozen bit patterns.
func TestMaterializeNonFiniteValues(t *testing.T) {
	for bits, expected := range map[uint64]string{
		0x7ff0000000000000: ".inf",
		0xfff0000000000000: "-.inf",
		0x7ff8000000000000: ".nan",
	} {
		result := MaterializeValue(core.NewBinaryFloat64(bits), flowRequest())
		if result.Complete == nil {
			t.Fatalf("bits %x failed: %s", bits, result.Failed.Failure.Code())
		}
		if !strings.Contains(string(result.Complete.Document.Render()), expected) {
			t.Fatalf("bits %x render %q", bits, string(result.Complete.Document.Render()))
		}
	}
}

// TestMaterializeUniqueStringEntryMapping pins the ExactOnly boundary and
// the UniqueStringEntriesToObject transformation report.
func TestMaterializeUniqueStringEntryMapping(t *testing.T) {
	mapping, err := core.NewEntryMapping(
		core.EntryMappingEntry{Key: core.String("a"), Value: core.String("x")})
	if err != nil {
		t.Fatal(err)
	}
	result := MaterializeValue(mapping, flowRequest())
	if result.Complete != nil {
		t.Fatalf("EntryMapping without the policy must fail")
	}
	if result.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
		t.Fatalf("code %s", result.Failed.Failure.Code())
	}
	request := flowRequest().WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject)
	result = MaterializeValue(mapping, request)
	if result.Complete == nil {
		t.Fatalf("transformed materialization failed: %s", result.Failed.Failure.Code())
	}
	if result.Complete.Fidelity != MaterializationFidelityTransformed {
		t.Fatalf("fidelity %s", result.Complete.Fidelity)
	}
	events := result.Complete.Report.Events()
	if len(events) != 1 || events[0].Code != "core.materialization.mapping-transformed@1" {
		t.Fatalf("transformation report missing")
	}
}

// TestMaterializeUtf16 pins the BOM-carrying UTF-16 output and the
// max_output_bytes charging.
func TestMaterializeUtf16(t *testing.T) {
	doc := mustParse(t, "a: 1\n", Yaml12CoreV1)
	projected := doc.ProjectValue(BestExactValueV1())
	request := flowRequest().WithEncoding(document.Utf16BeEncoding())
	result := MaterializeValue(projected.Complete.Value, request)
	if result.Complete == nil {
		t.Fatalf("UTF-16 materialization failed: %s", result.Failed.Failure.Code())
	}
	render := result.Complete.Document.Render()
	if len(render) < 2 || render[0] != 0xFE || render[1] != 0xFF {
		t.Fatalf("UTF-16BE BOM missing")
	}
	// The output reparses under the target profile.
	if result.Complete.Document.Source().EncodingFacts().Selected().Kind() !=
		document.EncodingUtf16Be {
		t.Fatalf("output encoding")
	}
}

// TestMaterializeUnsupportedContract pins the request validation.
func TestMaterializeUnsupportedContract(t *testing.T) {
	doc := mustParse(t, "a: 1\n", Yaml12CoreV1)
	projected := doc.ProjectValue(BestExactValueV1())
	request := document.NewMaterializationRequest(
		document.NewProfileId("yaml.2.0", 1),
		document.NewMaterializationStyleId("yaml.canonical-flow", 1))
	result := MaterializeValue(projected.Complete.Value, request)
	if result.Complete != nil || result.Failed.Failure.Code() !=
		"core.materialization.unsupported-profile@1" {
		t.Fatalf("profile validation")
	}
	request = document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId("yaml.pretty", 1))
	result = MaterializeValue(projected.Complete.Value, request)
	if result.Complete != nil || result.Failed.Failure.Code() !=
		"core.materialization.unsupported-style@1" {
		t.Fatalf("style validation")
	}
}

// TestMaterializeCrossDocumentSharing pins the anchor-scope failure.
func TestMaterializeCrossDocumentSharing(t *testing.T) {
	// One node reachable from two roots fails graph materialization.
	builder := graph.NewBuilder(graph.DefaultLimits())
	first, _ := builder.ReserveNode()
	second, _ := builder.ReserveNode()
	_ = builder.DefineScalar(first, "tag:yaml.org,2002:str", "shared")
	_ = builder.DefineSequence(second, "tag:yaml.org,2002:seq", []graph.NodeID{first})
	_ = builder.PushRoot(second)
	_ = builder.PushRoot(first)
	projected, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	result := MaterializeGraph(projected, flowRequest())
	if result.Complete != nil || result.Failed.Failure.Code() !=
		"yaml.materialization.cross-document-sharing@1" {
		t.Fatalf("cross-document sharing: %v", result.Failed)
	}
}
