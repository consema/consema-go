package json

import "consema.dev/consema/document"

// AssociationPlacement is the explicit association placement of one
// structural edit (document.AssociationPlacement; consema-document
// AssociationPlacement; RFC 0004). The shared type lives in go/document;
// this package keeps the placement constructors and kind constants of
// its public surface.

// PlacementKind is the closed placement category.
type PlacementKind = document.PlacementKind

// The four frozen placements.
const (
	// PlacementStart places at the container start.
	PlacementStart = document.PlacementStart
	// PlacementEnd places at the container end.
	PlacementEnd = document.PlacementEnd
	// PlacementBefore places before one exact anchor association.
	PlacementBefore = document.PlacementBefore
	// PlacementAfter places after one exact anchor association.
	PlacementAfter = document.PlacementAfter
)

// AssociationPlacement is the explicit association placement of one
// structural edit.
type AssociationPlacement = document.AssociationPlacement

// PlacementAtStart places at the container start.
func PlacementAtStart() AssociationPlacement { return document.PlacementAtStart() }

// PlacementAtEnd places at the container end.
func PlacementAtEnd() AssociationPlacement { return document.PlacementAtEnd() }

// BeforeAnchor places before one exact anchor association.
func BeforeAnchor(anchor document.NodeRef) AssociationPlacement {
	return document.BeforeAnchor(anchor)
}

// AfterAnchor places after one exact anchor association.
func AfterAnchor(anchor document.NodeRef) AssociationPlacement {
	return document.AfterAnchor(anchor)
}
