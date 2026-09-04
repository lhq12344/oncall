param(
    [string]$EvidencePath,
    [string]$WslDistro
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$BackendRoot = Resolve-Path (Join-Path $PSScriptRoot "..")
$RepoRoot = Resolve-Path (Join-Path $BackendRoot "..")
if ([string]::IsNullOrWhiteSpace($EvidencePath)) {
    $EvidencePath = Join-Path $BackendRoot ".gocache/phase13-race-evidence.json"
}
if ([string]::IsNullOrWhiteSpace($WslDistro)) {
    $WslDistro = [Environment]::GetEnvironmentVariable("ONCALL_PHASE13_WSL_DISTRO")
}
if ([string]::IsNullOrWhiteSpace($WslDistro)) {
    $WslDistro = "Ubuntu-24.04"
}

function ConvertTo-WslPath {
    param([Parameter(Mandatory = $true)][string]$WindowsPath)

    $fullPath = [IO.Path]::GetFullPath($WindowsPath)
    if ($fullPath -notmatch '^([A-Za-z]):[\\/](.*)$') {
        throw "cannot translate Windows path to WSL path: $WindowsPath"
    }
    $drive = $Matches[1].ToLowerInvariant()
    $rest = $Matches[2] -replace '\\', '/'
    return "/mnt/$drive/$rest"
}

$resolvedEvidencePath = $EvidencePath
if (-not [IO.Path]::IsPathRooted($resolvedEvidencePath)) {
    $resolvedEvidencePath = Join-Path $RepoRoot $resolvedEvidencePath
}
$parent = Split-Path -Parent $resolvedEvidencePath
if (-not [string]::IsNullOrWhiteSpace($parent)) {
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
}

$wslBackendRoot = ConvertTo-WslPath $BackendRoot.Path
if ($wslBackendRoot.Contains("'")) {
    throw "backend WSL path contains a single quote and cannot be shell-quoted safely"
}

$command = "go test -race ./..."
$wslScript = "cd '$wslBackendRoot' && mkdir -p /tmp/oncall-race-cache && GOCACHE=/tmp/oncall-race-cache GOFLAGS=-p=2 CGO_ENABLED=1 $command 2>&1"
$output = (& wsl.exe -d $WslDistro -- bash -lc $wslScript) | Out-String
$exitCode = $LASTEXITCODE
Write-Host $output

$evidence = [ordered]@{
    version = "phase13.race.evidence/v1"
    generated_at = (Get-Date).ToUniversalTime().ToString("o")
    repo_root = $RepoRoot.Path
    backend_root = $BackendRoot.Path
    command = $command
    runner = "wsl:$WslDistro"
    exit_code = $exitCode
    status = if ($exitCode -eq 0) { "complete" } else { "failed" }
    output_tail = ($output -replace "`0", "").Trim()
}
$evidence | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $resolvedEvidencePath -Encoding UTF8
Write-Host "Race evidence written to $resolvedEvidencePath"

if ($exitCode -ne 0) {
    Write-Error "race evidence command failed with exit code $exitCode"
}
