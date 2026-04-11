#!/usr/bin/env bash
# -----------------------------------------------------------------------------
# Lotus Exchange - PostgreSQL restore script
#
# Drops and recreates the bettingdb database, then restores from the supplied
# pg_dump custom-format file.
#
# Usage:
#   ./scripts/restore-db.sh /path/to/dumpfile.dump          # interactive
#   ./scripts/restore-db.sh /path/to/dumpfile.dump --force  # no prompt
# -----------------------------------------------------------------------------
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <dump-file> [--force]" >&2
  exit 2
fi

DUMP_FILE="$1"
FORCE="false"
if [[ "${2:-}" == "--force" ]]; then
  FORCE="true"
fi

if [[ ! -f "${DUMP_FILE}" ]]; then
  echo "error: dump file not found: ${DUMP_FILE}" >&2
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)"
PROJECT_ROOT="$(cd -- "${SCRIPT_DIR}/.." &> /dev/null && pwd)"
ENV_FILE="${PROJECT_ROOT}/.env"

if [[ -f "${ENV_FILE}" ]]; then
  # shellcheck disable=SC1090
  set -a
  source "${ENV_FILE}"
  set +a
fi

export PGHOST="${PGHOST:-${POSTGRES_HOST:-localhost}}"
export PGPORT="${PGPORT:-${POSTGRES_PORT:-5432}}"
export PGUSER="${PGUSER:-${POSTGRES_USER:-lotus}}"
export PGPASSWORD="${PGPASSWORD:-${POSTGRES_PASSWORD:-}}"

DB_NAME="bettingdb"

if [[ "${FORCE}" != "true" ]]; then
  echo "WARNING: this will DROP and recreate the '${DB_NAME}' database on ${PGHOST}:${PGPORT}." >&2
  read -r -p "Type 'yes' to continue: " CONFIRM
  if [[ "${CONFIRM}" != "yes" ]]; then
    echo "aborted." >&2
    exit 1
  fi
fi

# Drop & recreate against the maintenance database.
psql --dbname=postgres --quiet --command="DROP DATABASE IF EXISTS ${DB_NAME};"
psql --dbname=postgres --quiet --command="CREATE DATABASE ${DB_NAME};"

pg_restore \
  --verbose \
  --clean \
  --if-exists \
  --no-owner \
  -d "${DB_NAME}" \
  "${DUMP_FILE}"

echo "restore complete: ${DUMP_FILE} -> ${DB_NAME}"
