param(
    [string]$EvidencePath,
    [Parameter(Mandatory = $true)]
    [ValidateSet("dependency", "cozeloop_trace", "mutation_approval", "data_flywheel_publish")]
    [string]$Gate,

    [ValidateSet("redis", "elasticsearch", "milvus", "kubernetes", "cozeloop")]
    [string]$Component,
    [switch]$ObservedDegraded,
    [switch]$UnrelatedCapabilitiesAvailable,

    [string]$TraceID,
    [string]$Endpoint,
    [string[]]$SpanCategories,

    [switch]$BackendUIPath,
    [switch]$MatchingApprovalAllowed,
    [switch]$StalePlanRevisionRejected,
    [switch]$ChangedTargetRejected,
    [switch]$ChangedArgsRejected,
    [switch]$ChangedSnapshotHashRejected,

    [string]$CanaryKnowledgeID,
    [switch]$StagingEvalPassed,
    [switch]$CanaryPublished,
    [switch]$RetrievedAfterPublish,
    [string]$RollbackVersion,

    [string]$EvidenceRef
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
    $EvidencePath = Join-Path $RepoRoot "docs/architecture/phase13-production-evidence.json"
}
if (-not [IO.Path]::IsPathRooted($EvidencePath)) {
    $EvidencePath = Join-Path $RepoRoot $EvidencePath
}
if (-not (Test-Path -LiteralPath $EvidencePath -PathType Leaf)) {
    Write-Error "Production evidence file not found: $EvidencePath. Run phase13-production-evidence-init.ps1 first."
}

function Get-Gate {
    param([object]$Document, [string]$Id)
    $matches = @($Document.gates | Where-Object { $_.id -eq $Id })
    if ($matches.Count -ne 1) {
        Write-Error "expected exactly one production evidence gate $Id, found $($matches.Count)"
    }
    return $matches[0]
}

function Set-StringFieldIfProvided {
    param([object]$Object, [string]$Name, [string]$Value)
    if (-not [string]::IsNullOrWhiteSpace($Value)) {
        $Object.$Name = $Value
    }
}

function Set-TrueIfSwitchPresent {
    param([object]$Object, [string]$Name, [switch]$Value)
    if ($Value.IsPresent) {
        $Object.$Name = $true
    }
}

$doc = Get-Content -LiteralPath $EvidencePath -Raw | ConvertFrom-Json
if ($doc.version -ne "phase13.production.evidence/v1") {
    Write-Error "Unexpected production evidence version: $($doc.version)"
}
$doc.date = (Get-Date).ToString("yyyy-MM-dd")
$doc.status = "external_required"

switch ($Gate) {
    "dependency" {
        if ([string]::IsNullOrWhiteSpace($Component)) {
            Write-Error "-Component is required when -Gate dependency"
        }
        $gateDoc = Get-Gate $doc "live_optional_dependency_fault_injection"
        $drills = @($gateDoc.drills | Where-Object { $_.component -eq $Component })
        if ($drills.Count -ne 1) {
            Write-Error "expected exactly one dependency drill for $Component, found $($drills.Count)"
        }
        $drill = $drills[0]
        Set-TrueIfSwitchPresent $drill "observed_degraded" $ObservedDegraded
        Set-TrueIfSwitchPresent $drill "unrelated_capabilities_available" $UnrelatedCapabilitiesAvailable
        Set-StringFieldIfProvided $drill "evidence_ref" $EvidenceRef
    }
    "cozeloop_trace" {
        $gateDoc = Get-Gate $doc "live_cozeloop_trace"
        Set-StringFieldIfProvided $gateDoc "trace_id" $TraceID
        Set-StringFieldIfProvided $gateDoc "endpoint" $Endpoint
        if ($null -ne $SpanCategories -and $SpanCategories.Count -gt 0) {
            $gateDoc.span_categories = @($SpanCategories | ForEach-Object { $_.Trim().ToLowerInvariant() } | Where-Object { $_ })
        }
    }
    "mutation_approval" {
        $gateDoc = Get-Gate $doc "live_mutation_approval"
        Set-TrueIfSwitchPresent $gateDoc "backend_ui_path" $BackendUIPath
        Set-TrueIfSwitchPresent $gateDoc "matching_approval_allowed" $MatchingApprovalAllowed
        Set-TrueIfSwitchPresent $gateDoc "stale_plan_revision_rejected" $StalePlanRevisionRejected
        Set-TrueIfSwitchPresent $gateDoc "changed_target_rejected" $ChangedTargetRejected
        Set-TrueIfSwitchPresent $gateDoc "changed_args_rejected" $ChangedArgsRejected
        Set-TrueIfSwitchPresent $gateDoc "changed_snapshot_hash_rejected" $ChangedSnapshotHashRejected
        Set-StringFieldIfProvided $gateDoc "evidence_ref" $EvidenceRef
    }
    "data_flywheel_publish" {
        $gateDoc = Get-Gate $doc "live_data_flywheel_publish"
        Set-StringFieldIfProvided $gateDoc "canary_knowledge_id" $CanaryKnowledgeID
        Set-TrueIfSwitchPresent $gateDoc "staging_eval_passed" $StagingEvalPassed
        Set-TrueIfSwitchPresent $gateDoc "canary_published" $CanaryPublished
        Set-TrueIfSwitchPresent $gateDoc "retrieved_after_publish" $RetrievedAfterPublish
        Set-StringFieldIfProvided $gateDoc "rollback_version" $RollbackVersion
        Set-StringFieldIfProvided $gateDoc "evidence_ref" $EvidenceRef
    }
}

$doc | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $EvidencePath -Encoding UTF8
Write-Host "Production evidence updated: $EvidencePath"
