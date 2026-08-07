package yaml

import (
	"consema.dev/consema/document"
)

func validateProofReplacement(baseBytes []byte, index int, replacement document.SourceReplacement,
	hasPrevious bool, previous *document.SourceReplacement) *UntouchedByteProofError {
	oldStart := replacement.OldStart()
	oldEnd := replacement.OldEnd()
	if oldStart > oldEnd || oldEnd > len(baseBytes) ||
		len(replacement.Original()) != oldEnd-oldStart {
		return &UntouchedByteProofError{Kind: ProofErrorInvalidReplacement, Index: index}
	}
	if string(baseBytes[oldStart:oldEnd]) != string(replacement.Original()) {
		return &UntouchedByteProofError{Kind: ProofErrorOriginalMismatch, Index: index}
	}
	if hasPrevious {
		if previous.OldStart() > previous.OldEnd() || previous.OldEnd() > oldStart {
			return &UntouchedByteProofError{Kind: ProofErrorReplacementOrder, Index: index}
		}
		if oldStart == oldEnd && previous.OldEnd() == oldStart {
			return &UntouchedByteProofError{Kind: ProofErrorDuplicateInsertion, Index: index}
		}
	}
	return nil
}

func validateRegions(regions []UntouchedByteRegion) *UntouchedByteProofError {
	previous := UntouchedByteRegion{oldEnd: -1, newEnd: -1}
	for index, region := range regions {
		if region.oldStart > region.oldEnd || region.newStart > region.newEnd ||
			region.oldEnd-region.oldStart != region.newEnd-region.newStart ||
			region.oldStart < previous.oldEnd || region.newStart < previous.newEnd {
			return &UntouchedByteProofError{Kind: ProofErrorInvalidRegion, Index: index}
		}
		previous = region
	}
	return nil
}
