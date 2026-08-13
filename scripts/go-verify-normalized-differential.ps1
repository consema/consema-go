param(
    [string]$CaseFile = '',
    [string]$OutDir = '',
    # consema-rs checkout directory (multi-repo mode); default: <repo
    # root>\consema-rs (CI layout) or a sibling consema-rs checkout (G109)
    [string]$RustWorkspace = ''
)

# ---------------------------------------------------------------------------
# Cross-language normalized-result differential verification (milestone
# 0.15.0 G1.5, bidirectional since 0.19.0 G5.2;
# https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md
# §4.4 and §2.6; roadmap §16.2 line 1494 (the cross-language
# normalized-result differential harness), §16.6 line 1554 (bidirectional),
# §11.2 lines 849-861; G114 line re-verification 2026-08-13).
#
# Bidirectional pipeline (Go never imports or calls Rust, RFC 0016 §1.1):
#   1. builds the minimal Rust evidence example
#      (consema-conformance/examples/emit_normalized_results.rs);
#   2. forward direction: runs it over the provisioned case set
#      (conformance/differential/normalized/cases.json, the shared
#      single-authority case directory of the consema repository;
#      provisioned from the spec repository — G122) into <OutDir> as
#      one `<case-id>.txt` normalized-facts file per case;
#   3. forward comparison + reverse emission: runs the Go side
#      (`go test ./conformance/differential/normalized/` with
#      CONSEMA_DIFFERENTIAL_NORMALIZED_RUST_DIR set), which computes the Go
#      normalized results for the same input set and compares them field by
#      field with the Rust evidence files (case id + field + both values on
#      divergence), and emits the Go-side evidence files into the Go
#      evidence directory (CONSEMA_DIFFERENTIAL_NORMALIZED_GO_DIR);
#   4. reverse direction: runs the Rust example's consume mode
#      (`--consume <go-evidence-dir>`), which recomputes the Rust results
#      and compares them field by field with the Go evidence files.
#
# Any divergence in either direction exits non-zero: forward via the Go
# test, reverse via the consume mode's exit 1.
#
# The compared facts are the language-neutral behavior surface of roadmap
# §11.2: parse formation, diagnostic code/order (never text), query
# count/identity/order, projection/materialization reports, edit result
# bytes or failure codes, and resource-limit completion semantics. A
# divergence is a finding for the roadmap §11.3 process (minimal
# cross-language reproducer -> classify as implementation/test/spec gap),
# never a silent Rust-side "fix".
#
# --- Differential corpus append discipline (roadmap §17.4 line 1615;
# https://github.com/consema/consema/blob/main/docs/go-implementation-plan.md §4.4) ---
# Any differential case found by a pilot or audit joins the input set:
#   1. triage the finding per roadmap §11.3 and reduce it to a minimal
#      cross-language reproducer;
#   2. append the minimal case to cases.json in this directory
#      (conformance/differential/normalized/cases.json) using the same
#      schema as the existing entries (kind/format/profile/source/steps);
#      the integrity guards are automatic and enforced by
#      TestCaseFileIntegrity on every `go test ./...` run: the manifest id
#      must stay consema.differential.normalized@1, ids must be unique, and
#      the case count must stay exactly 108 (normalized_test.go
#      expectedCaseCount). The case then runs in both directions of this
#      script forever.
#   3. a language-neutral defect exposed by the finding (a real bug, not a
#      harness artifact) additionally goes into the regression corpus: the
#      `regressions` array of conformance/corpora/mutation-v1.json,
#      following the existing workflow in conformance/corpora/README.md
#      ("Adding a fuzz finding to the corpus"), which the mutation_corpus
#      replay test covers. That corpus is read-only here: this script never
#      writes it.
#
# Requirements: cargo (or $env:CONSEMA_CARGO) and go on PATH; the Rust
# workspace is the consema-rs checkout (<repo root>\consema-rs by default,
# -RustWorkspace overrides). Windows
# PowerShell 5.1 compatible, no third-party dependencies.
# ---------------------------------------------------------------------------

$ErrorActionPreference = 'Stop'
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
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error 'go is not on PATH'
    exit 1
}

# --- case set ----------------------------------------------------------------
if ($CaseFile -eq '') {
    $CaseFile = Join-Path $workspaceRoot 'conformance\differential\normalized\cases.json'
}
if (-not (Test-Path $CaseFile)) {
    Write-Error "normalized differential case file not found: $CaseFile"
    exit 1
}
# UTF8 explicit: PowerShell 5.1 Get-Content defaults to the ANSI codepage.
$cases = Get-Content $CaseFile -Raw -Encoding UTF8 | ConvertFrom-Json
$caseCount = @($cases.cases).Count
if ($caseCount -ne 108) {
    Write-Error "normalized differential case file has $caseCount cases, want exactly 108 (normalized_test.go expectedCaseCount)"
    exit 1
}

# --- Rust side ---------------------------------------------------------------
$cargo = if ($env:CONSEMA_CARGO) { $env:CONSEMA_CARGO } else { 'cargo' }
if (-not (Get-Command $cargo -ErrorAction SilentlyContinue)) {
    Write-Error "cargo is not available ('$cargo')"
    exit 1
}
Write-Host "[1/4] building the Rust evidence example (emit_normalized_results)..."
Push-Location $RustWorkspace
try {
    & $cargo build --locked -p consema-conformance --example emit_normalized_results
    $buildExit = $LASTEXITCODE
}
finally {
    Pop-Location
}
if ($buildExit -ne 0) { exit $buildExit }

$targetDir = if ($env:CARGO_TARGET_DIR) { $env:CARGO_TARGET_DIR } else { Join-Path $RustWorkspace 'target' }
$example = Join-Path $targetDir 'debug\examples\emit_normalized_results.exe'
if (-not (Test-Path $example)) {
    Write-Error "Rust example binary not found: $example"
    exit 1
}
if ($OutDir -eq '') {
    $OutDir = Join-Path $targetDir 'go-differential-normalized'
}
# The env vars are consumed by `go test` from the package directory, so
# they must be absolute.
$OutDir = [System.IO.Path]::GetFullPath($OutDir)
if (Test-Path $OutDir) { Remove-Item $OutDir -Recurse -Force }
New-Item -ItemType Directory -Force $OutDir | Out-Null

# --- forward direction: Rust emits, Go compares ------------------------------
Write-Host "[2/4] forward: running the Rust example over $caseCount cases -> $OutDir"
& $example $CaseFile $OutDir
if ($LASTEXITCODE -ne 0) {
    Write-Error "emit_normalized_results failed (exit $LASTEXITCODE)"
    exit $LASTEXITCODE
}

# --- Go side: forward comparison + reverse emission ---------------------------
$goEvidenceDir = Join-Path $targetDir 'go-differential-normalized-go'
$goEvidenceDir = [System.IO.Path]::GetFullPath($goEvidenceDir)
if (Test-Path $goEvidenceDir) { Remove-Item $goEvidenceDir -Recurse -Force }
Write-Host "[3/4] running the Go differential test (normalized_test.go) + emitting the Go evidence files -> $goEvidenceDir"
$env:CONSEMA_DIFFERENTIAL_NORMALIZED_RUST_DIR = $OutDir
$env:CONSEMA_DIFFERENTIAL_NORMALIZED_GO_DIR = $goEvidenceDir
$env:CONSEMA_DIFFERENTIAL_CASES_DIR = Join-Path $workspaceRoot 'conformance\differential'
# Capture files live outside $OutDir and $goEvidenceDir: those directories
# must contain only the `<case-id>.txt` evidence files (the Go test and the
# consume mode each reject any other file).
$logDir = Join-Path $env:TEMP 'consema-go-normalized'
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
        & go test -count=1 -v ./conformance/differential/normalized/ 1> $stdoutFile 2> $stderrFile
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

# The differential test must have RUN (not skipped) and passed; the Go
# emitter must have RUN too.
$output = Get-Content $stdoutFile -Raw
if ($output -match '--- SKIP: TestNormalizedDifferential') {
    Write-Error 'the differential test skipped: the Rust evidence directory was not provisioned'
    exit 1
}
if ($output -match '--- SKIP: TestEmitGoNormalizedResults') {
    Write-Error 'the Go evidence emitter skipped: the Go evidence directory was not provisioned'
    exit 1
}
if ($output -notmatch '--- PASS: TestNormalizedDifferential' -or
    $output -notmatch '--- PASS: TestEmitGoNormalizedResults') {
    Write-Error "the Go differential tests did not pass (go test exit $testCode)"
    if ($testCode -eq 0) { exit 1 } else { exit $testCode }
}
if ($testCode -ne 0) {
    exit $testCode
}

$summary = [regex]::Match($output, 'normalized-result differential: \d+/\d+ equal')
if ($summary.Success) {
    Write-Host "RESULT (forward): $($summary.Value)"
} else {
    Write-Error 'cannot find the normalized-result differential summary line in the test output'
    exit 1
}

# --- reverse direction: Rust consumes and compares the Go evidence ------------
Write-Host "[4/4] reverse: running the Rust consume mode against the Go evidence files ($goEvidenceDir)"
$reverseLog = Join-Path $logDir 'rust-consume.stdout.txt'
$reverseErr = Join-Path $logDir 'rust-consume.stderr.txt'
# G131: same EAP relaxation as the forward leg (PS 5.1 NativeCommandError).
$previousEAP = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
try {
    & $example $CaseFile $OutDir --consume $goEvidenceDir 1> $reverseLog 2> $reverseErr
    $consumeCode = $LASTEXITCODE
}
finally {
    $ErrorActionPreference = $previousEAP
}
Get-Content $reverseLog | ForEach-Object { Write-Host $_ }
if (Test-Path $reverseErr) {
    Get-Content $reverseErr | ForEach-Object { Write-Host $_ }
}
if ($consumeCode -ne 0) {
    Write-Error "the Rust consume mode found divergences or failed (exit $consumeCode)"
    exit $consumeCode
}
$reverseSummary = [regex]::Match((Get-Content $reverseLog -Raw), 'reverse normalized-result differential: \d+/\d+ equal')
if ($reverseSummary.Success) {
    Write-Host "RESULT (reverse): $($reverseSummary.Value)"
} else {
    Write-Error 'cannot find the reverse normalized-result differential summary line in the consume-mode output'
    exit 1
}
Write-Host "bidirectional normalized-result differential verification complete (exit 0)"
exit 0
