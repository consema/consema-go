package toml

import (
	"fmt"
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// ProjectionTarget is the versioned TOML projection target contract
// (consema-toml projection.rs). The unexported field makes the set
// closed.
type ProjectionTarget struct {
	name string
}

// ProjectionTargetBestExactCoreV1 is the frozen exact-first TOML-to-core
// mapping (`toml.best-exact-core@1`, RFC 0001 §5).
var ProjectionTargetBestExactCoreV1 = ProjectionTarget{name: "BestExactCore@1"}

// String returns the stable target spelling.
func (t ProjectionTarget) String() string { return t.name }

// ProjectionLimits are the projection resource limits (consema-toml
// projection.rs).
type ProjectionLimits struct {
	// MaxValueNodes is the maximum produced PortableValue nodes.
	MaxValueNodes int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceEntries is the maximum provenance locations and origins
	// combined.
	MaxProvenanceEntries int
	// MaxDepth is the maximum recursive container depth.
	MaxDepth int
}

// DefaultProjectionLimits returns the frozen defaults (1M value nodes,
// 100k report entries, 2M provenance entries, depth 256).
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxValueNodes:        1_000_000,
		MaxReportEntries:     100_000,
		MaxProvenanceEntries: 2_000_000,
		MaxDepth:             256,
	}
}

// ProjectionRequest is the immutable explicit projection request
// (consema-toml projection.rs).
type ProjectionRequest struct {
	target ProjectionTarget
	limits ProjectionLimits
}

// NewProjectionRequest creates an explicit request with default resource
// limits.
func NewProjectionRequest(target ProjectionTarget) ProjectionRequest {
	return ProjectionRequest{target: target, limits: DefaultProjectionLimits()}
}

// WithLimits replaces the immutable resource limits.
func (r ProjectionRequest) WithLimits(limits ProjectionLimits) ProjectionRequest {
	r.limits = limits
	return r
}

// Target returns the frozen target contract.
func (r ProjectionRequest) Target() ProjectionTarget { return r.target }

// Limits returns the projection resource limits.
func (r ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// Fidelity is the projection fidelity classification (consema-toml
// projection.rs).
type Fidelity string

// The three frozen fidelity levels.
const (
	// FidelityExact means the target directly and completely represents
	// TOML value semantics.
	FidelityExact Fidelity = "Exact"
	// FidelityTransformed means complete semantics survive an explicit
	// reversible re-encoding.
	FidelityTransformed Fidelity = "Transformed"
	// FidelityLossy means at least one source fact cannot be recovered.
	FidelityLossy Fidelity = "Lossy"
)

// ProjectedLocation is a projected value or association location
// (consema-toml projection.rs).
type ProjectedLocation struct {
	// Path is the portable value location of Value locations.
	Path protocol.ValuePath
	// Association is the portable association location of Association
	// locations.
	Association *protocol.AssociationLocation
	// Kind is "Value" or "Association".
	Kind string
}

// ProvenanceRelation is the source-to-projection relation
// (consema-toml projection.rs).
type ProvenanceRelation string

// The two frozen relations.
const (
	// ProvenanceRelationDirect is a direct native semantic origin.
	ProvenanceRelationDirect ProvenanceRelation = "Direct"
	// ProvenanceRelationDerived is derived without a one-to-one literal
	// origin.
	ProvenanceRelationDerived ProvenanceRelation = "Derived"
)

// SourceOrigin is one exact source origin (consema-toml projection.rs).
type SourceOrigin struct {
	// Snapshot is the source document snapshot.
	Snapshot document.SnapshotIdentity
	// Node is the exact structural identity.
	Node document.NodeRef
	// Span is the exact source range.
	Span document.Span
	// Relation is the source relation.
	Relation ProvenanceRelation
}

// ProvenanceEntry is one many-valued provenance mapping entry
// (consema-toml projection.rs).
type ProvenanceEntry struct {
	// Projected is the projected value or association.
	Projected ProjectedLocation
	// Origins are the one or more exact source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable multi-map from projected locations to
// source origins (consema-toml projection.rs).
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically generated entries.
func (m ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionReport is the complete ordered projection report
// (consema-toml projection.rs). Exact TOML 1.0 projections emit no
// transformation or loss events.
type ProjectionReport struct {
	events []*protocol.Diagnostic
}

// Events returns the ordered structured transformation/loss diagnostics.
func (r ProjectionReport) Events() []*protocol.Diagnostic {
	return append([]*protocol.Diagnostic(nil), r.events...)
}

// CompleteProjection is the complete successful projection; its value is
// never partial (consema-toml projection.rs).
type CompleteProjection struct {
	// Value is the complete immutable public value.
	Value core.Value
	// Fidelity is the worst fidelity of the whole operation.
	Fidelity Fidelity
	// Report is the machine-readable transformation/loss report.
	Report ProjectionReport
	// Provenance is the value and object-association provenance.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is the failed attempt without a partial
// PortableValue (consema-toml projection.rs).
type FailedProjectionAttempt struct {
	// Diagnostics are the ordered diagnostics explaining the failure.
	Diagnostics []*protocol.Diagnostic
	// Report holds the events discovered before the failed completion
	// check.
	Report ProjectionReport
	// PartialAnalysis are the stable paths locally analyzed before
	// failure; the TOML projection always fails atomically.
	PartialAnalysis []string
}

// ProjectionResult is the projection completion algebra (consema-toml
// projection.rs). Exactly one outcome is non-nil.
type ProjectionResult struct {
	// Complete is the complete success outcome.
	Complete *CompleteProjection
	// Failed is the failed attempt with no value.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind classifies a stable projection failure
// (consema-toml projection.rs).
type ProjectionFailureKind uint8

// The three stable projection failure classes.
const (
	// ProjectionFailureUnrepresentableDateTime: TOML temporal fields are
	// outside PortableValue v1.
	ProjectionFailureUnrepresentableDateTime ProjectionFailureKind = iota
	// ProjectionFailureResourceLimit: a declared resource limit was
	// reached.
	ProjectionFailureResourceLimit
	// ProjectionFailureCoreInvariant: a valid TOML table violated the core
	// unique-key invariant.
	ProjectionFailureCoreInvariant
)

// ProjectionFailure is the internal projection failure; it carries the
// frozen diagnostic code (RFC 0016 §6).
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Name is the stable limit name of ResourceLimit failures.
	Name string
}

// Code returns the frozen registered code.
func (f *ProjectionFailure) Code() string {
	switch f.Kind {
	case ProjectionFailureUnrepresentableDateTime:
		return "toml.projection.unrepresentable-datetime@1"
	case ProjectionFailureResourceLimit:
		return "core.projection.resource-limit@1"
	case ProjectionFailureCoreInvariant:
		return "toml.projection.core-invariant@1"
	}
	return "toml.projection.core-invariant@1"
}

// Error implements error; the text is human presentation only.
func (f *ProjectionFailure) Error() string {
	switch f.Kind {
	case ProjectionFailureUnrepresentableDateTime:
		return "toml: TOML temporal value is not exactly representable"
	case ProjectionFailureResourceLimit:
		return "toml: projection resource limit " + f.Name + " was reached"
	case ProjectionFailureCoreInvariant:
		return "toml: TOML table violated the core unique-key invariant"
	}
	return "toml: projection failed"
}

// Project applies one immutable explicit projection request
// (consema-toml projection.rs). A failure never returns a partial
// value.
func (d *Document) Project(request ProjectionRequest) ProjectionResult {
	context := projectionContext{
		document: d,
		limits:   request.limits,
	}
	value, failure := context.projectItem(d.root, protocol.RootValuePath(), 0)
	if failure != nil {
		diagnostic := projectionFailureDiagnostic(d, failure)
		return ProjectionResult{Failed: &FailedProjectionAttempt{
			Diagnostics: []*protocol.Diagnostic{diagnostic},
		}}
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value:      value,
		Fidelity:   FidelityExact,
		Provenance: context.provenance,
	}}
}

type projectionContext struct {
	document        *Document
	limits          ProjectionLimits
	valueNodes      int
	provenanceUnits int
	provenance      ProvenanceMap
}

// projectItem mirrors the Rust project_item (consema-toml
// projection.rs).
func (c *projectionContext) projectItem(index int, path protocol.ValuePath,
	depth int) (core.Value, *ProjectionFailure) {
	if depth > c.limits.MaxDepth {
		return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit, Name: "max_depth"}
	}
	c.valueNodes++
	if c.valueNodes > c.limits.MaxValueNodes {
		return nil, &ProjectionFailure{Kind: ProjectionFailureResourceLimit, Name: "max_value_nodes"}
	}
	item := &c.document.entities[index].item
	var value core.Value
	switch item.kind {
	case itemString:
		value = core.String(item.str)
	case itemInteger:
		value = core.NewInteger(big.NewInt(item.integer))
	case itemFloat:
		value = core.NewBinaryFloat64(item.bits)
	case itemBoolean:
		value = core.Boolean(item.boolean)
	case itemDateTime:
		projected, err := projectDateTime(item.dateTime)
		if err != nil {
			return nil, err
		}
		value = projected
	case itemArray, itemArrayOfTables:
		items := make([]core.Value, 0, len(item.elements))
		for ordinal, elementIndex := range item.elements {
			element := c.document.entities[elementIndex].element
			childPath := path.Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(ordinal)})
			child, failure := c.projectItem(element.item, childPath, depth+1)
			if failure != nil {
				return nil, failure
			}
			items = append(items, child)
			if failure := c.addOrigin(ProjectedLocation{Kind: "Value", Path: childPath},
				elementIndex, document.RoleTomlArrayElement, ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
		}
		value = core.NewArray(items...)
	case itemInlineTable, itemTable:
		entries := item.entries
		builder := core.NewObjectBuilder()
		for ordinal, entryIndex := range entries {
			entry := c.document.entities[entryIndex].entry
			keyName := c.document.entities[entry.key].key.name
			childPath := path.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: keyName})
			child, failure := c.projectItem(entry.item, childPath, depth+1)
			if failure != nil {
				return nil, failure
			}
			if err := builder.Insert(keyName, child); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
			association := protocol.NewAssociationLocation(path, uint64(ordinal),
				protocol.AssociationRoleObjectEntry)
			if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &association},
				entryIndex, document.RoleTomlEntry, ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			keyAssociation := protocol.NewAssociationLocation(path, uint64(ordinal),
				protocol.AssociationRoleObjectKey)
			if failure := c.addOrigin(ProjectedLocation{Kind: "Association", Association: &keyAssociation},
				entry.key, document.RoleTomlKey, ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
		}
		value = builder.Build()
	default:
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if failure := c.addOrigin(ProjectedLocation{Kind: "Value", Path: path},
		index, document.RoleTomlItem, ProvenanceRelationDirect); failure != nil {
		return nil, failure
	}
	return value, nil
}

func (c *projectionContext) addOrigin(projected ProjectedLocation, index int,
	role document.NodeRole, relation ProvenanceRelation) *ProjectionFailure {
	c.provenanceUnits++
	if c.provenanceUnits > c.limits.MaxProvenanceEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, Name: "max_provenance_entries"}
	}
	origin := SourceOrigin{
		Snapshot: c.document.SnapshotIdentity(),
		Node:     c.document.nodeRef(index, role),
		Span:     c.document.entities[index].span,
		Relation: relation,
	}
	for position := range c.provenance.entries {
		entry := &c.provenance.entries[position]
		if sameProjectedLocation(entry.Projected, projected) {
			entry.Origins = append(entry.Origins, origin)
			return nil
		}
	}
	c.provenance.entries = append(c.provenance.entries, ProvenanceEntry{
		Projected: projected,
		Origins:   []SourceOrigin{origin},
	})
	return nil
}

func sameProjectedLocation(left, right ProjectedLocation) bool {
	if left.Kind != right.Kind {
		return false
	}
	if left.Kind == "Value" {
		return left.Path.Equal(right.Path)
	}
	if left.Association == nil || right.Association == nil {
		return left.Association == right.Association
	}
	return left.Association.Equal(*right.Association)
}

// projectDateTime maps one TOML temporal datum onto the PortableValue v1
// closure (consema-toml projection.rs; RFC 0001 §5).
func projectDateTime(value TomlDateTime) (core.Value, *ProjectionFailure) {
	unrepresentable := &ProjectionFailure{Kind: ProjectionFailureUnrepresentableDateTime}
	switch {
	case value.Date != nil && value.Time == nil && value.Offset == nil:
		date, err := core.NewDate(big.NewInt(int64(value.Date.Year)), value.Date.Month, value.Date.Day)
		if err != nil {
			return nil, unrepresentable
		}
		return date, nil
	case value.Date == nil && value.Time != nil && value.Offset == nil:
		time, err := coreTimeOf(*value.Time)
		if err != nil {
			return nil, unrepresentable
		}
		return time, nil
	case value.Date != nil && value.Time != nil && value.Offset == nil:
		date, err := core.NewDate(big.NewInt(int64(value.Date.Year)), value.Date.Month, value.Date.Day)
		if err != nil {
			return nil, unrepresentable
		}
		time, err := coreTimeOf(*value.Time)
		if err != nil {
			return nil, unrepresentable
		}
		return core.NewLocalDateTime(date, time), nil
	case value.Date != nil && value.Time != nil && value.Offset != nil:
		date, err := core.NewDate(big.NewInt(int64(value.Date.Year)), value.Date.Month, value.Date.Day)
		if err != nil {
			return nil, unrepresentable
		}
		time, err := coreTimeOf(*value.Time)
		if err != nil {
			return nil, unrepresentable
		}
		offsetSeconds := int32(0)
		if !value.Offset.Z {
			offsetSeconds = int32(value.Offset.Minutes) * 60
		}
		offset, err := core.NewOffsetDateTime(core.NewLocalDateTime(date, time), offsetSeconds)
		if err != nil {
			return nil, unrepresentable
		}
		return offset, nil
	}
	return nil, unrepresentable
}

func coreTimeOf(value TomlTime) (core.Time, error) {
	fraction := core.NewDecimal(big.NewInt(int64(value.Nanosecond)), big.NewInt(-9))
	return core.NewTime(value.Hour, value.Minute, value.Second, fraction)
}

// projectionFailureDiagnostic maps the failure onto the frozen diagnostic
// (consema-toml projection.rs).
func projectionFailureDiagnostic(document *Document, failure *ProjectionFailure) *protocol.Diagnostic {
	var code string
	var category protocol.DiagnosticCategory
	var primary *protocol.SourceLocation
	switch failure.Kind {
	case ProjectionFailureUnrepresentableDateTime:
		code = "toml.projection.unrepresentable-datetime@1"
		category = protocol.CategoryProjection
		rootSpan := document.entities[document.root].span
		primary = &protocol.SourceLocation{StartByte: uint64(rootSpan.StartByte()), EndByte: uint64(rootSpan.EndByte())}
	case ProjectionFailureResourceLimit:
		code = "core.projection.resource-limit@1"
		category = protocol.CategoryResource
	case ProjectionFailureCoreInvariant:
		code = "toml.projection.core-invariant@1"
		category = protocol.CategoryProjection
	}
	arguments := map[string]string(nil)
	if failure.Kind == ProjectionFailureResourceLimit {
		arguments = map[string]string{"limit": failure.Name}
	}
	diagnostic, err := protocol.NewDiagnostic(code, category, protocol.SeverityError, primary,
		nil, arguments, nil, nil, 0, protocol.NewErrorCodeRegistry(protocol.ErrorRegistryV7))
	if err != nil {
		panic("toml: unregistered projection code " + fmt.Sprint(code))
	}
	return diagnostic
}
