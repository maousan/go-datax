#!/usr/bin/env bash
# Remove only the containers and volumes created by setup-containers.sh.
# The host's existing MySQL/PostgreSQL installations are never touched.
set -euo pipefail

MYSQL_CONTAINER=go-datax-bench-mysql
MYSQL_VOLUME=go-datax-bench-mysql-data
PG_CONTAINER=go-datax-bench-pg
PG_VOLUME=go-datax-bench-pg-data

docker rm -f "$MYSQL_CONTAINER" "$PG_CONTAINER" >/dev/null 2>&1 || true
docker volume rm "$MYSQL_VOLUME" "$PG_VOLUME" >/dev/null 2>&1 || true
echo "removed benchmark containers and volumes"
