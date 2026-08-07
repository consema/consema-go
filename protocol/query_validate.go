package protocol

import (
	"consema.dev/consema/core"
)

// This file transcribes the operator validation table of
// crates/consema-core/src/query.rs:899-1897. Every operator row pins its
// expected input role, its output role, and its argument value kinds; the
// argument-value semantic checks (closed kind-name vocabularies, non-empty
// tags, even byte lengths) follow at the end of validateOperator, in the
// Rust order.

// QueryFailureKind is one failure class of query definition validation and
// binding (consema-core/src/query.rs:3114+; the query_failure_code mapping,
// error_registry.rs:1515-1529).
type QueryFailureKind uint8

const (
	// FailureDomainMismatch: the domain ID or version is unavailable or
	// mismatched.
	FailureDomainMismatch QueryFailureKind = iota
	// FailureUnknownOperator: the operator ID/version is unknown.
	FailureUnknownOperator
	// FailureWrongArgumentType: an argument has the wrong value kind.
	FailureWrongArgumentType
	// FailureInvalidArgument: an argument is malformed or missing.
	FailureInvalidArgument
	// FailureInvalidOperatorComposition: the operator role composition is
	// invalid.
	FailureInvalidOperatorComposition
	// FailureMissingCapability: the capability binding failed.
	FailureMissingCapability
	// FailureRequiredTypeMismatch: a required value type did not match.
	FailureRequiredTypeMismatch
	// FailureCardinalityViolation: the query selection cardinality was
	// violated.
	FailureCardinalityViolation
	// FailureResourceLimit: a query resource limit was reached.
	FailureResourceLimit
	// FailureCancelled: query execution was cancelled.
	FailureCancelled
	// FailureTargetUnavailable: the target native semantics are unavailable.
	FailureTargetUnavailable
)

// QueryFailure is the typed query failure. It implements error and the
// RFC 0016 §6 Code() contract with the frozen registered codes.
type QueryFailure struct {
	// Kind identifies the failure.
	Kind QueryFailureKind
	// Domain is the offending domain (DomainMismatch).
	Domain *QueryDomain
	// Operator is the offending operator ID.
	Operator string
	// Version is the offending operator version (UnknownOperator).
	Version uint32
	// Argument is the offending argument name (InvalidArgument,
	// WrongArgumentType).
	Argument string
	// ExpectedKind is the required argument value kind name.
	ExpectedKind string
	// ExpectedRole is the required input match role (composition).
	ExpectedRole MatchRole
	// ActualRole is the actual input match role (composition).
	ActualRole MatchRole
	// Capability is the missing capability (binding).
	Capability *CapabilityId
}

// Error implements error. The text is human presentation only (RFC 0016 §6).
func (f *QueryFailure) Error() string {
	code := f.Code()
	switch f.Kind {
	case FailureDomainMismatch:
		return "protocol: " + code + ": domain mismatch for " + f.Domain.id + "@" + uint32String(f.Domain.version)
	case FailureUnknownOperator:
		return "protocol: " + code + ": unknown operator " + f.Operator + "@" + uint32String(f.Version)
	case FailureWrongArgumentType:
		return "protocol: " + code + ": operator " + f.Operator + " argument " + f.Argument + " wants " + f.ExpectedKind
	case FailureInvalidArgument:
		return "protocol: " + code + ": operator " + f.Operator + " argument " + f.Argument + " is invalid"
	case FailureInvalidOperatorComposition:
		return "protocol: " + code + ": operator " + f.Operator + " wants " + string(f.ExpectedRole) + " but input is " + string(f.ActualRole)
	case FailureMissingCapability:
		return "protocol: " + code + ": missing capability " + f.Capability.Namespace() + "@" + uint32String(f.Capability.Version())
	case FailureRequiredTypeMismatch:
		return "protocol: " + code + ": required value type did not match"
	case FailureCardinalityViolation:
		return "protocol: " + code + ": query selection cardinality was violated"
	case FailureResourceLimit:
		return "protocol: " + code + ": query resource limit was reached"
	case FailureCancelled:
		return "protocol: " + code + ": query execution was cancelled"
	case FailureTargetUnavailable:
		return "protocol: " + code + ": target native semantics are unavailable"
	}
	return "protocol: " + code
}

// Code returns the frozen registered code for the failure
// (error_registry.rs:1515-1529).
func (f *QueryFailure) Code() string {
	switch f.Kind {
	case FailureDomainMismatch:
		return "core.query.domain-mismatch@1"
	case FailureUnknownOperator:
		return "core.query.unknown-operator@1"
	case FailureWrongArgumentType:
		return "core.query.wrong-argument-type@1"
	case FailureInvalidArgument:
		return "core.query.invalid-argument@1"
	case FailureInvalidOperatorComposition:
		return "core.query.invalid-composition@1"
	case FailureMissingCapability:
		return "core.query.missing-capability@1"
	case FailureRequiredTypeMismatch:
		return "core.query.required-type-mismatch@1"
	case FailureCardinalityViolation:
		return "core.query.cardinality-violation@1"
	case FailureResourceLimit:
		return "core.query.resource-limit@1"
	case FailureCancelled:
		return "core.query.cancelled@1"
	case FailureTargetUnavailable:
		return "core.query.target-unavailable@1"
	}
	return "core.query.invalid-argument@1"
}

// QueryFailureDomainMismatch builds the domain-mismatch failure.
func QueryFailureDomainMismatch(domain *QueryDomain) *QueryFailure {
	return &QueryFailure{Kind: FailureDomainMismatch, Domain: domain}
}

// QueryFailureMissingCapability builds the missing-capability failure.
func QueryFailureMissingCapability(capability *CapabilityId) *QueryFailure {
	return &QueryFailure{Kind: FailureMissingCapability, Capability: capability}
}

// argSpec is one operator argument's required value kind name.
type argSpec struct {
	name string
	kind string
}

// operatorSpec is one operator row: expected input role, output role, and
// argument kinds.
type operatorSpec struct {
	expected  MatchRole
	output    MatchRole
	arguments []argSpec
}

// The argument value-kind spellings used by the table (the
// PortableValueKind names of consema-core, including the kinds outside the
// Go value model; definitions carrying them cannot be constructed in Go,
// doc.go).
const (
	kindNameString  = "String"
	kindNameBoolean = "Boolean"
	kindNameInteger = "Integer"
	kindNameBytes   = "Bytes"
)

// operatorTable maps "(domain, operator)" to its validation row. The
// generic rows (core.take, core.distinct-by-identity) are domain-agnostic
// and resolved in validateOperator.
var operatorTable = map[string]operatorSpec{
	// core.portable-value-query@1
	"core.portable-value-query/core.try-object-entries":        {RoleValue, RoleObjectEntry, nil},
	"core.portable-value-query/core.object-entry-value":        {RoleObjectEntry, RoleValue, nil},
	"core.portable-value-query/core.object-entry-name-equals":  {RoleObjectEntry, RoleObjectEntry, []argSpec{{"name", kindNameString}}},
	"core.portable-value-query/core.try-entry-mapping-entries": {RoleValue, RoleEntryMappingEntry, nil},
	"core.portable-value-query/core.entry-key":                 {RoleEntryMappingEntry, RoleValue, nil},
	"core.portable-value-query/core.entry-value":               {RoleEntryMappingEntry, RoleValue, nil},
	"core.portable-value-query/core.try-sequence-elements":     {RoleValue, RoleValue, nil},
	"core.portable-value-query/core.where-type":                {RoleValue, RoleValue, []argSpec{{"kind", kindNameString}}},
	"core.portable-value-query/core.require-type":              {RoleValue, RoleValue, []argSpec{{"kind", kindNameString}}},

	// json.native-semantic-query@1|2
	"json.native-semantic-query/json.try-object-members":  {RoleJsonValue, RoleJsonObjectMember, nil},
	"json.native-semantic-query/json.member-name-equals":  {RoleJsonObjectMember, RoleJsonObjectMember, []argSpec{{"name", kindNameString}}},
	"json.native-semantic-query/json.member-value":        {RoleJsonObjectMember, RoleJsonValue, nil},
	"json.native-semantic-query/json.try-array-elements":  {RoleJsonValue, RoleJsonArrayElement, nil},
	"json.native-semantic-query/json.array-element-value": {RoleJsonArrayElement, RoleJsonValue, nil},

	// toml.native-semantic-query@1
	"toml.native-semantic-query/toml.try-table-entries":  {RoleTomlItem, RoleTomlEntry, nil},
	"toml.native-semantic-query/toml.entry-name-equals":  {RoleTomlEntry, RoleTomlEntry, []argSpec{{"name", kindNameString}}},
	"toml.native-semantic-query/toml.entry-item":         {RoleTomlEntry, RoleTomlItem, nil},
	"toml.native-semantic-query/toml.try-array-elements": {RoleTomlItem, RoleTomlArrayElement, nil},
	"toml.native-semantic-query/toml.array-element-item": {RoleTomlArrayElement, RoleTomlItem, nil},

	// yaml.native-semantic-query@1
	"yaml.native-semantic-query/yaml.documents":               {RoleYamlStream, RoleYamlDocument, nil},
	"yaml.native-semantic-query/yaml.document-root":           {RoleYamlDocument, RoleYamlNode, nil},
	"yaml.native-semantic-query/yaml.where-node-kind":         {RoleYamlNode, RoleYamlNode, []argSpec{{"kind", kindNameString}}},
	"yaml.native-semantic-query/yaml.where-tag":               {RoleYamlNode, RoleYamlNode, []argSpec{{"tag", kindNameString}}},
	"yaml.native-semantic-query/yaml.scalar-canonical-equals": {RoleYamlNode, RoleYamlNode, []argSpec{{"canonical", kindNameString}}},
	"yaml.native-semantic-query/yaml.try-sequence-elements":   {RoleYamlNode, RoleYamlSequenceElement, nil},
	"yaml.native-semantic-query/yaml.sequence-element-node":   {RoleYamlSequenceElement, RoleYamlNode, nil},
	"yaml.native-semantic-query/yaml.try-mapping-entries":     {RoleYamlNode, RoleYamlMappingEntry, nil},
	"yaml.native-semantic-query/yaml.mapping-entry-key":       {RoleYamlMappingEntry, RoleYamlNode, nil},
	"yaml.native-semantic-query/yaml.mapping-entry-value":     {RoleYamlMappingEntry, RoleYamlNode, nil},
	"yaml.native-semantic-query/yaml.anchor-definition":       {RoleYamlNode, RoleYamlAnchorDefinition, nil},
	"yaml.native-semantic-query/yaml.anchor-node":             {RoleYamlAnchorDefinition, RoleYamlNode, nil},
	"yaml.native-semantic-query/yaml.alias-occurrences":       {RoleYamlStream, RoleYamlAliasOccurrence, nil},
	"yaml.native-semantic-query/yaml.alias-target":            {RoleYamlAliasOccurrence, RoleYamlNode, nil},

	// ini.native-semantic-query@1
	"ini.native-semantic-query/ini.document-sections": {RoleIniDocument, RoleIniSection, nil},
	"ini.native-semantic-query/ini.section-entries":   {RoleIniSection, RoleIniEntry, nil},
	"ini.native-semantic-query/ini.all-entries":       {RoleIniDocument, RoleIniEntry, nil},
	"ini.native-semantic-query/ini.entry-section":     {RoleIniEntry, RoleIniSection, nil},
	"ini.native-semantic-query/ini.section-name-equals": {RoleIniSection, RoleIniSection, []argSpec{
		{"name", kindNameString}, {"comparison", kindNameString},
	}},
	"ini.native-semantic-query/ini.entry-key-equals": {RoleIniEntry, RoleIniEntry, []argSpec{
		{"key", kindNameString}, {"comparison", kindNameString},
	}},
	"ini.native-semantic-query/ini.entry-value-state-is": {RoleIniEntry, RoleIniEntry, []argSpec{{"state", kindNameString}}},
	"ini.native-semantic-query/ini.physical-lines":       {RoleIniDocument, RoleIniPhysicalLine, nil},
	"ini.native-semantic-query/ini.logical-lines":        {RoleIniDocument, RoleIniLogicalLine, nil},

	// java-properties.native-semantic-query@1
	"java-properties.native-semantic-query/properties.document-properties":        {RolePropertiesDocument, RolePropertiesProperty, nil},
	"java-properties.native-semantic-query/properties.natural-lines":              {RolePropertiesDocument, RolePropertiesNaturalLine, nil},
	"java-properties.native-semantic-query/properties.logical-lines":              {RolePropertiesDocument, RolePropertiesLogicalLine, nil},
	"java-properties.native-semantic-query/properties.logical-line-natural-lines": {RolePropertiesLogicalLine, RolePropertiesNaturalLine, nil},
	"java-properties.native-semantic-query/properties.property-key-equals":        {RolePropertiesProperty, RolePropertiesProperty, []argSpec{{"key", kindNameBytes}}},
	"java-properties.native-semantic-query/properties.property-value-state-is":    {RolePropertiesProperty, RolePropertiesProperty, []argSpec{{"state", kindNameString}}},
	"java-properties.native-semantic-query/properties.property-escapes":           {RolePropertiesProperty, RolePropertiesEscape, nil},
	"java-properties.native-semantic-query/properties.duplicate-group":            {RolePropertiesProperty, RolePropertiesProperty, nil},

	// json.lossless-syntax-query@1|2
	"json.lossless-syntax-query/json.syntax-kind-is":     {RoleJsonSyntaxPiece, RoleJsonSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"json.lossless-syntax-query/json.syntax-text-equals": {RoleJsonSyntaxPiece, RoleJsonSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// toml.lossless-syntax-query@1
	"toml.lossless-syntax-query/toml.syntax-kind-is":     {RoleTomlSyntaxPiece, RoleTomlSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"toml.lossless-syntax-query/toml.syntax-text-equals": {RoleTomlSyntaxPiece, RoleTomlSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// yaml.lossless-syntax-query@1
	"yaml.lossless-syntax-query/yaml.syntax-kind-is":     {RoleYamlSyntaxPiece, RoleYamlSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"yaml.lossless-syntax-query/yaml.syntax-text-equals": {RoleYamlSyntaxPiece, RoleYamlSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// ini.lossless-syntax-query@1
	"ini.lossless-syntax-query/ini.syntax-kind-is":     {RoleIniSyntaxPiece, RoleIniSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"ini.lossless-syntax-query/ini.syntax-text-equals": {RoleIniSyntaxPiece, RoleIniSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// java-properties.lossless-syntax-query@1
	"java-properties.lossless-syntax-query/properties.syntax-kind-is":          {RolePropertiesSyntaxPiece, RolePropertiesSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"java-properties.lossless-syntax-query/properties.syntax-text-equals":      {RolePropertiesSyntaxPiece, RolePropertiesSyntaxPiece, []argSpec{{"text", kindNameString}}},
	"java-properties.lossless-syntax-query/properties.syntax-raw-bytes-equals": {RolePropertiesSyntaxPiece, RolePropertiesSyntaxPiece, []argSpec{{"bytes", kindNameBytes}}},
	"java-properties.lossless-syntax-query/properties.syntax-utf16be-equals":   {RolePropertiesSyntaxPiece, RolePropertiesSyntaxPiece, []argSpec{{"code_units", kindNameBytes}}},

	// core.portable-graph-query@1
	"core.portable-graph-query/graph.reachable-nodes":       {RoleGraphNode, RoleGraphNode, nil},
	"core.portable-graph-query/graph.where-kind":            {RoleGraphNode, RoleGraphNode, []argSpec{{"kind", kindNameString}}},
	"core.portable-graph-query/graph.where-tag":             {RoleGraphNode, RoleGraphNode, []argSpec{{"tag", kindNameString}}},
	"core.portable-graph-query/graph.try-sequence-elements": {RoleGraphNode, RoleGraphSequenceElement, nil},
	"core.portable-graph-query/graph.sequence-element-node": {RoleGraphSequenceElement, RoleGraphNode, nil},
	"core.portable-graph-query/graph.try-mapping-entries":   {RoleGraphNode, RoleGraphMappingEntry, nil},
	"core.portable-graph-query/graph.mapping-entry-key":     {RoleGraphMappingEntry, RoleGraphNode, nil},
	"core.portable-graph-query/graph.mapping-entry-value":   {RoleGraphMappingEntry, RoleGraphNode, nil},

	// xml.native-semantic-query@1
	"xml.native-semantic-query/xml.document-root":               {RoleXmlDocument, RoleXmlElement, nil},
	"xml.native-semantic-query/xml.document-declaration":        {RoleXmlDocument, RoleXmlDeclaration, nil},
	"xml.native-semantic-query/xml.document-doctype":            {RoleXmlDocument, RoleXmlDoctype, nil},
	"xml.native-semantic-query/xml.document-prolog":             {RoleXmlDocument, RoleXmlPrologItem, nil},
	"xml.native-semantic-query/xml.document-epilog":             {RoleXmlDocument, RoleXmlPrologItem, nil},
	"xml.native-semantic-query/xml.element-children":            {RoleXmlElement, RoleXmlContentItem, nil},
	"xml.native-semantic-query/xml.element-child-elements":      {RoleXmlElement, RoleXmlElement, nil},
	"xml.native-semantic-query/xml.element-descendants":         {RoleXmlElement, RoleXmlElement, nil},
	"xml.native-semantic-query/xml.element-child-text":          {RoleXmlElement, RoleXmlText, nil},
	"xml.native-semantic-query/xml.element-child-cdata":         {RoleXmlElement, RoleXmlCdata, nil},
	"xml.native-semantic-query/xml.element-child-comments":      {RoleXmlElement, RoleXmlComment, nil},
	"xml.native-semantic-query/xml.element-child-pi":            {RoleXmlElement, RoleXmlProcessingInstruction, nil},
	"xml.native-semantic-query/xml.element-attributes":          {RoleXmlElement, RoleXmlAttribute, nil},
	"xml.native-semantic-query/xml.element-namespace-bindings":  {RoleXmlElement, RoleXmlNamespaceBinding, nil},
	"xml.native-semantic-query/xml.element-in-scope-namespaces": {RoleXmlElement, RoleXmlNamespaceBinding, nil},
	"xml.native-semantic-query/xml.text-references":             {RoleXmlText, RoleXmlReference, nil},
	"xml.native-semantic-query/xml.content-parent":              {RoleAny, RoleAny, nil},
	"xml.native-semantic-query/xml.attribute-element":           {RoleAny, RoleAny, nil},
	"xml.native-semantic-query/xml.reference-text":              {RoleAny, RoleAny, nil},
	"xml.native-semantic-query/xml.name-equals": {RoleAny, RoleAny, []argSpec{
		{"prefix", kindNameString}, {"local", kindNameString},
		{"namespace", kindNameString}, {"comparison", kindNameString},
	}},
	"xml.native-semantic-query/xml.attribute-value-equals": {RoleXmlAttribute, RoleXmlAttribute, []argSpec{{"value", kindNameString}}},
	"xml.native-semantic-query/xml.pi-target-equals":       {RoleXmlProcessingInstruction, RoleXmlProcessingInstruction, []argSpec{{"target", kindNameString}}},
	"xml.native-semantic-query/xml.reference-kind-is":      {RoleXmlReference, RoleXmlReference, []argSpec{{"kind", kindNameString}}},
	"xml.native-semantic-query/xml.reference-name-equals":  {RoleXmlReference, RoleXmlReference, []argSpec{{"name", kindNameString}}},
	"xml.native-semantic-query/xml.node-kind-is":           {RoleAny, RoleAny, []argSpec{{"kind", kindNameString}}},

	// xml.lossless-syntax-query@1
	"xml.lossless-syntax-query/xml.syntax-kind-is":     {RoleXmlSyntaxPiece, RoleXmlSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"xml.lossless-syntax-query/xml.syntax-text-equals": {RoleXmlSyntaxPiece, RoleXmlSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// plist.native-semantic-query@1
	"plist.native-semantic-query/plist.document-root":       {RolePlistValue, RolePlistValue, nil},
	"plist.native-semantic-query/plist.dict-entries":        {RolePlistValue, RolePlistDictEntry, nil},
	"plist.native-semantic-query/plist.dict-entry-key":      {RolePlistDictEntry, RolePlistKey, nil},
	"plist.native-semantic-query/plist.dict-entry-value":    {RolePlistDictEntry, RolePlistValue, nil},
	"plist.native-semantic-query/plist.dict-key-equals":     {RolePlistDictEntry, RolePlistDictEntry, []argSpec{{"key", kindNameString}}},
	"plist.native-semantic-query/plist.duplicate-key-group": {RolePlistDictEntry, RolePlistDictEntry, nil},
	"plist.native-semantic-query/plist.array-elements":      {RolePlistValue, RolePlistArrayElement, nil},
	"plist.native-semantic-query/plist.value-type-is":       {RoleAny, RoleAny, []argSpec{{"kind", kindNameString}}},
	"plist.native-semantic-query/plist.value-as-integer":    {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-real":       {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-string":     {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-data":       {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-date":       {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-uid":        {RoleAny, RoleAny, nil},
	"plist.native-semantic-query/plist.value-as-boolean-is": {RoleAny, RoleAny, []argSpec{{"value", kindNameBoolean}}},

	// plist.lossless-syntax-query@1
	"plist.lossless-syntax-query/plist.syntax-kind-is":     {RolePlistSyntaxPiece, RolePlistSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"plist.lossless-syntax-query/plist.syntax-text-equals": {RolePlistSyntaxPiece, RolePlistSyntaxPiece, []argSpec{{"text", kindNameString}}},

	// plist.binary-structure-query@1
	"plist.binary-structure-query/plist.object-table":  {RoleAny, RolePlistBinaryObject, nil},
	"plist.binary-structure-query/plist.object-offset": {RoleAny, RolePlistBinaryOffset, nil},
	"plist.binary-structure-query/plist.object-refs":   {RoleAny, RolePlistBinaryRef, nil},
	"plist.binary-structure-query/plist.offset-table":  {RoleAny, RolePlistBinaryOffset, nil},
	"plist.binary-structure-query/plist.trailer-facts": {RoleAny, RolePlistBinaryTrailer, nil},
	"plist.binary-structure-query/plist.top-object":    {RoleAny, RolePlistBinaryObject, nil},

	// hcl.native-semantic-query@1
	"hcl.native-semantic-query/hcl.document-body":           {RoleHclBody, RoleHclBody, nil},
	"hcl.native-semantic-query/hcl.body-items":              {RoleHclBody, RoleHclAttribute, nil},
	"hcl.native-semantic-query/hcl.body-attributes":         {RoleHclBody, RoleHclAttribute, nil},
	"hcl.native-semantic-query/hcl.body-blocks":             {RoleHclBody, RoleHclBlock, nil},
	"hcl.native-semantic-query/hcl.body-block-type-equals":  {RoleHclBody, RoleHclBlock, []argSpec{{"type", kindNameString}}},
	"hcl.native-semantic-query/hcl.attribute-name":          {RoleAny, RoleAny, nil},
	"hcl.native-semantic-query/hcl.attribute-name-equals":   {RoleAny, RoleAny, []argSpec{{"name", kindNameString}}},
	"hcl.native-semantic-query/hcl.attribute-expression":    {RoleAny, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.attribute-literal-value": {RoleAny, RoleAny, []argSpec{{"accessor", kindNameString}}},
	"hcl.native-semantic-query/hcl.block-type":              {RoleAny, RoleAny, nil},
	"hcl.native-semantic-query/hcl.block-type-equals":       {RoleAny, RoleAny, []argSpec{{"type", kindNameString}}},
	"hcl.native-semantic-query/hcl.block-labels":            {RoleAny, RoleHclBlockLabel, nil},
	"hcl.native-semantic-query/hcl.block-nested-body":       {RoleAny, RoleHclBody, nil},
	"hcl.native-semantic-query/hcl.block-label-equals":      {RoleHclBlockLabel, RoleHclBlockLabel, []argSpec{{"label", kindNameString}}},
	"hcl.native-semantic-query/hcl.expression-kind-is":      {RoleHclExpression, RoleHclExpression, []argSpec{{"kind", kindNameString}}},
	"hcl.native-semantic-query/hcl.expression-is-literal":   {RoleHclExpression, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.expression-text":         {RoleHclExpression, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.expression-children":     {RoleHclExpression, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.template-parts":          {RoleHclExpression, RoleHclTemplatePart, nil},
	"hcl.native-semantic-query/hcl.tuple-elements":          {RoleHclExpression, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.object-entries":          {RoleHclExpression, RoleHclExpression, nil},
	"hcl.native-semantic-query/hcl.error-regions":           {RoleAny, RoleHclErrorRegion, nil},

	// hcl.lossless-syntax-query@1
	"hcl.lossless-syntax-query/hcl.syntax-kind-is":     {RoleHclSyntaxPiece, RoleHclSyntaxPiece, []argSpec{{"kind", kindNameString}}},
	"hcl.lossless-syntax-query/hcl.syntax-text-equals": {RoleHclSyntaxPiece, RoleHclSyntaxPiece, []argSpec{{"text", kindNameString}}},
}

// RoleAny is the table placeholder for input-dependent operator rows; the
// per-operator role checks below replace it.
const RoleAny MatchRole = ""

// validateOperator validates one operator call against its domain and input
// role (query.rs:899-1897). The semantic argument checks (kind-name
// vocabularies, non-empty tags, state sets) mirror the Rust checks in order.
func validateOperator(domain *QueryDomain, operator *OperatorCall, input MatchRole) (MatchRole, *QueryFailure) {
	if operator.version != 1 {
		return "", &QueryFailure{Kind: FailureUnknownOperator, Operator: operator.id, Version: operator.version}
	}
	key := domain.id + "/" + operator.id
	spec, ok := operatorTable[key]
	if !ok {
		// The domain-agnostic generic rows.
		switch operator.id {
		case "core.take":
			spec = operatorSpec{input, input, []argSpec{{"count", kindNameInteger}}}
		case "core.distinct-by-identity":
			spec = operatorSpec{input, input, nil}
		default:
			return "", &QueryFailure{Kind: FailureUnknownOperator, Operator: operator.id, Version: operator.version}
		}
	}
	if spec.expected != RoleAny && input != spec.expected {
		return "", &QueryFailure{
			Kind: FailureInvalidOperatorComposition, Operator: operator.id,
			ExpectedRole: spec.expected, ActualRole: input,
		}
	}
	// Input-dependent role rows (they also fix the output role).
	if err := checkInputDependentRoles(domain.id, operator.id, input, &spec); err != nil {
		return "", err
	}
	if len(operator.arguments) != len(spec.arguments) {
		return "", &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "argument-set"}
	}
	for _, argument := range spec.arguments {
		value, exists := operator.arguments[argument.name]
		if !exists || value.Kind().String() != argument.kind {
			return "", &QueryFailure{
				Kind: FailureWrongArgumentType, Operator: operator.id,
				Argument: argument.name, ExpectedKind: argument.kind,
			}
		}
	}
	// Semantic argument-value checks (query.rs:1634-1897).
	if failure := checkOperatorArguments(domain, operator); failure != nil {
		return "", failure
	}
	return spec.output, nil
}

// checkInputDependentRoles applies the role-union rows that accept several
// input roles (ini.duplicate-group, the XML parent/kind unions, the plist
// value-operator union, the binary-structure union, and the HCL
// attribute/block union). Each handled row also fixes the output role,
// mirroring the Rust rows.
func checkInputDependentRoles(domainID, operatorID string, input MatchRole, spec *operatorSpec) *QueryFailure {
	switch {
	case domainID == "ini.native-semantic-query" && operatorID == "ini.duplicate-group":
		if input != RoleIniSection && input != RoleIniEntry {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleIniSection, ActualRole: input,
			}
		}
		spec.output = input
	case domainID == "xml.native-semantic-query" &&
		(operatorID == "xml.content-parent" || operatorID == "xml.attribute-element" || operatorID == "xml.reference-text"):
		if !xmlContentInputRoles(input) {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleXmlContentItem, ActualRole: input,
			}
		}
		spec.output = RoleXmlElement
	case domainID == "xml.native-semantic-query" && operatorID == "xml.name-equals":
		// The name-equals row types by its input role (query.rs:1265-1274).
		spec.output = input
	case domainID == "xml.native-semantic-query" && operatorID == "xml.node-kind-is":
		if !xmlNodeKindRoles(input) {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleXmlDocument, ActualRole: input,
			}
		}
		spec.output = input
	case domainID == "plist.native-semantic-query" &&
		(operatorID == "plist.value-type-is" || operatorID == "plist.value-as-integer" ||
			operatorID == "plist.value-as-real" || operatorID == "plist.value-as-string" ||
			operatorID == "plist.value-as-data" || operatorID == "plist.value-as-date" ||
			operatorID == "plist.value-as-uid" || operatorID == "plist.value-as-boolean-is"):
		if input != RolePlistValue && input != RolePlistArrayElement {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RolePlistValue, ActualRole: input,
			}
		}
		spec.output = input
	case domainID == "plist.binary-structure-query":
		// The structure facts are document-level; every operator accepts any
		// binary-structure match as input so that chains of structure
		// operators validate (query.rs:1406-1442). The table row already
		// pins the operator's output role.
		if !plistBinaryInputRoles(input) {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RolePlistBinaryStructure, ActualRole: input,
			}
		}
	case domainID == "hcl.native-semantic-query" &&
		(operatorID == "hcl.attribute-name" || operatorID == "hcl.attribute-name-equals" ||
			operatorID == "hcl.block-type" || operatorID == "hcl.block-type-equals"):
		// The attribute/block union accepts chains from hcl.body-items@1
		// (query.rs:1467-1524); each operator acts on its own matches only.
		if input != RoleHclAttribute && input != RoleHclBlock {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleHclAttribute, ActualRole: input,
			}
		}
		spec.output = input
	case domainID == "hcl.native-semantic-query" && operatorID == "hcl.attribute-literal-value":
		// The typed literal accessor family accepts the expression directly
		// or the owning attribute (query.rs:1495-1506).
		if input != RoleHclExpression && input != RoleHclAttribute {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleHclExpression, ActualRole: input,
			}
		}
		spec.output = input
	case domainID == "hcl.native-semantic-query" &&
		(operatorID == "hcl.attribute-expression" || operatorID == "hcl.block-labels" ||
			operatorID == "hcl.block-nested-body"):
		if input != RoleHclAttribute && input != RoleHclBlock {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleHclAttribute, ActualRole: input,
			}
		}
	case domainID == "hcl.native-semantic-query" && operatorID == "hcl.error-regions":
		if !hclErrorRegionInputRoles(input) {
			return &QueryFailure{
				Kind: FailureInvalidOperatorComposition, Operator: operatorID,
				ExpectedRole: RoleHclBody, ActualRole: input,
			}
		}
		spec.output = RoleHclErrorRegion
	}
	return nil
}

func xmlContentInputRoles(input MatchRole) bool {
	switch input {
	case RoleXmlContentItem, RoleXmlAttribute, RoleXmlNamespaceBinding, RoleXmlReference,
		RoleXmlElement, RoleXmlText, RoleXmlCdata, RoleXmlComment, RoleXmlProcessingInstruction:
		return true
	}
	return false
}

func xmlNodeKindRoles(input MatchRole) bool {
	switch input {
	case RoleXmlDocument, RoleXmlDeclaration, RoleXmlDoctype, RoleXmlPrologItem,
		RoleXmlElement, RoleXmlContentItem, RoleXmlAttribute, RoleXmlNamespaceBinding,
		RoleXmlText, RoleXmlCdata, RoleXmlComment, RoleXmlProcessingInstruction,
		RoleXmlReference, RoleXmlErrorRegion:
		return true
	}
	return false
}

func plistBinaryInputRoles(input MatchRole) bool {
	switch input {
	case RolePlistBinaryStructure, RolePlistBinaryObject, RolePlistBinaryOffset,
		RolePlistBinaryRef, RolePlistBinaryTrailer:
		return true
	}
	return false
}

func hclErrorRegionInputRoles(input MatchRole) bool {
	switch input {
	case RoleHclBody, RoleHclAttribute, RoleHclBlock, RoleHclBlockLabel,
		RoleHclExpression, RoleHclTemplatePart, RoleHclErrorRegion:
		return true
	}
	return false
}

// checkOperatorArguments applies the semantic argument-value checks of the
// Rust validator (query.rs:1634-1897), in order.
func checkOperatorArguments(domain *QueryDomain, operator *OperatorCall) *QueryFailure {
	stringArg := func(name string) (string, bool) {
		value, exists := operator.arguments[name]
		if !exists {
			return "", false
		}
		text, ok := value.(core.String)
		return string(text), ok
	}
	switch {
	case operator.id == "core.take":
		// The argument-set check guarantees the Integer kind.
		number := operator.arguments["count"].(core.Integer).Int()
		if number.Sign() < 0 || !number.IsUint64() {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "count"}
		}
	case operator.id == "core.where-type" || operator.id == "core.require-type":
		kind, _ := stringArg("kind")
		if !isValueKindName(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: "value-kind", Argument: kind}
		}
	case operator.id == "json.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isJSONSyntaxKind(domain.version, kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "toml.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isTOMLSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "yaml.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isYAMLSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "ini.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isINISyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "properties.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isPropertiesSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "xml.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isXMLSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "plist.value-type-is":
		kind, _ := stringArg("kind")
		if !isPlistValueKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "plist.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isPlistSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "hcl.expression-kind-is":
		kind, _ := stringArg("kind")
		if !isHCLExpressionKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "hcl.syntax-kind-is":
		kind, _ := stringArg("kind")
		if !isHCLSyntaxKind(kind) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "hcl.attribute-literal-value":
		accessor, _ := stringArg("accessor")
		if !isHCLLiteralAccessor(accessor) {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "accessor"}
		}
	case operator.id == "properties.property-key-equals" || operator.id == "properties.syntax-utf16be-equals":
		// The Bytes-typed arguments are validated against the language-
		// neutral argument-kind vocabulary; the even-length check is
		// transcribed verbatim for parity (the Rust `UTF16BE/1` argument
		// must carry a whole number of code units).
		name := "key"
		if operator.id == "properties.syntax-utf16be-equals" {
			name = "code_units"
		}
		value, exists := operator.arguments[name]
		if !exists {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: name}
		}
		bytes, ok := value.(core.Bytes)
		if !ok || len(bytes)%2 != 0 {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: name}
		}
	case operator.id == "properties.property-value-state-is":
		state, _ := stringArg("state")
		if state != "ImplicitEmpty" && state != "ExplicitEmpty" && state != "Present" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "state"}
		}
	case operator.id == "ini.section-name-equals" || operator.id == "ini.entry-key-equals":
		comparison, _ := stringArg("comparison")
		if comparison != "OriginalExact" && comparison != "ProfileEquivalent" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "comparison"}
		}
	case operator.id == "ini.entry-value-state-is":
		state, _ := stringArg("state")
		if state != "Missing" && state != "Empty" && state != "Present" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "state"}
		}
	case operator.id == "yaml.where-node-kind":
		kind, _ := stringArg("kind")
		if kind != "Scalar" && kind != "Sequence" && kind != "Mapping" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "yaml.where-tag":
		tag, _ := stringArg("tag")
		if tag == "" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "tag"}
		}
	case operator.id == "graph.where-kind":
		kind, _ := stringArg("kind")
		if kind != "Scalar" && kind != "Sequence" && kind != "Mapping" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "kind"}
		}
	case operator.id == "graph.where-tag":
		tag, _ := stringArg("tag")
		if tag == "" {
			return &QueryFailure{Kind: FailureInvalidArgument, Operator: operator.id, Argument: "tag"}
		}
	}
	return nil
}

// isValueKindName accepts the frozen fifteen-kind vocabulary of the
// value-kind arguments (query.rs:2187-2209), matching the closed core model
// (doc.go).
func isValueKindName(kind string) bool {
	switch kind {
	case "Null", "Boolean", "Integer", "Decimal", "BinaryFloat32", "BinaryFloat64",
		"String", "Bytes", "Date", "Time", "LocalDateTime", "OffsetDateTime",
		"Sequence", "Object", "EntryMapping":
		return true
	}
	return false
}

// The frozen syntax-kind and value-kind vocabularies
// (query.rs:1900-2185). Spellings are language-neutral and byte-exact.
func isJSONSyntaxKind(domainVersion uint32, kind string) bool {
	switch kind {
	case "Bom", "Whitespace", "LineComment", "BlockComment", "LeftBrace", "RightBrace",
		"LeftBracket", "RightBracket", "Colon", "Comma", "String", "Number",
		"True", "False", "Null", "ErrorRegion":
		return true
	}
	return domainVersion == 2 && kind == "Identifier"
}

func isTOMLSyntaxKind(kind string) bool {
	switch kind {
	case "Whitespace", "Newline", "Comment", "String", "Bare", "Equals",
		"LeftBracket", "RightBracket", "LeftBrace", "RightBrace", "Comma", "Dot":
		return true
	}
	return false
}

func isYAMLSyntaxKind(kind string) bool {
	switch kind {
	case "Bom", "Whitespace", "Newline", "Comment", "Directive", "DocumentStart",
		"DocumentEnd", "FlowSequenceStart", "FlowSequenceEnd", "FlowMappingStart",
		"FlowMappingEnd", "FlowEntry", "SequenceEntry", "ExplicitKey", "MappingValue",
		"Anchor", "Alias", "Tag", "PlainScalar", "SingleQuotedScalar",
		"DoubleQuotedScalar", "LiteralBlockHeader", "FoldedBlockHeader",
		"BlockScalarContent", "ErrorRegion":
		return true
	}
	return false
}

func isINISyntaxKind(kind string) bool {
	switch kind {
	case "Bom", "Whitespace", "LineBreak", "CommentMarker", "CommentText",
		"SectionOpen", "SectionName", "SectionClose", "EntryKey", "Delimiter",
		"Quote", "EntryValue", "ContinuationMarker", "ErrorRegion":
		return true
	}
	return false
}

func isPropertiesSyntaxKind(kind string) bool {
	switch kind {
	case "Bom", "Whitespace", "LineBreak", "CommentMarker", "CommentText",
		"Key", "Separator", "Value", "EscapeMarker", "EscapeBody",
		"ContinuationMarker", "ErrorRegion":
		return true
	}
	return false
}

func isXMLSyntaxKind(kind string) bool {
	switch kind {
	case "bom", "whitespace", "line-break", "declaration-open", "declaration-name",
		"declaration-value", "declaration-close", "doctype-open", "doctype-name",
		"dtd-markup", "doctype-close", "tag-open", "tag-close",
		"empty-element-close", "end-tag-open", "prefix", "local-name", "colon",
		"attribute-name", "equals", "quote", "attribute-value",
		"namespace-declaration", "text", "entity-reference", "character-reference",
		"cdata-open", "cdata-text", "cdata-close", "comment-open", "comment-text",
		"comment-close", "processing-instruction-open",
		"processing-instruction-target", "processing-instruction-content",
		"processing-instruction-close", "error-region":
		return true
	}
	return false
}

func isPlistValueKind(kind string) bool {
	switch kind {
	case "dict", "array", "string", "integer", "real", "boolean", "date", "data", "uid":
		return true
	}
	return false
}

func isPlistSyntaxKind(kind string) bool {
	switch kind {
	case "bom", "whitespace", "line-break", "declaration-open", "declaration-name",
		"declaration-value", "declaration-close", "doctype-open", "doctype-body",
		"doctype-close", "plist-open", "plist-version-name", "plist-version-value",
		"plist-close", "dict-open", "dict-close", "key-open", "key-close",
		"array-open", "array-close", "string-open", "string-close", "integer-open",
		"integer-close", "real-open", "real-close", "date-open", "date-close",
		"data-open", "data-close", "true", "false", "text", "entity-reference",
		"character-reference", "cdata-open", "cdata-text", "cdata-close",
		"comment-open", "comment-text", "comment-close",
		"processing-instruction-open", "processing-instruction-target",
		"processing-instruction-content", "processing-instruction-close",
		"error-region":
		return true
	}
	return false
}

func isHCLExpressionKind(kind string) bool {
	switch kind {
	case "number", "boolean", "null", "template", "function-call", "variable-ref",
		"traversal", "unary", "binary", "conditional", "for-tuple", "for-object",
		"tuple", "object", "parenthesized":
		return true
	}
	return false
}

func isHCLSyntaxKind(kind string) bool {
	switch kind {
	case "Whitespace", "LineBreak", "LineComment", "InlineComment", "Identifier",
		"Equals", "Number", "StringOpen", "StringContent", "StringClose",
		"InterpolationOpen", "InterpolationContent", "InterpolationClose",
		"DirectiveOpen", "DirectiveContent", "DirectiveClose", "HeredocOpen",
		"HeredocContent", "HeredocClose", "BraceOpen", "BraceClose", "BracketOpen",
		"BracketClose", "ParenOpen", "ParenClose", "Comma", "Colon", "QuestionMark",
		"Operator", "ErrorRegion":
		return true
	}
	return false
}

func isHCLLiteralAccessor(accessor string) bool {
	switch accessor {
	case "as-string", "as-integer", "as-real", "as-boolean-is", "as-null-is":
		return true
	}
	return false
}
