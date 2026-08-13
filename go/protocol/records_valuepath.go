package protocol

// Portable value-path and association-location wire records. These are the
// language-neutral portable-location facts of `core.query-result@1`,
// `core.provenance-map@1`, and `core.materialization-*` records
// (consema-rs/consema-core value_path.rs; query.rs:441-560).

import (
	"consema.dev/consema/core"
)

// ValuePathSegment is one portable path segment (value_path.rs: the four
// closed segment kinds).
type ValuePathSegment struct {
	// Kind is "ObjectValue", "SequenceElement", "EntryKey", or
	// "EntryValue".
	Kind string
	// Key is the object key of ObjectValue segments.
	Key string
	// Index is the ordinal of SequenceElement/EntryKey/EntryValue segments.
	Index uint64
}

// ValuePath is an ordered portable value path rooted at the input value.
type ValuePath struct {
	segments []ValuePathSegment
}

// RootValuePath returns the empty path.
func RootValuePath() ValuePath {
	return ValuePath{}
}

// Child returns the path extended by one segment.
func (p ValuePath) Child(segment ValuePathSegment) ValuePath {
	segments := make([]ValuePathSegment, 0, len(p.segments)+1)
	segments = append(segments, p.segments...)
	segments = append(segments, segment)
	return ValuePath{segments: segments}
}

// Segments returns the ordered path segments.
func (p ValuePath) Segments() []ValuePathSegment {
	return append([]ValuePathSegment(nil), p.segments...)
}

// LenString returns a stable structural key of the path.
func (p ValuePath) LenString() string {
	key := ""
	for _, segment := range p.segments {
		key += segment.Kind
		if segment.Kind == "ObjectValue" {
			key += segment.Key
		}
	}
	return key
}

// Equal reports whether two paths are identical.
func (p ValuePath) Equal(other ValuePath) bool {
	if len(p.segments) != len(other.segments) {
		return false
	}
	for index := range p.segments {
		if p.segments[index] != other.segments[index] {
			return false
		}
	}
	return true
}

// Less reports whether p precedes other in the canonical wire order.
func (p ValuePath) Less(other ValuePath) bool {
	limit := len(p.segments)
	if len(other.segments) < limit {
		limit = len(other.segments)
	}
	for index := 0; index < limit; index++ {
		left, right := p.segments[index], other.segments[index]
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Kind == "ObjectValue" {
			if left.Key != right.Key {
				return left.Key < right.Key
			}
		} else if left.Index != right.Index {
			return left.Index < right.Index
		}
	}
	return len(p.segments) < len(other.segments)
}

// AssociationRole is the closed association role of an association
// location (query.rs:526-537).
type AssociationRole string

// The three frozen association roles.
const (
	AssociationRoleObjectEntry      AssociationRole = "ObjectEntry"
	AssociationRoleObjectKey        AssociationRole = "ObjectKey"
	AssociationRoleEntryMappingItem AssociationRole = "EntryMappingEntry"
)

// AssociationLocation is one portable association location: a container
// path, a zero-based ordinal, and a role (query.rs:514-523).
type AssociationLocation struct {
	container ValuePath
	ordinal   uint64
	role      AssociationRole
}

// NewAssociationLocation builds one association location.
func NewAssociationLocation(container ValuePath, ordinal uint64, role AssociationRole) AssociationLocation {
	return AssociationLocation{container: container, ordinal: ordinal, role: role}
}

// Container returns the container path.
func (l AssociationLocation) Container() ValuePath { return l.container }

// Ordinal returns the zero-based association ordinal.
func (l AssociationLocation) Ordinal() uint64 { return l.ordinal }

// Role returns the association role.
func (l AssociationLocation) Role() AssociationRole { return l.role }

// Equal reports whether two association locations are identical.
func (l AssociationLocation) Equal(other AssociationLocation) bool {
	return l.container.Equal(other.container) && l.ordinal == other.ordinal && l.role == other.role
}

// Less reports whether l precedes other in the canonical wire order
// (container, then ordinal, then role).
func (l AssociationLocation) Less(other AssociationLocation) bool {
	if !l.container.Equal(other.container) {
		return l.container.Less(other.container)
	}
	if l.ordinal != other.ordinal {
		return l.ordinal < other.ordinal
	}
	return l.role < other.role
}

// pathValue encodes a ValuePath (query.rs:441-465).
func pathValue(path ValuePath) (core.Value, error) {
	segments := make([]core.Value, 0, len(path.segments))
	for _, segment := range path.segments {
		switch segment.Kind {
		case "ObjectValue":
			value, err := core.NewObject(
				core.Entry{Key: "kind", Value: core.String("ObjectValue")},
				core.Entry{Key: "key", Value: core.String(segment.Key)},
			)
			if err != nil {
				return nil, err
			}
			segments = append(segments, value)
		case "SequenceElement", "EntryKey", "EntryValue":
			value, err := core.NewObject(
				core.Entry{Key: "kind", Value: core.String(segment.Kind)},
				core.Entry{Key: "index", Value: integerValue(segment.Index)},
			)
			if err != nil {
				return nil, err
			}
			segments = append(segments, value)
		default:
			return nil, invalid("$", "unknown path segment kind")
		}
	}
	array := core.NewArray(segments...)
	return core.NewObject(core.Entry{Key: "segments", Value: array})
}

// parsePath strictly decodes a ValuePath (query.rs:466-513).
func parsePath(value core.Value, path string) (ValuePath, error) {
	fields, err := exactFields(value, []string{"segments"}, path)
	if err != nil {
		return ValuePath{}, err
	}
	result := RootValuePath()
	segments, err := sequenceOf(fields[0], path+".segments")
	if err != nil {
		return ValuePath{}, err
	}
	for index, segment := range segments {
		segmentPath := path + ".segments[" + uint32String(uint32(index)) + "]"
		entries, ok := segment.(*core.Object)
		if !ok {
			return ValuePath{}, protocolError(KindWrongType, segmentPath, "expected path segment Object")
		}
		if len(entries.Entries()) == 0 || entries.Entries()[0].Key != "kind" {
			return ValuePath{}, invalid(segmentPath, "missing segment kind")
		}
		kind, err := stringOf(entries.Entries()[0].Value, segmentPath+".kind")
		if err != nil {
			return ValuePath{}, err
		}
		switch kind {
		case "ObjectValue":
			fields, err := exactFields(segment, []string{"kind", "key"}, segmentPath)
			if err != nil {
				return ValuePath{}, err
			}
			key, err := stringOf(fields[1], segmentPath+".key")
			if err != nil {
				return ValuePath{}, err
			}
			result = result.Child(ValuePathSegment{Kind: "ObjectValue", Key: key})
		case "SequenceElement", "EntryKey", "EntryValue":
			fields, err := exactFields(segment, []string{"kind", "index"}, segmentPath)
			if err != nil {
				return ValuePath{}, err
			}
			indexNumber, err := unsigned64(fields[1], segmentPath+".index")
			if err != nil {
				return ValuePath{}, err
			}
			result = result.Child(ValuePathSegment{Kind: kind, Index: indexNumber})
		default:
			return ValuePath{}, invalid(segmentPath, "unknown path segment")
		}
	}
	return result, nil
}

// associationValue encodes an AssociationLocation (query.rs:514-523).
func associationValue(location AssociationLocation) (core.Value, error) {
	container, err := pathValue(location.container)
	if err != nil {
		return nil, err
	}
	return core.NewObject(
		core.Entry{Key: "container", Value: container},
		core.Entry{Key: "ordinal", Value: integerValue(location.ordinal)},
		core.Entry{Key: "role", Value: core.String(string(location.role))},
	)
}

// parseAssociation strictly decodes an AssociationLocation
// (query.rs:525-553).
func parseAssociation(value core.Value, path string) (AssociationLocation, error) {
	fields, err := exactFields(value, []string{"container", "ordinal", "role"}, path)
	if err != nil {
		return AssociationLocation{}, err
	}
	container, err := parsePath(fields[0], path+".container")
	if err != nil {
		return AssociationLocation{}, err
	}
	ordinal, err := unsigned64(fields[1], path+".ordinal")
	if err != nil {
		return AssociationLocation{}, err
	}
	role, err := parseAssociationRole(fields[2], path+".role")
	if err != nil {
		return AssociationLocation{}, err
	}
	return NewAssociationLocation(container, ordinal, role), nil
}

func parseAssociationRole(value core.Value, path string) (AssociationRole, error) {
	text, err := stringOf(value, path)
	if err != nil {
		return "", err
	}
	switch AssociationRole(text) {
	case AssociationRoleObjectEntry, AssociationRoleObjectKey, AssociationRoleEntryMappingItem:
		return AssociationRole(text), nil
	}
	return "", invalid(path, "unknown association role")
}
