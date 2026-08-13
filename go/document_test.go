package consema

// The Document union acceptance tests (consema-rs/consema/src/lib.rs tests:
// common_document_facade_is_opaque_and_typed).

import (
	"context"
	"testing"

	"consema.dev/consema/document"
	hclpkg "consema.dev/consema/hcl"
	"consema.dev/consema/ini"
	jsonpkg "consema.dev/consema/json"
	"consema.dev/consema/plist"
	"consema.dev/consema/properties"
	"consema.dev/consema/toml"
	xmlpkg "consema.dev/consema/xml"
	"consema.dev/consema/yaml"
)

func TestDocumentFacadeIsOpaqueAndTyped(t *testing.T) {
	jsonDoc, failure := ParseJSON(context.Background(), []byte(`{"a":1}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("JSON facade parse: %s", failure.Error())
	}
	if string(jsonDoc.Render()) != `{"a":1}` {
		t.Fatalf("JSON facade render differs")
	}
	if jsonDoc.FormationStatus() != document.FormationStatusComplete {
		t.Fatalf("JSON facade formation status differs")
	}
	if jsonDoc.Profile() != document.NewProfileId("json.strict", 1) {
		t.Fatalf("JSON facade profile differs")
	}
	if jsonDoc.FormatFamily() != document.NewFormatFamilyId("json", 1) {
		t.Fatalf("JSON facade family differs")
	}
	if len(jsonDoc.Diagnostics()) != 0 {
		t.Fatalf("JSON facade diagnostics must be empty")
	}
	if _, ok := jsonDoc.AsJSON(); !ok {
		t.Fatalf("JSON adapter must succeed")
	}
	if _, ok := jsonDoc.AsTOML(); ok {
		t.Fatalf("TOML adapter must fail on a JSON document")
	}

	tomlDoc, tomlFailure := ParseTOML([]byte("value = 1\n"), toml.Toml10V1,
		document.DefaultParseLimits())
	if tomlFailure != nil {
		t.Fatalf("TOML facade parse: %s", tomlFailure.Error())
	}
	if string(tomlDoc.Render()) != "value = 1\n" {
		t.Fatalf("TOML facade render differs")
	}
	if tomlDoc.Profile() != document.NewProfileId("toml.1.0", 1) {
		t.Fatalf("TOML facade profile differs")
	}
	if tomlDoc.FormatFamily() != document.NewFormatFamilyId("toml", 1) {
		t.Fatalf("TOML facade family differs")
	}
	if _, ok := tomlDoc.AsTOML(); !ok {
		t.Fatalf("TOML adapter must succeed")
	}
	if _, ok := tomlDoc.AsJSON(); ok {
		t.Fatalf("JSON adapter must fail on a TOML document")
	}

	other, failure := ParseJSON(context.Background(), []byte(`{}`),
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure != nil {
		t.Fatalf("second JSON facade parse: %s", failure.Error())
	}
	if jsonDoc.SnapshotIdentity() == other.SnapshotIdentity() {
		t.Fatalf("snapshot identities must be distinct")
	}
}

func TestDocumentFacadeCoversAllEightFamilies(t *testing.T) {
	// One typed adapter per family; every adapter rejects the wrong
	// family, and every document reports its exact profile and family.
	cases := []struct {
		name    string
		parse   func(t *testing.T) (*Document, []string)
		family  string
		profile string
		render  string
	}{
		{
			name: "yaml", family: "yaml", profile: "yaml.1.2-core",
			render: "value: 1\n",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParseYAML([]byte("value: 1\n"), yaml.Yaml12CoreV1,
					document.DefaultParseLimits())
				if failure != nil {
					t.Fatalf("YAML facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "ini", "properties",
					"xml", "plist", "hcl"}
			},
		},
		{
			name: "ini", family: "ini", profile: "ini.portable",
			render: "[section]\nvalue=1\n",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParseINI([]byte("[section]\nvalue=1\n"), ini.PortableV1,
					ini.IniEncodingProfileDefault(), ini.DefaultIniParseLimits())
				if failure != nil {
					t.Fatalf("INI facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "yaml", "properties",
					"xml", "plist", "hcl"}
			},
		},
		{
			name: "properties", family: "java-properties", profile: "java-properties.reader",
			render: "name=api\n",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParseProperties([]byte("name=api\n"), properties.PropertiesReaderV1,
					properties.ReaderEncodingSelection(document.Utf8Encoding()),
					properties.DefaultPropertiesParseLimits())
				if failure != nil {
					t.Fatalf("Properties facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "yaml", "ini", "xml",
					"plist", "hcl"}
			},
		},
		{
			name: "xml", family: "xml", profile: "xml.1.0-safe",
			render: "<service><name>catalog</name></service>",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParseXML(context.Background(),
					[]byte("<service><name>catalog</name></service>"), xmlpkg.XmlProfileSafeV1,
					xmlpkg.XmlEncodingProfileDefault(), xmlpkg.DefaultXmlParseLimits())
				if failure != nil {
					t.Fatalf("XML facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "yaml", "ini",
					"properties", "plist", "hcl"}
			},
		},
		{
			name: "plist", family: "plist", profile: "plist.xml",
			render: "<plist version=\"1.0\"><string>x</string></plist>",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParsePlist(
					[]byte("<plist version=\"1.0\"><string>x</string></plist>"),
					plist.PlistProfileXmlV1, plist.PlistEncodingProfileDefault(),
					plist.DefaultPlistParseLimits())
				if failure != nil {
					t.Fatalf("plist facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "yaml", "ini",
					"properties", "xml", "hcl"}
			},
		},
		{
			name: "hcl", family: "hcl", profile: "hcl.native",
			render: "a = 1\n",
			parse: func(t *testing.T) (*Document, []string) {
				doc, failure := ParseHCL(context.Background(), []byte("a = 1\n"),
					hclpkg.HclProfileNativeV1, hclpkg.HclEncodingSelectionProfileDefault(),
					hclpkg.DefaultHclParseLimits())
				if failure != nil {
					t.Fatalf("HCL facade parse: %s", failure.Error())
				}
				return doc, []string{"json", "toml", "yaml", "ini",
					"properties", "xml", "plist"}
			},
		},
	}
	adapters := []struct {
		family string
		ok     func(*Document) bool
	}{
		{"json", func(d *Document) bool { _, ok := d.AsJSON(); return ok }},
		{"toml", func(d *Document) bool { _, ok := d.AsTOML(); return ok }},
		{"yaml", func(d *Document) bool { _, ok := d.AsYAML(); return ok }},
		{"ini", func(d *Document) bool { _, ok := d.AsINI(); return ok }},
		{"java-properties", func(d *Document) bool { _, ok := d.AsProperties(); return ok }},
		{"xml", func(d *Document) bool { _, ok := d.AsXML(); return ok }},
		{"plist", func(d *Document) bool { _, ok := d.AsPlist(); return ok }},
		{"hcl", func(d *Document) bool { _, ok := d.AsHCL(); return ok }},
	}
	for _, test := range cases {
		doc, _ := test.parse(t)
		if doc.Profile().ID() != test.profile {
			t.Fatalf("%s facade profile differs: %s", test.name, doc.Profile().ID())
		}
		if doc.FormatFamily().ID() != test.family {
			t.Fatalf("%s facade family differs: %s", test.name, doc.FormatFamily().ID())
		}
		if string(doc.Render()) != test.render {
			t.Fatalf("%s facade render differs: %q, want %q", test.name,
				doc.Render(), test.render)
		}
		if doc.FormationStatus() != document.FormationStatusComplete {
			t.Fatalf("%s facade formation status differs", test.name)
		}
		if len(doc.Diagnostics()) != 0 {
			t.Fatalf("%s facade diagnostics must be empty", test.name)
		}
		for _, adapter := range adapters {
			expect := adapter.family == test.family
			if got := adapter.ok(doc); got != expect {
				t.Fatalf("%s facade: %s adapter = %v, want %v", test.name,
					adapter.family, got, expect)
			}
		}
	}
}

func TestDocumentFacadeFormationFailure(t *testing.T) {
	// Non-UTF-8 bytes fail the JSON input contract with the typed source
	// failure; no document exists.
	_, failure := ParseJSON(context.Background(), []byte{0xff},
		jsonpkg.JsonProfileStrictV1, document.DefaultParseLimits())
	if failure == nil {
		t.Fatalf("invalid UTF-8 must fail formation")
	}
	if failure.Code() == "" {
		t.Fatalf("formation failure must carry a registered code")
	}
}
