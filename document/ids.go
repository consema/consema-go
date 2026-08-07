package document

// FormatFamilyId is a stable namespaced format family contract (document
// lib.rs:344-372).
type FormatFamilyId struct {
	id      string
	version uint32
}

// NewFormatFamilyId creates a format family ID.
func NewFormatFamilyId(id string, version uint32) FormatFamilyId {
	return FormatFamilyId{id: id, version: version}
}

// ID returns the namespace.
func (f FormatFamilyId) ID() string { return f.id }

// Version returns the version.
func (f FormatFamilyId) Version() uint32 { return f.version }

// ProfileId is an immutable named language profile (document
// lib.rs:374-402).
type ProfileId struct {
	id      string
	version uint32
}

// NewProfileId creates a profile ID.
func NewProfileId(id string, version uint32) ProfileId {
	return ProfileId{id: id, version: version}
}

// ID returns the namespace.
func (p ProfileId) ID() string { return p.id }

// Version returns the version.
func (p ProfileId) Version() uint32 { return p.version }

// MaterializationStyleId is a versioned format-owned materialization style
// identifier (materialization.rs:11-39).
type MaterializationStyleId struct {
	id      string
	version uint32
}

// NewMaterializationStyleId creates a versioned style identifier.
func NewMaterializationStyleId(id string, version uint32) MaterializationStyleId {
	return MaterializationStyleId{id: id, version: version}
}

// ID returns the namespaced style ID without the version suffix.
func (s MaterializationStyleId) ID() string { return s.id }

// Version returns the immutable style version.
func (s MaterializationStyleId) Version() uint32 { return s.version }

// NewlinePolicy is the explicit output newline policy (materialization.rs:
// 41-62).
type NewlinePolicy string

// The three frozen newline policies.
const (
	// NewlineNone emits no final or layout newline; only supported by
	// compact profiles.
	NewlineNone NewlinePolicy = "None"
	// NewlineLf emits ASCII LF.
	NewlineLf NewlinePolicy = "Lf"
	// NewlineCrLf emits ASCII CR followed by LF.
	NewlineCrLf NewlinePolicy = "CrLf"
)

// Bytes returns the exact selected newline bytes.
func (p NewlinePolicy) Bytes() []byte {
	switch p {
	case NewlineLf:
		return []byte("\n")
	case NewlineCrLf:
		return []byte("\r\n")
	}
	return nil
}

// MappingPolicy is the explicit treatment of ordered mappings at
// object-only targets (materialization.rs:64-72).
type MappingPolicy string

// The two frozen ordered-mapping policies.
const (
	// MappingPolicyRequireObject requires a native PortableValue Object.
	MappingPolicyRequireObject MappingPolicy = "RequireObject"
	// MappingPolicyUniqueStringEntriesToObject permits a unique String-key
	// EntryMapping to become an Object and reports the transformation.
	MappingPolicyUniqueStringEntriesToObject MappingPolicy = "UniqueStringEntriesToObject"
)

// RepresentabilityPolicy is the closed v1 representability policy
// (materialization.rs:73-79).
type RepresentabilityPolicy string

// The frozen v1 representability policy.
const (
	// RepresentabilityPolicyExactOnly rejects every value that cannot
	// round-trip through the target's exact projection contract.
	RepresentabilityPolicyExactOnly RepresentabilityPolicy = "ExactOnly"
)
