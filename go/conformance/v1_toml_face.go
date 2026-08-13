package conformance

// The TOML face of the shared cross-format suites. The 0.15.0 G1.3
// milestone implements the `toml.lossless-syntax-query@1` cases of
// `consema.syntax-query.conformance@1` (8 cases) and the TOML operation
// cases of `consema.operations.conformance@1` (12 executed cases; the
// operation-registry case needs both the JSON and TOML registries and is a
// documented skip until the JSON family lands). The shared suite files
// dispatch these cases to the exported handlers below; the vector files
// are the authority and no expectation literals are embedded.
//
// The two shared runner files (`syntax_query_v1.go`, `operations_v1.go`,
// and `v1.go`) are edited by the milestone coordinator; the handlers here
// are the toml-face implementations those files call.

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

// tomlSyntaxOperators maps the vector filter operator spellings onto the
// full lossless-syntax-query operator ids.
func tomlSyntaxOperatorCall(operator, argument string) *protocol.OperatorCall {
	switch operator {
	case "kind-is":
		return protocol.NewOperatorCall("toml.syntax-kind-is", 1).
			WithArgument("kind", core.String(argument))
	case "text-equals":
		return protocol.NewOperatorCall("toml.syntax-text-equals", 1).
			WithArgument("text", core.String(argument))
	case "take":
		return protocol.NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(big.NewInt(argumentInt(argument))))
	case "distinct-by-identity":
		return protocol.NewOperatorCall("core.distinct-by-identity", 1)
	}
	return protocol.NewOperatorCall(operator, 1)
}

func argumentInt(argument string) int64 {
	number, ok := new(big.Int).SetString(argument, 10)
	if !ok {
		return 0
	}
	return number.Int64()
}

// RunSyntaxQueryTomlFace executes one `syntax.toml.*` case of the shared
// syntax-query suite (consema-rs/crates/consema-conformance/src/syntax_query_v1.rs
// run_toml). The handler is called by the shared syntax_query_v1.go runner
// for every case whose ID starts with "syntax.toml.".
func RunSyntaxQueryTomlFace(vector *caseData, report *SuiteReport) {
	profile, ok := stringField(vector.Input, "profile")
	if !ok || profile != "toml.1.0@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unknown TOML profile"})
		return
	}
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	executable, failure := tomlSyntaxDefinition(vector)
	if failure != nil {
		expectQueryFailure(vector, report, failure)
		return
	}
	ctx := context.Background()
	if cancelled, ok := booleanField(vector.Input, "cancelled"); ok && cancelled {
		cancelledContext, cancel := context.WithCancel(ctx)
		cancel()
		ctx = cancelledContext
	}
	limits := protocol.DefaultQueryLimits()
	if maxSteps, ok := integerField(vector.Input, "max_steps"); ok {
		limits.MaxSteps = int(maxSteps)
	}
	if maxResults, ok := integerField(vector.Input, "max_results"); ok {
		limits.MaxResults = int(maxResults)
	}
	matches, failure := toml.ExecuteTomlSyntaxQuery(ctx, executable, document, limits)
	if failure != nil {
		expectQueryFailure(vector, report, failure)
		return
	}
	compareSyntaxMatches(vector, report, document, matches)
}

// tomlSyntaxDefinition builds the lossless-syntax-query definition from the
// vector filters (consema-rs/crates/consema-conformance/src/syntax_query_v1.rs
// definition).
func tomlSyntaxDefinition(vector *caseData) (*protocol.ExecutableQuery, *protocol.QueryFailure) {
	filtersValue, ok := objectField(vector.Input, "filters")
	if !ok {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "filters"}
	}
	filters, ok := filtersValue.(*core.Array)
	if !ok {
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "filters"}
	}
	var branches []*protocol.QueryExpression
	for _, filterValue := range filters.Items() {
		filter, ok := filterValue.(*core.Object)
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "filter"}
		}
		operatorValue, ok := filter.Get("operator")
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "operator"}
		}
		operator, ok := operatorValue.(core.String)
		if !ok {
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "operator"}
		}
		var argument string
		if argumentValue, ok := filter.Get("argument"); ok {
			text, ok := argumentValue.(core.String)
			if !ok {
				return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
					Operator: string(operator), Argument: "argument"}
			}
			argument = string(text)
		}
		branches = append(branches,
			(&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
				Then(tomlSyntaxOperatorCall(string(operator), argument)))
	}
	var expression *protocol.QueryExpression
	switch combine, _ := stringField(vector.Input, "combine"); combine {
	case "Single", "":
		switch len(branches) {
		case 0:
			expression = &protocol.QueryExpression{Kind: protocol.ExpressionInput}
		case 1:
			expression = branches[0]
		default:
			return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
				Operator: "vector", Argument: "combine"}
		}
	case "StructureOrderMerge":
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge, Branches: branches}
	case "Concat":
		expression = &protocol.QueryExpression{Kind: protocol.ExpressionConcat, Branches: branches}
	default:
		return nil, &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
			Operator: "vector", Argument: "combine"}
	}
	definition := protocol.NewQueryDefinition(protocol.DomainTOMLLosslessSyntaxV1()).
		WithExpression(expression)
	selection, failure := tomlSelection(vector)
	if failure != nil {
		return nil, failure
	}
	definition = definition.WithSelection(selection)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, failure
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return validated.Bind(capabilities)
}

func tomlSelection(vector *caseData) (protocol.QuerySelection, *protocol.QueryFailure) {
	selection, _ := stringField(vector.Input, "selection")
	switch selection {
	case "", "All":
		return protocol.SelectionAll, nil
	case "First":
		return protocol.SelectionFirst, nil
	case "Last":
		return protocol.SelectionLast, nil
	case "ZeroOrOne":
		return protocol.SelectionZeroOrOne, nil
	case "RequireOne":
		return protocol.SelectionRequireOne, nil
	}
	return "", &protocol.QueryFailure{Kind: protocol.FailureInvalidArgument,
		Operator: "vector", Argument: "selection"}
}

// expectQueryFailure compares one query failure with the expected code.
func expectQueryFailure(vector *caseData, report *SuiteReport, failure *protocol.QueryFailure) {
	expected, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	if failure.Code() != expected {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: fmt.Sprintf("query failure code %s != %s", failure.Code(), expected)})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// compareSyntaxMatches compares the executed syntax matches with the
// expected facts (kind, text, ordinal, role) and the terminal state.
func compareSyntaxMatches(vector *caseData, report *SuiteReport, document *toml.Document,
	matches []toml.TomlSyntaxMatch) {
	expectedValue, ok := objectField(vector.Expected, "matches")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.matches"})
		return
	}
	expected, ok := expectedValue.(*core.Array)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected.matches must be Sequence"})
		return
	}
	if len(matches) != len(expected.Items()) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: fmt.Sprintf("match count differs: actual %d, expected %d", len(matches), len(expected.Items()))})
		return
	}
	source := document.Source().Bytes()
	for index, match := range matches {
		fields, ok := expected.Items()[index].(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected match must be Object"})
			return
		}
		kind, ok := stringField(fields, "kind")
		if !ok || kind != match.Kind().AsStr() {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected match.kind differs"})
			return
		}
		text, ok := stringField(fields, "text")
		if !ok || text != string(source[match.Span().StartByte():match.Span().EndByte()]) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected match.text differs"})
			return
		}
		ordinal, ok := integerField(fields, "ordinal")
		if !ok || uint64(match.Ordinal()) != ordinal {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected match.ordinal differs"})
			return
		}
		role, ok := stringField(fields, "role")
		if !ok || role != "TomlSyntaxPiece" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected match.role differs"})
			return
		}
	}
	terminal, ok := stringField(vector.Expected, "terminal")
	if !ok || terminal != "Completed" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected terminal differs"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// RunOperationsTOMLFace executes one TOML-operation case of the shared
// operations suite (consema-rs/crates/consema-conformance/src/operations_v1.rs
// run_case). The handler is called by the shared operations_v1.go runner
// for every case ID in the TOML dispatch list.
func RunOperationsTOMLFace(vector *caseData, report *SuiteReport) {
	switch vector.ID {
	case "operations.v1.operation-registry":
		runOperationRegistryPair(vector, report)
	case "operations.v1.materialize-toml-native":
		runMaterializeTomlNative(vector, report)
	case "operations.v1.materialize-toml-explicit-mapping",
		"operations.v1.materialize-toml-implicit-mapping-rejected":
		runMaterializeTomlMapping(vector, report)
	case "operations.v1.materialize-toml-null-rejected":
		runMaterializeTomlFailure(vector, report, core.NullValue())
	case "operations.v1.materialize-toml-output-limit":
		runMaterializeTomlLimit(vector, report)
	case "operations.v1.toml-root-insert":
		runTomlRootInsert(vector, report)
	case "operations.v1.toml-inline-rename":
		runTomlInlineRename(vector, report)
	case "operations.v1.toml-array-remove":
		runTomlArrayRemove(vector, report)
	case "operations.v1.toml-conflict-atomic":
		runTomlConflictAtomic(vector, report)
	case "operations.v1.toml-dry-run-proof-patch":
		runTomlDryRun(vector, report)
	case "operations.v1.toml-structural-matrix":
		runTomlStructuralMatrix(vector, report)
	case "operations.v1.toml-conflict-matrix":
		runTomlConflictMatrix(vector, report)
	default:
		report.Failed = append(report.Failed, CaseFailure{
			ID:      vector.ID,
			Message: "runner does not recognize published TOML operation case",
		})
	}
}

// tomlRequest builds the frozen TOML materialization request.
func tomlRequest(mappingPolicy document.MappingPolicy) document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)).
		WithNewline(document.NewlineLf).
		WithMappingPolicy(mappingPolicy)
}

func tomlProject(document *toml.Document) (core.Value, string) {
	result := document.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if result.Failed != nil {
		return nil, "TOML projection failed: " + result.Failed.Diagnostics[0].Code
	}
	return result.Complete.Value, ""
}

func runMaterializeTomlNative(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	doc, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	value, message := tomlProject(doc)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	result := toml.Materialize(value, tomlRequest(document.MappingPolicyRequireObject))
	if result.Failed != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unexpected materialization failure: " + result.Failed.Failure.Code()})
		return
	}
	reparsed, message := parseTomlBytes(result.Complete.Document.Render())
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	reprojected, message := tomlProject(reparsed)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	fidelity, ok := stringField(vector.Expected, "fidelity")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.fidelity"})
		return
	}
	minimum, ok := integerField(vector.Expected, "minimum_provenance_entries")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.minimum_provenance_entries"})
		return
	}
	reprojectsEqual, ok := booleanField(vector.Expected, "reprojects_equal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.reprojects_equal"})
		return
	}
	if string(result.Complete.Fidelity) != fidelity ||
		uint64(len(result.Complete.Provenance.Entries())) < minimum ||
		core.Equal(reprojected, value) != reprojectsEqual {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// runMaterializeTomlMapping covers the explicit-mapping and
// implicit-mapping-rejected cases; the input source is strict JSON
// projected as an EntryMapping (the JSON family projection; the mini
// decoder keeps the runner self-contained until the JSON package lands).
func runMaterializeTomlMapping(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	value, message := strictJSONValue([]byte(source), true)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	policyText, ok := stringField(vector.Input, "mapping_policy")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.mapping_policy"})
		return
	}
	var policy document.MappingPolicy
	switch policyText {
	case "RequireObject":
		policy = document.MappingPolicyRequireObject
	case "UniqueStringEntriesToObject":
		policy = document.MappingPolicyUniqueStringEntriesToObject
	default:
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "unknown mapping policy"})
		return
	}
	result := toml.Materialize(value, tomlRequest(policy))
	if result.Complete != nil {
		output, ok := stringField(vector.Expected, "output")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
			return
		}
		fidelity, ok := stringField(vector.Expected, "fidelity")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.fidelity"})
			return
		}
		eventCode, _ := stringField(vector.Expected, "event_code")
		hasEvent := false
		for _, event := range result.Complete.Report.Events() {
			if event.Code == eventCode {
				hasEvent = true
			}
		}
		if string(result.Complete.Document.Render()) != output ||
			string(result.Complete.Fidelity) != fidelity || !hasEvent {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "materialization output or report did not match"})
			return
		}
		report.Passed = append(report.Passed, vector.ID)
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.code"})
		return
	}
	hasDocument, ok := booleanField(vector.Expected, "has_document")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.has_document"})
		return
	}
	if result.Failed.Failure.Code() != code || hasDocument {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization failure facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runMaterializeTomlFailure(vector *caseData, report *SuiteReport, value core.Value) {
	result := toml.Materialize(value, tomlRequest(document.MappingPolicyRequireObject))
	if result.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unexpected materialization success"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.code"})
		return
	}
	hasDocument, ok := booleanField(vector.Expected, "has_document")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.has_document"})
		return
	}
	if result.Failed.Failure.Code() != code || hasDocument {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization failure facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runMaterializeTomlLimit(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	outputLimit, ok := integerField(vector.Input, "max_output_bytes")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.max_output_bytes"})
		return
	}
	doc, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	value, message := tomlProject(doc)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	request := tomlRequest(document.MappingPolicyRequireObject).WithLimits(
		document.MaterializationLimits{MaxOutputBytes: int(outputLimit)})
	result := toml.Materialize(value, request)
	if result.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unexpected materialization success"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.code"})
		return
	}
	hasDocument, ok := booleanField(vector.Expected, "has_document")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.has_document"})
		return
	}
	if result.Failed.Failure.Code() != code || hasDocument {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialization limit facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// tomlRootEntry finds one direct root entry by name.
func tomlRootEntry(document *toml.Document, name string) (toml.TomlEntry, string) {
	entries, ok := document.Root().TableEntries()
	if !ok {
		return toml.TomlEntry{}, "root is not a table"
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry, ""
		}
	}
	return toml.TomlEntry{}, "missing root entry " + name
}

func runTomlRootInsert(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	key, ok := stringField(vector.Input, "key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.key"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.InsertEntry(document.Root().NodeRef(), key, core.Boolean(true), toml.PlacementEnd())
	commit, failure := document.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
		return
	}
	if string(commit.Document.Render()) != output {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit output did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlInlineRename(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	tableName, ok := stringField(vector.Input, "table")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.table"})
		return
	}
	targetOrdinal, ok := integerField(vector.Input, "target_ordinal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.target_ordinal"})
		return
	}
	key, ok := stringField(vector.Input, "key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.key"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	tableEntry, message := tomlRootEntry(document, tableName)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	entries, ok := tableEntry.Item().TableEntries()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected inline table"})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.RenameEntry(entries[targetOrdinal].NodeRef(), key)
	commit, failure := document.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
		return
	}
	if string(commit.Document.Render()) != output {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit output did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlArrayRemove(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	arrayName, ok := stringField(vector.Input, "array")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.array"})
		return
	}
	targetOrdinal, ok := integerField(vector.Input, "target_ordinal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.target_ordinal"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	arrayEntry, message := tomlRootEntry(document, arrayName)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	elements, ok := arrayEntry.Item().ArrayElements()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected array"})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.RemoveArrayElement(elements[targetOrdinal].NodeRef())
	commit, failure := document.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
		return
	}
	if string(commit.Document.Render()) != output {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit output did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlConflictAtomic(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	key, ok := stringField(vector.Input, "key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.key"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.InsertEntry(document.Root().NodeRef(), key, core.Boolean(true), toml.PlacementStart())
	_, failure := document.Commit(builder.Build())
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "duplicate key edit must fail"})
		return
	}
	code, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.code"})
		return
	}
	baseUnchanged, ok := booleanField(vector.Expected, "base_unchanged")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.base_unchanged"})
		return
	}
	if failure.Code() != code || (string(document.Render()) == source) != baseUnchanged {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "conflict facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// verifyTomlCommit replays the patch and verifies the untouched proof.
func verifyTomlCommit(vector *caseData, report *SuiteReport, base, target *document.SourceSnapshot,
	patch *document.SourcePatch, proof toml.UntouchedByteProof, output []byte) bool {
	replayed, err := patch.Apply(base, document.DefaultSourcePatchLimits())
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "patch replay: " + err.Error()})
		return false
	}
	patchReplays := string(replayed.Bytes()) == string(output)
	proofVerifies := proof.Verify(base, target, patch.Replacements()) == nil
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
		return false
	}
	expectedPatchReplays, ok := booleanField(vector.Expected, "patch_replays")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.patch_replays"})
		return false
	}
	expectedProofVerifies, ok := booleanField(vector.Expected, "proof_verifies")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.proof_verifies"})
		return false
	}
	if string(output) != expected || patchReplays != expectedPatchReplays ||
		proofVerifies != expectedProofVerifies {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "commit verification facts did not match"})
		return false
	}
	return true
}

func runTomlDryRun(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	key, ok := stringField(vector.Input, "key")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.key"})
		return
	}
	value, ok := stringField(vector.Input, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.value"})
		return
	}
	sourceID, ok := stringField(vector.Input, "source_id")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source_id"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.InsertEntry(document.Root().NodeRef(), key, core.String(value), toml.PlacementEnd())
	transaction := builder.Build()
	planSourceID, err := toml.NewEditPlanSourceId(sourceID)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	plan, failure := document.DryRun(transaction, *planSourceID)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "dry run: " + failure.Name()})
		return
	}
	commit, failure := document.Commit(transaction)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "commit: " + failure.Name()})
		return
	}
	output, ok := stringField(vector.Expected, "output")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.output"})
		return
	}
	if string(commit.Document.Render()) != output {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit output did not match"})
		return
	}
	safe := true
	for _, operation := range plan.Operations() {
		for _, argument := range operation.Summary {
			if strings.Contains(argument, "secret") {
				safe = false
			}
		}
	}
	redacted, err := plan.WithAllReplacementsRedacted(true, true)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
		return
	}
	redactedLeaks := strings.Contains(redacted.String(), "secret")
	sameReplacements, ok := booleanField(vector.Expected, "same_replacements")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.same_replacements"})
		return
	}
	sameTargetDigest, ok := booleanField(vector.Expected, "same_target_digest")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.same_target_digest"})
		return
	}
	safeSummary, ok := booleanField(vector.Expected, "safe_summary")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.safe_summary"})
		return
	}
	redactedDebug, ok := booleanField(vector.Expected, "redacted_debug")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.redacted_debug"})
		return
	}
	replacementsMatch := reflect.DeepEqual(plan.Replacements(), commit.SourcePatch.Replacements())
	digestMatch := plan.TargetDigest().Equal(commit.SourcePatch.TargetDigest())
	if replacementsMatch != sameReplacements || digestMatch != sameTargetDigest ||
		safe != safeSummary || redactedLeaks != !redactedDebug {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "dry-run facts did not match"})
		return
	}
	if !verifyTomlCommit(vector, report, document.Source(), commit.Document.Source(),
		commit.SourcePatch, commit.UntouchedProof, commit.Document.Render()) {
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlStructuralMatrix(vector *caseData, report *SuiteReport) {
	cases, ok := sequenceField(vector.Input, "cases")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.cases"})
		return
	}
	completed := 0
	for _, item := range cases {
		fields, ok := item.(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item must be Object"})
			return
		}
		operation, ok := stringField(fields, "operation")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks operation"})
			return
		}
		source, ok := stringField(fields, "source")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks source"})
			return
		}
		document, message := parseTomlBytes([]byte(source))
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		builder := toml.NewEditTransactionBuilder(document)
		switch operation {
		case "insert-standard-table":
			tableName, ok := stringField(fields, "table")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks table"})
				return
			}
			key, ok := stringField(fields, "key")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks key"})
				return
			}
			tableEntry, message := tomlRootEntry(document, tableName)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder.InsertEntry(tableEntry.Item().NodeRef(), key, core.String("localhost"), toml.PlacementEnd())
		case "insert-inline":
			tableName, ok := stringField(fields, "table")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks table"})
				return
			}
			key, ok := stringField(fields, "key")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks key"})
				return
			}
			beforeOrdinal, ok := integerField(fields, "before_ordinal")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks before_ordinal"})
				return
			}
			tableEntry, message := tomlRootEntry(document, tableName)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			entries, ok := tableEntry.Item().TableEntries()
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected inline table"})
				return
			}
			builder.InsertEntry(tableEntry.Item().NodeRef(), key,
				core.NewArray(core.Boolean(true)), toml.PlacementBefore(entries[beforeOrdinal].NodeRef()))
		case "remove-inline":
			tableName, ok := stringField(fields, "table")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks table"})
				return
			}
			targetOrdinal, ok := integerField(fields, "target_ordinal")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks target_ordinal"})
				return
			}
			tableEntry, message := tomlRootEntry(document, tableName)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			entries, ok := tableEntry.Item().TableEntries()
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected inline table"})
				return
			}
			builder.RemoveEntry(entries[targetOrdinal].NodeRef())
		case "insert-array-start":
			arrayName, ok := stringField(fields, "array")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks array"})
				return
			}
			arrayEntry, message := tomlRootEntry(document, arrayName)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder.InsertArrayElement(arrayEntry.Item().NodeRef(),
				core.NewInteger(big.NewInt(1)), toml.PlacementStart())
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown TOML matrix operation: " + operation})
			return
		}
		commit, failure := document.Commit(builder.Build())
		if failure != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "matrix edit failed: " + failure.Name()})
			return
		}
		expected, ok := stringField(fields, "expected")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks expected"})
			return
		}
		if string(commit.Document.Render()) != expected {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "TOML matrix output mismatch for " + operation})
			return
		}
		completed++
	}
	expectedCompleted, ok := integerField(vector.Expected, "completed")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.completed"})
		return
	}
	if uint64(completed) != expectedCompleted {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "matrix completion count did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlConflictMatrix(vector *caseData, report *SuiteReport) {
	cases, ok := sequenceField(vector.Input, "cases")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.cases"})
		return
	}
	failedAtomically := 0
	for _, item := range cases {
		fields, ok := item.(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item must be Object"})
			return
		}
		mode, ok := stringField(fields, "mode")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks mode"})
			return
		}
		source, ok := stringField(fields, "source")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks source"})
			return
		}
		document, message := parseTomlBytes([]byte(source))
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		var failure *toml.EditFailure
		switch mode {
		case "duplicate-target":
			entry, message := tomlRootEntry(document, "a")
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder := toml.NewEditTransactionBuilder(document)
			builder.RenameEntry(entry.NodeRef(), "x")
			builder.RemoveEntry(entry.NodeRef())
			_, failure = document.Commit(builder.Build())
		case "removed-anchor":
			entry, message := tomlRootEntry(document, "a")
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder := toml.NewEditTransactionBuilder(document)
			builder.RemoveEntry(entry.NodeRef())
			builder.InsertEntry(document.Root().NodeRef(), "x", core.Boolean(true),
				toml.PlacementBefore(entry.NodeRef()))
			_, failure = document.Commit(builder.Build())
		case "ancestor-descendant":
			entry, message := tomlRootEntry(document, "a")
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder := toml.NewEditTransactionBuilder(document)
			builder.SemanticScalar(entry.Item().NodeRef(), core.NewInteger(big.NewInt(3)),
				toml.RepresentationPreserveCompatible)
			builder.RemoveEntry(entry.NodeRef())
			_, failure = document.Commit(builder.Build())
		case "unsupported-table-remove":
			entry, message := tomlRootEntry(document, "service")
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			builder := toml.NewEditTransactionBuilder(document)
			builder.RemoveEntry(entry.NodeRef())
			_, failure = document.Commit(builder.Build())
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown TOML conflict mode: " + mode})
			return
		}
		if failure == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "TOML conflict mode " + mode + " unexpectedly completed"})
			return
		}
		code, ok := stringField(fields, "code")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "matrix item lacks code"})
			return
		}
		if failure.Code() != code || string(document.Render()) != source {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "TOML conflict mismatch for " + mode})
			return
		}
		failedAtomically++
	}
	expectedFailed, ok := integerField(vector.Expected, "failed_atomically")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.failed_atomically"})
		return
	}
	if uint64(failedAtomically) != expectedFailed {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "matrix failure count did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// strictJSONValue decodes one strict-JSON text into the core value model
// with exact number preservation; when asMapping is set, every object
// becomes an EntryMapping (the ProjectAsEntryMapping face of the
// materialize-toml cases; the decoder keeps the runner self-contained
// until the JSON family package lands).
func strictJSONValue(bytes []byte, asMapping bool) (core.Value, string) {
	decoder := json.NewDecoder(strings.NewReader(string(bytes)))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, "input source is not strict JSON: " + err.Error()
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, "input source has trailing content"
	}
	value, err := strictJSONConvert(raw, asMapping)
	if err != nil {
		return nil, err.Error()
	}
	return value, ""
}

func strictJSONConvert(raw any, asMapping bool) (core.Value, error) {
	switch item := raw.(type) {
	case nil:
		return core.NullValue(), nil
	case bool:
		return core.Boolean(item), nil
	case string:
		return core.String(item), nil
	case json.Number:
		return strictJSONNumber(item.String())
	case []any:
		items := make([]core.Value, 0, len(item))
		for _, element := range item {
			converted, err := strictJSONConvert(element, asMapping)
			if err != nil {
				return nil, err
			}
			items = append(items, converted)
		}
		return core.NewArray(items...), nil
	case map[string]any:
		keys := make([]string, 0, len(item))
		for key := range item {
			keys = append(keys, key)
		}
		sortStrings(keys)
		entries := make([]core.Entry, 0, len(keys))
		mappingEntries := make([]core.EntryMappingEntry, 0, len(keys))
		for _, key := range keys {
			converted, err := strictJSONConvert(item[key], asMapping)
			if err != nil {
				return nil, err
			}
			entries = append(entries, core.Entry{Key: key, Value: converted})
			mappingEntries = append(mappingEntries, core.EntryMappingEntry{Key: core.String(key), Value: converted})
		}
		if asMapping {
			return core.NewEntryMapping(mappingEntries...)
		}
		return core.NewObject(entries...)
	}
	return nil, fmt.Errorf("unsupported JSON value %T", raw)
}

func strictJSONNumber(text string) (core.Value, error) {
	if integer, ok := new(big.Int).SetString(text, 10); ok {
		return core.NewInteger(integer), nil
	}
	coefficientText := text
	exponent := big.NewInt(0)
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		exponentText := text[index+1:]
		coefficientText = text[:index]
		parsed, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return nil, fmt.Errorf("JSON number %q is not exact", text)
		}
		exponent = parsed
	}
	scale := big.NewInt(0)
	if index := strings.IndexByte(coefficientText, '.'); index >= 0 {
		fraction := coefficientText[index+1:]
		coefficientText = coefficientText[:index] + fraction
		scale = big.NewInt(int64(-len(fraction)))
	}
	coefficient, ok := new(big.Int).SetString(coefficientText, 10)
	if !ok {
		return nil, fmt.Errorf("JSON number %q is not exact", text)
	}
	exponent.Add(exponent, scale)
	return core.NewDecimal(coefficient, exponent), nil
}

func sortStrings(values []string) {
	for index := 1; index < len(values); index++ {
		for position := index; position > 0 && values[position] < values[position-1]; position-- {
			values[position], values[position-1] = values[position-1], values[position]
		}
	}
}

// runOperationRegistryPair compares the JSON and TOML format operation
// registries against the vector facts (operations_v1.rs
// operation_registry).
func runOperationRegistryPair(vector *caseData, report *SuiteReport) {
	jsonRegistry := jsonpkg.FormatOperationRegistryFor(jsonpkg.JsonProfileStrictV1)
	tomlRegistry := toml.NewFormatOperationRegistry(toml.Toml10V1)
	jsonCount, ok := integerField(vector.Expected, "json_operation_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.json_operation_count"})
		return
	}
	tomlCount, ok := integerField(vector.Expected, "toml_operation_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.toml_operation_count"})
		return
	}
	requiredJSON, ok := stringField(vector.Expected, "required_json")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.required_json"})
		return
	}
	requiredTOML, ok := stringField(vector.Expected, "required_toml")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.required_toml"})
		return
	}
	jsonOperations := jsonRegistry.Operations()
	tomlOperations := tomlRegistry.Operations()
	hasJSON, hasTOML := false, false
	for _, operation := range jsonOperations {
		if operation.ID() == requiredJSON {
			hasJSON = true
		}
	}
	for _, operation := range tomlOperations {
		if operation.ID.ID()+"@"+fmt.Sprint(operation.ID.Version()) == requiredTOML {
			hasTOML = true
		}
	}
	if uint64(len(jsonOperations)) != jsonCount || uint64(len(tomlOperations)) != tomlCount ||
		!hasJSON || !hasTOML {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "operation registry facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}
