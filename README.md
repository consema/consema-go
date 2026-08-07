# Consema Go SDK (`go/`)

The Go implementation of the language-neutral Consema contracts (RFC 0016;
`docs/go-implementation-plan.md`). Milestone 0.14.0 G0.1 delivers the
scaffold and the `core` package; G0.2 delivers the `graph` package; G0.3
delivers the `protocol` package.

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
`encoding/binary`). Policy: plan §1.3; RFC 0016 §10 rejected alternatives.

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
