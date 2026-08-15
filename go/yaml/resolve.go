package yaml

import (
	"math/big"
	"strings"
)

// The frozen standard repository tag identifiers (consema-yaml native.rs:
// 17-31).
const (
	tagNull      = "tag:yaml.org,2002:null"
	tagBool      = "tag:yaml.org,2002:bool"
	tagInt       = "tag:yaml.org,2002:int"
	tagFloat     = "tag:yaml.org,2002:float"
	tagStr       = "tag:yaml.org,2002:str"
	tagSeq       = "tag:yaml.org,2002:seq"
	tagMap       = "tag:yaml.org,2002:map"
	tagTimestamp = "tag:yaml.org,2002:timestamp"
	tagBinary    = "tag:yaml.org,2002:binary"
	tagMerge     = "tag:yaml.org,2002:merge"
	tagOmap      = "tag:yaml.org,2002:omap"
	tagPairs     = "tag:yaml.org,2002:pairs"
	tagSet       = "tag:yaml.org,2002:set"
	tagValue     = "tag:yaml.org,2002:value"
	tagYaml      = "tag:yaml.org,2002:yaml"
)

var standardGraphTags = map[string]bool{
	tagNull: true, tagBool: true, tagInt: true, tagFloat: true, tagStr: true,
	tagSeq: true, tagMap: true, tagTimestamp: true, tagBinary: true, tagMerge: true,
	tagOmap: true, tagPairs: true, tagSet: true, tagValue: true, tagYaml: true,
}

func isStandardCollectionTag(tag string) bool {
	return tag == tagSeq || tag == tagMap || tag == tagOmap || tag == tagPairs || tag == tagSet
}

func isStandardScalarTag(tag string) bool {
	switch tag {
	case tagNull, tagBool, tagInt, tagFloat, tagStr, tagTimestamp, tagBinary, tagMerge,
		tagValue, tagYaml:
		return true
	}
	return false
}

// resolveCollectionTag validates an explicit collection tag and returns the
// resolved node tag (consema-yaml native.rs resolve_collection_tag).
func resolveCollectionTag(explicit string, hasExplicit bool, expected string) (string, *FormationFailure) {
	if !hasExplicit {
		return expected, nil
	}
	if explicit == "!" {
		return expected, nil
	}
	valid := false
	switch expected {
	case tagSeq:
		valid = explicit == tagSeq || explicit == tagOmap || explicit == tagPairs
	case tagMap:
		valid = explicit == tagMap || explicit == tagSet
	}
	if (isStandardCollectionTag(explicit) && !valid) || isStandardScalarTag(explicit) {
		return "", newNativeFailure("yaml.tag.kind-mismatch@1")
	}
	return explicit, nil
}

// resolveScalar resolves one decoded scalar presentation into its resolved
// tag and native scalar facts (consema-yaml native.rs resolve_scalar).
func resolveScalar(decoded string, style YamlScalarStyle, explicit string,
	hasExplicit bool, profile YamlProfile) (string, nativeScalar, *FormationFailure) {
	if hasExplicit {
		tag := explicit
		if isStandardCollectionTag(tag) {
			return "", nativeScalar{}, newNativeFailure("yaml.tag.kind-mismatch@1")
		}
		if tag == "!" || tag == tagStr {
			return tagStr, nativeScalar{decoded: decoded, canonical: decoded,
				kind: ScalarKindString, style: style}, nil
		}
		switch tag {
		case tagNull, tagBool, tagInt, tagFloat:
			canonical, kind, ok, failure := resolveExplicit(decoded, tag, profile)
			if failure != nil {
				return "", nativeScalar{}, failure
			}
			if !ok {
				return "", nativeScalar{}, newNativeFailure("yaml.scalar.invalid-explicit-tag@1")
			}
			return tag, nativeScalar{decoded: decoded, canonical: canonical,
				kind: kind, style: style}, nil
		case tagTimestamp:
			canonical, ok := parseTimestamp(decoded)
			if !ok {
				return "", nativeScalar{}, newNativeFailure("yaml.scalar.invalid-explicit-tag@1")
			}
			return tag, nativeScalar{decoded: decoded, canonical: canonical,
				kind: ScalarKindTimestamp, style: style}, nil
		case tagBinary:
			canonical, ok := canonicalBase64(decoded)
			if !ok {
				return "", nativeScalar{}, newNativeFailure("yaml.scalar.invalid-explicit-tag@1")
			}
			return tag, nativeScalar{decoded: decoded, canonical: canonical,
				kind: ScalarKindBinary, style: style}, nil
		case tagMerge, tagValue, tagYaml:
			return tag, nativeScalar{decoded: decoded, canonical: decoded,
				kind: ScalarKindTagged, style: style}, nil
		}
		return tag, nativeScalar{decoded: decoded, canonical: decoded,
			kind: ScalarKindCustom, style: style}, nil
	}
	if style != ScalarStylePlain {
		return tagStr, nativeScalar{decoded: decoded, canonical: decoded,
			kind: ScalarKindString, style: style}, nil
	}
	return resolveImplicit(decoded, profile)
}

// resolveExplicit canonicalizes one scalar under an explicit standard tag
// (consema-yaml native.rs resolve_explicit).
func resolveExplicit(decoded, tag string, profile YamlProfile) (string, YamlScalarKind, bool, *FormationFailure) {
	switch tag {
	case tagNull:
		if _, ok := parseNull(decoded); !ok {
			return "", 0, false, nil
		}
		return "", ScalarKindNull, true, nil
	case tagBool:
		canonical, ok := parseBool(decoded, profile)
		if !ok {
			return "", 0, false, nil
		}
		return canonical, ScalarKindBoolean, true, nil
	case tagInt:
		canonical, ok, failure := parseInteger(decoded, profile)
		if failure != nil {
			return "", 0, false, failure
		}
		if !ok {
			return "", 0, false, nil
		}
		return canonical, ScalarKindInteger, true, nil
	default: // tagFloat
		canonical, ok, failure := parseFloat(decoded, profile)
		if failure != nil {
			return "", 0, false, failure
		}
		if !ok {
			return "", 0, false, nil
		}
		return canonical, ScalarKindFloat, true, nil
	}
}

// resolveImplicit resolves one plain scalar per the frozen profile schemas
// (consema-yaml native.rs resolve_implicit): null, bool, integer, float,
// then timestamp under the 1.1 profile only. First match wins.
func resolveImplicit(decoded string, profile YamlProfile) (string, nativeScalar, *FormationFailure) {
	if _, ok := parseNull(decoded); ok {
		return tagNull, nativeScalar{decoded: decoded, canonical: "",
			kind: ScalarKindNull, style: ScalarStylePlain}, nil
	}
	if canonical, ok := parseBool(decoded, profile); ok {
		return tagBool, nativeScalar{decoded: decoded, canonical: canonical,
			kind: ScalarKindBoolean, style: ScalarStylePlain}, nil
	}
	if canonical, ok, failure := parseInteger(decoded, profile); failure != nil {
		return "", nativeScalar{}, failure
	} else if ok {
		return tagInt, nativeScalar{decoded: decoded, canonical: canonical,
			kind: ScalarKindInteger, style: ScalarStylePlain}, nil
	}
	if canonical, ok, failure := parseFloat(decoded, profile); failure != nil {
		return "", nativeScalar{}, failure
	} else if ok {
		return tagFloat, nativeScalar{decoded: decoded, canonical: canonical,
			kind: ScalarKindFloat, style: ScalarStylePlain}, nil
	}
	if profile == Yaml11CompatV1 {
		if canonical, ok := parseTimestamp(decoded); ok {
			return tagTimestamp, nativeScalar{decoded: decoded, canonical: canonical,
				kind: ScalarKindTimestamp, style: ScalarStylePlain}, nil
		}
	}
	return tagStr, nativeScalar{decoded: decoded, canonical: decoded,
		kind: ScalarKindString, style: ScalarStylePlain}, nil
}

// parseNull matches the four null spellings (consema-yaml native.rs).
func parseNull(value string) (string, bool) {
	switch value {
	case "", "~", "null", "Null", "NULL":
		return "", true
	}
	return "", false
}

// parseBool matches the frozen boolean spellings per profile (native.rs:
// 750-766).
func parseBool(value string, profile YamlProfile) (string, bool) {
	switch value {
	case "true", "True", "TRUE":
		return "true", true
	case "false", "False", "FALSE":
		return "false", true
	}
	if profile == Yaml11CompatV1 {
		switch value {
		case "y", "Y", "yes", "Yes", "YES", "on", "On", "ON":
			return "true", true
		case "n", "N", "no", "No", "NO", "off", "Off", "OFF":
			return "false", true
		}
	}
	return "", false
}

// parseInteger canonicalizes one integer spelling per profile (native.rs:
// 768-801). The canonical value is the exact base-10 decimal string. An
// over-limit number magnitude is a fatal resource-limit failure, never a
// fallthrough.
func parseInteger(value string, profile YamlProfile) (string, bool, *FormationFailure) {
	sign, unsigned := splitSign(value)
	if !signOK(sign) {
		return "", false, nil
	}
	var cleaned string
	if profile == Yaml11CompatV1 {
		valid, ok := validUnderscored(unsigned)
		if !ok {
			return "", false, nil
		}
		cleaned = strings.ReplaceAll(valid, "_", "")
	} else if strings.ContainsRune(unsigned, '_') {
		return "", false, nil
	} else {
		cleaned = unsigned
	}
	var base int64
	var digits string
	switch {
	case strings.HasPrefix(cleaned, "0b"):
		base, digits = 2, cleaned[2:]
	case strings.HasPrefix(cleaned, "0o"):
		if profile == Yaml11CompatV1 {
			return "", false, nil
		}
		base, digits = 8, cleaned[2:]
	case strings.HasPrefix(cleaned, "0x"):
		base, digits = 16, cleaned[2:]
	case profile == Yaml11CompatV1 && len(cleaned) > 1 && cleaned[0] == '0':
		base, digits = 8, cleaned
	case profile == Yaml11CompatV1 && strings.ContainsRune(cleaned, ':'):
		return parseSexagesimalInteger(sign, cleaned)
	default:
		base, digits = 10, cleaned
	}
	if count := integerMagnitudeDigits(digits, base); count > maxNumberMagnitudeDigits {
		return "", false, numberMagnitudeFailure(count)
	}
	magnitude, ok := parseBaseMagnitude(digits, base)
	if !ok {
		return "", false, nil
	}
	return signedDecimalString(sign, magnitude), true, nil
}

// integerMagnitudeDigits counts the significant digit characters of one
// integer magnitude lexeme in the given base (hex digits for base 16,
// decimal digits otherwise).
func integerMagnitudeDigits(text string, base int64) int {
	digits := 0
	for index := 0; index < len(text); index++ {
		character := text[index]
		switch {
		case base == 16:
			if character >= '0' && character <= '9' ||
				character >= 'a' && character <= 'f' ||
				character >= 'A' && character <= 'F' {
				digits++
			}
		case character >= '0' && character <= '9':
			digits++
		}
	}
	return digits
}

// parseFloat canonicalizes one float spelling per profile (native.rs:
// 803-829). Non-finite values use the frozen spellings; finite values are
// the exact normalized decimal canonical form. An over-limit number
// magnitude is a fatal resource-limit failure, never a fallthrough.
func parseFloat(value string, profile YamlProfile) (string, bool, *FormationFailure) {
	switch value {
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF":
		return ".inf", true, nil
	case "-.inf", "-.Inf", "-.INF":
		return "-.inf", true, nil
	case ".nan", ".NaN", ".NAN":
		return ".nan", true, nil
	}
	var cleaned string
	if profile == Yaml11CompatV1 {
		valid, ok := validUnderscored(value)
		if !ok {
			return "", false, nil
		}
		cleaned = strings.ReplaceAll(valid, "_", "")
	} else if strings.ContainsRune(value, '_') {
		return "", false, nil
	} else {
		cleaned = value
	}
	if profile == Yaml11CompatV1 && strings.ContainsRune(cleaned, ':') {
		return parseSexagesimalFloat(cleaned)
	}
	if !strings.ContainsAny(cleaned, ".eE") {
		return "", false, nil
	}
	normalized := normalizeDecimalLexeme(cleaned)
	coefficient, exponent, ok, failure := parseJSONDecimal(normalized)
	if failure != nil {
		return "", false, failure
	}
	if !ok {
		return "", false, nil
	}
	return decimalCanonical(coefficient, exponent), true, nil
}

// normalizeDecimalLexeme rewrites leading dots and trailing mantissa dots
// into the JSON-number grammar (native.rs).
func normalizeDecimalLexeme(value string) string {
	var builder strings.Builder
	body := value
	if strings.HasPrefix(body, "+") {
		body = body[1:]
	}
	if strings.HasPrefix(body, "-.") {
		builder.WriteString("-0")
		builder.WriteString(body[1:])
		body = ""
	} else if strings.HasPrefix(body, ".") {
		builder.WriteString("0")
		builder.WriteString(body)
		body = ""
	}
	if body != "" {
		builder.WriteString(body)
	}
	text := builder.String()
	exponent := len(text)
	for index, character := range text {
		if character == 'e' || character == 'E' {
			exponent = index
			break
		}
	}
	if exponent > 0 && text[exponent-1] == '.' {
		text = text[:exponent] + "0" + text[exponent:]
	}
	return text
}

// maxNumberMagnitudeDigits is the frozen cross-language upper bound on
// the total digit count (coefficient plus exponent) of one parsed number
// lexeme (wave-4 default, shared with the Rust reference). The check runs
// before any big.Int allocation; an over-limit lexeme is a fatal
// resource-limit failure, never a truncation.
const maxNumberMagnitudeDigits = 100_000

// numberMagnitudeFailure builds the frozen core.parse.resource-limit@1
// failure for one number lexeme over maxNumberMagnitudeDigits.
func numberMagnitudeFailure(observed int) *FormationFailure {
	return resourceLimitFailure("number-magnitude-digits", observed, maxNumberMagnitudeDigits)
}

// decimalMagnitudeDigits counts the significant decimal digits of one
// number lexeme (the coefficient digits plus the exponent digits; sign,
// fraction point, and exponent markers are not digits).
func decimalMagnitudeDigits(text string) int {
	digits := 0
	for index := 0; index < len(text); index++ {
		if text[index] >= '0' && text[index] <= '9' {
			digits++
		}
	}
	return digits
}

// parseJSONDecimal parses one strict JSON number lexeme into the exact
// coefficient/exponent form (the consema-core Decimal::parse_json_number
// contract used by native.rs). It returns a frozen resource-limit failure
// when the lexeme exceeds maxNumberMagnitudeDigits, checked before any
// big.Int allocation.
func parseJSONDecimal(value string) (*big.Int, *big.Int, bool, *FormationFailure) {
	if digits := decimalMagnitudeDigits(value); digits > maxNumberMagnitudeDigits {
		return nil, nil, false, numberMagnitudeFailure(digits)
	}
	body := value
	negative := false
	if strings.HasPrefix(body, "-") {
		negative = true
		body = body[1:]
	} else if strings.HasPrefix(body, "+") {
		body = body[1:]
	}
	if body == "" {
		return nil, nil, false, nil
	}
	exponentIndex := len(body)
	for index, character := range body {
		if character == 'e' || character == 'E' {
			exponentIndex = index
			break
		}
	}
	mantissa := body[:exponentIndex]
	exponentText := ""
	if exponentIndex < len(body) {
		exponentText = body[exponentIndex+1:]
		if exponentText == "" {
			return nil, nil, false, nil
		}
	}
	pointIndex := strings.IndexByte(mantissa, '.')
	if pointIndex >= 0 {
		if strings.IndexByte(mantissa[pointIndex+1:], '.') >= 0 {
			return nil, nil, false, nil
		}
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	if digits == "" || !allASCIIHexDigits(digits) {
		return nil, nil, false, nil
	}
	coefficient := new(big.Int)
	if _, ok := coefficient.SetString(digits, 10); !ok {
		return nil, nil, false, nil
	}
	if negative {
		coefficient.Neg(coefficient)
	}
	exponent := new(big.Int)
	if exponentText != "" {
		if _, ok := exponent.SetString(exponentText, 10); !ok {
			return nil, nil, false, nil
		}
	}
	if pointIndex >= 0 {
		fractionLength := int64(len(mantissa) - pointIndex - 1)
		exponent.Sub(exponent, big.NewInt(fractionLength))
	}
	return coefficient, exponent, true, nil
}

// decimalCanonical is the frozen canonical decimal spelling (native.rs:
// 914-920): plain coefficient when the exponent is zero, else
// `{coefficient}e{exponent}`.
func decimalCanonical(coefficient, exponent *big.Int) string {
	if exponent.Sign() == 0 {
		return coefficient.String()
	}
	return coefficient.String() + "e" + exponent.String()
}

// parseSexagesimalInteger canonicalizes one YAML 1.1 base-60 integer
// (native.rs). The magnitude digit bound is enforced before the
// parseBaseMagnitude allocation.
func parseSexagesimalInteger(sign int8, value string) (string, bool, *FormationFailure) {
	parts := strings.Split(value, ":")
	first := parts[0]
	if first == "" || !allASCIIDigits(first) {
		return "", false, nil
	}
	if count := decimalMagnitudeDigits(value); count > maxNumberMagnitudeDigits {
		return "", false, numberMagnitudeFailure(count)
	}
	magnitude, ok := parseBaseMagnitude(first, 10)
	if !ok {
		return "", false, nil
	}
	count := 0
	for _, part := range parts[1:] {
		component, ok := parseU8(part)
		if !ok || component > 59 || part == "" || len(part) > 2 {
			return "", false, nil
		}
		multiplyAdd(&magnitude, 60, component)
		count++
	}
	if count == 0 {
		return "", false, nil
	}
	return signedDecimalString(sign, magnitude), true, nil
}

// parseSexagesimalFloat canonicalizes one YAML 1.1 base-60 float
// (native.rs). The coefficient magnitude bound is enforced before the
// big.Int allocation.
func parseSexagesimalFloat(value string) (string, bool, *FormationFailure) {
	sign, unsigned := splitSign(value)
	if !signOK(sign) {
		return "", false, nil
	}
	parts := strings.Split(unsigned, ":")
	last := parts[len(parts)-1]
	point := strings.IndexByte(last, '.')
	if point < 0 {
		return "", false, nil
	}
	whole := last[:point]
	fraction := last[point+1:]
	if fraction == "" || !allASCIIDigits(fraction) {
		return "", false, nil
	}
	var magnitude []uint8
	intermediate := parts[:len(parts)-1]
	for index, part := range intermediate {
		component, ok := parseU64(part)
		if !ok {
			return "", false, nil
		}
		if index > 0 && component > 59 {
			return "", false, nil
		}
		if index == 0 {
			var ok bool
			magnitude, ok = parseBaseMagnitude(part, 10)
			if !ok {
				return "", false, nil
			}
		} else {
			multiplyAdd(&magnitude, 60, uint8(component))
		}
	}
	if len(intermediate) == 0 {
		return "", false, nil
	}
	wholeValue, ok := parseU8(whole)
	if !ok || wholeValue > 59 {
		return "", false, nil
	}
	multiplyAdd(&magnitude, 60, wholeValue)
	combined := positiveDecimalString(magnitude)
	coefficientText := combined + fraction
	if digits := decimalMagnitudeDigits(coefficientText); digits > maxNumberMagnitudeDigits {
		return "", false, numberMagnitudeFailure(digits)
	}
	coefficient := new(big.Int)
	if _, ok := coefficient.SetString(coefficientText, 10); !ok {
		return "", false, nil
	}
	if sign < 0 {
		coefficient.Neg(coefficient)
	}
	exponent := big.NewInt(-int64(len(fraction)))
	return decimalCanonical(coefficient, exponent), true, nil
}

// parseTimestamp canonicalizes one YAML 1.1 timestamp (native.rs).
// The canonical form is `{date}T{hh}:{mm}:{ss}{.fraction}{zone}` with a
// `Z` zone default for a missing zone.
func parseTimestamp(value string) (string, bool) {
	if !isASCII(value) || len(value) < 10 {
		return "", false
	}
	if !validDate(value[:10]) {
		return "", false
	}
	if len(value) == 10 {
		return value, true
	}
	return canonicalTimestamp(value)
}

func validDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	if !allASCIIDigits(value[:4]) || !allASCIIDigits(value[5:7]) || !allASCIIDigits(value[8:10]) {
		return false
	}
	year := parseInt32(value[:4])
	month, monthOK := parseU8(value[5:7])
	day, dayOK := parseU8(value[8:10])
	if year == nil || !monthOK || !dayOK {
		return false
	}
	leap := *year%4 == 0 && (*year%100 != 0 || *year%400 == 0)
	var maxDay uint8
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		maxDay = 31
	case 4, 6, 9, 11:
		maxDay = 30
	case 2:
		if leap {
			maxDay = 29
		} else {
			maxDay = 28
		}
	default:
		return false
	}
	return day != 0 && day <= maxDay
}

func canonicalTimestamp(value string) (string, bool) {
	rest := strings.TrimLeft(value[10:], " \tTt")
	hour, tail, ok := takeOneOrTwoDigits(rest)
	if !ok {
		return "", false
	}
	tail, ok = stripPrefixByte(tail, ':')
	if !ok {
		return "", false
	}
	minute, tail, ok := takeTwoDigits(tail)
	if !ok {
		return "", false
	}
	tail, ok = stripPrefixByte(tail, ':')
	if !ok {
		return "", false
	}
	second, tail, ok := takeTwoDigits(tail)
	if !ok {
		return "", false
	}
	if hour > 23 || minute > 59 || second > 60 {
		return "", false
	}
	fraction := ""
	if strings.HasPrefix(tail, ".") {
		afterDot := tail[1:]
		length := 0
		for length < len(afterDot) && afterDot[length] >= '0' && afterDot[length] <= '9' {
			length++
		}
		if length == 0 {
			return "", false
		}
		fraction = afterDot[:length]
		tail = afterDot[length:]
	}
	tail = strings.TrimLeft(tail, " \t")
	zone := "Z"
	if tail != "" && tail != "Z" && tail != "z" {
		var ok bool
		zone, ok = canonicalZone(tail)
		if !ok {
			return "", false
		}
	}
	fractionText := ""
	if fraction != "" {
		fractionText = "." + fraction
	}
	return sprintfDate(value[:10]) + "T" + two(hour) + ":" + two(minute) + ":" + two(second) +
		fractionText + zone, true
}

func canonicalZone(value string) (string, bool) {
	var sign byte
	switch value[0] {
	case '+':
		sign = '+'
	case '-':
		sign = '-'
	default:
		return "", false
	}
	rest := value[1:]
	hour, tail, ok := takeOneOrTwoDigits(rest)
	if !ok {
		return "", false
	}
	var minute uint8
	if strings.HasPrefix(tail, ":") {
		var ok bool
		minute, tail, ok = takeTwoDigits(tail[1:])
		if !ok || tail != "" {
			return "", false
		}
	} else if tail != "" {
		return "", false
	}
	if hour > 23 || minute > 59 {
		return "", false
	}
	return string([]byte{sign}) + two(hour) + ":" + two(minute), true
}

// canonicalBase64 validates and cleans one !!binary scalar (native.rs:
// 1077-...): all ASCII whitespace is removed, canonical padding bits are
// enforced, and the canonical value is the cleaned base64 text.
func canonicalBase64(value string) (string, bool) {
	var cleaned strings.Builder
	for _, character := range value {
		if character == ' ' || character == '\t' || character == '\r' || character == '\n' {
			continue
		}
		cleaned.WriteRune(character)
	}
	text := cleaned.String()
	if len(text)%4 != 0 {
		return "", false
	}
	padding := 0
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character == '=' {
			if index < len(text)-2 {
				return "", false
			}
			padding++
			continue
		}
		if !isBase64Alphabet(character) {
			return "", false
		}
		if padding > 0 {
			return "", false
		}
	}
	if padding > 2 {
		return "", false
	}
	if padding > 0 {
		// The last significant sextet must have its unused low bits zero.
		last := text[len(text)-1-padding]
		var value6 uint8
		if last >= 'A' && last <= 'Z' {
			value6 = last - 'A'
		} else if last >= 'a' && last <= 'z' {
			value6 = last - 'a' + 26
		} else if last >= '0' && last <= '9' {
			value6 = last - '0' + 52
		} else if last == '+' {
			value6 = 62
		} else {
			value6 = 63
		}
		if padding == 1 {
			if value6&0b00000011 != 0 {
				return "", false
			}
		} else if value6&0b00001111 != 0 {
			return "", false
		}
	}
	return text, true
}

func isBase64Alphabet(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' || character == '+' || character == '/'
}

// decodeBase64Value decodes validated canonical base64 text into bytes
// (native.rs decode_base64): full chunks only, canonical padding.
func decodeBase64Value(value string) ([]byte, bool) {
	var output []byte
	for offset := 0; offset+4 <= len(value); offset += 4 {
		chunk := value[offset : offset+4]
		var accumulator uint32
		var bytesIn = 0
		for index := 0; index < 4; index++ {
			character := chunk[index]
			var sextet uint32
			switch {
			case character == '=':
				sextet = 0
				bytesIn--
			case character >= 'A' && character <= 'Z':
				sextet = uint32(character - 'A')
			case character >= 'a' && character <= 'z':
				sextet = uint32(character-'a') + 26
			case character >= '0' && character <= '9':
				sextet = uint32(character-'0') + 52
			case character == '+':
				sextet = 62
			case character == '/':
				sextet = 63
			default:
				return nil, false
			}
			accumulator = accumulator<<6 | sextet
		}
		bytesIn += 3
		for index := 0; index < bytesIn; index++ {
			output = append(output, byte(accumulator>>(16-8*index)))
		}
	}
	return output, true
}

func splitSign(value string) (int8, string) {
	if strings.HasPrefix(value, "-") {
		return -1, value[1:]
	}
	if strings.HasPrefix(value, "+") {
		return 1, value[1:]
	}
	return 1, value
}

func signOK(sign int8) bool { return sign == 1 || sign == -1 }

// validUnderscored validates the YAML 1.1 underscore rule (native.rs:
// 930-943): every underscore must be flanked by ASCII alphanumerics and
// not first or last.
func validUnderscored(value string) (string, bool) {
	bytes := []byte(value)
	for index, item := range bytes {
		if item == '_' && (index == 0 || index+1 == len(bytes) ||
			!isASCIIAlphanumeric(bytes[index-1]) || !isASCIIAlphanumeric(bytes[index+1])) {
			return "", false
		}
	}
	return value, true
}

func isASCIIAlphanumeric(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z'
}

func allASCIIDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func allASCIIHexDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') &&
			!(character >= 'A' && character <= 'F') {
			return false
		}
	}
	return true
}

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x80 {
			return false
		}
	}
	return true
}

// parseBaseMagnitude builds the big-endian base-256 magnitude of one digit
// string in the given base (native.rs).
func parseBaseMagnitude(value string, base int64) ([]uint8, bool) {
	if value == "" {
		return nil, false
	}
	var magnitude []uint8
	for _, digit := range value {
		var digitValue uint8
		switch {
		case digit >= '0' && digit <= '9':
			digitValue = uint8(digit - '0')
		case digit >= 'a' && digit <= 'f':
			digitValue = uint8(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			digitValue = uint8(digit-'A') + 10
		default:
			return nil, false
		}
		if int64(digitValue) >= base {
			return nil, false
		}
		multiplyAdd(&magnitude, uint8(base), digitValue)
	}
	return magnitude, true
}

// multiplyAdd multiplies one big-endian base-256 magnitude and adds one
// addend (native.rs).
func multiplyAdd(magnitude *[]uint8, multiplier, addend uint8) {
	var carry uint16 = uint16(addend)
	for index := len(*magnitude) - 1; index >= 0; index-- {
		value := uint16((*magnitude)[index])*uint16(multiplier) + carry
		(*magnitude)[index] = uint8(value)
		carry = value >> 8
	}
	for carry > 0 {
		*magnitude = append([]uint8{uint8(carry)}, (*magnitude)...)
		carry >>= 8
	}
}

// signedDecimalString converts one signed base-256 magnitude into its exact
// base-10 string (native.rs BigInteger::from_sign_and_magnitude).
func signedDecimalString(sign int8, magnitude []uint8) string {
	if len(magnitude) == 0 {
		return "0"
	}
	value := new(big.Int).SetBytes(magnitude)
	if sign < 0 {
		value.Neg(value)
	}
	return value.String()
}

func positiveDecimalString(magnitude []uint8) string {
	if len(magnitude) == 0 {
		return "0"
	}
	return new(big.Int).SetBytes(magnitude).String()
}

func parseU8(value string) (uint8, bool) {
	if value == "" || !allASCIIDigits(value) {
		return 0, false
	}
	var result uint16
	for index := 0; index < len(value); index++ {
		result = result*10 + uint16(value[index]-'0')
	}
	if result > 255 {
		return 0, false
	}
	return uint8(result), true
}

func parseU64(value string) (uint64, bool) {
	if value == "" || !allASCIIDigits(value) {
		return 0, false
	}
	var result uint64
	for index := 0; index < len(value); index++ {
		result = result*10 + uint64(value[index]-'0')
	}
	return result, true
}

func parseInt32(value string) *int32 {
	if !allASCIIDigits(value) {
		return nil
	}
	var result int64
	for index := 0; index < len(value); index++ {
		result = result*10 + int64(value[index]-'0')
	}
	if result > 2147483647 {
		return nil
	}
	converted := int32(result)
	return &converted
}

func takeTwoDigits(value string) (uint8, string, bool) {
	if len(value) < 2 || value[0] < '0' || value[0] > '9' || value[1] < '0' || value[1] > '9' {
		return 0, "", false
	}
	return (value[0]-'0')*10 + (value[1] - '0'), value[2:], true
}

func takeOneOrTwoDigits(value string) (uint8, string, bool) {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return 0, "", false
	}
	if len(value) > 1 && value[1] >= '0' && value[1] <= '9' {
		return (value[0]-'0')*10 + (value[1] - '0'), value[2:], true
	}
	return value[0] - '0', value[1:], true
}

func stripPrefixByte(value string, prefix byte) (string, bool) {
	if strings.HasPrefix(value, string([]byte{prefix})) {
		return value[1:], true
	}
	return "", false
}

func two(value uint8) string {
	if value < 10 {
		return "0" + string([]byte{'0' + value})
	}
	return string([]byte{'0' + value/10, '0' + value%10})
}

// sprintfDate formats the frozen YYYY-MM-DD prefix verbatim.
func sprintfDate(prefix string) string { return prefix }
