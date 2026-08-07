package hcl

import (
	"context"
	"fmt"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// FormationFailureKind classifies a fatal formation failure (the Rust
// FatalFormationFailure; RFC 0014 §3).
type FormationFailureKind uint8

// The closed formation failure classes.
const (
	// FormationFailureSource: the raw bytes could not form a valid source
	// snapshot under the UTF-8 source contract, or the caller's encoding
	// selection conflicts with it.
	FormationFailureSource FormationFailureKind = iota
	// FormationFailureResourceLimit: a configured parse bound was exceeded;
	// nothing is truncated to success (hard gate 4).
	FormationFailureResourceLimit
	// FormationFailureCancelled: the caller's context was cancelled during
	// bounded formation. This is a Go-idiomatic resource guard, never a
	// language-neutral fact.
	FormationFailureCancelled
)

// FormationFailure is a fatal HCL formation failure. It implements error,
// the RFC 0016 §6 Code() contract, and Unwrap for the wrapped source
// failure.
type FormationFailure struct {
	// Kind identifies the failure.
	Kind FormationFailureKind
	// Source is the wrapped source construction failure of Kind Source.
	Source *document.SourceError
	// Code is the stable frozen diagnostic code of the failure.
	Code string
	// Category is the frozen diagnostic category of the failure.
	Category protocol.DiagnosticCategory
	// Arguments are the deterministic failure arguments.
	Arguments map[string]string
	// Primary is the optional primary source location.
	Primary *protocol.SourceLocation
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
		return "hcl: " + e.Code
	case FormationFailureResourceLimit:
		return fmt.Sprintf("hcl: parse limit %s exceeded: observed %d, limit %d",
			e.Name, e.Observed, e.Limit)
	case FormationFailureCancelled:
		return "hcl: parse cancelled"
	}
	return "hcl: formation failure"
}

// DiagnosticCode returns the stable frozen code of the failure.
func (e *FormationFailure) DiagnosticCode() string { return e.Code }

// Diagnostics returns the ordered failure diagnostics (one entry).
func (e *FormationFailure) Diagnostics() []*protocol.Diagnostic {
	return []*protocol.Diagnostic{{
		Code:       e.Code,
		Category:   e.Category,
		Severity:   protocol.SeverityError,
		Primary:    e.Primary,
		Arguments:  e.Arguments,
		Occurrence: 0,
	}}
}

// Unwrap returns the wrapped source failure of Kind Source.
func (e *FormationFailure) Unwrap() error {
	if e.Kind == FormationFailureSource && e.Source != nil {
		return e.Source
	}
	return nil
}

// Parse forms one complete immutable HCL document snapshot under one exact
// profile (RFC 0014 §1, §3, §5; RFC 0016 §5.1).
//
// The profile is selected by the caller before formation; neither the `.tf`
// nor the `.tfvars` extension selects a profile, representation, or
// encoding. The source contract is the frozen UTF-8 contract of RFC 0014
// §2: a BOM is Recovered with `hcl.parse.byte-order-mark@1`, invalid UTF-8
// is a fatal formation failure with `hcl.parse.invalid-utf8@1`, and a lone
// CR is Recovered with `hcl.parse.lone-cr@1`. Under `hcl.tfvars@1`, a
// block anywhere at the top level makes formation Recovered with one
// `hcl.tfvars.block-not-allowed@1` diagnostic per top-level block
// occurrence (RFC 0014 §5).
//
// ctx carries cancellation only: an already-cancelled context fails fast,
// and a cancellation during bounded formation aborts with
// FormationFailureCancelled.
func Parse(ctx context.Context, source []byte, profile HclProfile,
	selection HclEncodingSelection, limits HclParseLimits) (*Document, *FormationFailure) {
	if _, ok := selection.Validate(); !ok {
		return nil, &FormationFailure{
			Kind:      FormationFailureSource,
			Code:      "hcl.parse.encoding@1",
			Category:  protocol.CategoryEncoding,
			Arguments: map[string]string{},
		}
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, &FormationFailure{Kind: FormationFailureCancelled}
	}
	if len(source) > limits.Common.MaxSourceBytes {
		return nil, resourceLimitFailure("source-bytes", len(source), limits.Common.MaxSourceBytes)
	}
	snapshot, err := document.NewSourceSnapshotFromRaw(source,
		document.NewEncodingRequest(document.Utf8Encoding()).
			WithBomPolicy(document.BomPolicyTreatAsContent),
		document.SourceLimits{
			MaxRawBytes:         limits.Common.MaxSourceBytes,
			MaxDecodedUTF8Bytes: limits.MaxDecodedUTF8Bytes,
			MaxDecodedScalars:   limits.MaxDecodedScalars,
		})
	if err != nil {
		sourceError, ok := err.(*document.SourceError)
		if !ok {
			sourceError = &document.SourceError{Kind: document.SourceErrorInvalidUtf8}
		}
		switch sourceError.Kind {
		case document.SourceErrorInvalidUtf8, document.SourceErrorInvalidSequence:
			return nil, &FormationFailure{
				Kind:     FormationFailureSource,
				Source:   sourceError,
				Code:     "hcl.parse.invalid-utf8@1",
				Category: protocol.CategoryEncoding,
				Primary: &protocol.SourceLocation{
					SourceID:  "snapshot",
					StartByte: uint64(sourceError.ByteOffset),
					EndByte:   uint64(sourceError.ByteOffset),
				},
				Arguments: map[string]string{},
			}
		case document.SourceErrorResourceLimit:
			return nil, resourceLimitFailure(sourceError.Name, sourceError.Observed, sourceError.Limit)
		case document.SourceErrorOffsetOverflow:
			return nil, &FormationFailure{
				Kind:      FormationFailureResourceLimit,
				Code:      "hcl.limit.offset-overflow@1",
				Category:  protocol.CategoryResource,
				Arguments: map[string]string{},
			}
		default:
			return nil, &FormationFailure{
				Kind:      FormationFailureSource,
				Source:    sourceError,
				Code:      "hcl.parse.internal@1",
				Category:  protocol.CategoryResource,
				Arguments: map[string]string{},
			}
		}
	}
	document, failure := parseHCL(snapshot, profile, limits)
	if failure != nil {
		return nil, lexErrorFailure(failure)
	}
	return document, nil
}

// lexErrorFailure maps one internal lexer/parser error onto the typed
// formation failure.
func lexErrorFailure(err error) *FormationFailure {
	if lexErr, ok := err.(*hclLexError); ok {
		return &FormationFailure{
			Kind:      FormationFailureResourceLimit,
			Code:      lexErr.code,
			Category:  lexErr.category,
			Arguments: lexErr.arguments,
		}
	}
	return &FormationFailure{
		Kind:      FormationFailureResourceLimit,
		Code:      "hcl.parse.internal@1",
		Category:  protocol.CategoryResource,
		Arguments: map[string]string{},
	}
}

// resourceLimitFailure builds a `hcl.limit.<name>@1` fatal failure (RFC
// 0014 §11).
func resourceLimitFailure(name string, observed, limit int) *FormationFailure {
	return &FormationFailure{
		Kind:      FormationFailureResourceLimit,
		Code:      "hcl.limit." + name + "@1",
		Category:  protocol.CategoryResource,
		Name:      name,
		Observed:  observed,
		Limit:     limit,
		Arguments: map[string]string{"limit": intString(limit), "observed": intString(observed)},
	}
}

// errorRegistry returns the current error-code registry used to validate
// every diagnostic the family produces. The `hcl.*` codes are registered
// by RFC 0014 §11 outside the consema-protocol core registry, so
// construction never relies on registry validation for them; the registry
// is kept for the shared-core codes of the operation surfaces.
func errorRegistry() protocol.ErrorCodeRegistry {
	return protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7)
}
