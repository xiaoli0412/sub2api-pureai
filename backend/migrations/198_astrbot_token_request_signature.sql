-- Persist installation signing ciphertext and atomically consumed request nonces.
-- The nonce table stores only the hash supplied by the service, never the
-- caller's plaintext nonce.
ALTER TABLE astrbot_tokens
    ADD COLUMN IF NOT EXISTS installation_signing_secret_ciphertext TEXT;

CREATE TABLE IF NOT EXISTS astrbot_request_nonces (
    id BIGSERIAL PRIMARY KEY,
    token_id VARCHAR(64) NOT NULL,
    installation_id VARCHAR(128) NOT NULL,
    nonce_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_astrbot_request_nonces_identity
        UNIQUE (token_id, installation_id, nonce_hash)
);

CREATE INDEX IF NOT EXISTS idx_astrbot_request_nonces_expires_at
    ON astrbot_request_nonces(expires_at);
