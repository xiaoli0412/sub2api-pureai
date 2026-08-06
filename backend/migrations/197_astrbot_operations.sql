-- AstrBot mutations are persisted separately from generic HTTP idempotency
-- records so an unexecuted confirmation can never be mistaken for a replay.
CREATE TABLE IF NOT EXISTS astrbot_operations (
    id BIGSERIAL PRIMARY KEY,
    operation_id VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_id VARCHAR(64) NOT NULL REFERENCES astrbot_tokens(token_id) ON DELETE CASCADE,
    installation_id VARCHAR(128) NOT NULL,
    platform_user_hash CHAR(64),
    session_hash CHAR(64),
    conversation_hash CHAR(64),
    operation_type VARCHAR(64) NOT NULL,
    method VARCHAR(16) NOT NULL,
    route VARCHAR(256) NOT NULL,
    target_id BIGINT,
    payload_json TEXT NOT NULL,
    diff_json TEXT NOT NULL DEFAULT '{}',
    canonical_payload_hash CHAR(64) NOT NULL,
    idempotency_key_hash CHAR(64) NOT NULL,
    state VARCHAR(32) NOT NULL,
    confirmation_count SMALLINT NOT NULL DEFAULT 0,
    first_confirmation_key_hash CHAR(64),
    first_confirmer_user_id BIGINT,
    first_confirmed_at TIMESTAMPTZ,
    second_confirmation_key_hash CHAR(64),
    second_confirmer_user_id BIGINT,
    second_confirmed_at TIMESTAMPTZ,
    challenge_hash CHAR(64),
    challenge_expires_at TIMESTAMPTZ,
    locked_until TIMESTAMPTZ,
    response_status INTEGER,
    response_body TEXT,
    error_reason VARCHAR(128),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT astrbot_operations_state_check CHECK (state IN (
        'prepared', 'confirmed_once', 'confirmed_twice', 'executing',
        'succeeded', 'failed_retryable', 'cancelled', 'expired'
    )),
    CONSTRAINT astrbot_operations_confirmation_count_check CHECK (confirmation_count BETWEEN 0 AND 2)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_astrbot_operations_idempotency
    ON astrbot_operations(token_id, operation_type, (COALESCE(target_id, 0)), idempotency_key_hash);
CREATE INDEX IF NOT EXISTS idx_astrbot_operations_binding
    ON astrbot_operations(user_id, token_id, operation_id);
CREATE INDEX IF NOT EXISTS idx_astrbot_operations_state_lock
    ON astrbot_operations(state, locked_until);
CREATE INDEX IF NOT EXISTS idx_astrbot_operations_expiry
    ON astrbot_operations(expires_at);
