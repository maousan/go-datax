# measure-recovery.ps1 - measure failure recovery time for a crashed batch run.
#
# Method
# ------
#   1. truncate the destination (outside the measurement);
#   2. start bin/datax.exe normally;
#   3. after -KillAfterSeconds, force-kill the process (no cleanup, like a crash);
#   4. count the rows that were committed before the kill;
#   5. truncate the destination (the operator cleanup a retry would need);
#   6. re-run the same job through measure-run.ps1 and wait for success.
#
# Recovery time is wall clock from the kill to the successful retry exit. It
# includes destination cleanup, process startup, the transfer and shutdown.
#
# Usage (from the repository root):
#   powershell -ExecutionPolicy Bypass -File bench/scripts/measure-recovery.ps1

[CmdletBinding()]
param(
    [int]$Iterations = 3,
    [int]$KillAfterSeconds = 10,
    [string]$Config = "bench/configs/mysql2postgres.json",
    [string]$Exe = "bin/datax.exe",
    [string]$OutDir = "bench/results/raw"
)

$ErrorActionPreference = "Stop"
$scriptDir = $PSScriptRoot
$repoRoot = (Get-Location).Path
$outPath = if ([System.IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $repoRoot $OutDir }
New-Item -ItemType Directory -Force -Path $outPath | Out-Null

$exePath = if ([System.IO.Path]::IsPathRooted($Exe)) { $Exe } else { Join-Path $repoRoot $Exe }
$configPath = if ([System.IO.Path]::IsPathRooted($Config)) { $Config } else { Join-Path $repoRoot $Config }
if (-not (Test-Path -LiteralPath $exePath)) { throw "engine binary not found: $exePath" }

function Reset-Postgres {
    & docker exec go-datax-bench-pg psql -v ON_ERROR_STOP=1 -U postgres -d datax_bench -tAc "TRUNCATE TABLE bench_dest" 2>$null | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "failed to truncate PostgreSQL destination" }
}

function Get-PostgresRows {
    $out = (& docker exec go-datax-bench-pg psql -U postgres -d datax_bench -tAc "SELECT COUNT(*) FROM bench_dest" 2>$null | Out-String).Trim()
    return [int64]$out
}

$summary = New-Object System.Collections.Generic.List[object]

for ($i = 1; $i -le $Iterations; $i++) {
    Write-Host ("--- recovery run {0}: kill after {1}s ---" -f $i, $KillAfterSeconds)
    Reset-Postgres

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $exePath
    $psi.Arguments = '-job "' + $configPath + '"'
    $psi.WorkingDirectory = $repoRoot
    $psi.UseShellExecute = $false
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.CreateNoWindow = $true

    $proc = New-Object System.Diagnostics.Process
    $proc.StartInfo = $psi
    $startedAt = Get-Date
    [void]$proc.Start()
    $stdoutTask = $proc.StandardOutput.ReadToEndAsync()
    $stderrTask = $proc.StandardError.ReadToEndAsync()

    $deadline = $startedAt.AddSeconds($KillAfterSeconds)
    while (-not $proc.HasExited -and (Get-Date) -lt $deadline) {
        Start-Sleep -Milliseconds 100
    }

    $exitedBeforeKill = $proc.HasExited
    if (-not $exitedBeforeKill) {
        Stop-Process -Id $proc.Id -Force
    }
    $killAt = Get-Date
    $proc.WaitForExit()

    $partialRows = Get-PostgresRows

    # Drain the killed process' redirected streams so the handle is released.
    $killedStdout = Join-Path $outPath "s6-recovery-kill$i.stdout.log"
    $killedStderr = Join-Path $outPath "s6-recovery-kill$i.stderr.log"
    Set-Content -LiteralPath $killedStdout -Value $stdoutTask.Result -Encoding UTF8
    Set-Content -LiteralPath $killedStderr -Value $stderrTask.Result -Encoding UTF8

    $runDurationBeforeKill = [Math]::Round(((Get-Date) - $startedAt).TotalSeconds, 3)

    Reset-Postgres
    $retry = & (Join-Path $scriptDir "measure-run.ps1") `
        -Config $Config `
        -Label "s6-recovery-run$i" `
        -Iteration $i `
        -Scenario "s6" `
        -OutDir $OutDir

    $recoverySeconds = [Math]::Round(((Get-Date) - $killAt).TotalSeconds, 3)
    $verified = Get-PostgresRows

    $entry = [pscustomobject]@{
        iteration                 = $i
        exitBeforeKill            = $exitedBeforeKill
        runSecondsBeforeKill      = $runDurationBeforeKill
        partialRowsCommitted      = $partialRows
        retryExitCode             = $retry.exitCode
        retryDurationSeconds      = $retry.durationSeconds
        retryRows                 = $retry.engineTotalRecords
        retryPeakWorkingSetMB     = $retry.peakWorkingSetMB
        recoverySeconds           = $recoverySeconds
        verifiedRows              = $verified
        expectedRows              = 1000000
        retryJson                 = "bench/results/raw/s6-recovery-run$i-iter$i.json"
    }
    $summary.Add($entry)
    Write-Host ("kill at {0}s, {1} rows committed, retry ok in {2}s, recovery {3}s, peak {4}MB" -f `
        $runDurationBeforeKill, $partialRows, $retry.durationSeconds, $recoverySeconds, $retry.peakWorkingSetMB)

    Start-Sleep -Seconds 3
}

$csvPath = Join-Path $outPath "recovery-summary.csv"
$summary | Export-Csv -LiteralPath $csvPath -NoTypeInformation -Encoding UTF8
$summary | Format-Table -AutoSize | Out-String | Write-Host
Write-Host "recovery summary written to bench/results/raw/recovery-summary.csv"
