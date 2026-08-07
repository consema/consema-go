# Consema Go SDK (`go/`)

The Go implementation of the language-neutral Consema contracts (RFC 0016;
`docs/go-implementation-plan.md`). Milestone 0.14.0 G0.1 delivers the
scaffold and the `core` package; G0.2 delivers the `graph` package; G0.3
delivers the `protocol` package; 0.15.0 G1.1 delivers the `document`
package (source snapshots, structural locations, formation status, limits,
materialization requests, and verifiable source patches).

## Layout

- `go.mod` — the single module `consema.dev/consema` (RFC 0016 §3.1;
  plan §0.2). Minimum Go version frozen at `go 1.26` for 0.14.0 (plan §1.3).
- `core/` — the value model and PVCE/1 codec:
  - `value.go` — `Value` (closed fifteen-kind interface), `Kind`, `Null`,
    `Boolean`, `String`, `Bytes`, `Object`/`ObjectBuilder` (ordered,
    unique-key entries), `Array`, `Entry`,
    `EntryMapping`/`EntryMappingBuilder` (ordered arbitrary-key
    associations, duplicates allowed);
  - `numbers.go` — `Integer` (wraps `*big.Int`), `Decimal` (canonical
    coefficient × 10^exponent), `BinaryFloat32` (exact IEEE-754 binary32
    bits), `BinaryFloat64` (exact IEEE-754 binary64 bits);
  - `temporal.go` — `Date` (proleptic Gregorian, astronomical years),
    `Time` (wall clock, exact fractional second), `LocalDateTime`,
    `OffsetDateTime` (fixed UTC offset < 24 h);
  - `equal.go` — `Equal` (strict, order-sensitive) and `Hash` (deterministic
    FNV-1a over the canonical PVCE/1 bytes);
  - `pvce.go` — `EncodePVCE`/`EncodePVCEBounded`/`DecodePVCE`,
    `DecodeLimits`/`EncodeLimits`;
  - `errors.go` — `PVCEError` (typed errors with the frozen
    `core.pvce.*@1` registered codes, RFC 0016 §6), `DuplicateKeyError`.
- `protocol/` — the language-neutral protocol layer (G0.3; RFC 0016 §3.2):
  - `contract.go` — the contract registry (v1-v7, 16/18/25/25/30/38/41
    records) and the `core.protocol-message@1` envelope;
  - `error_registry.go` — the error-code registry (v1-v7,
    55/62/90/92/132/166/187 codes) and the `core.error-code-registry@1`
    manifest;
  - `canonical.go` — the canonical tagged JSON transport
    (`core.portable-value-json@1`, RFC 0015 §3.2) and the protocol PVCE
    wrappers;
  - `diagnostic.go` — `Diagnostic` construction validation (registry-bound
    code/category rules) and the `core.diagnostic@1` record;
  - `registry_descriptor.go` — `ProfileDescriptor`, `CapabilityDeclaration`,
    `RegistryManifest`, `CapabilitySet`;
  - `query.go`/`query_validate.go` — `QueryDefinition` validation and
    binding (the full domain/operator table of consema-core) and the
    `core.query-definition@1` record;
  - `exit_class.go` — `ExitClass` and `ClassifyErrorCode` (RFC 0015 §5);
  - `cli.go`/`cli_json.go` — the three CLI machine records
    (CliOutputMessage/BatchPlanMessage/BatchResultMessage) with the full
    presence/cross-constraint validation of RFC 0015 §4/§8/§9.
- `graph/` — the PortableGraph model and PGCE/1 codec (RFC 0006; G0.2):
  - `graph.go` — `Graph`, `Builder` (reserve/define/root lifecycle),
    `NodeID` (graph-local identity), `Node`, `NodeKind`, `MappingEntry`,
    `Limits`;
  - `equal.go` — `Equal` (strict, canonical-numbering, cycle-safe) and
    `Hash` (deterministic FNV-1a over the canonical PGCE/1 bytes);
  - `pgce.go` — `EncodePGCE`/`EncodePGCEBounded`/`DecodePGCE`,
    `PGCELimits`;
  - `errors.go` — `GraphError` (`core.graph.*@1` codes) and `PGCEError`
    (`core.pgce.*@1` codes), both with the RFC 0016 §6 `Code()` contract.
- `document/` — the source-snapshot and patch surface (0.15.0 G1.1; RFC
  0016 §3.2), mirroring the capability face of crates/consema-document:
  - `source.go` — `SourceSnapshot` (immutable raw bytes, SHA-256
    `ContentDigest`, resolved `EncodingFacts`, decoded text, checkpointed
    `DecodedPosition`/`DecodedOffset` coordinate conversion), `SourceLimits`,
    `SourceError` (`core.source.*@1` codes);
  - `encoding.go` — `SourceEncoding` (closed set: binary/utf-8/utf-16le/
    utf-16be/latin-1/versioned Windows code pages), `BomPolicy`/`BomKind`,
    `EncodingRequest`, `EncodingFacts`;
  - `cp874_table.go`/`cp1250_table.go`…`cp1258_table.go`/`cp932_table.go`
    — the frozen Python-stdlib code-page data shared with
    `protocol/cp932_table.go`; 874 and 1250-1258 decode completely, CP932
    decodes exactly like go/protocol (CP936/CP949/CP950 are recognized but
    not yet decoded);
  - `location.go` — `SnapshotIdentity`, `NodeRef`, `Span` (snapshot-bound
    byte offsets), `NodeRole`, `DocumentAuthority`, `LocationError`;
  - `structural.go` — `BinaryRegion`/`BinaryStructuralIndex` (exact
    binary coverage);
  - `source_patch.go` — `SourcePatch` (atomic create/apply with
    original-byte preconditions and digest verification),
    `SourceReplacement`, `SourcePatchLimits`, `SourcePatchError`
    (document-layer codes; structural failures carry
    `core.protocol.invalid-value@1` exactly like the Rust document layer);
  - `formation.go` — `FormationStatus` (closed two-value
    Complete/Recovered, RFC 0016 §5.1 F10);
  - `ids.go`/`limits.go`/`materialization.go` — `FormatFamilyId`,
    `ProfileId`, `MaterializationStyleId`, `NewlinePolicy`, `MappingPolicy`,
    `RepresentabilityPolicy`, `ParseLimits`, `MaterializationLimits`,
    `MaterializationRequest`;
  - `placement.go` — `AssociationPlacement` + `PlacementKind` (the four
    frozen placements; 0.16.0 G2.4);
  - `change_set.go` — `ChangeSet`/`SourceEdit`/`NodeMapping` (the shared
    old-to-new edit records; 0.16.0 G2.4);
  - `edit_plan.go` — `EditPlan`/`EditPlanSourceId` (the shared validated
    dry-run plan; 0.16.0 G2.4);
  - `untouched.go` — `UntouchedByteProof`/`UntouchedByteRegion`/
    `UntouchedByteProofError` (the shared verifiable byte evidence;
    0.16.0 G2.4);
  - `structural.go` — `StructuralPieceKind`/`StructuralPiece`/
    `LosslessStructuralIndex` (the shared exhaustive token/trivia/
    error-region coverage; 0.16.0 G2.4). The shared edit records are the
    canonical implementations of the consema-document records
    (change_set.rs, edit_plan.rs, untouched_proof.rs, lib.rs); the five
    family packages expose them under their established local names as
    aliases and thin wrappers;
- `json/` — the JSON family surface (0.15.0 G1.2; RFC 0016 §5), mirroring
  the capability face of crates/consema-json:
  - `profile.go` — `JsonProfile` (StrictV1/JsoncBoundedV1/Json5StandardV1)
    and `JsonSyntaxKind` (the closed lossless kind vocabulary with the
    stable query spellings);
  - `formation.go` — `Parse` (context-cancellable, typed
    `FormationFailure` with the frozen registered codes);
  - `parser.go` — the lossless JSON/JSONC/JSON5 lexer and parser:
    token/trivia/error-region coverage, recovery diagnostics, the JSON5
    lexical extensions (identifiers, single-quoted strings, hex and
    non-finite numbers, extended whitespace);
  - `document.go`/`semantic.go` — `Document` (render, formation status,
    diagnostics, lossless index, syntax kinds), `JsonValue`
    (kind/boolean/integer/decimal/binary64/string/array/object views with
    `SemanticAvailability`), `JsonObjectMember`/`JsonArrayElement`;
  - `query.go` — `ExecuteJSONQuery`/`ExecuteJSONSyntaxQuery` (+ cursors)
    over validated `protocol.ExecutableQuery` definitions, with result and
    step budgets and context cancellation;
  - `projection.go` — `ProjectionRequest` (+ builder with duplicate
    policies), `Project` with fidelity/report/provenance records and the
    frozen failure codes;
  - `materialization.go` — `Materialize` (canonical compact/pretty, JSON5
    non-finite literals, two-phase formation, provenance map) with the
    frozen failure codes;
  - `edit.go`/`placement.go`/`change_set.go`/`untouched.go` —
    `EditTransaction` (+ builder, seven operations), atomic `Commit`/
    `DryRun` with `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and `FormatOperationRegistry`. The shared edit
    records come from go/document (0.16.0 G2.4); the operation registry
    is format-local.
- `toml/` — the TOML family surface (0.15.0 G1.3; RFC 0001, RFC 0016 §5),
  mirroring the capability face of crates/consema-toml:
  - `toml.go` — `TomlProfile` (Toml10V1), `TomlSyntaxKind` (the closed
    twelve-kind lossless vocabulary with the stable query spellings),
    `TomlItemKind` (the fifteen native item categories incl. table/
    inline-table/dotted-table/array-of-tables — never JSON object/member
    types), `Document` (render, formation status, diagnostics, lossless
    index, syntax kinds), `TomlItem`/`TomlEntry`/`TomlArrayElement`
    handles, `FormationFailure` with the frozen registered codes;
  - `parser.go` — `Parse` over the full TOML 1.0 grammar: byte-exact
    tokenizer with token/trivia coverage, tables, arrays-of-tables,
    dotted keys, inline tables, all scalar forms, the toml_edit-equivalent
    duplicate/implicit/dotted table semantics, and the four parse limits;
  - `query.go` — `ExecuteTomlQuery`/`ExecuteTomlSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation;
  - `projection.go` — `ProjectionRequest`, `Project` with
    fidelity/report/provenance records and the frozen failure codes
    (incl. `toml.projection.unrepresentable-datetime@1`);
  - `materialization.go` — `Materialize` (canonical-document style,
    canonical byte-closure verification, mapping policy, provenance map)
    with the frozen failure codes;
  - `edit.go`/`operation_registry.go` — `EditTransaction` (+ builder,
    scalar and structural operations), atomic `Commit`/`DryRun` with
    `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the seven-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4); the
    operation registry is format-local.
- `yaml/` — the YAML family surface (0.16.0 G2.1; RFC 0007, RFC 0016 §5),
  mirroring the capability face of crates/consema-yaml:
  - `yaml.go` — `YamlProfile` (Yaml12CoreV1 / Yaml11CompatV1, explicit
    selection never dialect guessing), `YamlSyntaxKind` (the closed
    twenty-five-kind lossless vocabulary with the stable query spellings),
    `YamlNodeKind`/`YamlScalarKind`/`YamlScalarStyle`, `Document` (render,
    formation status, diagnostics, lossless index, syntax kinds),
    `YamlDocument`/`YamlNode`/`YamlScalar`/`YamlSequenceItem`/
    `YamlMappingEntry`/`YamlAlias` handles, `FormationFailure` with the
    frozen registered codes;
  - `parser.go`/`resolve.go` — `Parse` over the YAML 1.2.2 presentation
    grammar with the two profiles: byte-exact tokenizer/parser with
    token/trivia coverage, block/flow collections, compact notation,
    block scalars with chomping/folding, anchors/aliases with the
    most-recent-preceding rule, the Core and frozen 1.1 scalar schemas,
    tag directives and explicit-tag validation (never custom tag
    constructors), UTF-8/UTF-16 BOM sources, and the parse limits;
  - `syntax.go` — the lossless syntax tokenizer producing the exact piece
    layout (anchor/alias lexemes for the native composition);
  - `query.go` — `ExecuteYamlQuery`/`ExecuteYamlSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation;
  - `projection.go` — `ProjectGraph` (best-exact graph with provenance)
    and `ProjectValue` (sharing/tag/mapping policies, fidelity, report,
    provenance) with the frozen failure codes;
  - `materialization.go` — `MaterializeGraph`/`MaterializeValue`
    (canonical-block/canonical-flow styles, `&gN` anchors for sharing and
    cycles, canonical byte-closure verification, UTF-16 output, mapping
    policy) with the frozen failure codes;
  - `edit.go`/`operation_registry.go`/`untouched.go` —
    `EditTransaction` (+ builder, the eight frozen operations with the
    anchor-safe rules), atomic `Commit`/`DryRun` with `ChangeSet`,
    `SourcePatch` derivation, `UntouchedByteProof`, and the eight-
    operation `FormatOperationRegistry`. The shared edit records come
    from go/document (0.16.0 G2.4); the operation registry is
    format-local.
- `ini/` — the INI family surface (0.16.0 G2.2; RFC 0009, RFC 0016 §5),
  mirroring the capability face of crates/consema-ini:
  - `profile.go` — `IniProfile` (PortableV1 / WindowsV1 /
    PythonConfigParserV1, explicit selection never dialect guessing),
    `IniEncodingSelection` (profile default, explicit UTF-8/UTF-16LE/
    code page, BOM policy), `IniParseLimits`, the closed fourteen-kind
    `IniSyntaxKind` lossless vocabulary, `IniValueState`/
    `IniQuoteStyle`/`IniLogicalLineKind`;
  - `formation.go`/`parser.go`/`python_case.go` — `Parse` under one
    exact profile: physical-line scanning with limits, per-profile
    comments/sections/entries/continuations, recovery with stable
    diagnostics, deterministic duplicate/case-equivalence groups, the
    pinned Unicode 16.0 `optionxform`, and exhaustive raw-source syntax
    pieces; `Document` with physical/logical lines, section/entry/
    error handles, and exact render identity;
  - `query.go` — `ExecuteIniQuery`/`ExecuteIniSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions (the ten native
    operators, decoded-text syntax matching, profile comparison modes,
    step/result budgets, context cancellation, ordered cursor);
  - `projection.go` — `Project` (best-exact nested EntryMapping and
    explicit RequireObject with comparison/collision policies,
    fidelity/report/provenance) with the frozen failure codes;
  - `materialization.go`/`code_page.go` — `Materialize` (portable/
    windows/python canonical styles, strict encoding with UTF-16 BOM
    and Windows code pages, canonical byte-closure verification) with
    the frozen failure codes;
  - `edit.go`/`change_set.go`/`operation_registry.go`/`untouched.go` —
    `EditTransaction` (+ builder, the eight frozen operations with
    profile-specific quote/multiline/comment ownership), atomic
    `Commit`/`DryRun` with `ChangeSet`, `EditPlan`, `SourcePatch`
    derivation, `UntouchedByteProof`, and the eight-operation
    `FormatOperationRegistry`. The shared edit records come from
    go/document (0.16.0 G2.4); the operation registry is format-local.
- `properties/` — the Java Properties family surface (0.16.0 G2.3; RFC
  0010, RFC 0016 §5), mirroring the capability face of
  crates/consema-properties:
  - `properties.go` — `PropertiesProfile` (ReaderV1 / Latin1V1, explicit
    encoding selection never platform guessing), `JavaString` (exact
    UTF-16 code units, unpaired surrogates preserved as native content,
    `UTF16BE/1` bytes, well-formedness status), `PropertiesSyntaxKind`
    (the closed twelve-kind lossless vocabulary with the stable query
    spellings), `PropertiesValueState`/`PropertiesEscapeKind`/
    `PropertiesLogicalLineKind`, `PropertiesParseLimits`, and
    `FormationFailure` with the frozen registered codes;
  - `document.go`/`parser.go` — `ParseReader`/`ParseLatin1` over the
    natural/logical-line grammar: continuation with the JDK EOF
    backslash rule, key/separator/element splitting, exact left-to-right
    escape decoding, recovery with stable diagnostics, duplicate-key
    groups, exhaustive syntax coverage, and the `Document` with
    `PropertiesNaturalLine`/`PropertiesLogicalLine`/`PropertiesComment`/
    `PropertiesEscape`/`Property`/`PropertiesErrorLine` snapshot-bound
    handles;
  - `query.go` — `ExecutePropertiesQuery`/`ExecutePropertiesSyntaxQuery`
    (+ ordered cursors with cancellation) over validated
    `protocol.ExecutableQuery` definitions, with exact `UTF16BE/1` key
    matching, duplicate preservation, and step/result budgets;
  - `projection.go` — `Project` (best-exact EntryMapping preserving every
    association, explicit unique-Object under RequireUnique/FirstWins/
    LastWinsJdkTable) with atomic unpaired-surrogate failure, fidelity,
    report, and provenance with the frozen failure codes;
  - `materialization.go`/`cp_encode_table.go` — `Materialize`
    (canonical Reader/Latin-1 styles, uppercase `\uXXXX` escapes,
    UTF-16 BOM output, frozen Windows code-page encoding from the
    go/document decode authority, exact reparse/closure verification)
    with the frozen failure codes;
  - `edit.go`/`change_set.go`/`untouched.go`/`operation_registry.go` —
    `EditTransaction` (+ builder, the five frozen operations:
    replace-semantic-value, replace-literal-value, insert-property,
    remove-property, rename-property), atomic `Commit`/`DryRun` with
    `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the five-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4); the
    operation registry is format-local.
- `xml/` — the XML family surface (0.17.0 G3.1; RFC 0012, RFC 0016 §5),
  mirroring the capability face of crates/consema-xml:
  - `profile.go` — `XmlProfile` (SafeV1), `XmlEncodingSelection`
    (profile default, explicit UTF-8/UTF-16LE/UTF-16BE), `XmlParseLimits`
    (common limits plus the element/attribute/namespace-declaration/
    mixed-content/DQname/comment/PI/CDATA/text/DTD/entity counts, the
    six-dimension entity expansion budgets, and recovery regions), and
    the closed `XmlSyntaxKind` lossless vocabulary (RFC 0012 §7);
  - `namespace.go` — `QName`/`ExpandedName`/`Binding`, the immutable
    ancestry-derived `NamespaceScope` (default namespace on elements
    only, permanent `xml` binding, reserved `xmlns`, expanded-name
    attribute uniqueness), and the four namespace failure kinds;
  - `entity.go` — the five predefined entities, XML 1.0 character
    validation, replacement-text validation (never markup), and the
    document-wide `EntityExpansionState` accounting with the six frozen
    breach categories;
  - `document.go` — the immutable namespace-aware native tree
    (`XmlElement`/`XmlContentItem` handles, ordered attributes and
    namespace bindings as separate associations, ordered mixed content,
    reference fragments, declaration/doctype facts, error regions) with
    exact render identity and `TextSemantic` line-end normalization;
  - `parser.go` — `Parse` over the XML 1.0 grammar: an independent
    tokenizer mirroring the reference token surface (top-level
    whitespace/after-elements states, `]]>` and `<?xml ` guards, DTD
    subset scanning), namespace resolution at start-tag finalization,
    bounded internal entities with deterministic recovery, exhaustive
    raw-byte piece coverage, and deterministic diagnostic ordering;
  - `query.go` — `ExecuteXMLQuery`/`ExecuteXMLSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions, with step/result
    budgets and context cancellation (native document order, bounded
    pre-order descendants, in-scope namespace chains);
  - `projection.go` — `Project` under `ElementTreeRequest` (the exact
    `xml.element-tree@1` record), `TextContentRequest`, and
    `SimpleEntryMappingRequest` (all policies mandatory) with
    fidelity/report/provenance records and the frozen failure codes;
  - `materialization.go` — `Materialize` (xml.safe-canonical-document
    style: deterministic spelling, generated prefixes, UTF-8/UTF-16
    output with BOM, canonical byte-closure verification, provenance
    map) with the frozen failure codes;
  - `edit.go`/`change_set.go` — `EditTransaction` (+ builder, the eight
    frozen operations: replace-text, insert/remove/rename/set-value
    attribute, insert/remove/rename element), atomic `Commit`/`DryRun`
    with `ChangeSet`, `EditPlan`, `SourcePatch` derivation,
    `UntouchedByteProof`, and the eight-operation
    `FormatOperationRegistry`. The shared edit records come from
    go/document (0.16.0 G2.4); the operation registry is format-local.
- `plist/` — the plist family surface (0.17.0 G3.2; RFC 0013), mirroring
  the capability face of crates/consema-plist:
  - `profile.go` — `PlistProfile` (XmlV1/BinaryV1), `PlistEncodingSelection`
    (profile default, explicit UTF-8/UTF-16LE/UTF-16BE), `PlistParseLimits`
    (common limits plus object/dict/array/duplicate-key-group/string/data/
    UID/extended-size/offset/ref/fact limits and conversion budgets), and
    the closed `PlistSyntaxKind` lossless vocabulary (RFC 0013 §8.2);
  - `native.go` — the shared representation-independent native value model
    (`PlistString` exact UTF-16 code units with surrogate status,
    `PlistKey`, `PlistInteger`, `PlistReal` with the Float32/Float64 width
    fact, `PlistBoolean`, `PlistDate` exact double seconds, `PlistData`,
    `PlistUID`, `PlistDict`/`PlistArray` ordered associations, the acyclic
    `PlistDocument` arena with shared identity and content-based structural
    equality, `PlistDocumentBuilder`);
  - `parser_xml.go` — `Parse` over the `plist.xml@1` grammar: an
    independent tokenizer mirroring the reference token surface (leading
    BOM/U+FEFF skip, declaration/doctype/element/attribute/text/CDATA
    states, after-elements guards), the strict Apple DOCTYPE and
    `<plist version="1.0">` contracts, the value grammars (integer
    decimal/hex, real specials, whole-second dates with calendar
    validation, strict base64), dictionary association rules, recovery
    with exhaustive piece coverage, and deterministic diagnostic ordering;
  - `parser_binary.go` — `plist.binary@1` formation: header/trailer
    mandatory integrity checks (RFC 0013 §5.11), offset-table validation
    with prefix-cut recovery, the full marker table with extended sizes,
    binary object/offset/ref/trailer facts and region coverage, and the
    hardened offset/ref width and bounds checks;
  - `query.go` — `ExecutePlistNativeQuery`/`ExecutePlistSyntaxQuery`/
    `ExecutePlistBinaryQuery` over validated `protocol.ExecutableQuery`
    definitions, with step/result budgets and context cancellation
    (native source order, lossless syntax pieces, document-level binary
    structure facts);
  - `projection.go` — `Project` under the exact `plist.value-tree@1`
    record and the explicit-policy `plist.projection.require-object@1`
    target (UID policy, collision policies, fidelity/report/provenance
    records, and the frozen failure codes);
  - `materialization.go` — `Materialize` (`plist.xml-canonical@1` /
    `plist.binary-canonical@1`: the Apple header spelling, deterministic
    indentation, minimal-width deduplicated object tables, whole-second
    dates with the authorized `TruncateWithReport` policy, canonical
    byte-closure verification) with the frozen failure codes;
  - `conversion.go`/`document.go` — `ConvertTo` across the two
    representations: serialization, reparse closure, native-model
    equality, the `representation-change` + per-node value-mapped report,
    and atomic `plist.conversion.inexpressible@1` failures;
  - `edit.go` — `EditTransaction` (+ builder, the six frozen operations:
    set-value, insert/remove-dict-entry, rename-dict-key,
    insert/remove-array-element), atomic `Commit`/`DryRun` with
    `ChangeSet`, `SourcePatch` derivation, `UntouchedByteProof`, XML
    byte-level spans, binary structural rewrites (reference blocks,
    offset table, trailer), and the six-operation `FormatOperationRegistry`.
    The shared edit records come from go/document (0.16.0 G2.4).
- `hcl/` — the HCL family surface (0.18.0 G4.1; RFC 0014), mirroring the
  capability face of crates/consema-hcl:
  - `profile.go` — `HclProfile` (NativeV1/TfvarsV1), `HclEncodingSelection`
    (profile default or explicit UTF-8; any other explicit encoding is a
    source-contract conflict with `hcl.parse.encoding@1`), `HclParseLimits`
    (common limits plus decoded text, body/expression/template depth,
    per-body item counts, identifier/string/number/template/heredoc
    lengths, constructor extents, recovery/error/piece/report counts), and
    the closed thirty-kind `HclSyntaxKind` lossless vocabulary (RFC 0014
    §7.2, no `Bom` kind);
  - `formation.go`/`document.go` — `Parse` over the UTF-8-only source
    contract (BOM Recovered with `hcl.parse.byte-order-mark@1`, invalid
    UTF-8 fatal, lone CR Recovered), the self-owned tokenizer, the
    body/expression grammar with deterministic recovery (bracket-aware
    expression regions, unterminated string/heredoc bounds, the
    duplicate-attribute exclusion), and the tfvars profile gate
    (`hcl.tfvars.block-not-allowed@1` per top-level block, which stays a
    native item of the Recovered document);
  - `lexer.go`/`parser.go` — the independent tokenizer (47 token kinds,
    template frame stack for quoted/heredoc/interpolation nesting, UAX
    #31 identifiers, the frozen number grammar, `$${`/`%%{` escapes) and
    the recursive-descent expression parser (full operator precedence,
    traversals/splats, for-expressions, constructors, quoted templates
    and heredocs with region re-lexing for interpolation and directive
    interiors);
  - `expression.go`/`native.go` — the unevaluated expression AST
    (fifteen closed kinds, canonical-decimal numbers by pure decimal
    string arithmetic, structural equality, the literal-complete
    boundary, typed literal extraction) and the body tree
    (`HclDocument`/`HclBody`/`HclAttribute`/`HclBlock`/`HclBlockLabel`/
    `HclErrorRegion` with exact-span double preservation);
  - `query.go` — `ExecuteHCLNativeQuery`/`ExecuteHCLSyntaxQuery` over
    validated `protocol.ExecutableQuery` definitions (native body tree
    operators including the typed literal accessor family with
    `hcl.query.type-mismatch@1`/`hcl.query.non-literal@1`, the error-
    region facts, and the lossless syntax kinds), with step/result
    budgets and context cancellation;
  - `projection.go` — `Project` under the exact `hcl.projection.body@1`
    record (typed members, ordered interleaved items, duplicate object
    keys preserved as ordered entries) with the explicit
    `ProjectExpression` policy producing the authorized `hcl.expression@1`
    record (kind family, exact text, FNV-1a structural fingerprint, and
    the versioned payload codec), atomic `hcl.projection.*@1` failures,
    and the transformed-event report;
  - `materialization.go` — `Materialize` (`hcl.canonical-document@1`:
    UTF-8 without BOM, LF line endings, two-space indentation, minimal
    deterministic escapes, canonical decimal spellings, always-quoted
    labels) with the standalone expression-text validation, the reparse
    closure walk by canonical-decimal/structural equality, and the frozen
    failure codes;
  - `edit.go` — `EditTransaction` (+ builder, the six frozen operations:
    set-attribute-value, insert/remove/rename-attribute,
    insert/remove-block; tfvars publishes the four attribute operations
    only), atomic `Commit`/`DryRun` with the sequential per-operation
    pipeline (splice fold, reparse verification, `ChangeSet`,
    `SourcePatch` derivation, `UntouchedByteProof`, node mappings), and
    the `FormatOperationRegistry`. The shared edit records come from
    go/document.
- `consema` (the package root, `*.go` directly in `go/`) — the facade
  surface (0.15.0 G1.4; RFC 0016 §3.2), mirroring crates/consema:
  - `document.go` — the `Document` union over the implemented format
    documents (currently JSON and TOML; additive as the remaining
    families land) with typed adapters and the common immutable
    snapshot facts;
  - `registry.go` — `Families`/`Profiles`/`QueryDomains`/
    `OperationRegistryFor` (8 families / 16 profiles / 21 query
    domains / 16 operation registries, RFC 0015 §6.2), derived from
    the implementing packages where they exist (the json/toml
    registries never re-declare the operation surface) and declared
    capability facts for the not-yet-implemented families, plus the
    `ParseDocument` single parse entry by profile id;
  - `conversion.go` — `ConvertJSON`/`ConvertTOML`, the explicit
    two-stage projection → PortableValue → materialization composition
    with the retained two-stage report and both provenance directions
    (`core.conversion.*@1` failure codes; cross-family convert lives in
    the root package only).

The RFC 0016 §4.1 mapping (the language-neutral fifteen-kind contract,
配置内容统一处理标准与 Rust 参考实现.md §10): Object → `*core.Object`
(ordered `[]Entry`, never a Go map), Array → `*core.Array`, String →
`core.String`, Integer → `core.Integer`, Decimal → `core.Decimal`,
BinaryFloat32 → `core.BinaryFloat32` (exact IEEE-754 binary32 bits),
BinaryFloat64 → `core.BinaryFloat64`, Bytes → `core.Bytes` (octet sequence),
Date/Time/LocalDateTime/OffsetDateTime → the four `core` temporal types,
Boolean → `core.Boolean`, Null → `core.Null`, EntryMapping →
`*core.EntryMapping` (ordered arbitrary-key associations, duplicates
allowed).

## Build and test

```
cd go
go build ./...
go vet ./...
go test ./...
go test -race ./...
go mod tidy
gofmt -l .
```

All four quality gates are expected to be clean: `go build`, `go vet`, `go
test`, and `gofmt -l` (plan §6).

## Stdlib-only policy

`go.mod` declares zero third-party dependencies (no `require` lines); the
module uses only the Go standard library (`math/big`, `hash/fnv`,
`encoding/binary`, `crypto/sha256`, `unicode/utf8`). Policy: plan §1.3;
RFC 0016 §10 rejected alternatives.

## Golden-bytes provenance

The PVCE/1 golden vectors in `core/pvce_test.go` (`TestPVCEGoldenBytes`) are
transcribed byte-for-byte from the Rust reference codec's in-code pins:

- `crates/consema-pvce/src/lib.rs:1192-1201` — `object_byte_vector_is_frozen`
  (`{"a": Integer(1)}` → hex `5056434501410a01200201611003010101`);
- `crates/consema-pvce/src/lib.rs:1336-1342` — `byte_vector_is_frozen`
  (Null → `50564345010000`; Integer(-256) → `5056434501100402020100`).

The additional-kind golden vectors in `core/fifteen_test.go`
(`TestPVCEFifteenKindGoldenBytes`) are pinned to the Rust encoder's bytes
for the exact values of the Rust `every_core_kind_round_trips` test
(`crates/consema-pvce/src/lib.rs:1129-1174`): BinaryFloat32(0x7fc00001) →
`505643450112047fc00001`, Bytes([0, 255]) → `505643450121030200ff`,
Date(-12345-02-28) → `505643450130070402023039021c`,
Time(23:59:58.125) → `5056434501310c173b3a080301017d03020103`,
LocalDateTime(-12345-02-28T23:59:58.125) →
`50564345013215070402023039021c0c173b3a080301017d03020103`,
OffsetDateTime(…-23:00) →
`5056434501331b070402023039021c0c173b3a080301017d03020103050203014370`,
EntryMapping{true → null} → `505643450142050102000000`, plus the
seven-kind sequence vector. The canonical JSON transport golden vectors in
`protocol/fifteen_transport_test.go` (`TestFifteenKindJSONGoldenVectors`)
are pinned to the Rust `value_transport.rs` encoder output for the same
values.

The PGCE/1 golden vectors in `graph/pgce_test.go` (`TestPGCEGoldenBytes`) are
transcribed byte-for-byte from the Rust reference codec's in-code pins:

- `crates/consema-graph/src/pgce.rs:664-678` — `scalar_byte_vector_is_frozen`
  (scalar "x" tagged `tag:yaml.org,2002:str` → hex
  `504743450101010020157461673a79616d6c2e6f72672c323030323a7374720178`);
- `crates/consema-graph/src/pgce.rs:680-686` — `empty_graph_byte_vector_is_frozen`
  (empty graph → `50474345010000`).

Both are also the shared conformance vector expectations
(`conformance/vectors/portable-graph-v1.json`: `pgce.empty-vector` and
`pgce.scalar-vector`).

The Rust side is the authority for the bytes (roadmap §16.1 hard gate: "Rust
与 Go 的 PVCE/PGCE bytes 完全一致"); any change must land in both languages
together.

## Fuzz targets

Go native fuzzing (`go test -fuzz`) covering the 0.14.0 capability surface
(milestone G0.5; roadmap §16.1 "Go fuzz targets"). Discipline mirrors the
Rust fuzz targets of 0.13.0 (docs/fuzz-evidence-0.13.0.md §2): resource
limits are fixed at the production defaults
(`core.DefaultDecodeLimits` / `graph.DefaultPGCELimits` /
`protocol.DefaultProtocolLimits`), limit failures are passes, and property
assertions detect encode/decode asymmetry. The round-trip targets use
deterministic per-package fifteen-kind value-space / graph generators
(`core/fuzzgen_test.go`, `graph/fuzzgen_test.go`, and the transport
generator inside `protocol/json_fuzz_test.go` — each lives in its own test
package because a shared generator would create an import cycle, the same
reason the Rust fuzz drivers live per crate).

| Target | Package | Asserted property |
|---|---|---|
| `FuzzPVCE` | `core/` | arbitrary bytes → `DecodePVCE`: never panic; limit semantics never bypassed; decode→encode fixed point (successful decode re-encodes to exactly the input bytes) |
| `FuzzPVCEEncodeDecode` | `core/` | `decode(encode(x)) == x` for generated fifteen-kind values, `Equal` holds, re-encode byte-stable |
| `FuzzPGCE` | `graph/` | arbitrary bytes → `DecodePGCE`: never panic; limit semantics never bypassed; decode→encode fixed point |
| `FuzzPGCEEncodeDecode` | `graph/` | `decode(encode(g))` is `Equal` to g (sharing and cycles included), re-encode byte-stable |
| `FuzzCanonicalJSON` | `protocol/` | arbitrary bytes → `DecodeJSON`: never panic; limit semantics never bypassed; decode→encode fixed point |
| `FuzzJSONEncodeDecode` | `protocol/` | canonical transport `decode(encode(x)) == x`, `Equal` holds, re-encode byte-stable |

Run one target (the anchored regex is required: `-fuzz=FuzzPVCE` alone
matches both `FuzzPVCE` and `FuzzPVCEEncodeDecode` and refuses to run):

```
cd go
go test -fuzz='^FuzzPVCE$' -fuzztime=30s ./core/
go test -fuzz='^FuzzPVCEEncodeDecode$' -fuzztime=30s ./core/
go test -fuzz='^FuzzPGCE$' -fuzztime=30s ./graph/
go test -fuzz='^FuzzPGCEEncodeDecode$' -fuzztime=30s ./graph/
go test -fuzz='^FuzzCanonicalJSON$' -fuzztime=30s ./protocol/
go test -fuzz='^FuzzJSONEncodeDecode$' -fuzztime=30s ./protocol/
```

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

The long-running release-candidate fuzz clean-run (the 72h-class soak of
roadmap §22.4) is a 0.19.0 milestone item (plan §2.6 G5.4), not part of
0.14.0.

## Cross-language byte parity

The 0.14.0 hard gate "Rust 与 Go 的 PVCE/PGCE bytes 完全一致" (roadmap
§16.1; plan §4.4) is verified by a bidirectional byte-equality harness.
Beyond the shared vectors — whose `pvce.*`/`pgce.*` hex fields are covered by
the Go conformance runner (G0.4) — this harness compares the encoders of
both languages byte-for-byte on a data-driven case set, plus the
bidirectional direction (Rust bytes → Go decode → Go re-encode).

Go never imports or calls Rust (RFC 0016 §1.1 cgo ban): both sides encode
the same checked-in case set, and the Rust encoder's bytes are compared as
files.

- `go/conformance/differential/cases.json` — the shared input set: 68 cases
  (51 PVCE transport values + 17 PGCE graphs) covering all fifteen kinds,
  golden vectors, integer/varint/container boundaries, nesting, sharing, and
  cycles (≥ 40 required by the milestone; the integrity test fails if the
  file drifts below that or loses kind coverage).
- `go/conformance/differential/differential_test.go` — the Go side: parses
  `cases.json` (embedded), encodes each case, compares byte-for-byte with
  the Rust hex files, decodes the Rust bytes and re-encodes them. Without
  `CONSEMA_DIFFERENTIAL_RUST_DIR` the byte-parity test skips (documented
  skip, never silent) and the case-file integrity test still runs.
- `crates/consema-conformance/examples/emit_parity_bytes.rs` — the minimal
  Rust encoder driver (justification: no existing Rust entry point encodes
  arbitrary values to PVCE/PGCE and prints bytes; it reuses the published
  codecs only, no new encoding logic).
- `scripts/go-verify-byte-parity.ps1` — the orchestrator: builds the Rust
  example, runs it over the case set, then runs the Go test with the byte
  directory provisioned.

Run it (PowerShell 5.1, no third-party dependencies):

```
powershell -File scripts/go-verify-byte-parity.ps1
```

Measured 2026-08-07: **byte parity 68/68 equal (51 pvce, 17 pgce), zero
byte differences**, bidirectional decode/re-encode stable on every case.
