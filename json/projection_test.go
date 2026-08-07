package json

import (
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

func project(t *testing.T, doc *Document, target ProjectionTarget) ProjectionResult {
	t.Helper()
	request, failure := NewProjectionRequestBuilder(target).Build()
	if failure != nil {
		t.Fatalf("build: %v", failure)
	}
	return doc.Project(request)
}

func TestProjectionBestExactUsesEntryMappingForDuplicates(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2}`, JsonProfileStrictV1)
	result := project(t, doc, ProjectionTargetBestExactCoreV1)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	complete := result.Complete
	if complete.Value.Kind() != core.KindEntryMapping {
		t.Fatalf("kind %v", complete.Value.Kind())
	}
	if complete.Fidelity != FidelityTransformed {
		t.Errorf("fidelity %v", complete.Fidelity)
	}
	associations := 0
	for _, entry := range complete.Provenance.Entries() {
		if entry.Projected.IsAssociation {
			associations++
		}
	}
	if associations != 2 {
		t.Errorf("association origins %d", associations)
	}
}

func TestProjectionObjectRejectDuplicates(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2}`, JsonProfileStrictV1)
	request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := doc.Project(request)
	if result.Complete != nil {
		t.Fatal("projection must fail under Reject")
	}
	if result.Failed == nil || len(result.Failed.Diagnostics) == 0 {
		t.Fatal("missing failure diagnostics")
	}
	if result.Failed.Diagnostics[0].Code != "json.projection.duplicate-keys@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestProjectionObjectLastWins(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2}`, JsonProfileStrictV1)
	request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		GlobalDuplicatePolicy(DuplicateKeyPolicyLastWins).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := doc.Project(request)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	if result.Complete.Fidelity != FidelityLossy {
		t.Errorf("fidelity %v", result.Complete.Fidelity)
	}
	hasEvent := false
	for _, event := range result.Complete.Report.Events() {
		if event.Kind == ProjectionEventDuplicateCollapsed {
			hasEvent = true
		}
	}
	if !hasEvent {
		t.Error("missing DuplicateCollapsed event")
	}
	object, ok := result.Complete.Value.(*core.Object)
	if !ok || object.Len() != 1 {
		t.Fatalf("value %v", result.Complete.Value)
	}
	value, _ := object.Get("a")
	if integer, ok := value.(core.Integer); !ok || integer.String() != "2" {
		t.Errorf("last-wins value %v", value)
	}
}

func TestProjectionObjectKeyProvenance(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"b":2}`, JsonProfileStrictV1)
	result := project(t, doc, ProjectionTargetProjectAsObjectV1)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	keys := 0
	entries := 0
	for _, entry := range result.Complete.Provenance.Entries() {
		if !entry.Projected.IsAssociation {
			continue
		}
		switch entry.Projected.Association.Role() {
		case protocol.AssociationRoleObjectKey:
			keys++
		case protocol.AssociationRoleObjectEntry:
			entries++
		}
	}
	if keys != 2 || entries != 2 {
		t.Errorf("key origins %d, entry origins %d", keys, entries)
	}
}

func TestProjectionExactNodePolicyOverridesGlobal(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2}`, JsonProfileStrictV1)
	request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		ExactNodeDuplicatePolicy(doc.Root().NodeRef(), DuplicateKeyPolicyFirstWins).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := doc.Project(request)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	if result.Complete.Fidelity != FidelityLossy {
		t.Errorf("fidelity %v", result.Complete.Fidelity)
	}
	// An exact-node policy on a scalar root is rejected.
	scalar := parseForTest(t, "1", JsonProfileStrictV1)
	request, failure = NewProjectionRequestBuilder(ProjectionTargetBestExactCoreV1).
		ExactNodeDuplicatePolicy(scalar.Root().NodeRef(), DuplicateKeyPolicyFirstWins).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result = scalar.Project(request)
	if result.Complete != nil {
		t.Fatal("scalar policy target must fail")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.invalid-policy-target@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestProjectionTargetNotApplicable(t *testing.T) {
	// BestExactCore on a JSON5 document is rejected.
	json5 := parseForTest(t, "{a:1}", JsonProfileJson5StandardV1)
	result := project(t, json5, ProjectionTargetBestExactCoreV1)
	if result.Complete != nil {
		t.Fatal("old target must fail on JSON5")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.target-not-applicable@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
	// Json5BestExactCore on a strict document is rejected.
	strict := parseForTest(t, "null", JsonProfileStrictV1)
	result = project(t, strict, ProjectionTargetJson5BestExactCoreV1)
	if result.Complete != nil {
		t.Fatal("json5 target must fail on strict")
	}
	// ProjectAsObject on a scalar root is rejected.
	result = project(t, strict, ProjectionTargetProjectAsObjectV1)
	if result.Complete != nil {
		t.Fatal("object target must fail on a scalar root")
	}
}

func TestProjectionJSON5DuplicatesNonFinite(t *testing.T) {
	json5 := parseForTest(t, "{a:Infinity,a:-NaN}", JsonProfileJson5StandardV1)
	result := project(t, json5, ProjectionTargetJson5BestExactCoreV1)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	mapping, ok := result.Complete.Value.(*core.EntryMapping)
	if !ok || mapping.Len() != 2 {
		t.Fatalf("value %v", result.Complete.Value)
	}
	entries := mapping.Entries()
	if entries[0].Value.(core.BinaryFloat64).Bits() != 0x7ff0_0000_0000_0000 {
		t.Errorf("entry 0 bits %x", entries[0].Value.(core.BinaryFloat64).Bits())
	}
	if entries[1].Value.(core.BinaryFloat64).Bits() != 0xfff8_0000_0000_0000 {
		t.Errorf("entry 1 bits %x", entries[1].Value.(core.BinaryFloat64).Bits())
	}
}

func TestProjectionExactScalars(t *testing.T) {
	doc := parseForTest(t, `{"n":null,"b":true,"i":123,"d":1.25,"s":"x","a":[1],"o":{"k":"v"}}`,
		JsonProfileStrictV1)
	result := project(t, doc, ProjectionTargetBestExactCoreV1)
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
	}
	if result.Complete.Fidelity != FidelityExact {
		t.Errorf("fidelity %v", result.Complete.Fidelity)
	}
	object, ok := result.Complete.Value.(*core.Object)
	if !ok || object.Len() != 7 {
		t.Fatalf("value %v", result.Complete.Value)
	}
	decimal, _ := object.Get("d")
	if value, ok := decimal.(core.Decimal); !ok || value.Coefficient().String() != "125" ||
		value.Exponent().String() != "-2" {
		t.Errorf("decimal %v", decimal)
	}
}

func TestProjectionConflictingPolicyRules(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	// Two exact-node rules for the same node with different policies
	// conflict at build time.
	builder := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		ExactNodeDuplicatePolicy(doc.Root().NodeRef(), DuplicateKeyPolicyFirstWins).
		ExactNodeDuplicatePolicy(doc.Root().NodeRef(), DuplicateKeyPolicyLastWins)
	_, failure := builder.Build()
	if failure == nil || failure.Kind != ProjectionFailureConflictingPolicyRules {
		t.Fatalf("failure %v", failure)
	}
	// Sequential global policies replace, so no conflict arises.
	builder = NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		GlobalDuplicatePolicy(DuplicateKeyPolicyFirstWins).
		GlobalDuplicatePolicy(DuplicateKeyPolicyLastWins)
	if _, failure := builder.Build(); failure != nil {
		t.Fatalf("replacement must not conflict: %v", failure)
	}
}

func TestProjectionWrongSnapshotPolicy(t *testing.T) {
	first := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	second := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		ExactNodeDuplicatePolicy(first.Root().NodeRef(), DuplicateKeyPolicyFirstWins).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := second.Project(request)
	if result.Complete != nil {
		t.Fatal("foreign policy node must fail")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.wrong-snapshot-policy@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestProjectionRecoveredDocumentRejected(t *testing.T) {
	for _, profile := range []JsonProfile{JsonProfileStrictV1, JsonProfileJsoncBoundedV1,
		JsonProfileJson5StandardV1} {
		doc := parseForTest(t, `{"a"1,...}`, profile)
		mustForm(t, doc, document.FormationStatusRecovered)
		target := ProjectionTargetBestExactCoreV1
		if profile == JsonProfileJson5StandardV1 {
			target = ProjectionTargetJson5BestExactCoreV1
		}
		result := project(t, doc, target)
		if result.Complete != nil {
			t.Fatalf("%v: recovered document accepted by projection", profile)
		}
		if result.Failed.Diagnostics[0].Code != "json.projection.incomplete-document@1" {
			t.Errorf("%v: code %s", profile, result.Failed.Diagnostics[0].Code)
		}
		// The same member content without recovery trauma stays operational.
		complete := parseForTest(t, `{"a":1}`, profile)
		result = project(t, complete, target)
		if result.Failed != nil {
			t.Fatalf("%v: complete projection failed: %v", profile, result.Failed.Diagnostics)
		}
	}
}

func TestProjectionKeyProvenanceOfRetainedOccurrence(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"a":2}`, JsonProfileStrictV1)
	for _, policy := range []DuplicateKeyPolicy{DuplicateKeyPolicyFirstWins, DuplicateKeyPolicyLastWins} {
		request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
			GlobalDuplicatePolicy(policy).Build()
		if failure != nil {
			t.Fatal(failure)
		}
		result := doc.Project(request)
		if result.Failed != nil {
			t.Fatalf("projection failed: %v", result.Failed.Diagnostics)
		}
		keys := 0
		for _, entry := range result.Complete.Provenance.Entries() {
			if entry.Projected.IsAssociation &&
				entry.Projected.Association.Role() == protocol.AssociationRoleObjectKey {
				keys++
			}
		}
		if keys != 1 {
			t.Errorf("policy %v: key origins %d", policy, keys)
		}
	}
}

func TestProjectionProvenanceLimitFailsWholeProjection(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"b":2,"c":3}`, JsonProfileStrictV1)
	limits := DefaultProjectionLimits()
	limits.MaxProvenanceEntries = 6
	request, failure := NewProjectionRequestBuilder(ProjectionTargetProjectAsObjectV1).
		Limits(limits).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := doc.Project(request)
	if result.Complete != nil {
		t.Fatal("provenance limit must fail the whole projection")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestProjectionDepthLimit(t *testing.T) {
	doc := parseForTest(t, "[[[1]]]", JsonProfileStrictV1)
	limits := DefaultProjectionLimits()
	limits.MaxDepth = 2
	request, failure := NewProjectionRequestBuilder(ProjectionTargetBestExactCoreV1).
		Limits(limits).Build()
	if failure != nil {
		t.Fatal(failure)
	}
	result := doc.Project(request)
	if result.Complete != nil {
		t.Fatal("depth limit must fail the projection")
	}
	if result.Failed.Diagnostics[0].Code != "core.projection.resource-limit@1" {
		t.Errorf("code %s", result.Failed.Diagnostics[0].Code)
	}
}
