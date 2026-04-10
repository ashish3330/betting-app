#!/bin/bash
# ============================================================
# Lotus Exchange — Database Setup Script
# Run this once to create the database, then every time to
# ensure schema is up to date.
#
# Usage:
#   ./scripts/setup-db.sh
#
# Prerequisites:
#   - PostgreSQL running on localhost:5432
#   - psql CLI available
# ============================================================

set -euo pipefail

# Configurable via env vars
DB_NAME="${POSTGRES_DB:-bettingdb}"
DB_USER="${POSTGRES_USER:-lotus}"
DB_PASS="${POSTGRES_PASSWORD:-L0tus!Xchg2026}"
DB_HOST="${POSTGRES_HOST:-localhost}"
DB_PORT="${POSTGRES_PORT:-5432}"

echo "🔧 Lotus Exchange — Database Setup"
echo "──────────────────────────────────"
echo "Host: $DB_HOST:$DB_PORT"
echo "Database: $DB_NAME"
echo "User: $DB_USER"
echo ""

# Create user if not exists
echo "→ Creating database user '$DB_USER'..."
psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -tc \
  "SELECT 1 FROM pg_roles WHERE rolname='$DB_USER'" | grep -q 1 || \
  psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -c \
  "CREATE USER $DB_USER WITH PASSWORD '$DB_PASS' CREATEDB;"
echo "  ✓ User ready"

# Create database if not exists
echo "→ Creating database '$DB_NAME'..."
psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -tc \
  "SELECT 1 FROM pg_database WHERE datname='$DB_NAME'" | grep -q 1 || \
  psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -c \
  "CREATE DATABASE $DB_NAME OWNER $DB_USER;"
echo "  ✓ Database ready"

# Grant privileges
psql -h "$DB_HOST" -p "$DB_PORT" -U postgres -c \
  "GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $DB_USER;" 2>/dev/null || true

# Connection string for remaining operations
export PGPASSWORD="$DB_PASS"
PSQL="psql -h $DB_HOST -p $DB_PORT -U $DB_USER -d $DB_NAME"

# Run all migration files in order
echo "→ Running migrations..."
for migration in migrations/*.sql; do
  if [ -f "$migration" ]; then
    name=$(basename "$migration")
    echo "  → $name"
    $PSQL -f "$migration" 2>&1 | grep -v "already exists\|NOTICE\|duplicate key" || true
  fi
done
echo "  ✓ Migrations complete"

# Seed the default users (the Go server also does this via /api/v1/seed,
# but this ensures users exist even before the server starts)
echo "→ Seeding default users..."
$PSQL -c "
  INSERT INTO auth.users (username, email, password_hash, role, path, balance, credit_limit, commission_rate, status, referral_code)
  VALUES
    ('superadmin', 'sa@lotus.com', '', 'superadmin', '1', 10000000, 10000000, 5, 'active', 'REF-SA-001'),
    ('admin1', 'ad@lotus.com', '', 'admin', '1.2', 5000000, 5000000, 4, 'active', 'REF-AD-001'),
    ('master1', 'ma@lotus.com', '', 'master', '1.2.3', 1000000, 1000000, 3, 'active', 'REF-MA-001'),
    ('agent1', 'ag@lotus.com', '', 'agent', '1.2.3.4', 500000, 500000, 2, 'active', 'REF-AG-001'),
    ('player1', 'p1@lotus.com', '', 'client', '1.2.3.4.5', 100000, 100000, 2, 'active', 'REF-P1-001'),
    ('player2', 'p2@lotus.com', '', 'client', '1.2.3.4.6', 100000, 100000, 2, 'active', 'REF-P2-001')
  ON CONFLICT (username) DO NOTHING;
" 2>&1 | grep -v "0 rows\|INSERT" || true

# Update parent_id references
$PSQL -c "
  UPDATE auth.users SET parent_id = 1 WHERE username = 'admin1' AND parent_id IS NULL;
  UPDATE auth.users SET parent_id = 2 WHERE username = 'master1' AND parent_id IS NULL;
  UPDATE auth.users SET parent_id = 3 WHERE username = 'agent1' AND parent_id IS NULL;
  UPDATE auth.users SET parent_id = 4 WHERE username IN ('player1', 'player2') AND parent_id IS NULL;
" 2>/dev/null || true

echo "  ✓ Users seeded (passwords set on first server start via /api/v1/seed)"

echo ""
echo "✅ Database setup complete!"
echo ""
echo "Connection string:"
echo "  postgres://$DB_USER:****@$DB_HOST:$DB_PORT/$DB_NAME?sslmode=disable&search_path=betting,auth,public"
echo ""
echo "Next steps:"
echo "  1. Start the server: ./scripts/start-dev.sh"
echo "  2. Hit POST /api/v1/seed to set passwords and credit chain"
