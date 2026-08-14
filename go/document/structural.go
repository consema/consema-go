package document

// BinaryRegion is one format-owned region in an opaque binary source
// (document lib.rs).
type BinaryRegion struct {
	node NodeRef
	span Span
	kind string
}

// NewBinaryRegion creates a region; its snapshot, role, kind, and coverage
// are validated by the index.
func NewBinaryRegion(node NodeRef, span Span, kind string) BinaryRegion {
	return BinaryRegion{node: node, span: span, kind: kind}
}

// NodeRef returns the process-local structural identity.
func (r BinaryRegion) NodeRef() NodeRef { return r.node }

// Span returns the exact raw byte range.
func (r BinaryRegion) Span() Span { return r.span }

// Kind returns the non-empty stable format-owned kind.
func (r BinaryRegion) Kind() string { return r.kind }

// BinaryStructuralIndex is the exhaustive ordered format-owned region
// coverage for one opaque binary source (document lib.rs).
type BinaryStructuralIndex struct {
	regions []BinaryRegion
}

// NewBinaryStructuralIndex validates exact raw-byte coverage, snapshot
// binding, roles, kinds, and unique identities.
func NewBinaryStructuralIndex(identity SnapshotIdentity, sourceLen int,
	regions []BinaryRegion) (*BinaryStructuralIndex, error) {
	next := 0
	identities := make(map[NodeRef]bool, len(regions))
	for _, region := range regions {
		if region.span.snapshot != identity || region.node.snapshot != identity {
			return nil, &LocationError{Kind: LocationWrongSnapshot}
		}
		if region.node.role != RoleBinaryRegion {
			return nil, &LocationError{Kind: LocationWrongRole}
		}
		if region.kind == "" {
			return nil, &LocationError{Kind: LocationInvalidBinaryRegionKind}
		}
		if identities[region.node] {
			return nil, &LocationError{Kind: LocationDuplicateStructuralIdentity}
		}
		identities[region.node] = true
		if region.span.startByte != next ||
			region.span.endByte <= region.span.startByte ||
			region.span.endByte > sourceLen {
			return nil, &LocationError{Kind: LocationIncompleteStructuralCoverage}
		}
		next = region.span.endByte
	}
	if next != sourceLen {
		return nil, &LocationError{Kind: LocationIncompleteStructuralCoverage}
	}
	return &BinaryStructuralIndex{regions: append([]BinaryRegion(nil), regions...)}, nil
}

// Regions returns the ordered exhaustive regions.
func (i *BinaryStructuralIndex) Regions() []BinaryRegion {
	return append([]BinaryRegion(nil), i.regions...)
}

// StructuralPieceKind is the lossless class of one structural piece
// (document lib.rs).
type StructuralPieceKind uint8

// The three frozen piece classes.
const (
	// PieceToken is a lexical token.
	PieceToken StructuralPieceKind = iota
	// PieceTrivia is whitespace, newline, comment, or profile trivia.
	PieceTrivia
	// PieceErrorRegion is bytes not accepted as token or trivia.
	PieceErrorRegion
)

// StructuralPiece is one source byte interval and its lossless class
// (document lib.rs).
type StructuralPiece struct {
	span Span
	kind StructuralPieceKind
}

// NewStructuralPiece creates one piece; the enclosing index validates
// coverage and snapshot binding.
func NewStructuralPiece(span Span, kind StructuralPieceKind) StructuralPiece {
	return StructuralPiece{span: span, kind: kind}
}

// Span returns the exact raw byte range.
func (p StructuralPiece) Span() Span { return p.span }

// Kind returns the lossless class.
func (p StructuralPiece) Kind() StructuralPieceKind { return p.kind }

// KindName returns the stable piece class: "Token", "Trivia", or
// "ErrorRegion".
func (p StructuralPiece) KindName() string {
	switch p.kind {
	case PieceToken:
		return "Token"
	case PieceTrivia:
		return "Trivia"
	case PieceErrorRegion:
		return "ErrorRegion"
	}
	return "Token"
}

// LosslessStructuralIndex is the exhaustive ordered token/trivia/error-
// region coverage of one source (document lib.rs). Every source
// byte belongs to exactly one piece, in source order. The index validates
// exact byte coverage and snapshot binding at construction.
type LosslessStructuralIndex struct {
	pieces []StructuralPiece
}

// NewLosslessStructuralIndex validates exact raw-byte coverage of the
// source and snapshot binding (document lib.rs; the syntax piece
// identities are the source ordinals, so no duplicate-identity check
// applies).
func NewLosslessStructuralIndex(identity SnapshotIdentity, sourceLen int,
	pieces []StructuralPiece) (*LosslessStructuralIndex, error) {
	next := 0
	for _, piece := range pieces {
		if piece.span.snapshot != identity {
			return nil, &LocationError{Kind: LocationWrongSnapshot}
		}
		if piece.span.startByte != next || piece.span.endByte <= piece.span.startByte ||
			piece.span.endByte > sourceLen {
			return nil, &LocationError{Kind: LocationIncompleteStructuralCoverage}
		}
		next = piece.span.endByte
	}
	if next != sourceLen {
		return nil, &LocationError{Kind: LocationIncompleteStructuralCoverage}
	}
	return &LosslessStructuralIndex{pieces: append([]StructuralPiece(nil), pieces...)}, nil
}

// Pieces returns the ordered exhaustive pieces. The returned slice is a
// copy; pieces are logically immutable.
func (i *LosslessStructuralIndex) Pieces() []StructuralPiece {
	return append([]StructuralPiece(nil), i.pieces...)
}
