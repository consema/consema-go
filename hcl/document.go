package hcl

import (
	"context"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// formedDocument is one formed HCL native pipeline result before the
// profile gate (RFC 0014 §3).
type formedDocument struct {
	source       *document.SourceSnapshot
	authority    document.DocumentAuthority
	recovered    bool
	diagnostics  []*protocol.Diagnostic
	document     *HclDocument
	errorRegions []HclErrorRegion
	index        *document.LosslessStructuralIndex
	syntaxKinds  []HclSyntaxKind
	limits       HclParseLimits
}

// Document is one formed HCL document under one exact profile (RFC 0014
// §1, §3).
//
// The profile is a private field, not a representation choice: both
// profiles share the one syntax system and the one native model, and the
// profile gates Complete formation (the tfvars top-level restriction of
// RFC 0014 §5) and the operation surface published over this document.
// Every returned fact is an immutable snapshot fact; completed documents
// are logically immutable and safe for concurrent reads.
type Document struct {
	authority    document.DocumentAuthority
	source       *document.SourceSnapshot
	profile      HclProfile
	status       document.FormationStatus
	diagnostics  []*protocol.Diagnostic
	document     *HclDocument
	errorRegions []HclErrorRegion
	index        *document.LosslessStructuralIndex
	syntaxKinds  []HclSyntaxKind
	limits       HclParseLimits
}

// SnapshotIdentity returns the snapshot identity to which every NodeRef
// and Span belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	return d.authority.Identity()
}

// Source returns the exact immutable source snapshot.
func (d *Document) Source() *document.SourceSnapshot { return d.source }

// Render returns the exact original bytes; unmodified rendering is
// byte-exact. The returned slice is a copy.
func (d *Document) Render() []byte { return d.source.Bytes() }

// FormatFamily returns the HCL format family contract.
func (d *Document) FormatFamily() document.FormatFamilyId {
	return document.NewFormatFamilyId("hcl", 1)
}

// Profile returns the exact language profile.
func (d *Document) Profile() document.ProfileId { return d.profile.ID() }

// FormationStatus returns whether recovery structure was required (RFC
// 0014 §3).
func (d *Document) FormationStatus() document.FormationStatus { return d.status }

// Status returns the formation status; it is the same fact as
// FormationStatus.
func (d *Document) Status() document.FormationStatus { return d.status }

// Diagnostics returns the deterministically ordered document diagnostics.
// The returned slice must not be modified.
func (d *Document) Diagnostics() []*protocol.Diagnostic { return d.diagnostics }

// Document returns the native body tree bound to the frozen source; always
// present under both profiles, an empty body being a valid body (RFC 0014
// §3, §6).
func (d *Document) Document() *HclDocument { return d.document }

// Native returns the native body tree; it is the same fact as Document.
func (d *Document) Native() *HclDocument { return d.document }

// RootBody returns the root body of the native tree.
func (d *Document) RootBody() *HclBody { return d.document.body }

// LosslessStructuralIndex returns the exhaustive ordered lossless piece
// coverage of the raw bytes; always present under both profiles because
// both share the one syntax system (RFC 0014 §7.2).
func (d *Document) LosslessStructuralIndex() *document.LosslessStructuralIndex {
	return d.index
}

// LosslessSyntaxKinds returns the format-specific kind for every
// structural piece, in the same source order. The returned slice must not
// be modified.
func (d *Document) LosslessSyntaxKinds() []HclSyntaxKind { return d.syntaxKinds }

// ErrorRegions returns the recovered error regions in source order (RFC
// 0014 §3, §7.2). The tfvars gate never contributes an error region: a
// rejected top-level block is a proven construct, not a recovered region.
// The returned slice must not be modified.
func (d *Document) ErrorRegions() []HclErrorRegion { return d.errorRegions }

// nodeRef issues the typed node handle for one structural identity.
func (d *Document) nodeRef(index uint64, role document.NodeRole) document.NodeRef {
	return d.authority.NodeRef(index, role)
}

// span issues one snapshot-bound span.
func (d *Document) span(start, end int) document.Span {
	span, err := d.authority.Span(start, end)
	if err != nil {
		return document.Span{}
	}
	return span
}

// DecodedText returns the decoded source text view.
func (d *Document) DecodedText() string {
	text, _ := d.source.DecodedText()
	return text
}

// Limits returns the parse limits applied during formation.
func (d *Document) Limits() HclParseLimits { return d.limits }

// selector returns the profile selector of the formed document.
func (d *Document) selector() HclProfile { return d.profile }

// parseHCL runs the frozen native pipeline: source construction, the
// lexer pass, the parser pass, and the tfvars profile gate (RFC 0014 §3,
// §5). The returned error is a fatal formation failure.
func parseHCL(source *document.SourceSnapshot, profile HclProfile,
	limits HclParseLimits) (*Document, error) {
	lexed, err := lexSource(source, document.NewDocumentAuthority(), limits, 0, source.Len(), true)
	if err != nil {
		return nil, err
	}
	parser := newParser(lexed, source, limits, limits.Common.MaxDiagnostics)
	formed, err := parser.parse()
	if err != nil {
		return nil, err
	}
	status := document.FormationStatusComplete
	diagnostics := formed.diagnostics
	if formed.recovered {
		status = document.FormationStatusRecovered
	}
	if profile == HclProfileTfvarsV1 {
		for _, item := range formed.document.body.items {
			if item.AsBlock() != nil {
				status = document.FormationStatusRecovered
				block := item.AsBlock()
				span := block.Span()
				diagnostics = append(diagnostics, &protocol.Diagnostic{
					Code:      "hcl.tfvars.block-not-allowed@1",
					Category:  protocol.CategorySyntax,
					Severity:  protocol.SeverityError,
					Primary:   diagnosticLocation(span),
					Arguments: map[string]string{},
				})
			}
		}
	}
	sortDiagnostics(diagnostics)
	return &Document{
		authority:    formed.authority,
		source:       source,
		profile:      profile,
		status:       status,
		diagnostics:  diagnostics,
		document:     formed.document,
		errorRegions: formed.errorRegions,
		index:        formed.index,
		syntaxKinds:  formed.syntaxKinds,
		limits:       limits,
	}, nil
}

// diagnosticLocation renders one span as a transferable source location.
func diagnosticLocation(span document.Span) *protocol.SourceLocation {
	return &protocol.SourceLocation{
		SourceID:  "snapshot",
		StartByte: uint64(span.StartByte()),
		EndByte:   uint64(span.EndByte()),
	}
}

// sortDiagnostics sorts one diagnostic list deterministically by primary
// start byte, then end byte, then code (the stable occurrence order).
func sortDiagnostics(diagnostics []*protocol.Diagnostic) {
	for i := 1; i < len(diagnostics); i++ {
		for j := i; j > 0; j-- {
			left := diagnostics[j-1]
			right := diagnostics[j]
			if diagnosticLess(left, right) {
				break
			}
			diagnostics[j-1], diagnostics[j] = right, left
		}
	}
}

func diagnosticLess(left, right *protocol.Diagnostic) bool {
	leftStart, rightStart := diagnosticStart(left), diagnosticStart(right)
	if leftStart != rightStart {
		return leftStart < rightStart
	}
	leftEnd, rightEnd := diagnosticEnd(left), diagnosticEnd(right)
	if leftEnd != rightEnd {
		return leftEnd < rightEnd
	}
	if left.Occurrence != right.Occurrence {
		return left.Occurrence < right.Occurrence
	}
	return left.Code < right.Code
}

func diagnosticStart(diagnostic *protocol.Diagnostic) uint64 {
	if diagnostic.Primary != nil {
		return diagnostic.Primary.StartByte
	}
	return 0
}

func diagnosticEnd(diagnostic *protocol.Diagnostic) uint64 {
	if diagnostic.Primary != nil {
		return diagnostic.Primary.EndByte
	}
	return 0
}

// contextBackground returns the background context for internal parses;
// the formation surface is cancellable through Parse.
func contextBackground() context.Context { return context.Background() }
