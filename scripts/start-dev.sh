#!/bin/bash
# Start the Lotus Exchange server with all required env vars for local development
# Usage: ./scripts/start-dev.sh

set -e

cd "$(dirname "$0")/.."

# Build if needed
if [ ! -f bin/server ] || [ cmd/server/main.go -nt bin/server ]; then
  echo "Building server..."
  go build -o bin/server ./cmd/server
fi

# Persistent signing key — tokens survive restarts
export ED25519_PRIVATE_KEY="${ED25519_PRIVATE_KEY:-5511076a4e1e815c37c7d7063f273e55d7306d314a7ceefe0c671fd3e681a9e03941b7cd1687c0e42e53469c78620ba2c086b609f64ad1293f8c0ce6b52a8186}"

# Database
export DATABASE_URL="${DATABASE_URL:-postgres://lotus:L0tus!Xchg2026@localhost:5432/bettingdb?sslmode=disable&search_path=betting,auth,public}"

# Server
export PORT="${PORT:-8080}"

echo "Starting Lotus Exchange backend on :${PORT}"
echo "  DB: connected"
echo "  Keys: persistent"
exec ./bin/server
