# Lotus Exchange API Documentation

**Base URL:** `https://api.lotusexchange.com`
**API Version:** v1
**Last Updated:** 2026-04-05

---

## Table of Contents

- [Overview](#overview)
- [Authentication Flow](#authentication-flow)
- [Role Hierarchy](#role-hierarchy)
- [Rate Limiting](#rate-limiting)
- [Error Response Format](#error-response-format)
- [Pagination](#pagination)
- [Endpoints](#endpoints)
  - [Health](#health)
  - [Authentication](#authentication)
  - [Sports and Markets (Public)](#sports-and-markets-public)
  - [Casino (Public)](#casino-public)
  - [Betting](#betting)
  - [Market Management](#market-management)
  - [Wallet](#wallet)
  - [Payment](#payment)
  - [Payment Webhooks](#payment-webhooks)
  - [Casino (Protected)](#casino-protected)
  - [Cashout](#cashout)
  - [Hierarchy](#hierarchy)
  - [Risk](#risk)
  - [Reports](#reports)
  - [KYC](#kyc)
  - [Responsible Gambling](#responsible-gambling)
  - [Notifications](#notifications)
  - [Admin](#admin)
  - [Fraud](#fraud)
- [WebSocket Protocol](#websocket-protocol)
- [Testing Guide](#testing-guide)

---

## Overview

The Lotus Exchange API is a REST API that powers the Lotus Exchange betting platform. All endpoints are prefixed with `/api/v1` unless otherwise noted. Request and response bodies use JSON (`Content-Type: application/json`).

Protected endpoints require a valid JWT access token in the `Authorization` header:

```
Authorization: Bearer <access_token>
```

---

## Authentication Flow

Lotus Exchange uses a JWT-based authentication system with short-lived access tokens and long-lived refresh tokens.

### Token Lifecycle

1. **Register or Login** -- The server returns an `access_token` (expires in 15 minutes) and a `refresh_token` (expires in 7 days). The refresh token is also set as an `HttpOnly` cookie.
2. **Access Protected Resources** -- Include the access token in the `Authorization: Bearer <token>` header on every request.
3. **Refresh** -- When the access token expires, call `POST /api/v1/auth/refresh` with the refresh token. The server issues a new access token and rotates the refresh token (the old refresh token is invalidated).
4. **Logout** -- Call `POST /api/v1/auth/logout` to invalidate all tokens for the session. The refresh token cookie is cleared.

### Refresh Token Rotation

Each call to the refresh endpoint invalidates the previous refresh token and returns a new one. If a previously-invalidated refresh token is reused, the server revokes the entire token family (all sessions for that user) as a security measure.

---

## Role Hierarchy

The platform enforces a strict role hierarchy for multi-level agent management:

```
superadmin > admin > master > agent > client
```

| Role        | Description                                                                 |
|-------------|-----------------------------------------------------------------------------|
| superadmin  | Full platform access. Can manage all users, markets, and system settings.   |
| admin       | Can manage masters, agents, and clients. Access to reports and risk tools.  |
| master      | Can manage agents and their downstream clients.                             |
| agent       | Can manage clients. Can transfer credit to clients.                         |
| client      | End user. Can place bets, manage wallet, and view own reports.              |

A user can only manage users at a level strictly below their own. Credit flows downward through the hierarchy.

---

## Rate Limiting

All API endpoints are rate-limited. Limits are applied per IP for public endpoints and per user for authenticated endpoints.

| Endpoint Category | Limit              |
|-------------------|--------------------|
| Authentication    | 10 requests/minute |
| Public Data       | 60 requests/minute |
| Betting           | 30 requests/minute |
| Wallet/Payment    | 20 requests/minute |
| Admin             | 60 requests/minute |
| WebSocket         | 1 connection/user  |

When a rate limit is exceeded, the server responds with `429 Too Many Requests`:

```json
{
  "status": "error",
  "code": "RATE_LIMIT_EXCEEDED",
  "message": "Too many requests. Try again in 32 seconds.",
  "retry_after": 32
}
```

The `Retry-After` header is also set on the response.

---

## Error Response Format

All errors follow a consistent structure:

```json
{
  "status": "error",
  "code": "ERROR_CODE",
  "message": "Human-readable description of what went wrong.",
  "details": {}
}
```

| Field     | Type   | Description                                                       |
|-----------|--------|-------------------------------------------------------------------|
| status    | string | Always `"error"` for error responses.                             |
| code      | string | Machine-readable error code (e.g., `INVALID_CREDENTIALS`).       |
| message   | string | Human-readable message suitable for display.                      |
| details   | object | Optional. Additional context such as field validation errors.     |

### Common Error Codes

| Code                    | HTTP Status | Description                              |
|-------------------------|-------------|------------------------------------------|
| VALIDATION_ERROR        | 400         | Request body failed validation.          |
| INVALID_CREDENTIALS     | 401         | Wrong username or password.              |
| TOKEN_EXPIRED           | 401         | Access token has expired.                |
| TOKEN_INVALID           | 401         | Token is malformed or revoked.           |
| FORBIDDEN               | 403         | Insufficient role or permissions.        |
| NOT_FOUND               | 404         | Resource does not exist.                 |
| DUPLICATE_ENTRY         | 409         | Resource already exists.                 |
| INSUFFICIENT_BALANCE    | 422         | Wallet balance too low for operation.    |
| MARKET_SUSPENDED        | 422         | Market is not accepting bets.            |
| RATE_LIMIT_EXCEEDED     | 429         | Too many requests.                       |
| INTERNAL_ERROR          | 500         | Unexpected server error.                 |

---

## Pagination

List endpoints support cursor-based or offset-based pagination via query parameters.

| Parameter | Type    | Default | Description                          |
|-----------|---------|---------|--------------------------------------|
| limit     | integer | 20      | Number of items per page (max 100).  |
| offset    | integer | 0       | Number of items to skip.             |

Paginated responses include metadata:

```json
{
  "status": "success",
  "data": [ ... ],
  "pagination": {
    "total": 243,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

---

## Endpoints

---

### Health

#### GET /health

Check whether the API server is running.

**Authentication:** None

**Response:**

```json
{
  "status": "ok",
  "timestamp": "2026-04-05T12:00:00Z",
  "version": "1.0.0"
}
```

| Status | Description         |
|--------|---------------------|
| 200    | Server is healthy.  |

---

### Authentication

#### POST /api/v1/auth/register

Create a new user account.

**Authentication:** None

**Request Body:**

```json
{
  "username": "string (3-30 chars, alphanumeric + underscore)",
  "email": "string (valid email)",
  "password": "string (min 8 chars, must include uppercase, lowercase, digit)",
  "phone": "string (optional, E.164 format)",
  "referral_code": "string (optional)"
}
```

**Response (201 Created):**

```json
{
  "status": "success",
  "data": {
    "user": {
      "id": "usr_abc123",
      "username": "johndoe",
      "email": "john@example.com",
      "role": "client",
      "created_at": "2026-04-05T12:00:00Z"
    },
    "access_token": "eyJhbGciOi...",
    "refresh_token": "eyJhbGciOi...",
    "expires_in": 900
  }
}
```

| Status | Description                                  |
|--------|----------------------------------------------|
| 201    | Account created successfully.                |
| 400    | Validation error (missing/invalid fields).   |
| 409    | Username or email already in use.            |

---

#### POST /api/v1/auth/login

Authenticate an existing user.

**Authentication:** None

**Request Body:**

```json
{
  "username": "string",
  "password": "string"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "user": {
      "id": "usr_abc123",
      "username": "johndoe",
      "email": "john@example.com",
      "role": "client",
      "status": "active",
      "last_login": "2026-04-04T18:30:00Z"
    },
    "access_token": "eyJhbGciOi...",
    "refresh_token": "eyJhbGciOi...",
    "expires_in": 900
  }
}
```

| Status | Description                              |
|--------|------------------------------------------|
| 200    | Login successful.                        |
| 400    | Missing required fields.                 |
| 401    | Invalid credentials.                     |
| 403    | Account suspended or locked.             |

---

#### POST /api/v1/auth/logout

Invalidate the current session and revoke tokens.

**Authentication:** None (refresh token sent via cookie or body)

**Request Body:**

```json
{
  "refresh_token": "string (optional if sent via cookie)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Logged out successfully."
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Logout successful.             |

---

#### POST /api/v1/auth/refresh

Exchange a valid refresh token for a new access token. The refresh token is rotated.

**Authentication:** None (refresh token sent via cookie or body)

**Request Body:**

```json
{
  "refresh_token": "string (optional if sent via cookie)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "access_token": "eyJhbGciOi...",
    "refresh_token": "eyJhbGciOi...",
    "expires_in": 900
  }
}
```

| Status | Description                                          |
|--------|------------------------------------------------------|
| 200    | Tokens refreshed.                                    |
| 401    | Refresh token is invalid, expired, or reused.        |

---

### Sports and Markets (Public)

#### GET /api/v1/sports

List all available sports.

**Authentication:** None

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "sport_cricket",
      "name": "Cricket",
      "slug": "cricket",
      "active": true,
      "display_order": 1,
      "competitions_count": 12
    },
    {
      "id": "sport_football",
      "name": "Football",
      "slug": "football",
      "active": true,
      "display_order": 2,
      "competitions_count": 24
    }
  ]
}
```

| Status | Description             |
|--------|-------------------------|
| 200    | List of sports.         |

---

#### GET /api/v1/competitions?sport={sport}

List competitions for a given sport.

**Authentication:** None

**Query Parameters:**

| Parameter | Type   | Required | Description                        |
|-----------|--------|----------|------------------------------------|
| sport     | string | Yes      | Sport slug (e.g., `cricket`).      |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "comp_ipl2026",
      "name": "IPL 2026",
      "sport": "cricket",
      "region": "India",
      "starts_at": "2026-03-22T00:00:00Z",
      "ends_at": "2026-05-30T00:00:00Z",
      "events_count": 74
    }
  ]
}
```

| Status | Description                          |
|--------|--------------------------------------|
| 200    | List of competitions.                |
| 400    | Missing or invalid sport parameter.  |

---

#### GET /api/v1/events?competition_id={id}

List events (matches) within a competition.

**Authentication:** None

**Query Parameters:**

| Parameter      | Type   | Required | Description          |
|----------------|--------|----------|----------------------|
| competition_id | string | Yes      | Competition ID.      |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "evt_001",
      "name": "Mumbai Indians v Chennai Super Kings",
      "competition_id": "comp_ipl2026",
      "sport": "cricket",
      "status": "upcoming",
      "starts_at": "2026-04-10T14:00:00Z",
      "is_live": false,
      "markets_count": 5
    }
  ]
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | List of events.                            |
| 400    | Missing or invalid competition_id.         |

---

#### GET /api/v1/events/{id}/markets

List all markets for a specific event.

**Authentication:** None

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Event ID.   |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "mkt_matchodds_001",
      "event_id": "evt_001",
      "name": "Match Odds",
      "type": "match_odds",
      "status": "open",
      "runners": [
        {
          "id": "run_mi",
          "name": "Mumbai Indians",
          "sort_priority": 1
        },
        {
          "id": "run_csk",
          "name": "Chennai Super Kings",
          "sort_priority": 2
        }
      ],
      "created_at": "2026-04-05T10:00:00Z"
    }
  ]
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | List of markets.             |
| 404    | Event not found.             |

---

#### GET /api/v1/markets?sport={sport}

List all open markets for a sport.

**Authentication:** None

**Query Parameters:**

| Parameter | Type   | Required | Description              |
|-----------|--------|----------|--------------------------|
| sport     | string | Yes      | Sport slug.              |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "mkt_matchodds_001",
      "event_id": "evt_001",
      "event_name": "Mumbai Indians v Chennai Super Kings",
      "name": "Match Odds",
      "type": "match_odds",
      "status": "open",
      "starts_at": "2026-04-10T14:00:00Z"
    }
  ]
}
```

| Status | Description                       |
|--------|-----------------------------------|
| 200    | List of markets for the sport.    |
| 400    | Missing or invalid sport.         |

---

#### GET /api/v1/markets/{id}/odds

Get the current best available odds for a market.

**Authentication:** None

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "open",
    "last_updated": "2026-04-05T12:05:30Z",
    "runners": [
      {
        "runner_id": "run_mi",
        "name": "Mumbai Indians",
        "back": [
          { "price": 1.85, "size": 50000 },
          { "price": 1.84, "size": 25000 },
          { "price": 1.83, "size": 10000 }
        ],
        "lay": [
          { "price": 1.87, "size": 40000 },
          { "price": 1.88, "size": 30000 },
          { "price": 1.89, "size": 15000 }
        ]
      },
      {
        "runner_id": "run_csk",
        "name": "Chennai Super Kings",
        "back": [
          { "price": 2.10, "size": 35000 },
          { "price": 2.08, "size": 20000 },
          { "price": 2.06, "size": 12000 }
        ],
        "lay": [
          { "price": 2.12, "size": 28000 },
          { "price": 2.14, "size": 18000 },
          { "price": 2.16, "size": 9000 }
        ]
      }
    ]
  }
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | Current odds for the market. |
| 404    | Market not found.            |

---

### Casino (Public)

#### GET /api/v1/casino/providers

List all casino game providers.

**Authentication:** None

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "prov_evolution",
      "name": "Evolution Gaming",
      "slug": "evolution",
      "logo_url": "https://cdn.lotusexchange.com/providers/evolution.png",
      "game_count": 45,
      "active": true
    }
  ]
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | List of providers.       |

---

#### GET /api/v1/casino/games

List all available casino games. Supports filtering and pagination.

**Authentication:** None

**Query Parameters:**

| Parameter  | Type    | Required | Description                            |
|------------|---------|----------|----------------------------------------|
| provider   | string  | No       | Filter by provider slug.               |
| category   | string  | No       | Filter by category slug.               |
| search     | string  | No       | Search by game name.                   |
| limit      | integer | No       | Items per page (default 20, max 100).  |
| offset     | integer | No       | Offset for pagination.                 |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "game_roulette01",
      "name": "Lightning Roulette",
      "provider": "evolution",
      "category": "live-casino",
      "thumbnail_url": "https://cdn.lotusexchange.com/games/lightning-roulette.jpg",
      "is_live": true,
      "min_bet": 10,
      "max_bet": 500000
    }
  ],
  "pagination": {
    "total": 120,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description          |
|--------|----------------------|
| 200    | List of games.       |

---

#### GET /api/v1/casino/categories

List all casino game categories.

**Authentication:** None

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "cat_livecasino",
      "name": "Live Casino",
      "slug": "live-casino",
      "display_order": 1,
      "game_count": 45
    },
    {
      "id": "cat_slots",
      "name": "Slots",
      "slug": "slots",
      "display_order": 2,
      "game_count": 60
    }
  ]
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | List of categories.      |

---

#### GET /api/v1/casino/games/{category}

List games within a specific category.

**Authentication:** None

**Path Parameters:**

| Parameter | Type   | Description                        |
|-----------|--------|------------------------------------|
| category  | string | Category slug (e.g., `live-casino`).|

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20, max 100). |
| offset    | integer | No       | Offset for pagination.                |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "game_roulette01",
      "name": "Lightning Roulette",
      "provider": "evolution",
      "category": "live-casino",
      "thumbnail_url": "https://cdn.lotusexchange.com/games/lightning-roulette.jpg",
      "is_live": true,
      "min_bet": 10,
      "max_bet": 500000
    }
  ],
  "pagination": {
    "total": 45,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description                      |
|--------|----------------------------------|
| 200    | Games in the category.           |
| 404    | Category not found.              |

---

### Betting

All betting endpoints require JWT authentication.

#### POST /api/v1/bet/place

Place a new back or lay bet on a market.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "market_id": "string",
  "runner_id": "string",
  "side": "back | lay",
  "price": "number (decimal odds, e.g., 1.85)",
  "stake": "number (amount in user currency)"
}
```

**Response (201 Created):**

```json
{
  "status": "success",
  "data": {
    "bet_id": "bet_x7k9m2",
    "market_id": "mkt_matchodds_001",
    "runner_id": "run_mi",
    "side": "back",
    "price": 1.85,
    "stake": 1000,
    "potential_profit": 850,
    "liability": 1000,
    "status": "matched",
    "matched_amount": 1000,
    "unmatched_amount": 0,
    "placed_at": "2026-04-05T12:10:00Z"
  }
}
```

| Status | Description                                           |
|--------|-------------------------------------------------------|
| 201    | Bet placed successfully.                              |
| 400    | Validation error (invalid odds, stake, etc.).         |
| 401    | Not authenticated.                                    |
| 422    | Insufficient balance or market suspended.             |

---

#### DELETE /api/v1/bet/{betId}/cancel?market_id={id}&side={back|lay}

Cancel an unmatched (or partially matched) bet.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| betId     | string | Bet ID.     |

**Query Parameters:**

| Parameter  | Type   | Required | Description                          |
|------------|--------|----------|--------------------------------------|
| market_id  | string | Yes      | Market the bet belongs to.           |
| side       | string | Yes      | `back` or `lay`.                     |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "bet_id": "bet_x7k9m2",
    "status": "cancelled",
    "refunded_amount": 500,
    "cancelled_at": "2026-04-05T12:15:00Z"
  }
}
```

| Status | Description                                              |
|--------|----------------------------------------------------------|
| 200    | Bet cancelled successfully.                              |
| 400    | Missing required query parameters.                       |
| 401    | Not authenticated.                                       |
| 404    | Bet not found.                                           |
| 422    | Bet is fully matched and cannot be cancelled.            |

---

#### GET /api/v1/market/{marketId}/orderbook

Get the full order book for a market.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| marketId  | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "open",
    "runners": [
      {
        "runner_id": "run_mi",
        "name": "Mumbai Indians",
        "back_orders": [
          { "price": 1.85, "size": 50000, "count": 12 },
          { "price": 1.84, "size": 25000, "count": 6 }
        ],
        "lay_orders": [
          { "price": 1.87, "size": 40000, "count": 8 },
          { "price": 1.88, "size": 30000, "count": 5 }
        ],
        "last_traded_price": 1.86,
        "total_matched": 1250000
      }
    ],
    "last_updated": "2026-04-05T12:10:30Z"
  }
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | Order book data.             |
| 401    | Not authenticated.           |
| 404    | Market not found.            |

---

### Market Management

#### GET /api/v1/markets/list

List markets with filtering. For admin or agent use.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter | Type    | Required | Description                                |
|-----------|---------|----------|--------------------------------------------|
| sport     | string  | No       | Filter by sport slug.                      |
| status    | string  | No       | Filter by status: `open`, `suspended`, `settled`, `voided`. |
| limit     | integer | No       | Items per page (default 20).               |
| offset    | integer | No       | Pagination offset.                         |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "mkt_matchodds_001",
      "event_name": "Mumbai Indians v Chennai Super Kings",
      "name": "Match Odds",
      "sport": "cricket",
      "status": "open",
      "total_matched": 1250000,
      "created_at": "2026-04-05T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 85,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | List of markets.       |
| 401    | Not authenticated.     |

---

#### GET /api/v1/markets/detail/{id}

Get detailed information about a specific market.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "mkt_matchodds_001",
    "event_id": "evt_001",
    "event_name": "Mumbai Indians v Chennai Super Kings",
    "name": "Match Odds",
    "type": "match_odds",
    "sport": "cricket",
    "status": "open",
    "runners": [
      {
        "id": "run_mi",
        "name": "Mumbai Indians",
        "status": "active",
        "last_traded_price": 1.86,
        "total_matched": 750000
      },
      {
        "id": "run_csk",
        "name": "Chennai Super Kings",
        "status": "active",
        "last_traded_price": 2.10,
        "total_matched": 500000
      }
    ],
    "total_matched": 1250000,
    "created_at": "2026-04-05T10:00:00Z",
    "starts_at": "2026-04-10T14:00:00Z"
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Market details.        |
| 401    | Not authenticated.     |
| 404    | Market not found.      |

---

#### POST /api/v1/markets/create

Create a new market (admin/superadmin only).

**Authentication:** Required (JWT, admin or superadmin role)

**Request Body:**

```json
{
  "event_id": "string",
  "name": "string",
  "type": "match_odds | bookmaker | fancy | over_under",
  "runners": [
    { "name": "string", "sort_priority": "integer" },
    { "name": "string", "sort_priority": "integer" }
  ],
  "starts_at": "string (ISO 8601 datetime)"
}
```

**Response (201 Created):**

```json
{
  "status": "success",
  "data": {
    "id": "mkt_newmarket_002",
    "event_id": "evt_001",
    "name": "Top Batsman",
    "type": "fancy",
    "status": "open",
    "runners": [
      { "id": "run_001", "name": "Rohit Sharma", "sort_priority": 1 },
      { "id": "run_002", "name": "MS Dhoni", "sort_priority": 2 }
    ],
    "created_at": "2026-04-05T12:20:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 201    | Market created.                            |
| 400    | Validation error.                          |
| 401    | Not authenticated.                         |
| 403    | Insufficient role.                         |

---

### Wallet

#### GET /api/v1/wallet/balance

Get the authenticated user's wallet balance.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "user_id": "usr_abc123",
    "balance": 50000.00,
    "exposure": 12000.00,
    "available_balance": 38000.00,
    "credit_limit": 100000.00,
    "currency": "INR"
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Wallet balance.        |
| 401    | Not authenticated.     |

---

#### GET /api/v1/wallet/ledger?limit={n}&offset={n}

Get the transaction ledger for the authenticated user.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20, max 100). |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "txn_001",
      "type": "bet_placement",
      "amount": -1000.00,
      "balance_after": 49000.00,
      "description": "Back bet on Mumbai Indians @ 1.85",
      "reference_id": "bet_x7k9m2",
      "created_at": "2026-04-05T12:10:00Z"
    },
    {
      "id": "txn_002",
      "type": "deposit",
      "amount": 10000.00,
      "balance_after": 50000.00,
      "description": "UPI deposit",
      "reference_id": "pay_upi_001",
      "created_at": "2026-04-05T11:00:00Z"
    }
  ],
  "pagination": {
    "total": 156,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description             |
|--------|-------------------------|
| 200    | Ledger entries.         |
| 401    | Not authenticated.      |

---

#### POST /api/v1/wallet/deposit

Add funds to the wallet (internal/credit-based deposit by agent or admin).

**Authentication:** Required (JWT, agent role or above)

**Request Body:**

```json
{
  "user_id": "string",
  "amount": "number (positive)",
  "remark": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "transaction_id": "txn_003",
    "user_id": "usr_abc123",
    "amount": 5000.00,
    "new_balance": 55000.00,
    "remark": "Weekly credit top-up",
    "created_at": "2026-04-05T12:30:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | Deposit successful.                        |
| 400    | Validation error.                          |
| 401    | Not authenticated.                         |
| 403    | Insufficient role.                         |
| 422    | Amount exceeds available credit limit.     |

---

### Payment

#### POST /api/v1/payment/deposit/upi

Initiate a UPI deposit.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "amount": "number (min 100)",
  "upi_id": "string (optional, for preferred VPA)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "payment_id": "pay_upi_002",
    "amount": 5000,
    "upi_deeplink": "upi://pay?pa=lotus@ybl&pn=LotusExchange&am=5000&tr=pay_upi_002",
    "qr_code_url": "https://cdn.lotusexchange.com/qr/pay_upi_002.png",
    "expires_at": "2026-04-05T12:45:00Z",
    "status": "pending"
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | UPI payment initiated.         |
| 400    | Invalid amount.                |
| 401    | Not authenticated.             |

---

#### POST /api/v1/payment/deposit/crypto

Initiate a cryptocurrency deposit.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "amount": "number",
  "currency": "USDT | BTC | ETH",
  "network": "TRC20 | ERC20 | BEP20 | BTC"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "payment_id": "pay_crypto_001",
    "amount": 100,
    "currency": "USDT",
    "network": "TRC20",
    "deposit_address": "TXyz123abc456def789ghi012jkl345mno",
    "exchange_rate": 83.50,
    "inr_equivalent": 8350,
    "expires_at": "2026-04-05T13:00:00Z",
    "status": "awaiting_payment"
  }
}
```

| Status | Description                          |
|--------|--------------------------------------|
| 200    | Crypto deposit address generated.    |
| 400    | Invalid currency or network.         |
| 401    | Not authenticated.                   |

---

#### POST /api/v1/payment/withdraw

Request a withdrawal.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "amount": "number (min 500)",
  "method": "upi | bank_transfer | crypto",
  "details": {
    "upi_id": "string (if method is upi)",
    "account_number": "string (if method is bank_transfer)",
    "ifsc": "string (if method is bank_transfer)",
    "account_name": "string (if method is bank_transfer)",
    "crypto_address": "string (if method is crypto)",
    "crypto_network": "string (if method is crypto)"
  }
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "withdrawal_id": "wd_001",
    "amount": 5000,
    "method": "upi",
    "status": "pending_review",
    "estimated_completion": "2026-04-05T18:00:00Z",
    "created_at": "2026-04-05T12:35:00Z"
  }
}
```

| Status | Description                                    |
|--------|------------------------------------------------|
| 200    | Withdrawal request submitted.                  |
| 400    | Validation error.                              |
| 401    | Not authenticated.                             |
| 422    | Insufficient balance or KYC not completed.     |

---

#### GET /api/v1/payment/transactions?limit={n}&offset={n}

List payment transactions for the authenticated user.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20, max 100). |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "pay_upi_002",
      "type": "deposit",
      "method": "upi",
      "amount": 5000,
      "status": "completed",
      "created_at": "2026-04-05T12:00:00Z",
      "completed_at": "2026-04-05T12:02:00Z"
    },
    {
      "id": "wd_001",
      "type": "withdrawal",
      "method": "upi",
      "amount": 5000,
      "status": "pending_review",
      "created_at": "2026-04-05T12:35:00Z",
      "completed_at": null
    }
  ],
  "pagination": {
    "total": 34,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description                |
|--------|----------------------------|
| 200    | Transaction list.          |
| 401    | Not authenticated.         |

---

#### GET /api/v1/payment/transaction/{id}

Get details of a specific payment transaction.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description      |
|-----------|--------|------------------|
| id        | string | Transaction ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "pay_upi_002",
    "type": "deposit",
    "method": "upi",
    "amount": 5000,
    "status": "completed",
    "gateway_reference": "rzp_pay_abc123",
    "metadata": {
      "upi_id": "user@upi"
    },
    "created_at": "2026-04-05T12:00:00Z",
    "completed_at": "2026-04-05T12:02:00Z"
  }
}
```

| Status | Description                        |
|--------|------------------------------------|
| 200    | Transaction details.               |
| 401    | Not authenticated.                 |
| 404    | Transaction not found.             |

---

### Payment Webhooks

These endpoints are called by payment gateways. They are public but protected by signature verification.

#### POST /api/v1/payment/webhook/razorpay

Receive payment status updates from Razorpay.

**Authentication:** Razorpay webhook signature verification (`X-Razorpay-Signature` header)

**Request Body:** Razorpay standard webhook payload.

```json
{
  "entity": "event",
  "event": "payment.captured",
  "payload": {
    "payment": {
      "entity": {
        "id": "pay_abc123",
        "amount": 500000,
        "currency": "INR",
        "status": "captured",
        "notes": {
          "payment_id": "pay_upi_002"
        }
      }
    }
  }
}
```

**Response (200 OK):**

```json
{
  "status": "success"
}
```

| Status | Description                              |
|--------|------------------------------------------|
| 200    | Webhook processed.                       |
| 400    | Invalid signature or malformed payload.  |

---

#### POST /api/v1/payment/webhook/crypto

Receive transaction confirmations from the crypto payment processor.

**Authentication:** HMAC signature verification (`X-Webhook-Signature` header)

**Request Body:**

```json
{
  "transaction_id": "string",
  "address": "string",
  "amount": "number",
  "currency": "USDT",
  "network": "TRC20",
  "confirmations": 20,
  "status": "confirmed",
  "tx_hash": "0xabc123..."
}
```

**Response (200 OK):**

```json
{
  "status": "success"
}
```

| Status | Description                              |
|--------|------------------------------------------|
| 200    | Webhook processed.                       |
| 400    | Invalid signature or payload.            |

---

### Casino (Protected)

#### POST /api/v1/casino/session

Start a new casino game session.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "game_id": "string",
  "currency": "INR (default)"
}
```

**Response (201 Created):**

```json
{
  "status": "success",
  "data": {
    "session_id": "cas_sess_001",
    "game_id": "game_roulette01",
    "game_url": "https://games.lotusexchange.com/launch/cas_sess_001?token=abc123",
    "balance": 50000.00,
    "created_at": "2026-04-05T13:00:00Z"
  }
}
```

| Status | Description                            |
|--------|----------------------------------------|
| 201    | Session created.                       |
| 400    | Invalid game ID.                       |
| 401    | Not authenticated.                     |
| 422    | Insufficient balance.                  |

---

#### GET /api/v1/casino/session/{id}

Get status of an active casino session.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description   |
|-----------|--------|---------------|
| id        | string | Session ID.   |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "session_id": "cas_sess_001",
    "game_id": "game_roulette01",
    "game_name": "Lightning Roulette",
    "status": "active",
    "total_wagered": 15000,
    "total_won": 12000,
    "net_pnl": -3000,
    "started_at": "2026-04-05T13:00:00Z",
    "last_activity_at": "2026-04-05T13:15:00Z"
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Session details.         |
| 401    | Not authenticated.       |
| 404    | Session not found.       |

---

#### DELETE /api/v1/casino/session/{id}

End (close) an active casino session.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description   |
|-----------|--------|---------------|
| id        | string | Session ID.   |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "session_id": "cas_sess_001",
    "status": "closed",
    "total_wagered": 25000,
    "total_won": 18000,
    "net_pnl": -7000,
    "duration_seconds": 1800,
    "closed_at": "2026-04-05T13:30:00Z"
  }
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | Session closed.              |
| 401    | Not authenticated.           |
| 404    | Session not found.           |

---

#### GET /api/v1/casino/history

Get the authenticated user's casino game history.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20, max 100). |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "session_id": "cas_sess_001",
      "game_name": "Lightning Roulette",
      "provider": "Evolution Gaming",
      "total_wagered": 25000,
      "total_won": 18000,
      "net_pnl": -7000,
      "started_at": "2026-04-05T13:00:00Z",
      "closed_at": "2026-04-05T13:30:00Z"
    }
  ],
  "pagination": {
    "total": 42,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Casino history.        |
| 401    | Not authenticated.     |

---

#### POST /api/v1/casino/webhook/settlement

Receive round settlement callbacks from the game provider.

**Authentication:** Required (JWT, internal service token)

**Request Body:**

```json
{
  "session_id": "string",
  "round_id": "string",
  "action": "bet | win | refund",
  "amount": "number",
  "game_id": "string",
  "timestamp": "string (ISO 8601)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "transaction_id": "cas_txn_001",
    "balance": 48000.00
  }
}
```

| Status | Description                          |
|--------|--------------------------------------|
| 200    | Settlement processed.                |
| 400    | Invalid payload.                     |
| 401    | Invalid service token.               |
| 409    | Duplicate round settlement.          |

---

### Cashout

#### POST /api/v1/cashout/offer

Request a cashout offer for an active bet.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "bet_id": "string"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "offer_id": "co_offer_001",
    "bet_id": "bet_x7k9m2",
    "original_stake": 1000,
    "cashout_amount": 780,
    "profit_loss": -220,
    "expires_at": "2026-04-05T12:20:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | Cashout offer generated.                   |
| 401    | Not authenticated.                         |
| 404    | Bet not found.                             |
| 422    | Bet not eligible for cashout.              |

---

#### POST /api/v1/cashout/accept

Accept a cashout offer.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "offer_id": "string"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "offer_id": "co_offer_001",
    "bet_id": "bet_x7k9m2",
    "cashout_amount": 780,
    "status": "settled",
    "new_balance": 50780.00,
    "settled_at": "2026-04-05T12:18:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | Cashout accepted and settled.              |
| 401    | Not authenticated.                         |
| 404    | Offer not found.                           |
| 410    | Offer expired.                             |
| 422    | Offer no longer valid (odds changed).      |

---

#### GET /api/v1/cashout/offers

List all active cashout offers for the authenticated user.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "offer_id": "co_offer_001",
      "bet_id": "bet_x7k9m2",
      "market_name": "Match Odds",
      "runner_name": "Mumbai Indians",
      "original_stake": 1000,
      "cashout_amount": 780,
      "expires_at": "2026-04-05T12:20:00Z"
    }
  ]
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Active cashout offers.   |
| 401    | Not authenticated.       |

---

### Hierarchy

#### GET /api/v1/hierarchy/children

Get all downstream users (recursively) in the user's hierarchy.

**Authentication:** Required (JWT, agent role or above)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20).          |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "usr_child01",
      "username": "agent_john",
      "role": "agent",
      "status": "active",
      "balance": 25000,
      "exposure": 5000,
      "parent_id": "usr_abc123",
      "children_count": 15,
      "created_at": "2026-01-15T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 48,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Hierarchy children.    |
| 401    | Not authenticated.     |
| 403    | Insufficient role.     |

---

#### GET /api/v1/hierarchy/children/direct

Get only direct (immediate) children of the authenticated user.

**Authentication:** Required (JWT, agent role or above)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "usr_child01",
      "username": "agent_john",
      "role": "agent",
      "status": "active",
      "balance": 25000,
      "exposure": 5000,
      "created_at": "2026-01-15T10:00:00Z"
    }
  ]
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Direct children.               |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |

---

#### POST /api/v1/hierarchy/credit/transfer

Transfer credit to a direct child user.

**Authentication:** Required (JWT, agent role or above)

**Request Body:**

```json
{
  "to_user_id": "string",
  "amount": "number (positive for deposit, negative for withdrawal)",
  "remark": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "transaction_id": "cred_txn_001",
    "from_user_id": "usr_abc123",
    "to_user_id": "usr_child01",
    "amount": 10000,
    "from_new_balance": 40000,
    "to_new_balance": 35000,
    "remark": "Weekly credit",
    "created_at": "2026-04-05T13:00:00Z"
  }
}
```

| Status | Description                                          |
|--------|------------------------------------------------------|
| 200    | Credit transferred.                                  |
| 400    | Invalid amount or user.                              |
| 401    | Not authenticated.                                   |
| 403    | Target user is not a direct child.                   |
| 422    | Insufficient balance for transfer.                   |

---

#### GET /api/v1/hierarchy/user/{id}

Get details of a specific downstream user.

**Authentication:** Required (JWT, must be an ancestor of the target user)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_child01",
    "username": "agent_john",
    "role": "agent",
    "status": "active",
    "email": "john@example.com",
    "phone": "+919876543210",
    "balance": 25000,
    "exposure": 5000,
    "credit_limit": 50000,
    "commission_rate": 2.5,
    "parent_id": "usr_abc123",
    "children_count": 15,
    "created_at": "2026-01-15T10:00:00Z",
    "last_login": "2026-04-05T10:00:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | User details.                              |
| 401    | Not authenticated.                         |
| 403    | User is not in your hierarchy.             |
| 404    | User not found.                            |

---

#### PUT /api/v1/hierarchy/user/{id}/status

Update the status of a downstream user (activate, suspend, lock).

**Authentication:** Required (JWT, must be an ancestor of the target user)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Request Body:**

```json
{
  "status": "active | suspended | locked",
  "reason": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_child01",
    "username": "agent_john",
    "status": "suspended",
    "reason": "Suspicious betting pattern",
    "updated_at": "2026-04-05T13:10:00Z"
  }
}
```

| Status | Description                                |
|--------|--------------------------------------------|
| 200    | Status updated.                            |
| 400    | Invalid status value.                      |
| 401    | Not authenticated.                         |
| 403    | User is not in your hierarchy.             |
| 404    | User not found.                            |

---

### Risk

#### GET /api/v1/risk/market/{id}

Get risk exposure for a specific market.

**Authentication:** Required (JWT, agent role or above)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "market_name": "Match Odds",
    "total_back_exposure": 750000,
    "total_lay_exposure": 620000,
    "net_exposure": 130000,
    "runner_exposures": [
      {
        "runner_id": "run_mi",
        "runner_name": "Mumbai Indians",
        "pnl_if_wins": -85000,
        "pnl_if_loses": 130000
      },
      {
        "runner_id": "run_csk",
        "runner_name": "Chennai Super Kings",
        "pnl_if_wins": 130000,
        "pnl_if_loses": -85000
      }
    ]
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Market risk data.        |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |
| 404    | Market not found.        |

---

#### GET /api/v1/risk/user/{id}

Get risk exposure for a specific user.

**Authentication:** Required (JWT, must be an ancestor or the user themselves)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "user_id": "usr_abc123",
    "username": "johndoe",
    "total_exposure": 12000,
    "balance": 50000,
    "available_balance": 38000,
    "active_bets": 5,
    "market_exposures": [
      {
        "market_id": "mkt_matchodds_001",
        "market_name": "Match Odds",
        "exposure": 8000,
        "potential_loss": -8000,
        "potential_profit": 6800
      }
    ]
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | User risk data.          |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |
| 404    | User not found.          |

---

#### GET /api/v1/risk/exposure

Get aggregate risk exposure for the authenticated user's hierarchy.

**Authentication:** Required (JWT, agent role or above)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "total_exposure": 450000,
    "total_users_with_exposure": 32,
    "top_exposures": [
      {
        "user_id": "usr_client01",
        "username": "high_roller",
        "exposure": 85000,
        "balance": 120000
      }
    ],
    "market_summary": [
      {
        "market_id": "mkt_matchodds_001",
        "market_name": "Match Odds",
        "total_exposure": 200000,
        "users_count": 18
      }
    ]
  }
}
```

| Status | Description                      |
|--------|----------------------------------|
| 200    | Aggregate exposure data.         |
| 401    | Not authenticated.               |
| 403    | Insufficient role.               |

---

### Reports

#### GET /api/v1/reports/pnl

Get profit and loss report for the authenticated user.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter  | Type   | Required | Description                                 |
|------------|--------|----------|---------------------------------------------|
| from       | string | No       | Start date (ISO 8601, default: 30 days ago).|
| to         | string | No       | End date (ISO 8601, default: now).           |
| sport      | string | No       | Filter by sport slug.                        |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "period": {
      "from": "2026-03-06T00:00:00Z",
      "to": "2026-04-05T23:59:59Z"
    },
    "total_bets": 142,
    "total_stake": 250000,
    "total_profit": 18500,
    "total_loss": -12000,
    "net_pnl": 6500,
    "commission_paid": 325,
    "by_sport": [
      {
        "sport": "cricket",
        "bets": 98,
        "net_pnl": 8200
      },
      {
        "sport": "football",
        "bets": 44,
        "net_pnl": -1700
      }
    ]
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | PnL report.            |
| 401    | Not authenticated.     |

---

#### GET /api/v1/reports/market/{id}

Get a report for a specific market.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "market_name": "Match Odds",
    "event_name": "Mumbai Indians v Chennai Super Kings",
    "status": "settled",
    "winning_runner": "run_mi",
    "total_matched": 1250000,
    "your_bets": [
      {
        "bet_id": "bet_x7k9m2",
        "runner_name": "Mumbai Indians",
        "side": "back",
        "price": 1.85,
        "stake": 1000,
        "pnl": 850,
        "status": "won"
      }
    ],
    "your_net_pnl": 850
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Market report.           |
| 401    | Not authenticated.       |
| 404    | Market not found.        |

---

#### GET /api/v1/reports/dashboard

Get dashboard summary data for the authenticated user.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "balance": 50000,
    "exposure": 12000,
    "available_balance": 38000,
    "today_pnl": 2500,
    "week_pnl": 6500,
    "active_bets": 5,
    "recent_bets": [
      {
        "bet_id": "bet_x7k9m2",
        "market_name": "Match Odds",
        "runner_name": "Mumbai Indians",
        "side": "back",
        "stake": 1000,
        "status": "matched",
        "placed_at": "2026-04-05T12:10:00Z"
      }
    ],
    "upcoming_events": [
      {
        "event_id": "evt_001",
        "name": "Mumbai Indians v Chennai Super Kings",
        "starts_at": "2026-04-10T14:00:00Z"
      }
    ]
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Dashboard data.          |
| 401    | Not authenticated.       |

---

#### GET /api/v1/reports/volume

Get trading volume report.

**Authentication:** Required (JWT, agent role or above)

**Query Parameters:**

| Parameter  | Type   | Required | Description                                  |
|------------|--------|----------|----------------------------------------------|
| from       | string | No       | Start date (ISO 8601, default: 30 days ago). |
| to         | string | No       | End date (ISO 8601, default: now).            |
| group_by   | string | No       | `day`, `week`, `month` (default: `day`).      |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "total_volume": 15000000,
    "total_bets": 4520,
    "unique_users": 312,
    "by_period": [
      {
        "date": "2026-04-05",
        "volume": 850000,
        "bets": 245,
        "users": 89
      }
    ],
    "by_sport": [
      {
        "sport": "cricket",
        "volume": 10500000,
        "percentage": 70.0
      }
    ]
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Volume report.           |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |

---

#### GET /api/v1/reports/hierarchy/pnl

Get PnL report aggregated across the user's hierarchy.

**Authentication:** Required (JWT, agent role or above)

**Query Parameters:**

| Parameter | Type   | Required | Description                                  |
|-----------|--------|----------|----------------------------------------------|
| from      | string | No       | Start date (ISO 8601, default: 30 days ago). |
| to        | string | No       | End date (ISO 8601, default: now).            |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "period": {
      "from": "2026-03-06T00:00:00Z",
      "to": "2026-04-05T23:59:59Z"
    },
    "total_pnl": 125000,
    "total_commission": 6250,
    "children_summary": [
      {
        "user_id": "usr_child01",
        "username": "agent_john",
        "role": "agent",
        "pnl": 45000,
        "commission": 2250,
        "downstream_users": 15
      }
    ]
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Hierarchy PnL report.    |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |

---

### KYC

#### GET /api/v1/kyc/status

Get the KYC verification status for the authenticated user.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "kyc_status": "verified | pending | rejected | not_submitted",
    "submitted_at": "2026-03-20T10:00:00Z",
    "verified_at": "2026-03-21T14:00:00Z",
    "rejection_reason": null,
    "documents": [
      {
        "type": "aadhaar",
        "status": "verified",
        "submitted_at": "2026-03-20T10:00:00Z"
      },
      {
        "type": "pan",
        "status": "verified",
        "submitted_at": "2026-03-20T10:00:00Z"
      }
    ]
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | KYC status.            |
| 401    | Not authenticated.     |

---

#### POST /api/v1/kyc/submit

Submit KYC documents for verification.

**Authentication:** Required (JWT)

**Request Body (multipart/form-data):**

| Field          | Type   | Required | Description                                  |
|----------------|--------|----------|----------------------------------------------|
| document_type  | string | Yes      | `aadhaar`, `pan`, `passport`, `driving_license`. |
| document_front | file   | Yes      | Front image (JPEG/PNG, max 5MB).             |
| document_back  | file   | No       | Back image (if applicable).                  |
| document_number| string | Yes      | Document number for verification.            |
| full_name      | string | Yes      | Name as it appears on the document.          |
| date_of_birth  | string | Yes      | Date of birth (YYYY-MM-DD).                  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "kyc_status": "pending",
    "document_type": "aadhaar",
    "submitted_at": "2026-04-05T13:00:00Z",
    "estimated_review_time": "24 hours"
  }
}
```

| Status | Description                                  |
|--------|----------------------------------------------|
| 200    | Documents submitted for review.              |
| 400    | Validation error (missing fields, bad file). |
| 401    | Not authenticated.                           |
| 409    | KYC already submitted and pending.           |

---

### Responsible Gambling

#### GET /api/v1/responsible-gambling/limits

Get the authenticated user's gambling limits.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "daily_deposit_limit": 50000,
    "weekly_deposit_limit": 200000,
    "monthly_deposit_limit": 500000,
    "daily_loss_limit": 25000,
    "weekly_loss_limit": 100000,
    "session_time_limit_minutes": 240,
    "daily_deposit_used": 5000,
    "weekly_deposit_used": 15000,
    "daily_loss_used": 2000
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Current limits.        |
| 401    | Not authenticated.     |

---

#### PUT /api/v1/responsible-gambling/limits

Update gambling limits. Decreases take effect immediately; increases have a 24-hour cooling period.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "daily_deposit_limit": "number (optional)",
  "weekly_deposit_limit": "number (optional)",
  "monthly_deposit_limit": "number (optional)",
  "daily_loss_limit": "number (optional)",
  "weekly_loss_limit": "number (optional)",
  "session_time_limit_minutes": "integer (optional, min 15)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "daily_deposit_limit": 30000,
    "effective_at": "2026-04-05T13:00:00Z",
    "message": "Limit decrease applied immediately."
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Limits updated.                |
| 400    | Invalid limit values.          |
| 401    | Not authenticated.             |

---

#### POST /api/v1/responsible-gambling/self-exclude

Self-exclude from the platform for a specified period.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "duration": "6_months | 1_year | 5_years | permanent",
  "reason": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "exclusion_id": "excl_001",
    "duration": "6_months",
    "starts_at": "2026-04-05T13:00:00Z",
    "ends_at": "2026-10-05T13:00:00Z",
    "message": "Your account has been self-excluded. All active bets will be settled. You will not be able to log in until the exclusion period ends."
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Self-exclusion applied.        |
| 400    | Invalid duration.              |
| 401    | Not authenticated.             |

---

#### POST /api/v1/responsible-gambling/cooling-off

Activate a short-term cooling-off period.

**Authentication:** Required (JWT)

**Request Body:**

```json
{
  "hours": "integer (1, 6, 12, 24, 48, 72)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "cooling_off_until": "2026-04-06T13:00:00Z",
    "message": "Cooling-off period activated. You will not be able to place bets for 24 hours."
  }
}
```

| Status | Description                        |
|--------|------------------------------------|
| 200    | Cooling-off period activated.      |
| 400    | Invalid hours value.               |
| 401    | Not authenticated.                 |

---

#### GET /api/v1/responsible-gambling/session

Get current session activity statistics.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "session_start": "2026-04-05T11:00:00Z",
    "duration_minutes": 120,
    "time_limit_minutes": 240,
    "time_remaining_minutes": 120,
    "bets_placed": 8,
    "total_wagered": 15000,
    "net_pnl": -2000
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Session statistics.      |
| 401    | Not authenticated.       |

---

### Notifications

#### GET /api/v1/notifications

Get notifications for the authenticated user.

**Authentication:** Required (JWT)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20, max 100). |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "notif_001",
      "type": "bet_settled",
      "title": "Bet Settled",
      "message": "Your bet on Mumbai Indians has won. Profit: 850 INR.",
      "read": false,
      "data": {
        "bet_id": "bet_x7k9m2",
        "market_id": "mkt_matchodds_001"
      },
      "created_at": "2026-04-05T14:00:00Z"
    }
  ],
  "pagination": {
    "total": 56,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Notification list.     |
| 401    | Not authenticated.     |

---

#### GET /api/v1/notifications/unread-count

Get the count of unread notifications.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "unread_count": 3
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Unread count.          |
| 401    | Not authenticated.     |

---

#### POST /api/v1/notifications/{id}/read

Mark a specific notification as read.

**Authentication:** Required (JWT)

**Path Parameters:**

| Parameter | Type   | Description       |
|-----------|--------|-------------------|
| id        | string | Notification ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "message": "Notification marked as read."
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Marked as read.                |
| 401    | Not authenticated.             |
| 404    | Notification not found.        |

---

#### POST /api/v1/notifications/read-all

Mark all notifications as read for the authenticated user.

**Authentication:** Required (JWT)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "marked_count": 3
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | All notifications marked read. |
| 401    | Not authenticated.             |

---

### Admin

All admin endpoints require JWT authentication with `superadmin` or `admin` role.

#### GET /api/v1/admin/dashboard

Get platform-wide dashboard statistics.

**Authentication:** Required (JWT, superadmin/admin)

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "total_users": 15420,
    "active_users_today": 2340,
    "total_volume_today": 8500000,
    "total_pnl_today": -125000,
    "active_markets": 45,
    "pending_withdrawals": 23,
    "pending_kyc": 12,
    "fraud_alerts": 3,
    "system_health": {
      "api_latency_ms": 45,
      "websocket_connections": 1820,
      "matching_engine_lag_ms": 2
    }
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Dashboard data.          |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |

---

#### GET /api/v1/admin/users

List all users with filtering and pagination.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type    | Required | Description                                          |
|-----------|---------|----------|------------------------------------------------------|
| role      | string  | No       | Filter by role.                                      |
| status    | string  | No       | Filter by status: `active`, `suspended`, `locked`.   |
| search    | string  | No       | Search by username or email.                         |
| limit     | integer | No       | Items per page (default 20).                         |
| offset    | integer | No       | Pagination offset.                                   |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "usr_abc123",
      "username": "johndoe",
      "email": "john@example.com",
      "role": "client",
      "status": "active",
      "balance": 50000,
      "exposure": 12000,
      "parent_id": "usr_agent01",
      "created_at": "2026-01-15T10:00:00Z",
      "last_login": "2026-04-05T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 15420,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | User list.             |
| 401    | Not authenticated.     |
| 403    | Insufficient role.     |

---

#### GET /api/v1/admin/users/{id}

Get detailed information about a specific user.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_abc123",
    "username": "johndoe",
    "email": "john@example.com",
    "phone": "+919876543210",
    "role": "client",
    "status": "active",
    "balance": 50000,
    "exposure": 12000,
    "available_balance": 38000,
    "credit_limit": 100000,
    "commission_rate": 2.0,
    "parent_id": "usr_agent01",
    "kyc_status": "verified",
    "total_bets": 342,
    "total_volume": 850000,
    "lifetime_pnl": 25000,
    "created_at": "2026-01-15T10:00:00Z",
    "last_login": "2026-04-05T10:00:00Z"
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | User details.          |
| 401    | Not authenticated.     |
| 403    | Insufficient role.     |
| 404    | User not found.        |

---

#### PUT /api/v1/admin/users/{id}/status

Update a user's account status.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Request Body:**

```json
{
  "status": "active | suspended | locked",
  "reason": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_abc123",
    "status": "suspended",
    "reason": "Terms of service violation",
    "updated_at": "2026-04-05T14:00:00Z",
    "updated_by": "usr_admin01"
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Status updated.                |
| 400    | Invalid status.                |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |
| 404    | User not found.                |

---

#### PUT /api/v1/admin/users/{id}/credit-limit

Update a user's credit limit.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Request Body:**

```json
{
  "credit_limit": "number (>= 0)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_abc123",
    "credit_limit": 200000,
    "previous_credit_limit": 100000,
    "updated_at": "2026-04-05T14:00:00Z",
    "updated_by": "usr_admin01"
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Credit limit updated.          |
| 400    | Invalid credit limit value.    |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |
| 404    | User not found.                |

---

#### PUT /api/v1/admin/users/{id}/commission

Update a user's commission rate.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Request Body:**

```json
{
  "commission_rate": "number (0.0 - 100.0, percentage)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "usr_abc123",
    "commission_rate": 3.5,
    "previous_commission_rate": 2.0,
    "updated_at": "2026-04-05T14:00:00Z",
    "updated_by": "usr_admin01"
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Commission rate updated.       |
| 400    | Invalid commission value.      |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |
| 404    | User not found.                |

---

#### GET /api/v1/admin/markets

List all markets with admin-level details.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type    | Required | Description                                                    |
|-----------|---------|----------|----------------------------------------------------------------|
| status    | string  | No       | Filter: `open`, `suspended`, `settled`, `voided`.              |
| sport     | string  | No       | Filter by sport slug.                                          |
| limit     | integer | No       | Items per page (default 20).                                   |
| offset    | integer | No       | Pagination offset.                                             |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "mkt_matchodds_001",
      "event_name": "Mumbai Indians v Chennai Super Kings",
      "name": "Match Odds",
      "sport": "cricket",
      "status": "open",
      "total_matched": 1250000,
      "total_bets": 342,
      "net_platform_pnl": 15000,
      "created_at": "2026-04-05T10:00:00Z"
    }
  ],
  "pagination": {
    "total": 245,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Market list.           |
| 401    | Not authenticated.     |
| 403    | Insufficient role.     |

---

#### POST /api/v1/admin/markets/{id}/suspend

Suspend a market, halting all new bets.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Request Body:**

```json
{
  "reason": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "suspended",
    "reason": "Rain delay",
    "suspended_at": "2026-04-05T14:30:00Z",
    "suspended_by": "usr_admin01"
  }
}
```

| Status | Description                            |
|--------|----------------------------------------|
| 200    | Market suspended.                      |
| 401    | Not authenticated.                     |
| 403    | Insufficient role.                     |
| 404    | Market not found.                      |
| 422    | Market already suspended or settled.   |

---

#### POST /api/v1/admin/markets/{id}/resume

Resume a previously suspended market.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "open",
    "resumed_at": "2026-04-05T15:00:00Z",
    "resumed_by": "usr_admin01"
  }
}
```

| Status | Description                      |
|--------|----------------------------------|
| 200    | Market resumed.                  |
| 401    | Not authenticated.               |
| 403    | Insufficient role.               |
| 404    | Market not found.                |
| 422    | Market is not currently suspended.|

---

#### POST /api/v1/admin/markets/{id}/settle

Settle a market by declaring the winning runner.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Request Body:**

```json
{
  "winning_runner_id": "string"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "settled",
    "winning_runner_id": "run_mi",
    "winning_runner_name": "Mumbai Indians",
    "total_settled_bets": 342,
    "total_payout": 980000,
    "platform_pnl": 15000,
    "settled_at": "2026-04-05T18:00:00Z",
    "settled_by": "usr_admin01"
  }
}
```

| Status | Description                              |
|--------|------------------------------------------|
| 200    | Market settled.                          |
| 400    | Invalid winning runner.                  |
| 401    | Not authenticated.                       |
| 403    | Insufficient role.                       |
| 404    | Market not found.                        |
| 422    | Market already settled or voided.        |

---

#### POST /api/v1/admin/markets/{id}/void

Void a market, refunding all bets.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | Market ID.  |

**Request Body:**

```json
{
  "reason": "string"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "market_id": "mkt_matchodds_001",
    "status": "voided",
    "reason": "Match abandoned due to weather",
    "total_refunded_bets": 342,
    "total_refund_amount": 1250000,
    "voided_at": "2026-04-05T18:00:00Z",
    "voided_by": "usr_admin01"
  }
}
```

| Status | Description                              |
|--------|------------------------------------------|
| 200    | Market voided, all bets refunded.        |
| 400    | Missing reason.                          |
| 401    | Not authenticated.                       |
| 403    | Insufficient role.                       |
| 404    | Market not found.                        |
| 422    | Market already settled or voided.        |

---

#### GET /api/v1/admin/bets

List all bets across the platform with filtering.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter  | Type    | Required | Description                                                 |
|------------|---------|----------|-------------------------------------------------------------|
| user_id    | string  | No       | Filter by user.                                             |
| market_id  | string  | No       | Filter by market.                                           |
| status     | string  | No       | Filter: `matched`, `unmatched`, `cancelled`, `settled`.     |
| side       | string  | No       | Filter: `back`, `lay`.                                      |
| min_stake  | number  | No       | Minimum stake filter.                                       |
| limit      | integer | No       | Items per page (default 20).                                |
| offset     | integer | No       | Pagination offset.                                          |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "bet_id": "bet_x7k9m2",
      "user_id": "usr_abc123",
      "username": "johndoe",
      "market_id": "mkt_matchodds_001",
      "market_name": "Match Odds",
      "runner_name": "Mumbai Indians",
      "side": "back",
      "price": 1.85,
      "stake": 1000,
      "matched_amount": 1000,
      "status": "matched",
      "pnl": null,
      "placed_at": "2026-04-05T12:10:00Z"
    }
  ],
  "pagination": {
    "total": 8450,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

| Status | Description            |
|--------|------------------------|
| 200    | Bet list.              |
| 401    | Not authenticated.     |
| 403    | Insufficient role.     |

---

#### GET /api/v1/admin/reports/pnl

Get platform-wide PnL report.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type   | Required | Description                                  |
|-----------|--------|----------|----------------------------------------------|
| from      | string | No       | Start date (ISO 8601, default: 30 days ago). |
| to        | string | No       | End date (ISO 8601, default: now).            |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "period": {
      "from": "2026-03-06T00:00:00Z",
      "to": "2026-04-05T23:59:59Z"
    },
    "platform_pnl": 1250000,
    "total_volume": 85000000,
    "total_bets": 45200,
    "total_commission": 425000,
    "by_sport": [
      {
        "sport": "cricket",
        "pnl": 875000,
        "volume": 59500000
      }
    ],
    "by_day": [
      {
        "date": "2026-04-05",
        "pnl": 45000,
        "volume": 2800000
      }
    ]
  }
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | Platform PnL report.         |
| 401    | Not authenticated.           |
| 403    | Insufficient role.           |

---

#### GET /api/v1/admin/reports/volume

Get platform-wide trading volume report.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type   | Required | Description                                  |
|-----------|--------|----------|----------------------------------------------|
| from      | string | No       | Start date (ISO 8601, default: 30 days ago). |
| to        | string | No       | End date (ISO 8601, default: now).            |
| group_by  | string | No       | `day`, `week`, `month` (default: `day`).      |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "total_volume": 85000000,
    "total_bets": 45200,
    "unique_users": 4320,
    "by_period": [
      {
        "date": "2026-04-05",
        "volume": 2800000,
        "bets": 1520,
        "users": 890
      }
    ]
  }
}
```

| Status | Description                  |
|--------|------------------------------|
| 200    | Volume report.               |
| 401    | Not authenticated.           |
| 403    | Insufficient role.           |

---

#### GET /api/v1/admin/fraud/alerts

Get active fraud alerts.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type    | Required | Description                               |
|-----------|---------|----------|-------------------------------------------|
| status    | string  | No       | Filter: `open`, `resolved`, `dismissed`.  |
| severity  | string  | No       | Filter: `low`, `medium`, `high`, `critical`.|
| limit     | integer | No       | Items per page (default 20).              |
| offset    | integer | No       | Pagination offset.                        |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "fraud_alert_001",
      "type": "unusual_betting_pattern",
      "severity": "high",
      "status": "open",
      "user_id": "usr_suspect01",
      "username": "suspect_user",
      "description": "User placed 15 large bets in 2 minutes on the same market.",
      "details": {
        "market_id": "mkt_matchodds_001",
        "bet_count": 15,
        "total_stake": 500000,
        "time_window_seconds": 120
      },
      "created_at": "2026-04-05T14:00:00Z"
    }
  ],
  "pagination": {
    "total": 8,
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Fraud alert list.        |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |

---

#### GET /api/v1/admin/kyc/pending

Get all pending KYC submissions awaiting review.

**Authentication:** Required (JWT, superadmin/admin)

**Query Parameters:**

| Parameter | Type    | Required | Description                           |
|-----------|---------|----------|---------------------------------------|
| limit     | integer | No       | Items per page (default 20).          |
| offset    | integer | No       | Pagination offset.                    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "kyc_sub_001",
      "user_id": "usr_abc123",
      "username": "johndoe",
      "document_type": "aadhaar",
      "document_number": "XXXX-XXXX-1234",
      "full_name": "John Doe",
      "submitted_at": "2026-04-05T13:00:00Z"
    }
  ],
  "pagination": {
    "total": 12,
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Pending KYC list.              |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |

---

#### POST /api/v1/admin/kyc/{id}/approve

Approve a KYC submission.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description        |
|-----------|--------|--------------------|
| id        | string | KYC submission ID. |

**Request Body:**

```json
{
  "notes": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "kyc_sub_001",
    "user_id": "usr_abc123",
    "status": "approved",
    "approved_by": "usr_admin01",
    "approved_at": "2026-04-05T15:00:00Z"
  }
}
```

| Status | Description                            |
|--------|----------------------------------------|
| 200    | KYC approved.                          |
| 401    | Not authenticated.                     |
| 403    | Insufficient role.                     |
| 404    | KYC submission not found.              |
| 422    | Submission not in pending state.       |

---

#### POST /api/v1/admin/kyc/{id}/reject

Reject a KYC submission.

**Authentication:** Required (JWT, superadmin/admin)

**Path Parameters:**

| Parameter | Type   | Description        |
|-----------|--------|--------------------|
| id        | string | KYC submission ID. |

**Request Body:**

```json
{
  "reason": "string (required)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "kyc_sub_001",
    "user_id": "usr_abc123",
    "status": "rejected",
    "reason": "Document image is blurry and unreadable.",
    "rejected_by": "usr_admin01",
    "rejected_at": "2026-04-05T15:00:00Z"
  }
}
```

| Status | Description                            |
|--------|----------------------------------------|
| 200    | KYC rejected.                          |
| 400    | Missing rejection reason.              |
| 401    | Not authenticated.                     |
| 403    | Insufficient role.                     |
| 404    | KYC submission not found.              |
| 422    | Submission not in pending state.       |

---

### Fraud

#### GET /api/v1/fraud/alerts

Get fraud alerts (similar to admin fraud alerts but scoped to the user's hierarchy).

**Authentication:** Required (JWT, admin role or above)

**Query Parameters:**

| Parameter | Type    | Required | Description                               |
|-----------|---------|----------|-------------------------------------------|
| status    | string  | No       | Filter: `open`, `resolved`, `dismissed`.  |
| severity  | string  | No       | Filter: `low`, `medium`, `high`, `critical`.|
| limit     | integer | No       | Items per page (default 20).              |
| offset    | integer | No       | Pagination offset.                        |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": [
    {
      "id": "fraud_alert_001",
      "type": "unusual_betting_pattern",
      "severity": "high",
      "status": "open",
      "user_id": "usr_suspect01",
      "username": "suspect_user",
      "description": "User placed 15 large bets in 2 minutes on the same market.",
      "created_at": "2026-04-05T14:00:00Z"
    }
  ],
  "pagination": {
    "total": 3,
    "limit": 20,
    "offset": 0,
    "has_more": false
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | Fraud alert list.        |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |

---

#### POST /api/v1/fraud/alerts/{id}/resolve

Resolve a fraud alert.

**Authentication:** Required (JWT, admin role or above)

**Path Parameters:**

| Parameter | Type   | Description      |
|-----------|--------|------------------|
| id        | string | Fraud alert ID.  |

**Request Body:**

```json
{
  "resolution": "confirmed_fraud | false_positive | under_monitoring",
  "action_taken": "string (optional, e.g., 'Account suspended')",
  "notes": "string (optional)"
}
```

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "id": "fraud_alert_001",
    "status": "resolved",
    "resolution": "confirmed_fraud",
    "action_taken": "Account suspended",
    "resolved_by": "usr_admin01",
    "resolved_at": "2026-04-05T16:00:00Z"
  }
}
```

| Status | Description                    |
|--------|--------------------------------|
| 200    | Alert resolved.                |
| 400    | Invalid resolution type.       |
| 401    | Not authenticated.             |
| 403    | Insufficient role.             |
| 404    | Alert not found.               |

---

#### GET /api/v1/fraud/user/{id}/risk

Get the fraud risk profile for a specific user.

**Authentication:** Required (JWT, admin role or above)

**Path Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| id        | string | User ID.    |

**Response (200 OK):**

```json
{
  "status": "success",
  "data": {
    "user_id": "usr_suspect01",
    "username": "suspect_user",
    "risk_score": 78,
    "risk_level": "high",
    "flags": [
      {
        "type": "rapid_betting",
        "description": "Frequently places many bets in short time windows.",
        "occurrences": 12,
        "last_seen": "2026-04-05T14:00:00Z"
      },
      {
        "type": "large_stake_variance",
        "description": "Stake sizes vary by more than 10x within single sessions.",
        "occurrences": 5,
        "last_seen": "2026-04-04T22:00:00Z"
      }
    ],
    "related_accounts": [
      {
        "user_id": "usr_related01",
        "username": "related_user",
        "relationship": "same_ip",
        "confidence": 0.85
      }
    ],
    "alerts_count": {
      "open": 2,
      "resolved": 5,
      "total": 7
    }
  }
}
```

| Status | Description              |
|--------|--------------------------|
| 200    | User risk profile.       |
| 401    | Not authenticated.       |
| 403    | Insufficient role.       |
| 404    | User not found.          |

---

## WebSocket Protocol

### Connection

Connect to the WebSocket server with a valid JWT token:

```
wss://api.lotusexchange.com/ws?token=<jwt_token>
```

The connection is authenticated on open. If the token is invalid or expired, the server closes the connection with code `4001`.

### Message Format

All messages are JSON-encoded.

**Client to Server:**

```json
{
  "type": "string",
  "data": {}
}
```

**Server to Client:**

```json
{
  "type": "string",
  "data": {},
  "timestamp": "string (ISO 8601)"
}
```

### Message Types

#### subscribe

Subscribe to real-time updates for specific markets.

**Client sends:**

```json
{
  "type": "subscribe",
  "data": {
    "channels": ["market:mkt_matchodds_001", "market:mkt_bookmaker_001"]
  }
}
```

**Server acknowledges:**

```json
{
  "type": "subscribed",
  "data": {
    "channels": ["market:mkt_matchodds_001", "market:mkt_bookmaker_001"]
  },
  "timestamp": "2026-04-05T12:00:00Z"
}
```

To unsubscribe:

```json
{
  "type": "unsubscribe",
  "data": {
    "channels": ["market:mkt_matchodds_001"]
  }
}
```

#### ping / pong

Keep the connection alive. The client should send a ping every 30 seconds. If the server does not receive a ping within 60 seconds, the connection is closed.

**Client sends:**

```json
{
  "type": "ping"
}
```

**Server responds:**

```json
{
  "type": "pong",
  "timestamp": "2026-04-05T12:00:30Z"
}
```

#### odds_update

Real-time odds update pushed by the server for subscribed markets.

**Server sends:**

```json
{
  "type": "odds_update",
  "data": {
    "market_id": "mkt_matchodds_001",
    "runners": [
      {
        "runner_id": "run_mi",
        "back": [
          { "price": 1.86, "size": 48000 },
          { "price": 1.85, "size": 22000 },
          { "price": 1.84, "size": 11000 }
        ],
        "lay": [
          { "price": 1.88, "size": 38000 },
          { "price": 1.89, "size": 27000 },
          { "price": 1.90, "size": 14000 }
        ],
        "last_traded_price": 1.87
      }
    ],
    "status": "open"
  },
  "timestamp": "2026-04-05T12:05:31Z"
}
```

#### Other Server-Pushed Events

The server may push additional event types to connected clients:

| Type               | Description                                       |
|--------------------|---------------------------------------------------|
| `bet_matched`      | A pending bet has been matched.                   |
| `bet_settled`      | A bet has been settled (market result declared).  |
| `market_suspended` | A subscribed market has been suspended.           |
| `market_resumed`   | A subscribed market has resumed.                  |
| `balance_update`   | The user's wallet balance has changed.            |
| `notification`     | A new notification for the user.                  |

**Example -- bet_matched:**

```json
{
  "type": "bet_matched",
  "data": {
    "bet_id": "bet_x7k9m2",
    "market_id": "mkt_matchodds_001",
    "matched_amount": 500,
    "remaining_unmatched": 0,
    "status": "matched"
  },
  "timestamp": "2026-04-05T12:10:05Z"
}
```

**Example -- balance_update:**

```json
{
  "type": "balance_update",
  "data": {
    "balance": 49000,
    "exposure": 13000,
    "available_balance": 36000
  },
  "timestamp": "2026-04-05T12:10:05Z"
}
```

### Error Handling

If the client sends a malformed message, the server responds:

```json
{
  "type": "error",
  "data": {
    "code": "INVALID_MESSAGE",
    "message": "Message must be valid JSON with a 'type' field."
  },
  "timestamp": "2026-04-05T12:00:00Z"
}
```

### Connection Close Codes

| Code | Description                          |
|------|--------------------------------------|
| 1000 | Normal closure.                     |
| 4001 | Authentication failed.              |
| 4002 | Token expired (reconnect with new). |
| 4003 | Rate limit exceeded.                |
| 4004 | Account suspended.                  |

---

## Testing Guide

### Prerequisites

- A running instance of the Lotus Exchange API (default: `http://localhost:8080`)
- `curl` and `jq` installed

### Environment Setup

```bash
# Set base URL
export BASE_URL="http://localhost:8080"
```

### 1. Health Check

```bash
curl -s "$BASE_URL/health" | jq .
```

### 2. Register a New User

```bash
curl -s -X POST "$BASE_URL/api/v1/auth/register" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "email": "test@example.com",
    "password": "SecurePass123"
  }' | jq .
```

### 3. Login

```bash
# Login and capture tokens
RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "password": "SecurePass123"
  }')

echo "$RESPONSE" | jq .

# Extract tokens
ACCESS_TOKEN=$(echo "$RESPONSE" | jq -r '.data.access_token')
REFRESH_TOKEN=$(echo "$RESPONSE" | jq -r '.data.refresh_token')
```

### 4. Browse Sports and Markets

```bash
# List sports
curl -s "$BASE_URL/api/v1/sports" | jq .

# List competitions for cricket
curl -s "$BASE_URL/api/v1/competitions?sport=cricket" | jq .

# List events in a competition
curl -s "$BASE_URL/api/v1/events?competition_id=comp_ipl2026" | jq .

# Get markets for an event
curl -s "$BASE_URL/api/v1/events/evt_001/markets" | jq .

# Get odds for a market
curl -s "$BASE_URL/api/v1/markets/mkt_matchodds_001/odds" | jq .
```

### 5. Place a Bet (Authenticated)

```bash
curl -s -X POST "$BASE_URL/api/v1/bet/place" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "market_id": "mkt_matchodds_001",
    "runner_id": "run_mi",
    "side": "back",
    "price": 1.85,
    "stake": 1000
  }' | jq .
```

### 6. Check Wallet Balance

```bash
curl -s "$BASE_URL/api/v1/wallet/balance" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

### 7. View Order Book

```bash
curl -s "$BASE_URL/api/v1/market/mkt_matchodds_001/orderbook" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

### 8. Cancel a Bet

```bash
curl -s -X DELETE "$BASE_URL/api/v1/bet/bet_x7k9m2/cancel?market_id=mkt_matchodds_001&side=back" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

### 9. Refresh Token

```bash
RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/auth/refresh" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\": \"$REFRESH_TOKEN\"}")

echo "$RESPONSE" | jq .

ACCESS_TOKEN=$(echo "$RESPONSE" | jq -r '.data.access_token')
REFRESH_TOKEN=$(echo "$RESPONSE" | jq -r '.data.refresh_token')
```

### 10. Initiate a UPI Deposit

```bash
curl -s -X POST "$BASE_URL/api/v1/payment/deposit/upi" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -d '{
    "amount": 5000
  }' | jq .
```

### 11. Check KYC Status

```bash
curl -s "$BASE_URL/api/v1/kyc/status" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

### 12. Get Notifications

```bash
curl -s "$BASE_URL/api/v1/notifications" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

### 13. WebSocket Connection (using websocat)

```bash
# Install websocat: brew install websocat (macOS) or cargo install websocat
websocat "ws://localhost:8080/ws?token=$ACCESS_TOKEN"

# Then type JSON messages:
# {"type":"ping"}
# {"type":"subscribe","data":{"channels":["market:mkt_matchodds_001"]}}
```

### 14. Admin Operations (requires admin credentials)

```bash
# Login as admin
ADMIN_RESPONSE=$(curl -s -X POST "$BASE_URL/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "admin",
    "password": "AdminPass123"
  }')

ADMIN_TOKEN=$(echo "$ADMIN_RESPONSE" | jq -r '.data.access_token')

# Get admin dashboard
curl -s "$BASE_URL/api/v1/admin/dashboard" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq .

# List all users
curl -s "$BASE_URL/api/v1/admin/users?limit=10" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq .

# Suspend a market
curl -s -X POST "$BASE_URL/api/v1/admin/markets/mkt_matchodds_001/suspend" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"reason": "Suspicious activity detected"}' | jq .

# Settle a market
curl -s -X POST "$BASE_URL/api/v1/admin/markets/mkt_matchodds_001/settle" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -d '{"winning_runner_id": "run_mi"}' | jq .
```

### 15. Logout

```bash
curl -s -X POST "$BASE_URL/api/v1/auth/logout" \
  -H "Content-Type: application/json" \
  -d "{\"refresh_token\": \"$REFRESH_TOKEN\"}" | jq .
```

---

## Changelog

| Date       | Version | Description            |
|------------|---------|------------------------|
| 2026-04-05 | 1.0.0   | Initial API release.   |
