package protocol

import (
	"sort"
	"strings"

	"consema.dev/consema/core"
)

// This file implements the stable public diagnostic and failure code
// registry (consema-rs/consema-protocol/src/error_registry.rs). The v7 registry
// pins 187 codes (55/62/90/92/132/166/187 across v1..v7); every code carries
// its semantic category, first release, and a human-facing description.
// The records are transcribed verbatim from the Rust registries, which are
// the content authority.

// DiagnosticCategory is the semantic category of one registered error code
// (the language-neutral category spellings of the error-code manifest).
type DiagnosticCategory string

// The eleven frozen diagnostic categories.
const (
	CategoryLexical         DiagnosticCategory = "Lexical"
	CategorySyntax          DiagnosticCategory = "Syntax"
	CategoryConformance     DiagnosticCategory = "Conformance"
	CategorySemantic        DiagnosticCategory = "Semantic"
	CategoryQuery           DiagnosticCategory = "Query"
	CategoryProjection      DiagnosticCategory = "Projection"
	CategoryMaterialization DiagnosticCategory = "Materialization"
	CategoryConversion      DiagnosticCategory = "Conversion"
	CategoryEdit            DiagnosticCategory = "Edit"
	CategoryResource        DiagnosticCategory = "Resource"
	CategoryEncoding        DiagnosticCategory = "Encoding"
)

// ParseDiagnosticCategory parses one canonical category spelling.
func ParseDiagnosticCategory(name string) (DiagnosticCategory, error) {
	switch name {
	case "Lexical":
		return CategoryLexical, nil
	case "Syntax":
		return CategorySyntax, nil
	case "Conformance":
		return CategoryConformance, nil
	case "Semantic":
		return CategorySemantic, nil
	case "Query":
		return CategoryQuery, nil
	case "Projection":
		return CategoryProjection, nil
	case "Materialization":
		return CategoryMaterialization, nil
	case "Conversion":
		return CategoryConversion, nil
	case "Edit":
		return CategoryEdit, nil
	case "Resource":
		return CategoryResource, nil
	case "Encoding":
		return CategoryEncoding, nil
	}
	return "", invalid("$.category", "unknown error-code category")
}

// String returns the canonical category spelling.
func (c DiagnosticCategory) String() string { return string(c) }

// ErrorCodeDescriptor is one stable public code registry record.
type ErrorCodeDescriptor struct {
	// Code is the full namespaced code including `@version`.
	Code string
	// Category is the semantic category.
	Category DiagnosticCategory
	// Introduced is the first Consema release containing the code.
	Introduced string
	// Description is the human-facing summary; not part of control flow.
	Description string
}

// ErrorRegistryVersion selects one frozen semantic-model error registry.
type ErrorRegistryVersion uint8

const (
	// ErrorRegistryV1 is the Consema 0.3 error registry (55 codes).
	ErrorRegistryV1 ErrorRegistryVersion = iota
	// ErrorRegistryV2 is the Consema 0.4 error registry (62 codes).
	ErrorRegistryV2
	// ErrorRegistryV3 is the Consema 0.5 error registry (90 codes).
	ErrorRegistryV3
	// ErrorRegistryV4 is the Consema 0.6 error registry (92 codes).
	ErrorRegistryV4
	// ErrorRegistryV5 is the Consema 0.7 error registry (132 codes).
	ErrorRegistryV5
	// ErrorRegistryV6 is the Consema 0.8 error registry (166 codes).
	ErrorRegistryV6
	// ErrorRegistryV7 is the Consema 0.12 error registry (187 codes,
	// including the CLI error family).
	ErrorRegistryV7
)

// ErrorCodeRegistry is a closed, explicitly versioned error-code registry.
type ErrorCodeRegistry struct {
	version ErrorRegistryVersion
}

// NewErrorCodeRegistry returns the registry for one frozen semantic-model
// version.
func NewErrorCodeRegistry(version ErrorRegistryVersion) ErrorCodeRegistry {
	return ErrorCodeRegistry{version: version}
}

// DefaultErrorCodeRegistry returns the semantic-model v1 registry (the Rust
// Default).
func DefaultErrorCodeRegistry() ErrorCodeRegistry {
	return NewErrorCodeRegistry(ErrorRegistryV1)
}

// Version reports the semantic-model version of the registry.
func (r ErrorCodeRegistry) Version() ErrorRegistryVersion { return r.version }

// Codes returns the sorted immutable descriptors (a copy; the records are
// never mutated).
func (r ErrorCodeRegistry) Codes() []ErrorCodeDescriptor {
	return append([]ErrorCodeDescriptor(nil), codesForVersion(r.version)...)
}

// Contains reports whether an exact full code is registered.
func (r ErrorCodeRegistry) Contains(candidate string) bool {
	return r.Descriptor(candidate) != nil
}

// Descriptor returns the exact registered descriptor for one code.
func (r ErrorCodeRegistry) Descriptor(candidate string) *ErrorCodeDescriptor {
	records := codesForVersion(r.version)
	index := sort.Search(len(records), func(i int) bool { return records[i].Code >= candidate })
	if index < len(records) && records[index].Code == candidate {
		return &records[index]
	}
	return nil
}

// Validate rejects an unregistered public code
// (error_registry.rs:1495-1510).
func (r ErrorCodeRegistry) Validate(candidate string) error {
	return r.validateAt(candidate, "$.code")
}

func (r ErrorCodeRegistry) validateAt(candidate, path string) error {
	if r.Contains(candidate) {
		return nil
	}
	return invalid(path, "unregistered public code: "+candidate)
}

func errorCode(id string, category DiagnosticCategory, introduced, description string) ErrorCodeDescriptor {
	return ErrorCodeDescriptor{Code: id, Category: category, Introduced: introduced, Description: description}
}

// codesForVersion returns the frozen records of one semantic-model version.
// Versions v2..v7 are built as sorted merges of the previous version plus
// the version's new codes, mirroring the Rust const-merge builders
// (error_registry.rs:412-1367); the test battery re-pins the counts,
// sortedness, and superset relationships.
func codesForVersion(version ErrorRegistryVersion) []ErrorCodeDescriptor {
	switch version {
	case ErrorRegistryV1:
		return errorCodesV1
	case ErrorRegistryV2:
		return mergeErrorCodes(errorCodesV1, newCodesV2)
	case ErrorRegistryV3:
		return mergeErrorCodes(codesForVersion(ErrorRegistryV2), newCodesV3)
	case ErrorRegistryV4:
		return mergeErrorCodes(codesForVersion(ErrorRegistryV3), newCodesV4)
	case ErrorRegistryV5:
		return mergeErrorCodes(codesForVersion(ErrorRegistryV4), newCodesV5)
	case ErrorRegistryV6:
		return mergeErrorCodes(codesForVersion(ErrorRegistryV5), newCodesV6)
	case ErrorRegistryV7:
		return mergeErrorCodes(codesForVersion(ErrorRegistryV6), newCodesV7)
	}
	return nil
}

// mergeErrorCodes merges two strictly sorted code lists into one strictly
// sorted list, rejecting duplicates.
func mergeErrorCodes(old, added []ErrorCodeDescriptor) []ErrorCodeDescriptor {
	merged := make([]ErrorCodeDescriptor, 0, len(old)+len(added))
	left, right := 0, 0
	for left < len(old) && right < len(added) {
		if old[left].Code < added[right].Code {
			merged = append(merged, old[left])
			left++
		} else {
			merged = append(merged, added[right])
			right++
		}
	}
	merged = append(merged, old[left:]...)
	merged = append(merged, added[right:]...)
	return merged
}

// The semantic-model v1 records (ERROR_CODES_V1, 55 codes). Strictly sorted
// by code; introduced versions and descriptions transcribed verbatim from
// consema-rs/consema-protocol/src/error_registry.rs:31-362.
var errorCodesV1 = []ErrorCodeDescriptor{
	errorCode("core.diagnostic.truncated@1", CategoryResource, "0.1.0", "Diagnostic limit truncated a sequence"),
	errorCode("core.parse.resource-limit@1", CategoryResource, "0.1.0", "Parser resource limit was reached"),
	errorCode("core.projection.conflicting-policy@1", CategoryProjection, "0.1.0", "Projection policy rules conflict"),
	errorCode("core.projection.invalid-policy-target@1", CategoryProjection, "0.1.0", "Projection policy target is invalid"),
	errorCode("core.projection.resource-limit@1", CategoryResource, "0.1.0", "Projection resource limit was reached"),
	errorCode("core.projection.target-not-applicable@1", CategoryProjection, "0.1.0", "Projection target does not apply"),
	errorCode("core.projection.wrong-snapshot-policy@1", CategoryProjection, "0.1.0", "Projection policy uses another snapshot"),
	errorCode("core.protocol.invalid-json@1", CategoryEncoding, "0.3.0", "Protocol JSON is invalid"),
	errorCode("core.protocol.invalid-pvce@1", CategoryEncoding, "0.3.0", "Protocol PVCE is invalid"),
	errorCode("core.protocol.invalid-value@1", CategoryEncoding, "0.3.0", "Protocol field value violates its invariant"),
	errorCode("core.protocol.missing-field@1", CategoryEncoding, "0.3.0", "Required protocol field is absent"),
	errorCode("core.protocol.non-canonical-json@1", CategoryEncoding, "0.3.0", "Protocol JSON is not canonical"),
	errorCode("core.protocol.process-local-handle@1", CategoryEncoding, "0.3.0", "Process-local handle cannot cross the wire"),
	errorCode("core.protocol.resource-limit@1", CategoryResource, "0.3.0", "Protocol resource limit was reached"),
	errorCode("core.protocol.schema-mismatch@1", CategoryEncoding, "0.3.0", "Protocol schema or field order does not match"),
	errorCode("core.protocol.unknown-contract@1", CategoryEncoding, "0.3.0", "Protocol contract ID or version is unknown"),
	errorCode("core.protocol.unknown-field@1", CategoryEncoding, "0.3.0", "Fixed protocol schema contains an unknown field"),
	errorCode("core.protocol.wrong-type@1", CategoryEncoding, "0.3.0", "Protocol field has the wrong value type"),
	errorCode("core.query.cancelled@1", CategoryQuery, "0.3.0", "Query execution was cancelled"),
	errorCode("core.query.cardinality-violation@1", CategoryQuery, "0.3.0", "Query selection cardinality was violated"),
	errorCode("core.query.domain-mismatch@1", CategoryQuery, "0.3.0", "Query domain is unknown or mismatched"),
	errorCode("core.query.invalid-argument@1", CategoryQuery, "0.3.0", "Query operator argument is invalid"),
	errorCode("core.query.invalid-composition@1", CategoryQuery, "0.3.0", "Query operator roles cannot be composed"),
	errorCode("core.query.missing-capability@1", CategoryQuery, "0.3.0", "Query implementation lacks a required capability"),
	errorCode("core.query.required-type-mismatch@1", CategoryQuery, "0.3.0", "Required query value type did not match"),
	errorCode("core.query.resource-limit@1", CategoryResource, "0.3.0", "Query resource limit was reached"),
	errorCode("core.query.target-unavailable@1", CategoryQuery, "0.3.0", "Target native semantics are unavailable"),
	errorCode("core.query.unknown-operator@1", CategoryQuery, "0.3.0", "Query operator ID or version is unknown"),
	errorCode("core.query.wrong-argument-type@1", CategoryQuery, "0.3.0", "Query operator argument has the wrong type"),
	errorCode("core.source.invalid-utf8@1", CategoryLexical, "0.1.0", "Source bytes are not valid UTF-8"),
	errorCode("json.edit.representation-fallback@1", CategoryEdit, "0.1.0", "JSON edit used an authorized canonical fallback"),
	errorCode("json.object.duplicate-member@1", CategorySemantic, "0.1.0", "JSON object contains duplicate member names"),
	errorCode("json.projection.duplicate-keys@1", CategoryProjection, "0.1.0", "JSON projection encountered duplicate keys"),
	errorCode("json.projection.semantic-unavailable@1", CategoryProjection, "0.1.0", "Recovered JSON region lacks native semantics"),
	errorCode("json.strict.comment-not-allowed@1", CategoryConformance, "0.1.0", "Strict JSON profile rejects comments"),
	errorCode("json.strict.leading-bom@1", CategoryConformance, "0.1.0", "Strict JSON source has a leading BOM"),
	errorCode("json.strict.trailing-comma@1", CategoryConformance, "0.1.0", "Strict JSON profile rejects trailing commas"),
	errorCode("json.syntax.expected-object-key@1", CategorySyntax, "0.1.0", "JSON object key was expected"),
	errorCode("json.syntax.expected-value@1", CategorySyntax, "0.1.0", "JSON value was expected"),
	errorCode("json.syntax.invalid-number@1", CategorySyntax, "0.1.0", "JSON number syntax is invalid"),
	errorCode("json.syntax.invalid-string-escape@1", CategorySyntax, "0.1.0", "JSON string escape is invalid"),
	errorCode("json.syntax.missing-array-close@1", CategorySyntax, "0.1.0", "JSON array close delimiter is missing"),
	errorCode("json.syntax.missing-colon@1", CategorySyntax, "0.1.0", "JSON member colon is missing"),
	errorCode("json.syntax.missing-comma@1", CategorySyntax, "0.1.0", "JSON container comma is missing"),
	errorCode("json.syntax.missing-object-close@1", CategorySyntax, "0.1.0", "JSON object close delimiter is missing"),
	errorCode("json.syntax.missing-value@1", CategorySyntax, "0.1.0", "JSON value is missing"),
	errorCode("json.syntax.trailing-content@1", CategorySyntax, "0.1.0", "JSON has trailing content"),
	errorCode("json.syntax.unexpected-character@1", CategorySyntax, "0.1.0", "JSON has an unexpected character"),
	errorCode("json.syntax.unexpected-word@1", CategorySyntax, "0.1.0", "JSON has an unexpected word"),
	errorCode("json.syntax.unterminated-block-comment@1", CategorySyntax, "0.1.0", "JSONC block comment is unterminated"),
	errorCode("json.syntax.unterminated-string@1", CategorySyntax, "0.1.0", "JSON string is unterminated"),
	errorCode("toml.edit.representation-fallback@1", CategoryEdit, "0.2.0", "TOML edit used an authorized canonical fallback"),
	errorCode("toml.parse.syntax@1", CategorySyntax, "0.2.0", "TOML syntax is invalid"),
	errorCode("toml.projection.core-invariant@1", CategoryProjection, "0.2.0", "TOML projection hit a core invariant"),
	errorCode("toml.projection.unrepresentable-datetime@1", CategoryProjection, "0.2.0", "TOML temporal value is not exactly representable"),
}

// The semantic-model v2 additions (7 codes: SOURCE_CODES_V2_BEFORE_UTF8
// plus SOURCE_CODES_V2_AFTER_UTF8).
var newCodesV2 = []ErrorCodeDescriptor{
	errorCode("core.source.encoding-conflict@1", CategoryEncoding, "0.4.0", "Source encoding facts conflict"),
	errorCode("core.source.invalid-sequence@1", CategoryLexical, "0.4.0", "Source bytes are invalid for the selected encoding"),
	errorCode("core.source.patch-base-mismatch@1", CategoryEdit, "0.4.0", "SourcePatch base digest does not match"),
	errorCode("core.source.patch-original-mismatch@1", CategoryEdit, "0.4.0", "SourcePatch original-byte precondition does not match"),
	errorCode("core.source.patch-target-mismatch@1", CategoryEdit, "0.4.0", "SourcePatch target digest does not match"),
	errorCode("core.source.resource-limit@1", CategoryResource, "0.4.0", "Source construction or patch limit was reached"),
	errorCode("core.source.unsupported-bom@1", CategoryEncoding, "0.4.0", "Source begins with an unsupported byte-order mark"),
}

// The semantic-model v3 additions (28 codes: NEW_CODES_V3).
var newCodesV3 = []ErrorCodeDescriptor{
	errorCode("core.conversion.materialization-failed@1", CategoryConversion, "0.5.0", "Conversion target materialization failed"),
	errorCode("core.conversion.projection-failed@1", CategoryConversion, "0.5.0", "Conversion source projection failed"),
	errorCode("core.conversion.unauthorized-loss@1", CategoryConversion, "0.5.0", "Conversion encountered loss without explicit authorization"),
	errorCode("core.edit.conflicting-edits@1", CategoryEdit, "0.5.0", "Edit operations have conflicting source ownership"),
	errorCode("core.edit.duplicate-key@1", CategoryEdit, "0.5.0", "Edit would create a duplicate key"),
	errorCode("core.edit.exact-literal-requires-literal@1", CategoryEdit, "0.5.0", "Exact literal policy requires a literal operation"),
	errorCode("core.edit.formation-failed@1", CategoryEdit, "0.5.0", "Edited bytes did not form the required target document"),
	errorCode("core.edit.incomplete-target@1", CategoryEdit, "0.5.0", "Edit target is not a complete syntax node"),
	errorCode("core.edit.invalid-literal@1", CategoryEdit, "0.5.0", "Edit literal is invalid for the target profile"),
	errorCode("core.edit.operation-unsupported@1", CategoryEdit, "0.5.0", "Edit operation is not supported for the target"),
	errorCode("core.edit.precondition-failed@1", CategoryEdit, "0.5.0", "Edit original-byte or digest precondition failed"),
	errorCode("core.edit.representation-incompatible@1", CategoryEdit, "0.5.0", "Edit representation policy cannot preserve the target category"),
	errorCode("core.edit.resource-limit@1", CategoryResource, "0.5.0", "Edit planning or commit resource limit was reached"),
	errorCode("core.edit.semantic-unavailable@1", CategoryEdit, "0.5.0", "Edit target native semantics are unavailable"),
	errorCode("core.edit.target-not-found@1", CategoryEdit, "0.5.0", "Edit target or placement anchor was not found"),
	errorCode("core.edit.unsupported-value@1", CategoryEdit, "0.5.0", "Edit value is not representable by the target profile"),
	errorCode("core.edit.wrong-role@1", CategoryEdit, "0.5.0", "Edit target has the wrong structural role"),
	errorCode("core.edit.wrong-snapshot@1", CategoryEdit, "0.5.0", "Edit target belongs to another snapshot"),
	errorCode("core.materialization.formation-failed@1", CategoryMaterialization, "0.5.0", "Generated bytes did not form the target profile"),
	errorCode("core.materialization.invalid-request@1", CategoryMaterialization, "0.5.0", "Materialization request fields are contradictory"),
	errorCode("core.materialization.mapping-transformed@1", CategoryMaterialization, "0.5.0", "Ordered mapping was explicitly transformed into an object"),
	errorCode("core.materialization.resource-limit@1", CategoryResource, "0.5.0", "Materialization resource limit was reached"),
	errorCode("core.materialization.unrepresentable@1", CategoryMaterialization, "0.5.0", "Portable input cannot be represented by the target profile"),
	errorCode("core.materialization.unsupported-encoding@1", CategoryEncoding, "0.5.0", "Target profile does not support the requested encoding"),
	errorCode("core.materialization.unsupported-newline@1", CategoryMaterialization, "0.5.0", "Target style does not support the requested newline policy"),
	errorCode("core.materialization.unsupported-profile@1", CategoryMaterialization, "0.5.0", "Requested materialization profile is unavailable"),
	errorCode("core.materialization.unsupported-style@1", CategoryMaterialization, "0.5.0", "Requested materialization style is unavailable"),
	errorCode("json.projection.structure-reencoded@1", CategoryProjection, "0.5.0", "JSON object structure was reversibly represented as an entry mapping"),
}

// The semantic-model v4 additions (2 codes: NEW_CODES_V4).
var newCodesV4 = []ErrorCodeDescriptor{
	errorCode("json5.string.unescaped-line-separator@1", CategoryConformance, "0.6.0", "JSON5 string contains an unescaped Unicode line separator"),
	errorCode("json5.syntax.invalid-identifier@1", CategorySyntax, "0.6.0", "JSON5 IdentifierName syntax is invalid"),
}

// The semantic-model v5 additions (40 codes: NEW_CODES_V5).
var newCodesV5 = []ErrorCodeDescriptor{
	errorCode("core.graph.invalid@1", CategorySemantic, "0.7.0", "PortableGraph construction invariants were violated"),
	errorCode("core.graph.resource-limit@1", CategoryResource, "0.7.0", "PortableGraph construction or traversal limit was reached"),
	errorCode("core.pgce.invalid@1", CategoryEncoding, "0.7.0", "PGCE input is structurally invalid"),
	errorCode("core.pgce.non-canonical@1", CategoryEncoding, "0.7.0", "PGCE input is valid but not canonical"),
	errorCode("core.pgce.resource-limit@1", CategoryResource, "0.7.0", "PGCE encode or decode limit was reached"),
	errorCode("core.pgce.unsupported-version@1", CategoryEncoding, "0.7.0", "PGCE wire version is unsupported"),
	errorCode("yaml.alias.name-mismatch@1", CategorySemantic, "0.7.0", "YAML alias name does not match its resolved anchor"),
	errorCode("yaml.alias.name-unavailable@1", CategorySemantic, "0.7.0", "YAML alias event lacks a usable name"),
	errorCode("yaml.anchor.name-unavailable@1", CategorySemantic, "0.7.0", "YAML anchor event lacks a usable name"),
	errorCode("yaml.anchor.unknown@1", CategorySemantic, "0.7.0", "YAML alias refers to an undefined anchor"),
	errorCode("yaml.edit.anchor-dependency@1", CategoryEdit, "0.7.0", "YAML edit would leave a live alias without its anchor"),
	errorCode("yaml.edit.anchor-not-visible@1", CategoryEdit, "0.7.0", "YAML alias insertion target is not the visible anchor definition"),
	errorCode("yaml.edit.canonical-fallback@1", CategoryEdit, "0.7.0", "YAML edit used an authorized canonical scalar fallback"),
	errorCode("yaml.edit.invalid-anchor-name@1", CategoryEdit, "0.7.0", "YAML anchor edit name is invalid"),
	errorCode("yaml.edit.invalid-placement@1", CategoryEdit, "0.7.0", "YAML structural edit placement is invalid"),
	errorCode("yaml.edit.structural-container-conflict@1", CategoryEdit, "0.7.0", "Multiple structural edits target the same base YAML container"),
	errorCode("yaml.mapping.missing-value@1", CategorySemantic, "0.7.0", "YAML mapping event stream lacks an association value"),
	errorCode("yaml.materialization.cross-document-sharing@1", CategoryMaterialization, "0.7.0", "YAML cannot preserve graph sharing across document roots"),
	errorCode("yaml.materialization.round-trip-mismatch@1", CategoryMaterialization, "0.7.0", "Generated YAML did not reproduce the promised input value"),
	errorCode("yaml.materialization.tag-kind-mismatch@1", CategoryMaterialization, "0.7.0", "YAML tag is incompatible with the graph node kind"),
	errorCode("yaml.materialization.unsupported-tag@1", CategoryMaterialization, "0.7.0", "YAML materializer has no published constructor for a tag"),
	errorCode("yaml.native.invalid-source-span@1", CategorySemantic, "0.7.0", "YAML native event span is outside the source snapshot"),
	errorCode("yaml.native.trailing-events@1", CategorySemantic, "0.7.0", "YAML native composition left trailing structural events"),
	errorCode("yaml.native.trailing-named-occurrence@1", CategorySemantic, "0.7.0", "YAML native composition left an unmatched anchor or alias occurrence"),
	errorCode("yaml.native.unexpected-end@1", CategorySemantic, "0.7.0", "YAML native event stream ended unexpectedly"),
	errorCode("yaml.native.unexpected-event@1", CategorySemantic, "0.7.0", "YAML native event order is invalid"),
	errorCode("yaml.parse.syntax@1", CategorySyntax, "0.7.0", "YAML source does not satisfy the selected grammar"),
	errorCode("yaml.profile.version-directive@1", CategoryConformance, "0.7.0", "YAML version directive conflicts with the selected profile"),
	errorCode("yaml.projection.cycle@1", CategoryProjection, "0.7.0", "YAML representation cycle cannot enter a PortableValue tree"),
	errorCode("yaml.projection.document-cardinality@1", CategoryProjection, "0.7.0", "YAML stream cardinality does not satisfy a single-value projection"),
	errorCode("yaml.projection.graph-invalid@1", CategoryProjection, "0.7.0", "YAML representation graph could not form a PortableGraph"),
	errorCode("yaml.projection.invalid-canonical-scalar@1", CategoryProjection, "0.7.0", "YAML canonical scalar cannot form its promised PortableValue kind"),
	errorCode("yaml.projection.mapping-not-object@1", CategoryProjection, "0.7.0", "YAML mapping does not satisfy the requested Object policy"),
	errorCode("yaml.projection.provenance-limit@1", CategoryResource, "0.7.0", "YAML graph projection provenance limit was reached"),
	errorCode("yaml.projection.resource-limit@1", CategoryResource, "0.7.0", "YAML value or graph projection limit was reached"),
	errorCode("yaml.projection.sharing@1", CategoryProjection, "0.7.0", "YAML shared identity requires explicit tree-duplication policy"),
	errorCode("yaml.projection.unrepresentable-timestamp@1", CategoryProjection, "0.7.0", "YAML timestamp is outside PortableValue temporal categories"),
	errorCode("yaml.projection.unsupported-tag@1", CategoryProjection, "0.7.0", "YAML tag has no published target projection semantics"),
	errorCode("yaml.scalar.invalid-explicit-tag@1", CategorySemantic, "0.7.0", "YAML scalar content is invalid for its explicit tag"),
	errorCode("yaml.tag.kind-mismatch@1", CategorySemantic, "0.7.0", "YAML tag is incompatible with the representation node kind"),
}

// The semantic-model v6 additions (34 codes: NEW_CODES_V6).
var newCodesV6 = []ErrorCodeDescriptor{
	errorCode("core.source.code-page-required@1", CategoryEncoding, "0.8.0", "The selected source profile requires an explicit Windows code page"),
	errorCode("core.source.unsupported-code-page@1", CategoryEncoding, "0.8.0", "The requested Windows code page is not in the portable registry"),
	errorCode("ini.edit.canonical-fallback@1", CategoryEdit, "0.8.0", "INI editing used an authorized canonical representation fallback"),
	errorCode("ini.edit.case-collision@1", CategoryEdit, "0.8.0", "INI editing would create a profile-equivalent name collision"),
	errorCode("ini.edit.invalid-name@1", CategoryEdit, "0.8.0", "INI section or entry name is invalid for the selected profile"),
	errorCode("ini.edit.invalid-placement@1", CategoryEdit, "0.8.0", "INI structural edit placement is invalid"),
	errorCode("ini.formation.case-collision@1", CategorySemantic, "0.8.0", "INI formation found profile-equivalent names with different spelling"),
	errorCode("ini.formation.duplicate-entry@1", CategorySemantic, "0.8.0", "INI formation found a duplicate entry"),
	errorCode("ini.formation.duplicate-section@1", CategorySemantic, "0.8.0", "INI formation found a duplicate section"),
	errorCode("ini.materialization.round-trip-mismatch@1", CategoryMaterialization, "0.8.0", "Generated INI did not reproduce the promised input value"),
	errorCode("ini.parse.invalid-character@1", CategorySyntax, "0.8.0", "INI source contains a character forbidden by the selected profile"),
	errorCode("ini.parse.invalid-continuation@1", CategorySyntax, "0.8.0", "INI continuation syntax is invalid"),
	errorCode("ini.parse.malformed-line@1", CategorySyntax, "0.8.0", "INI source line is malformed"),
	errorCode("ini.parse.malformed-section@1", CategorySyntax, "0.8.0", "INI section header is malformed"),
	errorCode("ini.parse.missing-delimiter@1", CategorySyntax, "0.8.0", "INI entry is missing a required key/value delimiter"),
	errorCode("ini.parse.missing-section@1", CategoryConformance, "0.8.0", "INI entry appears where the selected profile requires a section"),
	errorCode("ini.profile.encoding@1", CategoryEncoding, "0.8.0", "INI source encoding conflicts with the selected profile"),
	errorCode("ini.profile.mismatch@1", CategoryConformance, "0.8.0", "INI operation profile does not match the document profile"),
	errorCode("ini.projection.collision@1", CategoryProjection, "0.8.0", "INI projection encountered a rejected key or section collision"),
	errorCode("ini.projection.duplicate-collapsed@1", CategoryProjection, "0.8.0", "INI projection collapsed a duplicate under explicit policy"),
	errorCode("ini.projection.incomplete-document@1", CategoryProjection, "0.8.0", "Recovered INI syntax cannot enter a complete semantic projection"),
	errorCode("ini.query.invalid-name-mode@1", CategoryQuery, "0.8.0", "INI query name comparison mode is invalid"),
	errorCode("java-properties.edit.canonical-fallback@1", CategoryEdit, "0.8.0", "Properties editing used an authorized canonical representation fallback"),
	errorCode("java-properties.edit.invalid-placement@1", CategoryEdit, "0.8.0", "Properties structural edit placement is invalid"),
	errorCode("java-properties.java-string.invalid-wire@1", CategoryEncoding, "0.8.0", "Exact Java UTF-16 string wire content is invalid"),
	errorCode("java-properties.java-string.non-canonical-wire@1", CategoryEncoding, "0.8.0", "Exact Java UTF-16 string wire content is not canonical"),
	errorCode("java-properties.materialization.round-trip-mismatch@1", CategoryMaterialization, "0.8.0", "Generated Properties text did not reproduce the promised input value"),
	errorCode("java-properties.parse.malformed-unicode-escape@1", CategorySyntax, "0.8.0", "Properties Unicode escape is malformed"),
	errorCode("java-properties.profile.mismatch@1", CategoryConformance, "0.8.0", "Properties operation profile does not match the document profile"),
	errorCode("java-properties.projection.duplicate-collapsed@1", CategoryProjection, "0.8.0", "Properties projection collapsed a duplicate under explicit policy"),
	errorCode("java-properties.projection.incomplete-document@1", CategoryProjection, "0.8.0", "Recovered Properties syntax cannot enter a complete semantic projection"),
	errorCode("java-properties.projection.unpaired-surrogate@1", CategoryProjection, "0.8.0", "Properties content with an unpaired surrogate cannot become a PortableValue String"),
	errorCode("java-properties.query.invalid-code-unit-filter@1", CategoryQuery, "0.8.0", "Properties query UTF-16 code-unit filter is invalid"),
	errorCode("java-properties.source.profile-encoding@1", CategoryEncoding, "0.8.0", "Properties source encoding conflicts with the selected profile"),
}

// The semantic-model v7 additions (21 codes: NEW_CODES_V7 — the RFC 0015
// §13.1 CLI error family of 20 codes plus the 0.13.0
// json.projection.incomplete-document@1 registration, audit finding F3).
var newCodesV7 = []ErrorCodeDescriptor{
	errorCode("cli.data.invalid-request@1", CategoryEncoding, "0.12.0", "Request or plan file failed strict decoding"),
	errorCode("cli.data.io@1", CategoryEncoding, "0.12.0", "Input file could not be read"),
	errorCode("cli.detection.ambiguous@1", CategorySemantic, "0.12.0", "Candidate profiles are ambiguous and no profile was selected"),
	errorCode("cli.internal.unclassified@1", CategorySemantic, "0.12.0", "Unclassified internal CLI error"),
	errorCode("cli.interrupted.signal@1", CategorySemantic, "0.12.0", "CLI execution was interrupted by a signal"),
	errorCode("cli.limit.batch-count@1", CategoryResource, "0.12.0", "Batch file count exceeded the configured limit"),
	errorCode("cli.limit.file-size@1", CategoryResource, "0.12.0", "Input file exceeded the CLI file-size limit"),
	errorCode("cli.limit.manifest-size@1", CategoryResource, "0.12.0", "Manifest or request input exceeded the size limit"),
	errorCode("cli.usage.invalid-argument@1", CategorySyntax, "0.12.0", "Known argument received an invalid value"),
	errorCode("cli.usage.invalid-format@1", CategorySyntax, "0.12.0", "--format is missing or invalid"),
	errorCode("cli.usage.missing-plan@1", CategorySyntax, "0.12.0", "--apply requires a prior plan"),
	errorCode("cli.usage.missing-required@1", CategorySyntax, "0.12.0", "A required argument such as --profile is missing"),
	errorCode("cli.usage.redaction-pattern@1", CategorySyntax, "0.12.0", "--redact-keys pattern is invalid"),
	errorCode("cli.usage.unknown-argument@1", CategorySyntax, "0.12.0", "Unknown argument or rejected abbreviation"),
	errorCode("cli.usage.unknown-command@1", CategorySyntax, "0.12.0", "Unknown command"),
	errorCode("cli.write.io@1", CategoryEdit, "0.12.0", "Write I/O failure such as a full disk"),
	errorCode("cli.write.permission@1", CategoryEdit, "0.12.0", "Permission denied while writing the target"),
	errorCode("cli.write.read-only@1", CategoryEdit, "0.12.0", "Target file is read-only"),
	errorCode("cli.write.symlink-policy@1", CategoryEdit, "0.12.0", "Write path rejected by the symlink policy"),
	errorCode("cli.write.target-is-directory@1", CategoryEdit, "0.12.0", "Write target is a directory"),
	// Registered in 0.13.0 (audit finding F3): the 0.13.0 json
	// Recovered-document gate emits this code (consema-json projection.rs:756)
	// and the CLI's failed projection record requires it to be
	// registry-validated; without the entry the CLI panicked on `.expect`.
	errorCode("json.projection.incomplete-document@1", CategoryProjection, "0.13.0", "Recovered JSON syntax cannot enter a complete semantic projection"),
}

// errorCodeManifestValueFor encodes one `core.error-code-registry@1`
// payload (error_registry.rs:1573-1594).
func errorCodeManifestValueFor(registry ErrorCodeRegistry) (core.Value, error) {
	items := make([]core.Value, 0, len(codesForVersion(registry.version)))
	for _, descriptor := range codesForVersion(registry.version) {
		entry, err := core.NewObject(
			core.Entry{Key: "code", Value: core.String(descriptor.Code)},
			core.Entry{Key: "category", Value: core.String(descriptor.Category.String())},
			core.Entry{Key: "introduced", Value: core.String(descriptor.Introduced)},
			core.Entry{Key: "stability", Value: core.String("Stable")},
			core.Entry{Key: "description", Value: core.String(descriptor.Description)},
		)
		if err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.error-code-registry@1")},
		core.Entry{Key: "error_codes", Value: core.NewArray(items...)},
	)
}

// ErrorCodeManifestValue encodes the semantic-model v7
// `core.error-code-registry@1` payload.
func ErrorCodeManifestValue() (core.Value, error) {
	return errorCodeManifestValueFor(NewErrorCodeRegistry(ErrorRegistryV7))
}

// ErrorCodeManifestValueForVersion encodes one frozen semantic-model
// version's payload.
func ErrorCodeManifestValueForVersion(version ErrorRegistryVersion) (core.Value, error) {
	return errorCodeManifestValueFor(NewErrorCodeRegistry(version))
}

// ValidateErrorCodeManifestValue strictly validates one transferable
// `core.error-code-registry@1` value (error_registry.rs:1596-1645). Identity,
// ordering, category, and stability are normative; the description wording is
// presentation metadata and is not re-checked for equality.
func ValidateErrorCodeManifestValue(value core.Value) error {
	fields, err := schemaFields(value, "core.error-code-registry@1", []string{"schema", "error_codes"}, "$")
	if err != nil {
		return err
	}
	items, err := sequenceOf(fields[1], "$.error_codes")
	if err != nil {
		return err
	}
	previous := ""
	for index, item := range items {
		path := "$.error_codes[" + uint32String(uint32(index)) + "]"
		entry, err := exactFields(item, []string{"code", "category", "introduced", "stability", "description"}, path)
		if err != nil {
			return err
		}
		code, err := stringOf(entry[0], path+".code")
		if err != nil {
			return err
		}
		if err := validateVersionedCode(code, path+".code"); err != nil {
			return err
		}
		categoryText, err := stringOf(entry[1], path+".category")
		if err != nil {
			return err
		}
		if _, err := ParseDiagnosticCategory(categoryText); err != nil {
			return err
		}
		introduced, err := stringOf(entry[2], path+".introduced")
		if err != nil {
			return err
		}
		description, err := stringOf(entry[4], path+".description")
		if err != nil {
			return err
		}
		if introduced == "" || description == "" {
			return invalid(path, "introduced and description must be non-empty")
		}
		stability, err := stringOf(entry[3], path+".stability")
		if err != nil {
			return err
		}
		if stability != "Stable" {
			return invalid(path+".stability", "unknown error-code stability")
		}
		if previous != "" && previous >= code {
			return invalid("$.error_codes", "error codes must be sorted and unique")
		}
		previous = code
	}
	return nil
}

// validateVersionedCode requires the `id@version` shape of a registered code
// (error_registry.rs:1647-1655).
func validateVersionedCode(code, path string) error {
	at := strings.LastIndexByte(code, '@')
	if at < 0 {
		return invalid(path, "code lacks @version suffix")
	}
	id, versionText := code[:at], code[at+1:]
	if versionText == "" {
		return invalid(path, "code version is invalid")
	}
	var version uint64
	for index := 0; index < len(versionText); index++ {
		digit := versionText[index]
		if digit < '0' || digit > '9' {
			return invalid(path, "code version is invalid")
		}
		version = version*10 + uint64(digit-'0')
		if version > 0xffffffff {
			return invalid(path, "code version is invalid")
		}
	}
	if version == 0 {
		return invalid(path, "code version is invalid")
	}
	return validateIdentifier(id, path)
}
