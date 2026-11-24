# DataX reference baseline

This directory holds the reproducible benchmark harness for the DataX area and
the reference numbers that the new Pipeline platform is compared against
(issue [#3](https://github.com/maousan/go-datax/issues/3)).

The DataX area is frozen, so the harness only builds the existing engine and
runs job JSON files. It does not change engine code, plugins or DataX behaviour.

- Dataset: 1,000,000 deterministic rows with 12 mixed-type columns.
- Source/targets: MySQL 8.0 and PostgreSQL 16 containers, plus a local
  comma-separated TXT file and a SQLite file.
- Each scenario runs one warm-up plus three measured iterations.
- Raw per-run records live in `results/raw/`; the report is
  `results/2026-11-24-baseline.md`.

## Hardware and software used

| Item | Value |
|---|---|
| CPU | Intel Core i5-9600KF, 6 cores / 6 threads, 3.70 GHz |
| RAM | 48 GiB (51,446,976,512 bytes) |
| OS | Windows 11 专业版 10.0.26200 |
| Disk | NVMe SSD (D:), 18 GB free during the run |
| Go | go1.26.0 windows/amd64 |
| Engine | `v1.4.0`, built from this branch with `go build -o bin/datax.exe ./cmd/datax` |
| Docker | 29.1.2 |
| MySQL | `mysql:8.0` (server 8.0.46) |
| PostgreSQL | `postgres:16-alpine` (server 16.15) |
| Python | 3.12.10 (used only to read the SQLite row count) |

Container tuning: 4 GiB memory and 6 CPUs each; MySQL runs with
`--local-infile=1`, `--innodb-buffer-pool-size=1G`, `--max-connections=300`
and default durability (`innodb_flush_log_at_trx_commit=1`, `sync_binlog=1`).
`LOAD DATA` during setup disables the binary log for that session only.
The benchmark jobs themselves do not touch the binary-log settings.

## Dataset

`bench/tools/genrows` writes a deterministic CSV. Row `n` is a pure function
of `n`, so the same bytes are produced on any machine:

```
id,name,email,age,score,status,city,payload,created_at,updated_at,flag,note
1,user_00000001,user00000001@example.com,19,0.01,active,Shanghai,payload-0000000001-xxxx...,2025-01-01 00:00:01,2025-01-01 00:00:07,1,note-1
```

| Property | Value |
|---|---|
| Rows (MySQL scenarios) | 1,000,000 |
| Generated CSV (`rows-1m.txt`) | 172,837,467 bytes |
| MySQL table `bench_source` | 172.7 MiB (data + index) |
| Rows / bytes reported by the engine | 1,000,000 rows, 153,658,327 bytes (146.54 MiB) |
| Rows (file scenario) | 500,000 |
| Generated CSV (`rows-500k.txt`) | 86,363,185 bytes |
| Bytes reported by the engine (file scenario) | 500,000 rows, 78,381,056 bytes (74.75 MiB) |

`note` is set to `\N` for every seventh row. That is the DataX
`txtfilereader` default `nullFormat` and is interpreted as SQL `NULL` by
MySQL `LOAD DATA`, so the MySQL and TXT datasets carry the same nulls.

The MySQL source is loaded with `LOAD DATA LOCAL INFILE` from the same CSV
rather than an `INSERT ... SELECT`, because a 1M-row insert on this machine's
Docker volume took over ten minutes; the CSV load is dramatically faster and
keeps the two scenarios byte-identical.

## Reproduce

Run from the repository root (all paths in the job configs are relative to
it):

```bash
# 1. Start isolated databases and load the 1M-row dataset.
bash bench/scripts/setup-containers.sh

# 2. Build the engine and run all scenarios (1 warm-up + 3 iterations each).
powershell -ExecutionPolicy Bypass -File bench/scripts/run-bench.ps1

# Optional: run selected scenarios.
powershell -ExecutionPolicy Bypass -File bench/scripts/run-bench.ps1 -Only s4,s3

# Optional: regenerate the 500k TXT file.
powershell -ExecutionPolicy Bypass -File bench/scripts/run-bench.ps1 -RegenerateData
```

The containers use host ports **13306** (MySQL) and **15432** (PostgreSQL),
and the names `go-datax-bench-mysql` / `go-datax-bench-pg`. They never share a
container, volume or port with an existing database on the host.

Tear down only what the harness created:

```bash
bash bench/scripts/teardown-containers.sh
```

`setup-containers.sh --reset` deletes and recreates both containers and
volumes. `--no-schema` starts them without reloading data.

## Scenarios

| ID | Scenario | Config | Rows | Channel |
|---|---|---|---|---|
| S1 | MySQL → MySQL | `configs/mysql2mysql.json` | 1,000,000 | 4 |
| S2 | MySQL → MySQL + `dx_substr`/`dx_replace` | `configs/mysql2mysql-transform.json` | 1,000,000 | 4 |
| S3 | TXT file → SQLite | `configs/txt2sqlite.json` | 500,000 | 1 |
| S4 | MySQL → PostgreSQL | `configs/mysql2postgres.json` | 1,000,000 | 4 |
| S2' | MySQL → PostgreSQL + `dx_substr`/`dx_replace` | `configs/mysql2postgres-transform.json` | 1,000,000 | 4 |
| S5 | MySQL → SQLite (supplementary) | `configs/mysql2sqlite.json` | 1,000,000 | 1 |

**S1 and S2 cannot run on this engine.** The `mysqlwriter` plugin hard-codes the
Java-only JDBC parameters `yearIsDateType=false` and
`zeroDateTimeBehavior=convertToNull` into the Go MySQL DSN
(`plugins/writer/mysqlwriter/mysql_writer.go`). The Go driver sends every
unrecognised DSN key as a session variable, so connecting fails with
`Error 1193 (HY000): Unknown system variable 'yearIsDateType'` (or
`'zeroDateTimeBehavior'`, depending on map iteration order). Because the
ticket forbids changing engine behaviour, MySQL→MySQL is recorded as blocked
instead of being patched. S2' keeps the exact same source, transformers and
row count as S2 but uses the PostgreSQL writer, so transformer overhead is
still measurable; S4 is its no-transformer counterpart. S5 adds a second
database sink that works without an engine change.

`configs/mysql2mysql*.json` are kept in the tree as the ready-to-run configs
for a future engine fix.

S3 uses one file and therefore one channel: `txtfilereader` splits work by
file, so a single file cannot feed four channels. S5 uses one channel because
SQLite is single-writer; four concurrent writer tasks against one SQLite file
would serialise or return `SQLITE_BUSY`.

## How a run is measured

`scripts/measure-run.ps1` starts `bin/datax.exe -job <config>` directly (no
shell wrapper) and:

- measures wall-clock duration with a `Stopwatch` around the process;
- polls `System.Diagnostics.Process` every 25 ms for
  `WorkingSet64`, `PrivateMemorySize64` and `TotalProcessorTime`;
- reads `PeakWorkingSet64` on every poll. This is an OS-maintained high-water
  mark, so a spike between two polls is not lost as long as it happened before
  the last successful poll (the counter disappears when the process exits);
- parses the engine's final log lines for `Total N records, X MB`,
  `Record Write Speed`, `Job completed`, and transformer counters;
- writes a JSON record and the raw stdout/stderr logs to `results/raw/`.

`run-bench.ps1` resets the destination before every run, invokes the script,
then checks the destination row count independently:

- MySQL: `SELECT COUNT(*) FROM bench_dest` through `docker exec`.
- PostgreSQL: `SELECT COUNT(*)` through `psql` in the container.
- SQLite: `scripts/verify-sqlite.py`, read-only (`mode=ro`).

Destination reset is outside the measured window. For MySQL/PostgreSQL the job
itself also runs `TRUNCATE TABLE bench_dest` through `preSql` during
preparation; that is inside the measured window and matches normal DataX
behaviour. The SQLite file is deleted before each run and recreated by the
job's `preSql` DDL.

Rows/s is reported two ways:

- **wall clock**: engine-reported rows divided by the whole process lifetime
  (startup, connection, transfer and shutdown);
- **engine**: the `Record Write Speed` value from the engine, which divides by
  its own transfer window (from just before scheduling to just after it).

The report uses wall-clock rows/s as the headline number and keeps the engine
number for cross-checking.

## Files

```
bench/
  README.md
  configs/                   job JSON files for every scenario
  data/                      generated datasets (gitignored)
  results/2026-11-24-baseline.md
  results/raw/               per-run JSON, summary.csv, env.json, logs
  scripts/
    setup-containers.sh      start MySQL/PostgreSQL, load schema and data
    setup-mysql.sql          MySQL source/destination DDL
    setup-mysql-load.sql     LOAD DATA from the generated CSV
    setup-postgres.sql       PostgreSQL destination DDL
    teardown-containers.sh   remove only the benchmark containers/volumes
    run-bench.ps1            orchestrate scenarios, reset, verify, summarise
    measure-run.ps1          one measured run (duration, memory, exit code)
    measure-recovery.ps1     kill/retry experiment for recovery time
    verify-sqlite.py         read-only SQLite row count
  tools/genrows/main.go      deterministic dataset generator
```
