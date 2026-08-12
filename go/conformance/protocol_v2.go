package conformance

// The `consema.protocol.conformance@2` suite runner
// (crates/consema-conformance/src/protocol_v2.rs). The vector input drives
// every snapshot/patch construction; the vector expected facts drive every
// assertion.

import (
	"encoding/hex"
	"fmt"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runProtocolV2 executes the embedded semantic-model v2 protocol suite.
func runProtocolV2(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch vector.ID {
		case "protocol.v2.registry-manifest":
			_, err = protocolV2Registry(vector, false)
		case "protocol.v2.registry-v1-frozen":
			_, err = protocolV2Registry(vector, true)
		case "protocol.v2.error-code-manifest":
			_, err = protocolV2ErrorManifest(vector)
		case "protocol.v2.snapshot-dual-transport":
			_, err = protocolV2SnapshotTransport(vector)
		case "protocol.v2.patch-dual-transport":
			_, err = protocolV2PatchTransport(vector)
		case "protocol.v2.reject-source-under-v1":
			_, err = protocolV2RejectSourceV1(vector)
		case "protocol.v2.reject-forged-digest":
			_, err = protocolV2RejectForgedDigest(vector)
		case "protocol.v2.reject-forged-encoding":
			_, err = protocolV2RejectForgedEncoding(vector)
		case "protocol.v2.snapshot-resource-limit":
			_, err = protocolV2SnapshotResourceLimit(vector)
		case "protocol.v2.patch-resource-limit":
			_, err = protocolV2PatchResourceLimit(vector)
		case "protocol.v2.patch-stale-after-wire":
			_, err = protocolV2PatchStaleAfterWire(vector)
		default:
			err = fmt.Errorf("runner does not recognize published protocol v2 case")
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

// v2EncodingFromName parses one v1-style lowercase encoding name into the
// canonical Kind spelling.
func v2EncodingFromName(name string) (*protocol.SourceEncoding, error) {
	switch name {
	case "binary":
		return &protocol.SourceEncoding{Kind: "Binary"}, nil
	case "utf-8":
		return &protocol.SourceEncoding{Kind: "Utf8"}, nil
	case "utf-16le":
		return &protocol.SourceEncoding{Kind: "Utf16Le"}, nil
	case "utf-16be":
		return &protocol.SourceEncoding{Kind: "Utf16Be"}, nil
	case "latin-1":
		return &protocol.SourceEncoding{Kind: "Latin1"}, nil
	}
	return nil, fmt.Errorf("unknown encoding %q", name)
}

func protocolV2Registry(vector *caseData, frozenV1 bool) (bool, error) {
	var manifest *protocol.RegistryManifest
	var registry protocol.ContractRegistry
	var err error
	if frozenV1 {
		manifest, err = protocol.NewRegistryManifest(1,
			protocol.NewContractRegistry(protocol.RegistryV1),
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1))
		registry = protocol.NewContractRegistry(protocol.RegistryV1)
	} else {
		manifest, err = protocol.NewRegistryManifest(2,
			protocol.NewContractRegistry(protocol.RegistryV2),
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV2))
		registry = protocol.NewContractRegistry(protocol.RegistryV2)
	}
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
	recognizes, _ := booleanField(vector.Expected, "recognizes_source_snapshot")
	isCurrent, _ := booleanField(vector.Expected, "is_current")
	sourceContract, err := protocol.NewContractId("core.source-snapshot", 1)
	if err != nil {
		return false, err
	}
	if roundtripped.SemanticModel().Schema() != semanticModel ||
		uint64(len(roundtripped.Contracts())) != contractCount ||
		uint64(len(roundtripped.ErrorCodes())) != errorCodeCount ||
		registry.Recognizes(sourceContract) != recognizes {
		return false, fmt.Errorf("registry facts differ")
	}
	// The v2 suite binds is_current to the v2 manifest exactly, not to a
	// later library model.
	if !frozenV1 {
		v2Manifest, err := protocol.NewRegistryManifest(2,
			protocol.NewContractRegistry(protocol.RegistryV2),
			protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV2))
		if err != nil {
			return false, err
		}
		v2Value, err := v2Manifest.ToValue()
		if err != nil {
			return false, err
		}
		if core.Equal(roundtripValue, v2Value) != isCurrent {
			return false, fmt.Errorf("is_current fact differs")
		}
	} else if isCurrent {
		return false, fmt.Errorf("v1 manifest must not be current")
	}
	return true, nil
}

func protocolV2ErrorManifest(vector *caseData) (bool, error) {
	manifest, err := protocol.ErrorCodeManifestValueForVersion(protocol.ErrorRegistryV2)
	if err != nil {
		return false, err
	}
	if err := protocol.ValidateErrorCodeManifestValue(manifest); err != nil {
		return false, err
	}
	countValue, ok := objectField(manifest, "error_codes")
	if !ok {
		return false, fmt.Errorf("error_codes field missing")
	}
	count, ok := countValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("error_codes must be Sequence")
	}
	expectedCount, _ := integerField(vector.Expected, "error_code_count")
	requiredCode, _ := stringField(vector.Expected, "required_code")
	v2Registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV2)
	v1Registry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV1)
	if uint64(count.Len()) != expectedCount || !v2Registry.Contains(requiredCode) ||
		v1Registry.Contains(requiredCode) {
		return false, fmt.Errorf("error-code manifest facts differ")
	}
	return true, nil
}

// v2SnapshotFromVector builds one source snapshot from the vector input.
func v2SnapshotFromVector(vector *caseData, field string) (*protocol.SourceSnapshot, error) {
	rawHex, ok := caseInput(vector, field)
	if !ok {
		return nil, fmt.Errorf("missing input.%s", field)
	}
	hexText, ok := rawHex.(core.String)
	if !ok {
		return nil, fmt.Errorf("input.%s must be String", field)
	}
	raw, err := hex.DecodeString(string(hexText))
	if err != nil {
		return nil, fmt.Errorf("invalid hex in input.%s", field)
	}
	encodingName, ok := caseInput(vector, "encoding")
	if !ok {
		return nil, fmt.Errorf("missing input.encoding")
	}
	encoding, err := v2EncodingFromName(string(encodingName.(core.String)))
	if err != nil {
		return nil, err
	}
	return protocol.NewSourceSnapshotFromRaw(raw,
		protocol.NewEncodingRequest(encoding), protocol.DefaultSourceLimits())
}

// v2ReplacementsFromVector builds the replacement records from the vector
// input.
func v2ReplacementsFromVector(vector *caseData) ([]protocol.SourceReplacement, error) {
	replacementsValue, ok := caseInput(vector, "replacements")
	if !ok {
		return nil, fmt.Errorf("missing input.replacements")
	}
	replacements, ok := replacementsValue.(*core.Array)
	if !ok {
		return nil, fmt.Errorf("input.replacements must be Sequence")
	}
	output := make([]protocol.SourceReplacement, 0, replacements.Len())
	for _, item := range replacements.Items() {
		object, ok := item.(*core.Object)
		if !ok {
			return nil, fmt.Errorf("replacement must be Object")
		}
		oldStart, ok := integerField(object, "old_start")
		if !ok {
			return nil, fmt.Errorf("replacement old_start missing")
		}
		oldEnd, ok := integerField(object, "old_end")
		if !ok {
			return nil, fmt.Errorf("replacement old_end missing")
		}
		originalHex, ok := object.Get("original_hex")
		if !ok {
			return nil, fmt.Errorf("replacement original_hex missing")
		}
		original, err := hex.DecodeString(string(originalHex.(core.String)))
		if err != nil {
			return nil, err
		}
		replacementHex, ok := object.Get("replacement_hex")
		if !ok {
			return nil, fmt.Errorf("replacement replacement_hex missing")
		}
		replacement, err := hex.DecodeString(string(replacementHex.(core.String)))
		if err != nil {
			return nil, err
		}
		output = append(output, protocol.SourceReplacement{
			OldStart:    oldStart,
			OldEnd:      oldEnd,
			Original:    original,
			Replacement: replacement,
		})
	}
	return output, nil
}

func protocolV2SnapshotTransport(vector *caseData) (bool, error) {
	snapshot, err := v2SnapshotFromVector(vector, "raw_hex")
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV2)
	contract, err := protocol.NewContractId("core.source-snapshot", 1)
	if err != nil {
		return false, err
	}
	envelope, err := protocol.NewProtocolMessage(contract, payload, registry)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	jsonBytes, err := envelope.ToJSON(limits)
	if err != nil {
		return false, err
	}
	decodedJSON, err := envelope.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		return false, err
	}
	pvceBytes, err := envelope.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	decodedPVCE, err := envelope.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		return false, err
	}
	jsonEqual, _ := booleanField(vector.Expected, "json_equal")
	pvceEqual, _ := booleanField(vector.Expected, "pvce_equal")
	expectedDigest, _ := stringField(vector.Expected, "digest")
	decodedMessage := &protocol.SourceSnapshotMessageV1{}
	decoded, err := decodedMessage.FromValue(decodedJSON.Payload(), protocol.DefaultSourceLimits())
	if err != nil {
		return false, err
	}
	if !jsonEqual || !pvceEqual {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if !core.Equal(decodedJSON.Payload(), envelope.Payload()) ||
		!core.Equal(decodedPVCE.Payload(), envelope.Payload()) {
		return false, fmt.Errorf("dual transport did not close")
	}
	if snapshot.Digest().Hex() != expectedDigest {
		return false, fmt.Errorf("digest %s != %s", snapshot.Digest().Hex(), expectedDigest)
	}
	if string(decoded.Snapshot().Bytes()) != string(snapshot.Bytes()) {
		return false, fmt.Errorf("decoded snapshot differs")
	}
	return true, nil
}

func protocolV2PatchTransport(vector *caseData) (bool, error) {
	base, err := v2SnapshotFromVector(vector, "base_hex")
	if err != nil {
		return false, err
	}
	replacements, err := v2ReplacementsFromVector(vector)
	if err != nil {
		return false, err
	}
	patch, err := protocol.NewSourcePatch(base, replacements, map[string]string{"actor": "protocol-v2"},
		protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourcePatchMessageV1FromPatch(patch)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	registry := protocol.NewContractRegistry(protocol.RegistryV2)
	contract, err := protocol.NewContractId("core.source-patch", 1)
	if err != nil {
		return false, err
	}
	envelope, err := protocol.NewProtocolMessage(contract, payload, registry)
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	jsonBytes, err := envelope.ToJSON(limits)
	if err != nil {
		return false, err
	}
	decodedJSON, err := envelope.FromJSON(jsonBytes, limits, registry)
	if err != nil {
		return false, err
	}
	pvceBytes, err := envelope.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	decodedPVCE, err := envelope.FromPVCE(pvceBytes, limits, registry)
	if err != nil {
		return false, err
	}
	jsonEqual, _ := booleanField(vector.Expected, "json_equal")
	pvceEqual, _ := booleanField(vector.Expected, "pvce_equal")
	targetHex, _ := stringField(vector.Expected, "target_hex")
	decodedMessage := &protocol.SourcePatchMessageV1{}
	decoded, err := decodedMessage.FromValue(decodedJSON.Payload(), protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	if !jsonEqual || !pvceEqual {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	if !core.Equal(decodedJSON.Payload(), envelope.Payload()) ||
		!core.Equal(decodedPVCE.Payload(), envelope.Payload()) {
		return false, fmt.Errorf("dual transport did not close")
	}
	target, err := protocol.ApplySourcePatch(decoded.Patch(), base, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	if hex.EncodeToString(target) != targetHex {
		return false, fmt.Errorf("target hex %s != %s", hex.EncodeToString(target), targetHex)
	}
	return true, nil
}

func protocolV2RejectSourceV1(vector *caseData) (bool, error) {
	snapshot, err := v2SnapshotFromVector(vector, "raw_hex")
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	contract, err := protocol.NewContractId("core.source-snapshot", 1)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, payload, protocol.NewContractRegistry(protocol.RegistryV1))
	return false, expectErrorCode(vector, err)
}

func protocolV2RejectForgedDigest(vector *caseData) (bool, error) {
	snapshot, err := v2SnapshotFromVector(vector, "raw_hex")
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	forged, err := replaceObjectField(payload, "digest",
		replacementDigestValue(stringsRepeat("00", 32)))
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourceSnapshotMessageV1{}
	_, err = decoded.FromValue(forged, protocol.DefaultSourceLimits())
	return false, expectErrorCode(vector, err)
}

func protocolV2RejectForgedEncoding(vector *caseData) (bool, error) {
	snapshot, err := v2SnapshotFromVector(vector, "raw_hex")
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	encodingValue, ok := objectField(payload, "encoding")
	if !ok {
		return false, fmt.Errorf("encoding field missing")
	}
	forgedName, ok := caseInput(vector, "forged_selected")
	if !ok {
		return false, fmt.Errorf("missing input.forged_selected")
	}
	name, ok := forgedName.(core.String)
	if !ok {
		return false, fmt.Errorf("input.forged_selected must be String")
	}
	forgedEncoding, err := replaceObjectField(encodingValue, "selected", name)
	if err != nil {
		return false, err
	}
	forged, err := replaceObjectField(payload, "encoding", forgedEncoding)
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourceSnapshotMessageV1{}
	_, err = decoded.FromValue(forged, protocol.DefaultSourceLimits())
	return false, expectErrorCode(vector, err)
}

func protocolV2SnapshotResourceLimit(vector *caseData) (bool, error) {
	snapshot, err := v2SnapshotFromVector(vector, "raw_hex")
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(snapshot)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	limit, ok := integerFieldValue(vector.Input, "max_raw_bytes")
	if !ok {
		return false, fmt.Errorf("missing input.max_raw_bytes")
	}
	limits := protocol.DefaultSourceLimits()
	limits.MaxRawBytes = int(limit)
	decoded := &protocol.SourceSnapshotMessageV1{}
	_, err = decoded.FromValue(payload, limits)
	return false, expectErrorCode(vector, err)
}

func protocolV2PatchResourceLimit(vector *caseData) (bool, error) {
	base, err := v2SnapshotFromVector(vector, "base_hex")
	if err != nil {
		return false, err
	}
	replacements, err := v2ReplacementsFromVector(vector)
	if err != nil {
		return false, err
	}
	patch, err := protocol.NewSourcePatch(base, replacements, nil, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourcePatchMessageV1FromPatch(patch)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	limit, ok := integerFieldValue(vector.Input, "max_replacements")
	if !ok {
		return false, fmt.Errorf("missing input.max_replacements")
	}
	limits := protocol.DefaultSourcePatchLimits()
	limits.MaxReplacements = int(limit)
	decoded := &protocol.SourcePatchMessageV1{}
	_, err = decoded.FromValue(payload, limits)
	return false, expectErrorCode(vector, err)
}

func protocolV2PatchStaleAfterWire(vector *caseData) (bool, error) {
	base, err := v2SnapshotFromVector(vector, "base_hex")
	if err != nil {
		return false, err
	}
	replacements, err := v2ReplacementsFromVector(vector)
	if err != nil {
		return false, err
	}
	patch, err := protocol.NewSourcePatch(base, replacements, nil, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	message, err := protocol.NewSourcePatchMessageV1FromPatch(patch)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	transported, err := protocol.EncodePVCE(payload, limits)
	if err != nil {
		return false, err
	}
	transportedValue, err := protocol.DecodePVCE(transported, limits)
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourcePatchMessageV1{}
	decodedMessage, err := decoded.FromValue(transportedValue, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	staleHex, ok := caseInput(vector, "stale_hex")
	if !ok {
		return false, fmt.Errorf("missing input.stale_hex")
	}
	staleBytes, err := hex.DecodeString(string(staleHex.(core.String)))
	if err != nil {
		return false, err
	}
	encodingName, ok := caseInput(vector, "encoding")
	if !ok {
		return false, fmt.Errorf("missing input.encoding")
	}
	encoding, err := v2EncodingFromName(string(encodingName.(core.String)))
	if err != nil {
		return false, err
	}
	stale, err := protocol.NewSourceSnapshotFromRaw(staleBytes,
		protocol.NewEncodingRequest(encoding), protocol.DefaultSourceLimits())
	if err != nil {
		return false, err
	}
	_, err = protocol.ApplySourcePatch(decodedMessage.Patch(), stale, protocol.DefaultSourcePatchLimits())
	if err == nil {
		return false, fmt.Errorf("stale base must be rejected")
	}
	expectedCode, _ := stringField(vector.Expected, "code")
	applyError, ok := err.(*protocol.SourcePatchApplyError)
	if !ok || applyError.Code() != expectedCode {
		return false, fmt.Errorf("apply error %v != %s", err, expectedCode)
	}
	return true, nil
}

// replaceObjectField rebuilds an Object with one field replaced.
func replaceObjectField(value core.Value, name string, replacement core.Value) (core.Value, error) {
	object, ok := value.(*core.Object)
	if !ok {
		return nil, fmt.Errorf("value must be Object")
	}
	found := false
	entries := make([]core.Entry, 0, object.Len())
	for _, entry := range object.Entries() {
		if entry.Key == name {
			entry.Value = replacement
			found = true
		}
		entries = append(entries, entry)
	}
	if !found {
		return nil, fmt.Errorf("field %s is absent", name)
	}
	return core.NewObject(entries...)
}

// replacementDigestValue builds a {algorithm, hex} digest record.
func replacementDigestValue(hexText string) core.Value {
	value, _ := core.NewObject(
		core.Entry{Key: "algorithm", Value: core.String("sha256")},
		core.Entry{Key: "hex", Value: core.String(hexText)},
	)
	return value
}

// stringsRepeat repeats one string.
func stringsRepeat(text string, count int) string {
	output := ""
	for range count {
		output += text
	}
	return output
}
