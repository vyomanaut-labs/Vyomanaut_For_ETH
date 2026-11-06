#Requires -Version 7.0
# scripts/demo/host.ps1 — host a Vyomanaut network from this Windows laptop with one command.
#
#   pwsh scripts/demo/host.ps1 -Name "ETHGlobal demo"
#
# It starts, in order:
#   1. Postgres in Docker (deployments/dev/docker-compose.yml)
#   2. the relay that phones and laptops connect through (port 30300)
#   3. two free public tunnels, no account needed:
#        bore.pub            -> the relay       (raw TCP)
#        trycloudflare.com   -> the coordinator (HTTPS)
#   4. the coordinator plus local providers (scripts/demo/up.ps1)
#   5. the QR screen and your join desk, in your browser
#
# Guests scan the QR (phones) or open the address under it (laptops). When they
# register, their 6-digit code appears on your join desk; read it to them.
#
#   -Local      no tunnels: only devices on this Wi-Fi can join (or a phone over
#               USB with `adb reverse`). Use it when the venue blocks tunnels.
#   -ChainApi   also give the chain API (port 8090) a public address.
#   -Stop       stop everything this script and up.ps1 started.
#
# Needs: PowerShell 7, Go, Docker Desktop (running). cloudflared and bore are
# downloaded into %TEMP%\vyomanaut-host\tools on first use if not installed.
# psql is not needed: without it, this script runs psql inside the Postgres container.

[CmdletBinding()]
param(
    [string]$Name = "Vyomanaut network",
    [int]$Providers = 7,
    [int]$StorageGB = 10,
    [switch]$Local,
    [switch]$ChainApi,
    [switch]$Stop,
    [int]$Port = 8080,
    [int]$RelayPort = 30300
)

$ErrorActionPreference = "Stop"
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path (Join-Path $ScriptDir "..\..")).Path
Set-Location $RepoRoot

$HostDir = Join-Path $env:TEMP "vyomanaut-host"
$ToolDir = Join-Path $HostDir "tools"
$LogDir = Join-Path $HostDir "logs"
$PidFile = Join-Path $HostDir "pids"
$Compose = Join-Path $RepoRoot "deployments\dev\docker-compose.yml"

function Say($msg) { Write-Host "[host] $msg" -ForegroundColor Cyan }
function Fail($msg) { Write-Host "[host] $msg" -ForegroundColor Red; exit 1 }

function Stop-Hosted {
    if (Test-Path $PidFile) {
        foreach ($id in Get-Content $PidFile) {
            if ($id.Trim()) { Stop-Process -Id ([int]$id) -Force -ErrorAction SilentlyContinue }
        }
        Remove-Item $PidFile -Force
    }
    & (Join-Path $ScriptDir "down.ps1")
}

if ($Stop) {
    Stop-Hosted
    Say "Stopped. Postgres keeps running in Docker: docker compose -f deployments/dev/docker-compose.yml down"
    exit 0
}

New-Item -ItemType Directory -Force -Path $ToolDir, $LogDir | Out-Null
if (Test-Path $PidFile) {
    Say "A previous run is still recorded; stopping it first."
    Stop-Hosted
}
New-Item -ItemType File -Force -Path $PidFile | Out-Null

function Start-Hidden($file, [string[]]$argList, $logName) {
    $log = Join-Path $LogDir $logName
    $p = Start-Process -FilePath $file -ArgumentList $argList -RedirectStandardOutput $log `
        -RedirectStandardError "$log.err" -PassThru -WindowStyle Hidden
    Add-Content -Path $PidFile -Value $p.Id
    return $log
}

function Wait-ForMatch($logs, $pattern, $what, [int]$seconds = 45) {
    for ($i = 0; $i -lt $seconds * 2; $i++) {
        foreach ($l in $logs) {
            if (Test-Path $l) {
                $m = Select-String -Path $l -Pattern $pattern -ErrorAction SilentlyContinue | Select-Object -First 1
                if ($m) { return $m.Matches[0].Value }
            }
        }
        Start-Sleep -Milliseconds 500
    }
    Fail "$what did not start in $seconds s. Logs: $($logs -join ', ')"
}

function Get-LanIp {
    try {
        (Get-NetIPConfiguration | Where-Object { $_.IPv4DefaultGateway -ne $null -and $_.NetAdapter.Status -eq "Up" } |
            Select-Object -First 1).IPv4Address.IPAddress
    } catch { $null }
}

# ── 0. tools ──────────────────────────────────────────────────────────────
foreach ($t in "go", "docker") {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) { Fail "$t is not installed (see README section 4.1)." }
}
docker info *> $null
if ($LASTEXITCODE -ne 0) { Fail "Docker is not running. Start Docker Desktop, wait for it to say 'running', then try again." }

function Get-Tool($name, $url, [switch]$Zip) {
    $cmd = Get-Command $name -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $exe = Join-Path $ToolDir "$name.exe"
    if (Test-Path $exe) { return $exe }
    Say "Downloading $name (first run only) from $url"
    $ProgressPreference = "SilentlyContinue"
    if ($Zip) {
        $zipPath = Join-Path $ToolDir "$name.zip"
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $zipPath
        Expand-Archive -Force -Path $zipPath -DestinationPath $ToolDir
        Remove-Item $zipPath
    } else {
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $exe
    }
    if (-not (Test-Path $exe)) { Fail "Could not install $name. Install it yourself and run again." }
    return $exe
}

# ── 1. Postgres ───────────────────────────────────────────────────────────
Say "Starting Postgres in Docker"
docker compose -f $Compose up -d postgres
if ($LASTEXITCODE -ne 0) { Fail "docker compose could not start Postgres (is port 5432 free?)" }
$PgContainer = (docker compose -f $Compose ps -q postgres).Trim()
for ($i = 0; $i -lt 60; $i++) {
    if ((docker inspect -f "{{.State.Health.Status}}" $PgContainer) -eq "healthy") { break }
    Start-Sleep -Seconds 1
}

# up.ps1 calls psql. Without a local psql, run it inside the Postgres container:
# -h/-p are dropped (the container's own socket), -f is sent on stdin.
if (-not (Get-Command psql -CommandType Application -ErrorAction SilentlyContinue)) {
    $global:VyomanautPgContainer = $PgContainer
    function global:psql {
        $pass = [System.Collections.Generic.List[string]]::new()
        $file = $null
        for ($i = 0; $i -lt $args.Count; $i++) {
            $a = [string]$args[$i]
            if ($a -eq "-h" -or $a -eq "-p") { $i++ }
            elseif ($a -eq "-f") { $i++; $file = [string]$args[$i] }
            else { $pass.Add($a) }
        }
        $pass.Insert(0, "-q")
        if ($file) { Get-Content -Raw $file | docker exec -i $global:VyomanautPgContainer psql @pass }
        else { docker exec $global:VyomanautPgContainer psql @pass }
    }
    Say "No psql found; using the one inside the Postgres container"
}

# ── 2. relay ──────────────────────────────────────────────────────────────
Say "Building and starting the relay on port $RelayPort"
$env:CGO_ENABLED = "0"
$RelayExe = Join-Path $HostDir "relay.exe"
go build -o $RelayExe ./cmd/relay
if ($LASTEXITCODE -ne 0) { Fail "go build ./cmd/relay failed" }
$relayLog = Start-Hidden $RelayExe @("--listen", ":$RelayPort") "relay.log"

# ── 3. tunnels ────────────────────────────────────────────────────────────
$LanIp = Get-LanIp
if (-not $LanIp) { $LanIp = "127.0.0.1" }
foreach ($v in "VYOMANAUT_PUBLIC_URL", "VYOMANAUT_PUBLIC_RELAY", "VYOMANAUT_PUBLIC_CHAIN_API") {
    Remove-Item "Env:$v" -ErrorAction SilentlyContinue
}
if ($Local) {
    # No public address: the pages use whatever address each device reached
    # this laptop on, and the relay on that same host.
    $PublicUrl = "http://${LanIp}:$Port"
    $PublicRelay = "${LanIp}:$RelayPort"
} else {
    $Cloudflared = Get-Tool "cloudflared" "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe"
    $Bore = Get-Tool "bore" "https://github.com/ekzhang/bore/releases/download/v0.6.0/bore-v0.6.0-x86_64-pc-windows-msvc.zip" -Zip

    Say "Opening a public address for the relay (bore.pub)"
    $boreLog = Start-Hidden $Bore @("local", "$RelayPort", "--to", "bore.pub") "bore.log"
    $PublicRelay = Wait-ForMatch @($boreLog, "$boreLog.err") "bore\.pub:\d+" "The bore tunnel"

    Say "Opening a public HTTPS address for the coordinator (Cloudflare)"
    $cfLog = Start-Hidden $Cloudflared @("tunnel", "--no-autoupdate", "--url", "http://localhost:$Port") "cloudflared.log"
    $PublicUrl = Wait-ForMatch @($cfLog, "$cfLog.err") "https://[a-z0-9-]+\.trycloudflare\.com" "The Cloudflare tunnel"

    $env:VYOMANAUT_PUBLIC_URL = $PublicUrl
    $env:VYOMANAUT_PUBLIC_RELAY = $PublicRelay
    if ($ChainApi -or $env:VYOMANAUT_CHAIN_ENABLED) {
        $cf2Log = Start-Hidden $Cloudflared @("tunnel", "--no-autoupdate", "--url", "http://localhost:8090") "cloudflared-chain.log"
        $env:VYOMANAUT_PUBLIC_CHAIN_API = Wait-ForMatch @($cf2Log, "$cf2Log.err") "https://[a-z0-9-]+\.trycloudflare\.com" "The chain API tunnel"
    }
}

# ── 4. coordinator + local providers ──────────────────────────────────────
$env:VYOMANAUT_NETWORK_NAME = $Name
$env:VYOMANAUT_DOWNLOADS_DIR = Join-Path $RepoRoot "dist"
if (-not (Test-Path (Join-Path $env:VYOMANAUT_DOWNLOADS_DIR "vyomanaut-provider-android.apk"))) {
    Say "Note: dist\ has no app yet (scripts\release\build_release.ps1 builds it); downloads will come from GitHub releases."
}
Say "Starting the coordinator and $Providers local providers (up.ps1). This takes a few minutes."
& (Join-Path $ScriptDir "up.ps1") -Providers $Providers -StorageGB $StorageGB -AdvertiseHost $LanIp -Port $Port
if ($LASTEXITCODE) { Fail "up.ps1 failed; see its output above." }

$EnvFile = Join-Path $(if ($env:VYOMANAUT_DEMO_STATE_DIR) { $env:VYOMANAUT_DEMO_STATE_DIR } else { Join-Path $env:TEMP "vyomanaut-demo" }) "env.ps1"
. $EnvFile
$AdminKey = $env:ADMIN_API_KEY

# ── 5. screens ────────────────────────────────────────────────────────────
$ScreenUrl = "$PublicUrl/join/screen"
$DeskUrl = "http://localhost:$Port/join/desk#key=$AdminKey"
Start-Process $ScreenUrl
Start-Process $DeskUrl

Write-Host ""
Write-Host "  $Name is open." -ForegroundColor Green
Write-Host ""
Write-Host "  Join page (QR):  $PublicUrl/join"
Write-Host "  Big screen:      $ScreenUrl"
Write-Host "  Your join desk:  http://localhost:$Port/join/desk   (admin key below; already opened for you)"
Write-Host "  Relay:           $PublicRelay"
if ($env:VYOMANAUT_PUBLIC_CHAIN_API) { Write-Host "  Chain API:       $env:VYOMANAUT_PUBLIC_CHAIN_API" }
Write-Host ""
Write-Host "  Phones:   scan the QR, tap Download, then Open in the app."
Write-Host "  Laptops:  open $PublicUrl/join and paste the one line it shows."
Write-Host "  Codes:    appear on your join desk; read them out."
Write-Host ""
Write-Host "  Admin key: $AdminKey"
Write-Host "  Stop everything: pwsh scripts/demo/host.ps1 -Stop"
if (-not $Local) {
    Write-Host ""
    Write-Host "  The tunnel addresses change if you restart this script: start it once, before you share the QR." -ForegroundColor Yellow
}
