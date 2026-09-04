param(
    [switch]$SkipFrontend,
    [switch]$SkipIntegration,
    [switch]$SkipWslRace,
    [switch]$RequireRace,
    [string]$ExternalRaceEvidencePath,
    [string]$EvidencePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
$FrontendRoot = Join-Path $RepoRoot "frontend"
$LocalGoCache = Join-Path $BackendRoot ".gocache"
$Evidence = [ordered]@{
    version = "phase13.local.evidence/v1"
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $RepoRoot.Path
    backend_root = $BackendRoot.Path
    status = "running"
    gates = @()
}
$script:CurrentGate = [ordered]@{
    id = "setup"
    command = "phase13 local verification setup"
}

function Add-EvidenceGate {
    param(
        [Parameter(Mandatory = $true)][string]$Id,
        [Parameter(Mandatory = $true)][string]$Status,
        [Parameter(Mandatory = $true)][string]$Command,
        [string]$Note = ""
    )

    $Evidence.gates += [ordered]@{
        id = $Id
        status = $Status
        command = $Command
        note = $Note
    }
}

function Write-EvidenceFile {
    if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
        return
    }
    $resolvedPath = $EvidencePath
    if (-not [IO.Path]::IsPathRooted($resolvedPath)) {
        $resolvedPath = Join-Path $RepoRoot $resolvedPath
    }
    $parent = Split-Path -Parent $resolvedPath
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $Evidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $resolvedPath -Encoding UTF8
    Write-Host "Evidence written to $resolvedPath"
}

trap {
    if ($Evidence.status -eq "running") {
        $Evidence.status = "failed"
    }
    $Evidence["failed_gate"] = $script:CurrentGate
    $Evidence["error"] = $_.Exception.Message
    Write-EvidenceFile
    break
}

function Invoke-CheckedCommand {
    param(
        [Parameter(Mandatory = $true)][string]$GateID,
        [Parameter(Mandatory = $true)][string]$Label,
        [Parameter(Mandatory = $true)][scriptblock]$Command,
        [Parameter(Mandatory = $true)][string]$WorkingDirectory,
        [string]$CommandText = $Label
    )

    Write-Host "==> $Label"
    $script:CurrentGate = [ordered]@{
        id = $GateID
        command = $CommandText
    }
    Push-Location $WorkingDirectory
    try {
        & $Command
        if ($LASTEXITCODE -ne 0) {
            throw "$Label failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        Pop-Location
    }
}

function ConvertTo-WslPath {
    param([Parameter(Mandatory = $true)][string]$WindowsPath)

    $fullPath = [IO.Path]::GetFullPath($WindowsPath)
    if ($fullPath -notmatch '^([A-Za-z]):[\\/](.*)$') {
        return $null
    }
    $drive = $Matches[1].ToLowerInvariant()
    $rest = $Matches[2] -replace '\\', '/'
    return "/mnt/$drive/$rest"
}

function Invoke-WslRaceGate {
    if ($SkipWslRace) {
        return [ordered]@{
            status = "external_required"
            note = "WSL race fallback skipped by -SkipWslRace"
        }
    }
    if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) {
        return [ordered]@{
            status = "external_required"
            note = "wsl.exe is unavailable on PATH"
        }
    }
    $wslBackendRoot = ConvertTo-WslPath $BackendRoot.Path
    if ([string]::IsNullOrWhiteSpace($wslBackendRoot)) {
        return [ordered]@{
            status = "external_required"
            note = "backend path cannot be translated to a /mnt/<drive> WSL path"
        }
    }

    Write-Host "==> WSL fallback: go test -race ./..."
    if ($wslBackendRoot.Contains("'")) {
        return [ordered]@{
            status = "external_required"
            note = "backend WSL path contains a single quote and cannot be shell-quoted safely"
        }
    }
    $wslScript = "cd '$wslBackendRoot' && mkdir -p /tmp/oncall-race-cache && GOCACHE=/tmp/oncall-race-cache GOFLAGS=-p=2 CGO_ENABLED=1 go test -race ./... 2>&1"
    $wslDistro = [Environment]::GetEnvironmentVariable("ONCALL_PHASE13_WSL_DISTRO")
    if ([string]::IsNullOrWhiteSpace($wslDistro)) {
        $wslDistro = "Ubuntu-24.04"
    }
    $raceOutput = (& wsl.exe -d $wslDistro -- bash -lc $wslScript) | Out-String
    $normalizedRaceOutput = $raceOutput -replace "`0", ""
    $exitCode = $LASTEXITCODE
    Write-Host $raceOutput
    if ($exitCode -eq 0) {
        return [ordered]@{
            status = "complete"
            note = "Race gate completed through WSL distro $wslDistro with GOCACHE=/tmp/oncall-race-cache and GOFLAGS=-p=2"
        }
    }
    if ($normalizedRaceOutput -match '(?i)E_ACCESSDENIED|Permission denied|go: not found|gcc: not found|C compiler "gcc" not found|cannot create directory') {
        return [ordered]@{
            status = "external_required"
            note = "WSL race fallback could not run: $($normalizedRaceOutput.Trim())"
        }
    }
    throw "WSL race gate failed"
}

function Test-ExternalRaceEvidence {
    param([string]$Path)

    if ([string]::IsNullOrWhiteSpace($Path)) {
        return [ordered]@{
            status = "external_required"
            note = "no external race evidence path provided"
        }
    }
    $resolvedPath = $Path
    if (-not [IO.Path]::IsPathRooted($resolvedPath)) {
        $resolvedPath = Join-Path $RepoRoot $resolvedPath
    }
    if (-not (Test-Path -LiteralPath $resolvedPath -PathType Leaf)) {
        return [ordered]@{
            status = "external_required"
            note = "external race evidence file not found: $resolvedPath"
        }
    }

    $document = Get-Content -LiteralPath $resolvedPath -Raw | ConvertFrom-Json
    $requiredCommand = "go test -race ./..."
    $valid = $document.version -eq "phase13.race.evidence/v1" `
        -and $document.status -eq "complete" `
        -and [int]$document.exit_code -eq 0 `
        -and [string]$document.command -eq $requiredCommand `
        -and -not [string]::IsNullOrWhiteSpace([string]$document.repo_root) `
        -and -not [string]::IsNullOrWhiteSpace([string]$document.generated_at)

    if (-not $valid) {
        return [ordered]@{
            status = "external_required"
            note = "external race evidence file is incomplete or does not prove ${requiredCommand}: $resolvedPath"
        }
    }

    return [ordered]@{
        status = "complete"
        note = "Race gate satisfied by verified external race evidence: $resolvedPath"
    }
}

function Invoke-RaceGate {
    Write-Host "==> go test -race ./..."
    $script:CurrentGate = [ordered]@{
        id = "race"
        command = "go test -race ./..."
    }
    Push-Location $BackendRoot
    try {
        $env:GOCACHE = $LocalGoCache
        $env:CGO_ENABLED = "1"
        $isWindowsHost = [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
        if ($isWindowsHost -or $env:OS -eq "Windows_NT") {
            $raceOutput = (& cmd /c "go test -race ./... 2>&1") | Out-String
        }
        else {
            $raceOutput = (& sh -c "go test -race ./... 2>&1") | Out-String
        }
        if ($LASTEXITCODE -eq 0) {
            Write-Host $raceOutput
            return [ordered]@{
                status = "complete"
                note = "Race gate completed on the current host"
            }
        }
        if ($raceOutput -match 'C compiler "gcc" not found' -and -not $RequireRace) {
            Write-Warning "Race gate skipped: gcc is not available on PATH. Re-run with -RequireRace in CI/toolchains that provide gcc."
            Write-Host $raceOutput
            $wslRaceResult = Invoke-WslRaceGate
            if ($wslRaceResult.status -eq "complete") {
                return $wslRaceResult
            }
            $externalRaceResult = Test-ExternalRaceEvidence $ExternalRaceEvidencePath
            if ($externalRaceResult.status -eq "complete") {
                return $externalRaceResult
            }
            return [ordered]@{
                status = "external_required"
                note = "$($wslRaceResult.note); $($externalRaceResult.note)"
            }
        }
        Write-Host $raceOutput
        throw "race gate failed"
    }
    finally {
        $ErrorActionPreference = "Stop"
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path $LocalGoCache | Out-Null
$env:GOCACHE = $LocalGoCache
$env:GOFLAGS = "-p=2"

Invoke-CheckedCommand "gofmt" "gofmt -l ." {
    $unformatted = @(gofmt -l .)
    if ($LASTEXITCODE -ne 0) {
        throw "gofmt failed with exit code $LASTEXITCODE"
    }
    if ($unformatted.Count -gt 0) {
        $unformatted | ForEach-Object { Write-Host $_ }
        throw "gofmt found unformatted files"
    }
} $BackendRoot "gofmt -l ."
Add-EvidenceGate "gofmt" "complete" "gofmt -l ."
Invoke-CheckedCommand "go_test" "go test -count=1 ./..." { go test -count=1 ./... } $BackendRoot "go test -count=1 ./..."
Add-EvidenceGate "go_test" "complete" "go test -count=1 ./..."
Invoke-CheckedCommand "go_vet" "go vet ./..." { go vet ./... } $BackendRoot "go vet ./..."
Add-EvidenceGate "go_vet" "complete" "go vet ./..."

Invoke-CheckedCommand "intent_200" "Phase 13 intent gold gate" { go test -count=1 ./internal/orchestration -run TestPhaseThirteenIntentGoldMacroF1AndHighRiskRecall } $BackendRoot "go test -count=1 ./internal/orchestration -run TestPhaseThirteenIntentGoldMacroF1AndHighRiskRecall"
Add-EvidenceGate "intent_200" "complete" "go test -count=1 ./internal/orchestration -run TestPhaseThirteenIntentGoldMacroF1AndHighRiskRecall" "Expected >=200 cases, macro-F1 >=0.90, and high-risk recall >=0.95"
Invoke-CheckedCommand "security_30" "Phase 13 security gold gate" { go test -count=1 ./internal/orchestration -run TestPhaseThirteenSecurityGoldDeniedRedactedOrDegraded } $BackendRoot "go test -count=1 ./internal/orchestration -run TestPhaseThirteenSecurityGoldDeniedRedactedOrDegraded"
Add-EvidenceGate "security_30" "complete" "go test -count=1 ./internal/orchestration -run TestPhaseThirteenSecurityGoldDeniedRedactedOrDegraded" "Expected >=30 adversarial cases denied, redacted, or degraded"
Invoke-CheckedCommand "approval_snapshot_unit" "approval snapshot gate" { go test -count=1 ./internal/tools/policy -run TestApprovalSnapshotBindsPlanTargetAndArguments } $BackendRoot "go test -count=1 ./internal/tools/policy -run TestApprovalSnapshotBindsPlanTargetAndArguments"
Add-EvidenceGate "approval_snapshot_unit" "complete" "go test -count=1 ./internal/tools/policy -run TestApprovalSnapshotBindsPlanTargetAndArguments"
Invoke-CheckedCommand "data_flywheel_unit" "data flywheel governance gate" { go test -count=1 ./internal/improvement -run TestDataFlywheelGovernanceEndToEndDrill } $BackendRoot "go test -count=1 ./internal/improvement -run TestDataFlywheelGovernanceEndToEndDrill"
Add-EvidenceGate "data_flywheel_unit" "complete" "go test -count=1 ./internal/improvement -run TestDataFlywheelGovernanceEndToEndDrill"
Invoke-CheckedCommand "telemetry_redaction_unit" "telemetry redaction adapter gate" { go test -count=1 ./internal/telemetry -run TestRecorderRedactsBeforeAdapterSink } $BackendRoot "go test -count=1 ./internal/telemetry -run TestRecorderRedactsBeforeAdapterSink"
Add-EvidenceGate "telemetry_redaction_unit" "complete" "go test -count=1 ./internal/telemetry -run TestRecorderRedactsBeforeAdapterSink"
Invoke-CheckedCommand "prompt_cutover_naming" "prompt assembler cutover naming gate" { go test -count=1 ./internal/arch -run TestPromptAssemblerUsesCutoverNaming } $BackendRoot "go test -count=1 ./internal/arch -run TestPromptAssemblerUsesCutoverNaming"
Add-EvidenceGate "prompt_cutover_naming" "complete" "go test -count=1 ./internal/arch -run TestPromptAssemblerUsesCutoverNaming"
Invoke-CheckedCommand "policy_legacy_removed" "policy legacy package removal gate" { go test -count=1 ./internal/arch -run TestNoLegacyPolicyPackage } $BackendRoot "go test -count=1 ./internal/arch -run TestNoLegacyPolicyPackage"
Add-EvidenceGate "policy_legacy_removed" "complete" "go test -count=1 ./internal/arch -run TestNoLegacyPolicyPackage"
Invoke-CheckedCommand "rag_legacy_embedding_removed" "RAG legacy embedding fallback removal gate" { go test -count=1 ./internal/arch -run TestRAGDoesNotUseLegacyEmbeddingFallback } $BackendRoot "go test -count=1 ./internal/arch -run TestRAGDoesNotUseLegacyEmbeddingFallback"
Add-EvidenceGate "rag_legacy_embedding_removed" "complete" "go test -count=1 ./internal/arch -run TestRAGDoesNotUseLegacyEmbeddingFallback"
Invoke-CheckedCommand "incident_old_plan_bridge_removed" "incident old command-plan bridge removal gate" { go test -count=1 ./internal/arch -run TestIncidentRemediationProposalRejectsOldCommandPlanBridge } $BackendRoot "go test -count=1 ./internal/arch -run TestIncidentRemediationProposalRejectsOldCommandPlanBridge"
Add-EvidenceGate "incident_old_plan_bridge_removed" "complete" "go test -count=1 ./internal/arch -run TestIncidentRemediationProposalRejectsOldCommandPlanBridge"
Invoke-CheckedCommand "incident_execution_stage_alias_removed" "incident execution stage alias removal gate" { go test -count=1 ./internal/arch -run TestIncidentStateBridgeUsesCutoverExecutionStagesOnly } $BackendRoot "go test -count=1 ./internal/arch -run TestIncidentStateBridgeUsesCutoverExecutionStagesOnly"
Add-EvidenceGate "incident_execution_stage_alias_removed" "complete" "go test -count=1 ./internal/arch -run TestIncidentStateBridgeUsesCutoverExecutionStagesOnly"
Invoke-CheckedCommand "compact_placeholder_removed" "compact migration placeholder removal gate" { go test -count=1 ./internal/arch -run TestCompactCompatibilityPlaceholderRemoved } $BackendRoot "go test -count=1 ./internal/arch -run TestCompactCompatibilityPlaceholderRemoved"
Add-EvidenceGate "compact_placeholder_removed" "complete" "go test -count=1 ./internal/arch -run TestCompactCompatibilityPlaceholderRemoved"
Invoke-CheckedCommand "skill_middleware_compat_probe_removed" "skill middleware compatibility probe removal gate" { go test -count=1 ./internal/arch -run TestSkillMiddlewareProbeRemoved } $BackendRoot "go test -count=1 ./internal/arch -run TestSkillMiddlewareProbeRemoved"
Add-EvidenceGate "skill_middleware_compat_probe_removed" "complete" "go test -count=1 ./internal/arch -run TestSkillMiddlewareProbeRemoved"
Invoke-CheckedCommand "controller_run_event_streams_only" "controller RunEvent-only stream gate" { go test -count=1 ./internal/arch -run TestControllerStreamsEmitRunEventsOnly } $BackendRoot "go test -count=1 ./internal/arch -run TestControllerStreamsEmitRunEventsOnly"
Add-EvidenceGate "controller_run_event_streams_only" "complete" "go test -count=1 ./internal/arch -run TestControllerStreamsEmitRunEventsOnly"

if (-not $SkipIntegration) {
    Invoke-CheckedCommand "integration_harness" "go test -count=1 -tags=integration ./internal/integration" { go test -count=1 -tags=integration ./internal/integration } $BackendRoot "go test -count=1 -tags=integration ./internal/integration"
    Add-EvidenceGate "integration_harness" "complete" "go test -count=1 -tags=integration ./internal/integration"
}
else {
    Add-EvidenceGate "integration_harness" "skipped" "go test -count=1 -tags=integration ./internal/integration" "Skipped by -SkipIntegration"
}

Invoke-CheckedCommand "incident_replay_20" "incident replay" { go run ./cmd/replayctl --suite testdata/replay/incident } $BackendRoot "go run ./cmd/replayctl --suite testdata/replay/incident"
Add-EvidenceGate "incident_replay_20" "complete" "go run ./cmd/replayctl --suite testdata/replay/incident" "Expected passed=20"
Invoke-CheckedCommand "rag_eval_100" "RAG gold eval" { go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile all --corpus testdata/rag_eval_gold_corpus.jsonl } $BackendRoot "go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile all --corpus testdata/rag_eval_gold_corpus.jsonl"
Add-EvidenceGate "rag_eval_100" "complete" "go run ./cmd/ragctl eval --dataset testdata/rag_eval_gold.jsonl --profile all --corpus testdata/rag_eval_gold_corpus.jsonl" "Expected scored=100 and retrieval metrics equal 1 in offline BM25 mode"
Invoke-CheckedCommand "rag_inspect_smoke" "RAG inspect smoke" { go run ./cmd/ragctl inspect --profile knowledge --query "redis timeout" --top-k 20 --final-top-k 8 } $BackendRoot "go run ./cmd/ragctl inspect --profile knowledge --query redis timeout --top-k 20 --final-top-k 8"
Add-EvidenceGate "rag_inspect_smoke" "complete" "go run ./cmd/ragctl inspect --profile knowledge --query redis timeout --top-k 20 --final-top-k 8"

if (-not $SkipFrontend) {
    Invoke-CheckedCommand "frontend_lint" "npm run lint" { npm run lint } $FrontendRoot "npm run lint"
    Add-EvidenceGate "frontend_lint" "complete" "npm run lint"
    Invoke-CheckedCommand "frontend_test" "npm run test" { npm run test } $FrontendRoot "npm run test"
    Add-EvidenceGate "frontend_test" "complete" "npm run test"
    Invoke-CheckedCommand "sse_reconnect_unit" "frontend SSE reconnect reducer gate" { node --import tsx src/agent-events/reducer.test.ts } $FrontendRoot "node --import tsx src/agent-events/reducer.test.ts"
    Add-EvidenceGate "sse_reconnect_unit" "complete" "node --import tsx src/agent-events/reducer.test.ts"
    Invoke-CheckedCommand "frontend_build" "npm run build" { npm run build } $FrontendRoot "npm run build"
    Add-EvidenceGate "frontend_build" "complete" "npm run build"
    Invoke-CheckedCommand "frontend_run_event_only" "frontend RunEvent-only scan" {
        $forbiddenFrontendPatterns = @(
            'LegacyStreamEvent',
            'legacyToRunEvent',
            'reduceLegacyStreamEvent',
            'handleLegacyEvent',
            'legacy-run',
            'parseBashRequestFromText',
            'Stringer text from compatibility endpoints'
        ) -join '|'
        if (Get-Command rg -ErrorAction SilentlyContinue) {
            $matches = @(rg -n $forbiddenFrontendPatterns frontend/src --glob '*.ts' --glob '*.tsx')
            if ($LASTEXITCODE -eq 1) {
                $global:LASTEXITCODE = 0
            }
            elseif ($LASTEXITCODE -eq 0) {
                $matches | ForEach-Object { Write-Host $_ }
                throw "frontend RunEvent-only scan found legacy event adapters"
            }
            else {
                $matches | ForEach-Object { Write-Host $_ }
                throw "frontend RunEvent-only scan failed with exit code $LASTEXITCODE"
            }
        }
        else {
            $files = Get-ChildItem -Path frontend/src -Recurse -File -Include *.ts,*.tsx
            $matches = $files | Select-String -Pattern $forbiddenFrontendPatterns
            if ($matches) {
                $matches | ForEach-Object { Write-Host "$($_.Path):$($_.LineNumber):$($_.Line.Trim())" }
                throw "frontend RunEvent-only scan found legacy event adapters"
            }
        }
    } $RepoRoot "scan frontend/src for removed legacy stream event adapters"
    Add-EvidenceGate "frontend_run_event_only" "complete" "scan frontend/src for removed legacy stream event adapters"
}
else {
    Add-EvidenceGate "frontend_lint" "skipped" "npm run lint" "Skipped by -SkipFrontend"
    Add-EvidenceGate "frontend_test" "skipped" "npm run test" "Skipped by -SkipFrontend"
    Add-EvidenceGate "sse_reconnect_unit" "skipped" "node --import tsx src/agent-events/reducer.test.ts" "Skipped by -SkipFrontend"
    Add-EvidenceGate "frontend_build" "skipped" "npm run build" "Skipped by -SkipFrontend"
    Add-EvidenceGate "frontend_run_event_only" "skipped" "scan frontend/src for removed legacy stream event adapters" "Skipped by -SkipFrontend"
}

Invoke-CheckedCommand "legacy_cutover_scan" "legacy cutover scan" {
    $legacyPatterns = @(
        'go_agent/internal/' + 'toolkit',
        'internal/' + 'toolkit',
        'package ' + 'toolkit',
        'internal/tools/policy/' + 'legacy',
        'policy/' + 'legacy',
        'Legacy' + 'Retriever',
        'legacy' + 'Retriever',
        'legacy' + 'Collection',
        'embedding_' + 'legacy',
        'legacy_' + 'knowledge',
        'legacy_' + 'ops_cases',
        'legacy' + 'Command',
        'legacy' + 'Plan',
        '"execution", "execute_plan"',
        'compatibility alias',
        'CompatibilityAdapter',
        'legacy compact middleware',
        'Middleware' + 'Compatibility',
        'CheckEino' + 'Middleware' + 'Compatibility',
        'workflow/agent' + 'teams',
        '\[' + 'DONE' + '\]',
        '\[' + 'ERROR' + '\]',
        'Go ' + 'map',
        'map' + '\[command',
        'Global' + 'ES',
        'Get' + 'Elasticsearch',
        'NewV1With' + 'Hooks',
        'func New' + 'V1\('
    ) -join '|'
    if (Get-Command rg -ErrorAction SilentlyContinue) {
        $matches = @(rg -n $legacyPatterns backend frontend/src --glob '*.*' --glob '!backend/hack/phase13-verify.ps1')
        if ($LASTEXITCODE -eq 1) {
            $global:LASTEXITCODE = 0
        }
        elseif ($LASTEXITCODE -eq 0) {
            $matches | ForEach-Object { Write-Host $_ }
            throw "legacy cutover scan found forbidden patterns"
        }
        else {
            $matches | ForEach-Object { Write-Host $_ }
            throw "legacy cutover scan failed with exit code $LASTEXITCODE"
        }
    }
    else {
        $files = Get-ChildItem -Path backend,frontend/src -Recurse -File |
            Where-Object { $_.FullName -notlike "*backend$([IO.Path]::DirectorySeparatorChar)hack$([IO.Path]::DirectorySeparatorChar)phase13-verify.ps1" }
        $matches = $files | Select-String -Pattern $legacyPatterns
        if ($matches) {
            $matches | ForEach-Object { Write-Host "$($_.Path):$($_.LineNumber):$($_.Line.Trim())" }
            throw "legacy cutover scan found forbidden patterns"
        }
    }
} $RepoRoot "legacy cutover scan"
Add-EvidenceGate "legacy_cutover_scan" "complete" "legacy cutover scan"

Invoke-CheckedCommand "git_diff_check" "git diff --check" { git diff --check } $RepoRoot "git diff --check"
Add-EvidenceGate "git_diff_check" "complete" "git diff --check"

Invoke-CheckedCommand "changed_workspace_hygiene" "changed workspace hygiene scan" {
    $rawChangedPaths = git ls-files -z --modified --others --exclude-standard -- . ':!:backend/.gocache/**' ':!:frontend/dist/**' ':!:frontend/node_modules/**'
    $changedPaths = @($rawChangedPaths -split [char]0 | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $textExtensions = @('.go', '.ts', '.tsx', '.js', '.jsx', '.json', '.jsonl', '.md', '.yml', '.yaml', '.ps1', '.cmd', '.mod', '.sum', '.mmd', '.css', '.html')
    $violations = @()
    foreach ($rel in $changedPaths) {
        $path = Join-Path $RepoRoot $rel
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
            continue
        }
        $normalized = $rel.Replace('\\', '/')
        $ext = [IO.Path]::GetExtension($path).ToLowerInvariant()
        if ($textExtensions -notcontains $ext) {
            continue
        }
        $lineNo = 0
        foreach ($line in Get-Content -LiteralPath $path) {
            $lineNo++
            if ($line -match '[ \t]$') {
                $violations += "$($normalized):$($lineNo): trailing whitespace"
            }
            if ($line -match '^(<<<<<<<|=======|>>>>>>>)') {
                $violations += "$($normalized):$($lineNo): merge conflict marker"
            }
        }
    }
    if ($violations.Count -gt 0) {
        $violations | ForEach-Object { Write-Host $_ }
        throw "changed workspace hygiene scan found forbidden text markers"
    }
} $RepoRoot "scan changed and untracked text files for trailing whitespace and merge conflict markers"
Add-EvidenceGate "changed_workspace_hygiene" "complete" "scan changed and untracked text files for trailing whitespace and merge conflict markers"
$raceResult = Invoke-RaceGate
if ($raceResult.status -eq "complete") {
    Add-EvidenceGate "race" "complete" "go test -race ./..." $raceResult.note
}
else {
    Add-EvidenceGate "race" "external_required" "go test -race ./..." $raceResult.note
}

$hasIncompleteGate = @($Evidence.gates | Where-Object { $_.status -ne "complete" }).Count -gt 0
$Evidence.status = if ($hasIncompleteGate) { "external_required" } else { "complete" }
Write-EvidenceFile
if ($hasIncompleteGate) {
    Write-Host "Phase 13 local verification completed with external-required gates."
}
else {
    Write-Host "Phase 13 local verification complete."
}
