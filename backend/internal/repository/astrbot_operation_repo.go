package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type astrBotOperationRepository struct {
	db *sql.DB
}

func NewAstrBotOperationRepository(db *sql.DB) service.AstrBotOperationRepository {
	return &astrBotOperationRepository{db: db}
}

func (r *astrBotOperationRepository) Create(ctx context.Context, operation *service.AstrBotOperation) error {
	if r == nil || r.db == nil || operation == nil {
		return errors.New("astrbot operation repository is not configured")
	}
	var createdAt, updatedAt time.Time
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO astrbot_operations (
			operation_id, user_id, token_id, installation_id,
			platform_user_hash, session_hash, conversation_hash,
			operation_type, method, route, target_id, payload_json, diff_json,
			canonical_payload_hash, idempotency_key_hash, state, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, created_at, updated_at
	`, operation.OperationID, operation.UserID, operation.TokenID, operation.InstallationID,
		operation.PlatformUserHash, operation.SessionHash, operation.ConversationHash,
		operation.OperationType, operation.Method, operation.Route, operation.TargetID,
		operation.PayloadJSON, operation.DiffJSON, operation.CanonicalPayloadHash,
		operation.IdempotencyKeyHash, operation.State, operation.ExpiresAt,
	).Scan(&operation.ID, &createdAt, &updatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return service.ErrAstrBotOperationConflict
		}
		return fmt.Errorf("create AstrBot operation: %w", err)
	}
	operation.CreatedAt = createdAt
	operation.UpdatedAt = updatedAt
	return nil
}

func (r *astrBotOperationRepository) GetByID(ctx context.Context, operationID, tokenID string, userID int64) (*service.AstrBotOperation, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("astrbot operation repository is not configured")
	}
	return r.get(ctx, `
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	`, operationID, tokenID, userID)
}

func (r *astrBotOperationRepository) GetByIdentity(ctx context.Context, tokenID, operationType string, targetID *int64, idempotencyKeyHash string) (*service.AstrBotOperation, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("astrbot operation repository is not configured")
	}
	return r.get(ctx, `
		WHERE token_id = $1 AND operation_type = $2
		  AND target_id IS NOT DISTINCT FROM $3
		  AND idempotency_key_hash = $4
		ORDER BY id DESC
		LIMIT 1
	`, tokenID, operationType, targetID, idempotencyKeyHash)
}

func (r *astrBotOperationRepository) get(ctx context.Context, suffix string, args ...any) (*service.AstrBotOperation, error) {
	var operation service.AstrBotOperation
	var targetID sql.NullInt64
	var platformUserHash, sessionHash, conversationHash sql.NullString
	var firstKey, secondKey, challengeHash, errorReason sql.NullString
	var firstUser, secondUser sql.NullInt64
	var firstAt, secondAt, challengeExpires, lockedUntil, completedAt sql.NullTime
	var responseStatus sql.NullInt64
	var responseBody sql.NullString
	query := `
		SELECT id, operation_id, user_id, token_id, installation_id,
		       platform_user_hash, session_hash, conversation_hash,
		       operation_type, method, route, target_id, payload_json, diff_json,
		       canonical_payload_hash, idempotency_key_hash, state, confirmation_count,
		       first_confirmation_key_hash, first_confirmer_user_id, first_confirmed_at,
		       second_confirmation_key_hash, second_confirmer_user_id, second_confirmed_at,
		       challenge_hash, challenge_expires_at, locked_until,
		       response_status, response_body, error_reason, expires_at, completed_at,
		       created_at, updated_at
		FROM astrbot_operations ` + suffix
	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&operation.ID, &operation.OperationID, &operation.UserID, &operation.TokenID, &operation.InstallationID,
		&platformUserHash, &sessionHash, &conversationHash,
		&operation.OperationType, &operation.Method, &operation.Route, &targetID,
		&operation.PayloadJSON, &operation.DiffJSON, &operation.CanonicalPayloadHash,
		&operation.IdempotencyKeyHash, &operation.State, &operation.ConfirmationCount,
		&firstKey, &firstUser, &firstAt, &secondKey, &secondUser, &secondAt,
		&challengeHash, &challengeExpires, &lockedUntil, &responseStatus, &responseBody,
		&errorReason, &operation.ExpiresAt, &completedAt, &operation.CreatedAt, &operation.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAstrBotOperationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get AstrBot operation: %w", err)
	}
	if targetID.Valid {
		value := targetID.Int64
		operation.TargetID = &value
	}
	if platformUserHash.Valid {
		operation.PlatformUserHash = platformUserHash.String
	}
	if sessionHash.Valid {
		operation.SessionHash = sessionHash.String
	}
	if conversationHash.Valid {
		operation.ConversationHash = conversationHash.String
	}
	if firstKey.Valid {
		operation.FirstConfirmationKeyHash = firstKey.String
	}
	if secondKey.Valid {
		operation.SecondConfirmationKeyHash = secondKey.String
	}
	if challengeHash.Valid {
		operation.ChallengeHash = challengeHash.String
	}
	if errorReason.Valid {
		operation.ErrorReason = errorReason.String
	}
	if responseBody.Valid {
		operation.ResponseBody = responseBody.String
	}
	if responseStatus.Valid {
		value := int(responseStatus.Int64)
		operation.ResponseStatus = &value
	}
	if firstUser.Valid {
		value := firstUser.Int64
		operation.FirstConfirmerUserID = &value
	}
	if secondUser.Valid {
		value := secondUser.Int64
		operation.SecondConfirmerUserID = &value
	}
	if firstAt.Valid {
		value := firstAt.Time
		operation.FirstConfirmedAt = &value
	}
	if secondAt.Valid {
		value := secondAt.Time
		operation.SecondConfirmedAt = &value
	}
	if challengeExpires.Valid {
		value := challengeExpires.Time
		operation.ChallengeExpiresAt = &value
	}
	if lockedUntil.Valid {
		value := lockedUntil.Time
		operation.LockedUntil = &value
	}
	if completedAt.Valid {
		value := completedAt.Time
		operation.CompletedAt = &value
	}
	return &operation, nil
}

func (r *astrBotOperationRepository) ConfirmFirst(ctx context.Context, operationID, tokenID string, userID int64, installationID, keyHash, challengeHash string, challengeExpiresAt, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $6, confirmation_count = 1,
		    first_confirmation_key_hash = $7, first_confirmer_user_id = $3,
		    first_confirmed_at = $8, challenge_hash = $9,
		    challenge_expires_at = $10, updated_at = NOW()
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	  AND installation_id = $4 AND state = $5 AND expires_at > $8
	`, operationID, tokenID, userID, installationID, service.AstrBotOperationStatePrepared,
		service.AstrBotOperationStateConfirmedOnce, keyHash, now, challengeHash, challengeExpiresAt)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *astrBotOperationRepository) ConfirmSecond(ctx context.Context, operationID, tokenID string, userID int64, installationID, keyHash, challengeHash string, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $6, confirmation_count = 2,
		    second_confirmation_key_hash = $7, second_confirmer_user_id = $3,
		    second_confirmed_at = $8, challenge_hash = NULL,
		    challenge_expires_at = NULL, updated_at = NOW()
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	  AND installation_id = $4 AND state = $5 AND challenge_hash = $9
	  AND challenge_expires_at > $8 AND expires_at > $8
	`, operationID, tokenID, userID, installationID, service.AstrBotOperationStateConfirmedOnce,
		service.AstrBotOperationStateConfirmedTwice, keyHash, now, challengeHash)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *astrBotOperationRepository) ClaimExecution(ctx context.Context, operationID, tokenID string, userID int64, installationID string, now, lockedUntil time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $6, locked_until = $7, error_reason = NULL, updated_at = NOW()
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	  AND installation_id = $4
	  AND ((state = $5) OR (state IN ('failed_retryable', 'executing') AND (locked_until IS NULL OR locked_until <= $8)))
	  AND expires_at > $8
	`, operationID, tokenID, userID, installationID, service.AstrBotOperationStateConfirmedTwice,
		service.AstrBotOperationStateExecuting, lockedUntil, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *astrBotOperationRepository) MarkSucceeded(ctx context.Context, operationID string, responseStatus int, responseBody string, completedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $2, response_status = $3, response_body = $4,
		    locked_until = NULL, completed_at = $5, updated_at = NOW()
		WHERE operation_id = $1 AND state = $6
	`, operationID, service.AstrBotOperationStateSucceeded, responseStatus, responseBody, completedAt, service.AstrBotOperationStateExecuting)
	return err
}

func (r *astrBotOperationRepository) MarkFailedRetryable(ctx context.Context, operationID, errorReason string, lockedUntil time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $2, error_reason = $3, locked_until = $4, updated_at = NOW()
		WHERE operation_id = $1 AND state = $5
	`, operationID, service.AstrBotOperationStateFailedRetryable, errorReason, lockedUntil, service.AstrBotOperationStateExecuting)
	return err
}

func (r *astrBotOperationRepository) Cancel(ctx context.Context, operationID, tokenID string, userID int64, installationID string, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $6, completed_at = $7, updated_at = NOW()
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	  AND installation_id = $4 AND state IN ('prepared', 'confirmed_once', 'confirmed_twice', 'failed_retryable')
	  AND expires_at > $7
	`, operationID, tokenID, userID, installationID, service.AstrBotOperationStateCancelled, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (r *astrBotOperationRepository) Expire(ctx context.Context, operationID, tokenID string, userID int64, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE astrbot_operations
		SET state = $5, completed_at = $4, updated_at = NOW()
		WHERE operation_id = $1 AND token_id = $2 AND user_id = $3
	  AND state NOT IN ('succeeded', 'cancelled', 'expired') AND expires_at <= $4
	`, operationID, tokenID, userID, now, service.AstrBotOperationStateExpired)
	return err
}
