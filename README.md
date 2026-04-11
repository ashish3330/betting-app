# Lotus Exchange

Lotus Exchange is a full-stack betting platform: a sports betting exchange
(back/lay matching across Match Odds, Bookmaker, Fancy, and Session
markets), a third-party casino aggregator, an agent hierarchy with a
back-office admin panel, and a Next.js 15 frontend. The backend is a Go
microservices stack — one API gateway plus twelve services — and the
integration test suite passes 57 / 57 against `--mode=microservices` as of
commit `e116c28` (`cmd/server`, the legacy monolith, has been deleted).

## Documentation

- [`REFERENCE.md`](REFERENCE.md) — quick-start, full public API surface,
  per-service port / DB / NATS / Redis inventory, seeding, and integration
  tests.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — service topology
  diagram, service responsibility matrix, and data-flow diagrams for bet
  placement, settlement, and deposits.
- [`docs/RUNBOOKS.md`](docs/RUNBOOKS.md) — on-call runbooks for the
  production alerts defined in `deployments/prometheus/alerts.yml`.
- [`docs/SLO.md`](docs/SLO.md) — SLOs, error budgets, burn-rate alerts,
  and escalation policy.

## Prerequisite services

Local development expects the following available on `localhost` (Homebrew
or Docker both work):

- **PostgreSQL 16** (database `bettingdb`, schemas `auth` + `betting`
  created by `migrations/001_initial.sql`)
- **Redis 7+**
- **NATS 2.10+** with JetStream enabled (`nats-server -js`)
- **ClickHouse** (optional; only used by `reporting-service` and
  `admin-service` for analytics rollups when `CLICKHOUSE_URL` is set)
- **Go 1.22+** and **Node.js 20+** for building the backend and frontend

## Quick start

```bash
cp .env.example .env               # fill in secrets
./scripts/start-all.sh             # build + start gateway + 11 services
cd frontend && npm install && npm run dev
```

Then seed test users via the gateway (admin-service handles the route):

```bash
curl -X POST http://localhost:8080/api/v1/seed \
  -H "X-Seed-Secret: $SEED_SECRET"
```

Run the full integration suite against the stack:

```bash
ENCRYPTION_SECRET=$ENCRYPTION_SECRET \
SEED_SECRET=$SEED_SECRET \
  go run ./scripts/api-test --mode=microservices
```

See [`REFERENCE.md`](REFERENCE.md) for the complete route list and
per-service details, and [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
for the topology diagram and data-flow walkthroughs.
