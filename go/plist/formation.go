package plist

// This file defines the typed formation failure and the profile-selected
// parse entry point (consema-plist lib.rs; RFC 0013 §1, §3). The profile is
// selected by the caller before formation; neither the `bplist00` magic
// number nor a `.plist` extension selects semantics. Formation is
// side-effect free: it never fetches the Apple DTD or any other URI.

import (
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// FormationFailureKind classifies a fatal plist formation failure (the Rust
// FatalFormationFailure; consema-document lib.rs).
type FormationFailureKind uint8

// The closed formation failure classes.
const (
	// FormationFailureSource: the raw bytes could not form a valid source
	// snapshot.
	FormationFailureSource FormationFailureKind = iota
	// FormationFailureResourceLimit: a configured parse bound was exceeded;
	// nothing is truncated to success.
	FormationFailureResourceLimit
	// FormationFailureCancelled: the caller's context was cancelled during
	// bounded formation. This is a Go-idiomatic resource guard, never a
	// language-neutral fact. Reserved for a future cancellable parse
	// entry: the plist family mirrors the Rust facade today and `Parse`
	// takes no context, so this kind is not yet constructed.
	FormationFailureCancelled
)

// FormationFailure is a fatal plist formation failure. It implements error,
// the RFC 0016 §6 Code() contract, and Unwrap for the wrapped source
// failure.
type FormationFailure struct {
	// Kind identifies the failure.
	Kind FormationFailureKind
	// Source is the wrapped source construction failure of Kind Source.
	Source *document.SourceError
	// Diagnostics are the ordered fatal diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Name is the stable limit name of a ResourceLimit.
	Name string
	// Observed is the observed amount of a ResourceLimit.
	Observed int
	// Limit is the configured maximum of a ResourceLimit.
	Limit int
}

// Error implements error.
func (e *FormationFailure) Error() string {
	switch e.Kind {
	case FormationFailureSource:
		if e.Source != nil {
			return "plist: " + e.Source.Error()
		}
		return "plist: source formation failure"
	case FormationFailureResourceLimit:
		return fmt.Sprintf("plist: parse limit %s exceeded: observed %d, limit %d",
			e.Name, e.Observed, e.Limit)
	case FormationFailureCancelled:
		return "plist: parse cancelled"
	}
	return "plist: formation failure"
}

// Code returns the frozen registered code for the failure. Cancellation
// uses the only registered cancellation code; it is a Go-only guard and
// never appears in conformance comparison.
func (e *FormationFailure) Code() string {
	switch e.Kind {
	case FormationFailureSource:
		if e.Source != nil {
			return e.Source.Code()
		}
		if len(e.Diagnostics) > 0 {
			return e.Diagnostics[0].Code
		}
		return "core.source.invalid-sequence@1"
	case FormationFailureResourceLimit:
		return "core.parse.resource-limit@1"
	case FormationFailureCancelled:
		return "core.query.cancelled@1"
	}
	if len(e.Diagnostics) > 0 {
		return e.Diagnostics[0].Code
	}
	return "core.parse.resource-limit@1"
}

// Unwrap returns the wrapped source failure of Kind Source.
func (e *FormationFailure) Unwrap() error {
	if e.Kind == FormationFailureSource {
		return e.Source
	}
	return nil
}

// Parse forms one immutable `plist.xml@1` or `plist.binary@1` document
// snapshot from raw bytes (consema-plist lib.rs parse; RFC 0013 §1, §3).
// The profile selects the representation, the encoding selection follows
// the RFC 0013 §2 source contract, and the limits bound formation.
func Parse(source []byte, profile PlistProfile, selection PlistEncodingSelection,
	limits PlistParseLimits) (*Document, *FormationFailure) {
	authority := document.NewDocumentAuthority()
	sourceLimits := document.SourceLimits{
		MaxRawBytes:         limits.Common.MaxSourceBytes,
		MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
		MaxDecodedScalars:   limits.MaxDecodedScalars,
	}
	var snapshot *document.SourceSnapshot
	var err error
	switch profile {
	case PlistProfileXmlV1:
		request, failure := xmlEncodingRequest(selection)
		if failure != nil {
			return nil, failure
		}
		snapshot, err = document.NewSourceSnapshotFromRaw(source, request, sourceLimits)
		if err != nil {
			return nil, sourceFailure(err)
		}
		if failure := validateProfileEncoding(snapshot, selection); failure != nil {
			return nil, failure
		}
	case PlistProfileBinaryV1:
		if explicit := selection.Explicit(); explicit != nil && !explicit.Equal(document.BinaryEncoding()) {
			return nil, encodingFailure("plist.binary.encoding@1")
		}
		snapshot, err = document.NewSourceSnapshotFromBinary(source, sourceLimits)
		if err != nil {
			return nil, sourceFailure(err)
		}
	default:
		return nil, encodingFailure("plist.xml.encoding@1")
	}
	document, failure := buildDocument(authority, snapshot, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return document, nil
}

// parseXML forms one `plist.xml@1` document from raw bytes; used by the
// conversion and edit layers under the profile default selection.
func parseXML(source []byte, limits PlistParseLimits) (*Document, *FormationFailure) {
	return Parse(source, PlistProfileXmlV1, PlistEncodingProfileDefault(), limits)
}

// parseBinary forms one `plist.binary@1` document from raw bytes; used by
// the conversion and edit layers under the profile default selection.
func parseBinary(source []byte, limits PlistParseLimits) (*Document, *FormationFailure) {
	return Parse(source, PlistProfileBinaryV1, PlistEncodingProfileDefault(), limits)
}

// sourceFailure wraps one source construction failure.
func sourceFailure(err error) *FormationFailure {
	sourceError, ok := err.(*document.SourceError)
	if !ok {
		sourceError = &document.SourceError{Kind: document.SourceErrorInvalidUtf8}
	}
	return &FormationFailure{Kind: FormationFailureSource, Source: sourceError}
}

// encodingFailure builds the fatal source-contract conflict of one profile.
func encodingFailure(code string) *FormationFailure {
	return &FormationFailure{
		Kind: FormationFailureSource,
		Diagnostics: []*protocol.Diagnostic{newDiagnostic(code, protocol.CategoryEncoding,
			protocol.SeverityError, nil, nil, 0)},
	}
}

// xmlEncodingRequest resolves the RFC 0013 §2.1 document-entity request.
func xmlEncodingRequest(selection PlistEncodingSelection) (document.EncodingRequest, *FormationFailure) {
	if explicit := selection.Explicit(); explicit != nil {
		switch explicit.Kind() {
		case document.EncodingUtf8, document.EncodingUtf16Le, document.EncodingUtf16Be:
			// Admitted document-entity encodings (RFC 0013 §2.1).
		default:
			return document.EncodingRequest{}, encodingFailure("plist.xml.encoding@1")
		}
		request := document.NewEncodingRequest(document.Utf8Encoding())
		return request.WithCallerOverride(*explicit), nil
	}
	return document.NewEncodingRequest(document.Utf8Encoding()).
		WithBomPolicy(document.BomPolicyDetectUnicode), nil
}

// validateProfileEncoding enforces the RFC 0013 §2.1 table: an explicit
// caller choice is evidence, not permission to contradict a BOM.
func validateProfileEncoding(snapshot *document.SourceSnapshot,
	selection PlistEncodingSelection) *FormationFailure {
	facts := snapshot.EncodingFacts()
	valid := false
	if explicit := selection.Explicit(); explicit != nil {
		switch explicit.Kind() {
		case document.EncodingUtf8:
			valid = facts.Selected().Equal(document.Utf8Encoding())
		case document.EncodingUtf16Le:
			bom := facts.Bom()
			valid = facts.Selected().Equal(document.Utf16LeEncoding()) &&
				bom != nil && *bom == document.BomUtf16Le
		case document.EncodingUtf16Be:
			bom := facts.Bom()
			valid = facts.Selected().Equal(document.Utf16BeEncoding()) &&
				bom != nil && *bom == document.BomUtf16Be
		}
	} else {
		selected := facts.Selected()
		valid = selected.Equal(document.Utf8Encoding()) ||
			selected.Equal(document.Utf16LeEncoding()) ||
			selected.Equal(document.Utf16BeEncoding())
	}
	if valid {
		return nil
	}
	return encodingFailure("plist.xml.encoding@1")
}

// buildDocument forms one document under the exact profile; the profile
// entry points split here so the shared Document wrap is single.
func buildDocument(authority document.DocumentAuthority, snapshot *document.SourceSnapshot,
	profile PlistProfile, selection PlistEncodingSelection,
	limits PlistParseLimits) (*Document, *FormationFailure) {
	switch profile {
	case PlistProfileXmlV1:
		return parseXMLDocument(authority, snapshot, limits)
	case PlistProfileBinaryV1:
		return parseBinaryDocument(authority, snapshot, limits)
	}
	return nil, encodingFailure("plist.xml.encoding@1")
}

// newDiagnostic constructs one diagnostic directly. The `plist.*` codes are
// not part of the consema-protocol core error registry (RFC 0013 §12), so
// the registry-validated constructor does not apply here; construction
// mirrors the Rust `consema_core::Diagnostic::new` path.
func newDiagnostic(code string, category protocol.DiagnosticCategory, severity protocol.Severity,
	location *protocol.SourceLocation, arguments map[string]string, occurrence uint64) *protocol.Diagnostic {
	if arguments == nil {
		arguments = map[string]string{}
	}
	return &protocol.Diagnostic{
		Code: code, Category: category, Severity: severity,
		Primary: location, Arguments: arguments, Occurrence: occurrence,
	}
}

// locationOf maps one raw span to the transferable location record.
func locationOf(authority document.DocumentAuthority, start, end int) *protocol.SourceLocation {
	span, err := authority.Span(start, end)
	if err != nil {
		return nil
	}
	location, err := protocol.NewSourceLocation(
		fmt.Sprintf("%d", authority.Identity().AsU64()), uint64(span.StartByte()), uint64(span.EndByte()))
	if err != nil {
		return nil
	}
	return location
}

// diagnosticSink records bounded ordered diagnostics with the house
// truncation marker (consema-plist parser_xml.rs DiagnosticSink).
type diagnosticSink struct {
	diagnostics []*protocol.Diagnostic
	max         int
	occurrence  uint64
	truncated   bool
}

func newDiagnosticSink(max int) *diagnosticSink {
	return &diagnosticSink{max: max}
}

// push appends one diagnostic, assigning the deterministic occurrence
// ordinal and enforcing the budget.
func (s *diagnosticSink) push(diagnostic *protocol.Diagnostic) {
	diagnostic.Occurrence = s.occurrence
	s.occurrence++
	if len(s.diagnostics) < s.max {
		s.diagnostics = append(s.diagnostics, diagnostic)
		return
	}
	if !s.truncated {
		s.truncated = true
		s.diagnostics = append(s.diagnostics, newDiagnostic("core.diagnostic.truncated@1",
			protocol.CategoryResource, protocol.SeverityWarning, nil, nil, s.occurrence))
	}
}

// finish sorts the diagnostics deterministically: primary start byte
// (absent last), category, code, occurrence (consema-core diagnostic.rs
// sort; the Rust sink applies the same order).
func (s *diagnosticSink) finish() []*protocol.Diagnostic {
	out := append([]*protocol.Diagnostic(nil), s.diagnostics...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && diagnosticLess(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// diagnosticLess is the deterministic diagnostic order of the Rust
// `Diagnostic::sort_deterministically`: absent primary last, then start
// byte, category, code, occurrence.
func diagnosticLess(left, right *protocol.Diagnostic) bool {
	leftStart, leftHas := diagnosticStart(left)
	rightStart, rightHas := diagnosticStart(right)
	if leftHas != rightHas {
		return leftHas
	}
	if leftStart != rightStart {
		return leftStart < rightStart
	}
	if left.Category != right.Category {
		return left.Category < right.Category
	}
	if left.Code != right.Code {
		return left.Code < right.Code
	}
	return left.Occurrence < right.Occurrence
}

func diagnosticStart(diagnostic *protocol.Diagnostic) (uint64, bool) {
	if diagnostic.Primary == nil {
		return 0, false
	}
	return diagnostic.Primary.StartByte, true
}
