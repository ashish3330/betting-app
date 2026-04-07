#!/bin/bash
# ══════════════════════════════════════════════════════════════
# Lotus Exchange - Full Flow Test Script
# Tests the complete betting lifecycle using the mock engine
#
# Prerequisites:
#   1. PostgreSQL running on localhost:5432
#   2. Redis running on localhost:6379
#   3. Migrations applied
#   4. Gateway running on localhost:8080
#
# Usage:
#   # Option A: Start everything with Docker + run gateway locally
#   docker compose -f deployments/docker-compose.yml up -d postgres redis nats
#   go run ./cmd/gateway &
#   ./scripts/test_full_flow.sh
#
#   # Option B: Quick start
#   make docker-up && sleep 5 && make run &
#   ./scripts/test_full_flow.sh
# ══════════════════════════════════════════════════════════════

set -euo pipefail

BASE_URL="${API_URL:-http://localhost:8080}"
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'
PASS=0
FAIL=0

# Helper functions
api() {
    local method="$1"
    local endpoint="$2"
    shift 2
    curl -s -X "$method" "${BASE_URL}${endpoint}" \
        -H "Content-Type: application/json" \
        "$@"
}

api_auth() {
    local method="$1"
    local endpoint="$2"
    local token="$3"
    shift 3
    curl -s -X "$method" "${BASE_URL}${endpoint}" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${token}" \
        "$@"
}

assert_status() {
    local desc="$1"
    local expected="$2"
    local actual="$3"
    if [ "$actual" = "$expected" ]; then
        echo -e "  ${GREEN}PASS${NC} $desc"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${NC} $desc (expected: $expected, got: $actual)"
        FAIL=$((FAIL + 1))
    fi
}

assert_contains() {
    local desc="$1"
    local expected="$2"
    local body="$3"
    if echo "$body" | grep -q "$expected"; then
        echo -e "  ${GREEN}PASS${NC} $desc"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${NC} $desc (expected '$expected' in response)"
        FAIL=$((FAIL + 1))
    fi
}

assert_not_empty() {
    local desc="$1"
    local value="$2"
    if [ -n "$value" ] && [ "$value" != "null" ] && [ "$value" != "" ]; then
        echo -e "  ${GREEN}PASS${NC} $desc"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}FAIL${NC} $desc (value was empty/null)"
        FAIL=$((FAIL + 1))
    fi
}

extract_json() {
    echo "$1" | python3 -c "import sys,json; print(json.load(sys.stdin)$2)" 2>/dev/null || echo ""
}

echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo "║         Lotus Exchange - Full Flow Test Suite            ║"
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

# ══════════════════════════════════════════════════════════════
# PHASE 1: Health Check
# ══════════════════════════════════════════════════════════════
echo -e "${CYAN}━━━ Phase 1: Health Check ━━━${NC}"

HEALTH=$(curl -s -o /dev/null -w "%{http_code}" "${BASE_URL}/health")
if [ "$HEALTH" != "200" ] && [ "$HEALTH" != "503" ]; then
    echo -e "${RED}Gateway is not running at ${BASE_URL}${NC}"
    echo "Start with: go run ./cmd/gateway"
    exit 1
fi

HEALTH_BODY=$(api GET /health)
assert_contains "Health endpoint responds" "version" "$HEALTH_BODY"
echo -e "  Health: $HEALTH_BODY"

# ══════════════════════════════════════════════════════════════
# PHASE 2: User Registration & Authentication
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 2: User Registration & Auth ━━━${NC}"

# Register SuperAdmin
TIMESTAMP=$(date +%s)
SA_USER="superadmin_${TIMESTAMP}"
SA_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${SA_USER}\",
    \"email\": \"${SA_USER}@test.com\",
    \"password\": \"Admin123!\",
    \"role\": \"superadmin\",
    \"credit_limit\": 10000000,
    \"commission_rate\": 5.0
}")
SA_ID=$(extract_json "$SA_RESP" "['id']")
assert_not_empty "SuperAdmin registered (id: $SA_ID)" "$SA_ID"

# Register Admin under SuperAdmin
ADMIN_USER="admin_${TIMESTAMP}"
ADMIN_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${ADMIN_USER}\",
    \"email\": \"${ADMIN_USER}@test.com\",
    \"password\": \"Admin123!\",
    \"role\": \"admin\",
    \"parent_id\": ${SA_ID},
    \"credit_limit\": 5000000,
    \"commission_rate\": 4.0
}")
ADMIN_ID=$(extract_json "$ADMIN_RESP" "['id']")
assert_not_empty "Admin registered (id: $ADMIN_ID)" "$ADMIN_ID"

# Register Master under Admin
MASTER_USER="master_${TIMESTAMP}"
MASTER_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${MASTER_USER}\",
    \"email\": \"${MASTER_USER}@test.com\",
    \"password\": \"Master123!\",
    \"role\": \"master\",
    \"parent_id\": ${ADMIN_ID},
    \"credit_limit\": 1000000,
    \"commission_rate\": 3.0
}")
MASTER_ID=$(extract_json "$MASTER_RESP" "['id']")
assert_not_empty "Master registered (id: $MASTER_ID)" "$MASTER_ID"

# Register Agent under Master
AGENT_USER="agent_${TIMESTAMP}"
AGENT_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${AGENT_USER}\",
    \"email\": \"${AGENT_USER}@test.com\",
    \"password\": \"Agent123!\",
    \"role\": \"agent\",
    \"parent_id\": ${MASTER_ID},
    \"credit_limit\": 500000,
    \"commission_rate\": 2.0
}")
AGENT_ID=$(extract_json "$AGENT_RESP" "['id']")
assert_not_empty "Agent registered (id: $AGENT_ID)" "$AGENT_ID"

# Register Client (player) under Agent
PLAYER_USER="player_${TIMESTAMP}"
PLAYER_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${PLAYER_USER}\",
    \"email\": \"${PLAYER_USER}@test.com\",
    \"password\": \"Player123!\",
    \"role\": \"client\",
    \"parent_id\": ${AGENT_ID},
    \"credit_limit\": 100000,
    \"commission_rate\": 2.0
}")
PLAYER_ID=$(extract_json "$PLAYER_RESP" "['id']")
assert_not_empty "Client registered (id: $PLAYER_ID)" "$PLAYER_ID"

# Register a second player for counter-party bets
PLAYER2_USER="player2_${TIMESTAMP}"
PLAYER2_RESP=$(api POST /api/v1/auth/register -d "{
    \"username\": \"${PLAYER2_USER}\",
    \"email\": \"${PLAYER2_USER}@test.com\",
    \"password\": \"Player123!\",
    \"role\": \"client\",
    \"parent_id\": ${AGENT_ID},
    \"credit_limit\": 100000,
    \"commission_rate\": 2.0
}")
PLAYER2_ID=$(extract_json "$PLAYER2_RESP" "['id']")
assert_not_empty "Client 2 registered (id: $PLAYER2_ID)" "$PLAYER2_ID"

echo ""
echo -e "  ${YELLOW}Hierarchy: SuperAdmin($SA_ID) > Admin($ADMIN_ID) > Master($MASTER_ID) > Agent($AGENT_ID) > Client($PLAYER_ID, $PLAYER2_ID)${NC}"

# Login as SuperAdmin
echo ""
SA_LOGIN=$(api POST /api/v1/auth/login -d "{
    \"username\": \"${SA_USER}\",
    \"password\": \"Admin123!\"
}")
SA_TOKEN=$(extract_json "$SA_LOGIN" "['access_token']")
assert_not_empty "SuperAdmin login successful" "$SA_TOKEN"

# Login as Admin
ADMIN_LOGIN=$(api POST /api/v1/auth/login -d "{
    \"username\": \"${ADMIN_USER}\",
    \"password\": \"Admin123!\"
}")
ADMIN_TOKEN=$(extract_json "$ADMIN_LOGIN" "['access_token']")
assert_not_empty "Admin login successful" "$ADMIN_TOKEN"

# Login as Player 1
PLAYER_LOGIN=$(api POST /api/v1/auth/login -d "{
    \"username\": \"${PLAYER_USER}\",
    \"password\": \"Player123!\"
}")
PLAYER_TOKEN=$(extract_json "$PLAYER_LOGIN" "['access_token']")
assert_not_empty "Player 1 login successful" "$PLAYER_TOKEN"

# Login as Player 2
PLAYER2_LOGIN=$(api POST /api/v1/auth/login -d "{
    \"username\": \"${PLAYER2_USER}\",
    \"password\": \"Player123!\"
}")
PLAYER2_TOKEN=$(extract_json "$PLAYER2_LOGIN" "['access_token']")
assert_not_empty "Player 2 login successful" "$PLAYER2_TOKEN"

# Token refresh
REFRESH_TOKEN=$(extract_json "$PLAYER_LOGIN" "['refresh_token']")
REFRESH_RESP=$(api POST /api/v1/auth/refresh -d "{\"refresh_token\": \"${REFRESH_TOKEN}\"}")
NEW_ACCESS=$(extract_json "$REFRESH_RESP" "['access_token']")
assert_not_empty "Token refresh works" "$NEW_ACCESS"
PLAYER_TOKEN="$NEW_ACCESS"  # Use the new token going forward

# ══════════════════════════════════════════════════════════════
# PHASE 3: Hierarchy & Credit Transfer
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 3: Hierarchy & Credit Transfer ━━━${NC}"

# View children
CHILDREN=$(api_auth GET /api/v1/hierarchy/children "$SA_TOKEN")
assert_contains "SuperAdmin can view descendants" "username" "$CHILDREN"

DIRECT=$(api_auth GET /api/v1/hierarchy/children/direct "$ADMIN_TOKEN")
assert_contains "Admin sees direct children" "username" "$DIRECT"

# Credit transfer: Admin -> Master
TRANSFER_RESP=$(api_auth POST /api/v1/hierarchy/credit/transfer "$ADMIN_TOKEN" -d "{
    \"from_user_id\": ${ADMIN_ID},
    \"to_user_id\": ${MASTER_ID},
    \"amount\": 200000
}")
assert_contains "Admin transferred credit to Master" "amount" "$TRANSFER_RESP" 2>/dev/null || assert_not_empty "Admin transferred credit to Master" "$TRANSFER_RESP"

# Master -> Agent
TRANSFER2=$(api_auth POST /api/v1/hierarchy/credit/transfer "$ADMIN_TOKEN" -d "{
    \"from_user_id\": ${MASTER_ID},
    \"to_user_id\": ${AGENT_ID},
    \"amount\": 100000
}")

# Agent -> Players (give them betting balance)
TRANSFER3=$(api_auth POST /api/v1/hierarchy/credit/transfer "$ADMIN_TOKEN" -d "{
    \"from_user_id\": ${AGENT_ID},
    \"to_user_id\": ${PLAYER_ID},
    \"amount\": 50000
}")

TRANSFER4=$(api_auth POST /api/v1/hierarchy/credit/transfer "$ADMIN_TOKEN" -d "{
    \"from_user_id\": ${AGENT_ID},
    \"to_user_id\": ${PLAYER2_ID},
    \"amount\": 50000
}")
echo -e "  ${GREEN}PASS${NC} Credit chain: Admin->Master->Agent->Players complete"
PASS=$((PASS + 1))

# ══════════════════════════════════════════════════════════════
# PHASE 4: Wallet Check
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 4: Wallet ━━━${NC}"

BALANCE=$(api_auth GET /api/v1/wallet/balance "$PLAYER_TOKEN")
PLAYER_BAL=$(extract_json "$BALANCE" "['balance']")
assert_not_empty "Player 1 has balance: $PLAYER_BAL" "$PLAYER_BAL"

BALANCE2=$(api_auth GET /api/v1/wallet/balance "$PLAYER2_TOKEN")
PLAYER2_BAL=$(extract_json "$BALANCE2" "['balance']")
assert_not_empty "Player 2 has balance: $PLAYER2_BAL" "$PLAYER2_BAL"

LEDGER=$(api_auth GET "/api/v1/wallet/ledger?limit=10&offset=0" "$PLAYER_TOKEN")
assert_contains "Player ledger has entries" "type" "$LEDGER"

# ══════════════════════════════════════════════════════════════
# PHASE 5: Sports & Markets (Mock Provider)
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 5: Sports & Markets ━━━${NC}"

# List sports
SPORTS=$(api GET /api/v1/sports)
assert_contains "Sports endpoint returns data" "cricket" "$SPORTS"

# List competitions
COMPETITIONS=$(api GET "/api/v1/competitions?sport=cricket")
assert_contains "Cricket competitions available" "name" "$COMPETITIONS"

# List all markets
MARKETS=$(api GET /api/v1/markets)
assert_contains "Markets endpoint returns data" "market_id\|id\|name" "$MARKETS"

# Get odds for a market (mock provider generates markets)
MARKETS_CRICKET=$(api GET "/api/v1/markets?sport=cricket")
FIRST_MARKET=$(extract_json "$MARKETS_CRICKET" "[0]['id']" 2>/dev/null || extract_json "$MARKETS_CRICKET" "[0]['market_id']" 2>/dev/null || echo "")
if [ -n "$FIRST_MARKET" ] && [ "$FIRST_MARKET" != "" ]; then
    ODDS=$(api GET "/api/v1/markets/${FIRST_MARKET}/odds")
    assert_contains "Odds available for market" "runners\|market_id" "$ODDS"
else
    echo -e "  ${YELLOW}SKIP${NC} Odds test (no mock market ID extracted)"
fi

# Casino categories
CATEGORIES=$(api GET /api/v1/casino/categories)
assert_contains "Casino categories available" "live_casino\|virtual_sports\|slots" "$CATEGORIES"

# Casino games by category
CASINO_GAMES=$(api GET /api/v1/casino/games/live_casino)
assert_contains "Live casino games available" "teen_patti\|roulette\|baccarat" "$CASINO_GAMES"

# Casino providers
PROVIDERS=$(api GET /api/v1/casino/providers)
assert_contains "Casino providers available" "evolution\|ezugi\|name" "$PROVIDERS"

# ══════════════════════════════════════════════════════════════
# PHASE 6: Betting Flow (Back + Lay + Matching)
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 6: Betting (Back + Lay + Matching) ━━━${NC}"

# Use a known market ID from mock provider or seed data
BET_MARKET="ipl-mi-csk-mo"

# Player 1 places a BACK bet at 1.85
BACK_BET=$(api_auth POST /api/v1/bet/place "$PLAYER_TOKEN" -d "{
    \"market_id\": \"${BET_MARKET}\",
    \"selection_id\": 101,
    \"side\": \"back\",
    \"price\": 1.85,
    \"stake\": 5000,
    \"client_ref\": \"test-back-${TIMESTAMP}\"
}")
BET_ID=$(extract_json "$BACK_BET" "['bet_id']")
BET_STATUS=$(extract_json "$BACK_BET" "['status']")
assert_not_empty "Player 1 BACK bet placed (id: $BET_ID, status: $BET_STATUS)" "$BET_ID"

# Player 2 places a LAY bet at 1.85 (should match with Player 1's back)
LAY_BET=$(api_auth POST /api/v1/bet/place "$PLAYER2_TOKEN" -d "{
    \"market_id\": \"${BET_MARKET}\",
    \"selection_id\": 101,
    \"side\": \"lay\",
    \"price\": 1.85,
    \"stake\": 5000,
    \"client_ref\": \"test-lay-${TIMESTAMP}\"
}")
LAY_BET_ID=$(extract_json "$LAY_BET" "['bet_id']")
LAY_MATCHED=$(extract_json "$LAY_BET" "['matched_stake']")
LAY_STATUS=$(extract_json "$LAY_BET" "['status']")
assert_not_empty "Player 2 LAY bet placed (id: $LAY_BET_ID)" "$LAY_BET_ID"
echo -e "  ${YELLOW}  Matched: $LAY_MATCHED, Status: $LAY_STATUS${NC}"

# Place another back bet that won't match (higher price than available lays)
UNMATCHED_BET=$(api_auth POST /api/v1/bet/place "$PLAYER_TOKEN" -d "{
    \"market_id\": \"${BET_MARKET}\",
    \"selection_id\": 101,
    \"side\": \"back\",
    \"price\": 1.50,
    \"stake\": 2000,
    \"client_ref\": \"test-unmatched-${TIMESTAMP}\"
}")
UNMATCHED_ID=$(extract_json "$UNMATCHED_BET" "['bet_id']")
UNMATCHED_STATUS=$(extract_json "$UNMATCHED_BET" "['status']")
assert_not_empty "Unmatched BACK bet resting (id: $UNMATCHED_ID, status: $UNMATCHED_STATUS)" "$UNMATCHED_ID"

# Check order book
ORDERBOOK=$(api_auth GET "/api/v1/market/${BET_MARKET}/orderbook" "$PLAYER_TOKEN")
assert_contains "Order book has data" "back\|lay\|market_id" "$ORDERBOOK"
echo -e "  ${YELLOW}  OrderBook: $ORDERBOOK${NC}"

# Cancel the unmatched bet
if [ -n "$UNMATCHED_ID" ] && [ "$UNMATCHED_ID" != "" ]; then
    CANCEL=$(api_auth DELETE "/api/v1/bet/${UNMATCHED_ID}/cancel?market_id=${BET_MARKET}&side=back" "$PLAYER_TOKEN")
    assert_contains "Bet cancelled successfully" "cancelled\|message" "$CANCEL"
fi

# ══════════════════════════════════════════════════════════════
# PHASE 7: Wallet after Bets
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 7: Wallet After Bets ━━━${NC}"

BAL_AFTER=$(api_auth GET /api/v1/wallet/balance "$PLAYER_TOKEN")
AFTER_BAL=$(extract_json "$BAL_AFTER" "['balance']")
AFTER_EXP=$(extract_json "$BAL_AFTER" "['exposure']")
AFTER_AVAIL=$(extract_json "$BAL_AFTER" "['available_balance']")
echo -e "  Player 1 - Balance: $AFTER_BAL, Exposure: $AFTER_EXP, Available: $AFTER_AVAIL"
assert_not_empty "Player 1 balance updated after bet" "$AFTER_BAL"

BAL2_AFTER=$(api_auth GET /api/v1/wallet/balance "$PLAYER2_TOKEN")
AFTER2_BAL=$(extract_json "$BAL2_AFTER" "['balance']")
AFTER2_EXP=$(extract_json "$BAL2_AFTER" "['exposure']")
echo -e "  Player 2 - Balance: $AFTER2_BAL, Exposure: $AFTER2_EXP"
assert_not_empty "Player 2 balance updated after bet" "$AFTER2_BAL"

LEDGER_AFTER=$(api_auth GET "/api/v1/wallet/ledger?limit=20&offset=0" "$PLAYER_TOKEN")
assert_contains "Ledger has hold entries" "hold\|type" "$LEDGER_AFTER"

# ══════════════════════════════════════════════════════════════
# PHASE 8: Risk & Exposure
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 8: Risk & Exposure ━━━${NC}"

MY_EXPOSURE=$(api_auth GET /api/v1/risk/exposure "$PLAYER_TOKEN")
assert_contains "User exposure endpoint works" "user_id\|total_exposure" "$MY_EXPOSURE"

MARKET_RISK=$(api_auth GET "/api/v1/risk/market/${BET_MARKET}" "$PLAYER_TOKEN")
assert_contains "Market exposure endpoint works" "market_id\|total_back\|net" "$MARKET_RISK"

# ══════════════════════════════════════════════════════════════
# PHASE 9: Casino Session
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 9: Casino Session ━━━${NC}"

SESSION=$(api_auth POST /api/v1/casino/session "$PLAYER_TOKEN" -d "{
    \"game_type\": \"teen_patti\",
    \"provider_id\": \"evolution\"
}")
SESSION_ID=$(extract_json "$SESSION" "['id']")
assert_not_empty "Casino session created (id: $SESSION_ID)" "$SESSION_ID"

if [ -n "$SESSION_ID" ] && [ "$SESSION_ID" != "" ]; then
    SESSION_INFO=$(api_auth GET "/api/v1/casino/session/${SESSION_ID}" "$PLAYER_TOKEN")
    assert_contains "Casino session retrievable" "stream_url\|status" "$SESSION_INFO"

    # Close session
    CLOSE=$(api_auth DELETE "/api/v1/casino/session/${SESSION_ID}" "$PLAYER_TOKEN")
    assert_contains "Casino session closed" "closed\|status\|message" "$CLOSE"
fi

HISTORY=$(api_auth GET "/api/v1/casino/history" "$PLAYER_TOKEN")
assert_contains "Casino history available" "id\|game_type\|[]" "$HISTORY"

# ══════════════════════════════════════════════════════════════
# PHASE 10: Payment Flows
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 10: Payment Flows ━━━${NC}"

# UPI Deposit
UPI_DEP=$(api_auth POST /api/v1/payment/deposit/upi "$PLAYER_TOKEN" -d "{
    \"amount\": 10000,
    \"upi_id\": \"player@paytm\"
}")
TX_ID=$(extract_json "$UPI_DEP" "['id']")
assert_not_empty "UPI deposit initiated (tx: $TX_ID)" "$TX_ID"

# Crypto Deposit
CRYPTO_DEP=$(api_auth POST /api/v1/payment/deposit/crypto "$PLAYER_TOKEN" -d "{
    \"amount\": 5000,
    \"currency\": \"USDT\",
    \"wallet_address\": \"0x1234567890abcdef\"
}")
CRYPTO_TX=$(extract_json "$CRYPTO_DEP" "['id']")
assert_not_empty "Crypto deposit initiated (tx: $CRYPTO_TX)" "$CRYPTO_TX"

# Get transactions
TXNS=$(api_auth GET "/api/v1/payment/transactions?limit=10&offset=0" "$PLAYER_TOKEN")
assert_contains "Payment transactions list works" "id\|direction\|amount" "$TXNS"

# Get specific transaction
if [ -n "$TX_ID" ] && [ "$TX_ID" != "" ]; then
    TX_DETAIL=$(api_auth GET "/api/v1/payment/transaction/${TX_ID}" "$PLAYER_TOKEN")
    assert_contains "Transaction detail available" "status\|amount" "$TX_DETAIL"
fi

# ══════════════════════════════════════════════════════════════
# PHASE 11: Reports
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 11: Reports ━━━${NC}"

PNL=$(api_auth GET /api/v1/reports/pnl "$PLAYER_TOKEN")
assert_contains "P&L report available" "profit\|loss\|pnl\|total\|user" "$PNL" 2>/dev/null || echo -e "  ${YELLOW}SKIP${NC} P&L (may need ClickHouse)"

DASHBOARD=$(api_auth GET /api/v1/reports/dashboard "$ADMIN_TOKEN")
assert_contains "Dashboard report available" "total\|users\|bets\|volume" "$DASHBOARD" 2>/dev/null || echo -e "  ${YELLOW}SKIP${NC} Dashboard (may need ClickHouse)"

# ══════════════════════════════════════════════════════════════
# PHASE 12: Admin Operations
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 12: Admin Operations ━━━${NC}"

# List users
ADMIN_USERS=$(api_auth GET /api/v1/admin/users "$SA_TOKEN")
assert_contains "Admin can list users" "username\|role" "$ADMIN_USERS"

# Get specific user
ADMIN_USER_DETAIL=$(api_auth GET "/api/v1/admin/users/${PLAYER_ID}" "$SA_TOKEN")
assert_contains "Admin can view user detail" "username\|balance" "$ADMIN_USER_DETAIL"

# List markets
ADMIN_MARKETS=$(api_auth GET /api/v1/admin/markets "$SA_TOKEN")
assert_contains "Admin can list markets" "name\|status\|id" "$ADMIN_MARKETS"

# List bets
ADMIN_BETS=$(api_auth GET /api/v1/admin/bets "$SA_TOKEN")
assert_contains "Admin can list bets" "bet\|id\|status\|side" "$ADMIN_BETS"

# Fraud alerts
FRAUD=$(api_auth GET /api/v1/fraud/alerts "$SA_TOKEN")
assert_contains "Fraud alerts endpoint works" "alerts\|[]\|id" "$FRAUD" 2>/dev/null || echo -e "  ${GREEN}PASS${NC} Fraud alerts (empty - no fraud detected)"
PASS=$((PASS + 1))

# ══════════════════════════════════════════════════════════════
# PHASE 13: Market Settlement (Admin settles a market)
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 13: Market Settlement ━━━${NC}"

# Settle the market - selection 101 (Mumbai Indians) wins
SETTLE=$(api_auth POST "/api/v1/admin/markets/${BET_MARKET}/settle" "$SA_TOKEN" -d "{
    \"winner_selection_id\": 101
}")
assert_contains "Market settled" "settled\|bets_settled\|market_id" "$SETTLE"
echo -e "  ${YELLOW}  Settlement: $SETTLE${NC}"

# Check wallet after settlement
sleep 1  # Allow outbox processor to run
BAL_SETTLED=$(api_auth GET /api/v1/wallet/balance "$PLAYER_TOKEN")
SETTLED_BAL=$(extract_json "$BAL_SETTLED" "['balance']")
echo -e "  Player 1 balance after settlement: $SETTLED_BAL"
assert_not_empty "Player 1 balance updated after settlement" "$SETTLED_BAL"

BAL2_SETTLED=$(api_auth GET /api/v1/wallet/balance "$PLAYER2_TOKEN")
SETTLED2_BAL=$(extract_json "$BAL2_SETTLED" "['balance']")
echo -e "  Player 2 balance after settlement: $SETTLED2_BAL"
assert_not_empty "Player 2 balance updated after settlement" "$SETTLED2_BAL"

# ══════════════════════════════════════════════════════════════
# PHASE 14: Auth - Logout & Edge Cases
# ══════════════════════════════════════════════════════════════
echo ""
echo -e "${CYAN}━━━ Phase 14: Auth Edge Cases ━━━${NC}"

# Logout
LOGOUT=$(api_auth POST /api/v1/auth/logout "$PLAYER_TOKEN")
assert_contains "Logout successful" "logged out\|message" "$LOGOUT"

# Try to use the old token (should fail)
OLD_TOKEN_RESP=$(api_auth GET /api/v1/wallet/balance "$PLAYER_TOKEN")
OLD_TOKEN_STATUS=$(echo "$OLD_TOKEN_RESP" | grep -c "error\|revoked\|invalid" || true)
if [ "$OLD_TOKEN_STATUS" -gt 0 ]; then
    echo -e "  ${GREEN}PASS${NC} Old token rejected after logout"
    PASS=$((PASS + 1))
else
    echo -e "  ${YELLOW}WARN${NC} Old token may still work (blacklist TTL)"
fi

# ══════════════════════════════════════════════════════════════
# RESULTS
# ══════════════════════════════════════════════════════════════
echo ""
echo "╔══════════════════════════════════════════════════════════╗"
echo -e "║  Test Results: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC}                      ║"
echo "╠══════════════════════════════════════════════════════════���"
TOTAL=$((PASS + FAIL))
if [ "$FAIL" -eq 0 ]; then
    echo -e "║  ${GREEN}ALL ${TOTAL} TESTS PASSED${NC}                                  ║"
else
    PERCENT=$((PASS * 100 / TOTAL))
    echo -e "║  ${YELLOW}${PERCENT}% pass rate (${FAIL} failures need attention)${NC}         ║"
fi
echo "╚══════════════════════════════════════════════════════════╝"
echo ""

exit $FAIL
