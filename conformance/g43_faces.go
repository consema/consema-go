package conformance

// The G4.3 faces of the shared suites (docs/go-implementation-plan.md §2.5;
// the 0.18.0 full-operation-parity flip of the remaining documented skips):
//
//   - protocol.change-set.actual-edit-roundtrip (protocol-v1): a real JSON
//     edit transaction commits, the change set is externalized into the
//     transferable core.change-set@1 record, and the record round-trips
//     (RFC 0004 §16; change.rs from_document);
//   - query.root-result-limit and query.cursor-failure-terminal (v1): the
//     portable-value query execution and lazy cursor terminal semantics of
//     the existing executor and the root-package cursor engine
//     (consema-core query.rs; capability core.query.ordered-results@1);
//   - syntax.cursor.completed/cancelled/failed (syntax-query-v1): the
//     ordered cursor terminal states (consema-core OrderedQueryCursor;
//     capability core.query.cursor-terminal@1).
//
// Every handler is data-driven: the vector input and expected facts drive
// the execution, and no expectation literal lives here (the
// protocol.change-set construction data mirrors the Rust runner's fixed
// construction, whose expectations the vector pins).

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	consema "consema.dev/consema"
	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/json"
	"consema.dev/consema/protocol"
)

// RunProtocolV1ChangeSetEditFace executes the
// protocol.change-set.actual-edit-roundtrip case of the
// `consema.protocol.conformance@1` suite: one real JSON edit transaction
// commits (RFC 0004 §13), the committed change set is externalized with
// caller-stable locators, the first replacement must carry the exact edit
// bytes, and the transferable record must round-trip strictly equal
// (protocol_v1.rs change_set_roundtrip).
func RunProtocolV1ChangeSetEditFace(vector *caseData, report *SuiteReport) {
	doc, failure := json.Parse(context.Background(), []byte("1"),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failG43Case(vector, report, "parse failed: "+failure.Error())
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.SemanticScalar(doc.Root().NodeRef(), core.NewInteger(big.NewInt(2)),
		json.RepresentationPolicyCanonicalForProfile)
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		failG43Case(vector, report, "commit failed: "+editFailure.Error())
		return
	}
	oldSnapshot := doc.SnapshotIdentity()
	message, err := consema.ChangeSetMessageFromDocument(&commit.ChangeSet,
		"source:old", "source:new", func(node document.NodeRef) (string, bool) {
			if node.Snapshot() == oldSnapshot {
				return "json:root:old", true
			}
			return "json:root:new", true
		})
	if err != nil {
		failG43Case(vector, report, "change-set externalization failed: "+err.Error())
		return
	}
	edits := message.SourceEdits()
	if len(edits) != 1 {
		failG43Case(vector, report, fmt.Sprintf("source edit count %d != 1", len(edits)))
		return
	}
	expectedHex, ok := caseExpected(vector, "replacement_hex")
	if !ok {
		failG43Case(vector, report, "missing expected.replacement_hex")
		return
	}
	hexText, ok := expectedHex.(core.String)
	if !ok {
		failG43Case(vector, report, "expected.replacement_hex must be String")
		return
	}
	if hex.EncodeToString(edits[0].Replacement) != string(hexText) {
		failG43Case(vector, report, fmt.Sprintf("replacement hex %s != %s",
			hex.EncodeToString(edits[0].Replacement), string(hexText)))
		return
	}
	value, err := message.ToValue()
	if err != nil {
		failG43Case(vector, report, "encode failed: "+err.Error())
		return
	}
	decoded := &protocol.ChangeSetMessage{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		failG43Case(vector, report, "decode failed: "+err.Error())
		return
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		failG43Case(vector, report, "re-encode failed: "+err.Error())
		return
	}
	strictEqual, ok := booleanField(vector.Expected, "strict_equal")
	if !ok {
		failG43Case(vector, report, "missing expected.strict_equal")
		return
	}
	if core.Equal(value, roundtripValue) != strictEqual {
		failG43Case(vector, report, "change-set round-trip equality differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// RunV1PortableQueryFace executes the portable query execution cases of
// the `consema.conformance@1` suite (lib.rs query_root_limit and
// query_cursor_failure): the root result limit fails the complete
// execution, and the lazy cursor yields local discoveries until the result
// limit stops the stream with a Failed terminal.
func RunV1PortableQueryFace(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "query.root-result-limit":
		runV1RootResultLimitFace(vector, report)
	case "query.cursor-failure-terminal":
		runV1CursorFailureTerminalFace(vector, report)
	default:
		failG43Case(vector, report, "runner does not recognize published v1 portable-query case")
	}
}

func runV1RootResultLimitFace(vector *caseData, report *SuiteReport) {
	maxResults, ok := integerField(vector.Input, "max_results")
	if !ok {
		failG43Case(vector, report, "missing input.max_results")
		return
	}
	validated, failure := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).Validate()
	if failure != nil {
		failG43Case(vector, report, "validate failed: "+failure.Error())
		return
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		failG43Case(vector, report, "bind failed: "+failure.Error())
		return
	}
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = int(maxResults)
	_, queryFailure := executable.ExecutePortable(core.NullValue(), limits)
	if queryFailure == nil || queryFailure.Kind != protocol.FailureResourceLimit {
		failG43Case(vector, report, "expected ResourceLimitExceeded")
		return
	}
	expectedStatus, ok := stringField(vector.Expected, "status")
	if !ok {
		failG43Case(vector, report, "missing expected.status")
		return
	}
	expectedFailure, ok := stringField(vector.Expected, "failure")
	if !ok {
		failG43Case(vector, report, "missing expected.failure")
		return
	}
	if expectedStatus != "Failed" || expectedFailure != queryFailureName(queryFailure.Kind) {
		failG43Case(vector, report, fmt.Sprintf("failure facts differ: %s/%s",
			expectedStatus, expectedFailure))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1CursorFailureTerminalFace(vector *caseData, report *SuiteReport) {
	elementValues, ok := sequenceField(vector.Input, "elements")
	if !ok {
		failG43Case(vector, report, "missing input.elements")
		return
	}
	items := make([]core.Value, 0, len(elementValues))
	for _, element := range elementValues {
		value, ok := g43ValueFromInput(element)
		if !ok {
			failG43Case(vector, report, "unrepresentable cursor element")
			return
		}
		items = append(items, value)
	}
	sequence := core.NewArray(items...)
	maxResults, ok := integerField(vector.Input, "max_results")
	if !ok {
		failG43Case(vector, report, "missing input.max_results")
		return
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("core.try-sequence-elements", 1))
	validated, failure := protocol.NewQueryDefinition(protocol.DomainPortableValueV1()).
		WithExpression(expression).Validate()
	if failure != nil {
		failG43Case(vector, report, "validate failed: "+failure.Error())
		return
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		failG43Case(vector, report, "bind failed: "+failure.Error())
		return
	}
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = int(maxResults)
	cursor, err := consema.NewPortableCursor(executable, sequence, limits, context.Background())
	if err != nil {
		failG43Case(vector, report, "cursor construction failed: "+err.Error())
		return
	}
	yielded := 0
	for {
		_, queryFailure, ok := cursor.NextMatch()
		if !ok {
			failG43Case(vector, report, "stream should have failed")
			return
		}
		if queryFailure == nil {
			yielded++
			continue
		}
		expectedYielded, ok := integerField(vector.Expected, "yielded_before_failure")
		if !ok {
			failG43Case(vector, report, "missing expected.yielded_before_failure")
			return
		}
		expectedTerminal, ok := stringField(vector.Expected, "terminal")
		if !ok {
			failG43Case(vector, report, "missing expected.terminal")
			return
		}
		terminal := cursor.TerminalState()
		if queryFailure.Kind != protocol.FailureResourceLimit ||
			uint64(yielded) != expectedYielded ||
			terminal == nil || string(*terminal) != expectedTerminal {
			failG43Case(vector, report, "cursor failure facts differ")
			return
		}
		break
	}
	if _, _, ok := cursor.NextMatch(); ok {
		failG43Case(vector, report, "stream must be closed after the failure")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// RunSyntaxCursorFace executes the syntax.cursor.* cases of the
// `consema.syntax-query.conformance@1` suite (syntax_query_v1.rs
// run_cursor): the ordered cursor terminal semantics — Completed after
// exhaustion, Cancelled when cancellation pre-empts the stream, Failed for
// a declared failing stream.
func RunSyntaxCursorFace(vector *caseData, report *SuiteReport) {
	valueItems, ok := sequenceField(vector.Input, "values")
	if !ok {
		failG43Case(vector, report, "missing input.values")
		return
	}
	values := make([]core.Value, 0, len(valueItems))
	for _, item := range valueItems {
		integer, ok := item.(core.Integer)
		if !ok {
			failG43Case(vector, report, "cursor value must be an Integer")
			return
		}
		values = append(values, integer)
	}
	mode, ok := stringField(vector.Input, "mode")
	if !ok {
		failG43Case(vector, report, "missing input.mode")
		return
	}
	var yielded int
	var terminal *consema.CursorTerminalState
	switch mode {
	case "Completed":
		cursor := consema.NewOrderedCursor(values)
		for {
			_, ok := cursor.Next()
			if !ok {
				break
			}
			if cursor.TerminalState() != nil {
				failG43Case(vector, report, "terminal state set before exhaustion")
				return
			}
			yielded++
		}
		terminal = cursor.TerminalState()
	case "Cancelled":
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cursor := consema.NewOrderedCursorWithCancellation(values, ctx)
		if _, ok := cursor.Next(); ok {
			if cursor.TerminalState() != nil {
				failG43Case(vector, report, "terminal state set before cancellation")
				return
			}
			yielded++
		}
		cancel()
		if _, ok := cursor.Next(); ok {
			failG43Case(vector, report, "cancelled cursor must stop yielding")
			return
		}
		terminal = cursor.TerminalState()
	case "Failed":
		cursor := consema.NewOrderedCursorWithTerminal(values, consema.CursorFailed)
		for {
			_, ok := cursor.Next()
			if !ok {
				break
			}
			if cursor.TerminalState() != nil {
				failG43Case(vector, report, "terminal state set before exhaustion")
				return
			}
			yielded++
		}
		terminal = cursor.TerminalState()
	default:
		failG43Case(vector, report, "unknown cursor mode "+mode)
		return
	}
	expectedYielded, ok := integerField(vector.Expected, "yielded")
	if !ok {
		failG43Case(vector, report, "missing expected.yielded")
		return
	}
	expectedTerminal, ok := stringField(vector.Expected, "terminal")
	if !ok {
		failG43Case(vector, report, "missing expected.terminal")
		return
	}
	if uint64(yielded) != expectedYielded ||
		terminal == nil || string(*terminal) != expectedTerminal {
		failG43Case(vector, report, "cursor terminal facts differ")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// g43ValueFromInput decodes one compact vector descriptor into a portable
// value (the integer descriptor used by the cursor vectors).
func g43ValueFromInput(value core.Value) (core.Value, bool) {
	if text, ok := stringField(value, "integer"); ok {
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, false
		}
		return core.NewInteger(integer), true
	}
	return nil, false
}

// queryFailureName maps one query failure kind onto the stable Rust
// variant spelling used by the vectors (query.rs QueryFailure; the Go
// vocabulary keeps one name per code, RFC 0016 §5.3 F3).
func queryFailureName(kind protocol.QueryFailureKind) string {
	switch kind {
	case protocol.FailureDomainMismatch:
		return "DomainMismatch"
	case protocol.FailureUnknownOperator:
		return "UnknownOperator"
	case protocol.FailureWrongArgumentType:
		return "WrongArgumentType"
	case protocol.FailureInvalidArgument:
		return "InvalidArgument"
	case protocol.FailureInvalidOperatorComposition:
		return "InvalidOperatorComposition"
	case protocol.FailureMissingCapability:
		return "MissingRequiredCapability"
	case protocol.FailureRequiredTypeMismatch:
		return "RequiredTypeMismatch"
	case protocol.FailureCardinalityViolation:
		return "CardinalityViolation"
	case protocol.FailureResourceLimit:
		return "ResourceLimitExceeded"
	case protocol.FailureCancelled:
		return "Cancelled"
	case protocol.FailureTargetUnavailable:
		return "TargetUnavailable"
	}
	return "InvalidArgument"
}

// failG43Case records one G4.3-face case failure.
func failG43Case(vector *caseData, report *SuiteReport, message string) {
	report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
}
