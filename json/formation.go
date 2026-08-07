package json

import (
	"context"
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// FormationFailureKind classifies a fatal formation failure (the Rust
// FatalFormationFailure; consema-document lib.rs).
type FormationFailureKind uint8

// The closed formation failure classes.
const (
	// FormationFailureSource: the raw bytes could not form a valid source
	// snapshot (the UTF-8 input contract).
	FormationFailureSource FormationFailureKind = iota
	// FormationFailureResourceLimit: a configured parse bound was exceeded;
	// nothing is truncated to success.
	FormationFailureResourceLimit
	// FormationFailureCancelled: the caller's context was cancelled during
	// bounded formation. This is a Go-idiomatic resource guard, never a
	// language-neutral fact (the Rust parse has no cancellation).
	FormationFailureCancelled
)

// FormationFailure is a fatal JSON formation failure. It implements error,
// the RFC 0016 §6 Code() contract, and Unwrap for the wrapped source
// failure.
type FormationFailure struct {
	// Kind identifies the failure.
	Kind FormationFailureKind
	// Source is the wrapped source construction failure of Kind Source.
	Source *document.SourceError
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
		return "json: " + e.Source.Error()
	case FormationFailureResourceLimit:
		return fmt.Sprintf("json: parse limit %s exceeded: observed %d, limit %d",
			e.Name, e.Observed, e.Limit)
	case FormationFailureCancelled:
		return "json: parse cancelled"
	}
	return "json: formation failure"
}

// Code returns the frozen registered code for the failure. Cancellation
// uses the only registered cancellation code; it is a Go-only guard and
// never appears in conformance comparison.
func (e *FormationFailure) Code() string {
	switch e.Kind {
	case FormationFailureSource:
		return e.Source.Code()
	case FormationFailureResourceLimit:
		return "core.parse.resource-limit@1"
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

// Parse forms one complete immutable JSON/JSONC/JSON5 document snapshot
// (consema-json parser.rs; RFC 0016 §5.1). The raw bytes must be valid
// UTF-8 (the JSON family input contract); a violation is a fatal source
// failure. ctx carries cancellation only: an already-cancelled context
// fails fast, and a cancellation during bounded formation aborts with
// FormationFailureCancelled.
func Parse(ctx context.Context, source []byte, profile JsonProfile,
	limits document.ParseLimits) (*Document, *FormationFailure) {
	if len(source) > limits.MaxSourceBytes {
		return nil, &FormationFailure{Kind: FormationFailureResourceLimit,
			Name: "source-bytes", Observed: len(source), Limit: limits.MaxSourceBytes}
	}
	snapshot, err := document.NewSourceSnapshotFromUTF8(source)
	if err != nil {
		sourceError, ok := err.(*document.SourceError)
		if !ok {
			sourceError = &document.SourceError{Kind: document.SourceErrorInvalidUtf8}
		}
		return nil, &FormationFailure{Kind: FormationFailureSource, Source: sourceError}
	}
	document, failure := buildDocument(ctx, snapshot, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return document, nil
}

// errorRegistry returns the current error-code registry used to validate
// every diagnostic the family produces. The json.* codes landed across
// semantic-model v1-v7 (0.13.0), so the current v7 registry is required
// (the protocol package's DefaultErrorCodeRegistry pins v1).
func errorRegistry() protocol.ErrorCodeRegistry {
	return protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
}
