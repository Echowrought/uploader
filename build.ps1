param(
    [ValidatePattern('^(dev|v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$')]
    [string]$Version = 'dev'
)

$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    if (Test-Path -LiteralPath dist) {
        Remove-Item -LiteralPath dist -Recurse -Force
    }
    & (Join-Path $PSScriptRoot 'scripts\check.ps1')
    New-Item -ItemType Directory -Force -Path dist | Out-Null
    $versionFlag = "-X cwgrpguploader/internal/buildinfo.Version=$Version"
    go build -trimpath "-ldflags=-H=windowsgui $versionFlag" -o dist/cwg-rpg-uploader.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Tray build failed' }
    go build -tags=console -trimpath "-ldflags=$versionFlag" -o dist/cwg-rpg-uploader-console.exe .
    if ($LASTEXITCODE -ne 0) { throw 'Console build failed' }
    & (Join-Path $PSScriptRoot 'scripts\notices.ps1')
    Copy-Item README.md, VERIFYING.md, LICENSE, THIRD_PARTY_NOTICES.md -Destination dist
    Write-Host 'Built dist/cwg-rpg-uploader.exe and dist/cwg-rpg-uploader-console.exe'
} catch {
    if (Test-Path -LiteralPath dist) {
        Remove-Item -LiteralPath dist -Recurse -Force
    }
    throw
} finally {
    Pop-Location
}
