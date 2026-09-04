param(
    [string]$LocalEvidencePath,
    [string]$LiveEvidencePath,
    [string]$ProductionEvidenceVerificationPath,
    [string]$EvidencePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
$script:CurrentGate = [ordered]@{
    id = "status_summary"
    command = "summarize Phase 13 evidence documents"
}
$script:FailedSummary = $null

function Resolve-OptionalPath {
    param([string]$Path, [string]$Fallback)
    $candidate = if ([string]::IsNullOrWhiteSpace($Path)) { $Fallback } else { $Path }
    if (-not [IO.Path]::IsPathRooted($candidate)) {
        $candidate = Join-Path $RepoRoot $candidate
    }
    return $candidate
}

function Read-EvidenceDocument {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        return $null
    }
    return Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
}

function Count-Gates {
    param([object[]]$Gates, [string]$Status)
    return @($Gates | Where-Object { $_.status -eq $Status }).Count
}

function Get-DocumentField {
    param([object]$Document, [string]$Name, [object]$Fallback = $null)
    if ($null -eq $Document) {
        return $Fallback
    }
    $property = $Document.PSObject.Properties[$Name]
    if ($null -eq $property) {
        return $Fallback
    }
    return $property.Value
}

function New-SourceSummary {
    param([string]$Name, [string]$Path, [object]$Document)
    if ($null -eq $Document) {
        return [ordered]@{
            name = $Name
            path = $Path
            status = "missing"
            complete = 0
            failed = 0
            external_required = 0
            partial = 0
            skipped = 0
        }
    }
    return [ordered]@{
        name = $Name
        path = $Path
        version = Get-DocumentField $Document "version" "unknown"
        status = Get-DocumentField $Document "status" "unknown"
        generated_at = Get-DocumentField $Document "generated_at" ""
        complete = Count-Gates @(Get-DocumentField $Document "gates" @()) "complete"
        failed = Count-Gates @(Get-DocumentField $Document "gates" @()) "failed"
        external_required = Count-Gates @(Get-DocumentField $Document "gates" @()) "external_required"
        partial = Count-Gates @(Get-DocumentField $Document "gates" @()) "partial"
        skipped = Count-Gates @(Get-DocumentField $Document "gates" @()) "skipped"
    }
}

function Write-StatusFile {
    param([object]$Summary)
    if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
        return
    }
    $resolvedPath = Resolve-OptionalPath $EvidencePath $EvidencePath
    $parent = Split-Path -Parent $resolvedPath
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    $Summary | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $resolvedPath -Encoding UTF8
    Write-Host "Phase 13 status written to $resolvedPath"
}

trap {
    $summary = [ordered]@{
        version = "phase13.status/v1"
        generated_at = (Get-Date).ToUniversalTime().ToString("o")
        repo_root = $RepoRoot.Path
        status = "failed"
        failed_gate = $script:CurrentGate
        error = $_.Exception.Message
        open_count = 1
        open_items = @([ordered]@{ source = "status"; id = $script:CurrentGate.id; status = "failed"; note = $_.Exception.Message })
    }
    Write-StatusFile $summary
    break
}

$LocalEvidencePath = Resolve-OptionalPath $LocalEvidencePath "backend/.gocache/phase13-local-final-current.json"
$LiveEvidencePath = Resolve-OptionalPath $LiveEvidencePath "backend/.gocache/phase13-live-final-current.json"
$ProductionEvidenceVerificationPath = Resolve-OptionalPath $ProductionEvidenceVerificationPath "backend/.gocache/phase13-production-evidence-current-check.json"

$local = Read-EvidenceDocument $LocalEvidencePath
$live = Read-EvidenceDocument $LiveEvidencePath
$production = Read-EvidenceDocument $ProductionEvidenceVerificationPath

$sources = @(
    $(New-SourceSummary "local" $LocalEvidencePath $local)
    $(New-SourceSummary "live" $LiveEvidencePath $live)
    $(New-SourceSummary "production" $ProductionEvidenceVerificationPath $production)
)

$openItems = @()
$localStatus = Get-DocumentField $local "status" "missing"
$localGates = @(Get-DocumentField $local "gates" @())
if ($null -eq $local -or $localStatus -ne "complete" -or (Count-Gates $localGates "failed") -gt 0 -or (Count-Gates $localGates "external_required") -gt 0 -or (Count-Gates $localGates "skipped") -gt 0) {
    $openItems += [ordered]@{ source = "local"; id = "local_verification"; status = $localStatus; note = "local Phase 13 verification must be complete with no failed/skipped/external_required gates" }
}
if ($null -eq $live) {
    $openItems += [ordered]@{ source = "live"; id = "live_verification"; status = "missing"; note = "run backend/hack/phase13-live-verify.ps1 with ONCALL_LIVE_* variables" }
}
else {
    foreach ($gate in @(Get-DocumentField $live "gates" @() | Where-Object { $_.status -ne "complete" })) {
        $openItems += [ordered]@{ source = "live"; id = (Get-DocumentField $gate "id" "unknown"); status = (Get-DocumentField $gate "status" "unknown"); note = (Get-DocumentField $gate "note" "") }
    }
}
if ($null -eq $production) {
    $openItems += [ordered]@{ source = "production"; id = "production_evidence"; status = "missing"; note = "run backend/hack/phase13-production-evidence-verify.ps1 after filling production evidence" }
}
else {
    foreach ($gate in @(Get-DocumentField $production "gates" @() | Where-Object { $_.status -ne "complete" })) {
        $openItems += [ordered]@{ source = "production"; id = (Get-DocumentField $gate "id" "unknown"); status = (Get-DocumentField $gate "status" "unknown"); note = (Get-DocumentField $gate "note" "") }
    }
}

$overallStatus = if ($openItems.Count -eq 0) { "complete" } elseif (@($openItems | Where-Object { $_.status -eq "failed" }).Count -gt 0) { "failed" } else { "external_required" }
$summary = [ordered]@{
    version = "phase13.status/v1"
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $RepoRoot.Path
    status = $overallStatus
    sources = $sources
    open_count = $openItems.Count
    open_items = $openItems
}

Write-StatusFile $summary

$summary | ConvertTo-Json -Depth 10
