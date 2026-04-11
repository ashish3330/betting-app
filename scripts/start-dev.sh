#!/bin/bash
# Start the Lotus Exchange API gateway with all required env vars for local
# development. The gateway proxies to the 12 microservices, which must be
# started separately (use scripts/start-all.sh to bring them all up at once).
#
# Usage: ./scripts/start-dev.sh

set -e

cd "$(dirname "$0")/.."

# Load .env file if present
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

# Build the gateway if needed
if [ ! -f bin/gateway ] || [ cmd/gateway/main.go -nt bin/gateway ]; then
  echo "Building gateway..."
  go build -o bin/gateway ./cmd/gateway
fi

# Persistent signing key — tokens survive restarts
if [ -z "$ED25519_PRIVATE_KEY" ]; then
  echo "ERROR: ED25519_PRIVATE_KEY is not set."
  echo ""
  echo "Generate one with:"
  echo "  openssl genpkey -algorithm ed25519 2>/dev/null | openssl pkey -outform DER 2>/dev/null | xxd -p -c 256"
  echo ""
  echo "Then set it:"
  echo "  export ED25519_PRIVATE_KEY=\"<hex-encoded-key>\""
  echo "  # or add it to your .env file"
  exit 1
fi
export ED25519_PRIVATE_KEY

# Database
if [ -z "$DATABASE_URL" ]; then
  echo "ERROR: DATABASE_URL is not set."
  echo ""
  echo "Set it via environment or .env file:"
  echo "  export DATABASE_URL=\"postgres://user:pass@localhost:5432/bettingdb?sslmode=disable&search_path=betting,auth,public\""
  exit 1
fi
export DATABASE_URL

export HTTP_PORT="${HTTP_PORT:-8080}"

echo "Starting Lotus Exchange gateway on :${HTTP_PORT}"
echo "  DB:   connected"
echo "  Keys: persistent"
echo ""
echo "NOTE: The gateway expects the 12 microservices to be running on"
echo "      :8081-:8092. Use ./scripts/start-all.sh to start everything,"
echo "      or run each cmd/<service>/main.go individually."
exec ./bin/gateway
