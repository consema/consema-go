package document

import (
	"encoding/hex"
	"strings"
	"testing"
)

func utf8Source(t *testing.T, text string) *SourceSnapshot {
	t.Helper()
	snapshot, err := NewSourceSnapshotFromUTF8([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func patchMetadata() map[string]string {
	return map[string]string{"actor": "test"}
}

// TestPatchCreationAndApplicationAreExactAndRepeatable covers the
// create/apply round-trip, exact target bytes, digest identity, and
// replayability (source-v1.json: source.patch.success).
func TestPatchCreationAndApplicationAreExactAndRepeatable(t *testing.T) {
	base := utf8Source(t, "name = old\n")
	replacements := []SourceReplacement{
		NewSourceReplacement(0, 0, nil, []byte("# ")),
		NewSourceReplacement(7, 10, []byte("old"), []byte("new")),
	}
	patch, err := NewSourcePatch(base, replacements, patchMetadata(), DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	first, err := patch.Apply(base, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	second, err := patch.Apply(base, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got := string(first.Bytes()); got != "# name = new\n" {
		t.Errorf("target bytes %q", got)
	}
	if !first.Digest().Equal(patch.TargetDigest()) {
		t.Error("applied digest != patch target digest")
	}
	if hex.EncodeToString(second.Bytes()) != hex.EncodeToString(first.Bytes()) {
		t.Error("apply is not repeatable")
	}
	if patch.Metadata()["actor"] != "test" {
		t.Error("metadata not retained")
	}
	if patch.BaseDigest().Hex() != base.Digest().Hex() {
		t.Error("base digest")
	}
}

// TestPatchPreconditionFailures covers every frozen negative fact
// (source-v1.json: source.patch.reject-*).
func TestPatchPreconditionFailures(t *testing.T) {
	base := utf8Source(t, "abc")

	// Stale base: the base digest does not match the patch.
	stale, err := NewSourceSnapshotFromUTF8([]byte("abd"))
	if err != nil {
		t.Fatal(err)
	}
	patch, err := NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(1, 2, []byte("b"), []byte("B")),
	}, nil, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := patch.Apply(stale, DefaultSourcePatchLimits()); codeOf(err) != "core.source.patch-base-mismatch@1" {
		t.Errorf("stale base = %v", err)
	}

	// Wrong original: the declared precondition bytes do not appear.
	wrong, err := NewSourcePatchFromFacts(base.Digest(), DigestOf([]byte("aBc")), base.EncodingFacts(),
		[]SourceReplacement{NewSourceReplacement(1, 2, []byte("x"), []byte("B"))},
		nil, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrong.Apply(base, DefaultSourcePatchLimits()); codeOf(err) != "core.source.patch-original-mismatch@1" {
		t.Errorf("wrong original = %v", err)
	}

	// Overlapping old ranges are not valid patches.
	_, err = NewSourcePatch(utf8Source(t, "abcdef"), []SourceReplacement{
		NewSourceReplacement(1, 4, []byte("bcd"), nil),
		NewSourceReplacement(3, 5, []byte("de"), nil),
	}, nil, DefaultSourcePatchLimits())
	if err == nil || codeOf(err) != "core.protocol.invalid-value@1" {
		t.Errorf("overlap = %v, want core.protocol.invalid-value@1", err)
	}

	// Two insertions at the same zero-width point are rejected.
	_, err = NewSourcePatch(utf8Source(t, "abc"), []SourceReplacement{
		NewSourceReplacement(2, 2, nil, []byte("x")),
		NewSourceReplacement(2, 2, nil, []byte("y")),
	}, nil, DefaultSourcePatchLimits())
	if err == nil || codeOf(err) != "core.protocol.invalid-value@1" {
		t.Errorf("duplicate insertion = %v", err)
	}

	// A wrong target digest is rejected at application.
	wrongTarget, err := NewSourcePatchFromFacts(base.Digest(), DigestOf([]byte("deliberately-wrong-target")),
		base.EncodingFacts(),
		[]SourceReplacement{NewSourceReplacement(0, 2, []byte("ab"), []byte("cd"))},
		nil, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongTarget.Apply(base, DefaultSourcePatchLimits()); codeOf(err) != "core.source.patch-target-mismatch@1" {
		t.Errorf("wrong target = %v", err)
	}

	// Encoding drift is rejected with the encoding-conflict code
	// (source-v1.json: source.patch.reject-encoding-change).
	latinBase, err := NewSourceSnapshotFromRaw([]byte("ab"), NewEncodingRequest(Latin1Encoding()), DefaultSourceLimits())
	if err != nil {
		t.Fatal(err)
	}
	encodingDrift, err := NewSourcePatchFromFacts(latinBase.Digest(), DigestOf([]byte{0xff, 0xfe, 0x41, 0x00}),
		latinBase.EncodingFacts(),
		[]SourceReplacement{NewSourceReplacement(0, 2, []byte("ab"), []byte{0xff, 0xfe, 0x41, 0x00})},
		nil, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encodingDrift.Apply(latinBase, DefaultSourcePatchLimits()); codeOf(err) != "core.source.encoding-conflict@1" {
		t.Errorf("encoding drift = %v, want core.source.encoding-conflict@1", err)
	}
}

// TestPatchLimitsAreCheckedBeforeTargetAllocation covers the patch budget
// facts (source-v1.json: source.resource.patch-count-limit).
func TestPatchLimitsAreCheckedBeforeTargetAllocation(t *testing.T) {
	base := utf8Source(t, "a")
	limits := DefaultSourcePatchLimits()
	limits.MaxReplacements = 0
	_, err := NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(1, 1, nil, []byte("b")),
	}, nil, limits)
	if err == nil || codeOf(err) != "core.source.resource-limit@1" {
		t.Errorf("replacement count limit = %v", err)
	}
	limits = DefaultSourcePatchLimits()
	limits.Source.MaxRawBytes = 2
	_, err = NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(1, 1, nil, []byte("large")),
	}, nil, limits)
	if err == nil || codeOf(err) != "core.source.resource-limit@1" {
		t.Errorf("target raw bytes limit = %v", err)
	}
}

// TestPatchReplacementOrderValidation covers the canonical-order rules:
// inverted ranges, length disagreements, and order violations.
func TestPatchReplacementOrderValidation(t *testing.T) {
	base := utf8Source(t, "abc")
	if _, err := NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(2, 1, nil, []byte("x")),
	}, nil, DefaultSourcePatchLimits()); codeOf(err) != "core.protocol.invalid-value@1" {
		t.Errorf("inverted range = %v", err)
	}
	if _, err := NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(1, 3, []byte("b"), []byte("x")),
	}, nil, DefaultSourcePatchLimits()); codeOf(err) != "core.protocol.invalid-value@1" {
		t.Errorf("original length disagreement = %v", err)
	}
	// Unsorted replacement order is rejected.
	if _, err := NewSourcePatch(base, []SourceReplacement{
		NewSourceReplacement(1, 2, []byte("b"), []byte("B")),
		NewSourceReplacement(0, 1, []byte("a"), []byte("A")),
	}, nil, DefaultSourcePatchLimits()); codeOf(err) != "core.protocol.invalid-value@1" {
		t.Errorf("unsorted order = %v", err)
	}
}

// TestPatchRedactionHidesPayloadsFromPresentation covers the redacted
// review presentation; exact bytes remain available for application.
func TestPatchRedactionHidesPayloadsFromPresentation(t *testing.T) {
	base := utf8Source(t, "secret")
	replacement := NewSourceReplacement(0, 6, []byte("secret"), []byte("hidden")).
		WithOriginalRedacted(true).WithReplacementRedacted(true)
	if string(replacement.Original()) != "secret" || string(replacement.Replacement()) != "hidden" {
		t.Error("redaction must not change the exact bytes")
	}
	if !replacement.RedactOriginal() || !replacement.RedactReplacement() {
		t.Error("redaction flags")
	}
	patch, err := NewSourcePatch(base, []SourceReplacement{replacement}, nil, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	redacted, err := patch.WithAllReplacementsRedacted(true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !redacted.Replacements()[0].RedactOriginal() {
		t.Error("with-all redaction")
	}
	if _, err := patch.WithReplacementRedacted(7, true, true); err == nil {
		t.Error("unknown replacement index must fail")
	}
	// The patch still applies: exact bytes are required, not the redacted
	// presentation.
	applied, err := patch.Apply(base, DefaultSourcePatchLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(applied.Bytes()), "hidden") {
		t.Errorf("applied bytes %q", applied.Bytes())
	}
}

func codeOf(err error) string {
	type coded interface{ Code() string }
	if codedError, ok := err.(coded); ok {
		return codedError.Code()
	}
	return ""
}
