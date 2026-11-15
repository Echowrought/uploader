$ErrorActionPreference = 'Stop'

. (Join-Path $PSScriptRoot 'go-tests.ps1')

$previousEncoding = [Console]::OutputEncoding
Push-Location (Join-Path $PSScriptRoot '..')
try {
    [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
    & (Join-Path $PSScriptRoot 'test-go-tests.ps1')
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
    if ($LASTEXITCODE -ne 0) { throw 'Workflow lint failed' }
    Invoke-GoTests -BuildArguments @('./...')
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    # Only the entry point changes with this tag; shared packages were checked above.
    Invoke-GoTests -BuildArguments @('-tags=console', '.')
    go vet -tags=console .
    if ($LASTEXITCODE -ne 0) { throw 'Console go vet failed' }
    Write-Host 'Workflow lint, tests, and go vet passed for both builds'
} finally {
    [Console]::OutputEncoding = $previousEncoding
    Pop-Location
}
