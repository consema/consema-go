package ini

import (
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// FormationFailure is the fatal formation failure before any complete
// Document exists (consema-document lib.rs). It implements error
// and the RFC 0016 §6 Code() contract with the first diagnostic's
// registered code.
type FormationFailure struct {
	diagnostics []*protocol.Diagnostic
}

// Diagnostics returns the ordered diagnostics explaining the failure.
func (f *FormationFailure) Diagnostics() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), f.diagnostics...)
}

// Error implements error; the text is human presentation only (RFC 0016 §6).
func (f *FormationFailure) Error() string {
	if len(f.diagnostics) == 0 {
		return "ini: formation failed"
	}
	return "ini: formation failed: " + f.diagnostics[0].Code
}

// Code returns the registered code of the first diagnostic.
func (f *FormationFailure) Code() string {
	if len(f.diagnostics) == 0 {
		return "ini.profile.encoding@1"
	}
	return f.diagnostics[0].Code
}

// newFormationFailure builds a single-diagnostic fatal failure against the
// frozen registry.
func newFormationFailure(code string, category protocol.DiagnosticCategory,
	spanStart, spanEnd int, arguments map[string]string) *FormationFailure {
	var primary *protocol.SourceLocation
	if spanStart >= 0 {
		primary = &protocol.SourceLocation{StartByte: uint64(spanStart), EndByte: uint64(spanEnd)}
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, protocol.SeverityError, primary,
		nil, arguments, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		// All formation codes are registered; an unregistered code is an
		// internal invariant violation and must never produce a document.
		panic("ini: unregistered formation code " + code)
	}
	return &FormationFailure{diagnostics: []*protocol.Diagnostic{diagnostic}}
}

// profileFailure builds the frozen ini.profile.encoding@1 diagnostic
// (parser.rs).
func profileFailure() *FormationFailure {
	return newFormationFailure("ini.profile.encoding@1", protocol.CategoryEncoding, -1, -1, nil)
}

// resourceLimitFailure builds the frozen core.parse.resource-limit@1
// diagnostic (consema-document lib.rs).
func resourceLimitFailure(name string, observed, limit int) *FormationFailure {
	return newFormationFailure("core.parse.resource-limit@1", protocol.CategoryResource,
		-1, -1, map[string]string{
			"limit":    fmt.Sprint(limit),
			"name":     name,
			"observed": fmt.Sprint(observed),
		})
}

// sourceFailure maps one source snapshot construction failure onto the
// frozen INI formation codes.
func sourceFailure(err error) *FormationFailure {
	if sourceError, ok := err.(*document.SourceError); ok {
		switch sourceError.Kind {
		case document.SourceErrorInvalidSequence:
			return newFormationFailure("core.source.invalid-sequence@1",
				protocol.CategoryLexical, sourceError.ByteOffset, sourceError.ByteOffset, nil)
		case document.SourceErrorEncodingConflict:
			return newFormationFailure("core.source.encoding-conflict@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorUnsupportedBom:
			return newFormationFailure("core.source.unsupported-bom@1",
				protocol.CategoryEncoding, -1, -1, nil)
		case document.SourceErrorResourceLimit:
			return resourceLimitFailure(sourceError.Name, sourceError.Observed, sourceError.Limit)
		case document.SourceErrorOffsetOverflow:
			return resourceLimitFailure("source-coordinate-overflow", 1, 0)
		}
	}
	return profileFailure()
}

// Parse forms one immutable INI snapshot under exactly one selected profile
// (consema-ini lib.rs; parser.rs). The encoding selection is
// explicit: portable applies its frozen ASCII UTF-8 contract, Windows
// accepts BOM-detected UTF-16LE or one caller-selected code page, and
// Python accepts any complete text source selected unambiguously by the
// caller or a BOM. Decoding failures, profile/encoding conflicts, and
// limit failures return a FormationFailure with a registered diagnostic
// code; no partial Document is ever returned.
func Parse(source []byte, profile IniProfile, selection IniEncodingSelection,
	limits IniParseLimits) (*Document, *FormationFailure) {
	request, failure := encodingRequest(profile, selection)
	if failure != nil {
		return nil, failure
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(source, request,
		document.SourceLimits{
			MaxRawBytes:         limits.Common.MaxSourceBytes,
			MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
			MaxDecodedScalars:   limits.MaxDecodedScalars,
		})
	if err != nil {
		return nil, sourceFailure(err)
	}
	if failure := validateProfileEncoding(snapshot, profile, selection); failure != nil {
		return nil, failure
	}
	return newParser(snapshot, profile, limits).parse()
}

// encodingRequest builds the frozen source encoding request for one
// profile and selection (parser.rs).
func encodingRequest(profile IniProfile,
	selection IniEncodingSelection) (document.EncodingRequest, *FormationFailure) {
	encoding := document.Utf8Encoding()
	if explicit := selection.Explicit(); explicit != nil {
		encoding = *explicit
	}
	if encoding.Kind() == document.EncodingBinary {
		return document.EncodingRequest{}, profileFailure()
	}
	request := document.NewEncodingRequest(document.Utf8Encoding())
	if explicit := selection.Explicit(); explicit != nil {
		request = request.WithCallerOverride(*explicit)
	}
	if encoding.Kind() == document.EncodingWindowsCodePage {
		request = request.WithBomPolicy(document.BomPolicyTreatAsContent)
	}
	if profile.isPortable() && !encoding.Equal(document.Utf8Encoding()) {
		return document.EncodingRequest{}, profileFailure()
	}
	return request, nil
}

// validateProfileEncoding enforces the frozen profile encoding contract
// (parser.rs).
func validateProfileEncoding(snapshot *document.SourceSnapshot, profile IniProfile,
	selection IniEncodingSelection) *FormationFailure {
	facts := snapshot.EncodingFacts()
	valid := false
	switch {
	case profile.isPortable():
		valid = facts.Selected().Equal(document.Utf8Encoding()) && facts.Bom() == nil
	case profile.isWindows():
		explicit := selection.Explicit()
		switch {
		case explicit == nil:
			valid = (facts.Selected().Equal(document.Utf16LeEncoding()) &&
				facts.Bom() != nil && *facts.Bom() == document.BomUtf16Le) ||
				(facts.Selected().Equal(document.Utf8Encoding()) && facts.Bom() == nil &&
					allASCII(snapshot.Bytes()))
		case explicit.Equal(document.Utf16LeEncoding()):
			valid = facts.Selected().Equal(document.Utf16LeEncoding()) &&
				facts.Bom() != nil && *facts.Bom() == document.BomUtf16Le
		case explicit.Kind() == document.EncodingWindowsCodePage:
			valid = facts.Selected().Equal(*explicit) &&
				facts.BomPolicy() == document.BomPolicyTreatAsContent && facts.Bom() == nil
		}
	case profile.isPython():
		valid = facts.Selected().Kind() != document.EncodingBinary
	}
	if valid {
		return nil
	}
	return profileFailure()
}

// allASCII reports whether every byte is ASCII.
func allASCII(bytes []byte) bool {
	for _, byte := range bytes {
		if byte >= 0x80 {
			return false
		}
	}
	return true
}
