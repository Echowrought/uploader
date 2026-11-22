# Regenerate committed Windows icon assets; normal application builds do not run this.
$ErrorActionPreference = 'Stop'
$previousEncoding = [Console]::OutputEncoding
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('cwg-icon-' + [Guid]::NewGuid().ToString('N'))
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    New-Item -ItemType Directory -Path $workspace | Out-Null
    $archive = Join-Path $workspace 'resvg.zip'
    Invoke-WebRequest -UseBasicParsing `
        -Uri 'https://github.com/linebender/resvg/releases/download/v0.50.0/resvg-win-x86_64.zip' `
        -OutFile $archive
    $expected = 'e4817e40ac63d3eb5c9e5762526cf6cc91f0cc108de38cd09dd60be765184efd'
    if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $expected) {
        throw 'SVG renderer download hash mismatch'
    }
    Expand-Archive -LiteralPath $archive -DestinationPath $workspace
    $renderer = Join-Path $workspace 'resvg.exe'
    $sizes = @(16, 20, 24, 32, 40, 48, 64, 128, 256)
    $images = [Collections.Generic.List[byte[]]]::new()
    foreach ($size in $sizes) {
        $png = Join-Path $workspace "$size.png"
        & $renderer --width $size --height $size assets/icon.svg $png
        if ($LASTEXITCODE -ne 0) { throw "SVG rendering failed at $size pixels" }
        $images.Add([IO.File]::ReadAllBytes($png))
    }

    # Windows 10+ accepts PNG-compressed entries in ICO files.
    $icon = Join-Path $workspace 'icon.ico'
    $writer = [IO.BinaryWriter]::new([IO.File]::Create($icon))
    try {
        $writer.Write([uint16]0)
        $writer.Write([uint16]1)
        $writer.Write([uint16]$sizes.Count)
        $offset = [uint32](6 + 16 * $sizes.Count)
        for ($i = 0; $i -lt $sizes.Count; $i++) {
            $dimension = if ($sizes[$i] -eq 256) { [byte]0 } else { [byte]$sizes[$i] }
            $writer.Write($dimension)
            $writer.Write($dimension)
            $writer.Write([byte]0)
            $writer.Write([byte]0)
            $writer.Write([uint16]1)
            $writer.Write([uint16]32)
            $writer.Write([uint32]$images[$i].Length)
            $writer.Write($offset)
            $offset += $images[$i].Length
        }
        foreach ($image in $images) { $writer.Write($image) }
    } finally {
        $writer.Dispose()
    }
    $resource = Join-Path $workspace 'icon_windows_amd64.syso'
    go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico $icon -manifest assets/windows.manifest -o $resource
    if ($LASTEXITCODE -ne 0) { throw 'Windows icon and manifest resource generation failed' }
    Copy-Item -LiteralPath $icon -Destination internal/windows/icon.ico
    Copy-Item -LiteralPath $resource -Destination internal/windows/icon_windows_amd64.syso
    Write-Host 'Regenerated internal/windows/icon.ico and icon_windows_amd64.syso'
} finally {
    Remove-Item -LiteralPath $workspace -Recurse -Force -ErrorAction SilentlyContinue
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
