$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'go-tests.ps1')

function Test-GoTestReporting {
    $cases = @(
        @{
            Name = 'Parallel test and package times are not summed'; BuildArguments = @('./...'); Fails = $false; Contains = 'example/b'
            Report = @(
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestFirst'; Elapsed = 6 },
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestSecond'; Elapsed = 6 },
                @{ Action = 'pass'; Package = 'example/a'; Elapsed = 9.999 },
                @{ Action = 'pass'; Package = 'example/b'; Elapsed = 9 }
            )
        },
        @{
            Name = 'Exactly ten seconds fails'; BuildArguments = @('./...'); Fails = $true; Contains = 'TestSlow'
            Report = @(
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestSlow'; Elapsed = 10 },
                @{ Action = 'pass'; Package = 'example/a'; Elapsed = 10 }
            )
        },
        @{
            Name = 'Slow diagnostics exclude nested tests'; BuildArguments = @('./...'); Fails = $true; Contains = 'TestSlow'
            Report = @(
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestSlow/child'; Elapsed = 8 },
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestSlow'; Elapsed = 8.1 },
                @{ Action = 'pass'; Package = 'example/a'; Test = 'TestOther'; Elapsed = 3 },
                @{ Action = 'pass'; Package = 'example/a'; Elapsed = 12 }
            )
        },
        @{
            Name = 'Console entry point is checked'; BuildArguments = @('-tags=console', '.'); Fails = $true; Contains = 'example/console'
            Report = @(@{ Action = 'pass'; Package = 'example/console'; Elapsed = 11 })
        },
        @{
            Name = 'Ordinary test failure is preserved'; BuildArguments = @('./...'); Fails = $true; Contains = 'original failure details'; Exit = 1
            Report = @(
                @{ Action = 'output'; Package = 'example/a'; Output = "original failure details`n" },
                @{ Action = 'fail'; Package = 'example/a'; Elapsed = 1 }
            )
        },
        @{
            Name = 'Missing reports fail closed'; BuildArguments = @('./...'); Fails = $true; Contains = 'no package test results'; Report = @()
        }
    )
    foreach ($fixture in $cases) {
        $calls = [Collections.Generic.List[string]]::new()
        $messages = [Collections.Generic.List[string]]::new()

        $failure = $null
        try {
            Invoke-GoTests -BuildArguments $fixture.BuildArguments 6>&1 |
                ForEach-Object { $messages.Add($_.ToString()) }
        } catch { $failure = $_ }

        $output = ($messages -join "`n") + "`n" + $failure
        if (($null -ne $failure) -ne $fixture.Fails -or -not $output.Contains($fixture.Contains)) {
            throw "Reporter check failed: $($fixture.Name)`n$output"
        }
        if ($output.Contains('TestSlow/child')) { throw 'Nested test was double-counted in diagnostics' }
        $expectedCall = 'test -json -count=1 ' + ($fixture.BuildArguments -join ' ')
        if ($calls.Count -ne 1 -or $calls[0] -ne $expectedCall) {
            throw "Incorrect test command: $calls"
        }
    }
}

# A dumb Go stub emits configured reporter events; the checks never sleep or
# launch subprocesses. This function exists only in this script's local scope.
function go {
    $calls.Add(($args -join ' '))
    $fixture.Report | ForEach-Object { $_ | ConvertTo-Json -Compress }
    $global:LASTEXITCODE = [int]$fixture.Exit
}

$previousExitCode = $global:LASTEXITCODE
try {
    Test-GoTestReporting
    Write-Host 'Go test reporter checks passed'
} finally {
    $global:LASTEXITCODE = $previousExitCode
}
