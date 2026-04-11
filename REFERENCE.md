# Lotus Exchange — Reference

Lotus Exchange is a full-stack online betting platform: a sports betting
exchange (back/lay matching across Match Odds, Bookmaker, Fancy and Session
markets), a third-party casino aggregator, a multi-level agent hierarchy
(superadmin → admin → master → agent → client), a back-office admin panel, and
a Next.js 15 frontend. The backend is a Go microservices stack: an API
**gateway** on port 8080 proxies every request to one of **twelve** downstream
services on ports 8081–8092. Only the odds feed is mocked (by
`internal/odds/mock.go`, driven by geometric Brownian motion for prices and a
Poisson process for scores); everything else — users, wallets, bets, ledger,
deposits, withdrawals, KYC, casino sessions, notifications, audit — is
persisted in **PostgreSQL 16** (schemas `auth` and `betting`) with **Redis 7**
for hot state (balances, exposure, order books, OTPs, JWT blacklist, rate
limits) and **NATS 2.10+ / JetStream** for service-to-service events. An
optional **ClickHouse** tier powers reporting/analytics when `CLICKHOUSE_URL`
is set.

The canonical implementation of every route now lives in `cmd/*-service` +
`cmd/gateway`. The legacy monolith at `cmd/server` was deleted in commit
`e116c28`; no route on the public API still flows through it. The integration
test suite `scripts/api-test/main.go` runs **57 of 57** tests green against
`--mode=microservices`.

See also:

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — service topology, NATS
  subjects, and data-flow diagrams.
- [`docs/RUNBOOKS.md`](docs/RUNBOOKS.md) — on-call runbooks.
- [`docs/SLO.md`](docs/SLO.md) — service level objectives and burn-rate alerts.

---

## Architecture

### Service inventory

All services share one Postgres instance (schemas `auth` + `betting`), one
Redis instance, and one NATS cluster. The "owns DB tables" column lists the
tables each service has primary write responsibility for — other services may
read from the same schema but mutations are expected to go through the owner.

| Service                  | Command                    | Port  | Owns DB tables                                                                                                      | NATS subjects (pub / sub)                                                                                                         | Redis keys                                                                               |
| ------------------------ | -------------------------- | ----- | ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| API Gateway              | `cmd/gateway`              | 8080  | _none_ (reads `auth.users` / `auth.user_sessions` for JWT validation only)                                          | _none_                                                                                                                            | _none_ (uses Redis only for the shared auth blacklist check)                             |
| Auth Service             | `cmd/auth-service`         | 8081  | `auth.users`, `auth.user_sessions`                                                                                  | pub: `auth.register`, `auth.login`, `auth.logout`                                                                                 | `user:<id>`, `blacklist:<token>`, `otp:<userID>`                                         |
| Wallet Service           | `cmd/wallet-service`       | 8082  | `betting.ledger`, `betting.wallet_accounts`                                                                         | sub (request/reply): `wallet.balance`, `wallet.hold`, `wallet.release`, `wallet.deposit`, `wallet.settle`                         | `wallet:balance:<id>`, `exposure:user:<id>`                                              |
| Matching Engine          | `cmd/matching-engine`      | 8083  | `betting.bets`, `betting.bet_fills`, `betting.markets`, `betting.runners`                                           | pub (JetStream `BETS` stream, subjects `bets.>`): `bets.placed`, `bets.cancelled`, `bets.settled`                                 | `ob:{marketID}:back:z`, `ob:{marketID}:back:h`, `ob:{marketID}:lay:z`, `ob:{marketID}:lay:h`, `market:suspended:{marketID}` |
| Payment Service          | `cmd/payment-service`      | 8084  | `betting.payment_transactions`, `betting.deposit_requests`, `betting.bank_accounts`, `betting.daily_account_usage`  | _(settles via `wallet.deposit` request/reply to wallet-service)_                                                                  | _(no dedicated keys; shares auth blacklist)_                                             |
| Casino Service           | `cmd/casino-service`       | 8085  | `betting.casino_providers`, `betting.casino_games`, `betting.casino_sessions`, `betting.casino_bets`                | _(none today; settlement webhook writes directly + calls wallet)_                                                                 | _(none)_                                                                                 |
| Odds Service             | `cmd/odds-service`         | 8086  | `betting.sports`, `betting.competitions`, `betting.events`                                                          | pub (JetStream `ODDS` stream, subject `odds.update.>`): `odds.update.<marketID>`                                                  | `odds:market:<marketID>` (30 s TTL), `odds:latest:<marketID>`                            |
| Fraud Service            | `cmd/fraud-service`        | 8087  | `betting.fraud_alerts`                                                                                              | sub: `bets.placed`, `auth.login`                                                                                                  | `freq:<userID>`, `fraud:ip:<userID>`, `fraud:device:<userID>`, `fraud:arb:<userID>`      |
| Reporting Service        | `cmd/reporting-service`    | 8088  | _(read-only over `betting.bets` / `betting.ledger`; writes analytics into ClickHouse if configured)_                 | _(none)_                                                                                                                          | _(none)_                                                                                 |
| Risk Service             | `cmd/risk-service`         | 8089  | _(read-only over `betting.bets` + Redis exposure hashes)_                                                            | _(none)_                                                                                                                          | reads `exposure:user:<id>`                                                               |
| Hierarchy Service        | `cmd/hierarchy-service`    | 8090  | `auth.users` (ltree `path` column), `betting.responsible_gambling`, KYC tables                                      | _(none today)_                                                                                                                    | `exclusion:<userID>`, `session:start:<userID>`                                           |
| Notification Service     | `cmd/notification-service` | 8091  | `betting.notifications`                                                                                             | sub: `bets.settled`, `payment.deposit.completed`, `auth.login`                                                                    | _(none)_                                                                                 |
| Admin Service            | `cmd/admin-service`        | 8092  | `betting.audit_log`, `betting.markets` (mutations via admin panel)                                                  | _(none; drives matching/wallet/hierarchy via in-process calls on its pod since the seed bootstrap needs transactional writes)_    | _(none)_                                                                                 |

**Gateway routing table.** `cmd/gateway/main.go` owns the prefix → service map.
The gateway is the only service exposed to the public network; every
downstream listener is intended to be cluster-internal. The gateway validates
JWTs (via the shared `internal/auth` package + `auth.users` / Redis
`blacklist:`), injects `X-User-ID`, `X-Username` and `X-Role` headers, and
reverse-proxies the request to the correct backend.

### Shared infrastructure

- **PostgreSQL 16** — single instance, schemas `auth` and `betting` (+
  `public`). Migrations live under `migrations/` and run in numeric order
  (`001_initial.sql` … `011_notification_idempotency_unique.sql`). Each
  service opens its own pool with `search_path=betting,auth,public`.
- **Redis 7** — shared by every service; used for balance cache, exposure
  hashes, order-book ZSets/hashes (matching engine), JWT blacklist, OTP
  cache, rate limiter buckets, dedupe idempotency keys, and fraud counters.
- **NATS 2.10+ (JetStream)** — two file-backed streams today: `BETS`
  (`bets.>` subjects, published by matching-engine) and `ODDS`
  (`odds.update.>`, published by odds-service). Core NATS (non-JetStream)
  is used for the wallet request/reply subjects `wallet.*`.
- **ClickHouse** (optional) — reporting-service and admin-service connect
  when `CLICKHOUSE_URL` is set; otherwise the services fall back to
  Postgres-only mode and log `clickhouse not set, using postgres-only mode`
  at startup.

---

## Local development

### Prerequisites

- Go 1.22+
- Node.js 20+ (for `frontend/`)
- PostgreSQL 16 (DB `bettingdb`, schemas created by `migrations/001_initial.sql`)
- Redis 7+
- NATS 2.10+ with JetStream enabled
- (optional) ClickHouse for analytics

Install Homebrew recipes for a single-box dev machine:

```bash
brew install postgresql@16 redis nats-server
brew services start postgresql@16
brew services start redis
nats-server -js &
```

### One-shot boot

Copy `.env.example` to `.env`, fill in secrets, then build and launch every
service with:

```bash
./scripts/start-all.sh
```

`start-all.sh` builds each `cmd/*-service` (plus `cmd/gateway`) into `./bin/`
and starts them as child processes, with the gateway started last so the
health fan-out succeeds on first tick.

> **Note.** `start-all.sh` currently starts 12 processes —
> `gateway` plus 11 services — but **does not** start `admin-service`. If
> you need the admin/panel/seed routes locally, either add
> `admin-service` to the `SERVICES` array in that script or run
> `go run ./cmd/admin-service` in a separate terminal. The integration
> test suite depends on `admin-service` being up because `POST
> /api/v1/seed` is served there.

Each service listens on its own port (see the inventory table above);
`gateway` on `:8080` is the only endpoint the frontend talks to.

### Seeding test users

The bootstrap endpoint lives on `admin-service` and is exposed via the
gateway at `POST /api/v1/seed`. It is a public route (no JWT required) but
gated by a shared header secret:

```bash
curl -X POST http://localhost:8080/api/v1/seed \
  -H "X-Seed-Secret: $SEED_SECRET"
```

Success creates one user per role (superadmin, admin, master, agent,
client) with passwords drawn from `SEED_*_PASSWORD` env vars, plus a few
sample markets. The handler is idempotent — re-running it will upsert the
fixed set of test users rather than duplicating rows.

### Frontend

```bash
cd frontend
npm install
npm run dev          # Next.js on :3000, proxies API calls to localhost:8080
```

### Reset & reseed

`scripts/reset-and-seed.sh` drops the schemas, re-runs all migrations in
order, and calls the seed endpoint — use it when a test run leaves the DB
in an inconsistent state.

---

## Integration tests

The canonical end-to-end test harness is
`scripts/api-test/main.go`. It exercises every public API route via the
gateway with the same AES-256-GCM envelope, JWT auth, and role checks that a
real client would use.

```bash
ENCRYPTION_SECRET=$ENCRYPTION_SECRET \
SEED_SECRET=$SEED_SECRET \
  go run ./scripts/api-test --mode=microservices
```

Against the current `microservice-paradigm` head the suite prints **57 /
57 passed, 0 failed, 0 skipped**. The `endpointsMissingInMicroservices`
map inside `main.go` is intentionally empty now that every monolith
endpoint has been ported to the appropriate microservice — any future
regression on a ported route shows up as a real failure instead of a
silent skip.

`--mode=microservices` is the only supported mode today. The
`--mode=monolith` flag still exists in the source for historical reasons
but targets a binary (`cmd/server`) that no longer exists.

---

## Public API routes

All routes below are the external paths; every one of them is served
through the gateway at `:8080`. The "served by" column is the downstream
service that actually handles the request.

### Auth — `auth-service`

| Method | Path                               | Served by     | Auth  | Notes                                        |
| ------ | ---------------------------------- | ------------- | ----- | -------------------------------------------- |
| POST   | `/api/v1/auth/register`            | auth-service  | no    | Creates a `client`-role user                 |
| POST   | `/api/v1/auth/login`               | auth-service  | no    | Returns access + refresh tokens              |
| POST   | `/api/v1/auth/logout`              | auth-service  | no    | Blacklists the access token in Redis         |
| POST   | `/api/v1/auth/refresh`             | auth-service  | no    | Refresh token rotation                       |
| POST   | `/api/v1/auth/otp/verify`          | auth-service  | no    | Pre-login TOTP challenge                     |
| POST   | `/api/v1/auth/otp/resend`          | auth-service  | no    | Non-enumerating resend                       |
| POST   | `/api/v1/auth/change-password`     | auth-service  | yes   |                                              |
| POST   | `/api/v1/auth/otp/generate`        | auth-service  | yes   | Returns a TOTP secret for enrollment         |
| POST   | `/api/v1/auth/otp/enable`          | auth-service  | yes   | Activates TOTP after verification            |
| GET    | `/api/v1/auth/sessions`            | auth-service  | yes   | List active sessions                         |
| DELETE | `/api/v1/auth/sessions`            | auth-service  | yes   | Sign out of every other session              |
| GET    | `/api/v1/auth/login-history`       | auth-service  | yes   | Paginated login audit                        |

### Wallet — `wallet-service`

| Method | Path                               | Auth |
| ------ | ---------------------------------- | ---- |
| GET    | `/api/v1/wallet/balance`           | yes  |
| GET    | `/api/v1/wallet/ledger`            | yes  |
| GET    | `/api/v1/wallet/statement`         | yes  |
| GET    | `/api/v1/wallet/deposits`          | yes  |
| GET    | `/api/v1/wallet/withdrawals`       | yes  |
| POST   | `/api/v1/wallet/deposit`           | yes  |

Wallet also exposes the NATS subjects `wallet.balance`, `wallet.hold`,
`wallet.release`, `wallet.deposit`, `wallet.settle` (request/reply) — these
are how the matching-engine, payment-service and admin-service mutate
balances.

### Matching engine & bets — `matching-engine`

| Method | Path                                      | Auth |
| ------ | ----------------------------------------- | ---- |
| POST   | `/api/v1/bet/place`                       | yes  |
| DELETE | `/api/v1/bet/{betId}/cancel`              | yes  |
| GET    | `/api/v1/bets`                            | yes  |
| GET    | `/api/v1/bets/history`                    | yes  |
| GET    | `/api/v1/positions/{marketId}`            | yes  |
| GET    | `/api/v1/market/{marketId}/orderbook`     | no   |
| POST   | `/api/v1/cashout/offer`                   | yes  |
| POST   | `/api/v1/cashout/accept`                  | yes  |
| GET    | `/api/v1/cashout/offers`                  | yes  |

The matching engine publishes `bets.placed` on `POST /api/v1/bet/place`
and `bets.cancelled` on `DELETE .../cancel` via JetStream.

### Payments — `payment-service`

| Method | Path                                       | Auth | Notes                           |
| ------ | ------------------------------------------ | ---- | ------------------------------- |
| POST   | `/api/v1/payment/deposit/upi`              | yes  |                                 |
| POST   | `/api/v1/payment/deposit/crypto`           | yes  |                                 |
| POST   | `/api/v1/payment/withdraw`                 | yes  |                                 |
| GET    | `/api/v1/payment/transactions`             | yes  |                                 |
| GET    | `/api/v1/payment/transaction/{id}`         | yes  |                                 |
| POST   | `/api/v1/payment/webhook/razorpay`         | no   | HMAC signature verified in-proc |
| POST   | `/api/v1/payment/webhook/crypto`           | no   | HMAC signature verified in-proc |

### Casino — `casino-service`

| Method | Path                                         | Auth | Notes                              |
| ------ | -------------------------------------------- | ---- | ---------------------------------- |
| GET    | `/api/v1/casino/providers`                   | no   |                                    |
| GET    | `/api/v1/casino/games`                       | no   |                                    |
| GET    | `/api/v1/casino/categories`                  | no   |                                    |
| GET    | `/api/v1/casino/games/{category}`            | no   |                                    |
| POST   | `/api/v1/casino/session`                     | yes  | Opens a provider session           |
| GET    | `/api/v1/casino/session/{id}`                | yes  |                                    |
| DELETE | `/api/v1/casino/session/{id}`                | yes  |                                    |
| GET    | `/api/v1/casino/history`                     | yes  |                                    |
| POST   | `/api/v1/casino/webhook/settlement`          | no   | Provider-signed settlement callback |

### Odds / sports feed — `odds-service`

| Method | Path                                         | Auth |
| ------ | -------------------------------------------- | ---- |
| GET    | `/api/v1/sports`                             | no   |
| GET    | `/api/v1/competitions`                       | no   |
| GET    | `/api/v1/events`                             | no   |
| GET    | `/api/v1/events/{id}/markets`                | no   |
| GET    | `/api/v1/markets`                            | no   |
| GET    | `/api/v1/markets/{id}/odds`                  | no   |
| GET    | `/api/v1/odds/status`                        | no   |
| GET    | `/ws`                                        | yes  |
| GET    | `/api/v1/config`                             | no   |

The WebSocket at `/ws` is accepted by the gateway, JWT is validated against
the `access_token` cookie (or `?token=` query), and the frames are proxied
to `odds-service` which streams odds updates published on
`odds.update.<marketID>`.

### Hierarchy, referral, responsible-gambling, KYC — `hierarchy-service`

| Method | Path                                                  | Auth |
| ------ | ----------------------------------------------------- | ---- |
| GET    | `/api/v1/hierarchy/children`                          | yes  |
| GET    | `/api/v1/hierarchy/children/direct`                   | yes  |
| POST   | `/api/v1/hierarchy/credit/transfer`                   | yes  |
| GET    | `/api/v1/hierarchy/user/{id}`                         | yes  |
| PUT    | `/api/v1/hierarchy/user/{id}/status`                  | yes  |
| GET    | `/api/v1/referral/code`                               | yes  |
| GET    | `/api/v1/referral/stats`                              | yes  |
| GET    | `/api/v1/responsible-gambling/limits`                 | yes  |
| PUT    | `/api/v1/responsible-gambling/limits`                 | yes  |
| POST   | `/api/v1/responsible-gambling/self-exclude`           | yes  |
| POST   | `/api/v1/responsible-gambling/cooling-off`            | yes  |
| GET    | `/api/v1/responsible-gambling/session`                | yes  |
| GET    | `/api/v1/responsible/limits`                          | yes  |
| PUT    | `/api/v1/responsible/limits`                          | yes  |
| POST   | `/api/v1/responsible/self-exclude`                    | yes  |
| POST   | `/api/v1/responsible/cooling-off`                     | yes  |
| GET    | `/api/v1/responsible/session`                         | yes  |
| GET    | `/api/v1/kyc/status`                                  | yes  |
| POST   | `/api/v1/kyc/submit`                                  | yes  |
| GET    | `/api/v1/admin/kyc/pending`                           | yes + admin |
| POST   | `/api/v1/admin/kyc/{id}/approve`                      | yes + admin |
| POST   | `/api/v1/admin/kyc/{id}/reject`                       | yes + admin |

Both `/api/v1/responsible/` and `/api/v1/responsible-gambling/` prefixes
resolve to the same handlers; the shorter alias exists because the original
monolith and the current Next.js frontend use it.

### Notifications — `notification-service`

| Method | Path                                             | Auth |
| ------ | ------------------------------------------------ | ---- |
| GET    | `/api/v1/notifications`                          | yes  |
| GET    | `/api/v1/notifications/unread-count`             | yes  |
| POST   | `/api/v1/notifications/{id}/read`                | yes  |
| POST   | `/api/v1/notifications/read-all`                 | yes  |

Notifications are created reactively on `bets.settled`,
`payment.deposit.completed`, and `auth.login` NATS events. There is no
write API — every notification row is the side-effect of an event.

### Risk — `risk-service`

| Method | Path                                  | Auth |
| ------ | ------------------------------------- | ---- |
| GET    | `/api/v1/risk/market/{id}`            | yes  |
| GET    | `/api/v1/risk/user/{id}`              | yes  |
| GET    | `/api/v1/risk/exposure`               | yes  |

### Fraud — `fraud-service`

| Method | Path                                             | Auth          |
| ------ | ------------------------------------------------ | ------------- |
| GET    | `/api/v1/fraud/alerts`                           | yes + admin   |
| POST   | `/api/v1/fraud/alerts/{id}/resolve`              | yes + admin   |
| GET    | `/api/v1/fraud/user/{id}/risk`                   | yes + admin   |

### Reporting — `reporting-service`

| Method | Path                                      | Auth        |
| ------ | ----------------------------------------- | ----------- |
| GET    | `/api/v1/reports/pnl`                     | yes         |
| GET    | `/api/v1/reports/market/{id}`             | yes + admin |
| GET    | `/api/v1/reports/dashboard`               | yes + admin |
| GET    | `/api/v1/reports/volume`                  | yes + admin |
| GET    | `/api/v1/reports/hierarchy/pnl`           | yes + admin |

### Admin & operator panel — `admin-service`

`admin-service` hosts three logically distinct route groups:

- `/api/v1/admin/*` — superadmin + admin only. Full platform mutations.
- `/api/v1/panel/*` — any non-client (superadmin, admin, master, agent),
  scoped to the caller's downline.
- `/api/v1/seed` — public (header-secret gated) — dev-only bootstrap.

| Method | Path                                              | Auth        |
| ------ | ------------------------------------------------- | ----------- |
| POST   | `/api/v1/seed`                                    | `X-Seed-Secret` header only |
| GET    | `/api/v1/admin/dashboard`                         | yes + admin |
| GET    | `/api/v1/admin/users`                             | yes + admin |
| GET    | `/api/v1/admin/users/{id}`                        | yes + admin |
| PUT    | `/api/v1/admin/users/{id}/status`                 | yes + admin |
| PUT    | `/api/v1/admin/users/{id}/credit-limit`           | yes + admin |
| PUT    | `/api/v1/admin/users/{id}/commission`             | yes + admin |
| GET    | `/api/v1/admin/markets`                           | yes + admin |
| POST   | `/api/v1/admin/markets/{id}/suspend`              | yes + admin |
| POST   | `/api/v1/admin/markets/{id}/resume`               | yes + admin |
| POST   | `/api/v1/admin/markets/{id}/settle`               | yes + admin |
| POST   | `/api/v1/admin/markets/{id}/void`                 | yes + admin |
| GET    | `/api/v1/admin/bets`                              | yes + admin |
| GET    | `/api/v1/admin/reports/pnl`                       | yes + admin |
| GET    | `/api/v1/admin/reports/volume`                    | yes + admin |
| GET    | `/api/v1/admin/fraud/alerts`                      | yes + admin |
| GET    | `/api/v1/panel/dashboard`                         | yes, non-client |
| GET    | `/api/v1/panel/users`                             | yes, non-client |
| GET    | `/api/v1/panel/audit`                             | yes, non-client |
| GET    | `/api/v1/panel/reports/pnl`                       | yes, non-client |
| GET    | `/api/v1/panel/reports/volume`                    | yes, non-client |

---

## Frontend

The browser app is a Next.js 15 project rooted at `frontend/` using the
App Router (`frontend/src/app/`), TypeScript, and Tailwind. It talks to
the gateway only — `NEXT_PUBLIC_API_URL` defaults to `http://localhost:8080`
and `NEXT_PUBLIC_WS_URL` to `ws://localhost:8081` in `.env.example`. Every
API call is wrapped in the AES-256-GCM envelope defined in
`frontend/src/lib/api.ts` so the browser sees the same ciphertext shape as
`scripts/api-test/main.go`.

Key pages:

- `frontend/src/app/login` — credential + OTP flow.
- `frontend/src/app/sports`, `.../virtual-sports` — odds + bet slip.
- `frontend/src/app/casino/*` — catalog and per-game launcher.
- `frontend/src/app/wallet`, `.../account/*` — balance, statement, KYC, referrals.
- `frontend/src/app/panel/*` — operator panel (agents/masters/admins).

---

## Security model

- **Transport envelope.** All `POST` / `PUT` / `DELETE` bodies and every
  non-trivial `GET` response are wrapped in an AES-256-GCM envelope
  (`{"d": "<base64(nonce||ciphertext)>"}`). Key derivation is
  `SHA-256(ENCRYPTION_SECRET)`. The middleware lives in
  `internal/middleware` and is applied on every downstream service (the
  gateway itself deliberately skips it so responses are not double-encrypted).
- **JWT.** ED25519 signed, 15 min access / 168 h refresh. Revocation is
  done via a Redis `blacklist:<token>` key with TTL matching the token
  remaining lifetime. The gateway validates tokens for every
  `requireAuth: true` route and forwards `X-User-ID` / `X-Username` /
  `X-Role` headers to downstream services.
- **Role hierarchy.** `superadmin → admin → master → agent → client`.
  Credit can only flow downward through the tree; uplines can transfer
  credit to direct children via `/api/v1/hierarchy/credit/transfer`.
