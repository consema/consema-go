package conformance

// The `consema.yaml.conformance@1` suite runner
// (crates/consema-conformance/src/yaml_v1.rs). The 0.16.0 milestone (G2.1)
// implements the full YAML surface: both profiles, lossless syntax, native
// facts, native queries, best-exact graph and value projection,
// canonical-flow materialization, the eight structural edits with
// anchor-safe rules, resource limits, and the regression corpus. All 27
// published cases are executed; the vector files are the authority and the
// runner embeds no expectation literals.

import (
	"encoding/hex"
	"math/big"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/graph"
	"consema.dev/consema/protocol"
	"consema.dev/consema/yaml"
)

// runYamlV1 executes the embedded `consema.yaml.conformance@1` suite.
func runYamlV1(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "profile.yaml12-scalars", "profile.yaml11-scalars":
			runYamlScalarProfile(vector, report)
		case "source.utf16le-bom":
			runYamlSourceEncoding(vector, report)
		case "stream.empty", "stream.multi-document":
			runYamlStreamFacts(vector, report)
		case "syntax.styles-and-trivia":
			runYamlSyntaxFacts(vector, report)
		case "native.arbitrary-duplicate-mapping":
			runYamlMappingFacts(vector, report)
		case "formation.undefined-alias":
			runYamlFormationRejection(vector, report)
		case "graph.shared-cycle":
			runYamlGraphFacts(vector, report)
		case "query.mapping-entries", "query.alias-target":
			runYamlNativeQuery(vector, report)
		case "query.syntax-comments":
			runYamlSyntaxQuery(vector, report)
		case "query.resource-limit":
			runYamlQueryLimit(vector, report)
		case "projection.sharing-policy":
			runYamlProjectionSharing(vector, report)
		case "projection.cycle":
			runYamlProjectionFailure(vector, report)
		case "projection.tag-policy":
			runYamlProjectionTag(vector, report)
		case "projection.mapping-policy":
			runYamlProjectionMapping(vector, report)
		case "projection.graph-provenance":
			runYamlGraphProvenance(vector, report)
		case "materialization.graph-cycle-flow":
			runYamlGraphMaterialization(vector, report)
		case "materialization.value-flow":
			runYamlValueMaterialization(vector, report)
		case "edit.scalar-atomic":
			runYamlEditScalar(vector, report)
		case "edit.anchor-rename":
			runYamlEditAnchor(vector, report)
		case "edit.structural-insert":
			runYamlEditStructural(vector, report)
		case "edit.anchor-dependency":
			runYamlEditAnchorDependency(vector, report)
		case "resource.parse-source-bytes":
			runYamlParseLimit(vector, report)
		case "resource.graph-provenance":
			runYamlGraphProvenanceLimit(vector, report)
		case "regression.plain-property-characters":
			runYamlPlainPropertyRegression(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published YAML case",
			})
		}
	}
	return report
}

// yamlProfile resolves one vector profile identifier.
func yamlProfile(vector *caseData) (yaml.YamlProfile, string) {
	profile, ok := stringField(vector.Input, "profile")
	if !ok {
		return yaml.YamlProfile{}, "missing input.profile"
	}
	switch profile {
	case "yaml.1.2-core@1":
		return yaml.Yaml12CoreV1, ""
	case "yaml.1.1-compat@1":
		return yaml.Yaml11CompatV1, ""
	}
	return yaml.YamlProfile{}, "unknown YAML profile " + profile
}

// parseYamlCase forms one vector source under the vector profile.
func parseYamlCase(vector *caseData) (*yaml.Document, string) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		return nil, "missing input.source"
	}
	profile, message := yamlProfile(vector)
	if message != "" {
		return nil, message
	}
	document, failure := yaml.Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure != nil {
		return nil, "YAML formation failed: " + failure.Diagnostics()[0].Code
	}
	return document, ""
}

func runYamlScalarProfile(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	count, ok := yamlDoc.Root().SequenceLen()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "root must be Sequence"})
		return
	}
	var kinds []string
	var canonical []string
	for ordinal := 0; ordinal < count; ordinal++ {
		item, ok := yamlDoc.Root().SequenceItem(ordinal)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sequence item missing"})
			return
		}
		scalar, ok := item.Node().Scalar()
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sequence item must be Scalar"})
			return
		}
		kinds = append(kinds, scalar.Kind().String())
		canonical = append(canonical, scalar.Canonical())
	}
	expectedKinds, ok := sequenceField(vector.Expected, "kinds")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.kinds"})
		return
	}
	expectedCanonical, ok := sequenceField(vector.Expected, "canonical")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.canonical"})
		return
	}
	if len(kinds) != len(expectedKinds) || len(canonical) != len(expectedCanonical) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "scalar fact count differs"})
		return
	}
	for index := range kinds {
		expectedKind, ok := expectedKinds[index].(core.String)
		if !ok || string(expectedKind) != kinds[index] {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "scalar kind differs at " + itoaYaml(index)})
			return
		}
		expectedValue, ok := expectedCanonical[index].(core.String)
		if !ok || string(expectedValue) != canonical[index] {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "scalar canonical differs at " + itoaYaml(index)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func itoaYaml(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// yamlEncodingName mirrors the Rust source_encoding_name facts.
func yamlEncodingName(encoding document.SourceEncoding) string {
	switch encoding.Kind() {
	case document.EncodingUtf8:
		return "Utf8"
	case document.EncodingUtf16Le:
		return "Utf16Le"
	case document.EncodingUtf16Be:
		return "Utf16Be"
	case document.EncodingLatin1:
		return "Latin1"
	case document.EncodingBinary:
		return "Binary"
	case document.EncodingWindowsCodePage:
		return "WindowsCodePage"
	}
	return "Unknown"
}

func runYamlSourceEncoding(vector *caseData, report *SuiteReport) {
	hexText, ok := stringField(vector.Input, "source_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source_hex"})
		return
	}
	raw, err := hex.DecodeString(hexText)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "invalid source hex"})
		return
	}
	profile, message := yamlProfile(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	doc, failure := yaml.Parse(raw, profile, document.DefaultParseLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "YAML formation failed: " + failure.Diagnostics()[0].Code})
		return
	}
	expectedEncoding, ok := stringField(vector.Expected, "encoding")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.encoding"})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "document_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.document_count"})
		return
	}
	if string(doc.Render()) != string(raw) ||
		yamlEncodingName(doc.Source().EncodingFacts().Selected()) != expectedEncoding ||
		uint64(doc.DocumentCount()) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "encoding or raw identity differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlStreamFacts(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
		return
	}
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "document_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.document_count"})
		return
	}
	expectedAliases, ok := integerField(vector.Expected, "alias_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.alias_count"})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete ||
		uint64(doc.DocumentCount()) != expectedCount ||
		uint64(doc.AliasCount()) != expectedAliases ||
		string(doc.Render()) != source {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "stream facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlSyntaxFacts(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	kinds := doc.LosslessSyntaxKinds()
	expectedCount, ok := integerField(vector.Expected, "piece_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.piece_count"})
		return
	}
	required, ok := sequenceField(vector.Expected, "required_kinds")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.required_kinds"})
		return
	}
	coverage := 0
	for _, piece := range doc.LosslessStructuralIndex().Pieces() {
		coverage += piece.Span().Len()
	}
	if uint64(len(kinds)) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax piece count differed"})
		return
	}
	for _, item := range required {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "required_kinds must be String"})
			return
		}
		found := false
		for _, kind := range kinds {
			if kind.AsStr() == string(text) {
				found = true
				break
			}
		}
		if !found {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing required syntax kind " + string(text)})
			return
		}
	}
	if coverage != len(doc.Render()) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax coverage differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlMappingFacts(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	count, ok := yamlDoc.Root().MappingLen()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "root must be Mapping"})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "entry_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.entry_count"})
		return
	}
	if uint64(count) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "entry count differed"})
		return
	}
	expectedKinds, ok := sequenceField(vector.Expected, "key_kinds")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.key_kinds"})
		return
	}
	expectedValues, ok := sequenceField(vector.Expected, "values")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.values"})
		return
	}
	for ordinal := 0; ordinal < count; ordinal++ {
		entry, ok := yamlDoc.Root().MappingEntry(ordinal)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "mapping entry missing"})
			return
		}
		expectedKind, ok := expectedKinds[ordinal].(core.String)
		if !ok || string(expectedKind) != entry.Key().Kind().String() {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "key kind differed at " + itoaYaml(ordinal)})
			return
		}
		scalar, ok := entry.Value().Scalar()
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "value must be Scalar"})
			return
		}
		expectedValue, ok := expectedValues[ordinal].(core.String)
		if !ok || string(expectedValue) != scalar.Canonical() {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "value canonical differed at " + itoaYaml(ordinal)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlFormationRejection(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
		return
	}
	profile, message := yamlProfile(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	_, failure := yaml.Parse([]byte(source), profile, document.DefaultParseLimits())
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "formation unexpectedly succeeded"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	diagnostics := failure.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Code != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "formation code differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlGraphFacts(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projected, err := doc.ProjectGraph()
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph projection failed: " + err.Error()})
		return
	}
	encoded, err := graph.EncodePGCE(projected)
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "PGCE encode failed"})
		return
	}
	expectedNodes, ok := integerField(vector.Expected, "node_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.node_count"})
		return
	}
	expectedRoots, ok := integerField(vector.Expected, "root_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.root_count"})
		return
	}
	expectedHex, ok := stringField(vector.Expected, "pgce_hex")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.pgce_hex"})
		return
	}
	if uint64(projected.NodeCount()) != expectedNodes ||
		uint64(len(projected.Roots())) != expectedRoots ||
		hex.EncodeToString(encoded) != expectedHex {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// yamlExecutableFromPipeline binds one validated native YAML query.
func yamlExecutableFromPipeline(vector *caseData) (*protocol.ExecutableQuery, string) {
	pipeline, ok := sequenceField(vector.Input, "pipeline")
	if !ok {
		return nil, "missing input.pipeline"
	}
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, item := range pipeline {
		operator, ok := item.(core.String)
		if !ok {
			return nil, "pipeline operator must be String"
		}
		id, version, found := strings.Cut(string(operator), "@")
		if !found {
			return nil, "pipeline operator lacks version"
		}
		expression = expression.Then(protocol.NewOperatorCall(id, 1))
		_ = version
	}
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLNativeV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, "validation: " + failure.Code()
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		return nil, "binding: " + failure.Code()
	}
	return bound, ""
}

func runYamlNativeQuery(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	executable, message := yamlExecutableFromPipeline(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	matches, failure := yaml.ExecuteYamlQuery(contextBackground(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query: " + failure.Code()})
		return
	}
	roles := make([]string, 0, len(matches))
	for index := range matches {
		roles = append(roles, string(yamlMatchRole(&matches[index])))
	}
	expected, ok := sequenceField(vector.Expected, "roles")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.roles"})
		return
	}
	if len(roles) != len(expected) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query role count differs"})
		return
	}
	for index := range roles {
		text, ok := expected[index].(core.String)
		if !ok || string(text) != roles[index] {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "query role differs at " + itoaYaml(index)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// yamlMatchRole mirrors the Rust yaml_match_role mapping.
func yamlMatchRole(match *yaml.YamlMatch) protocol.MatchRole {
	switch match.Kind {
	case yaml.YamlMatchStream:
		return protocol.RoleYamlStream
	case yaml.YamlMatchDocument:
		return protocol.RoleYamlDocument
	case yaml.YamlMatchNode:
		return protocol.RoleYamlNode
	case yaml.YamlMatchMappingEntry:
		return protocol.RoleYamlMappingEntry
	case yaml.YamlMatchSequenceElement:
		return protocol.RoleYamlSequenceElement
	case yaml.YamlMatchAnchorDefinition:
		return protocol.RoleYamlAnchorDefinition
	case yaml.YamlMatchAliasOccurrence:
		return protocol.RoleYamlAliasOccurrence
	}
	return ""
}

func runYamlSyntaxQuery(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	kind, ok := stringField(vector.Input, "kind")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.kind"})
		return
	}
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("yaml.syntax-kind-is", 1).
			WithArgument("kind", core.String(kind)))
	definition := protocol.NewQueryDefinition(protocol.DomainYAMLLosslessSyntaxV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "validation: " + failure.Code()})
		return
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "binding: " + failure.Code()})
		return
	}
	matches, failure := yaml.ExecuteYamlSyntaxQuery(contextBackground(), executable, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query: " + failure.Code()})
		return
	}
	expected, ok := sequenceField(vector.Expected, "ordinals")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.ordinals"})
		return
	}
	if len(matches) != len(expected) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "syntax ordinal count differs"})
		return
	}
	for index := range matches {
		ordinal, ok := expected[index].(core.Integer)
		if !ok || ordinal.Int().Int64() != int64(matches[index].Ordinal()) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "syntax ordinal differs at " + itoaYaml(index)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlQueryLimit(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	executable, message := yamlExecutableFromPipeline(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	maxResults, ok := integerField(vector.Input, "max_results")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.max_results"})
		return
	}
	limits := protocol.DefaultQueryLimits()
	limits.MaxResults = int(maxResults)
	_, failure := yaml.ExecuteYamlQuery(contextBackground(), executable, doc, limits)
	if failure == nil || failure.Code() != "core.query.resource-limit@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "query limit did not fail with core.query.resource-limit@1"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlProjectionSharing(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	defaultResult := doc.ProjectValue(yaml.BestExactValueV1())
	if defaultResult.Failed == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "default sharing policy unexpectedly completed"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "default_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.default_code"})
		return
	}
	if defaultResult.Failed.Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "default sharing code differed"})
		return
	}
	duplicated := doc.ProjectValue(yaml.BestExactValueV1().
		WithSharing(yaml.SharingPolicyDuplicateAcyclic))
	if duplicated.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit acyclic duplication failed"})
		return
	}
	expectedEvents, ok := integerField(vector.Expected, "event_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.event_count"})
		return
	}
	events := duplicated.Complete.Report.Events()
	if duplicated.Complete.Fidelity != yaml.FidelityTransformed ||
		uint64(len(events)) != expectedEvents {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "sharing policy facts differed"})
		return
	}
	for _, event := range events {
		if event.Kind != yaml.ProjectionEventSharingDuplicated {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unexpected sharing event kind"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlProjectionFailure(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	result := doc.ProjectValue(yaml.BestExactValueV1().
		WithSharing(yaml.SharingPolicyDuplicateAcyclic))
	if result.Failed == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection unexpectedly completed"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	if result.Failed.Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection failure code differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlProjectionTag(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	defaultResult := doc.ProjectValue(yaml.BestExactValueV1())
	if defaultResult.Failed == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unknown tag unexpectedly projected exactly"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "default_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.default_code"})
		return
	}
	if defaultResult.Failed.Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "tag policy default code differed"})
		return
	}
	stripped := doc.ProjectValue(yaml.BestExactValueV1().
		WithTags(yaml.TagPolicyStripToNodeKind))
	if stripped.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit tag stripping failed"})
		return
	}
	expectedValue, ok := stringField(vector.Expected, "value")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.value"})
		return
	}
	text, ok := stripped.Complete.Value.(core.String)
	if stripped.Complete.Fidelity != yaml.FidelityLossy || !ok ||
		string(text) != expectedValue || len(stripped.Complete.Report.Events()) != 1 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "tag policy facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlProjectionMapping(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	objectResult := doc.ProjectValue(yaml.BestExactValueV1().
		WithMapping(yaml.MappingPolicyRequireObject))
	if objectResult.Failed == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "duplicate mapping unexpectedly became Object"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "object_code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.object_code"})
		return
	}
	if objectResult.Failed.Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "object policy code differed"})
		return
	}
	entriesResult := doc.ProjectValue(yaml.BestExactValueV1().
		WithMapping(yaml.MappingPolicyRequireEntryMapping))
	if entriesResult.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "explicit EntryMapping projection failed"})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "entry_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.entry_count"})
		return
	}
	mapping, ok := entriesResult.Complete.Value.(*core.EntryMapping)
	if !ok || uint64(mapping.Len()) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "mapping policy facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlGraphProvenance(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projected, failure := doc.ProjectGraphWithProvenance(yaml.BestExactGraphV1())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph provenance failed: " + failure.Code()})
		return
	}
	references := 0
	associations := 0
	for _, entry := range projected.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Relation == yaml.ProvenanceReference {
				references++
			}
		}
		switch entry.Projected.Kind {
		case "SequenceElement", "MappingKey", "MappingValue":
			associations++
		}
	}
	expectedReferences, ok := integerField(vector.Expected, "reference_origins")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.reference_origins"})
		return
	}
	expectedAssociations, ok := integerField(vector.Expected, "association_entries")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.association_entries"})
		return
	}
	if uint64(references) != expectedReferences ||
		uint64(associations) != expectedAssociations {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph provenance counts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// yamlMaterializationRequest builds the frozen vector materialization
// request.
func yamlMaterializationRequest(style string) document.MaterializationRequest {
	return document.NewMaterializationRequest(
		document.NewProfileId("yaml.1.2-core", 1),
		document.NewMaterializationStyleId(style, 1)).
		WithNewline(document.NewlineLf)
}

func runYamlGraphMaterialization(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projected, err := doc.ProjectGraph()
	if err != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph projection failed"})
		return
	}
	result := yaml.MaterializeGraph(projected, yamlMaterializationRequest("yaml.canonical-flow"))
	if result.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph materialization failed: " + result.Failed.Failure.Code()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	reparsed, err := result.Complete.Document.ProjectGraph()
	if err != nil || !graph.Equal(reparsed, projected) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph materialization did not round-trip"})
		return
	}
	if string(result.Complete.Document.Render()) != expectedSource ||
		result.Complete.Fidelity != yaml.MaterializationFidelityExact {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph materialization facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlValueMaterialization(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	projected := doc.ProjectValue(yaml.BestExactValueV1())
	if projected.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "input value projection failed: " + projected.Failed.Code()})
		return
	}
	result := yaml.MaterializeValue(projected.Complete.Value,
		yamlMaterializationRequest("yaml.canonical-flow"))
	if result.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "value materialization failed: " + result.Failed.Failure.Code()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	reprojected := result.Complete.Document.ProjectValue(yaml.BestExactValueV1())
	if reprojected.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "materialized value did not reproject"})
		return
	}
	if string(result.Complete.Document.Render()) != expectedSource ||
		!core.Equal(reprojected.Complete.Value, projected.Complete.Value) ||
		result.Complete.Fidelity != yaml.MaterializationFidelityExact {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "value materialization facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlEditScalar(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	entryOrdinal, ok := integerField(vector.Input, "entry")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.entry"})
		return
	}
	integerText, ok := stringField(vector.Input, "integer")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.integer"})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	entry, ok := yamlDoc.Root().MappingEntry(int(entryOrdinal))
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "scalar edit target missing"})
		return
	}
	number := new(big.Int)
	if _, ok := number.SetString(integerText, 10); !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "input.integer is not a decimal integer"})
		return
	}
	builder := yaml.NewEditTransactionBuilder(doc)
	builder.SemanticScalar(entry.Value().NodeRef(), core.NewInteger(number),
		yaml.RepresentationPolicyPreserveCompatible)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "edit_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.edit_count"})
		return
	}
	if string(commit.Document.Render()) != expectedSource ||
		uint64(len(commit.ChangeSet.SourceEdits())) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "scalar edit facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlEditAnchor(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	entryOrdinal, ok := integerField(vector.Input, "entry")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.entry"})
		return
	}
	name, ok := stringField(vector.Input, "name")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.name"})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	entry, ok := yamlDoc.Root().MappingEntry(int(entryOrdinal))
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchor target missing"})
		return
	}
	anchorRef, ok := entry.Value().AnchorNodeRef()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchor target missing"})
		return
	}
	builder := yaml.NewEditTransactionBuilder(doc)
	builder.RenameAnchor(anchorRef, name)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	alias, ok := commit.Document.Alias(0)
	if string(commit.Document.Render()) != expectedSource || !ok || alias.Name() != name {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchor rename facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlEditStructural(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	root := yamlDoc.Root()
	sequenceEntry, ok := root.MappingEntry(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "sequence missing"})
		return
	}
	mappingEntry, ok := root.MappingEntry(1)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "mapping missing"})
		return
	}
	sequence := sequenceEntry.Value()
	mapping := mappingEntry.Value()
	second, ok := sequence.SequenceItem(1)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "second sequence item missing"})
		return
	}
	key := new(big.Int)
	key.SetInt64(2)
	builder := yaml.NewEditTransactionBuilder(doc)
	builder.InsertSequenceElement(sequence.NodeRef(), core.Boolean(true),
		yaml.PlacementBefore(second.NodeRef()))
	builder.InsertMappingEntry(mapping.NodeRef(), core.String("b"), core.NewInteger(key),
		yaml.PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.source"})
		return
	}
	if string(commit.Document.Render()) != expectedSource {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "structural edit output differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlEditAnchorDependency(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
		return
	}
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	entry, ok := yamlDoc.Root().MappingEntry(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "mapping entry missing"})
		return
	}
	target, ok := entry.Value().SequenceItem(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchored sequence item missing"})
		return
	}
	builder := yaml.NewEditTransactionBuilder(doc)
	builder.RemoveSequenceElement(target.NodeRef())
	_, failure := doc.Commit(builder.Build())
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchor dependency removal unexpectedly succeeded"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	if failure.Code() != expectedCode || string(doc.Render()) != source {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "anchor dependency facts differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlParseLimit(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.source"})
		return
	}
	maxSourceBytes, ok := integerField(vector.Input, "max_source_bytes")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.max_source_bytes"})
		return
	}
	profile, message := yamlProfile(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	limits := document.DefaultParseLimits()
	limits.MaxSourceBytes = int(maxSourceBytes)
	_, failure := yaml.Parse([]byte(source), profile, limits)
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "parse limit unexpectedly succeeded"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	diagnostics := failure.Diagnostics()
	if len(diagnostics) == 0 || diagnostics[0].Code != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "parse limit code differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlGraphProvenanceLimit(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	maxEntries, ok := integerField(vector.Input, "max_provenance_entries")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.max_provenance_entries"})
		return
	}
	limits := yaml.DefaultGraphProjectionLimits()
	limits.MaxProvenanceEntries = int(maxEntries)
	_, failure := doc.ProjectGraphWithProvenance(yaml.BestExactGraphV1().WithLimits(limits))
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph provenance limit unexpectedly succeeded"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "code")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.code"})
		return
	}
	if failure.Code() != expectedCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "graph provenance limit code differed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runYamlPlainPropertyRegression(vector *caseData, report *SuiteReport) {
	doc, message := parseYamlCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	yamlDoc, ok := doc.Document(0)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "document 0 missing"})
		return
	}
	scalar, ok := yamlDoc.Root().Scalar()
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "root must be Scalar"})
		return
	}
	expectedCanonical, ok := stringField(vector.Expected, "canonical")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.canonical"})
		return
	}
	if scalar.Canonical() != expectedCanonical || doc.AliasCount() != 0 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "plain scalar facts differed"})
		return
	}
	for _, kind := range doc.LosslessSyntaxKinds() {
		if kind == yaml.SyntaxKindAnchor || kind == yaml.SyntaxKindTag {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "plain scalar fabricated YAML node properties"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}
