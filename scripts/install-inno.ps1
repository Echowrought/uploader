# Fetch a pinned compiler in portable mode; do not register/install it system-wide.
param(
    [Parameter(Mandatory = $true)]
    [string]$Destination
)

$ErrorActionPreference = 'Stop'
$Destination = [IO.Path]::GetFullPath($Destination)
New-Item -ItemType Directory -Force -Path $Destination | Out-Null
$download = Join-Path $Destination 'innosetup-7.1.1-x64.exe'
try {
    Invoke-WebRequest -UseBasicParsing `
        -Uri 'https://github.com/jrsoftware/issrc/releases/download/is-7_1_1/innosetup-7.1.1-x64.exe' `
        -OutFile $download
    $expected = '8a5a6711c3766a6e5fb74f280ba0e3551e719ce94538d901fdbd1f8f9920b3f0'
    if ((Get-FileHash -LiteralPath $download -Algorithm SHA256).Hash -ne $expected) {
        throw 'Inno Setup compiler download hash mismatch'
    }
    $process = Start-Process -FilePath $download -ArgumentList @(
        '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-',
        '/CURRENTUSER', '/PORTABLE=1', '/NOICONS', '/TASKS=""',
        ('/DIR="{0}"' -f $Destination)
    ) -Wait -PassThru
    if ($process.ExitCode -ne 0) { throw "Inno Setup compiler extraction failed: $($process.ExitCode)" }
    $compiler = Join-Path $Destination 'ISCC.exe'
    if (-not (Test-Path -LiteralPath $compiler -PathType Leaf)) { throw 'ISCC.exe was not extracted' }
    Write-Output $compiler
} finally {
    Remove-Item -LiteralPath $download -Force -ErrorAction SilentlyContinue
}
