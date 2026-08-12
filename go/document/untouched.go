package document

import (
	"errors"
	"fmt"
)

// UntouchedByteRegion is one maximal unchanged raw-byte interval mapped
// across two source snapshots (untouched_proof.rs:7-59).
type UntouchedByteRegion struct {
	oldStart int
	oldEnd   int
	newStart int
	newEnd   int
}

// NewUntouchedByteRegion creates one region fact; the enclosing proof
// validates length and ordering.
func NewUntouchedByteRegion(oldStart, oldEnd, newStart, newEnd int) UntouchedByteRegion {
	return UntouchedByteRegion{oldStart: oldStart, oldEnd: oldEnd, newStart: newStart, newEnd: newEnd}
}

// OldStart returns the inclusive start in the base snapshot.
func (r UntouchedByteRegion) OldStart() int { return r.oldStart }

// OldEnd returns the exclusive end in the base snapshot.
func (r UntouchedByteRegion) OldEnd() int { return r.oldEnd }

// NewStart returns the inclusive start in the target snapshot.
func (r UntouchedByteRegion) NewStart() int { return r.newStart }

// NewEnd returns the exclusive end in the target snapshot.
func (r UntouchedByteRegion) NewEnd() int { return r.newEnd }

// UntouchedByteProofErrorKind classifies a proof construction or
// verification failure (untouched_proof.rs:134-172).
type UntouchedByteProofErrorKind uint8

// The closed proof failure classes.
const (
	// ProofErrorEncodingMismatch: base and target encoding facts differ.
	ProofErrorEncodingMismatch UntouchedByteProofErrorKind = iota
	// ProofErrorInvalidReplacement: a replacement has an inverted or
	// out-of-bounds old interval.
	ProofErrorInvalidReplacement
	// ProofErrorReplacementOrder: replacements are not in canonical
	// non-overlapping order.
	ProofErrorReplacementOrder
	// ProofErrorDuplicateInsertion: two replacements target the same
	// insertion point.
	ProofErrorDuplicateInsertion
	// ProofErrorOriginalMismatch: base bytes do not satisfy an
	// original-byte precondition.
	ProofErrorOriginalMismatch
	// ProofErrorTargetMismatch: the supplied target bytes are not the
	// exact result of the replacement set.
	ProofErrorTargetMismatch
	// ProofErrorCoordinateOverflow: a target coordinate calculation
	// overflowed.
	ProofErrorCoordinateOverflow
	// ProofErrorInvalidRegion: a transferred region has an invalid range,
	// unequal lengths, order, or canonicality.
	ProofErrorInvalidRegion
	// ProofErrorDigestMismatch: supplied snapshots do not have the proof's
	// declared digests.
	ProofErrorDigestMismatch
	// ProofErrorProofMismatch: region facts differ from the canonical proof
	// of the supplied replacement set.
	ProofErrorProofMismatch
)

// UntouchedByteProofError is the typed proof failure. It implements
// error and the RFC 0016 §6 Code() contract with the generic
// invalid-input code.
type UntouchedByteProofError struct {
	// Kind identifies the failure.
	Kind UntouchedByteProofErrorKind
	// Index is the zero-based replacement or region position.
	Index int
}

// Error implements error; the text is human presentation only.
func (e *UntouchedByteProofError) Error() string {
	switch e.Kind {
	case ProofErrorEncodingMismatch:
		return "untouched proof: encoding facts differ"
	case ProofErrorInvalidReplacement:
		return fmt.Sprintf("untouched proof: invalid replacement %d", e.Index)
	case ProofErrorReplacementOrder:
		return fmt.Sprintf("untouched proof: replacement order failed at %d", e.Index)
	case ProofErrorDuplicateInsertion:
		return fmt.Sprintf("untouched proof: duplicate insertion at %d", e.Index)
	case ProofErrorOriginalMismatch:
		return fmt.Sprintf("untouched proof: original bytes mismatch at %d", e.Index)
	case ProofErrorTargetMismatch:
		return "untouched proof: target bytes are not the exact replacement result"
	case ProofErrorCoordinateOverflow:
		return "untouched proof: coordinate overflow"
	case ProofErrorInvalidRegion:
		return fmt.Sprintf("untouched proof: invalid region %d", e.Index)
	case ProofErrorDigestMismatch:
		return "untouched proof: snapshot digest mismatch"
	case ProofErrorProofMismatch:
		return "untouched proof: region facts differ from the canonical proof"
	}
	return "untouched proof: failure"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *UntouchedByteProofError) Code() string { return "core.protocol.invalid-value@1" }

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan (untouched_proof.rs:61-132).
type UntouchedByteProof struct {
	baseDigest   ContentDigest
	targetDigest ContentDigest
	regions      []UntouchedByteRegion
}

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot
// (untouched_proof.rs:70-82).
func CreateUntouchedByteProof(base, target *SourceSnapshot,
	replacements []SourceReplacement) (UntouchedByteProof, error) {
	regions, failure := expectedRegions(base, target, replacements)
	if failure != nil {
		return UntouchedByteProof{}, failure
	}
	return UntouchedByteProof{
		baseDigest:   base.Digest(),
		targetDigest: target.Digest(),
		regions:      regions,
	}, nil
}

// Verify rechecks digests, replacement preconditions, exact target bytes,
// and every region fact (untouched_proof.rs:98-113).
func (p UntouchedByteProof) Verify(base, target *SourceSnapshot,
	replacements []SourceReplacement) *UntouchedByteProofError {
	if !base.Digest().Equal(p.baseDigest) || !target.Digest().Equal(p.targetDigest) {
		return &UntouchedByteProofError{Kind: ProofErrorDigestMismatch}
	}
	expected, failure := expectedRegions(base, target, replacements)
	if failure != nil {
		return failure
	}
	if len(expected) != len(p.regions) {
		return &UntouchedByteProofError{Kind: ProofErrorProofMismatch}
	}
	for index := range expected {
		if expected[index] != p.regions[index] {
			return &UntouchedByteProofError{Kind: ProofErrorProofMismatch}
		}
	}
	return nil
}

// BaseDigest returns the required base content identity.
func (p UntouchedByteProof) BaseDigest() ContentDigest { return p.baseDigest }

// TargetDigest returns the required target content identity.
func (p UntouchedByteProof) TargetDigest() ContentDigest { return p.targetDigest }

// Regions returns the canonical maximal unchanged regions. The returned
// slice is a copy.
func (p UntouchedByteProof) Regions() []UntouchedByteRegion {
	return append([]UntouchedByteRegion(nil), p.regions...)
}

// IsUntouchedProofError reports whether one error is a proof failure.
func IsUntouchedProofError(err error) bool {
	var proofError *UntouchedByteProofError
	return errors.As(err, &proofError)
}

// expectedRegions derives the canonical regions and verifies the exact
// target bytes (untouched_proof.rs:182-245). The canonical proof compares
// the raw source bytes, drops zero-length intervals, and merges adjacent
// unchanged intervals, exactly like the Rust reference.
func expectedRegions(base, target *SourceSnapshot,
	replacements []SourceReplacement) ([]UntouchedByteRegion, *UntouchedByteProofError) {
	if !base.EncodingFacts().Equal(target.EncodingFacts()) {
		return nil, &UntouchedByteProofError{Kind: ProofErrorEncodingMismatch}
	}
	baseBytes := base.Bytes()
	targetBytes := target.Bytes()
	var regions []UntouchedByteRegion
	oldCursor := 0
	newCursor := 0
	var previous *SourceReplacement
	for index := range replacements {
		replacement := &replacements[index]
		if failure := validateProofReplacement(baseBytes, previous, replacement, index); failure != nil {
			return nil, failure
		}
		unchangedLength := replacement.OldStart() - oldCursor
		if unchangedLength < 0 {
			return nil, &UntouchedByteProofError{Kind: ProofErrorInvalidReplacement, Index: index}
		}
		newUnchangedEnd := newCursor + unchangedLength
		if newUnchangedEnd > len(targetBytes) ||
			string(baseBytes[oldCursor:replacement.OldStart()]) != string(targetBytes[newCursor:newUnchangedEnd]) {
			return nil, &UntouchedByteProofError{Kind: ProofErrorTargetMismatch}
		}
		pushProofRegion(&regions, UntouchedByteRegion{
			oldStart: oldCursor, oldEnd: replacement.OldStart(),
			newStart: newCursor, newEnd: newUnchangedEnd,
		})
		replacementEnd := newUnchangedEnd + len(replacement.Replacement())
		if replacementEnd > len(targetBytes) ||
			string(targetBytes[newUnchangedEnd:replacementEnd]) != string(replacement.Replacement()) {
			return nil, &UntouchedByteProofError{Kind: ProofErrorTargetMismatch}
		}
		oldCursor = replacement.OldEnd()
		newCursor = replacementEnd
		previous = replacement
	}
	tailLength := len(baseBytes) - oldCursor
	newEnd := newCursor + tailLength
	if newEnd != len(targetBytes) || string(baseBytes[oldCursor:]) != string(targetBytes[newCursor:newEnd]) {
		return nil, &UntouchedByteProofError{Kind: ProofErrorTargetMismatch}
	}
	pushProofRegion(&regions, UntouchedByteRegion{
		oldStart: oldCursor, oldEnd: len(baseBytes),
		newStart: newCursor, newEnd: newEnd,
	})
	if failure := validateProofRegions(regions); failure != nil {
		return nil, failure
	}
	return regions, nil
}

// validateProofReplacement enforces one replacement's canonical structure
// (untouched_proof.rs:247-281).
func validateProofReplacement(base []byte, previous *SourceReplacement,
	replacement *SourceReplacement, index int) *UntouchedByteProofError {
	if replacement.OldStart() > replacement.OldEnd() ||
		replacement.OldEnd() > len(base) ||
		len(replacement.Original()) != replacement.OldEnd()-replacement.OldStart() {
		return &UntouchedByteProofError{Kind: ProofErrorInvalidReplacement, Index: index}
	}
	if previous != nil {
		if replacement.OldStart() == replacement.OldEnd() &&
			previous.OldStart() == previous.OldEnd() &&
			replacement.OldStart() == previous.OldStart() {
			return &UntouchedByteProofError{Kind: ProofErrorDuplicateInsertion, Index: index}
		}
		if replacement.OldStart() < previous.OldStart() ||
			(replacement.OldStart() == previous.OldStart() && replacement.OldEnd() <= previous.OldEnd()) ||
			replacement.OldStart() < previous.OldEnd() {
			return &UntouchedByteProofError{Kind: ProofErrorReplacementOrder, Index: index}
		}
	}
	if string(base[replacement.OldStart():replacement.OldEnd()]) != string(replacement.Original()) {
		return &UntouchedByteProofError{Kind: ProofErrorOriginalMismatch, Index: index}
	}
	return nil
}

// pushProofRegion merges adjacent regions and drops empty ones
// (untouched_proof.rs:283-295).
func pushProofRegion(regions *[]UntouchedByteRegion, region UntouchedByteRegion) {
	if region.oldStart == region.oldEnd {
		return
	}
	if count := len(*regions); count > 0 {
		previous := &(*regions)[count-1]
		if previous.oldEnd == region.oldStart && previous.newEnd == region.newStart {
			previous.oldEnd = region.oldEnd
			previous.newEnd = region.newEnd
			return
		}
	}
	*regions = append(*regions, region)
}

// validateProofRegions enforces the canonical region structure
// (untouched_proof.rs:297-317).
func validateProofRegions(regions []UntouchedByteRegion) *UntouchedByteProofError {
	var previous *UntouchedByteRegion
	for index := range regions {
		region := &regions[index]
		if region.oldStart >= region.oldEnd || region.newStart >= region.newEnd ||
			region.oldEnd-region.oldStart != region.newEnd-region.newStart {
			return &UntouchedByteProofError{Kind: ProofErrorInvalidRegion, Index: index}
		}
		if previous != nil {
			if region.oldStart < previous.oldEnd || region.newStart < previous.newEnd ||
				(region.oldStart == previous.oldEnd && region.newStart == previous.newEnd) {
				return &UntouchedByteProofError{Kind: ProofErrorInvalidRegion, Index: index}
			}
		}
		previous = region
	}
	return nil
}
