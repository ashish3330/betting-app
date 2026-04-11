# Lotus Exchange — Service Level Objectives

Owner: SRE
Last reviewed: 2026-04-11
Review cadence: quarterly

This document defines the customer-facing SLOs for Lotus Exchange and the SLIs,
error budgets, burn-rate alerts and escalation policy that operationalize them.

The "critical user journey" for the exchange is: a logged-in customer places a
bet, the order is matched, funds are held in the wallet, and on market close
the bet is settled and winnings are credited. Every SLO below maps to a step in
that journey.

---

## 1. SLOs

### 1.1 Availability SLO — bet placement

| Field            | Value                                              |
| ---------------- | -------------------------------------------------- |
| Endpoint         | `POST /api/v1/bet/place`                           |
| Objective        | 99.9% successful responses over a 30-day window    |
| Measurement      | Server-side (gateway)                              |
| Good event       | HTTP response with status `2xx` or `4xx` (client)  |
| Bad event        | HTTP response with status `5xx` or no response     |
| Error budget     | 0.1% of requests, ~43.2 minutes / 30 days          |

Note: client errors (`4xx`) are intentionally counted as "good" because they
represent the service correctly rejecting an invalid request — the platform is
working as designed. Network timeouts and `502/503/504` are bad.

### 1.2 Latency SLO — bet placement

| Field            | Value                                              |
| ---------------- | -------------------------------------------------- |
| Endpoint         | `POST /api/v1/bet/place`                           |
| Objective        | 99% of requests complete in < 200 ms over 30 days  |
| Measurement      | Server-side, end-to-end inside the gateway handler |
| Good event       | Request whose total duration is below 200 ms       |
| Bad event        | Request whose total duration is at or above 200 ms |
| Error budget     | 1% of requests / 30 days                           |

### 1.3 Settlement SLO

| Field            | Value                                              |
| ---------------- | -------------------------------------------------- |
| Pipeline         | Market close → settlement → wallet credit          |
| Objective        | 100% of settlement events processed within 30 s of market close, measured monthly |
| Measurement      | `settled_at - market_closed_at` per settlement event |
| Good event       | Settlement event with delta < 30 s                 |
| Bad event        | Settlement event with delta >= 30 s, or never settled |
| Error budget     | 0% — any miss is an incident                       |

A 100% target is unusual but appropriate here: late settlement directly
breaches customer trust and regulatory expectations around fund availability.
The reality is that occasional misses will happen; each one becomes a
ticketed incident with a postmortem.

---

## 2. Error budget table

For the availability SLO (99.9% of requests):

| Window | Allowed bad time     | Allowed bad requests at 1k req/s |
| ------ | -------------------- | -------------------------------- |
| 30 d   | ~43 min 12 s         | ~2.59 M                          |
| 7 d    | ~10 min 5 s          | ~604 K                           |
| 1 d    | ~1 min 26 s          | ~86 K                            |
| 1 h    | ~3.6 s               | ~3 600                           |

For the latency SLO (99% of requests under 200 ms), the budget is 1% of
requests per window — at 1k req/s that's roughly 600k slow requests per week,
86k per day. This is a much larger budget than availability and that's
intentional: latency degrades gracefully, hard failures don't.

When > 50% of the monthly budget is consumed before week 3, freeze risky
deploys (schema changes, dependency upgrades, config sweeps). When > 100% is
consumed, freeze all non-mitigation changes until the next monthly window.

---

## 3. SLI definitions

All SLIs are derived from metrics defined in `pkg/metrics/metrics.go`.

### 3.1 Availability SLI

```
sli_availability =
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place", status!~"5.."}[30d]))
  /
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place"}[30d]))
```

### 3.2 Latency SLI

```
sli_latency =
  sum(rate(lotus_http_request_duration_seconds_bucket{path="/api/v1/bet/place", le="0.2"}[30d]))
  /
  sum(rate(lotus_http_request_duration_seconds_count{path="/api/v1/bet/place"}[30d]))
```

The `le="0.2"` bucket exists in the histogram definition, so this query is
exact rather than interpolated.

### 3.3 Settlement SLI

The metric `lotus_settlement_duration_seconds` measures wall-clock time from
market close to wallet credit. The 30 s bucket is the relevant boundary:

```
sli_settlement =
  sum(rate(lotus_settlement_duration_seconds_bucket{le="30"}[30d]))
  /
  sum(rate(lotus_settlement_duration_seconds_count[30d]))
```

(The histogram in `metrics.go` does not currently include a `le=30` bucket;
add it before this SLI can be reported. Until then, treat late settlements
detected via the existing `le="10"` bucket plus a separate "late settlement"
counter.)

---

## 4. Burn-rate alerts

Burn-rate alerting uses two windows so that fast burns page immediately while
slow steady burns still get caught before the budget is exhausted. Thresholds
follow the Google SRE multi-window multi-burn-rate pattern.

| Severity | Long window | Short window | Burn rate | Pages? | Budget consumed if held |
| -------- | ----------- | ------------ | --------- | ------ | ----------------------- |
| Page     | 1 h         | 5 m          | 14.4x     | Yes    | 2% in 1 h               |
| Page     | 6 h         | 30 m         | 6x        | Yes    | 5% in 6 h               |
| Ticket   | 24 h        | 2 h          | 3x        | No     | 10% in 24 h             |
| Ticket   | 72 h        | 6 h          | 1x        | No     | 10% in 72 h             |

For the bet-placement availability SLO (0.001 budget):

```
# Fast burn (page)
(
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place", status=~"5.."}[1h]))
  /
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place"}[1h]))
) > (14.4 * 0.001)
and
(
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place", status=~"5.."}[5m]))
  /
  sum(rate(lotus_http_requests_total{path="/api/v1/bet/place"}[5m]))
) > (14.4 * 0.001)
```

The `and` against the short window prevents stale long-window alerts from
firing after a short blip has cleared.

---

## 5. Escalation policy (stub)

1. **L1 — On-call SRE.** Pager fires via PagerDuty. Acknowledge within 5 min,
   start triage using the runbook linked in the alert annotation. Update
   `#incident-active` in Slack.
2. **L2 — Service owner.** If the alert is service-specific (matching engine,
   wallet, settlement, gateway) and L1 cannot mitigate within 15 min, page
   the service owner.
3. **L3 — Engineering manager + SRE lead.** Engaged automatically if the
   incident is open more than 30 min, or immediately for any
   `LedgerImbalanceDetected` or settlement SLO breach.
4. **Comms.** For customer-visible incidents, the L1 declares an incident in
   Statuspage within 10 min of acknowledging the page. Updates every 30 min
   until resolved.
5. **Postmortem.** Required within 5 working days for any SLO breach, any
   `severity: page` alert that triggers user-visible impact, and any incident
   touching the wallet, ledger, or settlement pipeline.

---

## 6. Review

This document and its targets are reviewed quarterly. Changes to SLO targets
require sign-off from SRE lead, Product, and Compliance (because of the
regulatory implications of the settlement SLO).
