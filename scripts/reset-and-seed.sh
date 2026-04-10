#!/bin/bash
# Reset database and seed fresh data for testing
# Usage: ./scripts/reset-and-seed.sh
#
# Required environment variables (or set in .env):
#   DATABASE_URL              - PostgreSQL connection string
#   SEED_SUPERADMIN_PASSWORD  - Password for superadmin user
#   SEED_ADMIN_PASSWORD       - Password for admin users
#   SEED_MASTER_PASSWORD      - Password for master users
#   SEED_AGENT_PASSWORD       - Password for agent users
#   SEED_PLAYER_PASSWORD      - Password for player users

set -e
cd "$(dirname "$0")/.."

# Load .env file if present
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

# Validate required vars
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL is not set. Set it in your environment or .env file."
  exit 1
fi

DB_URL="$DATABASE_URL"

SEED_SUPERADMIN_PASSWORD="${SEED_SUPERADMIN_PASSWORD:?ERROR: SEED_SUPERADMIN_PASSWORD is not set}"
SEED_ADMIN_PASSWORD="${SEED_ADMIN_PASSWORD:?ERROR: SEED_ADMIN_PASSWORD is not set}"
SEED_MASTER_PASSWORD="${SEED_MASTER_PASSWORD:?ERROR: SEED_MASTER_PASSWORD is not set}"
SEED_AGENT_PASSWORD="${SEED_AGENT_PASSWORD:?ERROR: SEED_AGENT_PASSWORD is not set}"
SEED_PLAYER_PASSWORD="${SEED_PLAYER_PASSWORD:?ERROR: SEED_PLAYER_PASSWORD is not set}"

echo "=== Resetting Database ==="
psql "$DB_URL" -c "
TRUNCATE auth.login_history CASCADE;
TRUNCATE auth.user_sessions CASCADE;
TRUNCATE betting.bets CASCADE;
TRUNCATE betting.ledger CASCADE;
TRUNCATE betting.notifications CASCADE;
TRUNCATE betting.audit_log CASCADE;
TRUNCATE betting.deposit_requests CASCADE;
TRUNCATE betting.bank_accounts CASCADE;
TRUNCATE betting.daily_account_usage CASCADE;
TRUNCATE betting.cashout_offers CASCADE;
TRUNCATE betting.payment_transactions CASCADE;
TRUNCATE betting.settlement_events CASCADE;
TRUNCATE auth.users CASCADE;
ALTER SEQUENCE auth.users_id_seq RESTART WITH 1;
"

echo "=== Running Migrations ==="
for f in migrations/0*.sql; do
  echo "  Running $(basename $f)..."
  psql "$DB_URL" -f "$f" 2>&1 | grep -E "^(CREATE|ALTER|ERROR)" || true
done

echo ""
echo "=== Creating Users ==="
API="${API_URL:-http://localhost:8080}"

# Superadmin
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"superadmin\",\"email\":\"superadmin@lotus.com\",\"password\":\"${SEED_SUPERADMIN_PASSWORD}\",\"role\":\"superadmin\"}" -o /dev/null -w "  superadmin: %{http_code}\n"

# Set root balance
psql "$DB_URL" -c "UPDATE auth.users SET balance=10000000, credit_limit=10000000 WHERE id=1;" > /dev/null

# Hierarchy
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"admin1\",\"email\":\"admin1@lotus.com\",\"password\":\"${SEED_ADMIN_PASSWORD}\",\"role\":\"admin\"}" -o /dev/null -w "  admin1: %{http_code}\n"
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"master1\",\"email\":\"master1@lotus.com\",\"password\":\"${SEED_MASTER_PASSWORD}\",\"role\":\"master\"}" -o /dev/null -w "  master1: %{http_code}\n"
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"agent1\",\"email\":\"agent1@lotus.com\",\"password\":\"${SEED_AGENT_PASSWORD}\",\"role\":\"agent\"}" -o /dev/null -w "  agent1: %{http_code}\n"
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"player1\",\"email\":\"player1@lotus.com\",\"password\":\"${SEED_PLAYER_PASSWORD}\",\"role\":\"client\"}" -o /dev/null -w "  player1: %{http_code}\n"
curl -s "$API/api/v1/auth/register" -X POST -H "Content-Type: application/json" \
  -d "{\"username\":\"player2\",\"email\":\"player2@lotus.com\",\"password\":\"${SEED_PLAYER_PASSWORD}\",\"role\":\"client\"}" -o /dev/null -w "  player2: %{http_code}\n"

# Set hierarchy + balances
psql "$DB_URL" -c "
SET search_path TO betting, auth, public;
UPDATE auth.users SET parent_id=1, path='1.2'::betting.ltree, credit_limit=5000000, balance=500000 WHERE id=2;
UPDATE auth.users SET parent_id=2, path='1.2.3'::betting.ltree, credit_limit=1000000, balance=200000 WHERE id=3;
UPDATE auth.users SET parent_id=3, path='1.2.3.4'::betting.ltree, credit_limit=500000, balance=100000 WHERE id=4;
UPDATE auth.users SET parent_id=4, path='1.2.3.4.5'::betting.ltree, credit_limit=100000, balance=50000 WHERE id=5;
UPDATE auth.users SET parent_id=4, path='1.2.3.4.6'::betting.ltree, credit_limit=100000, balance=50000 WHERE id=6;
" > /dev/null

echo ""
echo "=== Seeding Market Data ==="
curl -s -X POST "$API/api/v1/seed" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  Users: {len(d.get(\"users\",[]))}'); print(f'  Credits: {len(d.get(\"credits\",[]))}')" 2>/dev/null || echo "  Seed endpoint called"

echo ""
echo "=== Clearing Frontend Cache ==="
rm -rf frontend/.next/cache 2>/dev/null && echo "  Next.js cache cleared" || true

echo ""
echo "=== Fresh Start Ready ==="
echo "  Users created: superadmin, admin1, master1, agent1, player1, player2"
echo "  Passwords: as configured in SEED_*_PASSWORD environment variables"
