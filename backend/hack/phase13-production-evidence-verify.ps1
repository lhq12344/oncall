param(
    [switch]$AllowPartial,
    [string]$ProductionEvidencePath,
    [string]$EvidencePath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
if ([string]::IsNullOrWhiteSpace($ProductionEvidencePath)) {
    $ProductionEvidencePath = Join-Path $RepoRoot "docs/architecture/phase13-production-evidence.json"
}
$TemplatePath = Join-Path $RepoRoot "docs/architecture/phase13-production-evidence-template.json"

$Evidence = [ordered]@{
    version = "phase13.production.evidence.verification/v1"
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $RepoRoot.Path
    evidence_path = $ProductionEvidencePath
    template_path = $TemplatePath
    status = "running"
    allow_partial = [bool]$AllowPartial
    gates = @()
}
$script:CurrentGate = [ordered]@{
    id = "production_evidence_file"
    command = "read phase13 production evidence JSON"
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
    $Evidence | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $resolvedPath -Encoding UTF8
    Write-Host "Production evidence verification written to $resolvedPath"
}

function Test-TruthyField {
    param([object]$Object, [string]$Name)
    $property = $Object.PSObject.Properties[$Name]
    return $null -ne $property -and [bool]$property.Value
}

function Test-NonEmptyField {
    param([object]$Object, [string]$Name)
    $property = $Object.PSObject.Properties[$Name]
    return $null -ne $property -and -not [string]::IsNullOrWhiteSpace([string]$property.Value)
}

function Get-Gate {
    param([object]$Document, [string]$Id)
    return @($Document.gates | Where-Object { $_.id -eq $Id })[0]
}

function Add-ProductionGateResult {
    param([string]$Id, [bool]$Passed, [string]$Note)
    if ($Passed) {
        Add-EvidenceGate $Id "complete" "validate $Id production evidence" $Note
    }
    else {
        Add-EvidenceGate $Id "external_required" "validate $Id production evidence" $Note
    }
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

if (-not (Test-Path -LiteralPath $ProductionEvidencePath -PathType Leaf)) {
    $Evidence.status = if ($AllowPartial) { "partial" } else { "missing_environment" }
    Add-EvidenceGate "production_evidence_file" "external_required" "read phase13 production evidence JSON" "Create $ProductionEvidencePath from $TemplatePath after running production/live drills"
    Write-EvidenceFile
    if (-not $AllowPartial) {
        Write-Error "Missing production evidence file: $ProductionEvidencePath"
    }
    Write-Host "Phase 13 production evidence is partial; evidence file is missing."
    return
}

$document = Get-Content -LiteralPath $ProductionEvidencePath -Raw | ConvertFrom-Json
if ($document.version -ne "phase13.production.evidence/v1") {
    Write-Error "Unexpected production evidence version: $($document.version)"
}

$dependencyGate = Get-Gate $document "live_optional_dependency_fault_injection"
$requiredComponents = @("redis", "elasticsearch", "milvus", "kubernetes", "cozeloop")
$dependencyMissing = @()
foreach ($component in $requiredComponents) {
    $drill = @($dependencyGate.drills | Where-Object { $_.component -eq $component })[0]
    if ($null -eq $drill -or -not (Test-TruthyField $drill "observed_degraded") -or -not (Test-TruthyField $drill "unrelated_capabilities_available") -or -not (Test-NonEmptyField $drill "evidence_ref")) {
        $dependencyMissing += $component
    }
}
Add-ProductionGateResult "live_optional_dependency_fault_injection" ($dependencyMissing.Count -eq 0) $(if ($dependencyMissing.Count -eq 0) { "all dependency drills present" } else { "missing or incomplete drills: $($dependencyMissing -join ', ')" })

$traceGate = Get-Gate $document "live_cozeloop_trace"
$requiredTraceCategories = @("model", "rag", "tool", "workflow", "error", "token", "cost")
$presentCategories = @($traceGate.span_categories | ForEach-Object { [string]$_ })
$missingCategories = @($requiredTraceCategories | Where-Object { $presentCategories -notcontains $_ })
$tracePassed = (Test-NonEmptyField $traceGate "trace_id") -and (Test-NonEmptyField $traceGate "endpoint") -and $missingCategories.Count -eq 0
Add-ProductionGateResult "live_cozeloop_trace" $tracePassed $(if ($tracePassed) { "trace id and required span categories present" } else { "missing trace fields or span categories: $($missingCategories -join ', ')" })

$approvalGate = Get-Gate $document "live_mutation_approval"
$approvalFields = @("backend_ui_path", "matching_approval_allowed", "stale_plan_revision_rejected", "changed_target_rejected", "changed_args_rejected", "changed_snapshot_hash_rejected")
$missingApproval = @($approvalFields | Where-Object { -not (Test-TruthyField $approvalGate $_) })
$approvalPassed = $missingApproval.Count -eq 0 -and (Test-NonEmptyField $approvalGate "evidence_ref")
Add-ProductionGateResult "live_mutation_approval" $approvalPassed $(if ($approvalPassed) { "backend/UI approval drill evidence present" } else { "missing approval proof fields: $($missingApproval -join ', ')" })

$flywheelGate = Get-Gate $document "live_data_flywheel_publish"
$flywheelFields = @("staging_eval_passed", "canary_published", "retrieved_after_publish")
$missingFlywheel = @($flywheelFields | Where-Object { -not (Test-TruthyField $flywheelGate $_) })
$flywheelPassed = $missingFlywheel.Count -eq 0 -and (Test-NonEmptyField $flywheelGate "canary_knowledge_id") -and (Test-NonEmptyField $flywheelGate "rollback_version") -and (Test-NonEmptyField $flywheelGate "evidence_ref")
Add-ProductionGateResult "live_data_flywheel_publish" $flywheelPassed $(if ($flywheelPassed) { "live flywheel publish/retrieval evidence present" } else { "missing flywheel proof fields: $($missingFlywheel -join ', ')" })

$remaining = @($Evidence.gates | Where-Object { $_.status -ne "complete" })
if ($remaining.Count -gt 0) {
    $Evidence.status = if ($AllowPartial) { "partial" } else { "external_required" }
    Write-EvidenceFile
    if (-not $AllowPartial) {
        Write-Error "Production evidence incomplete: $(@($remaining | ForEach-Object { $_.id }) -join ', ')"
    }
    Write-Host "Phase 13 production evidence verification is partial."
    return
}

$Evidence.status = "complete"
Write-EvidenceFile
Write-Host "Phase 13 production evidence verification complete."
