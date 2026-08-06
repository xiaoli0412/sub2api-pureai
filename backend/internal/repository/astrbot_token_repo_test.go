package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type capturedSQLValue struct {
	value driver.Value
}

func (m *capturedSQLValue) Match(value driver.Value) bool {
	m.value = value
	return true
}

func sqlValueString(value driver.Value) string {
	switch value := value.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func astrBotTokenCreateFixtures() (*service.AstrBotToken, service.AstrBotTokenCreateOperation, service.AstrBotTokenCreateReplay, *service.AuditLog, string) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	rawToken := "astrbot_token-id_secret-value"
	actorUserID := int64(7)
		return &service.AstrBotToken{
			TokenID:         "token-id",
			UserID:          7,
			Name:            "production",
			Scopes:          []string{service.AstrBotScopeRead},
			InstallationID:  "installation-1",
			InstallationSigningSecretCiphertext: "encrypted-installation-secret",
		}, service.AstrBotTokenCreateOperation{
			Scope:              "astrbot:token:create:7",
			IdempotencyKeyHash: "idempotency-hash",
			RequestFingerprint: "request-fingerprint",
			LockedUntil:        now.Add(time.Minute),
			ExpiresAt:          now.Add(15 * time.Minute),
		}, service.AstrBotTokenCreateReplay{
			EncryptedRawToken:           "encrypted-token-ciphertext",
			EncryptedInstallationSecret: "encrypted-installation-secret",
		}, &service.AuditLog{
			ActorUserID: &actorUserID,
			ActorEmail:  "admin@example.test",
			ActorRole:   "admin",
			AuthMethod:  service.AuditAuthMethodAstrBotToken,
			Action:      service.AuditActionAstrBotTokenCreate,
			Method:      "POST",
			Path:        "/api/v1/admin/astrbot/tokens",
			RequestID:   "request-id",
			StatusCode:  201,
			Extra:       map[string]any{"token_name": "production"},
		}, rawToken
}

func expectAstrBotTokenIdempotencyClaim(mock sqlmock.Sqlmock, operation service.AstrBotTokenCreateOperation, recordID int64) {
	expectation := mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO idempotency_records"))
	expectation.WithArgs(operation.Scope, operation.IdempotencyKeyHash, operation.RequestFingerprint,
		service.IdempotencyStatusProcessing, operation.LockedUntil, operation.ExpiresAt)
	expectation.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(recordID))
}

func expectAstrBotTokenInsert(mock sqlmock.Sqlmock, token *service.AstrBotToken, tokenHash string, result error) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	expectation := mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO astrbot_tokens"))
		expectation.WithArgs(token.TokenID, tokenHash, token.UserID, token.Name, `["bot:read"]`, token.InstallationID, `{}`, token.InstallationSigningSecretCiphertext, token.ExpiresAt)
	if result != nil {
		expectation.WillReturnError(result)
		return
	}
	expectation.WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(42, createdAt, createdAt))
}

func expectAuditInsert(mock sqlmock.Sqlmock, result error) {
	expectation := mock.ExpectExec(regexp.QuoteMeta("INSERT INTO audit_logs"))
	expectation.WithArgs(
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
		sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
	)
	if result != nil {
		expectation.WillReturnError(result)
		return
	}
	expectation.WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestAstrBotTokenRepositoryCreateIdempotentCommitsTokenReplayAndAuditAtomically(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	token, operation, replay, audit, rawToken := astrBotTokenCreateFixtures()
	mock.ExpectBegin()
	expectAstrBotTokenIdempotencyClaim(mock, operation, 99)
	expectAstrBotTokenInsert(mock, token, "token-hash", nil)

	var responseBody capturedSQLValue
	expectAuditInsert(mock, nil)
	update := mock.ExpectExec(regexp.QuoteMeta("UPDATE idempotency_records"))
	update.WithArgs(99, service.IdempotencyStatusSucceeded, 201, &responseBody, operation.ExpiresAt)
	update.WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := (&astrBotTokenRepository{db: db}).CreateIdempotent(
		context.Background(), token, "token-hash", operation, replay, audit,
	)

	require.NoError(t, err)
	require.False(t, result.Replayed)
	require.Equal(t, replay.EncryptedRawToken, result.EncryptedRawToken)
	storedResponse := sqlValueString(responseBody.value)
	require.NotContains(t, storedResponse, rawToken)
	require.NotContains(t, storedResponse, "Authorization")
	require.Contains(t, storedResponse, replay.EncryptedRawToken)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryCreateIdempotentRollsBackWhenAuditInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	token, operation, replay, audit, _ := astrBotTokenCreateFixtures()
	mock.ExpectBegin()
	expectAstrBotTokenIdempotencyClaim(mock, operation, 99)
	expectAstrBotTokenInsert(mock, token, "token-hash", nil)
	expectAuditInsert(mock, errors.New("audit database unavailable"))
	mock.ExpectRollback()

	_, err = (&astrBotTokenRepository{db: db}).CreateIdempotent(
		context.Background(), token, "token-hash", operation, replay, audit,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "insert astrbot token audit")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryCreateIdempotentRollsBackWhenTokenInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	token, operation, replay, audit, _ := astrBotTokenCreateFixtures()
	mock.ExpectBegin()
	expectAstrBotTokenIdempotencyClaim(mock, operation, 99)
	expectAstrBotTokenInsert(mock, token, "token-hash", errors.New("token insert failed"))
	mock.ExpectRollback()

	_, err = (&astrBotTokenRepository{db: db}).CreateIdempotent(
		context.Background(), token, "token-hash", operation, replay, audit,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "token insert failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryCreateIdempotentReplaysEncryptedPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	_, operation, replay, audit, _ := astrBotTokenCreateFixtures()
	replay.Token = service.AstrBotTokenReplayMetadata{TokenID: "token-id", UserID: 7, Name: "production"}
	body, err := json.Marshal(replay)
	require.NoError(t, err)
	expiresAt := time.Now().UTC().Add(time.Minute)
	createdAt := expiresAt.Add(-time.Minute)

	mock.ExpectBegin()
	claim := mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO idempotency_records"))
	claim.WithArgs(operation.Scope, operation.IdempotencyKeyHash, operation.RequestFingerprint,
		service.IdempotencyStatusProcessing, operation.LockedUntil, operation.ExpiresAt)
	claim.WillReturnError(sql.ErrNoRows)
	selectExisting := mock.ExpectQuery(regexp.QuoteMeta("SELECT id, request_fingerprint, status"))
	selectExisting.WithArgs(operation.Scope, operation.IdempotencyKeyHash)
	selectExisting.WillReturnRows(sqlmock.NewRows([]string{
		"id", "request_fingerprint", "status", "response_status", "response_body",
		"error_reason", "locked_until", "expires_at", "created_at", "updated_at",
	}).AddRow(99, operation.RequestFingerprint, service.IdempotencyStatusSucceeded, 201,
		string(body), nil, nil, expiresAt, createdAt, expiresAt))
	mock.ExpectRollback()

	result, err := (&astrBotTokenRepository{db: db}).CreateIdempotent(
		context.Background(), &service.AstrBotToken{}, "token-hash", operation, replay, audit,
	)

	require.NoError(t, err)
	require.True(t, result.Replayed)
	require.Equal(t, replay.EncryptedRawToken, result.EncryptedRawToken)
	require.Equal(t, replay.Token.TokenID, result.Token.TokenID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryConsumeRequestNonceFirstUse(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash", expiresAt).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

	consumed, err := (&astrBotTokenRepository{db: db}).ConsumeRequestNonce(context.Background(), "token-id", "installation-id", "nonce-hash", expiresAt)

	require.NoError(t, err)
	require.True(t, consumed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryConsumeRequestNonceRejectsDuplicate(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash", expiresAt).WillReturnError(sql.ErrNoRows)

	consumed, err := (&astrBotTokenRepository{db: db}).ConsumeRequestNonce(context.Background(), "token-id", "installation-id", "nonce-hash", expiresAt)

	require.NoError(t, err)
	require.False(t, consumed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryConsumeRequestNonceReclaimsExpiredNonce(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash", expiresAt).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))

	consumed, err := (&astrBotTokenRepository{db: db}).ConsumeRequestNonce(context.Background(), "token-id", "installation-id", "nonce-hash", expiresAt)

	require.NoError(t, err)
	require.True(t, consumed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstrBotTokenRepositoryConsumeRequestNonceFailsClosedOnDatabaseErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM astrbot_request_nonces")).WithArgs("token-id", "installation-id", "nonce-hash").WillReturnError(errors.New("nonce cleanup failed"))

	consumed, err := (&astrBotTokenRepository{db: db}).ConsumeRequestNonce(context.Background(), "token-id", "installation-id", "nonce-hash", expiresAt)

	require.Error(t, err)
	require.False(t, consumed)
	require.NoError(t, mock.ExpectationsWereMet())
}
