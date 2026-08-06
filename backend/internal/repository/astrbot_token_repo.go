package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const (
	astrBotTokenReplayMaxBytes = 16 * 1024
)

type astrBotTokenRepository struct {
	db *sql.DB
}

func NewAstrBotTokenRepository(db *sql.DB) service.AstrBotTokenRepository {
	return &astrBotTokenRepository{db: db}
}

func (r *astrBotTokenRepository) Create(ctx context.Context, token *service.AstrBotToken, tokenHash string) error {
	if r == nil || r.db == nil || token == nil {
		return errors.New("astrbot token repository is not configured")
	}
	if tokenHash == "" {
		return errors.New("astrbot token hash is required")
	}
	scopes, err := json.Marshal(token.Scopes)
	if err != nil {
		return fmt.Errorf("marshal astrbot scopes: %w", err)
	}
	allowlist, err := json.Marshal(token.ResourceAllowlist)
	if err != nil {
		return fmt.Errorf("marshal astrbot resource allowlist: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, `
				INSERT INTO astrbot_tokens
				    (token_id, token_hash, user_id, name, scopes, installation_id, resource_allowlist, installation_signing_secret_ciphertext, expires_at)
				VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8, $9)
				RETURNING id, created_at, updated_at
			`, token.TokenID, tokenHash, token.UserID, token.Name, string(scopes), token.InstallationID, string(allowlist), token.InstallationSigningSecretCiphertext, token.ExpiresAt).
		Scan(&token.ID, &token.CreatedAt, &token.UpdatedAt); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return service.ErrAstrBotTokenCollision
		}
		return err
	}
	return nil
}

func (r *astrBotTokenRepository) CreateIdempotent(ctx context.Context, token *service.AstrBotToken, tokenHash string, operation service.AstrBotTokenCreateOperation, replay service.AstrBotTokenCreateReplay, audit *service.AuditLog) (*service.AstrBotTokenCreateResult, error) {
	if r == nil || r.db == nil || token == nil {
		return nil, errors.New("astrbot token repository is not configured")
	}
	if tokenHash == "" || operation.Scope == "" || operation.IdempotencyKeyHash == "" || operation.RequestFingerprint == "" {
		return nil, errors.New("astrbot token creation operation is incomplete")
	}
	if operation.ExpiresAt.IsZero() {
		return nil, errors.New("astrbot token creation operation expiry is required")
	}
	if audit == nil {
		return nil, errors.New("astrbot token creation audit is required")
	}
	if replay.EncryptedRawToken == "" || len(replay.EncryptedRawToken) > astrBotTokenReplayMaxBytes || replay.EncryptedInstallationSecret == "" || len(replay.EncryptedInstallationSecret) > astrBotTokenReplayMaxBytes {
		return nil, errors.New("astrbot token replay payload is invalid")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	rollback := func(cause error) (*service.AstrBotTokenCreateResult, error) {
		_ = tx.Rollback()
		return nil, cause
	}

	var recordID int64
	claimErr := tx.QueryRowContext(ctx, `
		INSERT INTO idempotency_records
		    (scope, idempotency_key_hash, request_fingerprint, status, locked_until, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (scope, idempotency_key_hash) DO NOTHING
		RETURNING id
	`, operation.Scope, operation.IdempotencyKeyHash, operation.RequestFingerprint,
		service.IdempotencyStatusProcessing, operation.LockedUntil, operation.ExpiresAt).Scan(&recordID)

	if errors.Is(claimErr, sql.ErrNoRows) {
		var existing service.IdempotencyRecord
		var responseStatus sql.NullInt64
		var responseBody sql.NullString
		var errorReason sql.NullString
		var lockedUntil sql.NullTime
		if err := tx.QueryRowContext(ctx, `
			SELECT id, request_fingerprint, status, response_status, response_body,
			       error_reason, locked_until, expires_at, created_at, updated_at
			FROM idempotency_records
			WHERE scope = $1 AND idempotency_key_hash = $2
			FOR UPDATE
		`, operation.Scope, operation.IdempotencyKeyHash).Scan(
			&existing.ID, &existing.RequestFingerprint, &existing.Status, &responseStatus,
			&responseBody, &errorReason, &lockedUntil, &existing.ExpiresAt,
			&existing.CreatedAt, &existing.UpdatedAt); err != nil {
			return rollback(err)
		}
		if existing.RequestFingerprint != operation.RequestFingerprint {
			return rollback(service.ErrIdempotencyKeyConflict)
		}

		if !existing.ExpiresAt.After(time.Now()) {
			if existing.Status == service.IdempotencyStatusSucceeded {
				return rollback(service.ErrIdempotencyStoreUnavail)
			}
			if existing.Status != service.IdempotencyStatusProcessing {
				return rollback(service.ErrIdempotencyKeyConflict)
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE idempotency_records
				SET status = $2, response_status = NULL, response_body = NULL,
				    error_reason = NULL, locked_until = $3, expires_at = $4, updated_at = NOW()
				WHERE id = $1
			`, existing.ID, service.IdempotencyStatusProcessing, operation.LockedUntil, operation.ExpiresAt); err != nil {
				return rollback(err)
			}
			recordID = existing.ID
		} else {
			switch existing.Status {
			case service.IdempotencyStatusProcessing:
				return rollback(service.ErrIdempotencyInProgress)
			case service.IdempotencyStatusSucceeded:
				if !responseBody.Valid || responseBody.String == "" || len(responseBody.String) > astrBotTokenReplayMaxBytes {
					return rollback(service.ErrIdempotencyStoreUnavail)
				}
				var stored service.AstrBotTokenCreateReplay
				if err := json.Unmarshal([]byte(responseBody.String), &stored); err != nil ||
					stored.EncryptedRawToken == "" || len(stored.EncryptedRawToken) > astrBotTokenReplayMaxBytes ||
					stored.EncryptedInstallationSecret == "" || len(stored.EncryptedInstallationSecret) > astrBotTokenReplayMaxBytes ||
					stored.Token.TokenID == "" {
					return rollback(service.ErrIdempotencyStoreUnavail)
				}
				_ = tx.Rollback()
				return &service.AstrBotTokenCreateResult{
					Token:                       astrBotTokenFromReplay(stored.Token),
					EncryptedRawToken:           stored.EncryptedRawToken,
					EncryptedInstallationSecret: stored.EncryptedInstallationSecret,
					Replayed:                    true,
				}, nil
			default:
				return rollback(service.ErrIdempotencyKeyConflict)
			}
		}
	}
	if claimErr != nil {
		return rollback(claimErr)
	}

	scopes, err := json.Marshal(token.Scopes)
	if err != nil {
		return rollback(fmt.Errorf("marshal astrbot scopes: %w", err))
	}
	allowlist, err := json.Marshal(token.ResourceAllowlist)
	if err != nil {
		return rollback(fmt.Errorf("marshal astrbot resource allowlist: %w", err))
	}
	if err := tx.QueryRowContext(ctx, `
			INSERT INTO astrbot_tokens
			    (token_id, token_hash, user_id, name, scopes, installation_id, resource_allowlist, installation_signing_secret_ciphertext, expires_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8, $9)
			RETURNING id, created_at, updated_at
		`, token.TokenID, tokenHash, token.UserID, token.Name, string(scopes), token.InstallationID, string(allowlist), token.InstallationSigningSecretCiphertext, token.ExpiresAt).
		Scan(&token.ID, &token.CreatedAt, &token.UpdatedAt); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return rollback(service.ErrAstrBotTokenCollision)
		}
		return rollback(err)
	}

	replay.Token = astrBotTokenReplayMetadata(token)
	encodedReplay, err := json.Marshal(replay)
	if err != nil {
		return rollback(fmt.Errorf("marshal astrbot token replay: %w", err))
	}
	if len(encodedReplay) > astrBotTokenReplayMaxBytes {
		return rollback(errors.New("astrbot token replay payload is too large"))
	}
	if err := insertAuditLog(ctx, tx, audit); err != nil {
		return rollback(fmt.Errorf("insert astrbot token audit: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE idempotency_records

		SET status = $2, response_status = $3, response_body = $4,
		    error_reason = NULL, locked_until = NULL, expires_at = $5, updated_at = NOW()
		WHERE id = $1
	`, recordID, service.IdempotencyStatusSucceeded, 201, string(encodedReplay), operation.ExpiresAt); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &service.AstrBotTokenCreateResult{Token: token, EncryptedRawToken: replay.EncryptedRawToken, EncryptedInstallationSecret: replay.EncryptedInstallationSecret}, nil
}

func astrBotTokenReplayMetadata(token *service.AstrBotToken) service.AstrBotTokenReplayMetadata {
	return service.AstrBotTokenReplayMetadata{
		ID: token.ID, TokenID: token.TokenID, UserID: token.UserID, Name: token.Name,
		Scopes: append([]string(nil), token.Scopes...), InstallationID: token.InstallationID,
		ResourceAllowlist: token.ResourceAllowlist, ExpiresAt: token.ExpiresAt, RevokedAt: token.RevokedAt,
		LastUsedAt: token.LastUsedAt, CreatedAt: token.CreatedAt, UpdatedAt: token.UpdatedAt,
	}
}

func astrBotTokenFromReplay(value service.AstrBotTokenReplayMetadata) *service.AstrBotToken {
	return &service.AstrBotToken{
		ID: value.ID, TokenID: value.TokenID, UserID: value.UserID, Name: value.Name,
		Scopes: append([]string(nil), value.Scopes...), InstallationID: value.InstallationID,
		ResourceAllowlist: value.ResourceAllowlist, ExpiresAt: value.ExpiresAt, RevokedAt: value.RevokedAt,
		LastUsedAt: value.LastUsedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func (r *astrBotTokenRepository) ListByUserID(ctx context.Context, userID int64) ([]*service.AstrBotToken, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, token_id, user_id, name, scopes, installation_id, resource_allowlist, expires_at, revoked_at,
		       last_used_at, created_at, updated_at
		FROM astrbot_tokens
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*service.AstrBotToken
	for rows.Next() {
		var token service.AstrBotToken
		var scopes, allowlist []byte
		if err := rows.Scan(&token.ID, &token.TokenID, &token.UserID, &token.Name, &scopes,
			&token.InstallationID, &allowlist, &token.ExpiresAt, &token.RevokedAt,
			&token.LastUsedAt, &token.CreatedAt, &token.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(scopes, &token.Scopes); err != nil {
			return nil, fmt.Errorf("decode astrbot scopes: %w", err)
		}
		if len(allowlist) > 0 && string(allowlist) != "null" {
			if err := json.Unmarshal(allowlist, &token.ResourceAllowlist); err != nil {
				return nil, fmt.Errorf("decode astrbot resource allowlist: %w", err)
			}
		}
		result = append(result, &token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *astrBotTokenRepository) GetActiveByTokenID(ctx context.Context, tokenID string) (*service.AstrBotToken, string, error) {
	var token service.AstrBotToken
	var tokenHash string
	var scopes, allowlist []byte
	var expiresAt, revokedAt, lastUsedAt *time.Time
	var installationID, signingSecretCiphertext sql.NullString
	if err := r.db.QueryRowContext(ctx, `
		SELECT id, token_id, token_hash, user_id, name, scopes, installation_id,
		       resource_allowlist, installation_signing_secret_ciphertext, expires_at,
		       revoked_at, last_used_at, created_at, updated_at
		FROM astrbot_tokens
		WHERE token_id = $1 AND revoked_at IS NULL
	`, tokenID).Scan(&token.ID, &token.TokenID, &tokenHash, &token.UserID, &token.Name, &scopes,
		&installationID, &allowlist, &signingSecretCiphertext, &expiresAt, &revokedAt,
		&lastUsedAt, &token.CreatedAt, &token.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", service.ErrAstrBotTokenInvalid
		}
		return nil, "", err
	}
	if err := json.Unmarshal(scopes, &token.Scopes); err != nil {
		return nil, "", fmt.Errorf("decode astrbot scopes: %w", err)
	}
	if installationID.Valid {
		token.InstallationID = installationID.String
	}
	if len(allowlist) > 0 && string(allowlist) != "null" {
		if err := json.Unmarshal(allowlist, &token.ResourceAllowlist); err != nil {
			return nil, "", fmt.Errorf("decode astrbot resource allowlist: %w", err)
		}
	}
	if signingSecretCiphertext.Valid {
		token.InstallationSigningSecretCiphertext = signingSecretCiphertext.String
	}
	token.ExpiresAt = expiresAt
	token.RevokedAt = revokedAt
	token.LastUsedAt = lastUsedAt
	return &token, tokenHash, nil
}

func (r *astrBotTokenRepository) RevokeByTokenIDAndUserID(ctx context.Context, tokenID string, userID int64, revokedAt time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_tokens
		SET revoked_at = COALESCE(revoked_at, $1), updated_at = NOW()
		WHERE token_id = $2 AND user_id = $3
	`, revokedAt, tokenID, userID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (r *astrBotTokenRepository) TouchLastUsed(ctx context.Context, tokenID string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_tokens
		SET last_used_at = $1, updated_at = NOW()
		WHERE token_id = $2
		  AND revoked_at IS NULL
		  AND (last_used_at IS NULL OR last_used_at < $1 - INTERVAL '1 minute')
	`, now, tokenID)
	return err
}

func (r *astrBotTokenRepository) ConsumeRequestNonce(ctx context.Context, tokenID, installationID, nonceHash string, expiresAt time.Time) (bool, error) {
	if r == nil || r.db == nil || tokenID == "" || installationID == "" || nonceHash == "" || expiresAt.IsZero() {
		return false, errors.New("astrbot request nonce input is invalid")
	}
	if _, err := r.db.ExecContext(ctx, `
		DELETE FROM astrbot_request_nonces
		WHERE token_id = $1 AND installation_id = $2 AND nonce_hash = $3
		  AND expires_at <= NOW()
	`, tokenID, installationID, nonceHash); err != nil {
		return false, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO astrbot_request_nonces (token_id, installation_id, nonce_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token_id, installation_id, nonce_hash) DO NOTHING
		RETURNING id
	`, tokenID, installationID, nonceHash, expiresAt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return id > 0, nil
}
