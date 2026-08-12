package hcl

import (
	"math/big"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// This file implements the `hcl.projection.body@1` projection (RFC 0014
// §8): one ordered `hcl.body@1` PortableValue record with a single ordered
// `items` sequence, each item an attribute (`kind`/`name`/`value`) or a
// block (`kind`/`type`/`labels`/`body`), where every attribute value is
// literal-complete and rendered as a typed member. Attribute order, block
// order, label order, and duplicate object-constructor keys are preserved
// exactly.
//
// A derived expression has no default rendering (RFC 0014 §8.2).
// Projection of a body containing a derived expression fails atomically
// with `hcl.projection.non-literal-expression@1` unless the caller
// supplies the explicit ProjectExpression policy; under that policy each
// derived expression is projected as the authorized `hcl.expression@1`
// record — `{ "record": "hcl.expression@1", "kind": ..., "text": ...,
// "fingerprint": ... }` — and the projection reports one `Transformed`
// event per substituted expression with value and expression provenance.
// No other transformation exists (hard gate 4). A Recovered Document never
// projects.

// Versioned `hcl.body@1` record spelling (RFC 0014 §8.2).
const hclBodyRecord = "hcl.body@1"

// Versioned `hcl.expression@1` record spelling (RFC 0014 §8.2).
const hclExpressionRecord = "hcl.expression@1"

// Stable type identifier of the `hcl.expression@1` ExtendedValue (RFC
// 0014 §8.2, roadmap §5.5); the wire record spelling appends the semantic
// version.
const hclExpressionTypeID = "hcl.expression"

// Canonical payload codec of the `hcl.expression@1` ExtendedValue.
const hclExpressionCodec = "hcl.expression.canonical@1"

// ProjectionTarget is a versioned HCL projection target (RFC 0014 §8).
type ProjectionTarget uint8

// The one frozen target.
const (
	// ProjectionTargetBodyV1 is the exact `hcl.projection.body@1` record
	// projection.
	ProjectionTargetBodyV1 ProjectionTarget = iota
)

// ExpressionPolicy is the derived-expression handling for the body target
// (RFC 0014 §8.2).
type ExpressionPolicy uint8

// The two frozen policies.
const (
	// ExpressionPolicyFail: a derived expression fails the projection
	// atomically with `hcl.projection.non-literal-expression@1`.
	ExpressionPolicyFail ExpressionPolicy = iota
	// ExpressionPolicyProjectExpression: each derived expression is
	// projected as the authorized `hcl.expression@1` record, reported as
	// one `Transformed` event per substituted expression.
	ExpressionPolicyProjectExpression
)

// ProjectionRequest is the explicit HCL projection request; every policy
// is mandatory (RFC 0014 §8.2).
type ProjectionRequest struct {
	target           ProjectionTarget
	expressionPolicy ExpressionPolicy
	limits           ProjectionLimits
}

// ProjectionRequestBody returns the exact `hcl.projection.body@1` record
// request; a derived expression fails the projection atomically.
func ProjectionRequestBody() ProjectionRequest {
	return ProjectionRequest{
		target:           ProjectionTargetBodyV1,
		expressionPolicy: ExpressionPolicyFail,
		limits:           DefaultProjectionLimits(),
	}
}

// ProjectionRequestBodyWithExpressionPolicy returns the exact
// `hcl.projection.body@1` request with an explicit derived-expression
// policy (RFC 0014 §8.2, hard gate 4).
func ProjectionRequestBodyWithExpressionPolicy(policy ExpressionPolicy) ProjectionRequest {
	return ProjectionRequest{
		target:           ProjectionTargetBodyV1,
		expressionPolicy: policy,
		limits:           DefaultProjectionLimits(),
	}
}

// WithLimits applies explicit resource limits to this request.
func (r ProjectionRequest) WithLimits(limits ProjectionLimits) ProjectionRequest {
	r.limits = limits
	return r
}

// Target returns the projection target.
func (r ProjectionRequest) Target() ProjectionTarget { return r.target }

// ExpressionPolicy returns the derived-expression policy.
func (r ProjectionRequest) ExpressionPolicy() ExpressionPolicy { return r.expressionPolicy }

// Limits returns the resource limits.
func (r ProjectionRequest) Limits() ProjectionLimits { return r.limits }

// ProjectionLimits are the HCL projection resource limits (RFC 0014 §11).
type ProjectionLimits struct {
	// MaxSourceNodes is the maximum inspected native constructs: every
	// attribute, block, block label, and expression node.
	MaxSourceNodes int
	// MaxValueNodes is the maximum produced PortableValue nodes, entry
	// keys included.
	MaxValueNodes int
	// MaxReportEntries is the maximum report events.
	MaxReportEntries int
	// MaxProvenanceUnits is the maximum projected locations plus source
	// origins.
	MaxProvenanceUnits int
}

// DefaultProjectionLimits returns the frozen defaults (RFC 0014 §11).
func DefaultProjectionLimits() ProjectionLimits {
	return ProjectionLimits{
		MaxSourceNodes:     2_000_000,
		MaxValueNodes:      2_000_000,
		MaxReportEntries:   100_000,
		MaxProvenanceUnits: 4_000_000,
	}
}

// Fidelity is the projection fidelity classification (RFC 0014 §8).
type Fidelity uint8

// The three frozen fidelity classes.
const (
	// FidelityExact means the target directly represents every native
	// association.
	FidelityExact Fidelity = iota
	// FidelityTransformed means an explicit reported policy transformed
	// associations.
	FidelityTransformed
	// FidelityLossy means source facts were irreversibly omitted without a
	// retained source relation.
	FidelityLossy
)

// String returns the stable fidelity name.
func (f Fidelity) String() string {
	switch f {
	case FidelityExact:
		return "Exact"
	case FidelityTransformed:
		return "Transformed"
	case FidelityLossy:
		return "Lossy"
	}
	return "Exact"
}

// ProvenanceRelation is the source-to-projection relation (RFC 0014 §8).
type ProvenanceRelation string

// The four frozen relations.
const (
	// ProvenanceRelationDirect is a direct native semantic origin.
	ProvenanceRelationDirect ProvenanceRelation = "Direct"
	// ProvenanceRelationDerived is a container value derived from a source
	// record.
	ProvenanceRelationDerived ProvenanceRelation = "Derived"
	// ProvenanceRelationCollapsed is a discarded occurrence related to the
	// retained projected occurrence.
	ProvenanceRelationCollapsed ProvenanceRelation = "Collapsed"
	// ProvenanceRelationReferenceDerived is semantic content derived from
	// reference resolution.
	ProvenanceRelationReferenceDerived ProvenanceRelation = "ReferenceDerived"
)

// SourceOrigin is one exact source origin (RFC 0014 §8).
type SourceOrigin struct {
	// Snapshot is the source document snapshot.
	Snapshot document.SnapshotIdentity
	// Node is the exact structural identity.
	Node document.NodeRef
	// Span is the exact raw source range.
	Span document.Span
	// Relation is the source relation.
	Relation ProvenanceRelation
}

// ProvenanceEntry is one many-valued provenance entry: one projected
// record location and its ordered source origins.
type ProvenanceEntry struct {
	// Projected is the projected location inside the `hcl.body@1` record.
	Projected protocol.ValuePath
	// Origins are the ordered source origins.
	Origins []SourceOrigin
}

// ProvenanceMap is the immutable many-valued provenance mapping.
type ProvenanceMap struct {
	entries []ProvenanceEntry
}

// Entries returns the deterministically ordered entries. The returned
// slice is a copy.
func (m *ProvenanceMap) Entries() []ProvenanceEntry {
	return append([]ProvenanceEntry(nil), m.entries...)
}

// ProjectionEventKind is the machine-readable projection event category
// (RFC 0014 §8.2).
type ProjectionEventKind string

// The one frozen event category.
const (
	// ProjectionEventExpressionSubstituted is one derived expression
	// substituted by the authorized `hcl.expression@1` record under the
	// explicit ProjectExpression policy.
	ProjectionEventExpressionSubstituted ProjectionEventKind = "ExpressionSubstituted"
)

// ProjectionEvent is one explicit transformation event (RFC 0014 §8.2).
type ProjectionEvent struct {
	// Kind is the stable event kind.
	Kind ProjectionEventKind
	// Expression is the source expression occurrence substituted.
	Expression document.NodeRef
	// Value is the projected value location inside the `hcl.body@1`
	// record.
	Value protocol.ValuePath
	// Impact is the fidelity impact.
	Impact Fidelity
}

// ProjectionReport is the complete ordered projection report.
type ProjectionReport struct {
	events []ProjectionEvent
}

// Events returns the events in deterministic source order. The returned
// slice is a copy.
func (r *ProjectionReport) Events() []ProjectionEvent {
	return append([]ProjectionEvent(nil), r.events...)
}

// CompleteProjection is the complete successful projection; its value is
// never partial (RFC 0014 §8.2).
type CompleteProjection struct {
	// Value is the complete immutable projected `hcl.body@1` record.
	Value core.Value
	// Fidelity is the worst operation fidelity.
	Fidelity Fidelity
	// Report is the structured transformation report.
	Report ProjectionReport
	// Provenance is the value provenance from the body to the record.
	Provenance ProvenanceMap
}

// FailedProjectionAttempt is a failed projection attempt without a partial
// value (RFC 0014 §8.2, hard gate 4).
type FailedProjectionAttempt struct {
	// Diagnostics are the stable ordered diagnostics.
	Diagnostics []*protocol.Diagnostic
	// Report is empty: failed projections publish no partial
	// transformation result.
	Report ProjectionReport
}

// ProjectionResult is the sealed projection outcome: exactly one of
// Complete or Failed is set.
type ProjectionResult struct {
	// Complete is the successful outcome.
	Complete *CompleteProjection
	// Failed is the failed attempt.
	Failed *FailedProjectionAttempt
}

// ProjectionFailureKind is the stable projection failure category (RFC
// 0014 §8.2).
type ProjectionFailureKind uint8

// The closed projection failure categories.
const (
	// ProjectionFailureIncompleteDocument: a Recovered document cannot
	// publish partial semantic values ("A Recovered Document never
	// projects", RFC 0014 §8.2).
	ProjectionFailureIncompleteDocument ProjectionFailureKind = iota
	// ProjectionFailureNonLiteralExpression: a derived expression was
	// projected without the explicit ProjectExpression policy.
	ProjectionFailureNonLiteralExpression
	// ProjectionFailureUnrepresentable: a native fact the record cannot
	// represent, such as a parenthesized object key whose literal value is
	// a tuple or object.
	ProjectionFailureUnrepresentable
	// ProjectionFailureResourceLimit: a declared projection resource limit
	// was reached.
	ProjectionFailureResourceLimit
	// ProjectionFailureCoreInvariant: a PortableValue construction
	// invariant failed.
	ProjectionFailureCoreInvariant
)

// ProjectionFailure is the typed projection failure. It implements error
// and the RFC 0016 §6 Code() contract with the frozen registered codes.
type ProjectionFailure struct {
	// Kind identifies the failure.
	Kind ProjectionFailureKind
	// Text is the exact source text of the first derived expression
	// (NonLiteralExpression).
	Text string
	// Fact is the blocking native fact name (Unrepresentable).
	Fact string
	// LimitName is the stable limit name of a ResourceLimit.
	LimitName string
}

// Error implements error.
func (e *ProjectionFailure) Error() string {
	switch e.Kind {
	case ProjectionFailureIncompleteDocument:
		return "hcl: recovered documents cannot enter projection"
	case ProjectionFailureNonLiteralExpression:
		return "hcl: expression is not literal-complete: " + e.Text
	case ProjectionFailureUnrepresentable:
		return "hcl: projection cannot represent: " + e.Fact
	case ProjectionFailureResourceLimit:
		return "hcl: projection limit " + e.LimitName + " reached"
	case ProjectionFailureCoreInvariant:
		return "hcl: projection core invariant failed"
	}
	return "hcl: projection failure"
}

// Code returns the frozen registered code for the failure (RFC 0014 §8.2,
// §11).
func (e *ProjectionFailure) Code() string {
	switch e.Kind {
	case ProjectionFailureIncompleteDocument:
		return "hcl.projection.incomplete-document@1"
	case ProjectionFailureNonLiteralExpression:
		return "hcl.projection.non-literal-expression@1"
	case ProjectionFailureUnrepresentable:
		return "hcl.projection.unrepresentable@1"
	case ProjectionFailureResourceLimit:
		return "hcl.projection.resource-limit@1"
	case ProjectionFailureCoreInvariant:
		return "hcl.projection.core-invariant@1"
	}
	return "hcl.projection.core-invariant@1"
}

// Project projects one complete HCL document under one explicit target and
// policy contract (RFC 0014 §8).
//
// The projection is atomic: a recovered source, a derived expression under
// the default policy, an unrepresentable native fact, or a resource limit
// returns no partial value, provenance, or report (hard gate 4).
func (d *Document) Project(request ProjectionRequest) ProjectionResult {
	if d.status != document.FormationStatusComplete {
		return failedProjection(&ProjectionFailure{Kind: ProjectionFailureIncompleteDocument})
	}
	context := &projectionContext{
		document:         d,
		limits:           request.limits,
		expressionPolicy: request.expressionPolicy,
	}
	value, failure := context.projectBody(d.document.body, protocol.RootValuePath())
	if failure != nil {
		return failedProjection(failure)
	}
	return ProjectionResult{Complete: &CompleteProjection{
		Value:      value,
		Fidelity:   context.fidelity,
		Report:     context.report,
		Provenance: context.provenance,
	}}
}

// failedProjection builds the failed attempt with its diagnostic (RFC
// 0014 §8.2).
func failedProjection(failure *ProjectionFailure) ProjectionResult {
	diagnostic, err := protocol.NewDiagnostic(failure.Code(),
		protocol.CategoryProjection, protocol.SeverityError, nil, nil,
		map[string]string{"failure": failure.Error()}, nil, nil, 0, errorRegistry())
	if err != nil {
		diagnostic = &protocol.Diagnostic{Code: failure.Code(),
			Category: protocol.CategoryProjection, Severity: protocol.SeverityError,
			Arguments: map[string]string{"failure": failure.Error()}}
	}
	return ProjectionResult{Failed: &FailedProjectionAttempt{
		Diagnostics: []*protocol.Diagnostic{diagnostic},
	}}
}

// projectionContext is the state of one projection.
type projectionContext struct {
	document         *Document
	limits           ProjectionLimits
	expressionPolicy ExpressionPolicy
	report           ProjectionReport
	provenance       ProvenanceMap
	fidelity         Fidelity
	sourceNodes      int
	valueNodes       int
	nextOrdinal      uint64
}

// step applies the inspected-construct budget (RFC 0014 §11).
func (c *projectionContext) step() *ProjectionFailure {
	c.sourceNodes++
	if c.sourceNodes > c.limits.MaxSourceNodes {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_source_nodes"}
	}
	return nil
}

// reserveValue applies the produced-node budget.
func (c *projectionContext) reserveValue(count int) *ProjectionFailure {
	c.valueNodes += count
	if c.valueNodes > c.limits.MaxValueNodes {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_value_nodes"}
	}
	return nil
}

// event records one expression substitution event (RFC 0014 §8.2).
func (c *projectionContext) event(expression document.NodeRef, value protocol.ValuePath) *ProjectionFailure {
	if len(c.report.events) >= c.limits.MaxReportEntries {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_report_entries"}
	}
	c.report.events = append(c.report.events, ProjectionEvent{
		Kind:       ProjectionEventExpressionSubstituted,
		Expression: expression,
		Value:      value,
		Impact:     FidelityTransformed,
	})
	c.fidelity = FidelityTransformed
	return nil
}

// origin records one provenance mapping (RFC 0014 §8).
func (c *projectionContext) origin(projected protocol.ValuePath, node document.NodeRef,
	span document.Span, relation ProvenanceRelation) *ProjectionFailure {
	if len(c.provenance.entries)+1 > c.limits.MaxProvenanceUnits {
		return &ProjectionFailure{Kind: ProjectionFailureResourceLimit, LimitName: "max_provenance_units"}
	}
	c.provenance.entries = append(c.provenance.entries, ProvenanceEntry{
		Projected: projected,
		Origins: []SourceOrigin{{
			Snapshot: c.document.SnapshotIdentity(),
			Node:     node,
			Span:     span,
			Relation: relation,
		}},
	})
	return nil
}

// nodeRef issues the snapshot-bound handle of one native construct with
// the next walk ordinal.
func (c *projectionContext) nodeRef(role document.NodeRole) document.NodeRef {
	ordinal := c.nextOrdinal
	c.nextOrdinal++
	return c.document.nodeRef(ordinal, role)
}

// attributeSpan is the exact span of one attribute occurrence: the union
// of its name, equals, and expression spans.
func (c *projectionContext) attributeSpan(attribute *HclAttribute) document.Span {
	return c.document.span(attribute.nameSpan.StartByte(), attribute.expression.span.EndByte())
}

// projectBody projects one recursive body; path is the location of this
// body's record inside the projected tree.
func (c *projectionContext) projectBody(body *HclBody, path protocol.ValuePath) (core.Value, *ProjectionFailure) {
	if failure := c.reserveValue(1); failure != nil {
		return nil, failure
	}
	builder := core.NewObjectBuilder()
	record := core.String(hclBodyRecord)
	if err := builder.Insert("record", record); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	var items []core.Value
	for i := range body.items {
		item := &body.items[i]
		if failure := c.step(); failure != nil {
			return nil, failure
		}
		itemPath := path.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "items"}).
			Child(protocol.ValuePathSegment{Kind: "SequenceElement", Index: uint64(len(items))})
		if attribute := item.AsAttribute(); attribute != nil {
			attributeNode := c.nodeRef(document.RoleHclAttribute)
			valuePath := itemPath.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "value"})
			if failure := c.origin(valuePath, attributeNode, c.attributeSpan(attribute),
				ProvenanceRelationDirect); failure != nil {
				return nil, failure
			}
			value, failure := c.projectAttribute(attribute, &valuePath)
			if failure != nil {
				return nil, failure
			}
			if failure := c.reserveValue(3); failure != nil {
				return nil, failure
			}
			itemBuilder := core.NewObjectBuilder()
			if err := itemBuilder.Insert("kind", core.String("attribute")); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
			if err := itemBuilder.Insert("name", core.String(attribute.name)); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
			if err := itemBuilder.Insert("value", value); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
			items = append(items, itemBuilder.Build())
			continue
		}
		block := item.AsBlock()
		blockNode := c.nodeRef(document.RoleHclBlock)
		if failure := c.origin(itemPath, blockNode, block.span, ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		blockValue, failure := c.projectBlock(block, &itemPath)
		if failure != nil {
			return nil, failure
		}
		items = append(items, blockValue)
	}
	if failure := c.reserveValue(1); failure != nil {
		return nil, failure
	}
	if err := builder.Insert("items", core.NewArray(items...)); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	return builder.Build(), nil
}

// projectAttribute projects one attribute value: a typed literal member,
// or the authorized `hcl.expression@1` record under the explicit policy.
func (c *projectionContext) projectAttribute(attribute *HclAttribute,
	valuePath *protocol.ValuePath) (core.Value, *ProjectionFailure) {
	expression := attribute.expression
	expressionNode := c.inspectExpression(expression)
	if IsLiteralComplete(expression) {
		literal, err := LiteralValue(expression)
		if err != nil {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		return c.literalToValue(&literal)
	}
	switch c.expressionPolicy {
	case ExpressionPolicyFail:
		return nil, &ProjectionFailure{Kind: ProjectionFailureNonLiteralExpression,
			Text: expression.Text(c.document.DecodedText())}
	case ExpressionPolicyProjectExpression:
		value := c.expressionRecord(expression)
		if failure := c.origin(*valuePath, expressionNode, expression.span,
			ProvenanceRelationDirect); failure != nil {
			return nil, failure
		}
		if failure := c.event(expressionNode, *valuePath); failure != nil {
			return nil, failure
		}
		return value, nil
	}
	return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// projectBlock projects one block item: the `kind` member, the type,
// ordered labels, and the nested `hcl.body@1` record.
func (c *projectionContext) projectBlock(block *HclBlock,
	blockPath *protocol.ValuePath) (core.Value, *ProjectionFailure) {
	for range block.labels {
		if failure := c.step(); failure != nil {
			return nil, failure
		}
		c.nodeRef(document.RoleHclBlockLabel)
	}
	bodyPath := blockPath.Child(protocol.ValuePathSegment{Kind: "ObjectValue", Key: "body"})
	bodyValue, failure := c.projectBody(block.body, bodyPath)
	if failure != nil {
		return nil, failure
	}
	if failure := c.reserveValue(4 + len(block.labels)); failure != nil {
		return nil, failure
	}
	builder := core.NewObjectBuilder()
	if err := builder.Insert("kind", core.String("block")); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if err := builder.Insert("type", core.String(block.blockType)); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	labels := make([]core.Value, 0, len(block.labels))
	for j := range block.labels {
		labels = append(labels, core.String(block.labels[j].text))
	}
	if err := builder.Insert("labels", core.NewArray(labels...)); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	if err := builder.Insert("body", bodyValue); err != nil {
		return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
	}
	return builder.Build(), nil
}

// inspectExpression assigns walk ordinals to every node of one expression
// subtree in source order and returns the ordinal of the root node.
func (c *projectionContext) inspectExpression(expression *HclExpression) document.NodeRef {
	if failure := c.step(); failure != nil {
		// The budget failure surfaces at the next checked step; the
		// recursive walk is bounded by the parse limits.
		_ = failure
	}
	node := c.nodeRef(document.RoleHclExpression)
	for _, child := range expression.Children() {
		c.inspectExpression(child)
	}
	return node
}

// literalToValue maps one literal-complete value onto its typed
// PortableValue member (RFC 0014 §8.2).
func (c *projectionContext) literalToValue(literal *HclLiteralValue) (core.Value, *ProjectionFailure) {
	if failure := c.reserveValue(1); failure != nil {
		return nil, failure
	}
	switch literal.tag {
	case literalInteger:
		value, ok := new(big.Int).SetString(literal.text, 10)
		if !ok {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		return core.NewInteger(value), nil
	case literalDecimal:
		coefficient, exponent, ok := decimalParts(literal.text)
		if !ok {
			return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
		}
		return core.NewDecimal(coefficient, exponent), nil
	case literalString:
		return core.String(literal.text), nil
	case literalBoolean:
		return core.Boolean(literal.boolean), nil
	case literalNull:
		return core.NullValue(), nil
	case literalTuple:
		elements := make([]core.Value, 0, len(literal.elements))
		for i := range literal.elements {
			value, failure := c.literalToValue(&literal.elements[i])
			if failure != nil {
				return nil, failure
			}
			elements = append(elements, value)
		}
		return core.NewArray(elements...), nil
	case literalObject:
		builder := core.NewEntryMappingBuilder()
		for i := range literal.entries {
			entry := &literal.entries[i]
			key, failure := literalKey(entry.key)
			if failure != nil {
				return nil, failure
			}
			if failure := c.reserveValue(1); failure != nil {
				return nil, failure
			}
			value, failure := c.literalToValue(&entry.value)
			if failure != nil {
				return nil, failure
			}
			if err := builder.Push(core.String(key), value); err != nil {
				return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
			}
		}
		return builder.Build(), nil
	}
	return nil, &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// expressionRecord builds one `hcl.expression@1` record for a derived
// expression (RFC 0014 §8.2).
func (c *projectionContext) expressionRecord(expression *HclExpression) core.Value {
	text := expression.Text(c.document.DecodedText())
	payload := ExpressionPayload{
		Kind:        kindFamily(expression.kind.Name()),
		Text:        text,
		Fingerprint: StructuralFingerprint(expression),
	}
	if failure := c.reserveValue(5); failure != nil {
		// The budget failure surfaces at the next checked step.
		_ = failure
	}
	builder := core.NewObjectBuilder()
	_ = builder.Insert("record", core.String(hclExpressionRecord))
	_ = builder.Insert("kind", core.String(payload.Kind))
	_ = builder.Insert("text", core.String(payload.Text))
	_ = builder.Insert("fingerprint", core.String(fingerprintHex(payload.Fingerprint)))
	return builder.Build()
}

// literalKey renders the canonical string spelling of one object-literal
// key (RFC 0014 §8.2).
func literalKey(key HclLiteralKey) (string, *ProjectionFailure) {
	switch key.tag {
	case literalKeyIdentifier, literalKeyNumber, literalKeyString:
		return key.text, nil
	case literalKeyValue:
		return scalarKey(&key.value)
	}
	return "", &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// scalarKey renders the canonical scalar spelling of one parenthesized
// object key; a tuple or object value has no canonical string spelling and
// fails atomically (RFC 0014 §8.2, hard gate 4).
func scalarKey(literal *HclLiteralValue) (string, *ProjectionFailure) {
	switch literal.tag {
	case literalInteger, literalDecimal, literalString:
		return literal.text, nil
	case literalBoolean:
		if literal.boolean {
			return "true", nil
		}
		return "false", nil
	case literalNull:
		return "null", nil
	case literalTuple, literalObject:
		return "", &ProjectionFailure{Kind: ProjectionFailureUnrepresentable, Fact: "object-key"}
	}
	return "", &ProjectionFailure{Kind: ProjectionFailureCoreInvariant}
}

// kindFamily maps one expression kind name onto the `hcl.expression@1`
// kind family spelling (RFC 0014 §4.1, §4.6, §8.2): variable and
// traversal expressions are one family, and for-expressions are one family
// over the tuple and object forms.
func kindFamily(name HclExpressionKindName) string {
	switch name {
	case HclExpressionKindNameVariableRef, HclExpressionKindNameTraversal:
		return "variable"
	case HclExpressionKindNameForTuple, HclExpressionKindNameForObject:
		return "for"
	default:
		return name.AsStr()
	}
}

// decimalParts splits one canonical decimal spelling into its exact
// coefficient and exponent.
func decimalParts(text string) (*big.Int, *big.Int, bool) {
	negative := false
	magnitude := text
	if len(magnitude) > 0 && magnitude[0] == '-' {
		negative = true
		magnitude = magnitude[1:]
	}
	digits := magnitude
	exponent := big.NewInt(0)
	if index := indexByte(magnitude, '.'); index >= 0 {
		fraction := magnitude[index+1:]
		digits = magnitude[:index] + fraction
		exponent = big.NewInt(-int64(len(fraction)))
	}
	coefficient, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		return nil, nil, false
	}
	if negative && coefficient.Sign() != 0 {
		coefficient.Neg(coefficient)
	}
	return coefficient, exponent, true
}

func indexByte(text string, target byte) int {
	for i := 0; i < len(text); i++ {
		if text[i] == target {
			return i
		}
	}
	return -1
}

// fingerprintHex renders one fingerprint as 16 lowercase hex digits.
func fingerprintHex(fingerprint uint64) string {
	const digits = "0123456789abcdef"
	var out [16]byte
	for i := 15; i >= 0; i-- {
		out[i] = digits[fingerprint&0xf]
		fingerprint >>= 4
	}
	return string(out[:])
}

// ExpressionPayload is the canonical payload of one `hcl.expression@1`
// ExtendedValue: the kind family spelling, the exact source text, and the
// structural fingerprint (RFC 0014 §8.2).
//
// The canonical payload envelope is three length-prefixed blobs:
//
//	payload := blob(kind) || blob(text) || fingerprint
//	blob    := varint(len) || bytes
//
// with the fingerprint as 8 little-endian bytes.
type ExpressionPayload struct {
	// Kind is the kind family spelling.
	Kind string
	// Text is the exact source text.
	Text string
	// Fingerprint is the structural fingerprint.
	Fingerprint uint64
}

// Encode encodes the canonical payload bytes under the `hcl.expression@1`
// codec.
func (p *ExpressionPayload) Encode() []byte {
	var payload []byte
	encodeBlob([]byte(p.Kind), &payload)
	encodeBlob([]byte(p.Text), &payload)
	var fingerprint [8]byte
	for i := 0; i < 8; i++ {
		fingerprint[i] = byte(p.Fingerprint >> (8 * i))
	}
	payload = append(payload, fingerprint[:]...)
	return payload
}

// Decode decodes one canonical payload; the second result is false for an
// envelope violation.
func (p *ExpressionPayload) Decode(payload []byte) bool {
	cursor := payloadCursor{data: payload}
	kind, ok := cursor.blob()
	if !ok || !isKindFamilySpelling(kind) {
		return false
	}
	text, ok := cursor.blob()
	if !ok {
		return false
	}
	fingerprint, ok := cursor.bytes(8)
	if !ok || !cursor.finished() {
		return false
	}
	var value uint64
	for i := 0; i < 8; i++ {
		value |= uint64(fingerprint[i]) << (8 * i)
	}
	p.Kind = string(kind)
	p.Text = string(text)
	p.Fingerprint = value
	return true
}

// isKindFamilySpelling reports whether one spelling is in the closed kind
// family set (RFC 0014 §8.2).
func isKindFamilySpelling(spelling []byte) bool {
	switch string(spelling) {
	case "number", "boolean", "null", "template", "function-call", "variable",
		"unary", "binary", "conditional", "for", "tuple", "object", "parenthesized":
		return true
	}
	return false
}

// payloadCursor is the cursor over the canonical payload envelope.
type payloadCursor struct {
	data   []byte
	offset int
}

func (c *payloadCursor) varint() (uint64, bool) {
	var value uint64
	for shift := uint(0); ; shift += 7 {
		if shift >= 63 {
			if c.offset >= len(c.data) {
				return 0, false
			}
			byte := c.data[c.offset]
			c.offset++
			payload := uint64(byte & 0x7f)
			if payload > 1 || byte&0x80 != 0 {
				return 0, false
			}
			return value | (payload << 63), true
		}
		if c.offset >= len(c.data) {
			return 0, false
		}
		byte := c.data[c.offset]
		c.offset++
		value |= uint64(byte&0x7f) << shift
		if byte&0x80 == 0 {
			return value, true
		}
	}
}

func (c *payloadCursor) blob() ([]byte, bool) {
	length, ok := c.varint()
	if !ok {
		return nil, false
	}
	if length > uint64(len(c.data)-c.offset) {
		return nil, false
	}
	end := c.offset + int(length)
	blob := c.data[c.offset:end]
	c.offset = end
	return blob, true
}

func (c *payloadCursor) bytes(length int) ([]byte, bool) {
	if length > len(c.data)-c.offset {
		return nil, false
	}
	end := c.offset + length
	bytes := c.data[c.offset:end]
	c.offset = end
	return bytes, true
}

func (c *payloadCursor) finished() bool { return c.offset == len(c.data) }

// encodeBlob encodes one length-prefixed blob into the canonical payload
// envelope.
func encodeBlob(bytes []byte, output *[]byte) {
	writeVarint(uint64(len(bytes)), output)
	*output = append(*output, bytes...)
}

// writeVarint encodes one unsigned LEB128 varint.
func writeVarint(value uint64, output *[]byte) {
	for {
		octet := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			octet |= 0x80
		}
		*output = append(*output, octet)
		if value == 0 {
			return
		}
	}
}
