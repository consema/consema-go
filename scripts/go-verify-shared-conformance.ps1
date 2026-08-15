param(
    [string]$RustOutDir = '',
    [string]$GoReportFile = '',
    [switch]$StrictSkips,
    # consema-rs checkout directory (multi-repo mode); default: <repo
    # root>\consema-rs (CI layout) or a sibling consema-rs checkout (G109)
    [string]$RustWorkspace = '',
    # Feature-Complete Manifest path; default: <repo root>\docs\
    # fc-manifest-0.13.0.json (local provision) then
    # <repo root>\consema-repo\docs\fc-manifest-0.13.0.json (CI multi-repo
    # mode) — G109
    [string]$ManifestPath = ''
)

# ---------------------------------------------------------------------------
# Shared dual-runner conformance verification (milestone 0.19.0 G5.1;
# https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §2.6 and §4.1/§4.5; roadmap §16.6
# 「`0.19.0`：双语言一致性与产品 Beta」「shared conformance runner
# orchestration」).
#
# Runs the same 18 vector suites with both independent runners in one batch
# and compares them case by case:
#   1. verifies the conformance/vectors aggregate sha256 against the
#      Feature-Complete Manifest independently (the Go runner verifies it
#      itself; the Rust runner embeds its vectors and has no digest check,
#      so this script does the one independent verification — §4.5);
#   2. runs the Rust reference runner over all 18 embedded suites via the
#      auxiliary example emit_conformance_reports.rs (the example reuses the
#      published run_* entry points only; it exists because no other entry
#      point prints the runner's per-case verdicts) into <RustOutDir> as the
#      shared report shared-conformance.json;
#   3. runs the Go runner CLI (go run ./cmd/consema-conformance) over the
#      same 18 suites from the repository vectors;
#   4. runs the Go comparison core (go/conformance/shared.go, integration
#      hook shared_run_test.go) which converts the Go report to the shared
#      contract and compares the two sides case by case — the same case must
#      get the same verdict on both sides; a skip must be the same skip on
#      both sides;
#   5. prints the per-suite two-side counts table from the two shared report
#      files and the overall judgment.
#
# Comparison semantics: passed/passed, skipped/skipped (same case and
# capability claim, both documented), and failed/failed are consistent. A
# case skipped on exactly one side with the required documentation
# (capability + reason, RFC 0016 §7) while the other side passes it is a
# recorded documented-skip asymmetry: reported case by case, and blocking
# under -StrictSkips (the "skip 必须两侧同 skip" rule; the CI go-differential
# job passes -StrictSkips, so the roadmap §16.6 hard gate enforces the rule
# — G057, adversarial audit 2026-08-13). An undocumented skip, a
# pass-vs-fail or skip-vs-fail disagreement, and any inventory divergence
# (suite or case present on one side only, per-file suite identifier
# disagreement) are hard mismatches.
#
# Exit code: 0 = both runners conformant on all 18 suites, no hard
# mismatches, digest verified (documented-skip asymmetries are reported,
# blocking only under -StrictSkips); non-zero = any side failed, any hard
# mismatch, digest mismatch, a strict-mode skip asymmetry, or a harness
# error. The Go runner's RFC 0015 exit class (2 = non-conformant data) is
# propagated.
#
# Requirements: cargo (or $env:CONSEMA_CARGO) and go (or $env:CONSEMA_GO —
# wave-4 2026-08-15, ENTRY 36: the harness honors the override like the
# ts/py/kt harnesses); the Rust workspace is the consema-rs checkout
# (<repo root>\consema-rs by default, -RustWorkspace overrides). Windows
# PowerShell 5.1 compatible and Windows-host-only (wave-5 P2 disclosure):
# the default Rust emitter binary path and the default data/manifest paths below
# use Windows separators and the .exe form, so the harness fails at binary lookup
# on a POSIX pwsh7 host even when all listed prerequisites are met; no third-party
# dependencies.
# ---------------------------------------------------------------------------

$ErrorActionPreference = 'Stop'
# Per-invocation unique directory suffix (G44, 2026-08-14): a fixed shared
# capture/evidence/output/workDir path would let two concurrent runs
# truncate or interleave each other's files and flip the SKIPPED/PASSED
# verdicts; every default TEMP/target path below carries this nonce.
$nonce = [Guid]::NewGuid().ToString('N')
$workspaceRoot = Split-Path -Parent $PSScriptRoot
$goDir = Join-Path $workspaceRoot 'go'
# The Rust emitter workspace lives in the consema-rs repository checkout
# (multi-repo mode): this repository carries the Go implementation only.
# Default resolution (G109): <repo root>\consema-rs (CI) first, then a
# sibling consema-rs checkout; -RustWorkspace overrides either.
if (-not $RustWorkspace) {
    $nested = Join-Path $workspaceRoot 'consema-rs'
    $sibling = Join-Path (Split-Path -Parent $workspaceRoot) 'consema-rs'
    if (Test-Path (Join-Path $nested 'Cargo.toml')) {
        $RustWorkspace = $nested
    }
    elseif (Test-Path (Join-Path $sibling 'Cargo.toml')) {
        $RustWorkspace = $sibling
    }
    else {
        Write-Error "consema-rs checkout not found: tried $nested (CI multi-repo mode) and $sibling (side-by-side layout); pass -RustWorkspace explicitly"
        exit 1
    }
}
$RustWorkspace = [IO.Path]::GetFullPath($RustWorkspace)
$vectorsDir = Join-Path $workspaceRoot 'conformance\vectors'
$fixturesDir = Join-Path $workspaceRoot 'conformance\fixtures'
# The Feature-Complete Manifest is authoritative in the consema spec
# repository. Default (G109): the locally provisioned copy (<repo
# root>\docs\fc-manifest-0.13.0.json, per CONTRIBUTING "Conformance 数据
# 同步") first, then the CI multi-repo checkout
# (<repo root>\consema-repo\docs\fc-manifest-0.13.0.json);
# -ManifestPath overrides either.
if (-not $ManifestPath) {
    $localManifest = Join-Path $workspaceRoot 'docs\fc-manifest-0.13.0.json'
    $ciManifest = Join-Path $workspaceRoot 'consema-repo\docs\fc-manifest-0.13.0.json'
    if (Test-Path $localManifest) {
        $ManifestPath = $localManifest
    }
    elseif (Test-Path $ciManifest) {
        $ManifestPath = $ciManifest
    }
    else {
        Write-Error "Feature-Complete Manifest not found: tried $localManifest (local provision) and $ciManifest (CI multi-repo mode); pass -ManifestPath explicitly"
        exit 1
    }
}

# --- repo layout sanity ------------------------------------------------------
if (-not (Test-Path (Join-Path $RustWorkspace 'Cargo.toml')) -or
    -not (Test-Path (Join-Path $RustWorkspace 'consema-conformance\Cargo.toml'))) {
    Write-Error "consema-rs workspace not found: $RustWorkspace (checkout consema/consema-rs beside this repository, or pass -RustWorkspace)"
    exit 1
}
if (-not (Test-Path (Join-Path $goDir 'go.mod'))) {
    Write-Error "Go module not found: $goDir"
    exit 1
}
if (-not (Test-Path $vectorsDir) -or -not (Test-Path $fixturesDir)) {
    Write-Error "conformance vectors/fixtures not found under $workspaceRoot"
    exit 1
}
if (-not (Test-Path $manifestPath)) {
    Write-Error "Feature-Complete Manifest not found: $manifestPath"
    exit 1
}
# Wave-4 ENTRY 36: the main-language toolchain honors $env:CONSEMA_GO
# (like the ts/py/kt harnesses honor their overrides); default 'go'.
$go = if ($env:CONSEMA_GO) { $env:CONSEMA_GO } else { 'go' }
if (-not (Get-Command $go -ErrorAction SilentlyContinue)) {
    Write-Error "go is not available ('$go'; set CONSEMA_GO to override)"
    exit 1
}

# --- [1/6] independent aggregate digest verification (§4.5) ------------------
# The aggregate algorithm (fc-manifest conformance_suite.note): file-name
# byte-order sort, per-file sha256 lowercase hex, lines `{basename}:{digest}`
# joined with '\n' without a trailing newline, then sha256 of that UTF-8
# string. The Go runner performs the same check itself; the Rust runner has
# no digest check, so this script verifies the inventory once for both sides
# before either runner executes.
#
# NOTE (2026-08-10 revision): the digest is defined against the canonical
# checkout bytes (LF; .gitattributes eol=lf), not the working-tree bytes.
# The 2026-08-07 recorded value e3d6578858... was computed on a CRLF working
# tree (core.autocrlf=true) and has been replaced by the canonical-state
# value 35bebc8d...; the 2026-08-12 P2-B vector reinforcement (508 -> 519
# cases) replaced it again with cfd6e296... (the manifest's
# digests.conformance_suite.aggregate_sha256 field — the field name is the
# anchor; wave-4 R40, 2026-08-15: the old "fc-manifest-0.13.0.json:39"
# line-number reference pointed into a live provisioned file whose line
# numbers drift on every re-vendor; G115 re-verification 2026-08-13). On
# a CRLF working tree this step fails as expected — run with
# `git config core.autocrlf false` (or a clean LF checkout).
Write-Host "[1/6] verifying the conformance/vectors aggregate digest against the Feature-Complete Manifest..."
$manifest = Get-Content $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
$record = $manifest.digests.conformance_suite
if ($null -eq $record -or $record.aggregate_sha256 -eq '') {
    Write-Error "manifest conformance_suite record is absent"
    exit 1
}
$names = @(Get-ChildItem -LiteralPath $vectorsDir -Filter '*.json' | ForEach-Object { $_.Name })
[System.Array]::Sort($names, [System.StringComparer]::Ordinal)
$lines = @()
$caseTotal = 0
foreach ($name in $names) {
    $path = Join-Path $vectorsDir $name
    $vector = Get-Content $path -Raw -Encoding UTF8 | ConvertFrom-Json
    $caseTotal += @($vector.cases).Count
    $digest = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    $lines += "$name`:$digest"
}
$aggregateText = [string]::Join("`n", $lines)
$sha = [System.Security.Cryptography.SHA256]::Create()
try {
    $aggregateBytes = $sha.ComputeHash([System.Text.Encoding]::UTF8.GetBytes($aggregateText))
}
finally {
    $sha.Dispose()
}
$computed = ([System.BitConverter]::ToString($aggregateBytes)).Replace('-', '').ToLowerInvariant()
if ($names.Count -ne $record.suites -or $caseTotal -ne $record.cases -or $computed -ne $record.aggregate_sha256) {
    Write-Error ("vectors digest mismatch: computed $computed, recorded $($record.aggregate_sha256) " +
        "($($names.Count) suites / $caseTotal cases vs manifest $($record.suites) / $($record.cases))")
    exit 1
}
Write-Host "vectors digest: $computed (recorded $($record.aggregate_sha256), $($names.Count) suites, $caseTotal cases)"

# --- [2/6] Rust side: run all 18 embedded suites -----------------------------
$cargo = if ($env:CONSEMA_CARGO) { $env:CONSEMA_CARGO } else { 'cargo' }
if (-not (Get-Command $cargo -ErrorAction SilentlyContinue)) {
    Write-Error "cargo is not available ('$cargo')"
    exit 1
}
$buildWatch = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "[2/6] building the Rust conformance report example (emit_conformance_reports)..."
Push-Location $RustWorkspace
try {
    & $cargo build --locked -p consema-conformance --example emit_conformance_reports
    $buildExit = $LASTEXITCODE
}
finally {
    Pop-Location
}
if ($buildExit -ne 0) { exit $buildExit }
$buildWatch.Stop()

$targetDir = if ($env:CARGO_TARGET_DIR) { $env:CARGO_TARGET_DIR } else { Join-Path $RustWorkspace 'target' }
$example = Join-Path $targetDir 'debug\examples\emit_conformance_reports.exe'
if (-not (Test-Path $example)) {
    Write-Error "Rust example binary not found: $example"
    exit 1
}
if ($RustOutDir -eq '') {
    $RustOutDir = Join-Path $targetDir "go-shared-conformance-$nonce"
}
$RustOutDir = [System.IO.Path]::GetFullPath($RustOutDir)
if (Test-Path $RustOutDir) { Remove-Item $RustOutDir -Recurse -Force }
New-Item -ItemType Directory -Force $RustOutDir | Out-Null

$runWatch = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "running the Rust runner over all 18 suites -> $RustOutDir"
& $example $RustOutDir
if ($LASTEXITCODE -ne 0) {
    Write-Error "emit_conformance_reports failed (exit $LASTEXITCODE)"
    exit $LASTEXITCODE
}
$runWatch.Stop()
Write-Host ("Rust side: build {0}s, run {1}s" -f
    [math]::Round($buildWatch.Elapsed.TotalSeconds, 1),
    [math]::Round($runWatch.Elapsed.TotalSeconds, 1))
$rustReport = Join-Path $RustOutDir 'shared-conformance.json'
if (-not (Test-Path $rustReport)) {
    Write-Error "Rust shared report not found: $rustReport"
    exit 1
}

# --- [3/6] Go side: run the same 18 suites with the Go runner CLI ------------
$logDir = Join-Path $env:TEMP "consema-shared-conformance-$nonce"
New-Item -ItemType Directory -Force $logDir | Out-Null
if ($GoReportFile -eq '') {
    $GoReportFile = Join-Path $logDir 'go-shared-conformance.json'
}
$GoReportFile = [System.IO.Path]::GetFullPath($GoReportFile)
$cliOutFile = Join-Path $logDir 'go-cli.stdout.txt'
$cliErrFile = Join-Path $logDir 'go-cli.stderr.txt'
$cliWatch = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "[3/6] running the Go runner CLI over the same 18 suites..."
Push-Location $goDir
try {
    # G131 (adversarial audit 2026-08-13): under EAP=Stop, PowerShell 5.1
    # turns native-command stderr with redirection into a terminating
    # NativeCommandError — exactly on the failure path whose diagnostics
    # this block exists to capture. EAP is relaxed around the native call.
    $previousEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $go run ./cmd/consema-conformance -vectors $vectorsDir -fixtures $fixturesDir -manifest $manifestPath 1> $cliOutFile 2> $cliErrFile
        $goCliCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousEAP
    }
}
finally {
    Pop-Location
}
$cliWatch.Stop()
Get-Content $cliOutFile | ForEach-Object { Write-Host $_ }
if (Test-Path $cliErrFile) {
    Get-Content $cliErrFile | ForEach-Object { Write-Host $_ }
}
if ($goCliCode -ne 0) {
    Write-Error "Go conformance runner failed (exit $goCliCode)"
    exit $goCliCode
}
Write-Host ("Go side: run {0}s" -f [math]::Round($cliWatch.Elapsed.TotalSeconds, 1))

# --- [4/6] case-id-level comparison (the Go compare core) --------------------
$stdoutFile = Join-Path $logDir 'go-test.stdout.txt'
$stderrFile = Join-Path $logDir 'go-test.stderr.txt'
$env:CONSEMA_SHARED_CONFORMANCE_RUST_DIR = $RustOutDir
$env:CONSEMA_SHARED_CONFORMANCE_GO_REPORT = $GoReportFile
if ($StrictSkips) {
    $env:CONSEMA_SHARED_CONFORMANCE_STRICT = '1'
}
else {
    $env:CONSEMA_SHARED_CONFORMANCE_STRICT = ''
}
$testWatch = [System.Diagnostics.Stopwatch]::StartNew()
Write-Host "[4/6] comparing the two sides case by case (shared_run_test.go)..."
Push-Location $goDir
try {
    # G131: same EAP relaxation as the other redirected native calls
    # (PS 5.1 NativeCommandError on native-command stderr under EAP=Stop).
    $previousEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $go test -count=1 -v ./conformance/ -run '^TestSharedConformanceDualRunner$' 1> $stdoutFile 2> $stderrFile
        $testCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousEAP
    }
}
finally {
    Pop-Location
}
$testWatch.Stop()
Get-Content $stdoutFile | ForEach-Object { Write-Host $_ }
if (Test-Path $stderrFile) {
    Get-Content $stderrFile | ForEach-Object { Write-Host $_ }
}

# The comparison must have RUN (not skipped) and passed.
$output = Get-Content $stdoutFile -Raw
if ($output -match '--- SKIP: TestSharedConformanceDualRunner') {
    Write-Error 'the shared conformance test skipped: the Rust shared report was not provisioned'
    exit 1
}
if ($output -notmatch '--- PASS: TestSharedConformanceDualRunner') {
    Write-Error "the shared conformance test did not pass (go test exit $testCode)"
    if ($testCode -eq 0) { exit 1 } else { exit $testCode }
}
if ($testCode -ne 0) {
    exit $testCode
}
$summary = [regex]::Match($output, 'shared conformance: \d+/\d+ cases agree, \d+ hard mismatches, \d+ documented-skip asymmetries \(\d+ suites\)')
if (-not $summary.Success) {
    Write-Error 'cannot find the shared conformance summary line in the test output'
    exit 1
}
Write-Host ("comparison: {0}s" -f [math]::Round($testWatch.Elapsed.TotalSeconds, 1))

# --- [5/6] per-suite two-side counts table (from the two report files) -------
if (-not (Test-Path $GoReportFile)) {
    Write-Error "Go shared report not found: $GoReportFile"
    exit 1
}
$goShared = Get-Content $GoReportFile -Raw -Encoding UTF8 | ConvertFrom-Json
$rustShared = Get-Content $rustReport -Raw -Encoding UTF8 | ConvertFrom-Json
$rustByFile = @{}
foreach ($rustSuite in @($rustShared.suites)) {
    $rustByFile[$rustSuite.file] = $rustSuite
}
Write-Host ''
Write-Host 'per-suite two-side counts (go passed/skipped/failed | rust passed/skipped/failed):'
foreach ($goSuite in @($goShared.suites)) {
    $rustSuite = $rustByFile[$goSuite.file]
    $rustPassed = 0
    $rustSkipped = 0
    $rustFailed = 0
    if ($null -ne $rustSuite) {
        $rustPassed = @($rustSuite.passed).Count
        $rustSkipped = @($rustSuite.skipped).Count
        $rustFailed = @($rustSuite.failed).Count
    }
    Write-Host ("  {0}: go {1}/{2}/{3} | rust {4}/{5}/{6}" -f
        $goSuite.file,
        @($goSuite.passed).Count, @($goSuite.skipped).Count, @($goSuite.failed).Count,
        $rustPassed, $rustSkipped, $rustFailed)
}

# --- [6/6] overall verdict ---------------------------------------------------
Write-Host "RESULT: $($summary.Value)"
Write-Host 'shared conformance verification complete (exit 0)'
exit 0
