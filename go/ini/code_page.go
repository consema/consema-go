package ini

import (
	"consema.dev/consema/document"
)

// This file implements the strict canonical Windows code-page encode
// direction (materialization.rs code_page_encoding through encoding_rs).
// The frozen tables transcribed from go/document are inverted into
// scalar-to-byte maps at package init; a scalar with no exact byte mapping
// fails the whole operation with UnsupportedEncoding, matching the strict
// encode contract. The two-byte 936/949/950 pages are not admitted on the
// encode side, mirroring go/document's decode posture; 932 encodes exactly
// through the single-scalar table subset plus ASCII and half-width
// katakana.

// malformedByteSentinel marks a byte that encoding_rs 0.8.35 decodes as
// Malformed in the single-byte authority tables.
const malformedByteSentinel = 0xFFFF

// singleByteEncode maps each frozen single-byte page to its exact
// scalar-to-byte table; the lowest byte is preferred when a scalar has
// multiple spellings.
var singleByteEncode = map[uint16]map[rune]byte{}

// cp932Encode maps each frozen CP932 two-byte scalar to its exact code.
var cp932Encode = map[rune]uint16{}

func init() {
	singleByteEncode[874] = invertSingleByte(cp874Table)
	singleByteEncode[1250] = invertSingleByte(cp1250Table)
	singleByteEncode[1251] = invertSingleByte(cp1251Table)
	singleByteEncode[1252] = invertSingleByte(cp1252Table)
	singleByteEncode[1253] = invertSingleByte(cp1253Table)
	singleByteEncode[1254] = invertSingleByte(cp1254Table)
	singleByteEncode[1255] = invertSingleByte(cp1255Table)
	singleByteEncode[1256] = invertSingleByte(cp1256Table)
	singleByteEncode[1257] = invertSingleByte(cp1257Table)
	singleByteEncode[1258] = invertSingleByte(cp1258Table)
	for _, pair := range cp932Table {
		cp932Encode[rune(pair.rune)] = pair.code
	}
}

// invertSingleByte builds the scalar-to-byte map of one page.
func invertSingleByte(table [128]uint16) map[rune]byte {
	inverse := make(map[rune]byte, 128)
	for index, scalar := range table {
		if scalar == malformedByteSentinel {
			continue
		}
		byte := byte(index) + 0x80
		if existing, ok := inverse[rune(scalar)]; !ok || byte < existing {
			inverse[rune(scalar)] = byte
		}
	}
	return inverse
}

// encodeCodePage encodes one text fragment under one frozen Windows code
// page, strictly and bounded (materialization.rs).
func encodeCodePage(text string, page document.WindowsCodePage,
	maxOutputBytes int) ([]byte, *MaterializationFailure) {
	var output []byte
	switch page.Number() {
	case 65001:
		return encodeFragment(text, document.Utf8Encoding(), maxOutputBytes)
	case 874, 1250, 1251, 1252, 1253, 1254, 1255, 1256, 1257, 1258:
		table := singleByteEncode[page.Number()]
		for _, character := range text {
			var encoded byte
			if character < 0x80 {
				encoded = byte(character)
			} else {
				var ok bool
				encoded, ok = table[character]
				if !ok {
					return nil, &MaterializationFailure{Kind: MaterializationUnsupportedEncoding}
				}
			}
			if len(output)+1 > maxOutputBytes {
				return nil, &MaterializationFailure{Kind: MaterializationResourceLimit,
					LimitName: "output-bytes"}
			}
			output = append(output, encoded)
		}
		return output, nil
	case 932:
		for _, character := range text {
			var bytes [2]byte
			size := 0
			switch {
			case character < 0x80:
				bytes[0] = byte(character)
				size = 1
			case character >= 0xFF61 && character <= 0xFF9F:
				bytes[0] = byte(character-0xFF61) + 0xA1
				size = 1
			default:
				code, ok := cp932Encode[character]
				if !ok {
					return nil, &MaterializationFailure{Kind: MaterializationUnsupportedEncoding}
				}
				bytes[0] = byte(code >> 8)
				bytes[1] = byte(code)
				size = 2
			}
			if len(output)+size > maxOutputBytes {
				return nil, &MaterializationFailure{Kind: MaterializationResourceLimit,
					LimitName: "output-bytes"}
			}
			output = append(output, bytes[:size]...)
		}
		return output, nil
	}
	return nil, &MaterializationFailure{Kind: MaterializationUnsupportedEncoding}
}
