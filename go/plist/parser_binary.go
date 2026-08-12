package plist

// This file implements `plist.binary@1` formation (consema-plist
// parser_binary.rs; RFC 0013 §2.2, §3, §5, §12). The parser reads the raw
// `bplist00` byte layout in one deterministic forward pass: header,
// trailer, offset table, then the object table. The mandatory integrity
// checks of RFC 0013 §5.11 run before any object is decoded; a violated
// check makes the affected construct Recovered rather than inventing facts.
// Object-table recovery is prefix-based: the first object that fails any
// structural or value check cuts the proven prefix, every proven object
// keeps its facts and native value, and all bytes from the end of the last
// proven object to the offset table form one error region.
//
// The native arena adds nodes in object-table order so arena indices equal
// object indices; shared references and forward references resolve through
// PlistValueRef, and build rejects cycles (Recovered) and container-depth
// violations (fatal).

import (
	"math"

	"consema.dev/consema/document"
	"consema.dev/consema/protocol"
)

// The exact `bplist00` header bytes (RFC 0013 §5.1).
var binaryHeader = []byte("bplist00")

// The minimum admissible source length: 8-byte header, at least one 1-byte
// object, at least one 1-byte offset entry, and the 32-byte trailer (RFC
// 0013 §2.2).
const minSourceBytes = 42

// The trailer byte length (RFC 0013 §5.10).
const trailerBytes = 32

// The largest legal integer/offset/ref payload width in bytes (RFC 0013
// §5.11).
const maxFieldWidth = 8

// BinaryObjectFact is one proven object-table entry fact (RFC 0013 §8.3
// `plist.object-table@1`).
type BinaryObjectFact struct {
	index  int
	offset int
	marker byte
	span   document.Span
}

// Index returns the object-table ordinal.
func (f BinaryObjectFact) Index() int { return f.index }

// Offset returns the marker byte offset (equals the offset-table entry
// value).
func (f BinaryObjectFact) Offset() int { return f.offset }

// Marker returns the marker byte; the low nibble preserves non-minimal
// width facts.
func (f BinaryObjectFact) Marker() byte { return f.marker }

// Span returns the exact marker-through-payload byte range.
func (f BinaryObjectFact) Span() document.Span { return f.span }

// BinaryOffsetFact is one validated offset-table entry fact (RFC 0013 §8.3
// `plist.object-offset@1`).
type BinaryOffsetFact struct {
	index  int
	offset int
	span   document.Span
}

// Index returns the object-table ordinal of this entry.
func (f BinaryOffsetFact) Index() int { return f.index }

// Offset returns the decoded absolute file offset of the object's marker
// byte.
func (f BinaryOffsetFact) Offset() int { return f.offset }

// Span returns the exact byte range of this entry inside the offset table.
func (f BinaryOffsetFact) Span() document.Span { return f.span }

// BinaryObjectRefFact is one decoded object reference of a proven
// container (RFC 0013 §8.3 `plist.object-refs@1`). For dictionaries, keys
// occupy positions `0..count` and values `count..2*count`.
type BinaryObjectRefFact struct {
	owner    int
	position int
	target   int
	span     document.Span
}

// Owner returns the referencing object index.
func (f BinaryObjectRefFact) Owner() int { return f.owner }

// Position returns the ordinal of this reference within the owner's
// reference block.
func (f BinaryObjectRefFact) Position() int { return f.position }

// Target returns the decoded target object index.
func (f BinaryObjectRefFact) Target() int { return f.target }

// Span returns the exact byte range of this reference inside the owner's
// payload.
func (f BinaryObjectRefFact) Span() document.Span { return f.span }

// BinaryTrailerFacts are the trailer field facts (RFC 0013 §5.10, §8.3
// `plist.trailer-facts@1` and `plist.top-object@1`). The raw field values
// are always recorded — they are bytes of the source — while validity is
// carried by formation diagnostics and status.
type BinaryTrailerFacts struct {
	sortVersion       byte
	offsetIntSize     byte
	objectRefSize     byte
	numObjects        uint64
	topObject         uint64
	offsetTableOffset uint64
	span              document.Span
}

// SortVersion returns the `sortVersion` byte (0 or 1; canonical
// materialization writes 0).
func (f BinaryTrailerFacts) SortVersion() byte { return f.sortVersion }

// OffsetIntSize returns the `offsetIntSize` byte.
func (f BinaryTrailerFacts) OffsetIntSize() byte { return f.offsetIntSize }

// ObjectRefSize returns the `objectRefSize` byte.
func (f BinaryTrailerFacts) ObjectRefSize() byte { return f.objectRefSize }

// NumObjects returns the `numObjects` value.
func (f BinaryTrailerFacts) NumObjects() uint64 { return f.numObjects }

// TopObject returns the `topObject` value (the native document root when
// proven).
func (f BinaryTrailerFacts) TopObject() uint64 { return f.topObject }

// OffsetTableOffset returns the `offsetTableOffset` value.
func (f BinaryTrailerFacts) OffsetTableOffset() uint64 { return f.offsetTableOffset }

// Span returns the exact byte range of the 32-byte trailer.
func (f BinaryTrailerFacts) Span() document.Span { return f.span }

// BinaryFacts are the complete binary structure facts of one parse (RFC
// 0013 §8.3).
type BinaryFacts struct {
	objects []BinaryObjectFact
	offsets []BinaryOffsetFact
	refs    []BinaryObjectRefFact
	trailer BinaryTrailerFacts
}

// Objects returns the proven object facts in object-table order.
func (f *BinaryFacts) Objects() []BinaryObjectFact { return f.objects }

// Offsets returns the validated offset-table entry facts in object-table
// order.
func (f *BinaryFacts) Offsets() []BinaryOffsetFact { return f.offsets }

// Refs returns the proven reference facts ordered by owner then position.
func (f *BinaryFacts) Refs() []BinaryObjectRefFact { return f.refs }

// Trailer returns the trailer field facts.
func (f *BinaryFacts) Trailer() BinaryTrailerFacts { return f.trailer }

// shapeKind is the decoded kind of one object, without its payload value.
type shapeKind uint8

const (
	shapeFalse shapeKind = iota
	shapeTrue
	shapeInteger
	shapeReal
	shapeDate
	shapeData
	shapeAsciiString
	shapeUtf16String
	shapeUID
	shapeArray
	shapeDict
)

func (k shapeKind) isString() bool {
	return k == shapeAsciiString || k == shapeUtf16String
}

// refTarget is one decoded object-table reference with its exact byte
// span.
type refTarget struct {
	target int
	span   document.Span
}

// objectShape is the structural facts of one object: kind, marker, byte
// extent, and refs.
type objectShape struct {
	kind         shapeKind
	marker       byte
	offset       int
	extent       int
	count        int
	keyCount     int
	payloadStart int
	refs         []refTarget
}

// binaryParser is the formation state for one binary source.
type binaryParser struct {
	authority document.DocumentAuthority
	source    *document.SourceSnapshot
	limits    PlistParseLimits
	sink      *diagnosticSink
	recovered bool
	// headerOK records whether the source starts with the `bplist00` magic.
	// A source whose header is not the magic is not a binary plist at all:
	// its trailer bytes are plain source text, so a trailer field can never
	// be a genuine limit breach (RFC 0013 §5.1; the frozen Foundation fact
	// "an XML fixture under the binary profile is Recovered with the header
	// diagnostic and no native model"). Trailer limit checks are demoted to
	// recoveries for such sources; the genuine binary path keeps them fatal.
	headerOK bool
	uidCount int
	extended int
	facts    int
}

// parseBinaryDocument forms one `plist.binary@1` document from a validated
// source snapshot.
func parseBinaryDocument(authority document.DocumentAuthority, snapshot *document.SourceSnapshot,
	limits PlistParseLimits) (*Document, *FormationFailure) {
	parser := &binaryParser{
		authority: authority,
		source:    snapshot,
		limits:    limits,
		sink:      newDiagnosticSink(limits.Common.MaxDiagnostics),
	}
	return parser.parse()
}

func (p *binaryParser) parse() (*Document, *FormationFailure) {
	bytes := p.source.Bytes()
	length := len(bytes)
	if length < minSourceBytes {
		return nil, p.fatalDiagnostics("plist.binary.minimum-size@1", protocol.CategorySyntax, nil)
	}
	trailerStart := length - trailerBytes

	// Header (RFC 0013 §5.1): any other version string is Recovered.
	headerOK := string(bytes[:8]) == string(binaryHeader)
	p.headerOK = headerOK
	if !headerOK {
		p.recover("plist.binary.header@1", p.locationOfRaw(0, 8),
			map[string]string{"expected": "bplist00"})
	}

	// Trailer facts are bytes of the source and are always recorded.
	raw := readRawTrailer(bytes)
	if failure := p.recordFact(); failure != nil {
		return nil, failure
	}
	trailerFacts := BinaryTrailerFacts{
		sortVersion:       raw.sortVersion,
		offsetIntSize:     raw.offsetIntSize,
		objectRefSize:     raw.objectRefSize,
		numObjects:        raw.numObjects,
		topObject:         raw.topObject,
		offsetTableOffset: raw.offsetTableOffset,
		span:              p.mustSpan(trailerStart, length),
	}

	// Mandatory integrity checks run before any object is decoded (RFC
	// 0013 §5.11). A limit or arithmetic-overflow violation is fatal; the
	// `Ok(false)` outcome (an unrecoverable trailer, RFC 0013 §5.11
	// mandatory checks) still forms a Recovered document with exhaustive
	// error-region coverage and no native document.
	trailerOK, limitFailure := p.validateTrailer(&raw)
	if limitFailure != nil {
		return nil, limitFailure
	}
	if !trailerOK {
		regions := []document.BinaryRegion{
			p.region(0, map[bool]string{true: "header", false: "error-region"}[headerOK], 0, 8),
			p.region(1, "error-region", 8, trailerStart),
			p.region(2, "error-region", trailerStart, length),
		}
		return p.finish(nil, &BinaryFacts{trailer: trailerFacts}, regions)
	}

	offsetTableOffset := int(raw.offsetTableOffset)
	numObjects := int(raw.numObjects)
	offsetIntSize := int(raw.offsetIntSize)
	objectRefSize := int(raw.objectRefSize)
	tableBytes, overflow := mulInt(numObjects, offsetIntSize)
	if overflow {
		return nil, p.overflowFailure()
	}
	if tableBytes > p.limits.MaxOffsetTableBytes {
		return nil, p.fatalLimit("offset-table-bytes", tableBytes, p.limits.MaxOffsetTableBytes)
	}

	offsetFacts, objectOffsets, entryCut, factFailure := p.readOffsetTable(
		offsetTableOffset, numObjects, offsetIntSize)
	if factFailure != nil {
		return nil, factFailure
	}
	shapes, shapeCut, scanFailure := p.scanObjects(objectOffsets, entryCut,
		offsetTableOffset, objectRefSize, numObjects)
	if scanFailure != nil {
		return nil, scanFailure
	}
	cut := p.verifyDictKeys(shapes, shapeCut)

	// Native document eligibility: the top object and every reference of a
	// proven object must stay inside the proven prefix.
	topObject := int(raw.topObject)
	nativeUnproven := false
	if topObject >= cut {
		p.recover("plist.binary.unproven-top-object@1",
			p.locationOfRaw(trailerStart+16, trailerStart+24),
			map[string]string{"top-object": itoa(topObject)})
		nativeUnproven = true
	}
	for owner := 0; owner < cut; owner++ {
		for _, reference := range shapes[owner].refs {
			if reference.target >= cut {
				p.recover("plist.binary.unproven-reference@1",
					p.locationOfRaw(reference.span.StartByte(), reference.span.EndByte()),
					map[string]string{"owner": itoa(owner), "target": itoa(reference.target)})
				nativeUnproven = true
				break
			}
		}
		if nativeUnproven {
			break
		}
	}

	var native *PlistDocument
	if !nativeUnproven {
		values, failure := p.buildValues(shapes, cut)
		if failure != nil {
			return nil, failure
		}
		builder := NewPlistDocumentBuilderWithLimits(p.limits.arenaLimits())
		for _, value := range values {
			if _, err := builder.Add(value); err != nil {
				if arenaError, ok := err.(*PlistArenaError); ok &&
					arenaError.Kind == PlistArenaErrorObjectLimitExceeded {
					return nil, p.fatalLimit("object-count", cut, arenaError.Limit)
				}
				return nil, p.internalFailure()
			}
		}
		built, err := builder.Build(PlistValueRef{index: topObject})
		if err != nil {
			if arenaError, ok := err.(*PlistArenaError); ok {
				switch arenaError.Kind {
				case PlistArenaErrorCycleDetected:
					p.recover("plist.binary.cycle@1", nil, nil)
					native = nil
				case PlistArenaErrorContainerDepthLimitExceeded:
					return nil, p.fatalLimit("container-depth", arenaError.Node.Index(),
						arenaError.Limit)
				default:
					return nil, p.internalFailure()
				}
			} else {
				return nil, p.internalFailure()
			}
		} else {
			native = built
		}
	}

	// Facts of the proven prefix (RFC 0013 §8.3).
	objects := make([]BinaryObjectFact, 0, cut)
	for index := 0; index < cut; index++ {
		if failure := p.recordFact(); failure != nil {
			return nil, failure
		}
		objects = append(objects, BinaryObjectFact{
			index:  index,
			offset: shapes[index].offset,
			marker: shapes[index].marker,
			span:   p.mustSpan(shapes[index].offset, shapes[index].offset+shapes[index].extent),
		})
	}
	refs := make([]BinaryObjectRefFact, 0)
	for owner := 0; owner < cut; owner++ {
		for position, reference := range shapes[owner].refs {
			if failure := p.recordFact(); failure != nil {
				return nil, failure
			}
			refs = append(refs, BinaryObjectRefFact{
				owner: owner, position: position, target: reference.target,
				span: reference.span,
			})
		}
	}
	facts := &BinaryFacts{objects: objects, offsets: offsetFacts, refs: refs, trailer: trailerFacts}

	// Exhaustive region coverage: positional structures, proven parts of
	// the object table, error regions for unproven bytes, and padding for
	// format-admitted gaps.
	regions := make([]document.BinaryRegion, 0, 4)
	regions = append(regions, p.region(0,
		map[bool]string{true: "header", false: "error-region"}[headerOK], 0, 8))
	if cut > 0 {
		lastEnd := shapes[cut-1].offset + shapes[cut-1].extent
		regions = append(regions, p.region(1, "object-table", 8, lastEnd))
		if cut < numObjects {
			if lastEnd < offsetTableOffset {
				regions = append(regions, p.region(2, "error-region", lastEnd, offsetTableOffset))
			}
		} else if lastEnd < offsetTableOffset {
			regions = append(regions, p.region(2, "padding", lastEnd, offsetTableOffset))
		}
	} else if 8 < offsetTableOffset {
		regions = append(regions, p.region(1, "error-region", 8, offsetTableOffset))
	}
	regions = append(regions, p.region(len(regions), "offset-table",
		offsetTableOffset, offsetTableOffset+tableBytes))
	regions = append(regions, p.region(len(regions), "trailer", trailerStart, length))
	return p.finish(native, facts, regions)
}

func (p *binaryParser) finish(native *PlistDocument, facts *BinaryFacts,
	regions []document.BinaryRegion) (*Document, *FormationFailure) {
	errorRegions := 0
	for _, region := range regions {
		if region.Kind() == "error-region" {
			errorRegions++
		}
	}
	if errorRegions > p.limits.MaxRecoveryRegions {
		return nil, p.fatalLimit("recovery-regions", errorRegions, p.limits.MaxRecoveryRegions)
	}
	index, err := document.NewBinaryStructuralIndex(p.authority.Identity(), p.source.Len(), regions)
	if err != nil {
		return nil, p.coverageFailure()
	}
	status := document.FormationStatusComplete
	if p.recovered {
		status = document.FormationStatusRecovered
	}
	return &Document{
		authority:      p.authority,
		source:         p.source,
		representation: PlistRepresentationBinary,
		status:         status,
		diagnostics:    p.sink.finish(),
		native:         native,
		binaryFacts:    facts,
		binaryIndex:    index,
		limits:         p.limits,
	}, nil
}

// validateTrailer enforces the mandatory trailer checks (RFC 0013 §5.11)
// and records a `plist.binary.trailer@1` diagnostic per violated check.
// The result is `(false, failure)` when a configured limit or the checked
// arithmetic fails — the same fatal outcome as the Rust parser — and
// `(false, nil)` when a mandatory check merely fails (formation continues
// as Recovered with exhaustive error-region coverage).
func (p *binaryParser) validateTrailer(raw *rawTrailer) (bool, *FormationFailure) {
	ok := true
	length := p.source.Len()
	start := length - trailerBytes

	if raw.unused != [5]byte{} {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start, start+5),
			map[string]string{"check": "unused-bytes"})
		ok = false
	}
	if raw.sortVersion != 0 && raw.sortVersion != 1 {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+5, start+6),
			map[string]string{"check": "sort-version",
				"sort-version": "0x" + strconvHex(raw.sortVersion)})
		ok = false
	}
	if raw.offsetIntSize < 1 || raw.offsetIntSize > maxFieldWidth {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+6, start+7),
			map[string]string{"check": "offset-int-size",
				"offset-int-size": itoa(int(raw.offsetIntSize))})
		ok = false
	} else if int(raw.offsetIntSize) > p.limits.MaxOffsetIntSize {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.fatalLimit("offset-int-size", int(raw.offsetIntSize),
			p.limits.MaxOffsetIntSize)
	}
	if raw.objectRefSize < 1 || raw.objectRefSize > maxFieldWidth {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+7, start+8),
			map[string]string{"check": "object-ref-size",
				"object-ref-size": itoa(int(raw.objectRefSize))})
		ok = false
	} else if int(raw.objectRefSize) > p.limits.MaxObjectRefSize {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.fatalLimit("object-ref-size", int(raw.objectRefSize),
			p.limits.MaxObjectRefSize)
	}
	if raw.numObjects == 0 {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+8, start+16),
			map[string]string{"check": "num-objects"})
		ok = false
	} else if raw.numObjects > uint64(p.limits.MaxObjectCount) {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.fatalLimit("object-count", int(raw.numObjects),
			p.limits.MaxObjectCount)
	}
	if raw.topObject >= raw.numObjects {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+16, start+24),
			map[string]string{"check": "top-object", "top-object": strconvUint(raw.topObject)})
		ok = false
	}
	maxTableOffset := uint64(length - trailerBytes)
	if raw.offsetTableOffset < 9 || raw.offsetTableOffset >= maxTableOffset {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start+24, start+32),
			map[string]string{"check": "offset-table-offset",
				"offset-table-offset": strconvUint(raw.offsetTableOffset)})
		ok = false
	}
	if raw.offsetIntSize >= 1 && raw.offsetIntSize < maxFieldWidth {
		capacity := uint64(1) << (8 * raw.offsetIntSize)
		if capacity <= raw.offsetTableOffset {
			p.recover("plist.binary.trailer@1", p.locationOfRaw(start+24, start+32),
				map[string]string{"check": "offset-int-size-sufficiency"})
			ok = false
		}
	}
	if raw.objectRefSize >= 1 && raw.objectRefSize < maxFieldWidth {
		capacity := uint64(1) << (8 * raw.objectRefSize)
		if capacity <= raw.numObjects {
			p.recover("plist.binary.trailer@1", p.locationOfRaw(start+7, start+8),
				map[string]string{"check": "object-ref-size-sufficiency"})
			ok = false
		}
	}
	tableBytes, overflow := mulUint(raw.numObjects, uint64(raw.offsetIntSize))
	if overflow {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.overflowFailure()
	}
	expected, overflow := addUint(raw.offsetTableOffset, tableBytes)
	if overflow {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.overflowFailure()
	}
	expected, overflow = addUint(expected, trailerBytes)
	if overflow {
		if !p.headerOK {
			return false, nil // not a binary plist; the trailer is source text
		}
		return false, p.overflowFailure()
	}
	if expected != uint64(length) {
		p.recover("plist.binary.trailer@1", p.locationOfRaw(start, length),
			map[string]string{"check": "total-length",
				"expected": strconvUint(expected), "observed": itoa(length)})
		ok = false
	}
	return ok, nil
}

// readOffsetTable reads and validates the offset table in entry order (RFC
// 0013 §5.10, §5.11). The first invalid entry cuts the proven prefix; a
// binary-facts limit breach is fatal.
func (p *binaryParser) readOffsetTable(offsetTableOffset, numObjects,
	offsetIntSize int) ([]BinaryOffsetFact, []int, int, *FormationFailure) {
	bytes := p.source.Bytes()
	facts := make([]BinaryOffsetFact, 0, numObjects)
	offsets := make([]int, 0, numObjects)
	cut := numObjects
	for index := 0; index < numObjects; index++ {
		start := offsetTableOffset + index*offsetIntSize
		end := start + offsetIntSize
		if end > len(bytes) {
			locationStart := start
			if locationStart > len(bytes)-1 {
				locationStart = len(bytes) - 1
			}
			p.recover("plist.binary.offset-table@1",
				p.locationOfRaw(locationStart, len(bytes)),
				map[string]string{"index": itoa(index), "end": itoa(end)})
			cut = index
			break
		}
		value := readBEUint(bytes, start, offsetIntSize)
		if value < 8 || int(value) >= offsetTableOffset {
			p.recover("plist.binary.offset-table@1", p.locationOfRaw(start, end),
				map[string]string{"index": itoa(index), "value": "0x" + strconvHexOfUint(value)})
			cut = index
			break
		}
		if failure := p.recordFact(); failure != nil {
			return nil, nil, index, failure
		}
		facts = append(facts, BinaryOffsetFact{
			index: index, offset: int(value), span: p.mustSpan(start, end)})
		offsets = append(offsets, int(value))
	}
	return facts, offsets, cut, nil
}

// scanObjects scans objects in index order and returns the proven shapes
// plus the prefix cut (RFC 0013 §5.2-5.9). A shape's fatal limit failure
// is propagated; a shape fault only cuts the proven prefix.
func (p *binaryParser) scanObjects(objectOffsets []int, cut, offsetTableOffset,
	objectRefSize, numObjects int) ([]objectShape, int, *FormationFailure) {
	shapes := make([]objectShape, 0, cut)
	for index := 0; index < cut; index++ {
		shape, ok, fatal := p.scanObject(index, objectOffsets[index], offsetTableOffset,
			objectRefSize, numObjects)
		if fatal != nil {
			return nil, index, fatal
		}
		if !ok {
			cut = index
			break
		}
		shapes = append(shapes, shape)
	}
	return shapes, cut, nil
}

// scanObject decodes one object's marker, size, extent, and references;
// ok=false is a fault that cuts the proven prefix at index.
func (p *binaryParser) scanObject(index, offset, tableEnd, objectRefSize,
	numObjects int) (objectShape, bool, *FormationFailure) {
	bytes := p.source.Bytes()
	if offset >= len(bytes) {
		p.recover("plist.binary.offset-table@1",
			p.locationOfRaw(len(bytes)-1, len(bytes)),
			map[string]string{"index": itoa(index), "value": "0x" + strconvHexOfUint(uint64(offset))})
		return objectShape{}, false, nil
	}
	marker := bytes[offset]
	var kind shapeKind
	var count, extBytes int
	switch {
	case marker == 0x08:
		kind = shapeFalse
	case marker == 0x09:
		kind = shapeTrue
	case marker >= 0x10 && marker <= 0x13:
		kind = shapeInteger
	case marker == 0x22:
		kind = shapeReal
	case marker == 0x23:
		kind = shapeReal
	case marker == 0x33:
		kind = shapeDate
	case marker >= 0x40 && marker <= 0x4F:
		kind = shapeData
		count, extBytes, ok, fatal := p.sizedCount(marker, offset, index)
		if fatal != nil {
			return objectShape{}, false, fatal
		}
		if !ok {
			return objectShape{}, false, nil
		}
		if count > p.limits.MaxDataBytes {
			return objectShape{}, false, p.fatalLimit("data-bytes", count, p.limits.MaxDataBytes)
		}
		return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
			objectRefSize, numObjects, index)
	case marker >= 0x50 && marker <= 0x5F:
		kind = shapeAsciiString
		count, extBytes, ok, fatal := p.sizedCount(marker, offset, index)
		if fatal != nil {
			return objectShape{}, false, fatal
		}
		if !ok {
			return objectShape{}, false, nil
		}
		if count > p.limits.MaxStringCodeUnits {
			return objectShape{}, false, p.fatalLimit("string-code-units", count, p.limits.MaxStringCodeUnits)
		}
		return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
			objectRefSize, numObjects, index)
	case marker >= 0x60 && marker <= 0x6F:
		kind = shapeUtf16String
		count, extBytes, ok, fatal := p.sizedCount(marker, offset, index)
		if fatal != nil {
			return objectShape{}, false, fatal
		}
		if !ok {
			return objectShape{}, false, nil
		}
		if count > p.limits.MaxStringCodeUnits {
			return objectShape{}, false, p.fatalLimit("string-code-units", count, p.limits.MaxStringCodeUnits)
		}
		return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
			objectRefSize, numObjects, index)
	case marker >= 0x80 && marker <= 0x8F:
		kind = shapeUID
		count = int(marker&0x0F) + 1
	case marker >= 0xA0 && marker <= 0xAF:
		kind = shapeArray
		count, extBytes, ok, fatal := p.sizedCount(marker, offset, index)
		if fatal != nil {
			return objectShape{}, false, fatal
		}
		if !ok {
			return objectShape{}, false, nil
		}
		if count > p.limits.MaxArrayElements {
			return objectShape{}, false, p.fatalLimit("array-elements", count, p.limits.MaxArrayElements)
		}
		return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
			objectRefSize, numObjects, index)
	case marker >= 0xD0 && marker <= 0xDF:
		kind = shapeDict
		count, extBytes, ok, fatal := p.sizedCount(marker, offset, index)
		if fatal != nil {
			return objectShape{}, false, fatal
		}
		if !ok {
			return objectShape{}, false, nil
		}
		if count > p.limits.MaxDictEntries {
			return objectShape{}, false, p.fatalLimit("dict-entries", count, p.limits.MaxDictEntries)
		}
		return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
			objectRefSize, numObjects, index)
	default:
		p.recover("plist.binary.marker@1", p.locationOfRaw(offset, offset+1),
			map[string]string{"marker": "0x" + strconvHex(marker), "object": itoa(index)})
		return objectShape{}, false, nil
	}
	return p.continueScan(kind, marker, offset, tableEnd, count, extBytes,
		objectRefSize, numObjects, index)
}

// continueScan completes one object shape after the marker branch.
func (p *binaryParser) continueScan(kind shapeKind, marker byte, offset, tableEnd,
	count, extBytes, objectRefSize, numObjects, index int) (objectShape, bool, *FormationFailure) {
	bytes := p.source.Bytes()
	payloadStart := offset + 1 + extBytes
	payloadLen := 0
	switch kind {
	case shapeUID, shapeData, shapeAsciiString, shapeFalse, shapeTrue:
		payloadLen = count
	case shapeInteger, shapeReal:
		payloadLen = integerPayloadWidth(marker, kind)
	case shapeDate:
		payloadLen = 8
	case shapeUtf16String:
		payloadLen = count * 2
	case shapeArray:
		payloadLen = count * objectRefSize
	case shapeDict:
		payloadLen = count * 2 * objectRefSize
	}
	extent := 1 + extBytes + payloadLen
	end := offset + extent
	if end > tableEnd {
		p.recover("plist.binary.extent@1", p.locationOfRaw(offset, offset+1),
			map[string]string{"object": itoa(index), "end": itoa(end),
				"table-end": itoa(tableEnd)})
		return objectShape{}, false, nil
	}

	// Value-validity checks that cut the prefix here (RFC 0013 §5.5-5.8).
	switch kind {
	case shapeAsciiString:
		for at := payloadStart; at < end; at++ {
			if bytes[at] >= 0x80 {
				p.recover("plist.binary.string@1", p.locationOfRaw(at, at+1),
					map[string]string{"byte": "0x" + strconvHex(bytes[at]), "object": itoa(index)})
				return objectShape{}, false, nil
			}
		}
	case shapeDate:
		seconds := float64FromBits(readBEUint(bytes, payloadStart, 8))
		if !isFinite(seconds) {
			p.recover("plist.binary.date@1", p.locationOfRaw(payloadStart, payloadStart+8),
				map[string]string{"object": itoa(index)})
			return objectShape{}, false, nil
		}
	case shapeUID:
		value := readBEUint(bytes, payloadStart, count)
		if value > 0xFFFF_FFFF {
			p.recover("plist.binary.uid@1", p.locationOfRaw(payloadStart, payloadStart+count),
				map[string]string{"value": "0x" + strconvHexOfUint(value), "object": itoa(index)})
			return objectShape{}, false, nil
		}
		p.uidCount++
		if p.uidCount > p.limits.MaxUIDCount {
			return objectShape{}, false, p.fatalLimit("uid-count", p.uidCount, p.limits.MaxUIDCount)
		}
	}

	// Container references (RFC 0013 §5.9).
	refs := make([]refTarget, 0)
	if kind == shapeArray || kind == shapeDict {
		total := count
		if kind == shapeDict {
			total = count * 2
		}
		for position := 0; position < total; position++ {
			refStart := payloadStart + position*objectRefSize
			refEnd := refStart + objectRefSize
			target := readBEUint(bytes, refStart, objectRefSize)
			if int(target) >= numObjects {
				p.recover("plist.binary.reference@1", p.locationOfRaw(refStart, refEnd),
					map[string]string{"owner": itoa(index), "target": itoa(int(target))})
				return objectShape{}, false, nil
			}
			refs = append(refs, refTarget{target: int(target), span: p.mustSpan(refStart, refEnd)})
		}
		p.facts += total
		if p.facts > p.limits.MaxBinaryFacts {
			return objectShape{}, false, p.fatalLimit("binary-facts", p.facts, p.limits.MaxBinaryFacts)
		}
	}
	keyCount := 0
	if kind == shapeDict {
		keyCount = count
	}
	return objectShape{
		kind: kind, marker: marker, offset: offset, extent: extent,
		count: count, keyCount: keyCount, payloadStart: payloadStart, refs: refs,
	}, true, nil
}

func integerPayloadWidth(marker byte, kind shapeKind) int {
	if kind == shapeInteger {
		return 1 << (marker & 0x0F)
	}
	if marker == 0x22 {
		return 4
	}
	return 8
}

// sizedCount reads a sized construct's count, honoring the extended-size
// integer rule (RFC 0013 §5.4); ok=false is a fault, fatal is a limit
// breach.
func (p *binaryParser) sizedCount(marker byte, objectOffset, index int) (int, int, bool, *FormationFailure) {
	nibble := int(marker & 0x0F)
	if nibble != 0x0F {
		return nibble, 0, true, nil
	}
	return p.readCount(objectOffset, index)
}

// readCount reads one extended-size integer and enforces its limits (RFC
// 0013 §5.4, §12); ok=false is a fault, fatal is a limit breach.
func (p *binaryParser) readCount(objectOffset, index int) (int, int, bool, *FormationFailure) {
	bytes := p.source.Bytes()
	if objectOffset+1 >= len(bytes) {
		p.recover("plist.binary.offset-table@1",
			p.locationOfRaw(len(bytes)-1, len(bytes)),
			map[string]string{"index": itoa(index),
				"value": "0x" + strconvHexOfUint(uint64(objectOffset))})
		return 0, 0, false, nil
	}
	marker := bytes[objectOffset+1]
	if marker < 0x10 || marker > 0x13 {
		p.recover("plist.binary.extended-size@1", p.locationOfRaw(objectOffset+1, objectOffset+2),
			map[string]string{"marker": "0x" + strconvHex(marker), "object": itoa(index)})
		return 0, 0, false, nil
	}
	width := 1 << (marker & 0x0F)
	value := readBEUint(bytes, objectOffset+2, width)
	if value > uint64(p.limits.MaxExtendedSizeValue) {
		return 0, 0, false, p.fatalLimit("extended-size-value", int(value),
			p.limits.MaxExtendedSizeValue)
	}
	p.extended++
	if p.extended > p.limits.MaxExtendedSizeIntegers {
		return 0, 0, false, p.fatalLimit("extended-size-integers", p.extended,
			p.limits.MaxExtendedSizeIntegers)
	}
	return int(value), 1 + width, true, nil
}

// verifyDictKeys verifies that every dictionary key target is a string
// object (RFC 0013 §5.9); the first violating dictionary cuts the proven
// prefix.
func (p *binaryParser) verifyDictKeys(shapes []objectShape, cut int) int {
	for index := 0; index < cut; index++ {
		shape := shapes[index]
		if shape.kind != shapeDict {
			continue
		}
		for _, keyRef := range shape.refs[:shape.keyCount] {
			if keyRef.target >= cut {
				continue
			}
			if !shapes[keyRef.target].kind.isString() {
				p.recover("plist.binary.non-string-key@1",
					p.locationOfRaw(keyRef.span.StartByte(), keyRef.span.EndByte()),
					map[string]string{"key-object": itoa(keyRef.target), "object": itoa(index)})
				return index
			}
		}
	}
	return cut
}

// buildValues builds native values in object-table order so arena indices
// equal object indices; the caller guarantees every reference stays inside
// the proven prefix and every key target is a string.
func (p *binaryParser) buildValues(shapes []objectShape, cut int) ([]PlistValue, *FormationFailure) {
	bytes := p.source.Bytes()
	values := make([]PlistValue, 0, cut)
	for index := 0; index < cut; index++ {
		shape := shapes[index]
		var value PlistValue
		switch shape.kind {
		case shapeFalse:
			value = NewPlistValueBoolean(NewPlistBoolean(false))
		case shapeTrue:
			value = NewPlistValueBoolean(NewPlistBoolean(true))
		case shapeInteger:
			value = NewPlistValueInteger(NewPlistInteger(p.readInteger(shape.payloadStart,
				integerPayloadWidth(shape.marker, shape.kind))))
		case shapeReal:
			width := integerPayloadWidth(shape.marker, shape.kind)
			bits := readBEUint(bytes, shape.payloadStart, width)
			if width == 4 {
				value = NewPlistValueReal(NewPlistRealFromBits(RealWidthFloat32, bits))
			} else {
				value = NewPlistValueReal(NewPlistRealFromBits(RealWidthFloat64, bits))
			}
		case shapeDate:
			seconds := float64FromBits(readBEUint(bytes, shape.payloadStart, 8))
			date, valid := NewPlistDateFromSeconds(seconds)
			if !valid {
				return nil, p.internalFailure()
			}
			value = NewPlistValueDate(date)
		case shapeData:
			value = NewPlistValueData(NewPlistDataFromBytes(
				bytes[shape.payloadStart : shape.payloadStart+shape.count]))
		case shapeAsciiString:
			units := make([]uint16, 0, shape.count)
			for at := shape.payloadStart; at < shape.payloadStart+shape.count; at++ {
				units = append(units, uint16(bytes[at]))
			}
			value = NewPlistValueString(NewPlistStringFromCodeUnits(units))
		case shapeUtf16String:
			units := make([]uint16, 0, shape.count)
			at := shape.payloadStart
			for position := 0; position < shape.count; position++ {
				units = append(units, uint16(bytes[at])<<8|uint16(bytes[at+1]))
				at += 2
			}
			value = NewPlistValueString(NewPlistStringFromCodeUnits(units))
		case shapeUID:
			value = NewPlistValueUID(NewPlistUID(uint32(readBEUint(bytes, shape.payloadStart,
				shape.count))))
		case shapeArray:
			elements := make([]PlistValueRef, 0, shape.count)
			for _, reference := range shape.refs {
				elements = append(elements, PlistValueRef{index: reference.target})
			}
			value = NewPlistValueArray(NewPlistArrayFromElements(elements))
		case shapeDict:
			value = NewPlistValueDict(NewPlistDictFromEntries(nil))
		}
		values = append(values, value)
	}
	// Dictionary entries need the key target's string content, which is
	// only complete after every node exists; forward key references are
	// therefore materialized in a second pass.
	for index := 0; index < cut; index++ {
		shape := shapes[index]
		if shape.kind != shapeDict {
			continue
		}
		entries := make([]PlistDictEntry, 0, shape.keyCount)
		groups := map[string]int{}
		for position := 0; position < shape.keyCount; position++ {
			keyRef := shape.refs[position]
			stringValue, ok := values[keyRef.target].AsString()
			if !ok {
				return nil, p.internalFailure()
			}
			key := NewPlistKeyFromString(stringValue)
			group := groups[codeUnitKey(key)] + 1
			groups[codeUnitKey(key)] = group
			if group > p.limits.MaxDuplicateKeyGroupMembers {
				return nil, p.fatalLimit("duplicate-key-group", group,
					p.limits.MaxDuplicateKeyGroupMembers)
			}
			entries = append(entries, NewPlistDictEntry(key,
				PlistValueRef{index: shape.refs[shape.keyCount+position].target}))
		}
		values[index] = NewPlistValueDict(NewPlistDictFromEntries(entries))
	}
	return values, nil
}

func (p *binaryParser) readInteger(payloadStart, width int) int64 {
	bytes := p.source.Bytes()
	if width < 8 {
		// 1-, 2-, and 4-byte integers are unsigned (RFC 0013 §5.3).
		return int64(readBEUint(bytes, payloadStart, width))
	}
	// 8-byte integers are signed two's complement (RFC 0013 §5.3).
	var raw [8]byte
	copy(raw[:], bytes[payloadStart:payloadStart+8])
	return int64(uint64(raw[0])<<56 | uint64(raw[1])<<48 | uint64(raw[2])<<40 |
		uint64(raw[3])<<32 | uint64(raw[4])<<24 | uint64(raw[5])<<16 |
		uint64(raw[6])<<8 | uint64(raw[7]))
}

// recordFact records one structural fact against the binary-facts limit.
func (p *binaryParser) recordFact() *FormationFailure {
	p.facts++
	if p.facts > p.limits.MaxBinaryFacts {
		return p.fatalLimit("binary-facts", p.facts, p.limits.MaxBinaryFacts)
	}
	return nil
}

// recover records one recovery diagnostic and marks the parse Recovered.
func (p *binaryParser) recover(code string, location *protocol.SourceLocation,
	arguments map[string]string) {
	p.recovered = true
	p.sink.push(newDiagnostic(code, protocol.CategorySyntax, protocol.SeverityError,
		location, arguments, 0))
}

func (p *binaryParser) mustSpan(start, end int) document.Span {
	span, err := p.authority.Span(start, end)
	if err != nil {
		return document.Span{}
	}
	return span
}

func (p *binaryParser) locationOfRaw(start, end int) *protocol.SourceLocation {
	return locationOf(p.authority, start, end)
}

func (p *binaryParser) region(index int, kind string, start, end int) document.BinaryRegion {
	return document.NewBinaryRegion(
		p.authority.NodeRef(uint64(index), document.RoleBinaryRegion),
		p.mustSpan(start, end), kind)
}

// fatalLimit builds the `plist.limit.<name>@1` resource-limit failure.
func (p *binaryParser) fatalLimit(name string, observed, limit int) *FormationFailure {
	return &FormationFailure{
		Kind: FormationFailureResourceLimit, Name: name, Observed: observed, Limit: limit,
		Diagnostics: []*protocol.Diagnostic{newDiagnostic("plist.limit."+name+"@1",
			protocol.CategoryResource, protocol.SeverityError, nil,
			map[string]string{"limit": itoa(limit), "observed": itoa(observed)}, 0)},
	}
}

func (p *binaryParser) fatalDiagnostics(code string, category protocol.DiagnosticCategory,
	location *protocol.SourceLocation) *FormationFailure {
	return &FormationFailure{
		Diagnostics: []*protocol.Diagnostic{newDiagnostic(code, category,
			protocol.SeverityError, location, nil, 0)},
	}
}

func (p *binaryParser) overflowFailure() *FormationFailure {
	return p.fatalDiagnostics("plist.binary.overflow@1", protocol.CategoryResource, nil)
}

func (p *binaryParser) internalFailure() *FormationFailure {
	return p.fatalDiagnostics("plist.binary.internal@1", protocol.CategoryResource, nil)
}

func (p *binaryParser) coverageFailure() *FormationFailure {
	return p.fatalDiagnostics("plist.binary.coverage@1", protocol.CategorySyntax, nil)
}

// rawTrailer is the raw trailer field layout (RFC 0013 §5.10).
type rawTrailer struct {
	unused            [5]byte
	sortVersion       byte
	offsetIntSize     byte
	objectRefSize     byte
	numObjects        uint64
	topObject         uint64
	offsetTableOffset uint64
}

func readRawTrailer(bytes []byte) rawTrailer {
	start := len(bytes) - trailerBytes
	return rawTrailer{
		unused:            [5]byte{bytes[start], bytes[start+1], bytes[start+2], bytes[start+3], bytes[start+4]},
		sortVersion:       bytes[start+5],
		offsetIntSize:     bytes[start+6],
		objectRefSize:     bytes[start+7],
		numObjects:        readBEUint(bytes, start+8, 8),
		topObject:         readBEUint(bytes, start+16, 8),
		offsetTableOffset: readBEUint(bytes, start+24, 8),
	}
}

// readBEUint reads one big-endian unsigned value of the given width; the
// caller pre-validates the window.
func readBEUint(bytes []byte, start, width int) uint64 {
	var value uint64
	for index := 0; index < width; index++ {
		value = value<<8 | uint64(bytes[start+index])
	}
	return value
}

// writeBE appends one big-endian unsigned value of exactly `width` bytes.
func writeBE(out []byte, value uint64, width int) []byte {
	for shift := width - 1; shift >= 0; shift-- {
		out = append(out, byte((value>>(8*shift))&0xFF))
	}
	return out
}

// writeSizedMarker writes one sized marker: counts below `0x0F` fit the
// low nibble, while the nibble `0x0F` itself is the extended-size sentinel
// (RFC 0013 §5.4). Every count of 15 or more follows the marker with a
// `0x10`-style size marker and count object.
func writeSizedMarker(out []byte, marker byte, count int) []byte {
	if count < 0x0F {
		return append(out, marker|byte(count))
	}
	out = append(out, marker|0x0F)
	width := minimalUnsignedWidth(uint64(count))
	out = append(out, 0x10|byte(log2Width(width)))
	return writeBE(out, uint64(count), width)
}

func mulInt(left, right int) (int, bool) {
	product := left * right
	if right != 0 && product/right != left {
		return 0, true
	}
	return product, false
}

func mulUint(left, right uint64) (uint64, bool) {
	product := left * right
	if right != 0 && product/right != left {
		return 0, true
	}
	return product, false
}

func addUint(left, right uint64) (uint64, bool) {
	sum := left + right
	if sum < left {
		return 0, true
	}
	return sum, false
}

func float64FromBits(bits uint64) float64 {
	return math.Float64frombits(bits)
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func strconvHex(value byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0F]})
}

func strconvHexOfUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [16]byte
	index := len(digits)
	const hexDigits = "0123456789abcdef"
	for value > 0 {
		index--
		digits[index] = hexDigits[value&0x0F]
		value >>= 4
	}
	return string(digits[index:])
}

func strconvUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
