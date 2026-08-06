-- Resource metadata is kept separate from the bearer secret. An empty
-- allowlist preserves the existing administrator-token behavior until a
-- caller explicitly narrows a token to selected resources.
ALTER TABLE astrbot_tokens
    ADD COLUMN IF NOT EXISTS installation_id VARCHAR(128),
    ADD COLUMN IF NOT EXISTS resource_allowlist JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_astrbot_tokens_installation
    ON astrbot_tokens(installation_id)
    WHERE revoked_at IS NULL AND installation_id IS NOT NULL;
