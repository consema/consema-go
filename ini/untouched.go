package ini

import (
	"fmt"

	"consema.dev/consema/document"
)

// This file implements the untouched-byte proof (consema-document
// untouched_proof.rs; the same record shapes as go/yaml/untouched.go).

// UntouchedByteRegion is one maximal unchanged raw-byte interval mapped
// across two source snapshots.
type UntouchedByteRegion struct {
	oldStart, oldEnd int
	newStart, newEnd int
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
// verification failure.
type UntouchedByteProofErrorKind uint8

// The stable proof failure classes.
const (
	ProofErrorEncodingMismatch UntouchedByteProofErrorKind = iota
	ProofErrorInvalidReplacement
	ProofErrorReplacementOrder
	ProofErrorDuplicateInsertion
	ProofErrorOriginalMismatch
	ProofErrorTargetMismatch
	ProofErrorCoordinateOverflow
	ProofErrorInvalidRegion
	ProofErrorDigestMismatch
	ProofErrorProofMismatch
)

// UntouchedByteProofError is a proof construction or verification failure.
type UntouchedByteProofError struct {
	// Kind identifies the failure.
	Kind UntouchedByteProofErrorKind
	// Index is the zero-based replacement or region position.
	Index int
}

// Error implements error.
func (e *UntouchedByteProofError) Error() string {
	switch e.Kind {
	case ProofErrorEncodingMismatch:
		return "ini: untouched-byte proof encoding mismatch"
	case ProofErrorInvalidReplacement:
		return fmt.Sprintf("ini: untouched-byte proof invalid replacement at %d", e.Index)
	case ProofErrorReplacementOrder:
		return fmt.Sprintf("ini: untouched-byte proof replacement order at %d", e.Index)
	case ProofErrorDuplicateInsertion:
		return fmt.Sprintf("ini: untouched-byte proof duplicate insertion at %d", e.Index)
	case ProofErrorOriginalMismatch:
		return fmt.Sprintf("ini: untouched-byte proof original mismatch at %d", e.Index)
	case ProofErrorTargetMismatch:
		return "ini: untouched-byte proof target mismatch"
	case ProofErrorCoordinateOverflow:
		return "ini: untouched-byte proof coordinate overflow"
	case ProofErrorInvalidRegion:
		return fmt.Sprintf("ini: untouched-byte proof invalid region at %d", e.Index)
	case ProofErrorDigestMismatch:
		return "ini: untouched-byte proof digest mismatch"
	case ProofErrorProofMismatch:
		return "ini: untouched-byte proof region mismatch"
	}
	return "ini: untouched-byte proof error"
}

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan.
type UntouchedByteProof struct {
	baseDigest   document.ContentDigest
	targetDigest document.ContentDigest
	regions      []UntouchedByteRegion
}

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot.
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (UntouchedByteProof, error) {
	regions, err := expectedRegions(base, target, replacements)
	if err != nil {
		return UntouchedByteProof{}, err
	}
	return UntouchedByteProof{
		baseDigest:   base.Digest(),
		targetDigest: target.Digest(),
		regions:      regions,
	}, nil
}

// Verify rechecks digests, replacement preconditions, exact target bytes,
// and every region fact.
func (p UntouchedByteProof) Verify(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) error {
	if !base.Digest().Equal(p.baseDigest) || !target.Digest().Equal(p.targetDigest) {
		return &UntouchedByteProofError{Kind: ProofErrorDigestMismatch}
	}
	expected, err := expectedRegions(base, target, replacements)
	if err != nil {
		return err
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

// BaseDigest returns the required base digest.
func (p UntouchedByteProof) BaseDigest() document.ContentDigest { return p.baseDigest }

// TargetDigest returns the required target digest.
func (p UntouchedByteProof) TargetDigest() document.ContentDigest { return p.targetDigest }

// Regions returns the canonical maximal unchanged regions.
func (p UntouchedByteProof) Regions() []UntouchedByteRegion {
	return append([]UntouchedByteRegion(nil), p.regions...)
}

// expectedRegions computes the canonical maximal unchanged regions.
func expectedRegions(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) ([]UntouchedByteRegion, error) {
	if !base.EncodingFacts().Equal(target.EncodingFacts()) {
		return nil, &UntouchedByteProofError{Kind: ProofErrorEncodingMismatch}
	}
	baseBytes := base.Bytes()
	targetBytes := target.Bytes()
	regions := make([]UntouchedByteRegion, 0, len(replacements)+1)
	oldCursor := 0
	newCursor := 0
	for index, replacement := range replacements {
		var previous *document.SourceReplacement
		if index > 0 {
			previous = &replacements[index-1]
		}
		if failure := validateProofReplacement(baseBytes, index, replacement,
			previous != nil, previous); failure != nil {
			return nil, failure
		}
		unchangedLen := replacement.OldStart() - oldCursor
		newUnchangedEnd := newCursor + unchangedLen
		if newUnchangedEnd > len(targetBytes) ||
			string(targetBytes[newCursor:newUnchangedEnd]) != string(baseBytes[oldCursor:replacement.OldStart()]) {
			return nil, &UntouchedByteProofError{Kind: ProofErrorTargetMismatch}
		}
		regions = append(regions, UntouchedByteRegion{
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
	}
	tailLen := len(baseBytes) - oldCursor
	newEnd := newCursor + tailLen
	if newEnd != len(targetBytes) ||
		string(targetBytes[newCursor:newEnd]) != string(baseBytes[oldCursor:]) {
		return nil, &UntouchedByteProofError{Kind: ProofErrorTargetMismatch}
	}
	regions = append(regions, UntouchedByteRegion{
		oldStart: oldCursor, oldEnd: len(baseBytes),
		newStart: newCursor, newEnd: newEnd,
	})
	if failure := validateRegions(regions); failure != nil {
		return nil, failure
	}
	return regions, nil
}

// validateProofReplacement checks one replacement against the base bytes
// and the previous replacement.
func validateProofReplacement(base []byte, index int, replacement document.SourceReplacement,
	hasPrevious bool, previous *document.SourceReplacement) error {
	if replacement.OldStart() < 0 || replacement.OldEnd() < replacement.OldStart() ||
		replacement.OldEnd() > len(base) {
		return &UntouchedByteProofError{Kind: ProofErrorInvalidReplacement, Index: index}
	}
	if hasPrevious && replacement.OldStart() < previous.OldEnd() {
		return &UntouchedByteProofError{Kind: ProofErrorReplacementOrder, Index: index}
	}
	if hasPrevious && replacement.OldStart() == previous.OldEnd() &&
		replacement.OldStart() == replacement.OldEnd() {
		return &UntouchedByteProofError{Kind: ProofErrorDuplicateInsertion, Index: index}
	}
	original := base[replacement.OldStart():replacement.OldEnd()]
	if string(original) != string(replacement.Original()) {
		return &UntouchedByteProofError{Kind: ProofErrorOriginalMismatch, Index: index}
	}
	return nil
}

// validateRegions checks the region length, ordering, and overlap
// invariants; regions skip the replaced intervals, so they need not be
// contiguous (untouched_proof.rs validate_regions).
func validateRegions(regions []UntouchedByteRegion) error {
	previous := UntouchedByteRegion{oldStart: -1, oldEnd: -1, newStart: -1, newEnd: -1}
	for index, region := range regions {
		if region.oldStart < 0 || region.oldStart > region.oldEnd ||
			region.newStart < 0 || region.newStart > region.newEnd ||
			region.oldEnd-region.oldStart != region.newEnd-region.newStart ||
			region.oldStart < previous.oldEnd || region.newStart < previous.newEnd {
			return &UntouchedByteProofError{Kind: ProofErrorInvalidRegion, Index: index}
		}
		previous = region
	}
	return nil
}
