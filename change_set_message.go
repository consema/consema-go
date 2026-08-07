package consema

// This file implements the change-set externalization composition
// (crates/consema-protocol/src/change.rs `ChangeSetMessage::from_document`;
// RFC 0004 §16): one document-layer ChangeSet becomes the transferable
// `core.change-set@1` record with caller-assigned stable source and node
// identities. The document layer owns the change facts; the protocol
// package owns the wire record; only this root package may compose them
// (RFC 0016 §3.2: cross-package composition lives in the root package).

import (
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// ChangeSetMessageFromDocument externalizes one change set with caller-
// stable source identities and a caller-supplied node locator
// (change.rs:184-262). The locator must resolve every NodeRef that
// participates in the change set's node mappings; an unresolvable old or
// new NodeRef is a protocol error (process-local handle), exactly as the
// Rust composition maps it.
func ChangeSetMessageFromDocument(changeSet *document.ChangeSet,
	oldSourceID, newSourceID string,
	locator func(document.NodeRef) (string, bool)) (*protocol.ChangeSetMessage, error) {
	return ChangeSetMessageFromDocumentWithRegistry(changeSet, oldSourceID, newSourceID,
		locator, protocol.DefaultErrorCodeRegistry())
}

// ChangeSetMessageFromDocumentWithRegistry externalizes one change set
// under one explicit semantic-model error registry (change.rs:200-262).
func ChangeSetMessageFromDocumentWithRegistry(changeSet *document.ChangeSet,
	oldSourceID, newSourceID string, locator func(document.NodeRef) (string, bool),
	registry protocol.ErrorCodeRegistry) (*protocol.ChangeSetMessage, error) {
	sourceEdits := make([]*protocol.SourceEditMessage, 0, len(changeSet.SourceEdits()))
	for _, edit := range changeSet.SourceEdits() {
		message, err := protocol.NewSourceEditMessage(
			uint64(edit.OldSpan.StartByte()), uint64(edit.OldSpan.EndByte()),
			uint64(edit.NewSpan.StartByte()), uint64(edit.NewSpan.EndByte()),
			edit.Replacement)
		if err != nil {
			return nil, err
		}
		sourceEdits = append(sourceEdits, message)
	}
	nodeMappings := make([]*protocol.NodeMappingMessage, 0, len(changeSet.NodeMappings()))
	for _, mapping := range changeSet.NodeMappings() {
		oldLocator, ok := locator(mapping.Old)
		if !ok {
			return nil, &protocol.ProtocolError{
				Kind:   protocol.KindProcessLocalHandle,
				Path:   "$.node_mappings.old",
				Detail: "old NodeRef has no stable caller locator",
			}
		}
		var newLocators []string
		if mapping.New != nil {
			newLocator, ok := locator(*mapping.New)
			if !ok {
				return nil, &protocol.ProtocolError{
					Kind:   protocol.KindProcessLocalHandle,
					Path:   "$.node_mappings.new",
					Detail: "new NodeRef has no stable caller locator",
				}
			}
			newLocators = []string{newLocator}
		}
		message, err := protocol.NewNodeMappingMessage([]string{oldLocator}, newLocators,
			mapping.Status, mapping.Reason)
		if err != nil {
			return nil, err
		}
		nodeMappings = append(nodeMappings, message)
	}
	return protocol.NewChangeSetMessage(oldSourceID, newSourceID, sourceEdits,
		nodeMappings, changeSet.Diagnostics())
}
