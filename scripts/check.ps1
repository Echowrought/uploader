$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'go-tests.ps1')

$previousEncoding = [Console]::OutputEncoding
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    & (Join-Path $PSScriptRoot 'test-go-tests.ps1')
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
    if ($LASTEXITCODE -ne 0) { throw 'Workflow lint failed' }
    # Keep fixture-heavy packages from competing for filesystem I/O.
    Invoke-GoTests -BuildArguments @('-p', '1', './...')
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    # Only Windows has a separate tray/console build selection.
    if ((go env GOOS) -eq 'windows') {
        Invoke-GoTests -BuildArguments @('-tags=console', './cmd/uploader')
        go vet -tags=console ./cmd/uploader
        if ($LASTEXITCODE -ne 0) { throw 'Console go vet failed' }
    }
    Write-Host 'Workflow lint, tests, and go vet passed for the native platform'
} finally {
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
