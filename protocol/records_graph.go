package protocol

// Graph query-result, provenance, and projection records
// (crates/consema-protocol/src/graph_query.rs, graph_projection.rs): the
// `core.graph-query-result@1`, `core.graph-provenance-map@1`, and
// `core.graph-projection-result@1` records. Every match and location uses
// canonical wire node IDs bound to the complete graph carried by the record.

import (
	"consema.dev/consema/core"
	"consema.dev/consema/graph"
)

// GraphQueryMatchMessage is one graph match expressed only with canonical
// wire node IDs (graph_query.rs:12-50).
type GraphQueryMatchMessage struct {
	// Kind is "Node", "SequenceElement", or "MappingEntry".
	Kind string
	// Node is the canonical node ID of Node matches.
	Node uint64
	// Parent is the canonical parent ID of association matches.
	Parent uint64
	// Ordinal is the zero-based association ordinal.
	Ordinal uint64
	// Key is the canonical key node ID of MappingEntry matches.
	Key uint64
	// Value is the canonical value node ID of MappingEntry matches.
	Value uint64
}

func (m GraphQueryMatchMessage) role() MatchRole {
	switch m.Kind {
	case "Node":
		return RoleGraphNode
	case "SequenceElement":
		return RoleGraphSequenceElement
	case "MappingEntry":
		return RoleGraphMappingEntry
	}
	return RoleGraphNode
}

// GraphQueryResultMessage is the complete or explicitly non-complete
// `core.graph-query-result@1` record (graph_query.rs:52-61).
type GraphQueryResultMessage struct {
	domain      *QueryDomain
	role        MatchRole
	graph       *PortableGraphMessage
	matches     []GraphQueryMatchMessage
	completion  *Completion
	diagnostics []*Diagnostic
}

// NewGraphQueryResultMessage validates graph binding, uniform match roles,
// associations, and counts (graph_query.rs:64-100).
func NewGraphQueryResultMessage(domain *QueryDomain, role MatchRole, graphMessage *PortableGraphMessage,
	matches []GraphQueryMatchMessage, completion *Completion,
	diagnostics []*Diagnostic) (*GraphQueryResultMessage, error) {
	if !domain.Equal(DomainPortableGraphV1()) || !isGraphRole(role) {
		return nil, invalid("$", "graph result requires core.portable-graph-query@1 and a graph role")
	}
	if completion.Produced() != uint64(len(matches)) {
		return nil, invalid("$", "completion count or graph match role is inconsistent")
	}
	for _, match := range matches {
		if match.role() != role {
			return nil, invalid("$", "completion count or graph match role is inconsistent")
		}
	}
	if err := validateGraphMatches(graphMessage, matches); err != nil {
		return nil, err
	}
	return &GraphQueryResultMessage{
		domain:      domain,
		role:        role,
		graph:       graphMessage,
		matches:     matches,
		completion:  completion,
		diagnostics: diagnostics,
	}, nil
}

// Domain returns the exact query domain.
func (m *GraphQueryResultMessage) Domain() *QueryDomain { return m.domain }

// Role returns the uniform result role.
func (m *GraphQueryResultMessage) Role() MatchRole { return m.role }

// Graph returns the complete graph that gives every canonical ID meaning.
func (m *GraphQueryResultMessage) Graph() *PortableGraphMessage { return m.graph }

// Matches returns the ordered graph matches.
func (m *GraphQueryResultMessage) Matches() []GraphQueryMatchMessage { return m.matches }

// Completion returns the explicit terminal state.
func (m *GraphQueryResultMessage) Completion() *Completion { return m.completion }

// Diagnostics returns the ordered diagnostics.
func (m *GraphQueryResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.graph-query-result@1` (graph_query.rs:191-215).
func (m *GraphQueryResultMessage) ToValue() (core.Value, error) {
	matches := make([]core.Value, 0, len(m.matches))
	for _, match := range m.matches {
		value, err := graphQueryMatchValue(match)
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
	graphValue, err := m.graph.ToValue()
	if err != nil {
		return nil, err
	}
	completion, err := m.completion.ToValue()
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.graph-query-result@1")},
		core.Entry{Key: "domain_id", Value: core.String(m.domain.id)},
		core.Entry{Key: "domain_version", Value: integerValue(uint64(m.domain.version))},
		core.Entry{Key: "role", Value: core.String(string(m.role))},
		core.Entry{Key: "graph", Value: graphValue},
		core.Entry{Key: "matches", Value: core.NewArray(matches...)},
		core.Entry{Key: "completion", Value: completion},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
	)
}

// FromValueWithRegistry strictly decodes with explicit graph limits and
// semantic-model registry (graph_query.rs:229-...).
func (m *GraphQueryResultMessage) FromValueWithRegistry(value core.Value,
	limits graph.PGCELimits, registry ErrorCodeRegistry) (*GraphQueryResultMessage, error) {
	fields, err := schemaFields(value, "core.graph-query-result@1",
		[]string{"schema", "domain_id", "domain_version", "role", "graph", "matches",
			"completion", "diagnostics"}, "$")
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
	graphMessage := &PortableGraphMessage{}
	graphMessage, err = graphMessage.FromValue(fields[4], limits)
	if err != nil {
		return nil, err
	}
	matchValues, err := sequenceOf(fields[5], "$.matches")
	if err != nil {
		return nil, err
	}
	matches := make([]GraphQueryMatchMessage, 0, len(matchValues))
	for index, matchValue := range matchValues {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		match, err := parseGraphQueryMatch(matchValue, path)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[6], registry)
	if err != nil {
		return nil, err
	}
	diagnosticValues, err := sequenceOf(fields[7], "$.diagnostics")
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
	return NewGraphQueryResultMessage(NewQueryDomain(domainID, domainVersion), role,
		graphMessage, matches, completion, diagnostics)
}

// FromValue strictly decodes with default PGCE limits.
func (m *GraphQueryResultMessage) FromValue(value core.Value) (*GraphQueryResultMessage, error) {
	return m.FromValueWithRegistry(value, graph.DefaultPGCELimits(), DefaultErrorCodeRegistry())
}

// validateGraphMatches resolves every match against the exact graph and
// rejects dangling associations (graph_query.rs:...).
func validateGraphMatches(graphMessage *PortableGraphMessage, matches []GraphQueryMatchMessage) error {
	order, ids := graphMessage.WireLayout()
	resolve := func(canonical uint64, path string) (graph.NodeID, error) {
		if canonical >= uint64(len(order)) {
			return graph.NodeID{}, invalid(path, "canonical node ID out of range")
		}
		return order[canonical], nil
	}
	for index, match := range matches {
		path := "$.matches[" + uint32String(uint32(index)) + "]"
		switch match.Kind {
		case "Node":
			if _, err := resolve(match.Node, path+".node"); err != nil {
				return err
			}
		case "SequenceElement":
			parent, err := resolve(match.Parent, path+".parent")
			if err != nil {
				return err
			}
			child, err := resolve(match.Node, path+".node")
			if err != nil {
				return err
			}
			node, ok := graphMessage.graph.Node(parent)
			if !ok || node.Kind() != graph.KindSequence {
				return invalid(path, "sequence element parent is not a sequence")
			}
			items, _ := node.SequenceItems()
			if match.Ordinal >= uint64(len(items)) {
				return invalid(path, "sequence element ordinal out of range")
			}
			if canonical, _ := ids[items[match.Ordinal]]; canonical != match.Node || items[match.Ordinal] != child {
				return invalid(path, "sequence element does not reference the child node")
			}
		case "MappingEntry":
			parent, err := resolve(match.Parent, path+".parent")
			if err != nil {
				return err
			}
			key, err := resolve(match.Key, path+".key")
			if err != nil {
				return err
			}
			value, err := resolve(match.Value, path+".value")
			if err != nil {
				return err
			}
			node, ok := graphMessage.graph.Node(parent)
			if !ok || node.Kind() != graph.KindMapping {
				return invalid(path, "mapping entry parent is not a mapping")
			}
			entries, _ := node.MappingEntries()
			if match.Ordinal >= uint64(len(entries)) {
				return invalid(path, "mapping entry ordinal out of range")
			}
			entry := entries[match.Ordinal]
			if entry.Key != key || entry.Value != value {
				return invalid(path, "mapping entry does not reference the key/value nodes")
			}
		default:
			return invalid(path, "unknown graph query match kind")
		}
	}
	return nil
}

func isGraphRole(role MatchRole) bool {
	switch role {
	case RoleGraphNode, RoleGraphSequenceElement, RoleGraphMappingEntry:
		return true
	}
	return false
}

func graphQueryMatchValue(match GraphQueryMatchMessage) (core.Value, error) {
	switch match.Kind {
	case "Node":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Node")},
			core.Entry{Key: "node", Value: integerValue(match.Node)},
		)
	case "SequenceElement":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("SequenceElement")},
			core.Entry{Key: "parent", Value: integerValue(match.Parent)},
			core.Entry{Key: "ordinal", Value: integerValue(match.Ordinal)},
			core.Entry{Key: "node", Value: integerValue(match.Node)},
		)
	case "MappingEntry":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("MappingEntry")},
			core.Entry{Key: "parent", Value: integerValue(match.Parent)},
			core.Entry{Key: "ordinal", Value: integerValue(match.Ordinal)},
			core.Entry{Key: "key", Value: integerValue(match.Key)},
			core.Entry{Key: "value", Value: integerValue(match.Value)},
		)
	}
	return nil, invalid("$", "unknown graph query match kind")
}

func parseGraphQueryMatch(value core.Value, path string) (GraphQueryMatchMessage, error) {
	entries, ok := value.(*core.Object)
	if !ok {
		return GraphQueryMatchMessage{}, protocolError(KindWrongType, path, "expected graph match Object")
	}
	if len(entries.Entries()) == 0 || entries.Entries()[0].Key != "kind" {
		return GraphQueryMatchMessage{}, invalid(path, "kind must be the first String field")
	}
	kind, err := stringOf(entries.Entries()[0].Value, path+".kind")
	if err != nil {
		return GraphQueryMatchMessage{}, err
	}
	switch kind {
	case "Node":
		fields, err := exactFields(value, []string{"kind", "node"}, path)
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		node, err := unsigned64(fields[1], path+".node")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		return GraphQueryMatchMessage{Kind: "Node", Node: node}, nil
	case "SequenceElement":
		fields, err := exactFields(value, []string{"kind", "parent", "ordinal", "node"}, path)
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		parent, err := unsigned64(fields[1], path+".parent")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		ordinal, err := unsigned64(fields[2], path+".ordinal")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		node, err := unsigned64(fields[3], path+".node")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		return GraphQueryMatchMessage{Kind: "SequenceElement", Parent: parent, Ordinal: ordinal, Node: node}, nil
	case "MappingEntry":
		fields, err := exactFields(value, []string{"kind", "parent", "ordinal", "key", "value"}, path)
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		parent, err := unsigned64(fields[1], path+".parent")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		ordinal, err := unsigned64(fields[2], path+".ordinal")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		key, err := unsigned64(fields[3], path+".key")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		value, err := unsigned64(fields[4], path+".value")
		if err != nil {
			return GraphQueryMatchMessage{}, err
		}
		return GraphQueryMatchMessage{Kind: "MappingEntry", Parent: parent, Ordinal: ordinal, Key: key, Value: value}, nil
	}
	return GraphQueryMatchMessage{}, invalid(path, "unknown graph query match kind")
}

// GraphProjectedLocationMessage is one projected graph location expressed
// with canonical wire node IDs (graph_projection.rs:13-41).
type GraphProjectedLocationMessage struct {
	// Kind is "Root", "Node", "SequenceElement", "MappingKey", or
	// "MappingValue".
	Kind string
	// Ordinal is the root ordinal or association ordinal.
	Ordinal uint64
	// Node is the canonical node ID.
	Node uint64
	// Parent is the canonical parent node ID.
	Parent uint64
}

// Less reports whether the location precedes other in the canonical wire
// order (graph_projection.rs: the Rust Ord derivation).
func (l GraphProjectedLocationMessage) Less(other GraphProjectedLocationMessage) bool {
	if l.Kind != other.Kind {
		return graphLocationRank(l.Kind) < graphLocationRank(other.Kind)
	}
	switch l.Kind {
	case "Root":
		return l.Ordinal < other.Ordinal
	case "Node":
		return l.Node < other.Node
	case "SequenceElement", "MappingKey", "MappingValue":
		if l.Parent != other.Parent {
			return l.Parent < other.Parent
		}
		return l.Ordinal < other.Ordinal
	}
	return false
}

// Equal reports whether two projected locations are identical.
func (l GraphProjectedLocationMessage) Equal(other GraphProjectedLocationMessage) bool {
	return l.Kind == other.Kind && l.Ordinal == other.Ordinal && l.Node == other.Node &&
		l.Parent == other.Parent
}

// graphLocationRank is the closed variant order of the projected graph
// locations (the Rust Ord derivation: Root < Node < SequenceElement <
// MappingKey < MappingValue).
func graphLocationRank(kind string) int {
	switch kind {
	case "Root":
		return 0
	case "Node":
		return 1
	case "SequenceElement":
		return 2
	case "MappingKey":
		return 3
	case "MappingValue":
		return 4
	}
	return 5
}

// GraphProvenanceRelationMessage is the exact YAML-source relation to a
// projected graph fact (graph_projection.rs:43-56).
type GraphProvenanceRelationMessage string

// The two frozen graph relations.
const (
	GraphRelationDirect    GraphProvenanceRelationMessage = "Direct"
	GraphRelationReference GraphProvenanceRelationMessage = "Reference"
)

// GraphSourceOriginMessage is a transferable graph origin with
// caller-assigned identities (graph_projection.rs:58-74).
type GraphSourceOriginMessage struct {
	// SourceID is the stable source identity.
	SourceID string
	// NodeLocator is the optional stable caller node locator.
	NodeLocator *string
	// StartByte is the inclusive source byte start.
	StartByte uint64
	// EndByte is the exclusive source byte end.
	EndByte uint64
	// Relation is the exact graph provenance relation.
	Relation GraphProvenanceRelationMessage
}

// NewGraphSourceOriginMessage validates one externalized graph origin
// (graph_projection.rs:76-102).
func NewGraphSourceOriginMessage(sourceID string, nodeLocator *string, startByte, endByte uint64,
	relation GraphProvenanceRelationMessage) (*GraphSourceOriginMessage, error) {
	if sourceID == "" || len(sourceID) > 1024 || startByte > endByte ||
		(nodeLocator != nil && (*nodeLocator == "" || len(*nodeLocator) > 4096)) {
		return nil, invalid("$.origin", "invalid source identity, locator, or half-open range")
	}
	return &GraphSourceOriginMessage{
		SourceID:    sourceID,
		NodeLocator: nodeLocator,
		StartByte:   startByte,
		EndByte:     endByte,
		Relation:    relation,
	}, nil
}

// GraphProvenanceEntryMessage is one graph location and all ordered source
// origins (graph_projection.rs:104-112).
type GraphProvenanceEntryMessage struct {
	// Projected is the projected graph location.
	Projected GraphProjectedLocationMessage
	// Origins are the one or more source origins.
	Origins []GraphSourceOriginMessage
}

// GraphProvenanceMapMessage is the sorted unique
// `core.graph-provenance-map@1` record (graph_projection.rs:114-120).
type GraphProvenanceMapMessage struct {
	entries []GraphProvenanceEntryMessage
}

// NewGraphProvenanceMapMessage validates canonical location order,
// uniqueness, and non-empty origins (graph_projection.rs:121-141).
func NewGraphProvenanceMapMessage(entries []GraphProvenanceEntryMessage) (*GraphProvenanceMapMessage, error) {
	for _, entry := range entries {
		if len(entry.Origins) == 0 {
			return nil, invalid("$.entries", "graph provenance locations must be sorted, unique, and have origins")
		}
	}
	for index := 1; index < len(entries); index++ {
		if !entries[index-1].Projected.Less(entries[index].Projected) {
			return nil, invalid("$.entries", "graph provenance locations must be sorted, unique, and have origins")
		}
	}
	return &GraphProvenanceMapMessage{entries: entries}, nil
}

// Entries returns the sorted provenance entries.
func (m *GraphProvenanceMapMessage) Entries() []GraphProvenanceEntryMessage { return m.entries }

// ValidateAgainst validates every projected location against one exact
// graph message (graph_projection.rs:143-158).
func (m *GraphProvenanceMapMessage) ValidateAgainst(graphMessage *PortableGraphMessage) error {
	order, _ := graphMessage.WireLayout()
	for index, entry := range m.entries {
		if err := validateGraphLocation(graphMessage, order, entry.Projected,
			"$.entries["+uint32String(uint32(index))+".projected]"); err != nil {
			return err
		}
	}
	return nil
}

// validateGraphLocation validates one projected location against the exact
// graph (graph_projection.rs:360-...).
func validateGraphLocation(graphMessage *PortableGraphMessage, canonical []graph.NodeID,
	location GraphProjectedLocationMessage, path string) error {
	resolve := func(id uint64, name string) (graph.NodeID, error) {
		if id >= uint64(len(canonical)) {
			return graph.NodeID{}, invalid(path+"."+name, "canonical node ID out of range")
		}
		return canonical[id], nil
	}
	switch location.Kind {
	case "Root":
		if location.Ordinal >= uint64(len(graphMessage.graph.Roots())) {
			return invalid(path, "root ordinal out of range")
		}
	case "Node":
		if _, err := resolve(location.Node, "node"); err != nil {
			return err
		}
	case "SequenceElement":
		parent, err := resolve(location.Parent, "parent")
		if err != nil {
			return err
		}
		node, ok := graphMessage.graph.Node(parent)
		if !ok || node.Kind() != graph.KindSequence {
			return invalid(path, "sequence element parent is not a sequence")
		}
		items, _ := node.SequenceItems()
		if location.Ordinal >= uint64(len(items)) {
			return invalid(path, "sequence element ordinal out of range")
		}
	case "MappingKey", "MappingValue":
		parent, err := resolve(location.Parent, "parent")
		if err != nil {
			return err
		}
		node, ok := graphMessage.graph.Node(parent)
		if !ok || node.Kind() != graph.KindMapping {
			return invalid(path, "mapping location parent is not a mapping")
		}
		entries, _ := node.MappingEntries()
		if location.Ordinal >= uint64(len(entries)) {
			return invalid(path, "mapping location ordinal out of range")
		}
	default:
		return invalid(path, "unknown projected graph location")
	}
	return nil
}

// ToValue encodes `core.graph-provenance-map@1` (graph_projection.rs:160-182).
func (m *GraphProvenanceMapMessage) ToValue() (core.Value, error) {
	entries := make([]core.Value, 0, len(m.entries))
	for _, entry := range m.entries {
		projected, err := graphLocationValue(entry.Projected)
		if err != nil {
			return nil, err
		}
		origins := make([]core.Value, 0, len(entry.Origins))
		for _, origin := range entry.Origins {
			value, err := graphOriginValue(origin)
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
		core.Entry{Key: "schema", Value: core.String("core.graph-provenance-map@1")},
		core.Entry{Key: "entries", Value: core.NewArray(entries...)},
	)
}

// FromValue strictly decodes one graph provenance map
// (graph_projection.rs:183-208).
func (m *GraphProvenanceMapMessage) FromValue(value core.Value) (*GraphProvenanceMapMessage, error) {
	fields, err := schemaFields(value, "core.graph-provenance-map@1", []string{"schema", "entries"}, "$")
	if err != nil {
		return nil, err
	}
	entryValues, err := sequenceOf(fields[1], "$.entries")
	if err != nil {
		return nil, err
	}
	entries := make([]GraphProvenanceEntryMessage, 0, len(entryValues))
	for index, entryValue := range entryValues {
		path := "$.entries[" + uint32String(uint32(index)) + "]"
		entryFields, err := exactFields(entryValue, []string{"projected", "origins"}, path)
		if err != nil {
			return nil, err
		}
		projected, err := parseGraphLocation(entryFields[0], path+".projected")
		if err != nil {
			return nil, err
		}
		originValues, err := sequenceOf(entryFields[1], path+".origins")
		if err != nil {
			return nil, err
		}
		origins := make([]GraphSourceOriginMessage, 0, len(originValues))
		for originIndex, originValue := range originValues {
			originPath := path + ".origins[" + uint32String(uint32(originIndex)) + "]"
			origin, err := parseGraphOrigin(originValue, originPath)
			if err != nil {
				return nil, err
			}
			origins = append(origins, *origin)
		}
		entries = append(entries, GraphProvenanceEntryMessage{Projected: projected, Origins: origins})
	}
	return NewGraphProvenanceMapMessage(entries)
}

func graphLocationValue(location GraphProjectedLocationMessage) (core.Value, error) {
	switch location.Kind {
	case "Root":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Root")},
			core.Entry{Key: "ordinal", Value: integerValue(location.Ordinal)},
		)
	case "Node":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String("Node")},
			core.Entry{Key: "node", Value: integerValue(location.Node)},
		)
	case "SequenceElement", "MappingKey", "MappingValue":
		return core.NewObject(
			core.Entry{Key: "kind", Value: core.String(location.Kind)},
			core.Entry{Key: "parent", Value: integerValue(location.Parent)},
			core.Entry{Key: "ordinal", Value: integerValue(location.Ordinal)},
		)
	}
	return nil, invalid("$", "unknown projected graph location")
}

func parseGraphLocation(value core.Value, path string) (GraphProjectedLocationMessage, error) {
	entries, ok := value.(*core.Object)
	if !ok {
		return GraphProjectedLocationMessage{}, protocolError(KindWrongType, path, "expected location Object")
	}
	if len(entries.Entries()) == 0 || entries.Entries()[0].Key != "kind" {
		return GraphProjectedLocationMessage{}, invalid(path, "kind must be the first String field")
	}
	kind, err := stringOf(entries.Entries()[0].Value, path+".kind")
	if err != nil {
		return GraphProjectedLocationMessage{}, err
	}
	switch kind {
	case "Root":
		fields, err := exactFields(value, []string{"kind", "ordinal"}, path)
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		ordinal, err := unsigned64(fields[1], path+".ordinal")
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		return GraphProjectedLocationMessage{Kind: "Root", Ordinal: ordinal}, nil
	case "Node":
		fields, err := exactFields(value, []string{"kind", "node"}, path)
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		node, err := unsigned64(fields[1], path+".node")
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		return GraphProjectedLocationMessage{Kind: "Node", Node: node}, nil
	case "SequenceElement", "MappingKey", "MappingValue":
		fields, err := exactFields(value, []string{"kind", "parent", "ordinal"}, path)
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		parent, err := unsigned64(fields[1], path+".parent")
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		ordinal, err := unsigned64(fields[2], path+".ordinal")
		if err != nil {
			return GraphProjectedLocationMessage{}, err
		}
		return GraphProjectedLocationMessage{Kind: kind, Parent: parent, Ordinal: ordinal}, nil
	}
	return GraphProjectedLocationMessage{}, invalid(path, "unknown projected graph location")
}

func graphOriginValue(origin GraphSourceOriginMessage) (core.Value, error) {
	return core.NewObject(
		core.Entry{Key: "source_id", Value: core.String(origin.SourceID)},
		core.Entry{Key: "node_locator", Value: nullableString(origin.NodeLocator)},
		core.Entry{Key: "start_byte", Value: integerValue(origin.StartByte)},
		core.Entry{Key: "end_byte", Value: integerValue(origin.EndByte)},
		core.Entry{Key: "relation", Value: core.String(string(origin.Relation))},
	)
}

func parseGraphOrigin(value core.Value, path string) (*GraphSourceOriginMessage, error) {
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
	switch GraphProvenanceRelationMessage(relationText) {
	case GraphRelationDirect, GraphRelationReference:
	default:
		return nil, invalid(path+".relation", "unknown graph provenance relation")
	}
	return NewGraphSourceOriginMessage(sourceID, nodeLocator, startByte, endByte,
		GraphProvenanceRelationMessage(relationText))
}

// GraphProjectionResultMessage is the atomic exact
// `core.graph-projection-result@1` record (graph_projection.rs:232-244).
type GraphProjectionResultMessage struct {
	completion  *Completion
	graph       *PortableGraphMessage
	hasGraph    bool
	provenance  *GraphProvenanceMapMessage
	diagnostics []*Diagnostic
}

// NewGraphProjectionResultMessage validates atomic success, produced
// count, and complete graph provenance (graph_projection.rs:245-...).
func NewGraphProjectionResultMessage(completion *Completion, graphMessage *PortableGraphMessage,
	hasGraph bool, provenance *GraphProvenanceMapMessage,
	diagnostics []*Diagnostic) (*GraphProjectionResultMessage, error) {
	success := completion.Status() == CompletionSuccess
	if success != hasGraph {
		return nil, invalid("$", "only a successful graph projection carries a graph")
	}
	if success {
		if err := provenance.ValidateAgainst(graphMessage); err != nil {
			return nil, err
		}
	} else if len(provenance.entries) != 0 {
		return nil, invalid("$.provenance", "failed projection cannot claim complete provenance")
	}
	return &GraphProjectionResultMessage{
		completion:  completion,
		graph:       graphMessage,
		hasGraph:    hasGraph,
		provenance:  provenance,
		diagnostics: diagnostics,
	}, nil
}

// Completion returns the completion state.
func (m *GraphProjectionResultMessage) Completion() *Completion { return m.completion }

// Graph returns the complete graph only on success.
func (m *GraphProjectionResultMessage) Graph() (*PortableGraphMessage, bool) {
	return m.graph, m.hasGraph
}

// Provenance returns the complete graph provenance.
func (m *GraphProjectionResultMessage) Provenance() *GraphProvenanceMapMessage { return m.provenance }

// Diagnostics returns the ordered diagnostics.
func (m *GraphProjectionResultMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.graph-projection-result@1`
// (graph_projection.rs:283-302).
func (m *GraphProjectionResultMessage) ToValue() (core.Value, error) {
	var graphValue core.Value = core.NullValue()
	if m.hasGraph {
		value, err := m.graph.ToValue()
		if err != nil {
			return nil, err
		}
		graphValue, err = core.NewObject(core.Entry{Key: "portable_graph", Value: value})
		if err != nil {
			return nil, err
		}
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
		core.Entry{Key: "schema", Value: core.String("core.graph-projection-result@1")},
		core.Entry{Key: "completion", Value: completion},
		core.Entry{Key: "graph", Value: graphValue},
		core.Entry{Key: "provenance", Value: provenance},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
	)
}

// FromValueWithRegistry strictly decodes with explicit graph limits and
// semantic-model registry (graph_projection.rs:321-...).
func (m *GraphProjectionResultMessage) FromValueWithRegistry(value core.Value,
	limits graph.PGCELimits, registry ErrorCodeRegistry) (*GraphProjectionResultMessage, error) {
	fields, err := schemaFields(value, "core.graph-projection-result@1",
		[]string{"schema", "completion", "graph", "provenance", "diagnostics"}, "$")
	if err != nil {
		return nil, err
	}
	completion := &Completion{}
	completion, err = completion.FromValueWithRegistry(fields[1], registry)
	if err != nil {
		return nil, err
	}
	var graphMessage *PortableGraphMessage
	hasGraph := false
	if _, isNull := fields[2].(core.Null); !isNull {
		graphFields, err := exactFields(fields[2], []string{"portable_graph"}, "$.graph")
		if err != nil {
			return nil, err
		}
		message := &PortableGraphMessage{}
		graphMessage, err = message.FromValue(graphFields[0], limits)
		if err != nil {
			return nil, err
		}
		hasGraph = true
	}
	provenance := &GraphProvenanceMapMessage{}
	provenance, err = provenance.FromValue(fields[3])
	if err != nil {
		return nil, err
	}
	diagnosticValues, err := sequenceOf(fields[4], "$.diagnostics")
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
	return NewGraphProjectionResultMessage(completion, graphMessage, hasGraph, provenance, diagnostics)
}

// FromValue strictly decodes with default PGCE limits.
func (m *GraphProjectionResultMessage) FromValue(value core.Value) (*GraphProjectionResultMessage, error) {
	return m.FromValueWithRegistry(value, graph.DefaultPGCELimits(), DefaultErrorCodeRegistry())
}
