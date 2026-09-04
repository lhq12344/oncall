param(
    [string]$EvidencePath,
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
$TemplatePath = Join-Path $RepoRoot "docs/architecture/phase13-production-evidence-template.json"
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
    $EvidencePath = Join-Path $RepoRoot "docs/architecture/phase13-production-evidence.json"
}
if (-not [IO.Path]::IsPathRooted($EvidencePath)) {
    $EvidencePath = Join-Path $RepoRoot $EvidencePath
}
if ((Test-Path -LiteralPath $EvidencePath -PathType Leaf) -and -not $Force) {
    Write-Error "Production evidence file already exists: $EvidencePath. Re-run with -Force to overwrite."
}

$doc = Get-Content -LiteralPath $TemplatePath -Raw | ConvertFrom-Json
$doc.date = (Get-Date).ToString("yyyy-MM-dd")
$doc.status = "external_required"

$requiredGateIDs = @(
    "live_optional_dependency_fault_injection",
    "live_cozeloop_trace",
    "live_mutation_approval",
    "live_data_flywheel_publish"
)
foreach ($gateID in $requiredGateIDs) {
    if (@($doc.gates | Where-Object { $_.id -eq $gateID }).Count -ne 1) {
        Write-Error "production evidence template must contain exactly one $gateID gate"
    }
}

$dependencyGate = @($doc.gates | Where-Object { $_.id -eq "live_optional_dependency_fault_injection" })[0]
$requiredDrillFields = @("component", "observed_degraded", "unrelated_capabilities_available", "evidence_ref")
foreach ($drill in @($dependencyGate.drills)) {
    foreach ($field in $requiredDrillFields) {
        if ($null -eq $drill.PSObject.Properties[$field]) {
            Write-Error "production evidence dependency drill is missing required field $field"
        }
    }
}

foreach ($gate in $doc.gates) {
    switch ($gate.id) {
        "live_optional_dependency_fault_injection" {
            foreach ($drill in $gate.drills) {
                $envName = "ONCALL_LIVE_{0}_ADDR" -f ($drill.component.ToUpperInvariant())
                if ($drill.component -eq "cozeloop") {
                    $url = [Environment]::GetEnvironmentVariable("ONCALL_LIVE_COZELOOP_ADDR")
                    if ([string]::IsNullOrWhiteSpace($url)) {
                        $url = [Environment]::GetEnvironmentVariable("COZELOOP_API_BASE_URL")
                    }
                    if (-not [string]::IsNullOrWhiteSpace($url)) {
                        $drill.evidence_ref = "connectivity precheck endpoint: $url; outage drill still required"
                    }
                    continue
                }
                $addr = [Environment]::GetEnvironmentVariable($envName)
                if (-not [string]::IsNullOrWhiteSpace($addr)) {
                    $drill.evidence_ref = "connectivity precheck endpoint: $addr; outage drill still required"
                }
            }
        }
        "live_cozeloop_trace" {
            $endpoint = [Environment]::GetEnvironmentVariable("COZELOOP_API_BASE_URL")
            if ([string]::IsNullOrWhiteSpace($endpoint)) {
                $endpoint = [Environment]::GetEnvironmentVariable("ONCALL_LIVE_COZELOOP_ADDR")
            }
            if (-not [string]::IsNullOrWhiteSpace($endpoint)) {
                $gate.endpoint = $endpoint
            }
        }
    }
}

$parent = Split-Path -Parent $EvidencePath
if (-not [string]::IsNullOrWhiteSpace($parent)) {
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
}
$doc | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $EvidencePath -Encoding UTF8
Write-Host "Production evidence scaffold written to $EvidencePath"
