package normalized

// The source face of the normalized-result differential harness: raw
// SourceSnapshot construction with encoding-resolution requests, byte-exact
// position conversions, and SourcePatch application (roadmap §11.2
// "native result 的规范化事实" and the source-v1 capability surface;
// RFC 0016 §3.2 consema/document mapping). Mirrored by the Rust example.

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"consema.dev/consema/document"
)

// runSourceCase executes one source-face case and returns its ordered
// normalized facts.
func runSourceCase(c *fileCase) ([]string, error) {
	facts := &facts{}
	raw, err := sourceRawBytes(c.Input)
	if err != nil {
		return nil, err
	}
	request, err := buildEncodingRequest(c.Request)
	if err != nil {
		return nil, err
	}
	limits := document.DefaultSourceLimits()
	snapshot, sourceErr := document.NewSourceSnapshotFromRaw(raw, request, limits)
	if sourceErr != nil {
		facts.set("source.status", "Failed")
		facts.set("source.failure", sourceCode(sourceErr))
		facts.set("source.encoding", "")
		facts.set("source.bom", "")
		facts.set("source.declared", "")
		facts.set("source.digest", "")
		facts.set("source.len", "")
		facts.set("source.text", "")
		emitPositionFacts(facts, c.Positions, raw, nil)
		emitPatchFacts(facts, c, raw, nil, request)
		return facts.lines, nil
	}
	facts.set("source.status", "Ok")
	facts.set("source.failure", "")
	facts.set("source.encoding", snapshot.EncodingFacts().Selected().AsStr())
	facts.set("source.bom", bomName(snapshot.EncodingFacts().Bom()))
	facts.set("source.declared", encodingName(snapshot.EncodingFacts().Declaration()))
	facts.set("source.digest", snapshot.Digest().Hex())
	facts.set("source.len", strconv.Itoa(snapshot.Len()))
	if text, ok := snapshot.DecodedText(); ok {
		facts.set("source.text", escape(text))
	} else {
		facts.set("source.text", "binary")
	}
	emitPositionFacts(facts, c.Positions, raw, snapshot)
	emitPatchFacts(facts, c, raw, snapshot, request)
	return facts.lines, nil
}

// sourceRawBytes resolves the raw input bytes of a source case.
func sourceRawBytes(input *sourceInputDesc) ([]byte, error) {
	if input == nil {
		return nil, fmt.Errorf("source case without input")
	}
	if input.RawHex != "" {
		return hex.DecodeString(input.RawHex)
	}
	return []byte(input.Source), nil
}

// buildEncodingRequest builds the encoding-resolution request.
func buildEncodingRequest(desc *encodingRequestDesc) (document.EncodingRequest, error) {
	if desc == nil {
		return document.EncodingRequest{}, fmt.Errorf("source case without request")
	}
	defaultEncoding, ok := encodingByName(desc.ProfileDefault)
	if !ok {
		return document.EncodingRequest{}, fmt.Errorf("unknown profile_default %q", desc.ProfileDefault)
	}
	request := document.NewEncodingRequest(defaultEncoding)
	if desc.Declaration != "" {
		declaration, ok := encodingByName(desc.Declaration)
		if !ok {
			return document.EncodingRequest{}, fmt.Errorf("unknown declaration %q", desc.Declaration)
		}
		request = request.WithDeclaration(declaration)
	}
	if desc.CallerOverride != "" {
		override, ok := encodingByName(desc.CallerOverride)
		if !ok {
			return document.EncodingRequest{}, fmt.Errorf("unknown caller_override %q", desc.CallerOverride)
		}
		request = request.WithCallerOverride(override)
	}
	switch desc.BomPolicy {
	case "", "DetectUnicode":
		// the default
	case "TreatAsContent":
		request = request.WithBomPolicy(document.BomPolicyTreatAsContent)
	default:
		return document.EncodingRequest{}, fmt.Errorf("unknown bom_policy %q", desc.BomPolicy)
	}
	return request, nil
}

// encodingByName resolves one stable encoding name.
func encodingByName(name string) (document.SourceEncoding, bool) {
	switch name {
	case "binary":
		return document.BinaryEncoding(), true
	case "utf-8":
		return document.Utf8Encoding(), true
	case "utf-16le":
		return document.Utf16LeEncoding(), true
	case "utf-16be":
		return document.Utf16BeEncoding(), true
	case "latin-1":
		return document.Latin1Encoding(), true
	case "windows-1252":
		page, ok := document.WindowsCodePageFromNumber(1252)
		if !ok {
			return document.SourceEncoding{}, false
		}
		return document.WindowsCodePageEncoding(page), true
	}
	return document.SourceEncoding{}, false
}

// bomName renders one detected BOM fact.
func bomName(bom *document.BomKind) string {
	if bom == nil {
		return ""
	}
	return bom.Encoding().AsStr()
}

// encodingName renders one optional encoding fact.
func encodingName(encoding *document.SourceEncoding) string {
	if encoding == nil {
		return ""
	}
	return encoding.AsStr()
}

// emitPositionFacts emits the byte-exact position conversions.
func emitPositionFacts(facts *facts, positions []int, raw []byte, snapshot *document.SourceSnapshot) {
	for index, rawByte := range positions {
		key := fmt.Sprintf("source.position.%d.", index)
		if snapshot == nil {
			facts.set(key+"raw_byte", strconv.Itoa(rawByte))
			facts.set(key+"decoded_utf8", "")
			facts.set(key+"scalars", "")
			facts.set(key+"utf16", "")
			continue
		}
		position, err := snapshot.DecodedPosition(rawByte)
		if err != nil {
			facts.set(key+"raw_byte", strconv.Itoa(rawByte))
			facts.set(key+"decoded_utf8", "")
			facts.set(key+"scalars", "")
			facts.set(key+"utf16", "")
			continue
		}
		facts.set(key+"raw_byte", strconv.Itoa(position.RawByte))
		facts.set(key+"decoded_utf8", strconv.Itoa(position.DecodedUTF8Byte))
		facts.set(key+"scalars", strconv.Itoa(position.UnicodeScalarOffset))
		facts.set(key+"utf16", strconv.Itoa(position.UTF16CodeUnitOffset))
	}
}

// emitPatchFacts emits the optional SourcePatch application facts.
func emitPatchFacts(facts *facts, c *fileCase, raw []byte, snapshot *document.SourceSnapshot,
	request document.EncodingRequest) {
	key := "patch."
	if c.Patch == nil {
		facts.set(key+"status", "Skipped")
		facts.set(key+"failure", "")
		facts.set(key+"output", "")
		facts.set(key+"replacement_count", "")
		return
	}
	if snapshot == nil {
		facts.set(key+"status", "Skipped")
		facts.set(key+"failure", "")
		facts.set(key+"output", "")
		facts.set(key+"replacement_count", "")
		return
	}
	replacements, err := buildSourceReplacements(snapshot, c.Patch.Replacements)
	if err != nil {
		facts.set(key+"status", "Failed")
		facts.set(key+"failure", "core.protocol.invalid-value@1")
		facts.set(key+"output", "")
		facts.set(key+"replacement_count", "")
		return
	}
	limits := document.DefaultSourcePatchLimits()
	patch, err := document.NewSourcePatch(snapshot, replacements, nil, limits)
	if err != nil {
		facts.set(key+"status", "Failed")
		facts.set(key+"failure", sourcePatchCode(err))
		facts.set(key+"output", "")
		facts.set(key+"replacement_count", "")
		return
	}
	base := snapshot
	if c.Patch.ApplyTo == "tampered" {
		tampered := append([]byte(nil), raw...)
		if len(tampered) > 0 {
			tampered[len(tampered)-1] ^= 0x01
		}
		tamperedSnapshot, err := document.NewSourceSnapshotFromRaw(tampered, request, document.DefaultSourceLimits())
		if err != nil {
			facts.set(key+"status", "Failed")
			facts.set(key+"failure", sourcePatchCode(err))
			facts.set(key+"output", "")
			facts.set(key+"replacement_count", "")
			return
		}
		base = tamperedSnapshot
	}
	target, err := patch.Apply(base, limits)
	if err != nil {
		facts.set(key+"status", "Failed")
		facts.set(key+"failure", sourcePatchCode(err))
		facts.set(key+"output", "")
		facts.set(key+"replacement_count", "")
		return
	}
	facts.set(key+"status", "Applied")
	facts.set(key+"failure", "")
	facts.set(key+"output", escape(string(target.Bytes())))
	facts.set(key+"replacement_count", strconv.Itoa(len(replacements)))
}

// buildSourceReplacements builds the replacements from the descriptor; the
// original bytes are taken from the base snapshot (both sides do the same).
func buildSourceReplacements(snapshot *document.SourceSnapshot, descriptions []patchReplacementDesc) ([]document.SourceReplacement, error) {
	base := snapshot.Bytes()
	replacements := make([]document.SourceReplacement, 0, len(descriptions))
	for _, desc := range descriptions {
		if desc.OldStart < 0 || desc.OldEnd < desc.OldStart || desc.OldEnd > len(base) {
			return nil, fmt.Errorf("replacement range out of bounds")
		}
		replacement, err := hex.DecodeString(desc.ReplacementHex)
		if err != nil {
			return nil, err
		}
		original := base[desc.OldStart:desc.OldEnd]
		replacements = append(replacements, document.NewSourceReplacement(
			desc.OldStart, desc.OldEnd, original, replacement))
	}
	return replacements, nil
}

// sourceCode extracts the stable code of a source construction error.
func sourceCode(err error) string {
	if sourceError, ok := err.(*document.SourceError); ok {
		return sourceError.Code()
	}
	return "core.source.invalid-sequence@1"
}

// sourcePatchCode extracts the stable code of a patch construction error
// (the Go SourcePatchError and SourceError code mappings).
func sourcePatchCode(err error) string {
	switch e := err.(type) {
	case *document.SourcePatchError:
		return e.Code()
	case *document.SourceError:
		return e.Code()
	}
	return "core.protocol.invalid-value@1"
}
