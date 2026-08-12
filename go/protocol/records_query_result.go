package protocol

// The `core.query-result@1` record and its native match locators
// (crates/consema-protocol/src/query.rs). Native matches are externalized by
// the caller with stable source identities; raw process-local node handles
// are explicitly rejected.

import (
	"consema.dev/consema/core"
)

// NativeMatchLocator is a transferable locator for one native match
// (query.rs:56-62).
type NativeMatchLocator struct {
	sourceID    string
	nodeLocator string
	role        MatchRole
	ordinal     uint64
}

// NewNativeMatchLocator creates a transferable locator for one native
// match (query.rs:66-90).
func NewNativeMatchLocator(sourceID, nodeLocator string, role MatchRole,
	ordinal uint64) (*NativeMatchLocator, error) {
	if sourceID == "" || len(sourceID) > 1024 || nodeLocator == "" || len(nodeLocator) > 4096 ||
		!isNativeRole(role) {
		return nil, invalid("$.native_match", "invalid source, locator, or native role")
	}
	return &NativeMatchLocator{
		sourceID:    sourceID,
		nodeLocator: nodeLocator,
		role:        role,
		ordinal:     ordinal,
	}, nil
}

// NativeMatchLocatorFromProcessLocal is the explicit rejection adapter for
// raw process-local handles: a Go NodeRef equivalent must be externalized to
// a stable caller locator before wire encoding (query.rs:92-97).
func NativeMatchLocatorFromProcessLocal() error {
	return protocolError(KindProcessLocalHandle, "$.native_match.node",
		"NodeRef must be externalized to a stable caller locator")
}

// SourceID returns the stable source identity.
func (l *NativeMatchLocator) SourceID() string { return l.sourceID }

// NodeLocator returns the stable caller locator.
func (l *NativeMatchLocator) NodeLocator() string { return l.nodeLocator }

// Role returns the native match role.
func (l *NativeMatchLocator) Role() MatchRole { return l.role }

// Ordinal returns the standard-order ordinal.
func (l *NativeMatchLocator) Ordinal() uint64 { return l.ordinal }

// ProtocolQueryMatch is one transferable query match (query.rs:127-146).
type ProtocolQueryMatch struct {
	// Kind is "Value", "ObjectEntry", "EntryMappingEntry", or "Native".
	Kind string
	// Path is the value path of Value matches.
	Path ValuePath
	// Value is the match value of Value matches.
	Value core.Value
	// Location is the association location of ObjectEntry/EntryMappingEntry
	// matches.
	Location AssociationLocation
	// Key is the object key or entry key of association matches.
	Key core.Value
	// ValuePath is the value path of the association value.
	ValuePath ValuePath
	// KeyPath is the key path of EntryMappingEntry matches.
	KeyPath ValuePath
	// Native is the native locator of Native matches.
	Native *NativeMatchLocator
}

func (m ProtocolQueryMatch) role() MatchRole {
	switch m.Kind {
	case "Value":
		return RoleValue
	case "ObjectEntry":
		return RoleObjectEntry
	case "EntryMappingEntry":
		return RoleEntryMappingEntry
	case "Native":
		return m.Native.role
	}
	return RoleValue
}

// QueryResultMessage is the complete or explicitly non-complete
// `core.query-result@1` record (query.rs:148-155).
type QueryResultMessage struct {
	domain      *QueryDomain
	role        MatchRole
	matches     []ProtocolQueryMatch
	completion  *Completion
	diagnostics []*Diagnostic
}

// NewQueryResultMessage validates domain, match roles, ordering ordinals,
// and completion counts (query.rs:158-191).
func NewQueryResultMessage(domain *QueryDomain, role MatchRole, matches []ProtocolQueryMatch,
	completion *Completion, diagnostics []*Diagnostic) (*QueryResultMessage, error) {
	if !isV1Role(role) {
		return nil, invalid("$.role", "role is not published by core.query-result@1")
	}
	if completion.Produced() != uint64(len(matches)) {
		return nil, invalid("$", "completion count or match role is inconsistent")
	}
	for _, match := range matches {
		if match.role() != role {
			return nil, invalid("$", "completion count or match role is inconsistent")
		}
	}
	previous := uint64(0)
	seen := false
	for _, match := range matches {
		if match.Kind != "Native" {
			continue
		}
		if seen && match.Native.ordinal <= previous {
			return nil, invalid("$.matches", "native match ordinals must be strictly increasing")
		}
		previous = match.Native.ordinal
		seen = true
	}
	return &QueryResultMessage{
		domain:      domain,
		role:        role,
		matches:     matches,
		completion:  completion,
		diagnostics: diagnostics,
	}, nil
}

// NewQueryResultFromPortableExecution converts a completed portable query
// execution (query.rs:193-215).
func NewQueryResultFromPortableExecution(domain *QueryDomain, role MatchRole,
	matches []ProtocolQueryMatch) (*QueryResultMessage, error) {
	count := uint64(len(matches))
	completion, err := NewCompletion(CompletionSuccess, count, count, nil, nil)
	if err != nil {
		return nil, err
	}
	return NewQueryResultMessage(domain, role, matches, completion, nil)
}

// Domain returns the query domain.
func (m *QueryResultMessage) Domain() *QueryDomain { return m.domain }

// Role returns the uniform result role.
func (m *QueryResultMessage) Role() MatchRole { return m.role }

// Matches returns the ordered matches.
func (m *QueryResultMessage) Matches() []ProtocolQueryMatch { return m.matches }

// Completion returns the explicit terminal state.
func (m *QueryResultMessage) Completion() *Completion { return m.completion }

// Diagnostics returns the ordered operation diagnostics.
func (m *QueryResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.query-result@1` (query.rs:256-282).
func (m *QueryResultMessage) ToValue() (core.Value, error) {
	matches := make([]core.Value, 0, len(m.matches))
	for _, match := range m.matches {
		value, err := protocolMatchValue(match)
		if err != nil {
			return nil, err
		}
		matches = append(matches, value)
	}
	diagnostics := make([]core.Value, 0, len(m.diagnostics))
	for _, diagnostic := range m.diagnostics {
		value, err := diagnostic.ToValue()
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, value)
	}
	completion, err := m.completion.ToValue()
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.query-result@1")},
		core.Entry{Key: "domain_id", Value: core.String(m.domain.id)},
		core.Entry{Key: "domain_version", Value: integerValue(uint64(m.domain.version))},
		core.Entry{Key: "role", Value: core.String(string(m.role))},
		core.Entry{Key: "matches", Value: core.NewArray(matches...)},
		core.Entry{Key: "completion", Value: completion},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
	)
}

// FromValueWithRegistry strictly decodes `core.query-result@1` under one
// explicit semantic-model registry (query.rs:298-...).
func (m *QueryResultMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*QueryResultMessage, error) {
	fields, err := schemaFields(value, "core.query-result@1",
		[]string{"schema", "domain_id", "domain_version", "role", "matches", "completion", "diagnostics"}, "$")
	if err != nil {
		return nil, err
	}
	domainID, err := stringOf(fields[1], "$.domain_id")
	if err != nil {
		return nil, err
	}
	domainVersion, err := unsigned32(fields[2], "$.domain_version")
	if err != nil {
		return nil, err
	}
	roleText, err := stringOf(fields[3], "$.role")
	if err != nil {
		return nil, err
	}
	role, ok := ParseMatchRole(roleText)
	if !ok {
		return nil, invalid("$.role", "unknown match role")
	}
	matchValues, err := sequenceOf(fields[4], "$.matches")
	if err != nil {
		return nil, err
	}
	matches := make([]ProtocolQueryMatch, 0, len(matchValues))
	for index, matchValue := range matchValues {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		match, err := parseProtocolMatch(matchValue, path)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[5], registry)
	if err != nil {
		return nil, err
	}
	diagnosticValues, err := sequenceOf(fields[6], "$.diagnostics")
	if err != nil {
		return nil, err
	}
	diagnostics := make([]*Diagnostic, 0, len(diagnosticValues))
	for _, diagnosticValue := range diagnosticValues {
		diagnostic := &Diagnostic{}
		decoded, err := diagnostic.FromValue(diagnosticValue, registry)
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, decoded)
	}
	return NewQueryResultMessage(NewQueryDomain(domainID, domainVersion), role, matches, completion, diagnostics)
}

// FromValue strictly decodes `core.query-result@1` under the v1 registry.
func (m *QueryResultMessage) FromValue(value core.Value) (*QueryResultMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// protocolMatchValue encodes one transferable match (query.rs:380-437).
func protocolMatchValue(match ProtocolQueryMatch) (core.Value, error) {
	switch match.Kind {
	case "Value":
		path, err := pathValue(match.Path)
		if err != nil {
			return nil, err
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Value")},
			core.Entry{Key: "path", Value: path},
			core.Entry{Key: "value", Value: match.Value},
		)
	case "ObjectEntry":
		location, err := associationValue(match.Location)
		if err != nil {
			return nil, err
		}
		valuePath, err := pathValue(match.ValuePath)
		if err != nil {
			return nil, err
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("ObjectEntry")},
			core.Entry{Key: "location", Value: location},
			core.Entry{Key: "key", Value: match.Key},
			core.Entry{Key: "value_path", Value: valuePath},
			core.Entry{Key: "value", Value: match.Value},
		)
	case "EntryMappingEntry":
		location, err := associationValue(match.Location)
		if err != nil {
			return nil, err
		}
		keyPath, err := pathValue(match.KeyPath)
		if err != nil {
			return nil, err
		}
		valuePath, err := pathValue(match.ValuePath)
		if err != nil {
			return nil, err
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("EntryMappingEntry")},
			core.Entry{Key: "location", Value: location},
			core.Entry{Key: "key_path", Value: keyPath},
			core.Entry{Key: "key", Value: match.Key},
			core.Entry{Key: "value_path", Value: valuePath},
			core.Entry{Key: "value", Value: match.Value},
		)
	case "Native":
		// Native matches are flat on the wire (query.rs:362-369): the
		// locator fields sit beside "kind" rather than under a nested
		// "native_match" record.
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Native")},
			core.Entry{Key: "role", Value: core.String(string(match.Native.role))},
			core.Entry{Key: "source_id", Value: core.String(match.Native.sourceID)},
			core.Entry{Key: "node_locator", Value: core.String(match.Native.nodeLocator)},
			core.Entry{Key: "ordinal", Value: integerValue(match.Native.ordinal)},
		)
	}
	return nil, invalid("$", "unknown query match kind")
}

func parseProtocolMatch(value core.Value, path string) (ProtocolQueryMatch, error) {
	entries, ok := value.(*core.Object)
	if !ok {
		return ProtocolQueryMatch{}, protocolError(KindWrongType, path, "expected match Object")
	}
	if len(entries.Entries()) == 0 || entries.Entries()[0].Key != "kind" {
		return ProtocolQueryMatch{}, invalid(path, "kind must be the first String field")
	}
	kind, err := stringOf(entries.Entries()[0].Value, path+".kind")
	if err != nil {
		return ProtocolQueryMatch{}, err
	}
	switch kind {
	case "Value":
		fields, err := exactFields(value, []string{"kind", "path", "value"}, path)
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		decodedPath, err := parsePath(fields[1], path+".path")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		return ProtocolQueryMatch{Kind: "Value", Path: decodedPath, Value: fields[2]}, nil
	case "ObjectEntry":
		fields, err := exactFields(value,
			[]string{"kind", "location", "key", "value_path", "value"}, path)
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		location, err := parseAssociation(fields[1], path+".location")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		valuePath, err := parsePath(fields[3], path+".value_path")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		return ProtocolQueryMatch{Kind: "ObjectEntry", Location: location, Key: fields[2],
			ValuePath: valuePath, Value: fields[4]}, nil
	case "EntryMappingEntry":
		fields, err := exactFields(value,
			[]string{"kind", "location", "key_path", "key", "value_path", "value"}, path)
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		location, err := parseAssociation(fields[1], path+".location")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		keyPath, err := parsePath(fields[2], path+".key_path")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		valuePath, err := parsePath(fields[4], path+".value_path")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		return ProtocolQueryMatch{Kind: "EntryMappingEntry", Location: location, KeyPath: keyPath,
			Key: fields[3], ValuePath: valuePath, Value: fields[5]}, nil
	case "Native":
		// The Native match is flat on the wire (query.rs:422-435): the
		// locator fields sit beside "kind" in canonical order kind, role,
		// source_id, node_locator, ordinal.
		fields, err := exactFields(value,
			[]string{"kind", "role", "source_id", "node_locator", "ordinal"}, path)
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		roleText, err := stringOf(fields[1], path+".role")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		role, ok := ParseMatchRole(roleText)
		if !ok {
			return ProtocolQueryMatch{}, invalid(path+".role", "unknown match role")
		}
		sourceID, err := stringOf(fields[2], path+".source_id")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		nodeLocator, err := stringOf(fields[3], path+".node_locator")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		ordinal, err := unsigned64(fields[4], path+".ordinal")
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		locator, err := NewNativeMatchLocator(sourceID, nodeLocator, role, ordinal)
		if err != nil {
			return ProtocolQueryMatch{}, err
		}
		return ProtocolQueryMatch{Kind: "Native", Native: locator}, nil
	}
	return ProtocolQueryMatch{}, invalid(path, "unknown query match kind")
}

// isV1Role reports whether the role is published by core.query-result@1
// (query.rs:628-...): every role except the graph, YAML, line-format,
// XML, plist, and HCL families.
func isV1Role(role MatchRole) bool {
	switch role {
	case RoleGraphNode, RoleGraphSequenceElement, RoleGraphMappingEntry,
		RoleYamlStream, RoleYamlDocument, RoleYamlNode, RoleYamlMappingEntry,
		RoleYamlSequenceElement, RoleYamlAnchorDefinition, RoleYamlAliasOccurrence,
		RoleYamlSyntaxPiece,
		RoleIniDocument, RoleIniSection, RoleIniDefaultSection, RoleIniEntry,
		RoleIniPhysicalLine, RoleIniLogicalLine, RoleIniErrorLine, RoleIniSyntaxPiece,
		RolePropertiesDocument, RolePropertiesNaturalLine, RolePropertiesLogicalLine,
		RolePropertiesProperty, RolePropertiesComment, RolePropertiesEscape,
		RolePropertiesErrorLine, RolePropertiesSyntaxPiece,
		RoleXmlDocument, RoleXmlDeclaration, RoleXmlDoctype, RoleXmlPrologItem,
		RoleXmlElement, RoleXmlContentItem, RoleXmlAttribute, RoleXmlNamespaceBinding,
		RoleXmlText, RoleXmlCdata, RoleXmlComment, RoleXmlProcessingInstruction,
		RoleXmlReference, RoleXmlErrorRegion, RoleXmlSyntaxPiece,
		RolePlistValue, RolePlistDictEntry, RolePlistKey, RolePlistArrayElement,
		RolePlistSyntaxPiece, RolePlistBinaryStructure, RolePlistBinaryObject,
		RolePlistBinaryOffset, RolePlistBinaryRef, RolePlistBinaryTrailer,
		RoleHclBody, RoleHclAttribute, RoleHclBlock, RoleHclBlockLabel,
		RoleHclExpression, RoleHclTemplatePart, RoleHclErrorRegion, RoleHclSyntaxPiece:
		return false
	}
	return true
}

// isNativeRole reports whether the role may appear in a Native match
// locator (query.rs:711-...).
func isNativeRole(role MatchRole) bool {
	switch role {
	case RoleJsonValue, RoleJsonObjectMember, RoleJsonArrayElement,
		RoleTomlItem, RoleTomlEntry, RoleTomlArrayElement,
		RoleJsonSyntaxPiece, RoleTomlSyntaxPiece:
		return true
	}
	return false
}

// ParseMatchRole parses one canonical match-role spelling into the closed
// role set.
func ParseMatchRole(text string) (MatchRole, bool) {
	switch MatchRole(text) {
	case RoleValue, RoleObjectEntry, RoleEntryMappingEntry,
		RoleJsonValue, RoleJsonObjectMember, RoleJsonArrayElement,
		RoleTomlItem, RoleTomlEntry, RoleTomlArrayElement,
		RoleYamlStream, RoleYamlDocument, RoleYamlNode, RoleYamlMappingEntry,
		RoleYamlSequenceElement, RoleYamlAnchorDefinition, RoleYamlAliasOccurrence,
		RoleJsonSyntaxPiece, RoleTomlSyntaxPiece, RoleYamlSyntaxPiece,
		RoleIniDocument, RoleIniSection, RoleIniDefaultSection, RoleIniEntry,
		RoleIniPhysicalLine, RoleIniLogicalLine, RoleIniErrorLine, RoleIniSyntaxPiece,
		RolePropertiesDocument, RolePropertiesNaturalLine, RolePropertiesLogicalLine,
		RolePropertiesProperty, RolePropertiesComment, RolePropertiesEscape,
		RolePropertiesErrorLine, RolePropertiesSyntaxPiece,
		RoleGraphNode, RoleGraphSequenceElement, RoleGraphMappingEntry,
		RoleXmlDocument, RoleXmlDeclaration, RoleXmlDoctype, RoleXmlPrologItem,
		RoleXmlElement, RoleXmlContentItem, RoleXmlAttribute, RoleXmlNamespaceBinding,
		RoleXmlText, RoleXmlCdata, RoleXmlComment, RoleXmlProcessingInstruction,
		RoleXmlReference, RoleXmlErrorRegion, RoleXmlSyntaxPiece,
		RolePlistValue, RolePlistDictEntry, RolePlistKey, RolePlistArrayElement,
		RolePlistSyntaxPiece, RolePlistBinaryStructure, RolePlistBinaryObject,
		RolePlistBinaryOffset, RolePlistBinaryRef, RolePlistBinaryTrailer,
		RoleHclBody, RoleHclAttribute, RoleHclBlock, RoleHclBlockLabel,
		RoleHclExpression, RoleHclTemplatePart, RoleHclErrorRegion, RoleHclSyntaxPiece:
		return MatchRole(text), true
	}
	return "", false
}
