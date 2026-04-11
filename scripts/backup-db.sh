#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# Lotus Exchange - PostgreSQL backup script
#
# Reads PG connection settings from .env (sourced from the project root) and
# writes a custom-format, compressed pg_dump to /tmp.
#
# Usage:    ./scripts/backup-db.sh
# -----------------------------------------------------------------------------
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/.." &> /dev/null && pwd)"
ENV_FILE="${PROJECT_ROOT}/.env"

if [[ -f "${ENV_FILE}" ]]; then
  # shellcheck disable=SC1090
  set -a
  source "${ENV_FILE}"
  set +a
else
  echo "warning: ${ENV_FILE} not found; relying on existing PG* env vars" >&2
fi

# Map .env vars onto the standard libpq variables that pg_dump understands.
export PGHOST="${PGHOST:-${POSTGRES_HOST:-localhost}}"
export PGPORT="${PGPORT:-${POSTGRES_PORT:-5432}}"
export PGUSER="${PGUSER:-${POSTGRES_USER:-lotus}}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-}}"
export PGDATABASE="${PGDATABASE:-${POSTGRES_DB:-bettingdb}}"

TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
DUMP_PATH="/tmp/lotus-bettingdb-${TIMESTAMP}.dump"

pg_dump \
  --format=custom \
  --compress=9 \
  --file="${DUMP_PATH}" \
  bettingdb

echo "${DUMP_PATH}"
