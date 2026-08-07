package yaml

import (
	"testing"

	"consema.dev/consema/core"
)

// TestProjectValueSharing pins the default rejection and the explicit
// duplication report (projection.rs tests).
func TestProjectValueSharing(t *testing.T) {
	doc := mustParse(t, "[&x {k: v}, *x]\n", Yaml12CoreV1)
	defaultResult := doc.ProjectValue(BestExactValueV1())
	if defaultResult.Failed == nil ||
		defaultResult.Failed.Code() != "yaml.projection.sharing@1" {
		t.Fatalf("default sharing: %v", defaultResult.Failed)
	}
	duplicated := doc.ProjectValue(BestExactValueV1().
		WithSharing(SharingPolicyDuplicateAcyclic))
	if duplicated.Complete == nil {
		t.Fatalf("duplication failed: %s", duplicated.Failed.Code())
	}
	if duplicated.Complete.Fidelity != FidelityTransformed {
		t.Fatalf("fidelity %s", duplicated.Complete.Fidelity)
	}
	events := duplicated.Complete.Report.Events()
	if len(events) != 3 {
		t.Fatalf("event count %d", len(events))
	}
	for _, event := range events {
		if event.Kind != ProjectionEventSharingDuplicated ||
			event.Policy != "DuplicateAcyclicSharing@1" {
			t.Fatalf("event %v", event)
		}
	}
}

// TestProjectValueCycle pins the cycle rejection even under duplication.
func TestProjectValueCycle(t *testing.T) {
	doc := mustParse(t, "&x [*x]\n", Yaml12CoreV1)
	result := doc.ProjectValue(BestExactValueV1().
		WithSharing(SharingPolicyDuplicateAcyclic))
	if result.Failed == nil || result.Failed.Code() != "yaml.projection.cycle@1" {
		t.Fatalf("cycle: %v", result.Failed)
	}
}

// TestProjectValueTagPolicy pins the strip behavior with the decoded
// content (projection.rs tests).
func TestProjectValueTagPolicy(t *testing.T) {
	doc := mustParse(t, "!example value\n", Yaml12CoreV1)
	defaultResult := doc.ProjectValue(BestExactValueV1())
	if defaultResult.Failed == nil ||
		defaultResult.Failed.Code() != "yaml.projection.unsupported-tag@1" {
		t.Fatalf("default tag: %v", defaultResult.Failed)
	}
	stripped := doc.ProjectValue(BestExactValueV1().WithTags(TagPolicyStripToNodeKind))
	if stripped.Complete == nil {
		t.Fatalf("strip failed")
	}
	if stripped.Complete.Fidelity != FidelityLossy {
		t.Fatalf("fidelity %s", stripped.Complete.Fidelity)
	}
	text, ok := stripped.Complete.Value.(core.String)
	if !ok || string(text) != "value" {
		t.Fatalf("stripped value %v", stripped.Complete.Value)
	}
	events := stripped.Complete.Report.Events()
	if len(events) != 1 || events[0].Kind != ProjectionEventTagStripped ||
		events[0].Policy != "StripToNodeKind@1" {
		t.Fatalf("strip events %v", events)
	}
}

// TestProjectValueMappingPolicy pins the Object/EntryMapping selection
// (projection.rs tests).
func TestProjectValueMappingPolicy(t *testing.T) {
	doc := mustParse(t, "{a: 1, a: 2}\n", Yaml12CoreV1)
	objectResult := doc.ProjectValue(BestExactValueV1().
		WithMapping(MappingPolicyRequireObject))
	if objectResult.Failed == nil ||
		objectResult.Failed.Code() != "yaml.projection.mapping-not-object@1" {
		t.Fatalf("object policy: %v", objectResult.Failed)
	}
	entriesResult := doc.ProjectValue(BestExactValueV1().
		WithMapping(MappingPolicyRequireEntryMapping))
	if entriesResult.Complete == nil {
		t.Fatalf("entry mapping failed")
	}
	mapping, ok := entriesResult.Complete.Value.(*core.EntryMapping)
	if !ok || mapping.Len() != 2 {
		t.Fatalf("entry mapping %v", entriesResult.Complete.Value)
	}
	// Best-exact selects EntryMapping for duplicate keys and Object for
	// unique string keys.
	bestExact := doc.ProjectValue(BestExactValueV1())
	if bestExact.Complete == nil {
		t.Fatalf("best-exact failed: %s", bestExact.Failed.Code())
	}
	if _, ok := bestExact.Complete.Value.(*core.EntryMapping); !ok {
		t.Fatalf("duplicate keys must project as EntryMapping")
	}
	unique := mustParse(t, "{a: 1, b: 2}\n", Yaml12CoreV1)
	uniqueResult := unique.ProjectValue(BestExactValueV1())
	if uniqueResult.Complete == nil {
		t.Fatalf("unique failed")
	}
	if _, ok := uniqueResult.Complete.Value.(*core.Object); !ok {
		t.Fatalf("unique string keys must project as Object")
	}
}

// TestProjectValueDocumentCardinality pins the exact-one-document rule.
func TestProjectValueDocumentCardinality(t *testing.T) {
	doc := mustParse(t, "---\na\n---\nb\n", Yaml12CoreV1)
	result := doc.ProjectValue(BestExactValueV1())
	if result.Failed == nil ||
		result.Failed.Code() != "yaml.projection.document-cardinality@1" {
		t.Fatalf("cardinality: %v", result.Failed)
	}
	empty := mustParse(t, "", Yaml12CoreV1)
	result = empty.ProjectValue(BestExactValueV1())
	if result.Failed == nil ||
		result.Failed.Code() != "yaml.projection.document-cardinality@1" {
		t.Fatalf("empty cardinality: %v", result.Failed)
	}
}

// TestProjectValueScalars pins the exact scalar lowering including the
// frozen non-finite bit patterns and the binary bytes.
func TestProjectValueScalars(t *testing.T) {
	doc := mustParse(t, "[.inf, -.inf, .nan, !!binary SGVsbG8=]\n", Yaml12CoreV1)
	result := doc.ProjectValue(BestExactValueV1())
	if result.Complete == nil {
		t.Fatalf("projection failed: %s", result.Failed.Code())
	}
	array, ok := result.Complete.Value.(*core.Array)
	if !ok || array.Len() != 4 {
		t.Fatalf("array %v", result.Complete.Value)
	}
	inf, ok := array.At(0).(core.BinaryFloat64)
	if !ok || uint64(inf) != 0x7ff0000000000000 {
		t.Fatalf(".inf bits")
	}
	negInf, ok := array.At(1).(core.BinaryFloat64)
	if !ok || uint64(negInf) != 0xfff0000000000000 {
		t.Fatalf("-.inf bits")
	}
	nan, ok := array.At(2).(core.BinaryFloat64)
	if !ok || uint64(nan) != 0x7ff8000000000000 {
		t.Fatalf(".nan bits")
	}
	bytes, ok := array.At(3).(core.Bytes)
	if !ok || string(bytes.Content()) != "Hello" {
		t.Fatalf("binary bytes %q", bytes.Content())
	}
}

// TestProjectValueTimestamp pins the timestamp lowering including the no-
// zone failure (projection.rs project_timestamp).
func TestProjectValueTimestamp(t *testing.T) {
	doc := mustParse(t, "!!timestamp 2001-12-15T02:59:43Z\n", Yaml11CompatV1)
	result := doc.ProjectValue(BestExactValueV1())
	if result.Complete == nil {
		t.Fatalf("projection failed: %s", result.Failed.Code())
	}
	value, ok := result.Complete.Value.(core.OffsetDateTime)
	if !ok || value.OffsetSeconds() != 0 {
		t.Fatalf("timestamp %v", result.Complete.Value)
	}
	// A zone-less timestamp follows the published 1.1 UTC rule and lowers
	// exactly with a zero offset.
	doc = mustParse(t, "!!timestamp 2001-12-15T02:59:43\n", Yaml11CompatV1)
	result = doc.ProjectValue(BestExactValueV1())
	if result.Complete == nil {
		t.Fatalf("zone-less timestamp failed: %s", result.Failed.Code())
	}
	value, ok = result.Complete.Value.(core.OffsetDateTime)
	if !ok || value.OffsetSeconds() != 0 {
		t.Fatalf("zone-less timestamp %v", result.Complete.Value)
	}
	// Leap seconds are not rounded.
	doc = mustParse(t, "!!timestamp 2001-12-15T02:59:60Z\n", Yaml11CompatV1)
	result = doc.ProjectValue(BestExactValueV1())
	if result.Failed == nil ||
		result.Failed.Code() != "yaml.projection.unrepresentable-timestamp@1" {
		t.Fatalf("leap second: %v", result.Failed)
	}
}

// TestProjectValueLimits pins the resource-limit failures.
func TestProjectValueLimits(t *testing.T) {
	doc := mustParse(t, "[one, two]\n", Yaml12CoreV1)
	limits := DefaultValueProjectionLimits()
	limits.MaxValueNodes = 1
	result := doc.ProjectValue(BestExactValueV1().WithLimits(limits))
	if result.Failed == nil ||
		result.Failed.Code() != "yaml.projection.resource-limit@1" ||
		result.Failed.LimitName != "max_value_nodes" {
		t.Fatalf("value-nodes limit: %v", result.Failed)
	}
	limits = DefaultValueProjectionLimits()
	limits.MaxAmplificationRatio = 0
	result = doc.ProjectValue(BestExactValueV1().WithLimits(limits))
	if result.Failed == nil ||
		result.Failed.LimitName != "max_amplification_ratio" {
		t.Fatalf("amplification limit: %v", result.Failed)
	}
}

// TestProjectGraphCustomTag pins the graph projection tag boundary.
func TestProjectGraphCustomTag(t *testing.T) {
	doc := mustParse(t, "!custom value\n", Yaml12CoreV1)
	_, err := doc.ProjectGraph()
	if err == nil {
		t.Fatalf("custom tag must fail graph projection")
	}
	if code := err.(interface{ Code() string }).Code(); code != "yaml.projection.unsupported-tag@1" {
		t.Fatalf("code %s", code)
	}
}

// TestProjectGraphProvenance pins the reference origins of alias edges.
func TestProjectGraphProvenance(t *testing.T) {
	doc := mustParse(t, "&root [one, *root]\n", Yaml12CoreV1)
	projected, failure := doc.ProjectGraphWithProvenance(BestExactGraphV1())
	if failure != nil {
		t.Fatalf("provenance failed: %s", failure.Code())
	}
	references := 0
	for _, entry := range projected.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == ProvenanceReference {
				references++
			}
		}
	}
	if references != 1 {
		t.Fatalf("reference origins %d", references)
	}
	// The graph identity is shared, never expanded.
	rootID := projected.Graph.Roots()[0]
	rootNode, _ := projected.Graph.Node(rootID)
	items, _ := rootNode.SequenceItems()
	if items[1] != rootID {
		t.Fatalf("alias edge must share the target identity")
	}
}

// TestProjectEmptyStreamGraph pins the zero-root empty graph.
func TestProjectEmptyStreamGraph(t *testing.T) {
	doc := mustParse(t, "", Yaml12CoreV1)
	projected, err := doc.ProjectGraph()
	if err != nil {
		t.Fatalf("empty graph projection failed: %v", err)
	}
	if projected.NodeCount() != 0 || len(projected.Roots()) != 0 {
		t.Fatalf("empty graph facts")
	}
}
