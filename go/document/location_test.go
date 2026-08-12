package document

import "testing"

// TestNodeRefsAreSnapshotBound covers the snapshot-binding facts of node
// handles and spans.
func TestNodeRefsAreSnapshotBound(t *testing.T) {
	first := NewDocumentAuthority()
	second := NewDocumentAuthority()
	node := first.NodeRef(0, RoleValue)
	if node.Role() != RoleValue || node.Index() != 0 || node.Snapshot() != first.Identity() {
		t.Errorf("node facts = %v %d %v", node.Role(), node.Index(), node.Snapshot())
	}
	if err := second.Verify(node); locationName(err) != "WrongSnapshot" {
		t.Errorf("cross-snapshot verify = %v, want WrongSnapshot", err)
	}
	if err := first.Verify(node); err != nil {
		t.Errorf("own-snapshot verify = %v", err)
	}
	if index, err := second.ResolveIndex(node); err == nil || index != 0 {
		t.Errorf("cross-snapshot resolve = %d, %v", index, err)
	}
	if index, err := first.ResolveIndex(node); err != nil || index != 0 {
		t.Errorf("own-snapshot resolve = %d, %v", index, err)
	}
}

// TestSpanValidation covers the inverted-span rejection and the byte-offset
// semantics.
func TestSpanValidation(t *testing.T) {
	authority := NewDocumentAuthority()
	if _, err := authority.Span(3, 2); locationName(err) != "InvertedSpan" {
		t.Errorf("inverted span = %v", err)
	}
	span, err := authority.Span(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	if span.StartByte() != 1 || span.EndByte() != 4 || span.Len() != 3 || span.IsEmpty() {
		t.Errorf("span facts = %+v", span)
	}
	if span.Snapshot() != authority.Identity() {
		t.Error("span snapshot binding")
	}
	empty, err := authority.Span(2, 2)
	if err != nil || !empty.IsEmpty() {
		t.Errorf("empty span = %+v, %v", empty, err)
	}
}

// TestBinaryRegionsCoverExactBytes covers the binary-coverage capability
// facts (source-v1.json: source.binary.*).
func TestBinaryRegionsCoverExactBytes(t *testing.T) {
	authority := NewDocumentAuthority()
	header := NewBinaryRegion(authority.NodeRef(0, RoleBinaryRegion), mustSpan(t, authority, 0, 1), "example.header@1")
	payload := NewBinaryRegion(authority.NodeRef(1, RoleBinaryRegion), mustSpan(t, authority, 1, 4), "example.payload@1")
	index, err := NewBinaryStructuralIndex(authority.Identity(), 4, []BinaryRegion{header, payload})
	if err != nil {
		t.Fatal(err)
	}
	regions := index.Regions()
	if len(regions) != 2 {
		t.Fatalf("region count %d", len(regions))
	}
	if regions[0].Kind() != "example.header@1" || regions[1].Span().EndByte() != 4 {
		t.Errorf("region facts %+v", regions)
	}
	if regions[0].NodeRef().Role() != RoleBinaryRegion {
		t.Error("region role")
	}

	// Empty source has an empty valid index.
	empty, err := NewBinaryStructuralIndex(NewDocumentAuthority().Identity(), 0, nil)
	if err != nil || len(empty.Regions()) != 0 {
		t.Errorf("empty coverage = %v, %v", empty, err)
	}

	// A coverage gap is rejected.
	gap := NewBinaryRegion(authority.NodeRef(2, RoleBinaryRegion), mustSpan(t, authority, 2, 4), "example.payload@1")
	if _, err := NewBinaryStructuralIndex(authority.Identity(), 4, []BinaryRegion{header, gap}); locationName(err) != "IncompleteStructuralCoverage" {
		t.Errorf("gap = %v, want IncompleteStructuralCoverage", err)
	}

	// A wrong role is rejected.
	wrongRole := NewBinaryRegion(authority.NodeRef(3, RoleToken), mustSpan(t, authority, 0, 1), "example.byte@1")
	if _, err := NewBinaryStructuralIndex(authority.Identity(), 1, []BinaryRegion{wrongRole}); locationName(err) != "WrongRole" {
		t.Errorf("wrong role = %v, want WrongRole", err)
	}

	// An empty kind is rejected.
	emptyKind := NewBinaryRegion(authority.NodeRef(4, RoleBinaryRegion), mustSpan(t, authority, 0, 1), "")
	if _, err := NewBinaryStructuralIndex(authority.Identity(), 1, []BinaryRegion{emptyKind}); locationName(err) != "InvalidBinaryRegionKind" {
		t.Errorf("empty kind = %v, want InvalidBinaryRegionKind", err)
	}

	// A duplicate structural identity is rejected.
	duplicate := []BinaryRegion{
		NewBinaryRegion(authority.NodeRef(5, RoleBinaryRegion), mustSpan(t, authority, 0, 1), "a"),
		NewBinaryRegion(authority.NodeRef(5, RoleBinaryRegion), mustSpan(t, authority, 1, 2), "b"),
	}
	if _, err := NewBinaryStructuralIndex(authority.Identity(), 2, duplicate); locationName(err) != "DuplicateStructuralIdentity" {
		t.Errorf("duplicate identity = %v, want DuplicateStructuralIdentity", err)
	}
}

func mustSpan(t *testing.T, authority DocumentAuthority, start, end int) Span {
	t.Helper()
	span, err := authority.Span(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return span
}
