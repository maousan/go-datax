#!/usr/bin/env python3
"""Summarise bench/results/raw/*.json into median/range tables.

Warm-up runs (iteration 0) and the blocked-scenario probe are excluded.
"""

import glob
import io
import json
import os
import statistics
import sys

SCENARIOS = {
    "s2": "MySQL -> PostgreSQL + transform",
    "s3": "TXT file -> SQLite",
    "s4": "MySQL -> PostgreSQL",
    "s5": "MySQL -> SQLite (supplementary)",
}


def load(path):
    with io.open(path, encoding="utf-8-sig") as handle:
        return json.load(handle)


def fmt(value, digits=0):
    if value is None:
        return "-"
    if digits == 0:
        return f"{value:,.0f}"
    return f"{value:,.{digits}f}"


def main():
    raw_dir = sys.argv[1] if len(sys.argv) > 1 else "bench/results/raw"
    runs = {}
    for path in sorted(glob.glob(os.path.join(raw_dir, "s*-run*-iter*.json"))):
        record = load(path)
        scenario = record.get("scenario")
        if scenario not in SCENARIOS:
            continue
        if record.get("iteration", 0) == 0:
            continue
        runs.setdefault(scenario, []).append(record)

    if not runs:
        print("no measured runs found", file=sys.stderr)
        return 1

    print("| ID | Scenario | n | rows/s median (range) | duration s median (range) | "
          "peak WS MB median (range) | rows | engine rec/s |")
    print("|---|---|---:|---|---|---|---:|---:|")
    for key, name in SCENARIOS.items():
        records = runs.get(key, [])
        if not records:
            continue
        rps = [r["rowsPerSecondWallClock"] for r in records if r["rowsPerSecondWallClock"]]
        dur = [r["durationSeconds"] for r in records]
        ws = [r["peakWorkingSetMB"] for r in records]
        rows = [r["engineTotalRecords"] for r in records]
        engine_speed = [r["engineRecordSpeed"] for r in records if r["engineRecordSpeed"]]
        print(
            f"| {key} | {name} | {len(records)} | "
            f"{fmt(statistics.median(rps))} ({fmt(min(rps))}-{fmt(max(rps))}) | "
            f"{fmt(statistics.median(dur), 1)} ({fmt(min(dur), 1)}-{fmt(max(dur), 1)}) | "
            f"{fmt(statistics.median(ws), 1)} ({fmt(min(ws), 1)}-{fmt(max(ws), 1)}) | "
            f"{fmt(statistics.median(rows))} | {fmt(statistics.median(engine_speed))} |"
        )

    transforms = [r for r in runs.get("s2", []) if r.get("transformUsedTime")]
    if transforms:
        print()
        print("Transformer used time (s2, engine counter): " +
              ", ".join(r["transformUsedTime"] for r in transforms))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
