package json

import "consema.dev/consema/document"

// This file implements the JSON-family untouched-byte proof surface over
// the shared go/document proof (consema-document untouched_proof.rs).

// UntouchedByteRegion is one maximal unchanged raw-byte interval mapped
// across two source snapshots.
type UntouchedByteRegion = document.UntouchedByteRegion

// UntouchedByteProofErrorKind classifies a proof construction or
// verification failure.
type UntouchedByteProofErrorKind = document.UntouchedByteProofErrorKind

// The closed proof failure classes.
const (
	// ProofErrorEncodingMismatch: base and target encoding facts differ.
	ProofErrorEncodingMismatch = document.ProofErrorEncodingMismatch
	// ProofErrorInvalidReplacement: a replacement has an inverted or
	// out-of-bounds old interval.
	ProofErrorInvalidReplacement = document.ProofErrorInvalidReplacement
	// ProofErrorReplacementOrder: replacements are not in canonical
	// non-overlapping order.
	ProofErrorReplacementOrder = document.ProofErrorReplacementOrder
	// ProofErrorDuplicateInsertion: two replacements target the same
	// insertion point.
	ProofErrorDuplicateInsertion = document.ProofErrorDuplicateInsertion
	// ProofErrorOriginalMismatch: base bytes do not satisfy an
	// original-byte precondition.
	ProofErrorOriginalMismatch = document.ProofErrorOriginalMismatch
	// ProofErrorTargetMismatch: the supplied target bytes are not the
	// exact result of the replacement set.
	ProofErrorTargetMismatch = document.ProofErrorTargetMismatch
	// ProofErrorCoordinateOverflow: a target coordinate calculation
	// overflowed.
	ProofErrorCoordinateOverflow = document.ProofErrorCoordinateOverflow
	// ProofErrorInvalidRegion: a transferred region has an invalid range,
	// unequal lengths, order, or canonicality.
	ProofErrorInvalidRegion = document.ProofErrorInvalidRegion
	// ProofErrorDigestMismatch: supplied snapshots do not have the proof's
	// declared digests.
	ProofErrorDigestMismatch = document.ProofErrorDigestMismatch
	// ProofErrorProofMismatch: region facts differ from the canonical
	// proof of the supplied replacement set.
	ProofErrorProofMismatch = document.ProofErrorProofMismatch
)

// UntouchedByteProofError is the typed proof failure. It implements
// error and the RFC 0016 §6 Code() contract with the generic
// invalid-input code.
type UntouchedByteProofError = document.UntouchedByteProofError

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan.
type UntouchedByteProof = document.UntouchedByteProof

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot.
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (*UntouchedByteProof, *UntouchedByteProofError) {
	proof, err := document.CreateUntouchedByteProof(base, target, replacements)
	if err != nil {
		return nil, err.(*document.UntouchedByteProofError)
	}
	return &proof, nil
}

// IsUntouchedProofError reports whether one error is a proof failure.
func IsUntouchedProofError(err error) bool {
	return document.IsUntouchedProofError(err)
}
