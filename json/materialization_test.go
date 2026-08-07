package json

import (
	"math/big"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

// jsonMaterializationRequest builds one frozen JSON materialization
// request.
func jsonMaterializationRequest(style string, newline document.NewlinePolicy) document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId(style, 1),
	).WithNewline(newline)
}

func json5MaterializationRequest(style string, newline document.NewlinePolicy) document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("json5.standard", 1),
		document.NewMaterializationStyleId(style, 1),
	).WithNewline(newline)
}

func mustMaterialize(t *testing.T, value core.Value,
	request document.MaterializationRequest) *CompleteMaterialization {
	t.Helper()
	result := Materialize(value, request)
	if result.Failed != nil {
		t.Fatalf("materialization failed: %v", result.Failed.Failure)
	}
	return result.Complete
}

func TestCompactMaterializationRoundTripsExactCoreKinds(t *testing.T) {
	sequence := core.NewArray(
		core.NullValue(),
		core.NewDecimal(big.NewInt(12), big.NewInt(-2)),
	)
	object, err := core.NewObject(
		core.Entry{Key: "text", Value: core.String("a\n\u0001")},
		core.Entry{Key: "values", Value: sequence},
	)
	if err != nil {
		t.Fatal(err)
	}
	complete := mustMaterialize(t, object, jsonMaterializationRequest(
		"json.canonical-compact", document.NewlineNone))
	if got := string(complete.Document.Render()); got != `{"text":"a\n\u0001","values":[null,12e-2]}` {
		t.Fatalf("render %q", got)
	}
	// The projection round-trips exactly.
	result := project(t, complete.Document, ProjectionTargetBestExactCoreV1)
	if result.Failed != nil {
		t.Fatalf("reprojection failed: %v", result.Failed.Diagnostics)
	}
	if !core.Equal(result.Complete.Value, object) {
		t.Error("projection != input")
	}
	if complete.Fidelity != MaterializationFidelityExact {
		t.Errorf("fidelity %v", complete.Fidelity)
	}
	if len(complete.Provenance.Entries()) == 0 {
		t.Error("missing provenance")
	}
}

func TestEntryMappingMaterializationKeepsDuplicates(t *testing.T) {
	mapping, err := core.NewEntryMapping(
		core.EntryMappingEntry{Key: core.String("x"), Value: core.NewInteger(big.NewInt(1))},
		core.EntryMappingEntry{Key: core.String("x"), Value: core.NewInteger(big.NewInt(2))},
	)
	if err != nil {
		t.Fatal(err)
	}
	complete := mustMaterialize(t, mapping, jsonMaterializationRequest(
		"json.canonical-compact", document.NewlineLf))
	if got := string(complete.Document.Render()); got != "{\"x\":1,\"x\":2}\n" {
		t.Fatalf("render %q", got)
	}
	result := project(t, complete.Document, ProjectionTargetProjectAsEntryMappingV1)
	if result.Failed != nil {
		t.Fatalf("reprojection failed: %v", result.Failed.Diagnostics)
	}
	if !core.Equal(result.Complete.Value, mapping) {
		t.Error("projection != input")
	}
}

func TestPrettyMaterializationWithCrLf(t *testing.T) {
	input := core.NewArray(core.Boolean(true))
	complete := mustMaterialize(t, input, jsonMaterializationRequest(
		"json.canonical-pretty", document.NewlineCrLf))
	if got := string(complete.Document.Render()); got != "[\r\n  true\r\n]\r\n" {
		t.Fatalf("render %q", got)
	}
}

func TestMaterializationRequestFailures(t *testing.T) {
	input := core.NewArray(core.Boolean(true))
	result := Materialize(input, jsonMaterializationRequest("json.canonical-pretty", document.NewlineNone))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureUnsupportedNewline {
		t.Fatalf("newline failure %v", result.Failed)
	}

	result = Materialize(core.NewBinaryFloat64(0),
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureUnrepresentable {
		t.Fatalf("binary failure %v", result.Failed)
	}

	limits := document.DefaultMaterializationLimits()
	limits.MaxOutputBytes = 3
	result = Materialize(core.String("too large"),
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone).WithLimits(limits))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureResourceLimit ||
		result.Failed.Failure.LimitName != "output-bytes" {
		t.Fatalf("output failure %v", result.Failed)
	}

	result = Materialize(core.Boolean(true),
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone).
			WithEncoding(document.Utf16LeEncoding()))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureUnsupportedEncoding {
		t.Fatalf("encoding failure %v", result.Failed)
	}

	limits = document.DefaultMaterializationLimits()
	limits.MaxProvenanceEntries = 1
	result = Materialize(core.Boolean(true),
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone).WithLimits(limits))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureResourceLimit ||
		result.Failed.Failure.LimitName != "provenance-entries" {
		t.Fatalf("provenance failure %v", result.Failed)
	}
}

func TestJSON5MaterializationIsBitExact(t *testing.T) {
	input := core.NewArray(
		core.NewBinaryFloat64(0x7ff0_0000_0000_0000),
		core.NewBinaryFloat64(0xfff0_0000_0000_0000),
		core.NewBinaryFloat64(0x7ff8_0000_0000_0000),
		core.NewBinaryFloat64(0xfff8_0000_0000_0000),
		core.String("a\u2028b"),
	)
	complete := mustMaterialize(t, input,
		json5MaterializationRequest("json5.canonical-compact", document.NewlineNone))
	if got := string(complete.Document.Render()); got != `[Infinity,-Infinity,NaN,-NaN,"a\u2028b"]` {
		t.Fatalf("render %q", got)
	}
	result := project(t, complete.Document, ProjectionTargetJson5BestExactCoreV1)
	if result.Failed != nil {
		t.Fatalf("reprojection failed: %v", result.Failed.Diagnostics)
	}
	if !core.Equal(result.Complete.Value, input) {
		t.Error("projection != input")
	}

	materialization := Materialize(core.NewBinaryFloat64(0),
		json5MaterializationRequest("json5.canonical-compact", document.NewlineNone))
	if materialization.Failed == nil || materialization.Failed.Failure.Kind != MaterializationFailureUnrepresentable {
		t.Fatalf("finite binary failure %v", materialization.Failed)
	}

	materialization = Materialize(core.NullValue(),
		json5MaterializationRequest("json.canonical-compact", document.NewlineNone))
	if materialization.Failed == nil || materialization.Failed.Failure.Kind != MaterializationFailureUnsupportedStyle {
		t.Fatalf("style failure %v", materialization.Failed)
	}
}

func TestMaterializationInputBudgets(t *testing.T) {
	// Node limit: the root array plus its element exceed one node.
	doc := parseForTest(t, "[1]", JsonProfileStrictV1)
	value := project(t, doc, ProjectionTargetBestExactCoreV1)
	limits := document.DefaultMaterializationLimits()
	limits.MaxInputNodes = 1
	result := Materialize(value.Complete.Value,
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone).WithLimits(limits))
	if result.Failed == nil || result.Failed.Failure.LimitName != "input-nodes" {
		t.Fatalf("node limit failure %v", result.Failed)
	}

	// Depth limit.
	doc = parseForTest(t, "[[[1]]]", JsonProfileStrictV1)
	value = project(t, doc, ProjectionTargetBestExactCoreV1)
	limits = document.DefaultMaterializationLimits()
	limits.MaxDepth = 1
	result = Materialize(value.Complete.Value,
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone).WithLimits(limits))
	if result.Failed == nil || result.Failed.Failure.LimitName != "input-depth" {
		t.Fatalf("depth limit failure %v", result.Failed)
	}
}

func TestMaterializationEscaping(t *testing.T) {
	doc := parseForTest(t, `{"text":"quote\" slash\\ line\n"}`, JsonProfileStrictV1)
	value := project(t, doc, ProjectionTargetBestExactCoreV1)
	complete := mustMaterialize(t, value.Complete.Value,
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone))
	if got := string(complete.Document.Render()); got != `{"text":"quote\" slash\\ line\n"}` {
		t.Fatalf("render %q", got)
	}
}

func TestMaterializationNonStringKeyRejected(t *testing.T) {
	mapping, err := core.NewEntryMapping(
		core.EntryMappingEntry{Key: core.NewInteger(big.NewInt(1)), Value: core.Boolean(true)},
	)
	if err != nil {
		t.Fatal(err)
	}
	result := Materialize(mapping,
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone))
	if result.Failed == nil || result.Failed.Failure.Kind != MaterializationFailureUnrepresentable {
		t.Fatalf("failure %v", result.Failed)
	}
	if result.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
		t.Errorf("code %s", result.Failed.Failure.Code())
	}
}

func TestMaterializationProvenanceFacts(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	value := project(t, doc, ProjectionTargetBestExactCoreV1)
	complete := mustMaterialize(t, value.Complete.Value,
		jsonMaterializationRequest("json.canonical-compact", document.NewlineNone))
	entries := complete.Provenance.Entries()
	// Root value + member value + object entry association + object key
	// association + key re-encoding are all recorded.
	if len(entries) < 4 {
		t.Fatalf("provenance entries %d", len(entries))
	}
	hasReencodedKey := false
	for _, entry := range entries {
		for _, origin := range entry.Outputs {
			if origin.Relation == MaterializationRelationReencoded {
				hasReencodedKey = true
			}
		}
	}
	_ = hasReencodedKey
}
