param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$')]
    [string]$Tag,
    [ValidatePattern('^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$')]
    [string]$PreviousTag,
    [string]$RepositoryUrl = 'https://github.com/cwg-rpg-community/uploader'
)

$ErrorActionPreference = 'Stop'
$RepositoryUrl = $RepositoryUrl.TrimEnd('/')
$notesDir = Join-Path $PSScriptRoot '../.github/release-notes'
$changesPath = Join-Path $notesDir "$Tag.md"
$changelog = if ($PreviousTag) { "$RepositoryUrl/compare/$PreviousTag...$Tag" } else { "$RepositoryUrl/commits/$Tag" }
$changes = if (Test-Path -LiteralPath $changesPath -PathType Leaf) {
    (Get-Content -Raw -LiteralPath $changesPath -Encoding utf8).Trim()
} else {
    'Add a short summary of the user-facing changes before publishing. See the full changelog below.'
}

$template = Get-Content -Raw -LiteralPath (Join-Path $notesDir 'template.md') -Encoding utf8
$template.Replace('{{REPOSITORY}}', $RepositoryUrl).
    Replace('{{DOWNLOAD}}', "$RepositoryUrl/releases/download/$Tag").
    Replace('{{TAG}}', $Tag).
    Replace('{{CHANGELOG}}', $changelog).
    Replace('{{CHANGES}}', $changes).TrimEnd()
