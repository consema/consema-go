package main

// Facts-only file detection (RFC 0015 §7; mirror of the Rust bin's
// detect.rs). Detection returns only deterministic facts: byte facts (size,
// digest), BOM facts, leading-byte signature facts ("markers"), and the
// candidate profile set each marker implies — every candidate carries its
// reason. There is no parse, no conclusion, and no side effect (hard gate
// 2): a marker never selects a Profile, representation, or encoding (RFC
// 0015 §7.2 rule 1), and a candidate set of more than one profile is a
// first-class ambiguity result (`ambiguous: true`), never a silent guess
// (RFC 0015 §7.2 rule 5). The marker set and its judgments are the
// deterministic table below.

import (
	"sort"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// Candidate is one candidate profile with the deterministic reason for its
// candidacy (RFC 0015 §7.1 `candidates`).
type Candidate struct {
	// Profile is the candidate profile.
	Profile document.ProfileId
	// Reason is the deterministic marker judgment that produced the
	// candidacy.
	Reason string
}

// DetectFacts is the full fact inventory of one file's leading bytes (RFC
// 0015 §7.1).
type DetectFacts struct {
	// Size is the number of bytes read from the file (the file size when
	// fully read).
	Size uint64
	// Digest is the SHA-256 of the exact bytes; nil when the read was
	// capped.
	Digest *protocol.ContentDigest
	// BOM is the BOM fact: "Utf8" | "Utf16Le" | "Utf16Be"; empty when
	// absent.
	BOM string
	// Markers are the signature facts determinable from leading bytes
	// (zero or one).
	Markers []string
	// Candidates are the candidate set derived from the marker, each with a
	// reason; an empty set means no candidate.
	Candidates []Candidate
	// Ambiguous reports whether the candidate set has more than one entry.
	Ambiguous bool
	// AmbiguityReasons are the deterministic ambiguity explanations.
	AmbiguityReasons []string
}

// marker is one marker judgment: the signature fact, its reason, and the
// candidate profile ids the signature is consistent with.
type marker struct {
	fact     string
	reason   string
	profiles []string
}

// The candidate profiles per marker (the fixed detection table; the
// marker-collision rows are the frozen ambiguity cases of RFC 0015 §7.2
// rule 5: INI vs Properties, JSON vs JSON5, XML vs plist.xml, TOML table vs
// INI section).
var (
	plistBinaryMarker = &marker{profiles: []string{"plist.binary"}}
	xmlPlistMarker    = &marker{profiles: []string{"xml.1.0-safe", "plist.xml"}}
	plistXMLMarker    = &marker{profiles: []string{"plist.xml"}}
	jsonFamilyMarker  = &marker{profiles: []string{"json.strict", "jsonc.bounded", "json5.standard"}}
	iniTomlMarker     = &marker{profiles: []string{
		"ini.portable", "ini.windows", "ini.python-configparser", "toml.1.0"}}
	iniPropertiesMarker = &marker{profiles: []string{
		"ini.portable", "ini.windows", "ini.python-configparser",
		"java-properties.reader", "java-properties.latin1"}}
	yamlFamilyMarker = &marker{profiles: []string{"yaml.1.2-core", "yaml.1.1-compat"}}
	tomlHclMarker    = &marker{profiles: []string{"toml.1.0", "hcl.native", "hcl.tfvars"}}
)

// detect builds the deterministic fact inventory of one byte buffer.
// fullyRead marks whether the buffer holds the complete file; when it is
// false (a capped read), the digest fact is absent instead of a partial
// digest, and the size is the read size — never a disguised full-file fact
// (RFC 0015 §3.4, §12).
func detect(bytes []byte, fullyRead bool) DetectFacts {
	facts := DetectFacts{
		Size:    uint64(len(bytes)),
		Digest:  nil,
		BOM:     detectBOM(bytes),
		Markers: []string{},
	}
	if fullyRead {
		digest := protocol.DigestOf(bytes)
		facts.Digest = &digest
	}
	if m := markerOf(bytes); m != nil {
		facts.Markers = append(facts.Markers, m.fact)
		for _, profileID := range m.profiles {
			// Resolve against the facade inventory: a table id the facade
			// does not publish contributes no candidate.
			if entry := profileByID(profileID); entry != nil {
				facts.Candidates = append(facts.Candidates, Candidate{
					Profile: entry.Profile,
					Reason:  m.reason,
				})
			}
		}
		sort.Slice(facts.Candidates, func(left, right int) bool {
			if facts.Candidates[left].Profile.ID() != facts.Candidates[right].Profile.ID() {
				return facts.Candidates[left].Profile.ID() < facts.Candidates[right].Profile.ID()
			}
			return facts.Candidates[left].Profile.Version() < facts.Candidates[right].Profile.Version()
		})
		if len(facts.Candidates) > 1 {
			var families []string
			for _, profileID := range m.profiles {
				if entry := profileByID(profileID); entry != nil {
					seen := false
					for _, family := range families {
						if family == entry.FamilyID {
							seen = true
							break
						}
					}
					if !seen {
						families = append(families, entry.FamilyID)
					}
				}
			}
			sort.Strings(families)
			if len(families) > 1 {
				facts.AmbiguityReasons = append(facts.AmbiguityReasons,
					m.fact+" is consistent with format families: "+joinComma(families))
			} else {
				facts.AmbiguityReasons = append(facts.AmbiguityReasons,
					m.fact+" is consistent with multiple profiles of the "+families[0]+" family")
			}
		}
	}
	facts.Ambiguous = len(facts.Candidates) > 1
	return facts
}

// detectBOM returns one BOM detection fact; no codepage guessing (RFC 0015
// §7.1 `bom`).
func detectBOM(bytes []byte) string {
	if len(bytes) >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		return "Utf8"
	}
	if len(bytes) >= 2 && bytes[0] == 0xFF && bytes[1] == 0xFE {
		return "Utf16Le"
	}
	if len(bytes) >= 2 && bytes[0] == 0xFE && bytes[1] == 0xFF {
		return "Utf16Be"
	}
	return ""
}

// markerOf returns one deterministic marker judgment from the leading bytes,
// or nil. Judgments are exclusive (at most one marker): a `bplist00` header
// wins over everything; otherwise the first content line (after an optional
// BOM and leading whitespace) decides. The `[section]`-line judgment
// requires a comma-free interior so that a leading JSON array (`[1, 2]`)
// stays a `[`-fact; a `key = value` line with whitespace on both sides of
// `=` is the `a = 1` shape (TOML/HCL), a bare `key=value` line is the INI /
// Java-Properties shape.
func markerOf(bytes []byte) *marker {
	if len(bytes) >= 8 && string(bytes[:8]) == "bplist00" {
		return &marker{
			fact:     "bplist00 header",
			reason:   "leading bplist00 header bytes",
			profiles: plistBinaryMarker.profiles,
		}
	}
	first := firstContentByte(bytes)
	if first < 0 {
		return nil
	}
	lineEnd := len(bytes)
	for offset := first; offset < len(bytes); offset++ {
		if bytes[offset] == '\n' {
			lineEnd = offset
			break
		}
	}
	content := bytes[first:lineEnd]
	if hasPrefixBytes(content, "<?xml") {
		return &marker{
			fact:     "XML declaration",
			reason:   "leading XML declaration",
			profiles: xmlPlistMarker.profiles,
		}
	}
	if hasPrefixBytes(content, "<plist") {
		after := byte(0)
		if len(content) > 6 {
			after = content[6]
		}
		if after == ' ' || after == '\t' || after == '\r' || after == '\n' ||
			after == '/' || after == '>' {
			return &marker{
				fact:     "plist root element",
				reason:   "leading plist root element",
				profiles: plistXMLMarker.profiles,
			}
		}
	}
	if len(content) > 2 && content[0] == '[' && content[len(content)-1] == ']' &&
		!containsByte(content[1:len(content)-1], ',') {
		return &marker{
			fact:     "[section] line",
			reason:   "leading [section] line",
			profiles: iniTomlMarker.profiles,
		}
	}
	if content[0] == '[' {
		return &marker{
			fact:     "first non-whitespace '['",
			reason:   "first non-whitespace byte is '['",
			profiles: jsonFamilyMarker.profiles,
		}
	}
	if content[0] == '{' {
		return &marker{
			fact:     "first non-whitespace '{'",
			reason:   "first non-whitespace byte is '{'",
			profiles: jsonFamilyMarker.profiles,
		}
	}
	if hasPrefixBytes(content, "%YAML") {
		return &marker{
			fact:     "%YAML directive",
			reason:   "leading %YAML directive",
			profiles: yamlFamilyMarker.profiles,
		}
	}
	for equal := 0; equal < len(content); equal++ {
		if content[equal] == '=' {
			spaced := equal > 0 && isASCIIWhitespace(content[equal-1]) &&
				equal+1 < len(content) && isASCIIWhitespace(content[equal+1])
			if spaced {
				return &marker{
					fact:     "a = 1 shape",
					reason:   "leading a = 1 assignment shape",
					profiles: tomlHclMarker.profiles,
				}
			}
			return &marker{
				fact:     "key=value line",
				reason:   "leading key=value line",
				profiles: iniPropertiesMarker.profiles,
			}
		}
	}
	if containsByte(content, ':') {
		return &marker{
			fact:     "key: value line",
			reason:   "leading key: value line",
			profiles: yamlFamilyMarker.profiles,
		}
	}
	return nil
}

// firstContentByte returns the index of the first byte that is neither a
// BOM nor whitespace, or -1.
func firstContentByte(bytes []byte) int {
	index := 0
	if len(bytes) >= 3 && bytes[0] == 0xEF && bytes[1] == 0xBB && bytes[2] == 0xBF {
		index = 3
	} else if len(bytes) >= 2 && ((bytes[0] == 0xFF && bytes[1] == 0xFE) ||
		(bytes[0] == 0xFE && bytes[1] == 0xFF)) {
		index = 2
	}
	for index < len(bytes) && isASCIIWhitespace(bytes[index]) {
		index++
	}
	if index < len(bytes) {
		return index
	}
	return -1
}

func isASCIIWhitespace(byte byte) bool {
	return byte == ' ' || byte == '\t' || byte == '\r' || byte == '\n'
}

func hasPrefixBytes(bytes []byte, prefix string) bool {
	if len(bytes) < len(prefix) {
		return false
	}
	return string(bytes[:len(prefix)]) == prefix
}

func containsByte(bytes []byte, target byte) bool {
	for _, byte := range bytes {
		if byte == target {
			return true
		}
	}
	return false
}

func joinComma(items []string) string {
	text := ""
	for index, item := range items {
		if index > 0 {
			text += ", "
		}
		text += item
	}
	return text
}
