package document

// BinaryRegion is one format-owned region in an opaque binary source
// (document lib.rs:492-528).
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
// coverage for one opaque binary source (document lib.rs:530-579).
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
