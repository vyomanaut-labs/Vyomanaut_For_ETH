# Builds the Vyomanaut Go provider for Android (arm64) and puts it where the app expects it.
# Run this on the Windows laptop that has Go installed. It builds from this repository (two folders up)
# and does not modify any Go file:
# the build tags of three storage files are swapped through a Go overlay so Android uses the
# pure-Go Badger engine (the RocksDB engine and its value-log store need C and are excluded).
#
# Usage (PowerShell, from the android folder):
#   .\scripts\build_android_provider.ps1
#   .\scripts\build_android_provider.ps1 -RepoPath 'D:\code\Vyomanaut_V2'
param(
    [string]$RepoPath = (Join-Path $PSScriptRoot '..\..'),
    [string]$OutFile  = (Join-Path $PSScriptRoot '..\app\src\main\jniLibs\arm64-v8a\libprovider.so')
)
$ErrorActionPreference = 'Stop'

function Fail([string]$message) {
    Write-Host "ERROR: $message" -ForegroundColor Red
    exit 1
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail 'Go is not on PATH. Install the Go version named in the repo go.mod and try again.'
}
$OutFile = [System.IO.Path]::GetFullPath($OutFile)
$RepoPath = [System.IO.Path]::GetFullPath($RepoPath)
if (-not (Test-Path (Join-Path $RepoPath 'cmd\provider\main.go'))) {
    Fail "Provider source not found at $RepoPath. Pass -RepoPath with the Vyomanaut repository folder."
}

$rocks  = Join-Path $RepoPath 'internal\storage\engine_rocksdb.go'
$vlog   = Join-Path $RepoPath 'internal\storage\vlog.go'
$badger = Join-Path $RepoPath 'internal\storage\engine_badger.go'
foreach ($file in @($rocks, $vlog, $badger)) {
    if (-not (Test-Path $file)) { Fail "Expected file is missing: $file" }
}

$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ('vyomanaut-android-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null

function New-TaggedCopy([string]$source, [string]$newFirstLine, [string]$destName) {
    $lines = [System.IO.File]::ReadAllLines($source)
    if ($lines.Length -eq 0 -or -not $lines[0].StartsWith('//go:build')) {
        Fail "Unexpected first line in $source (expected a //go:build line). The repo may have changed."
    }
    $lines[0] = $newFirstLine
    $dest = Join-Path $tmp $destName
    [System.IO.File]::WriteAllLines($dest, $lines, (New-Object System.Text.UTF8Encoding($false)))
    return $dest
}

# GOOS=linux with CGO off gives a static binary that Android can run. The RocksDB engine needs C,
# so it is excluded and the pure-Go Badger engine (already used on Windows) is selected instead.
$rocksCopy  = New-TaggedCopy $rocks  '//go:build (linux && !vyoandroid) || darwin' 'engine_rocksdb.go'
$vlogCopy   = New-TaggedCopy $vlog   '//go:build (linux && !vyoandroid) || darwin' 'vlog.go'
$badgerCopy = New-TaggedCopy $badger '//go:build windows || vyoandroid'            'engine_badger.go'

$overlayPath = Join-Path $tmp 'overlay.json'
@{ Replace = @{ $rocks = $rocksCopy; $vlog = $vlogCopy; $badger = $badgerCopy } } |
    ConvertTo-Json | Set-Content -Path $overlayPath -Encoding ASCII

New-Item -ItemType Directory -Force -Path (Split-Path $OutFile -Parent) | Out-Null

$previous = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
$env:GOOS = 'linux'
$env:GOARCH = 'arm64'
$env:CGO_ENABLED = '0'
Push-Location $RepoPath
try {
    Write-Host "Building provider for Android arm64 from $RepoPath ..."
    & go build -overlay $overlayPath -tags vyoandroid -trimpath -ldflags '-s -w' -o $OutFile ./cmd/provider
    if ($LASTEXITCODE -ne 0) {
        Fail 'go build failed. See the errors above.'
    }
}
finally {
    Pop-Location
    $env:GOOS = $previous.GOOS
    $env:GOARCH = $previous.GOARCH
    $env:CGO_ENABLED = $previous.CGO_ENABLED
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

$sizeMb = [math]::Round((Get-Item $OutFile).Length / 1MB, 1)
Write-Host "Built $OutFile ($sizeMb MB)" -ForegroundColor Green
Write-Host 'Next: in Android Studio choose Build > Rebuild Project, then Run the app on the phone.'
