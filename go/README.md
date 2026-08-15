# Consema Go SDK (`go/`)
The Go implementation of the language-neutral Consema contracts
(RFC 0002/0003/0004/0006 contract family; authority repo `docs/rfcs/`;
[`docs/go-implementation-plan.md`](https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md)).
All milestones 0.14.0-0.19.0
(G0.1-G5.6) are delivered — with the G5.4 three-platform caveat: Windows is
the measured platform, Linux runs in CI, and the macOS leg is pending (no
CI job, no measured record; see "Three-platform verification" below)
(G028, adversarial audit 2026-08-14: the unqualified "all delivered"
claim conflicted with the same file's macOS-pending statement). G0.1
delivered the scaffold and the `core`
package; G0.2 delivered the `graph` package; G0.3 delivered the `protocol`
package; 0.15.0 G1.1 delivered the `document` package (source snapshots,
structural locations, formation status, limits, materialization requests,
and verifiable source patches); the remaining milestones delivered the
eight format families and the CLI (per-milestone delivery records below).
> **docs/ reference note (G118, adversarial audit 2026-08-13):** the
> six-repo split moved the spec repository's `docs/` out of this
> repository, so bare `docs/…` references elsewhere in this repository
> are written as full GitHub URLs (github.com/consema/consema). The only
> locally provisioned docs file is `docs/fc-manifest-0.13.0.json`
> (see CONTRIBUTING.md "Conformance 数据同步").
## Layout
- `go.mod` —the single module `consema.dev/consema` (RFC 0016 §3.1;
  plan §0.2). Minimum Go version declared as `go 1.26`, frozen at 0.14.0
  (RFC 0020 §9.2, support-policy.md §2; a 2026-08-12 experiment lowered
  the directive to 1.24, restored to 1.26 by the G001 adversarial-audit
  ruling 2026-08-13 — see the go.mod header comment).
- `core/` —the value model and PVCE/1 codec:
  - `value.go` —`Value` (closed fifteen-kind interface), `Kind`, `Null`,
    `Boolean`, `String`, `Bytes`, `Object`/`ObjectBuilder` (ordered,
    unique-key entries), `Array`, `Entry`,
    `EntryMapping`/`EntryMappingBuilder` (ordered arbitrary-key
    associations, duplicates allowed);
  - `numbers.go` —`Integer` (wraps `*big.Int`), `Decimal` (canonical
    coefficient × 10^exponent), `BinaryFloat32` (exact IEEE-754 binary32
    bits), `BinaryFloat64` (exact IEEE-754 binary64 bits);
  - `temporal.go` —`Date` (proleptic Gregorian, astronomical years),
    `Time` (wall clock, exact fractional second), `LocalDateTime`,
    `OffsetDateTime` (fixed UTC offset < 24 h);
  - `equal.go` —`Equal` (strict, order-sensitive) and `Hash` (deterministic
    FNV-1a over the canonical PVCE/1 bytes);
  - `pvce.go` —`EncodePVCE`/`EncodePVCEBounded`/`DecodePVCE`,
    `DecodeLimits`/`EncodeLimits`;
  - `errors.go` —`PVCEError` (typed errors with the frozen
    `core.pvce.*@1` registered codes, RFC 0016 §6), `DuplicateKeyError`.
- `protocol/` —the language-neutral protocol layer (G0.3; RFC 0016 §3.2):
  - `contract.go` —the contract registry (v1-v7, 16/18/25/25/30/38/41
    records) and the `core.protocol-message@1` envelope;
  - `error_registry.go` —the error-code registry (v1-v7,
    55/62/90/92/132/166/187 codes) and the `core.error-code-registry@1`
    manifest;
  - `canonical.go` —the canonical tagged JSON transport
    (`core.portable-value-json@1`, RFC 0015 §3.2) and the protocol PVCE
    wrappers;
  - `diagnostic.go` —`Diagnostic` construction validation (registry-bound
    code/category rules) and the `core.diagnostic@1` record;
  - `registry_descriptor.go` —`ProfileDescriptor`, `CapabilityDeclaration`,
    `RegistryManifest`, `CapabilitySet`;
  - `query.go`/`query_validate.go` —`QueryDefinition` validation and
    binding (the full domain/operator table of consema-core) and the
    `core.query-definition@1` record;
  - `exit_class.go` —`ExitClass` and `ClassifyErrorCode` (RFC 0015 §5);
  - `cli.go`/`cli_json.go` —the three CLI machine records
    (CliOutputMessage/BatchPlanMessage/BatchResultMessage) with the full
    presence/cross-constraint validation of RFC 0015 §4/§8/§9.
- `graph/` —the PortableGraph model and PGCE/1 codec (RFC 0006; G0.2):
  - `graph.go` —`Graph`, `Builder` (reserve/define/root lifecycle),
    `NodeID` (graph-local identity), `Node`, `NodeKind`, `MappingEntry`,
    `Limits`;
  - `equal.go` —`Equal` (strict, canonical-numbering, cycle-safe) and
    `Hash` (deterministic FNV-1a over the canonical PGCE/1 bytes);
  - `pgce.go` —`EncodePGCE`/`EncodePGCEBounded`/`DecodePGCE`,
    `PGCELimits`;
  - `errors.go` —`GraphError` (`core.graph.*@1` codes) and `PGCEError`
    (`core.pgce.*@1` codes), both with the RFC 0016 §6 `Code()` contract.
- `document/` —the source-snapshot and patch surface (0.15.0 G1.1; RFC
  0016 §3.2), mirroring the capability face of [consema-rs/consema-document](https://github.com/consema/consema-rs/tree/main/consema-document):
  - `source.go` —`SourceSnapshot` (immutable raw bytes, SHA-256
    `ContentDigest`, resolved `EncodingFacts`, decoded text, checkpointed
    `DecodedPosition`/`DecodedOffset` coordinate conversion), `SourceLimits`,
    `SourceError` (`core.source.*@1` codes);
  - `encoding.go` —`SourceEncoding` (closed set: binary/utf-8/utf-16le/
    utf-16be/latin-1/versioned Windows code pages), `BomPolicy`/`BomKind`,
    `EncodingRequest`, `EncodingFacts`;
  - `cp874_table.go`/`cp1250_table.go`…`cp1258_table.go`/`cp932_table.go`
    —the frozen Python-stdlib code-page data shared with
    `protocol/cp932_table.go`; 874 and 1250-1258 decode completely, CP932
    decodes exactly like go/protocol (CP936/CP949/CP950 are recognized but
    not yet decoded — a registered language fork, wave-4 R37 2026-08-15:
    the Rust reference decodes all three fully via encoding_rs while the
    Go side hard-fails with `core.source.invalid-sequence@1` on every
    byte; the fork is deliberate and pinned by test —
    `document/source_test.go` TestWindowsCodePages pins the hard failure
    on all three pages, so any future decode implementation must flip the
    test together with this note);
  - `location.go` —`SnapshotIdentity`, `NodeRef`, `Span` (snapshot-bound
    byte offsets), `NodeRole`, `DocumentAuthority`, `LocationError`;
  - `structural.go` —`BinaryRegion`/`BinaryStructuralIndex` (exact
    binary coverage);
  - `source_patch.go` —`SourcePatch` (atomic create/apply with
    original-byte preconditions and digest verification),
    `SourceReplacement`, `SourcePatchLimits`, `SourcePatchError`
    (document-layer codes; structural failures carry
    `core.protocol.invalid-value@1` exactly like the Rust document layer);
  - `formation.go` —`FormationStatus` (closed two-value
    Complete/Recovered, RFC 0016 §5.1 F10);
  - `ids.go`/`limits.go`/`materialization.go` —`FormatFamilyId`,
    `ProfileId`, `MaterializationStyleId`, `NewlinePolicy`, `MappingPolicy`,
    `RepresentabilityPolicy`, `ParseLimits`, `MaterializationLimits`,
    `MaterializationRequest`;
  - `placement.go` —`AssociationPlacement` + `PlacementKind` (the four
    frozen placements; 0.16.0 G2.4);
  - `change_set.go` —`ChangeSet`/`SourceEdit`/`NodeMapping` (the shared
    old-to-new edit records; 0.16.0 G2.4);
  - `edit_plan.go` —`EditPlan`/`EditPlanSourceId` (the shared validated
    dry-run plan; 0.16.0 G2.4);
  - `untouched.go` —`UntouchedByteProof`/`UntouchedByteRegion`/
    `UntouchedByteProofError` (the shared verifiable byte evidence;
    0.16.0 G2.4);
  - `structural.go` —`StructuralPieceKind`/`StructuralPiece`/
    `LosslessStructuralIndex` (the shared exhaustive token/trivia/
    error-region coverage; 0.16.0 G2.4). The shared edit records are the
    canonical implementations of the consema-document records
    (change_set.rs, edit_plan.rs, untouched_proof.rs, lib.rs); the seven
    family packages expose them under their established local names as
    aliases and thin wrappers — json/toml/yaml/ini/properties/xml/hcl,
    while plist uses `document.ChangeSet` directly (G029, adversarial
    audit 2026-08-14: the "five family packages" count predated the xml
    and hcl families, and plist never re-exports the records);
- `json/` —the JSON family surface (0.15.0 G1.2; RFC 0016 §5), mirroring
  the capability face of [consema-rs/consema-json](https://github.com/consema/consema-rs/tree/main/consema-json):
  - `profile.go` —`JsonProfile` (StrictV1/JsoncBoundedV1/Json5StandardV1)
    and `JsonSyntaxKind` (the closed lossless kind vocabulary with the
    stable query spellings);
  - `formation.go` —`Parse` (context-cancellable, typed
    `FormationFailure` with the frozen registered codes);
  - `parser.go` —the lossless JSON/JSONC/JSON5 lexer and parser:
    token/trivia/error-region coverage, recovery diagnostics, the JSON5
    lexical extensions (identifiers, single-quoted strings, hex and
    non-finite numbers, extended whitespace);
  - `document.go`/`semantic.go` —`Document` (render, formation status,
    diagnostics, lossless index, syntax kinds), `JsonValue`
    (kind/boolean/integer/decimal/binary64/string/array/object views with
    `SemanticAvailability`), `JsonObjectMember`/`JsonArrayElement`;
  - `query.go` —`ExecuteJSONQuery`/`ExecuteJSONSyntaxQuery` (+ cursors)
    over validated `protocol.ExecutableQuery` definitions, with result and
    step budgets and context cancellation;
  - `projection.go` —`ProjectionRequest` (+ builder with duplicate
    policies), `Project` with fidelity/report/provenance records and the
    frozen failure codes;
  - `materialization.go` —`Materialize` (canonical compact/pretty, JSON5
    non-finite literals, two-phase formation, provenance map) with the
    frozen failure codes;
  - `edit.go`/`placement.go`/`change_set.go`/`untouched.go` —    `EditTransaction` (+ builder, eight operations), atomic `Commit`/
    `DryRun` with `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and `FormatOperationRegistry`. The shared edit
    records come from go/document (0.16.0 G2.4); the operation registry
    is format-local.
- `toml/` —the TOML family surface (0.15.0 G1.3; RFC 0001, RFC 0016 §5),
  mirroring the capability face of [consema-rs/consema-toml](https://github.com/consema/consema-rs/tree/main/consema-toml):
  - `toml.go` —`TomlProfile` (Toml10V1), `TomlSyntaxKind` (the closed
    twelve-kind lossless vocabulary with the stable query spellings),
    `TomlItemKind` (the fifteen native item categories incl. table/
    inline-table/dotted-table/array-of-tables —never JSON object/member
    types), `Document` (render, formation status, diagnostics, lossless
    index, syntax kinds), `TomlItem`/`TomlEntry`/`TomlArrayElement`
    handles, `FormationFailure` with the frozen registered codes;
  - `parser.go` —`Parse` over the full TOML 1.0 grammar: byte-exact
    tokenizer with token/trivia coverage, tables, arrays-of-tables,
    dotted keys, inline tables, all scalar forms, the toml_edit-equivalent
    duplicate/implicit/dotted table semantics, and the four parse limits;
  - `query.go` —`ExecuteTomlQuery`/`ExecuteTomlSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation;
  - `projection.go` —`ProjectionRequest`, `Project` with
    fidelity/report/provenance records and the frozen failure codes
    (incl. `toml.projection.unrepresentable-datetime@1`);
  - `materialization.go` —`Materialize` (canonical-document style,
    canonical byte-closure verification, mapping policy, provenance map)
    with the frozen failure codes;
  - `edit.go`/`operation_registry.go` —`EditTransaction` (+ builder,
    scalar and structural operations), atomic `Commit`/`DryRun` with
    `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the seven-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4); the
    operation registry is format-local.
- `yaml/` —the YAML family surface (0.16.0 G2.1; RFC 0007, RFC 0016 §5),
  mirroring the capability face of [consema-rs/consema-yaml](https://github.com/consema/consema-rs/tree/main/consema-yaml):
  - `yaml.go` —`YamlProfile` (Yaml12CoreV1 / Yaml11CompatV1, explicit
    selection never dialect guessing), `YamlSyntaxKind` (the closed
    twenty-five-kind lossless vocabulary with the stable query spellings),
    `YamlNodeKind`/`YamlScalarKind`/`YamlScalarStyle`, `Document` (render,
    formation status, diagnostics, lossless index, syntax kinds),
    `YamlDocument`/`YamlNode`/`YamlScalar`/`YamlSequenceItem`/
    `YamlMappingEntry`/`YamlAlias` handles, `FormationFailure` with the
    frozen registered codes;
  - `parser.go`/`resolve.go` —`Parse` over the YAML 1.2.2 presentation
    grammar with the two profiles: byte-exact tokenizer/parser with
    token/trivia coverage, block/flow collections, compact notation,
    block scalars with chomping/folding, anchors/aliases with the
    most-recent-preceding rule, the Core and frozen 1.1 scalar schemas,
    tag directives and explicit-tag validation (never custom tag
    constructors), UTF-8/UTF-16 BOM sources, and the parse limits;
  - `syntax.go` —the lossless syntax tokenizer producing the exact piece
    layout (anchor/alias lexemes for the native composition);
  - `query.go` —`ExecuteYamlQuery`/`ExecuteYamlSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation;
  - `projection.go` —`ProjectGraph` (best-exact graph with provenance)
    and `ProjectValue` (sharing/tag/mapping policies, fidelity, report,
    provenance) with the frozen failure codes;
  - `materialization.go` —`MaterializeGraph`/`MaterializeValue`
    (canonical-block/canonical-flow styles, `&gN` anchors for sharing and
    cycles, canonical byte-closure verification, UTF-16 output, mapping
    policy) with the frozen failure codes;
  - `edit.go`/`operation_registry.go`/`untouched.go` —    `EditTransaction` (+ builder, the eight frozen operations with the
    anchor-safe rules), atomic `Commit` with `ChangeSet`, `SourcePatch`
    derivation, `UntouchedByteProof`, and the eight-operation
    `FormatOperationRegistry`. **G2.1 gap:** go/yaml publishes `Commit`
    but no `DryRun` (its Rust counterpart publishes dry_run; see the
    root-package `go/edits.go` note), so YAML transactions cannot produce
    a transferable plan through the root dispatch. The shared edit
    records come from go/document (0.16.0 G2.4); the operation registry
    is format-local.
- `ini/` —the INI family surface (0.16.0 G2.2; RFC 0009, RFC 0016 §5),
  mirroring the capability face of [consema-rs/consema-ini](https://github.com/consema/consema-rs/tree/main/consema-ini):
  - `profile.go` —`IniProfile` (PortableV1 / WindowsV1 /
    PythonConfigParserV1, explicit selection never dialect guessing),
    `IniEncodingSelection` (profile default, explicit UTF-8/UTF-16LE/
    code page, BOM policy), `IniParseLimits`, the closed fourteen-kind
    `IniSyntaxKind` lossless vocabulary, `IniValueState`/
    `IniQuoteStyle`/`IniLogicalLineKind`;
  - `formation.go`/`parser.go`/`python_case.go` —`Parse` under one
    exact profile: physical-line scanning with limits, per-profile
    comments/sections/entries/continuations, recovery with stable
    diagnostics, deterministic duplicate/case-equivalence groups, the
    pinned Unicode 16.0 `optionxform`, and exhaustive raw-source syntax
    pieces; `Document` with physical/logical lines, section/entry/
    error handles, and exact render identity;
  - `query.go` —`ExecuteIniQuery`/`ExecuteIniSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions (the ten native
    operators, decoded-text syntax matching, profile comparison modes,
    step/result budgets, context cancellation, ordered cursor);
  - `projection.go` —`Project` (best-exact nested EntryMapping and
    explicit RequireObject with comparison/collision policies,
    fidelity/report/provenance) with the frozen failure codes;
  - `materialization.go`/`code_page.go` —`Materialize` (portable/
    windows/python canonical styles, strict encoding with UTF-16 BOM
    and Windows code pages, canonical byte-closure verification) with
    the frozen failure codes;
  - `edit.go`/`change_set.go`/`operation_registry.go`/`untouched.go` —    `EditTransaction` (+ builder, the eight frozen operations with
    profile-specific quote/multiline/comment ownership), atomic
    `Commit`/`DryRun` with `ChangeSet`, `EditPlan`, `SourcePatch`
    derivation, `UntouchedByteProof`, and the eight-operation
    `FormatOperationRegistry`. The shared edit records come from
    go/document (0.16.0 G2.4); the operation registry is format-local.
- `properties/` —the Java Properties family surface (0.16.0 G2.3; RFC
  0010, RFC 0016 §5), mirroring the capability face of
  [consema-rs/consema-properties](https://github.com/consema/consema-rs/tree/main/consema-properties):
  - `properties.go` —`PropertiesProfile` (ReaderV1 / Latin1V1, explicit
    encoding selection never platform guessing), `JavaString` (exact
    UTF-16 code units, unpaired surrogates preserved as native content,
    `UTF16BE/1` bytes, well-formedness status), `PropertiesSyntaxKind`
    (the closed twelve-kind lossless vocabulary with the stable query
    spellings), `PropertiesValueState`/`PropertiesEscapeKind`/
    `PropertiesLogicalLineKind`, `PropertiesParseLimits`, and
    `FormationFailure` with the frozen registered codes;
  - `document.go`/`parser.go` —`ParseReader`/`ParseLatin1` over the
    natural/logical-line grammar: continuation with the JDK EOF
    backslash rule, key/separator/element splitting, exact left-to-right
    escape decoding, recovery with stable diagnostics, duplicate-key
    groups, exhaustive syntax coverage, and the `Document` with
    `PropertiesNaturalLine`/`PropertiesLogicalLine`/`PropertiesComment`/
    `PropertiesEscape`/`Property`/`PropertiesErrorLine` snapshot-bound
    handles;
  - `query.go` —`ExecutePropertiesQuery`/`ExecutePropertiesSyntaxQuery`
    (+ ordered cursors with cancellation) over validated
    `protocol.ExecutableQuery` definitions, with exact `UTF16BE/1` key
    matching, duplicate preservation, and step/result budgets;
  - `projection.go` —`Project` (best-exact EntryMapping preserving every
    association, explicit unique-Object under RequireUnique/FirstWins/
    LastWinsJdkTable) with atomic unpaired-surrogate failure, fidelity,
    report, and provenance with the frozen failure codes;
  - `materialization.go`/`cp_encode_table.go` —`Materialize`
    (canonical Reader/Latin-1 styles, uppercase `\uXXXX` escapes,
    UTF-16 BOM output, frozen Windows code-page encoding from the
    go/document decode authority, exact reparse/closure verification)
    with the frozen failure codes;
  - `edit.go`/`change_set.go`/`untouched.go`/`operation_registry.go` —    `EditTransaction` (+ builder, the five frozen operations:
    replace-semantic-value, replace-literal-value, insert-property,
    remove-property, rename-property), atomic `Commit`/`DryRun` with
    `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the five-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4); the
    operation registry is format-local.
- `xml/` —the XML family surface (0.17.0 G3.1; RFC 0012, RFC 0016 §5),
  mirroring the capability face of [consema-rs/consema-xml](https://github.com/consema/consema-rs/tree/main/consema-xml):
  - `profile.go` —`XmlProfile` (SafeV1), `XmlEncodingSelection`
    (profile default, explicit UTF-8/UTF-16LE/UTF-16BE), `XmlParseLimits`
    (common limits plus the element/attribute/namespace-declaration/
    mixed-content/DQname/comment/PI/CDATA/text/DTD/entity counts, the
    six-dimension entity expansion budgets, and recovery regions), and
    the closed `XmlSyntaxKind` lossless vocabulary (RFC 0012 §7);
  - `namespace.go` —`QName`/`ExpandedName`/`Binding`, the immutable
    ancestry-derived `NamespaceScope` (default namespace on elements
    only, permanent `xml` binding, reserved `xmlns`, expanded-name
    attribute uniqueness), and the four namespace failure kinds;
  - `entity.go` —the five predefined entities, XML 1.0 character
    validation, replacement-text validation (never markup), and the
    document-wide `EntityExpansionState` accounting with the six frozen
    breach categories;
  - `document.go` —the immutable namespace-aware native tree
    (`XmlElement`/`XmlContentItem` handles, ordered attributes and
    namespace bindings as separate associations, ordered mixed content,
    reference fragments, declaration/doctype facts, error regions) with
    exact render identity and `TextSemantic` line-end normalization;
  - `parser.go` —`Parse` over the XML 1.0 grammar: an independent
    tokenizer mirroring the reference token surface (top-level
    whitespace/after-elements states, `]]>` and `<?xml ` guards, DTD
    subset scanning), namespace resolution at start-tag finalization,
    bounded internal entities with deterministic recovery, exhaustive
    raw-byte piece coverage, and deterministic diagnostic ordering;
  - `query.go` —`ExecuteXMLQuery`/`ExecuteXMLSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation (native document order, bounded
    pre-order descendants, in-scope namespace chains);
  - `projection.go` —`Project` under `ElementTreeRequest` (the exact
    `xml.element-tree@1` record), `TextContentRequest`, and
    `SimpleEntryMappingRequest` (all policies mandatory) with
    fidelity/report/provenance records and the frozen failure codes;
  - `materialization.go` —`Materialize` (xml.safe-canonical-document
    style: deterministic spelling, generated prefixes, UTF-8/UTF-16
    output with BOM, canonical byte-closure verification, provenance
    map) with the frozen failure codes;
  - `edit.go`/`change_set.go` —`EditTransaction` (+ builder, the eight
    frozen operations: replace-text, insert/remove/rename/set-value
    attribute, insert/remove/rename element), atomic `Commit`/`DryRun`
    with `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the eight-operation
    `FormatOperationRegistry`. The shared edit records come from
    go/document (0.16.0 G2.4); the operation registry is format-local.
- `plist/` —the plist family surface (0.17.0 G3.2; RFC 0013), mirroring
  the capability face of [consema-rs/consema-plist](https://github.com/consema/consema-rs/tree/main/consema-plist):
  - `profile.go` —`PlistProfile` (XmlV1/BinaryV1), `PlistEncodingSelection`
    (profile default, explicit UTF-8/UTF-16LE/UTF-16BE), `PlistParseLimits`
    (common limits plus object/dict/array/duplicate-key-group/string/data/
    UID/extended-size/offset/ref/fact limits and conversion budgets), and
    the closed `PlistSyntaxKind` lossless vocabulary (RFC 0013 §8.2);
  - `native.go` —the shared representation-independent native value model
    (`PlistString` exact UTF-16 code units with surrogate status,
    `PlistKey`, `PlistInteger`, `PlistReal` with the Float32/Float64 width
    fact, `PlistBoolean`, `PlistDate` exact double seconds, `PlistData`,
    `PlistUID`, `PlistDict`/`PlistArray` ordered associations, the acyclic
    `PlistDocument` arena with shared identity and content-based structural
    equality, `PlistDocumentBuilder`);
  - `parser_xml.go` —`Parse` over the `plist.xml@1` grammar: an
    independent tokenizer mirroring the reference token surface (leading
    BOM/U+FEFF skip, declaration/doctype/element/attribute/text/CDATA
    states, after-elements guards), the strict Apple DOCTYPE and
    `<plist version="1.0">` contracts, the value grammars (integer
    decimal/hex, real specials, whole-second dates with calendar
    validation, strict base64), dictionary association rules, recovery
    with exhaustive piece coverage, and deterministic diagnostic ordering;
  - `parser_binary.go` —`plist.binary@1` formation: header/trailer
    mandatory integrity checks (RFC 0013 §5.11), offset-table validation
    with prefix-cut recovery, the full marker table with extended sizes,
    binary object/offset/ref/trailer facts and region coverage, and the
    hardened offset/ref width and bounds checks;
  - `query.go` —`ExecutePlistNativeQuery`/`ExecutePlistSyntaxQuery`/
    `ExecutePlistBinaryQuery` over validated `protocol.ExecutableQuery`
    definitions, with step/result budgets and context cancellation
    (native source order, lossless syntax pieces, document-level binary
    structure facts);
  - `projection.go` —`Project` under the exact `plist.value-tree@1`
    record and the explicit-policy `plist.projection.require-object@1`
    target (UID policy, collision policies, fidelity/report/provenance
    records, and the frozen failure codes);
  - `materialization.go` —`Materialize` (`plist.xml-canonical@1` /
    `plist.binary-canonical@1`: the Apple header spelling, deterministic
    indentation, minimal-width deduplicated object tables, whole-second
    dates with the authorized `TruncateWithReport` policy, canonical
    byte-closure verification) with the frozen failure codes;
  - `conversion.go`/`document.go` —`ConvertTo` across the two
    representations: serialization, reparse closure, native-model
    equality, the `representation-change` + per-node value-mapped report,
    and atomic `plist.conversion.inexpressible@1` failures;
  - `edit.go` —`EditTransaction` (+ builder, the six frozen operations:
    set-value, insert/remove-dict-entry, rename-dict-key,
    insert/remove-array-element), atomic `Commit`/`DryRun` with
    `ChangeSet`, `SourcePatch` derivation, `UntouchedByteProof`, XML
    byte-level spans, binary structural rewrites (reference blocks,
    offset table, trailer), and the six-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4).
- `hcl/` —the HCL family surface (0.18.0 G4.1; RFC 0014), mirroring the
  capability face of [consema-rs/consema-hcl](https://github.com/consema/consema-rs/tree/main/consema-hcl):
  - `profile.go` —`HclProfile` (NativeV1/TfvarsV1), `HclEncodingSelection`
    (profile default or explicit UTF-8; any other explicit encoding is a
    source-contract conflict with `hcl.parse.encoding@1`), `HclParseLimits`
    (common limits plus decoded text, body/expression/template depth,
    per-body item counts, identifier/string/number/template/heredoc
    lengths, constructor extents, recovery/error/piece/report counts), and
    the closed thirty-kind `HclSyntaxKind` lossless vocabulary (RFC 0014
    §7.2, no `Bom` kind);
  - `formation.go`/`document.go` —`Parse` over the UTF-8-only source
    contract (BOM Recovered with `hcl.parse.byte-order-mark@1`, invalid
    UTF-8 fatal, lone CR Recovered), the self-owned tokenizer, the
    body/expression grammar with deterministic recovery (bracket-aware
    expression regions, unterminated string/heredoc bounds, the
    duplicate-attribute exclusion), and the tfvars profile gate
    (`hcl.tfvars.block-not-allowed@1` per top-level block, which stays a
    native item of the Recovered document);
  - `lexer.go`/`parser.go` —the independent tokenizer (47 token kinds,
    template frame stack for quoted/heredoc/interpolation nesting, UAX
    #31 identifiers, the frozen number grammar, `$${`/`%%{` escapes) and
    the recursive-descent expression parser (full operator precedence,
    traversals/splats, for-expressions, constructors, quoted templates
    and heredocs with region re-lexing for interpolation and directive
    interiors);
  - `expression.go`/`native.go` —the unevaluated expression AST
    (fifteen closed kinds, canonical-decimal numbers by pure decimal
    string arithmetic, structural equality, the literal-complete
    boundary, typed literal extraction) and the body tree
    (`HclDocument`/`HclBody`/`HclAttribute`/`HclBlock`/`HclBlockLabel`/
    `HclErrorRegion` with exact-span double preservation);
  - `query.go` —`ExecuteHCLNativeQuery`/`ExecuteHCLSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions (native body tree
    operators including the typed literal accessor family with
    `hcl.query.type-mismatch@1`/`hcl.query.non-literal@1`, the error-
    region facts, and the lossless syntax kinds), with step/result
    budgets and context cancellation;
  - `projection.go` —`Project` under the exact `hcl.projection.body@1`
    record (typed members, ordered interleaved items, duplicate object
    keys preserved as ordered entries) with the explicit
    `ProjectExpression` policy producing the authorized `hcl.expression@1`
    record (kind family, exact text, FNV-1a structural fingerprint, and
    the versioned payload codec), atomic `hcl.projection.*@1` failures,
    and the transformed-event report;
  - `materialization.go` —`Materialize` (`hcl.canonical-document@1`:
    UTF-8 without BOM, LF line endings, two-space indentation, minimal
    deterministic escapes, canonical decimal spellings, always-quoted
    labels) with the standalone expression-text validation, the reparse
    closure walk by canonical-decimal/structural equality, and the frozen
    failure codes;
  - `edit.go` —`EditTransaction` (+ builder, the six frozen operations:
    set-attribute-value, insert/remove/rename-attribute,
    insert/remove-block; tfvars publishes the four attribute operations
    only), atomic `Commit`/`DryRun` with the sequential per-operation
    pipeline (splice fold, reparse verification, `ChangeSet`,
    `SourcePatch` derivation, `UntouchedByteProof`, node mappings), and
    the `FormatOperationRegistry`. The shared edit records come from
    go/document.
- `consema` (the package root, `*.go` directly in `go/`) —the facade
  surface (0.15.0 G1.4; RFC 0016 §3.2), mirroring [consema-rs/consema](https://github.com/consema/consema-rs/tree/main/consema):
  - `document.go` —the `Document` union over the eight format families
    (JSON, TOML, YAML, INI, Properties, XML, plist, HCL; additive as
    the families landed through 0.15.0-0.18.0) with the typed adapters
    (`AsJSON`/`AsTOML`/`AsYAML`/`AsINI`/`AsProperties`/`AsXML`/
    `AsPlist`/`AsHCL`) and the common immutable snapshot facts;
  - `registry.go` —`Families`/`Profiles`/`QueryDomains`/
    `OperationRegistryFor` (8 families / 16 profiles / 21 query
    domains / 16 operation registries, RFC 0015 §6.2), derived from
    the implementing packages (all eight families are implemented —
    G056, adversarial audit 2026-08-13, the "not-yet-implemented
    families" note was stale; the registries never re-declare the
    operation surface and drift-guard tests assert backend equality),
    plus the `ParseDocument` single parse entry by profile id;
  - `conversion.go` —the eight `Convert*` entries
    (`ConvertJSON`/`ConvertTOML`/`ConvertYAML`/`ConvertINI`/
    `ConvertProperties`/`ConvertXML`/`ConvertPlist`/`ConvertHCL`), the
    explicit two-stage projection →PortableValue →materialization
    composition with the retained two-stage report and both provenance
    directions (`core.conversion.*@1` failure codes; cross-family
    convert lives in the root package only).
The RFC 0016 §4.1 mapping (the language-neutral fifteen-kind contract,
配置内容统一处理标准与 Rust 参考实现.md §10): Object → `*core.Object`
(ordered `[]Entry`, never a Go map), Array →`*core.Array`, String →`core.String`, Integer →`core.Integer`, Decimal →`core.Decimal`,
BinaryFloat32 →`core.BinaryFloat32` (exact IEEE-754 binary32 bits),
BinaryFloat64 →`core.BinaryFloat64`, Bytes →`core.Bytes` (octet sequence),
Date/Time/LocalDateTime/OffsetDateTime →the four `core` temporal types,
Boolean →`core.Boolean`, Null →`core.Null`, EntryMapping →`*core.EntryMapping` (ordered arbitrary-key associations, duplicates
allowed).
## SDK usage essentials (roadmap §21.2 / RFC 0016 §6)
The Go public API is held to the six stability policies of roadmap §21.2
「Go API」（未导出字段/只读方法、context 只做取消、error code 与
message 分离、有序结果不用 map、iterator 显式 Close、最低版本 CI
验证）; this is how each one shows up when using the SDK:
1. **Completed objects are logically immutable.** Parse results
   (`Document`, every family document, `document.SourceSnapshot`),
   value objects (`core.Object`/`Array`/`EntryMapping`), `graph.Graph`,
   and cursor types keep unexported fields and read-only accessors;
   accessors return copies of internal slices (e.g.
   `(*core.Object).Entries`, `(*graph.Graph).Roots`) and the value
   constructors copy caller input. The only mutation paths are the
   explicit builders (`NewObjectBuilder`, `NewArray`,
   `NewEditTransactionBuilder`, `graph.Builder`, ….
2. **`context.Context` carries cancellation/deadline only.** No business
   parameter is smuggled through context values (there is no
   `context.WithValue` in the module). Cancellable entries check the
   context before work and between steps (JSON/XML/HCL parse, all query
   executors and cursors); the TOML/YAML/INI/Properties/plist parse
   entries take no context, mirroring the Rust facade which has no
   cancellation. An already-cancelled context fails fast with the typed
   `FormationFailureCancelled` / `FailureCancelled` outcome.
3. **Error code is separate from the message.** Every typed error
   returned by an SDK operation implements `Code() string` with the
   frozen registered code (RFC 0016 §6; e.g. `core.parse.resource-limit@1`,
   `hcl.parse.encoding@1`); `Error()` text is human presentation only
   and never participates in conformance comparison. The protocol layer
   classifies once: `protocol.ClassifyErrorCode` (RFC 0015 §5). Pattern:
   `if failure != nil { switch failure.Code() { …} }`.
4. **Ordered results are never maps.** Query matches, projection
   results, materialization results, edit results, and change sets are
   ordered slices or structured records; objects are `*core.Object`
   with ordered entries (never `map[string]Value`). The only maps in
   public shapes are unordered-by-nature argument/metadata carriers
   (`Diagnostic.Arguments`, `SourcePatch.Metadata`), and their wire
   forms sort names deterministically.
5. **Iterators have explicit completion/error semantics.** The root
   cursors (`OrderedCursor`, `PortableCursor`) close with exactly one
   terminal state —`Completed`, `Cancelled`, or `Failed` —surfaced by
   `TerminalState()`; family cursors (`JsonQueryCursor`, … signal
   exhaustion with a nil match and errors (including cancellation) with
   a `QueryFailure`. A failure never yields a partial result that could
   be mistaken for a complete one.
6. **Minimum Go version.** `go.mod` declares `go 1.26` — frozen at 0.14.0
   per RFC 0020 §9.2 / support-policy.md §2 (restored from a 2026-08-12
   1.24 experiment by the G001 adversarial-audit ruling; see the go.mod
   header comment). The hcl-go-v1 oracle's older `go` directive is its own
   manifest-pinned legacy, not SDK policy. The roadmap §21.2 CI
   verification leg — the gates below running in CI on the declared
   minimum version — is the `go-matrix` job (ci-go.yml: a 2-version matrix
   '1.26.0' declared minimum + '1.26.5' matrix-pinned — R19, wave-4
   2026-08-15: 'current stable' was a frozen-claim overreach, go.dev's
   current stable is 1.26.6 and the RFC 0020 §9.2 "current stable" leg was
   never satisfied; the matrix upgrade is post-1.0.0 and the docs state
   the frozen matrix fact — per RFC 0020 §9.2,
   fail-fast: false, each leg pinning its setup-go version and running
   genuinely under GOTOOLCHAIN=auto — G031, adversarial audit 2026-08-14:
   the minimum leg is the exact 1.26.0 patch, replacing '1.26.x' which
   resolved to the latest 1.26 patch and never exercised the declared
   minimum; G5.5 finding F1, **closed** in consema 仓 commit
   [937b33028e970794c4dcb2bd9819a48bd06cdb1f](https://github.com/consema/consema/commit/937b33028e970794c4dcb2bd9819a48bd06cdb1f)
   （Deliver Go 0.19.0 G5.4-G5.5: fuzz matrix, security matrix, usability
   review）; locally they are the commands in the next section.
## Go CLI（0.19.0 G5.6；productVersion 随 release train，见仓根 README `Version:` 行——G073，对抗审计 2026-08-14：节标题不再内联版本串）

`cmd/consema` is the Go implementation of the official `consema` CLI (RFC
0015; mirror of the Rust `[consema-rs/consema](https://github.com/consema/consema-rs/tree/main/consema)` bin). It is stdlib-only (self-
written deterministic argument parsing, no clap/flag-based guessing), sits
inside the module, and reaches format semantics through the root package
and the family packages' public APIs — flow.go imports the eight family
packages directly, a documented deviation from RFC 0015 §2.3 hard gate 1
(the Rust bin reaches semantics only through the facade; G055, adversarial
audit 2026-08-13).

- **Command surface** — the 11 frozen commands of RFC 0015 §6.1:
  `inspect`, `capabilities`, `query`, `project`, `materialize`,
  `convert`, `edit` (dry-run only; `--write` refused), `plan` (read-only
  batch manifest), `apply` (batch write from a prior plan manifest),
  `conformance` (embedded self-check subset: envelope round-trip, exit
  classification, redaction), `explain`. **Per-command format wiring
  (G066, adversarial audit 2026-08-13 — the command surface is fully
  delivered, but the per-family wiring is partial and disclosed here):
  `query` executes the portable-value domain only (native domains and
  XML/plist/HCL sources are refused with `cli.data.invalid-request@1`);
  `project` is wired for the json/toml families only; `edit` maps the
  ini operation vocabulary only (anchor placement is not wired);
  `materialize` and `convert` run across the eight families through the
  facade composition; the remaining commands are family-neutral.**
- **Machine protocol** — every command emits the `core.cli-output@1`
  envelope under `--json` (exactly one canonical JSON line + LF);
  diagnostics go to stderr; exit codes 0-5 classify exclusively through
  `protocol.ClassifyErrorCode` (RFC 0015 §5; the CLI never classifies
  itself). `--json --pretty` is a self-written whitespace-only indenter.
- **Request input** — `query`/`project`/`materialize`/`convert`/`edit`/
  `plan` consume strict canonical JSON or PVCE/1 via `--request-file` or
  stdin (`cli.request@1`/`cli.convert-request@1`/`cli.edit-request@1`
  wrappers; RFC 0015 §3.2).
- **plan/apply** — reuse the root package's `BatchPlanner`/
  `ApplyPlanFile` (RFC 0015 §8/§9): the six-step pre-write revalidation
  (stale digest, original-bytes precondition), the atomic fsio write
  engine (same-directory temp, atomic replace, read-back target-digest
  verification, symlink/read-only/directory policy), the interruption
  recovery three-way rule, and the documented `CONSEMA_APPLY_INTERRUPT_AFTER` /
  `CONSEMA_APPLY_WRITE_FAILURE` injection seams.
- **Redaction** — presentation-only (RFC 0015 §11): the frozen key-name
  pattern set plus `--redact-keys` globs; `--show-secrets` is the sole
  opt-out; on-disk manifests are never redacted.
- **Version** — `productVersion` defaults to the workspace version of the
  release train, the value asserted by the check-version-consistency gate
  against the repo-root README `Version:` line (go/cmd/consema/version.go;
  RFC 0015 §3.3: SemVer core
  syntax with an optional prerelease suffix, no git hashes or build
  metadata; the Go module version follows the product release train,
  RFC 0016 §9), with a build-time override via
  `-ldflags "-X main.productVersion=..."`.
- **Tests** — unit tests mirror the Rust bin's module tests; the
  process-level e2e suite (`e2e_test.go`) builds the binary in TestMain
  and drives it via `os/exec` (exit-code matrix, stdout/stderr
  separation, envelope shape, full plan→apply flow, stale/tampered/
  read-only/directory/interruption negatives).

## Build and test
```
cd go
go build ./...
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go mod tidy
gofmt -l .
```
The enforced quality gates (all run by the ci-go.yml go-matrix job) are
`go build`, `go vet`, `go test -count=1`, `go test -race`, and
`gofmt -l` — all expected to be clean. `go mod tidy` is a **local hygiene
check only**: no workflow or script executes it, so a go.mod/go.sum drift
stays green in CI (G028, adversarial audit 2026-08-14: the old "all quality
gates" wording implied CI enforcement of `go mod tidy`, which nothing
executes).

**Provision prerequisite (clean clone, mandatory; G065, adversarial audit
2026-08-13 — this section previously omitted it):** conformance data is
not in git (`.gitignore`); `go test ./...` runs the conformance runner
against repository-relative paths (`conformance/vectors`, …) and fails
loudly (never skips) when the data is missing. Before the first run, copy
`conformance/` to the repository root and
`docs/fc-manifest-0.13.0.json` to `docs/` from the consema spec
repository checkout pinned to the same ref as the ci-go.yml provision
step (R5, wave-4 2026-08-15 — see the root README "构建与测试"
prerequisite and CONTRIBUTING.md "Conformance 数据同步"). Three paths
(R35, wave-4 2026-08-15): the conformance suite fails loudly (never
skips), the differential harness tests skip on missing case sets instead
(G058), and the five family packages (`go/json`, `go/toml`, `go/yaml`,
`go/properties`, `go/pilot`) hard-read `../../conformance/...` with no
skip guard — on an unprovisioned clean clone they fail loudly too, so
only the differential harnesses ever skip.
## Stdlib-only policy
`go.mod` declares zero third-party dependencies (no `require` lines); the
module uses only the Go standard library (`math/big`, `hash/fnv`,
`encoding/binary`, `crypto/sha256`, `unicode/utf8`). Policy: plan §1.3;
RFC 0016 §10 rejected alternatives.
## Golden-bytes provenance
The PVCE/1 golden vectors in `core/pvce_test.go` (`TestPVCEGoldenBytes`) are
transcribed byte-for-byte from the Rust reference codec's in-code pins (all
cross-repository references are full GitHub URLs; the function names are
the anchors — line numbers may drift, wave-4 R40 2026-08-15):
- [consema-rs/consema-pvce/src/lib.rs](https://github.com/consema/consema-rs/blob/main/consema-pvce/src/lib.rs)
  —`object_byte_vector_is_frozen`
  (`{"a": Integer(1)}` →hex `5056434501410a01200201611003010101`);
- [consema-rs/consema-pvce/src/lib.rs](https://github.com/consema/consema-rs/blob/main/consema-pvce/src/lib.rs)
  —`byte_vector_is_frozen`
  (Null →`50564345010000`; Integer(-256) →`5056434501100402020100`).
The additional-kind golden vectors in `core/fifteen_test.go`
(`TestPVCEFifteenKindGoldenBytes`) are pinned to the Rust encoder's bytes
for the exact values of the Rust `every_core_kind_round_trips` test
([consema-rs/consema-pvce/src/lib.rs](https://github.com/consema/consema-rs/blob/main/consema-pvce/src/lib.rs)): BinaryFloat32(0x7fc00001) →`505643450112047fc00001`, Bytes([0, 255]) →`505643450121030200ff`,
Date(-12345-02-28) →`505643450130070402023039021c`,
Time(23:59:58.125) →`5056434501310c173b3a080301017d03020103`,
LocalDateTime(-12345-02-28T23:59:58.125) →`50564345013215070402023039021c0c173b3a080301017d03020103`,
OffsetDateTime(−23:00) →`5056434501331b070402023039021c0c173b3a080301017d03020103050203014370`,
EntryMapping{true →null} →`505643450142050102000000`, plus the
seven-kind sequence vector. The canonical JSON transport golden vectors in
`protocol/fifteen_transport_test.go` (`TestFifteenKindJSONGoldenVectors`)
are pinned to the Rust `value_transport.rs` encoder output for the same
values.
The PGCE/1 golden vectors in `graph/pgce_test.go` (`TestPGCEGoldenBytes`) are
transcribed byte-for-byte from the Rust reference codec's in-code pins:
- [consema-rs/consema-graph/src/pgce.rs](https://github.com/consema/consema-rs/blob/main/consema-graph/src/pgce.rs)
  —`scalar_byte_vector_is_frozen`
  (scalar "x" tagged `tag:yaml.org,2002:str` →hex
  `504743450101010020157461673a79616d6c2e6f72672c323030323a7374720178`);
- [consema-rs/consema-graph/src/pgce.rs](https://github.com/consema/consema-rs/blob/main/consema-graph/src/pgce.rs)
  —`empty_graph_byte_vector_is_frozen`
  (empty graph →`50474345010000`).
Both are also the shared conformance vector expectations
(`conformance/vectors/portable-graph-v1.json`: `pgce.empty-vector` and
`pgce.scalar-vector`).
The Rust side is the authority for the bytes (roadmap §16.1 hard gate: "Rust
与 Go 的 PVCE/PGCE bytes 完全一致"); any change must land in both languages
together.
## Fuzz targets
Go native fuzzing (`go test -fuzz`) covering the 0.14.0 capability surface
(milestone G0.5; roadmap §16.1 "Go fuzz targets"). Discipline mirrors the
Rust fuzz targets of 0.13.0 (https://github.com/consema/consema/blob/main/docs/fuzz-evidence-0.13.0.md §2): resource
limits are fixed at the production defaults
(`core.DefaultDecodeLimits` / `graph.DefaultPGCELimits` /
`protocol.DefaultProtocolLimits`), limit failures are passes, and property
assertions detect encode/decode asymmetry. The round-trip targets use
deterministic per-package fifteen-kind value-space / graph generators
(`core/fuzzgen_test.go`, `graph/fuzzgen_test.go`, and the transport
generator inside `protocol/json_fuzz_test.go` —each lives in its own test
package because a shared generator would create an import cycle, the same
reason the Rust fuzz drivers live per crate).
| Target | Package | Asserted property |
|---|---|---|
| `FuzzPVCE` | `core/` | arbitrary bytes →`DecodePVCE`: never panic; limit semantics never bypassed; decode→encode fixed point (successful decode re-encodes to exactly the input bytes) |
| `FuzzPVCEEncodeDecode` | `core/` | `decode(encode(x)) == x` for generated fifteen-kind values, `Equal` holds, re-encode byte-stable |
| `FuzzPGCE` | `graph/` | arbitrary bytes →`DecodePGCE`: never panic; limit semantics never bypassed; decode→encode fixed point |
| `FuzzPGCEEncodeDecode` | `graph/` | `decode(encode(g))` is `Equal` to g (sharing and cycles included), re-encode byte-stable |
| `FuzzCanonicalJSON` | `protocol/` | arbitrary bytes →`DecodeJSON`: never panic; limit semantics never bypassed; decode→encode fixed point |
| `FuzzJSONEncodeDecode` | `protocol/` | canonical transport `decode(encode(x)) == x`, `Equal` holds, re-encode byte-stable |
### Full-family fuzz targets (0.19.0 G5.4)
Milestone 0.19.0 G5.4 (https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.6; roadmap §22.4
「安全与质量」「release-candidate fuzz clean-run 达标」) extends the fuzz
surface to
every family: each `go/<family>/<family>_fuzz_test.go` feeds arbitrary
bytes to the family's Parse entry under the production default limits and
asserts the parse-closure invariants —a successful parse renders the
source byte-exactly (the parser must never fabricate, drop, or normalize
bytes), the closed formation-status contract holds, a Recovered document
publishes at least one diagnostic within the MaxDiagnostics budget, and
nothing panics or hangs (the fuzz engine bounds wall time). The
family-specific high-value faces mirror the Rust threat corpora
(SECURITY.md): YAML alias bombs (the generated alias count must
equal the emitted references —no invented aliases), binary plist
trailer/offset/object-table accounting (a limit breach must never report a
Complete document), XML entity expansion (billion laughs, cycles, deep
chains), and HCL heredoc/template/expression nesting.
| Target | Package | Asserted property |
|---|---|---|
| `FuzzParse` | `json/` | arbitrary bytes under strict/JSONC/JSON5 profiles: never panic; render closure (byte-exact); status closed; Recovered →diagnostics; diagnostics ≤MaxDiagnostics |
| `FuzzParse` | `toml/` | arbitrary bytes →TOML 1.0: never panic; render closure; always Complete with no diagnostics |
| `FuzzParse` | `yaml/` | arbitrary bytes →YAML 1.2: never panic; render closure; alias count bounded by source bytes |
| `FuzzAlias` | `yaml/` | generated anchor/alias documents: alias count equals the emitted references exactly; render closure |
| `FuzzParse` | `ini/` | arbitrary bytes under portable/Windows/Python profiles: never panic; render closure; Recovered →diagnostics |
| `FuzzParse` | `properties/` | arbitrary bytes →Java Properties reader: never panic; render closure; Recovered →diagnostics |
| `FuzzParse` | `xml/` | arbitrary bytes →`xml.1.0-safe@1` (entity corpora seeded): never panic; render closure; entity accounting never bypassed |
| `FuzzParseXML` | `plist/` | arbitrary bytes →`plist.xml@1`: never panic; render closure; Recovered →diagnostics |
| `FuzzParseBinary` | `plist/` | arbitrary bytes →`plist.binary@1` (Rust hardening seeds): never panic; render closure; no fake Complete |
| `FuzzParse` | `hcl/` | arbitrary bytes under native/tfvars profiles (heredoc/template seeds): never panic; render closure; Recovered →diagnostics |
Run one target (the anchored regex is required: `-fuzz=FuzzParse` alone
would match every `FuzzParse`-prefixed target and refuse to run):
```
cd go
go test -fuzz='^FuzzPVCE$' -fuzztime=30s ./core/
go test -fuzz='^FuzzPVCEEncodeDecode$' -fuzztime=30s ./core/
go test -fuzz='^FuzzPGCE$' -fuzztime=30s ./graph/
go test -fuzz='^FuzzPGCEEncodeDecode$' -fuzztime=30s ./graph/
go test -fuzz='^FuzzCanonicalJSON$' -fuzztime=30s ./protocol/
go test -fuzz='^FuzzJSONEncodeDecode$' -fuzztime=30s ./protocol/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./json/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./toml/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./yaml/
go test -fuzz='^FuzzAlias$' -fuzztime=30s ./yaml/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./ini/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./properties/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./xml/
go test -fuzz='^FuzzParseXML$' -fuzztime=30s ./plist/
go test -fuzz='^FuzzParseBinary$' -fuzztime=30s ./plist/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./hcl/
```
**Release-candidate fuzz clean-run (0.19.0 G5.4; roadmap §22.4「release-candidate fuzz clean-run」——G050，对抗审计 2026-08-14：改引节锚，行号删除).** The fuzz evidence is a release-candidate prerequisite (rc-candidate checklist item — rc-1.0.0-candidate.md §22.4/§4.1 口径), not a standing release.yml step: no workflow in this repository runs fuzz (2026-08-15 实测核对：.github/workflows 六份 workflow 均无 fuzz 调用). Wave-4 R27（母仓 5e0abad/e29e621/70e8884，2026-08-15）只记载 fuzz 驱动暂停与 C-2 遗留（驱动 2026-08-13 11:19 起未重启，账本冻结于 122,478 行 / 780.529 CPU-hours，toml/protocol-decode/plist/xml 四单位低于 72h 门槛如实遗留），不含任何 supersede 裁决；母仓路线图 §21.2「go vet、static analysis、race detector 和 fuzz 进入发布门禁」与 RFC 0020 §9.2（fuzz per §21.2）原文仍有效——本段 rc-candidate 检查单口径与母仓措辞的关系待母仓后续修订裁决，引用以母仓为准。
Measured 2026-08-10 (go 1.26.5, Windows 11): every target ran 30s of
fuzzing with no panic, no hang, and no limit bypass:
| Target | execs in 30s | result |
|---|---|---|
| `FuzzPVCE` | 5,492,685 | PASS |
| `FuzzPVCEEncodeDecode` | 7,199,295 | PASS |
| `FuzzPGCE` | 8,654,267 | PASS |
| `FuzzPGCEEncodeDecode` | 3,971,779 | PASS |
| `FuzzCanonicalJSON` | 9,585,481 | PASS |
| `FuzzJSONEncodeDecode` | 4,557,154 | PASS |
| `FuzzParse` (json) | 403,177 | PASS |
| `FuzzParse` (toml) | 1,166,022 | PASS |
| `FuzzParse` (yaml) | 276,046 | PASS |
| `FuzzAlias` (yaml) | 2,530,675 | PASS |
| `FuzzParse` (ini) | 753,665 | PASS |
| `FuzzParse` (properties) | 266,226 | PASS |
| `FuzzParse` (xml) | 1,291,752 | PASS |
| `FuzzParseXML` (plist) | 369,315 | PASS |
| `FuzzParseBinary` (plist) | 844,545 | PASS |
| `FuzzParse` (hcl) | 1,003,079 | PASS |
Four defects were found and fixed during the clean-run. Regression pins
(G059, adversarial audit 2026-08-13 — the "each failing input is pinned
under testdata/fuzz/" intro overstated the actual pins): defect 2 is a
seed in the `json` fuzz target's `f.Add` corpus (json_fuzz_test.go);
defect 3 has its seed under `yaml/testdata/fuzz/FuzzParse/`; defect 4 has
its seeds under `plist/testdata/fuzz/FuzzParseXML/`; defect 1 (the
`plist.binary@1` trailer-limit breach) has no separate corpus seed — its
trigger is the trailer parameter-breach shape that the plist fuzz targets
exercise through their parse-closure assertions:
1. `plist.binary@1` trailer limit breach (offsetIntSize/objectRefSize/
   numObjects beyond `MaxOffsetIntSize`/`MaxObjectRefSize`/`MaxObjectCount`)
   reported a **Complete document with no native model and no diagnostics**
   instead of a fatal limit failure — the `fatalLimit` result was discarded
   inside `validateTrailer`, and the same discard pattern affected
   `recordFact` and `scanObjects` (extended-size, string/data/array/dict,
   binary-facts limits). The failures now propagate like the Rust parser
   (`parser_binary.rs` `?`); the frozen Foundation fact is preserved — a
   non-`bplist00` header demotes trailer limit checks to recoveries because
   the trailer of a non-binary source is plain source text
   (conformance/oracle_plist_macos.go formatLeg; test
   `TestPlistMacOSFoundationDifferential`).
2. `json.strict.trailing-comma@1` was emitted with a Syntax category on the
   array path while the error registry (and the object path) register
   Conformance — the registry-validated diagnostic constructor panicked on
   `[1,2,3,]` under the strict profile. The array path now emits the
   registry-aligned Conformance category.
3. `yaml` parse hung forever on `e0: e0\n s:[a,t` (and any plain-scalar
   continuation whose scan ends at a stop `:` with a misleading
   continuation lookahead): `parsePlainBlock`'s loop appended an empty
   fragment without advancing. The loop now breaks when an iteration makes
   no progress — the scalar is complete and the caller resumes at the stop
   character (regression seed `yaml/testdata/fuzz/FuzzParse/`).
4. `plist.xml@1` — two termination defects in the error-recovery loop:
   `rest[1]` panicked on the 1-byte input `0<` (index out of range), and a
   tokenizer error at a resume `<` (e.g. `<!` markup the fragment state
   cannot start) restarted the fragment tokenizer at the same position,
   looping ~2M times until the syntax-pieces limit. The `rest[1]` access is
   length-guarded and the resume position now strictly advances, with the
   skipped byte covered by gap assembly (RFC 0013 §8.2). Regression seeds
   under `plist/testdata/fuzz/FuzzParseXML/`.
## Security matrix (0.19.0 G5.4)
`go/conformance/security_matrix_test.go` extends the 0.16.0 limits matrix
(`limits_matrix_test.go`) to the recovery-capable families and mirrors the
Rust hardening surface ([consema-rs/consema-conformance](https://github.com/consema/consema-rs/tree/main/consema-conformance)/tests/
{xml,plist,hcl,yaml}_hardening.rs; roadmap §22.4「安全与质量」
「XML/YAML/HCL/binary plist 专项 threat tests 通过」):
- **Limits matrices** —13 XML rows, 10 plist XML rows, 9 plist binary
  rows, 13 HCL rows (XML/plist 32 boundary rows + HCL 13 rows, 45 total;
  G061, adversarial audit 2026-08-13 — the old "(32 boundary rows total)"
  arithmetic only summed the XML+plist rows; SECURITY.md already used
  the correct split), each pinning the exact
  positive/negative boundary: N-1 fails with the family's frozen code
  (`xml.limit.*@1`, `plist.limit.*@1`, `hcl.limit.*@1`,
  `core.source.resource-limit@1`) and N succeeds. Rows whose parameter is
  provably unconsumed by the family's parse are documented in comments
  (xml MaxTokenCount/MaxRecoveryRegions, plist XML
  MaxTokenCount/MaxNodeCount, HCL MaxNodeCount/MaxNestingDepth) and not
  pinned, per the existing matrix discipline.
- **XML entity accounting** —4 recovery boundaries
  (`MaxEntityReferences`, `MaxEntityDeclarations`, `MaxEntityExpansionDepth`,
  `MaxExpandedEntityBytes`: N-1 recovers with `xml.entity.limit@1`, N
  completes) plus the billion-laughs (amplification@1), cyclic (cyclic@1),
  and 200-deep chain (limit@1 + cyclic@1) threat corpora.
- **Binary plist bounds** —the mutation/truncation closure over the Rust
  hardening seeds (no panic, byte-exact render, and the fake-completion
  guard: a Complete document must carry a native model).
- **HCL heredoc/expression depth** —2,000-level parens/chain/blocks/
  templates under the default budgets fail with
  `hcl.limit.expression-depth@1`/`body-depth@1`/`template-depth@1`, never
  a panic, under explicit tight budgets too.
- **YAML alias bomb** —the 16-reference corpus keeps the graph small
  (< 32 nodes), the default tree projection rejects shared identity
  (`yaml.projection.sharing@1`), and a bounded duplication fails with
  `yaml.projection.resource-limit@1`.
## Benchmark baseline (0.19.0 G5.4)
Per-family parse/render baselines in the spirit of the Rust BENCHMARKS
(https://github.com/consema/consema/blob/main/docs/BENCHMARKS-0.13.0.md) —simple, `go test -bench` runnable, recorded
here only (no frozen budget; that is a Rust-side discipline):
```
cd go
go test -bench=. -benchtime=1s ./json/ ./toml/ ./yaml/ ./ini/ ./properties/ ./xml/ ./plist/ ./hcl/
```
Measured 2026-08-10 (go 1.26.5, Windows 11):
| family | BenchmarkParse | BenchmarkRender |
|---|---|---|
| json/ | 108 µs/op (11.7 MB/s) | 1.45 µs/op (868 MB/s) |
| toml/ | 127 µs/op (7.7 MB/s) | 0.77 µs/op (1264 MB/s) |
| yaml/ | 464 µs/op (2.0 MB/s) | 0.91 µs/op (1031 MB/s) |
| ini/ | 291 µs/op (2.3 MB/s) | 0.64 µs/op (1023 MB/s) |
| properties/ | 527 µs/op (1.7 MB/s) | 0.78 µs/op (1159 MB/s) |
| xml/ | 279 µs/op (4.7 MB/s) | 1.04 µs/op (1257 MB/s) |
| plist/ | 710 µs/op (2.0 MB/s) | 2.52 µs/op (570 MB/s) |
| hcl/ | 2.27 ms/op (0.40 MB/s) | 1.35 µs/op (671 MB/s) |
## Cross-language byte parity
The 0.14.0 hard gate "Rust 与 Go 的 PVCE/PGCE bytes 完全一致" (roadmap
§16.1; plan §4.4) is verified by a bidirectional byte-equality harness.
Beyond the shared vectors —whose `pvce.*`/`pgce.*` hex fields are covered by
the Go conformance runner (G0.4) —this harness compares the encoders of
both languages byte-for-byte on a data-driven case set, plus the
bidirectional direction (Rust bytes →Go decode →Go re-encode).
Go never imports or calls Rust (RFC 0016 §1.1 cgo ban): both sides encode
the same provisioned case set, and the Rust encoder's bytes are compared as
files.
- `conformance/differential/cases.json` —the shared input set: 68 cases
  (51 PVCE transport values + 17 PGCE graphs) covering all fifteen kinds,
  golden vectors, integer/varint/container boundaries, nesting, sharing, and
  cycles (68 is the exact frozen count; the integrity test fails if the
  file drifts from it or loses kind coverage). Single-authority location
  of the consema repository (migrated from go/ on 2026-08-12,
  https://github.com/consema/consema/blob/main/docs/five-language-ci-design.md §3.5): the normalized and protocol-exchange
  case files live at `conformance/differential/normalized/cases.json` and
  `conformance/differential/protocol-exchange/cases.json`.
- `go/conformance/differential/differential_test.go` —the Go side: parses
  `cases.json` (loaded at runtime from the shared case directory:
  `CONSEMA_DIFFERENTIAL_CASES_DIR`, or the default walk-up probe for a
  `conformance/differential` checkout — this repo or a sibling consema),
  encodes each case, compares byte-for-byte with the Rust hex files, decodes
  the Rust bytes and re-encodes them. Without `CONSEMA_DIFFERENTIAL_RUST_DIR`
  the byte-parity test skips (documented skip, never silent) and the
  case-file integrity test still runs (when the case set is reachable).
- `[consema-rs/consema-conformance](https://github.com/consema/consema-rs/tree/main/consema-conformance)/examples/emit_parity_bytes.rs` —the minimal
  Rust encoder driver (justification: no existing Rust entry point encodes
  arbitrary values to PVCE/PGCE and prints bytes; it reuses the published
  codecs only, no new encoding logic).
- `scripts/go-verify-byte-parity.ps1` —the orchestrator: builds the Rust
  example, runs it over the case set, then runs the Go test with the byte
  directory provisioned.
Run it (PowerShell 5.1, no third-party dependencies):
```
powershell -File scripts/go-verify-byte-parity.ps1
```
Measured 2026-08-07: **byte parity 68/68 equal (51 pvce, 17 pgce), zero
byte differences**, bidirectional decode/re-encode stable on every case.
## Capability parity (0.18.0 G4.4 hard gate)
The Go mandatory capability set equals the Rust Feature-Complete Manifest
capability set (roadmap §16.5「`0.18.0`：Go HCL 与全操作 parity」
capability-parity 硬门禁「Go mandatory capability set 与 Rust
Feature-Complete Manifest 对齐」; `docs/fc-manifest-0.13.0.json`
capability_set): **8 families / 16 profiles / 21 query domains / 16
operation registries / 187 error codes**, with no "Rust-only" mandatory
behavior. Pinned by `go/capability_parity_test.go` —the five-number
inventory is read from the provisioned Feature-Complete Manifest
(`digests.capability_set.value`; the test skips with a documented skip
when the manifest is not provisioned — wave-4 2026-08-15, ENTRY 6: the
counts were previously hardcoded literals, which was itself a
re-declaration), and the per-id lists are transcribed identity pins of
the Rust published surface (facade registry, `consema capabilities`
payload, per-crate operation registries) compared against Go facts
derived from this module's registry surface; drift on either side fails
the test.
Runner state at 0.18.0: **506 passed / 2 documented skips / 0 failed**
(18 suites / 508 cases, aggregate digest `35bebc8d…`, defined against the
canonical LF checkout — the 2026-08-07 CRLF-working-tree value
`e3d6578858…` was replaced on 2026-08-10). This is the 0.18.0 historical
state: the G5.3 exchange findings (consema 仓 commit
[ada5020](https://github.com/consema/consema/commit/ada5020daa7ce04512fd56cf81e8f57fd2147c56)，
G078 对抗审计 2026-08-14：改引母仓完整 URL——该 SHA 在本仓 git 对象库不存在)
later flipped the 2 documented
skips to executed, and the 2026-08-12 P2-B vector reinforcement grew the
inventory to **519/519 cases with zero skips** (18 suites, aggregate digest
`cfd6e296…` — the same count the Rust/TS/Python/Kotlin runners pin);
cross-language normalized-result differential **108/108**; byte parity
**68/68**.
## Three-platform verification (0.19.0 G5.4; status: Windows measured + Linux CI, macOS pending)
Roadmap §22.4「安全与质量」「Windows/Linux/macOS 全部正式 target 通过」
requires the Go side to pass on Windows/Linux/macOS.
**Status (G069, adversarial audit 2026-08-13): Windows 11 is the measured
platform; Linux runs in CI (go-matrix, ubuntu-latest); macOS has no CI
job and no measured record — the macOS leg is pending.** Windows
(go 1.26.5) is the measured platform for the
G5.4 close-out: `gofmt -l .` clean, `go vet ./...` clean, `go build ./...`
clean, `go test -count=1 ./...` all green, `go test -race -count=1 ./...`
all green, `go mod tidy` no-op, all 16 fuzz targets 30s clean-run PASS,
and all 8×2 benchmarks measured above.
The Go gates run in CI as gatekeeper-landed jobs: `go-matrix` (ci-go.yml,
ubuntu-latest, a 2-version matrix '1.26.0' declared minimum + '1.26.5'
matrix-pinned per RFC 0020 §9.2 — R19, wave-4 2026-08-15: 'current
stable' was a frozen-claim overreach (go.dev current stable is 1.26.6);
the docs state the frozen matrix fact — with each leg
pinned — G031, adversarial audit 2026-08-14: the minimum leg is the exact
1.26.0 patch ('1.26.x' resolved to the latest patch and never exercised the
declared minimum); the G5.5 finding F1 was closed in consema 仓 commit
[937b33028e970794c4dcb2bd9819a48bd06cdb1f](https://github.com/consema/consema/commit/937b33028e970794c4dcb2bd9819a48bd06cdb1f)
（Deliver Go 0.19.0 G5.4-G5.5: fuzz matrix, security matrix, usability review）) and `go-differential`
(ci-go.yml, windows-latest, added 2026-08-12 — runs the four harnesses
go-verify-byte-parity / normalized-differential / protocol-exchange /
go-verify-shared-conformance; G062, adversarial audit 2026-08-13 — the
harness list previously omitted the shared-conformance §16.6 hard-gate
harness). plan §3's file-domain table still keeps `.github` off-limits to
Go agents (the jobs are landed by the gatekeeper). The macOS leg is not
yet executed — there is no macOS CI job and no measured record (G069,
adversarial audit 2026-08-13: the old text presented three-platform as
delivered; roadmap §22.4's three-platform requirement is pending); the
remaining Linux/macOS legs below are completed on demand with the exact
same commands:
```
cd go
gofmt -l .                 # must be empty
go vet ./...
go build ./...
go test -count=1 ./...
go test -race -count=1 ./...
go mod tidy                # must not modify go.mod/go.sum
# fuzz smoke (one representative target per family; full 16-target
# clean-run commands in the Fuzz targets section above):
go test -fuzz='^FuzzParse$' -fuzztime=30s ./json/
go test -fuzz='^FuzzAlias$' -fuzztime=30s ./yaml/
go test -fuzz='^FuzzParseBinary$' -fuzztime=30s ./plist/
go test -fuzz='^FuzzParse$' -fuzztime=30s ./hcl/
```

**Note: the 6-target table below is the 0.14.0 historical state (2026-08-07
record); the current 16-target 30s clean-run PASS is in the "Fuzz targets"
section above. The table is kept as historical fact.** (G060, adversarial
audit 2026-08-13: this note and table previously sat inside the code fence
above and rendered as literal text.)

Measured 2026-08-07 (go 1.26.5, Windows 11): every target ran 30s of
fuzzing with no panic, no hang, and no limit bypass:

| Target | execs in 30s | result |
|---|---|---|
| `FuzzPVCE` | 16,868,081 | PASS |
| `FuzzPVCEEncodeDecode` | 9,033,088 | PASS |
| `FuzzPGCE` | 10,446,192 | PASS |
| `FuzzPGCEEncodeDecode` | 10,970,351 | PASS |
| `FuzzCanonicalJSON` | 15,530,209 | PASS |
| `FuzzJSONEncodeDecode` | 6,637,952 | PASS |

```
go test -bench=. -benchtime=1s ./json/ ./toml/ ./yaml/ ./ini/ ./properties/ ./xml/ ./plist/ ./hcl/
```
The completion path is documented, not a CI job (go-implementation-plan §2.6「0.19.0 — 双语言一致性与产品 Beta」G5.4「三平台验证」；five-language-ci-design §5.3「每语言 rollout 顺序」L5 行「三平台矩阵按 Go 先例走文档化完成路径或显式 3-OS 矩阵 job——二选一」决策，已处置 2026-08-12：文档化完成路径，§10 记录)。拆分后本仓 .github/workflows 已是 Go 门禁域（ci-go.yml / release.yml / audit.yml / labeler.yml / pr-labels.yml / stale.yml 六个 workflow）。
