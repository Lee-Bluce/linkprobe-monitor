param(
  [string]$Goos = "linux",
  [string]$Goarch = "arm64",
  [string]$Output = "build",
  [string]$Version = "1.0.0"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Write-Host "==> building web assets"
Push-Location (Join-Path $root "web")
try {
  if (-not (Test-Path "node_modules")) {
    npm.cmd install --no-audit --no-fund | Out-Null
  }
  npm.cmd run build
} finally {
  Pop-Location
}

$buildTime = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$outFile = Join-Path $root (Join-Path $Output "linkprobe-$Goos-$Goarch")
New-Item -ItemType Directory -Force -Path (Join-Path $root $Output) | Out-Null

Write-Host "==> go build $Goos/$Goarch"
$env:GOOS = $Goos
$env:GOARCH = $Goarch
$env:CGO_ENABLED = "0"
Push-Location $root
try {
  go build -trimpath -buildvcs=false `
    -ldflags "-s -w -X main.version=$Version -X main.buildTime=$buildTime" `
    -o $outFile ./cmd/linkprobe
} finally {
  Pop-Location
}

Write-Host "==> $outFile"
