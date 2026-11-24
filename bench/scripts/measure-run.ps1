# measure-run.ps1 - run one DataX job and capture wall-clock duration, exit code
# and process memory, then write a JSON record next to the raw logs.
#
# Memory method
# -------------
# The engine runs as a native Windows process. While it runs, this script polls
# System.Diagnostics.Process at a fixed interval and records:
#   * WorkingSet64        - current resident set
#   * PeakWorkingSet64    - OS-maintained high-water mark of the working set
#   * PrivateMemorySize64 - current private (commit) bytes
# PeakWorkingSet64 is an OS counter, not a sampled value, so a short spike
# between two polls is still reported as long as it happened before the last
# successful poll. The process object (and therefore the counter) becomes
# unavailable once the process exits, hence the polling loop.
#
# Usage:
#   pwsh/powershell -File bench/scripts/measure-run.ps1 `
#       -Config bench/configs/mysql2mysql.json -Label s1-run1 -Iteration 1

[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Config,
    [Parameter(Mandatory = $true)][string]$Label,
    [Parameter(Mandatory = $true)][int]$Iteration,
    [string]$Scenario = "",
    [string]$Exe = "bin/datax.exe",
    [string]$OutDir = "bench/results/raw",
    [int]$PollMs = 25
)

$ErrorActionPreference = "Stop"

# Works on Windows PowerShell 5.1, which lacks [System.IO.Path]::GetRelativePath.
function Get-RelPath([string]$Base, [string]$Path) {
    $baseUri = New-Object System.Uri(($Base.TrimEnd("\") + "\"))
    $pathUri = New-Object System.Uri($Path)
    return [System.Uri]::UnescapeDataString($baseUri.MakeRelativeUri($pathUri).ToString()).Replace("\", "/")
}

$repoRoot = (Get-Location).Path
$exePath = if ([System.IO.Path]::IsPathRooted($Exe)) { $Exe } else { Join-Path $repoRoot $Exe }
$configPath = if ([System.IO.Path]::IsPathRooted($Config)) { $Config } else { Join-Path $repoRoot $Config }
$outPath = if ([System.IO.Path]::IsPathRooted($OutDir)) { $OutDir } else { Join-Path $repoRoot $OutDir }

if (-not (Test-Path -LiteralPath $exePath)) { throw "engine binary not found: $exePath" }
if (-not (Test-Path -LiteralPath $configPath)) { throw "job config not found: $configPath" }
New-Item -ItemType Directory -Force -Path $outPath | Out-Null

$exeHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $exePath).Hash
$exeInfo = Get-Item -LiteralPath $exePath
$stdoutLog = Join-Path $outPath "$Label-iter$Iteration.stdout.log"
$stderrLog = Join-Path $outPath "$Label-iter$Iteration.stderr.log"
$jsonPath = Join-Path $outPath "$Label-iter$Iteration.json"

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
$stopwatch = [System.Diagnostics.Stopwatch]::StartNew()
[void]$proc.Start()

$stdoutTask = $proc.StandardOutput.ReadToEndAsync()
$stderrTask = $proc.StandardError.ReadToEndAsync()

$peakWorkingSet = 0
$peakPrivate = 0
$samples = 0
$cpuSeconds = 0.0

while (-not $proc.HasExited) {
    try {
        $proc.Refresh()
        if (-not $proc.HasExited) {
            $samples++
            $cpuSeconds = $proc.TotalProcessorTime.TotalSeconds
            $curPrivate = $proc.PrivateMemorySize64
            if ($curPrivate -gt $peakPrivate) { $peakPrivate = $curPrivate }
            # PeakWorkingSet64 is a high-water mark maintained by the OS.
            $curPeak = $proc.PeakWorkingSet64
            if ($curPeak -gt $peakWorkingSet) { $peakWorkingSet = $curPeak }
        }
    } catch {
        # The process can exit between HasExited and the property reads.
    }
    Start-Sleep -Milliseconds $PollMs
}

$proc.WaitForExit()
$stopwatch.Stop()
$durationMs = [Math]::Round($stopwatch.Elapsed.TotalMilliseconds, 0)
$exitCode = $proc.ExitCode

$stdout = $stdoutTask.Result
$stderr = $stderrTask.Result
Set-Content -LiteralPath $stdoutLog -Value $stdout -Encoding UTF8
Set-Content -LiteralPath $stderrLog -Value $stderr -Encoding UTF8

$clean = [regex]::Replace($stdout, "$([char]27)\[[0-9;]*m", "")

# Final "Total <n> records, <x.yz>KB | Speed ... | Percentage 100.00%" line.
$totalRecords = $null
$totalBytes = $null
$totalBytesHuman = $null
$totalMatches = [regex]::Matches($clean, "Total (\d+) records, ([0-9.]+)([KMGT]?B)")
if ($totalMatches.Count -gt 0) {
    $m = $totalMatches[$totalMatches.Count - 1]
    $totalRecords = [int64]$m.Groups[1].Value
    $totalBytesHuman = $m.Groups[2].Value + $m.Groups[3].Value
    $multiplier = switch ($m.Groups[3].Value) {
        "B" { 1 }
        "KB" { 1024 }
        "MB" { 1048576 }
        "GB" { 1073741824 }
        "TB" { 1099511627776 }
    }
    $totalBytes = [int64]([double]$m.Groups[2].Value * $multiplier)
}

$recordSpeed = $null
$m = [regex]::Match($clean, "Record Write Speed\s+\{?.*?(\d+) rec/s")
if ($m.Success) { $recordSpeed = [int64]$m.Groups[1].Value }

$engineDuration = $null
$m = [regex]::Match($clean, "Job completed\s+\{.*?""duration"": ""([^""]+)""")
if ($m.Success) { $engineDuration = $m.Groups[1].Value }

$engineTotalCost = $null
$m = [regex]::Match($clean, "Job Total Cost\s+\{.*?""totalCosts"": ""([^""]+)""")
if ($m.Success) { $engineTotalCost = $m.Groups[1].Value }

$transformInputRecords = $null
$m = [regex]::Match($clean, "Transform Input Records\s+\{.*?""records"": (\d+)")
if ($m.Success) { $transformInputRecords = [int64]$m.Groups[1].Value }

$transformOutputRecords = $null
$m = [regex]::Match($clean, "Transform Output Records\s+\{.*?""records"": (\d+)")
if ($m.Success) { $transformOutputRecords = [int64]$m.Groups[1].Value }

$transformUsedTime = $null
$m = [regex]::Match($clean, "Transformer Used Time\s+\{.*?""usedTime"": ""([^""]+)""")
if ($m.Success) { $transformUsedTime = $m.Groups[1].Value }

$transformDirtyRecords = $null
$m = [regex]::Match($clean, "Transformer Dirty Records\s+\{.*?""records"": (\d+)")
if ($m.Success) { $transformDirtyRecords = [int64]$m.Groups[1].Value }

$engineError = $null
$m = [regex]::Match($clean, "DataX execution failed\s+\{.*?""error"": ""([^""]+)""")
if ($m.Success) { $engineError = $m.Groups[1].Value }

$rowsPerSecond = $null
if ($totalRecords -ne $null -and $durationMs -gt 0) {
    $rowsPerSecond = [Math]::Round($totalRecords / ($durationMs / 1000.0), 1)
}

$result = [ordered]@{
    label                    = $Label
    scenario                 = $Scenario
    iteration                = $Iteration
    config                   = (Get-RelPath $repoRoot $configPath)
    command                  = 'bin/datax.exe -job ' + (Get-RelPath $repoRoot $configPath)
    engine                   = [ordered]@{
        path          = (Get-RelPath $repoRoot $exePath)
        sizeBytes     = $exeInfo.Length
        sha256        = $exeHash
    }
    startedAt                = $startedAt.ToString("o")
    durationMs               = $durationMs
    durationSeconds          = [Math]::Round($durationMs / 1000.0, 3)
    exitCode                 = $exitCode
    peakWorkingSetBytes      = $peakWorkingSet
    peakWorkingSetMB         = [Math]::Round($peakWorkingSet / 1MB, 1)
    peakPrivateBytes         = $peakPrivate
    peakPrivateMB            = [Math]::Round($peakPrivate / 1MB, 1)
    memoryMethod             = "poll System.Diagnostics.Process every ${PollMs}ms; PeakWorkingSet64 is the OS high-water mark"
    memorySamples            = $samples
    cpuSeconds               = [Math]::Round($cpuSeconds, 2)
    engineTotalRecords       = $totalRecords
    engineTotalBytes         = $totalBytes
    engineTotalBytesHuman    = $totalBytesHuman
    engineRecordSpeed        = $recordSpeed
    engineDuration           = $engineDuration
    engineTotalCost          = $engineTotalCost
    rowsPerSecondWallClock   = $rowsPerSecond
    transformInputRecords    = $transformInputRecords
    transformOutputRecords   = $transformOutputRecords
    transformDirtyRecords    = $transformDirtyRecords
    transformUsedTime        = $transformUsedTime
    engineError              = $engineError
    stdoutLog                = (Get-RelPath $repoRoot $stdoutLog)
    stderrLog                = (Get-RelPath $repoRoot $stderrLog)
}

$result | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $jsonPath -Encoding UTF8

Write-Host ("{0}: exit={1} duration={2}s rows={3} rows/s={4} peakWS={5}MB" -f `
    $Label, $exitCode, [Math]::Round($durationMs / 1000.0, 3), $totalRecords, $rowsPerSecond, [Math]::Round($peakWorkingSet / 1MB, 1))

[pscustomobject]$result
