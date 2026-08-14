package document

import "fmt"

// SourcePatchLimits are the resource bounds for constructing or applying
// one source patch (source_patch.rs).
type SourcePatchLimits struct {
	// Source are the limits for the resulting source snapshot.
	Source SourceLimits
	// MaxReplacements is the maximum number of ordered replacements.
	MaxReplacements int
	// MaxPatchBytes is the maximum sum of original and replacement payload
	// bytes.
	MaxPatchBytes int
}

// DefaultSourcePatchLimits returns the frozen defaults (default source
// limits, 100k replacements, 128 MiB patch bytes).
func DefaultSourcePatchLimits() SourcePatchLimits {
	return SourcePatchLimits{
		Source:          DefaultSourceLimits(),
		MaxReplacements: 100_000,
		MaxPatchBytes:   128 << 20,
	}
}

// SourceReplacement is one raw-byte precondition and replacement in a
// source patch (source_patch.rs). The original bytes must appear
// byte-for-byte at the old range of the base source; after application the
// target bytes are exactly the replacement bytes.
type SourceReplacement struct {
	oldStart          int
	oldEnd            int
	original          []byte
	replacement       []byte
	redactOriginal    bool
	redactReplacement bool
}

// NewSourceReplacement creates one half-open raw-byte replacement.
func NewSourceReplacement(oldStart, oldEnd int, original, replacement []byte) SourceReplacement {
	return SourceReplacement{
		oldStart:    oldStart,
		oldEnd:      oldEnd,
		original:    append([]byte(nil), original...),
		replacement: append([]byte(nil), replacement...),
	}
}

// WithOriginalRedacted controls whether the original bytes are hidden in
// review/debug presentation.
func (r SourceReplacement) WithOriginalRedacted(redacted bool) SourceReplacement {
	r.redactOriginal = redacted
	return r
}

// WithReplacementRedacted controls whether replacement bytes are hidden in
// review/debug presentation.
func (r SourceReplacement) WithReplacementRedacted(redacted bool) SourceReplacement {
	r.redactReplacement = redacted
	return r
}

// OldStart returns the inclusive start raw byte.
func (r SourceReplacement) OldStart() int { return r.oldStart }

// OldEnd returns the exclusive end raw byte.
func (r SourceReplacement) OldEnd() int { return r.oldEnd }

// Original returns the exact bytes required at the old range.
func (r SourceReplacement) Original() []byte { return append([]byte(nil), r.original...) }

// Replacement returns the exact bytes written in place of the old range.
func (r SourceReplacement) Replacement() []byte { return append([]byte(nil), r.replacement...) }

// RedactOriginal reports whether review/debug presentation hides the
// original bytes.
func (r SourceReplacement) RedactOriginal() bool { return r.redactOriginal }

// RedactReplacement reports whether review/debug presentation hides the
// replacement bytes.
func (r SourceReplacement) RedactReplacement() bool { return r.redactReplacement }

// SourcePatchErrorKind classifies a stable source patch construction or
// application failure (source_patch.rs).
type SourcePatchErrorKind uint8

// The stable source patch failure classes.
const (
	// PatchErrorInvalidReplacement: a replacement start followed its end or
	// its original byte count disagreed with its range.
	PatchErrorInvalidReplacement SourcePatchErrorKind = iota
	// PatchErrorReplacementOrder: the replacement order was not canonical
	// or two old ranges overlapped.
	PatchErrorReplacementOrder
	// PatchErrorDuplicateInsertion: two replacements targeted the same
	// zero-width insertion point.
	PatchErrorDuplicateInsertion
	// PatchErrorBaseMismatch: the base raw bytes do not have the declared
	// digest.
	PatchErrorBaseMismatch
	// PatchErrorOriginalMismatch: the base bytes in one range do not equal
	// the declared precondition.
	PatchErrorOriginalMismatch
	// PatchErrorTargetMismatch: the computed result bytes do not have the
	// declared digest.
	PatchErrorTargetMismatch
	// PatchErrorEncodingMismatch: the base or resulting encoding facts
	// disagree with the patch.
	PatchErrorEncodingMismatch
	// PatchErrorResourceLimit: a patch count, byte, output, or allocation
	// bound was exceeded.
	PatchErrorResourceLimit
	// PatchErrorSource: the resulting bytes could not form a valid source
	// snapshot.
	PatchErrorSource
)

// SourcePatchError is a stable source patch construction or application
// failure. It implements error, the RFC 0016 §6 Code() contract, and
// Unwrap for the wrapped source error.
type SourcePatchError struct {
	// Kind identifies the failure.
	Kind SourcePatchErrorKind
	// Index is the zero-based replacement position of the failing
	// replacement.
	Index int
	// Name is the stable limit name of a ResourceLimit.
	Name string
	// Observed is the observed amount of a ResourceLimit.
	Observed int
	// Limit is the configured maximum of a ResourceLimit.
	Limit int
	// Source is the wrapped source construction failure.
	Source *SourceError
}

// Error implements error.
func (e *SourcePatchError) Error() string {
	switch e.Kind {
	case PatchErrorInvalidReplacement:
		return fmt.Sprintf("source patch: invalid replacement %d", e.Index)
	case PatchErrorReplacementOrder:
		return fmt.Sprintf("source patch: replacement order failed at %d", e.Index)
	case PatchErrorDuplicateInsertion:
		return fmt.Sprintf("source patch: duplicate insertion at %d", e.Index)
	case PatchErrorBaseMismatch:
		return "source patch: base digest does not match"
	case PatchErrorOriginalMismatch:
		return fmt.Sprintf("source patch: original bytes mismatch at %d", e.Index)
	case PatchErrorTargetMismatch:
		return "source patch: target digest does not match"
	case PatchErrorEncodingMismatch:
		return "source patch: encoding facts mismatch"
	case PatchErrorResourceLimit:
		return fmt.Sprintf("source patch: limit %s exceeded: observed %d, limit %d", e.Name, e.Observed, e.Limit)
	case PatchErrorSource:
		return "source patch: " + e.Source.Error()
	}
	return "source patch error"
}

// Code returns the frozen registered code for the failure (source_patch.rs:
// 434-458). Structural replacement failures carry the generic registered
// invalid-input code, exactly as the Rust document layer maps them.
func (e *SourcePatchError) Code() string {
	switch e.Kind {
	case PatchErrorBaseMismatch:
		return "core.source.patch-base-mismatch@1"
	case PatchErrorOriginalMismatch:
		return "core.source.patch-original-mismatch@1"
	case PatchErrorTargetMismatch:
		return "core.source.patch-target-mismatch@1"
	case PatchErrorEncodingMismatch:
		return "core.source.encoding-conflict@1"
	case PatchErrorResourceLimit:
		return "core.source.resource-limit@1"
	case PatchErrorSource:
		return e.Source.Code()
	case PatchErrorInvalidReplacement, PatchErrorReplacementOrder, PatchErrorDuplicateInsertion:
		return "core.protocol.invalid-value@1"
	}
	return "core.protocol.invalid-value@1"
}

// Unwrap returns the wrapped source error of PatchErrorSource.
func (e *SourcePatchError) Unwrap() error {
	if e.Kind == PatchErrorSource {
		return e.Source
	}
	return nil
}

// SourcePatchRedactionError is a review-redaction selection failure; patch
// bytes and application facts are unchanged (source_patch.rs).
type SourcePatchRedactionErrorKind uint8

// The redaction failure classes.
const (
	// PatchRedactionAllocationFailed: the redacted review view could not
	// allocate its replacement index.
	PatchRedactionAllocationFailed SourcePatchRedactionErrorKind = iota
	// PatchRedactionUnknownReplacement: the requested replacement index does
	// not exist.
	PatchRedactionUnknownReplacement
)

// SourcePatchRedactionError is a typed review-redaction selection failure.
type SourcePatchRedactionError struct {
	// Kind identifies the failure.
	Kind SourcePatchRedactionErrorKind
	// Index is the requested zero-based replacement index.
	Index int
}

// Error implements error.
func (e *SourcePatchRedactionError) Error() string {
	if e.Kind == PatchRedactionUnknownReplacement {
		return fmt.Sprintf("source patch: unknown replacement index %d", e.Index)
	}
	return "source patch: redaction allocation failed"
}

// Code returns the registered invalid-input code (RFC 0016 §6).
func (e *SourcePatchRedactionError) Code() string { return "core.protocol.invalid-value@1" }

// SourcePatch is the immutable, transferable set of facts needed to verify
// one raw source transition (source_patch.rs). Applying the patch
// is atomic: either every original-byte precondition holds, the result
// reproduces the patch's encoding facts and target digest, and a new
// immutable snapshot exists, or nothing is returned.
type SourcePatch struct {
	baseDigest   ContentDigest
	targetDigest ContentDigest
	encoding     EncodingFacts
	replacements []SourceReplacement
	metadata     map[string]string
}

// NewSourcePatch builds a self-consistent patch against one immutable base
// snapshot (source_patch.rs).
func NewSourcePatch(base *SourceSnapshot, replacements []SourceReplacement,
	metadata map[string]string, limits SourcePatchLimits) (*SourcePatch, error) {
	if err := validatePatchReplacements(replacements, limits); err != nil {
		return nil, err
	}
	targetBytes, err := applyPatchReplacements(base.bytes, replacements, limits)
	if err != nil {
		return nil, err
	}
	target, err := NewSourceSnapshotFromRaw(targetBytes, base.encoding.resolutionRequest(), limits.Source)
	if err != nil {
		return nil, &SourcePatchError{Kind: PatchErrorSource, Source: sourceErrorOf(err)}
	}
	if !target.encoding.Equal(base.encoding) {
		return nil, &SourcePatchError{Kind: PatchErrorEncodingMismatch}
	}
	return &SourcePatch{
		baseDigest:   base.digest,
		targetDigest: target.digest,
		encoding:     base.encoding,
		replacements: append([]SourceReplacement(nil), replacements...),
		metadata:     cloneMetadata(metadata),
	}, nil
}

// NewSourcePatchFromFacts creates a patch from externally supplied facts
// after structural and resource validation (source_patch.rs). The
// application still verifies every digest, encoding, and original-byte
// precondition.
func NewSourcePatchFromFacts(baseDigest, targetDigest ContentDigest, encoding EncodingFacts,
	replacements []SourceReplacement, metadata map[string]string,
	limits SourcePatchLimits) (*SourcePatch, error) {
	if err := validatePatchReplacements(replacements, limits); err != nil {
		return nil, err
	}
	return &SourcePatch{
		baseDigest:   baseDigest,
		targetDigest: targetDigest,
		encoding:     encoding,
		replacements: append([]SourceReplacement(nil), replacements...),
		metadata:     cloneMetadata(metadata),
	}, nil
}

// Apply applies all facts atomically and returns a new immutable snapshot
// only on complete success (source_patch.rs).
func (p *SourcePatch) Apply(base *SourceSnapshot, limits SourcePatchLimits) (*SourceSnapshot, error) {
	if err := validatePatchReplacements(p.replacements, limits); err != nil {
		return nil, err
	}
	if !base.digest.Equal(p.baseDigest) {
		return nil, &SourcePatchError{Kind: PatchErrorBaseMismatch}
	}
	if !base.encoding.Equal(p.encoding) {
		return nil, &SourcePatchError{Kind: PatchErrorEncodingMismatch}
	}
	targetBytes, err := applyPatchReplacements(base.bytes, p.replacements, limits)
	if err != nil {
		return nil, err
	}
	target, err := NewSourceSnapshotFromRaw(targetBytes, p.encoding.resolutionRequest(), limits.Source)
	if err != nil {
		return nil, &SourcePatchError{Kind: PatchErrorSource, Source: sourceErrorOf(err)}
	}
	if !target.encoding.Equal(p.encoding) {
		return nil, &SourcePatchError{Kind: PatchErrorEncodingMismatch}
	}
	if !target.digest.Equal(p.targetDigest) {
		return nil, &SourcePatchError{Kind: PatchErrorTargetMismatch}
	}
	return target, nil
}

// BaseDigest returns the required base content identity.
func (p *SourcePatch) BaseDigest() ContentDigest { return p.baseDigest }

// TargetDigest returns the required result content identity.
func (p *SourcePatch) TargetDigest() ContentDigest { return p.targetDigest }

// EncodingFacts returns the encoding facts that both base and result must
// reproduce.
func (p *SourcePatch) EncodingFacts() EncodingFacts { return p.encoding }

// Replacements returns the ordered non-overlapping replacements.
func (p *SourcePatch) Replacements() []SourceReplacement {
	return append([]SourceReplacement(nil), p.replacements...)
}

// Metadata returns the deterministic audit metadata, which never affects
// application. The returned map is a copy; iteration order is unspecified
// and irrelevant to application.
func (p *SourcePatch) Metadata() map[string]string { return cloneMetadata(p.metadata) }

// WithAllReplacementsRedacted marks every replacement payload for redacted
// review/debug presentation. Exact bytes remain present for digest and
// original-byte precondition checks (source_patch.rs).
func (p *SourcePatch) WithAllReplacementsRedacted(redactOriginal, redactReplacement bool) (*SourcePatch, error) {
	replacements := make([]SourceReplacement, 0, len(p.replacements))
	for _, replacement := range p.replacements {
		replacements = append(replacements,
			replacement.WithOriginalRedacted(redactOriginal).WithReplacementRedacted(redactReplacement))
	}
	return &SourcePatch{
		baseDigest:   p.baseDigest,
		targetDigest: p.targetDigest,
		encoding:     p.encoding,
		replacements: replacements,
		metadata:     cloneMetadata(p.metadata),
	}, nil
}

// WithReplacementRedacted marks one exact replacement payload for redacted
// review/debug presentation (source_patch.rs).
func (p *SourcePatch) WithReplacementRedacted(index int, redactOriginal, redactReplacement bool) (*SourcePatch, error) {
	if index < 0 || index >= len(p.replacements) {
		return nil, &SourcePatchRedactionError{Kind: PatchRedactionUnknownReplacement, Index: index}
	}
	replacements := append([]SourceReplacement(nil), p.replacements...)
	replacements[index] = replacements[index].WithOriginalRedacted(redactOriginal).
		WithReplacementRedacted(redactReplacement)
	return &SourcePatch{
		baseDigest:   p.baseDigest,
		targetDigest: p.targetDigest,
		encoding:     p.encoding,
		replacements: replacements,
		metadata:     cloneMetadata(p.metadata),
	}, nil
}

// validatePatchReplacements enforces the replacement ordering, range, and
// budget rules (source_patch.rs).
func validatePatchReplacements(replacements []SourceReplacement, limits SourcePatchLimits) error {
	if len(replacements) > limits.MaxReplacements {
		return &SourcePatchError{Kind: PatchErrorResourceLimit, Name: "patch-replacements",
			Observed: len(replacements), Limit: limits.MaxReplacements}
	}
	patchBytes := 0
	var previous *SourceReplacement
	for index := range replacements {
		replacement := &replacements[index]
		if replacement.oldStart > replacement.oldEnd ||
			len(replacement.original) != replacement.oldEnd-replacement.oldStart {
			return &SourcePatchError{Kind: PatchErrorInvalidReplacement, Index: index}
		}
		if previous != nil {
			if replacement.oldStart == replacement.oldEnd &&
				previous.oldStart == previous.oldEnd &&
				replacement.oldStart == previous.oldStart {
				return &SourcePatchError{Kind: PatchErrorDuplicateInsertion, Index: index}
			}
			if (replacement.oldStart < previous.oldStart ||
				(replacement.oldStart == previous.oldStart && replacement.oldEnd <= previous.oldEnd)) ||
				replacement.oldStart < previous.oldEnd {
				return &SourcePatchError{Kind: PatchErrorReplacementOrder, Index: index}
			}
		}
		next := patchBytes + len(replacement.original) + len(replacement.replacement)
		if next > limits.MaxPatchBytes {
			return &SourcePatchError{Kind: PatchErrorResourceLimit, Name: "patch-bytes",
				Observed: next, Limit: limits.MaxPatchBytes}
		}
		patchBytes = next
		previous = replacement
	}
	return nil
}

// applyPatchReplacements splices the ordered replacements into the base
// bytes, verifying each original precondition and the target byte budget
// (source_patch.rs).
func applyPatchReplacements(base []byte, replacements []SourceReplacement, limits SourcePatchLimits) ([]byte, error) {
	targetLen := len(base)
	for index := range replacements {
		replacement := &replacements[index]
		if replacement.oldEnd > len(base) ||
			string(base[replacement.oldStart:replacement.oldEnd]) != string(replacement.original) {
			return nil, &SourcePatchError{Kind: PatchErrorOriginalMismatch, Index: index}
		}
		targetLen = targetLen - len(replacement.original) + len(replacement.replacement)
		if targetLen > limits.Source.MaxRawBytes {
			return nil, &SourcePatchError{Kind: PatchErrorResourceLimit, Name: "target-raw-bytes",
				Observed: targetLen, Limit: limits.Source.MaxRawBytes}
		}
	}
	output := make([]byte, 0, targetLen)
	cursor := 0
	for index := range replacements {
		replacement := &replacements[index]
		output = append(output, base[cursor:replacement.oldStart]...)
		output = append(output, replacement.replacement...)
		cursor = replacement.oldEnd
	}
	output = append(output, base[cursor:]...)
	return output, nil
}

func sourceErrorOf(err error) *SourceError {
	if sourceError, ok := err.(*SourceError); ok {
		return sourceError
	}
	return &SourceError{Kind: SourceErrorInvalidSequence}
}

func cloneMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
