package protocol

// The `core.portable-graph@1` record: the canonical readable graph plus its
// exact PGCE/1 bytes (crates/consema-protocol/src/portable_graph.rs). Both
// forms are cross-validated on decode.

import (
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
)

// PortableGraphMessage is the validated readable graph and exact PGCE/1
// bytes of `core.portable-graph@1` (portable_graph.rs:12-20).
type PortableGraphMessage struct {
	graph *graph.Graph
	pgce  []byte
}

// NewPortableGraphMessageFromGraph canonically encodes one complete graph
// under explicit PGCE limits (portable_graph.rs:23-31).
func NewPortableGraphMessageFromGraph(g *graph.Graph, limits graph.PGCELimits) (*PortableGraphMessage, error) {
	pgce, err := graph.EncodePGCEBounded(g, limits)
	if err != nil {
		return nil, mapGraphEncodeError(err)
	}
	return &PortableGraphMessage{graph: g, pgce: pgce}, nil
}

// Graph returns the complete immutable graph.
func (m *PortableGraphMessage) Graph() *graph.Graph { return m.graph }

// PGCE returns the exact canonical PGCE/1 bytes.
func (m *PortableGraphMessage) PGCE() []byte { return append([]byte(nil), m.pgce...) }

// ToValue encodes the fixed readable graph plus PGCE schema
// (portable_graph.rs:44-126).
func (m *PortableGraphMessage) ToValue() (core.Value, error) {
	order, ids := canonicalLayout(m.graph)
	roots := make([]core.Value, 0, len(m.graph.Roots()))
	for _, root := range m.graph.Roots() {
		roots = append(roots, integerValue(ids[root]))
	}
	nodes := make([]core.Value, 0, m.graph.NodeCount())
	for wireID, nodeID := range order {
		node, ok := m.graph.Node(nodeID)
		if !ok {
			return nil, invalid("$", "completed graph IDs resolve")
		}
		id := integerValue(uint64(wireID))
		var record *core.Object
		var err error
		switch node.Kind() {
		case graph.KindScalar:
			content, _ := node.ScalarContent()
			record, err = core.NewObject(
				core.Entry{Key: "id", Value: id},
				core.Entry{Key: "kind", Value: core.String("Scalar")},
				core.Entry{Key: "tag", Value: core.String(node.Tag())},
				core.Entry{Key: "canonical_content", Value: core.String(content)},
			)
		case graph.KindSequence:
			items, _ := node.SequenceItems()
			itemValues := make([]core.Value, 0, len(items))
			for _, item := range items {
				itemValues = append(itemValues, integerValue(ids[item]))
			}
			record, err = core.NewObject(
				core.Entry{Key: "id", Value: id},
				core.Entry{Key: "kind", Value: core.String("Sequence")},
				core.Entry{Key: "tag", Value: core.String(node.Tag())},
				core.Entry{Key: "items", Value: core.NewArray(itemValues...)},
			)
		case graph.KindMapping:
			entries, _ := node.MappingEntries()
			entryValues := make([]core.Value, 0, len(entries))
			for _, entry := range entries {
				value, err := core.NewObject(
					core.Entry{Key: "key", Value: integerValue(ids[entry.Key])},
					core.Entry{Key: "value", Value: integerValue(ids[entry.Value])},
				)
				if err != nil {
					return nil, err
				}
				entryValues = append(entryValues, value)
			}
			record, err = core.NewObject(
				core.Entry{Key: "id", Value: id},
				core.Entry{Key: "kind", Value: core.String("Mapping")},
				core.Entry{Key: "tag", Value: core.String(node.Tag())},
				core.Entry{Key: "entries", Value: core.NewArray(entryValues...)},
			)
		default:
			return nil, invalid("$", "unknown graph node kind")
		}
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, record)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.portable-graph@1")},
		core.Entry{Key: "encoding", Value: core.String("PGCE/1")},
		core.Entry{Key: "roots", Value: core.NewArray(roots...)},
		core.Entry{Key: "nodes", Value: core.NewArray(nodes...)},
		core.Entry{Key: "pgce", Value: core.NewBytes(m.pgce)},
	)
}

// FromValue strictly decodes and cross-validates the readable graph and
// PGCE/1 forms (portable_graph.rs:127-203).
func (m *PortableGraphMessage) FromValue(value core.Value, limits graph.PGCELimits) (*PortableGraphMessage, error) {
	fields, err := schemaFields(value, "core.portable-graph@1",
		[]string{"schema", "encoding", "roots", "nodes", "pgce"}, "$")
	if err != nil {
		return nil, err
	}
	encoding, err := stringOf(fields[1], "$.encoding")
	if err != nil {
		return nil, err
	}
	if encoding != "PGCE/1" {
		return nil, invalid("$.encoding", "expected PGCE/1")
	}
	rootValues, err := sequenceOf(fields[2], "$.roots")
	if err != nil {
		return nil, err
	}
	nodeValues, err := sequenceOf(fields[3], "$.nodes")
	if err != nil {
		return nil, err
	}
	if err := checkGraphCount("$.roots", len(rootValues), int(limits.MaxRoots)); err != nil {
		return nil, err
	}
	if err := checkGraphCount("$.nodes", len(nodeValues), int(limits.MaxNodes)); err != nil {
		return nil, err
	}
	pgceBytes, ok := fields[4].(core.Bytes)
	if !ok {
		return nil, protocolError(KindWrongType, "$.pgce", "expected Bytes")
	}
	if err := checkGraphCount("$.pgce", len(pgceBytes), int(limits.MaxStreamBytes)); err != nil {
		return nil, err
	}
	builder := graph.NewBuilder(graphLimitsOf(limits))
	ids := make([]graph.NodeID, 0, len(nodeValues))
	for range nodeValues {
		id, err := builder.ReserveNode()
		if err != nil {
			return nil, mapGraphBuildError(err)
		}
		ids = append(ids, id)
	}
	for index, record := range nodeValues {
		if err := defineGraphRecord(builder, ids, index, record, limits); err != nil {
			return nil, err
		}
	}
	for index, root := range rootValues {
		canonical, err := unsigned64(root, "$.roots["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		rootID, err := resolveGraphID(ids, canonical, "$.roots["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		if err := builder.PushRoot(rootID); err != nil {
			return nil, mapGraphBuildError(err)
		}
	}
	built, err := builder.Build()
	if err != nil {
		return nil, mapGraphBuildError(err)
	}
	if order := canonicalOrder(built); !equalNodeIDs(order, ids) {
		return nil, invalid("$.nodes", "node records are not in canonical first-discovery order")
	}
	decoded, err := graph.DecodePGCE(pgceBytes, limits)
	if err != nil {
		return nil, mapGraphDecodeError(err)
	}
	if !graph.Equal(built, decoded) {
		return nil, invalid("$", "readable graph and PGCE graph are not strictly equal")
	}
	canonical, err := graph.EncodePGCEBounded(built, limits)
	if err != nil {
		return nil, mapGraphEncodeError(err)
	}
	if string(canonical) != string(pgceBytes) {
		return nil, invalid("$.pgce", "PGCE bytes disagree with readable graph")
	}
	return &PortableGraphMessage{graph: built, pgce: canonical}, nil
}

// CanonicalNodeID resolves a graph-local handle to its stable canonical
// wire ID (portable_graph.rs:205-212).
func (m *PortableGraphMessage) CanonicalNodeID(node graph.NodeID) (uint64, bool) {
	_, ids := canonicalLayout(m.graph)
	id, ok := ids[node]
	return id, ok
}

// GraphNodeID resolves a canonical wire ID to the message's graph-local
// handle (portable_graph.rs:214-224).
func (m *PortableGraphMessage) GraphNodeID(canonical uint64) (graph.NodeID, bool) {
	order, _ := canonicalLayout(m.graph)
	if canonical >= uint64(len(order)) {
		return graph.NodeID{}, false
	}
	return order[canonical], true
}

// WireLayout returns the canonical first-discovery layout.
func (m *PortableGraphMessage) WireLayout() ([]graph.NodeID, map[graph.NodeID]uint64) {
	return canonicalLayout(m.graph)
}

// CanonicalLayout returns the canonical first-discovery layout.
func CanonicalLayout(g *graph.Graph) ([]graph.NodeID, map[graph.NodeID]uint64) {
	return canonicalLayout(g)
}

// canonicalLayout computes the canonical first-discovery node order of one
// graph (portable_graph.rs:232-...): roots first in root order, then every
// edge target in edge order.
func canonicalLayout(g *graph.Graph) (order []graph.NodeID, ids map[graph.NodeID]uint64) {
	order = make([]graph.NodeID, 0, g.NodeCount())
	ids = make(map[graph.NodeID]uint64, g.NodeCount())
	var visit func(id graph.NodeID)
	visit = func(id graph.NodeID) {
		if _, seen := ids[id]; seen {
			return
		}
		ids[id] = uint64(len(order))
		order = append(order, id)
		node, ok := g.Node(id)
		if !ok {
			return
		}
		switch node.Kind() {
		case graph.KindSequence:
			items, _ := node.SequenceItems()
			for _, item := range items {
				visit(item)
			}
		case graph.KindMapping:
			entries, _ := node.MappingEntries()
			for _, entry := range entries {
				visit(entry.Key)
				visit(entry.Value)
			}
		}
	}
	for _, root := range g.Roots() {
		visit(root)
	}
	// Every reserved node participates even when unreachable from the roots.
	if len(order) < g.NodeCount() {
		for _, id := range g.Nodes() {
			visit(id)
		}
	}
	return order, ids
}

func canonicalOrder(g *graph.Graph) []graph.NodeID {
	order, _ := canonicalLayout(g)
	return order
}

func equalNodeIDs(left, right []graph.NodeID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// kindOfRecord reads the canonical wire ID and the kind discriminator of
// one readable node record (portable_graph.rs: the kind-tagged records).
func kindOfRecord(record core.Value, path string) (string, error) {
	object, ok := record.(*core.Object)
	if !ok {
		return "", protocolError(KindWrongType, path, "expected node Object")
	}
	entries := object.Entries()
	if len(entries) == 0 || entries[0].Key != "id" {
		return "", invalid(path, "id must be the first field")
	}
	canonicalID, err := unsigned64(entries[0].Value, path+".id")
	if err != nil {
		return "", err
	}
	index, err := nodeIndexFromPath(path)
	if err != nil {
		return "", err
	}
	if canonicalID != index {
		return "", invalid(path+".id", "node records must carry canonical wire IDs")
	}
	for _, entry := range entries[1:] {
		if entry.Key == "kind" {
			kind, err := stringOf(entry.Value, path+".kind")
			if err != nil {
				return "", err
			}
			return kind, nil
		}
	}
	return "", invalid(path, "kind field is absent")
}

// nodeIndexFromPath extracts the node index of a "$.nodes[N]" path.
func nodeIndexFromPath(path string) (uint64, error) {
	var index uint64
	if _, err := fmt.Sscanf(path, "$.nodes[%d]", &index); err != nil {
		return 0, invalid(path, "invalid node path")
	}
	return index, nil
}

// checkGraphCount applies one resource limit.
func checkGraphCount(name string, observed, limit int) error {
	if observed > limit {
		return resource(name, "count exceeds configured limit")
	}
	return nil
}

// graphLimitsOf maps PGCE limits to graph construction limits
// (portable_graph.rs:147-157).
func graphLimitsOf(limits graph.PGCELimits) graph.Limits {
	return graph.Limits{
		MaxRoots:            limits.MaxRoots,
		MaxNodes:            limits.MaxNodes,
		MaxEdges:            limits.MaxEdges,
		MaxContainerEntries: limits.MaxContainerEntries,
		MaxTagBytes:         limits.MaxTagBytes,
		MaxScalarBytes:      limits.MaxScalarBytes,
		MaxTraversalDepth:   limits.MaxTraversalDepth,
	}
}

// defineGraphRecord builds one readable node record into the builder
// (portable_graph.rs:159-...).
func defineGraphRecord(builder *graph.Builder, ids []graph.NodeID, index int,
	record core.Value, limits graph.PGCELimits) error {
	path := "$.nodes[" + uint32String(uint32(index)) + "]"
	kind, err := kindOfRecord(record, path)
	if err != nil {
		return err
	}
	id := ids[index]
	switch kind {
	case "Scalar":
		contentFields, err := exactFields(record, []string{"id", "kind", "tag", "canonical_content"}, path)
		if err != nil {
			return err
		}
		tag, err := stringOf(contentFields[2], path+".tag")
		if err != nil {
			return err
		}
		content, err := stringOf(contentFields[3], path+".canonical_content")
		if err != nil {
			return err
		}
		if err := builder.DefineScalar(id, tag, content); err != nil {
			return mapGraphBuildError(err)
		}
		return nil
	case "Sequence":
		itemFields, err := exactFields(record, []string{"id", "kind", "tag", "items"}, path)
		if err != nil {
			return err
		}
		tag, err := stringOf(itemFields[2], path+".tag")
		if err != nil {
			return err
		}
		itemValues, err := sequenceOf(itemFields[3], path+".items")
		if err != nil {
			return err
		}
		items := make([]graph.NodeID, 0, len(itemValues))
		for itemIndex, itemValue := range itemValues {
			itemPath := path + ".items[" + uint32String(uint32(itemIndex)) + "]"
			itemNumber, err := unsigned64(itemValue, itemPath)
			if err != nil {
				return err
			}
			item, err := resolveGraphID(ids, itemNumber, itemPath)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := builder.DefineSequence(id, tag, items); err != nil {
			return mapGraphBuildError(err)
		}
		return nil
	case "Mapping":
		entryFields, err := exactFields(record, []string{"id", "kind", "tag", "entries"}, path)
		if err != nil {
			return err
		}
		tag, err := stringOf(entryFields[2], path+".tag")
		if err != nil {
			return err
		}
		entryValues, err := sequenceOf(entryFields[3], path+".entries")
		if err != nil {
			return err
		}
		entries := make([]graph.MappingEntry, 0, len(entryValues))
		for entryIndex, entryValue := range entryValues {
			entryPath := path + ".entries[" + uint32String(uint32(entryIndex)) + "]"
			entry, err := exactFields(entryValue, []string{"key", "value"}, entryPath)
			if err != nil {
				return err
			}
			keyNumber, err := unsigned64(entry[0], entryPath+".key")
			if err != nil {
				return err
			}
			key, err := resolveGraphID(ids, keyNumber, entryPath+".key")
			if err != nil {
				return err
			}
			valueNumber, err := unsigned64(entry[1], entryPath+".value")
			if err != nil {
				return err
			}
			value, err := resolveGraphID(ids, valueNumber, entryPath+".value")
			if err != nil {
				return err
			}
			entries = append(entries, graph.MappingEntry{Key: key, Value: value})
		}
		if err := builder.DefineMapping(id, tag, entries); err != nil {
			return mapGraphBuildError(err)
		}
		return nil
	}
	return invalid(path, "unknown graph node kind")
}

// resolveGraphID resolves one canonical wire ID into the message's graph
// handles.
func resolveGraphID(ids []graph.NodeID, canonical uint64, path string) (graph.NodeID, error) {
	if canonical >= uint64(len(ids)) {
		return graph.NodeID{}, invalid(path, "canonical node ID out of range")
	}
	return ids[canonical], nil
}

func mapGraphEncodeError(err error) error {
	return protocolError(KindInvalidValue, "$.pgce", err.Error())
}

func mapGraphBuildError(err error) error {
	return protocolError(KindInvalidValue, "$.nodes", err.Error())
}

func mapGraphDecodeError(err error) error {
	return protocolError(KindInvalidValue, "$.pgce", err.Error())
}
