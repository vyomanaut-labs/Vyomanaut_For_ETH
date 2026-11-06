# Join [[.NetworkName]] from this Windows computer.
#
#   irm [[.CoordinatorURL]]/join/install.ps1 | iex
#
# Downloads the Vyomanaut provider, registers this computer once (you need a
# phone number and the 6-digit code from the network's host), then starts
# lending storage. Run the same line again to start it again later.
# Everything lives in %USERPROFILE%\.vyomanaut-join\[[.Slug]]; delete that folder to leave.
& {
  $ErrorActionPreference = 'Stop'
  $ProgressPreference = 'SilentlyContinue'
  $Coord = [[ps .CoordinatorURL]]
  $Relay = [[ps .Relay]]
  $Mode = [[ps .Mode]]
  $Dir = Join-Path $env:USERPROFILE '.vyomanaut-join\[[.Slug]]'
  function Say($m) { Write-Host ''; Write-Host "[vyomanaut] $m" -ForegroundColor Cyan }

  New-Item -ItemType Directory -Force -Path $Dir | Out-Null
  $Bin = Join-Path $Dir 'provider.exe'
  if (-not (Test-Path $Bin)) {
    Say 'Downloading the provider for Windows ...'
    Invoke-WebRequest -UseBasicParsing -Uri "$Coord/join/download/provider-windows-amd64.exe" -OutFile "$Bin.part"
    Move-Item -Force "$Bin.part" $Bin
  }

  $Reg = Join-Path $Dir 'registration.json'
  if (-not (Test-Path $Reg)) {
    Say 'Joining [[.NetworkName]]'
    do {
      $Phone = (Read-Host 'Your phone number with country code (e.g. +919876500001)').Trim()
      $ok = $Phone -match '^\+[0-9]{8,15}$'
      if (-not $ok) { Write-Host '  Start with + and your country code, then the number.' }
    } until ($ok)
    $Gb = (Read-Host 'How many GB of this disk to lend (at least 10) [10]').Trim()
    if ($Gb -notmatch '^[0-9]+$') { $Gb = '10' }
    if ([int]$Gb -lt 10) { Write-Host '  The network needs at least 10 GB; using 10.'; $Gb = '10' }
    Say 'Now ask the host for your 6-digit code. It shows on their join desk.'
    & $Bin onboard "--microservice-url=$Coord" "--phone=$Phone" "--storage-gb=$Gb" "--data-dir=$Dir"
    if ($LASTEXITCODE -ne 0) { throw 'Registration did not finish. Run the same line again to retry.' }
  }

  $Gb = '10'
  $m = Select-String -Path $Reg -Pattern '"declared_storage_gb":(\d+)' -ErrorAction SilentlyContinue
  if ($m) { $Gb = $m.Matches[0].Groups[1].Value }

  Say "You're in. Lending $Gb GB to [[.NetworkName]]. Keep this window open; Ctrl+C stops it."
  & $Bin run "--mode=$Mode" "--microservice-url=$Coord" "--data-dir=$Dir" "--declared-storage-gb=$Gb" "--relay-addrs=$Relay"
}
