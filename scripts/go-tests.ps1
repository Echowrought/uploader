# Internal helper for check.ps1: consume Go's reporter rather than command wall time.
function Invoke-GoTests {
    param([string[]]$BuildArguments)

    # Drain native output before parsing so JSON processing cannot slow the tests.
    $lines = @(go test -json -count=1 @BuildArguments)
    $testExitCode = $LASTEXITCODE
    $events = @($lines | ForEach-Object { $_ | ConvertFrom-Json })
    $packages = @($events | Where-Object {
        $_.Package -and -not $_.Test -and $_.Action -in @('pass', 'fail', 'skip')
    })
    foreach ($package in $packages) {
        Write-Host ('{0} {1} {2:F3}s' -f $package.Action, $package.Package, [double]$package.Elapsed)
    }
    if ($testExitCode -ne 0) {
        $events | Where-Object { $_.Action -in @('output', 'build-output') } |
            ForEach-Object { Write-Host -NoNewline $_.Output }
        throw 'Go tests failed'
    }
    if ($packages.Count -eq 0) { throw 'Go returned no package test results' }

    # Parallel tests overlap, so individual test durations must not be summed.
    $budgetSeconds = 15
    $slow = @($packages | Where-Object { $_.Elapsed -ge $budgetSeconds })
    foreach ($package in $slow) {
        Write-Host ("Test budget exceeded: {0} reported {1:F3}s; must be under {2}s. Slowest top-level tests:" -f $package.Package, [double]$package.Elapsed, $budgetSeconds)
        $events | Where-Object {
            $_.Package -eq $package.Package -and $_.Test -and $_.Test -notmatch '/' -and
            $_.Action -in @('pass', 'fail', 'skip')
        } | Sort-Object Elapsed -Descending | Select-Object -First 5 |
            ForEach-Object { Write-Host ('  {0:F3}s {1}' -f [double]$_.Elapsed, $_.Test) }
    }
    if ($slow.Count -ne 0) { throw "Package test execution exceeded the $budgetSeconds-second budget" }
}
