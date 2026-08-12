package document

// PlacementKind is the closed placement category of one association
// insertion (consema-document lib.rs:261-272 AssociationPlacement).
type PlacementKind uint8

// The four frozen placements.
const (
	// PlacementStart places the new association at the container start.
	PlacementStart PlacementKind = iota
	// PlacementEnd places the new association at the container end.
	PlacementEnd
	// PlacementBefore places the new association immediately before one
	// exact anchor association.
	PlacementBefore
	// PlacementAfter places the new association immediately after one
	// exact anchor association.
	PlacementAfter
)

// AssociationPlacement is the explicit association placement of one
// structural edit (consema-document AssociationPlacement; RFC 0004).
// Values are constructed by the placement constructors; the four closed
// categories are Start, End, Before(anchor), and After(anchor).
type AssociationPlacement struct {
	kind   PlacementKind
	anchor NodeRef
}

// PlacementAtStart places the new association at the container start.
func PlacementAtStart() AssociationPlacement { return AssociationPlacement{kind: PlacementStart} }

// PlacementAtEnd places the new association at the container end.
func PlacementAtEnd() AssociationPlacement { return AssociationPlacement{kind: PlacementEnd} }

// BeforeAnchor places the new association immediately before one exact
// anchor association.
func BeforeAnchor(anchor NodeRef) AssociationPlacement {
	return AssociationPlacement{kind: PlacementBefore, anchor: anchor}
}

// AfterAnchor places the new association immediately after one exact
// anchor association.
func AfterAnchor(anchor NodeRef) AssociationPlacement {
	return AssociationPlacement{kind: PlacementAfter, anchor: anchor}
}

// Kind returns the closed placement category.
func (p AssociationPlacement) Kind() PlacementKind { return p.kind }

// Anchor returns the exact anchor association of Before/After placements.
func (p AssociationPlacement) Anchor() NodeRef { return p.anchor }
