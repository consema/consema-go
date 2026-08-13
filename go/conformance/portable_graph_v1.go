package conformance

// The `consema.portable-graph.conformance@1` suite runner
// (consema-rs/crates/consema-conformance/src/portable_graph_v1.rs). Every case builds
// its graph from the vector `input` and asserts the vector `expected`
// facts; the runner holds no expectation literals.

import (
	"encoding/hex"
	"fmt"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
	"consema.dev/consema/protocol"
)

// runPortableGraphV1 executes the embedded graph suite.
func runPortableGraphV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch {
		case vector.ID == "pgce.empty-vector" || vector.ID == "pgce.scalar-vector":
			_, err = graphPGCEVector(vector)
		case vector.ID == "graph.isomorphic-builder-numbering" || vector.ID == "graph.sharing-is-not-duplication":
			_, err = graphEquality(vector)
		case vector.ID == "pgce.cycle-roundtrip":
			_, err = graphPGCERoundtrip(vector)
		case vector.ID == "pgce.reject-nonminimal-varint" || vector.ID == "pgce.reject-noncanonical-node-order":
			_, err = graphPGCERejection(vector)
		case vector.ID == "query.reachable-canonical-order" || vector.ID == "query.distinct-shared-identity":
			_, err = graphQuery(vector)
		case vector.ID == "resource.pgce-stream-limit":
			_, err = graphPGCEStreamLimit(vector)
		default:
			err = fmt.Errorf("runner does not recognize published graph case")
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

// graphFromVector builds one graph from the vector descriptor
// {roots: [...], nodes: [{kind, tag, content|items|entries}]}.
func graphFromVector(value core.Value) (*graph.Graph, error) {
	nodesValue, ok := objectField(value, "nodes")
	if !ok {
		return nil, fmt.Errorf("graph.nodes missing")
	}
	nodes, ok := nodesValue.(*core.Array)
	if !ok {
		return nil, fmt.Errorf("graph.nodes must be Sequence")
	}
	rootsValue, ok := objectField(value, "roots")
	if !ok {
		return nil, fmt.Errorf("graph.roots missing")
	}
	roots, ok := rootsValue.(*core.Array)
	if !ok {
		return nil, fmt.Errorf("graph.roots must be Sequence")
	}
	builder := graph.NewBuilder(graph.DefaultLimits())
	ids := make([]graph.NodeID, 0, nodes.Len())
	for range nodes.Items() {
		id, err := builder.ReserveNode()
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	for index, nodeValue := range nodes.Items() {
		node, ok := nodeValue.(*core.Object)
		if !ok {
			return nil, fmt.Errorf("graph node %d must be Object", index)
		}
		kind, ok := stringField(node, "kind")
		if !ok {
			return nil, fmt.Errorf("graph node %d kind missing", index)
		}
		tag, ok := stringField(node, "tag")
		if !ok {
			return nil, fmt.Errorf("graph node %d tag missing", index)
		}
		switch kind {
		case "Scalar":
			content, ok := stringField(node, "content")
			if !ok {
				return nil, fmt.Errorf("scalar %d content missing", index)
			}
			if err := builder.DefineScalar(ids[index], tag, content); err != nil {
				return nil, err
			}
		case "Sequence":
			itemsValue, ok := node.Get("items")
			if !ok {
				return nil, fmt.Errorf("sequence %d items missing", index)
			}
			items, ok := itemsValue.(*core.Array)
			if !ok {
				return nil, fmt.Errorf("sequence %d items must be Sequence", index)
			}
			itemIDs := make([]graph.NodeID, 0, items.Len())
			for _, item := range items.Items() {
				itemID, err := graphReference(item, ids)
				if err != nil {
					return nil, err
				}
				itemIDs = append(itemIDs, itemID)
			}
			if err := builder.DefineSequence(ids[index], tag, itemIDs); err != nil {
				return nil, err
			}
		case "Mapping":
			entriesValue, ok := node.Get("entries")
			if !ok {
				return nil, fmt.Errorf("mapping %d entries missing", index)
			}
			entries, ok := entriesValue.(*core.Array)
			if !ok {
				return nil, fmt.Errorf("mapping %d entries must be Sequence", index)
			}
			mappingEntries := make([]graph.MappingEntry, 0, entries.Len())
			for _, entryValue := range entries.Items() {
				entry, ok := entryValue.(*core.Object)
				if !ok {
					return nil, fmt.Errorf("mapping entry must be Object")
				}
				keyValue, ok := entry.Get("key")
				if !ok {
					return nil, fmt.Errorf("mapping entry key missing")
				}
				valueValue, ok := entry.Get("value")
				if !ok {
					return nil, fmt.Errorf("mapping entry value missing")
				}
				key, err := graphReference(keyValue, ids)
				if err != nil {
					return nil, err
				}
				value, err := graphReference(valueValue, ids)
				if err != nil {
					return nil, err
				}
				mappingEntries = append(mappingEntries, graph.MappingEntry{Key: key, Value: value})
			}
			if err := builder.DefineMapping(ids[index], tag, mappingEntries); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown graph node kind %q", kind)
		}
	}
	for _, root := range roots.Items() {
		rootID, err := graphReference(root, ids)
		if err != nil {
			return nil, err
		}
		if err := builder.PushRoot(rootID); err != nil {
			return nil, err
		}
	}
	return builder.Build()
}

// graphReference resolves one integer graph descriptor into its node ID.
func graphReference(value core.Value, ids []graph.NodeID) (graph.NodeID, error) {
	integer, ok := value.(core.Integer)
	if !ok {
		return graph.NodeID{}, fmt.Errorf("graph reference must be Integer")
	}
	number := integer.Int()
	if !number.IsInt64() || number.Sign() < 0 || number.Int64() >= int64(len(ids)) {
		return graph.NodeID{}, fmt.Errorf("graph reference out of range")
	}
	return ids[number.Int64()], nil
}

func graphPGCEVector(vector *caseData) (bool, error) {
	graphValue, ok := caseInput(vector, "graph")
	if !ok {
		return false, fmt.Errorf("missing input.graph")
	}
	built, err := graphFromVector(graphValue)
	if err != nil {
		return false, err
	}
	encoded, err := graph.EncodePGCE(built)
	if err != nil {
		return false, err
	}
	expected, ok := caseExpected(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing expected.hex")
	}
	expectedHex, ok := expected.(core.String)
	if !ok {
		return false, fmt.Errorf("expected.hex must be String")
	}
	if hex.EncodeToString(encoded) != string(expectedHex) {
		return false, fmt.Errorf("pgce hex %s != %s", hex.EncodeToString(encoded), string(expectedHex))
	}
	return true, nil
}

func graphEquality(vector *caseData) (bool, error) {
	leftValue, ok := caseInput(vector, "left")
	if !ok {
		return false, fmt.Errorf("missing input.left")
	}
	rightValue, ok := caseInput(vector, "right")
	if !ok {
		return false, fmt.Errorf("missing input.right")
	}
	left, err := graphFromVector(leftValue)
	if err != nil {
		return false, err
	}
	right, err := graphFromVector(rightValue)
	if err != nil {
		return false, err
	}
	strictEqual, _ := booleanField(vector.Expected, "strict_equal")
	pgceEqual, _ := booleanField(vector.Expected, "pgce_equal")
	hashEqual, hasHash := booleanField(vector.Expected, "strict_hash_equal")
	if graph.Equal(left, right) != strictEqual {
		return false, fmt.Errorf("strict equality differs")
	}
	leftBytes, err := graph.EncodePGCE(left)
	if err != nil {
		return false, err
	}
	rightBytes, err := graph.EncodePGCE(right)
	if err != nil {
		return false, err
	}
	if (string(leftBytes) == string(rightBytes)) != pgceEqual {
		return false, fmt.Errorf("pgce equality differs")
	}
	if hasHash && (graph.Hash(left) == graph.Hash(right)) != hashEqual {
		return false, fmt.Errorf("hash equality differs")
	}
	return true, nil
}

func graphPGCERoundtrip(vector *caseData) (bool, error) {
	graphValue, ok := caseInput(vector, "graph")
	if !ok {
		return false, fmt.Errorf("missing input.graph")
	}
	built, err := graphFromVector(graphValue)
	if err != nil {
		return false, err
	}
	bytes, err := graph.EncodePGCE(built)
	if err != nil {
		return false, err
	}
	decoded, err := graph.DecodePGCE(bytes, graph.DefaultPGCELimits())
	if err != nil {
		return false, err
	}
	strictEqual, _ := booleanField(vector.Expected, "strict_equal")
	byteStable, _ := booleanField(vector.Expected, "byte_stable")
	if !strictEqual || !byteStable {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if !graph.Equal(decoded, built) {
		return false, fmt.Errorf("decoded graph is not strictly equal")
	}
	reencoded, err := graph.EncodePGCE(decoded)
	if err != nil {
		return false, err
	}
	if string(reencoded) != string(bytes) {
		return false, fmt.Errorf("re-encode is not byte-stable")
	}
	return true, nil
}

func graphPGCERejection(vector *caseData) (bool, error) {
	text, ok := caseInput(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing input.hex")
	}
	hexText, ok := text.(core.String)
	if !ok {
		return false, fmt.Errorf("input.hex must be String")
	}
	bytes, err := hex.DecodeString(string(hexText))
	if err != nil {
		return false, fmt.Errorf("invalid hex")
	}
	_, err = graph.DecodePGCE(bytes, graph.DefaultPGCELimits())
	if err == nil {
		return false, fmt.Errorf("published rejection vector must fail")
	}
	pgceError, ok := err.(*graph.PGCEError)
	if !ok {
		return false, fmt.Errorf("unexpected error type: %v", err)
	}
	failure, ok := caseExpected(vector, "failure")
	if !ok {
		return false, fmt.Errorf("missing expected.failure")
	}
	failureName, ok := failure.(core.String)
	if !ok {
		return false, fmt.Errorf("expected.failure must be String")
	}
	if pgceErrorKindName(pgceError.Kind) != string(failureName) {
		return false, fmt.Errorf("failure %s != %s", pgceErrorKindName(pgceError.Kind), string(failureName))
	}
	return true, nil
}

func pgceErrorKindName(kind graph.PGCEErrorKind) string {
	switch kind {
	case graph.ErrNonMinimalVarint:
		return "NonMinimalVarint"
	case graph.ErrNonCanonicalNodeOrder:
		return "NonCanonicalNodeOrder"
	case graph.ErrResourceLimit:
		return "ResourceLimit"
	case graph.ErrInvalidMagic:
		return "InvalidMagic"
	case graph.ErrUnsupportedVersion:
		return "UnsupportedVersion"
	case graph.ErrUnexpectedEnd:
		return "UnexpectedEof"
	case graph.ErrTrailingBytes:
		return "TrailingBytes"
	case graph.ErrVarintOverflow:
		return "VarintOverflow"
	case graph.ErrUnknownNodeKind:
		return "UnknownNodeKind"
	case graph.ErrInvalidUTF8:
		return "InvalidUtf8"
	case graph.ErrInvalidTag:
		return "InvalidTag"
	case graph.ErrReferenceOutOfRange:
		return "ReferenceOutOfRange"
	case graph.ErrInvalidGraph:
		return "InvalidGraph"
	case graph.ErrNonCanonicalEncoding:
		return "NonCanonicalEncoding"
	}
	return "Unknown"
}

func graphQuery(vector *caseData) (bool, error) {
	graphValue, ok := caseInput(vector, "graph")
	if !ok {
		return false, fmt.Errorf("missing input.graph")
	}
	built, err := graphFromVector(graphValue)
	if err != nil {
		return false, err
	}
	pipelineValue, ok := caseInput(vector, "pipeline")
	if !ok {
		return false, fmt.Errorf("missing input.pipeline")
	}
	pipeline, ok := pipelineValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.pipeline must be Sequence")
	}
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, item := range pipeline.Items() {
		text, ok := item.(core.String)
		if !ok {
			return false, fmt.Errorf("pipeline descriptor must be String")
		}
		id, version, ok := strings.Cut(string(text), "@")
		if !ok {
			return false, fmt.Errorf("pipeline descriptor lacks version: %s", string(text))
		}
		var versionNumber uint64
		if _, err := fmt.Sscanf(version, "%d", &versionNumber); err != nil {
			return false, fmt.Errorf("invalid pipeline version: %s", string(text))
		}
		expression = expression.Then(protocol.NewOperatorCall(id, uint32(versionNumber)))
	}
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	definition := protocol.NewQueryDefinition(protocol.DomainPortableGraphV1()).
		WithExpression(expression)
	validated, failure := definition.Validate()
	if failure != nil {
		return false, failure
	}
	executable, failure := validated.Bind(capabilities)
	if failure != nil {
		return false, failure
	}
	matches, failure := executable.ExecuteGraph(built, protocol.DefaultQueryLimits())
	if failure != nil {
		return false, failure
	}
	expectedIDs, ok := caseExpected(vector, "builder_node_ids")
	if !ok {
		return false, fmt.Errorf("missing expected.builder_node_ids")
	}
	idsValue, ok := expectedIDs.(*core.Array)
	if !ok {
		return false, fmt.Errorf("expected.builder_node_ids must be Sequence")
	}
	if len(matches) != idsValue.Len() {
		return false, fmt.Errorf("match count %d != %d", len(matches), idsValue.Len())
	}
	for index, match := range matches {
		expected, ok := idsValue.At(index).(core.Integer)
		if !ok {
			return false, fmt.Errorf("expected id must be Integer")
		}
		if match.Kind != "Node" {
			return false, fmt.Errorf("query result was not a node")
		}
		if match.Node.AsUint64() != expected.Int().Uint64() {
			return false, fmt.Errorf("match %d id %d != %d", index, match.Node.AsUint64(), expected.Int().Uint64())
		}
	}
	return true, nil
}

func graphPGCEStreamLimit(vector *caseData) (bool, error) {
	graphValue, ok := caseInput(vector, "graph")
	if !ok {
		return false, fmt.Errorf("missing input.graph")
	}
	built, err := graphFromVector(graphValue)
	if err != nil {
		return false, err
	}
	limit, ok := integerFieldValue(vector.Input, "max_stream_bytes")
	if !ok {
		return false, fmt.Errorf("missing input.max_stream_bytes")
	}
	limits := graph.DefaultPGCELimits()
	limits.MaxStreamBytes = int(limit)
	_, err = graph.EncodePGCEBounded(built, limits)
	if err == nil {
		return false, fmt.Errorf("published resource vector must fail")
	}
	pgceError, ok := err.(*graph.PGCEError)
	if !ok || pgceError.Kind != graph.ErrResourceLimit {
		return false, fmt.Errorf("expected ResourceLimit")
	}
	failure, ok := caseExpected(vector, "failure")
	if !ok {
		return false, fmt.Errorf("missing expected.failure")
	}
	if string(failure.(core.String)) != "ResourceLimit" {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	limitName, ok := caseExpected(vector, "limit")
	if !ok {
		return false, fmt.Errorf("missing expected.limit")
	}
	if pgceError.Field != string(limitName.(core.String)) {
		return false, fmt.Errorf("limit %s != %s", pgceError.Field, string(limitName.(core.String)))
	}
	return true, nil
}
