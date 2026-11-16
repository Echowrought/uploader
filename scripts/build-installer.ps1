param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$')]
    [string]$Version,
    [string]$BuildDirectory = 'dist',
    [string]$OutputDirectory = 'dist',
    [string]$ISCCPath = $env:ISCC_PATH
)

$ErrorActionPreference = 'Stop'
$previousEncoding = [Console]::OutputEncoding
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    $build = (Resolve-Path -LiteralPath $BuildDirectory).Path
    foreach ($name in @('cwg-rpg-uploader.exe', 'README.md', 'VERIFYING.md', 'LICENSE', 'THIRD_PARTY_NOTICES.md')) {
        if (-not (Test-Path -LiteralPath (Join-Path $build $name) -PathType Leaf)) {
            throw "Missing build input $name; run build.ps1 first"
        }
    }
    if (-not $ISCCPath) { $ISCCPath = (Get-Command ISCC.exe -ErrorAction Stop).Source }
    $compiler = (Resolve-Path -LiteralPath $ISCCPath).Path
    New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
    $directory = (Resolve-Path -LiteralPath $OutputDirectory).Path
    $basename = "cwg-rpg-uploader-$Version-windows-amd64-setup"
    $executable = Join-Path $directory "$basename.exe"
    if (Test-Path -LiteralPath $executable) { Remove-Item -LiteralPath $executable -Force }
    try {
        & $compiler "/DAppVersion=$($Version.Substring(1))" "/DBuildDir=$build" "/O$directory" "/F$basename" `
            (Join-Path $PSScriptRoot '..\installer\cwg-rpg-uploader.iss')
        if ($LASTEXITCODE -ne 0) { throw 'Installer compilation failed' }
    } catch {
        if (Test-Path -LiteralPath $executable) { Remove-Item -LiteralPath $executable -Force }
        throw
    }
    Write-Host "Built $executable (not launched)"
} finally {
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
