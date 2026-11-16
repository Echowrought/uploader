param(
    [ValidatePattern('^(dev|v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$')]
    [string]$Version = 'dev'
)

# Compile/package on either host. Native Linux tests run separately through check.ps1.
$ErrorActionPreference = 'Stop'
$variables = @('GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED')
$previous = @{}
foreach ($name in $variables) { $previous[$name] = [Environment]::GetEnvironmentVariable($name, 'Process') }
Push-Location $PSScriptRoot
try {
    if (Test-Path -LiteralPath dist-linux) {
        Remove-Item -LiteralPath dist-linux -Recurse -Force
    }
    $env:GOOS = 'linux'
    $env:GOARCH = 'amd64'
    $env:GOAMD64 = 'v1'
    $env:CGO_ENABLED = '0'
    go vet ./cmd/uploader
    if ($LASTEXITCODE -ne 0) { throw 'Linux go vet failed' }
    New-Item -ItemType Directory -Path dist-linux | Out-Null
    $versionFlag = "-X cwgrpguploader/internal/buildinfo.Version=$Version"
    go build -trimpath "-ldflags=$versionFlag" -o dist-linux/cwg-rpg-uploader-console ./cmd/uploader
    if ($LASTEXITCODE -ne 0) { throw 'Linux console build failed' }
    Copy-Item README.md, CONFIGURATION.md, VERIFYING.md, LICENSE -Destination dist-linux
    & (Join-Path $PSScriptRoot 'scripts/notices.ps1') -OutputPath (Join-Path $PSScriptRoot 'dist-linux/THIRD_PARTY_NOTICES.md')
    Write-Host 'Built dist-linux/cwg-rpg-uploader-console (Linux amd64; no C runtime or tray required)'
} catch {
    if (Test-Path -LiteralPath dist-linux) {
        Remove-Item -LiteralPath dist-linux -Recurse -Force
    }
    throw
} finally {
    Pop-Location
    foreach ($name in $variables) { [Environment]::SetEnvironmentVariable($name, $previous[$name], 'Process') }
}
