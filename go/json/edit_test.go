package json

import (
	"math/big"
	"strings"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

func objectMembers(t *testing.T, doc *Document) []JsonObjectMember {
	t.Helper()
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("members unavailable")
	}
	return members.Value()
}

func arrayElements(t *testing.T, doc *Document) []JsonArrayElement {
	t.Helper()
	elements := doc.Root().ArrayElements()
	if !elements.IsAvailable() {
		t.Fatal("elements unavailable")
	}
	return elements.Value()
}

func commit(t *testing.T, doc *Document, tx *EditTransaction) *EditCommit {
	t.Helper()
	commit, failure := doc.Commit(tx)
	if failure != nil {
		t.Fatalf("commit failed: %v", failure)
	}
	return commit
}

func mustFail(t *testing.T, doc *Document, tx *EditTransaction, kind EditFailureKind) *EditFailure {
	t.Helper()
	_, failure := doc.Commit(tx)
	if failure == nil || failure.Kind != kind {
		t.Fatalf("failure %v, want %v", failure, kind)
	}
	return failure
}

func TestSemanticEditChangesOnlyLiteralAndKeepsTrivia(t *testing.T) {
	doc := parseForTest(t, "{ /* lead */ \"a\" : 1 // tail\n}", JsonProfileJsoncBoundedV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewInteger(big.NewInt(200)), RepresentationPolicyPreserveCompatible)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != "{ /* lead */ \"a\" : 200 // tail\n}" {
		t.Fatalf("render %q", got)
	}
	if len(result.ChangeSet.SourceEdits()) != 1 {
		t.Fatalf("source edits %d", len(result.ChangeSet.SourceEdits()))
	}
	if result.Document.SnapshotIdentity() == doc.SnapshotIdentity() {
		t.Error("snapshot identity must change")
	}
}

func TestObjectStructuralEditsPreserveTrivia(t *testing.T) {
	source := `{ /*lead*/ "a":1, /*keep*/ "a":2, "z":3, }`
	doc := parseForTest(t, source, JsonProfileJsoncBoundedV1)
	members := objectMembers(t, doc)

	insert := NewEditTransactionBuilder(doc)
	insert.InsertMember(doc.Root().NodeRef(), "x",
		core.NewArray(core.Boolean(true)), BeforeAnchor(members[1].NodeRef()))
	inserted := commit(t, doc, insert.Build())
	if got := string(inserted.Document.Render()); got != `{ /*lead*/ "a":1, /*keep*/ "x":[true],"a":2, "z":3, }` {
		t.Fatalf("insert render %q", got)
	}

	rename := NewEditTransactionBuilder(doc)
	rename.RenameMember(members[1].NodeRef(), "b")
	renamed := commit(t, doc, rename.Build())
	if got := string(renamed.Document.Render()); got != `{ /*lead*/ "a":1, /*keep*/ "b":2, "z":3, }` {
		t.Fatalf("rename render %q", got)
	}

	remove := NewEditTransactionBuilder(doc)
	remove.RemoveMember(members[0].NodeRef())
	removed := commit(t, doc, remove.Build())
	if got := string(removed.Document.Render()); got != `{ /*lead*/  /*keep*/ "a":2, "z":3, }` {
		t.Fatalf("remove render %q", got)
	}
}

func TestArrayInsertAndRemove(t *testing.T) {
	empty := parseForTest(t, "[ /*inside*/ ]", JsonProfileJsoncBoundedV1)
	atStart := NewEditTransactionBuilder(empty)
	atStart.InsertArrayElement(empty.Root().NodeRef(),
		core.NewInteger(big.NewInt(1)), PlacementAtStart())
	if got := string(commit(t, empty, atStart.Build()).Document.Render()); got != "[1 /*inside*/ ]" {
		t.Fatalf("start render %q", got)
	}
	atEnd := NewEditTransactionBuilder(empty)
	atEnd.InsertArrayElement(empty.Root().NodeRef(),
		core.NewInteger(big.NewInt(1)), PlacementAtEnd())
	if got := string(commit(t, empty, atEnd.Build()).Document.Render()); got != "[ /*inside*/ 1]" {
		t.Fatalf("end render %q", got)
	}

	doc := parseForTest(t, "[1, /*keep*/ 2, 3,]", JsonProfileJsoncBoundedV1)
	elements := arrayElements(t, doc)
	insert := NewEditTransactionBuilder(doc)
	insert.InsertArrayElement(doc.Root().NodeRef(), core.String("end"),
		AfterAnchor(elements[2].NodeRef()))
	if got := string(commit(t, doc, insert.Build()).Document.Render()); got != "[1, /*keep*/ 2, 3,\"end\",]" {
		t.Fatalf("after render %q", got)
	}
	remove := NewEditTransactionBuilder(doc)
	remove.RemoveArrayElement(elements[1].NodeRef())
	if got := string(commit(t, doc, remove.Build()).Document.Render()); got != "[1, /*keep*/  3,]" {
		t.Fatalf("remove render %q", got)
	}
}

func TestStructuralConflictsFailAtomically(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"b":2}`, JsonProfileStrictV1)
	members := objectMembers(t, doc)

	removedAnchor := NewEditTransactionBuilder(doc)
	removedAnchor.RemoveMember(members[0].NodeRef()).
		InsertMember(doc.Root().NodeRef(), "x", core.Boolean(true),
			BeforeAnchor(members[0].NodeRef()))
	mustFail(t, doc, removedAnchor.Build(), EditFailurePlacementAnchorRemoved)

	duplicate := NewEditTransactionBuilder(doc)
	duplicate.RenameMember(members[0].NodeRef(), "x").
		RemoveMember(members[0].NodeRef())
	mustFail(t, doc, duplicate.Build(), EditFailureDuplicateTarget)

	sameBoundary := NewEditTransactionBuilder(doc)
	sameBoundary.InsertMember(doc.Root().NodeRef(), "x", core.Boolean(true), PlacementAtEnd()).
		InsertMember(doc.Root().NodeRef(), "y", core.Boolean(false), PlacementAtEnd())
	mustFail(t, doc, sameBoundary.Build(), EditFailureOverlappingOwnership)

	ancestorDescendant := NewEditTransactionBuilder(doc)
	ancestorDescendant.SemanticScalar(members[0].ValueNodeRef(),
		core.NewInteger(big.NewInt(3)), RepresentationPolicyPreserveCompatible).
		RemoveMember(members[0].NodeRef())
	mustFail(t, doc, ancestorDescendant.Build(), EditFailureAncestorDescendantConflict)

	if got := string(doc.Render()); got != `{"a":1,"b":2}` {
		t.Fatalf("base changed: %q", got)
	}
}

func TestMoveMemberKeepsTriviaAndPublishesArtifacts(t *testing.T) {
	source := "{ /*before-a*/ a:1, /*between*/ b:2, c:3, }"
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	members := objectMembers(t, doc)
	builder := NewEditTransactionBuilder(doc)
	builder.MoveMember(members[1].NodeRef(), PlacementAtStart())
	tx := builder.Build()
	plan, failure := doc.DryRun(tx, "config.json5")
	if failure != nil {
		t.Fatalf("dry run: %v", failure)
	}
	result := commit(t, doc, tx)
	if got := string(result.Document.Render()); got != "{ /*before-a*/ b:2,a:1, /*between*/  c:3, }" {
		t.Fatalf("render %q", got)
	}
	if !sameReplacements(plan.Replacements(), result.SourcePatch.Replacements()) {
		t.Error("plan and commit replacements differ")
	}
	if plan.TargetDigest() != result.SourcePatch.TargetDigest() {
		t.Error("plan and commit target digests differ")
	}
	if failure := result.UntouchedProof.Verify(doc.Source(), result.Document.Source(),
		result.SourcePatch.Replacements()); failure != nil {
		t.Errorf("proof verify: %v", failure)
	}
	hasUnmapped := false
	for _, mapping := range result.ChangeSet.NodeMappings() {
		if mapping.Old == members[1].NodeRef() && mapping.Status == protocol.MappingUnmapped &&
			mapping.Reason != nil && *mapping.Reason == "member-reparsed-after-move" {
			hasUnmapped = true
		}
	}
	if !hasUnmapped {
		t.Error("missing member-reparsed-after-move mapping")
	}

	toEnd := NewEditTransactionBuilder(doc)
	toEnd.MoveMember(members[0].NodeRef(), PlacementAtEnd())
	if got := string(commit(t, doc, toEnd.Build()).Document.Render()); got != "{ /*before-a*/  /*between*/ b:2, c:3,a:1, }" {
		t.Fatalf("to-end render %q", got)
	}

	noOp := NewEditTransactionBuilder(doc)
	noOp.MoveMember(members[1].NodeRef(), BeforeAnchor(members[2].NodeRef()))
	noOpCommit := commit(t, doc, noOp.Build())
	if string(noOpCommit.Document.Render()) != source {
		t.Error("no-op move changed the document")
	}
	if len(noOpCommit.ChangeSet.SourceEdits()) != 0 {
		t.Error("no-op move produced source edits")
	}
}

func sameReplacements(left, right []document.SourceReplacement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].OldStart() != right[index].OldStart() ||
			left[index].OldEnd() != right[index].OldEnd() ||
			string(left[index].Original()) != string(right[index].Original()) ||
			string(left[index].Replacement()) != string(right[index].Replacement()) {
			return false
		}
	}
	return true
}

func valueMembers(t *testing.T, value JsonValue) []JsonObjectMember {
	t.Helper()
	members := value.ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("members unavailable")
	}
	return members.Value()
}

func TestMoveMemberRejectsCrossObjectAndSelf(t *testing.T) {
	doc := parseForTest(t, `{"left":{"a":1,"x":2},"right":{"b":3}}`, JsonProfileStrictV1)
	roots := objectMembers(t, doc)
	left := valueMembers(t, roots[0].Value())
	right := valueMembers(t, roots[1].Value())

	cross := NewEditTransactionBuilder(doc)
	cross.MoveMember(left[0].NodeRef(), BeforeAnchor(right[0].NodeRef()))
	mustFail(t, doc, cross.Build(), EditFailureTargetNotFound)

	selfAnchor := NewEditTransactionBuilder(doc)
	selfAnchor.MoveMember(left[0].NodeRef(), AfterAnchor(left[0].NodeRef()))
	mustFail(t, doc, selfAnchor.Build(), EditFailurePlacementAnchorModified)

	anchorRenamed := NewEditTransactionBuilder(doc)
	anchorRenamed.MoveMember(left[0].NodeRef(), AfterAnchor(left[1].NodeRef())).
		RenameMember(left[1].NodeRef(), "renamed")
	mustFail(t, doc, anchorRenamed.Build(), EditFailurePlacementAnchorModified)

	descendant := NewEditTransactionBuilder(doc)
	descendant.MoveMember(left[0].NodeRef(), PlacementAtEnd()).
		SemanticScalar(left[0].ValueNodeRef(), core.NewInteger(big.NewInt(9)),
			RepresentationPolicyPreserveCompatible)
	mustFail(t, doc, descendant.Build(), EditFailureAncestorDescendantConflict)

	if got := string(doc.Render()); got != `{"left":{"a":1,"x":2},"right":{"b":3}}` {
		t.Fatalf("base changed: %q", got)
	}
}

func TestDryRunAndCommitHaveIdenticalArtifacts(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	builder := NewEditTransactionBuilder(doc)
	builder.InsertMember(doc.Root().NodeRef(), "secret-name", core.String("secret-value"),
		PlacementAtEnd())
	tx := builder.Build()
	plan, failure := doc.DryRun(tx, "config.json")
	if failure != nil {
		t.Fatalf("dry run: %v", failure)
	}
	result := commit(t, doc, tx)
	if got := string(result.Document.Render()); got != `{"a":1,"secret-name":"secret-value"}` {
		t.Fatalf("render %q", got)
	}
	if !sameReplacements(plan.Replacements(), result.SourcePatch.Replacements()) {
		t.Error("plan and commit replacements differ")
	}
	if plan.TargetDigest() != result.SourcePatch.TargetDigest() {
		t.Error("plan and commit target digests differ")
	}
	if len(plan.Operations()) != 1 {
		t.Fatalf("plan operations %d", len(plan.Operations()))
	}
	for _, operation := range plan.Operations() {
		for _, value := range operation.Summary {
			if strings.Contains(value, "secret") {
				t.Errorf("summary leaks secret: %q", value)
			}
		}
	}
	redacted, err := plan.WithAllReplacementsRedacted(true, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(redacted.DebugString(), "secret") {
		t.Error("redacted debug leaks secret")
	}
	if failure := result.UntouchedProof.Verify(doc.Source(), result.Document.Source(),
		result.SourcePatch.Replacements()); failure != nil {
		t.Errorf("proof verify: %v", failure)
	}
}

func TestWrongSnapshotRejectedAtomically(t *testing.T) {
	first := parseForTest(t, "1", JsonProfileStrictV1)
	second := parseForTest(t, "2", JsonProfileStrictV1)
	builder := NewEditTransactionBuilder(second)
	builder.LiteralScalar(first.Root().NodeRef(), []byte("3"))
	mustFail(t, second, builder.Build(), EditFailureWrongSnapshot)
	if got := string(second.Render()); got != "2" {
		t.Fatalf("second changed: %q", got)
	}
}

func TestObjectKeyReplacementMustRemainString(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.LiteralScalar(member.KeyNodeRef(), []byte("2"))
	mustFail(t, doc, builder.Build(), EditFailureInvalidLiteral)
}

func TestPreserveCompatibleKeepsDecimalFractionScale(t *testing.T) {
	doc := parseForTest(t, `{"a": 1.00}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewDecimal(big.NewInt(25), big.NewInt(-1)), RepresentationPolicyPreserveCompatible)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != `{"a": 2.50}` {
		t.Fatalf("render %q", got)
	}
}

func TestPreserveCompatibleKeepsExponentMarkerAndSign(t *testing.T) {
	doc := parseForTest(t, `{"a": 1E+02}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewInteger(big.NewInt(2)), RepresentationPolicyPreserveCompatible)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != `{"a": 2E+0}` {
		t.Fatalf("render %q", got)
	}
}

func TestPreserveCompatibleRejectsUnrepresentableFractionScale(t *testing.T) {
	doc := parseForTest(t, `{"a": 1.000}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewDecimal(big.NewInt(1), big.NewInt(-4)), RepresentationPolicyPreserveCompatible)
	mustFail(t, doc, builder.Build(), EditFailureRepresentationIncompatible)
}

func TestPreserveCompatibleKeepsStringEscapeStyle(t *testing.T) {
	doc := parseForTest(t, `{"a": "aA"}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(), core.String("xA"),
		RepresentationPolicyPreserveCompatible)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != `{"a": "xA"}` {
		t.Fatalf("render %q", got)
	}
}

func TestCanonicalForProfileIsIndependentOfOldSpelling(t *testing.T) {
	doc := parseForTest(t, `{"a": 1.00}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewDecimal(big.NewInt(25), big.NewInt(-1)), RepresentationPolicyCanonicalForProfile)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != `{"a": 25e-1}` {
		t.Fatalf("render %q", got)
	}
}

func TestPreserveElseCanonicalReportsActualFallback(t *testing.T) {
	doc := parseForTest(t, `{"a": 1.000}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewDecimal(big.NewInt(1), big.NewInt(-4)), RepresentationPolicyPreserveElseCanonical)
	result := commit(t, doc, builder.Build())
	if got := string(result.Document.Render()); got != `{"a": 1e-4}` {
		t.Fatalf("render %q", got)
	}
	fallbacks := 0
	for _, diagnostic := range result.ChangeSet.Diagnostics() {
		if diagnostic.Code == "json.edit.representation-fallback@1" {
			fallbacks++
		}
	}
	if fallbacks != 1 {
		t.Errorf("fallback diagnostics %d", fallbacks)
	}
}

func TestPreserveCompatibleRejectsCategoryChange(t *testing.T) {
	doc := parseForTest(t, `{"a": 1}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(),
		core.NewDecimal(big.NewInt(1), big.NewInt(0)), RepresentationPolicyPreserveCompatible)
	mustFail(t, doc, builder.Build(), EditFailureRepresentationIncompatible)
}

func TestJSON5ScalarEditsPreserveLexicalCategories(t *testing.T) {
	source := `{hex:+0X0f,lead:+.50,trail:1.,exp:1.0E+2,single:'a\x20\v\0\q',nf:+Infinity,}`
	doc := parseForTest(t, source, JsonProfileJson5StandardV1)
	members := objectMembers(t, doc)
	builder := NewEditTransactionBuilder(doc)
	builder.
		SemanticScalar(members[0].ValueNodeRef(), core.NewInteger(big.NewInt(16)),
			RepresentationPolicyPreserveCompatible).
		SemanticScalar(members[1].ValueNodeRef(),
			core.NewDecimal(big.NewInt(75), big.NewInt(-2)), RepresentationPolicyPreserveCompatible).
		SemanticScalar(members[2].ValueNodeRef(), core.NewInteger(big.NewInt(2)),
			RepresentationPolicyPreserveCompatible).
		SemanticScalar(members[3].ValueNodeRef(),
			core.NewDecimal(big.NewInt(34), big.NewInt(-1)), RepresentationPolicyPreserveCompatible).
		SemanticScalar(members[4].ValueNodeRef(), core.String("a \v\x00q"),
			RepresentationPolicyPreserveCompatible).
		SemanticScalar(members[5].ValueNodeRef(),
			core.NewBinaryFloat64(0x7ff8_0000_0000_0000), RepresentationPolicyPreserveCompatible)
	commit := commit(t, doc, builder.Build())
	expected := `{hex:+0X10,lead:+.75,trail:2.,exp:3.4E+0,single:'a\x20\v\0\q',nf:+NaN,}`
	if got := string(commit.Document.Render()); got != expected {
		t.Fatalf("render %q != %q", got, expected)
	}
}

func TestJSON5StructuralEditsQuoteNamesAndKeepNonFinite(t *testing.T) {
	doc := parseForTest(t, "{a:1, /*keep*/ b:2,}", JsonProfileJson5StandardV1)
	members := objectMembers(t, doc)
	insert := NewEditTransactionBuilder(doc)
	insert.InsertMember(doc.Root().NodeRef(), "x\"",
		core.NewBinaryFloat64(0x7ff0_0000_0000_0000), BeforeAnchor(members[1].NodeRef()))
	if got := string(commit(t, doc, insert.Build()).Document.Render()); got != `{a:1, /*keep*/ "x\"":Infinity,b:2,}` {
		t.Fatalf("insert render %q", got)
	}

	rename := NewEditTransactionBuilder(doc)
	rename.RenameMember(members[0].NodeRef(), "renamed")
	if got := string(commit(t, doc, rename.Build()).Document.Render()); got != `{"renamed":1, /*keep*/ b:2,}` {
		t.Fatalf("rename render %q", got)
	}

	strict := parseForTest(t, "0", JsonProfileStrictV1)
	unsupported := NewEditTransactionBuilder(strict)
	unsupported.SemanticScalar(strict.Root().NodeRef(),
		core.NewBinaryFloat64(0x7ff0_0000_0000_0000), RepresentationPolicyCanonicalForProfile)
	mustFail(t, strict, unsupported.Build(), EditFailureUnsupportedSemanticValue)
}

func TestRecoveredDocumentsRejectedAtCommit(t *testing.T) {
	for _, profile := range []JsonProfile{JsonProfileStrictV1, JsonProfileJsoncBoundedV1,
		JsonProfileJson5StandardV1} {
		doc := parseForTest(t, `{"a`, profile)
		mustForm(t, doc, document.FormationStatusRecovered)
		builder := NewEditTransactionBuilder(doc)
		builder.InsertMember(doc.Root().NodeRef(), "fuzz", core.Boolean(true), PlacementAtEnd())
		failure := mustFail(t, doc, builder.Build(), EditFailureRecoveredDocument)
		if failure.Code() != "core.edit.incomplete-target@1" {
			t.Errorf("%v: code %s", profile, failure.Code())
		}
		mustFail(t, doc, NewEditTransactionBuilder(doc).Build(), EditFailureRecoveredDocument)
	}
	complete := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	builder := NewEditTransactionBuilder(complete)
	builder.InsertMember(complete.Root().NodeRef(), "fuzz", core.Boolean(true), PlacementAtEnd())
	commit(t, complete, builder.Build())
}

func TestUntouchedProofTamperDetection(t *testing.T) {
	doc := parseForTest(t, `{"a":1}`, JsonProfileStrictV1)
	member := objectMembers(t, doc)[0]
	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(), core.NewInteger(big.NewInt(2)),
		RepresentationPolicyPreserveCompatible)
	result := commit(t, doc, builder.Build())
	tampered := parseForTest(t, `{"a":3}`, JsonProfileStrictV1)
	failure := result.UntouchedProof.Verify(doc.Source(), tampered.Source(),
		result.SourcePatch.Replacements())
	if failure == nil || failure.Kind != ProofErrorDigestMismatch {
		t.Fatalf("tamper failure %v", failure)
	}
}

func TestPatchReplaysOnCommit(t *testing.T) {
	doc := parseForTest(t, "{ /*lead*/ \"a\":1, /*keep*/ \"a\":2, \"z\":3, }",
		JsonProfileJsoncBoundedV1)
	members := objectMembers(t, doc)
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveMember(members[0].NodeRef())
	result := commit(t, doc, builder.Build())
	limits := sourcePatchLimits(doc.parseLimits, len(result.ChangeSet.SourceEdits()))
	replay, err := result.SourcePatch.Apply(doc.Source(), limits)
	if err != nil {
		t.Fatalf("patch apply: %v", err)
	}
	if string(replay.Bytes()) != string(result.Document.Render()) {
		t.Error("patch replay differs from commit")
	}
}

func TestCommitFailureKeepsBaseUnchanged(t *testing.T) {
	doc := parseForTest(t, `{"a":1,"b":2}`, JsonProfileStrictV1)
	members := objectMembers(t, doc)
	original := string(doc.Render())
	// A transaction whose preparation fails must not modify the base.
	builder := NewEditTransactionBuilder(doc)
	builder.RemoveMember(members[0].NodeRef()).
		InsertMember(doc.Root().NodeRef(), "x", core.Boolean(true),
			BeforeAnchor(members[0].NodeRef()))
	mustFail(t, doc, builder.Build(), EditFailurePlacementAnchorRemoved)
	if got := string(doc.Render()); got != original {
		t.Fatalf("base changed: %q", got)
	}
}

func TestOperationRegistrySurface(t *testing.T) {
	for _, profile := range []JsonProfile{JsonProfileStrictV1, JsonProfileJsoncBoundedV1,
		JsonProfileJson5StandardV1} {
		registry := FormatOperationRegistryFor(profile)
		operations := registry.Operations()
		if len(operations) != 8 {
			t.Fatalf("%v: operation count %d", profile, len(operations))
		}
		required := "json.edit.move-member@1"
		found := false
		for _, operation := range operations {
			if operation.ID() == required {
				found = true
			}
		}
		if !found {
			t.Errorf("%v: missing %s", profile, required)
		}
	}
}
