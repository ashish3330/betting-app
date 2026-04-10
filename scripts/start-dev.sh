#!/bin/bash
# Start the Lotus Exchange server with all required env vars for local development
# Usage: ./scripts/start-dev.sh

set -e

cd "$(dirname "$0")/.."

# Load .env file if present
if [ -f .env ]; then
  set -a
  source .env
  set +a
fi

# Build if needed
if [ ! -f bin/server ] || [ cmd/server/main.go -nt bin/server ]; then
  echo "Building server..."
  go build -o bin/server ./cmd/server
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

# Server
export PORT="${PORT:-8080}"

echo "Starting Lotus Exchange backend on :${PORT}"
echo "  DB: connected"
echo "  Keys: persistent"
exec ./bin/server
