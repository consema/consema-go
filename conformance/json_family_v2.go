package conformance

// The `consema.json-family.conformance@2` suite runner, mirroring
// crates/consema-conformance/src/json_family_v2.rs. The 0.15.0 milestone
// G1.2 implements the whole JSON-family surface (json5.standard@1
// formation, json.lossless-syntax-query@2, json.native-semantic-query@2,
// json5.projection.best-exact-core@1, json5.canonical-compact@1,
// json.edit.move-member@1, json.edit.replace-scalar-semantic@1,
// core.parse.limits@1) through the json package, so the formation, query,
// projection, materialization, edit, registry, and security cases
// execute. The three conversion cases (json5.convert.*) publish the
// core.conversion@1 capability, which lands with 0.15.0 G1.4 (the root
// package Convert* composition); they remain documented skips.

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

// runJsonFamilyV2 executes the `consema.json-family.conformance@2` suite.
func runJsonFamilyV2(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		action, ok := stringField(vector.Input, "action")
		if !ok {
			failJSONCase(vector, report, "missing input.action")
			continue
		}
		switch action {
		case "parse":
			runJSONFamilyParseCase(vector, report)
		case "syntax-query":
			runJSONFamilySyntaxQueryCase(vector, report)
		case "native-query":
			runJSONFamilyNativeQueryCase(vector, report)
		case "project":
			runJSONFamilyProjectionCase(vector, report)
		case "materialize":
			runJSONFamilyMaterializationCase(vector, report)
		case "convert":
			RunOperationsConvertFace(vector, report)
		case "move-member":
			runJSONFamilyMoveMemberCase(vector, report)
		case "edit-scalars":
			runJSONFamilyEditScalarsCase(vector, report)
		case "registry-v4":
			runJSONFamilyRegistryV4Case(vector, report)
		case "parse-limit":
			runJSONFamilyParseLimitCase(vector, report)
		default:
			failJSONCase(vector, report, "unknown input.action "+action)
		}
	}
	return report
}

func runJSONFamilyParseCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		failJSONCase(vector, report, "missing input.profile")
		return
	}
	profile, ok := parseJSONProfile(profileName)
	if !ok {
		failJSONCase(vector, report, "unknown profile "+profileName)
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	if string(doc.Render()) != source {
		failJSONCase(vector, report, "render differs from source")
		return
	}
	if expected, ok := stringField(vector.Expected, "formation"); ok {
		if doc.FormationStatus().String() != expected {
			failJSONCase(vector, report, fmt.Sprintf("formation %s != %s",
				doc.FormationStatus(), expected))
			return
		}
	}
	if codes, ok := jsonExpectedStrings(vector, "diagnostic_contains"); ok {
		for _, code := range codes {
			if !jsonHasDiagnostic(doc, code) {
				failJSONCase(vector, report, "missing diagnostic "+code)
				return
			}
		}
	}
	if kinds, ok := jsonExpectedStrings(vector, "syntax_contains"); ok {
		for _, kind := range kinds {
			if !jsonHasSyntaxKind(doc, kind) {
				failJSONCase(vector, report, "missing syntax kind "+kind)
				return
			}
		}
	}
	root := doc.Root()
	if expected, ok := stringField(vector.Expected, "root_kind"); ok {
		kind := root.Kind()
		if !kind.IsAvailable() || kind.Value().String() != expected {
			failJSONCase(vector, report, fmt.Sprintf("root kind %v != %s", kind, expected))
			return
		}
	}
	if expected, ok := stringField(vector.Expected, "root_bits"); ok {
		binary := root.AsBinaryFloat64()
		if !binary.IsAvailable() {
			failJSONCase(vector, report, "root is not BinaryFloat64")
			return
		}
		if got := fmt.Sprintf("%016x", binary.Value().Bits()); got != expected {
			failJSONCase(vector, report, fmt.Sprintf("root bits %s != %s", got, expected))
			return
		}
	}
	if expected, ok := stringField(vector.Expected, "root_integer"); ok {
		integer := root.AsInteger()
		if !integer.IsAvailable() || integer.Value().String() != expected {
			failJSONCase(vector, report, "root integer mismatch")
			return
		}
	}
	if jsonHasField(vector.Expected, "member_names") || jsonHasField(vector.Expected, "member_kinds") {
		members := root.ObjectMembers()
		if !members.IsAvailable() {
			failJSONCase(vector, report, "root is not an object")
			return
		}
		actual := members.Value()
		if expected, ok := jsonExpectedStrings(vector, "member_names"); ok {
			names := make([]string, 0, len(actual))
			for _, member := range actual {
				name := member.Name()
				if !name.IsAvailable() {
					failJSONCase(vector, report, "member name unavailable")
					return
				}
				names = append(names, *name.Value())
			}
			if strings.Join(names, "\x00") != strings.Join(expected, "\x00") {
				failJSONCase(vector, report, fmt.Sprintf("member names %v != %v", names, expected))
				return
			}
		}
		if expected, ok := jsonExpectedStrings(vector, "member_kinds"); ok {
			kinds := make([]string, 0, len(actual))
			for _, member := range actual {
				kind := member.Value().Kind()
				if !kind.IsAvailable() {
					failJSONCase(vector, report, "member kind unavailable")
					return
				}
				kinds = append(kinds, kind.Value().String())
			}
			if strings.Join(kinds, "\x00") != strings.Join(expected, "\x00") {
				failJSONCase(vector, report, fmt.Sprintf("member kinds %v != %v", kinds, expected))
				return
			}
		}
	}
	if jsonHasField(vector.Expected, "element_kinds") || jsonHasField(vector.Expected, "element_strings") ||
		jsonHasField(vector.Expected, "element_decimals") {
		elements := root.ArrayElements()
		if !elements.IsAvailable() {
			failJSONCase(vector, report, "root is not an array")
			return
		}
		actual := elements.Value()
		values := make([]json.JsonValue, 0, len(actual))
		for _, element := range actual {
			values = append(values, element.Value())
		}
		if expected, ok := jsonExpectedStrings(vector, "element_kinds"); ok {
			kinds := make([]string, 0, len(values))
			for _, value := range values {
				kind := value.Kind()
				if !kind.IsAvailable() {
					failJSONCase(vector, report, "element kind unavailable")
					return
				}
				kinds = append(kinds, kind.Value().String())
			}
			if strings.Join(kinds, "\x00") != strings.Join(expected, "\x00") {
				failJSONCase(vector, report, fmt.Sprintf("element kinds %v != %v", kinds, expected))
				return
			}
		}
		if expected, ok := jsonExpectedStrings(vector, "element_strings"); ok {
			strings_ := make([]string, 0, len(values))
			for _, value := range values {
				text := value.AsString()
				if !text.IsAvailable() {
					failJSONCase(vector, report, "element is not a string")
					return
				}
				strings_ = append(strings_, *text.Value())
			}
			if strings.Join(strings_, "\x00") != strings.Join(expected, "\x00") {
				failJSONCase(vector, report, fmt.Sprintf("element strings %v != %v", strings_, expected))
				return
			}
		}
		if jsonHasField(vector.Expected, "element_decimals") {
			pairs := make([]string, 0, len(values))
			for _, value := range values {
				decimal := value.AsDecimal()
				if !decimal.IsAvailable() {
					failJSONCase(vector, report, "element is not a decimal")
					return
				}
				pairs = append(pairs, decimal.Value().Coefficient().String()+"\x00"+decimal.Value().Exponent().String())
			}
			expected := jsonExpectedDecimalPairs(vector)
			if strings.Join(pairs, "\x01") != strings.Join(expected, "\x01") {
				failJSONCase(vector, report, fmt.Sprintf("element decimals %v != %v", pairs, expected))
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilySyntaxQueryCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	kindName, ok := stringField(vector.Input, "kind")
	if !ok {
		failJSONCase(vector, report, "missing input.kind")
		return
	}
	doc, formationFailure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJson5StandardV1, document.DefaultParseLimits())
	if formationFailure != nil {
		failJSONCase(vector, report, formationFailure.Error())
		return
	}
	query, bindFailure := jsonSyntaxV2Query(kindName)
	if bindFailure != nil {
		failJSONCase(vector, report, bindFailure.Error())
		return
	}
	matches, queryFailure := json.ExecuteJSONSyntaxQuery(context.Background(), query, doc,
		protocol.DefaultQueryLimits())
	if queryFailure != nil {
		failJSONCase(vector, report, queryFailure.Error())
		return
	}
	text, _ := doc.Source().DecodedText()
	actual := make([]string, 0, len(matches))
	for _, match := range matches {
		actual = append(actual, text[match.Span().StartByte():match.Span().EndByte()])
	}
	expected, ok := jsonExpectedStrings(vector, "texts")
	if !ok {
		failJSONCase(vector, report, "missing expected.texts")
		return
	}
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		failJSONCase(vector, report, fmt.Sprintf("texts %v != %v", actual, expected))
		return
	}
	if rejected, ok := booleanField(vector.Expected, "v1_rejected"); ok && rejected {
		v1Query, failure := jsonSyntaxV1Query()
		if failure != nil {
			failJSONCase(vector, report, failure.Error())
			return
		}
		_, queryFailure := json.ExecuteJSONSyntaxQuery(context.Background(), v1Query, doc,
			protocol.DefaultQueryLimits())
		if queryFailure == nil || queryFailure.Kind != protocol.FailureDomainMismatch {
			failJSONCase(vector, report, "v1 query must be domain-rejected on JSON5")
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyNativeQueryCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJson5StandardV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	executable, bindFailure := bindQuery(protocol.DomainJSONNativeV2(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
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
	if len(matches) != 1 || matches[0].Kind != json.JsonMatchValue || matches[0].ValueKind == nil {
		failJSONCase(vector, report, "native v2 root result is not one available value")
		return
	}
	expected, ok := stringField(vector.Expected, "kind")
	if !ok {
		failJSONCase(vector, report, "missing expected.kind")
		return
	}
	if matches[0].ValueKind.String() != expected {
		failJSONCase(vector, report, fmt.Sprintf("kind %s != %s", matches[0].ValueKind, expected))
		return
	}
	if rejected, ok := booleanField(vector.Expected, "v1_rejected"); ok && rejected {
		v1, bindFailure := bindQuery(protocol.DomainJSONNativeV1(),
			&protocol.QueryExpression{Kind: protocol.ExpressionInput})
		if bindFailure != nil {
			failJSONCase(vector, report, bindFailure.Error())
			return
		}
		_, queryFailure := json.ExecuteJSONQuery(context.Background(), v1, doc,
			protocol.DefaultQueryLimits())
		if queryFailure == nil || queryFailure.Kind != protocol.FailureDomainMismatch {
			failJSONCase(vector, report, "v1 query must be domain-rejected on JSON5")
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyProjectionCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	targetName, ok := stringField(vector.Input, "target")
	if !ok {
		failJSONCase(vector, report, "missing input.target")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJson5StandardV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	var target json.ProjectionTarget
	switch targetName {
	case "json5-best-exact":
		target = json.ProjectionTargetJson5BestExactCoreV1
	case "json-best-exact":
		target = json.ProjectionTargetBestExactCoreV1
	default:
		failJSONCase(vector, report, "unknown projection target "+targetName)
		return
	}
	request, buildFailure := json.NewProjectionRequestBuilder(target).Build()
	if buildFailure != nil {
		failJSONCase(vector, report, buildFailure.Error())
		return
	}
	result := doc.Project(request)
	complete, _ := booleanField(vector.Expected, "complete")
	if result.Complete != nil {
		if !complete {
			failJSONCase(vector, report, "projection unexpectedly completed")
			return
		}
		expectedKind, ok := stringField(vector.Expected, "kind")
		if !ok {
			failJSONCase(vector, report, "missing expected.kind")
			return
		}
		if result.Complete.Value.Kind().String() != expectedKind {
			failJSONCase(vector, report, fmt.Sprintf("kind %s != %s",
				result.Complete.Value.Kind(), expectedKind))
			return
		}
		if jsonHasField(vector.Expected, "binary_bits") {
			mapping, ok := result.Complete.Value.(*core.EntryMapping)
			if !ok {
				failJSONCase(vector, report, "projection is not EntryMapping")
				return
			}
			expected, _ := jsonExpectedStrings(vector, "binary_bits")
			actual := make([]string, 0, mapping.Len())
			for _, entry := range mapping.Entries() {
				binary, ok := entry.Value.(core.BinaryFloat64)
				if !ok {
					failJSONCase(vector, report, "entry is not BinaryFloat64")
					return
				}
				actual = append(actual, fmt.Sprintf("%016x", binary.Bits()))
			}
			if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
				failJSONCase(vector, report, fmt.Sprintf("binary bits %v != %v", actual, expected))
				return
			}
		}
	} else {
		if complete {
			failJSONCase(vector, report, "projection unexpectedly failed")
			return
		}
		expectedCode, ok := stringField(vector.Expected, "code")
		if !ok {
			failJSONCase(vector, report, "missing expected.code")
			return
		}
		if !jsonDiagnosticCodePresent(result.Failed.Diagnostics, expectedCode) {
			failJSONCase(vector, report, "missing failure code "+expectedCode)
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyMaterializationCase(vector *caseData, report *SuiteReport) {
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		failJSONCase(vector, report, "missing input.profile")
		return
	}
	style, ok := stringField(vector.Input, "style")
	if !ok {
		failJSONCase(vector, report, "missing input.style")
		return
	}
	values, ok := sequenceField(vector.Input, "values")
	if !ok {
		failJSONCase(vector, report, "missing input.values")
		return
	}
	items := make([]core.Value, 0, len(values))
	for _, value := range values {
		converted, ok := jsonMaterializationValue(value)
		if !ok {
			failJSONCase(vector, report, "unrepresentable materialization value")
			return
		}
		items = append(items, converted)
	}
	profile, ok := parseJSONProfile(profileName)
	if !ok {
		failJSONCase(vector, report, "unknown profile "+profileName)
		return
	}
	request := document.NewMaterializationRequest(profile.ID(),
		document.NewMaterializationStyleId(style, 1)).WithNewline(document.NewlineNone)
	result := json.Materialize(core.NewArray(items...), request)
	if result.Complete != nil {
		expected, ok := stringField(vector.Expected, "output")
		if !ok {
			failJSONCase(vector, report, "missing expected.output")
			return
		}
		if string(result.Complete.Document.Render()) != expected {
			failJSONCase(vector, report, "output mismatch")
			return
		}
	} else {
		expectedFailure, ok := stringField(vector.Expected, "failure")
		if !ok {
			failJSONCase(vector, report, "missing expected.failure")
			return
		}
		if jsonMaterializationFailureName(result.Failed.Failure) != expectedFailure {
			failJSONCase(vector, report, fmt.Sprintf("failure %s != %s",
				jsonMaterializationFailureName(result.Failed.Failure), expectedFailure))
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyMoveMemberCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	profileName, ok := stringField(vector.Input, "profile")
	if !ok {
		failJSONCase(vector, report, "missing input.profile")
		return
	}
	profile, ok := parseJSONProfile(profileName)
	if !ok {
		failJSONCase(vector, report, "unknown profile "+profileName)
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	targetPath, ok := jsonOrdinalPath(vector, "target_path")
	if !ok {
		failJSONCase(vector, report, "missing input.target_path")
		return
	}
	target, ok := jsonResolveMember(doc, targetPath)
	if !ok {
		failJSONCase(vector, report, "target path does not resolve")
		return
	}
	placementName, ok := stringField(vector.Input, "placement")
	if !ok {
		failJSONCase(vector, report, "missing input.placement")
		return
	}
	var placement json.AssociationPlacement
	switch placementName {
	case "start":
		placement = json.PlacementAtStart()
	case "end":
		placement = json.PlacementAtEnd()
	case "before", "after":
		anchorPath, ok := jsonOrdinalPath(vector, "anchor_path")
		if !ok {
			failJSONCase(vector, report, "missing input.anchor_path")
			return
		}
		anchor, ok := jsonResolveMember(doc, anchorPath)
		if !ok {
			failJSONCase(vector, report, "anchor path does not resolve")
			return
		}
		if placementName == "before" {
			placement = json.BeforeAnchor(anchor.NodeRef())
		} else {
			placement = json.AfterAnchor(anchor.NodeRef())
		}
	default:
		failJSONCase(vector, report, "unknown placement "+placementName)
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	builder.MoveMember(target.NodeRef(), placement)
	tx := builder.Build()
	commit, editFailure := doc.Commit(tx)
	if editFailure != nil {
		expected, ok := stringField(vector.Expected, "failure")
		if !ok {
			failJSONCase(vector, report, "missing expected.failure")
			return
		}
		if editFailure.Name() != expected {
			failJSONCase(vector, report, fmt.Sprintf("failure %s != %s", editFailure.Name(), expected))
			return
		}
		report.Passed = append(report.Passed, vector.ID)
		return
	}
	expected, ok := stringField(vector.Expected, "output")
	if !ok {
		failJSONCase(vector, report, "missing expected.output")
		return
	}
	if string(commit.Document.Render()) != expected {
		failJSONCase(vector, report, "output mismatch")
		return
	}
	plan, dryFailure := doc.DryRun(tx, "conformance.json5")
	if dryFailure != nil {
		failJSONCase(vector, report, dryFailure.Error())
		return
	}
	patchEqual, _ := booleanField(vector.Expected, "patch_equal")
	if sameJSONReplacements(plan.Replacements(), commit.SourcePatch.Replacements()) != patchEqual ||
		plan.TargetDigest() != commit.SourcePatch.TargetDigest() {
		failJSONCase(vector, report, "plan and commit artifacts differ")
		return
	}
	proofValid, _ := booleanField(vector.Expected, "proof_valid")
	if (commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		commit.SourcePatch.Replacements()) == nil) != proofValid {
		failJSONCase(vector, report, "untouched proof verdict differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyEditScalarsCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	doc, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJson5StandardV1, document.DefaultParseLimits())
	if failure != nil {
		failJSONCase(vector, report, failure.Error())
		return
	}
	members := doc.Root().ObjectMembers()
	if !members.IsAvailable() {
		failJSONCase(vector, report, "root is not an object")
		return
	}
	replacements, ok := sequenceField(vector.Input, "replacements")
	if !ok {
		failJSONCase(vector, report, "missing input.replacements")
		return
	}
	builder := json.NewEditTransactionBuilder(doc)
	for _, item := range replacements {
		ordinal, ok := integerField(item, "ordinal")
		if !ok {
			failJSONCase(vector, report, "replacement ordinal is invalid")
			return
		}
		value, ok := jsonScalarReplacement(item)
		if !ok {
			failJSONCase(vector, report, "replacement value is invalid")
			return
		}
		all := members.Value()
		if int(ordinal) >= len(all) {
			failJSONCase(vector, report, "replacement ordinal out of range")
			return
		}
		builder.SemanticScalar(all[int(ordinal)].ValueNodeRef(), value,
			json.RepresentationPolicyPreserveCompatible)
	}
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
		failJSONCase(vector, report, "output mismatch")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyRegistryV4Case(vector *caseData, report *SuiteReport) {
	contracts := protocol.NewContractRegistry(protocol.RegistryV4)
	v4 := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV4)
	v3 := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV3)
	contractCount, ok := integerField(vector.Expected, "contract_count")
	if !ok {
		failJSONCase(vector, report, "missing expected.contract_count")
		return
	}
	errorCount, ok := integerField(vector.Expected, "error_code_count")
	if !ok {
		failJSONCase(vector, report, "missing expected.error_code_count")
		return
	}
	v3ErrorCount, ok := integerField(vector.Expected, "v3_error_code_count")
	if !ok {
		failJSONCase(vector, report, "missing expected.v3_error_code_count")
		return
	}
	newCode, ok := stringField(vector.Expected, "new_code")
	if !ok {
		failJSONCase(vector, report, "missing expected.new_code")
		return
	}
	if uint64(len(contracts.Contracts())) != contractCount ||
		uint64(len(v4.Codes())) != errorCount ||
		uint64(len(v3.Codes())) != v3ErrorCount ||
		!v4.Contains(newCode) || v3.Contains(newCode) {
		failJSONCase(vector, report, "registry facts differ")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runJSONFamilyParseLimitCase(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		failJSONCase(vector, report, "missing input.source")
		return
	}
	maxDepth, ok := integerField(vector.Input, "max_depth")
	if !ok {
		failJSONCase(vector, report, "missing input.max_depth")
		return
	}
	limits := document.DefaultParseLimits()
	limits.MaxNestingDepth = int(maxDepth)
	_, failure := json.Parse(context.Background(), []byte(source),
		json.JsonProfileJson5StandardV1, limits)
	fatal, _ := booleanField(vector.Expected, "fatal")
	if (failure != nil) != fatal {
		failJSONCase(vector, report, "fatal verdict differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// bindQuery validates and binds one JSON-domain query.
func bindQuery(domain *protocol.QueryDomain,
	expression *protocol.QueryExpression) (*protocol.ExecutableQuery, error) {
	validated, failure := protocol.NewQueryDefinition(domain).WithExpression(expression).Validate()
	if failure != nil {
		return nil, fmt.Errorf("validate: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, fmt.Errorf("bind: %v", failure)
	}
	return executable, nil
}

func jsonSyntaxV2Query(kind string) (*protocol.ExecutableQuery, error) {
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", core.String(kind)))
	return bindQuery(protocol.DomainJSONLosslessSyntaxV2(), expression)
}

func jsonSyntaxV1Query() (*protocol.ExecutableQuery, error) {
	return bindQuery(protocol.DomainJSONLosslessSyntaxV1(),
		&protocol.QueryExpression{Kind: protocol.ExpressionInput})
}

func parseJSONProfile(name string) (json.JsonProfile, bool) {
	switch name {
	case "json.strict@1":
		return json.JsonProfileStrictV1, true
	case "jsonc.bounded@1":
		return json.JsonProfileJsoncBoundedV1, true
	case "json5.standard@1":
		return json.JsonProfileJson5StandardV1, true
	}
	return json.JsonProfileStrictV1, false
}

func jsonHasDiagnostic(doc *json.Document, code string) bool {
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func jsonDiagnosticCodePresent(diagnostics []*protocol.Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func jsonHasSyntaxKind(doc *json.Document, name string) bool {
	for _, kind := range doc.LosslessSyntaxKinds() {
		if kind.AsStr() == name {
			return true
		}
	}
	return false
}

func jsonHasField(value core.Value, name string) bool {
	_, ok := objectField(value, name)
	return ok
}

func jsonExpectedStrings(vector *caseData, name string) ([]string, bool) {
	values, ok := sequenceField(vector.Expected, name)
	if !ok {
		return nil, false
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(core.String)
		if !ok {
			return nil, false
		}
		output = append(output, string(text))
	}
	return output, true
}

func jsonExpectedDecimalPairs(vector *caseData) []string {
	values, ok := sequenceField(vector.Expected, "element_decimals")
	if !ok {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		pair, ok := value.(*core.Array)
		if !ok || pair.Len() != 2 {
			return nil
		}
		coefficient, ok := pair.At(0).(core.String)
		if !ok {
			return nil
		}
		exponent, ok := pair.At(1).(core.String)
		if !ok {
			return nil
		}
		output = append(output, string(coefficient)+"\x00"+string(exponent))
	}
	return output
}

func jsonMaterializationValue(value core.Value) (core.Value, bool) {
	if bits, ok := stringField(value, "bits"); ok {
		parsed, ok := new(big.Int).SetString(bits, 16)
		if !ok || parsed.BitLen() > 64 || parsed.Sign() < 0 {
			return nil, false
		}
		return core.NewBinaryFloat64(parsed.Uint64()), true
	}
	if text, ok := stringField(value, "string"); ok {
		return core.String(text), true
	}
	if flag, ok := booleanField(value, "null"); ok && flag {
		return core.NullValue(), true
	}
	return nil, false
}

func jsonScalarReplacement(value core.Value) (core.Value, bool) {
	if text, ok := stringField(value, "integer"); ok {
		integer, ok := new(big.Int).SetString(text, 10)
		if !ok {
			return nil, false
		}
		return core.NewInteger(integer), true
	}
	if coefficientText, ok := stringField(value, "decimal_coefficient"); ok {
		exponentText, ok := stringField(value, "decimal_exponent")
		if !ok {
			return nil, false
		}
		coefficient, ok := new(big.Int).SetString(coefficientText, 10)
		if !ok {
			return nil, false
		}
		exponent, ok := new(big.Int).SetString(exponentText, 10)
		if !ok {
			return nil, false
		}
		return core.NewDecimal(coefficient, exponent), true
	}
	if text, ok := stringField(value, "string"); ok {
		return core.String(text), true
	}
	if bits, ok := stringField(value, "bits"); ok {
		parsed, ok := new(big.Int).SetString(bits, 16)
		if !ok || parsed.BitLen() > 64 || parsed.Sign() < 0 {
			return nil, false
		}
		return core.NewBinaryFloat64(parsed.Uint64()), true
	}
	return nil, false
}

func jsonOrdinalPath(vector *caseData, name string) ([]int, bool) {
	values, ok := sequenceField(vector.Input, name)
	if !ok {
		return nil, false
	}
	path := make([]int, 0, len(values))
	for _, value := range values {
		integer, ok := value.(core.Integer)
		if !ok {
			return nil, false
		}
		number := integer.Int()
		if !number.IsInt64() || number.Sign() < 0 {
			return nil, false
		}
		path = append(path, int(number.Int64()))
	}
	return path, true
}

func jsonResolveMember(doc *json.Document, path []int) (json.JsonObjectMember, bool) {
	if len(path) == 0 {
		return json.JsonObjectMember{}, false
	}
	value := doc.Root()
	for depth, ordinal := range path {
		members := value.ObjectMembers()
		if !members.IsAvailable() {
			return json.JsonObjectMember{}, false
		}
		all := members.Value()
		if ordinal >= len(all) {
			return json.JsonObjectMember{}, false
		}
		if depth+1 == len(path) {
			return all[ordinal], true
		}
		value = all[ordinal].Value()
	}
	return json.JsonObjectMember{}, false
}

func jsonMaterializationFailureName(failure *json.MaterializationFailure) string {
	switch failure.Kind {
	case json.MaterializationFailureInvalidRequest:
		return "InvalidRequest"
	case json.MaterializationFailureUnsupportedProfile:
		return "UnsupportedProfile"
	case json.MaterializationFailureUnsupportedStyle:
		return "UnsupportedStyle"
	case json.MaterializationFailureUnsupportedEncoding:
		return "UnsupportedEncoding"
	case json.MaterializationFailureUnsupportedNewline:
		return "UnsupportedNewline"
	case json.MaterializationFailureUnrepresentable:
		return "Unrepresentable"
	case json.MaterializationFailureResourceLimit:
		return "ResourceLimit"
	case json.MaterializationFailureFormationFailed:
		return "FormationFailed"
	}
	return "FormationFailed"
}

func sameJSONReplacements(left, right []document.SourceReplacement) bool {
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

func failJSONCase(vector *caseData, report *SuiteReport, message string) {
	report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
}
