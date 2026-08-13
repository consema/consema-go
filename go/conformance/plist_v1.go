package conformance

// The `consema.plist.conformance@1` suite runner
// (consema-rs/crates/consema-conformance/src/plist_v1.rs). The 0.17.0 milestone (G3.2)
// implements the full plist surface: XML and binary formation with
// recovery, the three query domains, value-tree and require-object
// projection, both canonical materializations with the reparse closure,
// cross-representation conversion, and the six structural edits. All 45
// published cases are executed; the vector files are the authority and the
// runner embeds no expectation literals.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"consema.dev/consema/core"
	"consema.dev/consema/document"
	"consema.dev/consema/plist"
	"consema.dev/consema/protocol"
)

// runPlistV1 executes the embedded `consema.plist.conformance@1` suite.
func runPlistV1(runner *Runner, data *suiteData) *SuiteReport {
	report := &SuiteReport{}
	for index := range data.Cases {
		vector := &data.Cases[index]
		switch vector.Capability {
		case "plist.xml-formation@1":
			runPlistXMLFormation(vector, report)
		case "plist.binary-formation@1":
			runPlistBinaryFormation(vector, report)
		case "plist.limit@1":
			runPlistLimit(vector, report)
		case "plist.query@1":
			runPlistQuery(vector, report)
		case "plist.projection@1":
			runPlistProjection(vector, report)
		case "plist.materialization@1":
			runPlistMaterialization(runner, vector, report)
		case "plist.conversion@1":
			runPlistConversion(vector, report)
		case "plist.edit@1":
			runPlistEdit(vector, report)
		default:
			report.Failed = append(report.Failed, CaseFailure{
				ID:      vector.ID,
				Message: "runner does not recognize published plist capability",
			})
		}
	}
	return report
}

// ---------------------------------------------------------------------------
// Limit
// ---------------------------------------------------------------------------

// runPlistLimit executes one `plist.limit@1` case: the parse must fail
// fatally under the declared limits and the failure must carry the expected
// plist.limit.*@1 code (RFC 0013 §11; mirror of plist_v1.rs run_limit).
func runPlistLimit(vector *caseData, report *SuiteReport) {
	profile, message := plistProfileOf(vector.Input)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	bytes, message := plistSourceBytes(vector.Input, profile)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	limits, message := plistLimitsOf(vector.Input)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	_, failure := plist.Parse(bytes, profile, plist.PlistEncodingProfileDefault(), limits)
	if failure == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "parse must fail fatally under the declared limits"})
		return
	}
	if status, ok := stringField(vector.Expected, "status"); ok && status != "FatalFormationFailure" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "limit case status must be FatalFormationFailure"})
		return
	}
	expectedCode, ok := stringField(vector.Expected, "diagnostic")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.diagnostic"})
		return
	}
	for _, diagnostic := range failure.Diagnostics {
		if diagnostic.Code == expectedCode {
			report.Passed = append(report.Passed, vector.ID)
			return
		}
	}
	report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
		Message: "diagnostic " + expectedCode + " not found"})
}

// plistLimitsOf reads the `input.limits` object into a PlistParseLimits;
// every vector limit name is the Go limits field spelling.
func plistLimitsOf(value core.Value) (plist.PlistParseLimits, string) {
	limits := plist.DefaultPlistParseLimits()
	entries, ok := objectField(value, "limits")
	if !ok {
		return limits, ""
	}
	object, ok := entries.(*core.Object)
	if !ok {
		return limits, "input.limits must be an object"
	}
	for _, entry := range object.Entries() {
		integer, ok := entry.Value.(core.Integer)
		if !ok {
			return limits, "limit " + entry.Key + " must be a non-negative integer"
		}
		number := integer.Int()
		if number.Sign() < 0 || !number.IsInt64() {
			return limits, "limit " + entry.Key + " must be a non-negative integer"
		}
		value := int(number.Int64())
		switch entry.Key {
		case "max_container_depth":
			limits.MaxContainerDepth = value
		case "max_object_count":
			limits.MaxObjectCount = value
		case "max_string_code_units":
			limits.MaxStringCodeUnits = value
		case "max_data_bytes":
			limits.MaxDataBytes = value
		default:
			return limits, "unknown plist limit " + entry.Key
		}
	}
	return limits, ""
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// plistVectorField reads one vector fact from a value or its `input`
// member: samples carry their facts at the top level, case-level inputs
// wrap them under `input`.
func plistVectorField(value core.Value, name string) (core.Value, bool) {
	if field, ok := objectField(value, name); ok {
		return field, true
	}
	return caseInputOf(value, name)
}

// caseInputOf reads one named input member of a case object.
func caseInputOf(value core.Value, name string) (core.Value, bool) {
	input, ok := objectField(value, "input")
	if !ok {
		return nil, false
	}
	return objectField(input, name)
}

// plistProfileOf resolves the frozen profile of one vector value.
func plistProfileOf(value core.Value) (plist.PlistProfile, string) {
	switch profile, ok := plistStringVectorField(value, "profile"); {
	case !ok:
		return plist.PlistProfileXmlV1, "missing profile"
	case profile == "plist.xml@1":
		return plist.PlistProfileXmlV1, ""
	case profile == "plist.binary@1":
		return plist.PlistProfileBinaryV1, ""
	default:
		return plist.PlistProfileXmlV1, "unknown profile " + profile
	}
}

// plistStringVectorField reads one String vector fact.
func plistStringVectorField(value core.Value, name string) (string, bool) {
	field, ok := plistVectorField(value, name)
	if !ok {
		return "", false
	}
	text, ok := field.(core.String)
	if !ok {
		return "", false
	}
	return string(text), true
}

// plistSourceBytes reads the raw source bytes of one vector input or
// sample: `source` (with an optional `encoding`) for the XML profile,
// `hex` for the binary profile.
func plistSourceBytes(value core.Value, profile plist.PlistProfile) ([]byte, string) {
	if profile == plist.PlistProfileBinaryV1 {
		text, ok := plistStringVectorField(value, "hex")
		if !ok {
			return nil, "missing input.hex"
		}
		decoded, err := hex.DecodeString(text)
		if err != nil {
			return nil, "invalid hex"
		}
		return decoded, ""
	}
	source, ok := plistStringVectorField(value, "source")
	if !ok {
		return nil, "missing input.source"
	}
	encoding, _ := plistStringVectorField(value, "encoding")
	if encoding == "utf16le-bom" {
		bytes := []byte{0xFF, 0xFE}
		for _, unit := range encodeUTF16(source) {
			bytes = append(bytes, byte(unit), byte(unit>>8))
		}
		return bytes, ""
	}
	return []byte(source), ""
}

// encodeUTF16 encodes one decoded text into UTF-16 code units.
func encodeUTF16(text string) []uint16 {
	units := make([]uint16, 0, len(text))
	for _, rune := range text {
		if rune < 0x10000 {
			units = append(units, uint16(rune))
		} else {
			rune -= 0x10000
			units = append(units, uint16(0xD800+(rune>>10)), uint16(0xDC00+(rune&0x3FF)))
		}
	}
	return units
}

// plistFormValue forms one document from a case-level input or one sample
// descriptor.
func plistFormValue(value core.Value) (*plist.Document, string) {
	profile, message := plistProfileOf(value)
	if message != "" {
		return nil, message
	}
	bytes, message := plistSourceBytes(value, profile)
	if message != "" {
		return nil, message
	}
	return plistFormBytes(bytes, profile)
}

func plistFormCase(vector *caseData) (*plist.Document, string) {
	return plistFormValue(vector.Input)
}

// plistSampleProfile resolves one sample's profile; samples without their
// own `profile` fact inherit the case-level input profile.
func plistSampleProfile(vector *caseData, sample core.Value) (plist.PlistProfile, string) {
	if profile, ok := objectField(sample, "profile"); ok {
		text, ok := profile.(core.String)
		if !ok {
			return plist.PlistProfileXmlV1, "sample profile must be a String"
		}
		switch string(text) {
		case "plist.xml@1":
			return plist.PlistProfileXmlV1, ""
		case "plist.binary@1":
			return plist.PlistProfileBinaryV1, ""
		default:
			return plist.PlistProfileXmlV1, "unknown profile " + string(text)
		}
	}
	return plistProfileOf(vector.Input)
}

func plistFormSample(vector *caseData, sample core.Value) (*plist.Document, string) {
	profile, message := plistSampleProfile(vector, sample)
	if message != "" {
		return nil, message
	}
	bytes, message := plistSourceBytes(sample, profile)
	if message != "" {
		return nil, message
	}
	return plistFormBytes(bytes, profile)
}

// plistFormBytes forms one document under one profile with default limits.
func plistFormBytes(bytes []byte, profile plist.PlistProfile) (*plist.Document, string) {
	document, failure := plist.Parse(bytes, profile, plist.PlistEncodingProfileDefault(),
		plist.DefaultPlistParseLimits())
	if failure != nil {
		return nil, "plist formation failed: " + failure.Code()
	}
	return document, ""
}

// plistExpectedString reads one expected String field.
func plistExpectedString(expected core.Value, name string) (string, bool) {
	return stringField(expected, name)
}

// plistExpectedInteger reads one expected Integer field as int64.
func plistExpectedInteger(expected core.Value, name string) (int64, bool) {
	field, ok := objectField(expected, name)
	if !ok {
		return 0, false
	}
	integer, ok := field.(core.Integer)
	if !ok {
		return 0, false
	}
	number := integer.Int()
	if !number.IsInt64() {
		return 0, false
	}
	return number.Int64(), true
}

// plistExpectedBoolean reads one expected Boolean field.
func plistExpectedBoolean(expected core.Value, name string) (bool, bool) {
	return booleanField(expected, name)
}

// plistExpectedSequence reads one expected Sequence field.
func plistExpectedSequence(expected core.Value, name string) ([]core.Value, bool) {
	return sequenceField(expected, name)
}

// plistExpectedF64 reads one expected numeric fact as an exact double
// (binary float or decimal).
func plistExpectedF64(expected core.Value, name string) (float64, bool) {
	field, ok := objectField(expected, name)
	if !ok {
		return 0, false
	}
	return plistValueF64(field)
}

// plistValueF64 converts one numeric value to its exact double.
func plistValueF64(value core.Value) (float64, bool) {
	switch item := value.(type) {
	case core.BinaryFloat64:
		return math.Float64frombits(uint64(item)), true
	case core.BinaryFloat32:
		return float64(math.Float32frombits(uint32(item))), true
	case core.Decimal:
		return decimalToF64(&item)
	}
	return 0, false
}

// decimalToF64 converts one exact decimal to its double value.
func decimalToF64(decimal *core.Decimal) (float64, bool) {
	coefficient := decimal.Coefficient()
	exponent := decimal.Exponent()
	if !coefficient.IsInt64() || !exponent.IsInt64() {
		return 0, false
	}
	value := float64(coefficient.Int64())
	exponentValue := exponent.Int64()
	if exponentValue > 0 {
		power := exponentValue
		if power > 308 {
			power = 308
		}
		value *= math.Pow10(int(power))
	} else if exponentValue < 0 {
		power := -exponentValue
		if power > 308 {
			power = 308
		}
		value /= math.Pow10(int(power))
	}
	return value, true
}

// plistBitsEqual is the exact bit equality of two doubles.
func plistBitsEqual(left, right float64) bool {
	return math.Float64bits(left) == math.Float64bits(right)
}

func plistStatusName(status document.FormationStatus) string {
	return status.String()
}

// plistAssertExpectedStatus asserts the `expected.status` and optional
// `expected.diagnostic` facts.
func plistAssertExpectedStatus(doc *plist.Document, expected core.Value) string {
	if status, ok := plistExpectedString(expected, "status"); ok {
		if plistStatusName(doc.FormationStatus()) != status {
			return "status " + plistStatusName(doc.FormationStatus()) + " != " + status
		}
	}
	if diagnostic, ok := plistExpectedString(expected, "diagnostic"); ok {
		found := false
		for _, item := range doc.Diagnostics() {
			if item.Code == diagnostic {
				found = true
				break
			}
		}
		if !found {
			return "diagnostic " + diagnostic + " not found"
		}
	}
	return ""
}

// plistRootValue returns the root native value.
func plistRootValue(doc *plist.Document) (plist.PlistValue, string) {
	native := doc.NativeDocument()
	if native == nil {
		return plist.PlistValue{}, "no native document"
	}
	return native.RootValue(), ""
}

// plistDictEntries returns the ordered associations of one dictionary.
func plistDictEntries(value *plist.PlistValue) (plist.PlistDict, string) {
	dict, ok := value.AsDict()
	if !ok {
		return plist.PlistDict{}, "expected dict"
	}
	return dict, ""
}

func plistEntryKeyText(entry plist.PlistDictEntry) (string, string) {
	text, err := entry.Key().ToUnicode()
	if err != nil {
		return "", "key not unicode"
	}
	return text, ""
}

func plistDictKeysOf(doc *plist.Document, value *plist.PlistValue) ([]string, string) {
	dict, message := plistDictEntries(value)
	if message != "" {
		return nil, message
	}
	keys := make([]string, 0, dict.Len())
	for _, entry := range dict.Entries() {
		text, message := plistEntryKeyText(entry)
		if message != "" {
			return nil, message
		}
		keys = append(keys, text)
	}
	return keys, ""
}

// plistEntryByKey returns the associated value of the first entry with the
// given key.
func plistEntryByKey(doc *plist.Document, value *plist.PlistValue, name string) (*plist.PlistValue, string) {
	native := doc.NativeDocument()
	if native == nil {
		return nil, "no native document"
	}
	dict, message := plistDictEntries(value)
	if message != "" {
		return nil, message
	}
	for _, entry := range dict.Entries() {
		text, message := plistEntryKeyText(entry)
		if message != "" {
			return nil, message
		}
		if text == name {
			entryValue, ok := native.Get(entry.Value())
			if !ok {
				return nil, "entry value missing"
			}
			return &entryValue, ""
		}
	}
	return nil, "dict entry " + name + " not found"
}

func plistDuplicateGroupsOf(entries []plist.PlistDictEntry) (int, string) {
	counts := map[string]int{}
	for _, entry := range entries {
		text, message := plistEntryKeyText(entry)
		if message != "" {
			return 0, message
		}
		counts[text]++
	}
	groups := 0
	for _, count := range counts {
		if count > 1 {
			groups++
		}
	}
	return groups, ""
}

func plistValueKindName(value *plist.PlistValue) string {
	return value.Kind().AsStr()
}

func plistValueText(value *plist.PlistValue) (string, bool) {
	stringValue, ok := value.AsString()
	if !ok {
		return "", false
	}
	text, err := stringValue.ToUnicode()
	if err != nil {
		return "", false
	}
	return text, true
}

func plistValueInteger(value *plist.PlistValue) (int64, bool) {
	integer, ok := value.AsInteger()
	if !ok {
		return 0, false
	}
	return integer.Value(), true
}

func plistValueReal(value *plist.PlistValue) (float64, bool) {
	real, ok := value.AsReal()
	if !ok {
		return 0, false
	}
	return real.AsFloat64(), true
}

func plistValueBoolean(value *plist.PlistValue) (bool, bool) {
	boolean, ok := value.AsBoolean()
	if !ok {
		return false, false
	}
	return boolean.Value(), true
}

func plistValueDataHex(value *plist.PlistValue) (string, bool) {
	data, ok := value.AsData()
	if !ok {
		return "", false
	}
	return hex.EncodeToString(data.Bytes()), true
}

func plistValueSeconds(value *plist.PlistValue) (float64, bool) {
	date, ok := value.AsDate()
	if !ok {
		return 0, false
	}
	return date.Seconds(), true
}

// plistCompareScalarValue compares one native scalar against one expected
// portable scalar fact (string, integer, or boolean).
func plistCompareScalarValue(value *plist.PlistValue, expected core.Value) string {
	switch item := expected.(type) {
	case core.String:
		actual, ok := plistValueText(value)
		if !ok || actual != string(item) {
			return "value mismatch"
		}
	case core.Integer:
		number := item.Int()
		if !number.IsInt64() {
			return "unsupported expected scalar"
		}
		actual, ok := plistValueInteger(value)
		if !ok || actual != number.Int64() {
			return "integer value mismatch"
		}
	case core.Boolean:
		actual, ok := plistValueBoolean(value)
		if !ok || actual != bool(item) {
			return "boolean value mismatch"
		}
	default:
		return "unsupported expected scalar"
	}
	return ""
}

// plistAssertStrings compares one string slice against one expected
// sequence of String facts.
func plistAssertStrings(actual []string, expected []core.Value, what string) string {
	if len(actual) != len(expected) {
		return what + " count differs"
	}
	for index := range actual {
		text, ok := expected[index].(core.String)
		if !ok {
			return what + " must be a string"
		}
		if actual[index] != string(text) {
			return what + " differs from expected"
		}
	}
	return ""
}

// plistAssertU64Field asserts one expected integer field against one
// actual unsigned value.
func plistAssertU64Field(expected core.Value, name string, actual uint64) string {
	expectedValue, ok := plistExpectedInteger(expected, name)
	if !ok {
		return ""
	}
	if expectedValue < 0 || uint64(expectedValue) != actual {
		return name + " differs from expected"
	}
	return ""
}

// plistScalarObjects counts the scalar (non-container) objects of one
// binary document.
func plistScalarObjects(doc *plist.Document) int {
	facts := doc.BinaryFacts()
	if facts == nil {
		return 0
	}
	count := 0
	for _, object := range facts.Objects() {
		marker := object.Marker()
		if marker >= 0xA0 && marker <= 0xAF {
			continue
		}
		if marker >= 0xD0 && marker <= 0xDF {
			continue
		}
		count++
	}
	return count
}

// plistNativeValueOf resolves one arena reference.
func plistNativeValueOf(doc *plist.Document, reference plist.PlistValueRef) (*plist.PlistValue, string) {
	native := doc.NativeDocument()
	if native == nil {
		return nil, "no native document"
	}
	value, ok := native.Get(reference)
	if !ok {
		return nil, "arena reference missing"
	}
	return &value, ""
}

// ---------------------------------------------------------------------------
// XML formation
// ---------------------------------------------------------------------------

func runPlistXMLFormation(vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistXMLFormationSamples(vector, array.Items(), report)
		return
	}
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertExpectedStatus(doc, vector.Expected); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() == document.FormationStatusComplete {
		if render, ok := plistExpectedString(vector.Expected, "render"); ok {
			if string(doc.Render()) != render {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "render mismatch"})
				return
			}
		}
		if renderHex, ok := plistExpectedString(vector.Expected, "render_hex"); ok {
			if hex.EncodeToString(doc.Render()) != renderHex {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "render_hex mismatch"})
				return
			}
		}
		if message := plistXMLNativeFacts(doc, vector.Expected); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPlistXMLFormationSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	expected := vector.Expected
	statuses, ok := plistExpectedSequence(expected, "statuses")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.statuses"})
		return
	}
	diagnostics, ok := plistExpectedSequence(expected, "diagnostics")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.diagnostics"})
		return
	}
	if len(samples) != len(statuses) || len(samples) != len(diagnostics) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "status/diagnostic count mismatch"})
		return
	}
	integers, _ := plistExpectedSequence(expected, "integers")
	seconds, _ := plistExpectedSequence(expected, "seconds")
	dataHexes, _ := plistExpectedSequence(expected, "data_hexes")
	values, _ := plistExpectedSequence(expected, "values")
	for index, sample := range samples {
		doc, message := plistFormSample(vector, sample)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		status, ok := statuses[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "status must be a string"})
			return
		}
		if plistStatusName(doc.FormationStatus()) != string(status) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample status mismatch"})
			return
		}
		if code, ok := diagnostics[index].(core.String); ok {
			found := false
			for _, diagnostic := range doc.Diagnostics() {
				if diagnostic.Code == string(code) {
					found = true
					break
				}
			}
			if !found {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample diagnostic " + string(code) + " not found"})
				return
			}
		}
		if string(status) != "Complete" {
			continue
		}
		root, message := plistRootValue(doc)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if integers != nil {
			expectedValue, ok := integers[index].(core.Integer)
			expectedInteger := int64(0)
			if ok && expectedValue.Int().IsInt64() {
				expectedInteger = expectedValue.Int().Int64()
			}
			actual, actualOK := plistValueInteger(&root)
			if actualOK != ok || actual != expectedInteger {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample integer mismatch"})
				return
			}
		}
		if seconds != nil {
			expectedSeconds, ok := plistValueF64(seconds[index])
			actual, actualOK := plistValueSeconds(&root)
			if actualOK != ok || !plistBitsEqual(actual, expectedSeconds) {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample seconds mismatch"})
				return
			}
		}
		if dataHexes != nil {
			expectedHex, ok := dataHexes[index].(core.String)
			actual, actualOK := plistValueDataHex(&root)
			expectedText := ""
			if ok {
				expectedText = string(expectedHex)
			}
			if actualOK != ok || actual != expectedText {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample data hex mismatch"})
				return
			}
		}
		if values != nil {
			expectedValue := values[index]
			text, isText := expectedValue.(core.String)
			if isText && string(text) == "" {
				// An empty expectation admits both empty strings and empty
				// data leaves (`<data></data>` and `<string/>`).
				textValue, hasText := plistValueText(&root)
				empty := (hasText && textValue == "") || func() bool {
					data, hasData := root.AsData()
					return hasData && len(data.Bytes()) == 0
				}()
				if !empty {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "sample value is not empty"})
					return
				}
			} else if isText {
				actual, actualOK := plistValueText(&root)
				if !actualOK || actual != string(text) {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "sample value mismatch"})
					return
				}
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// plistXMLNativeFacts asserts the native-model facts of one complete XML
// formation case.
func plistXMLNativeFacts(doc *plist.Document, expected core.Value) string {
	root, message := plistRootValue(doc)
	if message != "" {
		return message
	}
	if value, ok := plistExpectedString(expected, "root_value"); ok {
		actual, actualOK := plistValueText(&root)
		if !actualOK || actual != value {
			return "root value mismatch"
		}
	}
	if keys, ok := plistExpectedSequence(expected, "keys"); ok {
		actual, message := plistDictKeysOf(doc, &root)
		if message != "" {
			return message
		}
		if message := plistAssertStrings(actual, keys, "key"); message != "" {
			return message
		}
	}
	if associations, ok := plistExpectedInteger(expected, "associations"); ok {
		dict, message := plistDictEntries(&root)
		if message != "" {
			return message
		}
		if int64(dict.Len()) != associations {
			return "associations mismatch"
		}
	}
	if groups, ok := plistExpectedInteger(expected, "duplicate_groups"); ok {
		dict, message := plistDictEntries(&root)
		if message != "" {
			return message
		}
		actual, message := plistDuplicateGroupsOf(dict.Entries())
		if message != "" {
			return message
		}
		if int64(actual) != groups {
			return "duplicate_groups mismatch"
		}
	}
	if values, ok := plistExpectedSequence(expected, "values"); ok {
		dict, message := plistDictEntries(&root)
		if message != "" {
			return message
		}
		if dict.Len() != len(values) {
			return "value count mismatch"
		}
		for index, entry := range dict.Entries() {
			value, message := plistNativeValueOf(doc, entry.Value())
			if message != "" {
				return message
			}
			if message := plistCompareScalarValue(value, values[index]); message != "" {
				return message
			}
		}
	}
	if integerValue, ok := plistExpectedInteger(expected, "integer_value"); ok {
		value, message := plistEntryByKey(doc, &root, "count")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueInteger(value)
		if !actualOK || actual != integerValue {
			return "integer_value mismatch"
		}
	}
	if integerValue, ok := plistExpectedInteger(expected, "negative_integer"); ok {
		value, message := plistEntryByKey(doc, &root, "negative")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueInteger(value)
		if !actualOK || actual != integerValue {
			return "negative_integer mismatch"
		}
	}
	if realValue, ok := plistExpectedF64(expected, "real_value"); ok {
		value, message := plistEntryByKey(doc, &root, "ratio")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueReal(value)
		if !actualOK || !plistBitsEqual(actual, realValue) {
			return "real_value mismatch"
		}
	}
	if dataHex, ok := plistExpectedString(expected, "data_hex"); ok {
		value, message := plistEntryByKey(doc, &root, "payload")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueDataHex(value)
		if !actualOK || actual != dataHex {
			return "data_hex mismatch"
		}
	}
	if seconds, ok := plistExpectedF64(expected, "date_seconds"); ok {
		value, message := plistEntryByKey(doc, &root, "born")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueSeconds(value)
		if !actualOK || !plistBitsEqual(actual, seconds) {
			return "date_seconds mismatch"
		}
	}
	if booleans, ok := plistExpectedSequence(expected, "bool_values"); ok {
		dict, message := plistDictEntries(&root)
		if message != "" {
			return message
		}
		var expectedValues []bool
		for _, item := range booleans {
			if boolean, ok := item.(core.Boolean); ok {
				expectedValues = append(expectedValues, bool(boolean))
			}
		}
		var actualValues []bool
		for _, entry := range dict.Entries() {
			value, message := plistNativeValueOf(doc, entry.Value())
			if message != "" {
				return message
			}
			if boolean, ok := plistValueBoolean(value); ok {
				actualValues = append(actualValues, boolean)
			}
		}
		if len(actualValues) != len(expectedValues) {
			return "bool_values mismatch"
		}
		for index := range actualValues {
			if actualValues[index] != expectedValues[index] {
				return "bool_values mismatch"
			}
		}
	}
	if nested, ok := plistExpectedSequence(expected, "nested_array"); ok {
		arrayValue, message := plistEntryByKey(doc, &root, "tags")
		if message != "" {
			return message
		}
		array, ok := arrayValue.AsArray()
		if !ok {
			return "tags must be an array"
		}
		if array.Len() != len(nested) {
			return "nested array count mismatch"
		}
		for index, element := range array.Elements() {
			value, message := plistNativeValueOf(doc, element)
			if message != "" {
				return message
			}
			if text, ok := nested[index].(core.String); ok {
				actual, actualOK := plistValueText(value)
				if !actualOK || actual != string(text) {
					return "nested element text mismatch"
				}
			} else if _, isObject := nested[index].(*core.Object); isObject {
				dict, isDict := value.AsDict()
				if !isDict || !dict.IsEmpty() {
					return "nested element must be an empty dict"
				}
			} else {
				return "unsupported nested expectation"
			}
		}
	}
	if stringValues, ok := objectField(expected, "string_values"); ok {
		mapping, ok := stringValues.(*core.Object)
		if !ok {
			return "string_values must be an object"
		}
		for _, entry := range mapping.Entries() {
			value, message := plistEntryByKey(doc, &root, entry.Key)
			if message != "" {
				return message
			}
			expectedText, ok := entry.Value.(core.String)
			if !ok {
				return "expected string value"
			}
			actual, actualOK := plistValueText(value)
			if !actualOK || actual != string(expectedText) {
				return "string value mismatch"
			}
		}
	}
	if normalized, ok := plistExpectedBoolean(expected, "line_end_normalized"); ok {
		value, message := plistEntryByKey(doc, &root, "lines")
		if message != "" {
			return message
		}
		text, actualOK := plistValueText(value)
		if !actualOK {
			return "lines value missing"
		}
		hasCR := false
		for _, character := range text {
			if character == '\r' {
				hasCR = true
				break
			}
		}
		if hasCR == normalized {
			return "line-end normalization mismatch"
		}
	}
	needsReals := false
	if _, ok := plistExpectedInteger(expected, "real_count"); ok {
		needsReals = true
	}
	if _, ok := plistExpectedBoolean(expected, "nan_admitted"); ok {
		needsReals = true
	}
	if _, ok := plistExpectedBoolean(expected, "infinities_admitted"); ok {
		needsReals = true
	}
	if _, ok := plistExpectedF64(expected, "exponent_value"); ok {
		needsReals = true
	}
	if needsReals {
		array, ok := root.AsArray()
		if !ok {
			return "root must be an array"
		}
		var reals []*plist.PlistValue
		for _, element := range array.Elements() {
			value, message := plistNativeValueOf(doc, element)
			if message != "" {
				return message
			}
			if _, isReal := value.AsReal(); isReal {
				reals = append(reals, value)
			}
		}
		if count, ok := plistExpectedInteger(expected, "real_count"); ok {
			if int64(len(reals)) != count {
				return "real_count mismatch"
			}
		}
		if admitted, ok := plistExpectedBoolean(expected, "nan_admitted"); ok {
			actual := false
			for _, value := range reals {
				if real, isReal := value.AsReal(); isReal && math.IsNaN(real.AsFloat64()) {
					actual = true
					break
				}
			}
			if actual != admitted {
				return "nan_admitted mismatch"
			}
		}
		if admitted, ok := plistExpectedBoolean(expected, "infinities_admitted"); ok {
			actual := false
			for _, value := range reals {
				if real, isReal := value.AsReal(); isReal && math.IsInf(real.AsFloat64(), 0) {
					actual = true
					break
				}
			}
			if actual != admitted {
				return "infinities_admitted mismatch"
			}
		}
		if exponent, ok := plistExpectedF64(expected, "exponent_value"); ok {
			actual := false
			for _, value := range reals {
				if real, isReal := value.AsReal(); isReal && plistBitsEqual(real.AsFloat64(), exponent) {
					actual = true
					break
				}
			}
			if !actual {
				return "exponent_value mismatch"
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Binary formation
// ---------------------------------------------------------------------------

func runPlistBinaryFormation(vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistBinaryFormationSamples(vector, array.Items(), report)
		return
	}
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertExpectedStatus(doc, vector.Expected); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if facts := doc.BinaryFacts(); facts != nil {
		trailer := facts.Trailer()
		if message := plistAssertU64Field(vector.Expected, "num_objects", trailer.NumObjects()); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertU64Field(vector.Expected, "top_object", trailer.TopObject()); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertU64Field(vector.Expected, "offset_int_size",
			uint64(trailer.OffsetIntSize())); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertU64Field(vector.Expected, "object_ref_size",
			uint64(trailer.ObjectRefSize())); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertU64Field(vector.Expected, "sort_version",
			uint64(trailer.SortVersion())); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertU64Field(vector.Expected, "offset_table_offset",
			trailer.OffsetTableOffset()); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if refsOfTop, ok := plistExpectedSequence(vector.Expected, "refs_of_top"); ok {
			top := int(trailer.TopObject())
			var refs []refPair
			for _, reference := range facts.Refs() {
				if reference.Owner() == top {
					refs = append(refs, refPair{reference.Position(), reference.Target()})
				}
			}
			sortRefPairs(refs)
			actual := make([]int64, 0, len(refs))
			for _, reference := range refs {
				actual = append(actual, int64(reference.target))
			}
			var expectedRefs []int64
			for _, item := range refsOfTop {
				if integer, ok := item.(core.Integer); ok && integer.Int().IsInt64() {
					expectedRefs = append(expectedRefs, integer.Int().Int64())
				}
			}
			if len(actual) != len(expectedRefs) {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "refs_of_top mismatch"})
				return
			}
			for index := range actual {
				if actual[index] != expectedRefs[index] {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "refs_of_top mismatch"})
					return
				}
			}
		}
		if shared, ok := plistExpectedInteger(vector.Expected, "shared_ref_count"); ok {
			counts := map[int]int{}
			for _, reference := range facts.Refs() {
				counts[reference.Target()]++
			}
			sharedCount := int64(0)
			for _, count := range counts {
				if count > 1 {
					sharedCount++
				}
			}
			if sharedCount != shared {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "shared_ref_count mismatch"})
				return
			}
		}
	}
	if doc.FormationStatus() == document.FormationStatusComplete {
		if message := plistBinaryNativeFacts(doc, vector.Expected); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// refPair is one (position, target) reference fact of the top object.
type refPair struct{ position, target int }

func sortRefPairs(refs []refPair) {
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && refs[j].position < refs[j-1].position; j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

// plistBinaryNativeFacts asserts the native-model facts of one complete
// binary formation case.
func plistBinaryNativeFacts(doc *plist.Document, expected core.Value) string {
	root, message := plistRootValue(doc)
	if message != "" {
		return message
	}
	if value, ok := plistExpectedString(expected, "value"); ok {
		actual, actualOK := plistValueText(&root)
		if !actualOK || actual != value {
			return "value mismatch"
		}
	}
	if kind, ok := plistExpectedString(expected, "top_kind"); ok {
		if plistValueKindName(&root) != kind {
			return "top_kind mismatch"
		}
	}
	if keys, ok := plistExpectedSequence(expected, "keys"); ok {
		actual, message := plistDictKeysOf(doc, &root)
		if message != "" {
			return message
		}
		if message := plistAssertStrings(actual, keys, "key"); message != "" {
			return message
		}
	}
	if values, ok := plistExpectedSequence(expected, "values"); ok {
		dict, message := plistDictEntries(&root)
		if message != "" {
			return message
		}
		if dict.Len() != len(values) {
			return "value count mismatch"
		}
		for index, entry := range dict.Entries() {
			value, message := plistNativeValueOf(doc, entry.Value())
			if message != "" {
				return message
			}
			if message := plistCompareScalarValue(value, values[index]); message != "" {
				return message
			}
		}
	}
	if value, ok := plistExpectedInteger(expected, "int_value"); ok {
		entry, message := plistEntryByKey(doc, &root, "int")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueInteger(entry)
		if !actualOK || actual != value {
			return "int_value mismatch"
		}
	}
	if value, ok := plistExpectedF64(expected, "real_value"); ok {
		entry, message := plistEntryByKey(doc, &root, "real")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueReal(entry)
		if !actualOK || !plistBitsEqual(actual, value) {
			return "real_value mismatch"
		}
	}
	if value, ok := plistExpectedF64(expected, "f32_value"); ok {
		entry, message := plistEntryByKey(doc, &root, "f32")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueReal(entry)
		if !actualOK || !plistBitsEqual(actual, value) {
			return "f32_value mismatch"
		}
	}
	if value, ok := plistExpectedString(expected, "data_hex"); ok {
		entry, message := plistEntryByKey(doc, &root, "data")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueDataHex(entry)
		if !actualOK || actual != value {
			return "data_hex mismatch"
		}
	}
	if value, ok := plistExpectedF64(expected, "date_seconds"); ok {
		entry, message := plistEntryByKey(doc, &root, "date")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueSeconds(entry)
		if !actualOK || !plistBitsEqual(actual, value) {
			return "date_seconds mismatch"
		}
	}
	if value, ok := plistExpectedF64(expected, "fractional_seconds"); ok {
		entry, message := plistEntryByKey(doc, &root, "fractional")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueSeconds(entry)
		if !actualOK || !plistBitsEqual(actual, value) {
			return "fractional_seconds mismatch"
		}
	}
	if booleans, ok := plistExpectedSequence(expected, "bool_values"); ok {
		entry, message := plistEntryByKey(doc, &root, "bool")
		if message != "" {
			return message
		}
		var expectedValues []bool
		for _, item := range booleans {
			if boolean, ok := item.(core.Boolean); ok {
				expectedValues = append(expectedValues, bool(boolean))
			}
		}
		var actualValues []bool
		if array, isArray := entry.AsArray(); isArray {
			for _, element := range array.Elements() {
				value, message := plistNativeValueOf(doc, element)
				if message != "" {
					return message
				}
				if boolean, ok := plistValueBoolean(value); ok {
					actualValues = append(actualValues, boolean)
				}
			}
		} else if boolean, ok := plistValueBoolean(entry); ok {
			actualValues = append(actualValues, boolean)
		}
		if len(actualValues) != len(expectedValues) {
			return "bool_values mismatch"
		}
		for index := range actualValues {
			if actualValues[index] != expectedValues[index] {
				return "bool_values mismatch"
			}
		}
	}
	if elements, ok := plistExpectedSequence(expected, "array_elements"); ok {
		entry, message := plistEntryByKey(doc, &root, "array")
		if message != "" {
			return message
		}
		array, isArray := entry.AsArray()
		if !isArray {
			return "array must be an array"
		}
		if array.Len() != len(elements) {
			return "array count mismatch"
		}
		for index, element := range array.Elements() {
			value, message := plistNativeValueOf(doc, element)
			if message != "" {
				return message
			}
			expectedInteger, ok := elements[index].(core.Integer)
			if !ok || !expectedInteger.Int().IsInt64() {
				return "expected element must be an integer"
			}
			actual, actualOK := plistValueInteger(value)
			if !actualOK || actual != expectedInteger.Int().Int64() {
				return "array element mismatch"
			}
		}
	}
	if value, ok := plistExpectedString(expected, "str_value"); ok {
		entry, message := plistEntryByKey(doc, &root, "str")
		if message != "" {
			return message
		}
		actual, actualOK := plistValueText(entry)
		if !actualOK || actual != value {
			return "str_value mismatch"
		}
	}
	return ""
}

// plistWidthNonMinimal reports whether the root scalar object of one binary
// document carries a non-minimal width fact (integers and UIDs, RFC 0013
// §5.3, §5.8).
func plistWidthNonMinimal(doc *plist.Document, root *plist.PlistValue) (bool, bool) {
	facts := doc.BinaryFacts()
	if facts == nil || len(facts.Objects()) == 0 {
		return false, false
	}
	marker := facts.Objects()[0].Marker()
	if integer, ok := plistValueInteger(root); ok {
		width := 1 << (marker & 0x0F)
		minimal := 8
		switch {
		case integer <= 0xFF:
			minimal = 1
		case integer <= 0xFFFF:
			minimal = 2
		case integer <= 0xFFFF_FFFF:
			minimal = 4
		}
		return width > minimal, true
	}
	if uid, ok := root.AsUID(); ok {
		width := int(marker&0x0F) + 1
		value := uid.Value()
		minimal := 4
		switch {
		case value <= 0xFF:
			minimal = 1
		case value <= 0xFFFF:
			minimal = 2
		case value <= 0xFF_FFFF:
			minimal = 3
		}
		return width > minimal, true
	}
	return false, false
}

func runPlistBinaryFormationSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	expected := vector.Expected
	statuses, ok := plistExpectedSequence(expected, "statuses")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.statuses"})
		return
	}
	diagnostics, ok := plistExpectedSequence(expected, "diagnostics")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.diagnostics"})
		return
	}
	if len(samples) != len(statuses) || len(samples) != len(diagnostics) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "status/diagnostic count mismatch"})
		return
	}
	integers, _ := plistExpectedSequence(expected, "integers")
	strings, _ := plistExpectedSequence(expected, "strings")
	uids, _ := plistExpectedSequence(expected, "uids")
	var documents []*plist.Document
	for index, sample := range samples {
		doc, message := plistFormSample(vector, sample)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		status, ok := statuses[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "status must be a string"})
			return
		}
		if plistStatusName(doc.FormationStatus()) != string(status) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sample status mismatch"})
			return
		}
		if code, ok := diagnostics[index].(core.String); ok {
			found := false
			for _, diagnostic := range doc.Diagnostics() {
				if diagnostic.Code == string(code) {
					found = true
					break
				}
			}
			if !found {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "sample diagnostic " + string(code) + " not found"})
				return
			}
		}
		if string(status) == "Complete" {
			root, message := plistRootValue(doc)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			if integers != nil {
				expectedValue, ok := integers[index].(core.Integer)
				expectedInteger := int64(0)
				if ok && expectedValue.Int().IsInt64() {
					expectedInteger = expectedValue.Int().Int64()
				}
				actual, actualOK := plistValueInteger(&root)
				if actualOK != ok || actual != expectedInteger {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "sample integer mismatch"})
					return
				}
			}
			if strings != nil {
				expectedValue, ok := strings[index].(core.String)
				expectedText := ""
				if ok {
					expectedText = string(expectedValue)
				}
				actual, actualOK := plistValueText(&root)
				if actualOK != ok || actual != expectedText {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "sample string mismatch"})
					return
				}
			}
			if uids != nil {
				expectedValue, ok := uids[index].(core.Integer)
				expectedUID := int64(0)
				if ok && expectedValue.Int().IsInt64() {
					expectedUID = expectedValue.Int().Int64()
				}
				actual, actualOK := root.AsUID()
				actualValue := int64(0)
				if actualOK {
					actualValue = int64(actual.Value())
				}
				if actualOK != ok || actualValue != expectedUID {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "sample uid mismatch"})
					return
				}
			}
		}
		documents = append(documents, doc)
	}
	if observed, ok := plistExpectedBoolean(expected, "non_minimal_width_observed"); ok {
		actual := false
		for _, doc := range documents {
			root, message := plistRootValue(doc)
			if message == "" {
				if nonMinimal, has := plistWidthNonMinimal(doc, &root); has && nonMinimal {
					actual = true
					break
				}
			}
		}
		if actual != observed {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "non_minimal_width_observed mismatch"})
			return
		}
	}
	if _, hasUnpairedHex := plistExpectedString(expected, "unpaired_utf16be_hex"); hasUnpairedHex {
		var unpaired *plist.Document
		for _, doc := range documents {
			root, message := plistRootValue(doc)
			if message != "" {
				continue
			}
			if stringValue, ok := root.AsString(); ok &&
				stringValue.Status() == plist.PlistStringUnpairedSurrogate {
				unpaired = doc
				break
			}
		}
		if unpaired == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "no unpaired-surrogate sample"})
			return
		}
		root, _ := plistRootValue(unpaired)
		stringValue, _ := root.AsString()
		if unpairedHex, ok := plistExpectedString(expected, "unpaired_utf16be_hex"); ok {
			if hex.EncodeToString(stringValue.UTF16BEBytes()) != unpairedHex {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "unpaired_utf16be_hex mismatch"})
				return
			}
		}
		if unpairedStatus, ok := plistExpectedString(expected, "unpaired_status"); ok {
			if stringValue.Status().String() != unpairedStatus {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "unpaired_status mismatch"})
				return
			}
		}
	}
	if accepted, ok := plistExpectedBoolean(expected, "sort_version_one_accepted"); ok {
		actual := false
		for _, doc := range documents {
			if doc.FormationStatus() == document.FormationStatusComplete {
				if facts := doc.BinaryFacts(); facts != nil && facts.Trailer().SortVersion() == 1 {
					actual = true
					break
				}
			}
		}
		if actual != accepted {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "sort_version_one_accepted mismatch"})
			return
		}
	}
	_, hasExtendedLength := plistExpectedInteger(expected, "extended_array_length")
	_, hasExtendedObject := plistExpectedBoolean(expected, "extended_count_is_object")
	if hasExtendedLength || hasExtendedObject {
		var complete *plist.Document
		for _, doc := range documents {
			if doc.FormationStatus() == document.FormationStatusComplete {
				complete = doc
				break
			}
		}
		if complete == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "no complete sample"})
			return
		}
		root, _ := plistRootValue(complete)
		if length, ok := plistExpectedInteger(expected, "extended_array_length"); ok {
			array, isArray := root.AsArray()
			if !isArray || int64(array.Len()) != length {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "extended_array_length mismatch"})
				return
			}
		}
		if countIsObject, ok := plistExpectedBoolean(expected, "extended_count_is_object"); ok {
			facts := complete.BinaryFacts()
			if facts == nil || len(facts.Objects()) == 0 {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "missing binary facts"})
				return
			}
			extended := facts.Objects()[0].Marker()&0x0F == 0x0F
			if extended != countIsObject {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "extended_count_is_object mismatch"})
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// ---------------------------------------------------------------------------
// Query
// ---------------------------------------------------------------------------

// plistCapabilities builds the frozen query capability set.
func plistCapabilities() *protocol.CapabilitySet {
	capabilities := protocol.NewCapabilitySet()
	capabilities.Insert(protocol.NewCapabilityId("core.query.ordered-results", 1))
	return capabilities
}

// plistBuildFilters builds the frozen operator vocabulary from one vector
// filter list.
func plistBuildFilters(filters []core.Value) ([]*protocol.OperatorCall, string) {
	calls := make([]*protocol.OperatorCall, 0, len(filters))
	for _, filter := range filters {
		operator, ok := stringField(filter, "operator")
		if !ok {
			return nil, "missing filter.operator"
		}
		name, versionText, found := splitOperator(operator)
		if !found {
			return nil, "operator lacks version: " + operator
		}
		version, err := parseUint32(versionText)
		if err != nil {
			return nil, "invalid operator version: " + operator
		}
		call := protocol.NewOperatorCall(name, version)
		if argument, ok := stringField(filter, "argument"); ok {
			switch name {
			case "plist.dict-key-equals":
				call.WithArgument("key", core.String(argument))
			case "plist.value-type-is":
				call.WithArgument("kind", core.String(argument))
			default:
				call.WithArgument("argument", core.String(argument))
			}
		}
		calls = append(calls, call)
	}
	return calls, ""
}

// splitOperator splits one `name@version` operator spelling.
func splitOperator(operator string) (string, string, bool) {
	for index := 0; index < len(operator); index++ {
		if operator[index] == '@' {
			return operator[:index], operator[index+1:], true
		}
	}
	return "", "", false
}

func parseUint32(text string) (uint32, error) {
	value, ok := new(big.Int).SetString(text, 10)
	if !ok || value.Sign() < 0 || value.BitLen() > 32 {
		return 0, errInvalidUint32
	}
	return uint32(value.Uint64()), nil
}

var errInvalidUint32 = errInvalidUint32Type{}

type errInvalidUint32Type struct{}

func (errInvalidUint32Type) Error() string { return "invalid uint32" }

// plistExecuteNative executes one validated native query chain.
func plistExecuteNative(doc *plist.Document, calls []*protocol.OperatorCall) ([]plist.PlistMatch, *protocol.QueryFailure) {
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, call := range calls {
		expression = expression.Then(call)
	}
	definition := protocol.NewQueryDefinition(protocol.DomainPlistNativeV1()).
		WithExpression(expression).WithSelection(protocol.SelectionAll)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, failure
	}
	executable, failure := validated.Bind(plistCapabilities())
	if failure != nil {
		return nil, failure
	}
	return plist.ExecutePlistNativeQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
}

// plistQueryFailureCode is the stable vector spelling of one QueryFailure
// kind.
func plistQueryFailureCode(failure *protocol.QueryFailure) string {
	switch failure.Kind {
	case protocol.FailureDomainMismatch:
		return "plist.query.domain-mismatch@1"
	case protocol.FailureUnknownOperator:
		return "plist.query.unknown-operator@1"
	case protocol.FailureWrongArgumentType:
		return "plist.query.wrong-argument-type@1"
	case protocol.FailureInvalidArgument:
		return "plist.query.invalid-argument@1"
	case protocol.FailureInvalidOperatorComposition:
		return "plist.query.invalid-composition@1"
	case protocol.FailureMissingCapability:
		return "plist.query.missing-capability@1"
	case protocol.FailureRequiredTypeMismatch:
		return "plist.query.type-mismatch@1"
	case protocol.FailureCardinalityViolation:
		return "plist.query.cardinality-violation@1"
	case protocol.FailureResourceLimit:
		return "plist.query.resource-limit@1"
	case protocol.FailureCancelled:
		return "plist.query.cancelled@1"
	case protocol.FailureTargetUnavailable:
		return "plist.query.target-unavailable@1"
	}
	return "plist.query.invalid-argument@1"
}

func plistDictEntryKeys(matches []plist.PlistMatch) ([]string, string) {
	keys := make([]string, 0, len(matches))
	for _, item := range matches {
		if item.Kind == plist.PlistMatchDictEntry && item.Key != nil {
			text, err := item.Key.ToUnicode()
			if err != nil {
				return nil, "key not unicode"
			}
			keys = append(keys, text)
		}
	}
	return keys, ""
}

func plistDuplicateKeyGroups(matches []plist.PlistMatch) (int, string) {
	keys, message := plistDictEntryKeys(matches)
	if message != "" {
		return 0, message
	}
	counts := map[string]int{}
	for _, key := range keys {
		counts[key]++
	}
	groups := 0
	for _, count := range counts {
		if count > 1 {
			groups++
		}
	}
	return groups, ""
}

// plistMatchPayload is the value payload of one native-domain match, when
// it carries one.
func plistMatchPayload(match *plist.PlistMatch) (*plist.PlistValueRef, plist.PlistValueKind, bool) {
	switch match.Kind {
	case plist.PlistMatchValue:
		if match.Value != nil && match.ValueKind != nil {
			return match.Value, *match.ValueKind, true
		}
	case plist.PlistMatchDictEntry, plist.PlistMatchArrayElement:
		if match.Value != nil && match.ValueKind != nil {
			return match.Value, *match.ValueKind, true
		}
	}
	return nil, 0, false
}

func runPlistQuery(vector *caseData, report *SuiteReport) {
	domain, ok := stringField(vector.Input, "domain")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.domain"})
		return
	}
	switch domain {
	case "plist.native-semantic-query@1":
		runPlistNativeQuery(vector, report)
	case "plist.binary-structure-query@1":
		runPlistBinaryStructureQuery(vector, report)
	default:
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unknown query domain " + domain})
	}
}

// plistAssertTypedMatches asserts one typed match list against its
// `{kind, ...}` expectation sequence.
func plistAssertTypedMatches(doc *plist.Document, matches []plist.PlistMatch,
	expectedMatches []core.Value) string {
	if len(matches) != len(expectedMatches) {
		return "match count differs from expected"
	}
	for index, actual := range matches {
		expectedMatch := expectedMatches[index]
		expectedKind, ok := stringField(expectedMatch, "kind")
		if !ok {
			return "missing expected match kind"
		}
		reference, kind, has := plistMatchPayload(&actual)
		if !has {
			return "match without value payload"
		}
		if kind.AsStr() != expectedKind {
			return "typed match kind mismatch"
		}
		value, message := plistNativeValueOf(doc, *reference)
		if message != "" {
			return message
		}
		if expectedValue, ok := objectField(expectedMatch, "value"); ok {
			if integer, ok := expectedValue.(core.Integer); ok && integer.Int().IsInt64() {
				actualInteger, actualOK := plistValueInteger(value)
				if !actualOK || actualInteger != integer.Int().Int64() {
					return "typed match integer mismatch"
				}
			}
		}
		if expectedSeconds, ok := objectField(expectedMatch, "seconds"); ok {
			if seconds, ok := plistValueF64(expectedSeconds); ok {
				actualSeconds, actualOK := plistValueSeconds(value)
				if !actualOK || !plistBitsEqual(actualSeconds, seconds) {
					return "typed match date seconds mismatch"
				}
			}
		}
	}
	return ""
}

func runPlistNativeQuery(vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistNativeQuerySamples(vector, array.Items(), report)
		return
	}
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "native-query input must form completely"})
		return
	}
	filters, ok := sequenceField(vector.Input, "filters")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.filters"})
		return
	}
	calls, message := plistBuildFilters(filters)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	matches, failure := plistExecuteNative(doc, calls)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "execute: " + failure.Code()})
		return
	}
	terminal, ok := plistExpectedString(vector.Expected, "terminal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.terminal"})
		return
	}
	if terminal != "Completed" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "terminal " + terminal + " != Completed"})
		return
	}
	if keys, ok := plistExpectedSequence(vector.Expected, "keys"); ok {
		actual, message := plistDictEntryKeys(matches)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertStrings(actual, keys, "key"); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
	}
	if valueTypes, ok := plistExpectedSequence(vector.Expected, "value_types"); ok {
		var actual []string
		for _, item := range matches {
			if item.Kind == plist.PlistMatchDictEntry && item.ValueKind != nil {
				actual = append(actual, item.ValueKind.AsStr())
			}
		}
		var expectedTypes []string
		for _, item := range valueTypes {
			if text, ok := item.(core.String); ok {
				expectedTypes = append(expectedTypes, string(text))
			}
		}
		if len(actual) != len(expectedTypes) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "value_types mismatch"})
			return
		}
		for index := range actual {
			if actual[index] != expectedTypes[index] {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "value_types mismatch"})
				return
			}
		}
	}
	if groups, ok := plistExpectedInteger(vector.Expected, "duplicate_groups"); ok {
		actual, message := plistDuplicateKeyGroups(matches)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if int64(actual) != groups {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "duplicate_groups mismatch"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPlistNativeQuerySamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "native-query input must form completely"})
		return
	}
	terminals, ok := plistExpectedSequence(vector.Expected, "terminals")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.terminals"})
		return
	}
	if len(samples) != len(terminals) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "terminal count mismatch"})
		return
	}
	mismatchCode, _ := plistExpectedString(vector.Expected, "mismatch_code")
	integerMatches, _ := plistExpectedSequence(vector.Expected, "integer_matches")
	dateMatches, _ := plistExpectedSequence(vector.Expected, "date_matches")
	for index, sample := range samples {
		filters, ok := sequenceField(sample, "filters")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing sample filters"})
			return
		}
		lastOperator := ""
		if len(filters) > 0 {
			lastOperator, _ = stringField(filters[len(filters)-1], "operator")
		}
		calls, message := plistBuildFilters(filters)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		terminal, ok := terminals[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "terminal must be a string"})
			return
		}
		switch string(terminal) {
		case "Completed":
			matches, failure := plistExecuteNative(doc, calls)
			if failure != nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "execute: " + failure.Code()})
				return
			}
			if lastOperator == "plist.value-as-integer@1" && integerMatches != nil {
				if message := plistAssertTypedMatches(doc, matches, integerMatches); message != "" {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: message})
					return
				}
			} else if lastOperator == "plist.value-as-date@1" && dateMatches != nil {
				if message := plistAssertTypedMatches(doc, matches, dateMatches); message != "" {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: message})
					return
				}
			}
		case "Failed":
			_, failure := plistExecuteNative(doc, calls)
			if failure == nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "execution must fail"})
				return
			}
			if plistQueryFailureCode(failure) != mismatchCode {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "query failure code mismatch"})
				return
			}
		default:
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "unknown terminal " + string(terminal)})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// trailerFacts is the captured trailer field facts of one structure query.
type trailerFacts struct {
	sortVersion       byte
	offsetIntSize     byte
	objectRefSize     byte
	numObjects        uint64
	topObject         uint64
	offsetTableOffset uint64
}

// plistExecuteBinaryStructure executes one validated binary-structure query
// chain.
func plistExecuteBinaryStructure(calls []*protocol.OperatorCall,
	doc *plist.Document) ([]plist.PlistBinaryMatch, *protocol.QueryFailure) {
	expression := &protocol.QueryExpression{Kind: protocol.ExpressionInput}
	for _, call := range calls {
		expression = expression.Then(call)
	}
	definition := protocol.NewQueryDefinition(protocol.DomainPlistBinaryStructureV1()).
		WithExpression(expression).WithSelection(protocol.SelectionAll)
	validated, failure := definition.Validate()
	if failure != nil {
		return nil, failure
	}
	executable, failure := validated.Bind(plistCapabilities())
	if failure != nil {
		return nil, failure
	}
	return plist.ExecutePlistBinaryQuery(context.Background(), executable, doc,
		protocol.DefaultQueryLimits())
}

func runPlistBinaryStructureQuery(vector *caseData, report *SuiteReport) {
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "binary-structure-query input must form completely"})
		return
	}
	filters, ok := sequenceField(vector.Input, "filters")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.filters"})
		return
	}
	calls, message := plistBuildFilters(filters)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	terminal, ok := plistExpectedString(vector.Expected, "terminal")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.terminal"})
		return
	}
	// Composition: the full chain validates, binds, and executes (RFC 0013
	// §8.3) before any fact is asserted.
	_, failure := plistExecuteBinaryStructure(calls, doc)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "execute: " + failure.Code()})
		return
	}
	if terminal != "Completed" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "terminal " + terminal + " != Completed"})
		return
	}
	// Facts: every structure operator projects its document-level fact set
	// once from any binary-structure input match, so each filter is also
	// executed standalone and its facts collected.
	var trailer *trailerFacts
	var objects []objectFact
	var offsets []offsetFact
	topMarker := byte(0)
	hasTopMarker := false
	var topRefs []int
	for _, call := range calls {
		matches, failure := plistExecuteBinaryStructure([]*protocol.OperatorCall{call}, doc)
		if failure != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "execute: " + failure.Code()})
			return
		}
		for _, item := range matches {
			switch item.Kind {
			case plist.PlistBinaryMatchTrailer:
				facts := trailerFacts{
					sortVersion: item.SortVersion, offsetIntSize: item.OffsetIntSize,
					objectRefSize: item.ObjectRefSize, numObjects: item.NumObjects,
					topObject: item.TopObject, offsetTableOffset: item.OffsetTableOffset,
				}
				trailer = &facts
			case plist.PlistBinaryMatchObject:
				objects = append(objects, objectFact{item.Index, item.Marker})
			case plist.PlistBinaryMatchOffset:
				offsets = append(offsets, offsetFact{item.Index, item.Offset})
			case plist.PlistBinaryMatchTopObject:
				topMarker = item.Marker
				hasTopMarker = true
				topRefs = nil
				for _, reference := range item.Refs {
					topRefs = append(topRefs, reference.Target())
				}
			}
		}
	}
	if trailer == nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing trailer facts match"})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "num_objects", trailer.numObjects); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "top_object", trailer.topObject); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "offset_int_size",
		uint64(trailer.offsetIntSize)); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "object_ref_size",
		uint64(trailer.objectRefSize)); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "sort_version",
		uint64(trailer.sortVersion)); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if message := plistAssertU64Field(vector.Expected, "offset_table_offset",
		trailer.offsetTableOffset); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	sortObjectFacts(objects)
	sortOffsetFacts(offsets)
	if objectOffsets, ok := plistExpectedSequence(vector.Expected, "object_offsets"); ok {
		expected := make([]int64, 0, len(objectOffsets))
		for _, item := range objectOffsets {
			if integer, ok := item.(core.Integer); ok && integer.Int().IsInt64() {
				expected = append(expected, integer.Int().Int64())
			}
		}
		actual := make([]int64, 0, len(offsets))
		for _, offset := range offsets {
			actual = append(actual, int64(offset.offset))
		}
		if len(actual) != len(expected) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "object_offsets mismatch"})
			return
		}
		for index := range actual {
			if actual[index] != expected[index] {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "object_offsets mismatch"})
				return
			}
		}
	}
	if markers, ok := plistExpectedSequence(vector.Expected, "markers"); ok {
		expected := make([]string, 0, len(markers))
		for _, item := range markers {
			if text, ok := item.(core.String); ok {
				expected = append(expected, string(text))
			}
		}
		actual := make([]string, 0, len(objects))
		for _, object := range objects {
			actual = append(actual, hex.EncodeToString([]byte{object.marker}))
		}
		if len(actual) != len(expected) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "markers mismatch"})
			return
		}
		for index := range actual {
			if actual[index] != expected[index] {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "markers mismatch"})
				return
			}
		}
	}
	if marker, ok := plistExpectedString(vector.Expected, "top_marker"); ok {
		if !hasTopMarker || hex.EncodeToString([]byte{topMarker}) != marker {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "top_marker mismatch"})
			return
		}
	}
	if refs, ok := plistExpectedSequence(vector.Expected, "top_refs"); ok {
		expected := make([]int64, 0, len(refs))
		for _, item := range refs {
			if integer, ok := item.(core.Integer); ok && integer.Int().IsInt64() {
				expected = append(expected, integer.Int().Int64())
			}
		}
		actual := make([]int64, 0, len(topRefs))
		for _, target := range topRefs {
			actual = append(actual, int64(target))
		}
		if len(actual) != len(expected) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "top_refs mismatch"})
			return
		}
		for index := range actual {
			if actual[index] != expected[index] {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "top_refs mismatch"})
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

type objectFact struct {
	index  int
	marker byte
}

func sortObjectFacts(objects []objectFact) {
	for i := 1; i < len(objects); i++ {
		for j := i; j > 0 && objects[j].index < objects[j-1].index; j-- {
			objects[j], objects[j-1] = objects[j-1], objects[j]
		}
	}
}

type offsetFact struct{ index, offset int }

func sortOffsetFacts(offsets []offsetFact) {
	for i := 1; i < len(offsets); i++ {
		for j := i; j > 0 && offsets[j].index < offsets[j-1].index; j-- {
			offsets[j], offsets[j-1] = offsets[j-1], offsets[j]
		}
	}
}

// ---------------------------------------------------------------------------
// Projection
// ---------------------------------------------------------------------------

// plistPortableKindName is the stable kind name of one projected portable
// value.
func plistPortableKindName(value core.Value) (string, bool) {
	switch item := value.(type) {
	case *core.EntryMapping:
		return "dict", true
	case *core.Array:
		return "array", true
	case core.String:
		return "string", true
	case core.Integer:
		return "integer", true
	case core.BinaryFloat64, core.BinaryFloat32:
		return "real", true
	case core.Boolean:
		return "boolean", true
	case core.Bytes:
		return "data", true
	case *core.Object:
		if _, hasSeconds := objectField(item, "seconds"); hasSeconds {
			return "date", true
		}
		if _, hasUID := objectField(item, "uid"); hasUID {
			return "uid", true
		}
	}
	return "", false
}

// plistAssertLeaf asserts one projected leaf against its `{kind, ...}`
// expectation.
func plistAssertLeaf(actual core.Value, expected core.Value) string {
	kind, ok := stringField(expected, "kind")
	if !ok {
		return "missing leaf kind"
	}
	actualKind, ok := plistPortableKindName(actual)
	if !ok || actualKind != kind {
		return "leaf kind mismatch"
	}
	switch kind {
	case "string":
		text, ok := stringField(expected, "text")
		if !ok {
			return "missing leaf text"
		}
		if actualText, ok := actual.(core.String); !ok || string(actualText) != text {
			return "leaf text mismatch"
		}
	case "integer":
		expectedValue, ok := objectField(expected, "value")
		if !ok {
			return "missing leaf integer"
		}
		expectedInteger, ok := expectedValue.(core.Integer)
		if !ok || !expectedInteger.Int().IsInt64() {
			return "missing leaf integer"
		}
		actualInteger, ok := actual.(core.Integer)
		if !ok || !actualInteger.Int().IsInt64() ||
			actualInteger.Int().Int64() != expectedInteger.Int().Int64() {
			return "leaf integer mismatch"
		}
	case "real":
		expectedValue, ok := objectField(expected, "value")
		if !ok {
			return "missing leaf real"
		}
		expectedF64, ok := plistValueF64(expectedValue)
		if !ok {
			return "missing leaf real"
		}
		actualF64, ok := plistValueF64(actual)
		if !ok || !plistBitsEqual(actualF64, expectedF64) {
			return "leaf real mismatch"
		}
	case "boolean":
		expectedValue, ok := objectField(expected, "value")
		if !ok {
			return "missing leaf boolean"
		}
		expectedBoolean, ok := expectedValue.(core.Boolean)
		if !ok {
			return "missing leaf boolean"
		}
		actualBoolean, ok := actual.(core.Boolean)
		if !ok || actualBoolean != expectedBoolean {
			return "leaf boolean mismatch"
		}
	case "data":
		expectedHex, ok := stringField(expected, "hex")
		if !ok {
			return "missing leaf hex"
		}
		actualBytes, ok := actual.(core.Bytes)
		if !ok || hex.EncodeToString(actualBytes) != expectedHex {
			return "leaf data hex mismatch"
		}
	case "date":
		expectedSeconds, ok := objectField(expected, "seconds")
		if !ok {
			return "missing leaf seconds"
		}
		expectedF64, ok := plistValueF64(expectedSeconds)
		if !ok {
			return "missing leaf seconds"
		}
		actualObject, ok := actual.(*core.Object)
		if !ok {
			return "actual leaf date missing"
		}
		actualSeconds, ok := objectField(actualObject, "seconds")
		if !ok {
			return "actual leaf date missing"
		}
		actualF64, ok := plistValueF64(actualSeconds)
		if !ok || !plistBitsEqual(actualF64, expectedF64) {
			return "leaf date seconds mismatch"
		}
	default:
		return "unknown leaf kind " + kind
	}
	return ""
}

// plistProjectionRequest builds the projection request of one sample or the
// case-level default.
func plistProjectionRequest(value core.Value) plist.ProjectionRequest {
	collision, _ := stringField(value, "collision_policy")
	switch collision {
	case "Reject":
		return plist.NewProjectionRequestRequireObject(plist.CollisionPolicyReject)
	case "First":
		return plist.NewProjectionRequestRequireObject(plist.CollisionPolicyFirst)
	case "Last":
		return plist.NewProjectionRequestRequireObject(plist.CollisionPolicyLast)
	}
	return plist.NewProjectionRequestValueTree()
}

func runPlistProjection(vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistProjectionSamples(vector, array.Items(), report)
		return
	}
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	result := plist.Project(doc, plist.NewProjectionRequestValueTree())
	if result.Failed != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "projection must complete"})
		return
	}
	projection := result.Complete
	if record, ok := plistExpectedString(vector.Expected, "record"); ok {
		actual, ok := objectField(projection.Value, "record")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing record member"})
			return
		}
		recordText, ok := actual.(core.String)
		if !ok || string(recordText) != record {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "record mismatch"})
			return
		}
	}
	rootValue, ok := objectField(projection.Value, "root")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing root member"})
		return
	}
	if kind, ok := plistExpectedString(vector.Expected, "root_kind"); ok {
		actual, has := plistPortableKindName(rootValue)
		if !has || actual != kind {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "root_kind mismatch"})
			return
		}
	}
	if keys, ok := plistExpectedSequence(vector.Expected, "keys"); ok {
		mapping, ok := rootValue.(*core.EntryMapping)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "root must be an entry mapping"})
			return
		}
		actual := make([]string, 0, mapping.Len())
		for _, entry := range mapping.Entries() {
			if text, ok := entry.Key.(core.String); ok {
				actual = append(actual, string(text))
			}
		}
		expectedKeys := make([]string, 0, len(keys))
		for _, item := range keys {
			if text, ok := item.(core.String); ok {
				expectedKeys = append(expectedKeys, string(text))
			}
		}
		if len(actual) != len(expectedKeys) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "keys mismatch"})
			return
		}
		for index := range actual {
			if actual[index] != expectedKeys[index] {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "keys mismatch"})
				return
			}
		}
	}
	if leaves, ok := objectField(vector.Expected, "leaves"); ok {
		leavesObject, ok := leaves.(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "leaves must be an object"})
			return
		}
		mapping, ok := rootValue.(*core.EntryMapping)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "root must be an entry mapping"})
			return
		}
		for _, leaf := range leavesObject.Entries() {
			entry := findMappingEntry(mapping, leaf.Key)
			if entry == nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "leaf entry " + leaf.Key + " missing"})
				return
			}
			if message := plistAssertLeaf(entry.Value, leaf.Value); message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: message})
				return
			}
		}
	}
	if arrayLeaves, ok := objectField(vector.Expected, "array_leaves"); ok {
		arrayLeavesObject, ok := arrayLeaves.(*core.Object)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "array_leaves must be an object"})
			return
		}
		mapping, ok := rootValue.(*core.EntryMapping)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "root must be an entry mapping"})
			return
		}
		for _, leaf := range arrayLeavesObject.Entries() {
			entry := findMappingEntry(mapping, leaf.Key)
			if entry == nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "array leaf entry " + leaf.Key + " missing"})
				return
			}
			elements, ok := entry.Value.(*core.Array)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "array leaf must be a sequence"})
				return
			}
			expectedElements, ok := leaf.Value.(*core.Array)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "expected array leaf must be a sequence"})
				return
			}
			if elements.Len() != expectedElements.Len() {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "array leaf count mismatch"})
				return
			}
			for index := range elements.Items() {
				expectedText, ok := expectedElements.Items()[index].(core.String)
				if !ok {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "array leaf element must be a string"})
					return
				}
				actualText, ok := elements.Items()[index].(core.String)
				if !ok || actualText != expectedText {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "array leaf element mismatch"})
					return
				}
			}
		}
	}
	if preserved, ok := plistExpectedBoolean(vector.Expected, "association_order_preserved"); ok {
		if !preserved {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "association order not preserved"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// findMappingEntry finds one entry mapping association by exact key.
func findMappingEntry(mapping *core.EntryMapping, key string) *core.EntryMappingEntry {
	for index := range mapping.Entries() {
		entry := &mapping.Entries()[index]
		if text, ok := entry.Key.(core.String); ok && string(text) == key {
			return entry
		}
	}
	return nil
}

func runPlistProjectionSamples(vector *caseData, samples []core.Value, report *SuiteReport) {
	fidelities, _ := plistExpectedSequence(vector.Expected, "fidelities")
	codes, _ := plistExpectedSequence(vector.Expected, "codes")
	eventsAfterFirst, _ := plistExpectedInteger(vector.Expected, "events_after_first")
	firstCompletedChecked := false
	for index, sample := range samples {
		doc, message := plistFormSample(vector, sample)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		request := plistProjectionRequest(sample)
		result := plist.Project(doc, request)
		if fidelities != nil {
			expectedFidelity, ok := fidelities[index].(core.String)
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "fidelity must be a string"})
				return
			}
			fidelityOK := (result.Failed != nil && string(expectedFidelity) == "Failed") ||
				(result.Complete != nil &&
					(string(expectedFidelity) == "Transformed" || string(expectedFidelity) == "Exact"))
			if !fidelityOK {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "projection fidelity mismatch"})
				return
			}
		}
		if codes != nil {
			if expectedCode, ok := codes[index].(core.String); ok {
				if result.Complete != nil {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "projection must fail"})
					return
				}
				if len(result.Failed.Diagnostics) == 0 ||
					result.Failed.Diagnostics[0].Code != string(expectedCode) {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "projection code mismatch"})
					return
				}
			}
		}
		if result.Complete != nil && !firstCompletedChecked {
			firstCompletedChecked = true
			if firstSample, ok := objectField(vector.Expected, "first_sample"); ok {
				keys, ok := sequenceField(firstSample, "keys")
				if !ok {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "missing first_sample keys"})
					return
				}
				values, ok := sequenceField(firstSample, "values")
				if !ok {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "missing first_sample values"})
					return
				}
				object, ok := result.Complete.Value.(*core.Object)
				if !ok {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "require-object projection must be an object"})
					return
				}
				if object.Len() != len(keys) {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "first_sample key count mismatch"})
					return
				}
				for position, entry := range object.Entries() {
					expectedKey, ok := keys[position].(core.String)
					if !ok {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "expected key must be a string"})
						return
					}
					expectedValue, ok := values[position].(core.String)
					if !ok {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "expected value must be a string"})
						return
					}
					actualValue, ok := entry.Value.(core.String)
					if entry.Key != string(expectedKey) || !ok || actualValue != expectedValue {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "first_sample mismatch"})
						return
					}
				}
			}
			if eventsAfterFirst > 0 {
				events := 0
				for _, event := range result.Complete.Report.Events() {
					if event.Kind == plist.ProjectionEventAssociationDiscarded {
						events++
					}
				}
				if int64(events) != eventsAfterFirst {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "events_after_first mismatch"})
					return
				}
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// ---------------------------------------------------------------------------
// Materialization
// ---------------------------------------------------------------------------

// plistMaterializationRequest builds the frozen request of one style.
func plistMaterializationRequest(style string) (document.MaterializationRequest, string) {
	switch style {
	case "plist.xml-canonical@1":
		return document.NewMaterializationRequest(
			document.NewProfileId("plist.xml", 1),
			document.NewMaterializationStyleId("plist.xml-canonical", 1)), ""
	case "plist.binary-canonical@1":
		return document.NewMaterializationRequest(
			document.NewProfileId("plist.binary", 1),
			document.NewMaterializationStyleId("plist.binary-canonical", 1),
		).WithEncoding(document.BinaryEncoding()).WithNewline(document.NewlineNone), ""
	default:
		return document.MaterializationRequest{}, "unknown materialization style " + style
	}
}

// plistMaterializationFailureCode is the stable vector spelling of one
// materialization failure.
func plistMaterializationFailureCode(failure *plist.MaterializationFailure) string {
	return failure.Code()
}

// plistOrderedRecords re-reads the raw plist vector file with an
// order-preserving decoder and extracts the ordered `input.record` values
// of one case, keyed by case id. The shared runner loader sorts Object
// members (Go map iteration), which would destroy the materialization
// record's ordered association facts; the vector's record member order is a
// materialization input fact (RFC 0013 §9), so the plist runner decodes it
// directly from the raw file.
func plistOrderedRecords(runner *Runner, vector *caseData) (map[string]core.Value, string) {
	path := filepath.Join(runner.VectorsDir, "plist-v1.json")
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, "plist-v1.json is unreadable: " + err.Error()
	}
	decoder := json.NewDecoder(strings.NewReader(string(bytes)))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, "plist-v1.json root must be an object"
	}
	records := map[string]core.Value{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, "plist-v1.json key error"
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, "plist-v1.json key must be a string"
		}
		if key != "cases" {
			value, err := decodeOrderedJSONValue(decoder)
			if err != nil {
				return nil, "plist-v1.json value error"
			}
			_ = value
			continue
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
			return nil, "plist-v1.json cases must be a sequence"
		}
		for decoder.More() {
			if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
				return nil, "plist-v1.json case must be an object"
			}
			var caseID string
			var input *core.Object
			var record core.Value
			var hasRecord bool
			for decoder.More() {
				fieldToken, err := decoder.Token()
				if err != nil {
					return nil, "plist-v1.json case field error"
				}
				field, ok := fieldToken.(string)
				if !ok {
					return nil, "plist-v1.json case field must be a string"
				}
				value, err := decodeOrderedJSONValue(decoder)
				if err != nil {
					return nil, "plist-v1.json case value error"
				}
				switch field {
				case "id":
					if text, ok := value.(core.String); ok {
						caseID = string(text)
					}
				case "input":
					if object, ok := value.(*core.Object); ok {
						input = object
					}
				}
			}
			if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
				return nil, "plist-v1.json case close error"
			}
			if input != nil {
				if field, ok := input.Get("record"); ok {
					record = field
					hasRecord = true
				}
			}
			if hasRecord {
				records[caseID] = record
			}
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return nil, "plist-v1.json cases close error"
		}
	}
	return records, ""
}

// decodeOrderedJSONValue decodes one JSON value preserving Object member
// order (the vector file is the authority and never goes through the
// canonical transport decoder).
func decodeOrderedJSONValue(decoder *json.Decoder) (core.Value, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch item := token.(type) {
	case json.Delim:
		switch item {
		case '{':
			var entries []core.Entry
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errInvalidVectorKey
				}
				value, err := decodeOrderedJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				entries = append(entries, core.Entry{Key: key, Value: value})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return core.NewObject(entries...)
		case '[':
			var items []core.Value
			for decoder.More() {
				value, err := decodeOrderedJSONValue(decoder)
				if err != nil {
					return nil, err
				}
				items = append(items, value)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return core.NewArray(items...), nil
		}
	case string:
		return core.String(item), nil
	case json.Number:
		value, err := numberTextValue(item.String())
		if err != nil {
			return nil, err
		}
		return value, nil
	case bool:
		return core.Boolean(item), nil
	case nil:
		return core.NullValue(), nil
	}
	return nil, errInvalidVectorKey
}

// errInvalidVectorKey is the ordered-decode failure sentinel.
var errInvalidVectorKey = errInvalidVectorKeyType{}

type errInvalidVectorKeyType struct{}

func (errInvalidVectorKeyType) Error() string { return "invalid vector key" }

// plistCompleteMaterialization completes one materialization.
func plistCompleteMaterialization(record core.Value,
	request document.MaterializationRequest) (*plist.CompleteMaterialization, string) {
	result := plist.Materialize(record, request)
	if result.Failed != nil {
		return nil, "materialization failed: " + result.Failed.Failure.Code()
	}
	return result.Complete, ""
}

// plistAddTruncatePolicy copies one record object with the truncation
// policy member added.
func plistAddTruncatePolicy(record core.Value, policy core.Value) (core.Value, string) {
	object, ok := record.(*core.Object)
	if !ok {
		return nil, "record must be an object"
	}
	entries := make([]core.Entry, 0, object.Len()+1)
	for _, entry := range object.Entries() {
		entries = append(entries, entry)
	}
	entries = append(entries, core.Entry{Key: "truncate_policy", Value: policy})
	built, err := core.NewObject(entries...)
	if err != nil {
		return nil, "record insert"
	}
	return built, ""
}

func runPlistMaterialization(runner *Runner, vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistMaterializationSamples(runner, vector, array.Items(), report)
		return
	}
	style, ok := stringField(vector.Input, "style")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.style"})
		return
	}
	record, ok := objectField(vector.Input, "record")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.record"})
		return
	}
	// The shared loader sorts Object members; the vector's record member
	// order is a materialization input fact, so the record is decoded from
	// the raw file order-preserving.
	if ordered, message := plistOrderedRecords(runner, vector); message == "" {
		if decoded, exists := ordered[vector.ID]; exists {
			record = decoded
		}
	}
	request, message := plistMaterializationRequest(style)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	complete, message := plistCompleteMaterialization(record, request)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if closure, ok := plistExpectedBoolean(vector.Expected, "closure"); ok && closure {
		if complete.Document.FormationStatus() != document.FormationStatusComplete {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "materialized document must be complete"})
			return
		}
	}
	if render, ok := plistExpectedString(vector.Expected, "render"); ok {
		if string(complete.Document.Render()) != render {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "render mismatch"})
			return
		}
	}
	if renderHex, ok := plistExpectedString(vector.Expected, "render_hex"); ok {
		if hex.EncodeToString(complete.Document.Render()) != renderHex {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "render_hex mismatch"})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPlistMaterializationSamples(runner *Runner, vector *caseData, samples []core.Value, report *SuiteReport) {
	expected := vector.Expected
	ordered, _ := plistOrderedRecords(runner, vector)
	canonicalHex, _ := plistExpectedString(expected, "canonical_hex")
	conversionRender, _ := plistExpectedString(expected, "conversion_render")
	closure, _ := plistExpectedBoolean(expected, "closure")
	representationChange, _ := plistExpectedBoolean(expected, "representation_change_reported")
	deduplicated, _ := plistExpectedInteger(expected, "deduplicated_scalars")
	renders, _ := plistExpectedSequence(expected, "renders")
	codes, _ := plistExpectedSequence(expected, "codes")
	truncationEvents, _ := plistExpectedInteger(expected, "truncation_events")
	for index, sample := range samples {
		style, ok := stringField(sample, "style")
		if !ok {
			style, ok = stringField(vector.Input, "style")
			if !ok {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "missing sample style"})
				return
			}
		}
		if record, hasRecord := objectField(sample, "record"); hasRecord {
			var recordValue core.Value = record
			if decoded, exists := ordered[vector.ID]; exists {
				recordValue = decoded
			}
			if policy, hasPolicy := objectField(sample, "truncate_policy"); hasPolicy {
				built, message := plistAddTruncatePolicy(record, policy)
				if message != "" {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: message})
					return
				}
				recordValue = built
			}
			request, message := plistMaterializationRequest(style)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			result := plist.Materialize(recordValue, request)
			if result.Complete != nil {
				if renders != nil {
					expectedRender, ok := renders[index].(core.String)
					if !ok {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "expected render must be a string"})
						return
					}
					if string(result.Complete.Document.Render()) != string(expectedRender) {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "render mismatch"})
						return
					}
				}
				if truncationEvents > 0 {
					events := 0
					for _, event := range result.Complete.Report.Events() {
						if event.Code == "plist.materialization.fractional-date@1" {
							events++
						}
					}
					if int64(events) != truncationEvents {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "truncation events mismatch"})
						return
					}
				}
				if closure {
					if result.Complete.Document.FormationStatus() != document.FormationStatusComplete {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "materialized document must be complete"})
						return
					}
				}
			} else {
				if codes != nil {
					expectedCode, isString := codes[index].(core.String)
					_, isNull := codes[index].(core.Null)
					if !isString && !isNull {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "expected code must be a string"})
						return
					}
					if isNull {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "materialization must complete"})
						return
					}
					if plistMaterializationFailureCode(result.Failed.Failure) != string(expectedCode) {
						report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
							Message: "materialization failure code mismatch"})
						return
					}
				} else {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "materialization must complete"})
					return
				}
			}
			continue
		}
		// Source-document samples: normalization materializes the projected
		// record directly (RFC 0013 §9, §10); conversion crosses the
		// representation boundary.
		doc, message := plistFormValue(sample)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if style == "plist.binary-canonical@1" {
			projection := plist.Project(doc, plist.NewProjectionRequestValueTree())
			if projection.Failed != nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "projection must complete"})
				return
			}
			request, message := plistMaterializationRequest(style)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			complete, message := plistCompleteMaterialization(projection.Complete.Value, request)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			if canonicalHex != "" {
				if hex.EncodeToString(complete.Document.Render()) != canonicalHex {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "canonical_hex mismatch"})
					return
				}
			}
			if deduplicated > 0 {
				baseScalars := plistScalarObjects(doc)
				committedScalars := plistScalarObjects(complete.Document)
				actual := int64(baseScalars - committedScalars)
				if actual != deduplicated {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "deduplicated_scalars mismatch"})
					return
				}
			}
			if closure {
				if complete.Document.FormationStatus() != document.FormationStatusComplete {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "materialized document must be complete"})
					return
				}
			}
		} else {
			converted, failure := doc.ConvertTo(plist.PlistProfileXmlV1,
				plist.DefaultPlistParseLimits())
			if failure != nil {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "conversion failed: " + failure.Code()})
				return
			}
			if conversionRender != "" {
				if string(converted.Document().Render()) != conversionRender {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "conversion_render mismatch"})
					return
				}
			}
			if representationChange {
				if !converted.Report().RepresentationChanged() {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "representation change not reported"})
					return
				}
			}
			if closure {
				if converted.Document().FormationStatus() != document.FormationStatusComplete {
					report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
						Message: "converted document must be complete"})
					return
				}
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// ---------------------------------------------------------------------------
// Conversion
// ---------------------------------------------------------------------------

func runPlistConversion(vector *caseData, report *SuiteReport) {
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "conversion input must form completely"})
		return
	}
	targetText, ok := plistExpectedString(vector.Expected, "target")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.target"})
		return
	}
	var target plist.PlistProfile
	switch targetText {
	case "plist.binary@1":
		target = plist.PlistProfileBinaryV1
	case "plist.xml@1":
		target = plist.PlistProfileXmlV1
	default:
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "unknown target profile " + targetText})
		return
	}
	converted, failure := doc.ConvertTo(target, plist.DefaultPlistParseLimits())
	if failure != nil {
		expectedCode, ok := plistExpectedString(vector.Expected, "code")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "conversion must complete"})
			return
		}
		if failure.Code() != expectedCode {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "conversion failure code mismatch"})
			return
		}
		report.Passed = append(report.Passed, vector.ID)
		return
	}
	if _, hasCode := plistExpectedString(vector.Expected, "code"); hasCode {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "conversion must fail"})
		return
	}
	if representationChange, ok := plistExpectedBoolean(vector.Expected,
		"representation_change_reported"); ok && representationChange {
		if !converted.Report().RepresentationChanged() {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "representation change not reported"})
			return
		}
	}
	if closure, ok := plistExpectedBoolean(vector.Expected, "closure"); ok && closure {
		if converted.Document().FormationStatus() != document.FormationStatusComplete {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "converted document must be complete"})
			return
		}
	}
	if roundTrip, ok := plistExpectedBoolean(vector.Expected, "round_trip"); ok && roundTrip {
		// Reparse closure across the boundary (RFC 0013 §7): the target
		// converted back under the source profile must carry the exact
		// source native model.
		sourceProfile, _ := plistProfileOf(vector.Input)
		back, failure := converted.Document().ConvertTo(sourceProfile,
			plist.DefaultPlistParseLimits())
		if failure != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "round-trip conversion failed: " + failure.Code()})
			return
		}
		if !doc.NativeDocument().Equal(back.Document().NativeDocument()) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "round-trip native model mismatch"})
			return
		}
	}
	if keys, ok := plistExpectedSequence(vector.Expected, "dict_keys"); ok {
		root, message := plistRootValue(converted.Document())
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		actual, message := plistDictKeysOf(converted.Document(), &root)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if message := plistAssertStrings(actual, keys, "key"); message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}

// ---------------------------------------------------------------------------
// Edit
// ---------------------------------------------------------------------------

// plistEditPath builds the operation target path from an explicit `path`
// sequence or the `dict` / `array` key name of one root-level container.
func plistEditPath(operation core.Value) (plist.EditPath, string) {
	if path, ok := sequenceField(operation, "path"); ok {
		steps := make([]plist.EditPathStep, 0, len(path))
		for _, element := range path {
			if text, ok := element.(core.String); ok {
				steps = append(steps, plist.NewEditPathStepDictKey(
					plist.NewPlistKeyFromUnicode(string(text)), 0))
			} else if integer, ok := element.(core.Integer); ok && integer.Int().IsInt64() {
				steps = append(steps, plist.NewEditPathStepArrayIndex(int(integer.Int().Int64())))
			} else {
				return plist.EditPath{}, "path step must be a string or integer"
			}
		}
		return plist.NewEditPath(steps), ""
	}
	if name, ok := stringField(operation, "dict"); ok {
		return plist.NewEditPath([]plist.EditPathStep{
			plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode(name), 0)}), ""
	}
	if name, ok := stringField(operation, "array"); ok {
		return plist.NewEditPath([]plist.EditPathStep{
			plist.NewEditPathStepDictKey(plist.NewPlistKeyFromUnicode(name), 0)}), ""
	}
	return plist.EditPath{}, "missing operation path"
}

// plistEditValue builds one typed edit value from its `{kind, ...}` fact.
func plistEditValue(value core.Value) (plist.EditValue, string) {
	kind, ok := stringField(value, "kind")
	if !ok {
		return plist.EditValue{}, "missing value kind"
	}
	switch kind {
	case "string":
		text, ok := stringField(value, "text")
		if !ok {
			return plist.EditValue{}, "missing text"
		}
		return plist.NewEditValueString(plist.NewPlistStringFromUnicode(text)), ""
	case "integer":
		payload, ok := objectField(value, "value")
		if !ok {
			return plist.EditValue{}, "missing integer value"
		}
		integer, ok := payload.(core.Integer)
		if !ok || !integer.Int().IsInt64() {
			return plist.EditValue{}, "missing integer value"
		}
		return plist.NewEditValueInteger(plist.NewPlistInteger(integer.Int().Int64())), ""
	case "real":
		payload, ok := objectField(value, "value")
		if !ok {
			return plist.EditValue{}, "missing real value"
		}
		real, ok := plistValueF64(payload)
		if !ok {
			return plist.EditValue{}, "missing real value"
		}
		return plist.NewEditValueReal(plist.NewPlistRealDouble(real)), ""
	case "boolean":
		payload, ok := objectField(value, "value")
		if !ok {
			return plist.EditValue{}, "missing boolean value"
		}
		boolean, ok := payload.(core.Boolean)
		if !ok {
			return plist.EditValue{}, "missing boolean value"
		}
		return plist.NewEditValueBoolean(plist.NewPlistBoolean(bool(boolean))), ""
	case "date":
		payload, ok := objectField(value, "seconds")
		if !ok {
			return plist.EditValue{}, "missing date seconds"
		}
		seconds, ok := plistValueF64(payload)
		if !ok {
			return plist.EditValue{}, "missing date seconds"
		}
		date, valid := plist.NewPlistDateFromSeconds(seconds)
		if !valid {
			return plist.EditValue{}, "date seconds must be finite"
		}
		return plist.NewEditValueDate(date), ""
	case "data":
		text, ok := stringField(value, "hex")
		if !ok {
			return plist.EditValue{}, "missing data hex"
		}
		decoded, err := hex.DecodeString(text)
		if err != nil {
			return plist.EditValue{}, "invalid data hex"
		}
		return plist.NewEditValueData(plist.NewPlistDataFromBytes(decoded)), ""
	case "uid":
		payload, ok := objectField(value, "value")
		if !ok {
			return plist.EditValue{}, "missing uid value"
		}
		integer, ok := payload.(core.Integer)
		if !ok || !integer.Int().IsInt64() || integer.Int().Sign() < 0 ||
			integer.Int().Int64() > int64(^uint32(0)) {
			return plist.EditValue{}, "uid out of range"
		}
		return plist.NewEditValueUID(plist.NewPlistUID(uint32(integer.Int().Int64()))), ""
	default:
		return plist.EditValue{}, "unknown value kind " + kind
	}
}

// plistBuildTransaction builds one transaction from the operation facts.
func plistBuildTransaction(doc *plist.Document, operations []core.Value) (*plist.EditTransaction, string) {
	builder := plist.NewEditTransactionBuilder(doc)
	for _, operation := range operations {
		op, ok := stringField(operation, "op")
		if !ok {
			return nil, "missing op"
		}
		switch op {
		case "plist.edit.set-value@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			valueField, ok := objectField(operation, "value")
			if !ok {
				return nil, "missing value"
			}
			value, message := plistEditValue(valueField)
			if message != "" {
				return nil, message
			}
			builder.SetValue(path, value)
		case "plist.edit.insert-dict-entry@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			keyText, ok := stringField(operation, "key")
			if !ok {
				return nil, "missing key"
			}
			valueField, ok := objectField(operation, "value")
			if !ok {
				return nil, "missing value"
			}
			value, message := plistEditValue(valueField)
			if message != "" {
				return nil, message
			}
			placement := plist.NewDictPlacementEnd()
			if placementText, ok := stringField(operation, "placement"); ok {
				if placementText != "End" {
					return nil, "unknown placement " + placementText
				}
			}
			builder.InsertDictEntry(path, plist.NewPlistKeyFromUnicode(keyText), value,
				placement, 0)
		case "plist.edit.remove-dict-entry@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			keyText, ok := stringField(operation, "key")
			if !ok {
				return nil, "missing key"
			}
			builder.RemoveDictEntry(path, plist.NewPlistKeyFromUnicode(keyText), 0)
		case "plist.edit.rename-dict-key@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			from, ok := stringField(operation, "from")
			if !ok {
				return nil, "missing from"
			}
			to, ok := stringField(operation, "to")
			if !ok {
				return nil, "missing to"
			}
			builder.RenameDictKey(path, plist.NewPlistKeyFromUnicode(from), 0,
				plist.NewPlistKeyFromUnicode(to))
		case "plist.edit.insert-array-element@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			index, ok := plistOperationUsize(operation, "index")
			if !ok {
				return nil, "missing index"
			}
			valueField, ok := objectField(operation, "value")
			if !ok {
				return nil, "missing value"
			}
			value, message := plistEditValue(valueField)
			if message != "" {
				return nil, message
			}
			builder.InsertArrayElement(path, index, value)
		case "plist.edit.remove-array-element@1":
			path, message := plistEditPath(operation)
			if message != "" {
				return nil, message
			}
			index, ok := plistOperationUsize(operation, "index")
			if !ok {
				return nil, "missing index"
			}
			builder.RemoveArrayElement(path, index)
		default:
			return nil, "unknown edit op " + op
		}
	}
	return builder.Build(), ""
}

// plistOperationUsize reads one non-negative integer operation fact.
func plistOperationUsize(operation core.Value, name string) (int, bool) {
	field, ok := objectField(operation, name)
	if !ok {
		return 0, false
	}
	integer, ok := field.(core.Integer)
	if !ok || !integer.Int().IsInt64() || integer.Int().Sign() < 0 {
		return 0, false
	}
	return int(integer.Int().Int64()), true
}

// plistReparse reparses one committed document under its own profile.
func plistReparse(doc *plist.Document) (*plist.Document, string) {
	profile := plist.PlistProfileXmlV1
	if doc.Profile().ID() == "plist.binary" {
		profile = plist.PlistProfileBinaryV1
	}
	return plistFormBytes(doc.Render(), profile)
}

// plistAssertEditNative asserts the vector facts of one committed edit's
// native model.
func plistAssertEditNative(expected core.Value, committed *plist.Document) string {
	root, message := plistRootValue(committed)
	if message != "" {
		return message
	}
	if kind, ok := plistExpectedString(expected, "top_kind"); ok {
		if plistValueKindName(&root) != kind {
			return "top_kind mismatch"
		}
	}
	if keys, ok := plistExpectedSequence(expected, "dict_a_keys"); ok {
		dictA, message := plistEntryByKey(committed, &root, "a")
		if message != "" {
			return message
		}
		actual, message := plistDictKeysOf(committed, dictA)
		if message != "" {
			return message
		}
		if message := plistAssertStrings(actual, keys, "key"); message != "" {
			return message
		}
	}
	if values, ok := plistExpectedSequence(expected, "dict_a_values"); ok {
		dictA, message := plistEntryByKey(committed, &root, "a")
		if message != "" {
			return message
		}
		dict, message := plistDictEntries(dictA)
		if message != "" {
			return message
		}
		if dict.Len() != len(values) {
			return "value count mismatch"
		}
		for index, entry := range dict.Entries() {
			value, message := plistNativeValueOf(committed, entry.Value())
			if message != "" {
				return message
			}
			if message := plistCompareScalarValue(value, values[index]); message != "" {
				return message
			}
		}
	}
	if elements, ok := plistExpectedSequence(expected, "arr_elements"); ok {
		arrayValue, message := plistEntryByKey(committed, &root, "arr")
		if message != "" {
			return message
		}
		array, isArray := arrayValue.AsArray()
		if !isArray {
			return "arr must be an array"
		}
		if array.Len() != len(elements) {
			return "array count mismatch"
		}
		for index, element := range array.Elements() {
			value, message := plistNativeValueOf(committed, element)
			if message != "" {
				return message
			}
			if message := plistCompareScalarValue(value, elements[index]); message != "" {
				return message
			}
		}
	}
	if elements, ok := plistExpectedSequence(expected, "elements"); ok {
		array, isArray := root.AsArray()
		if !isArray {
			return "root must be an array"
		}
		if array.Len() != len(elements) {
			return "array count mismatch"
		}
		for index, element := range array.Elements() {
			value, message := plistNativeValueOf(committed, element)
			if message != "" {
				return message
			}
			if message := plistCompareScalarValue(value, elements[index]); message != "" {
				return message
			}
		}
	}
	if kinds, ok := plistExpectedSequence(expected, "element_kinds"); ok {
		array, isArray := root.AsArray()
		if !isArray {
			return "root must be an array"
		}
		if array.Len() != len(kinds) {
			return "array count mismatch"
		}
		for index, element := range array.Elements() {
			value, message := plistNativeValueOf(committed, element)
			if message != "" {
				return message
			}
			expectedKind, ok := kinds[index].(core.String)
			if !ok {
				return "kind must be a string"
			}
			if plistValueKindName(value) != string(expectedKind) {
				return "element kind mismatch"
			}
		}
	}
	return ""
}

func runPlistEdit(vector *caseData, report *SuiteReport) {
	if samples, hasSamples := caseInput(vector, "samples"); hasSamples {
		array, ok := samples.(*core.Array)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "samples must be a Sequence"})
			return
		}
		runPlistEditConflicts(vector, array.Items(), report)
		return
	}
	doc, message := plistFormCase(vector)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	if doc.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit input must form completely"})
		return
	}
	operations, ok := sequenceField(vector.Input, "operations")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing input.operations"})
		return
	}
	transaction, message := plistBuildTransaction(doc, operations)
	if message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	commit, failure := doc.Commit(transaction)
	if failure != nil {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "edit failed: " + failure.Code()})
		return
	}
	committed := commit.Document
	if committed.FormationStatus() != document.FormationStatusComplete {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "committed document must be complete"})
		return
	}
	if reparseClosure, ok := plistExpectedBoolean(vector.Expected, "reparse_closure"); ok && reparseClosure {
		reparsed, message := plistReparse(committed)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		if reparsed.FormationStatus() != document.FormationStatusComplete {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "committed document must reparse completely"})
			return
		}
	}
	if patchReplays, ok := plistExpectedBoolean(vector.Expected, "patch_replays"); ok && patchReplays {
		replay, err := commit.SourcePatch.Apply(doc.Source(), document.DefaultSourcePatchLimits())
		if err != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "patch does not replay"})
			return
		}
		if string(replay.Bytes()) != string(committed.Render()) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "patch does not replay"})
			return
		}
	}
	untouchedByteProof, _ := plistExpectedBoolean(vector.Expected, "untouched_byte_proof")
	untouchedObjectBytes, _ := plistExpectedBoolean(vector.Expected, "untouched_object_bytes")
	if untouchedByteProof || untouchedObjectBytes {
		if proofError := commit.UntouchedProof.Verify(doc.Source(), committed.Source(),
			commit.SourcePatch.Replacements()); proofError != nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "untouched proof: " + proofError.Error()})
			return
		}
	}
	if untouchedObjectBytes {
		for _, region := range commit.UntouchedProof.Regions() {
			base := doc.Source().Bytes()[region.OldStart():region.OldEnd()]
			target := committed.Source().Bytes()[region.NewStart():region.NewEnd()]
			if string(base) != string(target) {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "untouched region content changed"})
				return
			}
		}
	}
	if message := plistAssertEditNative(vector.Expected, committed); message != "" {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
		return
	}
	report.Passed = append(report.Passed, vector.ID)
}

func runPlistEditConflicts(vector *caseData, samples []core.Value, report *SuiteReport) {
	expected := vector.Expected
	codes, ok := plistExpectedSequence(expected, "codes")
	if !ok {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "missing expected.codes"})
		return
	}
	baseUnchanged, _ := plistExpectedBoolean(expected, "base_unchanged")
	if len(samples) != len(codes) {
		report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
			Message: "code count mismatch"})
		return
	}
	for index, sample := range samples {
		doc, message := plistFormSample(vector, sample)
		if message != "" {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
			return
		}
		operations, ok := sequenceField(sample, "operations")
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "missing operations"})
			return
		}
		var transaction *plist.EditTransaction
		if wrong, hasWrong := objectField(sample, "wrong_source"); hasWrong {
			// The transaction is bound to another document's snapshot.
			other, message := plistFormValue(wrong)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
			transaction, message = plistBuildTransaction(other, operations)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
		} else {
			transaction, message = plistBuildTransaction(doc, operations)
			if message != "" {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID, Message: message})
				return
			}
		}
		_, failure := doc.Commit(transaction)
		if failure == nil {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "edit must fail"})
			return
		}
		expectedCode, ok := codes[index].(core.String)
		if !ok {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "expected code must be a string"})
			return
		}
		if failure.Code() != string(expectedCode) {
			report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
				Message: "edit failure code mismatch"})
			return
		}
		if baseUnchanged {
			if string(doc.Render()) != string(doc.Source().Bytes()) {
				report.Failed = append(report.Failed, CaseFailure{ID: vector.ID,
					Message: "base document changed"})
				return
			}
		}
	}
	report.Passed = append(report.Passed, vector.ID)
}
