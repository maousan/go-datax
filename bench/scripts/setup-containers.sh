#!/usr/bin/env bash
# Start the isolated benchmark databases and load the deterministic dataset.
#
#   bench/scripts/setup-containers.sh            # create/start + load schema
#   bench/scripts/setup-containers.sh --no-schema # create/start only
#   bench/scripts/setup-containers.sh --reset    # delete containers/volumes first
#
# Ports are deliberately non-default so the harness never touches an existing
# MySQL/PostgreSQL instance on the host.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

MYSQL_CONTAINER=go-datax-bench-mysql
MYSQL_VOLUME=go-datax-bench-mysql-data
MYSQL_PORT=13306
MYSQL_IMAGE=mysql:8.0

PG_CONTAINER=go-datax-bench-pg
PG_VOLUME=go-datax-bench-pg-data
PG_PORT=15432
PG_IMAGE=postgres:16-alpine

RESET=0
LOAD_SCHEMA=1
for arg in "$@"; do
  case "$arg" in
    --reset) RESET=1 ;;
    --no-schema) LOAD_SCHEMA=0 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

if [[ "$RESET" -eq 1 ]]; then
  docker rm -f "$MYSQL_CONTAINER" "$PG_CONTAINER" >/dev/null 2>&1 || true
  docker volume rm "$MYSQL_VOLUME" "$PG_VOLUME" >/dev/null 2>&1 || true
fi

if ! docker inspect "$MYSQL_CONTAINER" >/dev/null 2>&1; then
  docker run -d --name "$MYSQL_CONTAINER" \
    -p "$MYSQL_PORT:3306" \
    -e MYSQL_ROOT_PASSWORD=benchpass \
    -e MYSQL_DATABASE=datax_bench \
    -v "$MYSQL_VOLUME:/var/lib/mysql" \
    --memory=4g --cpus=6 \
    "$MYSQL_IMAGE" \
    --local-infile=1 --innodb-buffer-pool-size=1G --max-connections=300
else
  docker start "$MYSQL_CONTAINER" >/dev/null
fi

if ! docker inspect "$PG_CONTAINER" >/dev/null 2>&1; then
  docker run -d --name "$PG_CONTAINER" \
    -p "$PG_PORT:5432" \
    -e POSTGRES_PASSWORD=benchpass \
    -e POSTGRES_DB=datax_bench \
    -v "$PG_VOLUME:/var/lib/postgresql/data" \
    --memory=4g --cpus=6 \
    "$PG_IMAGE"
else
  docker start "$PG_CONTAINER" >/dev/null
fi

echo "waiting for MySQL on 127.0.0.1:$MYSQL_PORT ..."
for _ in $(seq 1 180); do
  if docker exec -e MYSQL_PWD=benchpass "$MYSQL_CONTAINER" mysqladmin ping -uroot --silent >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
docker exec -e MYSQL_PWD=benchpass "$MYSQL_CONTAINER" mysqladmin ping -uroot --silent >/dev/null

echo "waiting for PostgreSQL on 127.0.0.1:$PG_PORT ..."
for _ in $(seq 1 90); do
  if docker exec "$PG_CONTAINER" pg_isready -U postgres -d datax_bench >/dev/null 2>&1; then
    break
  fi
  sleep 2
done
docker exec "$PG_CONTAINER" pg_isready -U postgres -d datax_bench >/dev/null

if [[ "$LOAD_SCHEMA" -eq 1 ]]; then
  echo "loading MySQL schema ..."
  docker exec -i -e MYSQL_PWD=benchpass "$MYSQL_CONTAINER" \
    mysql -uroot datax_bench < "$SCRIPT_DIR/setup-mysql.sql"

  if [[ ! -f "$ROOT_DIR/bench/data/rows-1m.txt" ]]; then
    echo "generating 1,000,000-row dataset ..."
    (cd "$ROOT_DIR" && go run ./bench/tools/genrows -rows 1000000 -out bench/data/rows-1m.txt)
  fi

  echo "copying dataset into MySQL container and running LOAD DATA ..."
  docker cp "$ROOT_DIR/bench/data/rows-1m.txt" "$MYSQL_CONTAINER:/tmp/go-datax-bench-rows.txt"
  docker exec -i -e MYSQL_PWD=benchpass "$MYSQL_CONTAINER" \
    mysql --local-infile=1 -uroot datax_bench < "$SCRIPT_DIR/setup-mysql-load.sql"

  echo "loading PostgreSQL destination schema ..."
  docker exec -i "$PG_CONTAINER" \
    psql -v ON_ERROR_STOP=1 -U postgres -d datax_bench < "$SCRIPT_DIR/setup-postgres.sql"

  docker exec -e MYSQL_PWD=benchpass "$MYSQL_CONTAINER" mysql -uroot -N -B datax_bench -e \
    "SELECT CONCAT('bench_source rows=', COUNT(*)) FROM bench_source;"
fi

echo "databases ready: mysql=127.0.0.1:$MYSQL_PORT postgres=127.0.0.1:$PG_PORT"
