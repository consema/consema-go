package conformance

// The `consema.source.conformance@1` suite runner, mirroring
// crates/consema-conformance/src/source_v1.rs. The 0.15.0 milestone G1.1
// implements the whole document capability surface (core.source.snapshot@1,
// core.source.encoding@1, core.source.decoded-location@1,
// core.source.binary-coverage@1, core.source.patch@1,
// core.source.limits@1) through the document package, so every published
// case executes; nothing in this suite is deferred.

import (
	"encoding/hex"
	"fmt"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
)

// runSourceV1 executes the `consema.source.conformance@1` suite.
func runSourceV1(_ *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch {
		case vector.ID == "source.digest.sha256-empty" || vector.ID == "source.digest.sha256-abc":
			runSourceDigestCase(vector, report)
		case vector.ID == "source.identity.equal-bytes-distinct-snapshots":
			runSourceIdentityCase(vector, report)
		case strings.HasPrefix(vector.ID, "source.encoding."):
			runSourceEncodingCase(vector, report)
		case strings.HasPrefix(vector.ID, "source.location."):
			runSourceLocationCase(vector, report)
		case strings.HasPrefix(vector.ID, "source.binary."):
			runSourceBinaryCase(vector, report)
		case strings.HasPrefix(vector.ID, "source.patch."):
			runSourcePatchCase(vector, report)
		case strings.HasPrefix(vector.ID, "source.resource."):
			runSourceResourceCase(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published source case",
			})
		}
	}
	return report
}

func runSourceDigestCase(vector *caseData, report *SuiteReport) {
	raw, ok := sourceHexInput(vector, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_hex")
		return
	}
	expected, ok := stringField(vector.Expected, "digest")
	if !ok {
		failSourceCase(vector, report, "missing expected.digest")
		return
	}
	if got := document.DigestOf(raw).Hex(); got != expected {
		failSourceCase(vector, report, fmt.Sprintf("digest %s != %s", got, expected))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourceIdentityCase(vector *caseData, report *SuiteReport) {
	raw, ok := sourceHexInput(vector, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_hex")
		return
	}
	first, err := document.NewSourceSnapshotFromUTF8(raw)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	second, err := document.NewSourceSnapshotFromUTF8(raw)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	equalDigest, _ := booleanField(vector.Expected, "equal_digest")
	distinctSnapshot, _ := booleanField(vector.Expected, "distinct_snapshot")
	if first.Digest().Equal(second.Digest()) != equalDigest {
		failSourceCase(vector, report, "digest equality fact differs")
		return
	}
	if (first.Identity() != second.Identity()) != distinctSnapshot {
		failSourceCase(vector, report, "snapshot identity distinctness fact differs")
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourceEncodingCase(vector *caseData, report *SuiteReport) {
	raw, ok := sourceHexInput(vector, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_hex")
		return
	}
	request, err := sourceEncodingRequest(vector)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(raw, request, document.DefaultSourceLimits())
	if err != nil {
		expected, ok := stringField(vector.Expected, "code")
		if !ok {
			failSourceCase(vector, report, "missing expected.code")
			return
		}
		if sourceErrorCode(err) != expected {
			failSourceCase(vector, report, fmt.Sprintf("error code %s != %s", sourceErrorCode(err), expected))
		} else {
			report.Passed = append(report.Passed, vector.ID)
		}
		return
	}
	expectedRaw, ok := stringField(vector.Expected, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing expected.raw_hex")
		return
	}
	expectedSelected, ok := stringField(vector.Expected, "selected")
	if !ok {
		failSourceCase(vector, report, "missing expected.selected")
		return
	}
	expectedDecoded, ok := caseExpected(vector, "decoded_utf8_hex")
	if !ok {
		failSourceCase(vector, report, "missing expected.decoded_utf8_hex")
		return
	}
	if got := hex.EncodeToString(snapshot.Bytes()); got != expectedRaw {
		failSourceCase(vector, report, fmt.Sprintf("retained bytes %s != %s", got, expectedRaw))
		return
	}
	if got := snapshot.EncodingFacts().Selected().AsStr(); got != expectedSelected {
		failSourceCase(vector, report, fmt.Sprintf("selected %s != %s", got, expectedSelected))
		return
	}
	if _, isNull := expectedDecoded.(core.Null); isNull {
		if _, ok := snapshot.DecodedText(); ok {
			failSourceCase(vector, report, "decoded text must be unavailable for binary sources")
			return
		}
	} else {
		expectedHex, ok := expectedDecoded.(core.String)
		if !ok {
			failSourceCase(vector, report, "expected.decoded_utf8_hex must be String or Null")
			return
		}
		text, ok := snapshot.DecodedText()
		if !ok {
			failSourceCase(vector, report, "decoded text unavailable")
			return
		}
		if got := hex.EncodeToString([]byte(text)); got != string(expectedHex) {
			failSourceCase(vector, report, fmt.Sprintf("decoded text %s != %s", got, string(expectedHex)))
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourceLocationCase(vector *caseData, report *SuiteReport) {
	raw, ok := sourceHexInput(vector, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_hex")
		return
	}
	request, err := sourceEncodingRequest(vector)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(raw, request, document.DefaultSourceLimits())
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	if _, ok := snapshot.DecodedText(); !ok {
		_, err := snapshot.DecodedPosition(0)
		expected, ok := stringField(vector.Expected, "code")
		if !ok {
			failSourceCase(vector, report, "missing expected.code")
			return
		}
		if locationErrorName(err) != expected {
			failSourceCase(vector, report, fmt.Sprintf("location error %s != %s", locationErrorName(err), expected))
		} else {
			report.Passed = append(report.Passed, vector.ID)
		}
		return
	}
	rawByte, ok := integerField(vector.Input, "raw_byte")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_byte")
		return
	}
	position, err := snapshot.DecodedPosition(int(rawByte))
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	expectedUTF8, _ := integerField(vector.Expected, "decoded_utf8_byte")
	expectedScalar, _ := integerField(vector.Expected, "unicode_scalar_offset")
	expectedUTF16, _ := integerField(vector.Expected, "utf16_code_unit_offset")
	if uint64(position.DecodedUTF8Byte) != expectedUTF8 ||
		uint64(position.UnicodeScalarOffset) != expectedScalar ||
		uint64(position.UTF16CodeUnitOffset) != expectedUTF16 {
		failSourceCase(vector, report, fmt.Sprintf("position %+v does not match expected facts", position))
		return
	}
	for _, offset := range []document.DecodedOffset{
		document.NewUtf8ByteOffset(position.DecodedUTF8Byte),
		document.NewUnicodeScalarOffset(position.UnicodeScalarOffset),
		document.NewUtf16CodeUnitOffset(position.UTF16CodeUnitOffset),
	} {
		back, err := snapshot.RawByteAt(offset)
		if err != nil || back != int(rawByte) {
			failSourceCase(vector, report, fmt.Sprintf("raw_byte_at(%d) = %d, %v; want %d",
				offset.Value(), back, err, rawByte))
			return
		}
	}
	invalidRaw, ok := integerField(vector.Input, "invalid_raw_byte")
	if !ok {
		failSourceCase(vector, report, "missing input.invalid_raw_byte")
		return
	}
	if _, err := snapshot.DecodedPosition(int(invalidRaw)); locationErrorName(err) != "NotDecodedBoundary" {
		failSourceCase(vector, report, fmt.Sprintf("decoded_position(%d) = %v, want NotDecodedBoundary", invalidRaw, err))
		return
	}
	invalidUTF16, ok := integerField(vector.Input, "invalid_utf16_offset")
	if !ok {
		failSourceCase(vector, report, "missing input.invalid_utf16_offset")
		return
	}
	if _, err := snapshot.RawByteAt(document.NewUtf16CodeUnitOffset(int(invalidUTF16))); locationErrorName(err) != "DecodedOffsetNotBoundary" {
		failSourceCase(vector, report, fmt.Sprintf("raw_byte_at(utf16 %d) = %v, want DecodedOffsetNotBoundary", invalidUTF16, err))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourceBinaryCase(vector *caseData, report *SuiteReport) {
	sourceLen, ok := integerField(vector.Input, "source_len")
	if !ok {
		failSourceCase(vector, report, "missing input.source_len")
		return
	}
	regionValues, ok := sequenceField(vector.Input, "regions")
	if !ok {
		failSourceCase(vector, report, "input.regions must be a Sequence")
		return
	}
	authority := document.NewDocumentAuthority()
	regions := make([]document.BinaryRegion, 0, len(regionValues))
	for index, value := range regionValues {
		start, ok := integerField(value, "start")
		if !ok {
			failSourceCase(vector, report, fmt.Sprintf("region %d start must be Integer", index))
			return
		}
		end, ok := integerField(value, "end")
		if !ok {
			failSourceCase(vector, report, fmt.Sprintf("region %d end must be Integer", index))
			return
		}
		kind, ok := stringField(value, "kind")
		if !ok {
			failSourceCase(vector, report, fmt.Sprintf("region %d kind must be String", index))
			return
		}
		span, err := authority.Span(int(start), int(end))
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		regions = append(regions, document.NewBinaryRegion(
			authority.NodeRef(uint64(index), document.RoleBinaryRegion), span, kind))
	}
	index, err := document.NewBinaryStructuralIndex(authority.Identity(), int(sourceLen), regions)
	if err != nil {
		expected, ok := stringField(vector.Expected, "code")
		if !ok {
			failSourceCase(vector, report, "missing expected.code")
			return
		}
		if locationErrorName(err) != expected {
			failSourceCase(vector, report, fmt.Sprintf("location error %s != %s", locationErrorName(err), expected))
		} else {
			report.Passed = append(report.Passed, vector.ID)
		}
		return
	}
	regionCount, ok := integerField(vector.Expected, "region_count")
	if !ok {
		failSourceCase(vector, report, "missing expected.region_count")
		return
	}
	regions = index.Regions()
	lastEnd := 0
	if len(regions) > 0 {
		lastEnd = regions[len(regions)-1].Span().EndByte()
	}
	if uint64(len(regions)) != regionCount || lastEnd != int(sourceLen) {
		failSourceCase(vector, report, fmt.Sprintf("coverage %d regions ending at %d; want %d ending at %d",
			len(regions), lastEnd, regionCount, sourceLen))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourcePatchCase(vector *caseData, report *SuiteReport) {
	mode, ok := stringField(vector.Input, "mode")
	if !ok {
		failSourceCase(vector, report, "missing input.mode")
		return
	}
	base, err := sourceSnapshotFromCase(vector, "base_hex")
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	replacements, err := sourceReplacementValues(vector)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	limits, err := sourcePatchLimits(vector)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	metadata := map[string]string{"actor": "conformance"}
	switch mode {
	case "create-apply":
		patch, err := document.NewSourcePatch(base, replacements, metadata, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		target, err := patch.Apply(base, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		expected, ok := stringField(vector.Expected, "target_hex")
		if !ok {
			failSourceCase(vector, report, "missing expected.target_hex")
			return
		}
		if got := hex.EncodeToString(target.Bytes()); got != expected {
			failSourceCase(vector, report, fmt.Sprintf("target bytes %s != %s", got, expected))
			return
		}
		if !target.Digest().Equal(patch.TargetDigest()) {
			failSourceCase(vector, report, "applied digest != patch target digest")
			return
		}
		if patch.Metadata()["actor"] != "conformance" {
			failSourceCase(vector, report, "patch metadata not retained")
			return
		}
	case "stale-base":
		patch, err := document.NewSourcePatch(base, replacements, metadata, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		stale, err := sourceSnapshotFromCase(vector, "stale_hex")
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		_, applyErr := patch.Apply(stale, limits)
		expectSourcePatchError(vector, report, applyErr)
		return
	case "wrong-original":
		targetBytes, ok := sourceHexInput(vector, "target_hex")
		if !ok {
			failSourceCase(vector, report, "missing input.target_hex")
			return
		}
		patch, err := document.NewSourcePatchFromFacts(base.Digest(), document.DigestOf(targetBytes),
			base.EncodingFacts(), replacements, metadata, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		_, applyErr := patch.Apply(base, limits)
		expectSourcePatchError(vector, report, applyErr)
		return
	case "overlap", "count-limit":
		_, err := document.NewSourcePatch(base, replacements, metadata, limits)
		expectSourcePatchError(vector, report, err)
		return
	case "wrong-target":
		patch, err := document.NewSourcePatchFromFacts(base.Digest(),
			document.DigestOf([]byte("deliberately-wrong-target")),
			base.EncodingFacts(), replacements, metadata, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		_, applyErr := patch.Apply(base, limits)
		expectSourcePatchError(vector, report, applyErr)
		return
	case "encoding-change":
		targetBytes, ok := sourceHexInput(vector, "target_hex")
		if !ok {
			failSourceCase(vector, report, "missing input.target_hex")
			return
		}
		patch, err := document.NewSourcePatchFromFacts(base.Digest(), document.DigestOf(targetBytes),
			base.EncodingFacts(), replacements, metadata, limits)
		if err != nil {
			failSourceCase(vector, report, err.Error())
			return
		}
		_, applyErr := patch.Apply(base, limits)
		expectSourcePatchError(vector, report, applyErr)
		return
	default:
		failSourceCase(vector, report, "unknown patch mode "+mode)
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runSourceResourceCase(vector *caseData, report *SuiteReport) {
	if vector.ID == "source.resource.patch-count-limit" {
		runSourcePatchCase(vector, report)
		return
	}
	raw, ok := sourceHexInput(vector, "raw_hex")
	if !ok {
		failSourceCase(vector, report, "missing input.raw_hex")
		return
	}
	limits := document.DefaultSourceLimits()
	if value, ok := integerField(vector.Input, "max_raw_bytes"); ok {
		limits.MaxRawBytes = int(value)
	}
	if value, ok := integerField(vector.Input, "max_decoded_utf8_bytes"); ok {
		limits.MaxDecodedUTF8Bytes = int(value)
	}
	if value, ok := integerField(vector.Input, "max_decoded_scalars"); ok {
		limits.MaxDecodedScalars = int(value)
	}
	request, err := sourceEncodingRequest(vector)
	if err != nil {
		failSourceCase(vector, report, err.Error())
		return
	}
	_, err = document.NewSourceSnapshotFromRaw(raw, request, limits)
	expected, ok := stringField(vector.Expected, "code")
	if !ok {
		failSourceCase(vector, report, "missing expected.code")
		return
	}
	if sourceErrorCode(err) != expected {
		failSourceCase(vector, report, fmt.Sprintf("error code %s != %s", sourceErrorCode(err), expected))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// expectSourcePatchError asserts the frozen registered code of one patch
// failure.
func expectSourcePatchError(vector *caseData, report *SuiteReport, err error) {
	expected, ok := stringField(vector.Expected, "code")
	if !ok {
		failSourceCase(vector, report, "missing expected.code")
		return
	}
	if sourceErrorCode(err) != expected {
		failSourceCase(vector, report, fmt.Sprintf("error code %s != %s", sourceErrorCode(err), expected))
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

// sourceEncodingRequest rebuilds the deterministic resolution request from
// the vector's encoding facts.
func sourceEncodingRequest(vector *caseData) (document.EncodingRequest, error) {
	encodingName, ok := stringField(vector.Input, "encoding")
	if !ok {
		return document.EncodingRequest{}, fmt.Errorf("missing input.encoding")
	}
	encoding, err := parseSourceEncoding(encodingName)
	if err != nil {
		return document.EncodingRequest{}, err
	}
	request := document.NewEncodingRequest(encoding)
	if declaration, ok := stringField(vector.Input, "declaration"); ok {
		parsed, err := parseSourceEncoding(declaration)
		if err != nil {
			return document.EncodingRequest{}, err
		}
		request = request.WithDeclaration(parsed)
	}
	if override, ok := stringField(vector.Input, "caller_override"); ok {
		parsed, err := parseSourceEncoding(override)
		if err != nil {
			return document.EncodingRequest{}, err
		}
		request = request.WithCallerOverride(parsed)
	}
	return request, nil
}

func parseSourceEncoding(name string) (document.SourceEncoding, error) {
	switch name {
	case "binary":
		return document.BinaryEncoding(), nil
	case "utf-8":
		return document.Utf8Encoding(), nil
	case "utf-16le":
		return document.Utf16LeEncoding(), nil
	case "utf-16be":
		return document.Utf16BeEncoding(), nil
	case "latin-1":
		return document.Latin1Encoding(), nil
	}
	return document.SourceEncoding{}, fmt.Errorf("unknown encoding %q", name)
}

func sourceSnapshotFromCase(vector *caseData, name string) (*document.SourceSnapshot, error) {
	raw, ok := sourceHexInput(vector, name)
	if !ok {
		return nil, fmt.Errorf("missing input.%s", name)
	}
	request, err := sourceEncodingRequest(vector)
	if err != nil {
		return nil, err
	}
	return document.NewSourceSnapshotFromRaw(raw, request, document.DefaultSourceLimits())
}

func sourceReplacementValues(vector *caseData) ([]document.SourceReplacement, error) {
	values, ok := sequenceField(vector.Input, "replacements")
	if !ok {
		return nil, fmt.Errorf("input.replacements must be a Sequence")
	}
	replacements := make([]document.SourceReplacement, 0, len(values))
	for _, value := range values {
		start, ok := integerField(value, "old_start")
		if !ok {
			return nil, fmt.Errorf("replacement old_start must be Integer")
		}
		end, ok := integerField(value, "old_end")
		if !ok {
			return nil, fmt.Errorf("replacement old_end must be Integer")
		}
		original, ok := sourceHexField(value, "original_hex")
		if !ok {
			return nil, fmt.Errorf("replacement original_hex must be String")
		}
		replacement, ok := sourceHexField(value, "replacement_hex")
		if !ok {
			return nil, fmt.Errorf("replacement replacement_hex must be String")
		}
		replacements = append(replacements, document.NewSourceReplacement(int(start), int(end), original, replacement))
	}
	return replacements, nil
}

func sourcePatchLimits(vector *caseData) (document.SourcePatchLimits, error) {
	limits := document.DefaultSourcePatchLimits()
	if value, ok := integerField(vector.Input, "max_replacements"); ok {
		limits.MaxReplacements = int(value)
	}
	if value, ok := integerField(vector.Input, "max_patch_bytes"); ok {
		limits.MaxPatchBytes = int(value)
	}
	return limits, nil
}

func sourceHexInput(vector *caseData, name string) ([]byte, bool) {
	return sourceHexField(vector.Input, name)
}

func sourceHexField(value core.Value, name string) ([]byte, bool) {
	text, ok := stringField(value, name)
	if !ok {
		return nil, false
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

func sourceErrorCode(err error) string {
	if coded, ok := err.(interface{ Code() string }); ok {
		return coded.Code()
	}
	return "runner:unclassified"
}

func locationErrorName(err error) string {
	if locationError, ok := err.(*document.LocationError); ok {
		return locationError.Name()
	}
	return "runner:unclassified"
}

func failSourceCase(vector *caseData, report *SuiteReport, message string) {
	report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
}
