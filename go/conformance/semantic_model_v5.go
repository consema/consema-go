package conformance

// The `consema.semantic-model-v5.conformance@1` suite runner
// (consema-rs/crates/consema-conformance/src/semantic_model_v5.rs). The v5 registry
// facts and the graph/YAML wire records are verified data-driven: every
// graph and expectation comes from the vector file.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/graph"
	"consema.dev/consema/protocol"
)

// runSemanticModelV5 executes the embedded v5 suite.
func runSemanticModelV5(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch vector.ID {
		case "registry.v5-manifest":
			_, err = semanticModelV5RegistryManifest(vector)
		case "registry.v1-v4-frozen":
			_, err = semanticModelV5RegistryFrozen(vector)
		case "registry.v5-additive-contracts":
			_, err = semanticModelV5RegistryAdditions(vector)
		case "registry.v5-error-codes":
			_, err = semanticModelV5RegistryErrorCodes(vector)
		case "portable-graph.dual-transport":
			_, err = semanticModelV5GraphTransport(vector)
		case "portable-graph.reject-disagreement":
			_, err = semanticModelV5GraphDisagreement(vector)
		case "portable-graph.reject-node-limit":
			_, err = semanticModelV5GraphLimit(vector)
		case "graph-query.node-roundtrip", "graph-query.sequence-roundtrip",
			"graph-query.mapping-roundtrip", "graph-query.reject-dangling-association":
			_, err = semanticModelV5GraphQuery(vector)
		case "graph-provenance.reject-order":
			_, err = semanticModelV5GraphProvenanceOrder(vector)
		case "graph-projection.roundtrip", "graph-projection.reject-out-of-range":
			_, err = semanticModelV5GraphProjection(vector)
		case "yaml-query.native-roles", "yaml-query.syntax-roundtrip":
			_, err = semanticModelV5YamlQueryRoundtrip(vector)
		case "yaml-query.reject-domain-role":
			_, err = semanticModelV5YamlDomainRejection(vector)
		case "yaml-query.reject-process-local":
			_, err = semanticModelV5YamlProcessLocal(vector)
		case "protocol.v4-reject-v5-contract":
			_, err = semanticModelV5ProtocolV4Rejection(vector)
		case "protocol.v5-nested-error-code":
			_, err = semanticModelV5ProtocolNestedError(vector)
		case "protocol.reject-truncated-pvce":
			_, err = semanticModelV5ProtocolTruncatedPVCE(vector)
		case "protocol.reject-unknown-payload-field":
			_, err = semanticModelV5ProtocolUnknownField(vector)
		default:
			err = fmt.Errorf("runner does not recognize published v5 case")
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

func semanticModelV5RegistryManifest(vector *caseData) (bool, error) {
	manifest, err := protocol.NewRegistryManifest(5,
		protocol.NewContractRegistry(protocol.RegistryV5),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV5))
	if err != nil {
		return false, err
	}
	value, err := manifest.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.RegistryManifest{}
	roundtripped, err := decoded.FromValue(value)
	if err != nil {
		return false, err
	}
	roundtripValue, err := roundtripped.ToValue()
	if err != nil {
		return false, err
	}
	if !core.Equal(roundtripValue, value) {
		return false, fmt.Errorf("manifest round-trip changed the record")
	}
	semanticModel, _ := stringField(vector.Expected, "semantic_model")
	contractCount, _ := integerField(vector.Expected, "contract_count")
	errorCodeCount, _ := integerField(vector.Expected, "error_code_count")
	if roundtripped.SemanticModel().Schema() != semanticModel ||
		uint64(len(roundtripped.Contracts())) != contractCount ||
		uint64(len(roundtripped.ErrorCodes())) != errorCodeCount {
		return false, fmt.Errorf("v5 manifest facts differ")
	}
	return true, nil
}

func semanticModelV5RegistryFrozen(vector *caseData) (bool, error) {
	contractCounts, _ := integerSequenceField(vector.Expected, "contract_counts")
	errorCounts, _ := integerSequenceField(vector.Expected, "error_code_counts")
	if len(contractCounts) != 4 || len(errorCounts) != 4 {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	for index := 0; index < 4; index++ {
		version := uint32(index + 1)
		manifest, err := protocol.NewRegistryManifest(version,
			protocol.NewContractRegistry(protocol.ContractRegistryVersion(index)),
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryVersion(index)))
		if err != nil {
			return false, err
		}
		value, err := manifest.ToValue()
		if err != nil {
			return false, err
		}
		decoded := &protocol.RegistryManifest{}
		if _, err := decoded.FromValue(value); err != nil {
			return false, err
		}
		if uint64(len(manifest.Contracts())) != contractCounts[index] ||
			uint64(len(manifest.ErrorCodes())) != errorCounts[index] ||
			manifest.IsCurrent() {
			return false, fmt.Errorf("a frozen registry changed")
		}
	}
	return true, nil
}

func integerSequenceField(value core.Value, name string) ([]uint64, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return nil, false
	}
	items, ok := field.(*core.Array)
	if !ok {
		return nil, false
	}
	output := make([]uint64, 0, items.Len())
	for _, item := range items.Items() {
		integer, ok := item.(core.Integer)
		if !ok {
			return nil, false
		}
		output = append(output, integer.Int().Uint64())
	}
	return output, true
}

func stringSequenceField(value core.Value, name string) ([]string, bool) {
	field, ok := objectField(value, name)
	if !ok {
		return nil, false
	}
	items, ok := field.(*core.Array)
	if !ok {
		return nil, false
	}
	output := make([]string, 0, items.Len())
	for _, item := range items.Items() {
		text, ok := item.(core.String)
		if !ok {
			return nil, false
		}
		output = append(output, string(text))
	}
	return output, true
}

func semanticModelV5RegistryAdditions(vector *caseData) (bool, error) {
	v4 := protocol.NewContractRegistry(protocol.RegistryV4)
	v5 := protocol.NewContractRegistry(protocol.RegistryV5)
	expected, _ := stringSequenceField(vector.Expected, "contracts")
	var actual []string
	for _, descriptor := range v5.Contracts() {
		inV4 := false
		for _, old := range v4.Contracts() {
			if old.ID == descriptor.ID && old.Version == descriptor.Version {
				inV4 = true
				break
			}
		}
		if !inV4 {
			actual = append(actual, descriptor.ID+"@"+fmt.Sprintf("%d", descriptor.Version))
		}
	}
	if !equalStrings(actual, expected) {
		return false, fmt.Errorf("v5 additions differ: %v", actual)
	}
	return true, nil
}

func semanticModelV5RegistryErrorCodes(vector *caseData) (bool, error) {
	v4 := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV4)
	v5 := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV5)
	expected, _ := stringSequenceField(vector.Expected, "new_codes")
	var actual []string
	for _, descriptor := range v5.Codes() {
		if !v4.Contains(descriptor.Code) {
			actual = append(actual, descriptor.Code)
		}
	}
	errorCodeCount, _ := integerField(vector.Expected, "error_code_count")
	if uint64(len(v5.Codes())) != errorCodeCount || !equalStrings(actual, expected) {
		return false, fmt.Errorf("v5 error additions differ: %v", actual)
	}
	return true, nil
}

func equalStrings(left, right []string) bool {
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

// v5GraphFromVector builds one graph from the vector input descriptor.
func v5GraphFromVector(vector *caseData) (*graph.Graph, error) {
	graphValue, ok := caseInput(vector, "graph")
	if !ok {
		return nil, fmt.Errorf("missing input.graph")
	}
	return graphFromVector(graphValue)
}

// v5GraphMessage builds the portable-graph wire record.
func v5GraphMessage(g *graph.Graph) (*protocol.PortableGraphMessage, error) {
	return protocol.NewPortableGraphMessageFromGraph(g, graph.DefaultPGCELimits())
}

// v5Envelope builds one v5-registry protocol message.
func v5Envelope(schema string, payload core.Value) (*protocol.ProtocolMessage, error) {
	contract, err := parseContractSchema(schema)
	if err != nil {
		return nil, err
	}
	return protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV5))
}

func semanticModelV5GraphTransport(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	pgceHex, _ := stringField(vector.Expected, "pgce_hex")
	if hex.EncodeToString(message.PGCE()) != pgceHex {
		return false, fmt.Errorf("pgce hex %s != %s", hex.EncodeToString(message.PGCE()), pgceHex)
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	envelope, err := v5Envelope("core.portable-graph@1", payload)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	jsonBytes, err := envelope.ToJSON(limits)
	if err != nil {
		return false, err
	}
	pvceBytes, err := envelope.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV5)
	decodedJSON, err := envelope.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		return false, err
	}
	decodedPVCE, err := envelope.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		return false, err
	}
	if !core.Equal(decodedJSON.Payload(), envelope.Payload()) ||
		!core.Equal(decodedPVCE.Payload(), envelope.Payload()) {
		return false, fmt.Errorf("transport identity differed")
	}
	jsonSHA256, _ := stringField(vector.Expected, "json_sha256")
	pvceSHA256, _ := stringField(vector.Expected, "pvce_sha256")
	jsonDigest := sha256.Sum256(jsonBytes)
	pvceDigest := sha256.Sum256(pvceBytes)
	if hex.EncodeToString(jsonDigest[:]) != jsonSHA256 ||
		hex.EncodeToString(pvceDigest[:]) != pvceSHA256 {
		return false, fmt.Errorf("transport digests differed")
	}
	return true, nil
}

func semanticModelV5GraphDisagreement(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	nodesValue, ok := objectField(payload, "nodes")
	if !ok {
		return false, fmt.Errorf("nodes field missing")
	}
	nodes, ok := nodesValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("nodes must be Sequence")
	}
	index, ok := integerFieldValue(vector.Input, "node_index")
	if !ok {
		return false, fmt.Errorf("missing input.node_index")
	}
	replacement, ok := caseInput(vector, "replacement")
	if !ok {
		return false, fmt.Errorf("missing input.replacement")
	}
	replacementText, ok := replacement.(core.String)
	if !ok {
		return false, fmt.Errorf("input.replacement must be String")
	}
	changedNodes := make([]core.Value, 0, nodes.Len())
	for ordinal, nodeValue := range nodes.Items() {
		if uint64(ordinal) == index {
			node, err := replaceObjectField(nodeValue, "canonical_content", replacementText)
			if err != nil {
				return false, err
			}
			changedNodes = append(changedNodes, node)
		} else {
			changedNodes = append(changedNodes, nodeValue)
		}
	}
	changed, err := replaceObjectField(payload, "nodes", core.NewArray(changedNodes...))
	if err != nil {
		return false, err
	}
	decoded := &protocol.PortableGraphMessage{}
	_, err = decoded.FromValue(changed, graph.DefaultPGCELimits())
	if err == nil {
		return false, fmt.Errorf("readable and PGCE forms unexpectedly agreed")
	}
	return false, expectErrorCode(vector, err)
}

func semanticModelV5GraphLimit(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	limit, ok := integerFieldValue(vector.Input, "max_nodes")
	if !ok {
		return false, fmt.Errorf("missing input.max_nodes")
	}
	limits := graph.DefaultPGCELimits()
	limits.MaxNodes = int(limit)
	decoded := &protocol.PortableGraphMessage{}
	_, err = decoded.FromValue(payload, limits)
	return false, expectErrorCode(vector, err)
}

// v5ParseRole parses one match-role spelling from the vector.
func v5ParseRole(text string) (protocol.MatchRole, error) {
	role, ok := protocol.ParseMatchRole(text)
	if !ok {
		return "", fmt.Errorf("unknown match role %q", text)
	}
	return role, nil
}

// v5GraphMatchFromVector parses one graph match descriptor.
func v5GraphMatchFromVector(value core.Value) (protocol.GraphQueryMatchMessage, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return protocol.GraphQueryMatchMessage{}, fmt.Errorf("match must be Object")
	}
	kind, ok := stringField(object, "kind")
	if !ok {
		return protocol.GraphQueryMatchMessage{}, fmt.Errorf("match kind missing")
	}
	read := func(name string) (uint64, error) {
		number, ok := integerField(object, name)
		if !ok {
			return 0, fmt.Errorf("match %s missing", name)
		}
		return number, nil
	}
	switch kind {
	case "Node":
		node, err := read("node")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		return protocol.GraphQueryMatchMessage{Kind: "Node", Node: node}, nil
	case "SequenceElement":
		parent, err := read("parent")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		ordinal, err := read("ordinal")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		node, err := read("node")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		return protocol.GraphQueryMatchMessage{Kind: "SequenceElement", Parent: parent, Ordinal: ordinal, Node: node}, nil
	case "MappingEntry":
		parent, err := read("parent")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		ordinal, err := read("ordinal")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		key, err := read("key")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		value, err := read("value")
		if err != nil {
			return protocol.GraphQueryMatchMessage{}, err
		}
		return protocol.GraphQueryMatchMessage{Kind: "MappingEntry", Parent: parent, Ordinal: ordinal, Key: key, Value: value}, nil
	}
	return protocol.GraphQueryMatchMessage{}, fmt.Errorf("unknown graph match kind %q", kind)
}

func semanticModelV5GraphQuery(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	roleText, ok := caseInput(vector, "role")
	if !ok {
		return false, fmt.Errorf("missing input.role")
	}
	role, err := v5ParseRole(string(roleText.(core.String)))
	if err != nil {
		return false, err
	}
	matchValue, ok := caseInput(vector, "match")
	if !ok {
		return false, fmt.Errorf("missing input.match")
	}
	match, err := v5GraphMatchFromVector(matchValue)
	if err != nil {
		return false, err
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return false, err
	}
	result, err := protocol.NewGraphQueryResultMessage(protocol.DomainPortableGraphV1(),
		role, message, []protocol.GraphQueryMatchMessage{match}, completion, nil)
	accepted, _ := booleanField(vector.Expected, "accepted")
	if err != nil {
		if accepted {
			return false, err
		}
		return false, expectErrorCode(vector, err)
	}
	if !accepted {
		return false, fmt.Errorf("expected rejection")
	}
	value, err := result.ToValue()
	if err != nil {
		return false, err
	}
	return true, v5DualRoundtrip("core.graph-query-result@1", value)
}

// v5ProvenanceEntriesFromVector builds the graph provenance entries from
// the vector input.
func v5ProvenanceEntriesFromVector(vector *caseData) ([]protocol.GraphProvenanceEntryMessage, error) {
	locationsValue, ok := caseInput(vector, "locations")
	if !ok {
		return nil, fmt.Errorf("missing input.locations")
	}
	locations, ok := locationsValue.(*core.Array)
	if !ok {
		return nil, fmt.Errorf("input.locations must be Sequence")
	}
	sourceID, _ := stringField(vector.Input, "source_id")
	nodeLocator, _ := stringField(vector.Input, "node_locator")
	startByte, _ := integerFieldValue(vector.Input, "start_byte")
	endByte, _ := integerFieldValue(vector.Input, "end_byte")
	relationName, _ := stringField(vector.Input, "relation")
	var relation protocol.GraphProvenanceRelationMessage
	switch relationName {
	case "Direct":
		relation = protocol.GraphRelationDirect
	case "Reference":
		relation = protocol.GraphRelationReference
	default:
		return nil, fmt.Errorf("unknown graph provenance relation %q", relationName)
	}
	entries := make([]protocol.GraphProvenanceEntryMessage, 0, locations.Len())
	for _, locationValue := range locations.Items() {
		object, ok := locationValue.(*core.Object)
		if !ok {
			return nil, fmt.Errorf("location must be Object")
		}
		kind, ok := stringField(object, "kind")
		if !ok {
			return nil, fmt.Errorf("location kind missing")
		}
		var projected protocol.GraphProjectedLocationMessage
		switch kind {
		case "Root":
			ordinal, ok := integerField(object, "ordinal")
			if !ok {
				return nil, fmt.Errorf("root ordinal missing")
			}
			projected = protocol.GraphProjectedLocationMessage{Kind: "Root", Ordinal: ordinal}
		case "Node":
			node, ok := integerField(object, "node")
			if !ok {
				return nil, fmt.Errorf("node id missing")
			}
			projected = protocol.GraphProjectedLocationMessage{Kind: "Node", Node: node}
		case "SequenceElement", "MappingKey", "MappingValue":
			parent, ok := integerField(object, "parent")
			if !ok {
				return nil, fmt.Errorf("parent id missing")
			}
			ordinal, ok := integerField(object, "ordinal")
			if !ok {
				return nil, fmt.Errorf("ordinal missing")
			}
			projected = protocol.GraphProjectedLocationMessage{Kind: kind, Parent: parent, Ordinal: ordinal}
		default:
			return nil, fmt.Errorf("unknown projected graph location %q", kind)
		}
		origin, err := protocol.NewGraphSourceOriginMessage(sourceID,
			strPtr(nodeLocator), startByte, endByte, relation)
		if err != nil {
			return nil, err
		}
		entries = append(entries, protocol.GraphProvenanceEntryMessage{
			Projected: projected,
			Origins:   []protocol.GraphSourceOriginMessage{*origin},
		})
	}
	return entries, nil
}

func semanticModelV5GraphProvenanceOrder(vector *caseData) (bool, error) {
	entries, err := v5ProvenanceEntriesFromVector(vector)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewGraphProvenanceMapMessage(entries)
	return false, expectErrorCode(vector, err)
}

func semanticModelV5GraphProjection(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	entries, err := v5ProvenanceEntriesFromVector(vector)
	if err != nil {
		return false, err
	}
	provenance, err := protocol.NewGraphProvenanceMapMessage(entries)
	if err != nil {
		return false, err
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return false, err
	}
	result, err := protocol.NewGraphProjectionResultMessage(completion, message, true, provenance, nil)
	accepted, _ := booleanField(vector.Expected, "accepted")
	if err != nil {
		if accepted {
			return false, err
		}
		return false, expectErrorCode(vector, err)
	}
	if !accepted {
		return false, fmt.Errorf("expected rejection")
	}
	value, err := result.ToValue()
	if err != nil {
		return false, err
	}
	return true, v5DualRoundtrip("core.graph-projection-result@1", value)
}

func semanticModelV5YamlQueryRoundtrip(vector *caseData) (bool, error) {
	rolesValue, ok := caseInput(vector, "roles")
	if !ok {
		return false, fmt.Errorf("missing input.roles")
	}
	roles, ok := rolesValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.roles must be Sequence")
	}
	sourceID, _ := stringField(vector.Input, "source_id")
	count := 0
	for ordinal, roleValue := range roles.Items() {
		text, ok := roleValue.(core.String)
		if !ok {
			return false, fmt.Errorf("role must be String")
		}
		role, err := v5ParseRole(string(text))
		if err != nil {
			return false, err
		}
		var domain *protocol.QueryDomain
		if role == protocol.RoleYamlSyntaxPiece {
			domain = protocol.NewQueryDomain("yaml.lossless-syntax-query", 1)
		} else {
			domain = protocol.NewQueryDomain("yaml.native-semantic-query", 1)
		}
		locator, err := protocol.NewYamlMatchLocator(sourceID,
			fmt.Sprintf("/nodes/%d", ordinal), role, uint64(ordinal))
		if err != nil {
			return false, err
		}
		completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
		if err != nil {
			return false, err
		}
		result, err := protocol.NewYamlQueryResultMessage(domain, role,
			[]*protocol.YamlMatchLocator{locator}, completion, nil)
		if err != nil {
			return false, err
		}
		value, err := result.ToValue()
		if err != nil {
			return false, err
		}
		if err := v5DualRoundtrip("core.yaml-query-result@1", value); err != nil {
			return false, err
		}
		count++
	}
	roleCount, _ := integerField(vector.Expected, "role_count")
	if uint64(count) != roleCount {
		return false, fmt.Errorf("role count differed")
	}
	return true, nil
}

func semanticModelV5YamlDomainRejection(vector *caseData) (bool, error) {
	roleText, ok := caseInput(vector, "role")
	if !ok {
		return false, fmt.Errorf("missing input.role")
	}
	role, err := v5ParseRole(string(roleText.(core.String)))
	if err != nil {
		return false, err
	}
	locator, err := protocol.NewYamlMatchLocator("sha256:source", "/syntax/0", role, 0)
	if err != nil {
		return false, err
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewYamlQueryResultMessage(
		protocol.NewQueryDomain("yaml.native-semantic-query", 1), role,
		[]*protocol.YamlMatchLocator{locator}, completion, nil)
	return false, expectErrorCode(vector, err)
}

func semanticModelV5YamlProcessLocal(vector *caseData) (bool, error) {
	return false, expectErrorCode(vector, protocol.YamlMatchLocatorFromProcessLocal())
}

func semanticModelV5ProtocolV4Rejection(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	contract, err := parseContractSchema("core.portable-graph@1")
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV4))
	return false, expectErrorCode(vector, err)
}

func semanticModelV5ProtocolNestedError(vector *caseData) (bool, error) {
	codeText, ok := caseInput(vector, "failure_code")
	if !ok {
		return false, fmt.Errorf("missing input.failure_code")
	}
	code := string(codeText.(core.String))
	v4Code, _ := stringField(vector.Expected, "v4_code")
	v4Completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed, 1, 0,
		nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV4))
	_ = v4Completion
	if err == nil {
		return false, fmt.Errorf("v4 accepted a v5 diagnostic code")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Code() != v4Code {
		return false, fmt.Errorf("v4 nested rejection %v != %s", err, v4Code)
	}
	completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed, 1, 0,
		nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV5))
	if err != nil {
		return false, err
	}
	payload, err := completion.ToValue()
	if err != nil {
		return false, err
	}
	envelope, err := v5Envelope("core.completion@1", payload)
	if err != nil {
		return false, err
	}
	envelopeValue, err := envelope.ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.ProtocolMessage{}
	decodedEnvelope, err := decoded.FromValue(envelopeValue, protocol.NewContractRegistry(protocol.RegistryV5))
	if err != nil {
		return false, err
	}
	if !core.Equal(decodedEnvelope.Payload(), envelope.Payload()) {
		return false, fmt.Errorf("selected nested registry behavior differed")
	}
	return true, nil
}

func semanticModelV5ProtocolTruncatedPVCE(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	envelope, err := v5Envelope("core.portable-graph@1", payload)
	if err != nil {
		return false, err
	}
	bytes, err := envelope.ToPVCE(protocol.DefaultProtocolLimits())
	if err != nil {
		return false, err
	}
	truncate, ok := integerFieldValue(vector.Input, "truncate_bytes")
	if !ok {
		return false, fmt.Errorf("missing input.truncate_bytes")
	}
	cut := len(bytes) - int(truncate)
	if cut < 0 {
		cut = 0
	}
	_, err = envelope.FromPVCE(bytes[:cut], protocol.DefaultProtocolLimits(),
		protocol.NewContractRegistry(protocol.RegistryV5))
	return false, expectErrorCode(vector, err)
}

func semanticModelV5ProtocolUnknownField(vector *caseData) (bool, error) {
	built, err := v5GraphFromVector(vector)
	if err != nil {
		return false, err
	}
	message, err := v5GraphMessage(built)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	changed, err := appendObjectField(payload, "unknown", core.NullValue())
	if err != nil {
		return false, err
	}
	contract, err := parseContractSchema("core.portable-graph@1")
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, changed, protocol.NewContractRegistry(protocol.RegistryV5))
	return false, expectErrorCode(vector, err)
}

// appendObjectField rebuilds an Object with one field appended.
func appendObjectField(value core.Value, name string, appended core.Value) (core.Value, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, fmt.Errorf("value must be Object")
	}
	entries := append([]core.Entry(nil), object.Entries()...)
	entries = append(entries, core.Entry{Key: name, Value: appended})
	return core.NewObject(entries...)
}

// v5DualRoundtrip proves JSON/PVCE transport identity under the v5
// registry.
func v5DualRoundtrip(schema string, payload core.Value) error {
	envelope, err := v5Envelope(schema, payload)
	if err != nil {
		return err
	}
	limits := protocol.DefaultProtocolLimits()
	jsonBytes, err := envelope.ToJSON(limits)
	if err != nil {
		return err
	}
	pvceBytes, err := envelope.ToPVCE(limits)
	if err != nil {
		return err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV5)
	decodedJSON, err := envelope.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		return err
	}
	decodedPVCE, err := envelope.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		return err
	}
	if !core.Equal(decodedJSON.Payload(), envelope.Payload()) ||
		!core.Equal(decodedPVCE.Payload(), envelope.Payload()) {
		return fmt.Errorf("dual transport did not close")
	}
	return nil
}
