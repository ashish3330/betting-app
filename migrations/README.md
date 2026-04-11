# Lotus Exchange — Database Migrations

Canonical PostgreSQL + ClickHouse schema for the betting exchange. Every
numbered file is applied **in order** and must be idempotent: running the
same migration twice against the same database must be a no-op.

## Numbering convention

- Files are numbered `NNN_short_description.sql`, zero-padded to three
  digits (`001`, `002`, …).
- Numbers are **strictly increasing** — never edit or renumber a migration
  after it has been merged to `main`. If you need to fix a mistake,
  add a new migration that corrects it.
- One logical concern per file. Avoid multi-hundred-line catch-alls.
- Every statement must use `IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`
  or equivalent so the file can be safely re-applied. This lets us run
  the full directory against any database state (fresh install, partial
  install, live production) and converge to the same schema.

Migration `003_clickhouse.sql` is the one exception that runs against
ClickHouse rather than PostgreSQL.

## How to apply

All PostgreSQL migrations use the `lotus` role against the `bettingdb`
database (see `.env.example`):

```sh
PGPASSWORD="$POSTGRES_PASSWORD" psql \
    -h localhost -U lotus -d bettingdb \
    -v ON_ERROR_STOP=1 \
    -f migrations/001_initial.sql

# …repeat for 002 through 018.
```

`ON_ERROR_STOP=1` makes `psql` exit non-zero on the first error, which
is what the CI/deploy scripts rely on.

## File inventory

| File | Concern |
| --- | --- |
| `001_initial.sql` | Schemas, `auth.users`, `betting.markets`, `betting.runners`, partitioned `betting.bets`, `betting.ledger`, seed superadmin |
| `002_phase2.sql` | Casino sessions / bets, payment transactions, fraud alerts, audit log (original shape) |
| `003_clickhouse.sql` | ClickHouse analytics schema (`raw_bets`, `market_snapshots`, `audit_events`, PnL & volume MVs) |
| `004_production_hardening.sql` | KYC columns on `auth.users`, responsible gambling, cashout offers, user sessions, notifications, promotions, monthly-partition function, account statement MV |
| `005_multi_sport_games.sql` | Sports / competitions / events, casino providers and games catalogue, settlement events outbox |
| `006_deposit_payment.sql` | Bank accounts, daily account usage, deposit requests (Master/Agent workflow) |
| `007_audit_fixes.sql` | Performance indexes + `auth.users` auth columns (`referral_code`, `otp_enabled`, `is_demo`, `force_password_change`) + client_ref idempotency index |
| `012_auth_user_columns.sql` | Safety net for all `auth.users` columns the deleted monolith used to add at boot (referral / OTP / KYC). Mostly no-ops because 004 and 007 already cover them. |
| `013_bet_columns.sql` | `market_type`, `display_side`, `settled_at` on `betting.bets` |
| `014_audit_log_extensions.sql` | Flattened identity columns (`user_id`, `username`, `details`, `ip`), legacy `entity_type`/`entity_id` guards, and tamper-evident `hash_chain` on `betting.audit_log` |
| `015_casino_sessions.sql` | Idempotent re-declaration of `betting.casino_sessions` + active-session partial index |
| `016_kyc_documents.sql` | `betting.kyc_documents` (per-user document metadata; payloads in object storage) |
| `017_settlement_idempotency.sql` | `betting.settlement_idempotency` (guards SettleMarket against double-payout) and `betting.bet_fills` (matching-engine per-fill rows) |
| `018_complaints.sql` | `betting.complaints` (player complaint tickets, open → investigating → resolved/rejected) |

## Why 008–011 are missing

Migrations 008 through 011 were reserved at planning time for work
(notification idempotency, etc.) that ultimately landed either in earlier
files or in application-layer code. The gap is intentional — **do not
renumber 012-018 to fill it**; migrations must never be renumbered once
merged. New migrations continue at 019 and beyond.

## Migrations 012-018 in context

Before the refactor that deleted `cmd/server`, that binary ran an
in-process `runMigrations` function on startup that issued a long list
of `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` and `CREATE TABLE IF NOT
EXISTS` statements to backfill columns and tables missing from the
numbered migration files. With the monolith gone, the numbered files
in this directory are now the single source of truth for the schema,
and 012-018 port over every one of those boot-time statements so a DB
created fresh from `001` through `018` is byte-identical to the shape
the running services expect.

Each of 012-018 is fully idempotent: on an existing production database
the statements are silent no-ops (PostgreSQL prints
`NOTICE: ... already exists, skipping`). On a fresh database they create
the missing columns / tables / indexes.
