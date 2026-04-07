# Lotus Exchange — Betting Platform

## Quick Start

```bash
# 1. Start PostgreSQL, Redis, NATS (via Homebrew or Docker)
# 2. Start server
./scripts/start-dev.sh

# 3. Start frontend
cd frontend && npm run dev

# 4. Reset DB + seed users (optional)
./scripts/reset-and-seed.sh
```

## Architecture

- **Backend**: Go — `cmd/server/` (85 endpoints, AES-256-GCM encrypted API)
- **Frontend**: Next.js 15 + Tailwind — `frontend/`
- **Database**: PostgreSQL 16 with ltree hierarchy
- **Cache**: Redis 7
- **Messaging**: NATS with JetStream

## User Hierarchy

superadmin → admin → master → agent → client (player)

## Key Features

- Match Odds / Bookmaker / Fancy / Session markets
- Inline bet slip with 2-step confirmation
- Real-time odds fluctuation + live scores
- Deposit flow with UTR extraction (Tesseract OCR)
- Full admin panel with hierarchy, credit transfer, audit
- AES-256-GCM encryption on all API payloads
- ED25519 JWT authentication

## Odds

Mock data by default. Set `ODDS_API_KEY` env var for The Odds API v4 integration.

## Scripts

| Script | Purpose |
|--------|---------|
| `scripts/start-dev.sh` | Start backend with persistent keys |
| `scripts/reset-and-seed.sh` | Reset DB + create test users |
