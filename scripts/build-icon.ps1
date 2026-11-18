# Regenerate committed Windows icon assets; normal application builds do not run this.
$ErrorActionPreference = 'Stop'
$previousEncoding = [Console]::OutputEncoding
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('cwg-icon-' + [Guid]::NewGuid().ToString('N'))
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    New-Item -ItemType Directory -Path $workspace | Out-Null
    Add-Type -AssemblyName System.Drawing
    $source = [Drawing.Image]::FromFile((Resolve-Path assets/icon.png))
    $sizes = @(16, 20, 24, 32, 40, 48, 64, 128, 256)
    $images = [Collections.Generic.List[byte[]]]::new()
    try {
        foreach ($size in $sizes) {
            $bitmap = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
            $graphics = [Drawing.Graphics]::FromImage($bitmap)
            try {
                $graphics.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $graphics.PixelOffsetMode = [Drawing.Drawing2D.PixelOffsetMode]::HighQuality
                $graphics.CompositingQuality = [Drawing.Drawing2D.CompositingQuality]::HighQuality
                $graphics.DrawImage($source, 0, 0, $size, $size)
                $stream = [IO.MemoryStream]::new()
                $bitmap.Save($stream, [Drawing.Imaging.ImageFormat]::Png)
                $images.Add($stream.ToArray())
            } finally {
                $graphics.Dispose()
                $bitmap.Dispose()
            }
        }
    } finally {
        $source.Dispose()
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
    go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico $icon -o $resource
    if ($LASTEXITCODE -ne 0) { throw 'Windows icon resource generation failed' }
    Copy-Item -LiteralPath $icon -Destination internal/windows/icon.ico
    Copy-Item -LiteralPath $resource -Destination internal/windows/icon_windows_amd64.syso
    Write-Host 'Regenerated internal/windows/icon.ico and icon_windows_amd64.syso'
} finally {
    Remove-Item -LiteralPath $workspace -Recurse -Force -ErrorAction SilentlyContinue
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
