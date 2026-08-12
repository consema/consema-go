package plist_test

// Limit and recovery matrices transcribed from the Rust reference parser
// (consema-plist parser_binary.rs; RFC 0013 §5.10, §5.11, §12). Every
// limit breach is a fatal formation failure (hard gate 4), every malformed
// trailer field and out-of-range offset-table entry is Recovered with the
// frozen `plist.binary.trailer@1` / `plist.binary.offset-table@1`
// diagnostics, and a violated limit never truncates to success.

import (
	"testing"

	"consema.dev/consema/document"
	"consema.dev/consema/plist"
)

// parseBinaryLimits forms one binary document under explicit limits.
func parseBinaryLimits(t *testing.T, bytes []byte,
	limits plist.PlistParseLimits) (*plist.Document, *plist.FormationFailure) {
	t.Helper()
	return plist.Parse(bytes, plist.PlistProfileBinaryV1,
		plist.PlistEncodingProfileDefault(), limits)
}

// assertFailureDiagnostic asserts the fatal failure carries the code.
func assertFailureDiagnostic(t *testing.T, failure *plist.FormationFailure, code string) {
	t.Helper()
	if failure == nil {
		t.Fatalf("expected fatal failure with diagnostic %s", code)
	}
	for _, diagnostic := range failure.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostic %s not found in %v", code, failure.Diagnostics)
}

// assertTrailerCheck asserts one `plist.binary.trailer@1` recovery with the
// given `check` argument.
func assertTrailerCheck(t *testing.T, doc *plist.Document, check string) {
	t.Helper()
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "plist.binary.trailer@1" &&
			diagnostic.Arguments["check"] == check {
			return
		}
	}
	t.Fatalf("trailer check %q not found: %v", check, doc.Diagnostics())
}

// patchTrailerField overwrites one big-endian trailer field (fieldOffset
// relative to the 32-byte trailer start).
func patchTrailerField(bytes []byte, fieldOffset int, value uint64, width int) {
	start := len(bytes) - 32 + fieldOffset
	for shift := (width - 1) * 8; shift >= 0; shift -= 8 {
		bytes[start] = byte((value >> uint(shift)) & 0xFF)
		start++
	}
}

// TestBinaryParseLimitsAreFatal transcribes the Rust limit matrix
// (parser_binary.rs container_depth_limit_is_fatal,
// object_count_limit_is_fatal, per_value_limits_are_fatal,
// extended_size_limits_are_fatal, width_and_table_limits_are_fatal,
// binary_facts_limit_is_fatal, duplicate_key_group_limit_is_fatal,
// source_bytes_limit_is_fatal, recovery_regions_limit_is_fatal). Every
// breach is fatal with the frozen `plist.limit.<name>@1` code; nothing is
// truncated to success.
func TestBinaryParseLimitsAreFatal(t *testing.T) {
	t.Run("container-depth", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0xA1, 0x01}) // 0: [1]
		builder.object([]byte{0xA1, 0x02}) // 1: [2]
		builder.object([]byte{0xA1, 0x03}) // 2: [3]
		builder.object([]byte{0x08})       // 3: false
		limits := plist.DefaultPlistParseLimits()
		limits.MaxContainerDepth = 2
		doc, failure := parseBinaryLimits(t, builder.finish(0), limits)
		if failure == nil && doc != nil {
			t.Fatalf("depth limit must fail fatally")
		}
		assertFailureDiagnostic(t, failure, "plist.limit.container-depth@1")
	})

	t.Run("object-count", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxObjectCount = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.object-count@1")
	})

	t.Run("string-code-units", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x53, 'a', 'b', 'c'})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxStringCodeUnits = 2
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.string-code-units@1")
	})

	t.Run("data-bytes", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x42, 0x01, 0x02})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxDataBytes = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.data-bytes@1")
	})

	t.Run("array-elements", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0xA2, 0x00, 0x01})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxArrayElements = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.array-elements@1")
	})

	t.Run("dict-entries", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0xD2, 0x00, 0x00, 0x00, 0x00})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxDictEntries = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.dict-entries@1")
	})

	t.Run("uid-count", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x80, 0x01})
		builder.object([]byte{0x80, 0x02})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxUIDCount = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.uid-count@1")
	})

	t.Run("extended-size-value", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x4F, 0x10, 0x05})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxExtendedSizeValue = 4
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.extended-size-value@1")
	})

	t.Run("extended-size-integers", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x4F, 0x10, 0x01, 0x00})
		builder.object([]byte{0x4F, 0x10, 0x01, 0x00})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxExtendedSizeIntegers = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.extended-size-integers@1")
	})

	t.Run("offset-int-size", func(t *testing.T) {
		builder := newTestBinaryBuilder(2, 1)
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxOffsetIntSize = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.offset-int-size@1")
	})

	t.Run("object-ref-size", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 2)
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxObjectRefSize = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.object-ref-size@1")
	})

	t.Run("offset-table-bytes", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxOffsetTableBytes = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.offset-table-bytes@1")
	})

	t.Run("binary-facts", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxBinaryFacts = 1
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.binary-facts@1")
	})

	t.Run("duplicate-key-group", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x51, 'k'})
		builder.object([]byte{0x51, 'k'})
		builder.object([]byte{0x51, 'v'})
		builder.object([]byte{0x51, 'w'})
		builder.object([]byte{0xD2, 0x00, 0x01, 0x02, 0x03})
		limits := plist.DefaultPlistParseLimits()
		limits.MaxDuplicateKeyGroupMembers = 1
		_, failure := parseBinaryLimits(t, builder.finish(4), limits)
		assertFailureDiagnostic(t, failure, "plist.limit.duplicate-key-group@1")
	})

	t.Run("source-bytes", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		limits := plist.DefaultPlistParseLimits()
		limits.Common.MaxSourceBytes = 10
		_, failure := parseBinaryLimits(t, builder.finish(0), limits)
		if failure == nil || failure.Code() != "core.source.resource-limit@1" {
			t.Fatalf("source-bytes limit must fail with core.source.resource-limit@1, got %v",
				failure)
		}
	})

	t.Run("recovery-regions", func(t *testing.T) {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		bytes := builder.finish(0)
		bytes[0] = 'x' // corrupt header: one error region
		limits := plist.DefaultPlistParseLimits()
		limits.MaxRecoveryRegions = 0
		_, failure := parseBinaryLimits(t, bytes, limits)
		assertFailureDiagnostic(t, failure, "plist.limit.recovery-regions@1")
	})
}

// TestBinaryTrailerMalformedMatrix transcribes the Rust trailer recovery
// tests (parser_binary.rs trailer_num_objects_zero_is_rejected,
// trailer_top_object_out_of_range_is_rejected,
// trailer_offset_table_offset_range_is_rejected,
// trailer_total_length_mismatch_is_recovered,
// trailer_sufficiency_checks_are_enforced). Each malformed trailer field
// is Recovered with the frozen `plist.binary.trailer@1` check argument.
func TestBinaryTrailerMalformedMatrix(t *testing.T) {
	minimal := func() []byte {
		builder := newTestBinaryBuilder(1, 1)
		builder.object([]byte{0x08})
		return builder.finish(0)
	}

	// numObjects zero.
	bytes := minimal()
	patchTrailerField(bytes, 8, 0, 8)
	doc, failure := parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("num-objects parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("num-objects zero must recover")
	}
	assertTrailerCheck(t, doc, "num-objects")

	// topObject out of range (>= numObjects).
	bytes = minimal()
	patchTrailerField(bytes, 16, 1, 8)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("top-object parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("top-object out of range must recover")
	}
	assertTrailerCheck(t, doc, "top-object")

	// offsetTableOffset below the header or inside the trailer window.
	for _, value := range []uint64{0, 7, 8} {
		bytes = minimal()
		patchTrailerField(bytes, 24, value, 8)
		doc, failure = parseBinaryTest(t, bytes)
		if failure != nil {
			t.Fatalf("offset-table-offset %d parse failed: %s", value, failure.Code())
		}
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatalf("offset-table-offset %d must recover", value)
		}
		assertTrailerCheck(t, doc, "offset-table-offset")
	}
	// Exactly at the trailer start: passes the range check, fails the
	// total-length check; just past it: fails the range check.
	bytes = minimal()
	patchTrailerField(bytes, 24, uint64(len(bytes)-32), 8)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("offset-table-offset at trailer parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("offset-table-offset at trailer must recover")
	}
	assertTrailerCheck(t, doc, "total-length")
	bytes = minimal()
	patchTrailerField(bytes, 24, uint64(len(bytes)-31), 8)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("offset-table-offset past trailer parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("offset-table-offset past trailer must recover")
	}
	assertTrailerCheck(t, doc, "offset-table-offset")

	// Total length mismatch: an inserted byte between the object table and
	// the trailer.
	bytes = minimal()
	bytes = append(bytes[:len(bytes)-32], append([]byte{0xAB}, bytes[len(bytes)-32:]...)...)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("total-length parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("total-length mismatch must recover")
	}
	assertTrailerCheck(t, doc, "total-length")

	// offsetIntSize 1 cannot address an offset table at or beyond byte 256.
	builder := newTestBinaryBuilder(2, 1)
	payload := append([]byte{0x4F, 0x10, 0xFA}, make([]byte, 250)...)
	builder.object(payload)
	bytes = builder.finish(0)
	patchTrailerField(bytes, 6, 1, 1)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("offset-int-size-sufficiency parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("offset-int-size sufficiency must recover")
	}
	assertTrailerCheck(t, doc, "offset-int-size-sufficiency")

	// objectRefSize 1 cannot address 256 objects.
	builder = newTestBinaryBuilder(1, 2)
	for index := 0; index < 256; index++ {
		builder.object([]byte{0x08})
	}
	bytes = builder.finish(255)
	patchTrailerField(bytes, 7, 1, 1)
	doc, failure = parseBinaryTest(t, bytes)
	if failure != nil {
		t.Fatalf("object-ref-size-sufficiency parse failed: %s", failure.Code())
	}
	if doc.FormationStatus() != document.FormationStatusRecovered {
		t.Fatalf("object-ref-size sufficiency must recover")
	}
	assertTrailerCheck(t, doc, "object-ref-size-sufficiency")
}

// TestBinaryOffsetTableEntriesOutOfRangeCutPrefix transcribes
// offset_table_entries_out_of_range_cut_the_prefix: the first invalid
// entry cuts the proven prefix, the proven objects keep their facts, and
// the native document survives with the proven root.
func TestBinaryOffsetTableEntriesOutOfRangeCutPrefix(t *testing.T) {
	builder := newTestBinaryBuilder(1, 1)
	builder.object([]byte{0x08}) // 0 at offset 8
	builder.object([]byte{0x08}) // 1 at offset 9
	bytes := builder.finish(0)
	for _, bad := range []byte{7, 10, 255} {
		mutated := append([]byte(nil), bytes...)
		mutated[11] = bad // entry 1
		doc, failure := parseBinaryTest(t, mutated)
		if failure != nil {
			t.Fatalf("entry %d parse failed: %s", bad, failure.Code())
		}
		if doc.FormationStatus() != document.FormationStatusRecovered {
			t.Fatalf("entry %d must recover", bad)
		}
		assertDiagnostic(t, doc, "plist.binary.offset-table@1")
		facts := doc.BinaryFacts()
		if len(facts.Objects()) != 1 || len(facts.Offsets()) != 1 {
			t.Fatalf("entry %d: prefix cut mismatch: %d objects, %d offsets",
				bad, len(facts.Objects()), len(facts.Offsets()))
		}
		root := doc.NativeDocument().RootValue()
		if _, ok := root.AsBoolean(); !ok {
			t.Fatalf("entry %d: proven root must survive", bad)
		}
	}
}
