param(
    [string]$WslIp = "",
    [int[]]$Ports = @(3100, 6872),
    [string]$ListenAddress = "0.0.0.0",
    [switch]$ExposeMiddleware
)

$ErrorActionPreference = "Stop"

function Test-Admin {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Resolve-WslIp {
    if ($WslIp.Trim()) {
        return $WslIp.Trim()
    }

    $output = & wsl.exe hostname -I 2>$null
    if (-not $output) {
        throw "Could not read WSL IP. Pass -WslIp <ip> explicitly."
    }

    $candidate = ($output -split "\s+" | Where-Object { $_ -match "^\d{1,3}(\.\d{1,3}){3}$" } | Select-Object -First 1)
    if (-not $candidate) {
        throw "Could not parse WSL IP from: $output"
    }
    return $candidate
}

function Ensure-IpHelper {
    $service = Get-Service -Name iphlpsvc -ErrorAction SilentlyContinue
    if (-not $service) {
        throw "IP Helper service (iphlpsvc) was not found. Windows portproxy cannot run on this machine."
    }

    if ($service.StartType -eq "Disabled") {
        Set-Service -Name iphlpsvc -StartupType Manual
    }

    $service.Refresh()
    if ($service.Status -eq "Running") {
        return
    }

    try {
        Start-Service -Name iphlpsvc -ErrorAction Stop
    } catch {
        $detail = Get-CimInstance Win32_Service -Filter "Name='iphlpsvc'" |
            Select-Object Name, State, StartMode, ExitCode, ProcessId
        $detailText = $detail | Format-List | Out-String
        throw @"
Could not start IP Helper service (iphlpsvc), which is required by netsh portproxy.

Run these in Administrator PowerShell, then retry this script:
  Set-Service iphlpsvc -StartupType Manual
  Start-Service iphlpsvc
  Get-Service iphlpsvc

Current service detail:
$detailText
Original error:
$($_.Exception.Message)
"@
    }
}

if (-not (Test-Admin)) {
    throw "Run this script from an Administrator PowerShell."
}

$resolvedWslIp = Resolve-WslIp
Write-Host "Configuring Windows LAN port proxy to WSL IP $resolvedWslIp"
Ensure-IpHelper

if ($ExposeMiddleware) {
    Write-Warning "ExposeMiddleware opens Redis, Milvus, Attu, Etcd, and MinIO to the selected Windows listen address. Use only on trusted networks."
    $Ports = @($Ports + @(31029, 31953, 8000, 2379, 9000, 9001) | Select-Object -Unique)
}

foreach ($port in $Ports) {
    if ($ListenAddress -ne "0.0.0.0") {
        & netsh interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$port | Out-Null
    }
    & netsh interface portproxy delete v4tov4 listenaddress=$ListenAddress listenport=$port | Out-Null
    & netsh interface portproxy add v4tov4 listenaddress=$ListenAddress listenport=$port connectaddress=$resolvedWslIp connectport=$port

    $ruleName = "My_oncall WSL TCP $port"
    $existingRule = Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue
    if ($existingRule) {
        Remove-NetFirewallRule -DisplayName $ruleName
    }
    New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Action Allow -Protocol TCP -LocalPort $port | Out-Null
}

Write-Host ""
Write-Host "Portproxy rules:"
& netsh interface portproxy show v4tov4
Write-Host ""
Write-Host "Check listeners:"
foreach ($port in $Ports) {
    Write-Host "  netstat -ano | findstr :$port"
}
Write-Host ""
Write-Host "Use your Windows LAN IPv4 address, for example:"
Write-Host "  Frontend: http://<windows-lan-ip>:3100"
Write-Host "  Backend:  http://<windows-lan-ip>:6872"
if ($ExposeMiddleware) {
    Write-Host "  Redis:    <windows-lan-ip>:31029"
    Write-Host "  Milvus:   <windows-lan-ip>:31953"
    Write-Host "  Attu:     http://<windows-lan-ip>:8000"
    Write-Host "  Etcd:     http://<windows-lan-ip>:2379"
    Write-Host "  MinIO:    http://<windows-lan-ip>:9000"
    Write-Host "  MinIO UI: http://<windows-lan-ip>:9001"
}
