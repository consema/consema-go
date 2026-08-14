param(
    [string]$CaseFile = '',
    [string]$OutDir = '',
    # consema-rs checkout directory (multi-repo mode); default: <repo
    # root>\consema-rs (CI layout) or a sibling consema-rs checkout (G109)
    [string]$RustWorkspace = ''
)

# ---------------------------------------------------------------------------
# Cross-language PVCE/PGCE byte-parity verification (milestone 0.14.0 G0.5;
# https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
# §4.4; roadmap §16.1 hard gate: "Rust 与 Go 的 PVCE/PGCE bytes 完全一致").
#
# Pipeline (Go never imports or calls Rust, RFC 0016 §1.1):
#   1. builds the minimal Rust encoder example
#      (consema-conformance/examples/emit_parity_bytes.rs);
#   2. runs it over the provisioned case set
#      (conformance/differential/cases.json, the shared single-authority
#      case directory of the consema repository; provisioned from the spec
#      repository — G122, adversarial audit 2026-08-13) into <OutDir> as one
#      `<case-id>.hex` file per case;
#   3. runs the Go side (`go test ./conformance/differential/` with
#      CONSEMA_DIFFERENTIAL_RUST_DIR set) which compares Go encode bytes
#      with the Rust byte files and checks the bidirectional direction
#      (Rust bytes -> Go decode -> Go re-encode).
#
# Requirements: cargo (or $env:CONSEMA_CARGO) and go (or $env:CONSEMA_GO —
# wave-4 2026-08-15, ENTRY 36: the harness honors the override like the
# ts/py/kt harnesses honor CONSEMA_NODE/CONSEMA_PYTHON/CONSEMA_JAVA_HOME,
# so a non-PATH toolchain can be pinned); the Rust workspace is the
# consema-rs checkout (<repo root>\consema-rs by default, -RustWorkspace
# overrides). Windows
# PowerShell 5.1 compatible, no third-party dependencies.
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
# Default resolution (G109, adversarial audit 2026-08-13 — the old default
# only matched the CI nested layout): <repo root>\consema-rs (CI) first,
# then a sibling consema-rs checkout; -RustWorkspace overrides either.
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
# Wave-4 ENTRY 36: the main-language toolchain honors $env:CONSEMA_GO
# (like the ts/py/kt harnesses honor their overrides); default 'go'.
$go = if ($env:CONSEMA_GO) { $env:CONSEMA_GO } else { 'go' }
if (-not (Get-Command $go -ErrorAction SilentlyContinue)) {
    Write-Error "go is not available ('$go'; set CONSEMA_GO to override)"
    exit 1
}

# --- case set ----------------------------------------------------------------
if ($CaseFile -eq '') {
    $CaseFile = Join-Path $workspaceRoot 'conformance\differential\cases.json'
}
if (-not (Test-Path $CaseFile)) {
    Write-Error "differential case file not found: $CaseFile"
    exit 1
}
# UTF8 explicit: PowerShell 5.1 Get-Content defaults to the ANSI codepage.
$cases = Get-Content $CaseFile -Raw -Encoding UTF8 | ConvertFrom-Json
$caseCount = @($cases.cases).Count
# G157 (adversarial audit 2026-08-13): the loose >= 40 floor was widened
# from the test's exact frozen count; guard exactly (differential_test.go
# expectedCaseCount = 68) so a drift to 40..67 is caught here too.
if ($caseCount -ne 68) {
    Write-Error "differential case file has $caseCount cases, want exactly 68 (differential_test.go expectedCaseCount)"
    exit 1
}

# --- Rust side ---------------------------------------------------------------
$cargo = if ($env:CONSEMA_CARGO) { $env:CONSEMA_CARGO } else { 'cargo' }
if (-not (Get-Command $cargo -ErrorAction SilentlyContinue)) {
    Write-Error "cargo is not available ('$cargo')"
    exit 1
}
Write-Host "[1/3] building the Rust encoder example (emit_parity_bytes)..."
Push-Location $RustWorkspace
try {
    & $cargo build --locked -p consema-conformance --example emit_parity_bytes
    $buildExit = $LASTEXITCODE
}
finally {
    Pop-Location
}
if ($buildExit -ne 0) { exit $buildExit }

$targetDir = if ($env:CARGO_TARGET_DIR) { $env:CARGO_TARGET_DIR } else { Join-Path $RustWorkspace 'target' }
$example = Join-Path $targetDir 'debug\examples\emit_parity_bytes.exe'
if (-not (Test-Path $example)) {
    Write-Error "Rust example binary not found: $example"
    exit 1
}
if ($OutDir -eq '') {
    $OutDir = Join-Path $targetDir "go-differential-parity-$nonce"
}
# The env var is consumed by `go test` from the package directory, so it
# must be absolute.
$OutDir = [System.IO.Path]::GetFullPath($OutDir)
if (Test-Path $OutDir) { Remove-Item $OutDir -Recurse -Force }
New-Item -ItemType Directory -Force $OutDir | Out-Null

Write-Host "[2/3] running the Rust encoder over $caseCount cases -> $OutDir"
& $example $CaseFile $OutDir
if ($LASTEXITCODE -ne 0) {
    Write-Error "emit_parity_bytes failed (exit $LASTEXITCODE)"
    exit $LASTEXITCODE
}

# --- Go side -----------------------------------------------------------------
Write-Host "[3/3] running the Go differential test (differential_test.go)..."
$env:CONSEMA_DIFFERENTIAL_RUST_DIR = $OutDir
$env:CONSEMA_DIFFERENTIAL_CASES_DIR = Join-Path $workspaceRoot 'conformance\differential'
# Capture files live outside $OutDir: that directory must contain only the
# Rust encoder's `<case-id>.hex` files (the Go test rejects any other file).
$logDir = Join-Path $env:TEMP "consema-go-parity-$nonce"
New-Item -ItemType Directory -Force $logDir | Out-Null
$stdoutFile = Join-Path $logDir 'go-test.stdout.txt'
$stderrFile = Join-Path $logDir 'go-test.stderr.txt'
Push-Location $goDir
try {
    # G131 (adversarial audit 2026-08-13): under EAP=Stop, PowerShell 5.1
    # turns native-command stderr with redirection into a terminating
    # NativeCommandError — exactly on the failure path whose diagnostics
    # this block exists to capture. EAP is relaxed around the native call.
    $previousEAP = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & $go test -count=1 -v ./conformance/differential/ 1> $stdoutFile 2> $stderrFile
        $testCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousEAP
    }
}
finally {
    Pop-Location
}
Get-Content $stdoutFile | ForEach-Object { Write-Host $_ }
if (Test-Path $stderrFile) {
    Get-Content $stderrFile | ForEach-Object { Write-Host $_ }
}

# The parity test must have RUN (not skipped) and passed.
$output = Get-Content $stdoutFile -Raw
if ($output -match '--- SKIP: TestDifferentialByteParity') {
    Write-Error 'the differential test skipped: the Rust byte directory was not provisioned'
    exit 1
}
if ($output -notmatch '--- PASS: TestDifferentialByteParity') {
    Write-Error "the differential test did not pass (go test exit $testCode)"
    if ($testCode -eq 0) { exit 1 } else { exit $testCode }
}
if ($testCode -ne 0) {
    exit $testCode
}

$summary = [regex]::Match($output, 'byte parity: \d+/\d+ equal \(\d+ pvce, \d+ pgce\)')
if ($summary.Success) {
    Write-Host "RESULT: $($summary.Value)"
} else {
    Write-Error 'cannot find the byte-parity summary line in the test output'
    exit 1
}
Write-Host "byte parity verification complete (exit 0)"
exit 0
