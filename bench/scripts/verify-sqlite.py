#!/usr/bin/env python3
"""Print the row count of the SQLite benchmark destination.

Used by run-bench.ps1 to verify the file -> SQLite scenario. Reads the database
read-only so a verification step can never mutate benchmark output.
"""

import sqlite3
import sys


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: verify-sqlite.py <database.db>", file=sys.stderr)
        return 2

    connection = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
    try:
        (count,) = connection.execute("SELECT COUNT(*) FROM bench_dest").fetchone()
    finally:
        connection.close()

    print(count)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
