param(
    [string]$OutputDirectory = 'profiles',
    [string]$Toolchain = ''
)

$ErrorActionPreference = 'Stop'
$previousEncoding = [Console]::OutputEncoding
$previousToolchain = $env:GOTOOLCHAIN
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    if ($Toolchain) { $env:GOTOOLCHAIN = $Toolchain }
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $executable = Join-Path $OutputDirectory 'cwg-rpg-profile.exe'
    # Keep symbols for pprof. Do not change the release build or clear dist.
    go build -trimpath '-ldflags=-H=windowsgui' -o $executable ./cmd/profile
    if ($LASTEXITCODE -ne 0) { throw 'Profiling build failed; do not use any previous executable at this path' }
    Write-Host "Built $executable (uploads disabled; not launched)"
} finally {
    $env:GOTOOLCHAIN = $previousToolchain
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
