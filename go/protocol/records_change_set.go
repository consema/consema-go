package protocol

// The `core.change-set@1` record (consema-rs/consema-protocol/src/change.rs):
// transferable source edits, node mappings, and diagnostics of one edit
// commit, with caller-assigned stable source identities.

import (
	"consema.dev/consema/core"
)

// SourceEditMessage is one exact source edit between the old and new
// sources (change.rs:14-55).
type SourceEditMessage struct {
	// OldStart is the inclusive old-source start.
	OldStart uint64
	// OldEnd is the exclusive old-source end.
	OldEnd uint64
	// NewStart is the inclusive new-source start.
	NewStart uint64
	// NewEnd is the exclusive new-source end.
	NewEnd uint64
	// Replacement is the exact replacement bytes.
	Replacement []byte
}

// NewSourceEditMessage validates range order and replacement/new-range
// agreement (change.rs:27-45).
func NewSourceEditMessage(oldStart, oldEnd, newStart, newEnd uint64, replacement []byte) (*SourceEditMessage, error) {
	if oldStart > oldEnd || newStart > newEnd || newEnd-newStart != uint64(len(replacement)) {
		return nil, invalid("$.source_edit", "invalid ranges or replacement length")
	}
	return &SourceEditMessage{
		OldStart:    oldStart,
		OldEnd:      oldEnd,
		NewStart:    newStart,
		NewEnd:      newEnd,
		Replacement: append([]byte(nil), replacement...),
	}, nil
}

// NodeMappingStatus is the closed node-mapping topology status
// (change.rs:58-...).
type NodeMappingStatus string

// The six frozen mapping statuses.
const (
	MappingPreserved NodeMappingStatus = "Preserved"
	MappingReplaced  NodeMappingStatus = "Replaced"
	MappingDeleted   NodeMappingStatus = "Deleted"
	MappingSplit     NodeMappingStatus = "Split"
	MappingMerged    NodeMappingStatus = "Merged"
	MappingUnmapped  NodeMappingStatus = "Unmapped"
)

// NodeMappingMessage is one portable node-mapping fact using caller-defined
// stable locators (change.rs:57-124).
type NodeMappingMessage struct {
	// OldLocators are the one or more old locators.
	OldLocators []string
	// NewLocators are the zero or more new locators.
	NewLocators []string
	// Status is the mapping topology/status.
	Status NodeMappingStatus
	// Reason is the stable reason for non-trivial or unresolved mapping.
	Reason *string
}

// NewNodeMappingMessage validates locator topology against mapping status
// (change.rs:76-118).
func NewNodeMappingMessage(oldLocators, newLocators []string, status NodeMappingStatus,
	reason *string) (*NodeMappingMessage, error) {
	if !uniqueLocators(oldLocators) || !uniqueLocators(newLocators) {
		return nil, invalid("$.node_mapping", "locators must be non-empty, bounded, and unique per side")
	}
	for _, locator := range append(append([]string(nil), oldLocators...), newLocators...) {
		if locator == "" || len(locator) > 4096 {
			return nil, invalid("$.node_mapping", "locators must be non-empty, bounded, and unique per side")
		}
	}
	topology := false
	needsReason := false
	switch status {
	case MappingPreserved:
		topology = len(oldLocators) == 1 && len(newLocators) == 1
	case MappingReplaced:
		topology = len(oldLocators) == 1 && len(newLocators) <= 1
		needsReason = len(newLocators) == 0
	case MappingDeleted:
		topology = len(oldLocators) == 1 && len(newLocators) == 0
		needsReason = true
	case MappingSplit:
		topology = len(oldLocators) == 1 && len(newLocators) >= 2
		needsReason = true
	case MappingMerged:
		topology = len(oldLocators) >= 2 && len(newLocators) == 1
		needsReason = true
	case MappingUnmapped:
		topology = len(oldLocators) > 0 && len(newLocators) == 0
		needsReason = true
	}
	hasReason := reason != nil && *reason != "" && len(*reason) <= 1024
	if !topology || needsReason != hasReason {
		return nil, invalid("$.node_mapping", "mapping topology or reason contradicts status")
	}
	return &NodeMappingMessage{
		OldLocators: append([]string(nil), oldLocators...),
		NewLocators: append([]string(nil), newLocators...),
		Status:      status,
		Reason:      reason,
	}, nil
}

func uniqueLocators(locators []string) bool {
	seen := make(map[string]bool, len(locators))
	for _, locator := range locators {
		if seen[locator] {
			return false
		}
		seen[locator] = true
	}
	return true
}

// ChangeSetMessage is the complete `core.change-set@1` record with external
// source and node identities (change.rs:126-133).
type ChangeSetMessage struct {
	oldSourceID string
	newSourceID string
	sourceEdits []*SourceEditMessage
	nodeMaps    []*NodeMappingMessage
	diagnostics []*Diagnostic
}

// NewChangeSetMessage validates source identities, edit order, and global
// old-locator uniqueness (change.rs:134-181).
func NewChangeSetMessage(oldSourceID, newSourceID string, sourceEdits []*SourceEditMessage,
	nodeMaps []*NodeMappingMessage, diagnostics []*Diagnostic) (*ChangeSetMessage, error) {
	if oldSourceID == "" || newSourceID == "" || len(oldSourceID) > 1024 || len(newSourceID) > 1024 {
		return nil, invalid("$", "source IDs must be non-empty and bounded")
	}
	for index := 1; index < len(sourceEdits); index++ {
		if sourceEdits[index-1].OldEnd > sourceEdits[index].OldStart ||
			sourceEdits[index-1].NewEnd > sourceEdits[index].NewStart {
			return nil, invalid("$.source_edits", "edits must be ordered and non-overlapping in both snapshots")
		}
	}
	seenLocators := make(map[string]bool)
	for _, mapping := range nodeMaps {
		for _, locator := range mapping.OldLocators {
			if seenLocators[locator] {
				return nil, invalid("$.node_mappings", "an old locator may participate in only one mapping fact")
			}
			seenLocators[locator] = true
		}
	}
	return &ChangeSetMessage{
		oldSourceID: oldSourceID,
		newSourceID: newSourceID,
		sourceEdits: sourceEdits,
		nodeMaps:    nodeMaps,
		diagnostics: diagnostics,
	}, nil
}

// OldSourceID returns the stable old-source identity.
func (m *ChangeSetMessage) OldSourceID() string { return m.oldSourceID }

// NewSourceID returns the stable new-source identity.
func (m *ChangeSetMessage) NewSourceID() string { return m.newSourceID }

// SourceEdits returns the ordered source edits.
func (m *ChangeSetMessage) SourceEdits() []*SourceEditMessage { return m.sourceEdits }

// NodeMappings returns the ordered node mappings.
func (m *ChangeSetMessage) NodeMappings() []*NodeMappingMessage { return m.nodeMaps }

// Diagnostics returns the ordered diagnostics.
func (m *ChangeSetMessage) Diagnostics() []*Diagnostic { return m.diagnostics }

// ToValue encodes `core.change-set@1` (change.rs:302-331).
func (m *ChangeSetMessage) ToValue() (core.Value, error) {
	edits := make([]core.Value, 0, len(m.sourceEdits))
	for _, edit := range m.sourceEdits {
		value, err := core.NewObject(
			core.Entry{Key: "old_start", Value: integerValue(edit.OldStart)},
			core.Entry{Key: "old_end", Value: integerValue(edit.OldEnd)},
			core.Entry{Key: "new_start", Value: integerValue(edit.NewStart)},
			core.Entry{Key: "new_end", Value: integerValue(edit.NewEnd)},
			core.Entry{Key: "replacement", Value: core.NewBytes(edit.Replacement)},
		)
		if err != nil {
			return nil, err
		}
		edits = append(edits, value)
	}
	mappings := make([]core.Value, 0, len(m.nodeMaps))
	for _, mapping := range m.nodeMaps {
		oldLocators := make([]core.Value, 0, len(mapping.OldLocators))
		for _, locator := range mapping.OldLocators {
			oldLocators = append(oldLocators, core.String(locator))
		}
		newLocators := make([]core.Value, 0, len(mapping.NewLocators))
		for _, locator := range mapping.NewLocators {
			newLocators = append(newLocators, core.String(locator))
		}
		value, err := core.NewObject(
			core.Entry{Key: "old_locators", Value: core.NewArray(oldLocators...)},
			core.Entry{Key: "new_locators", Value: core.NewArray(newLocators...)},
			core.Entry{Key: "status", Value: core.String(string(mapping.Status))},
			core.Entry{Key: "reason", Value: nullableString(mapping.Reason)},
		)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, value)
	}
	diagnostics := make([]core.Value, 0, len(m.diagnostics))
	for _, diagnostic := range m.diagnostics {
		value, err := diagnostic.ToValue()
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, value)
	}
	return core.NewObject(
		core.Entry{Key: "schema", Value: core.String("core.change-set@1")},
		core.Entry{Key: "old_source_id", Value: core.String(m.oldSourceID)},
		core.Entry{Key: "new_source_id", Value: core.String(m.newSourceID)},
		core.Entry{Key: "source_edits", Value: core.NewArray(edits...)},
		core.Entry{Key: "node_mappings", Value: core.NewArray(mappings...)},
		core.Entry{Key: "diagnostics", Value: core.NewArray(diagnostics...)},
	)
}

// FromValue strictly decodes `core.change-set@1` under the v1 registry
// (change.rs:332-341).
func (m *ChangeSetMessage) FromValue(value core.Value) (*ChangeSetMessage, error) {
	return m.FromValueWithRegistry(value, DefaultErrorCodeRegistry())
}

// FromValueWithRegistry strictly decodes the record under one explicit
// semantic-model registry (change.rs:337-...).
func (m *ChangeSetMessage) FromValueWithRegistry(value core.Value, registry ErrorCodeRegistry) (*ChangeSetMessage, error) {
	fields, err := schemaFields(value, "core.change-set@1",
		[]string{"schema", "old_source_id", "new_source_id", "source_edits", "node_mappings", "diagnostics"}, "$")
	if err != nil {
		return nil, err
	}
	oldSourceID, err := stringOf(fields[1], "$.old_source_id")
	if err != nil {
		return nil, err
	}
	newSourceID, err := stringOf(fields[2], "$.new_source_id")
	if err != nil {
		return nil, err
	}
	editValues, err := sequenceOf(fields[3], "$.source_edits")
	if err != nil {
		return nil, err
	}
	sourceEdits := make([]*SourceEditMessage, 0, len(editValues))
	for index, editValue := range editValues {
		path := "$.source_edits[" + uint32String(uint32(index)) + "]"
		editFields, err := exactFields(editValue,
			[]string{"old_start", "old_end", "new_start", "new_end", "replacement"}, path)
		if err != nil {
			return nil, err
		}
		oldStart, err := unsigned64(editFields[0], path+".old_start")
		if err != nil {
			return nil, err
		}
		oldEnd, err := unsigned64(editFields[1], path+".old_end")
		if err != nil {
			return nil, err
		}
		newStart, err := unsigned64(editFields[2], path+".new_start")
		if err != nil {
			return nil, err
		}
		newEnd, err := unsigned64(editFields[3], path+".new_end")
		if err != nil {
			return nil, err
		}
		replacement, ok := editFields[4].(core.Bytes)
		if !ok {
			return nil, protocolError(KindWrongType, path+".replacement", "expected Bytes")
		}
		edit, err := NewSourceEditMessage(oldStart, oldEnd, newStart, newEnd, replacement)
		if err != nil {
			return nil, err
		}
		sourceEdits = append(sourceEdits, edit)
	}
	mappingValues, err := sequenceOf(fields[4], "$.node_mappings")
	if err != nil {
		return nil, err
	}
	nodeMaps := make([]*NodeMappingMessage, 0, len(mappingValues))
	for index, mappingValue := range mappingValues {
		path := "$.node_mappings[" + uint32String(uint32(index)) + "]"
		mappingFields, err := exactFields(mappingValue,
			[]string{"old_locators", "new_locators", "status", "reason"}, path)
		if err != nil {
			return nil, err
		}
		oldLocators, err := stringSequence(mappingFields[0], path+".old_locators")
		if err != nil {
			return nil, err
		}
		newLocators, err := stringSequence(mappingFields[1], path+".new_locators")
		if err != nil {
			return nil, err
		}
		statusText, err := stringOf(mappingFields[2], path+".status")
		if err != nil {
			return nil, err
		}
		reason, err := optionalString(mappingFields[3], path+".reason")
		if err != nil {
			return nil, err
		}
		mapping, err := NewNodeMappingMessage(oldLocators, newLocators, NodeMappingStatus(statusText), reason)
		if err != nil {
			return nil, err
		}
		nodeMaps = append(nodeMaps, mapping)
	}
	diagnosticValues, err := sequenceOf(fields[5], "$.diagnostics")
	if err != nil {
		return nil, err
	}
	diagnostics := make([]*Diagnostic, 0, len(diagnosticValues))
	for _, diagnosticValue := range diagnosticValues {
		diagnostic := &Diagnostic{}
		decoded, err := diagnostic.FromValue(diagnosticValue, registry)
		if err != nil {
			return nil, err
		}
		diagnostics = append(diagnostics, decoded)
	}
	return NewChangeSetMessage(oldSourceID, newSourceID, sourceEdits, nodeMaps, diagnostics)
}

// stringSequence reads a Sequence of String values.
func stringSequence(value core.Value, path string) ([]string, error) {
	items, err := sequenceOf(value, path)
	if err != nil {
		return nil, err
	}
	output := make([]string, 0, len(items))
	for index, item := range items {
		text, err := stringOf(item, path+"["+uint32String(uint32(index))+"]")
		if err != nil {
			return nil, err
		}
		output = append(output, text)
	}
	return output, nil
}
