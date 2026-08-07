package conformance

// The `consema.semantic-model-v6.conformance@1` suite runner
// (crates/consema-conformance/src/semantic_model_v6.rs). The v6 registry
// facts, the source-v2 records, and the line-format query records are
// verified data-driven from the vector file.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"path/filepath"

	"consema.dev/consema/core"
	"consema.dev/consema/protocol"
)

// runSemanticModelV6 executes the embedded v6 suite.
func runSemanticModelV6(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		var err error
		switch vector.ID {
		case "registry.v6-manifest":
			_, err = semanticModelV6RegistryManifest(vector)
		case "registry.v1-v5-frozen":
			_, err = semanticModelV6RegistryFrozen(runner, vector)
		case "registry.v6-additive-contracts":
			_, err = semanticModelV6RegistryContracts(vector)
		case "registry.v6-error-codes":
			_, err = semanticModelV6RegistryErrors(vector)
		case "source-encoding.mandatory-code-pages":
			_, err = semanticModelV6SourceCodePages(vector)
		case "source-encoding.reject-unsupported":
			_, err = semanticModelV6SourceRejectCodePage(vector)
		case "source.bom-policy-distinct":
			_, err = semanticModelV6SourceBomPolicy(vector)
		case "source.snapshot-v2-code-page-boundaries":
			_, err = semanticModelV6SourceBoundaries(vector)
		case "source.snapshot-v2-reject-digest":
			_, err = semanticModelV6SourceDigest(vector)
		case "source.patch-v2-atomic-apply":
			_, err = semanticModelV6SourcePatch(vector)
		case "materialization.request-v2-roundtrip":
			_, err = semanticModelV6MaterializationRequest(vector)
		case "materialization.result-v2-version-closure":
			_, err = semanticModelV6MaterializationResult(vector)
		case "java-utf16.edge-matrix":
			_, err = semanticModelV6JavaMatrix(vector)
		case "java-utf16.reject-noncanonical-unit", "java-utf16.reject-byte-mismatch":
			_, err = semanticModelV6JavaRejection(vector)
		case "ini-query.all-roles":
			_, err = semanticModelV6IniRoles(vector)
		case "properties-query.all-roles":
			_, err = semanticModelV6PropertiesRoles(vector)
		case "line-query.reject-domain-role":
			_, err = semanticModelV6LineDomainRejection(vector)
		case "line-query.reject-ordinal-and-count":
			_, err = semanticModelV6LineOrdinalRejection(vector)
		case "line-query.reject-process-local":
			_, err = semanticModelV6LineProcessLocal(vector)
		case "protocol.v1-v5-reject-v6-contracts":
			_, err = semanticModelV6ProtocolOldRejection(vector)
		case "protocol.exact-version-dispatch":
			_, err = semanticModelV6ProtocolVersionDispatch(vector)
		case "protocol.v6-nested-error-code":
			_, err = semanticModelV6ProtocolNestedError(vector)
		case "protocol.new-contract-canonical-bytes":
			_, err = semanticModelV6ProtocolCanonicalBytes(vector)
		case "protocol.new-payload-schema-and-limits":
			_, err = semanticModelV6ProtocolSchemaLimits(vector)
		default:
			err = fmt.Errorf("runner does not recognize published v6 case")
		}
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: err.Error()})
			continue
		}
		report.Passed = append(report.Passed, vector.ID)
	}
	return report
}

// v6CodePageEncoding resolves one published Windows code page.
func v6CodePageEncoding(number uint32) (*protocol.SourceEncoding, error) {
	encoding, ok := protocol.WindowsCodePageFromNumber(number)
	if !ok {
		return nil, fmt.Errorf("unsupported code page %d", number)
	}
	return encoding, nil
}

// v6CodePageSnapshot builds a TreatAsContent code-page snapshot.
func v6CodePageSnapshot(number uint32, bytes []byte) (*protocol.SourceSnapshot, error) {
	encoding, err := v6CodePageEncoding(number)
	if err != nil {
		return nil, err
	}
	return protocol.NewSourceSnapshotFromRaw(bytes,
		protocol.NewEncodingRequest(encoding).WithBomPolicy(protocol.BomPolicyTreatAsContent),
		protocol.DefaultSourceLimits())
}

func semanticModelV6RegistryManifest(vector *caseData) (bool, error) {
	manifest, err := protocol.NewRegistryManifest(6,
		protocol.NewContractRegistry(protocol.RegistryV6),
		protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV6))
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
		return false, fmt.Errorf("v6 manifest facts differ")
	}
	return true, nil
}

func semanticModelV6RegistryFrozen(runner *Runner, vector *caseData) (bool, error) {
	contractCounts, _ := integerSequenceField(vector.Expected, "contract_counts")
	errorCounts, _ := integerSequenceField(vector.Expected, "error_code_counts")
	if len(contractCounts) != 5 || len(errorCounts) != 5 {
		return false, fmt.Errorf("unexpected expectation facts")
	}
	for index := 0; index < 5; index++ {
		manifest, err := protocol.NewRegistryManifest(uint32(index+1),
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
			uint64(len(manifest.ErrorCodes())) != errorCounts[index] {
			return false, fmt.Errorf("a frozen registry changed")
		}
	}
	// The previous vector digests must match the exact files of the shared
	// inventory.
	previous, ok := caseInput(vector, "previous_vectors")
	if !ok {
		return false, fmt.Errorf("missing input.previous_vectors")
	}
	items, ok := previous.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.previous_vectors must be Sequence")
	}
	expected := [][2]string{
		{"semantic-model-v5", "semantic-model-v5.json"},
		{"protocol-v2", "protocol-v2.json"},
		{"source-v1", "source-v1.json"},
	}
	if items.Len() != len(expected) {
		return false, fmt.Errorf("previous vector count differed")
	}
	for index, item := range items.Items() {
		object, ok := item.(*core.Object)
		if !ok {
			return false, fmt.Errorf("previous vector must be Object")
		}
		name, _ := stringField(object, "name")
		recorded, _ := stringField(object, "sha256")
		bytes, err := os.ReadFile(filepath.Join(runner.VectorsDir, expected[index][1]))
		if err != nil {
			return false, err
		}
		digest := sha256.Sum256(bytes)
		if name != expected[index][0] || hex.EncodeToString(digest[:]) != recorded {
			return false, fmt.Errorf("a frozen vector changed")
		}
	}
	return true, nil
}

func semanticModelV6RegistryContracts(vector *caseData) (bool, error) {
	oldRegistry := protocol.NewContractRegistry(protocol.RegistryV5)
	current := protocol.NewContractRegistry(protocol.RegistryV6)
	expected, _ := stringSequenceField(vector.Expected, "contracts")
	for _, schema := range expected {
		contract, err := parseContractSchema(schema)
		if err != nil {
			return false, err
		}
		if oldRegistry.Recognizes(contract) || !current.Recognizes(contract) {
			return false, fmt.Errorf("v6 contract additions differ")
		}
	}
	if len(current.Contracts()) != len(oldRegistry.Contracts())+len(expected) {
		return false, fmt.Errorf("v6 contract additions differ")
	}
	return true, nil
}

func semanticModelV6RegistryErrors(vector *caseData) (bool, error) {
	oldRegistry := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV5)
	current := protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV6)
	expected, _ := stringSequenceField(vector.Expected, "new_codes")
	errorCodeCount, _ := integerField(vector.Expected, "error_code_count")
	if uint64(len(current.Codes())) != errorCodeCount || len(expected) != 34 {
		return false, fmt.Errorf("v6 error-code additions differ")
	}
	for _, code := range expected {
		if oldRegistry.Contains(code) {
			return false, fmt.Errorf("v6 error-code additions differ")
		}
		descriptor := current.Descriptor(code)
		if descriptor == nil || descriptor.Introduced != "0.8.0" || descriptor.Description == "" {
			return false, fmt.Errorf("v6 error-code additions differ")
		}
	}
	return true, nil
}

func semanticModelV6SourceCodePages(vector *caseData) (bool, error) {
	pages, ok := integerSequenceField(vector.Input, "code_pages")
	if !ok {
		return false, fmt.Errorf("missing input.code_pages")
	}
	accepted := 0
	for _, page := range pages {
		encoding, ok := protocol.WindowsCodePageFromNumber(uint32(page))
		if !ok {
			return false, fmt.Errorf("published code page rejected")
		}
		message := protocol.NewSourceEncodingMessageFromEncoding(encoding)
		decoded := &protocol.SourceEncodingMessage{}
		roundtripped, err := decoded.FromValue(message.ToValue())
		if err != nil {
			return false, err
		}
		if roundtripped.Encoding().Kind == encoding.Kind &&
			roundtripped.Encoding().WindowsCodePage != nil &&
			*roundtripped.Encoding().WindowsCodePage == *encoding.WindowsCodePage {
			accepted++
		}
	}
	acceptedCount, _ := integerField(vector.Expected, "accepted_count")
	if uint64(accepted) != acceptedCount {
		return false, fmt.Errorf("mandatory code-page count differed")
	}
	return true, nil
}

func semanticModelV6SourceRejectCodePage(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	value, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.source-encoding@1")},
		core.Entry{Key: "kind", Value: core.String("WindowsCodePage")},
		core.Entry{Key: "windows_code_page", Value: core.NewInteger(bigIntFromUint64(page))},
	)
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourceEncodingMessage{}
	_, err = decoded.FromValue(value)
	return false, expectErrorCode(vector, err)
}

func bigIntFromUint64(value uint64) *big.Int {
	return new(big.Int).SetUint64(value)
}

func semanticModelV6SourceBomPolicy(vector *caseData) (bool, error) {
	hexText, ok := caseInput(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing input.hex")
	}
	bytes, err := hex.DecodeString(string(hexText.(core.String)))
	if err != nil {
		return false, err
	}
	latin1 := &protocol.SourceEncoding{Kind: "Latin1"}
	detected, err := protocol.NewSourceSnapshotFromRaw(bytes,
		protocol.NewEncodingRequest(latin1), protocol.DefaultSourceLimits())
	if err != nil {
		return false, err
	}
	content, err := protocol.NewSourceSnapshotFromRaw(bytes,
		protocol.NewEncodingRequest(latin1).WithBomPolicy(protocol.BomPolicyTreatAsContent),
		protocol.DefaultSourceLimits())
	if err != nil {
		return false, err
	}
	detectedValue, err := protocol.NewSourceSnapshotMessageV2FromSnapshot(detected).ToValue()
	if err != nil {
		return false, err
	}
	if err := v6DualRoundtrip("core.source-snapshot@2", detectedValue); err != nil {
		return false, err
	}
	contentValue, err := protocol.NewSourceSnapshotMessageV2FromSnapshot(content).ToValue()
	if err != nil {
		return false, err
	}
	if err := v6DualRoundtrip("core.source-snapshot@2", contentValue); err != nil {
		return false, err
	}
	detectText, _ := stringField(vector.Expected, "detect_text")
	contentText, _ := stringField(vector.Expected, "content_text")
	detectedText, detectedOK := detected.DecodedText()
	contentTextValue, contentOK := content.DecodedText()
	if !detectedOK || !contentOK {
		return false, fmt.Errorf("BOM policies must decode text")
	}
	if detectedText != detectText || contentTextValue != contentText {
		return false, fmt.Errorf("BOM policies did not remain distinct")
	}
	if detected.EncodingFacts().BomPolicy() != protocol.BomPolicyDetectUnicode ||
		content.EncodingFacts().BomPolicy() != protocol.BomPolicyTreatAsContent {
		return false, fmt.Errorf("BOM policies did not remain distinct")
	}
	return true, nil
}

func semanticModelV6SourceBoundaries(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	hexText, ok := caseInput(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing input.hex")
	}
	bytes, err := hex.DecodeString(string(hexText.(core.String)))
	if err != nil {
		return false, err
	}
	snapshot, err := v6CodePageSnapshot(uint32(page), bytes)
	if err != nil {
		return false, err
	}
	payload, err := protocol.NewSourceSnapshotMessageV2FromSnapshot(snapshot).ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourceSnapshotMessageV2{}
	decodedMessage, err := decoded.FromValue(payload, protocol.DefaultSourceLimits())
	if err != nil {
		return false, err
	}
	text, _ := stringField(vector.Expected, "text")
	boundaries, _ := integerSequenceField(vector.Expected, "raw_boundaries")
	invalid, _ := integerField(vector.Expected, "invalid_raw_boundary")
	decodedText, ok := decodedMessage.Snapshot().DecodedText()
	if !ok {
		return false, fmt.Errorf("snapshot must decode text")
	}
	if decodedText != text {
		return false, fmt.Errorf("decoded text %q != %q", decodedText, text)
	}
	for _, boundary := range boundaries {
		if _, ok := decodedMessage.Snapshot().DecodedPosition(int(boundary)); !ok {
			return false, fmt.Errorf("boundary %d must resolve", boundary)
		}
	}
	if _, ok := decodedMessage.Snapshot().DecodedPosition(int(invalid)); ok {
		return false, fmt.Errorf("invalid raw boundary %d must fail", invalid)
	}
	rawByte, ok := decodedMessage.Snapshot().RawByteAt(1)
	if !ok || rawByte != 2 {
		return false, fmt.Errorf("raw byte at scalar 1 must be 2")
	}
	return true, nil
}

func semanticModelV6SourceDigest(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	hexText, ok := caseInput(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing input.hex")
	}
	bytes, err := hex.DecodeString(string(hexText.(core.String)))
	if err != nil {
		return false, err
	}
	snapshot, err := v6CodePageSnapshot(uint32(page), bytes)
	if err != nil {
		return false, err
	}
	encoded, err := protocol.NewSourceSnapshotMessageV2FromSnapshot(snapshot).ToValue()
	if err != nil {
		return false, err
	}
	digestValue, ok := objectField(encoded, "digest")
	if !ok {
		return false, fmt.Errorf("digest field missing")
	}
	forgedDigest, err := replaceObjectField(digestValue, "hex", core.String(stringsRepeat("0", 64)))
	if err != nil {
		return false, err
	}
	forged, err := replaceObjectField(encoded, "digest", forgedDigest)
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourceSnapshotMessageV2{}
	_, err = decoded.FromValue(forged, protocol.DefaultSourceLimits())
	if err == nil {
		return false, fmt.Errorf("forged digest must be rejected")
	}
	return false, expectErrorCode(vector, err)
}

func semanticModelV6SourcePatch(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	baseHex, ok := caseInput(vector, "base_hex")
	if !ok {
		return false, fmt.Errorf("missing input.base_hex")
	}
	baseBytes, err := hex.DecodeString(string(baseHex.(core.String)))
	if err != nil {
		return false, err
	}
	base, err := v6CodePageSnapshot(uint32(page), baseBytes)
	if err != nil {
		return false, err
	}
	start, ok := integerFieldValue(vector.Input, "start")
	if !ok {
		return false, fmt.Errorf("missing input.start")
	}
	end, ok := integerFieldValue(vector.Input, "end")
	if !ok {
		return false, fmt.Errorf("missing input.end")
	}
	replacementHex, ok := caseInput(vector, "replacement_hex")
	if !ok {
		return false, fmt.Errorf("missing input.replacement_hex")
	}
	replacementBytes, err := hex.DecodeString(string(replacementHex.(core.String)))
	if err != nil {
		return false, err
	}
	baseRaw := base.Bytes()
	if start > uint64(len(baseRaw)) || end > uint64(len(baseRaw)) || start > end {
		return false, fmt.Errorf("replacement range out of bounds")
	}
	patch, err := protocol.NewSourcePatch(base, []protocol.SourceReplacement{{
		OldStart:    start,
		OldEnd:      end,
		Original:    append([]byte(nil), baseRaw[start:end]...),
		Replacement: replacementBytes,
	}}, nil, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	wire, err := protocol.NewSourcePatchMessageV2FromPatch(patch).ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.SourcePatchMessageV2{}
	decodedMessage, err := decoded.FromValue(wire, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	target, err := protocol.ApplySourcePatch(decodedMessage.Patch(), base, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return false, err
	}
	targetHex, _ := stringField(vector.Expected, "target_hex")
	wrongBaseCode, _ := stringField(vector.Expected, "wrong_base_code")
	if hex.EncodeToString(target) != targetHex {
		return false, fmt.Errorf("target hex %s != %s", hex.EncodeToString(target), targetHex)
	}
	wrong, err := v6CodePageSnapshot(uint32(page), []byte("wrong"))
	if err != nil {
		return false, err
	}
	_, err = protocol.ApplySourcePatch(decodedMessage.Patch(), wrong, protocol.DefaultSourcePatchLimits())
	if err == nil {
		return false, fmt.Errorf("wrong base must be rejected")
	}
	applyError, ok := err.(*protocol.SourcePatchApplyError)
	if !ok || applyError.Code != wrongBaseCode {
		return false, fmt.Errorf("wrong-base code %v != %s", err, wrongBaseCode)
	}
	return true, nil
}

func semanticModelV6MaterializationRequest(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	profileName, _ := stringField(vector.Input, "profile")
	styleName, _ := stringField(vector.Input, "style")
	encoding, err := v6CodePageEncoding(uint32(page))
	if err != nil {
		return false, err
	}
	profile, err := protocol.NewProfileReference(profileName, 1)
	if err != nil {
		return false, err
	}
	request, err := protocol.NewMaterializationRequest(*profile, styleName, 1)
	if err != nil {
		return false, err
	}
	request = request.WithEncoding(encoding).WithNewline("CrLf")
	payload, err := protocol.NewMaterializationRequestMessageV2FromRequest(request).ToValue()
	if err != nil {
		return false, err
	}
	decoded := &protocol.MaterializationRequestMessageV2{}
	decodedMessage, err := decoded.FromValue(payload)
	if err != nil {
		return false, err
	}
	if !request.Equal(decodedMessage.Request()) {
		return false, fmt.Errorf("materialization request v2 differed")
	}
	encodingValue, ok := objectField(payload, "encoding")
	if !ok {
		return false, fmt.Errorf("encoding field missing")
	}
	kind, ok := stringField(encodingValue, "kind")
	if !ok {
		return false, fmt.Errorf("encoding kind missing")
	}
	encodingKind, _ := stringField(vector.Expected, "encoding_kind")
	if kind != encodingKind {
		return false, fmt.Errorf("encoding kind %s != %s", kind, encodingKind)
	}
	return true, nil
}

func semanticModelV6MaterializationResult(vector *caseData) (bool, error) {
	page, ok := integerFieldValue(vector.Input, "code_page")
	if !ok {
		return false, fmt.Errorf("missing input.code_page")
	}
	hexText, ok := caseInput(vector, "hex")
	if !ok {
		return false, fmt.Errorf("missing input.hex")
	}
	bytes, err := hex.DecodeString(string(hexText.(core.String)))
	if err != nil {
		return false, err
	}
	snapshot, err := v6CodePageSnapshot(uint32(page), bytes)
	if err != nil {
		return false, err
	}
	profile, err := protocol.NewProfileReference("ini.windows", 1)
	if err != nil {
		return false, err
	}
	report, err := protocol.NewMaterializationReportMessage(nil)
	if err != nil {
		return false, err
	}
	provenance, err := protocol.NewMaterializationProvenanceMapMessage(nil)
	if err != nil {
		return false, err
	}
	message, err := protocol.NewMaterializationResultMessageV2Complete(*profile,
		"target:ini", snapshot, "Exact", report, provenance)
	if err != nil {
		return false, err
	}
	payload, err := message.ToValue()
	if err != nil {
		return false, err
	}
	if err := v6DualRoundtrip("core.materialization-result@2", payload); err != nil {
		return false, err
	}
	utf8Snapshot, err := protocol.NewSourceSnapshotFromUTF8([]byte("k=v"))
	if err != nil {
		return false, err
	}
	portableProfile, err := protocol.NewProfileReference("ini.portable", 1)
	if err != nil {
		return false, err
	}
	v2Message, err := protocol.NewMaterializationResultMessageV2Complete(*portableProfile,
		"target:ini", utf8Snapshot, "Exact", report, provenance)
	if err != nil {
		return false, err
	}
	v2Payload, err := v2Message.ToValue()
	if err != nil {
		return false, err
	}
	v1SnapshotValue, err := protocol.NewSourceSnapshotMessageV1FromSnapshot(utf8Snapshot)
	if err != nil {
		return false, err
	}
	v1Value, err := v1SnapshotValue.ToValue()
	if err != nil {
		return false, err
	}
	outcomeValue, ok := objectField(v2Payload, "outcome")
	if !ok {
		return false, fmt.Errorf("outcome field missing")
	}
	forgedOutcome, err := replaceObjectField(outcomeValue, "snapshot", v1Value)
	if err != nil {
		return false, err
	}
	mixed, err := replaceObjectField(v2Payload, "outcome", forgedOutcome)
	if err != nil {
		return false, err
	}
	decoded := &protocol.MaterializationResultMessageV2{}
	_, err = decoded.FromValueWithRegistry(mixed, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV6))
	if err == nil {
		return false, fmt.Errorf("mixed version must be rejected")
	}
	mixedVersionCode, _ := stringField(vector.Expected, "mixed_version_code")
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Code() != mixedVersionCode {
		return false, fmt.Errorf("mixed version error %v != %s", err, mixedVersionCode)
	}
	return true, nil
}

func semanticModelV6JavaMatrix(vector *caseData) (bool, error) {
	casesValue, ok := caseInput(vector, "cases")
	if !ok {
		return false, fmt.Errorf("missing input.cases")
	}
	cases, ok := casesValue.(*core.Array)
	if !ok {
		return false, fmt.Errorf("input.cases must be Sequence")
	}
	accepted := 0
	for _, item := range cases.Items() {
		object, ok := item.(*core.Object)
		if !ok {
			return false, fmt.Errorf("Java case must be Object")
		}
		unitsValue, ok := object.Get("units")
		if !ok {
			return false, fmt.Errorf("units missing")
		}
		units, ok := unitsValue.(*core.Array)
		if !ok {
			return false, fmt.Errorf("units must be Sequence")
		}
		codeUnits := make([]uint16, 0, units.Len())
		for _, unitValue := range units.Items() {
			text, ok := unitValue.(core.String)
			if !ok {
				return false, fmt.Errorf("unit must be String")
			}
			var unit uint16
			if _, err := fmt.Sscanf(string(text), "%x", &unit); err != nil {
				return false, fmt.Errorf("invalid UTF-16 unit %q", string(text))
			}
			codeUnits = append(codeUnits, unit)
		}
		status, _ := stringField(object, "status")
		exact, err := protocol.NewJavaUtf16String(codeUnits, protocol.DefaultProtocolLimits())
		if err != nil {
			return false, err
		}
		expectedStatus := protocol.JavaWellFormedUnicode
		if status == "UnpairedSurrogate" {
			expectedStatus = protocol.JavaUnpairedSurrogate
		}
		exactValue, err := exact.ToValue()
		if err != nil {
			return false, err
		}
		decoded := &protocol.JavaUtf16String{}
		roundtripped, err := decoded.FromValue(exactValue, protocol.DefaultProtocolLimits())
		if err != nil {
			return false, err
		}
		roundtripValue, err := roundtripped.ToValue()
		if err != nil {
			return false, err
		}
		if exact.UnicodeStatus() == expectedStatus && core.Equal(roundtripValue, exactValue) {
			accepted++
		}
	}
	acceptedCount, _ := integerField(vector.Expected, "accepted_count")
	if uint64(accepted) != acceptedCount {
		return false, fmt.Errorf("Java UTF-16 edge matrix differed")
	}
	return true, nil
}

func semanticModelV6JavaRejection(vector *caseData) (bool, error) {
	unit, ok := caseInput(vector, "unit")
	if !ok {
		return false, fmt.Errorf("missing input.unit")
	}
	bytesHex, ok := caseInput(vector, "bytes_hex")
	if !ok {
		return false, fmt.Errorf("missing input.bytes_hex")
	}
	status, _ := stringField(vector.Input, "status")
	rawBytes, err := hex.DecodeString(string(bytesHex.(core.String)))
	if err != nil {
		return false, err
	}
	value, err := core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.java-utf16-string@1")},
		core.Entry{Key: "encoding", Value: core.String("UTF16BE/1")},
		core.Entry{Key: "code_units", Value: core.NewArray(unit)},
		core.Entry{Key: "bytes", Value: core.NewBytes(rawBytes)},
		core.Entry{Key: "unicode_status", Value: core.String(status)},
	)
	if err != nil {
		return false, err
	}
	decoded := &protocol.JavaUtf16String{}
	_, err = decoded.FromValue(value, protocol.DefaultProtocolLimits())
	if err == nil {
		return false, fmt.Errorf("published rejection must fail")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok {
		return false, fmt.Errorf("unexpected error type: %v", err)
	}
	expectedCode, _ := stringField(vector.Expected, "code")
	expectedPath, _ := stringField(vector.Expected, "path")
	if protocolError.Code() != expectedCode || protocolError.Path != expectedPath {
		return false, fmt.Errorf("rejection %s at %s != %s at %s",
			protocolError.Code(), protocolError.Path, expectedCode, expectedPath)
	}
	return true, nil
}

func semanticModelV6IniRoles(vector *caseData) (bool, error) {
	roles, err := v6RoleList(vector, "roles", v6ParseIniRole)
	if err != nil {
		return false, err
	}
	sourceID, _ := stringField(vector.Input, "source_id")
	for ordinal, role := range roles {
		domain := protocol.NewQueryDomain("ini.native-semantic-query", 1)
		if role == protocol.RoleIniSyntaxPiece {
			domain = protocol.NewQueryDomain("ini.lossless-syntax-query", 1)
		}
		locator, err := protocol.NewIniMatchLocator(sourceID,
			fmt.Sprintf("ini:node:%d", ordinal), role, uint64(ordinal))
		if err != nil {
			return false, err
		}
		completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
		if err != nil {
			return false, err
		}
		result, err := protocol.NewIniQueryResultMessage(domain, role,
			[]*protocol.IniMatchLocator{locator}, completion, nil)
		if err != nil {
			return false, err
		}
		value, err := result.ToValue()
		if err != nil {
			return false, err
		}
		if err := v6DualRoundtrip("core.ini-query-result@1", value); err != nil {
			return false, err
		}
	}
	roleCount, _ := integerField(vector.Expected, "role_count")
	if uint64(len(roles)) != roleCount {
		return false, fmt.Errorf("INI role count differed")
	}
	return true, nil
}

func semanticModelV6PropertiesRoles(vector *caseData) (bool, error) {
	roles, err := v6RoleList(vector, "roles", v6ParsePropertiesRole)
	if err != nil {
		return false, err
	}
	sourceID, _ := stringField(vector.Input, "source_id")
	for ordinal, role := range roles {
		domain := protocol.NewQueryDomain("java-properties.native-semantic-query", 1)
		if role == protocol.RolePropertiesSyntaxPiece {
			domain = protocol.NewQueryDomain("java-properties.lossless-syntax-query", 1)
		}
		locator, err := protocol.NewJavaPropertiesMatchLocator(sourceID,
			fmt.Sprintf("properties:node:%d", ordinal), role, uint64(ordinal))
		if err != nil {
			return false, err
		}
		completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 1, 1, nil, nil)
		if err != nil {
			return false, err
		}
		result, err := protocol.NewJavaPropertiesQueryResultMessage(domain, role,
			[]*protocol.JavaPropertiesMatchLocator{locator}, completion, nil)
		if err != nil {
			return false, err
		}
		value, err := result.ToValue()
		if err != nil {
			return false, err
		}
		if err := v6DualRoundtrip("core.java-properties-query-result@1", value); err != nil {
			return false, err
		}
	}
	roleCount, _ := integerField(vector.Expected, "role_count")
	if uint64(len(roles)) != roleCount {
		return false, fmt.Errorf("Properties role count differed")
	}
	return true, nil
}

func v6RoleList(vector *caseData, name string,
	parse func(string) (protocol.MatchRole, error)) ([]protocol.MatchRole, error) {
	rolesValue, ok := caseInput(vector, name)
	if !ok {
		return nil, fmt.Errorf("missing input.%s", name)
	}
	roles, ok := rolesValue.(*core.Array)
	if !ok {
		return nil, fmt.Errorf("input.%s must be Sequence", name)
	}
	output := make([]protocol.MatchRole, 0, roles.Len())
	for _, item := range roles.Items() {
		text, ok := item.(core.String)
		if !ok {
			return nil, fmt.Errorf("role must be String")
		}
		role, err := parse(string(text))
		if err != nil {
			return nil, err
		}
		output = append(output, role)
	}
	return output, nil
}

func v6ParseIniRole(text string) (protocol.MatchRole, error) {
	switch text {
	case "IniDocument", "IniPhysicalLine", "IniLogicalLine", "IniSection",
		"IniDefaultSection", "IniEntry", "IniErrorLine", "IniSyntaxPiece":
		return protocol.MatchRole(text), nil
	}
	return "", fmt.Errorf("unknown INI role %q", text)
}

func v6ParsePropertiesRole(text string) (protocol.MatchRole, error) {
	switch text {
	case "PropertiesDocument", "PropertiesNaturalLine", "PropertiesLogicalLine",
		"PropertiesProperty", "PropertiesComment", "PropertiesEscape",
		"PropertiesErrorLine", "PropertiesSyntaxPiece":
		return protocol.MatchRole(text), nil
	}
	return "", fmt.Errorf("unknown Properties role %q", text)
}

func semanticModelV6LineDomainRejection(vector *caseData) (bool, error) {
	roleText, ok := caseInput(vector, "role")
	if !ok {
		return false, fmt.Errorf("missing input.role")
	}
	role, err := v6ParseIniRole(string(roleText.(core.String)))
	if err != nil {
		return false, err
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, 0, 0, nil, nil)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewIniQueryResultMessage(
		protocol.NewQueryDomain("ini.native-semantic-query", 1), role, nil, completion, nil)
	return false, expectErrorCode(vector, err)
}

func semanticModelV6LineOrdinalRejection(vector *caseData) (bool, error) {
	roleText, ok := caseInput(vector, "role")
	if !ok {
		return false, fmt.Errorf("missing input.role")
	}
	role, err := v6ParsePropertiesRole(string(roleText.(core.String)))
	if err != nil {
		return false, err
	}
	ordinals, ok := integerSequenceField(vector.Input, "ordinals")
	if !ok {
		return false, fmt.Errorf("missing input.ordinals")
	}
	produced, ok := integerFieldValue(vector.Input, "produced")
	if !ok {
		return false, fmt.Errorf("missing input.produced")
	}
	matches := make([]*protocol.JavaPropertiesMatchLocator, 0, len(ordinals))
	for index, ordinal := range ordinals {
		locator, err := protocol.NewJavaPropertiesMatchLocator("source:properties",
			fmt.Sprintf("property:%d", index), role, ordinal)
		if err != nil {
			return false, err
		}
		matches = append(matches, locator)
	}
	completion, err := protocol.NewCompletion(protocol.CompletionSuccess, produced, produced, nil, nil)
	if err != nil {
		return false, err
	}
	_, err = protocol.NewJavaPropertiesQueryResultMessage(
		protocol.NewQueryDomain("java-properties.native-semantic-query", 1), role,
		matches, completion, nil)
	return false, expectErrorCode(vector, err)
}

func semanticModelV6LineProcessLocal(vector *caseData) (bool, error) {
	return false, expectErrorCode(vector, protocol.IniMatchLocatorFromProcessLocal())
}

// v6NewPayloads builds the eight v6 contract payloads
// (semantic_model_v6.rs new_payloads).
func v6NewPayloads() ([]struct {
	schema  string
	payload core.Value
}, error) {
	encoding, err := v6CodePageEncoding(1252)
	if err != nil {
		return nil, err
	}
	snapshot, err := v6CodePageSnapshot(1252, []byte("k=1"))
	if err != nil {
		return nil, err
	}
	patch, err := protocol.NewSourcePatch(snapshot, nil, nil, protocol.DefaultSourcePatchLimits())
	if err != nil {
		return nil, err
	}
	profile, err := protocol.NewProfileReference("ini.windows", 1)
	if err != nil {
		return nil, err
	}
	request, err := protocol.NewMaterializationRequest(*profile, "ini.windows-canonical", 1)
	if err != nil {
		return nil, err
	}
	request = request.WithEncoding(encoding).WithNewline("CrLf")
	report, err := protocol.NewMaterializationReportMessage(nil)
	if err != nil {
		return nil, err
	}
	provenance, err := protocol.NewMaterializationProvenanceMapMessage(nil)
	if err != nil {
		return nil, err
	}
	result, err := protocol.NewMaterializationResultMessageV2Complete(*profile,
		"target:ini", snapshot, "Exact", report, provenance)
	if err != nil {
		return nil, err
	}
	iniCompletion, err := protocol.NewCompletion(protocol.CompletionSuccess, 0, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	ini, err := protocol.NewIniQueryResultMessage(
		protocol.NewQueryDomain("ini.native-semantic-query", 1),
		protocol.RoleIniDocument, nil, iniCompletion, nil)
	if err != nil {
		return nil, err
	}
	properties, err := protocol.NewJavaPropertiesQueryResultMessage(
		protocol.NewQueryDomain("java-properties.native-semantic-query", 1),
		protocol.RolePropertiesDocument, nil, iniCompletion, nil)
	if err != nil {
		return nil, err
	}
	java, err := protocol.NewJavaUtf16String([]uint16{0xD800}, protocol.DefaultProtocolLimits())
	if err != nil {
		return nil, err
	}
	requestValue, err := protocol.NewMaterializationRequestMessageV2FromRequest(request).ToValue()
	if err != nil {
		return nil, err
	}
	resultValue, err := result.ToValue()
	if err != nil {
		return nil, err
	}
	iniValue, err := ini.ToValue()
	if err != nil {
		return nil, err
	}
	propertiesValue, err := properties.ToValue()
	if err != nil {
		return nil, err
	}
	javaValue, err := java.ToValue()
	if err != nil {
		return nil, err
	}
	patchValue, err := protocol.NewSourcePatchMessageV2FromPatch(patch).ToValue()
	if err != nil {
		return nil, err
	}
	snapshotValue, err := protocol.NewSourceSnapshotMessageV2FromSnapshot(snapshot).ToValue()
	if err != nil {
		return nil, err
	}
	return []struct {
		schema  string
		payload core.Value
	}{
		{"core.ini-query-result@1", iniValue},
		{"core.java-properties-query-result@1", propertiesValue},
		{"core.java-utf16-string@1", javaValue},
		{"core.materialization-request@2", requestValue},
		{"core.materialization-result@2", resultValue},
		{"core.source-encoding@1", protocol.NewSourceEncodingMessageFromEncoding(encoding).ToValue()},
		{"core.source-patch@2", patchValue},
		{"core.source-snapshot@2", snapshotValue},
	}, nil
}

func semanticModelV6ProtocolOldRejection(vector *caseData) (bool, error) {
	payloads, err := v6NewPayloads()
	if err != nil {
		return false, err
	}
	expectedCode, _ := stringField(vector.Expected, "code")
	rejected := 0
	for _, payload := range payloads {
		contract, err := parseContractSchema(payload.schema)
		if err != nil {
			return false, err
		}
		oldRegistries := []protocol.ContractRegistry{
			protocol.NewContractRegistry(protocol.RegistryV1),
			protocol.NewContractRegistry(protocol.RegistryV2),
			protocol.NewContractRegistry(protocol.RegistryV3),
			protocol.NewContractRegistry(protocol.RegistryV4),
			protocol.NewContractRegistry(protocol.RegistryV5),
		}
		allRejected := true
		for _, registry := range oldRegistries {
			_, err := protocol.NewProtocolMessage(contract, payload.payload, registry)
			if err == nil {
				allRejected = false
				continue
			}
			protocolError, ok := err.(*protocol.ProtocolError)
			if !ok || protocolError.Code() != expectedCode {
				allRejected = false
			}
		}
		if allRejected {
			rejected++
		}
	}
	rejectedPairs, _ := integerField(vector.Expected, "rejected_pairs")
	if uint64(rejected) != rejectedPairs {
		return false, fmt.Errorf("an old registry accepted a v6 contract")
	}
	return true, nil
}

func semanticModelV6ProtocolVersionDispatch(vector *caseData) (bool, error) {
	profile, err := protocol.NewProfileReference("ini.portable", 1)
	if err != nil {
		return false, err
	}
	request, err := protocol.NewMaterializationRequest(*profile, "ini.portable-canonical", 1)
	if err != nil {
		return false, err
	}
	v2Value, err := protocol.NewMaterializationRequestMessageV2FromRequest(request).ToValue()
	if err != nil {
		return false, err
	}
	disguised, err := replaceObjectField(v2Value, "schema", core.String("core.materialization-request@1"))
	if err != nil {
		return false, err
	}
	contract, err := parseContractSchema("core.materialization-request@1")
	if err != nil {
		return false, err
	}
	_, err = protocol.NewProtocolMessage(contract, disguised, protocol.NewContractRegistry(protocol.RegistryV6))
	if err == nil {
		return false, fmt.Errorf("disguised payload must be rejected")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Code() != "core.protocol.wrong-type@1" || protocolError.Path != "$.encoding" {
		return false, fmt.Errorf("version dispatch %v", err)
	}
	return true, nil
}

func semanticModelV6ProtocolNestedError(vector *caseData) (bool, error) {
	codeText, ok := caseInput(vector, "failure_code")
	if !ok {
		return false, fmt.Errorf("missing input.failure_code")
	}
	code := string(codeText.(core.String))
	v5Code, _ := stringField(vector.Expected, "v5_code")
	v5Completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed, 1, 0,
		nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV5))
	_ = v5Completion
	if err == nil {
		return false, fmt.Errorf("v5 accepted a v6 diagnostic code")
	}
	protocolError, ok := err.(*protocol.ProtocolError)
	if !ok || protocolError.Code() != v5Code {
		return false, fmt.Errorf("v5 nested rejection %v != %s", err, v5Code)
	}
	completion, err := protocol.NewCompletionWithRegistry(protocol.CompletionFailed, 1, 0,
		nil, &code, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV6))
	if err != nil {
		return false, err
	}
	payload, err := completion.ToValue()
	if err != nil {
		return false, err
	}
	if err := v6DualRoundtrip("core.completion@1", payload); err != nil {
		return false, err
	}
	return true, nil
}

func semanticModelV6ProtocolCanonicalBytes(vector *caseData) (bool, error) {
	encoding, err := v6CodePageEncoding(1252)
	if err != nil {
		return false, err
	}
	java, err := protocol.NewJavaUtf16String([]uint16{0x0000, 0xD83D, 0xDE00, 0xD800},
		protocol.DefaultProtocolLimits())
	if err != nil {
		return false, err
	}
	encodingMessage, err := protocol.NewProtocolMessage(mustContractId("core.source-encoding@1"),
		protocol.NewSourceEncodingMessageFromEncoding(encoding).ToValue(),
		protocol.NewContractRegistry(protocol.RegistryV6))
	if err != nil {
		return false, err
	}
	javaValue, err := java.ToValue()
	if err != nil {
		return false, err
	}
	javaMessage, err := protocol.NewProtocolMessage(mustContractId("core.java-utf16-string@1"),
		javaValue, protocol.NewContractRegistry(protocol.RegistryV6))
	if err != nil {
		return false, err
	}
	limits := protocol.DefaultProtocolLimits()
	encodingJSON, err := encodingMessage.ToJSON(limits)
	if err != nil {
		return false, err
	}
	encodingPVCE, err := encodingMessage.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	javaJSON, err := javaMessage.ToJSON(limits)
	if err != nil {
		return false, err
	}
	javaPVCE, err := javaMessage.ToPVCE(limits)
	if err != nil {
		return false, err
	}
	actual := []string{
		hex.EncodeToString(encodingJSON), hex.EncodeToString(encodingPVCE),
		hex.EncodeToString(javaJSON), hex.EncodeToString(javaPVCE),
	}
	for _, name := range []string{"source_encoding_json_hex", "source_encoding_pvce_hex",
		"java_utf16_json_hex", "java_utf16_pvce_hex"} {
		expected, _ := stringField(vector.Expected, name)
		if actual[0] != expected {
			return false, fmt.Errorf("canonical hex differs for %s", name)
		}
		actual = actual[1:]
	}
	return true, nil
}

func semanticModelV6ProtocolSchemaLimits(vector *caseData) (bool, error) {
	exact, err := protocol.NewJavaUtf16String([]uint16{0x0041}, protocol.DefaultProtocolLimits())
	if err != nil {
		return false, err
	}
	value, err := exact.ToValue()
	if err != nil {
		return false, err
	}
	unknown, err := appendObjectField(value, "unknown", core.NullValue())
	if err != nil {
		return false, err
	}
	decoded := &protocol.JavaUtf16String{}
	_, unknownErr := decoded.FromValue(unknown, protocol.DefaultProtocolLimits())
	unknownFieldCode, _ := stringField(vector.Expected, "unknown_field_code")
	if unknownErr == nil {
		return false, fmt.Errorf("unknown field must be rejected")
	}
	unknownProtocolError, ok := unknownErr.(*protocol.ProtocolError)
	if !ok || unknownProtocolError.Code() != unknownFieldCode ||
		unknownProtocolError.Path != "$.unknown" {
		return false, fmt.Errorf("unknown field rejection %v", unknownErr)
	}
	limit, ok := integerFieldValue(vector.Input, "max_units")
	if !ok {
		return false, fmt.Errorf("missing input.max_units")
	}
	limits := protocol.DefaultProtocolLimits()
	limits.MaxContainerEntries = int(limit)
	_, limitErr := decoded.FromValue(value, limits)
	limitCode, _ := stringField(vector.Expected, "limit_code")
	if limitErr == nil {
		return false, fmt.Errorf("unit limit must be rejected")
	}
	limitProtocolError, ok := limitErr.(*protocol.ProtocolError)
	if !ok || limitProtocolError.Code() != limitCode ||
		limitProtocolError.Path != "$.code_units" {
		return false, fmt.Errorf("limit rejection %v", limitErr)
	}
	return true, nil
}

// v6DualRoundtrip proves JSON/PVCE transport identity under the v6
// registry.
func v6DualRoundtrip(schema string, payload core.Value) error {
	envelope, err := protocol.NewProtocolMessage(mustContractId(schema), payload,
		protocol.NewContractRegistry(protocol.RegistryV6))
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
	registry := protocol.NewContractRegistry(protocol.RegistryV6)
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
		return fmt.Errorf("dual canonical transport did not close")
	}
	return nil
}
