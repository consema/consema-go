package protocol

import (
	"math/big"
	"testing"

	"consema.dev/consema/core"
)

func strArg(name, value string) core.Value { return core.String(value) }

func TestQueryDomainInputRoles(t *testing.T) {
	// Every frozen domain validates and pins its root role (query.rs:500-523).
	domains := []struct {
		domain *QueryDomain
		role   MatchRole
	}{
		{DomainPortableValueV1(), RoleValue},
		{DomainPortableGraphV1(), RoleGraphNode},
		{DomainJSONNativeV1(), RoleJsonValue},
		{DomainJSONNativeV2(), RoleJsonValue},
		{DomainTOMLNativeV1(), RoleTomlItem},
		{DomainYAMLNativeV1(), RoleYamlStream},
		{DomainININativeV1(), RoleIniDocument},
		{DomainJavaPropertiesNativeV1(), RolePropertiesDocument},
		{DomainXMLNativeV1(), RoleXmlDocument},
		{DomainJSONLosslessSyntaxV1(), RoleJsonSyntaxPiece},
		{DomainJSONLosslessSyntaxV2(), RoleJsonSyntaxPiece},
		{DomainTOMLLosslessSyntaxV1(), RoleTomlSyntaxPiece},
		{DomainYAMLLosslessSyntaxV1(), RoleYamlSyntaxPiece},
		{DomainINILosslessSyntaxV1(), RoleIniSyntaxPiece},
		{DomainJavaPropertiesLosslessSyntaxV1(), RolePropertiesSyntaxPiece},
		{DomainXMLLosslessSyntaxV1(), RoleXmlSyntaxPiece},
		{DomainPlistNativeV1(), RolePlistValue},
		{DomainPlistLosslessSyntaxV1(), RolePlistSyntaxPiece},
		{DomainPlistBinaryStructureV1(), RolePlistBinaryStructure},
		{DomainHCLNativeV1(), RoleHclBody},
		{DomainHCLLosslessSyntaxV1(), RoleHclSyntaxPiece},
	}
	for _, row := range domains {
		definition := NewQueryDefinition(row.domain)
		validated, failure := definition.Validate()
		if failure != nil {
			t.Errorf("%s@%d validate failed: %v", row.domain.ID(), row.domain.Version(), failure)
			continue
		}
		if validated.OutputRole() != row.role {
			t.Errorf("%s@%d output role = %s, want %s",
				row.domain.ID(), row.domain.Version(), validated.OutputRole(), row.role)
		}
	}
	// An unknown domain is a DomainMismatch with the registered code.
	unknown := NewQueryDomain("example.domain", 1)
	_, failure := NewQueryDefinition(unknown).Validate()
	if failure == nil || failure.Kind != FailureDomainMismatch || failure.Code() != "core.query.domain-mismatch@1" {
		t.Errorf("unknown domain: got %v", failure)
	}
	// An unknown domain version is a DomainMismatch.
	_, failure = NewQueryDefinition(NewQueryDomain("core.portable-value-query", 2)).Validate()
	if failure == nil || failure.Kind != FailureDomainMismatch {
		t.Errorf("unknown domain version: got %v", failure)
	}
}

func TestPortableValueOperatorChains(t *testing.T) {
	// A complete object-entry chain validates
	// (query.rs:912-921, 1596-1604).
	definition := NewQueryDefinition(DomainPortableValueV1())
	definition.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).Then(
			NewOperatorCall("core.try-object-entries", 1)).
			Then(NewOperatorCall("core.object-entry-name-equals", 1).
				WithArgument("name", strArg("name", "password"))).
			Then(NewOperatorCall("core.object-entry-value", 1)),
	)
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatalf("chain validation failed: %v", failure)
	}
	if validated.OutputRole() != RoleValue {
		t.Errorf("chain output role = %s, want Value", validated.OutputRole())
	}
	// Composition errors surface with the registered code.
	bad := NewQueryDefinition(DomainPortableValueV1())
	bad.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.object-entry-value", 1)))
	_, failure = bad.Validate()
	if failure == nil || failure.Kind != FailureInvalidOperatorComposition ||
		failure.Code() != "core.query.invalid-composition@1" {
		t.Errorf("composition: got %v", failure)
	}
	// Unknown operators are rejected.
	unknown := NewQueryDefinition(DomainPortableValueV1())
	unknown.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.unknown", 1)))
	_, failure = unknown.Validate()
	if failure == nil || failure.Kind != FailureUnknownOperator ||
		failure.Code() != "core.query.unknown-operator@1" {
		t.Errorf("unknown operator: got %v", failure)
	}
	// Wrong argument types are rejected.
	wrongType := NewQueryDefinition(DomainPortableValueV1())
	wrongType.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.try-object-entries", 1)).
		Then(NewOperatorCall("core.object-entry-name-equals", 1).
			WithArgument("name", core.NewInteger(newInt64(1)))))
	_, failure = wrongType.Validate()
	if failure == nil || failure.Kind != FailureWrongArgumentType ||
		failure.Code() != "core.query.wrong-argument-type@1" {
		t.Errorf("wrong argument type: got %v", failure)
	}
	// Missing or extra arguments are invalid.
	missing := NewQueryDefinition(DomainPortableValueV1())
	missing.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.try-object-entries", 1)).
		Then(NewOperatorCall("core.object-entry-name-equals", 1)))
	_, failure = missing.Validate()
	if failure == nil || failure.Kind != FailureInvalidArgument {
		t.Errorf("missing argument: got %v", failure)
	}
	// core.take requires a non-negative integer count.
	take := NewQueryDefinition(DomainPortableValueV1())
	take.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.take", 1).
			WithArgument("count", core.NewInteger(newInt64(-1)))))
	_, failure = take.Validate()
	if failure == nil || failure.Kind != FailureInvalidArgument {
		t.Errorf("negative take count: got %v", failure)
	}
	// core.distinct-by-identity is domain-agnostic.
	distinct := NewQueryDefinition(DomainPortableValueV1())
	distinct.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.distinct-by-identity", 1)))
	if _, failure := distinct.Validate(); failure != nil {
		t.Errorf("distinct-by-identity: %v", failure)
	}
	// Concat branches must agree on the output role.
	concat := NewQueryDefinition(DomainPortableValueV1())
	concat.WithExpression(&QueryExpression{Kind: ExpressionConcat, Branches: []*QueryExpression{
		{Kind: ExpressionInput},
		(&QueryExpression{Kind: ExpressionInput}).Then(NewOperatorCall("core.try-object-entries", 1)),
	}})
	_, failure = concat.Validate()
	if failure == nil || failure.Kind != FailureInvalidOperatorComposition {
		t.Errorf("concat mismatch: got %v", failure)
	}
}

func newInt64(value int64) *big.Int {
	return big.NewInt(value)
}

func TestIniDuplicateGroupOperatorRow(t *testing.T) {
	// ini.duplicate-group is the input-dependent table row (RoleAny
	// placeholder typed by checkInputDependentRoles; query.rs:1056-1065).
	// It validates from the section and entry inputs and rejects every
	// other input role with the registered composition code.
	fromSections := NewQueryDefinition(DomainININativeV1())
	fromSections.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).
			Then(NewOperatorCall("ini.document-sections", 1)).
			Then(NewOperatorCall("ini.duplicate-group", 1)),
	)
	validated, failure := fromSections.Validate()
	if failure != nil {
		t.Fatalf("section chain validation failed: %v", failure)
	}
	if validated.OutputRole() != RoleIniSection {
		t.Errorf("section chain output role = %s, want IniSection", validated.OutputRole())
	}

	fromEntries := NewQueryDefinition(DomainININativeV1())
	fromEntries.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).
			Then(NewOperatorCall("ini.all-entries", 1)).
			Then(NewOperatorCall("ini.duplicate-group", 1)),
	)
	validated, failure = fromEntries.Validate()
	if failure != nil {
		t.Fatalf("entry chain validation failed: %v", failure)
	}
	if validated.OutputRole() != RoleIniEntry {
		t.Errorf("entry chain output role = %s, want IniEntry", validated.OutputRole())
	}

	// The duplicate-group chain itself composes further (Section in,
	// Section out).
	chained := NewQueryDefinition(DomainININativeV1())
	chained.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).
			Then(NewOperatorCall("ini.document-sections", 1)).
			Then(NewOperatorCall("ini.duplicate-group", 1)).
			Then(NewOperatorCall("ini.section-entries", 1)),
	)
	if _, failure := chained.Validate(); failure != nil {
		t.Errorf("duplicate-group composition failed: %v", failure)
	}

	// A physical-line input is not a duplicate-group carrier: the row
	// rejects it with the frozen composition code.
	bad := NewQueryDefinition(DomainININativeV1())
	bad.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).
			Then(NewOperatorCall("ini.physical-lines", 1)).
			Then(NewOperatorCall("ini.duplicate-group", 1)),
	)
	_, failure = bad.Validate()
	if failure == nil || failure.Kind != FailureInvalidOperatorComposition ||
		failure.Code() != "core.query.invalid-composition@1" {
		t.Errorf("wrong-role chain: got %v", failure)
	}
}

func TestSyntaxKindVocabularies(t *testing.T) {
	// The frozen kind names validate; unknown names are invalid arguments.
	kindChecks := []struct {
		domain *QueryDomain
		valid  []string
	}{
		{DomainJSONLosslessSyntaxV1(), []string{"Bom", "Whitespace", "ErrorRegion"}},
		{DomainJSONLosslessSyntaxV2(), []string{"Identifier"}},
		{DomainTOMLLosslessSyntaxV1(), []string{"Bare", "Equals", "Dot"}},
		{DomainYAMLLosslessSyntaxV1(), []string{"DocumentStart", "Anchor", "Alias"}},
		{DomainINILosslessSyntaxV1(), []string{"SectionOpen", "EntryKey", "ErrorRegion"}},
		{DomainJavaPropertiesLosslessSyntaxV1(), []string{"Key", "Separator", "EscapeMarker"}},
		{DomainXMLLosslessSyntaxV1(), []string{"tag-close", "attribute-name", "error-region"}},
		{DomainPlistLosslessSyntaxV1(), []string{"plist-open", "key-open", "error-region"}},
	}
	for _, row := range kindChecks {
		for _, kind := range row.valid {
			definition := NewQueryDefinition(row.domain)
			definition.WithExpression((&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall(syntaxKindOperator(row.domain), 1).
					WithArgument("kind", strArg("kind", kind))))
			if _, failure := definition.Validate(); failure != nil {
				t.Errorf("%s@%d kind %s rejected: %v", row.domain.ID(), row.domain.Version(), kind, failure)
			}
		}
	}
	// A kind outside the vocabulary is an invalid argument.
	definition := NewQueryDefinition(DomainJSONLosslessSyntaxV1())
	definition.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", strArg("kind", "Identifier"))))
	_, failure := definition.Validate()
	if failure == nil || failure.Kind != FailureInvalidArgument ||
		failure.Code() != "core.query.invalid-argument@1" {
		t.Errorf("v1 Identifier kind: got %v", failure)
	}
	// ... but the JSON5 v2 domain accepts it.
	definitionV2 := NewQueryDefinition(DomainJSONLosslessSyntaxV2())
	definitionV2.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("json.syntax-kind-is", 1).
			WithArgument("kind", strArg("kind", "Identifier"))))
	if _, failure := definitionV2.Validate(); failure != nil {
		t.Errorf("v2 Identifier kind: %v", failure)
	}
}

func syntaxKindOperator(domain *QueryDomain) string {
	switch domain.ID() {
	case "json.lossless-syntax-query":
		return "json.syntax-kind-is"
	case "toml.lossless-syntax-query":
		return "toml.syntax-kind-is"
	case "yaml.lossless-syntax-query":
		return "yaml.syntax-kind-is"
	case "ini.lossless-syntax-query":
		return "ini.syntax-kind-is"
	case "java-properties.lossless-syntax-query":
		return "properties.syntax-kind-is"
	case "xml.lossless-syntax-query":
		return "xml.syntax-kind-is"
	case "plist.lossless-syntax-query":
		return "plist.syntax-kind-is"
	}
	return ""
}

func TestNativeOperatorChainsPerFamily(t *testing.T) {
	// One representative chain per native domain validates with the pinned
	// output role (query.rs:941-1594).
	chains := []struct {
		domain *QueryDomain
		chain  func() *QueryExpression
		role   MatchRole
	}{
		{DomainJSONNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("json.try-object-members", 1)).
				Then(NewOperatorCall("json.member-value", 1))
		}, RoleJsonValue},
		{DomainTOMLNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("toml.try-table-entries", 1)).
				Then(NewOperatorCall("toml.entry-item", 1))
		}, RoleTomlItem},
		{DomainYAMLNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("yaml.documents", 1)).
				Then(NewOperatorCall("yaml.document-root", 1))
		}, RoleYamlNode},
		{DomainININativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("ini.document-sections", 1)).
				Then(NewOperatorCall("ini.section-entries", 1))
		}, RoleIniEntry},
		{DomainJavaPropertiesNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("properties.document-properties", 1))
		}, RolePropertiesProperty},
		{DomainXMLNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("xml.document-root", 1)).
				Then(NewOperatorCall("xml.element-child-text", 1))
		}, RoleXmlText},
		{DomainPlistNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("plist.dict-entries", 1)).
				Then(NewOperatorCall("plist.dict-entry-value", 1))
		}, RolePlistValue},
		{DomainPlistBinaryStructureV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("plist.object-table", 1)).
				Then(NewOperatorCall("plist.object-refs", 1))
		}, RolePlistBinaryRef},
		{DomainHCLNativeV1(), func() *QueryExpression {
			return (&QueryExpression{Kind: ExpressionInput}).
				Then(NewOperatorCall("hcl.body-blocks", 1)).
				Then(NewOperatorCall("hcl.block-nested-body", 1))
		}, RoleHclBody},
	}
	for _, row := range chains {
		definition := NewQueryDefinition(row.domain)
		definition.WithExpression(row.chain())
		validated, failure := definition.Validate()
		if failure != nil {
			t.Errorf("%s chain: %v", row.domain.ID(), failure)
			continue
		}
		if validated.OutputRole() != row.role {
			t.Errorf("%s output = %s, want %s", row.domain.ID(), validated.OutputRole(), row.role)
		}
	}
	// The generic value operators validate on the core domain.
	definition := NewQueryDefinition(DomainPortableValueV1())
	definition.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.where-type", 1).
			WithArgument("kind", strArg("kind", "String"))))
	if _, failure := definition.Validate(); failure != nil {
		t.Errorf("where-type: %v", failure)
	}
	// where-type with a kind outside the 16-kind vocabulary is invalid.
	definition = NewQueryDefinition(DomainPortableValueV1())
	definition.WithExpression((&QueryExpression{Kind: ExpressionInput}).
		Then(NewOperatorCall("core.where-type", 1).
			WithArgument("kind", strArg("kind", "Bogus"))))
	if _, failure := definition.Validate(); failure == nil || failure.Kind != FailureInvalidArgument {
		t.Errorf("where-type bogus kind: got %v", failure)
	}
}

func TestQueryDefinitionProtocolValueRoundTrip(t *testing.T) {
	definition := NewQueryDefinition(DomainININativeV1())
	definition.WithExpression(
		(&QueryExpression{Kind: ExpressionInput}).
			Then(NewOperatorCall("ini.document-sections", 1)).
			Then(NewOperatorCall("ini.section-name-equals", 1).
				WithArgument("name", strArg("name", "server")).
				WithArgument("comparison", strArg("comparison", "ProfileEquivalent"))),
	).WithSelection(SelectionRequireOne)
	value, failure := definition.ToProtocolValue()
	if failure != nil {
		t.Fatal(failure)
	}
	decoded, failure := (&QueryDefinition{}).FromProtocolValue(value)
	if failure != nil {
		t.Fatal(failure)
	}
	if decoded.Domain().ID() != "ini.native-semantic-query" || decoded.Domain().Version() != 1 {
		t.Error("domain lost in round-trip")
	}
	if decoded.Selection() != SelectionRequireOne {
		t.Error("selection lost in round-trip")
	}
	// The decoded definition validates identically.
	validated, failure := decoded.Validate()
	if failure != nil {
		t.Fatalf("decoded definition does not validate: %v", failure)
	}
	if validated.OutputRole() != RoleIniSection {
		t.Error("decoded output role wrong")
	}
	// A malformed protocol value is rejected with the query codes.
	bad, _ := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.query-definition@1")},
		core.Entry{Key: "domain_id", Value: core.String("ini.native-semantic-query")},
		core.Entry{Key: "domain_version", Value: integerValue(1)},
		core.Entry{Key: "selection", Value: core.String("All")},
		core.Entry{Key: "expression", Value: core.String("Input")},
	)
	_, failure = (&QueryDefinition{}).FromProtocolValue(bad)
	if failure == nil || failure.Kind != FailureInvalidArgument {
		t.Errorf("malformed definition: got %v", failure)
	}
}

func TestQueryValidationDepthLimit(t *testing.T) {
	// Expressions nested beyond 256 levels hit the resource limit.
	expression := &QueryExpression{Kind: ExpressionInput}
	for index := 0; index < 300; index++ {
		expression = expression.Then(NewOperatorCall("core.try-sequence-elements", 1))
	}
	definition := NewQueryDefinition(DomainPortableValueV1()).WithExpression(expression)
	_, failure := definition.ToProtocolValue()
	if failure == nil || failure.Kind != FailureResourceLimit ||
		failure.Code() != "core.query.resource-limit@1" {
		t.Errorf("depth limit: got %v", failure)
	}
}

func TestQueryBinding(t *testing.T) {
	definition := NewQueryDefinition(DomainPortableValueV1())
	validated, failure := definition.Validate()
	if failure != nil {
		t.Fatal(failure)
	}
	// The required capability is core.query.ordered-results@1
	// (query.rs:526-529).
	capabilities := validated.RequiredCapabilities()
	if len(capabilities) != 1 || capabilities[0].Namespace() != "core.query.ordered-results" ||
		capabilities[0].Version() != 1 {
		t.Fatal("required capabilities wrong")
	}
	// Binding against a set without the capability fails with the registered
	// code.
	empty := NewCapabilitySet()
	_, failure = validated.Bind(empty)
	if failure == nil || failure.Kind != FailureMissingCapability ||
		failure.Code() != "core.query.missing-capability@1" {
		t.Errorf("bind without capability: got %v", failure)
	}
	// Binding against a set with the capability succeeds.
	full := NewCapabilitySet()
	full.Insert(NewCapabilityId("core.query.ordered-results", 1))
	executable, failure := validated.Bind(full)
	if failure != nil {
		t.Fatalf("bind failed: %v", failure)
	}
	if executable.OutputRole() != RoleValue {
		t.Error("executable output role wrong")
	}
}
