# Builds everything people download to use Vyomanaut, into dist/:
#
#   vyomanaut-provider-android.apk          the phone app (provider program bundled inside)
#   provider-<os>-<arch>[.exe]              PC provider (Windows, macOS, Linux)
#   client-<os>-<arch>[.exe]                data owner CLI: register, upload, retrieve
#   vyomachain-<os>-<arch>[.exe]            wallet tool: new wallet, bind, deposit USDT, claim, refund
#   relay-<os>-<arch>[.exe]                 relay node, for people running their own coordinator
#   SHA256SUMS.txt
#
# Every Go binary is built with CGO off, so nobody needs a C toolchain or RocksDB. The PC providers
# for macOS and Linux use the pure-Go Badger storage engine (the one Windows and Android already
# use) through a Go overlay that swaps three storage files' build tags; no Go file is changed.
#
# Usage (PowerShell, from the repository root, on a machine with Go and Android Studio):
#   .\scripts\release\build_release.ps1
#   .\scripts\release\build_release.ps1 -SkipAndroid      # Go binaries only
#
# Then attach everything in dist\ to a GitHub release. The README links to
# https://github.com/vyomanaut-labs/Vyomanaut_For_ETH/releases/latest/download/<file name>,
# so keep these file names.
param(
    [switch]$SkipAndroid,
    [string]$JavaHome = 'C:\Program Files\Android\Android Studio1\jbr'
)
$ErrorActionPreference = 'Stop'

$Repo = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..'))
$Dist = Join-Path $Repo 'dist'
New-Item -ItemType Directory -Force -Path $Dist | Out-Null

function Fail([string]$message) {
    Write-Host "ERROR: $message" -ForegroundColor Red
    exit 1
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { Fail 'Go is not on PATH.' }

# ── the pure-Go storage overlay (same idea as android\scripts\build_android_provider.ps1) ──
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('vyomanaut-release-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
$storage = Join-Path $Repo 'internal\storage'
function New-TaggedCopy([string]$name, [string]$newFirstLine) {
    $source = Join-Path $storage $name
    $lines = [System.IO.File]::ReadAllLines($source)
    if ($lines.Length -eq 0 -or -not $lines[0].StartsWith('//go:build')) {
        Fail "Unexpected first line in $source (expected a //go:build line)."
    }
    $lines[0] = $newFirstLine
    $dest = Join-Path $tmp $name
    [System.IO.File]::WriteAllLines($dest, $lines, (New-Object System.Text.UTF8Encoding($false)))
    return @{ From = $source; To = $dest }
}
$swaps = @(
    (New-TaggedCopy 'engine_rocksdb.go' '//go:build (linux || darwin) && !vyopurego'),
    (New-TaggedCopy 'vlog.go'           '//go:build (linux || darwin) && !vyopurego'),
    (New-TaggedCopy 'engine_badger.go'  '//go:build windows || vyopurego')
)
$replace = @{}
foreach ($s in $swaps) { $replace[$s.From] = $s.To }
$overlay = Join-Path $tmp 'overlay.json'
@{ Replace = $replace } | ConvertTo-Json | Set-Content -Path $overlay -Encoding ASCII

$targets = @(
    @{ OS = 'windows'; Arch = 'amd64'; Name = 'windows-amd64'; Ext = '.exe' },
    @{ OS = 'linux';   Arch = 'amd64'; Name = 'linux-amd64';   Ext = '' },
    @{ OS = 'linux';   Arch = 'arm64'; Name = 'linux-arm64';   Ext = '' },
    @{ OS = 'darwin';  Arch = 'arm64'; Name = 'macos-arm64';   Ext = '' },
    @{ OS = 'darwin';  Arch = 'amd64'; Name = 'macos-amd64';   Ext = '' }
)
$commands = @('provider', 'client', 'vyomachain', 'relay')

$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
Push-Location $Repo
try {
    foreach ($t in $targets) {
        $env:GOOS = $t.OS; $env:GOARCH = $t.Arch; $env:CGO_ENABLED = '0'
        foreach ($cmd in $commands) {
            $out = Join-Path $Dist ("{0}-{1}{2}" -f $cmd, $t.Name, $t.Ext)
            Write-Host "building $cmd for $($t.Name)"
            & go build -overlay $overlay -tags vyopurego -trimpath -ldflags '-s -w' -o $out "./cmd/$cmd"
            if ($LASTEXITCODE -ne 0) { Fail "go build $cmd for $($t.Name) failed" }
        }
    }
}
finally {
    Pop-Location
    $env:GOOS = $saved.GOOS; $env:GOARCH = $saved.GOARCH; $env:CGO_ENABLED = $saved.CGO_ENABLED
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

if (-not $SkipAndroid) {
    $android = Join-Path $Repo 'android'
    Write-Host 'building the Android provider binary and the app'
    & (Join-Path $android 'scripts\build_android_provider.ps1')
    if ($LASTEXITCODE -ne 0) { Fail 'build_android_provider.ps1 failed' }
    $env:JAVA_HOME = $JavaHome
    Push-Location $android
    try {
        & .\gradlew.bat --quiet assembleDebug
        if ($LASTEXITCODE -ne 0) { Fail 'gradle assembleDebug failed' }
    }
    finally { Pop-Location }
    Copy-Item (Join-Path $android 'app\build\outputs\apk\debug\app-debug.apk') (Join-Path $Dist 'vyomanaut-provider-android.apk') -Force
}

$sums = Get-ChildItem $Dist -File | Where-Object { $_.Name -ne 'SHA256SUMS.txt' } | Sort-Object Name | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
}
$sums | Set-Content -Path (Join-Path $Dist 'SHA256SUMS.txt') -Encoding ASCII
Write-Host "Done. $((Get-ChildItem $Dist -File).Count) files in $Dist" -ForegroundColor Green
