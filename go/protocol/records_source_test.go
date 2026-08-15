package protocol

import (
	"testing"
)

// utf8PatchFacts returns the wire encoding-facts record of an exact UTF-8
// source (profile default only; resolution picks the profile default).
func utf8PatchFacts() EncodingFacts {
	utf8 := &SourceEncoding{Kind: "Utf8"}
	return EncodingFacts{ProfileDefault: utf8, BomPolicy: "DetectUnicode", Selected: utf8}
}

func latin1PatchFacts() EncodingFacts {
	latin1 := &SourceEncoding{Kind: "Latin1"}
	return EncodingFacts{ProfileDefault: latin1, BomPolicy: "DetectUnicode", Selected: latin1}
}

func utf8PatchBase(text string) *SourceSnapshot {
	base, err := NewSourceSnapshotFromRaw([]byte(text),
		NewEncodingRequest(&SourceEncoding{Kind: "Utf8"}), DefaultSourceLimits())
	if err != nil {
		panic(err)
	}
	return base
}

func latin1PatchBase(text string) *SourceSnapshot {
	base, err := NewSourceSnapshotFromRaw([]byte(text),
		NewEncodingRequest(&SourceEncoding{Kind: "Latin1"}), DefaultSourceLimits())
	if err != nil {
		panic(err)
	}
	return base
}

// TestEncodingFactsNullOptionalFieldsRoundTrip pins the wave-5 P2 codec
// symmetry fix: the encoder writes Null for an absent profile_default /
// selected, and the decoder must accept that Null back (the kt and ts
// decoders handle the same fields as optional). Before the fix the Go
// codec's own encode could not be decoded (parseSourceEncodingValue
// rejected the Null).
func TestEncodingFactsNullOptionalFieldsRoundTrip(t *testing.T) {
	facts := EncodingFacts{
		BomPolicy: "DetectUnicode",
	}
	value, err := encodingFactsValue(&facts)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := parseEncodingFactsValue(value, "test")
	if err != nil {
		t.Fatalf("decode of the codec's own encode failed: %v", err)
	}
	if decoded.ProfileDefault != nil || decoded.Selected != nil {
		t.Fatalf("absent facts did not round-trip as nil: %#v", decoded)
	}
	if decoded.BomPolicy != "DetectUnicode" {
		t.Fatalf("bom_policy %q", decoded.BomPolicy)
	}
}

// TestSourcePatchApplyErrorCodesAreRegistered pins every failure branch of
// the SourcePatchApplyError surface: the branch must produce the registered
// code the Rust document layer maps the same failure class to
// (source_patch.rs, mirroring the source-v1.json patch vector
// expectations), and that code must be registered in the semantic-model v7
// registry (RFC 0015 §4.3: envelopes only carry registered codes).
func TestSourcePatchApplyErrorCodesAreRegistered(t *testing.T) {
	registry := NewErrorCodeRegistry(ErrorRegistryV7)

	replace := func(start, end uint64, original, replacement []byte) SourceReplacement {
		return SourceReplacement{OldStart: start, OldEnd: end,
			Original: original, Replacement: replacement}
	}

	cases := []struct {
		name string
		run  func() error
		want string
	}{
		{
			// source.patch.reject-stale-base: base digest precondition fails
			// first (source-v1.json vector expectation).
			name: "stale-base",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("abc")),
					TargetDigest: DigestOf([]byte("aBc")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 2, []byte("b"), []byte("B"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("abd"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.patch-base-mismatch@1",
		},
		{
			// source.patch.reject-original-mismatch: an original-byte
			// precondition that does not match the base bytes.
			name: "original-mismatch",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("abc")),
					TargetDigest: DigestOf([]byte("aBc")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 2, []byte("x"), []byte("B"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("abc"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.patch-original-mismatch@1",
		},
		{
			// An old span beyond the base bytes is an original-byte
			// precondition failure, exactly as the Rust document layer maps
			// it (source_patch.rs).
			name: "original-span-out-of-range",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte("xy")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 3, []byte("bc"), []byte("xy"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.patch-original-mismatch@1",
		},
		{
			// source.patch.reject-target-mismatch: the computed result does
			// not reproduce the declared target digest.
			name: "target-mismatch",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte("not cd")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(0, 2, []byte("ab"), []byte("cd"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.patch-target-mismatch@1",
		},
		{
			// source.patch.reject-encoding-change: the result bytes resolve
			// to different encoding facts than the base (source-v1.json
			// vector expectation; source_patch.rs).
			name: "target-encoding-drift",
			run: func() error {
				utf16 := []byte{0xff, 0xfe, 0x41, 0x00}
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf(utf16),
					Encoding:     latin1PatchFacts(),
					Replacements: []SourceReplacement{replace(0, 2, []byte("ab"), utf16)},
				}
				_, err := ApplySourcePatch(patch, latin1PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.encoding-conflict@1",
		},
		{
			// A wire encoding-facts record that cannot be reconciled is an
			// encoding conflict (source_patch.rs).
			name: "wire-encoding-facts-invalid",
			run: func() error {
				facts := utf8PatchFacts()
				facts.BomPolicy = "Bogus"
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte("cd")),
					Encoding:     facts,
					Replacements: []SourceReplacement{replace(0, 2, []byte("ab"), []byte("cd"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.encoding-conflict@1",
		},
		{
			// NewSourcePatch rejects a result whose encoding facts drift from
			// the base (source_patch.rs).
			name: "create-encoding-drift",
			run: func() error {
				utf16 := []byte{0xff, 0xfe, 0x41, 0x00}
				_, err := NewSourcePatch(latin1PatchBase("ab"),
					[]SourceReplacement{replace(0, 2, []byte("ab"), utf16)},
					map[string]string{}, DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.encoding-conflict@1",
		},
		{
			// Result bytes that cannot form the base's encoding fail with the
			// wrapped source error's code (source_patch.rs).
			name: "target-invalid-sequence",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte{0x61, 0xff}),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 2, []byte("b"), []byte{0xff})},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.invalid-sequence@1",
		},
		{
			// A result beginning with a UTF-32 BOM fails with the wrapped
			// unsupported-BOM source error's code (source_patch.rs).
			name: "target-unsupported-bom",
			run: func() error {
				utf32 := []byte{0xff, 0xfe, 0x00, 0x00}
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf(utf32),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(0, 2, []byte("ab"), utf32)},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.source.unsupported-bom@1",
		},
		{
			// source.resource.patch-count-limit: the replacement count budget
			// is exceeded (source-v1.json vector expectation).
			name: "replacement-count-limit",
			run: func() error {
				limits := DefaultSourcePatchLimits()
				limits.MaxReplacements = 0
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("a")),
					TargetDigest: DigestOf([]byte("ab")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 1, nil, []byte("b"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("a"), limits)
				return err
			},
			want: "core.source.resource-limit@1",
		},
		{
			// The summed original+replacement byte budget is exceeded
			// (source_patch.rs).
			name: "patch-bytes-limit",
			run: func() error {
				limits := DefaultSourcePatchLimits()
				limits.MaxPatchBytes = 1
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("a")),
					TargetDigest: DigestOf([]byte("abc")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(1, 1, nil, []byte("bc"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("a"), limits)
				return err
			},
			want: "core.source.resource-limit@1",
		},
		{
			// A replacement whose start follows its end or whose original
			// byte count disagrees with its range is structurally invalid
			// (source_patch.rs).
			name: "invalid-replacement",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte("ab")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{replace(2, 1, nil, []byte("x"))},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.protocol.invalid-value@1",
		},
		{
			// Two zero-width replacements at the same insertion point are
			// structurally invalid (source_patch.rs).
			name: "duplicate-insertion",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("ab")),
					TargetDigest: DigestOf([]byte("xab")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{
						replace(0, 0, nil, []byte("x")),
						replace(0, 0, nil, []byte("y")),
					},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("ab"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.protocol.invalid-value@1",
		},
		{
			// Overlapping old ranges violate the canonical replacement order
			// (source-v1.json source.patch.reject-overlap expectation).
			name: "replacement-order",
			run: func() error {
				patch := &SourcePatch{
					BaseDigest:   DigestOf([]byte("abcdef")),
					TargetDigest: DigestOf([]byte("aef")),
					Encoding:     utf8PatchFacts(),
					Replacements: []SourceReplacement{
						replace(1, 4, []byte("bcd"), nil),
						replace(3, 5, []byte("de"), nil),
					},
				}
				_, err := ApplySourcePatch(patch, utf8PatchBase("abcdef"), DefaultSourcePatchLimits())
				return err
			},
			want: "core.protocol.invalid-value@1",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.run()
			applyError, ok := err.(*SourcePatchApplyError)
			if !ok {
				t.Fatalf("error %v is not a *SourcePatchApplyError", err)
			}
			if got := applyError.Code(); got != testCase.want {
				t.Errorf("Code() = %q, want %q", got, testCase.want)
			}
			if !registry.Contains(applyError.Code()) {
				t.Errorf("Code() = %q is not registered in the semantic-model v7 error registry", applyError.Code())
			}
		})
	}
}

func TestSourceErrorCodesAreRegistered(t *testing.T) {
	registry := NewErrorCodeRegistry(ErrorRegistryV7)

	cases := []struct {
		name string
		err  *SourceError
		want string
	}{
		{"invalid-sequence", &SourceError{Kind: SourceErrorInvalidSequence, ByteOffset: 3},
			"core.source.invalid-sequence@1"},
		{"encoding-conflict", &SourceError{Kind: SourceErrorEncodingConflict},
			"core.source.encoding-conflict@1"},
		{"unsupported-bom", &SourceError{Kind: SourceErrorUnsupportedBom},
			"core.source.unsupported-bom@1"},
		{"resource-limit", &SourceError{Kind: SourceErrorResourceLimit, Limit: "raw-bytes"},
			"core.source.resource-limit@1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.err.Code(); got != testCase.want {
				t.Errorf("Code() = %q, want %q", got, testCase.want)
			}
			if !registry.Contains(testCase.err.Code()) {
				t.Errorf("Code() = %q is not registered in the semantic-model v7 error registry", testCase.err.Code())
			}
		})
	}
}
