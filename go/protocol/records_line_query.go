package protocol

// YAML, INI, and Java Properties query-result records
// (consema-rs/consema-protocol/src/yaml_query.rs, line_query.rs):
// `core.yaml-query-result@1`, `core.ini-query-result@1`, and
// `core.java-properties-query-result@1` with external caller-stable match
// locators.

import (
	"consema.dev/consema/core"
)

// YamlMatchLocator is a transferable YAML match locator
// (yaml_query.rs...).
type YamlMatchLocator struct {
	sourceID    string
	nodeLocator string
	role        MatchRole
	ordinal     uint64
}

// NewYamlMatchLocator creates a transferable YAML locator.
func NewYamlMatchLocator(sourceID, nodeLocator string, role MatchRole,
	ordinal uint64) (*YamlMatchLocator, error) {
	if sourceID == "" || len(sourceID) > 1024 || nodeLocator == "" || len(nodeLocator) > 4096 ||
		!isYamlRole(role) {
		return nil, invalid("$.yaml_match", "invalid source, locator, or YAML role")
	}
	return &YamlMatchLocator{
		sourceID:    sourceID,
		nodeLocator: nodeLocator,
		role:        role,
		ordinal:     ordinal,
	}, nil
}

// YamlMatchLocatorFromProcessLocal explicitly refuses a raw process-local
// YAML node handle (yaml_query.rs:...).
func YamlMatchLocatorFromProcessLocal() error {
	return protocolError(KindProcessLocalHandle, "$.yaml_match.node",
		"NodeRef requires a stable caller locator")
}

// SourceID returns the stable source identity.
func (l *YamlMatchLocator) SourceID() string { return l.sourceID }

// NodeLocator returns the stable caller-defined node locator.
func (l *YamlMatchLocator) NodeLocator() string { return l.nodeLocator }

// Role returns the exact YAML result role.
func (l *YamlMatchLocator) Role() MatchRole { return l.role }

// Ordinal returns the strictly increasing standard-result ordinal.
func (l *YamlMatchLocator) Ordinal() uint64 { return l.ordinal }

// YamlQueryResultMessage is the complete or explicitly non-complete
// `core.yaml-query-result@1` record (yaml_query.rs...).
type YamlQueryResultMessage struct {
	domain      *QueryDomain
	role        MatchRole
	matches     []*YamlMatchLocator
	completion  *Completion
	diagnostics []*Diagnostic
}

// NewYamlQueryResultMessage validates the YAML domain/role matrix,
// ordering, and produced count (yaml_query.rs).
func NewYamlQueryResultMessage(domain *QueryDomain, role MatchRole, matches []*YamlMatchLocator,
	completion *Completion, diagnostics []*Diagnostic) (*YamlQueryResultMessage, error) {
	if !yamlDomainAcceptsRole(domain, role) {
		return nil, invalid("$", "YAML query domain and result role are inconsistent")
	}
	if completion.Produced() != uint64(len(matches)) {
		return nil, invalid("$", "completion count, role, or YAML match ordinals are inconsistent")
	}
	previous := uint64(0)
	for index, match := range matches {
		if match.role != role {
			return nil, invalid("$", "completion count, role, or YAML match ordinals are inconsistent")
		}
		if index > 0 && match.ordinal <= previous {
			return nil, invalid("$", "completion count, role, or YAML match ordinals are inconsistent")
		}
		previous = match.ordinal
	}
	return &YamlQueryResultMessage{
		domain:      domain,
		role:        role,
		matches:     matches,
		completion:  completion,
		diagnostics: diagnostics,
	}, nil
}

// Domain returns the exact YAML query domain.
func (m *YamlQueryResultMessage) Domain() *QueryDomain { return m.domain }

// Role returns the uniform result role.
func (m *YamlQueryResultMessage) Role() MatchRole { return m.role }

// Matches returns the ordered external match locators.
func (m *YamlQueryResultMessage) Matches() []*YamlMatchLocator { return m.matches }

// Completion returns the explicit terminal state.
func (m *YamlQueryResultMessage) Completion() *Completion { return m.completion }

// Diagnostics returns the ordered diagnostics.
func (m *YamlQueryResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.yaml-query-result@1` (yaml_query.rs...).
func (m *YamlQueryResultMessage) ToValue() (core.Value, error) {
	locators := make([]externalLocator, 0, len(m.matches))
	for _, match := range m.matches {
		locators = append(locators, externalLocator{
			sourceID: match.sourceID, nodeLocator: match.nodeLocator,
			role: match.role, ordinal: match.ordinal,
		})
	}
	return encodeExternalQueryResult("core.yaml-query-result@1", m.domain, m.role,
		locators, m.completion, m.diagnostics)
}

// FromValueWithRegistry strictly decodes terminal facts under one explicit
// semantic-model registry (yaml_query.rs...).
func (m *YamlQueryResultMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*YamlQueryResultMessage, error) {
	fields, err := schemaFields(value, "core.yaml-query-result@1",
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
	matches := make([]*YamlMatchLocator, 0, len(matchValues))
	for index, matchValue := range matchValues {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		locator, err := parseExternalLocator(matchValue, path, isYamlRole)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &YamlMatchLocator{
			sourceID: locator.sourceID, nodeLocator: locator.nodeLocator,
			role: locator.role, ordinal: locator.ordinal,
		})
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[5], registry)
	if err != nil {
		return nil, err
	}
	diagnostics, err := parseDiagnostics(fields[6], registry)
	if err != nil {
		return nil, err
	}
	return NewYamlQueryResultMessage(NewQueryDomain(domainID, domainVersion), role,
		matches, completion, diagnostics)
}

// FromValue strictly decodes under the v1 registry.
func (m *YamlQueryResultMessage) FromValue(value core.Value) (*YamlQueryResultMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// IniMatchLocator is a transferable INI match locator (line_query.rs...).
type IniMatchLocator struct {
	sourceID    string
	nodeLocator string
	role        MatchRole
	ordinal     uint64
}

// NewIniMatchLocator creates a transferable INI locator.
func NewIniMatchLocator(sourceID, nodeLocator string, role MatchRole,
	ordinal uint64) (*IniMatchLocator, error) {
	if sourceID == "" || len(sourceID) > 1024 || nodeLocator == "" || len(nodeLocator) > 4096 ||
		!isIniRole(role) {
		return nil, invalid("$.ini_match", "invalid source, locator, or INI role")
	}
	return &IniMatchLocator{
		sourceID:    sourceID,
		nodeLocator: nodeLocator,
		role:        role,
		ordinal:     ordinal,
	}, nil
}

// IniMatchLocatorFromProcessLocal explicitly refuses a raw process-local
// INI node handle (line_query.rs).
func IniMatchLocatorFromProcessLocal() error {
	return protocolError(KindProcessLocalHandle, "$.ini_match.node",
		"NodeRef requires a stable caller locator")
}

// SourceID returns the stable source identity.
func (l *IniMatchLocator) SourceID() string { return l.sourceID }

// NodeLocator returns the stable caller-defined node locator.
func (l *IniMatchLocator) NodeLocator() string { return l.nodeLocator }

// Role returns the exact INI result role.
func (l *IniMatchLocator) Role() MatchRole { return l.role }

// Ordinal returns the strictly increasing standard-result ordinal.
func (l *IniMatchLocator) Ordinal() uint64 { return l.ordinal }

// IniQueryResultMessage is the complete or explicitly non-complete
// `core.ini-query-result@1` record (line_query.rs...).
type IniQueryResultMessage struct {
	domain      *QueryDomain
	role        MatchRole
	matches     []*IniMatchLocator
	completion  *Completion
	diagnostics []*Diagnostic
}

// NewIniQueryResultMessage validates the exact INI domain/role matrix,
// ordering, and produced count (line_query.rs).
func NewIniQueryResultMessage(domain *QueryDomain, role MatchRole, matches []*IniMatchLocator,
	completion *Completion, diagnostics []*Diagnostic) (*IniQueryResultMessage, error) {
	if err := validateLineResult(domain, role, completion, len(matches), iniDomainAcceptsRole); err != nil {
		return nil, err
	}
	for index, match := range matches {
		if match.role != role {
			return nil, invalid("$", "completion count, role, or INI match ordinals are inconsistent")
		}
		if index > 0 && match.ordinal <= matches[index-1].ordinal {
			return nil, invalid("$", "completion count, role, or INI match ordinals are inconsistent")
		}
	}
	return &IniQueryResultMessage{
		domain:      domain,
		role:        role,
		matches:     matches,
		completion:  completion,
		diagnostics: diagnostics,
	}, nil
}

// Domain returns the exact INI query domain.
func (m *IniQueryResultMessage) Domain() *QueryDomain { return m.domain }

// Role returns the uniform result role.
func (m *IniQueryResultMessage) Role() MatchRole { return m.role }

// Matches returns the ordered external INI match locators.
func (m *IniQueryResultMessage) Matches() []*IniMatchLocator { return m.matches }

// Completion returns the explicit terminal state.
func (m *IniQueryResultMessage) Completion() *Completion { return m.completion }

// Diagnostics returns the ordered diagnostics.
func (m *IniQueryResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.ini-query-result@1` (line_query.rs...).
func (m *IniQueryResultMessage) ToValue() (core.Value, error) {
	locators := make([]externalLocator, 0, len(m.matches))
	for _, match := range m.matches {
		locators = append(locators, externalLocator{
			sourceID: match.sourceID, nodeLocator: match.nodeLocator,
			role: match.role, ordinal: match.ordinal,
		})
	}
	return encodeExternalQueryResult("core.ini-query-result@1", m.domain, m.role,
		locators, m.completion, m.diagnostics)
}

// FromValueWithRegistry strictly decodes terminal facts under one explicit
// semantic-model registry (line_query.rs...).
func (m *IniQueryResultMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*IniQueryResultMessage, error) {
	fields, err := schemaFields(value, "core.ini-query-result@1",
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
	matches := make([]*IniMatchLocator, 0, len(matchValues))
	for index, matchValue := range matchValues {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		locator, err := parseExternalLocator(matchValue, path, isIniRole)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &IniMatchLocator{
			sourceID: locator.sourceID, nodeLocator: locator.nodeLocator,
			role: locator.role, ordinal: locator.ordinal,
		})
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[5], registry)
	if err != nil {
		return nil, err
	}
	diagnostics, err := parseDiagnostics(fields[6], registry)
	if err != nil {
		return nil, err
	}
	return NewIniQueryResultMessage(NewQueryDomain(domainID, domainVersion), role,
		matches, completion, diagnostics)
}

// FromValue strictly decodes under the v1 registry.
func (m *IniQueryResultMessage) FromValue(value core.Value) (*IniQueryResultMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// JavaPropertiesMatchLocator is a transferable Java Properties match
// locator (line_query.rs...).
type JavaPropertiesMatchLocator struct {
	sourceID    string
	nodeLocator string
	role        MatchRole
	ordinal     uint64
}

// NewJavaPropertiesMatchLocator creates a transferable Properties locator.
func NewJavaPropertiesMatchLocator(sourceID, nodeLocator string, role MatchRole,
	ordinal uint64) (*JavaPropertiesMatchLocator, error) {
	if sourceID == "" || len(sourceID) > 1024 || nodeLocator == "" || len(nodeLocator) > 4096 ||
		!isPropertiesRole(role) {
		return nil, invalid("$.properties_match", "invalid source, locator, or Properties role")
	}
	return &JavaPropertiesMatchLocator{
		sourceID:    sourceID,
		nodeLocator: nodeLocator,
		role:        role,
		ordinal:     ordinal,
	}, nil
}

// SourceID returns the stable source identity.
func (l *JavaPropertiesMatchLocator) SourceID() string { return l.sourceID }

// NodeLocator returns the stable caller-defined node locator.
func (l *JavaPropertiesMatchLocator) NodeLocator() string { return l.nodeLocator }

// Role returns the exact Properties result role.
func (l *JavaPropertiesMatchLocator) Role() MatchRole { return l.role }

// Ordinal returns the strictly increasing standard-result ordinal.
func (l *JavaPropertiesMatchLocator) Ordinal() uint64 { return l.ordinal }

// JavaPropertiesQueryResultMessage is the complete or explicitly
// non-complete `core.java-properties-query-result@1` record
// (line_query.rs...).
type JavaPropertiesQueryResultMessage struct {
	domain      *QueryDomain
	role        MatchRole
	matches     []*JavaPropertiesMatchLocator
	completion  *Completion
	diagnostics []*Diagnostic
}

// NewJavaPropertiesQueryResultMessage validates the exact Properties
// domain/role matrix, ordering, and produced count (line_query.rs).
func NewJavaPropertiesQueryResultMessage(domain *QueryDomain, role MatchRole,
	matches []*JavaPropertiesMatchLocator, completion *Completion,
	diagnostics []*Diagnostic) (*JavaPropertiesQueryResultMessage, error) {
	if err := validateLineResult(domain, role, completion, len(matches), propertiesDomainAcceptsRole); err != nil {
		return nil, err
	}
	for index, match := range matches {
		if match.role != role {
			return nil, invalid("$", "completion count, role, or Properties match ordinals are inconsistent")
		}
		if index > 0 && match.ordinal <= matches[index-1].ordinal {
			return nil, invalid("$", "completion count, role, or Properties match ordinals are inconsistent")
		}
	}
	return &JavaPropertiesQueryResultMessage{
		domain:      domain,
		role:        role,
		matches:     matches,
		completion:  completion,
		diagnostics: diagnostics,
	}, nil
}

// Domain returns the exact Properties query domain.
func (m *JavaPropertiesQueryResultMessage) Domain() *QueryDomain { return m.domain }

// Role returns the uniform result role.
func (m *JavaPropertiesQueryResultMessage) Role() MatchRole { return m.role }

// Matches returns the ordered external Properties match locators.
func (m *JavaPropertiesQueryResultMessage) Matches() []*JavaPropertiesMatchLocator { return m.matches }

// Completion returns the explicit terminal state.
func (m *JavaPropertiesQueryResultMessage) Completion() *Completion { return m.completion }

// Diagnostics returns the ordered diagnostics.
func (m *JavaPropertiesQueryResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.java-properties-query-result@1`
// (line_query.rs...).
func (m *JavaPropertiesQueryResultMessage) ToValue() (core.Value, error) {
	locators := make([]externalLocator, 0, len(m.matches))
	for _, match := range m.matches {
		locators = append(locators, externalLocator{
			sourceID: match.sourceID, nodeLocator: match.nodeLocator,
			role: match.role, ordinal: match.ordinal,
		})
	}
	return encodeExternalQueryResult("core.java-properties-query-result@1", m.domain, m.role,
		locators, m.completion, m.diagnostics)
}

// FromValueWithRegistry strictly decodes terminal facts under one explicit
// semantic-model registry (line_query.rs...).
func (m *JavaPropertiesQueryResultMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*JavaPropertiesQueryResultMessage, error) {
	fields, err := schemaFields(value, "core.java-properties-query-result@1",
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
	matches := make([]*JavaPropertiesMatchLocator, 0, len(matchValues))
	for index, matchValue := range matchValues {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		locator, err := parseExternalLocator(matchValue, path, isPropertiesRole)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &JavaPropertiesMatchLocator{
			sourceID: locator.sourceID, nodeLocator: locator.nodeLocator,
			role: locator.role, ordinal: locator.ordinal,
		})
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[5], registry)
	if err != nil {
		return nil, err
	}
	diagnostics, err := parseDiagnostics(fields[6], registry)
	if err != nil {
		return nil, err
	}
	return NewJavaPropertiesQueryResultMessage(NewQueryDomain(domainID, domainVersion), role,
		matches, completion, diagnostics)
}

// FromValue strictly decodes under the v1 registry.
func (m *JavaPropertiesQueryResultMessage) FromValue(value core.Value) (*JavaPropertiesQueryResultMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// ---------------------------------------------------------------------------
// Shared external-locator helpers
// ---------------------------------------------------------------------------

// externalLocator is the shared match-locator record
// {source_id, node_locator, role, ordinal}.
type externalLocator struct {
	sourceID    string
	nodeLocator string
	role        MatchRole
	ordinal     uint64
}

func encodeExternalQueryResult(schema string, domain *QueryDomain, role MatchRole,
	matches []externalLocator, completion *Completion, diagnostics []*Diagnostic) (core.Value, error) {
	matchValues := make([]core.Value, 0, len(matches))
	for _, locator := range matches {
		value, err := core.NewObject(
			core.Entry{Key: "source_id", Value: core.String(locator.sourceID)},
			core.Entry{Key: "node_locator", Value: core.String(locator.nodeLocator)},
			core.Entry{Key: "role", Value: core.String(string(locator.role))},
			core.Entry{Key: "ordinal", Value: integerValue(locator.ordinal)},
		)
		if err != nil {
			return nil, err
		}
		matchValues = append(matchValues, value)
	}
	diagnosticValues := make([]core.Value, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		value, err := diagnostic.ToValue()
		if err != nil {
			return nil, err
		}
		diagnosticValues = append(diagnosticValues, value)
	}
	completionValue, err := completion.ToValue()
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String(schema)},
		core.Entry{Key: "domain_id", Value: core.String(domain.id)},
		core.Entry{Key: "domain_version", Value: integerValue(uint64(domain.version))},
		core.Entry{Key: "role", Value: core.String(string(role))},
		core.Entry{Key: "matches", Value: core.NewArray(matchValues...)},
		core.Entry{Key: "completion", Value: completionValue},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnosticValues...)},
	)
}

// parseExternalLocator decodes one {source_id, node_locator, role, ordinal}
// match record.
func parseExternalLocator(value core.Value, path string,
	acceptRole func(MatchRole) bool) (*externalLocator, error) {
	fields, err := exactFields(value, []string{"source_id", "node_locator", "role", "ordinal"}, path)
	if err != nil {
		return nil, err
	}
	sourceID, err := stringOf(fields[0], path+".source_id")
	if err != nil {
		return nil, err
	}
	nodeLocator, err := stringOf(fields[1], path+".node_locator")
	if err != nil {
		return nil, err
	}
	roleText, err := stringOf(fields[2], path+".role")
	if err != nil {
		return nil, err
	}
	role, ok := ParseMatchRole(roleText)
	if !ok {
		return nil, invalid(path+".role", "unknown match role")
	}
	ordinal, err := unsigned64(fields[3], path+".ordinal")
	if err != nil {
		return nil, err
	}
	return &externalLocator{sourceID: sourceID, nodeLocator: nodeLocator, role: role, ordinal: ordinal}, nil
}

// validateLineResult checks the shared domain/role matrix, produced count,
// and ordinal order (line_query.rs:...).
func validateLineResult(domain *QueryDomain, role MatchRole, completion *Completion,
	matchCount int, acceptRole func(*QueryDomain, MatchRole) bool) error {
	if !acceptRole(domain, role) {
		return invalid("$", "line query domain and result role are inconsistent")
	}
	if completion.Produced() != uint64(matchCount) {
		return invalid("$", "completion count, role, or match ordinals are inconsistent")
	}
	return nil
}

func parseDiagnostics(value core.Value, registry ErrorCodeRegistry) ([]*Diagnostic, error) {
	diagnosticValues, err := sequenceOf(value, "$.diagnostics")
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
	return diagnostics, nil
}

func isYamlRole(role MatchRole) bool {
	switch role {
	case RoleYamlStream, RoleYamlDocument, RoleYamlNode, RoleYamlMappingEntry,
		RoleYamlSequenceElement, RoleYamlAnchorDefinition, RoleYamlAliasOccurrence,
		RoleYamlSyntaxPiece:
		return true
	}
	return false
}

func yamlDomainAcceptsRole(domain *QueryDomain, role MatchRole) bool {
	switch domain.id + "@" + uint32String(domain.version) {
	case "yaml.native-semantic-query@1":
		return isYamlRole(role) && role != RoleYamlSyntaxPiece
	case "yaml.lossless-syntax-query@1":
		return role == RoleYamlSyntaxPiece
	}
	return false
}

func isIniRole(role MatchRole) bool {
	switch role {
	case RoleIniDocument, RoleIniSection, RoleIniDefaultSection, RoleIniEntry,
		RoleIniPhysicalLine, RoleIniLogicalLine, RoleIniErrorLine, RoleIniSyntaxPiece:
		return true
	}
	return false
}

func iniDomainAcceptsRole(domain *QueryDomain, role MatchRole) bool {
	switch domain.id + "@" + uint32String(domain.version) {
	case "ini.native-semantic-query@1":
		return isIniRole(role) && role != RoleIniSyntaxPiece
	case "ini.lossless-syntax-query@1":
		return role == RoleIniSyntaxPiece
	}
	return false
}

func isPropertiesRole(role MatchRole) bool {
	switch role {
	case RolePropertiesDocument, RolePropertiesNaturalLine, RolePropertiesLogicalLine,
		RolePropertiesProperty, RolePropertiesComment, RolePropertiesEscape,
		RolePropertiesErrorLine, RolePropertiesSyntaxPiece:
		return true
	}
	return false
}

func propertiesDomainAcceptsRole(domain *QueryDomain, role MatchRole) bool {
	switch domain.id + "@" + uint32String(domain.version) {
	case "java-properties.native-semantic-query@1":
		return isPropertiesRole(role) && role != RolePropertiesSyntaxPiece
	case "java-properties.lossless-syntax-query@1":
		return role == RolePropertiesSyntaxPiece
	}
	return false
}
