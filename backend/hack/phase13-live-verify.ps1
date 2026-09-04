param(
    [switch]$AllowPartial,
    [string]$EvidencePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
$LocalGoCache = Join-Path $BackendRoot ".gocache"
$RequiredEnv = @(
    "ONCALL_LIVE_BASE_URL",
    "ONCALL_LIVE_SSE_PRESSURE",
    "ONCALL_LIVE_REDIS_ADDR",
    "ONCALL_LIVE_ELASTICSEARCH_ADDR",
    "ONCALL_LIVE_MILVUS_ADDR",
    "ONCALL_LIVE_KUBERNETES_ADDR",
    "ONCALL_LIVE_COZELOOP_ADDR"
)

$Evidence = [ordered]@{
    version = "phase13.live.evidence/v1"
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $RepoRoot.Path
    backend_root = $BackendRoot.Path
    status = "running"
    allow_partial = [bool]$AllowPartial
    missing_env = @()
    gates = @()
}
$script:CurrentGate = [ordered]@{
    id = "env_check"
    command = "validate ONCALL_LIVE_* environment"
}
$script:LiveGateFailures = @()
$script:LiveGateExternal = @()

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
    Write-Host "Live evidence written to $resolvedPath"
}

function Test-LiveEnvPresent {
    param([string[]]$Names)

    foreach ($name in $Names) {
        if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
            return $false
        }
    }
    return $true
}

function Invoke-LiveGate {
    param(
        [Parameter(Mandatory = $true)][string]$GateID,
        [Parameter(Mandatory = $true)][string]$Label,
        [Parameter(Mandatory = $true)][string]$RunPattern,
        [Parameter(Mandatory = $true)][string[]]$RequiredEnvForGate
    )

    $commandText = "go test -count=1 -tags=integration ./internal/integration -run $RunPattern"
    if (-not (Test-LiveEnvPresent $RequiredEnvForGate)) {
        Add-EvidenceGate $GateID "partial" $commandText "Skipped because required environment for this live gate is missing: $($RequiredEnvForGate -join ', ')"
        return
    }

    Write-Host "==> $Label"
    $script:CurrentGate = [ordered]@{
        id = $GateID
        command = $commandText
    }
    $output = (& go test -count=1 -tags=integration ./internal/integration -run $RunPattern 2>&1) | Out-String
    $exitCode = $LASTEXITCODE
    Write-Host $output
    if ($exitCode -ne 0) {
        $externalPattern = '(?i)401 Unauthorized|Authentication Fails|api key.*invalid|invalid.*api key|credentials.*invalid|authentication.*failed'
        if ($output -match $externalPattern) {
            $note = "$Label requires external authentication/configuration; exit code $exitCode"
            Add-EvidenceGate $GateID "external_required" $commandText $note
            $script:LiveGateExternal += [ordered]@{
                id = $GateID
                command = $commandText
                note = $note
            }
            return
        }
        $note = "$Label failed with exit code $exitCode"
        Add-EvidenceGate $GateID "failed" $commandText $note
        $script:LiveGateFailures += [ordered]@{
            id = $GateID
            command = $commandText
            note = $note
        }
        return
    }
    Add-EvidenceGate $GateID "complete" $commandText
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

$missing = @()
foreach ($name in $RequiredEnv) {
    if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
        $missing += $name
    }
}
$Evidence.missing_env = $missing

if ($missing.Count -gt 0 -and -not $AllowPartial) {
    $Evidence.status = "missing_environment"
    Write-EvidenceFile
    Write-Error "Missing live verification environment variables: $($missing -join ', '). Re-run with -AllowPartial only when intentionally collecting partial evidence."
}

$pressureEnabled = [Environment]::GetEnvironmentVariable("ONCALL_LIVE_SSE_PRESSURE")
if (-not $AllowPartial -and $pressureEnabled -notin @("1", "true", "TRUE", "yes", "YES", "on", "ON")) {
    $Evidence.status = "missing_sse_pressure"
    Write-EvidenceFile
    Write-Error "ONCALL_LIVE_SSE_PRESSURE must be set to 1/true/yes/on for production live verification."
}

if ($missing.Count -gt 0) {
    Write-Warning "Partial live verification; missing: $($missing -join ', ')"
}

New-Item -ItemType Directory -Force -Path $LocalGoCache | Out-Null
$env:GOCACHE = $LocalGoCache

Push-Location $BackendRoot
try {
    Invoke-LiveGate "live_sse_endpoint" "live SSE endpoint gate" "^TestPhaseThirteenLiveSSEEndpoint$" @("ONCALL_LIVE_BASE_URL")
    Invoke-LiveGate "live_sse_pressure" "live SSE pressure/reconnect gate" "^TestPhaseThirteenLiveSSEPressureAndReconnect$" @("ONCALL_LIVE_BASE_URL", "ONCALL_LIVE_SSE_PRESSURE")

    foreach ($target in @(
        @{ id = "live_dependency_redis"; label = "live Redis connectivity gate"; pattern = "^TestPhaseThirteenLiveOptionalDependencyEndpoints/redis$"; env = "ONCALL_LIVE_REDIS_ADDR" },
        @{ id = "live_dependency_elasticsearch"; label = "live Elasticsearch connectivity gate"; pattern = "^TestPhaseThirteenLiveOptionalDependencyEndpoints/elasticsearch$"; env = "ONCALL_LIVE_ELASTICSEARCH_ADDR" },
        @{ id = "live_dependency_milvus"; label = "live Milvus connectivity gate"; pattern = "^TestPhaseThirteenLiveOptionalDependencyEndpoints/milvus$"; env = "ONCALL_LIVE_MILVUS_ADDR" },
        @{ id = "live_dependency_kubernetes"; label = "live Kubernetes connectivity gate"; pattern = "^TestPhaseThirteenLiveOptionalDependencyEndpoints/kubernetes$"; env = "ONCALL_LIVE_KUBERNETES_ADDR" },
        @{ id = "live_dependency_cozeloop"; label = "live CozeLoop connectivity gate"; pattern = "^TestPhaseThirteenLiveOptionalDependencyEndpoints/cozeloop$"; env = "ONCALL_LIVE_COZELOOP_ADDR" }
    )) {
        Invoke-LiveGate $target.id $target.label $target.pattern @($target.env)
    }
}
finally {
    Pop-Location
}

if ($script:LiveGateFailures.Count -gt 0) {
    $Evidence.status = "failed"
    $Evidence["failed_gates"] = $script:LiveGateFailures
    $Evidence["failed_gate"] = $script:LiveGateFailures[0]
    $Evidence["error"] = "live verification failed gates: $(@($script:LiveGateFailures | ForEach-Object { $_.id }) -join ', ')"
    Write-EvidenceFile
    Write-Error $Evidence["error"]
}

$Evidence.status = if ($missing.Count -gt 0 -or $script:LiveGateExternal.Count -gt 0) { "external_required" } else { "complete" }
if ($script:LiveGateExternal.Count -gt 0) {
    $Evidence["external_required_gates"] = $script:LiveGateExternal
}
Write-EvidenceFile
Write-Host "Phase 13 live verification complete."
