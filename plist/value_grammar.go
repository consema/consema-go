package plist

// This file implements the plist scalar value grammars shared by the XML
// parser, the materializers, and the conversion layer (RFC 0013 §4.5-4.8,
// §10). The date calendar is the proleptic Gregorian calendar of the Rust
// reference (Hinnant's days_from_civil / civil_from_days).

import (
	"math"
	"strconv"
	"strings"
)

// parseIntegerText parses the frozen integer grammar (RFC 0013 §4.5):
// `S*(-|+)?S*[0-9]+` and `S*(-|+)?S*0[xX][0-9a-fA-F]+` into signed 64-bit.
func parseIntegerText(content string) (int64, bool) {
	bytes := []byte(strings.Trim(content, " \t\n\r"))
	index := 0
	negative := false
	if len(bytes) > 0 && (bytes[0] == '-' || bytes[0] == '+') {
		negative = bytes[0] == '-'
		index = 1
	}
	for index < len(bytes) && isXMLSpaceByte(bytes[index]) {
		index++
	}
	hex := false
	start := index
	if index+1 < len(bytes) && bytes[index] == '0' && (bytes[index+1] == 'x' || bytes[index+1] == 'X') {
		hex = true
		start = index + 2
	}
	end := start
	for end < len(bytes) {
		if hex {
			if !isHexDigit(bytes[end]) {
				break
			}
		} else if !isASCIIDigit(bytes[end]) {
			break
		}
		end++
	}
	if end == start {
		return 0, false
	}
	for end < len(bytes) && isXMLSpaceByte(bytes[end]) {
		end++
	}
	if end != len(bytes) {
		return 0, false
	}
	digits := string(bytes[start:end])
	magnitude := uint64(0)
	if hex {
		parsed, err := strconv.ParseUint(digits, 16, 64)
		if err != nil {
			return 0, false
		}
		magnitude = parsed
	} else {
		parsed, err := strconv.ParseUint(digits, 10, 64)
		if err != nil {
			return 0, false
		}
		magnitude = parsed
	}
	if negative {
		if magnitude > 1<<63 {
			return 0, false
		}
		if magnitude == 1<<63 {
			return math.MinInt64, true
		}
		return -int64(magnitude), true
	}
	if magnitude > math.MaxInt64 {
		return 0, false
	}
	return int64(magnitude), true
}

// parseRealText parses the frozen real grammar (RFC 0013 §4.6): the special
// spellings `nan`, `inf`, `±inf`, `infinity`, `±infinity`
// (case-insensitive) and otherwise `sign? digits ('.' digits)? ([eE] sign?
// digits)?`.
func parseRealText(content string) (float64, bool) {
	trimmed := strings.Trim(content, " \t\n\r")
	lower := strings.ToLower(trimmed)
	switch lower {
	case "nan":
		return math.NaN(), true
	case "inf", "+inf", "infinity", "+infinity":
		return math.Inf(1), true
	case "-inf", "-infinity":
		return math.Inf(-1), true
	}
	bytes := []byte(trimmed)
	index := 0
	if len(bytes) > 0 && (bytes[0] == '+' || bytes[0] == '-') {
		index++
	}
	digitsStart := index
	for index < len(bytes) && isASCIIDigit(bytes[index]) {
		index++
	}
	if index == digitsStart {
		return 0, false
	}
	if index < len(bytes) && bytes[index] == '.' {
		index++
		fractionStart := index
		for index < len(bytes) && isASCIIDigit(bytes[index]) {
			index++
		}
		if index == fractionStart {
			return 0, false
		}
	}
	if index < len(bytes) && (bytes[index] == 'e' || bytes[index] == 'E') {
		index++
		if index < len(bytes) && (bytes[index] == '+' || bytes[index] == '-') {
			index++
		}
		exponentStart := index
		for index < len(bytes) && isASCIIDigit(bytes[index]) {
			index++
		}
		if index == exponentStart {
			return 0, false
		}
	}
	if index != len(bytes) {
		return 0, false
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

// parseDateText parses the frozen date grammar (RFC 0013 §4.7):
// `[-]YYYY-MM-DDTHH:MM:SSZ` with calendar validation; the value is the
// exact double seconds since the plist epoch.
func parseDateText(content string) (float64, bool) {
	bytes := []byte(content)
	index := 0
	negative := false
	if len(bytes) > 0 && bytes[0] == '-' {
		negative = true
		index = 1
	}
	yearStart := index
	for index < len(bytes) && isASCIIDigit(bytes[index]) {
		index++
	}
	if index == yearStart {
		return 0, false
	}
	year, err := strconv.ParseUint(string(bytes[yearStart:index]), 10, 64)
	if err != nil || year > math.MaxUint32 {
		return 0, false
	}
	month, ok := expectTwoDigits(bytes, &index, '-')
	if !ok {
		return 0, false
	}
	day, ok := expectTwoDigits(bytes, &index, '-')
	if !ok {
		return 0, false
	}
	hour, ok := expectTwoDigits(bytes, &index, 'T')
	if !ok {
		return 0, false
	}
	minute, ok := expectTwoDigits(bytes, &index, ':')
	if !ok {
		return 0, false
	}
	second, ok := expectTwoDigits(bytes, &index, ':')
	if !ok {
		return 0, false
	}
	if index >= len(bytes) || bytes[index] != 'Z' {
		return 0, false
	}
	index++
	if index != len(bytes) {
		return 0, false
	}
	yearSigned := int64(year)
	if negative {
		yearSigned = -yearSigned
	}
	if month < 1 || month > 12 {
		return 0, false
	}
	if day == 0 || day > daysInMonth(yearSigned, month) {
		return 0, false
	}
	if hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	days := daysFromCivil(yearSigned, int64(month), int64(day))
	timeOfDay := int64(hour)*3600 + int64(minute)*60 + int64(second)
	unix, overflow := mulAdd(days, 86400, timeOfDay)
	if overflow {
		return 0, false
	}
	return float64(unix) - plistEpochOffsetUnix, true
}

func mulAdd(left int64, right int64, add int64) (int64, bool) {
	product := left * right
	if right != 0 && product/right != left {
		return 0, true
	}
	sum := product + add
	if (add > 0 && sum < product) || (add < 0 && sum > product) {
		return 0, true
	}
	return sum, false
}

// expectTwoDigits consumes `sep` then exactly two decimal digits.
func expectTwoDigits(bytes []byte, index *int, sep byte) (uint32, bool) {
	if *index >= len(bytes) || bytes[*index] != sep {
		return 0, false
	}
	*index++
	if *index+2 > len(bytes) || !isASCIIDigit(bytes[*index]) || !isASCIIDigit(bytes[*index+1]) {
		return 0, false
	}
	value := uint32(bytes[*index]-'0')*10 + uint32(bytes[*index+1]-'0')
	*index += 2
	return value, true
}

// daysFromCivil is the proleptic Gregorian calendar days since the Unix
// epoch (Hinnant's days_from_civil); exact for the 32-bit year bound.
func daysFromCivil(year, month, day int64) int64 {
	if month <= 2 {
		year--
	}
	era := year / 400
	if year < 0 {
		era = (year - 399) / 400
	}
	yearOfEra := year - era*400
	var monthAdjusted int64
	if month > 2 {
		monthAdjusted = month - 3
	} else {
		monthAdjusted = month + 9
	}
	dayOfYear := (153*monthAdjusted+2)/5 + day - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

// civilFromDays is the inverse calendar conversion of daysFromCivil
// (Hinnant's civil_from_days).
func civilFromDays(days int64) (year, month, day int64) {
	z := days + 719468
	era := z / 146097
	if z < 0 {
		era = (z - 146096) / 146097
	}
	dayOfEra := z - era*146097
	yearOfEra := (dayOfEra - dayOfEra/1460 + dayOfEra/36524 - dayOfEra/146096) / 365
	year = yearOfEra + era*400
	dayOfYear := dayOfEra - (365*yearOfEra + yearOfEra/4 - yearOfEra/100)
	monthPrime := (5*dayOfYear + 2) / 153
	day = dayOfYear - (153*monthPrime+2)/5 + 1
	if monthPrime < 10 {
		month = monthPrime + 3
	} else {
		month = monthPrime - 9
	}
	if month <= 2 {
		year++
	}
	return year, month, day
}

func isLeapYear(year int64) bool {
	return (year%4 == 0 && year%100 != 0) || year%400 == 0
}

func daysInMonth(year int64, month uint32) uint32 {
	switch month {
	case 1, 3, 5, 7, 8, 10, 12:
		return 31
	case 4, 6, 9, 11:
		return 30
	case 2:
		if isLeapYear(year) {
			return 29
		}
		return 28
	}
	return 0
}

// decodeBase64Text strictly decodes the standard-alphabet base64 with
// `=` padding and ASCII whitespace between characters (RFC 0013 §4.8).
func decodeBase64Text(content string) ([]byte, bool) {
	compact := make([]byte, 0, len(content))
	for index := 0; index < len(content); index++ {
		byte := content[index]
		if isXMLSpaceByte(byte) {
			continue
		}
		compact = append(compact, byte)
	}
	length := len(compact)
	if length == 0 {
		return []byte{}, true
	}
	// Padding must be present exactly as required for the final incomplete
	// group (RFC 0013 §4.8).
	end := length
	paddingCount := 0
	for end > 0 && compact[end-1] == '=' {
		end--
		paddingCount++
	}
	if paddingCount > 2 {
		return nil, false
	}
	expectedPadding := 0
	switch end % 4 {
	case 0:
		expectedPadding = 0
	case 2:
		expectedPadding = 2
	case 3:
		expectedPadding = 1
	default:
		return nil, false
	}
	if paddingCount != expectedPadding {
		return nil, false
	}
	// Every content byte must be in the standard alphabet; each decoded
	// group needs exactly `3 - paddingCount` output bytes.
	outLen := end / 4 * 3
	if paddingCount > 0 {
		outLen -= paddingCount
	}
	out := make([]byte, 0, outLen)
	for index := 0; index < end; index += 4 {
		group := [4]int{-1, -1, -1, -1}
		for offset := 0; offset < 4 && index+offset < end; offset++ {
			value, ok := base64Value(compact[index+offset])
			if !ok {
				return nil, false
			}
			group[offset] = value
		}
		if group[0] < 0 || group[1] < 0 {
			return nil, false
		}
		out = append(out, byte(group[0]<<2|group[1]>>4))
		if group[2] >= 0 {
			out = append(out, byte(group[1]<<4|group[2]>>2))
		}
		if group[3] >= 0 {
			out = append(out, byte(group[2]<<6|group[3]))
		}
	}
	return out, true
}

func base64Value(byte byte) (int, bool) {
	switch {
	case byte >= 'A' && byte <= 'Z':
		return int(byte - 'A'), true
	case byte >= 'a' && byte <= 'z':
		return int(byte-'a') + 26, true
	case byte >= '0' && byte <= '9':
		return int(byte-'0') + 52, true
	case byte == '+':
		return 62, true
	case byte == '/':
		return 63, true
	}
	return -1, false
}

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// encodeBase64 renders canonical standard-alphabet base64 with exact `=`
// padding (RFC 0013 §4.8, §10.1).
func encodeBase64(bytes []byte) string {
	var builder strings.Builder
	for chunk := 0; chunk < len(bytes); chunk += 3 {
		first := uint32(bytes[chunk])
		second := uint32(0)
		third := uint32(0)
		if chunk+1 < len(bytes) {
			second = uint32(bytes[chunk+1])
		}
		if chunk+2 < len(bytes) {
			third = uint32(bytes[chunk+2])
		}
		builder.WriteByte(base64Alphabet[first>>2])
		builder.WriteByte(base64Alphabet[(first&0x03)<<4|second>>4])
		if chunk+1 < len(bytes) {
			builder.WriteByte(base64Alphabet[(second&0x0F)<<2|third>>6])
		} else {
			builder.WriteByte('=')
		}
		if chunk+2 < len(bytes) {
			builder.WriteByte(base64Alphabet[third&0x3F])
		} else {
			builder.WriteByte('=')
		}
	}
	return builder.String()
}

// encodeBase64Wrapped renders canonical base64 wrapped so every line
// carries at most `76 - 8 * depth` characters (RFC 0013 §4.8, §10.1;
// Apple's MAXLINELEN counts the indentation against the budget). The
// first chunk follows the `<data>` tag inline; continuation chunks start
// on a new indented line.
func encodeBase64Wrapped(bytes []byte, depth int) string {
	budget := 76 - 8*depth
	if budget < 1 {
		budget = 1
	}
	var builder strings.Builder
	line := 0
	for chunk := 0; chunk < len(bytes); chunk += 3 {
		if line+4 > budget && line > 0 {
			builder.WriteByte('\n')
			writeIndent(&builder, depth)
			line = 0
		}
		first := uint32(bytes[chunk])
		second := uint32(0)
		third := uint32(0)
		if chunk+1 < len(bytes) {
			second = uint32(bytes[chunk+1])
		}
		if chunk+2 < len(bytes) {
			third = uint32(bytes[chunk+2])
		}
		builder.WriteByte(base64Alphabet[first>>2])
		builder.WriteByte(base64Alphabet[(first&0x03)<<4|second>>4])
		if chunk+1 < len(bytes) {
			builder.WriteByte(base64Alphabet[(second&0x0F)<<2|third>>6])
		} else {
			builder.WriteByte('=')
		}
		if chunk+2 < len(bytes) {
			builder.WriteByte(base64Alphabet[third&0x3F])
		} else {
			builder.WriteByte('=')
		}
		line += 4
	}
	return builder.String()
}

// escapeXMLText escapes XML text content (RFC 0013 §4.9, §10.1): `&`,
// `<`, `>`, and a literal CR, which XML line-end normalization would
// otherwise turn into LF (a character reference is not normalized).
func escapeXMLText(text string) string {
	var builder strings.Builder
	for _, character := range text {
		switch character {
		case '&':
			builder.WriteString("&amp;")
		case '<':
			builder.WriteString("&lt;")
		case '>':
			builder.WriteString("&gt;")
		case '\r':
			builder.WriteString("&#13;")
		default:
			builder.WriteRune(character)
		}
	}
	return builder.String()
}

// writeIndent appends one indentation level of four spaces.
func writeIndent(builder *strings.Builder, depth int) {
	for index := 0; index < depth; index++ {
		builder.WriteString("    ")
	}
}

// renderReal is the deterministic shortest-round-trip decimal spelling of
// one real (RFC 0013 §4.6, §10.1); the special spellings match the frozen
// grammar.
func renderReal(real PlistReal) string {
	value := real.AsFloat64()
	if math.IsNaN(value) {
		return "nan"
	}
	if math.IsInf(value, 1) {
		return "inf"
	}
	if math.IsInf(value, -1) {
		return "-inf"
	}
	return strconv.FormatFloat(value, 'g', -1, 64)
}

// realExpressible reports whether the exact bits of one real survive the
// XML spelling: the shortest-round-trip decimal is exact for every finite
// double, and the special spellings resolve to the canonical NaN bit
// pattern only (consema-plist document.rs real_expressible).
func realExpressible(real PlistReal) bool {
	value := real.AsFloat64()
	if math.IsNaN(value) {
		return math.Float64bits(value) == math.Float64bits(math.NaN())
	}
	if math.IsInf(value, 0) {
		return true
	}
	parsed, err := strconv.ParseFloat(renderReal(real), 64)
	return err == nil && math.Float64bits(parsed) == math.Float64bits(value)
}

// dateRangeError classifies one date decomposition failure under the XML
// calendar grammar (consema-plist document.rs DateRangeError). The zero
// value is the success sentinel, so callers compare against zero.
type dateRangeError uint8

const (
	dateRangeOK dateRangeError = iota
	dateRangeFractionalSeconds
	dateRangeYearOutOfRange
)

// exactUnixSecondsBound is `2^53`: the largest magnitude at which every
// integral double is exactly representable, so the day/second decomposition
// below it is exact.
const exactUnixSecondsBound = 9_007_199_254_740_992.0

// negativeZeroBits is the exact bit pattern of `-0.0`; the XML date
// spelling cannot distinguish signed zeros.
const negativeZeroBits = 0x8000_0000_0000_0000

// wholeSecondDate decomposes exact plist-epoch seconds into XML calendar
// fields (RFC 0013 §4.7, §5.5). The value must be whole-second, and the
// day/second decomposition must be exact: the Unix-seconds value must stay
// below `2^53` and the calendar year within the grammar's 32-bit
// magnitude.
func wholeSecondDate(seconds float64) (year, month, day, hour, minute, second int64, err dateRangeError) {
	if seconds != math.Trunc(seconds) {
		return 0, 0, 0, 0, 0, 0, dateRangeFractionalSeconds
	}
	unix := seconds + plistEpochOffsetUnix
	if math.Abs(unix) >= exactUnixSecondsBound {
		return 0, 0, 0, 0, 0, 0, dateRangeYearOutOfRange
	}
	unixInt := int64(unix)
	days := unixInt / 86400
	secondsOfDay := unixInt % 86400
	if unixInt < 0 && secondsOfDay != 0 {
		days--
		secondsOfDay += 86400
	}
	year, month, day = civilFromDays(days)
	magnitude := year
	if magnitude < 0 {
		magnitude = -magnitude
	}
	if uint64(magnitude) > math.MaxUint32 {
		return 0, 0, 0, 0, 0, 0, dateRangeYearOutOfRange
	}
	return year, month, day, secondsOfDay / 3600, (secondsOfDay % 3600) / 60, secondsOfDay % 60, dateRangeOK
}

// renderDate renders one whole-second XML date spelling of the calendar
// fields (RFC 0013 §4.7).
func renderDate(year, month, day, hour, minute, second int64) string {
	sign := ""
	magnitude := year
	if year < 0 {
		sign = "-"
		magnitude = -magnitude
	}
	return sign + pad4(magnitude) + "-" + pad2(month) + "-" + pad2(day) +
		"T" + pad2(hour) + ":" + pad2(minute) + ":" + pad2(second) + "Z"
}

func pad2(value int64) string {
	if value < 10 {
		return "0" + strconv.FormatInt(value, 10)
	}
	return strconv.FormatInt(value, 10)
}

func pad4(value int64) string {
	text := strconv.FormatInt(value, 10)
	for len(text) < 4 {
		text = "0" + text
	}
	return text
}

// minimalUnsignedWidth returns the minimal marker width of one unsigned
// count: 1, 2, 4, or 8 bytes.
func minimalUnsignedWidth(value uint64) int {
	switch {
	case value <= 0xFF:
		return 1
	case value <= 0xFFFF:
		return 2
	case value <= 0xFFFF_FFFF:
		return 4
	}
	return 8
}

// integerWidth returns the minimal marker width for one signed 64-bit
// integer: negatives always use the signed 8-byte form (RFC 0013 §5.3,
// §10.2).
func integerWidth(value int64) int {
	if value < 0 {
		return 8
	}
	return minimalUnsignedWidth(uint64(value))
}

// uidWidth returns the minimal byte width of one unsigned 32-bit UID value
// (RFC 0013 §5.8).
func uidWidth(value uint64) int {
	switch {
	case value <= 0xFF:
		return 1
	case value <= 0xFFFF:
		return 2
	case value <= 0xFF_FFFF:
		return 3
	}
	return 4
}

// float64Bits returns the exact IEEE-754 binary64 bit pattern of one
// double.
func float64Bits(value float64) uint64 {
	return math.Float64bits(value)
}

// float32FromBits32 returns the exact IEEE-754 binary32 value of one bit
// pattern.
func float32FromBits32(bits uint32) float32 {
	return math.Float32frombits(bits)
}

// refSizeFor returns the smallest width in bytes whose capacity
// (`2^(8 * width)`) exceeds `maxIndex`, satisfying the trailer sufficiency
// checks of RFC 0013 §5.11.
func refSizeFor(maxIndex int) int {
	size := 1
	capacity := 256
	for maxIndex >= capacity && size < 8 {
		size++
		capacity *= 256
	}
	return size
}
