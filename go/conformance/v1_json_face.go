package conformance

// The JSON-face handlers of the shared suites (v1.json, syntax-query-v1.json,
// operations-v1.json), mirroring the JSON drivers of
// consema-rs/consema-conformance/src/lib.rs, syntax_query_v1.rs, and
// operations_v1.rs. The shared runner files dispatch the JSON-capability
// cases to these exported handlers; the non-JSON faces (portable-value
// query execution, TOML, conversion, protocol records) stay with their
// owning milestones. Every handler is data-driven: the vector input and
// expected facts drive the execution, and no expectation literal lives
// here.

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/json"
	"consema.dev/consema/protocol"
)

// RunV1JSONFace executes one JSON-face case of the `consema.conformance@1`
// suite: the parse, JSON native query, projection, edit, and parse-limit
// cases published by the JSON family. The 18 JSON-face case IDs are
// dispatched internally; any other case ID is reported as a failure so
// the runner never silently ignores a published case.
func RunV1JSONFace(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "parse.strict-exact-roundtrip", "parse.jsonc-comments-trailing-comma",
		"parse.recovery-missing-close", "parse.duplicate-members",
		"parse.lossless-byte-coverage", "resource.parse-token-limit":
		runV1ParseFace(vector, report)
	case "query.json-duplicate-order":
		runV1QueryDuplicateOrderFace(vector, report)
	case "projection.best-exact-duplicate-mapping", "projection.object-reject-duplicates",
		"projection.object-last-wins", "projection.object-key-provenance":
		runV1ProjectionFace(vector, report)
	case "edit.scalar-minimal", "edit.preserve-decimal-scale",
		"edit.preserve-exponent-style", "edit.canonical-for-profile",
		"edit.preserve-else-canonical", "edit.preserve-incompatible-rejected",
		"edit.wrong-snapshot":
		runV1EditFace(vector, report)
	default:
		failJSONCase(vector, report, "runner does not recognize published v1 JSON-face case")
	}
}

// RunSyntaxQueryJSONFace executes one JSON syntax-query case of the
// `consema.syntax-query.conformance@1` suite. The 8 syntax.json.* case
// IDs are dispatched internally; any other case ID is reported as a
// failure.
func RunSyntaxQueryJSONFace(vector *caseData, report *SuiteReport) {
	if !strings.HasPrefix(vector.ID, "syntax.json.") {
		failJSONCase(vector, report, "runner does not recognize published syntax-query JSON-face case")
		return
	}
	runSyntaxQueryJSONCase(vector, report)
}

// RunOperationsJSONFace executes one JSON-face case of the
// `consema.operations.conformance@1` suite: the JSON materialization,
// JSON edit, materialization security matrix, and untouched-proof cases.
// The 16 JSON-face case IDs are dispatched internally; any other case ID
// is reported as a failure.
func RunOperationsJSONFace(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "operations.v1.materialize-json-compact",
		"operations.v1.materialize-json-pretty-crlf",
		"operations.v1.materialize-json-entry-mapping-duplicates":
		runOperationsMaterializeJSONSuccessFace(vector, report)
	case "operations.v1.materialize-json-nonstring-key-rejected",
		"operations.v1.materialize-json-float-rejected",
		"operations.v1.materialize-json-output-limit",
		"operations.v1.materialization-depth-limit":
		runOperationsMaterializeJSONFailureFace(vector, report)
	case "operations.v1.json-object-insert":
		runOperationsJSONObjectInsertFace(vector, report)
	case "operations.v1.json-object-remove-duplicate":
		runOperationsJSONObjectRemoveFace(vector, report)
	case "operations.v1.json-array-remove":
		runOperationsJSONArrayRemoveFace(vector, report)
	case "operations.v1.json-conflict-atomic":
		runOperationsJSONConflictFace(vector, report)
	case "operations.v1.json-dry-run-proof-patch":
		runOperationsJSONDryRunFace(vector, report)
	case "operations.v1.json-structural-matrix":
		runOperationsJSONStructuralMatrixFace(vector, report)
	case "operations.v1.json-conflict-matrix":
		runOperationsJSONConflictMatrixFace(vector, report)
	case "operations.v1.materialization-security-matrix":
		runOperationsMaterializationSecurityMatrixFace(vector, report)
	case "operations.v1.untouched-proof-tamper":
		runOperationsUntouchedProofTamperFace(vector, report)
	default:
		failJSONCase(vector, report, "runner does not recognize published operations JSON-face case")
	}
}

// runV1ParseFace executes the v1 parse cases.
func runV1ParseFace(vector *caseData, report *SuiteReport) {
	if vector.ID == "resource.parse-token-limit" {
		runV1ParseLimitFace(vector, report)
		return
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	profile, ok := v1JSONProfile(vector)
	if !ok {
		failJSONCase(vector, report, "unknown profile")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	switch vector.ID {
	case "parse.strict-exact-roundtrip", "parse.jsonc-comments-trailing-comma":
		formation, ok := stringField(vector.Expected, "formation")
		if !ok {
			failJSONCase(vector, report, "missing expected.formation")
			return
		}
		renderEquals, ok := booleanField(vector.Expected, "render_equals_source")
		if !ok {
			failJSONCase(vector, report, "missing expected.render_equals_source")
			return
		}
		if doc.FormationStatus().String() != formation || (string(doc.Render()) == source) != renderEquals {
			failJSONCase(vector, report, "formation or render fact differs")
			return
		}
	case "parse.recovery-missing-close":
		expected, ok := stringField(vector.Expected, "diagnostic")
		if !ok {
			failJSONCase(vector, report, "missing expected.diagnostic")
			return
		}
		if doc.FormationStatus().String() != "Recovered" || !jsonHasDiagnostic(doc, expected) {
			failJSONCase(vector, report, "recovery or diagnostic fact differs")
			return
		}
	case "parse.duplicate-members":
		expectedNames, ok := jsonExpectedStrings(vector, "member_names")
		if !ok {
			failJSONCase(vector, report, "missing expected.member_names")
			return
		}
		distinct, ok := booleanField(vector.Expected, "distinct_member_identity")
		if !ok {
			failJSONCase(vector, report, "missing expected.distinct_member_identity")
			return
		}
		diagnostic, ok := stringField(vector.Expected, "diagnostic")
		if !ok {
			failJSONCase(vector, report, "missing expected.diagnostic")
			return
		}
		members := doc.Root().ObjectMembers()
		if !members.IsAvailable() {
			failJSONCase(vector, report, "root is not an object")
			return
		}
		actual := members.Value()
		names := make([]string, 0, len(actual))
		identities := make(map[document.NodeRef]bool, len(actual))
		for _, member := range actual {
			name := member.Name()
			if !name.IsAvailable() {
				failJSONCase(vector, report, "member name unavailable")
				return
			}
			names = append(names, *name.Value())
			identities[member.NodeRef()] = true
		}
		if strings.Join(names, "\x00") != strings.Join(expectedNames, "\x00") ||
			(len(identities) == len(actual)) != distinct || !jsonHasDiagnostic(doc, diagnostic) {
			failJSONCase(vector, report, "duplicate-member facts differ")
			return
		}
	case "parse.lossless-byte-coverage":
		gapCount, ok := integerField(vector.Expected, "gap_count")
		if !ok {
			failJSONCase(vector, report, "missing expected.gap_count")
			return
		}
		overlapCount, ok := integerField(vector.Expected, "overlap_count")
		if !ok {
			failJSONCase(vector, report, "missing expected.overlap_count")
			return
		}
		covered, ok := integerField(vector.Expected, "covered_bytes")
		if !ok {
			failJSONCase(vector, report, "missing expected.covered_bytes")
			return
		}
		pieces := doc.LosslessStructuralIndex().Pieces()
		gaps := uint64(0)
		overlaps := uint64(0)
		for index := 1; index < len(pieces); index++ {
			if pieces[index-1].Span().EndByte() < pieces[index].Span().StartByte() {
				gaps++
			}
			if pieces[index-1].Span().EndByte() > pieces[index].Span().StartByte() {
				overlaps++
			}
		}
		lastEnd := uint64(0)
		if len(pieces) > 0 {
			lastEnd = uint64(pieces[len(pieces)-1].Span().EndByte())
		}
		if gaps != gapCount || overlaps != overlapCount || lastEnd != covered {
			failJSONCase(vector, report, "coverage facts differ")
			return
		}
	default:
		failJSONCase(vector, report, "runner does not recognize published v1 parse case")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1ParseLimitFace(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	maxTokens, ok := integerField(vector.Input, "max_token_count")
	if !ok {
		failJSONCase(vector, report, "missing input.max_token_count")
		return
	}
	limits := document.DefaultParseLimits()
	limits.MaxTokenCount = int(maxTokens)
	_, failure := json.Parse(context.Background(), []byte(source), json.JsonProfileStrictV1, limits)
	status, ok := stringField(vector.Expected, "status")
	if !ok {
		failJSONCase(vector, report, "missing expected.status")
		return
	}
	truncated, ok := booleanField(vector.Expected, "truncated_success")
	if !ok {
		failJSONCase(vector, report, "missing expected.truncated_success")
		return
	}
	if (failure != nil) != (status == "FatalFormationFailure") || truncated {
		failJSONCase(vector, report, "fatal formation fact differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1QueryDuplicateOrderFace(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	memberName, ok := stringField(vector.Input, "member_name")
	if !ok {
		failJSONCase(vector, report, "missing input.member_name")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.try-object-members", 1)).
		Then(protocol.NewOperatorCall("json.member-name-equals", 1).
			WithArgument("name", core.String(memberName)))
	executable, bindFailure := bindQuery(protocol.DomainJSONNativeV1(), expression)
	if bindFailure != nil {
		failJSONCase(vector, report, bindFailure.Error())
		return
	}
	matches, queryFailure := json.ExecuteJSONQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		failJSONCase(vector, report, queryFailure.Error())
		return
	}
	expectedOrdinals, ok := jsonExpectedOrdinals(vector, "ordinals")
	if !ok {
		failJSONCase(vector, report, "missing expected.ordinals")
		return
	}
	expectedCount, ok := integerField(vector.Expected, "count")
	if !ok {
		failJSONCase(vector, report, "missing expected.count")
		return
	}
	if uint64(len(matches)) != expectedCount || len(matches) != len(expectedOrdinals) {
		failJSONCase(vector, report, "match count differs")
		return
	}
	for index, match := range matches {
		if match.Kind != json.JsonMatchObjectMember || uint64(match.Ordinal) != expectedOrdinals[index] {
			failJSONCase(vector, report, "match ordinal differs")
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1ProjectionFace(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	switch vector.ID {
	case "projection.best-exact-duplicate-mapping":
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		result := doc.Project(request)
		if result.Failed != nil {
			failJSONCase(vector, report, "best exact failed")
			return
		}
		kind, ok := stringField(vector.Expected, "kind")
		if !ok {
			failJSONCase(vector, report, "missing expected.kind")
			return
		}
		fidelity, ok := stringField(vector.Expected, "fidelity")
		if !ok {
			failJSONCase(vector, report, "missing expected.fidelity")
			return
		}
		associations, ok := integerField(vector.Expected, "association_origins")
		if !ok {
			failJSONCase(vector, report, "missing expected.association_origins")
			return
		}
		actualKind := "Other"
		switch result.Complete.Value.Kind() {
		case core.KindEntryMapping:
			actualKind = "EntryMapping"
		case core.KindObject:
			actualKind = "Object"
		}
		associationCount := uint64(0)
		for _, entry := range result.Complete.Provenance.Entries() {
			if entry.Projected.IsAssociation {
				associationCount++
			}
		}
		if actualKind != kind || result.Complete.Fidelity.String() != fidelity ||
			associationCount != associations {
			failJSONCase(vector, report, "projection facts differ")
			return
		}
	case "projection.object-reject-duplicates":
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetProjectAsObjectV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		if result := doc.Project(request); result.Complete != nil {
			failJSONCase(vector, report, "reject projection must fail")
			return
		}
	case "projection.object-last-wins":
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetProjectAsObjectV1).
			GlobalDuplicatePolicy(json.DuplicateKeyPolicyLastWins).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		result := doc.Project(request)
		if result.Failed != nil {
			failJSONCase(vector, report, "authorized projection failed")
			return
		}
		fidelity, ok := stringField(vector.Expected, "fidelity")
		if !ok {
			failJSONCase(vector, report, "missing expected.fidelity")
			return
		}
		hasEvent := false
		for _, event := range result.Complete.Report.Events() {
			if event.Kind == json.ProjectionEventDuplicateCollapsed {
				hasEvent = true
			}
		}
		if result.Complete.Fidelity.String() != fidelity || !hasEvent {
			failJSONCase(vector, report, "last-wins facts differ")
			return
		}
	case "projection.object-key-provenance":
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetProjectAsObjectV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		result := doc.Project(request)
		if result.Failed != nil {
			failJSONCase(vector, report, "projection failed")
			return
		}
		keyOrigins, ok := integerField(vector.Expected, "key_association_origins")
		if !ok {
			failJSONCase(vector, report, "missing expected.key_association_origins")
			return
		}
		entryOrigins, ok := integerField(vector.Expected, "entry_association_origins")
		if !ok {
			failJSONCase(vector, report, "missing expected.entry_association_origins")
			return
		}
		keys := uint64(0)
		entries := uint64(0)
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
		if keys != keyOrigins || entries != entryOrigins {
			failJSONCase(vector, report, "provenance counts differ")
			return
		}
	default:
		failJSONCase(vector, report, "runner does not recognize published v1 projection case")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1EditFace(vector *caseData, report *SuiteReport) {
	if vector.ID == "edit.wrong-snapshot" {
		runV1EditWrongSnapshotFace(vector, report)
		return
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	profile, ok := v1JSONProfile(vector)
	if !ok {
		failJSONCase(vector, report, "unknown profile")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "member unavailable")
		return
	}
	newValue, ok := v1NewValue(vector)
	if !ok {
		failJSONCase(vector, report, "missing or unrepresentable input.new_value")
		return
	}
	policyName, ok := stringField(vector.Input, "policy")
	if !ok {
		failJSONCase(vector, report, "missing input.policy")
		return
	}
	policy, ok := v1RepresentationPolicy(policyName)
	if !ok {
		failJSONCase(vector, report, "unknown policy "+policyName)
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.SemanticScalar(members.Value()[0].ValueNodeRef(), newValue, policy)
	tx := builder.Build()
	if vector.ID == "edit.preserve-incompatible-rejected" {
		_, editFailure := doc.Commit(tx)
		if editFailure == nil || editFailure.Name() != "RepresentationIncompatible" {
			failJSONCase(vector, report, "expected RepresentationIncompatible")
			return
		}
		report.Passed = append(report.Passed, vector.ID)
		return
	}
	commit, editFailure := doc.Commit(tx)
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		failJSONCase(vector, report, "missing expected.source")
		return
	}
	if string(commit.Document.Render()) != expectedSource {
		failJSONCase(vector, report, "rendered source differs")
		return
	}
	editCount, ok := integerField(vector.Expected, "source_edit_count")
	if !ok {
		failJSONCase(vector, report, "missing expected.source_edit_count")
		return
	}
	if uint64(len(commit.ChangeSet.SourceEdits())) != editCount {
		failJSONCase(vector, report, "source edit count differs")
		return
	}
	fallback := uint64(0)
	if value, ok := integerField(vector.Expected, "fallback_diagnostics"); ok {
		fallback = value
	}
	fallbacks := uint64(0)
	for _, diagnostic := range commit.ChangeSet.Diagnostics() {
		if diagnostic.Code == "json.edit.representation-fallback@1" {
			fallbacks++
		}
	}
	if fallbacks != fallback {
		failJSONCase(vector, report, "fallback diagnostic count differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runV1EditWrongSnapshotFace(vector *caseData, report *SuiteReport) {
	firstSource, ok := stringField(vector.Input, "first")
	if !ok {
		failJSONCase(vector, report, "missing input.first")
		return
	}
	secondSource, ok := stringField(vector.Input, "second")
	if !ok {
		failJSONCase(vector, report, "missing input.second")
		return
	}
	literal, ok := stringField(vector.Input, "literal")
	if !ok {
		failJSONCase(vector, report, "missing input.literal")
		return
	}
	first, failure := json.Parse(context.Background(), []byte(firstSource),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	second, failure := json.Parse(context.Background(), []byte(secondSource),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	builder := json.NewEditTransactionBuilder(second)
	builder.LiteralScalar(first.Root().NodeRef(), []byte(literal))
	_, editFailure := second.Commit(builder.Build())
	expectedFailure, ok := stringField(vector.Expected, "failure")
	if !ok {
		failJSONCase(vector, report, "missing expected.failure")
		return
	}
	unchanged, ok := booleanField(vector.Expected, "second_unchanged")
	if !ok {
		failJSONCase(vector, report, "missing expected.second_unchanged")
		return
	}
	if editFailure == nil || editFailure.Name() != expectedFailure ||
		(string(second.Render()) == secondSource) != unchanged {
		failJSONCase(vector, report, "wrong-snapshot facts differ")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runSyntaxQueryJSONCase executes one syntax.json.* case.
func runSyntaxQueryJSONCase(vector *caseData, report *SuiteReport) {
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		failJSONCase(vector, report, "missing input.profile")
		return
	}
	var profile json.JsonProfile
	switch profileName {
	case "json.strict@1":
		profile = json.JsonProfileStrictV1
	case "jsonc.bounded@1":
		profile = json.JsonProfileJsoncBoundedV1
	default:
		failJSONCase(vector, report, "unknown JSON profile "+profileName)
		return
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	executable, bindFailure := syntaxQueryDefinition(vector, "json")
	if bindFailure != nil {
		expectJSONQueryCode(vector, report, bindFailure)
		return
	}
	ctx := context.Background()
	cancelled, _ := booleanField(vector.Input, "cancelled")
	if cancelled {
		cancelCtx, cancel := context.WithCancel(ctx)
		cancel()
		ctx = cancelCtx
	}
	limits := protocol.DefaultQueryLimits()
	if maxResults, ok := integerField(vector.Input, "max_results"); ok {
		limits.MaxResults = int(maxResults)
	}
	matches, queryFailure := json.ExecuteJSONSyntaxQuery(ctx, executable, doc, limits)
	if queryFailure != nil {
		expectJSONQueryCode(vector, report, queryFailure)
		return
	}
	expected, ok := jsonExpectedSyntaxMatches(vector, doc)
	if !ok {
		failJSONCase(vector, report, "expected matches are invalid")
		return
	}
	if len(matches) != len(expected) {
		failJSONCase(vector, report, "match count differs")
		return
	}
	text, _ := doc.Source().DecodedText()
	for index, match := range matches {
		if match.Kind().AsStr() != expected[index].kind ||
			text[match.Span().StartByte():match.Span().EndByte()] != expected[index].text ||
			match.Ordinal() != expected[index].ordinal ||
			match.NodeRef().Role() != document.RoleJsonSyntaxPiece {
			failJSONCase(vector, report, "match facts differ")
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// syntaxQueryDefinition builds the executable from the vector filters,
// mirroring the Rust pipeline helper (syntax_query_v1.rs).
func syntaxQueryDefinition(vector *caseData, format string) (*protocol.ExecutableQuery, *protocol.QueryFailure) {
	filterValues, ok := sequenceField(vector.Input, "filters")
	if !ok {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "filters"}
	}
	branches := make([]*protocol.QueryExpression, 0, len(filterValues))
	for _, filter := range filterValues {
		operator, ok := stringField(filter, "operator")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "operator"}
		}
		argument, hasArgument := objectField(filter, "argument")
		var call *protocol.OperatorCall
		switch operator {
		case "kind-is":
			if !hasArgument {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
					Operator: operator, Argument: "argument"}
			}
			call = protocol.NewOperatorCall(format+".syntax-kind-is", 1).
				WithArgument("kind", argument)
		case "text-equals":
			if !hasArgument {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
					Operator: operator, Argument: "argument"}
			}
			call = protocol.NewOperatorCall(format+".syntax-text-equals", 1).
				WithArgument("text", argument)
		case "take":
			if !hasArgument {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
					Operator: operator, Argument: "argument"}
			}
			call = protocol.NewOperatorCall("core.take", 1).WithArgument("count", argument)
		case "distinct-by-identity":
			call = protocol.NewOperatorCall("core.distinct-by-identity", 1)
		default:
			call = protocol.NewOperatorCall(operator, 1)
		}
		branches = append(branches,
			(&protocol.QueryExpression{Kind: protocol.ExpressionInput}).Then(call))
	}
	var expression *protocol.QueryExpression
	combine, _ := stringField(vector.Input, "combine")
	switch combine {
	case "Single", "":
		if len(branches) == 0 {
			expression = &protocol.QueryExpression{Kind: protocol.ExpressionInput}
		} else if len(branches) == 1 {
			expression = branches[0]
		} else {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "combine"}
		}
	case "StructureOrderMerge":
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
			Branches: branches}
	case "Concat":
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionConcat, Branches: branches}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "combine"}
	}
	selectionText, _ := stringField(vector.Input, "selection")
	var selection protocol.QuerySelection
	switch selectionText {
	case "All", "":
		selection = protocol.SelectionAll
	case "First":
		selection = protocol.SelectionFirst
	case "Last":
		selection = protocol.SelectionLast
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "selection"}
	}
	domain := protocol.DomainJSONLosslessSyntaxV1()
	validated, failure := protocol.NewQueryDefinition(domain).
		WithExpression(expression).WithSelection(selection).Validate()
	if failure != nil {
		return nil, failure
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return validated.Bind(capabilities)
}

// jsonExpectedSyntaxMatch is one expected syntax match fact.
type jsonExpectedSyntaxMatch struct {
	kind    string
	text    string
	ordinal int
}

// jsonExpectedSyntaxMatches reads the expected match facts.
func jsonExpectedSyntaxMatches(vector *caseData, doc *json.Document) ([]jsonExpectedSyntaxMatch, bool) {
	values, ok := sequenceField(vector.Expected, "matches")
	if !ok {
		return nil, false
	}
	output := make([]jsonExpectedSyntaxMatch, 0, len(values))
	for _, value := range values {
		kind, ok := stringField(value, "kind")
		if !ok {
			return nil, false
		}
		expectedText, ok := stringField(value, "text")
		if !ok {
			return nil, false
		}
		ordinal, ok := integerField(value, "ordinal")
		if !ok {
			return nil, false
		}
		role, ok := stringField(value, "role")
		if !ok {
			return nil, false
		}
		if role != "JsonSyntaxPiece" {
			return nil, false
		}
		output = append(output, jsonExpectedSyntaxMatch{
			kind: kind, text: expectedText, ordinal: int(ordinal)})
	}
	return output, true
}

// expectJSONQueryCode asserts one query failure code.
func expectJSONQueryCode(vector *caseData, report *SuiteReport, failure *protocol.QueryFailure) {
	expected, ok := stringField(vector.Expected, "code")
	if !ok {
		failJSONCase(vector, report, "missing expected.code")
		return
	}
	if failure.Code() != expected {
		failJSONCase(vector, report, fmt.Sprintf("failure code %s != %s", failure.Code(), expected))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runOperationsMaterializeJSONSuccessFace executes the successful JSON
// materialization cases.
func runOperationsMaterializeJSONSuccessFace(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	target := json.ProjectionTargetBestExactCoreV1
	if projection, ok := stringField(vector.Input, "projection"); ok && projection != "BestExactCore" {
		failJSONCase(vector, report, "unknown projection "+projection)
		return
	}
	request, buildFailure := json.NewProjectionRequestBuilder(target).Build()
	if buildFailure != nil {
		failJSONCase(vector, report, buildFailure.Error())
		return
	}
	result := doc.Project(request)
	if result.Failed != nil {
		failJSONCase(vector, report, "projection failed")
		return
	}
	style, _ := stringField(vector.Input, "style")
	if style == "" {
		style = "json.canonical-compact"
	}
	newline := document.NewlineNone
	if name, ok := stringField(vector.Input, "newline"); ok {
		newline = parseJSONNewline(name)
	}
	materialization := json.Materialize(result.Complete.Value, document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId(style, 1),
	).WithNewline(newline))
	if materialization.Failed != nil {
		failJSONCase(vector, report, materialization.Failed.Failure.Error())
		return
	}
	expectedOutput, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	if string(materialization.Complete.Document.Render()) != expectedOutput {
		failJSONCase(vector, report, "output differs")
		return
	}
	expectedFidelity, ok := stringField(vector.Expected, "fidelity")
	if !ok {
		failJSONCase(vector, report, "missing expected.fidelity")
		return
	}
	if materialization.Complete.Fidelity.String() != expectedFidelity {
		failJSONCase(vector, report, "fidelity differs")
		return
	}
	if minimum, ok := integerField(vector.Expected, "minimum_provenance_entries"); ok {
		if uint64(len(materialization.Complete.Provenance.Entries())) < minimum {
			failJSONCase(vector, report, "provenance entries below minimum")
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runOperationsMaterializeJSONFailureFace executes the failing JSON
// materialization cases.
func runOperationsMaterializeJSONFailureFace(vector *caseData, report *SuiteReport) {
	var value core.Value
	var limits = document.DefaultMaterializationLimits()
	switch vector.ID {
	case "operations.v1.materialize-json-nonstring-key-rejected":
		keyText, ok := stringField(vector.Input, "key_integer")
		if !ok {
			failJSONCase(vector, report, "missing input.key_integer")
			return
		}
		key, ok := new(big.Int).SetString(keyText, 10)
		if !ok {
			failJSONCase(vector, report, "invalid key_integer")
			return
		}
		mapping, err := core.NewEntryMapping(core.EntryMappingEntry{
			Key: core.NewInteger(key), Value: core.Boolean(true)})
		if err != nil {
			failJSONCase(vector, report, err.Error())
			return
		}
		value = mapping
	case "operations.v1.materialize-json-float-rejected":
		bits, ok := stringField(vector.Input, "binary64_bits")
		if !ok {
			failJSONCase(vector, report, "missing input.binary64_bits")
			return
		}
		parsed, ok := new(big.Int).SetString(bits, 16)
		if !ok || parsed.BitLen() > 64 || parsed.Sign() < 0 {
			failJSONCase(vector, report, "invalid binary64_bits")
			return
		}
		value = core.NewBinaryFloat64(parsed.Uint64())
	case "operations.v1.materialize-json-output-limit":
		source, ok := stringField(vector.Input, "source")
		if !ok {
			failJSONCase(vector, report, "missing input.source")
			return
		}
		doc, failure := json.Parse(context.Background(), []byte(source),
			json.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		projection := doc.Project(request)
		if projection.Failed != nil {
			failJSONCase(vector, report, "projection failed")
			return
		}
		value = projection.Complete.Value
		maxOutput, ok := integerField(vector.Input, "max_output_bytes")
		if !ok {
			failJSONCase(vector, report, "missing input.max_output_bytes")
			return
		}
		limits.MaxOutputBytes = int(maxOutput)
	case "operations.v1.materialization-depth-limit":
		source, ok := stringField(vector.Input, "source")
		if !ok {
			failJSONCase(vector, report, "missing input.source")
			return
		}
		doc, failure := json.Parse(context.Background(), []byte(source),
			json.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		projection := doc.Project(request)
		if projection.Failed != nil {
			failJSONCase(vector, report, "projection failed")
			return
		}
		value = projection.Complete.Value
		maxDepth, ok := integerField(vector.Input, "max_depth")
		if !ok {
			failJSONCase(vector, report, "missing input.max_depth")
			return
		}
		limits.MaxDepth = int(maxDepth)
	default:
		failJSONCase(vector, report, "runner does not recognize published materialization failure case")
		return
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("json.strict", 1),
		document.NewMaterializationStyleId("json.canonical-compact", 1),
	).WithNewline(document.NewlineNone).WithLimits(limits)
	materialization := json.Materialize(value, request)
	if materialization.Complete != nil {
		failJSONCase(vector, report, "materialization must fail")
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		failJSONCase(vector, report, "missing expected.code")
		return
	}
	hasDocument, ok := booleanField(vector.Expected, "has_document")
	if !ok {
		failJSONCase(vector, report, "missing expected.has_document")
		return
	}
	if materialization.Failed.Failure.Code() != expectedCode || hasDocument {
		failJSONCase(vector, report, "failure facts differ")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsJSONObjectInsertFace(vector *caseData, report *SuiteReport) {
	doc, ok := jsoncDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "root is not an object")
		return
	}
	beforeOrdinal, ok := integerField(vector.Input, "before_ordinal")
	if !ok {
		failJSONCase(vector, report, "missing input.before_ordinal")
		return
	}
	name, ok := stringField(vector.Input, "name")
	if !ok {
		failJSONCase(vector, report, "missing input.name")
		return
	}
	all := members.Value()
	if int(beforeOrdinal) >= len(all) {
		failJSONCase(vector, report, "before_ordinal out of range")
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.InsertMember(doc.Root().NodeRef(), name,
		core.NewArray(core.Boolean(true)), json.BeforeAnchor(all[int(beforeOrdinal)].NodeRef()))
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	if string(commit.Document.Render()) != expected {
		failJSONCase(vector, report, "output differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsJSONObjectRemoveFace(vector *caseData, report *SuiteReport) {
	doc, ok := jsoncDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "root is not an object")
		return
	}
	targetOrdinal, ok := integerField(vector.Input, "target_ordinal")
	if !ok {
		failJSONCase(vector, report, "missing input.target_ordinal")
		return
	}
	all := members.Value()
	if int(targetOrdinal) >= len(all) {
		failJSONCase(vector, report, "target_ordinal out of range")
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.RemoveMember(all[int(targetOrdinal)].NodeRef())
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	verifyJSONCommitFacts(vector, report, doc, commit)
}

func runOperationsJSONArrayRemoveFace(vector *caseData, report *SuiteReport) {
	doc, ok := jsoncDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	elements := doc.Root().ArrayElements()
	if !elements.IsAvailable() {
		failJSONCase(vector, report, "root is not an array")
		return
	}
	targetOrdinal, ok := integerField(vector.Input, "target_ordinal")
	if !ok {
		failJSONCase(vector, report, "missing input.target_ordinal")
		return
	}
	all := elements.Value()
	if int(targetOrdinal) >= len(all) {
		failJSONCase(vector, report, "target_ordinal out of range")
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.RemoveArrayElement(all[int(targetOrdinal)].NodeRef())
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	if string(commit.Document.Render()) != expected {
		failJSONCase(vector, report, "output differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsJSONConflictFace(vector *caseData, report *SuiteReport) {
	doc, ok := strictDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	original := string(doc.Render())
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "root is not an object")
		return
	}
	targetOrdinal, ok := integerField(vector.Input, "target_ordinal")
	if !ok {
		failJSONCase(vector, report, "missing input.target_ordinal")
		return
	}
	all := members.Value()
	if int(targetOrdinal) >= len(all) {
		failJSONCase(vector, report, "target_ordinal out of range")
		return
	}
	target := all[int(targetOrdinal)].NodeRef()
	builder := json.NewEditTransactionBuilder(doc)
	builder.RenameMember(target, "x").RemoveMember(target)
	_, editFailure := doc.Commit(builder.Build())
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		failJSONCase(vector, report, "missing expected.code")
		return
	}
	unchanged, ok := booleanField(vector.Expected, "base_unchanged")
	if !ok {
		failJSONCase(vector, report, "missing expected.base_unchanged")
		return
	}
	if editFailure == nil || editFailure.Code() != expectedCode ||
		(string(doc.Render()) == original) != unchanged {
		failJSONCase(vector, report, "conflict facts differ")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsJSONDryRunFace(vector *caseData, report *SuiteReport) {
	doc, ok := strictDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	name, ok := stringField(vector.Input, "name")
	if !ok {
		failJSONCase(vector, report, "missing input.name")
		return
	}
	value, ok := stringField(vector.Input, "value")
	if !ok {
		failJSONCase(vector, report, "missing input.value")
		return
	}
	sourceID, ok := stringField(vector.Input, "source_id")
	if !ok {
		failJSONCase(vector, report, "missing input.source_id")
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.InsertMember(doc.Root().NodeRef(), name, core.String(value), json.PlacementAtEnd())
	tx := builder.Build()
	plan, dryFailure := doc.DryRun(tx, sourceID)
	if dryFailure != nil {
		failJSONCase(vector, report, dryFailure.Error())
		return
	}
	commit, editFailure := doc.Commit(tx)
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	if string(commit.Document.Render()) != expected {
		failJSONCase(vector, report, "output differs")
		return
	}
	sameReplacements, _ := booleanField(vector.Expected, "same_replacements")
	if sameJSONReplacements(plan.Replacements(), commit.SourcePatch.Replacements()) != sameReplacements {
		failJSONCase(vector, report, "plan and commit replacements differ")
		return
	}
	sameDigest, _ := booleanField(vector.Expected, "same_target_digest")
	if (plan.TargetDigest() == commit.SourcePatch.TargetDigest()) != sameDigest {
		failJSONCase(vector, report, "plan and commit target digests differ")
		return
	}
	safe, _ := booleanField(vector.Expected, "safe_summary")
	if jsonPlanSummaryIsSafe(plan) != safe {
		failJSONCase(vector, report, "summary safety differs")
		return
	}
	redactedDebug, _ := booleanField(vector.Expected, "redacted_debug")
	redacted, err := plan.WithAllReplacementsRedacted(true, true)
	if err != nil {
		failJSONCase(vector, report, err.Error())
		return
	}
	if strings.Contains(redacted.DebugString(), "secret") == redactedDebug {
		failJSONCase(vector, report, "redacted debug presentation differs")
		return
	}
	verifyJSONCommitFacts(vector, report, doc, commit)
}

func runOperationsJSONStructuralMatrixFace(vector *caseData, report *SuiteReport) {
	items, ok := sequenceField(vector.Input, "cases")
	if !ok {
		failJSONCase(vector, report, "missing input.cases")
		return
	}
	completed := 0
	for _, item := range items {
		operation, ok := stringField(item, "operation")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks operation")
			return
		}
		source, ok := stringField(item, "source")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks source")
			return
		}
		doc, failure := json.Parse(context.Background(), []byte(source),
			json.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		builder := json.NewEditTransactionBuilder(doc)
		switch operation {
		case "insert-member-end":
			name, ok := stringField(item, "name")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks name")
				return
			}
			builder.InsertMember(doc.Root().NodeRef(), name, core.Boolean(true), json.PlacementAtEnd())
		case "remove-member":
			target, ok := matrixTargetMember(doc, item)
			if !ok {
				failJSONCase(vector, report, "matrix item target invalid")
				return
			}
			builder.RemoveMember(target)
		case "rename-member":
			target, ok := matrixTargetMember(doc, item)
			if !ok {
				failJSONCase(vector, report, "matrix item target invalid")
				return
			}
			name, ok := stringField(item, "name")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks name")
				return
			}
			builder.RenameMember(target, name)
		case "insert-array-start":
			builder.InsertArrayElement(doc.Root().NodeRef(),
				core.NewInteger(big.NewInt(1)), json.PlacementAtStart())
		case "insert-array-after":
			elements := doc.Root().ArrayElements()
			if !elements.IsAvailable() {
				failJSONCase(vector, report, "root is not an array")
				return
			}
			anchorOrdinal, ok := integerField(item, "anchor_ordinal")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks anchor_ordinal")
				return
			}
			all := elements.Value()
			if int(anchorOrdinal) >= len(all) {
				failJSONCase(vector, report, "anchor_ordinal out of range")
				return
			}
			builder.InsertArrayElement(doc.Root().NodeRef(), core.String("x"),
				json.AfterAnchor(all[int(anchorOrdinal)].NodeRef()))
		default:
			failJSONCase(vector, report, "unknown JSON matrix operation "+operation)
			return
		}
		commit, editFailure := doc.Commit(builder.Build())
		if editFailure != nil {
			failJSONCase(vector, report, editFailure.Error())
			return
		}
		expected, ok := stringField(item, "expected")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks expected")
			return
		}
		if string(commit.Document.Render()) != expected {
			failJSONCase(vector, report, "matrix output mismatch for "+operation)
			return
		}
		completed++
	}
	expectedCompleted, ok := integerField(vector.Expected, "completed")
	if !ok {
		failJSONCase(vector, report, "missing expected.completed")
		return
	}
	if uint64(completed) != expectedCompleted {
		failJSONCase(vector, report, "completed count differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsJSONConflictMatrixFace(vector *caseData, report *SuiteReport) {
	items, ok := sequenceField(vector.Input, "cases")
	if !ok {
		failJSONCase(vector, report, "missing input.cases")
		return
	}
	failedAtomically := 0
	for _, item := range items {
		mode, ok := stringField(item, "mode")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks mode")
			return
		}
		source, ok := stringField(item, "source")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks source")
			return
		}
		doc, failure := json.Parse(context.Background(), []byte(source),
			json.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		original := string(doc.Render())
		var editFailure *json.EditFailure
		switch mode {
		case "wrong-snapshot":
			foreignSource, ok := stringField(item, "foreign")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks foreign")
				return
			}
			foreign, failure := json.Parse(context.Background(), []byte(foreignSource),
				json.JsonProfileStrictV1, document.DefaultParseLimits())
			if failure != nil {
				failJSONCase(vector, report, failure.Error())
				return
			}
			builder := json.NewEditTransactionBuilder(doc)
			builder.LiteralScalar(foreign.Root().NodeRef(), []byte("3"))
			_, editFailure = doc.Commit(builder.Build())
		case "same-boundary":
			builder := json.NewEditTransactionBuilder(doc)
			builder.InsertMember(doc.Root().NodeRef(), "x", core.Boolean(true), json.PlacementAtEnd()).
				InsertMember(doc.Root().NodeRef(), "y", core.Boolean(false), json.PlacementAtEnd())
			_, editFailure = doc.Commit(builder.Build())
		case "removed-anchor":
			members := doc.Root().ObjectMembers()
			if !members.IsAvailable() {
				failJSONCase(vector, report, "root is not an object")
				return
			}
			member := members.Value()[0]
			builder := json.NewEditTransactionBuilder(doc)
			builder.RemoveMember(member.NodeRef()).
				InsertMember(doc.Root().NodeRef(), "x", core.Boolean(true),
					json.BeforeAnchor(member.NodeRef()))
			_, editFailure = doc.Commit(builder.Build())
		case "ancestor-descendant":
			members := doc.Root().ObjectMembers()
			if !members.IsAvailable() {
				failJSONCase(vector, report, "root is not an object")
				return
			}
			member := members.Value()[0]
			builder := json.NewEditTransactionBuilder(doc)
			builder.SemanticScalar(member.ValueNodeRef(), core.NewInteger(big.NewInt(3)),
				json.RepresentationPolicyPreserveCompatible).
				RemoveMember(member.NodeRef())
			_, editFailure = doc.Commit(builder.Build())
		default:
			failJSONCase(vector, report, "unknown JSON conflict mode "+mode)
			return
		}
		expectedCode, ok := stringField(item, "code")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks code")
			return
		}
		if editFailure == nil || editFailure.Code() != expectedCode ||
			string(doc.Render()) != original {
			failJSONCase(vector, report, "conflict mismatch for "+mode)
			return
		}
		failedAtomically++
	}
	expected, ok := integerField(vector.Expected, "failed_atomically")
	if !ok {
		failJSONCase(vector, report, "missing expected.failed_atomically")
		return
	}
	if uint64(failedAtomically) != expected {
		failJSONCase(vector, report, "failed_atomically count differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsMaterializationSecurityMatrixFace(vector *caseData, report *SuiteReport) {
	items, ok := sequenceField(vector.Input, "cases")
	if !ok {
		failJSONCase(vector, report, "missing input.cases")
		return
	}
	completed := 0
	for _, item := range items {
		mode, ok := stringField(item, "mode")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks mode")
			return
		}
		source, ok := stringField(item, "source")
		if !ok {
			failJSONCase(vector, report, "matrix item lacks source")
			return
		}
		doc, failure := json.Parse(context.Background(), []byte(source),
			json.JsonProfileStrictV1, document.DefaultParseLimits())
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		request, buildFailure := json.NewProjectionRequestBuilder(json.ProjectionTargetBestExactCoreV1).Build()
		if buildFailure != nil {
			failJSONCase(vector, report, buildFailure.Error())
			return
		}
		projection := doc.Project(request)
		if projection.Failed != nil {
			failJSONCase(vector, report, "projection failed")
			return
		}
		baseRequest := document.NewMaterializationRequest(
			document.NewProfileId("json.strict", 1),
			document.NewMaterializationStyleId("json.canonical-compact", 1),
		).WithNewline(document.NewlineNone)
		switch mode {
		case "node-limit", "provenance-limit":
			limit, ok := integerField(item, "limit")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks limit")
				return
			}
			limits := document.DefaultMaterializationLimits()
			if mode == "node-limit" {
				limits.MaxInputNodes = int(limit)
			} else {
				limits.MaxProvenanceEntries = int(limit)
			}
			materialization := json.Materialize(projection.Complete.Value,
				baseRequest.WithLimits(limits))
			if materialization.Complete != nil {
				failJSONCase(vector, report, "security case unexpectedly completed")
				return
			}
			expectedCode, ok := stringField(item, "code")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks code")
				return
			}
			if materialization.Failed.Failure.Code() != expectedCode {
				failJSONCase(vector, report, "security code mismatch for "+mode)
				return
			}
		case "escaping":
			materialization := json.Materialize(projection.Complete.Value, baseRequest)
			if materialization.Failed != nil {
				failJSONCase(vector, report, "escaping case unexpectedly failed")
				return
			}
			expected, ok := stringField(item, "expected")
			if !ok {
				failJSONCase(vector, report, "matrix item lacks expected")
				return
			}
			if string(materialization.Complete.Document.Render()) != expected {
				failJSONCase(vector, report, "escaping output mismatch")
				return
			}
		default:
			failJSONCase(vector, report, "unknown security mode "+mode)
			return
		}
		completed++
	}
	expected, ok := integerField(vector.Expected, "completed")
	if !ok {
		failJSONCase(vector, report, "missing expected.completed")
		return
	}
	if uint64(completed) != expected {
		failJSONCase(vector, report, "completed count differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runOperationsUntouchedProofTamperFace(vector *caseData, report *SuiteReport) {
	doc, ok := strictDocumentFromCase(vector)
	if !ok {
		failJSONCase(vector, report, "invalid source")
		return
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "root is not an object")
		return
	}
	member := members.Value()[0]
	builder := json.NewEditTransactionBuilder(doc)
	builder.SemanticScalar(member.ValueNodeRef(), core.NewInteger(big.NewInt(2)),
		json.RepresentationPolicyPreserveCompatible)
	commit, editFailure := doc.Commit(builder.Build())
	if editFailure != nil {
		failJSONCase(vector, report, editFailure.Error())
		return
	}
	tamperedSource, ok := stringField(vector.Input, "tampered_target")
	if !ok {
		failJSONCase(vector, report, "missing input.tampered_target")
		return
	}
	tampered, failure := json.Parse(context.Background(), []byte(tamperedSource),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	detected, ok := booleanField(vector.Expected, "tamper_detected")
	if !ok {
		failJSONCase(vector, report, "missing expected.tamper_detected")
		return
	}
	if (commit.UntouchedProof.Verify(doc.Source(), tampered.Source(),
		commit.SourcePatch.Replacements()) != nil) != detected {
		failJSONCase(vector, report, "tamper detection differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// verifyJSONCommitFacts verifies the shared commit artifacts of one case
// (operations_v1.rs verify_commit).
func verifyJSONCommitFacts(vector *caseData, report *SuiteReport, doc *json.Document,
	commit *json.EditCommit) {
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	limits := document.DefaultSourcePatchLimits()
	replay, err := commit.SourcePatch.Apply(doc.Source(), limits)
	if err != nil {
		failJSONCase(vector, report, err.Error())
		return
	}
	patchReplays, _ := booleanField(vector.Expected, "patch_replays")
	if (string(replay.Bytes()) == expected) != patchReplays ||
		string(commit.Document.Render()) != expected {
		failJSONCase(vector, report, "patch replay differs")
		return
	}
	proofVerifies, _ := booleanField(vector.Expected, "proof_verifies")
	if (commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()) == nil) != proofVerifies {
		failJSONCase(vector, report, "untouched proof verdict differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// jsonPlanSummaryIsSafe reports whether every summary value avoids the
// secret content.
func jsonPlanSummaryIsSafe(plan *json.EditPlan) bool {
	for _, operation := range plan.Operations() {
		for _, value := range operation.Summary {
			if strings.Contains(value, "secret") {
				return false
			}
		}
	}
	return true
}

// matrixTargetMember resolves one matrix item's target member.
func matrixTargetMember(doc *json.Document, item core.Value) (document.NodeRef, bool) {
	targetOrdinal, ok := integerField(item, "target_ordinal")
	if !ok {
		return document.NodeRef{}, false
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		return document.NodeRef{}, false
	}
	all := members.Value()
	if int(targetOrdinal) >= len(all) {
		return document.NodeRef{}, false
	}
	return all[int(targetOrdinal)].NodeRef(), true
}

// strictDocumentFromCase parses the case source under the strict profile.
func strictDocumentFromCase(vector *caseData) (*json.Document, bool) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, false
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		return nil, false
	}
	return doc, true
}

// jsoncDocumentFromCase parses the case source under the JSONC profile.
func jsoncDocumentFromCase(vector *caseData) (*json.Document, bool) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, false
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJsoncBoundedV1, document.DefaultParseLimits())
	if failure != nil {
		return nil, false
	}
	return doc, true
}

// v1JSONProfile reads the case profile under the v1 naming.
func v1JSONProfile(vector *caseData) (json.JsonProfile, bool) {
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		return json.JsonProfileStrictV1, false
	}
	return parseJSONProfile(profileName)
}

// v1NewValue reads one new_value descriptor.
func v1NewValue(vector *caseData) (core.Value, bool) {
	value, ok := caseInput(vector, "new_value")
	if !ok {
		return nil, false
	}
	if text, ok := stringField(value, "integer"); ok {
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, false
		}
		return core.NewInteger(integer), true
	}
	if text, ok := stringField(value, "decimal"); ok {
		decimal, err := parseDecimalNumber(core.String(text))
		if err != nil {
			return nil, false
		}
		return decimal, true
	}
	return nil, false
}

// v1RepresentationPolicy resolves one policy name.
func v1RepresentationPolicy(name string) (json.RepresentationPolicy, bool) {
	switch name {
	case "PreserveCompatible":
		return json.RepresentationPolicyPreserveCompatible, true
	case "CanonicalForProfile":
		return json.RepresentationPolicyCanonicalForProfile, true
	case "PreserveElseCanonical":
		return json.RepresentationPolicyPreserveElseCanonical, true
	case "ExactLiteral":
		return json.RepresentationPolicyExactLiteral, true
	}
	return json.RepresentationPolicyExactLiteral, false
}

// jsonExpectedOrdinals reads one expected ordinal sequence.
func jsonExpectedOrdinals(vector *caseData, name string) ([]uint64, bool) {
	values, ok := sequenceField(vector.Expected, name)
	if !ok {
		return nil, false
	}
	output := make([]uint64, 0, len(values))
	for _, value := range values {
		integer, ok := value.(core.Integer)
		if !ok {
			return nil, false
		}
		number := integer.Int()
		if !number.IsUint64() {
			return nil, false
		}
		output = append(output, number.Uint64())
	}
	return output, true
}

// parseJSONNewline resolves one newline policy name.
func parseJSONNewline(name string) document.NewlinePolicy {
	switch name {
	case "Lf":
		return document.NewlineLf
	case "CrLf":
		return document.NewlineCrLf
	}
	return document.NewlineNone
}
