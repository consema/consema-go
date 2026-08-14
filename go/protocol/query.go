package protocol

import (
	"consema.dev/consema/core"
)

// This file implements the versioned typed query definitions and their
// validation/binding (consema-rs/consema-core/src/query.rs). The domain/operator
// tables are transcribed verbatim; the operator validation is the language-
// neutral contract (RFC 0016 §5.4), so the full fifteen-kind argument
// vocabulary is accepted at definition time, matching the Rust core kinds
// (doc.go).

// MatchRole is one typed match role of the query model. The roles use the
// language-neutral spelling of the Rust MatchRole enum
// (consema-core/src/query.rs); they type operator composition during
// validation and name the output matches of query results.
type MatchRole string

// The closed match-role vocabulary.
const (
	RoleValue                    MatchRole = "Value"
	RoleObjectEntry              MatchRole = "ObjectEntry"
	RoleEntryMappingEntry        MatchRole = "EntryMappingEntry"
	RoleJsonValue                MatchRole = "JsonValue"
	RoleJsonObjectMember         MatchRole = "JsonObjectMember"
	RoleJsonArrayElement         MatchRole = "JsonArrayElement"
	RoleTomlItem                 MatchRole = "TomlItem"
	RoleTomlEntry                MatchRole = "TomlEntry"
	RoleTomlArrayElement         MatchRole = "TomlArrayElement"
	RoleYamlStream               MatchRole = "YamlStream"
	RoleYamlDocument             MatchRole = "YamlDocument"
	RoleYamlNode                 MatchRole = "YamlNode"
	RoleYamlMappingEntry         MatchRole = "YamlMappingEntry"
	RoleYamlSequenceElement      MatchRole = "YamlSequenceElement"
	RoleYamlAnchorDefinition     MatchRole = "YamlAnchorDefinition"
	RoleYamlAliasOccurrence      MatchRole = "YamlAliasOccurrence"
	RoleJsonSyntaxPiece          MatchRole = "JsonSyntaxPiece"
	RoleTomlSyntaxPiece          MatchRole = "TomlSyntaxPiece"
	RoleYamlSyntaxPiece          MatchRole = "YamlSyntaxPiece"
	RoleIniDocument              MatchRole = "IniDocument"
	RoleIniSection               MatchRole = "IniSection"
	RoleIniDefaultSection        MatchRole = "IniDefaultSection"
	RoleIniEntry                 MatchRole = "IniEntry"
	RoleIniPhysicalLine          MatchRole = "IniPhysicalLine"
	RoleIniLogicalLine           MatchRole = "IniLogicalLine"
	RoleIniErrorLine             MatchRole = "IniErrorLine"
	RoleIniSyntaxPiece           MatchRole = "IniSyntaxPiece"
	RolePropertiesDocument       MatchRole = "PropertiesDocument"
	RolePropertiesNaturalLine    MatchRole = "PropertiesNaturalLine"
	RolePropertiesLogicalLine    MatchRole = "PropertiesLogicalLine"
	RolePropertiesProperty       MatchRole = "PropertiesProperty"
	RolePropertiesComment        MatchRole = "PropertiesComment"
	RolePropertiesEscape         MatchRole = "PropertiesEscape"
	RolePropertiesErrorLine      MatchRole = "PropertiesErrorLine"
	RolePropertiesSyntaxPiece    MatchRole = "PropertiesSyntaxPiece"
	RoleGraphNode                MatchRole = "GraphNode"
	RoleGraphSequenceElement     MatchRole = "GraphSequenceElement"
	RoleGraphMappingEntry        MatchRole = "GraphMappingEntry"
	RoleXmlDocument              MatchRole = "XmlDocument"
	RoleXmlDeclaration           MatchRole = "XmlDeclaration"
	RoleXmlDoctype               MatchRole = "XmlDoctype"
	RoleXmlPrologItem            MatchRole = "XmlPrologItem"
	RoleXmlElement               MatchRole = "XmlElement"
	RoleXmlContentItem           MatchRole = "XmlContentItem"
	RoleXmlAttribute             MatchRole = "XmlAttribute"
	RoleXmlNamespaceBinding      MatchRole = "XmlNamespaceBinding"
	RoleXmlText                  MatchRole = "XmlText"
	RoleXmlCdata                 MatchRole = "XmlCdata"
	RoleXmlComment               MatchRole = "XmlComment"
	RoleXmlProcessingInstruction MatchRole = "XmlProcessingInstruction"
	RoleXmlReference             MatchRole = "XmlReference"
	RoleXmlErrorRegion           MatchRole = "XmlErrorRegion"
	RoleXmlSyntaxPiece           MatchRole = "XmlSyntaxPiece"
	RolePlistValue               MatchRole = "PlistValue"
	RolePlistDictEntry           MatchRole = "PlistDictEntry"
	RolePlistKey                 MatchRole = "PlistKey"
	RolePlistArrayElement        MatchRole = "PlistArrayElement"
	RolePlistSyntaxPiece         MatchRole = "PlistSyntaxPiece"
	RolePlistBinaryStructure     MatchRole = "PlistBinaryStructure"
	RolePlistBinaryObject        MatchRole = "PlistBinaryObject"
	RolePlistBinaryOffset        MatchRole = "PlistBinaryOffset"
	RolePlistBinaryRef           MatchRole = "PlistBinaryRef"
	RolePlistBinaryTrailer       MatchRole = "PlistBinaryTrailer"
	RoleHclBody                  MatchRole = "HclBody"
	RoleHclAttribute             MatchRole = "HclAttribute"
	RoleHclBlock                 MatchRole = "HclBlock"
	RoleHclBlockLabel            MatchRole = "HclBlockLabel"
	RoleHclExpression            MatchRole = "HclExpression"
	RoleHclTemplatePart          MatchRole = "HclTemplatePart"
	RoleHclErrorRegion           MatchRole = "HclErrorRegion"
	RoleHclSyntaxPiece           MatchRole = "HclSyntaxPiece"
)

// QueryDomain is a versioned query domain (consema-core/src/query.rs).
type QueryDomain struct {
	id      string
	version uint32
}

// NewQueryDomain creates a domain identifier.
func NewQueryDomain(id string, version uint32) *QueryDomain {
	return &QueryDomain{id: id, version: version}
}

// The frozen domain constructors (query.rs).
func DomainPortableValueV1() *QueryDomain { return NewQueryDomain("core.portable-value-query", 1) }
func DomainPortableGraphV1() *QueryDomain { return NewQueryDomain("core.portable-graph-query", 1) }
func DomainJSONNativeV1() *QueryDomain    { return NewQueryDomain("json.native-semantic-query", 1) }
func DomainJSONNativeV2() *QueryDomain    { return NewQueryDomain("json.native-semantic-query", 2) }
func DomainTOMLNativeV1() *QueryDomain    { return NewQueryDomain("toml.native-semantic-query", 1) }
func DomainYAMLNativeV1() *QueryDomain    { return NewQueryDomain("yaml.native-semantic-query", 1) }
func DomainININativeV1() *QueryDomain     { return NewQueryDomain("ini.native-semantic-query", 1) }
func DomainJavaPropertiesNativeV1() *QueryDomain {
	return NewQueryDomain("java-properties.native-semantic-query", 1)
}
func DomainXMLNativeV1() *QueryDomain { return NewQueryDomain("xml.native-semantic-query", 1) }
func DomainJSONLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("json.lossless-syntax-query", 1)
}
func DomainJSONLosslessSyntaxV2() *QueryDomain {
	return NewQueryDomain("json.lossless-syntax-query", 2)
}
func DomainTOMLLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("toml.lossless-syntax-query", 1)
}
func DomainYAMLLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("yaml.lossless-syntax-query", 1)
}
func DomainINILosslessSyntaxV1() *QueryDomain { return NewQueryDomain("ini.lossless-syntax-query", 1) }
func DomainJavaPropertiesLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("java-properties.lossless-syntax-query", 1)
}
func DomainXMLLosslessSyntaxV1() *QueryDomain { return NewQueryDomain("xml.lossless-syntax-query", 1) }
func DomainPlistNativeV1() *QueryDomain       { return NewQueryDomain("plist.native-semantic-query", 1) }
func DomainPlistLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("plist.lossless-syntax-query", 1)
}
func DomainPlistBinaryStructureV1() *QueryDomain {
	return NewQueryDomain("plist.binary-structure-query", 1)
}
func DomainHCLNativeV1() *QueryDomain { return NewQueryDomain("hcl.native-semantic-query", 1) }
func DomainHCLLosslessSyntaxV1() *QueryDomain {
	return NewQueryDomain("hcl.lossless-syntax-query", 1)
}

// ID returns the domain namespace.
func (d *QueryDomain) ID() string { return d.id }

// Version returns the domain version.
func (d *QueryDomain) Version() uint32 { return d.version }

// Equal reports whether two domains are identical.
func (d *QueryDomain) Equal(other *QueryDomain) bool {
	return d.id == other.id && d.version == other.version
}

// OperatorCall is one versioned operator call with deterministic arguments
// (query.rs).
type OperatorCall struct {
	id        string
	version   uint32
	arguments map[string]core.Value
}

// NewOperatorCall creates an operator call without arguments.
func NewOperatorCall(id string, version uint32) *OperatorCall {
	return &OperatorCall{id: id, version: version, arguments: map[string]core.Value{}}
}

// WithArgument adds or replaces a named argument.
func (o *OperatorCall) WithArgument(name string, value core.Value) *OperatorCall {
	o.arguments[name] = value
	return o
}

// ID returns the stable operator identifier.
func (o *OperatorCall) ID() string { return o.id }

// Version returns the operator contract version.
func (o *OperatorCall) Version() uint32 { return o.version }

// Arguments returns the deterministic sorted arguments.
func (o *OperatorCall) Arguments() map[string]core.Value {
	output := make(map[string]core.Value, len(o.arguments))
	for name, value := range o.arguments {
		output[name] = value
	}
	return output
}

// QueryExpression is the declarative operator tree (query.rs).
type QueryExpression struct {
	// Kind is the closed expression kind.
	Kind ExpressionKind
	// Input is the input expression of an Apply.
	Input *QueryExpression
	// Operator is the operator call of an Apply.
	Operator *OperatorCall
	// Branches are the branch expressions of Concat/StructureOrderMerge.
	Branches []*QueryExpression
}

// ExpressionKind is the closed query-expression kind.
type ExpressionKind uint8

const (
	// ExpressionInput is the domain root input.
	ExpressionInput ExpressionKind = iota
	// ExpressionApply applies an operator to an input expression.
	ExpressionApply
	// ExpressionConcat appends complete branch results in branch order.
	ExpressionConcat
	// ExpressionStructureOrderMerge merges branches by structural identity
	// order.
	ExpressionStructureOrderMerge
)

// Then applies one operator to the expression (the Rust `then` builder).
func (e *QueryExpression) Then(operator *OperatorCall) *QueryExpression {
	return &QueryExpression{Kind: ExpressionApply, Input: e, Operator: operator}
}

// QuerySelection is the cardinality selection applied to the complete
// standard result sequence (query.rs).
type QuerySelection string

// The five frozen selections.
const (
	SelectionAll        QuerySelection = "All"
	SelectionFirst      QuerySelection = "First"
	SelectionLast       QuerySelection = "Last"
	SelectionZeroOrOne  QuerySelection = "ZeroOrOne"
	SelectionRequireOne QuerySelection = "RequireOne"
)

// QueryDefinition is a transferable, not-yet-validated query definition
// (query.rs).
type QueryDefinition struct {
	domain     *QueryDomain
	expression *QueryExpression
	selection  QuerySelection
}

// NewQueryDefinition creates a definition rooted at the domain input.
func NewQueryDefinition(domain *QueryDomain) *QueryDefinition {
	return &QueryDefinition{
		domain:     domain,
		expression: &QueryExpression{Kind: ExpressionInput},
		selection:  SelectionAll,
	}
}

// WithExpression replaces the expression.
func (d *QueryDefinition) WithExpression(expression *QueryExpression) *QueryDefinition {
	d.expression = expression
	return d
}

// WithSelection sets the cardinality selection.
func (d *QueryDefinition) WithSelection(selection QuerySelection) *QueryDefinition {
	d.selection = selection
	return d
}

// Domain returns the domain contract.
func (d *QueryDefinition) Domain() *QueryDomain { return d.domain }

// Expression returns the operator expression.
func (d *QueryDefinition) Expression() *QueryExpression { return d.expression }

// Selection returns the cardinality selector.
func (d *QueryDefinition) Selection() QuerySelection { return d.selection }

// Validate validates the domain, argument schemas, composition, and role
// typing (query.rs). The required capability set of a validated
// query is always [core.query.ordered-results@1].
func (d *QueryDefinition) Validate() (*ValidatedQuery, *QueryFailure) {
	inputRole, ok := domainInputRole(d.domain.id, d.domain.version)
	if !ok {
		return nil, QueryFailureDomainMismatch(d.domain)
	}
	outputRole, failure := validateExpression(d.domain, d.expression, inputRole)
	if failure != nil {
		return nil, failure
	}
	orderedResults := NewCapabilityId("core.query.ordered-results", 1)
	return &ValidatedQuery{
		definition:           d,
		outputRole:           outputRole,
		requiredCapabilities: []*CapabilityId{orderedResults},
	}, nil
}

// domainInputRole maps a domain to its root match role (query.rs).
func domainInputRole(id string, version uint32) (MatchRole, bool) {
	switch {
	case id == "core.portable-value-query" && version == 1:
		return RoleValue, true
	case id == "core.portable-graph-query" && version == 1:
		return RoleGraphNode, true
	case id == "json.native-semantic-query" && (version == 1 || version == 2):
		return RoleJsonValue, true
	case id == "toml.native-semantic-query" && version == 1:
		return RoleTomlItem, true
	case id == "yaml.native-semantic-query" && version == 1:
		return RoleYamlStream, true
	case id == "ini.native-semantic-query" && version == 1:
		return RoleIniDocument, true
	case id == "java-properties.native-semantic-query" && version == 1:
		return RolePropertiesDocument, true
	case id == "xml.native-semantic-query" && version == 1:
		return RoleXmlDocument, true
	case id == "json.lossless-syntax-query" && (version == 1 || version == 2):
		return RoleJsonSyntaxPiece, true
	case id == "toml.lossless-syntax-query" && version == 1:
		return RoleTomlSyntaxPiece, true
	case id == "yaml.lossless-syntax-query" && version == 1:
		return RoleYamlSyntaxPiece, true
	case id == "ini.lossless-syntax-query" && version == 1:
		return RoleIniSyntaxPiece, true
	case id == "java-properties.lossless-syntax-query" && version == 1:
		return RolePropertiesSyntaxPiece, true
	case id == "xml.lossless-syntax-query" && version == 1:
		return RoleXmlSyntaxPiece, true
	case id == "plist.native-semantic-query" && version == 1:
		return RolePlistValue, true
	case id == "plist.lossless-syntax-query" && version == 1:
		return RolePlistSyntaxPiece, true
	case id == "plist.binary-structure-query" && version == 1:
		return RolePlistBinaryStructure, true
	case id == "hcl.native-semantic-query" && version == 1:
		return RoleHclBody, true
	case id == "hcl.lossless-syntax-query" && version == 1:
		return RoleHclSyntaxPiece, true
	}
	return "", false
}

// validateExpression checks the whole operator tree and returns its output
// role (query.rs).
func validateExpression(domain *QueryDomain, expression *QueryExpression, inputRole MatchRole) (MatchRole, *QueryFailure) {
	switch expression.Kind {
	case ExpressionInput:
		return inputRole, nil
	case ExpressionApply:
		actualInput, failure := validateExpression(domain, expression.Input, inputRole)
		if failure != nil {
			return "", failure
		}
		return validateOperator(domain, expression.Operator, actualInput)
	case ExpressionConcat, ExpressionStructureOrderMerge:
		var output MatchRole
		for _, branch := range expression.Branches {
			branchOutput, failure := validateExpression(domain, branch, inputRole)
			if failure != nil {
				return "", failure
			}
			if output != "" && output != branchOutput {
				return "", &QueryFailure{
					Kind:         FailureInvalidOperatorComposition,
					Operator:     "composition.concat",
					ExpectedRole: output,
					ActualRole:   branchOutput,
				}
			}
			output = branchOutput
		}
		if output == "" {
			return "", &QueryFailure{
				Kind:     FailureInvalidArgument,
				Operator: "composition.concat",
				Argument: "branches",
			}
		}
		return output, nil
	}
	return "", &QueryFailure{Kind: FailureInvalidArgument, Operator: "expression", Argument: "kind"}
}

// ValidatedQuery is a definition proven structurally valid for its domain
// (query.rs).
type ValidatedQuery struct {
	definition           *QueryDefinition
	outputRole           MatchRole
	requiredCapabilities []*CapabilityId
}

// OutputRole returns the final match role.
func (v *ValidatedQuery) OutputRole() MatchRole { return v.outputRole }

// RequiredCapabilities returns the required capability contracts.
func (v *ValidatedQuery) RequiredCapabilities() []*CapabilityId {
	return append([]*CapabilityId(nil), v.requiredCapabilities...)
}

// Definition returns the validated definition.
func (v *ValidatedQuery) Definition() *QueryDefinition { return v.definition }

// Bind binds the validated definition to implementation capabilities
// (query.rs).
func (v *ValidatedQuery) Bind(capabilities *CapabilitySet) (*ExecutableQuery, *QueryFailure) {
	for _, capability := range v.requiredCapabilities {
		if !capabilities.Contains(capability) {
			return nil, QueryFailureMissingCapability(capability)
		}
	}
	return &ExecutableQuery{validated: v}, nil
}

// ExecutableQuery is a fully validated and capability-bound query
// (query.rs). Execution against PortableValue values is provided by
// the family packages; this milestone pins the definition surface.
type ExecutableQuery struct {
	validated *ValidatedQuery
}

// Definition returns the validated definition.
func (e *ExecutableQuery) Definition() *QueryDefinition { return e.validated.definition }

// OutputRole returns the final match role.
func (e *ExecutableQuery) OutputRole() MatchRole { return e.validated.outputRole }

// ToProtocolValue encodes `core.query-definition@1` through the fixed-field
// PortableValue schema (query.rs).
func (d *QueryDefinition) ToProtocolValue() (core.Value, *QueryFailure) {
	expression, failure := encodeExpression(d.expression, 0)
	if failure != nil {
		return nil, failure
	}
	value, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.query-definition@1")},
		core.Entry{Key: "domain_id", Value: core.String(d.domain.id)},
		core.Entry{Key: "domain_version", Value: integerValue(uint64(d.domain.version))},
		core.Entry{Key: "selection", Value: core.String(string(d.selection))},
		core.Entry{Key: "expression", Value: expression},
	)
	if err != nil {
		return nil, queryProtocolError("schema")
	}
	return value, nil
}

// FromProtocolValue strictly decodes `core.query-definition@1`
// (query.rs). Unknown, reordered, or missing fields are rejected;
// structural/operator validation remains the explicit next lifecycle step.
func (d *QueryDefinition) FromProtocolValue(value core.Value) (*QueryDefinition, *QueryFailure) {
	fields, failure := exactObjectFields(value, []string{"schema", "domain_id",
		"domain_version", "selection", "expression"}, "core.query-definition@1")
	if failure != nil {
		return nil, failure
	}
	if schema, ok := fields[0].(core.String); !ok || string(schema) != "core.query-definition@1" {
		return nil, queryProtocolError("schema")
	}
	domainID, ok := fields[1].(core.String)
	if !ok {
		return nil, queryProtocolError("domain_id")
	}
	domainVersion, failure := queryUnsigned32(fields[2], "domain_version")
	if failure != nil {
		return nil, failure
	}
	selectionText, ok := fields[3].(core.String)
	if !ok {
		return nil, queryProtocolError("selection")
	}
	var selection QuerySelection
	switch string(selectionText) {
	case "All":
		selection = SelectionAll
	case "First":
		selection = SelectionFirst
	case "Last":
		selection = SelectionLast
	case "ZeroOrOne":
		selection = SelectionZeroOrOne
	case "RequireOne":
		selection = SelectionRequireOne
	default:
		return nil, queryProtocolError("selection")
	}
	expression, failure := decodeExpression(fields[4], 0)
	if failure != nil {
		return nil, failure
	}
	return NewQueryDefinition(NewQueryDomain(string(domainID), domainVersion)).
		WithExpression(expression).WithSelection(selection), nil
}

// encodeExpression encodes one expression node (query.rs).
func encodeExpression(expression *QueryExpression, depth int) (core.Value, *QueryFailure) {
	if depth > 256 {
		return nil, &QueryFailure{Kind: FailureResourceLimit}
	}
	switch expression.Kind {
	case ExpressionInput:
		value, err := core.NewObject(core.Entry{Key: "kind", Value: core.String("Input")})
		if err != nil {
			return nil, queryProtocolError("expression.kind")
		}
		return value, nil
	case ExpressionApply:
		input, failure := encodeExpression(expression.Input, depth+1)
		if failure != nil {
			return nil, failure
		}
		operator, failure := encodeOperator(expression.Operator)
		if failure != nil {
			return nil, failure
		}
		value, err := core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Apply")},
			core.Entry{Key: "input", Value: input},
			core.Entry{Key: "operator", Value: operator},
		)
		if err != nil {
			return nil, queryProtocolError("expression.kind")
		}
		return value, nil
	case ExpressionConcat, ExpressionStructureOrderMerge:
		kind := "Concat"
		if expression.Kind == ExpressionStructureOrderMerge {
			kind = "StructureOrderMerge"
		}
		branches := make([]core.Value, 0, len(expression.Branches))
		for _, branch := range expression.Branches {
			value, failure := encodeExpression(branch, depth+1)
			if failure != nil {
				return nil, failure
			}
			branches = append(branches, value)
		}
		value, err := core.NewObject(
			core.Entry{Key: "kind", Value: core.String(kind)},
			core.Entry{Key: "branches", Value: core.NewArray(branches...)},
		)
		if err != nil {
			return nil, queryProtocolError("expression.kind")
		}
		return value, nil
	}
	return nil, queryProtocolError("expression.kind")
}

// encodeOperator encodes one operator call (query.rs).
func encodeOperator(operator *OperatorCall) (core.Value, *QueryFailure) {
	arguments := make([]core.Entry, 0, len(operator.arguments))
	for _, name := range sortedMapKeys(operator.arguments) {
		arguments = append(arguments, core.Entry{Key: name, Value: operator.arguments[name]})
	}
	argumentObject, err := core.NewObject(arguments...)
	if err != nil {
		return nil, queryProtocolError("operator.arguments")
	}
	value, err := core.NewObject(
		core.Entry{Key: "id", Value: core.String(operator.id)},
		core.Entry{Key: "version", Value: integerValue(uint64(operator.version))},
		core.Entry{Key: "arguments", Value: argumentObject},
	)
	if err != nil {
		return nil, queryProtocolError("operator.id")
	}
	return value, nil
}

// sortedMapKeys returns the sorted keys of a value map.
func sortedMapKeys(values map[string]core.Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

func queryProtocolError(field string) *QueryFailure {
	return &QueryFailure{
		Kind:     FailureInvalidArgument,
		Operator: "core.query-definition@1",
		Argument: field,
	}
}

func queryUnsigned32(value core.Value, name string) (uint32, *QueryFailure) {
	integer, ok := value.(core.Integer)
	if !ok {
		return 0, queryProtocolError(name)
	}
	number := integer.Int()
	if !number.IsInt64() || number.Sign() < 0 || number.Int64() > int64(^uint32(0)) {
		return 0, queryProtocolError(name)
	}
	return uint32(number.Int64()), nil
}

// exactObjectFields strictly validates a fixed-field object
// (query.rs).
func exactObjectFields(value core.Value, names []string, context string) ([]core.Value, *QueryFailure) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, queryProtocolError(context)
	}
	entries := object.Entries()
	if len(entries) != len(names) {
		return nil, queryProtocolError(context)
	}
	values := make([]core.Value, len(entries))
	for index, entry := range entries {
		if entry.Key != names[index] {
			return nil, queryProtocolError(context)
		}
		values[index] = entry.Value
	}
	return values, nil
}

// decodeExpression strictly decodes one expression node (query.rs).
func decodeExpression(value core.Value, depth int) (*QueryExpression, *QueryFailure) {
	if depth > 256 {
		return nil, &QueryFailure{Kind: FailureResourceLimit}
	}
	object, ok := value.(*core.Object)
	if !ok {
		return nil, queryProtocolError("expression")
	}
	entries := object.Entries()
	if len(entries) == 0 {
		return nil, queryProtocolError("expression.kind")
	}
	kind, ok := entries[0].Value.(core.String)
	if !ok || entries[0].Key != "kind" {
		return nil, queryProtocolError("expression.kind")
	}
	switch string(kind) {
	case "Input":
		if len(entries) != 1 {
			return nil, queryProtocolError("expression.kind")
		}
		return &QueryExpression{Kind: ExpressionInput}, nil
	case "Apply":
		fields, failure := exactObjectFields(value, []string{"kind", "input", "operator"}, "Apply")
		if failure != nil {
			return nil, failure
		}
		input, failure := decodeExpression(fields[1], depth+1)
		if failure != nil {
			return nil, failure
		}
		operator, failure := decodeOperator(fields[2])
		if failure != nil {
			return nil, failure
		}
		return &QueryExpression{Kind: ExpressionApply, Input: input, Operator: operator}, nil
	case "Concat", "StructureOrderMerge":
		fields, failure := exactObjectFields(value, []string{"kind", "branches"}, string(kind))
		if failure != nil {
			return nil, failure
		}
		branches, ok := fields[1].(*core.Array)
		if !ok {
			return nil, queryProtocolError("expression.branches")
		}
		decoded := make([]*QueryExpression, 0, branches.Len())
		for _, branch := range branches.Items() {
			item, failure := decodeExpression(branch, depth+1)
			if failure != nil {
				return nil, failure
			}
			decoded = append(decoded, item)
		}
		if string(kind) == "Concat" {
			return &QueryExpression{Kind: ExpressionConcat, Branches: decoded}, nil
		}
		return &QueryExpression{Kind: ExpressionStructureOrderMerge, Branches: decoded}, nil
	}
	return nil, queryProtocolError("expression.kind")
}

// decodeOperator strictly decodes one operator call (query.rs).
func decodeOperator(value core.Value) (*OperatorCall, *QueryFailure) {
	fields, failure := exactObjectFields(value, []string{"id", "version", "arguments"}, "operator")
	if failure != nil {
		return nil, failure
	}
	id, ok := fields[0].(core.String)
	if !ok {
		return nil, queryProtocolError("operator.id")
	}
	version, failure := queryUnsigned32(fields[1], "operator.version")
	if failure != nil {
		return nil, failure
	}
	object, ok := fields[2].(*core.Object)
	if !ok {
		return nil, queryProtocolError("operator.arguments")
	}
	operator := NewOperatorCall(string(id), version)
	for _, entry := range object.Entries() {
		operator.WithArgument(entry.Key, entry.Value)
	}
	return operator, nil
}
