$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
try {
    go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7
    if ($LASTEXITCODE -ne 0) { throw 'Workflow lint failed' }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'go vet failed' }
    go test -tags=console ./...
    if ($LASTEXITCODE -ne 0) { throw 'Console tests failed' }
    go vet -tags=console ./...
    if ($LASTEXITCODE -ne 0) { throw 'Console go vet failed' }
    Write-Host 'Workflow lint, tests, and go vet passed for both builds'
} finally {
    Pop-Location
}
