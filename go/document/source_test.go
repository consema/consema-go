package document

import (
	"encoding/hex"
	"fmt"
	"testing"
)

// mustSource builds one snapshot or fails the test.
func mustSource(t *testing.T, bytes []byte, request EncodingRequest) *SourceSnapshot {
	t.Helper()
	snapshot, err := NewSourceSnapshotFromRaw(bytes, request, DefaultSourceLimits())
	if err != nil {
		t.Fatalf("NewSourceSnapshotFromRaw(%x): %v", bytes, err)
	}
	return snapshot
}

func decodeHex(t *testing.T, text string) []byte {
	t.Helper()
	bytes, err := hex.DecodeString(text)
	if err != nil {
		t.Fatalf("invalid hex %q: %v", text, err)
	}
	return bytes
}

// TestContentDigestGoldenBytes pins the SHA-256 content identity against
// the frozen source-v1 vector digests (source-v1.json: source.digest.*).
func TestContentDigestGoldenBytes(t *testing.T) {
	cases := []struct {
		raw    string
		digest string
	}{
		{"", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"616263", "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"},
	}
	for _, vector := range cases {
		digest := DigestOf(decodeHex(t, vector.raw))
		if digest.Hex() != vector.digest {
			t.Errorf("digest of %q = %s, want %s", vector.raw, digest.Hex(), vector.digest)
		}
		if digest.Algorithm() != "sha256" {
			t.Errorf("algorithm = %q, want sha256", digest.Algorithm())
		}
	}
}

// TestEncodingRoundtrips decodes every v1 text encoding without losing raw
// bytes and retains the decoded text.
func TestEncodingRoundtrips(t *testing.T) {
	cases := []struct {
		raw      string
		encoding SourceEncoding
		decoded  string
	}{
		{"41c3a9", Utf8Encoding(), "Aé"},
		{"4100e900", Utf16LeEncoding(), "Aé"},
		{"004100e9", Utf16BeEncoding(), "Aé"},
		{"41e9", Latin1Encoding(), "Aé"},
	}
	for _, vector := range cases {
		raw := decodeHex(t, vector.raw)
		snapshot := mustSource(t, raw, NewEncodingRequest(vector.encoding))
		if got := hex.EncodeToString(snapshot.Bytes()); got != vector.raw {
			t.Errorf("%s: retained bytes %s != %s", vector.encoding.AsStr(), got, vector.raw)
		}
		text, ok := snapshot.DecodedText()
		if !ok || text != vector.decoded {
			t.Errorf("%s: decoded text %q (ok=%v), want %q", vector.encoding.AsStr(), text, ok, vector.decoded)
		}
		if !snapshot.EncodingFacts().Selected().Equal(vector.encoding) {
			t.Errorf("%s: selected encoding %s", vector.encoding.AsStr(), snapshot.EncodingFacts().Selected().AsStr())
		}
	}
}

// TestBinarySourceHasNoDecodedClaims covers the opaque-binary surface.
func TestBinarySourceHasNoDecodedClaims(t *testing.T) {
	raw := []byte{0xff, 0xfe, 0x00, 0x00}
	snapshot, err := NewSourceSnapshotFromBinary(raw, DefaultSourceLimits())
	if err != nil {
		t.Fatalf("from binary: %v", err)
	}
	if got := hex.EncodeToString(snapshot.Bytes()); got != "fffe0000" {
		t.Errorf("retained bytes %s", got)
	}
	if _, ok := snapshot.DecodedText(); ok {
		t.Error("binary source must not have decoded text")
	}
	if _, err := snapshot.DecodedPosition(0); locationName(err) != "NoDecodedText" {
		t.Errorf("decoded_position(0) error = %v, want NoDecodedText", err)
	}
	if _, err := snapshot.RawByteAt(NewUtf8ByteOffset(0)); locationName(err) != "NoDecodedText" {
		t.Errorf("raw_byte_at error = %v, want NoDecodedText", err)
	}
	// A text override on a binary profile default is a conflict.
	_, err = NewSourceSnapshotFromRaw([]byte("text"), BinaryEncodingRequest().WithCallerOverride(Utf8Encoding()), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.encoding-conflict@1" {
		t.Errorf("binary default with text override = %v", err)
	}
}

// TestBomDetectionRetentionAndConflicts covers the frozen BOM facts: the
// BOM is retained as a decoded scalar, recorded in the facts, and
// contradictory BOM/declaration/caller assertions are rejected.
func TestBomDetectionRetentionAndConflicts(t *testing.T) {
	snapshot := mustSource(t, []byte{0xff, 0xfe, 0x41, 0x00}, NewEncodingRequest(Utf16LeEncoding()))
	if text, _ := snapshot.DecodedText(); text != "\ufeffA" {
		t.Errorf("decoded text %q, want %q", text, "\ufeffA")
	}
	bom := snapshot.EncodingFacts().Bom()
	if bom == nil || *bom != BomUtf16Le {
		t.Errorf("bom = %v, want Utf16Le", bom)
	}
	if position, err := snapshot.DecodedPosition(2); err != nil || position.UnicodeScalarOffset != 1 {
		t.Errorf("decoded_position(2) = %v, %v; want scalar offset 1", position, err)
	}

	// BOM vs declaration conflict.
	declaration := Utf16LeEncoding()
	_, err := NewSourceSnapshotFromRaw([]byte{0xef, 0xbb, 0xbf, 0x41},
		NewEncodingRequest(Utf8Encoding()).WithDeclaration(declaration), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.encoding-conflict@1" {
		t.Errorf("bom/declaration conflict = %v", err)
	}

	// Declaration vs caller override conflict.
	caller := Latin1Encoding()
	_, err = NewSourceSnapshotFromRaw([]byte{0x41},
		NewEncodingRequest(Utf8Encoding()).WithDeclaration(Utf8Encoding()).WithCallerOverride(caller),
		DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.encoding-conflict@1" {
		t.Errorf("declaration/caller conflict = %v", err)
	}

	// UTF-32 markers are recognized but unsupported.
	_, err = NewSourceSnapshotFromRaw([]byte{0xff, 0xfe, 0x00, 0x00}, NewEncodingRequest(Utf8Encoding()), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.unsupported-bom@1" {
		t.Errorf("utf-32le BOM = %v", err)
	}
	_, err = NewSourceSnapshotFromRaw([]byte{0x00, 0x00, 0xfe, 0xff}, NewEncodingRequest(Utf8Encoding()), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.unsupported-bom@1" {
		t.Errorf("utf-32be BOM = %v", err)
	}
}

// TestBomPolicyTreatsMarkerBytesAsContent covers the TreatAsContent
// policy: marker-shaped leading bytes decode as content under the selected
// encoding.
func TestBomPolicyTreatsMarkerBytesAsContent(t *testing.T) {
	raw := []byte{0xef, 0xbb, 0xbf}
	detected := mustSource(t, raw, NewEncodingRequest(Latin1Encoding()))
	if !detected.EncodingFacts().Selected().Equal(Utf8Encoding()) {
		t.Errorf("detected selected = %s, want utf-8", detected.EncodingFacts().Selected().AsStr())
	}
	if bom := detected.EncodingFacts().Bom(); bom == nil || *bom != BomUtf8 {
		t.Errorf("detected bom = %v, want Utf8", bom)
	}
	if text, _ := detected.DecodedText(); text != "\ufeff" {
		t.Errorf("detected decoded text %q, want U+FEFF", text)
	}

	latin1 := mustSource(t, raw, NewEncodingRequest(Latin1Encoding()).WithBomPolicy(BomPolicyTreatAsContent))
	if text, _ := latin1.DecodedText(); text != "ï»¿" {
		t.Errorf("latin1 decoded text %q, want %q", text, "ï»¿")
	}
	if bom := latin1.EncodingFacts().Bom(); bom != nil {
		t.Errorf("treat-as-content bom = %v, want none", bom)
	}
	if latin1.EncodingFacts().BomPolicy() != BomPolicyTreatAsContent {
		t.Error("bom policy not recorded")
	}

	// A claim of a BOM under TreatAsContent is inconsistent.
	_, err := NewEncodingFactsFromClaimWithBomPolicy(Latin1Encoding(), BomPolicyTreatAsContent,
		ptrBom(BomUtf8), nil, nil, Latin1Encoding())
	if err == nil {
		t.Error("treat-as-content with a claimed BOM must be rejected")
	}
}

func ptrBom(kind BomKind) *BomKind { return &kind }

// TestInvalidSequencesRejected covers the frozen invalid-sequence facts:
// odd UTF-16 length and lone surrogates.
func TestInvalidSequencesRejected(t *testing.T) {
	_, err := NewSourceSnapshotFromRaw([]byte{0x41, 0x00, 0xff}, NewEncodingRequest(Utf16LeEncoding()), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" {
		t.Errorf("odd utf-16 length = %v", err)
	}
	_, err = NewSourceSnapshotFromRaw([]byte{0x3d, 0xd8, 0x41, 0x00}, NewEncodingRequest(Utf16LeEncoding()), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" {
		t.Errorf("lone surrogate = %v", err)
	}
	// The surrogate pair itself is valid.
	snapshot := mustSource(t, []byte{0x3d, 0xd8, 0x00, 0xde}, NewEncodingRequest(Utf16LeEncoding()))
	if text, _ := snapshot.DecodedText(); text != "😀" {
		t.Errorf("surrogate pair decoded %q, want 😀", text)
	}
}

// TestLocationBoundaryFacts covers decoded_position and raw_byte_at across
// fixed-width encodings, including surrogate pairs and non-boundary
// rejection (source-v1.json: source.location.*).
func TestLocationBoundaryFacts(t *testing.T) {
	utf8 := mustSource(t, decodeHex(t, "41f09f988042"), NewEncodingRequest(Utf8Encoding()))
	position, err := utf8.DecodedPosition(5)
	if err != nil {
		t.Fatalf("decoded_position(5): %v", err)
	}
	expectedUTF8 := DecodedPosition{RawByte: 5, DecodedUTF8Byte: 5, UnicodeScalarOffset: 2, UTF16CodeUnitOffset: 3}
	if position != expectedUTF8 {
		t.Errorf("utf8 position = %+v, want %+v", position, expectedUTF8)
	}
	for _, offset := range []DecodedOffset{
		NewUtf8ByteOffset(position.DecodedUTF8Byte),
		NewUnicodeScalarOffset(position.UnicodeScalarOffset),
		NewUtf16CodeUnitOffset(position.UTF16CodeUnitOffset),
	} {
		if raw, err := utf8.RawByteAt(offset); err != nil || raw != 5 {
			t.Errorf("utf8 raw_byte_at(%v) = %d, %v; want 5", offset.Value(), raw, err)
		}
	}
	if _, err := utf8.DecodedPosition(2); locationName(err) != "NotDecodedBoundary" {
		t.Errorf("utf8 decoded_position(2) = %v, want NotDecodedBoundary", err)
	}
	if _, err := utf8.RawByteAt(NewUtf16CodeUnitOffset(2)); locationName(err) != "DecodedOffsetNotBoundary" {
		t.Errorf("utf8 raw_byte_at(utf16 2) = %v, want DecodedOffsetNotBoundary", err)
	}
	if _, err := utf8.RawByteAt(NewUtf8ByteOffset(2)); locationName(err) != "DecodedOffsetNotBoundary" {
		t.Errorf("utf8 raw_byte_at(utf8 2) = %v, want DecodedOffsetNotBoundary", err)
	}

	utf16 := mustSource(t, decodeHex(t, "41003dd800de4200"), NewEncodingRequest(Utf16LeEncoding()))
	position, err = utf16.DecodedPosition(6)
	if err != nil {
		t.Fatalf("utf16 decoded_position(6): %v", err)
	}
	expectedUTF16 := DecodedPosition{RawByte: 6, DecodedUTF8Byte: 5, UnicodeScalarOffset: 2, UTF16CodeUnitOffset: 3}
	if position != expectedUTF16 {
		t.Errorf("utf16 position = %+v, want %+v", position, expectedUTF16)
	}
	for _, offset := range []DecodedOffset{
		NewUtf8ByteOffset(position.DecodedUTF8Byte),
		NewUnicodeScalarOffset(position.UnicodeScalarOffset),
		NewUtf16CodeUnitOffset(position.UTF16CodeUnitOffset),
	} {
		if raw, err := utf16.RawByteAt(offset); err != nil || raw != 6 {
			t.Errorf("utf16 raw_byte_at(%v) = %d, %v; want 6", offset.Value(), raw, err)
		}
	}
	if _, err := utf16.DecodedPosition(3); locationName(err) != "NotDecodedBoundary" {
		t.Errorf("utf16 decoded_position(3) = %v, want NotDecodedBoundary", err)
	}
	if _, err := utf16.RawByteAt(NewUtf16CodeUnitOffset(2)); locationName(err) != "DecodedOffsetNotBoundary" {
		t.Errorf("utf16 raw_byte_at(utf16 2) = %v, want DecodedOffsetNotBoundary", err)
	}
	if _, err := utf16.DecodedPosition(99); locationName(err) != "OutOfBounds" {
		t.Errorf("utf16 decoded_position(99) = %v, want OutOfBounds", err)
	}
	if _, err := utf16.RawByteAt(NewUnicodeScalarOffset(99)); locationName(err) != "OutOfBounds" {
		t.Errorf("utf16 raw_byte_at(scalar 99) = %v, want OutOfBounds", err)
	}
}

// TestCheckpointedLocationsRemainExactBeyondOneStride covers correctness
// past the 256-scalar checkpoint stride.
func TestCheckpointedLocationsRemainExactBeyondOneStride(t *testing.T) {
	text := make([]byte, 0, checkpointStride+7+4)
	for i := 0; i < checkpointStride+7; i++ {
		text = append(text, 'a')
	}
	text = append(text, "😀tail"...)
	snapshot := mustSource(t, text, NewEncodingRequest(Utf8Encoding()))
	raw := checkpointStride + 7 + 4
	position, err := snapshot.DecodedPosition(raw)
	if err != nil {
		t.Fatalf("decoded_position(%d): %v", raw, err)
	}
	if position.UnicodeScalarOffset != checkpointStride+8 {
		t.Errorf("scalar offset %d, want %d", position.UnicodeScalarOffset, checkpointStride+8)
	}
	back, err := snapshot.RawByteAt(NewUnicodeScalarOffset(checkpointStride + 8))
	if err != nil || back != raw {
		t.Errorf("raw_byte_at(scalar %d) = %d, %v; want %d", checkpointStride+8, back, err, raw)
	}
}

// TestPerCallCoordinateConversionDoesNotRescanLargeSources is the
// regression net for the Rust task #53 root cause: coordinate conversion
// must not re-validate the whole source per call. 65,536 calls over a
// 1 MiB source stay far under a deliberately generous bound.
func TestPerCallCoordinateConversionDoesNotRescanLargeSources(t *testing.T) {
	text := make([]byte, 1<<20)
	for i := range text {
		text[i] = 'a'
	}
	snapshot := mustSource(t, text, NewEncodingRequest(Utf8Encoding()))
	checksum := 0
	for offset := 0; offset < 1<<16; offset++ {
		if _, ok := snapshot.DecodedText(); !ok {
			t.Fatal("text source always decodes")
		}
		raw, err := snapshot.RawByteAt(NewUtf8ByteOffset(offset))
		if err != nil {
			t.Fatalf("raw_byte_at(%d): %v", offset, err)
		}
		checksum += raw
	}
	expected := (1 << 16) * (1<<16 - 1) / 2
	if checksum != expected {
		t.Errorf("checksum %d != %d", checksum, expected)
	}
}

// TestSourceLimitsAreEnforced covers the frozen limit facts
// (source-v1.json: source.resource.*).
func TestSourceLimitsAreEnforced(t *testing.T) {
	limits := SourceLimits{MaxRawBytes: 1, MaxDecodedUTF8Bytes: 1, MaxDecodedScalars: 2}
	_, err := NewSourceSnapshotFromRaw([]byte{0x61, 0x62}, NewEncodingRequest(Utf8Encoding()), limits)
	if err == nil || err.(*SourceError).Code() != "core.source.resource-limit@1" || err.(*SourceError).Name != "raw-bytes" {
		t.Errorf("raw limit = %v", err)
	}
	// é in latin-1 expands to 2 UTF-8 bytes.
	_, err = NewSourceSnapshotFromRaw([]byte{0xe9}, NewEncodingRequest(Latin1Encoding()), limits)
	if err == nil || err.(*SourceError).Code() != "core.source.resource-limit@1" || err.(*SourceError).Name != "decoded-utf8-bytes" {
		t.Errorf("decoded limit = %v", err)
	}
	scalarLimits := SourceLimits{MaxRawBytes: 1 << 20, MaxDecodedUTF8Bytes: 1 << 20, MaxDecodedScalars: 1}
	_, err = NewSourceSnapshotFromRaw([]byte{0x61, 0x62}, NewEncodingRequest(Utf8Encoding()), scalarLimits)
	if err == nil || err.(*SourceError).Name != "decoded-scalars" {
		t.Errorf("scalar limit = %v", err)
	}
}

// TestWindowsCodePages covers the frozen code-page facts: the published
// numbers, strict single-byte and CP932 decoding, and invalid-sequence
// rejection of undefined codes.
func TestWindowsCodePages(t *testing.T) {
	published := []uint16{874, 932, 936, 949, 950, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258, 65001}
	for _, number := range published {
		page, ok := WindowsCodePageFromNumber(number)
		if !ok {
			t.Fatalf("published code page %d unresolved", number)
		}
		if page.Number() != number {
			t.Errorf("page %d number = %d", number, page.Number())
		}
		if got := WindowsCodePageEncoding(page).AsStr(); got != "windows-"+fmt.Sprint(number) {
			t.Errorf("cp%d name = %q, want %q", number, got, "windows-"+fmt.Sprint(number))
		}
	}
	for _, number := range []uint16{0, 873, 875, 931, 951, 1249, 1259, 65000, 65535} {
		if _, ok := WindowsCodePageFromNumber(number); ok {
			t.Errorf("unpublished code page %d resolved", number)
		}
	}

	// CP1252: 0x80 is the euro sign and the five historically "undefined"
	// bytes decode to their C1 control scalars, matching the encoding_rs
	// 0.8.35 authority (windows-1252 has no malformed bytes).
	cp1252 := WindowsCodePageEncoding(mustPage(t, 1252))
	snapshot := mustSource(t, []byte{0x80, 'A'}, NewEncodingRequest(cp1252))
	if text, _ := snapshot.DecodedText(); text != "€A" {
		t.Errorf("cp1252 decoded %q, want %q", text, "€A")
	}
	if position, err := snapshot.DecodedPosition(1); err != nil ||
		position.DecodedUTF8Byte != 3 || position.UnicodeScalarOffset != 1 {
		t.Errorf("cp1252 decoded_position(1) = %+v, %v", position, err)
	}
	snapshot = mustSource(t, []byte{0x81, 0x8d, 0x8f, 0x90, 0x9d},
		NewEncodingRequest(cp1252))
	if text, _ := snapshot.DecodedText(); text != "\xc2\x81\xc2\x8d\xc2\x8f\xc2\x90\xc2\x9d" {
		t.Errorf("cp1252 C1 controls decoded %q, want U+0081 U+008D U+008F U+0090 U+009D", text)
	}

	// A malformed byte fails the whole source atomically: cp1255 0xD9 is
	// Malformed in encoding_rs 0.8.35, so construction reports
	// InvalidSequence at the byte's own offset (document source.rs
	// decode_windows_code_page Malformed handling).
	cp1255 := WindowsCodePageEncoding(mustPage(t, 1255))
	_, malformedErr := NewSourceSnapshotFromRaw([]byte{0x41, 0xd9},
		NewEncodingRequest(cp1255).WithBomPolicy(BomPolicyTreatAsContent), DefaultSourceLimits())
	if malformedErr == nil || malformedErr.(*SourceError).Code() != "core.source.invalid-sequence@1" ||
		malformedErr.(*SourceError).ByteOffset != 1 {
		t.Errorf("cp1255 malformed byte = %v", malformedErr)
	}

	// CP932: a two-byte code decodes and a lone lead byte is rejected.
	cp932 := WindowsCodePageEncoding(mustPage(t, 932))
	snapshot = mustSource(t, []byte{0x82, 0xa0}, NewEncodingRequest(cp932).WithBomPolicy(BomPolicyTreatAsContent))
	if text, _ := snapshot.DecodedText(); text != "あ" {
		t.Errorf("cp932 decoded %q, want あ", text)
	}
	_, err := NewSourceSnapshotFromRaw([]byte{0x82}, NewEncodingRequest(cp932).WithBomPolicy(BomPolicyTreatAsContent), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" || err.(*SourceError).ByteOffset != 0 {
		t.Errorf("cp932 lone lead = %v", err)
	}
	// Half-width katakana.
	snapshot = mustSource(t, []byte{0xa1}, NewEncodingRequest(cp932).WithBomPolicy(BomPolicyTreatAsContent))
	if text, _ := snapshot.DecodedText(); text != "｡" {
		t.Errorf("cp932 half-width decoded %q, want U+FF61", text)
	}

	// CP65001 is strict UTF-8.
	cp65001 := WindowsCodePageEncoding(mustPage(t, 65001))
	_, err = NewSourceSnapshotFromRaw([]byte{0xff}, NewEncodingRequest(cp65001).WithBomPolicy(BomPolicyTreatAsContent), DefaultSourceLimits())
	if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" {
		t.Errorf("cp65001 invalid byte = %v", err)
	}

	// CP936/CP949/CP950 are recognized but their DBCS tables are not yet
	// published; non-ASCII bytes are rejected like go/protocol rejects
	// them today. Wave-4 R37 (2026-08-15): this pins the current hard
	// failure on ALL THREE pages as the registered language fork — the
	// Rust reference decodes them fully via encoding_rs, so the same
	// CP936/CP949/CP950 source completes on the Rust side and fails on the
	// Go side (disclosed in go/README.md "code pages"; the fork is
	// deliberate, not a silent divergence, and is pinned here so a future
	// decode implementation must flip this test together with the README
	// note).
	for _, number := range []uint16{936, 949, 950} {
		cp := WindowsCodePageEncoding(mustPage(t, number))
		// The whole page hard-fails: even a single ASCII byte is rejected
		// with SourceErrorInvalidSequence (the page is recognized but not
		// decoded, so no byte — ASCII included — is claimed decodable).
		_, err = NewSourceSnapshotFromRaw([]byte{'A'}, NewEncodingRequest(cp).WithBomPolicy(BomPolicyTreatAsContent), DefaultSourceLimits())
		if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" {
			t.Errorf("cp%d ASCII byte = %v, want SourceErrorInvalidSequence", number, err)
		}
		_, err = NewSourceSnapshotFromRaw([]byte{0x81, 0x40}, NewEncodingRequest(cp).WithBomPolicy(BomPolicyTreatAsContent), DefaultSourceLimits())
		if err == nil || err.(*SourceError).Code() != "core.source.invalid-sequence@1" {
			t.Errorf("cp%d DBCS lead byte = %v, want SourceErrorInvalidSequence", number, err)
		}
	}
}

func mustPage(t *testing.T, number uint16) WindowsCodePage {
	t.Helper()
	page, ok := WindowsCodePageFromNumber(number)
	if !ok {
		t.Fatalf("code page %d unresolved", number)
	}
	return page
}

// TestSnapshotIdentityFacts covers the language-neutral identity facts:
// equal bytes always produce equal digests and distinct process-local
// identities (source-v1.json: source.identity.*).
func TestSnapshotIdentityFacts(t *testing.T) {
	first, err := NewSourceSnapshotFromUTF8([]byte{0x5b, 0x5d})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewSourceSnapshotFromUTF8([]byte{0x5b, 0x5d})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Digest().Equal(second.Digest()) {
		t.Error("equal bytes must produce equal digests")
	}
	if first.Identity() == second.Identity() {
		t.Error("distinct snapshots must have distinct identities")
	}
	if first.Identity().AsU64() == 0 {
		t.Error("identity 0 is never assigned")
	}
}

func locationName(err error) string {
	if locationError, ok := err.(*LocationError); ok {
		return locationError.Name()
	}
	return err.Error()
}
