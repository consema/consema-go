package ini

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the INI-family edit record aliases: the change
// set and the transferable dry-run plan (document.ChangeSet and
// document.EditPlan; consema-document change_set.rs and edit_plan.rs;
// the same record shapes as the shared go/document records).

// SourceEdit is one exact source edit between the old and new snapshots
// (document.SourceEdit).
type SourceEdit = document.SourceEdit

// NodeMappingStatus is the closed old-to-new node mapping status
// (protocol.NodeMappingStatus; the six frozen values of the shared
// contract).
type NodeMappingStatus = protocol.NodeMappingStatus

// The mapping statuses published by the INI edit surface.
const (
	// NodeMappingReplaced maps the old node to a reparsed result node.
	NodeMappingReplaced = protocol.MappingReplaced
	// NodeMappingDeleted reports the old node was deleted.
	NodeMappingDeleted = protocol.MappingDeleted
	// NodeMappingUnmapped reports the old node has no result identity.
	NodeMappingUnmapped = protocol.MappingUnmapped
)

// NodeMapping is one old-to-new node identity fact (document.NodeMapping).
type NodeMapping = document.NodeMapping

// ChangeSet is the complete old-to-new change facts of one edit commit
// (document.ChangeSet).
type ChangeSet = document.ChangeSet

// EditPlan is the fully validated dry-run plan; possessing it does not
// authorize a write (document.EditPlan; RFC 0015 §8.1 read-only
// precedent).
type EditPlan = document.EditPlan
