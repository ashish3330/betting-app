# Lotus Exchange — On-call Runbooks

These are the runbooks linked from the `runbook_url` annotations in
`deployments/prometheus/alerts.yml`. They are written for the on-call SRE who
just got paged at 03:00 and is opening this file half-awake on a phone.

Each runbook follows the same shape:

1. **What fired**
2. **Why you should care (impact)**
3. **Triage in 60 seconds**
4. **Common causes**
5. **Mitigation**
6. **Escalation**
7. **After the fire**

---

## HighErrorRate

### What fired

`HighErrorRate` — `sum(rate(lotus_http_requests_total{status=~"5.."}[5m])) /
sum(rate(lotus_http_requests_total[5m])) > 0.01` for 5 minutes on at least one
service. Severity: page.

### Why you should care

More than 1% of HTTP requests across a service are returning 5xx. Customers
are seeing failed bet placements, failed logins, missing market data, or
broken account pages. This is a direct hit to revenue and to the bet
placement availability SLO. Every minute the alert is firing burns ~14x the
hourly budget.

### Triage in 60 seconds

1. Open the Lotus Overview Grafana dashboard. Look at the "HTTP Error Rate"
   panel — is the rise localized to one service, one endpoint, or global?
2. Check the "HTTP Request Rate (RED)" panel: is total traffic also abnormal?
   A spike + errors suggests overload; flat traffic + errors suggests a
   regression.
3. `kubectl -n lotus get pods` for the affected service. Are pods crash-looping
   (`CrashLoopBackOff`), pending, or evicted?
4. `kubectl -n lotus logs deploy/<svc> --since=10m | grep -iE 'panic|error|fatal'`
   — what is the most common stack trace?
5. Check the deploy timeline in ArgoCD / GitHub Actions. Was anything shipped
   in the last hour? If yes, that is your most likely suspect.

### Common causes

- Bad deploy: regression in handler code, broken migration, wrong feature
  flag default. Almost always the answer if errors started right after a
  release.
- Downstream dependency outage: Postgres, Redis, the matching engine, the
  payment provider. Check `DBConnectionPoolNearLimit`, Redis dashboards, and
  external provider status pages.
- Schema drift: a migration ran but a stale pod is still serving the old
  shape. Symptom: errors only on a subset of pods.
- Config change: a runtime config push (rate limits, feature flags) that
  unintentionally rejected valid traffic.
- Cert / DNS / mTLS expiry: less common but always check if errors are
  exclusively `502/503` from the gateway with empty response bodies.

### Mitigation

- **Bad deploy:** roll back immediately. `argocd app rollback lotus-<svc>` or
  `kubectl -n lotus rollout undo deployment/<svc>`. Do not try to forward-fix
  during an active page.
- **Dependency outage:** if it's an external provider, switch to the secondary
  provider via the feature flag (`payment.provider=secondary`). If it's
  Postgres or Redis, see the dedicated runbooks.
- **Overload:** scale out the affected deployment by 50% (`kubectl scale
  deployment/<svc> --replicas=$((N*3/2))`) and enable the request shedding
  flag (`gateway.shed_low_priority=true`).
- **Schema drift:** force a rollout (`kubectl rollout restart deploy/<svc>`)
  so all pods pick up the new shape.

### Escalation

If the error rate is not visibly recovering within 15 minutes of mitigation,
page the service owner (L2). If the affected path is `/api/v1/bet/place` or
`/api/v1/wallet/*`, also notify the duty manager in `#incident-active`.

### After the fire

Postmortem within 5 working days. Required sections: timeline, customer
impact in $ and request count, root cause, action items with owners and
due dates. File the action items as GitHub issues tagged `postmortem`.

---

## DBConnectionPoolNearLimit

### What fired

`DBConnectionPoolNearLimit` — `lotus_db_pool_connections{state="in_use"} /
lotus_db_pool_connections{state="open"} > 0.9` for 2 minutes on at least one
instance. Severity: page.

### Why you should care

The Postgres connection pool is more than 90% saturated. Once it hits 100%,
new requests will queue waiting for a connection, which manifests as latency
spikes, then handler timeouts, then 5xx. You are minutes away from a real
outage. If you see this fire, treat it as pre-incident, not as informational.

### Triage in 60 seconds

1. Which service is saturated? `lotus_db_pool_connections{state="in_use"}` is
   labelled by `instance` — open the "DB Connection Pool Utilisation" panel
   and identify the offender.
2. Is the saturation steady-state high or trending upward fast? Steady = leak
   or capacity problem; trending = sudden spike (a query went slow).
3. In Postgres: `SELECT state, count(*) FROM pg_stat_activity GROUP BY state;`
   How many sessions are `idle in transaction`? More than ~20 is a code bug
   (a transaction was opened and not closed).
4. `SELECT pid, now()-query_start AS age, state, query FROM pg_stat_activity
   WHERE state <> 'idle' ORDER BY age DESC LIMIT 20;` — what's the slowest
   query?

### Common causes

- A new slow query shipped in the last release (missing index, accidental
  full table scan, n+1 from a new feature). The query in step 4 will tell you.
- A long-running transaction holding rows: usually a settlement job, an
  admin batch, or a forgotten `BEGIN` in a handler.
- A traffic spike that the pool simply isn't sized for. Check
  `BetPlacementSpike` and the bets-placed-per-second panel.
- Connection leak: a code path that takes a connection and never returns it.
  Symptom: `in_use` climbs monotonically and never drops, even when traffic
  drops.
- A Postgres failover or restart that reset the pool stats, in which case
  this alert is spurious — but verify before assuming.

### Mitigation

- **Slow query:** kill the offender with `SELECT pg_cancel_backend(<pid>)`.
  If it's part of a deploy that landed in the last hour, roll back.
- **Long transaction:** identify the originating service from `application_name`
  in `pg_stat_activity` and bounce that pod (`kubectl rollout restart`).
- **Capacity:** raise the pool size in the affected service's config and
  redeploy. Default per-pod is 25; you can go to 50 short-term. Watch the
  Postgres `max_connections` budget — at 200 pods x 50 conns you exceed it.
- **Leak:** rolling restart of the affected deployment buys you time. File a
  P1 to find and fix the leak; do not leave the bandage in place.

### Escalation

If the pool stays above 95% after 10 minutes of mitigation, page the data
platform on-call. If Postgres itself is unhealthy (CPU > 90%, replication lag
high, or it's failed over), escalate to L3 immediately.

### After the fire

Add a ticket for the underlying cause. If this was a missing index, the fix
is a migration; if a leak, the fix is in code with a regression test.

---

## NotificationOutboxBacklog

### What fired

`NotificationOutboxBacklog` — `notification_outbox_pending > 1000` for 2
minutes. Severity: page.

### Why you should care

The transactional outbox pattern guarantees that domain events (bet
confirmed, settlement complete, withdrawal updated) are delivered to
downstream consumers (push, email, SMS, third-party webhooks). When the
outbox backlog grows, customers stop receiving these notifications even
though the underlying transaction succeeded. They will see "you bet was
placed" in the UI but no push, then they will email support complaining
they think their bet didn't go through.

### Triage in 60 seconds

1. Open the "Notification Outbox Depth" panel. Is the backlog growing,
   flat, or recovering?
2. `kubectl -n lotus get deploy notification-dispatcher` — is the dispatcher
   running and ready? How many replicas?
3. `kubectl -n lotus logs deploy/notification-dispatcher --tail=200` — look
   for repeated errors against a downstream provider (Twilio, Firebase, the
   webhook target).
4. `SELECT channel, count(*) FROM notification_outbox WHERE sent_at IS NULL
   GROUP BY channel;` — is the backlog concentrated on one channel? If yes,
   that downstream is your problem.

### Common causes

- A downstream provider is rate-limiting or returning 5xx (Firebase outage,
  Twilio degraded, customer's own webhook endpoint is down).
- The dispatcher is stuck on a poison message — same row failing repeatedly,
  blocking the queue. Look for the same message ID in repeated log lines.
- Dispatcher is scaled to zero or crash-looping.
- The outbox writer is producing faster than the dispatcher can drain
  because of a legitimate traffic spike. Cross-check `BetPlacementSpike`.
- A schema migration on `notification_outbox` left an index in an unusable
  state and `SELECT ... WHERE sent_at IS NULL` is now a full table scan.

### Mitigation

- **Provider down:** flip the per-channel feature flag
  (`notifications.<channel>.enabled=false`) so the dispatcher skips the dead
  channel and drains the others. Re-enable when the provider recovers.
- **Poison message:** mark the offending row as failed (`UPDATE
  notification_outbox SET sent_at=now(), status='dead_letter' WHERE id=...`)
  after capturing the payload to a Slack thread for forensics.
- **Capacity:** scale dispatcher pods (`kubectl scale
  deploy/notification-dispatcher --replicas=N*2`).
- **Migration / index issue:** `REINDEX INDEX CONCURRENTLY
  notification_outbox_pending_idx;` and then watch the backlog drain.

### Escalation

Engage the messaging team owner if backlog is still above 1000 after 15
minutes of mitigation. If a regulated notification (KYC, withdrawal status)
is affected, also notify Compliance.

### After the fire

Confirm the queue has fully drained back to its normal baseline (typically
< 100). File a postmortem if any customer notifications were dropped to
the dead-letter table.

---

## SettlementLatencyHigh

### What fired

`SettlementLatencyHigh` — `histogram_quantile(0.99, sum by (le)
(rate(lotus_settlement_duration_seconds_bucket[5m]))) > 5` for 5 minutes.
Severity: page.

### Why you should care

Settlement is the moment we credit winnings to customer wallets. When
settlement latency spikes, customers who just won are sitting at their
screen wondering where their money is. Past 30 seconds you breach the
settlement SLO; past a few minutes you start getting a wave of support
tickets and Twitter complaints. Sustained settlement latency is also a
ledger-integrity smell — make sure to check `LedgerImbalanceDetected`.

### Triage in 60 seconds

1. Open the "Settlement Latency p99" panel. Is the spike a step change or a
   gradual climb?
2. `kubectl -n lotus get pods -l app=settlement-worker`. Are workers healthy?
   How many replicas, how many ready?
3. `kubectl -n lotus logs deploy/settlement-worker --tail=200 | grep -iE
   'error|slow|retry'`. Look for retries against the wallet service or
   Postgres.
4. Check `DBConnectionPoolNearLimit` and the wallet service's error rate.
   Settlement reads/writes both heavily.
5. Is there a single mega-market currently settling (a major football final,
   for example)? Settlement of a market with millions of bets can dominate
   p99 legitimately for a short period.

### Common causes

- A large market settling — legitimate but visible. The fix is parallelism,
  not panic.
- Wallet service is degraded; settlement workers are stuck in retry loops.
  Symptom: wallet error rate is also up.
- Postgres is slow (locks, connection pool saturated, IO degraded).
- A bad deploy added an n+1 query to the settlement path.
- A market with malformed/unexpected data is causing the worker to thrash on
  the same record.

### Mitigation

- **Big market:** scale settlement workers temporarily
  (`kubectl scale deploy/settlement-worker --replicas=N*2`) and let it
  drain. Annotate the dashboard so the next on-call sees the cause.
- **Wallet degraded:** address the wallet incident first; settlement will
  recover automatically.
- **Postgres slow:** see the `DBConnectionPoolNearLimit` runbook.
- **Bad deploy:** roll back the settlement worker.
- **Poison market:** identify the market ID from logs, mark it for manual
  settlement (`UPDATE markets SET settlement_state='manual' WHERE id=...`),
  alert Trading Ops to handle by hand.

### Escalation

If p99 stays above 5 s after 15 minutes, page the Trading on-call. If any
settlement event is older than 30 s past market close, this is also an SLO
breach — declare an incident even if you're already mitigating, because
post-incident review is mandatory.

### After the fire

Re-check `lotus_ledger_imbalance` (when that metric exists) and run the
finance reconciliation job for the affected window. File a postmortem if
the settlement SLO was breached. If the cause was a single hot market,
add a capacity-planning ticket so the next big match is provisioned for.
