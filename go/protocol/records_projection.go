package protocol

// Projection request, report, provenance, and result records
// (consema-rs/consema-protocol/src/projection.rs): `core.projection-request@1`,
// `core.projection-report@1`, `core.provenance-map@1`, and
// `core.projection-result@1`.

import (
	"sort"

	"consema.dev/consema/core"
)

// ProjectionPolicy is a versioned policy contract with deterministic
// arguments (projection.rs).
type ProjectionPolicy struct {
	contract  *ContractId
	arguments map[string]core.Value
}

// NewProjectionPolicy creates one policy contract.
func NewProjectionPolicy(contract *ContractId, arguments map[string]core.Value) *ProjectionPolicy {
	return &ProjectionPolicy{contract: contract, arguments: arguments}
}

// Contract returns the policy identifier and version.
func (p *ProjectionPolicy) Contract() *ContractId { return p.contract }

// Arguments returns the deterministic arguments.
func (p *ProjectionPolicy) Arguments() map[string]core.Value {
	output := make(map[string]core.Value, len(p.arguments))
	for name, value := range p.arguments {
		output[name] = value
	}
	return output
}

// Equal reports whether two policies are identical.
func (p *ProjectionPolicy) Equal(other *ProjectionPolicy) bool {
	if p == nil || other == nil {
		return p == other
	}
	if !p.contract.Equal(other.contract) || len(p.arguments) != len(other.arguments) {
		return false
	}
	for name, value := range p.arguments {
		otherValue, ok := other.arguments[name]
		if !ok || !core.Equal(value, otherValue) {
			return false
		}
	}
	return true
}

// ProjectionScope is the transferable projection rule scope
// (projection.rs).
type ProjectionScope struct {
	// Kind is "Global", "ExactNativePath", or "ResolvedQuery".
	Kind string
	// SourceID is the stable source ID of ExactNativePath scopes.
	SourceID string
	// Path is the format-native path contract string of ExactNativePath
	// scopes.
	Path string
	// Query is the complete query definition of ResolvedQuery scopes.
	Query *QueryDefinition
}

// ProjectionRule is one auditable scoped projection policy rule
// (projection.rs).
type ProjectionRule struct {
	// RuleID is the stable request-local rule ID.
	RuleID string
	// Scope is the transferable scope.
	Scope ProjectionScope
	// Priority is the explicit semantic priority.
	Priority int32
	// Policy is the policy contract.
	Policy ProjectionPolicy
}

// ProjectionRequestMessage is the `core.projection-request@1` record
// (projection.rs).
type ProjectionRequestMessage struct {
	target        *ContractId
	defaultPolicy ProjectionPolicy
	rules         []ProjectionRule
	limits        map[string]uint64
}

// NewProjectionRequestMessage validates rule IDs, portable scopes, and
// semantic conflicts (projection.rs).
func NewProjectionRequestMessage(target *ContractId, defaultPolicy ProjectionPolicy,
	rules []ProjectionRule, limits map[string]uint64) (*ProjectionRequestMessage, error) {
	ruleIDs := make(map[string]bool, len(rules))
	for _, rule := range rules {
		if rule.RuleID == "" || len(rule.RuleID) > 255 {
			return nil, invalid("$.rules", "rule IDs must be non-empty and unique")
		}
		if ruleIDs[rule.RuleID] {
			return nil, invalid("$.rules", "rule IDs must be non-empty and unique")
		}
		ruleIDs[rule.RuleID] = true
		if err := validateProjectionScope(rule.Scope); err != nil {
			return nil, err
		}
	}
	for index, left := range rules {
		for _, right := range rules[index+1:] {
			if left.Priority == right.Priority && scopeEqual(left.Scope, right.Scope) &&
				!left.Policy.Equal(&right.Policy) {
				return nil, invalid("$.rules", "same-scope same-priority policies conflict")
			}
		}
	}
	for name := range limits {
		if !validLimitName(name) {
			return nil, invalid("$.limits", "limit names must be stable lowercase identifiers")
		}
	}
	return &ProjectionRequestMessage{
		target:        target,
		defaultPolicy: defaultPolicy,
		rules:         rules,
		limits:        limits,
	}, nil
}

// Target returns the target contract.
func (m *ProjectionRequestMessage) Target() *ContractId { return m.target }

// DefaultPolicy returns the default policy.
func (m *ProjectionRequestMessage) DefaultPolicy() ProjectionPolicy { return m.defaultPolicy }

// Rules returns the auditable rule declaration order.
func (m *ProjectionRequestMessage) Rules() []ProjectionRule { return m.rules }

// Limits returns the named operation limits.
func (m *ProjectionRequestMessage) Limits() map[string]uint64 {
	output := make(map[string]uint64, len(m.limits))
	for name, value := range m.limits {
		output[name] = value
	}
	return output
}

// ToValue encodes `core.projection-request@1` (projection.rs).
func (m *ProjectionRequestMessage) ToValue() (core.Value, error) {
	rules := make([]core.Value, 0, len(m.rules))
	for _, rule := range m.rules {
		value, err := projectionRuleValue(rule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, value)
	}
	defaultPolicy, err := projectionPolicyValue(m.defaultPolicy)
	if err != nil {
		return nil, err
	}
	limits, err := sortedUint64Object(m.limits)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.projection-request@1")},
		core.Entry{Key: "target", Value: referenceValue(m.target.ID(), m.target.Version())},
		core.Entry{Key: "default_policy", Value: defaultPolicy},
		core.Entry{Key: "rules", Value: core.NewArray(rules...)},
		core.Entry{Key: "limits", Value: limits},
	)
}

// FromValue strictly decodes `core.projection-request@1`
// (projection.rs).
func (m *ProjectionRequestMessage) FromValue(value core.Value) (*ProjectionRequestMessage, error) {
	fields, err := schemaFields(value, "core.projection-request@1",
		[]string{"schema", "target", "default_policy", "rules", "limits"}, "$")
	if err != nil {
		return nil, err
	}
	target, err := parseReference(fields[1], "$.target")
	if err != nil {
		return nil, err
	}
	defaultPolicy, err := parseProjectionPolicy(fields[2], "$.default_policy")
	if err != nil {
		return nil, err
	}
	ruleValues, err := sequenceOf(fields[3], "$.rules")
	if err != nil {
		return nil, err
	}
	rules := make([]ProjectionRule, 0, len(ruleValues))
	for index, ruleValue := range ruleValues {
		path := "$.rules[" + uint32String(uint32(index)) + "]"
		rule, err := parseProjectionRule(ruleValue, path)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	limits, err := parseUint64Object(fields[4], "$.limits")
	if err != nil {
		return nil, err
	}
	return NewProjectionRequestMessage(target, defaultPolicy, rules, limits)
}

// ProjectedLocationMessage is a projected value or association location
// (projection.rs).
type ProjectedLocationMessage struct {
	// Kind is "ValuePath" or "AssociationLocation".
	Kind string
	// Path is the portable value path of ValuePath locations.
	Path ValuePath
	// Association is the association location of AssociationLocation
	// locations.
	Association AssociationLocation
}

// Less reports whether the location precedes other in the canonical wire
// order.
func (l ProjectedLocationMessage) Less(other ProjectedLocationMessage) bool {
	if l.Kind != other.Kind {
		return l.Kind < other.Kind
	}
	if l.Kind == "AssociationLocation" {
		return l.Association.Less(other.Association)
	}
	return l.Path.Less(other.Path)
}

// Equal reports whether two projected locations are identical.
func (l ProjectedLocationMessage) Equal(other ProjectedLocationMessage) bool {
	if l.Kind != other.Kind {
		return false
	}
	if l.Kind == "AssociationLocation" {
		return l.Association.Equal(other.Association)
	}
	return l.Path.Equal(other.Path)
}

// ProvenanceRelation is the provenance relationship from a source fact to a
// projected fact (projection.rs).
type ProvenanceRelation string

// The five frozen relations.
const (
	RelationDirect    ProvenanceRelation = "Direct"
	RelationDerived   ProvenanceRelation = "Derived"
	RelationExpanded  ProvenanceRelation = "Expanded"
	RelationMerged    ProvenanceRelation = "Merged"
	RelationGenerated ProvenanceRelation = "Generated"
)

// SourceOriginMessage is a transferable source origin with stable external
// identities (projection.rs).
type SourceOriginMessage struct {
	// SourceID is the stable source identity.
	SourceID string
	// NodeLocator is the optional stable caller node locator.
	NodeLocator *string
	// StartByte is the inclusive source byte start.
	StartByte uint64
	// EndByte is the exclusive source byte end.
	EndByte uint64
	// Relation is the provenance relation.
	Relation ProvenanceRelation
}

// NewSourceOriginMessage validates a transferable source origin
// (projection.rs).
func NewSourceOriginMessage(sourceID string, nodeLocator *string, startByte, endByte uint64,
	relation ProvenanceRelation) (*SourceOriginMessage, error) {
	if sourceID == "" || len(sourceID) > 1024 || startByte > endByte ||
		(nodeLocator != nil && (*nodeLocator == "" || len(*nodeLocator) > 4096)) {
		return nil, invalid("$.origin", "invalid source identity, locator, or range")
	}
	return &SourceOriginMessage{
		SourceID:    sourceID,
		NodeLocator: nodeLocator,
		StartByte:   startByte,
		EndByte:     endByte,
		Relation:    relation,
	}, nil
}

// ProvenanceEntryMessage is one projected location and all of its source
// origins (projection.rs).
type ProvenanceEntryMessage struct {
	// Projected is the projected location.
	Projected ProjectedLocationMessage
	// Origins are the one or more ordered origins.
	Origins []SourceOriginMessage
}

// ProvenanceMapMessage is the sorted unique `core.provenance-map@1` record
// (projection.rs).
type ProvenanceMapMessage struct {
	entries []ProvenanceEntryMessage
}

// NewProvenanceMapMessage validates sorted unique projected locations and
// non-empty origins (projection.rs).
func NewProvenanceMapMessage(entries []ProvenanceEntryMessage) (*ProvenanceMapMessage, error) {
	for _, entry := range entries {
		if len(entry.Origins) == 0 {
			return nil, invalid("$.entries", "provenance locations must be sorted, unique, and have origins")
		}
	}
	for index := 1; index < len(entries); index++ {
		if !entries[index-1].Projected.Less(entries[index].Projected) {
			return nil, invalid("$.entries", "provenance locations must be sorted, unique, and have origins")
		}
	}
	return &ProvenanceMapMessage{entries: entries}, nil
}

// Entries returns the sorted provenance entries.
func (m *ProvenanceMapMessage) Entries() []ProvenanceEntryMessage { return m.entries }

// ToValue encodes `core.provenance-map@1` (projection.rs).
func (m *ProvenanceMapMessage) ToValue() (core.Value, error) {
	entries := make([]core.Value, 0, len(m.entries))
	for _, entry := range m.entries {
		projected, err := projectedLocationValue(entry.Projected)
		if err != nil {
			return nil, err
		}
		origins := make([]core.Value, 0, len(entry.Origins))
		for _, origin := range entry.Origins {
			value, err := sourceOriginValue(origin)
			if err != nil {
				return nil, err
			}
			origins = append(origins, value)
		}
		value, err := core.NewObject(
			core.Entry{Key: "projected", Value: projected},
			core.Entry{Key: "origins", Value: core.NewArray(origins...)},
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, value)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.provenance-map@1")},
		core.Entry{Key: "entries", Value: core.NewArray(entries...)},
	)
}

// FromValue strictly decodes `core.provenance-map@1`
// (projection.rs).
func (m *ProvenanceMapMessage) FromValue(value core.Value) (*ProvenanceMapMessage, error) {
	fields, err := schemaFields(value, "core.provenance-map@1", []string{"schema", "entries"}, "$")
	if err != nil {
		return nil, err
	}
	entryValues, err := sequenceOf(fields[1], "$.entries")
	if err != nil {
		return nil, err
	}
	entries := make([]ProvenanceEntryMessage, 0, len(entryValues))
	for index, entryValue := range entryValues {
		path := "$.entries[" + uint32String(uint32(index)) + "]"
		entryFields, err := exactFields(entryValue, []string{"projected", "origins"}, path)
		if err != nil {
			return nil, err
		}
		projected, err := parseProjectedLocation(entryFields[0], path+".projected")
		if err != nil {
			return nil, err
		}
		originValues, err := sequenceOf(entryFields[1], path+".origins")
		if err != nil {
			return nil, err
		}
		origins := make([]SourceOriginMessage, 0, len(originValues))
		for originIndex, originValue := range originValues {
			originPath := path + ".origins[" + uint32String(uint32(originIndex)) + "]"
			origin, err := parseSourceOrigin(originValue, originPath)
			if err != nil {
				return nil, err
			}
			origins = append(origins, *origin)
		}
		entries = append(entries, ProvenanceEntryMessage{Projected: projected, Origins: origins})
	}
	return NewProvenanceMapMessage(entries)
}

// ProjectionFidelity is the projection fidelity classification
// (projection.rs).
type ProjectionFidelity string

// The three frozen fidelities.
const (
	FidelityExact       ProjectionFidelity = "Exact"
	FidelityTransformed ProjectionFidelity = "Transformed"
	FidelityLossy       ProjectionFidelity = "Lossy"
)

// LossClassification is the event loss classification independent from
// reversibility (projection.rs).
type LossClassification string

// The three frozen loss classifications.
const (
	LossNone       LossClassification = "None"
	LossReversible LossClassification = "Reversible"
	LossLossy      LossClassification = "Lossy"
)

// ProjectionEventMessage is one machine-readable projection report event
// (projection.rs).
type ProjectionEventMessage struct {
	// Code is the stable event code.
	Code string
	// PolicyRuleID is the rule authorizing the event.
	PolicyRuleID *string
	// SourceLocations are the source ranges associated with the event.
	SourceLocations []SourceLocation
	// ProjectedLocation is the optional projected location.
	ProjectedLocation *ProjectedLocationMessage
	// OldCategory is the old semantic category.
	OldCategory *string
	// NewCategory is the new semantic category.
	NewCategory *string
	// Reversible reports whether the transform can be reversed from result
	// plus report.
	Reversible bool
	// LossClassification is the loss classification.
	LossClassification LossClassification
	// Arguments are the stable sorted event arguments.
	Arguments map[string]string
}

// ProjectionReportMessage is the ordered `core.projection-report@1` record
// (projection.rs).
type ProjectionReportMessage struct {
	events []ProjectionEventMessage
}

// NewProjectionReportMessage validates event cross-field invariants against
// the v1 error registry (projection.rs).
func NewProjectionReportMessage(events []ProjectionEventMessage) (*ProjectionReportMessage, error) {
	return NewProjectionReportMessageWithRegistry(events, DefaultErrorCodeRegistry())
}

// NewProjectionReportMessageWithRegistry validates events under one explicit
// semantic-model error registry (projection.rs).
func NewProjectionReportMessageWithRegistry(events []ProjectionEventMessage,
	registry ErrorCodeRegistry) (*ProjectionReportMessage, error) {
	for _, event := range events {
		if err := registry.validateAt(event.Code, "$.events.code"); err != nil {
			return nil, err
		}
	}
	for _, event := range events {
		if event.Code == "" ||
			(event.LossClassification == LossLossy && event.Reversible) ||
			(event.LossClassification == LossReversible && !event.Reversible) {
			return nil, invalid("$.events", "projection event fields are contradictory")
		}
	}
	return &ProjectionReportMessage{events: events}, nil
}

// Events returns the ordered events.
func (m *ProjectionReportMessage) Events() []ProjectionEventMessage { return m.events }

// ToValue encodes `core.projection-report@1` (projection.rs).
func (m *ProjectionReportMessage) ToValue() (core.Value, error) {
	events := make([]core.Value, 0, len(m.events))
	for _, event := range m.events {
		value, err := projectionEventValue(event)
		if err != nil {
			return nil, err
		}
		events = append(events, value)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.projection-report@1")},
		core.Entry{Key: "events", Value: core.NewArray(events...)},
	)
}

// FromValue strictly decodes `core.projection-report@1` under the v1
// registry (projection.rs).
func (m *ProjectionReportMessage) FromValue(value core.Value) (*ProjectionReportMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// FromValueWithRegistry strictly decodes the report under one explicit
// semantic-model registry (projection.rs).
func (m *ProjectionReportMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*ProjectionReportMessage, error) {
	fields, err := schemaFields(value, "core.projection-report@1", []string{"schema", "events"}, "$")
	if err != nil {
		return nil, err
	}
	eventValues, err := sequenceOf(fields[1], "$.events")
	if err != nil {
		return nil, err
	}
	events := make([]ProjectionEventMessage, 0, len(eventValues))
	for index, eventValue := range eventValues {
		path := "$.events[" + uint32String(uint32(index)) + "]"
		event, err := parseProjectionEvent(eventValue, path)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return NewProjectionReportMessageWithRegistry(events, registry)
}

// ProjectionResultMessage is the complete or explicitly failed
// `core.projection-result@1` record (projection.rs).
type ProjectionResultMessage struct {
	completion  *Completion
	value       core.Value
	hasValue    bool
	fidelity    *ProjectionFidelity
	report      *ProjectionReportMessage
	provenance  *ProvenanceMapMessage
	diagnostics []*Diagnostic
}

// NewProjectionResultMessage validates success/value/fidelity and
// loss-report invariants (projection.rs).
func NewProjectionResultMessage(completion *Completion, value core.Value, hasValue bool,
	fidelity *ProjectionFidelity, report *ProjectionReportMessage, provenance *ProvenanceMapMessage,
	diagnostics []*Diagnostic) (*ProjectionResultMessage, error) {
	success := completion.Status() == CompletionSuccess
	if success != hasValue || (success && fidelity == nil) || (!success && fidelity != nil) {
		return nil, invalid("$", "only successful projection may carry value and fidelity")
	}
	if fidelity != nil && *fidelity == FidelityLossy {
		found := false
		for _, event := range report.events {
			if event.LossClassification == LossLossy {
				found = true
				break
			}
		}
		if !found {
			return nil, invalid("$.report", "Lossy fidelity requires an explicit lossy event")
		}
	}
	if !success && len(provenance.entries) != 0 {
		return nil, invalid("$.provenance", "failed projection cannot claim completed provenance")
	}
	return &ProjectionResultMessage{
		completion:  completion,
		value:       value,
		hasValue:    hasValue,
		fidelity:    fidelity,
		report:      report,
		provenance:  provenance,
		diagnostics: diagnostics,
	}, nil
}

// Completion returns the completion state.
func (m *ProjectionResultMessage) Completion() *Completion { return m.completion }

// Value returns the complete projected value only on success.
func (m *ProjectionResultMessage) Value() (core.Value, bool) { return m.value, m.hasValue }

// Fidelity returns the fidelity only on success.
func (m *ProjectionResultMessage) Fidelity() *ProjectionFidelity { return m.fidelity }

// Report returns the projection report.
func (m *ProjectionResultMessage) Report() *ProjectionReportMessage { return m.report }

// Provenance returns the complete provenance only on success.
func (m *ProjectionResultMessage) Provenance() *ProvenanceMapMessage { return m.provenance }

// Diagnostics returns the ordered diagnostics.
func (m *ProjectionResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.projection-result@1` (projection.rs).
func (m *ProjectionResultMessage) ToValue() (core.Value, error) {
	var value core.Value = core.NullValue()
	if m.hasValue {
		value, _ = core.NewObject(core.Entry{Key: "portable_value", Value: m.value})
	}
	var fidelity core.Value = core.NullValue()
	if m.fidelity != nil {
		fidelity = core.String(string(*m.fidelity))
	}
	report, err := m.report.ToValue()
	if err != nil {
		return nil, err
	}
	provenance, err := m.provenance.ToValue()
	if err != nil {
		return nil, err
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
		core.Entry{Key: "schema", Value: core.String("core.projection-result@1")},
		core.Entry{Key: "completion", Value: completion},
		core.Entry{Key: "value", Value: value},
		core.Entry{Key: "fidelity", Value: fidelity},
		core.Entry{Key: "report", Value: report},
		core.Entry{Key: "provenance", Value: provenance},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
	)
}

// FromValueWithRegistry strictly decodes terminal facts under one explicit
// semantic-model registry (projection.rs).
func (m *ProjectionResultMessage) FromValueWithRegistry(value core.Value,
	registry ErrorCodeRegistry) (*ProjectionResultMessage, error) {
	fields, err := schemaFields(value, "core.projection-result@1",
		[]string{"schema", "completion", "value", "fidelity", "report", "provenance", "diagnostics"}, "$")
	if err != nil {
		return nil, err
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[1], registry)
	if err != nil {
		return nil, err
	}
	var projected core.Value
	hasValue := false
	if _, isNull := fields[2].(core.Null); !isNull {
		valueFields, err := exactFields(fields[2], []string{"portable_value"}, "$.value")
		if err != nil {
			return nil, err
		}
		projected = valueFields[0]
		hasValue = true
	}
	var fidelity *ProjectionFidelity
	if _, isNull := fields[3].(core.Null); !isNull {
		text, err := stringOf(fields[3], "$.fidelity")
		if err != nil {
			return nil, err
		}
		switch ProjectionFidelity(text) {
		case FidelityExact, FidelityTransformed, FidelityLossy:
			value := ProjectionFidelity(text)
			fidelity = &value
		default:
			return nil, invalid("$.fidelity", "unknown projection fidelity")
		}
	}
	report := &ProjectionReportMessage{}
	report, err = report.FromValueWithRegistry(fields[4], registry)
	if err != nil {
		return nil, err
	}
	provenance := &ProvenanceMapMessage{}
	provenance, err = provenance.FromValue(fields[5])
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
	return NewProjectionResultMessage(completion, projected, hasValue, fidelity, report, provenance, diagnostics)
}

// FromValue strictly decodes `core.projection-result@1` under the v1
// registry.
func (m *ProjectionResultMessage) FromValue(value core.Value) (*ProjectionResultMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// ---------------------------------------------------------------------------
// Value helpers
// ---------------------------------------------------------------------------

// parseReference strictly decodes one id/version contract reference
// (projection.rs).
func parseReference(value core.Value, path string) (*ContractId, error) {
	fields, err := exactFields(value, []string{"id", "version"}, path)
	if err != nil {
		return nil, err
	}
	id, err := stringOf(fields[0], path+".id")
	if err != nil {
		return nil, err
	}
	version, err := unsigned32(fields[1], path+".version")
	if err != nil {
		return nil, err
	}
	return NewContractId(id, version)
}

func projectionPolicyValue(policy ProjectionPolicy) (core.Value, error) {
	arguments, err := sortedValueObject(policy.arguments)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "id", Value: core.String(policy.contract.ID())},
		core.Entry{Key: "version", Value: integerValue(uint64(policy.contract.Version()))},
		core.Entry{Key: "arguments", Value: arguments},
	)
}

func parseProjectionPolicy(value core.Value, path string) (ProjectionPolicy, error) {
	fields, err := exactFields(value, []string{"id", "version", "arguments"}, path)
	if err != nil {
		return ProjectionPolicy{}, err
	}
	id, err := stringOf(fields[0], path+".id")
	if err != nil {
		return ProjectionPolicy{}, err
	}
	version, err := unsigned32(fields[1], path+".version")
	if err != nil {
		return ProjectionPolicy{}, err
	}
	object, ok := fields[2].(*core.Object)
	if !ok {
		return ProjectionPolicy{}, protocolError(KindWrongType, path+".arguments", "expected Object")
	}
	arguments := make(map[string]core.Value, len(object.Entries()))
	for _, entry := range object.Entries() {
		arguments[entry.Key] = entry.Value
	}
	contract, err := NewContractId(id, version)
	if err != nil {
		return ProjectionPolicy{}, err
	}
	return ProjectionPolicy{contract: contract, arguments: arguments}, nil
}

func validateProjectionScope(scope ProjectionScope) error {
	switch scope.Kind {
	case "Global":
		return nil
	case "ExactNativePath":
		if scope.SourceID == "" || len(scope.SourceID) > 1024 ||
			scope.Path == "" || len(scope.Path) > 4096 {
			return invalid("$.scope", "invalid exact native path scope")
		}
		return nil
	case "ResolvedQuery":
		if scope.Query == nil {
			return invalid("$.scope.query", "invalid query scope")
		}
		_, failure := scope.Query.Validate()
		if failure != nil {
			return invalid("$.scope.query", "invalid query scope: "+failure.Error())
		}
		return nil
	}
	return invalid("$.scope", "unknown projection scope")
}

func scopeEqual(left, right ProjectionScope) bool {
	if left.Kind != right.Kind {
		return false
	}
	switch left.Kind {
	case "Global":
		return true
	case "ExactNativePath":
		return left.SourceID == right.SourceID && left.Path == right.Path
	case "ResolvedQuery":
		if left.Query == nil || right.Query == nil {
			return left.Query == right.Query
		}
		leftValue, leftErr := left.Query.ToProtocolValue()
		rightValue, rightErr := right.Query.ToProtocolValue()
		return leftErr == nil && rightErr == nil && core.Equal(leftValue, rightValue)
	}
	return false
}

func projectionRuleValue(rule ProjectionRule) (core.Value, error) {
	scope, err := projectionScopeValue(rule.Scope)
	if err != nil {
		return nil, err
	}
	policy, err := projectionPolicyValue(rule.Policy)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "rule_id", Value: core.String(rule.RuleID)},
		core.Entry{Key: "scope", Value: scope},
		core.Entry{Key: "priority", Value: core.NewInteger(bigInt32(rule.Priority))},
		core.Entry{Key: "policy", Value: policy},
	)
}

func parseProjectionRule(value core.Value, path string) (ProjectionRule, error) {
	fields, err := exactFields(value, []string{"rule_id", "scope", "priority", "policy"}, path)
	if err != nil {
		return ProjectionRule{}, err
	}
	ruleID, err := stringOf(fields[0], path+".rule_id")
	if err != nil {
		return ProjectionRule{}, err
	}
	scope, err := parseProjectionScope(fields[1], path+".scope")
	if err != nil {
		return ProjectionRule{}, err
	}
	priority, err := signed32(fields[2], path+".priority")
	if err != nil {
		return ProjectionRule{}, err
	}
	policy, err := parseProjectionPolicy(fields[3], path+".policy")
	if err != nil {
		return ProjectionRule{}, err
	}
	return ProjectionRule{RuleID: ruleID, Scope: scope, Priority: priority, Policy: policy}, nil
}

func projectionScopeValue(scope ProjectionScope) (core.Value, error) {
	switch scope.Kind {
	case "Global":
		return core.NewObject(core.Entry{Key: "kind", Value: core.String("Global")})
	case "ExactNativePath":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("ExactNativePath")},
			core.Entry{Key: "source_id", Value: core.String(scope.SourceID)},
			core.Entry{Key: "path", Value: core.String(scope.Path)},
		)
	case "ResolvedQuery":
		query, failure := scope.Query.ToProtocolValue()
		if failure != nil {
			return nil, invalid("$.scope.query", "invalid query definition")
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("ResolvedQuery")},
			core.Entry{Key: "query", Value: query},
		)
	}
	return nil, invalid("$.scope", "unknown projection scope")
}

func parseProjectionScope(value core.Value, path string) (ProjectionScope, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return ProjectionScope{}, protocolError(KindWrongType, path, "expected scope Object")
	}
	entries := object.Entries()
	if len(entries) == 0 || entries[0].Key != "kind" {
		return ProjectionScope{}, invalid(path, "scope kind must be first")
	}
	kind, err := stringOf(entries[0].Value, path+".kind")
	if err != nil {
		return ProjectionScope{}, err
	}
	switch kind {
	case "Global":
		if _, err := exactFields(value, []string{"kind"}, path); err != nil {
			return ProjectionScope{}, err
		}
		return ProjectionScope{Kind: "Global"}, nil
	case "ExactNativePath":
		fields, err := exactFields(value, []string{"kind", "source_id", "path"}, path)
		if err != nil {
			return ProjectionScope{}, err
		}
		sourceID, err := stringOf(fields[1], path+".source_id")
		if err != nil {
			return ProjectionScope{}, err
		}
		scopePath, err := stringOf(fields[2], path+".path")
		if err != nil {
			return ProjectionScope{}, err
		}
		return ProjectionScope{Kind: "ExactNativePath", SourceID: sourceID, Path: scopePath}, nil
	case "ResolvedQuery":
		fields, err := exactFields(value, []string{"kind", "query"}, path)
		if err != nil {
			return ProjectionScope{}, err
		}
		definition := &QueryDefinition{}
		decoded, failure := definition.FromProtocolValue(fields[1])
		if failure != nil {
			return ProjectionScope{}, invalid(path+".query", "invalid query definition: "+failure.Error())
		}
		return ProjectionScope{Kind: "ResolvedQuery", Query: decoded}, nil
	}
	return ProjectionScope{}, invalid(path, "unknown projection scope")
}

func projectedLocationValue(location ProjectedLocationMessage) (core.Value, error) {
	switch location.Kind {
	case "ValuePath":
		value, err := pathValue(location.Path)
		if err != nil {
			return nil, err
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("ValuePath")},
			core.Entry{Key: "value", Value: value},
		)
	case "AssociationLocation":
		value, err := associationValue(location.Association)
		if err != nil {
			return nil, err
		}
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("AssociationLocation")},
			core.Entry{Key: "value", Value: value},
		)
	}
	return nil, invalid("$", "unknown projected location")
}

func parseProjectedLocation(value core.Value, path string) (ProjectedLocationMessage, error) {
	fields, err := exactFields(value, []string{"kind", "value"}, path)
	if err != nil {
		return ProjectedLocationMessage{}, err
	}
	kind, err := stringOf(fields[0], path+".kind")
	if err != nil {
		return ProjectedLocationMessage{}, err
	}
	switch kind {
	case "ValuePath":
		decoded, err := parsePath(fields[1], path+".value")
		if err != nil {
			return ProjectedLocationMessage{}, err
		}
		return ProjectedLocationMessage{Kind: "ValuePath", Path: decoded}, nil
	case "AssociationLocation":
		decoded, err := parseAssociation(fields[1], path+".value")
		if err != nil {
			return ProjectedLocationMessage{}, err
		}
		return ProjectedLocationMessage{Kind: "AssociationLocation", Association: decoded}, nil
	}
	return ProjectedLocationMessage{}, invalid(path, "unknown projected location")
}

func sourceOriginValue(origin SourceOriginMessage) (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "source_id", Value: core.String(origin.SourceID)},
		core.Entry{Key: "node_locator", Value: nullableString(origin.NodeLocator)},
		core.Entry{Key: "start_byte", Value: integerValue(origin.StartByte)},
		core.Entry{Key: "end_byte", Value: integerValue(origin.EndByte)},
		core.Entry{Key: "relation", Value: core.String(string(origin.Relation))},
	)
}

func parseSourceOrigin(value core.Value, path string) (*SourceOriginMessage, error) {
	fields, err := exactFields(value,
		[]string{"source_id", "node_locator", "start_byte", "end_byte", "relation"}, path)
	if err != nil {
		return nil, err
	}
	sourceID, err := stringOf(fields[0], path+".source_id")
	if err != nil {
		return nil, err
	}
	nodeLocator, err := optionalString(fields[1], path+".node_locator")
	if err != nil {
		return nil, err
	}
	startByte, err := unsigned64(fields[2], path+".start_byte")
	if err != nil {
		return nil, err
	}
	endByte, err := unsigned64(fields[3], path+".end_byte")
	if err != nil {
		return nil, err
	}
	relationText, err := stringOf(fields[4], path+".relation")
	if err != nil {
		return nil, err
	}
	switch ProvenanceRelation(relationText) {
	case RelationDirect, RelationDerived, RelationExpanded, RelationMerged, RelationGenerated:
	default:
		return nil, invalid(path+".relation", "unknown provenance relation")
	}
	return NewSourceOriginMessage(sourceID, nodeLocator, startByte, endByte, ProvenanceRelation(relationText))
}

func projectionEventValue(event ProjectionEventMessage) (core.Value, error) {
	sourceLocations := make([]core.Value, 0, len(event.SourceLocations))
	for _, location := range event.SourceLocations {
		value, err := locationValue(&location)
		if err != nil {
			return nil, err
		}
		sourceLocations = append(sourceLocations, value)
	}
	var projected core.Value = core.NullValue()
	if event.ProjectedLocation != nil {
		value, err := projectedLocationValue(*event.ProjectedLocation)
		if err != nil {
			return nil, err
		}
		projected = value
	}
	arguments, err := stringMapObject(event.Arguments)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "code", Value: core.String(event.Code)},
		core.Entry{Key: "policy_rule_id", Value: nullableString(event.PolicyRuleID)},
		core.Entry{Key: "source_locations", Value: core.NewArray(sourceLocations...)},
		core.Entry{Key: "projected_location", Value: projected},
		core.Entry{Key: "old_category", Value: nullableString(event.OldCategory)},
		core.Entry{Key: "new_category", Value: nullableString(event.NewCategory)},
		core.Entry{Key: "reversible", Value: core.Boolean(event.Reversible)},
		core.Entry{Key: "loss_classification", Value: core.String(string(event.LossClassification))},
		core.Entry{Key: "arguments", Value: arguments},
	)
}

func parseProjectionEvent(value core.Value, path string) (ProjectionEventMessage, error) {
	fields, err := exactFields(value,
		[]string{"code", "policy_rule_id", "source_locations", "projected_location",
			"old_category", "new_category", "reversible", "loss_classification", "arguments"}, path)
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	code, err := stringOf(fields[0], path+".code")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	policyRuleID, err := optionalString(fields[1], path+".policy_rule_id")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	locationValues, err := sequenceOf(fields[2], path+".source_locations")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	sourceLocations := make([]SourceLocation, 0, len(locationValues))
	for index, locationValue := range locationValues {
		locationPath := path + ".source_locations[" + uint32String(uint32(index)) + "]"
		locationFields, err := exactFields(locationValue,
			[]string{"source_id", "start_byte", "end_byte"}, locationPath)
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		sourceID, err := stringOf(locationFields[0], locationPath+".source_id")
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		startByte, err := unsigned64(locationFields[1], locationPath+".start_byte")
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		endByte, err := unsigned64(locationFields[2], locationPath+".end_byte")
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		location, err := NewSourceLocation(sourceID, startByte, endByte)
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		sourceLocations = append(sourceLocations, *location)
	}
	var projectedLocation *ProjectedLocationMessage
	if _, isNull := fields[3].(core.Null); !isNull {
		decoded, err := parseProjectedLocation(fields[3], path+".projected_location")
		if err != nil {
			return ProjectionEventMessage{}, err
		}
		projectedLocation = &decoded
	}
	oldCategory, err := optionalString(fields[4], path+".old_category")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	newCategory, err := optionalString(fields[5], path+".new_category")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	reversible, err := booleanOf(fields[6], path+".reversible")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	lossText, err := stringOf(fields[7], path+".loss_classification")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	switch LossClassification(lossText) {
	case LossNone, LossReversible, LossLossy:
	default:
		return ProjectionEventMessage{}, invalid(path+".loss_classification", "unknown loss classification")
	}
	arguments, err := stringMapFromObject(fields[8], path+".arguments")
	if err != nil {
		return ProjectionEventMessage{}, err
	}
	return ProjectionEventMessage{
		Code:               code,
		PolicyRuleID:       policyRuleID,
		SourceLocations:    sourceLocations,
		ProjectedLocation:  projectedLocation,
		OldCategory:        oldCategory,
		NewCategory:        newCategory,
		Reversible:         reversible,
		LossClassification: LossClassification(lossText),
		Arguments:          arguments,
	}, nil
}

// sortedValueObject encodes a deterministic sorted Object<String, Value>.
func sortedValueObject(values map[string]core.Value) (core.Value, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]core.Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, core.Entry{Key: name, Value: values[name]})
	}
	return core.NewObject(entries...)
}

// sortedUint64Object encodes a deterministic sorted Object<String, Integer>.
func sortedUint64Object(values map[string]uint64) (core.Value, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]core.Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, core.Entry{Key: name, Value: integerValue(values[name])})
	}
	return core.NewObject(entries...)
}

// parseUint64Object decodes an Object<String, Integer> map.
func parseUint64Object(value core.Value, path string) (map[string]uint64, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, protocolError(KindWrongType, path, "expected Object<String, Integer>")
	}
	output := make(map[string]uint64, len(object.Entries()))
	for _, entry := range object.Entries() {
		number, err := unsigned64(entry.Value, path+"."+entry.Key)
		if err != nil {
			return nil, err
		}
		output[entry.Key] = number
	}
	return output, nil
}
