# AstrBot Integration

This document describes the dedicated AstrBot control-plane API exposed by SUB2API.

## Base URL and authentication

The API is mounted below `/api/v1/bot`. Use the deployment HTTPS origin as the base URL:

```text
https://sub2api.example.com/api/v1/bot
```

Every request uses a dedicated bearer credential:

```http
Authorization: Bearer astrbot_<token_id>_<secret>
```

The bearer is separate from user JWTs, administrator API keys, gateway API keys, and session credentials. Query-string tokens, `x-api-key`, and `x-goog-api-key` are not accepted. The token owner must remain an active administrator.

SUB2API stores only a SHA-256 hash of the complete bearer. The raw bearer must never be placed in a URL, log, audit payload, ordinary DTO, idempotency scope or key, response cache, or rate-limit key. Rate limits use only `astrbot:token:<token_id>`.

## Token lifecycle

Token lifecycle endpoints use normal administrator authentication and are separate from the bot API:

```text
GET  /api/v1/admin/astrbot-tokens
POST /api/v1/admin/astrbot-tokens
POST /api/v1/admin/astrbot-tokens/:token_id/revoke
```

Create a token with an installation identity and an optional resource allowlist:

```http
POST /api/v1/admin/astrbot-tokens
Content-Type: application/json
Idempotency-Key: create-installation-2026-001

{
  "name": "astrbot-production",
  "scopes": ["bot:read", "bot:write"],
  "installation_id": "prod-installation-01",
  "resource_allowlist": {
    "account_ids": [12, 18],
    "channel_ids": [3],
    "group_ids": [],
    "user_ids": [],
    "log_sources": ["audit", "ops"]
  },
  "expires_at": "2027-01-01T00:00:00Z"
}
```

The creation response returns `raw_token` and `installation_secret` exactly once. Store both in the AstrBot secret manager immediately. The installation signing secret is independent from the bearer and is used to sign every bot request. Neither secret is returned by list, revoke, replay, or ordinary operation responses.

The creation operation is idempotent. Repeating the same request with the same key returns the persisted encrypted replay material and `X-Idempotency-Replayed: true`; it does not create another token. A changed payload with the same key returns `409`.

## Scopes and resources

Scopes are exact and independent:

- `bot:read` permits read endpoints.
- `bot:write` permits confirmed write endpoints.

`bot:read` does not imply `bot:write`, and `bot:write` does not imply `bot:read`.

An empty resource allowlist preserves unrestricted administrator behavior only when the deployment deliberately permits it. A non-empty allowlist is restrictive: a token may access only explicitly listed accounts, channels, groups, users, or log sources. An omitted resource category does not expand another category. Unauthorized or nonexistent resources use the same `404` response to avoid enumeration. Global aggregates such as costs, profit, consumption, and model prices require an unrestricted token unless a resource-aware endpoint is added.

## Signed requests and replay protection

Bearer authentication identifies the token. The installation secret authenticates the request instance. Bot requests must include:

```http
X-AstrBot-Timestamp: 2026-08-06T12:00:00Z
X-AstrBot-Nonce: unique-random-request-value
X-AstrBot-Signature: <lowercase-hex-HMAC-SHA256>
X-AstrBot-Platform-User: trusted-platform-sender-id
X-AstrBot-Session-ID: trusted-session-id
X-AstrBot-Conversation-ID: trusted-conversation-id
```

The signature is HMAC-SHA256 with the installation secret over this exact UTF-8 string:

```text
UPPERCASE_METHOD
REQUEST_URI
TIMESTAMP
NONCE
PLATFORM_USER
SESSION_ID
CONVERSATION_ID
BODY
```

Each value is one line. Binding values are trimmed and reject CR, LF, and NUL. The timestamp may be no more than five minutes old or thirty seconds in the future. Nonces are hashed before durable storage and consumed atomically under `(token_id, installation_id, nonce_hash)`. Reusing a nonce, using an expired timestamp, changing any binding value, or changing the body fails authentication.

The platform user, session, and conversation values must come from the trusted AstrBot event context. They must not be inferred by AI or selected from an arbitrary user-supplied identity. In deployments where the transport supplies an event envelope, the envelope values must be included in the signed body or verified by the installation adapter before forwarding.

## Read API

```text
GET /api/v1/bot/costs
GET /api/v1/bot/profit
GET /api/v1/bot/consumption
GET /api/v1/bot/channels/status
GET /api/v1/bot/accounts/status
GET /api/v1/bot/models/prices
```

Cost, profit, and consumption default to the previous 24 hours in UTC. Supply RFC3339 `start` and `end` to select another range; the maximum range is 366 days. Account responses exclude credentials, proxy secrets, arbitrary `extra` data, and raw upstream errors. Model prices expose only configured public price fields.

## Model audit and proactive delivery

The read-only `GET /api/v1/bot/model-audit` endpoint reports `total_requests`, `observed_requests`, `mismatch_count`, `match_count`, `mismatch_rate`, `no_response_model_count`, bounded aggregates, and bounded samples. `upstream_model_mismatch` is tri-state: `null` means no upstream response model was observed, `false` means an observed model matched, and `true` means it differed. The `mismatch=true|false` filter matches only the corresponding Boolean value; `NULL` is not treated as false. The compatibility `model-mismatches` command is the same read with `mismatch=true`.

The AstrBot plugin may poll with bounded `audit_schedules` and deterministic `alert_rules`. Each schedule has explicit targets, a bounded interval, a fixed `today`, `last24h`, or RFC3339 window, and bounded query filters. The plugin persists schedule slots and delivery rows in SQLite, uses owner leases and bounded retries, and never lets AI or returned operational data choose targets or execute writes. Delivery is at-least-once rather than exactly-once: a successful external send followed by a process failure can be delivered again.


Every write requires `bot:write` and a unique `Idempotency-Key`. The explicit command flow is:

```text
POST /operations/prepare
POST /operations/:id/confirm
POST /operations/:id/confirm-again
POST /operations/:id/execute
POST /operations/:id/cancel
```

The state machine is:

```text
prepared -> confirmed_once -> confirmed_twice -> executing
                                      -> succeeded
                                      -> failed_retryable
                                      -> cancelled / expired
```

`prepare` must be caused by an explicit user command. It records the normalized operation, target, payload hash, resource binding, installation, platform user, session, and conversation. The first confirmation returns a short-lived one-time challenge. The second confirmation must be made by the same bound identity and consumes the challenge atomically. Only then may `execute` claim a short lease and perform the mutation. AI may summarize or propose a change, but it cannot prepare, confirm, or execute a write.

The currently supported operation types are:

```text
channel.toggle
account.toggle
account.rate_multiplier
channel.pricing
cache.refresh
```

Legacy toggle, multiplier, pricing, and cache routes remain compatibility routes only. They require an operation ID referring to a fully confirmed operation and still require `Idempotency-Key`; they do not bypass the state machine.

## Auditing and errors

Sensitive reads and all writes are audited. Audit entries contain actor, route, request ID, resource, operation state, and outcome metadata, but not Authorization, raw bearer, installation secret, account credentials, proxy credentials, cookies, or full unredacted prompts.

Expected errors:

- `400`: malformed input, invalid time range, invalid resource allowlist, or missing idempotency key.
- `401`: missing/invalid bearer, invalid signature, expired/revoked token, inactive owner, stale timestamp, or replayed request.
- `403`: required exact scope is missing.
- `404`: resource does not exist or is outside the token allowlist.
- `409`: idempotency conflict, stale operation, invalid state, or operation already in progress.
- `429`: per-token rate limit exceeded.
- `503`: required idempotency, audit, or operation storage is unavailable.

Never commit a raw token, installation secret, private key, payment credential, or other secret to the repository.
