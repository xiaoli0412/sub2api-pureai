-- AstrBot uses a dedicated bearer-token namespace. Only the SHA-256 hash of the
-- complete bearer is persisted; the raw token is returned once at creation time.
CREATE TABLE IF NOT EXISTS astrbot_tokens (
    id BIGSERIAL PRIMARY KEY,
    token_id VARCHAR(64) NOT NULL UNIQUE,
    token_hash CHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(128) NOT NULL,
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_astrbot_tokens_user_created
    ON astrbot_tokens(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_astrbot_tokens_active
    ON astrbot_tokens(token_id)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_astrbot_tokens_expires
    ON astrbot_tokens(expires_at)
    WHERE revoked_at IS NULL AND expires_at IS NOT NULL;
