package consema

// This file implements the common opaque Document union over the format
// documents (crates/consema/src/lib.rs `Document`; RFC 0016 §3.2). The
// concrete representation is private; format access is only possible
// through the typed adapters. All returned facts are immutable snapshot
// facts. The union is additive: the JSON and TOML families land with
// 0.15.0 G1.4, and the remaining families (yaml/ini/properties/xml/plist/
// hcl) land with 0.16.0-0.18.0 without changing this type or the adapter
// semantics.

import (
	"context"

	"consema.dev/consema/document"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
)

// Document is the common opaque snapshot over the supported format
// documents. The concrete representation is private; format access is
// only possible through the typed adapters (AsJSON, AsTOML). Completed
// documents are logically immutable; concurrent reads are safe.
type Document struct {
	inner documentInner
}

// documentInner is the closed union payload. Additive: one entry per
// implemented format family, in the order the families land.
type documentInner struct {
	json *jsonpkg.Document
	toml *toml.Document
}

// ParseJSON forms one JSON/JSONC/JSON5 snapshot under an exact profile
// (crates/consema/src/lib.rs `Document::parse_json`). ctx carries
// cancellation only, passed through to the JSON family parse entry.
func ParseJSON(ctx context.Context, source []byte, profile jsonpkg.JsonProfile,
	limits document.ParseLimits) (*Document, *jsonpkg.FormationFailure) {
	document, failure := jsonpkg.Parse(ctx, source, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{json: document}}, nil
}

// ParseTOML forms one TOML 1.0 snapshot under the exact profile
// (crates/consema/src/lib.rs `Document::parse_toml`). The TOML family
// parse entry has no cancellation; the facade mirrors that.
func ParseTOML(source []byte, profile toml.TomlProfile,
	limits document.ParseLimits) (*Document, *toml.FormationFailure) {
	document, failure := toml.Parse(source, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{toml: document}}, nil
}

// Render returns the default rendering, byte-for-byte identical to the
// source.
func (d *Document) Render() []byte {
	switch {
	case d.inner.json != nil:
		return d.inner.json.Render()
	case d.inner.toml != nil:
		return d.inner.toml.Render()
	}
	return nil
}

// FormationStatus returns the formation status of the underlying
// snapshot.
func (d *Document) FormationStatus() document.FormationStatus {
	switch {
	case d.inner.json != nil:
		return d.inner.json.FormationStatus()
	case d.inner.toml != nil:
		return d.inner.toml.FormationStatus()
	}
	return document.FormationStatusComplete
}

// Diagnostics returns the deterministically ordered document
// diagnostics.
func (d *Document) Diagnostics() []*protocol.Diagnostic {
	switch {
	case d.inner.json != nil:
		return d.inner.json.Diagnostics()
	case d.inner.toml != nil:
		return d.inner.toml.Diagnostics()
	}
	return nil
}

// SnapshotIdentity is the snapshot identity to which every handle and
// span of this document belongs.
func (d *Document) SnapshotIdentity() document.SnapshotIdentity {
	switch {
	case d.inner.json != nil:
		return d.inner.json.SnapshotIdentity()
	case d.inner.toml != nil:
		return d.inner.toml.SnapshotIdentity()
	}
	return document.SnapshotIdentity(0)
}

// Profile returns the exact source profile of the underlying format
// document.
func (d *Document) Profile() document.ProfileId {
	switch {
	case d.inner.json != nil:
		return d.inner.json.Profile()
	case d.inner.toml != nil:
		return d.inner.toml.Profile()
	}
	return document.NewProfileId("", 0)
}

// FormatFamily returns the format family contract of the underlying
// document.
func (d *Document) FormatFamily() document.FormatFamilyId {
	switch {
	case d.inner.json != nil:
		return d.inner.json.FormatFamily()
	case d.inner.toml != nil:
		return d.inner.toml.FormatFamily()
	}
	return document.NewFormatFamilyId("", 0)
}

// AsJSON returns the typed JSON-family document; ok is false only when
// the snapshot is not a JSON document.
func (d *Document) AsJSON() (*jsonpkg.Document, bool) {
	return d.inner.json, d.inner.json != nil
}

// AsTOML returns the typed TOML document; ok is false only when the
// snapshot is not a TOML document.
func (d *Document) AsTOML() (*toml.Document, bool) {
	return d.inner.toml, d.inner.toml != nil
}
