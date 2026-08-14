param(
    [string]$CaseFile = '',
    [string]$OutDir = '',
    # consema-rs checkout directory (multi-repo mode); default: <repo
    # root>\consema-rs (CI layout) or a sibling consema-rs checkout (G109)
    [string]$RustWorkspace = ''
)

# ---------------------------------------------------------------------------
# Cross-language protocol exchange verification (milestone 0.19.0 G5.3;
# https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
# §2.6 and §4.4; roadmap §16.6「cross-language protocol exchange」与
# §22.2「protocol cross-encode/decode 100%」——G050，对抗审计 2026-08-14：
# 改引节锚，行号删除).
#
# Pipeline (Go never imports or calls Rust, RFC 0016 §1.1):
#   1. builds the Rust example (consema-conformance/examples/
#      emit_protocol_exchange.rs);
#   2. runs it in emit mode over the provisioned case set
#      (conformance/differential/protocol-exchange/cases.json, the shared
#      single-authority case directory of the consema repository;
#      provisioned from the spec repository — G122) into
#      <OutDir>/rust as one `<case-id>.json.hex`, `<case-id>.pvce.hex` or
#      `<case-id>.error.txt` file per case (the Rust side also verifies its
#      own decode/re-encode byte identity and its rejection codes);
#   3. runs the Go side (`go test ./conformance/differential/protocol-
#      exchange/` with CONSEMA_EXCHANGE_RUST_DIR set and
#      CONSEMA_EXCHANGE_GO_DIR set): byte parity with the Rust files,
#      Rust-bytes -> Go-decode record equivalence and byte-identical
#      re-encode, Go-side rejection codes, and writes the Go encoder's own
#      files into <OutDir>/go;
#   4. re-runs the Rust example in --verify mode over the Go files, closing
#      the Go-encode -> Rust-decode direction (record equivalence and
#      byte-identical re-encode on both transports, rejection-code
#      agreement).
#
# Requirements: cargo (or $env:CONSEMA_CARGO) and go (or $env:CONSEMA_GO —
# wave-4 2026-08-15, ENTRY 36: the harness honors the override like the
# ts/py/kt harnesses); the Rust workspace is the consema-rs checkout
# (<repo root>\consema-rs by default, -RustWorkspace overrides). Windows
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
$caseDir = Join-Path $workspaceRoot 'conformance\differential\protocol-exchange'

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
    $CaseFile = Join-Path $caseDir 'cases.json'
}
if (-not (Test-Path $CaseFile)) {
    Write-Error "protocol-exchange case file not found: $CaseFile"
    exit 1
}
# UTF8 explicit: PowerShell 5.1 Get-Content defaults to the ANSI codepage.
$cases = Get-Content $CaseFile -Raw -Encoding UTF8 | ConvertFrom-Json
$caseCount = @($cases.cases).Count
# G157: guard exactly like the byte-parity script (exchange_test.go
# expectedCaseCount = 83); the loose >= 40 floor missed drift to 40..82.
if ($caseCount -ne 83) {
    Write-Error "protocol-exchange case file has $caseCount cases, want exactly 83 (exchange_test.go expectedCaseCount)"
    exit 1
}

# --- Rust side ---------------------------------------------------------------
$cargo = if ($env:CONSEMA_CARGO) { $env:CONSEMA_CARGO } else { 'cargo' }
if (-not (Get-Command $cargo -ErrorAction SilentlyContinue)) {
    Write-Error "cargo is not available ('$cargo')"
    exit 1
}
Write-Host "[1/4] building the Rust exchange example (emit_protocol_exchange)..."
Push-Location $RustWorkspace
try {
    & $cargo build --locked -p consema-conformance --example emit_protocol_exchange
    $buildExit = $LASTEXITCODE
}
finally {
    Pop-Location
}
if ($buildExit -ne 0) { exit $buildExit }

$targetDir = if ($env:CARGO_TARGET_DIR) { $env:CARGO_TARGET_DIR } else { Join-Path $RustWorkspace 'target' }
$example = Join-Path $targetDir 'debug\examples\emit_protocol_exchange.exe'
if (-not (Test-Path $example)) {
    Write-Error "Rust example binary not found: $example"
    exit 1
}
if ($OutDir -eq '') {
    $OutDir = Join-Path $targetDir "go-exchange-$nonce"
}
# The env vars are consumed by `go test` from the package directory, so they
# must be absolute.
$OutDir = [System.IO.Path]::GetFullPath($OutDir)
$rustDir = Join-Path $OutDir 'rust'
$goDirOut = Join-Path $OutDir 'go'
if (Test-Path $OutDir) { Remove-Item $OutDir -Recurse -Force }
New-Item -ItemType Directory -Force $rustDir | Out-Null
New-Item -ItemType Directory -Force $goDirOut | Out-Null

Write-Host "[2/4] running the Rust emitter over $caseCount cases -> $rustDir"
& $example $CaseFile $rustDir
if ($LASTEXITCODE -ne 0) {
    Write-Error "emit_protocol_exchange (emit) failed (exit $LASTEXITCODE)"
    exit $LASTEXITCODE
}

# --- Go side -----------------------------------------------------------------
Write-Host "[3/4] running the Go exchange test (exchange_test.go)..."
$env:CONSEMA_EXCHANGE_RUST_DIR = $rustDir
$env:CONSEMA_EXCHANGE_GO_DIR = $goDirOut
$env:CONSEMA_DIFFERENTIAL_CASES_DIR = Join-Path $workspaceRoot 'conformance\differential'
$logDir = Join-Path $env:TEMP "consema-go-exchange-$nonce"
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
        & $go test -count=1 -v ./conformance/differential/protocol-exchange/ 1> $stdoutFile 2> $stderrFile
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

# The exchange test must have RUN (not skipped) and passed.
$output = Get-Content $stdoutFile -Raw
if ($output -match '--- SKIP: TestProtocolExchange') {
    Write-Error 'the exchange test skipped: the Rust byte directory was not provisioned'
    exit 1
}
if ($output -notmatch '--- PASS: TestProtocolExchange') {
    Write-Error "the exchange test did not pass (go test exit $testCode)"
    if ($testCode -eq 0) { exit 1 } else { exit $testCode }
}
if ($testCode -ne 0) {
    exit $testCode
}

# --- Rust verify over the Go bytes -------------------------------------------
Write-Host "[4/4] running the Rust verifier over the Go encoder bytes -> $goDirOut"
# Wave-4 ENTRY 25 (2026-08-15): the reverse leg previously asserted only
# the exit code and never consumed the verify-mode summary — a Rust verify
# regression to "zero cases processed, exit 0" or a summary-format change
# sailed through green. The leg now captures the summary line and asserts
# its accept/reject split equals the case file's (the same split the
# forward Go side verified).
$verifyLog = Join-Path $logDir 'rust-verify.stdout.txt'
$verifyErr = Join-Path $logDir 'rust-verify.stderr.txt'
# G131: same EAP relaxation as the other redirected native calls
# (PS 5.1 NativeCommandError on native-command stderr under EAP=Stop).
$previousEAP = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
try {
    & $example --verify $CaseFile $goDirOut 1> $verifyLog 2> $verifyErr
    $verifyCode = $LASTEXITCODE
}
finally {
    $ErrorActionPreference = $previousEAP
}
Get-Content $verifyLog | ForEach-Object { Write-Host $_ }
if (Test-Path $verifyErr) {
    Get-Content $verifyErr | ForEach-Object { Write-Host $_ }
}
if ($verifyCode -ne 0) {
    Write-Error "emit_protocol_exchange (verify) failed (exit $verifyCode)"
    exit $verifyCode
}
# Accept cases carry no expected.error_code field (exchange_test.go
# fileCase.Expected); a missing or empty field means accept.
$acceptCases = @($cases.cases | Where-Object { -not $_.expected.error_code }).Count
$rejectCases = $caseCount - $acceptCases
$verifySummary = [regex]::Match((Get-Content $verifyLog -Raw),
    'emit_protocol_exchange \(verify\): (\d+) accept cases and (\d+) reject cases verified')
if (-not $verifySummary.Success) {
    Write-Error 'cannot find the Rust verify-mode summary line in the verify output'
    exit 1
}
if ($verifySummary.Groups[1].Value -ne $acceptCases -or
    $verifySummary.Groups[2].Value -ne $rejectCases) {
    Write-Error ("Rust verify summary mismatch: {0} accept / {1} reject cases verified, " +
        "want {2} / {3} (the case file split the forward Go side verified)") -f
        $verifySummary.Groups[1].Value, $verifySummary.Groups[2].Value,
        $acceptCases, $rejectCases
    exit 1
}

$summary = [regex]::Match($output, 'protocol exchange: \d+/\d+ accept cases and \d+/\d+ reject cases verified')
if ($summary.Success) {
    Write-Host "RESULT: $($summary.Value)"
} else {
    Write-Error 'cannot find the exchange summary line in the test output'
    exit 1
}
Write-Host "cross-language protocol exchange verification complete (exit 0)"
exit 0
