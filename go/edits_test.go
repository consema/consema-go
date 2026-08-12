package consema

// The G4.3 end-to-end tests (docs/go-implementation-plan.md §2.5): for
// every family, parse -> edit -> commit -> patch apply -> digest
// verification through the root-package edit surface; the batch-plan full
// flow (plan -> manifest -> apply with the base-digest and original-bytes
// dual preconditions -> result manifest); the ordered cursor terminal
// semantics; and the change-set externalization round-trip.

import (
	"context"
	"math/big"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// TestJSONEditEndToEnd drives one JSON edit through the root-package
// surface: parse -> plan -> commit -> patch apply -> digest verification.
func TestJSONEditEndToEnd(t *testing.T) {
	doc, failure := ParseJSON(context.Background(), []byte(`{"a": 1}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsJSON()
	members := typed.Root().ObjectMembers()
	if !members.IsAvailable() {
		t.Fatal("root is not an object")
	}
	builder := jsonpkg.NewEditTransactionBuilder(typed)
	builder.SemanticScalar(members.Value()[0].ValueNodeRef(), core.NewInteger(big.NewInt(2)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	tx := NewJSONEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:json")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != `{"a": 2}` {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestTOMLEditEndToEnd drives one TOML edit through the root surface.
func TestTOMLEditEndToEnd(t *testing.T) {
	doc, failure := ParseTOML([]byte("a = 1\n"), toml.Toml10V1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsTOML()
	builder := toml.NewEditTransactionBuilder(typed)
	builder.InsertEntry(typed.Root().NodeRef(), "b", core.NewInteger(big.NewInt(2)), toml.PlacementEnd())
	tx := NewTOMLEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:toml")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	// The TOML family inserts root entries with its canonical quoted-key
	// spelling and owns no trailing newline for a root insertion
	// (toml_test.go TestEditRootAndStandardTableInsertions precedent).
	if string(commit.Document.Render()) != "a = 1\n\"b\" = 2" {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestYAMLEditCommitEndToEnd drives one YAML edit commit through the root
// surface; the yaml family publishes no dry-run in this milestone (G2.1
// gap), so the plan path is asserted as the documented unsupported error.
func TestYAMLEditCommitEndToEnd(t *testing.T) {
	doc, failure := ParseYAML([]byte("a: 1\n"), yaml.Yaml12CoreV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsYAML()
	root, ok := typed.Document(0)
	if !ok {
		t.Fatal("no yaml document")
	}
	entry, ok := root.Root().MappingEntry(0)
	if !ok {
		t.Fatal("no mapping entry")
	}
	builder := yaml.NewEditTransactionBuilder(typed)
	builder.SemanticScalar(entry.Value().NodeRef(), core.NewInteger(big.NewInt(2)),
		yaml.RepresentationPolicyPreserveCompatible)
	tx := NewYAMLEditTransaction(builder.Build())
	if _, err := PlanEdit(doc, tx, "memory:yaml"); err == nil {
		t.Fatal("yaml plan must be unsupported in this milestone")
	} else if unsupported, ok := err.(*EditUnsupportedError); !ok ||
		unsupported.Code() != "core.edit.operation-unsupported@1" {
		t.Fatalf("unexpected plan failure %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != "a: 2\n" {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, nil)
}

// TestINIEditEndToEnd drives one INI edit through the root surface.
func TestINIEditEndToEnd(t *testing.T) {
	doc, failure := ParseINI([]byte("[s]\nk=old\n"), ini.PortableV1,
		ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsINI()
	builder := ini.NewEditTransactionBuilder(typed)
	builder.SemanticValue(typed.Entries()[0].NodeRef(), "new value",
		ini.RepresentationPolicyCanonicalForProfile)
	tx := NewINIEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:ini")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != "[s]\nk=new value\n" {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestPropertiesEditEndToEnd drives one Java-Properties edit through the
// root surface.
func TestPropertiesEditEndToEnd(t *testing.T) {
	doc, failure := ParseProperties([]byte("a=one\n"), properties.PropertiesReaderV1,
		properties.ReaderEncodingSelection(document.Utf8Encoding()),
		properties.DefaultPropertiesParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsProperties()
	builder := properties.NewEditTransactionBuilder(typed)
	builder.SemanticValue(typed.Properties()[0].NodeRef(),
		properties.NewJavaStringFromUnicode("two"))
	tx := NewPropertiesEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:properties")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != "a=two\n" {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestXMLEditEndToEnd drives one XML edit through the root surface.
func TestXMLEditEndToEnd(t *testing.T) {
	doc, failure := ParseXML(context.Background(), []byte(`<root a="1"/>`),
		xmlpkg.XmlProfileSafeV1, xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsXML()
	builder := xmlpkg.NewEditTransactionBuilder(typed)
	builder.InsertAttribute(typed.Root().NodeRef(), xmlpkg.NewNameFacts(nil, "b", nil), "2",
		xmlpkg.AttributePlacementEnd())
	tx := NewXMLEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:xml")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != `<root a="1" b="2"/>` {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestPlistEditEndToEnd drives one plist edit through the root surface.
func TestPlistEditEndToEnd(t *testing.T) {
	source := `<plist version="1.0"><dict><key>a</key><string>old</string></dict></plist>`
	doc, failure := ParsePlist([]byte(source), plist.PlistProfileXmlV1,
		plist.PlistEncodingProfileDefault(), plist.DefaultPlistParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsPlist()
	builder := plist.NewEditTransactionBuilder(typed)
	builder.SetValue(plist.NewEditPath([]plist.EditPathStep{
		plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode("a"), 0),
	}), plist.NewEditValueString(plist.NewPlistStringFromUnicode("new")))
	tx := NewPlistEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:plist")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) !=
		`<plist version="1.0"><dict><key>a</key><string>new</string></dict></plist>` {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// TestHCLEditEndToEnd drives one HCL edit through the root surface.
func TestHCLEditEndToEnd(t *testing.T) {
	doc, failure := ParseHCL(context.Background(), []byte("a = 1\n"), hcl.HclProfileNativeV1,
		hcl.HclEncodingSelectionProfileDefault(), hcl.DefaultHclParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsHCL()
	builder := hcl.NewEditTransactionBuilder(typed)
	builder.SetAttributeValue(hcl.BodyPathRoot(), "a", hcl.EditValueIntegerV(2))
	tx := NewHCLEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "memory:hcl")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if string(commit.Document.Render()) != "a = 2\n" {
		t.Fatalf("render %q", commit.Document.Render())
	}
	verifyCommitFacts(t, doc, commit, plan)
}

// sourceOf returns the underlying document-layer source snapshot of one
// union document.
func sourceOf(doc *Document) *document.SourceSnapshot {
	switch {
	case doc.inner.json != nil:
		return doc.inner.json.Source()
	case doc.inner.toml != nil:
		return doc.inner.toml.Source()
	case doc.inner.yaml != nil:
		return doc.inner.yaml.Source()
	case doc.inner.ini != nil:
		return doc.inner.ini.Source()
	case doc.inner.properties != nil:
		return doc.inner.properties.Source()
	case doc.inner.xml != nil:
		return doc.inner.xml.Source()
	case doc.inner.plist != nil:
		return doc.inner.plist.Source()
	case doc.inner.hcl != nil:
		return doc.inner.hcl.Source()
	}
	return nil
}

// verifyCommitFacts closes the end-to-end chain: the patch reapplies to the
// base snapshot and reproduces the exact new bytes, the target digest
// matches the patch facts, and the untouched proof verifies (RFC 0004
// §15-§16). When a plan is supplied, the dry-run and the commit must agree
// on the replacement set and the target digest (RFC 0004 §20).
func verifyCommitFacts(t *testing.T, base *Document, commit *EditCommit,
	plan *document.EditPlan) {
	t.Helper()
	if commit.ChangeSet.BaseSnapshot() != base.SnapshotIdentity() {
		t.Fatalf("change set base snapshot differs")
	}
	limits := document.DefaultSourcePatchLimits()
	replay, err := commit.SourcePatch.Apply(sourceOf(base), limits)
	if err != nil {
		t.Fatalf("patch apply: %v", err)
	}
	if string(replay.Bytes()) != string(commit.Document.Render()) {
		t.Fatalf("patch replay bytes differ from the committed render")
	}
	if !replay.Digest().Equal(commit.SourcePatch.TargetDigest()) {
		t.Fatalf("replayed digest differs from the patch target digest")
	}
	if err := commit.UntouchedProof.Verify(sourceOf(base),
		sourceOf(commit.Document), commit.SourcePatch.Replacements()); err != nil {
		t.Fatalf("untouched proof: %v", err)
	}
	if plan != nil {
		if plan.TargetDigest() != commit.SourcePatch.TargetDigest() {
			t.Fatalf("dry-run and commit target digests differ")
		}
		if len(plan.Replacements()) != len(commit.SourcePatch.Replacements()) {
			t.Fatalf("dry-run and commit replacement sets differ")
		}
	}
}

// TestBatchPlanApplyFullFlow exercises the complete batch-plan protocol
// composition (RFC 0015 §8-§9): plan one file, close the manifest, decode
// it back from the wire value, apply with the exact base bytes
// (completed), with stale bytes (skipped-stale), and with a forged patch
// whose base digest matches but whose original bytes do not (failed
// original-bytes precondition), then close the result manifest.
func TestBatchPlanApplyFullFlow(t *testing.T) {
	doc, failure := ParseJSON(context.Background(), []byte(`{"a": 1}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsJSON()
	members := typed.Root().ObjectMembers()
	builder := jsonpkg.NewEditTransactionBuilder(typed)
	builder.SemanticScalar(members.Value()[0].ValueNodeRef(), core.NewInteger(big.NewInt(2)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	tx := NewJSONEditTransaction(builder.Build())
	plan, err := PlanEdit(doc, tx, "app.conf")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	planner := NewBatchPlanner("0.18.0")
	if err := planner.AddPlanned("app.conf", plan); err != nil {
		t.Fatalf("add planned: %v", err)
	}
	planMessage, err := planner.Build()
	if err != nil {
		t.Fatalf("build plan: %v", err)
	}
	if len(planMessage.Files()) != 1 {
		t.Fatalf("plan file count %d", len(planMessage.Files()))
	}
	entry := planMessage.Files()[0]
	if entry.Status() != protocol.PlanStatusPlanned {
		t.Fatalf("unexpected plan status %s", entry.Status())
	}
	if !entry.SourceDigest().Equal(entry.SourcePatch().BaseDigest) {
		t.Fatalf("source_digest != base_digest")
	}
	// The manifest round-trips through the wire value.
	value, err := planMessage.ToValue()
	if err != nil {
		t.Fatalf("plan encode: %v", err)
	}
	decodedPlan := &protocol.BatchPlanMessage{}
	roundtripped, err := decodedPlan.FromValue(value)
	if err != nil {
		t.Fatalf("plan decode: %v", err)
	}
	if len(roundtripped.Files()) != 1 {
		t.Fatalf("roundtripped plan file count %d", len(roundtripped.Files()))
	}
	limits := protocol.DefaultSourcePatchLimits()
	baseBytes := []byte(`{"a": 1}`)
	// Exact base bytes: completed with the verified target digest.
	completed, err := ApplyPlanFile(roundtripped.Files()[0], baseBytes, limits)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if completed.Status() != protocol.ResultStatusCompleted ||
		completed.TargetDigest() == nil ||
		!completed.TargetDigest().Equal(protocol.ContentDigestFromBytes(plan.TargetDigest().Bytes())) {
		t.Fatalf("completed facts differ: %s", completed.Status())
	}
	// Stale bytes: skipped-stale with the base-mismatch code, no apply
	// (RFC 0015 §9.3 step 1).
	stale, err := ApplyPlanFile(roundtripped.Files()[0], []byte(`{"a": 99}`), limits)
	if err != nil {
		t.Fatalf("stale apply: %v", err)
	}
	if stale.Status() != protocol.ResultStatusSkippedStale ||
		stale.FailureCode() == nil ||
		*stale.FailureCode() != "core.source.patch-base-mismatch@1" {
		t.Fatalf("stale facts differ: %s", stale.Status())
	}
	// Forged patch: the base digest matches but the original bytes do not
	// (RFC 0015 §9.3 step 2 — the dual precondition: base digest AND
	// original bytes are both checked before any apply). The forged
	// original keeps the replacement range length so the structural
	// validation passes and the byte precondition fails.
	forged := entry.SourcePatch()
	forged.Replacements[0].Original = []byte("9")
	originalMismatch, err := ApplyPlanFile(entry, baseBytes, limits)
	if err != nil {
		t.Fatalf("forged apply: %v", err)
	}
	if originalMismatch.Status() != protocol.ResultStatusFailed ||
		originalMismatch.FailureCode() == nil ||
		*originalMismatch.FailureCode() != "core.source.patch-original-mismatch@1" {
		t.Fatalf("forged facts differ: %s", originalMismatch.Status())
	}
	// Result manifest closure.
	resultMessage, err := protocol.NewBatchResultMessage("0.18.0",
		[]*protocol.BatchResultFileEntry{completed, stale, originalMismatch})
	if err != nil {
		t.Fatalf("build result: %v", err)
	}
	if len(resultMessage.Files()) != 3 {
		t.Fatalf("result file count %d", len(resultMessage.Files()))
	}
}

// TestBatchPlannerFailedEntry pins the per-file failed plan entry (RFC
// 0015 §8.2: one failing file does not fail the batch).
func TestBatchPlannerFailedEntry(t *testing.T) {
	planner := NewBatchPlanner("0.18.0")
	if err := planner.AddFailed("broken.conf", "core.source.invalid-utf8@1",
		[]*protocol.Diagnostic{}); err != nil {
		t.Fatalf("add failed: %v", err)
	}
	message, err := planner.Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(message.Files()) != 1 || message.Files()[0].Status() != protocol.PlanStatusFailed {
		t.Fatalf("failed entry facts differ")
	}
}

// TestOrderedCursorTerminals pins the three closed cursor terminal states
// (query.rs OrderedQueryCursor; capability core.query.cursor-terminal@1).
func TestOrderedCursorTerminals(t *testing.T) {
	values := []core.Value{core.NewInteger(big.NewInt(1)), core.NewInteger(big.NewInt(2))}

	cursor := NewOrderedCursor(values)
	yielded := 0
	for {
		_, ok := cursor.Next()
		if !ok {
			break
		}
		if cursor.TerminalState() != nil {
			t.Fatal("terminal set before exhaustion")
		}
		yielded++
	}
	if yielded != 2 || cursor.TerminalState() == nil ||
		*cursor.TerminalState() != CursorCompleted {
		t.Fatalf("completed cursor facts differ: %d/%v", yielded, cursor.TerminalState())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := NewOrderedCursorWithCancellation(values, ctx)
	if _, ok := cancelled.Next(); !ok {
		t.Fatal("cancelled cursor yielded nothing")
	}
	cancel()
	if _, ok := cancelled.Next(); ok {
		t.Fatal("cancelled cursor must stop yielding")
	}
	if cancelled.TerminalState() == nil || *cancelled.TerminalState() != CursorCancelled {
		t.Fatalf("cancelled terminal differs: %v", cancelled.TerminalState())
	}

	failed := NewOrderedCursorWithTerminal(values, CursorFailed)
	yielded = 0
	for {
		_, ok := failed.Next()
		if !ok {
			break
		}
		yielded++
	}
	if yielded != 2 || failed.TerminalState() == nil ||
		*failed.TerminalState() != CursorFailed {
		t.Fatalf("failed cursor facts differ: %d/%v", yielded, failed.TerminalState())
	}
}

// TestPortableCursorResultLimit pins the lazy portable cursor: the stream
// yields local discoveries until the result limit stops it with a Failed
// terminal (query.rs PortableQueryCursor; the query.cursor-failure-terminal
// vector facts).
func TestPortableCursorResultLimit(t *testing.T) {
	items := make([]core.Value, 0, 5)
	for i := int64(1); i <= 5; i++ {
		items = append(items, core.NewInteger(big.NewInt(i)))
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("core.try-sequence-elements", 1))
	validated, failure := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).
		WithExpression(expression).Validate()
	if failure != nil {
		t.Fatalf("validate: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("bind: %v", failure)
	}
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = 3
	cursor, err := NewPortableCursor(executable, core.NewArray(items...), limits, context.Background())
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	yielded := 0
	for {
		_, queryFailure, ok := cursor.NextMatch()
		if !ok {
			t.Fatal("stream should have failed")
		}
		if queryFailure == nil {
			yielded++
			continue
		}
		if queryFailure.Kind != protocol.FailureResourceLimit || yielded != 3 ||
			cursor.TerminalState() == nil || *cursor.TerminalState() != CursorFailed {
			t.Fatalf("cursor failure facts differ: %d/%v", yielded, cursor.TerminalState())
		}
		break
	}
	if _, _, ok := cursor.NextMatch(); ok {
		t.Fatal("stream must be closed after the failure")
	}

	// The complete execution surface fails identically at the root limit.
	validatedEmpty, failure := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).Validate()
	if failure != nil {
		t.Fatalf("validate empty: %v", failure)
	}
	executableEmpty, failure := validatedEmpty.Bind(capabilities)
	if failure != nil {
		t.Fatalf("bind empty: %v", failure)
	}
	zeroLimits := protocol.DefaultQueryLimits()
	zeroLimits.MaxResults = 0
	if _, queryFailure := executableEmpty.ExecutePortable(core.NullValue(), zeroLimits); queryFailure == nil || queryFailure.Kind != protocol.FailureResourceLimit {
		t.Fatal("root result limit must fail the complete execution")
	}
}

// TestPortableCursorCancellation pins the Cancelled terminal of the lazy
// cursor (query.rs PortableQueryCursor::next_match).
func TestPortableCursorCancellation(t *testing.T) {
	items := []core.Value{core.NewInteger(big.NewInt(1)), core.NewInteger(big.NewInt(2))}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("core.try-sequence-elements", 1))
	validated, failure := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).
		WithExpression(expression).Validate()
	if failure != nil {
		t.Fatalf("validate: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("bind: %v", failure)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cursor, err := NewPortableCursor(executable, core.NewArray(items...),
		protocol.DefaultQueryLimits(), ctx)
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if _, queryFailure, ok := cursor.NextMatch(); !ok || queryFailure != nil {
		t.Fatal("first match must yield")
	}
	cancel()
	_, queryFailure, ok := cursor.NextMatch()
	if !ok || queryFailure == nil || queryFailure.Kind != protocol.FailureCancelled ||
		cursor.TerminalState() == nil || *cursor.TerminalState() != CursorCancelled {
		t.Fatalf("cancelled cursor facts differ: %v/%v", queryFailure, cursor.TerminalState())
	}
	if _, _, ok := cursor.NextMatch(); ok {
		t.Fatal("stream must be closed after cancellation")
	}
}

// TestChangeSetMessageRoundTrip pins the change-set externalization: one
// real commit's change set becomes the transferable core.change-set@1
// record and round-trips strictly equal (change.rs from_document).
func TestChangeSetMessageRoundTrip(t *testing.T) {
	doc, failure := ParseJSON(context.Background(), []byte("1"),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsJSON()
	builder := jsonpkg.NewEditTransactionBuilder(typed)
	builder.SemanticScalar(typed.Root().NodeRef(), core.NewInteger(big.NewInt(2)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	tx := NewJSONEditTransaction(builder.Build())
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	oldSnapshot := doc.SnapshotIdentity()
	message, err := ChangeSetMessageFromDocument(&commit.ChangeSet,
		"source:old", "source:new", func(node document.NodeRef) (string, bool) {
			if node.Snapshot() == oldSnapshot {
				return "json:root:old", true
			}
			return "json:root:new", true
		})
	if err != nil {
		t.Fatalf("externalize: %v", err)
	}
	edits := message.SourceEdits()
	if len(edits) != 1 || string(edits[0].Replacement) != "2" {
		t.Fatalf("source edits differ: %v", edits)
	}
	value, err := message.ToValue()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded := &protocol.ChangeSetMessage{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !core.Equal(value, roundtripValue) {
		t.Fatal("change-set round-trip differs")
	}
	if roundtripped.OldSourceID() != "source:old" || roundtripped.NewSourceID() != "source:new" {
		t.Fatal("source identities differ")
	}
	if len(roundtripped.NodeMappings()) != len(commit.ChangeSet.NodeMappings()) {
		t.Fatal("node mapping count differs")
	}
}

// TestChangeSetMessageRejectsUnresolvableLocator pins the process-local
// handle rejection of the externalization (change.rs from_document: an
// unresolvable NodeRef is a protocol error).
func TestChangeSetMessageRejectsUnresolvableLocator(t *testing.T) {
	doc, failure := ParseJSON(context.Background(), []byte("1"),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse: %s", failure.Error())
	}
	typed, _ := doc.AsJSON()
	builder := jsonpkg.NewEditTransactionBuilder(typed)
	builder.SemanticScalar(typed.Root().NodeRef(), core.NewInteger(big.NewInt(2)),
		jsonpkg.RepresentationPolicyCanonicalForProfile)
	tx := NewJSONEditTransaction(builder.Build())
	commit, err := CommitEdit(doc, tx)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	_, err = ChangeSetMessageFromDocument(&commit.ChangeSet, "source:old", "source:new",
		func(document.NodeRef) (string, bool) { return "", false })
	if err == nil {
		t.Fatal("unresolvable locator must fail")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Kind != protocol.KindProcessLocalHandle {
		t.Fatalf("unexpected failure %v", err)
	}
}

// TestRedactionPatternMatchesFrozenSet pins the frozen v1 key-name
// redaction pattern set (RFC 0015 §11.2).
func TestRedactionPatternMatchesFrozenSet(t *testing.T) {
	for _, name := range []string{"password", "api_key", "private-key", "accessKey",
		"credential", "TOKEN", "auth"} {
		if !KeyMatchesRedactionPattern(name) {
			t.Errorf("pattern must match %q", name)
		}
	}
	for _, name := range []string{"name", "host", "port"} {
		if KeyMatchesRedactionPattern(name) {
			t.Errorf("pattern must not match %q", name)
		}
	}
}
