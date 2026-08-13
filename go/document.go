package consema

// This file implements the common opaque Document union over the format
// documents (consema-rs/crates/consema/src/lib.rs `Document`; RFC 0016 §3.2). The
// concrete representation is private; format access is only possible
// through the typed adapters. All returned facts are immutable snapshot
// facts. The union is additive: the JSON and TOML families land with
// 0.15.0 G1.4, and the remaining families (yaml/ini/properties/xml/plist/
// hcl) land with 0.16.0-0.18.0 without changing this type or the adapter
// semantics.

import (
	"context"

	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/protocol"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

// Document is the common opaque snapshot over the supported format
// documents. The concrete representation is private; format access is
// only possible through the typed adapters (AsJSON, AsTOML, AsYAML,
// AsINI, AsProperties, AsXML, AsPlist, AsHCL). Completed documents are
// logically immutable; concurrent reads are safe.
type Document struct {
	inner documentInner
}

// documentInner is the closed union payload. Additive: one entry per
// implemented format family, in the order the families land.
type documentInner struct {
	json       *jsonpkg.Document
	toml       *toml.Document
	yaml       *yaml.Document
	ini        *ini.Document
	properties *properties.Document
	xml        *xmlpkg.Document
	plist      *plist.Document
	hcl        *hclpkg.Document
}

// ParseJSON forms one JSON/JSONC/JSON5 snapshot under an exact profile
// (consema-rs/crates/consema/src/lib.rs `Document::parse_json`). ctx carries
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
// (consema-rs/crates/consema/src/lib.rs `Document::parse_toml`). The TOML family
// parse entry has no cancellation; the facade mirrors that.
func ParseTOML(source []byte, profile toml.TomlProfile,
	limits document.ParseLimits) (*Document, *toml.FormationFailure) {
	document, failure := toml.Parse(source, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{toml: document}}, nil
}

// ParseYAML forms one YAML stream snapshot under the exact frozen profile
// (consema-rs/crates/consema/src/lib.rs `Document::parse_yaml`). The YAML family
// parse entry has no cancellation; the facade mirrors that.
func ParseYAML(source []byte, profile yaml.YamlProfile,
	limits document.ParseLimits) (*Document, *yaml.FormationFailure) {
	document, failure := yaml.Parse(source, profile, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{yaml: document}}, nil
}

// ParseINI forms one INI snapshot under the exact profile and explicit
// encoding selection (consema-rs/crates/consema/src/lib.rs `Document::parse_ini`).
func ParseINI(source []byte, profile ini.IniProfile, selection ini.IniEncodingSelection,
	limits ini.IniParseLimits) (*Document, *ini.FormationFailure) {
	document, failure := ini.Parse(source, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{ini: document}}, nil
}

// ParseProperties forms one Java Properties snapshot under the exact
// profile and source contract (consema-rs/crates/consema/src/lib.rs
// `Document::parse_properties`).
func ParseProperties(source []byte, profile properties.PropertiesProfile,
	selection properties.PropertiesEncodingSelection,
	limits properties.PropertiesParseLimits) (*Document, *properties.FormationFailure) {
	document, failure := properties.Parse(source, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{properties: document}}, nil
}

// ParseXML forms one XML 1.0 safe snapshot under the exact profile and
// explicit encoding selection (consema-rs/crates/consema/src/lib.rs
// `Document::parse_xml`). ctx carries cancellation only, passed through
// to the XML family parse entry.
func ParseXML(ctx context.Context, source []byte, profile xmlpkg.XmlProfile,
	selection xmlpkg.XmlEncodingSelection,
	limits xmlpkg.XmlParseLimits) (*Document, *xmlpkg.FormationFailure) {
	document, failure := xmlpkg.Parse(ctx, source, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{xml: document}}, nil
}

// ParsePlist forms one Property List snapshot under the exact profile and
// explicit encoding selection (consema-rs/crates/consema/src/lib.rs
// `Document::parse_plist`).
func ParsePlist(source []byte, profile plist.PlistProfile, selection plist.PlistEncodingSelection,
	limits plist.PlistParseLimits) (*Document, *plist.FormationFailure) {
	document, failure := plist.Parse(source, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{plist: document}}, nil
}

// ParseHCL forms one HCL snapshot under the exact profile and explicit
// encoding selection (consema-rs/crates/consema/src/lib.rs `Document::parse_hcl`).
// ctx carries cancellation only, passed through to the HCL family parse
// entry.
func ParseHCL(ctx context.Context, source []byte, profile hclpkg.HclProfile,
	selection hclpkg.HclEncodingSelection,
	limits hclpkg.HclParseLimits) (*Document, *hclpkg.FormationFailure) {
	document, failure := hclpkg.Parse(ctx, source, profile, selection, limits)
	if failure != nil {
		return nil, failure
	}
	return &Document{inner: documentInner{hcl: document}}, nil
}

// Render returns the default rendering, byte-for-byte identical to the
// source.
func (d *Document) Render() []byte {
	switch {
	case d.inner.json != nil:
		return d.inner.json.Render()
	case d.inner.toml != nil:
		return d.inner.toml.Render()
	case d.inner.yaml != nil:
		return d.inner.yaml.Render()
	case d.inner.ini != nil:
		return d.inner.ini.Render()
	case d.inner.properties != nil:
		return d.inner.properties.Render()
	case d.inner.xml != nil:
		return d.inner.xml.Render()
	case d.inner.plist != nil:
		return d.inner.plist.Render()
	case d.inner.hcl != nil:
		return d.inner.hcl.Render()
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
	case d.inner.yaml != nil:
		return d.inner.yaml.FormationStatus()
	case d.inner.ini != nil:
		return d.inner.ini.FormationStatus()
	case d.inner.properties != nil:
		return d.inner.properties.FormationStatus()
	case d.inner.xml != nil:
		return d.inner.xml.FormationStatus()
	case d.inner.plist != nil:
		return d.inner.plist.FormationStatus()
	case d.inner.hcl != nil:
		return d.inner.hcl.FormationStatus()
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
	case d.inner.yaml != nil:
		return d.inner.yaml.Diagnostics()
	case d.inner.ini != nil:
		return d.inner.ini.Diagnostics()
	case d.inner.properties != nil:
		return d.inner.properties.Diagnostics()
	case d.inner.xml != nil:
		return d.inner.xml.Diagnostics()
	case d.inner.plist != nil:
		return d.inner.plist.Diagnostics()
	case d.inner.hcl != nil:
		return d.inner.hcl.Diagnostics()
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
	case d.inner.yaml != nil:
		return d.inner.yaml.SnapshotIdentity()
	case d.inner.ini != nil:
		return d.inner.ini.SnapshotIdentity()
	case d.inner.properties != nil:
		return d.inner.properties.SnapshotIdentity()
	case d.inner.xml != nil:
		return d.inner.xml.SnapshotIdentity()
	case d.inner.plist != nil:
		return d.inner.plist.SnapshotIdentity()
	case d.inner.hcl != nil:
		return d.inner.hcl.SnapshotIdentity()
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
	case d.inner.yaml != nil:
		return d.inner.yaml.Profile()
	case d.inner.ini != nil:
		return d.inner.ini.Profile()
	case d.inner.properties != nil:
		return d.inner.properties.Profile()
	case d.inner.xml != nil:
		return d.inner.xml.Profile()
	case d.inner.plist != nil:
		return d.inner.plist.Profile()
	case d.inner.hcl != nil:
		return d.inner.hcl.Profile()
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
	case d.inner.yaml != nil:
		return d.inner.yaml.FormatFamily()
	case d.inner.ini != nil:
		return d.inner.ini.FormatFamily()
	case d.inner.properties != nil:
		return d.inner.properties.FormatFamily()
	case d.inner.xml != nil:
		return d.inner.xml.FormatFamily()
	case d.inner.plist != nil:
		return d.inner.plist.FormatFamily()
	case d.inner.hcl != nil:
		return d.inner.hcl.FormatFamily()
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

// AsYAML returns the typed YAML document; ok is false only when the
// snapshot is not a YAML document.
func (d *Document) AsYAML() (*yaml.Document, bool) {
	return d.inner.yaml, d.inner.yaml != nil
}

// AsINI returns the typed INI document; ok is false only when the
// snapshot is not an INI document.
func (d *Document) AsINI() (*ini.Document, bool) {
	return d.inner.ini, d.inner.ini != nil
}

// AsProperties returns the typed Java Properties document; ok is false
// only when the snapshot is not a Properties document.
func (d *Document) AsProperties() (*properties.Document, bool) {
	return d.inner.properties, d.inner.properties != nil
}

// AsXML returns the typed XML document; ok is false only when the
// snapshot is not an XML document.
func (d *Document) AsXML() (*xmlpkg.Document, bool) {
	return d.inner.xml, d.inner.xml != nil
}

// AsPlist returns the typed Property List document; ok is false only
// when the snapshot is not a plist document.
func (d *Document) AsPlist() (*plist.Document, bool) {
	return d.inner.plist, d.inner.plist != nil
}

// AsHCL returns the typed HCL document; ok is false only when the
// snapshot is not an HCL document.
func (d *Document) AsHCL() (*hclpkg.Document, bool) {
	return d.inner.hcl, d.inner.hcl != nil
}
