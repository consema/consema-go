package document

import (
	"encoding/hex"
	"testing"

	"consema.dev/consema/protocol"
)

// The wire-consistency tests verify that the document package's facts stay
// aligned with the wire records of go/protocol/records_source.go
// (core.source-snapshot@1/@2, core.source-patch@1/@2, and
// core.source-encoding@1 share the same language-neutral facts). The
// document package is the format-facing surface; the protocol package is
// the transferable wire surface; both decode the same raw bytes to the
// same facts.

func protocolEncoding(t *testing.T, encoding SourceEncoding) *protocol.SourceEncoding {
	t.Helper()
	switch encoding.Kind() {
	case EncodingBinary:
		return &protocol.SourceEncoding{Kind: "Binary"}
	case EncodingUtf8:
		return &protocol.SourceEncoding{Kind: "Utf8"}
	case EncodingUtf16Le:
		return &protocol.SourceEncoding{Kind: "Utf16Le"}
	case EncodingUtf16Be:
		return &protocol.SourceEncoding{Kind: "Utf16Be"}
	case EncodingLatin1:
		return &protocol.SourceEncoding{Kind: "Latin1"}
	case EncodingWindowsCodePage:
		page, _ := encoding.WindowsCodePage()
		number := uint32(page.Number())
		return &protocol.SourceEncoding{Kind: "WindowsCodePage", WindowsCodePage: &number}
	}
	t.Fatalf("unexpected encoding %s", encoding.AsStr())
	return nil
}

// TestSnapshotWireFactsAgreeWithProtocolRecords compares the snapshot
// digest, selected encoding, and decoded text of the document layer with
// the protocol layer on the same raw bytes.
func TestSnapshotWireFactsAgreeWithProtocolRecords(t *testing.T) {
	raw, err := hex.DecodeString("efbbbf41f09f9880")
	if err != nil {
		t.Fatal(err)
	}
	encoding := Utf8Encoding()
	documentSnapshot, err := NewSourceSnapshotFromRaw(raw, NewEncodingRequest(encoding), DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	protocolSnapshot, err := protocol.NewSourceSnapshotFromRaw(raw,
		protocol.NewEncodingRequest(protocolEncoding(t, encoding)),
		protocol.DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	if documentSnapshot.Digest().Hex() != protocolSnapshot.Digest().Hex() {
		t.Errorf("digest %s != %s", documentSnapshot.Digest().Hex(), protocolSnapshot.Digest().Hex())
	}
	documentText, documentOK := documentSnapshot.DecodedText()
	protocolText, protocolOK := protocolSnapshot.DecodedText()
	if documentOK != protocolOK || documentText != protocolText {
		t.Errorf("decoded text %q/%v != %q/%v", documentText, documentOK, protocolText, protocolOK)
	}
	if documentSnapshot.EncodingFacts().Selected().AsStr() != "utf-8" ||
		protocolSnapshot.EncodingFacts().Selected().Kind != "Utf8" {
		t.Errorf("selected %s vs %s", documentSnapshot.EncodingFacts().Selected().AsStr(),
			protocolSnapshot.EncodingFacts().Selected().Kind)
	}
	if bom := documentSnapshot.EncodingFacts().Bom(); bom == nil || *bom != BomUtf8 {
		t.Errorf("document bom = %v", bom)
	}
}

// TestPatchWireFactsAgreeWithProtocolRecords compares the patch digest
// facts, encoding facts, and replacement facts of the two layers.
func TestPatchWireFactsAgreeWithProtocolRecords(t *testing.T) {
	base, err := NewSourceSnapshotFromUTF8([]byte("name = old\n"))
	if err != nil {
		t.Fatal(err)
	}
	protocolBase, err := protocol.NewSourceSnapshotFromUTF8([]byte("name = old\n"))
	if err != nil {
		t.Fatal(err)
	}
	documentReplacements := []SourceReplacement{
		NewSourceReplacement(0, 0, nil, []byte("# ")),
		NewSourceReplacement(7, 10, []byte("old"), []byte("new")),
	}
	documentPatch, err := NewSourcePatch(base, documentReplacements,
		map[string]string{"actor": "conformance"}, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	protocolReplacements := []protocol.SourceReplacement{
		{OldStart: 0, OldEnd: 0, Original: []byte{}, Replacement: []byte("# ")},
		{OldStart: 7, OldEnd: 10, Original: []byte("old"), Replacement: []byte("new")},
	}
	protocolPatch, err := protocol.NewSourcePatch(protocolBase, protocolReplacements,
		map[string]string{"actor": "conformance"}, protocol.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if documentPatch.BaseDigest().Hex() != protocolPatch.BaseDigest.Hex() {
		t.Errorf("base digest %s != %s", documentPatch.BaseDigest().Hex(), protocolPatch.BaseDigest.Hex())
	}
	if documentPatch.TargetDigest().Hex() != protocolPatch.TargetDigest.Hex() {
		t.Errorf("target digest %s != %s", documentPatch.TargetDigest().Hex(), protocolPatch.TargetDigest.Hex())
	}
	if documentPatch.EncodingFacts().Selected().AsStr() != "utf-8" ||
		protocolPatch.Encoding.Selected == nil || protocolPatch.Encoding.Selected.Kind != "Utf8" {
		t.Errorf("selected encoding drift")
	}
	if documentPatch.EncodingFacts().BomPolicy() != BomPolicyDetectUnicode ||
		protocolPatch.Encoding.BomPolicy != string(protocol.BomPolicyDetectUnicode) {
		t.Errorf("BOM policy drift")
	}
	documentReplacementsOut := documentPatch.Replacements()
	if len(documentReplacementsOut) != len(protocolPatch.Replacements) {
		t.Fatalf("replacement count %d != %d", len(documentReplacementsOut), len(protocolPatch.Replacements))
	}
	for index := range documentReplacementsOut {
		documentReplacement := documentReplacementsOut[index]
		protocolReplacement := protocolPatch.Replacements[index]
		if documentReplacement.OldStart() != int(protocolReplacement.OldStart) ||
			documentReplacement.OldEnd() != int(protocolReplacement.OldEnd) {
			t.Errorf("replacement %d range drift", index)
		}
		if hex.EncodeToString(documentReplacement.Original()) != hex.EncodeToString(protocolReplacement.Original) ||
			hex.EncodeToString(documentReplacement.Replacement()) != hex.EncodeToString(protocolReplacement.Replacement) {
			t.Errorf("replacement %d bytes drift", index)
		}
	}
	if documentPatch.Metadata()["actor"] != protocolPatch.Metadata["actor"] {
		t.Error("metadata drift")
	}
	// Both layers produce the same target bytes.
	documentTarget, err := documentPatch.Apply(base, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	protocolTargetBytes, err := protocol.ApplySourcePatch(protocolPatch, protocolBase, protocol.DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(documentTarget.Bytes()) != hex.EncodeToString(protocolTargetBytes) {
		t.Errorf("target bytes %x != %x", documentTarget.Bytes(), protocolTargetBytes)
	}
}

// TestWindowsCodePageWireAgreement compares the document layer's code-page
// decoding with the protocol layer byte-for-byte over every byte of the
// nine single-byte pages (874, 1250-1258) and a CP932 base-row code. The
// two layers share the encoding_rs 0.8.35 authority data, so every byte
// must decode to the same scalar or fail with the same invalid-sequence
// outcome in both.
func TestWindowsCodePageWireAgreement(t *testing.T) {
	for _, number := range []uint16{874, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258} {
		page, ok := WindowsCodePageFromNumber(number)
		if !ok {
			t.Fatalf("cp%d unpublished", number)
		}
		documentRequest := NewEncodingRequest(WindowsCodePageEncoding(page)).
			WithBomPolicy(BomPolicyTreatAsContent)
		protocolRequest := protocol.NewEncodingRequest(protocolEncoding(t, WindowsCodePageEncoding(page))).
			WithBomPolicy(protocol.BomPolicyTreatAsContent)
		for b := 0; b < 256; b++ {
			raw := []byte{byte(b)}
			documentSnapshot, documentErr := NewSourceSnapshotFromRaw(raw, documentRequest, DefaultSourceLimits())
			protocolSnapshot, protocolErr := protocol.NewSourceSnapshotFromRaw(raw,
				protocolRequest, protocol.DefaultSourceLimits())
			documentText, documentOK := "", false
			if documentSnapshot != nil {
				documentText, documentOK = documentSnapshot.DecodedText()
			}
			protocolText, protocolOK := "", false
			if protocolSnapshot != nil {
				protocolText, protocolOK = protocolSnapshot.DecodedText()
			}
			if documentOK != protocolOK || documentText != protocolText {
				t.Errorf("cp%d 0x%02X: document %q/%v vs protocol %q/%v (document err %v, protocol err %v)",
					number, b, documentText, documentOK, protocolText, protocolOK,
					documentErr, protocolErr)
			}
		}
	}

	// Full 1252 sweep in one snapshot: 0x80 is the euro sign and the five
	// historically "undefined" bytes decode to their C1 control scalars in
	// both layers (encoding_rs 0.8.35 authority: windows-1252 has no
	// malformed bytes).
	sweep := make([]byte, 128)
	for index := range sweep {
		sweep[index] = byte(0x80 + index)
	}
	cp1252, ok := WindowsCodePageFromNumber(1252)
	if !ok {
		t.Fatal("cp1252 unpublished")
	}
	document1252, err := NewSourceSnapshotFromRaw(sweep,
		NewEncodingRequest(WindowsCodePageEncoding(cp1252)).WithBomPolicy(BomPolicyTreatAsContent),
		DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	protocol1252, err := protocol.NewSourceSnapshotFromRaw(sweep,
		protocol.NewEncodingRequest(protocolEncoding(t, WindowsCodePageEncoding(cp1252))).
			WithBomPolicy(protocol.BomPolicyTreatAsContent),
		protocol.DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	documentText, _ := document1252.DecodedText()
	protocolText, _ := protocol1252.DecodedText()
	if documentText != protocolText {
		t.Errorf("cp1252 sweep decoded text differs:\n%s\n%s", documentText, protocolText)
	}
	if documentText[0:3] != "€" || documentText[3:5] != "\xc2\x81" {
		t.Errorf("cp1252 0x80/0x81 = %q, want euro + U+0081", documentText[0:6])
	}

	cp932, ok := WindowsCodePageFromNumber(932)
	if !ok {
		t.Fatal("cp932 unpublished")
	}
	raw := []byte{0x82, 0xa0, 0xa1}
	document932, err := NewSourceSnapshotFromRaw(raw,
		NewEncodingRequest(WindowsCodePageEncoding(cp932)).WithBomPolicy(BomPolicyTreatAsContent),
		DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	protocol932, err := protocol.NewSourceSnapshotFromRaw(raw,
		protocol.NewEncodingRequest(protocolEncoding(t, WindowsCodePageEncoding(cp932))).
			WithBomPolicy(protocol.BomPolicyTreatAsContent),
		protocol.DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	document932Text, _ := document932.DecodedText()
	protocol932Text, _ := protocol932.DecodedText()
	if document932Text != protocol932Text {
		t.Errorf("cp932 decoded text %q != %q", document932Text, protocol932Text)
	}
}
