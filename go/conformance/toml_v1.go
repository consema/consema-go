package conformance

// The `consema.toml.conformance@1` suite runner
// (consema-rs/consema-conformance/src/toml_v1.rs). The 0.15.0 milestone (G1.3)
// implements the full TOML surface: complete formation, lossless syntax,
// native items, native queries, best-exact-core projection, scalar edits,
// resource limits, and the corpus documents. All 18 published cases are
// executed; the vector files and fixtures are the authority and the runner
// embeds no expectation literals.

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

// runTomlV1 executes the embedded `consema.toml.conformance@1` suite.
func runTomlV1(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.ID {
		case "toml.parse.exact-roundtrip":
			runTomlExactRoundtrip(runner, vector, report)
		case "toml.parse.lossless-byte-coverage":
			runTomlLosslessCoverage(runner, vector, report)
		case "toml.native.dotted-segments":
			runTomlDottedSegments(vector, report)
		case "toml.native.table-flavors":
			runTomlTableFlavors(runner, vector, report)
		case "toml.native.array-aot-distinct":
			runTomlArrayAOTDistinct(runner, vector, report)
		case "toml.native.float-signed-zero":
			runTomlFloatSignedZero(vector, report)
		case "toml.query.nested-entry-order":
			runTomlNestedEntryQuery(runner, vector, report)
		case "toml.query.aot-element-order":
			runTomlAOTQuery(runner, vector, report)
		case "toml.projection.all-core-kinds":
			runTomlProjectionAllKinds(runner, vector, report)
		case "toml.projection.provenance":
			runTomlProjectionProvenance(vector, report)
		case "toml.projection.reject-leap-second":
			runTomlProjectionLeapSecond(vector, report)
		case "toml.edit.literal-minimal":
			runTomlEditLiteralMinimal(vector, report)
		case "toml.edit.reject-unrepresentable":
			runTomlEditRejectUnrepresentable(vector, report)
		case "toml.parse.reject-invalid":
			runTomlRejectInvalid(runner, vector, report)
		case "toml.resource.token-limit":
			runTomlTokenLimit(vector, report)
		case "toml.resource.node-depth-limits":
			runTomlNodeDepthLimits(vector, report)
		case "toml.corpus.cargo-manifest":
			runTomlCorpusDocument(runner, vector, report, "Cargo.toml")
		case "toml.corpus.pyproject":
			runTomlCorpusDocument(runner, vector, report, "pyproject.toml")
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published TOML case",
			})
		}
	}
	return report
}

// tomlFixtureBytes reads one fixture under conformance/fixtures/toml.
func tomlFixtureBytes(runner *Runner, name string) ([]byte, string) {
	path := filepath.Join(runner.FixturesDir, "toml", name)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "fixture " + name + " is unreadable: " + err.Error()
	}
	return bytes, ""
}

// cargoManifestBytes reads the toml.corpus.cargo-manifest fixture
// (conformance/fixtures/toml/Cargo.toml, the single authority for the
// corpus case since the six-repo split).
func cargoManifestBytes(runner *Runner) ([]byte, string) {
	path := filepath.Join(runner.FixturesDir, "toml", "Cargo.toml")
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "Cargo.toml is unreadable: " + err.Error()
	}
	return bytes, ""
}

// parseTomlFixture forms one fixture with default limits.
func parseTomlFixture(runner *Runner, name string) (*toml.Document, string) {
	bytes, message := tomlFixtureBytes(runner, name)
	if message != "" {
		return nil, message
	}
	return parseTomlBytes(bytes)
}

func parseTomlBytes(source []byte) (*toml.Document, string) {
	document, failure := toml.Parse(source, toml.Toml10V1, documentLimits())
	if failure != nil {
		return nil, "TOML formation failed: " + failure.Diagnostics()[0].Code
	}
	return document, ""
}

func documentLimits() document.ParseLimits {
	return document.DefaultParseLimits()
}

func tomlDirectItem(container toml.TomlItem, name string) (toml.TomlItem, string) {
	entries, ok := container.TableEntries()
	if !ok {
		return toml.TomlItem{}, "item is not a table"
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry.Item(), ""
		}
	}
	return toml.TomlItem{}, "missing direct entry " + name
}

func runTomlExactRoundtrip(runner *Runner, vector *caseData, report *SuiteReport) {
	source, message := tomlFixtureBytes(runner, "all-values.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	doc, message := parseTomlBytes(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if string(doc.Render()) != string(source) ||
		doc.FormationStatus() != document.FormationStatusComplete ||
		doc.FormatFamily().ID() != "toml" ||
		doc.Profile().ID() != "toml.1.0" ||
		len(doc.Diagnostics()) != 0 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact roundtrip facts did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlLosslessCoverage(runner *Runner, vector *caseData, report *SuiteReport) {
	source, message := tomlFixtureBytes(runner, "trivia-and-strings.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	document, message := parseTomlBytes(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	pieces := document.LosslessStructuralIndex().Pieces()
	if len(pieces) == 0 || pieces[0].Span().StartByte() != 0 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "first piece does not start at byte 0"})
		return
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "gap or overlap between pieces"})
			return
		}
	}
	if pieces[len(pieces)-1].Span().EndByte() != len(source) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "last piece does not reach the end"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlDottedSegments(vector *caseData, report *SuiteReport) {
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
	alpha, message := tomlDirectItem(document.Root(), "alpha")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	beta, message := tomlDirectItem(alpha, "beta")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	gamma, message := tomlDirectItem(beta, "gamma")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if alpha.Kind() != toml.ItemKindDottedTable || beta.Kind() != toml.ItemKindDottedTable {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "dotted segments are not DottedTable"})
		return
	}
	value, ok := gamma.AsInteger()
	if !ok || value != 1 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "leaf is not integer 1"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlTableFlavors(runner *Runner, vector *caseData, report *SuiteReport) {
	document, message := parseTomlFixture(runner, "application.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	service, message := tomlDirectItem(document.Root(), "service")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	database, message := tomlDirectItem(document.Root(), "database")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	observability, message := tomlDirectItem(document.Root(), "observability")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if service.Kind() != toml.ItemKindDottedTable ||
		database.Kind() != toml.ItemKindStandardTable ||
		observability.Kind() != toml.ItemKindImplicitTable {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "table flavors did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlArrayAOTDistinct(runner *Runner, vector *caseData, report *SuiteReport) {
	document, message := parseTomlFixture(runner, "application.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	database, message := tomlDirectItem(document.Root(), "database")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	timeouts, message := tomlDirectItem(database, "timeouts")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	upstreams, message := tomlDirectItem(document.Root(), "upstreams")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	elements, ok := upstreams.ArrayElements()
	if !ok || timeouts.Kind() != toml.ItemKindArray ||
		upstreams.Kind() != toml.ItemKindArrayOfTables || len(elements) != 2 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "array and array-of-tables identities did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlFloatSignedZero(vector *caseData, report *SuiteReport) {
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
	positive, message := tomlDirectItem(document.Root(), "positive")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	negative, message := tomlDirectItem(document.Root(), "negative")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	positiveBits, ok := positive.AsFloatBits()
	if !ok || positiveBits != 0 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "positive zero bits mismatch"})
		return
	}
	negativeBits, ok := negative.AsFloatBits()
	if !ok || negativeBits != 1<<63 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "negative zero bits mismatch"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// tomlExecutable binds one validated TOML native query with the shared
// ordered-results capability.
func tomlExecutable(expression *protocol.QueryExpression) (*protocol.ExecutableQuery, string) {
	definition := protocol.NewQueryDefinition(protocol.DomainTOMLNativeV1()).
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

// tomlNamedRootExpression builds Input -> try-table-entries ->
// entry-name-equals(name) -> entry-item.
func tomlNamedRootExpression(name string) *protocol.QueryExpression {
	return (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1)).
		Then(protocol.NewOperatorCall("toml.entry-name-equals", 1).
			WithArgument("name", core.String(name))).
		Then(protocol.NewOperatorCall("toml.entry-item", 1))
}

func runTomlNestedEntryQuery(runner *Runner, vector *caseData, report *SuiteReport) {
	path, ok := sequenceField(vector.Input, "path")
	if !ok || len(path) != 1 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.path"})
		return
	}
	name, ok := path[0].(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.path must be String"})
		return
	}
	document, message := parseTomlFixture(runner, "application.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expression := tomlNamedRootExpression(string(name)).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1))
	executable, message := tomlExecutable(expression)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	matches, failure := toml.ExecuteTomlQuery(contextBackground(), executable, document,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "query: " + failure.Code()})
		return
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if match.Kind == "Entry" {
			names = append(names, match.Name)
		}
	}
	expected, ok := sequenceField(vector.Expected, "names")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.names"})
		return
	}
	expectedNames := make([]string, 0, len(expected))
	for _, item := range expected {
		text, ok := item.(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected.names must be String"})
			return
		}
		expectedNames = append(expectedNames, string(text))
	}
	if len(names) != len(expectedNames) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "entry count differs from expected"})
		return
	}
	for index := range names {
		if names[index] != expectedNames[index] {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "entry order differs from expected"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlAOTQuery(runner *Runner, vector *caseData, report *SuiteReport) {
	path, ok := sequenceField(vector.Input, "path")
	if !ok || len(path) != 1 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.path"})
		return
	}
	name, ok := path[0].(core.String)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "input.path must be String"})
		return
	}
	document, message := parseTomlFixture(runner, "application.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	expression := tomlNamedRootExpression(string(name)).
		Then(protocol.NewOperatorCall("toml.try-array-elements", 1))
	executable, message := tomlExecutable(expression)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	matches, failure := toml.ExecuteTomlQuery(contextBackground(), executable, document,
		protocol.DefaultQueryLimits())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "query: " + failure.Code()})
		return
	}
	expected, ok := sequenceField(vector.Expected, "ordinals")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.ordinals"})
		return
	}
	if len(matches) != len(expected) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "element count differs from expected"})
		return
	}
	for index, match := range matches {
		if match.Kind != "ArrayElement" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "match is not an ArrayElement"})
			return
		}
		ordinal, ok := expected[index].(core.Integer)
		if !ok || ordinal.Int().Int64() != int64(match.Ordinal) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "element ordinal differs from expected"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlProjectionAllKinds(runner *Runner, vector *caseData, report *SuiteReport) {
	document, message := parseTomlFixture(runner, "all-values.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	result := document.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if result.Failed != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "exact projection failed: " + result.Failed.Diagnostics[0].Code})
		return
	}
	root, ok := result.Complete.Value.(*core.Object)
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "root is not Object"})
		return
	}
	kinds := map[core.Kind]bool{}
	for _, entry := range root.Entries() {
		kinds[entry.Value.Kind()] = true
	}
	status, _ := stringField(vector.Expected, "status")
	fidelity, _ := stringField(vector.Expected, "fidelity")
	rootKind, _ := stringField(vector.Expected, "root")
	if status != "Success" || fidelity != "Exact" || rootKind != "Object" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unexpected expectation facts"})
		return
	}
	for _, kind := range []core.Kind{core.KindString, core.KindBoolean, core.KindInteger,
		core.KindBinaryFloat64, core.KindDate, core.KindTime, core.KindLocalDateTime,
		core.KindOffsetDateTime, core.KindArray, core.KindObject} {
		if !kinds[kind] {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing projected kind " + kindName(kind)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func kindName(kind core.Kind) string {
	return kind.String()
}

func runTomlProjectionProvenance(vector *caseData, report *SuiteReport) {
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
	result := document.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if result.Failed != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection failed: " + result.Failed.Diagnostics[0].Code})
		return
	}
	snapshot := document.SnapshotIdentity()
	hasObjectEntry := false
	for _, entry := range result.Complete.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Snapshot != snapshot ||
				origin.Node.Snapshot() != snapshot || origin.Span.Snapshot() != snapshot {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "provenance origin is not snapshot bound"})
				return
			}
		}
		if entry.Projected.Kind == "Association" &&
			entry.Projected.Association.Role() == protocol.AssociationRoleObjectEntry {
			hasObjectEntry = true
		}
	}
	if !hasObjectEntry {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "no ObjectEntry association provenance"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlProjectionLeapSecond(vector *caseData, report *SuiteReport) {
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
	result := document.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if result.Complete != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "leap second projection succeeded"})
		return
	}
	if len(result.Failed.Diagnostics) != 1 ||
		result.Failed.Diagnostics[0].Code != "toml.projection.unrepresentable-datetime@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "leap second diagnostic did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlEditLiteralMinimal(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	literal, ok := stringField(vector.Input, "literal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.literal"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	hex, message := tomlDirectItem(document.Root(), "hex")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.LiteralScalar(hex.NodeRef(), []byte(literal))
	commit, failure := document.Commit(builder.Build())
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit: " + failure.Name()})
		return
	}
	expectedSource, ok := stringField(vector.Expected, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.source"})
		return
	}
	expectedCount, ok := integerField(vector.Expected, "source_edit_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing expected.source_edit_count"})
		return
	}
	if string(commit.Document.Render()) != expectedSource ||
		uint64(len(commit.ChangeSet.SourceEdits())) != expectedCount {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit output or source-edit count did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlEditRejectUnrepresentable(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	bitsText, ok := stringField(vector.Input, "binary64_bits")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.binary64_bits"})
		return
	}
	document, message := parseTomlBytes([]byte(source))
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	float, message := tomlDirectItem(document.Root(), "float")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	decoded, err := hex.DecodeString(bitsText)
	if err != nil || len(decoded) != 8 {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "expected 8 hex bytes"})
		return
	}
	var bits uint64
	for _, byte := range decoded {
		bits = bits<<8 | uint64(byte)
	}
	builder := toml.NewEditTransactionBuilder(document)
	builder.SemanticScalar(float.NodeRef(), core.NewBinaryFloat64(bits),
		toml.RepresentationCanonicalForProfile)
	_, failure := document.Commit(builder.Build())
	if failure == nil || failure.Name() != "UnsupportedSemanticValue" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unrepresentable scalar must fail with UnsupportedSemanticValue"})
		return
	}
	if string(document.Render()) != source {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "base source changed after failure"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlRejectInvalid(runner *Runner, vector *caseData, report *SuiteReport) {
	bytes, message := tomlFixtureBytes(runner, "invalid-duplicate.toml")
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	_, failure := toml.Parse(bytes, toml.Toml10V1, documentLimits())
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "duplicate key must fail formation"})
		return
	}
	diagnostics := failure.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "toml.parse.syntax@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "duplicate key diagnostic did not match"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlTokenLimit(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	tokenLimit, ok := integerField(vector.Input, "max_token_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.max_token_count"})
		return
	}
	limits := documentLimits()
	limits.MaxTokenCount = int(tokenLimit)
	_, failure := toml.Parse([]byte(source), toml.Toml10V1, limits)
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "token limit must fail formation"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlNodeDepthLimits(vector *caseData, report *SuiteReport) {
	source, ok := stringField(vector.Input, "source")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.source"})
		return
	}
	nodeLimit, ok := integerField(vector.Input, "max_node_count")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.max_node_count"})
		return
	}
	depthLimit, ok := integerField(vector.Input, "max_nesting_depth")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: "missing input.max_nesting_depth"})
		return
	}
	nodeLimits := documentLimits()
	nodeLimits.MaxNodeCount = int(nodeLimit)
	_, failure := toml.Parse([]byte(source), toml.Toml10V1, nodeLimits)
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "node limit must fail with core.parse.resource-limit@1"})
		return
	}
	depthLimits := documentLimits()
	depthLimits.MaxNestingDepth = int(depthLimit)
	_, failure = toml.Parse([]byte(source), toml.Toml10V1, depthLimits)
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "depth limit must fail with core.parse.resource-limit@1"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runTomlCorpusDocument(runner *Runner, vector *caseData, report *SuiteReport, name string) {
	var source []byte
	var message string
	if name == "Cargo.toml" {
		source, message = cargoManifestBytes(runner)
	} else {
		source, message = tomlFixtureBytes(runner, name)
	}
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	doc, message := parseTomlBytes(source)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if string(doc.Render()) != string(source) ||
		doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "corpus document facts did not match"})
		return
	}
	result := doc.Project(toml.NewProjectionRequest(toml.ProjectionTargetBestExactCoreV1))
	if result.Complete == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "corpus projection failed"})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// contextBackground returns the shared background context for query
// execution in the runner.
func contextBackground() context.Context { return context.Background() }
