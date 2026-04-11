# Lotus Exchange — Architecture

This document describes the runtime topology of the Lotus Exchange backend
after the monolith-to-microservices migration (commit `e116c28`,
`cmd/server` deleted). It should be read alongside
[`../REFERENCE.md`](../REFERENCE.md), which lists the full public API
surface.

---

## 1. System topology

```
                         ┌──────────────────────────┐
                         │  Browser (Next.js 15)    │
                         │  frontend/src/app/*      │
                         └────────────┬─────────────┘
                                      │  HTTPS + AES-256-GCM envelope
                                      │  (+ ws://…/ws for odds stream)
                                      ▼
                         ┌──────────────────────────┐
                         │  cmd/gateway  :8080      │
                         │  - JWT validation        │
                         │  - X-User-ID injection   │
                         │  - Reverse proxy         │
                         │  - WS proxy (/ws → odds) │
                         └────────────┬─────────────┘
                                      │  HTTP (cluster-internal)
                                      │
  ┌─────────────────┬─────────────────┼─────────────────┬──────────────────┐
  │                 │                 │                 │                  │
  ▼                 ▼                 ▼                 ▼                  ▼
┌──────────┐  ┌──────────┐  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐
│ auth     │  │ wallet   │  │ matching-    │  │ payment      │  │ casino        │
│ :8081    │  │ :8082    │  │ engine :8083 │  │ :8084        │  │ :8085         │
└────┬─────┘  └────┬─────┘  └──────┬───────┘  └──────┬───────┘  └──────┬────────┘
     │             │               │                 │                 │
  ┌──────────┐  ┌──────────┐  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐
  │ odds     │  │ fraud    │  │ reporting    │  │ risk         │  │ hierarchy     │
  │ :8086    │  │ :8087    │  │ :8088        │  │ :8089        │  │ :8090         │
  └────┬─────┘  └────┬─────┘  └──────┬───────┘  └──────┬───────┘  └──────┬────────┘
       │             │               │                 │                 │
                              ┌──────────────┐  ┌──────────────┐
                              │ notification │  │ admin        │
                              │ :8091        │  │ :8092        │
                              └──────┬───────┘  └──────┬───────┘
                                     │                 │
─────────────────────────────────────┼─────────────────┼──────────────────
                                     ▼                 ▼
         ┌──────────────────────────────────────────────────────────┐
         │                     Shared infrastructure                │
         │                                                          │
         │  ┌──────────────┐   ┌──────────┐   ┌──────────────────┐  │
         │  │ PostgreSQL 16│   │ Redis 7  │   │ NATS 2.10 + JS   │  │
         │  │ schemas:     │   │ wallet:* │   │ streams:         │  │
         │  │  auth        │   │ user:*   │   │   BETS  (bets.>) │  │
         │  │  betting     │   │ blacklist│   │   ODDS  (odds.>) │  │
         │  │              │   │ otp:*    │   │ core subjects:   │  │
         │  │              │   │ ob:*     │   │   wallet.*       │  │
         │  └──────────────┘   └──────────┘   └──────────────────┘  │
         │                                                          │
         │  ┌──────────────────────────────┐                        │
         │  │ ClickHouse (optional)        │  ← reporting-service,  │
         │  │ lotus_analytics              │     admin-service      │
         │  └──────────────────────────────┘                        │
         └──────────────────────────────────────────────────────────┘
```

**Connectivity rules.**

- The gateway is the **only** service reachable from outside the cluster.
- Every downstream service opens its own connection pools to Postgres,
  Redis, and (most of them) NATS. The gateway connects to Postgres + Redis
  only for JWT validation and the shared token blacklist.
- Service-to-service synchronous traffic is rare: the admin-service uses
  in-process calls to the `internal/wallet`, `internal/hierarchy`, and
  `internal/matching` packages on its own pod because the seed bootstrap
  needs transactional writes; everything else is event-driven over NATS.
- JetStream streams are file-storage backed: `BETS` captures everything
  published under `bets.>` (matching-engine → fraud/notification
  consumers); `ODDS` captures `odds.update.>` (odds-service →
  browser via the gateway WS proxy).

---

## 2. Service responsibility matrix

| Service                | Port  | Owns routes prefix                                                                                                         | Owns DB schema / tables                                                                                             | NATS subscribes                                                                                        | NATS publishes                                          |
| ---------------------- | ----- | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------- |
| `cmd/gateway`          | 8080  | _reverse-proxies everything_ (`/api/*`, `/ws`, `/health`, `/metrics`)                                                      | — (reads `auth.users` / `auth.user_sessions` for JWT only)                                                          | —                                                                                                      | —                                                       |
| `auth-service`         | 8081  | `/api/v1/auth/*`                                                                                                           | `auth.users`, `auth.user_sessions`                                                                                  | —                                                                                                      | `auth.register`, `auth.login`, `auth.logout`            |
| `wallet-service`       | 8082  | `/api/v1/wallet/*`                                                                                                         | `betting.ledger`, `betting.wallet_accounts`                                                                         | `wallet.balance`, `wallet.hold`, `wallet.release`, `wallet.deposit`, `wallet.settle` (request/reply)   | —                                                       |
| `matching-engine`      | 8083  | `/api/v1/bet/*`, `/api/v1/bets`, `/api/v1/positions/*`, `/api/v1/cashout/*`, `/api/v1/market/*/orderbook`                  | `betting.bets`, `betting.bet_fills`, `betting.markets`, `betting.runners`                                           | —                                                                                                      | `bets.placed`, `bets.cancelled`, `bets.settled` (JetStream `BETS`) |
| `payment-service`      | 8084  | `/api/v1/payment/*`                                                                                                        | `betting.payment_transactions`, `betting.deposit_requests`, `betting.bank_accounts`, `betting.daily_account_usage`  | —                                                                                                      | _(calls `wallet.deposit` request/reply; no direct pub)_ |
| `casino-service`       | 8085  | `/api/v1/casino/*`                                                                                                         | `betting.casino_providers`, `betting.casino_games`, `betting.casino_sessions`, `betting.casino_bets`                | —                                                                                                      | —                                                       |
| `odds-service`         | 8086  | `/api/v1/sports`, `/api/v1/competitions`, `/api/v1/events*`, `/api/v1/markets*`, `/api/v1/odds/*`, `/api/v1/config`, `/ws` | `betting.sports`, `betting.competitions`, `betting.events`                                                          | —                                                                                                      | `odds.update.<marketID>` (JetStream `ODDS`)             |
| `fraud-service`        | 8087  | `/api/v1/fraud/*`                                                                                                          | `betting.fraud_alerts`                                                                                              | `bets.placed`, `auth.login`                                                                            | —                                                       |
| `reporting-service`    | 8088  | `/api/v1/reports/*`                                                                                                        | _(read-only over `betting.bets`, `betting.ledger`; writes aggregates to ClickHouse when configured)_                 | —                                                                                                      | —                                                       |
| `risk-service`         | 8089  | `/api/v1/risk/*`                                                                                                           | _(read-only, reads `exposure:user:<id>` Redis hashes + `betting.bets`)_                                              | —                                                                                                      | —                                                       |
| `hierarchy-service`    | 8090  | `/api/v1/hierarchy/*`, `/api/v1/referral/*`, `/api/v1/responsible*/*`, `/api/v1/kyc/*`, `/api/v1/admin/kyc/*`              | `auth.users` (ltree `path`), `betting.responsible_gambling`, KYC tables                                             | —                                                                                                      | —                                                       |
| `notification-service` | 8091  | `/api/v1/notifications`, `/api/v1/notifications/*`                                                                         | `betting.notifications`                                                                                             | `bets.settled`, `payment.deposit.completed`, `auth.login`                                              | —                                                       |
| `admin-service`        | 8092  | `/api/v1/admin/*`, `/api/v1/panel/*`, `/api/v1/seed`, `/api/v1/fraud/*` (alias)                                            | `betting.audit_log` + mutations on `betting.markets` via settlement/suspend routes                                  | —                                                                                                      | —                                                       |

---

## 3. Data flows

### 3.1 Bet placement

Every bet that lands on a market travels this path:

```
Browser                Gateway            matching-engine        wallet-service        fraud-service     notification-service
   │                      │                     │                      │                    │                     │
   │  POST /api/v1/       │                     │                      │                    │                     │
   │  bet/place           │                     │                      │                    │                     │
   ├─────────────────────▶│                     │                      │                    │                     │
   │  { envelope }        │  validate JWT       │                      │                    │                     │
   │                      │  inject X-User-ID   │                      │                    │                     │
   │                      ├────────────────────▶│                      │                    │                     │
   │                      │                     │ NATS req/rep         │                    │                     │
   │                      │                     │ wallet.hold          │                    │                     │
   │                      │                     ├─────────────────────▶│                    │                     │
   │                      │                     │                      │ Postgres TX:       │                     │
   │                      │                     │                      │  UPDATE ledger     │                     │
   │                      │                     │                      │  SET balance,      │                     │
   │                      │                     │                      │      exposure      │                     │
   │                      │                     │◀─────────────────────┤ ok                 │                     │
   │                      │                     │                      │                    │                     │
   │                      │                     │ INSERT betting.bets  │                    │                     │
   │                      │                     │ Redis Lua match:     │                    │                     │
   │                      │                     │  ob:{m}:{side}:z/h   │                    │                     │
   │                      │                     │                      │                    │                     │
   │                      │                     │ JetStream publish:   │                    │                     │
   │                      │                     │ bets.placed          │                    │                     │
   │                      │                     ├──────────────────────┴───────────────────▶│                     │
   │                      │                     │                                          fraud.AnalyzeBet       │
   │                      │                     │                                           (Redis counters,      │
   │                      │                     │                                            INSERT fraud_alerts) │
   │                      │                     │                                                                 │
   │                      │◀────────────────────┤ 200 OK                                                          │
   │◀─────────────────────┤ encrypted response  │                                                                 │
```

`bets.placed` fan-out consumers today:

- `fraud-service` — runs bet-pattern analysis (`fraud.AnalyzeBet`) and may
  write to `betting.fraud_alerts`.
- (Future) any other service that subscribes to the `BETS` stream.

`notification-service` does **not** subscribe to `bets.placed` — only to
`bets.settled`, because players care about settlement not placement.

### 3.2 Settlement

```
Admin UI / cron        Gateway         admin-service           matching-engine         wallet-service       notification-service
    │                     │                  │                        │                        │                     │
    │ POST /api/v1/       │                  │                        │                        │                     │
    │ admin/markets/{id}/ │                  │                        │                        │                     │
    │ settle              │                  │                        │                        │                     │
    ├────────────────────▶│                  │                        │                        │                     │
    │                     │ validate JWT     │                        │                        │                     │
    │                     │ require admin    │                        │                        │                     │
    │                     ├─────────────────▶│                        │                        │                     │
    │                     │                  │ (in-process call)      │                        │                     │
    │                     │                  │ settlement.SettleMarket│                        │                     │
    │                     │                  ├───────────────────────▶│                        │                     │
    │                     │                  │                        │ loop over winners:     │                     │
    │                     │                  │                        │ NATS wallet.settle     │                     │
    │                     │                  │                        ├───────────────────────▶│                     │
    │                     │                  │                        │                        │ Postgres TX:        │
    │                     │                  │                        │                        │  credit winnings,   │
    │                     │                  │                        │                        │  commission,        │
    │                     │                  │                        │                        │  update ledger      │
    │                     │                  │                        │◀───────────────────────┤ ok                  │
    │                     │                  │                        │                                              │
    │                     │                  │                        │ JetStream publish:                           │
    │                     │                  │                        │ bets.settled { user_id, bet_id, amount }     │
    │                     │                  │                        ├─────────────────────────────────────────────▶│
    │                     │                  │                        │                                              │ INSERT
    │                     │                  │                        │                                              │ betting.notifications
    │                     │◀─────────────────┤                        │                                              │ ("Bet Settled")
    │◀────────────────────┤ 200 OK           │                        │                                              │
```

The admin-service co-locates `settlement.NewService` + `wallet.NewService`
+ `matching.NewEngine` on its own pod so the settlement loop can open a
single Postgres transaction across bets, ledger, and audit-log writes.
Wallet credits still route through NATS so the wallet-service remains the
single writer into `betting.ledger` balance rows.

### 3.3 Deposit (UPI / Razorpay)

```
Browser              Gateway         payment-service          Razorpay            wallet-service         notification-service
   │                    │                   │                     │                      │                     │
   │ POST /api/v1/      │                   │                     │                      │                     │
   │ payment/           │                   │                     │                      │                     │
   │ deposit/upi        │                   │                     │                      │                     │
   ├───────────────────▶│──────────────────▶│                     │                      │                     │
   │                    │                   │ INSERT pending tx   │                      │                     │
   │                    │                   │ return UPI intent   │                      │                     │
   │◀───────────────────┤                   │                     │                      │                     │
   │ user pays in UPI app ─────────────────────────────────────▶  │                      │                     │
   │                    │                   │                     │                      │                     │
   │                    │                   │  webhook POST       │                      │                     │
   │                    │                   │  /api/v1/payment/   │                      │                     │
   │                    │                   │  webhook/razorpay   │                      │                     │
   │                    │◀──────────────────┼─────────────────────┤                      │                     │
   │                    │ (public, no JWT)  │ HMAC verify         │                      │                     │
   │                    │──────────────────▶│ lookup tx_id        │                      │                     │
   │                    │                   │                     │                      │                     │
   │                    │                   │ Postgres TX:        │                      │                     │
   │                    │                   │  UPDATE             │                      │                     │
   │                    │                   │  payment_trans…     │                      │                     │
   │                    │                   │  SET status=        │                      │                     │
   │                    │                   │  'completed'        │                      │                     │
   │                    │                   │                     │                      │                     │
   │                    │                   │ NATS req/rep        │                      │                     │
   │                    │                   │ wallet.deposit      │                      │                     │
   │                    │                   ├────────────────────────────────────────────▶│                     │
   │                    │                   │                     │                      │ INSERT ledger       │
   │                    │                   │                     │                      │ deposit row         │
   │                    │                   │◀────────────────────────────────────────────┤ ok                  │
   │                    │                   │                                              │                     │
   │                    │                   │ (wallet-service or   │                      │                     │
   │                    │                   │  payment-service     │                      │                     │
   │                    │                   │  publishes           │                      │                     │
   │                    │                   │  payment.deposit.    │                      │                     │
   │                    │                   │  completed on NATS)  │                      │                     │
   │                    │                   ├──────────────────────┼──────────────────────┼────────────────────▶│
   │                    │                   │ 200 OK to webhook                                                  │ INSERT
   │                    │◀──────────────────┤                                                                    │ "Deposit
   │                    │ 200 OK            │                                                                    │  Completed"
```

> Implementation note. Today `payment-service` calls
> `wallet.DepositTx` via an in-process `wallet.NewService` instance
> (transactional write into `betting.ledger`) rather than through NATS
> request/reply — the NATS `wallet.deposit` subscriber in `wallet-service`
> exists for other callers. The `payment.deposit.completed` event consumed
> by `notification-service` is published by the service that completes the
> deposit; grep for it in the commit history to confirm the current
> emitter. The user-visible data flow (UI → webhook → ledger credit →
> notification) is unchanged regardless of which wire the deposit crosses.

---

## 4. What is mocked vs what is real

| Subsystem                | Real or mocked?                                                                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| Sports odds feed         | **Mocked by default** via `internal/odds/mock.go` (`MockProvider`, GBM price walk + Poisson score updates). Switch to a real feed by setting `ODDS_PROVIDER=entity_sports` + `ENTITY_SPORTS_API_KEY`, which activates `internal/odds/entity_sports.go`. |
| Users, sessions, JWT     | Real — `auth-service`, PostgreSQL `auth.users` / `auth.user_sessions`, Redis `blacklist:` / `user:` keys.          |
| Wallet, ledger, exposure | Real — `wallet-service`, PostgreSQL `betting.ledger`, Redis `wallet:balance:<id>` / `exposure:user:<id>`.          |
| Bets, order book         | Real — `matching-engine`, PostgreSQL `betting.bets`, Redis `ob:{marketID}:back/lay:z/h`.                           |
| Payments                 | Real schema + real webhook path. The Razorpay / crypto providers themselves are obviously external; local dev uses the webhook endpoints directly. |
| Casino sessions & bets   | Real — `casino-service`, PostgreSQL `betting.casino_sessions` / `betting.casino_bets`. Games/launchers are third-party; we persist the session.          |
| Notifications            | Real — `notification-service` writes `betting.notifications` rows reactively from NATS events.                     |
| Fraud alerts             | Real — `fraud-service` consumes `bets.placed` / `auth.login` and writes `betting.fraud_alerts`.                    |
| Reporting                | Real — `reporting-service` queries `betting.*` tables; uses ClickHouse for roll-ups when `CLICKHOUSE_URL` is set.  |
| KYC, responsible gambling, hierarchy, referrals | Real — all served by `hierarchy-service` against real Postgres tables.                          |

The phrase "only odds is mocked" is literal: if you run `start-all.sh` with
the defaults, the **only** piece of the stack that does not exercise real
infrastructure is the odds provider. Every other write ends up in
PostgreSQL (with Redis as hot cache and NATS as the event bus).
