package toml

import (
	"context"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// fixturesDir resolves the repository fixture directory from the test
// working directory (the go module root is go/).
func fixturesDir(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"../conformance/fixtures/toml",
		"../../conformance/fixtures/toml",
	} {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	t.Fatal("conformance/fixtures/toml not found")
	return ""
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join(fixturesDir(t), name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes
}

func mustParse(t *testing.T, source []byte) *Document {
	t.Helper()
	doc, failure := Parse(source, Toml10V1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("parse failed: %s", failure.Error())
	}
	return doc
}

func rootEntry(t *testing.T, container TomlItem, name string) TomlEntry {
	t.Helper()
	entries, ok := container.TableEntries()
	if !ok {
		t.Fatalf("root is not a table")
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return entry
		}
	}
	t.Fatalf("missing root entry %q", name)
	return TomlEntry{}
}

func TestCompleteDocumentPreservesSourceAndNativeCategories(t *testing.T) {
	source := readFixture(t, "all-values.toml")
	doc := mustParse(t, source)
	if string(doc.Render()) != string(source) {
		t.Fatalf("render is not byte-exact")
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("formation must be Complete")
	}
	if doc.FormatFamily().ID() != "toml" || doc.Profile().ID() != "toml.1.0" {
		t.Fatalf("family or profile mismatch")
	}
	if len(doc.Diagnostics()) != 0 {
		t.Fatalf("complete documents carry no diagnostics")
	}
	root := doc.Root()
	if root.Kind() != ItemKindRootTable {
		t.Fatalf("root kind = %s", root.Kind())
	}
	integer := rootEntry(t, doc.Root(), "integer").Item()
	value, ok := integer.AsInteger()
	if !ok || value != 42 {
		t.Fatalf("integer = %v", value)
	}
	hex := rootEntry(t, doc.Root(), "hex").Item()
	value, ok = hex.AsInteger()
	if !ok || value != 0xDEADBEEF {
		t.Fatalf("hex = %v", value)
	}
	float := rootEntry(t, doc.Root(), "float").Item()
	bits, ok := float.AsFloatBits()
	if !ok || bits != math.Float64bits(math.Copysign(0, -1)) {
		t.Fatalf("float bits = %x", bits)
	}
	inf := rootEntry(t, doc.Root(), "positive_infinity").Item()
	bits, _ = inf.AsFloatBits()
	if bits != math.Float64bits(math.Inf(1)) {
		t.Fatalf("inf bits = %x", bits)
	}
	date := rootEntry(t, doc.Root(), "local_date").Item()
	dateTime, ok := date.AsDateTime()
	if !ok || dateTime.Date == nil || dateTime.Time != nil {
		t.Fatalf("local date mismatch")
	}
	if date.Kind() != ItemKindLocalDate {
		t.Fatalf("kind = %s", date.Kind())
	}
	localTime := rootEntry(t, doc.Root(), "local_time").Item()
	dateTime, _ = localTime.AsDateTime()
	if dateTime.Time == nil || dateTime.Time.Nanosecond != 123456789 {
		t.Fatalf("local time mismatch")
	}
	offset := rootEntry(t, doc.Root(), "offset_date_time").Item()
	dateTime, _ = offset.AsDateTime()
	if dateTime.Offset == nil || dateTime.Offset.Z || dateTime.Offset.Minutes != -420 {
		t.Fatalf("offset mismatch: %+v", dateTime.Offset)
	}
	if offset.Kind() != ItemKindOffsetDateTime {
		t.Fatalf("kind = %s", offset.Kind())
	}
	ports := rootEntry(t, doc.Root(), "ports").Item()
	if ports.Kind() != ItemKindArray {
		t.Fatalf("ports kind = %s", ports.Kind())
	}
	elements, ok := ports.ArrayElements()
	if !ok || len(elements) != 3 {
		t.Fatalf("ports elements = %d", len(elements))
	}
	point := rootEntry(t, doc.Root(), "point").Item()
	if point.Kind() != ItemKindInlineTable {
		t.Fatalf("point kind = %s", point.Kind())
	}
	entries, ok := point.TableEntries()
	if !ok || len(entries) != 2 {
		t.Fatalf("point entries = %d", len(entries))
	}
}

func TestLosslessByteCoverage(t *testing.T) {
	source := readFixture(t, "trivia-and-strings.toml")
	doc := mustParse(t, source)
	pieces := doc.LosslessStructuralIndex().Pieces()
	if len(pieces) == 0 {
		t.Fatalf("no pieces")
	}
	if pieces[0].Span().StartByte() != 0 {
		t.Fatalf("first piece does not start at 0")
	}
	for index := 1; index < len(pieces); index++ {
		if pieces[index-1].Span().EndByte() != pieces[index].Span().StartByte() {
			t.Fatalf("gap or overlap at piece %d", index)
		}
	}
	if pieces[len(pieces)-1].Span().EndByte() != len(source) {
		t.Fatalf("last piece does not reach the end")
	}
	kinds := doc.LosslessSyntaxKinds()
	if len(kinds) != len(pieces) {
		t.Fatalf("kind count mismatch")
	}
	// The leading comment is one Comment piece.
	if kinds[0] != SyntaxKindComment {
		t.Fatalf("first kind = %s", kinds[0])
	}
	// The trailing newline is a Newline piece.
	if kinds[len(kinds)-1] != SyntaxKindNewline {
		t.Fatalf("last kind = %s", kinds[len(kinds)-1])
	}
}

func TestNativeDottedSegmentsAndTableFlavors(t *testing.T) {
	doc := mustParse(t, []byte("alpha.beta.gamma = 1\n"))
	alpha := rootEntry(t, doc.Root(), "alpha").Item()
	if alpha.Kind() != ItemKindDottedTable {
		t.Fatalf("alpha kind = %s", alpha.Kind())
	}
	beta := rootEntry(t, alpha, "beta").Item()
	if beta.Kind() != ItemKindDottedTable {
		t.Fatalf("beta kind = %s", beta.Kind())
	}
	gamma := rootEntry(t, beta, "gamma").Item()
	value, ok := gamma.AsInteger()
	if !ok || value != 1 {
		t.Fatalf("gamma = %v", value)
	}

	application := mustParse(t, readFixture(t, "application.toml"))
	if service := rootEntry(t, application.Root(), "service").Item(); service.Kind() != ItemKindDottedTable {
		t.Fatalf("service kind = %s", service.Kind())
	}
	if database := rootEntry(t, application.Root(), "database").Item(); database.Kind() != ItemKindStandardTable {
		t.Fatalf("database kind = %s", database.Kind())
	}
	if observability := rootEntry(t, application.Root(), "observability").Item(); observability.Kind() != ItemKindImplicitTable {
		t.Fatalf("observability kind = %s", observability.Kind())
	}
	database := rootEntry(t, application.Root(), "database").Item()
	if timeouts := rootEntry(t, database, "timeouts").Item(); timeouts.Kind() != ItemKindArray {
		t.Fatalf("timeouts kind = %s", timeouts.Kind())
	}
	upstreams := rootEntry(t, application.Root(), "upstreams").Item()
	if upstreams.Kind() != ItemKindArrayOfTables {
		t.Fatalf("upstreams kind = %s", upstreams.Kind())
	}
	elements, ok := upstreams.ArrayElements()
	if !ok || len(elements) != 2 {
		t.Fatalf("upstreams elements = %d", len(elements))
	}
	first := elements[0].Item()
	if name := rootEntry(t, first, "name").Item(); name.Kind() != ItemKindString {
		t.Fatalf("first upstream name kind = %s", name.Kind())
	}
}

func TestSyntaxAndResourceFailuresNeverFormDocuments(t *testing.T) {
	_, failure := Parse([]byte("value = [1,,2]"), Toml10V1, document.DefaultParseLimits())
	if failure == nil {
		t.Fatalf("invalid TOML must fail")
	}
	if failure.Diagnostics()[0].Code != "toml.parse.syntax@1" {
		t.Fatalf("code = %s", failure.Diagnostics()[0].Code)
	}

	duplicate := readFixture(t, "invalid-duplicate.toml")
	_, failure = Parse(duplicate, Toml10V1, document.DefaultParseLimits())
	if failure == nil || failure.Diagnostics()[0].Code != "toml.parse.syntax@1" {
		t.Fatalf("duplicate key must fail with toml.parse.syntax@1")
	}

	_, failure = Parse([]byte("x = 1"), Toml10V1, document.ParseLimits{MaxSourceBytes: 3})
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		t.Fatalf("source limit must fail")
	}

	_, failure = Parse([]byte("values = [1, 2, 3]"), Toml10V1,
		document.ParseLimits{MaxSourceBytes: 64 << 20, MaxNestingDepth: 256, MaxTokenCount: 3, MaxNodeCount: 1 << 20, MaxDiagnostics: 10000})
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		t.Fatalf("token limit must fail")
	}
	if failure.Diagnostics()[0].Arguments["name"] != "token_count" {
		t.Fatalf("limit name = %s", failure.Diagnostics()[0].Arguments["name"])
	}

	_, failure = Parse([]byte("value = [[[[1]]]]"), Toml10V1,
		document.ParseLimits{MaxSourceBytes: 64 << 20, MaxNestingDepth: 2, MaxTokenCount: 2 << 20, MaxNodeCount: 1 << 20, MaxDiagnostics: 10000})
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		t.Fatalf("depth limit must fail")
	}

	_, failure = Parse([]byte("value = [[[[1]]]]"), Toml10V1,
		document.ParseLimits{MaxSourceBytes: 64 << 20, MaxNestingDepth: 256, MaxTokenCount: 2 << 20, MaxNodeCount: 3, MaxDiagnostics: 10000})
	if failure == nil || failure.Diagnostics()[0].Code != "core.parse.resource-limit@1" {
		t.Fatalf("node limit must fail")
	}

	_, failure = Parse([]byte{0xFF}, Toml10V1, document.DefaultParseLimits())
	if failure == nil || failure.Diagnostics()[0].Code != "core.source.invalid-utf8@1" {
		t.Fatalf("invalid UTF-8 must fail with core.source.invalid-utf8@1")
	}
}

func TestItemHandlesAreSnapshotAndRoleBound(t *testing.T) {
	first := mustParse(t, []byte("x = 1"))
	second := mustParse(t, []byte("x = 2"))
	if _, err := second.Item(first.Root().NodeRef()); err == nil {
		t.Fatalf("cross-snapshot handle must fail")
	} else if err.(*TomlAccessError).Kind != TomlAccessWrongSnapshot {
		t.Fatalf("wrong snapshot kind")
	}
	entry := rootEntry(t, first.Root(), "x")
	if _, err := first.Item(entry.NodeRef()); err == nil {
		t.Fatalf("entry role handle must fail")
	} else if err.(*TomlAccessError).Kind != TomlAccessWrongRole {
		t.Fatalf("wrong role kind")
	}
}

func TestTomlLosslessSyntaxKindsDistinguishTokens(t *testing.T) {
	doc := mustParse(t, []byte("a.b = \"x\" # c\r\nlist = [1, 2]\ninline = {x=1}\n"))
	kinds := doc.LosslessSyntaxKinds()
	expected := []TomlSyntaxKind{
		SyntaxKindBare, SyntaxKindDot, SyntaxKindBare, SyntaxKindWhitespace,
		SyntaxKindEquals, SyntaxKindWhitespace, SyntaxKindString, SyntaxKindWhitespace,
		SyntaxKindComment, SyntaxKindNewline,
	}
	for index, kind := range expected {
		if kinds[index] != kind {
			t.Fatalf("kind[%d] = %s, want %s", index, kinds[index], kind)
		}
	}
	hasKind := func(kind TomlSyntaxKind) bool {
		for _, candidate := range kinds {
			if candidate == kind {
				return true
			}
		}
		return false
	}
	for _, kind := range []TomlSyntaxKind{SyntaxKindLeftBracket, SyntaxKindRightBracket,
		SyntaxKindLeftBrace, SyntaxKindRightBrace, SyntaxKindComma} {
		if !hasKind(kind) {
			t.Fatalf("missing kind %s", kind)
		}
	}
	if len(kinds) != len(doc.LosslessStructuralIndex().Pieces()) {
		t.Fatalf("kind count mismatch")
	}
}

func TestCorpusDocuments(t *testing.T) {
	for _, name := range []string{"pyproject.toml"} {
		source := readFixture(t, name)
		doc := mustParse(t, source)
		if string(doc.Render()) != string(source) {
			t.Fatalf("%s render is not byte-exact", name)
		}
		result := doc.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
		if result.Complete == nil {
			t.Fatalf("%s projection failed", name)
		}
	}
	// The corpus Cargo.toml (toml.corpus.cargo-manifest) is the
	// conformance fixture conformance/fixtures/toml/Cargo.toml.
	for _, candidate := range []string{
		"../Cargo.toml",
		"../../Cargo.toml",
		"../../conformance/fixtures/toml/Cargo.toml",
	} {
		bytes, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		doc := mustParse(t, bytes)
		if string(doc.Render()) != string(bytes) {
			t.Fatalf("Cargo.toml render is not byte-exact")
		}
		result := doc.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
		if result.Complete == nil {
			t.Fatalf("Cargo.toml projection failed")
		}
		return
	}
	t.Fatal("Cargo.toml not found")
}

// executable builds a bound executable query for the toml native domain.
func executable(t *testing.T, expression *protocol.QueryExpression,
	selection protocol.QuerySelection) *protocol.ExecutableQuery {
	t.Helper()
	definition := protocol.NewQueryDefinition(protocol.DomainTOMLNativeV1()).
		WithExpression(expression).
		WithSelection(selection)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %v", failure)
	}
	return bound
}

func namedRootExpression(name string) *protocol.QueryExpression {
	return (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1)).
		Then(protocol.NewOperatorCall("toml.entry-name-equals", 1).
			WithArgument("name", core.String(name))).
		Then(protocol.NewOperatorCall("toml.entry-item", 1))
}

func TestNestedEntryQueryRetainsDirectTomlRoles(t *testing.T) {
	doc := mustParse(t, []byte("server.host = 'localhost'\nserver.ports = [80, 443]\n"))
	expression := namedRootExpression("server").
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1))
	matches, failure := ExecuteTomlQuery(context.Background(), executable(t, expression, protocol.SelectionAll),
		doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %v", failure)
	}
	if len(matches) != 2 {
		t.Fatalf("match count = %d", len(matches))
	}
	if matches[0].Kind != "Entry" || matches[0].Name != "host" {
		t.Fatalf("match 0 = %+v", matches[0])
	}
	if matches[1].Name != "ports" {
		t.Fatalf("match 1 = %+v", matches[1])
	}
}

func TestArrayQueryObeysSelectionAndCancellation(t *testing.T) {
	doc := mustParse(t, []byte("values = [1, 2, 3]"))
	expression := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.try-table-entries", 1)).
		Then(protocol.NewOperatorCall("toml.entry-item", 1)).
		Then(protocol.NewOperatorCall("toml.try-array-elements", 1)).
		Then(protocol.NewOperatorCall("toml.array-element-item", 1))
	matches, failure := ExecuteTomlQuery(context.Background(),
		executable(t, expression, protocol.SelectionLast), doc, protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %v", failure)
	}
	if len(matches) != 1 || matches[0].KindName != ItemKindInteger {
		t.Fatalf("selection-last result = %+v", matches)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, failure = ExecuteTomlQuery(ctx, executable(t, expression, protocol.SelectionAll),
		doc, protocol.DefaultQueryLimits())
	if failure == nil || failure.Code() != "core.query.cancelled@1" {
		t.Fatalf("cancellation must fail with core.query.cancelled@1")
	}
}

func TestLosslessSyntaxQueryPreservesOrderKindAndText(t *testing.T) {
	doc := mustParse(t, []byte("a = 1 # note\nb = 2\n"))
	newlines := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.syntax-kind-is", 1).
			WithArgument("kind", core.String("Newline")))
	comment := (&protocol.QueryExpression{Kind: protocol.ExpressionInput}).
		Then(protocol.NewOperatorCall("toml.syntax-text-equals", 1).
			WithArgument("text", core.String("# note")))
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionStructureOrderMerge,
		Branches: []*protocol.QueryExpression{newlines, comment}}
	definition := protocol.NewQueryDefinition(protocol.DomainTOMLLosslessSyntaxV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("validation: %v", failure)
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	bound, failure := validated.Bind(capabilities)
	if failure != nil {
		t.Fatalf("binding: %v", failure)
	}
	matches, failure := ExecuteTomlSyntaxQuery(context.Background(), bound, doc,
		protocol.DefaultQueryLimits())
	if failure != nil {
		t.Fatalf("query: %v", failure)
	}
	if len(matches) != 3 {
		t.Fatalf("match count = %d", len(matches))
	}
	if matches[0].Kind() != SyntaxKindComment {
		t.Fatalf("match 0 kind = %s", matches[0].Kind())
	}
	if matches[0].NodeRef().Role() != document.RoleTomlSyntaxPiece {
		t.Fatalf("match 0 role = %s", matches[0].NodeRef().Role())
	}
	text := string(doc.Source().Bytes()[matches[0].Span().StartByte():matches[0].Span().EndByte()])
	if text != "# note" {
		t.Fatalf("match 0 text = %q", text)
	}
	if matches[0].Ordinal() > matches[1].Ordinal() || matches[1].Ordinal() > matches[2].Ordinal() {
		t.Fatalf("ordinals out of order")
	}
}

func TestProjectionAllCoreKinds(t *testing.T) {
	doc := mustParse(t, readFixture(t, "all-values.toml"))
	result := doc.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
	if result.Failed != nil {
		t.Fatalf("projection failed: %v", result.Failed.Diagnostics[0].Code)
	}
	complete := result.Complete
	if complete.Fidelity != FidelityExact {
		t.Fatalf("fidelity = %s", complete.Fidelity)
	}
	root, ok := complete.Value.(*core.Object)
	if !ok {
		t.Fatalf("root is not Object")
	}
	kinds := map[core.Kind]bool{}
	for _, entry := range root.Entries() {
		kinds[entry.Value.Kind()] = true
	}
	for _, kind := range []core.Kind{core.KindString, core.KindBoolean, core.KindInteger,
		core.KindBinaryFloat64, core.KindDate, core.KindTime, core.KindLocalDateTime,
		core.KindOffsetDateTime, core.KindArray, core.KindObject} {
		if !kinds[kind] {
			t.Fatalf("missing projected kind %v", kind)
		}
	}
	if complete.Report.Events() != nil && len(complete.Report.Events()) != 0 {
		t.Fatalf("exact projection emits no events")
	}
	if len(complete.Provenance.Entries()) == 0 {
		t.Fatalf("no provenance")
	}
}

func TestProjectionProvenanceAndLeapSecond(t *testing.T) {
	doc := mustParse(t, []byte("point = { x = 1, y = 2 }\n"))
	result := doc.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
	if result.Failed != nil {
		t.Fatalf("projection failed")
	}
	snapshot := doc.SnapshotIdentity()
	hasObjectEntry := false
	for _, entry := range result.Complete.Provenance.Entries() {
		for _, origin := range entry.Origins {
			if origin.Snapshot != snapshot ||
				origin.Node.Snapshot() != snapshot || origin.Span.Snapshot() != snapshot {
				t.Fatalf("provenance origin is not snapshot bound")
			}
		}
		if entry.Projected.Kind == "Association" &&
			entry.Projected.Association.Role() == protocol.AssociationRoleObjectEntry {
			hasObjectEntry = true
		}
	}
	if !hasObjectEntry {
		t.Fatalf("no ObjectEntry association provenance")
	}

	leap := mustParse(t, []byte("time = 23:59:60\n"))
	result = leap.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
	if result.Complete != nil {
		t.Fatalf("leap second projection must fail")
	}
	if result.Failed.Diagnostics[0].Code != "toml.projection.unrepresentable-datetime@1" {
		t.Fatalf("code = %s", result.Failed.Diagnostics[0].Code)
	}
}

func TestMaterializeCanonicalDocumentRoundTrips(t *testing.T) {
	date, err := core.NewDate(big.NewInt(2026), 8, 4)
	if err != nil {
		t.Fatalf("date: %v", err)
	}
	time, err := core.NewTime(12, 34, 56, core.NewDecimal(big.NewInt(123), big.NewInt(-3)))
	if err != nil {
		t.Fatalf("time: %v", err)
	}
	local := core.NewLocalDateTime(date, time)
	offset, err := core.NewOffsetDateTime(local, 8*60*60)
	if err != nil {
		t.Fatalf("offset: %v", err)
	}
	root, err := core.NewObject(
		core.Entry{Key: "date", Value: date},
		core.Entry{Key: "time", Value: time},
		core.Entry{Key: "local", Value: local},
		core.Entry{Key: "offset", Value: offset},
		core.Entry{Key: "items", Value: core.NewArray(core.NewInteger(big.NewInt(1)), core.String("two"))},
		core.Entry{Key: "nested", Value: mustObject(t, core.Entry{Key: "enabled", Value: core.Boolean(true)})},
		core.Entry{Key: "float", Value: core.NewBinaryFloat64(math.Float64bits(1.5))},
		core.Entry{Key: "nan", Value: core.NewBinaryFloat64(0x7ff8000000000000)},
		core.Entry{Key: "inf", Value: core.NewBinaryFloat64(math.Float64bits(math.Inf(1)))},
		core.Entry{Key: "negative", Value: core.NewBinaryFloat64(math.Float64bits(math.Copysign(0, -1)))},
	)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	request := document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1))
	result := Materialize(root, request)
	if result.Failed != nil {
		t.Fatalf("materialization failed: %v", result.Failed.Failure)
	}
	complete := result.Complete
	if complete.Fidelity != MaterializationFidelityExact {
		t.Fatalf("fidelity = %s", complete.Fidelity)
	}
	projection := complete.Document.Project(NewProjectionRequest(ProjectionTargetBestExactCoreV1))
	if projection.Failed != nil {
		t.Fatalf("reprojection failed")
	}
	if !core.Equal(projection.Complete.Value, root) {
		t.Fatalf("reprojection differs from the input")
	}
	if len(complete.Provenance.Entries()) == 0 {
		t.Fatalf("no provenance")
	}
	rendered := string(complete.Document.Render())
	for _, expected := range []string{`"date" = 2026-08-04`, `"nan" = nan`, `"inf" = inf`,
		`"negative" = -0.0`, `"time" = 12:34:56.123`, `"offset" = 2026-08-04T12:34:56.123+08:00`,
		`"items" = [1, "two"]`, `"nested" = { "enabled" = true }`} {
		if !contains(rendered, expected) {
			t.Fatalf("output lacks %q:\n%s", expected, rendered)
		}
	}
}

func mustObject(t *testing.T, entries ...core.Entry) *core.Object {
	t.Helper()
	object, err := core.NewObject(entries...)
	if err != nil {
		t.Fatalf("object: %v", err)
	}
	return object
}

func contains(text, fragment string) bool {
	for index := 0; index+len(fragment) <= len(text); index++ {
		if text[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}

func TestMaterializeMappingPolicyAndFailures(t *testing.T) {
	request := document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)).
		WithMappingPolicy(document.MappingPolicyUniqueStringEntriesToObject)
	mapping, err := core.NewEntryMapping(
		core.EntryMappingEntry{Key: core.String("a"), Value: core.Boolean(true)},
		core.EntryMappingEntry{Key: core.String("b"), Value: core.NewInteger(big.NewInt(2))},
	)
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	result := Materialize(mapping, request)
	if result.Failed != nil {
		t.Fatalf("mapping materialization failed: %v", result.Failed.Failure)
	}
	if result.Complete.Fidelity != MaterializationFidelityTransformed {
		t.Fatalf("fidelity = %s", result.Complete.Fidelity)
	}
	if string(result.Complete.Document.Render()) != "\"a\" = true\n\"b\" = 2\n" {
		t.Fatalf("output = %q", result.Complete.Document.Render())
	}
	if len(result.Complete.Report.Events()) != 1 ||
		result.Complete.Report.Events()[0].Code != "core.materialization.mapping-transformed@1" {
		t.Fatalf("missing mapping-transformed event")
	}

	// Null root is unrepresentable.
	nullResult := Materialize(core.NullValue(), document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)))
	if nullResult.Complete != nil ||
		nullResult.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
		t.Fatalf("null must be unrepresentable")
	}

	// A non-canonical NaN payload is unrepresentable.
	nanRoot := mustObject(t, core.Entry{Key: "nan", Value: core.NewBinaryFloat64(0x7ff8000000000001)})
	nanResult := Materialize(nanRoot, document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)))
	if nanResult.Complete != nil ||
		nanResult.Failed.Failure.Code() != "core.materialization.unrepresentable@1" {
		t.Fatalf("payload NaN must be unrepresentable")
	}

	// Output limit.
	limited := document.NewMaterializationRequest(
		document.NewProfileId("toml.1.0", 1),
		document.NewMaterializationStyleId("toml.canonical-document", 1)).
		WithLimits(document.MaterializationLimits{MaxOutputBytes: 3})
	limitResult := Materialize(mustObject(t, core.Entry{Key: "value", Value: core.Boolean(true)}), limited)
	if limitResult.Complete != nil ||
		limitResult.Failed.Failure.Code() != "core.materialization.resource-limit@1" {
		t.Fatalf("output limit must fail")
	}
}

func TestEditLiteralAndSemanticScalar(t *testing.T) {
	doc := mustParse(t, []byte("hex = 0x2A # keep\nname = 'old'\nfloat = 1.0\n"))
	hex := rootEntry(t, doc.Root(), "hex").Item()
	name := rootEntry(t, doc.Root(), "name").Item()
	float := rootEntry(t, doc.Root(), "float").Item()
	builder := NewEditTransactionBuilder(doc)
	builder.LiteralScalar(hex.NodeRef(), []byte("0x2B"))
	builder.SemanticScalar(name.NodeRef(), core.String("new\nvalue"), RepresentationPreserveCompatible)
	builder.SemanticScalar(float.NodeRef(), core.NewBinaryFloat64(math.Float64bits(math.Copysign(0, -1))),
		RepresentationPreserveCompatible)
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit: %v", failure)
	}
	if string(commit.Document.Render()) != "hex = 0x2B # keep\nname = \"new\\nvalue\"\nfloat = -0.0\n" {
		t.Fatalf("rendered = %q", commit.Document.Render())
	}
	if len(commit.ChangeSet.SourceEdits()) != 3 {
		t.Fatalf("source edit count = %d", len(commit.ChangeSet.SourceEdits()))
	}
	patch := commit.SourcePatch
	replayed, err := patch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("patch apply: %v", err)
	}
	if string(replayed.Bytes()) != string(commit.Document.Render()) {
		t.Fatalf("patch replay differs")
	}
	if err := commit.UntouchedProof.Verify(doc.Source(), commit.Document.Source(),
		patch.Replacements()); err != nil {
		t.Fatalf("proof verify: %v", err)
	}
	if len(commit.ChangeSet.NodeMappings()) != 3 {
		t.Fatalf("node mapping count = %d", len(commit.ChangeSet.NodeMappings()))
	}
}

func TestEditFailuresAreAtomic(t *testing.T) {
	doc := mustParse(t, []byte("value = 1\narray = [1, 2]\n"))
	value := rootEntry(t, doc.Root(), "value").Item()
	array := rootEntry(t, doc.Root(), "array").Item()

	builder := NewEditTransactionBuilder(doc)
	builder.SemanticScalar(value.NodeRef(), core.String("one"), RepresentationPreserveCompatible)
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureRepresentationIncompatible {
		t.Fatalf("kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.LiteralScalar(array.NodeRef(), []byte("3"))
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureWrongRole {
		t.Fatalf("container literal must be WrongRole")
	}

	builder = NewEditTransactionBuilder(doc)
	builder.LiteralScalar(value.NodeRef(), []byte("2"))
	builder.LiteralScalar(value.NodeRef(), []byte("3"))
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureDuplicateTarget {
		t.Fatalf("duplicate target kind = %v", failure.Kind)
	}
	if string(doc.Render()) != "value = 1\narray = [1, 2]\n" {
		t.Fatalf("base changed after failure")
	}

	builder = NewEditTransactionBuilder(doc)
	builder.SemanticScalar(value.NodeRef(), core.NewBinaryFloat64(0x7ff8000000000001),
		RepresentationCanonicalForProfile)
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureUnsupportedSemanticValue {
		t.Fatalf("payload NaN must be unsupported")
	}

	// Invalid literals.
	for _, literal := range [][]byte{[]byte(" 2"), []byte("2 # comment"), []byte("[1, 2]"),
		[]byte("2\nother = 3")} {
		builder = NewEditTransactionBuilder(doc)
		builder.LiteralScalar(value.NodeRef(), literal)
		if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureInvalidLiteral {
			t.Fatalf("literal %q kind = %v", literal, failure.Kind)
		}
	}
}

func TestEditRootAndStandardTableInsertions(t *testing.T) {
	doc := mustParse(t, []byte("root = 1\n\n[service]\nport = 80\n"))
	service := rootEntry(t, doc.Root(), "service").Item()

	builder := NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Root().NodeRef(), "enabled", core.Boolean(true), PlacementEnd())
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("root insert: %v", failure)
	}
	if string(commit.Document.Render()) != "root = 1\n\n\"enabled\" = true\n[service]\nport = 80\n" {
		t.Fatalf("root insert rendered = %q", commit.Document.Render())
	}
	if enabled := rootEntry(t, commit.Document.Root(), "enabled").Item(); !asBoolean(enabled) {
		t.Fatalf("root-owned insertion missing")
	}

	builder = NewEditTransactionBuilder(doc)
	builder.InsertEntry(service.NodeRef(), "host", core.String("localhost"), PlacementEnd())
	commit, failure = doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("table insert: %v", failure)
	}
	if string(commit.Document.Render()) != "root = 1\n\n[service]\nport = 80\n\"host\" = \"localhost\"" {
		t.Fatalf("table insert rendered = %q", commit.Document.Render())
	}
}

func TestEditInlineTableOperations(t *testing.T) {
	doc := mustParse(t, []byte("point = { a = 1, b = 2 }\n"))
	point := rootEntry(t, doc.Root(), "point").Item()
	entries, _ := point.TableEntries()

	builder := NewEditTransactionBuilder(doc)
	builder.InsertEntry(point.NodeRef(), "axis",
		core.NewArray(core.Boolean(true)), PlacementBefore(entries[1].NodeRef()))
	commit, failure := doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("inline insert: %v", failure)
	}
	if string(commit.Document.Render()) != "point = { a = 1, \"axis\" = [true],b = 2 }\n" {
		t.Fatalf("inline insert rendered = %q", commit.Document.Render())
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RenameEntry(entries[1].NodeRef(), "beta")
	commit, failure = doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("inline rename: %v", failure)
	}
	if string(commit.Document.Render()) != "point = { a = 1, \"beta\" = 2 }\n" {
		t.Fatalf("inline rename rendered = %q", commit.Document.Render())
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RemoveEntry(entries[0].NodeRef())
	commit, failure = doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("inline remove: %v", failure)
	}
	if string(commit.Document.Render()) != "point = {  b = 2 }\n" {
		t.Fatalf("inline remove rendered = %q", commit.Document.Render())
	}
}

func TestEditArrayInsertAndRemove(t *testing.T) {
	empty := mustParse(t, []byte("items = [ ]\n"))
	array := rootEntry(t, empty.Root(), "items").Item()
	builder := NewEditTransactionBuilder(empty)
	builder.InsertArrayElement(array.NodeRef(), core.NewInteger(big.NewInt(1)), PlacementStart())
	commit, failure := empty.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("empty array insert: %v", failure)
	}
	if string(commit.Document.Render()) != "items = [1 ]\n" {
		t.Fatalf("empty array insert rendered = %q", commit.Document.Render())
	}

	doc := mustParse(t, []byte("items = [1, # keep\n 2, 3,]\n"))
	array = rootEntry(t, doc.Root(), "items").Item()
	elements, _ := array.ArrayElements()
	builder = NewEditTransactionBuilder(doc)
	builder.InsertArrayElement(array.NodeRef(), core.String("end"), PlacementAfter(elements[2].NodeRef()))
	commit, failure = doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("array insert: %v", failure)
	}
	if string(commit.Document.Render()) != "items = [1, # keep\n 2, 3,\"end\",]\n" {
		t.Fatalf("array insert rendered = %q", commit.Document.Render())
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RemoveArrayElement(elements[1].NodeRef())
	commit, failure = doc.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("array remove: %v", failure)
	}
	if string(commit.Document.Render()) != "items = [1, # keep\n  3,]\n" {
		t.Fatalf("array remove rendered = %q", commit.Document.Render())
	}
}

func TestEditDependenciesAndTableRulesFailAtomically(t *testing.T) {
	doc := mustParse(t, []byte("a = 1\nb = 2\n\n[service]\nport = 80\n"))
	entries, _ := doc.Root().TableEntries()
	var a, b, service TomlEntry
	for _, entry := range entries {
		switch entry.Name() {
		case "a":
			a = entry
		case "b":
			b = entry
		case "service":
			service = entry
		}
	}

	builder := NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Root().NodeRef(), "a", core.Boolean(true), PlacementStart())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureDuplicateKey {
		t.Fatalf("duplicate key kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RenameEntry(b.NodeRef(), "a")
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureDuplicateKey {
		t.Fatalf("duplicate rename kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RemoveEntry(a.NodeRef())
	builder.InsertEntry(doc.Root().NodeRef(), "x", core.Boolean(true), PlacementBefore(a.NodeRef()))
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailurePlacementAnchorRemoved {
		t.Fatalf("removed anchor kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RenameEntry(a.NodeRef(), "x")
	builder.RemoveEntry(a.NodeRef())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureDuplicateTarget {
		t.Fatalf("duplicate target kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.RemoveEntry(service.NodeRef())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureUnsupportedOperation {
		t.Fatalf("table remove kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.SemanticScalar(a.Item().NodeRef(), core.NewInteger(big.NewInt(3)),
		RepresentationPreserveCompatible)
	builder.RemoveEntry(a.NodeRef())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureAncestorDescendantConflict {
		t.Fatalf("ancestor-descendant kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Root().NodeRef(), "x", core.Boolean(true), PlacementEnd())
	builder.InsertEntry(doc.Root().NodeRef(), "y", core.Boolean(false), PlacementEnd())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureOverlappingOwnership {
		t.Fatalf("same boundary kind = %v", failure.Kind)
	}

	builder = NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Root().NodeRef(), "null", core.NullValue(), PlacementStart())
	if _, failure := doc.Commit(builder.Build()); failure.Kind != EditFailureUnrepresentableValue {
		t.Fatalf("null value kind = %v", failure.Kind)
	}
	if string(doc.Render()) != "a = 1\nb = 2\n\n[service]\nport = 80\n" {
		t.Fatalf("base changed after failures")
	}
}

func TestDryRunAndCommitHaveIdenticalPatchAndTargetDigest(t *testing.T) {
	doc := mustParse(t, []byte("value = 1\n"))
	builder := NewEditTransactionBuilder(doc)
	builder.InsertEntry(doc.Root().NodeRef(), "secret-key", core.String("secret-value"), PlacementEnd())
	transaction := builder.Build()
	sourceID, err := NewEditPlanSourceId("config.toml")
	if err != nil {
		t.Fatalf("source id: %v", err)
	}
	plan, failure := doc.DryRun(transaction, *sourceID)
	if failure != nil {
		t.Fatalf("dry run: %v", failure)
	}
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		t.Fatalf("commit: %v", failure)
	}
	if string(commit.Document.Render()) != "value = 1\n\"secret-key\" = \"secret-value\"" {
		t.Fatalf("rendered = %q", commit.Document.Render())
	}
	if len(plan.Replacements()) != len(commit.SourcePatch.Replacements()) {
		t.Fatalf("replacement count differs")
	}
	if !plan.TargetDigest().Equal(commit.SourcePatch.TargetDigest()) {
		t.Fatalf("target digest differs")
	}
	redacted, err := plan.WithAllReplacementsRedacted(true, true)
	if err != nil {
		t.Fatalf("redaction: %v", err)
	}
	if contains(redacted.String(), "secret") {
		t.Fatalf("redacted plan leaks the secret")
	}
	replayed, err := plan.SourcePatch().Apply(doc.Source(), document.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatalf("plan patch apply: %v", err)
	}
	if string(replayed.Bytes()) != string(commit.Document.Render()) {
		t.Fatalf("plan patch replay differs")
	}
}

func TestUntouchedProofDetectsTampering(t *testing.T) {
	base := mustParse(t, []byte("value = 1\n"))
	builder := NewEditTransactionBuilder(base)
	builder.LiteralScalar(rootEntry(t, base.Root(), "value").Item().NodeRef(), []byte("2"))
	commit, failure := base.Commit(builder.Build())
	if failure != nil {
		t.Fatalf("commit: %v", failure)
	}
	tampered, err := document.NewSourceSnapshotFromUTF8([]byte("value = 3\n"))
	if err != nil {
		t.Fatalf("tampered source: %v", err)
	}
	if err := commit.UntouchedProof.Verify(base.Source(), tampered,
		commit.SourcePatch.Replacements()); err == nil {
		t.Fatalf("tampered target must fail verification")
	}
}

func TestFormatOperationRegistry(t *testing.T) {
	registry := NewFormatOperationRegistry(Toml10V1)
	operations := registry.Operations()
	if len(operations) != 7 {
		t.Fatalf("operation count = %d", len(operations))
	}
	found := false
	for _, operation := range operations {
		if operation.ID.ID() == "toml.edit.insert-entry" {
			found = true
		}
	}
	if !found {
		t.Fatalf("insert-entry missing")
	}
}

func asBoolean(item TomlItem) bool {
	value, ok := item.AsBoolean()
	return ok && value
}
