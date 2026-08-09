# AstrBot Security Model

## Security boundary

The AstrBot integration is a dedicated control-plane credential and is not a compatibility alias for user JWTs, administrator API keys, gateway API keys, or platform credentials. A bot token identifies one installation and one administrator owner. The owner must remain active and retain administrator privileges.

The API is a privileged operational interface. It is designed for a trusted AstrBot installation, not for direct exposure to untrusted chat participants. The plugin must authenticate the platform event, enforce its local sender/session/conversation allowlist, and forward only trusted binding metadata.

## Credential separation

There are three independent secret classes:

1. The raw bearer `astrbot_<token_id>_<secret>` authenticates the token.
2. The installation signing secret authenticates each request and binds the installation to an event context.
3. Platform, provider, account, proxy, OAuth, payment, and OpenAI-compatible API credentials remain outside ordinary AstrBot responses.

The bearer and installation signing secret are generated with cryptographically secure random bytes. Only hashes or encrypted ciphertext are persisted. Plaintext bearer material and signing secrets are returned only once during creation. Ciphertext is not returned by list, replay, audit, or ordinary operation DTOs.

Raw bearer material, Authorization values, signing secrets, account credentials, proxy secrets, cookies, and provider keys must not enter logs, query strings, ordinary DTOs, audit payloads, idempotency scopes or keys, response caches, or rate-limit keys.

## Request authentication and replay

Bearer authentication is followed by HMAC request authentication. The HMAC covers the method, request URI, timestamp, nonce, platform user, session ID, conversation ID, and exact body. Binding values reject control characters and are not accepted as an unsigned substitute for a trusted AstrBot event.

Requests are valid only in a short timestamp window. The nonce is hashed before storage and atomically consumed under the token and installation identity. A duplicate nonce, stale timestamp, future timestamp, invalid signature, missing signing key, or changed binding fails closed. HMAC comparison uses constant-time comparison.

A deployment must ensure that event identity values are obtained from AstrBot's trusted event object or from an authenticated adapter envelope. AI-generated text must never choose an installation, sender, session, conversation, resource, or credential.

## Proactive audit delivery

Model-audit reads preserve three-state mismatch semantics: `NULL` for no observed response model, `false` for an observed match, and `true` for an observed mismatch. Alert eligibility is deterministic and evaluated before any optional AI summary. Targets, schedules, rules, cooldowns, and write permissions are configuration or explicit-command inputs; AI, logs, upstream data, and natural-language suggestions cannot alter them.

The plugin persists delivery state and audit schedule slots in SQLite, claims rows with owner leases, and retries only bounded retryable failures. External platform delivery is at-least-once, not exactly-once: a process failure after a successful send can produce a duplicate on recovery. Stable delivery keys, lease checks, cooldowns, and recovery handling reduce duplicates without claiming an impossible guarantee.


AI is read/analyze/propose-only. It cannot call a mutation endpoint, manufacture a confirmation, select a target, or turn a recommendation into an execution. A write begins only after an explicit user command accepted by the plugin.

The server requires:

```text
prepared
  -> confirmed_once
  -> confirmed_twice
  -> executing
  -> succeeded / failed_retryable / cancelled / expired
```

The operation binds the user, token, installation, trusted platform identity hashes, operation type, target, normalized payload hash, and idempotency-key hash. The first confirmation produces a short-lived challenge; only its hash is stored. The second confirmation atomically consumes the challenge. Final execution claims a short lease and every mutation still requires `Idempotency-Key`.

Old mutation routes are compatibility paths only. They require an operation ID for a fully confirmed operation and cannot bypass the state machine. Successful operations replay the persisted result. Retryable failures remain explicit and do not silently repeat an unconfirmed request.

## Resource authorization

A token may have an empty allowlist for backwards-compatible unrestricted administrator operation only when the deployment explicitly chooses that posture, or a non-empty allowlist with explicit account, channel, group, user, and log-source IDs. A non-empty allowlist is restrictive. Missing categories do not mean unrestricted access.

The handler checks authorization before preparing an operation, again before execution, and on compatibility mutation routes. Account and channel list endpoints load only explicitly allowed IDs before applying filters and pagination. Global cost, profit, consumption, and model-price aggregates require an unrestricted token until resource-aware aggregate queries exist.

Resources outside the allowlist and nonexistent resources use uniform `404` behavior. This prevents account, channel, group, and model enumeration through status, error, count, and timing differences.

## Data exposure and redaction

Operational responses use explicit DTO allowlists. Account status may contain operational fields such as platform, type, status, scheduling, concurrency, priority, groups, and timestamps, but not credentials, proxy configuration, arbitrary extra JSON, cookies, upstream error bodies, or session material.

Future log APIs must apply field-level redaction before pagination, export, or AI analysis. Prompt and audit data are untrusted data, not instructions. Full prompts, credentials, Authorization headers, cookies, token material, and arbitrary JSON are excluded unless a separately reviewed scope and redaction policy permits a bounded metadata view.

Audit records should capture who, what, where, when, resource identity, operation state, request ID, and outcome. They must not capture secret values. Critical audit failures must fail closed for sensitive mutations rather than silently dropping the event.

## Prompt injection and AI controls

Logs, error messages, model names, usernames, and external platform messages may contain attacker-controlled text. The plugin must label them as data, use a fixed analysis schema, cap record and prompt sizes, and ignore instructions embedded in the data. Analysis results are recommendations with provenance, not commands.

The AI client must use configurable OpenAI-compatible endpoint, provider, model, timeout, and TLS policy. API keys are configuration secrets and never included in prompts, logs, reports, or backend requests. AI failure must fall back to deterministic counts and redacted summaries.

## External delivery and SSRF

Report delivery is an outward-facing mutation and must use explicit targets, allowlists, idempotency, retries, and audit records. Future webhook support must validate scheme, DNS results, private/link-local/reserved IPs, redirects, response size, timeout, and egress policy. Platform credentials remain in the plugin or platform secret store; they are not copied into SUB2API operation payloads.

## Tenant and settlement boundary

The current token model is administrator-scoped. It must not be handed to third-party settlement customers. Before customer delivery, introduce explicit tenant, membership, role, installation, and resource ownership links. Usage, audit, report, and settlement records need immutable owner/tenant snapshots.

Settlement messages require immutable usage snapshots, pricing and currency versions, deterministic rounding, statement hashes, idempotent delivery, acknowledgement, reconciliation, and audit. Preview is read-only; finalize/send requires the same explicit command and double confirmation as other outward-facing mutations.

## Operational guarantees

Security-sensitive behavior must be tested for:

- exact independent read/write scopes
- bearer and installation-secret separation
- raw-secret exclusion from DTOs, logs, audit, idempotency, and rate limits
- valid, stale, future, invalid, and replayed signatures
- installation and trusted binding mismatch
- allowlist denial with uniform 404
- concurrent confirmation and execution claims
- idempotent success replay and retryable failure
- cancellation and expiration
- rollback when token, idempotency, or audit persistence fails

Secrets must never be committed to the repository. Test fixtures must use placeholders only.
