package json

import "consema.dev/consema/document"

// AssociationPlacement is the explicit association placement of one
// structural edit (consema-document AssociationPlacement; RFC 0004).
// go/document does not yet carry this type, so the JSON family owns it
// until the document milestone that absorbs it.
type AssociationPlacement struct {
	kind   PlacementKind
	anchor document.NodeRef
}

// PlacementKind is the closed placement category.
type PlacementKind uint8

// The four frozen placements.
const (
	// PlacementStart places at the container start.
	PlacementStart PlacementKind = iota
	// PlacementEnd places at the container end.
	PlacementEnd
	// PlacementBefore places before one exact anchor association.
	PlacementBefore
	// PlacementAfter places after one exact anchor association.
	PlacementAfter
)

// PlacementAtStart places at the container start.
func PlacementAtStart() AssociationPlacement {
	return AssociationPlacement{kind: PlacementStart}
}

// PlacementAtEnd places at the container end.
func PlacementAtEnd() AssociationPlacement {
	return AssociationPlacement{kind: PlacementEnd}
}

// BeforeAnchor places before one exact anchor association.
func BeforeAnchor(anchor document.NodeRef) AssociationPlacement {
	return AssociationPlacement{kind: PlacementBefore, anchor: anchor}
}

// AfterAnchor places after one exact anchor association.
func AfterAnchor(anchor document.NodeRef) AssociationPlacement {
	return AssociationPlacement{kind: PlacementAfter, anchor: anchor}
}

// Kind returns the closed placement category.
func (p AssociationPlacement) Kind() PlacementKind { return p.kind }

// Anchor returns the exact anchor association of Before/After placements.
func (p AssociationPlacement) Anchor() document.NodeRef { return p.anchor }
