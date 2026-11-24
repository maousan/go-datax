# run-bench.ps1 - reproduce the DataX reference baseline.
#
# Prerequisites (see bench/README.md):
#   1. Docker Desktop running on Windows.
#   2. bash bench/scripts/setup-containers.sh   (creates DBs + loads 1M rows)
#   3. python on PATH (only used to read the SQLite destination row count)
#
# Usage (from the repository root):
#   go build -o bin/datax.exe ./cmd/datax
#   powershell -ExecutionPolicy Bypass -File bench/scripts/run-bench.ps1
#
# Each scenario runs one warm-up (recorded but excluded from the medians) and
# three measured iterations. Results land in bench/results/raw/.

[CmdletBinding()]
param(
    [int]$Iterations = 3,
    [int]$Warmups = 1,
    [string[]]$Only = @(),
    [switch]$SkipBuild,
    [switch]$RegenerateData
)

$ErrorActionPreference = "Stop"

$scriptDir = $PSScriptRoot
$repoRoot = (Get-Location).Path
$rawDir = Join-Path $repoRoot "bench/results/raw"
New-Item -ItemType Directory -Force -Path $rawDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot "bench/data") | Out-Null

# ---------------------------------------------------------------- environment
$envInfo = [ordered]@{
    capturedAt       = (Get-Date).ToString("o")
    osCaption        = (Get-CimInstance Win32_OperatingSystem).Caption
    osVersion        = (Get-CimInstance Win32_OperatingSystem).Version
    cpu              = (Get-CimInstance Win32_Processor | Select-Object -First 1).Name
    physicalCores    = (Get-CimInstance Win32_Processor | Select-Object -First 1).NumberOfCores
    logicalCpus      = (Get-CimInstance Win32_Processor | Select-Object -First 1).NumberOfLogicalProcessors
    memoryBytes      = (Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory
    goVersion        = (& go version 2>&1 | Out-String).Trim()
    dockerServer     = (& docker version --format '{{.Server.Version}}' 2>&1 | Out-String).Trim()
    dockerClient     = (& docker version --format '{{.Client.Version}}' 2>&1 | Out-String).Trim()
    mysqlImage       = (& docker image inspect mysql:8.0 --format '{{.Id}}' 2>&1 | Out-String).Trim()
    postgresImage    = (& docker image inspect postgres:16-alpine --format '{{.Id}}' 2>&1 | Out-String).Trim()
    mysqlVersion     = (& docker exec -e MYSQL_PWD=benchpass go-datax-bench-mysql mysql -uroot -N -B -e "SELECT VERSION()" 2>$null | Out-String).Trim()
    postgresVersion  = (& docker exec go-datax-bench-pg psql -U postgres -d datax_bench -tAc "SHOW server_version" 2>$null | Out-String).Trim()
}
$envInfo | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $rawDir "env.json") -Encoding UTF8

# ---------------------------------------------------------------- preflight
function Assert-Container([string]$Name) {
    $running = (& docker inspect --format '{{.State.Running}}' $Name 2>$null | Out-String).Trim()
    if ($running -ne "true") {
        throw "container $Name is not running. Run: bash bench/scripts/setup-containers.sh"
    }
}

Assert-Container "go-datax-bench-mysql"
Assert-Container "go-datax-bench-pg"

$sourceRows = [int64]((& docker exec -e MYSQL_PWD=benchpass go-datax-bench-mysql mysql -uroot -N -B datax_bench -e "SELECT COUNT(*) FROM bench_source" 2>$null | Out-String).Trim())
if ($sourceRows -ne 1000000) {
    throw "bench_source has $sourceRows rows, expected 1000000. Re-run: bash bench/scripts/setup-containers.sh --reset"
}

if (-not $SkipBuild) {
    Write-Host "building bin/datax.exe ..."
    & go build -o bin/datax.exe ./cmd/datax
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
}

$dataFile = Join-Path $repoRoot "bench/data/rows-500k.txt"
if ($RegenerateData -or -not (Test-Path -LiteralPath $dataFile)) {
    Write-Host "generating 500,000-row text dataset ..."
    & go run ./bench/tools/genrows -rows 500000 -out bench/data/rows-500k.txt
    if ($LASTEXITCODE -ne 0) { throw "dataset generation failed" }
}

# ---------------------------------------------------------------- scenarios
$scenarios = @(
    [pscustomobject]@{ Key = "s4"; Name = "MySQL -> PostgreSQL";            Config = "bench/configs/mysql2postgres.json";        Rows = 1000000; Kind = "postgres"; Db = "" }
    [pscustomobject]@{ Key = "s2"; Name = "MySQL -> PostgreSQL + transform"; Config = "bench/configs/mysql2postgres-transform.json"; Rows = 1000000; Kind = "postgres"; Db = "" }
    [pscustomobject]@{ Key = "s3"; Name = "TXT file -> SQLite";             Config = "bench/configs/txt2sqlite.json";            Rows = 500000;  Kind = "sqlite";   Db = "bench/data/file2sqlite.db" }
    [pscustomobject]@{ Key = "s5"; Name = "MySQL -> SQLite (supplementary)"; Config = "bench/configs/mysql2sqlite.json";         Rows = 1000000; Kind = "sqlite";   Db = "bench/data/mysql2sqlite.db" }
)

if ($Only.Count -gt 0) {
    $scenarios = $scenarios | Where-Object { $Only -contains $_.Key }
}

function Reset-Destination([string]$Kind, [string]$Db) {
    switch ($Kind) {
        "mysql" {
            & docker exec -e MYSQL_PWD=benchpass go-datax-bench-mysql mysql -uroot -N -B datax_bench -e "TRUNCATE TABLE bench_dest" 2>$null | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "failed to truncate MySQL destination" }
        }
        "postgres" {
            & docker exec go-datax-bench-pg psql -v ON_ERROR_STOP=1 -U postgres -d datax_bench -tAc "TRUNCATE TABLE bench_dest" 2>$null | Out-Null
            if ($LASTEXITCODE -ne 0) { throw "failed to truncate PostgreSQL destination" }
        }
        "sqlite" {
            foreach ($suffix in @("", "-wal", "-shm")) {
                $f = Join-Path $repoRoot ($Db + $suffix)
                if (Test-Path -LiteralPath $f) { Remove-Item -LiteralPath $f -Force }
            }
        }
    }
}

function Get-DestinationRows([string]$Kind, [string]$Db) {
    switch ($Kind) {
        "mysql" {
            $out = (& docker exec -e MYSQL_PWD=benchpass go-datax-bench-mysql mysql -uroot -N -B datax_bench -e "SELECT COUNT(*) FROM bench_dest" 2>$null | Out-String).Trim()
            return [int64]$out
        }
        "postgres" {
            $out = (& docker exec go-datax-bench-pg psql -U postgres -d datax_bench -tAc "SELECT COUNT(*) FROM bench_dest" 2>$null | Out-String).Trim()
            return [int64]$out
        }
        "sqlite" {
            $out = (& python -I (Join-Path $scriptDir "verify-sqlite.py") (Join-Path $repoRoot $Db) | Out-String).Trim()
            return [int64]$out
        }
    }
}

$summary = New-Object System.Collections.Generic.List[object]

foreach ($sc in $scenarios) {
    Write-Host ""
    Write-Host ("=== {0} ({1}) ===" -f $sc.Name, $sc.Config)

    $runs = @()
    for ($w = 1; $w -le $Warmups; $w++) { $runs += [pscustomobject]@{ Label = "$($sc.Key)-warmup$w"; Iteration = 0 } }
    for ($i = 1; $i -le $Iterations; $i++) { $runs += [pscustomobject]@{ Label = "$($sc.Key)-run$i"; Iteration = $i } }

    foreach ($run in $runs) {
        Write-Host ("--- {0} ---" -f $run.Label)
        Reset-Destination $sc.Kind $sc.Db

        $res = & (Join-Path $scriptDir "measure-run.ps1") `
            -Config $sc.Config `
            -Label $run.Label `
            -Iteration $run.Iteration `
            -Scenario $sc.Key `
            -OutDir "bench/results/raw"

        $verified = -1
        try {
            $verified = Get-DestinationRows $sc.Kind $sc.Db
        } catch {
            Write-Warning "destination verification failed: $_"
        }
        if ($verified -ne $sc.Rows) {
            Write-Warning ("destination has {0} rows, expected {1}" -f $verified, $sc.Rows)
        }

        $entry = [pscustomobject]@{
            label              = $res.label
            scenario           = $sc.Name
            key                = $sc.Key
            iteration          = $res.iteration
            config             = $res.config
            exitCode           = $res.exitCode
            durationSeconds    = $res.durationSeconds
            rows               = $res.engineTotalRecords
            bytes              = $res.engineTotalBytes
            rowsPerSecond      = $res.rowsPerSecondWallClock
            engineRowsPerSecond= $res.engineRecordSpeed
            peakWorkingSetMB   = $res.peakWorkingSetMB
            peakPrivateMB      = $res.peakPrivateMB
            cpuSeconds         = $res.cpuSeconds
            transformUsedTime  = $res.transformUsedTime
            engineError        = $res.engineError
            verifiedRows       = $verified
            expectedRows       = $sc.Rows
            json               = "bench/results/raw/$($run.Label)-iter$($run.Iteration).json"
            stdoutLog          = $res.stdoutLog
        }
        $summary.Add($entry)
        Start-Sleep -Seconds 3
    }
}

$csvPath = Join-Path $rawDir "summary.csv"
$summary | Export-Csv -LiteralPath $csvPath -NoTypeInformation -Encoding UTF8
$summary | Format-Table -AutoSize | Out-String | Write-Host

Write-Host ""
Write-Host "summary written to bench/results/raw/summary.csv"
