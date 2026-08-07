package xml

import (
	"context"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// FormationFailureKind classifies a fatal XML formation failure (the Rust
// FatalFormationFailure; consema-xml parser.rs).
type FormationFailureKind uint8

// The closed formation failure classes.
const (
	// FormationFailureSource: the raw bytes could not form a valid source
	// snapshot under the RFC 0012 §2 table.
	FormationFailureSource FormationFailureKind = iota
	// FormationFailureProfile: an XML profile or limit contract failed
	// fatally (xml.profile.* or xml.limit.* codes).
	FormationFailureProfile
	// FormationFailureCancelled: the caller's context was cancelled during
	// bounded formation. This is a Go-idiomatic resource guard, never a
	// language-neutral fact (the Rust parse has no cancellation).
	FormationFailureCancelled
)

// FormationFailure is a fatal XML formation failure. It implements error
// and the RFC 0016 §6 Code() contract, and Unwrap for the wrapped source
// failure.
type FormationFailure struct {
	// Kind identifies the failure.
	Kind FormationFailureKind
	// Source is the wrapped source construction failure of Kind Source.
	Source *document.SourceError
	// CodeValue is the stable `xml.*` code of Kind Profile failures.
	CodeValue string
}

// Error implements error; the text is human presentation only.
func (e *FormationFailure) Error() string {
	switch e.Kind {
	case FormationFailureSource:
		return "xml: " + e.Source.Error()
	case FormationFailureProfile:
		return "xml: formation profile or limit failure " + e.CodeValue
	case FormationFailureCancelled:
		return "xml: parse cancelled"
	}
	return "xml: formation failure"
}

// Code returns the stable registered code for the failure. Cancellation
// uses the only registered cancellation code; it is a Go-only guard and
// never appears in conformance comparison.
func (e *FormationFailure) Code() string {
	switch e.Kind {
	case FormationFailureSource:
		return e.Source.Code()
	case FormationFailureProfile:
		return e.CodeValue
	case FormationFailureCancelled:
		return "core.query.cancelled@1"
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

// Parse forms one complete immutable `xml.1.0-safe@1` document snapshot
// (RFC 0012 §2-4; consema-xml parser.rs). The Profile is selected before
// formation and never by extension; the parser consumes the supplied
// bytes and opens no other entity, file, URI, network connection,
// registry, classpath, or catalog. ctx carries cancellation only: an
// already-cancelled context fails fast, and a cancellation during bounded
// formation aborts with FormationFailureCancelled.
func Parse(ctx context.Context, source []byte, profile XmlProfile,
	selection XmlEncodingSelection, limits XmlParseLimits) (*Document, *FormationFailure) {
	if ctx != nil && ctx.Err() != nil {
		return nil, &FormationFailure{Kind: FormationFailureCancelled}
	}
	if !matchesProfile(profile) {
		return nil, profileFailure("xml.profile.unknown@1")
	}
	request, encodingFailure := encodingRequest(selection)
	if encodingFailure != nil {
		return nil, encodingFailure
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(source, request, document.SourceLimits{
		MaxRawBytes:         limits.Common.MaxSourceBytes,
		MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
		MaxDecodedScalars:   limits.MaxDecodedScalars,
	})
	if err != nil {
		sourceError, ok := err.(*document.SourceError)
		if !ok {
			sourceError = &document.SourceError{Kind: document.SourceErrorInvalidUtf8}
		}
		return nil, &FormationFailure{Kind: FormationFailureSource, Source: sourceError}
	}
	if failure := validateProfileEncoding(snapshot, selection); failure != nil {
		return nil, failure
	}
	decoded, ok := snapshot.DecodedText()
	if !ok {
		return nil, &FormationFailure{Kind: FormationFailureProfile, CodeValue: "xml.source.decoding@1"}
	}
	return buildDocument(ctx, snapshot, decoded, limits)
}

// matchesProfile reports whether the profile is one this package forms.
func matchesProfile(profile XmlProfile) bool {
	return profile == XmlProfileSafeV1
}

// encodingRequest resolves the source encoding request under the RFC 0012
// §2 table (parser.rs:56-80).
func encodingRequest(selection XmlEncodingSelection) (document.EncodingRequest, *FormationFailure) {
	request := document.NewEncodingRequest(document.Utf8Encoding()).
		WithBomPolicy(document.BomPolicyDetectUnicode)
	if selection.Kind() == XmlEncodingExplicitKind {
		encoding := selection.Encoding()
		admitted := encoding.Equal(document.Utf8Encoding()) ||
			encoding.Equal(document.Utf16LeEncoding()) ||
			encoding.Equal(document.Utf16BeEncoding())
		if !admitted {
			// UTF-32, Latin-1, Windows code pages, and other IANA encodings
			// are explicit v1 Profile exclusions.
			return document.EncodingRequest{}, profileFailure("xml.profile.encoding@1")
		}
		return request.WithCallerOverride(encoding), nil
	}
	return request, nil
}

// validateProfileEncoding verifies the resolved source facts under the
// profile table (parser.rs:82-108).
func validateProfileEncoding(snapshot *document.SourceSnapshot,
	selection XmlEncodingSelection) *FormationFailure {
	facts := snapshot.EncodingFacts()
	valid := false
	switch selection.Kind() {
	case XmlEncodingProfileDefaultKind:
		valid = facts.Selected().Equal(document.Utf8Encoding()) ||
			facts.Selected().Equal(document.Utf16LeEncoding()) ||
			facts.Selected().Equal(document.Utf16BeEncoding())
	case XmlEncodingExplicitKind:
		selected := facts.Selected()
		encoding := selection.Encoding()
		switch {
		case encoding.Equal(document.Utf8Encoding()):
			valid = selected.Equal(document.Utf8Encoding())
		case encoding.Equal(document.Utf16LeEncoding()):
			valid = selected.Equal(document.Utf16LeEncoding()) &&
				facts.Bom() != nil && *facts.Bom() == document.BomUtf16Le
		case encoding.Equal(document.Utf16BeEncoding()):
			valid = selected.Equal(document.Utf16BeEncoding()) &&
				facts.Bom() != nil && *facts.Bom() == document.BomUtf16Be
		}
	}
	if !valid {
		return profileFailure("xml.profile.encoding@1")
	}
	return nil
}

func profileFailure(code string) *FormationFailure {
	return &FormationFailure{Kind: FormationFailureProfile, CodeValue: code}
}

// errorRegistry returns the current error-code registry used to validate
// every diagnostic the family produces. The xml.* codes are registered by
// RFC 0012 as part of the `xml.1.0-safe@1` contract and do not enter the
// consema-protocol core error registry (RFC 0012 §12), so the family
// constructs its diagnostics directly; this helper exists for the same
// registry surface as the other family packages.
func errorRegistry() protocol.ErrorCodeRegistry {
	return protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
}
