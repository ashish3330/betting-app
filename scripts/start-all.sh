#!/bin/bash
# Start all Lotus Exchange microservices for local development
# Usage: ./scripts/start-all.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

# Load environment variables
if [ -f .env ]; then
  set -a
  source .env
  set +a
  echo "Loaded .env"
else
  echo "Warning: .env file not found, using defaults"
fi

SERVICES=(
  gateway
  auth-service
  wallet-service
  matching-engine
  payment-service
  casino-service
  odds-service
  fraud-service
  reporting-service
  risk-service
  hierarchy-service
  notification-service
  admin-service
)

# ---------------------------------------------------------------
# Build all services
# ---------------------------------------------------------------
echo "Building all services..."
mkdir -p bin

for svc in "${SERVICES[@]}"; do
  if [ -d "cmd/$svc" ]; then
    echo "  Building $svc..."
    go build -o "bin/$svc" "./cmd/$svc"
  else
    echo "  Skipping $svc (cmd/$svc not found)"
  fi
done
echo "Build complete."
echo ""

# ---------------------------------------------------------------
# Start services (gateway last since it depends on the others)
# ---------------------------------------------------------------
PIDS=()

cleanup() {
  echo ""
  echo "Shutting down all services..."
  for pid in "${PIDS[@]}"; do
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid" 2>/dev/null || true
    fi
  done
  wait
  echo "All services stopped."
}

trap cleanup EXIT INT TERM

echo "Starting services..."

for svc in "${SERVICES[@]}"; do
  if [ "$svc" = "gateway" ]; then
    continue  # start gateway last
  fi
  if [ -f "bin/$svc" ]; then
    echo "  Starting $svc..."
    "./bin/$svc" &
    PIDS+=($!)
  fi
done

# Give backend services a moment to initialize
echo "Waiting for backend services to initialize..."
sleep 2

# Start the gateway last
if [ -f "bin/gateway" ]; then
  echo "  Starting gateway..."
  "./bin/gateway" &
  PIDS+=($!)
fi

echo ""
echo "All services started. Press Ctrl+C to stop."
echo ""
echo "  Gateway:              http://localhost:${HTTP_PORT:-8080}"
echo "  Auth Service:         http://localhost:8081"
echo "  Wallet Service:       http://localhost:8082"
echo "  Matching Engine:      http://localhost:8083"
echo "  Payment Service:      http://localhost:8084"
echo "  Casino Service:       http://localhost:8085"
echo "  Odds Service:         http://localhost:8086"
echo "  Fraud Service:        http://localhost:8087"
echo "  Reporting Service:    http://localhost:8088"
echo "  Risk Service:         http://localhost:8089"
echo "  Hierarchy Service:    http://localhost:8090"
echo "  Notification Service: http://localhost:8091"
echo "  Admin Service:        http://localhost:8092"
echo ""

# Wait for all background processes
wait
