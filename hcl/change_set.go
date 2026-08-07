package hcl

import "consema.dev/consema/document"

// This file implements the HCL-family edit record aliases. The shared edit
// records (ChangeSet, EditPlan, SourceEdit, NodeMapping, SourcePatch,
// UntouchedByteProof, LosslessStructuralIndex) live in go/document
// (consema-document change_set.rs, edit_plan.rs, untouched_proof.rs,
// source_patch.rs); this package aliases them and keeps the format-local
// names that the HCL public surface publishes (RFC 0014 §10).

// SourceEdit is one exact source edit between the old and new snapshots
// (document.SourceEdit; consema-document ChangeSet source edits).
type SourceEdit = document.SourceEdit

// NodeMapping is one old-to-new node mapping fact (document.NodeMapping;
// consema-document ChangeSet node mappings).
type NodeMapping = document.NodeMapping

// ChangeSet is the complete old-to-new change facts of one edit commit
// (document.ChangeSet; RFC 0016 §5.3; consema-document ChangeSet).
type ChangeSet = document.ChangeSet

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (document.EditPlan; RFC 0015 §8.1 read-only
// precedent; consema-document edit_plan.rs).
type EditPlan = document.EditPlan

// UntouchedByteProof is the immutable evidence for every byte outside one
// exact replacement plan (document.UntouchedByteProof).
type UntouchedByteProof = document.UntouchedByteProof

// CreateUntouchedByteProof creates a proof only when the replacements
// exactly produce the supplied target snapshot
// (document.CreateUntouchedByteProof).
func CreateUntouchedByteProof(base, target *document.SourceSnapshot,
	replacements []document.SourceReplacement) (*UntouchedByteProof, error) {
	proof, err := document.CreateUntouchedByteProof(base, target, replacements)
	if err != nil {
		return nil, err
	}
	return &proof, nil
}
