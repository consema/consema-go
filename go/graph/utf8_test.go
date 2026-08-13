package graph

import (
	"testing"
)

// invalidUTF8 is a byte string that is not valid UTF-8.
var invalidUTF8 = "\xff\xfe"

// TestBuilderRejectsInvalidUTF8 pins the builder-layer UTF-8 invariant: the
// Rust side cannot construct a graph whose tag or scalar content is not
// valid UTF-8 (the Arc<str> invariant, consema-rs/crates/consema-graph/src/lib.rs:
// 94-157), so the Go builder validates explicitly and returns the typed
// ErrGraphInvalidUTF8. The typed error maps to the frozen registered code
// "core.graph.invalid@1".
func TestBuilderRejectsInvalidUTF8(t *testing.T) {
	b := NewBuilder(DefaultLimits())
	id := mustReserve(t, b)

	// Invalid UTF-8 scalar content.
	err := b.DefineScalar(id, tagStr, invalidUTF8)
	if !IsGraphError(err, ErrGraphInvalidUTF8) {
		t.Fatalf("DefineScalar(invalid content) = %v, want ErrGraphInvalidUTF8", err)
	}
	var graphErr *GraphError
	if !asGraphError(err, &graphErr) || graphErr.Code() != "core.graph.invalid@1" {
		t.Errorf("invalid-UTF-8 content code = %q, want core.graph.invalid@1", graphErr.Code())
	}

	// Invalid UTF-8 in the tag, for every definition path.
	for name, define := range map[string]func() error{
		"scalar":   func() error { return b.DefineScalar(id, invalidUTF8, "x") },
		"sequence": func() error { return b.DefineSequence(id, invalidUTF8, nil) },
		"mapping":  func() error { return b.DefineMapping(id, invalidUTF8, nil) },
	} {
		if err := define(); !IsGraphError(err, ErrGraphInvalidUTF8) {
			t.Errorf("%s tag: err = %v, want ErrGraphInvalidUTF8", name, err)
		}
	}

	// A control character in the tag is still the classic ErrGraphInvalidTag.
	if err := b.DefineScalar(id, "bad\x01tag", "x"); !IsGraphError(err, ErrGraphInvalidTag) {
		t.Errorf("control-char tag: err = %v, want ErrGraphInvalidTag", err)
	}
}

// TestDecodePGCEInvalidUTF8ThroughBuilder pins that wire-level invalid UTF-8
// is now intercepted by the builder layer and mapped back to the codec's
// ErrInvalidUTF8, so the strict decode surface is unchanged (the Rust
// Decoder::string InvalidUtf8, consema-rs/crates/consema-graph/src/pgce.rs:576-586).
func TestDecodePGCEInvalidUTF8ThroughBuilder(t *testing.T) {
	// One scalar root with invalid-UTF-8 content ("\xff").
	stream := []byte{'P', 'G', 'C', 'E', 0x01, 0x01, 0x01, 0x00,
		nodeScalar, 0x01, 'x', 0x01, 0xff}
	if _, err := DecodePGCE(stream, DefaultPGCELimits()); !IsPGCEError(err, ErrInvalidUTF8) {
		t.Errorf("invalid-utf-8 content err = %v, want InvalidUTF8", err)
	}
	// Invalid-UTF-8 tag in a sequence node.
	stream = []byte{'P', 'G', 'C', 'E', 0x01, 0x01, 0x01, 0x00,
		nodeSequence, 0x01, 0xff, 0x00}
	if _, err := DecodePGCE(stream, DefaultPGCELimits()); !IsPGCEError(err, ErrInvalidUTF8) {
		t.Errorf("invalid-utf-8 tag err = %v, want InvalidUTF8", err)
	}
}

// TestErrNonCanonicalEncodingIsDefenseInDepth documents the re-encode
// canonicality check of RFC 0006 §5 (DecodePGCE's final defense-in-depth
// rule, pgce.go). It is not reachable from any stream that passes the
// preceding structural checks, in either implementation:
//
//   - every structural failure class (magic, version, varint minimality,
//     counts, node-record octets, tag/content rules, references, trailing
//     bytes, reachability) is rejected before the check;
//   - the NonCanonicalNodeOrder check guarantees the decoded graph's wire
//     IDs equal its canonical IDs, so re-encoding under the canonical
//     layout reproduces the input bytes exactly;
//   - the encoder writes minimal varints, so a canonical stream can never
//     be re-emitted differently.
//
// The check therefore guards against encoder regressions, not against
// inputs. The Rust decoder has the same unreachable defense
// (consema-rs/crates/consema-graph/src/pgce.rs:502-504) and no test drives it either;
// its registered code is pinned by TestPGCEFailuresHaveStableCodes. The
// code table mapping is re-pinned here for completeness.
func TestErrNonCanonicalEncodingIsDefenseInDepth(t *testing.T) {
	if got := (&PGCEError{Kind: ErrNonCanonicalEncoding}).Code(); got != "core.pgce.non-canonical@1" {
		t.Errorf("NonCanonicalEncoding code = %q, want core.pgce.non-canonical@1", got)
	}
}

// asGraphError unwraps the typed error for assertions.
func asGraphError(err error, target **GraphError) bool {
	graphErr, ok := err.(*GraphError)
	if ok {
		*target = graphErr
	}
	return ok
}
