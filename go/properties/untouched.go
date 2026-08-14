package properties

import "consema.dev/consema/document"

// This file implements the Properties-family untouched-byte proof surface
// over the shared go/document proof (consema-document untouched_proof.rs):
// evidence that every byte outside the exact replacement plan is
// unchanged across the base and target snapshots.

// UntouchedByteRegion is one maximal unchanged raw-byte interval mapped
// across two source snapshots (document.UntouchedByteRegion;
// untouched_proof.rs).
type UntouchedByteRegion = document.UntouchedByteRegion

// NewUntouchedByteRegion creates one region fact; the enclosing proof
// validates length and ordering.
func NewUntouchedByteRegion(oldStart, oldEnd, newStart, newEnd int) UntouchedByteRegion {
	return document.NewUntouchedByteRegion(oldStart, oldEnd, newStart, newEnd)
}

// UntouchedByteProofErrorKind classifies a proof construction or
// verification failure (document.UntouchedByteProofErrorKind;
// untouched_proof.rs).
type UntouchedByteProofErrorKind = document.UntouchedByteProofErrorKind

// The stable proof failure classes.
const (
	ProofErrorEncodingMismatch   = document.ProofErrorEncodingMismatch
	ProofErrorInvalidReplacement = document.ProofErrorInvalidReplacement
	ProofErrorReplacementOrder   = document.ProofErrorReplacementOrder
	ProofErrorDuplicateInsertion = document.ProofErrorDuplicateInsertion
	ProofErrorOriginalMismatch   = document.ProofErrorOriginalMismatch
	ProofErrorTargetMismatch     = document.ProofErrorTargetMismatch
	ProofErrorCoordinateOverflow = document.ProofErrorCoordinateOverflow
	ProofErrorInvalidRegion      = document.ProofErrorInvalidRegion
	ProofErrorDigestMismatch     = document.ProofErrorDigestMismatch
	ProofErrorProofMismatch      = document.ProofErrorProofMismatch
)

// UntouchedByteProofError is a proof construction or verification
// failure (document.UntouchedByteProofError).
type UntouchedByteProofError = document.UntouchedByteProofError

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan (document.UntouchedByteProof;
// untouched_proof.rs).
type UntouchedByteProof = document.UntouchedByteProof

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot.
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (UntouchedByteProof, error) {
	return document.CreateUntouchedByteProof(base, target, replacements)
}
