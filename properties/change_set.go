package properties

import "consema.dev/consema/document"

// This file implements the Properties-family edit record aliases: the
// change set and the transferable dry-run plan (document.ChangeSet and
// document.EditPlan; consema-document change.rs, edit_plan.rs; RFC 0016
// §5.3). The shared records live in go/document; this package keeps the
// local names of its public surface.

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
