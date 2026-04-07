#!/bin/bash
# Lotus Exchange - Demo Setup Script
# Usage: ./scripts/demo.sh

set -e

echo "╔══════════════════════════════════════════════╗"
echo "║     🏏 Lotus Exchange - Demo Setup           ║"
echo "╠══════════════════════════════════════════════╣"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$PROJECT_DIR"

# Step 1: Start infrastructure
echo -e "\n${CYAN}[1/5] Starting infrastructure (Postgres, Redis, NATS, ClickHouse)...${NC}"
docker compose -f deployments/docker-compose.yml up -d postgres redis nats clickhouse
echo -e "${GREEN}✓ Infrastructure started${NC}"

# Wait for Postgres
echo -e "\n${CYAN}[2/5] Waiting for PostgreSQL to be ready...${NC}"
for i in {1..30}; do
    if docker compose -f deployments/docker-compose.yml exec -T postgres pg_isready -U lotus -d bettingdb >/dev/null 2>&1; then
        echo -e "${GREEN}✓ PostgreSQL ready${NC}"
        break
    fi
    echo "  Waiting... ($i/30)"
    sleep 2
done

# Step 3: Run migrations
echo -e "\n${CYAN}[3/5] Running database migrations...${NC}"
docker compose -f deployments/docker-compose.yml exec -T postgres psql -U lotus -d bettingdb -f /docker-entrypoint-initdb.d/001_initial.sql 2>/dev/null || echo "  (migrations may have already been applied)"
docker compose -f deployments/docker-compose.yml exec -T postgres psql -U lotus -d bettingdb -f /docker-entrypoint-initdb.d/002_phase2.sql 2>/dev/null || echo "  (phase 2 may have already been applied)"
echo -e "${GREEN}✓ Migrations applied${NC}"

# Step 4: Seed demo data
echo -e "\n${CYAN}[4/5] Seeding demo data...${NC}"
cat scripts/seed_demo.sql | docker compose -f deployments/docker-compose.yml exec -T postgres psql -U lotus -d bettingdb
echo -e "${GREEN}✓ Demo data seeded${NC}"

# Step 5: Build and start the gateway
echo -e "\n${CYAN}[5/5] Building and starting gateway...${NC}"
go build -o bin/gateway ./cmd/gateway
echo -e "${GREEN}✓ Gateway built${NC}"

echo ""
echo -e "╔══════════════════════════════════════════════╗"
echo -e "║  ${GREEN}Demo Ready!${NC}                                 ║"
echo -e "╠══════════════════════════════════════════════╣"
echo -e "║                                              ║"
echo -e "║  ${YELLOW}API:${NC}       http://localhost:8080             ║"
echo -e "║  ${YELLOW}Health:${NC}    http://localhost:8080/health      ║"
echo -e "║  ${YELLOW}WebSocket:${NC} ws://localhost:8080/ws            ║"
echo -e "║  ${YELLOW}Frontend:${NC}  http://localhost:3000             ║"
echo -e "║                                              ║"
echo -e "║  ${CYAN}Demo Users:${NC}                                  ║"
echo -e "║  superadmin / admin123  (SuperAdmin)         ║"
echo -e "║  admin1     / demo1234  (Admin)              ║"
echo -e "║  master1    / demo1234  (Master)             ║"
echo -e "║  agent1     / demo1234  (Agent)              ║"
echo -e "║  player1    / demo1234  (Client, ₹59,310)    ║"
echo -e "║  player2    / demo1234  (Client, ₹20,000)    ║"
echo -e "║  player3    / demo1234  (Client, ₹72,150)    ║"
echo -e "║                                              ║"
echo -e "║  ${CYAN}Live Markets:${NC}                                ║"
echo -e "║  MI vs CSK  (IN-PLAY, ₹25L matched)         ║"
echo -e "║  RCB vs KKR (Upcoming, 3hrs)                 ║"
echo -e "║  DC vs SRH  (Tomorrow)                       ║"
echo -e "║  GT vs LSG  (Settled - GT won)               ║"
echo -e "║                                              ║"
echo -e "╚══════════════════════════════════════════════╝"
echo ""
echo -e "${YELLOW}Starting gateway server...${NC}"
echo ""

./bin/gateway
