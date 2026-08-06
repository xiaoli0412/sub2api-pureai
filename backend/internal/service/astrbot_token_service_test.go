package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type astrBotTokenServiceRepoStub struct {
	createCalls       int
	hash              string
	token             *AstrBotToken
	createErr         error
	alwaysCreateError bool
	nonceResult       bool
	nonceResultSet    bool
	nonceErr          error
}

type astrBotTestEncryptor struct {
	decryptErr error
}

func (e astrBotTestEncryptor) Encrypt(plaintext string) (string, error) {
	return "cipher:" + plaintext, nil
}

func (e astrBotTestEncryptor) Decrypt(ciphertext string) (string, error) {
	if e.decryptErr != nil {
		return "", e.decryptErr
	}
	return strings.TrimPrefix(ciphertext, "cipher:"), nil
}

func (r *astrBotTokenServiceRepoStub) Create(_ context.Context, token *AstrBotToken, tokenHash string) error {
	r.createCalls++
	if r.createErr != nil {
		err := r.createErr
		if !r.alwaysCreateError && r.createCalls == 1 {
			r.createErr = nil
		}
		return err
	}
	r.token = token
	r.hash = tokenHash
	return nil
}

func (r *astrBotTokenServiceRepoStub) CreateIdempotent(context.Context, *AstrBotToken, string, AstrBotTokenCreateOperation, AstrBotTokenCreateReplay, *AuditLog) (*AstrBotTokenCreateResult, error) {
	return nil, ErrAstrBotTokenIdempotency
}

func (r *astrBotTokenServiceRepoStub) ListByUserID(context.Context, int64) ([]*AstrBotToken, error) {
	return nil, nil
}

func (r *astrBotTokenServiceRepoStub) GetActiveByTokenID(context.Context, string) (*AstrBotToken, string, error) {
	return nil, "", ErrAstrBotTokenInvalid
}

func (r *astrBotTokenServiceRepoStub) RevokeByTokenIDAndUserID(context.Context, string, int64, time.Time) (bool, error) {
	return false, nil
}

func (r *astrBotTokenServiceRepoStub) TouchLastUsed(context.Context, string, time.Time) error {
	return nil
}

func (r *astrBotTokenServiceRepoStub) ConsumeRequestNonce(context.Context, string, string, string, time.Time) (bool, error) {
	if r.nonceErr != nil {
		return false, r.nonceErr
	}
	if r.nonceResultSet {
		return r.nonceResult, nil
	}
	return true, nil
}

func TestParseAstrBotBearer_AllowsURLSafeUnderscores(t *testing.T) {
	value := astrBotBearerPrefix + "abc_def-12345678" + "_" + strings.Repeat("a_", 21) + "a"
	tokenID, ok := parseAstrBotBearer(value)
	require.True(t, ok)
	require.Equal(t, "abc_def-12345678", tokenID)
}

func TestAstrBotTokenServiceCreate_RetriesTokenCollision(t *testing.T) {
	repo := &astrBotTokenServiceRepoStub{createErr: ErrAstrBotTokenCollision}
	svc := NewAstrBotTokenService(repo, nil)

	created, err := svc.Create(context.Background(), CreateAstrBotTokenInput{
		UserID: 1,
		Name:   " production ",
		Scopes: []string{AstrBotScopeRead, AstrBotScopeRead},
	})

	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, 2, repo.createCalls)
	require.Equal(t, "production", repo.token.Name)
	require.NotEqual(t, created.RawToken, repo.hash)
	require.Equal(t, hashAstrBotBearer(created.RawToken), repo.hash)
	require.Contains(t, created.RawToken, astrBotBearerPrefix+created.Token.TokenID+"_")
	require.Equal(t, []string{AstrBotScopeRead}, created.Token.Scopes)
}

func TestAstrBotTokenServiceVerifyRequestSignatureAcceptsCanonicalBindings(t *testing.T) {
	repo := &astrBotTokenServiceRepoStub{}
	svc := NewAstrBotTokenServiceWithEncryptor(repo, nil, astrBotTestEncryptor{}, true)
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	token := &AstrBotToken{
		TokenID: "token-id", InstallationID: "installation-id",
		InstallationSigningSecretCiphertext: "cipher:installation-secret",
	}
	timestamp := now.Format(time.RFC3339)
	nonce := "nonce-1"
	method, path := "post", "/api/v1/bot/operations/prepare?x=1"
	platformUser, sessionID, conversationID, body := "platform-user", "session-1", "conversation-1", `{"operation_type":"cache.refresh"}`
	canonical := canonicalAstrBotRequest(method, path, timestamp, nonce, platformUser, sessionID, conversationID, body)
	mac := hmac.New(sha256.New, []byte("installation-secret"))
	_, err := mac.Write([]byte(canonical))
	require.NoError(t, err)
	signature := hex.EncodeToString(mac.Sum(nil))

	err = svc.VerifyRequestSignature(context.Background(), token, timestamp, nonce, method, path, platformUser, sessionID, conversationID, body, signature, now)

	require.NoError(t, err)
}

func TestAstrBotTokenServiceVerifyRequestSignatureRejectsBindingTampering(t *testing.T) {
	repo := &astrBotTokenServiceRepoStub{}
	svc := NewAstrBotTokenServiceWithEncryptor(repo, nil, astrBotTestEncryptor{}, true)
	now := time.Now().UTC().Truncate(time.Second)
	token := &AstrBotToken{TokenID: "token-id", InstallationID: "installation-id", InstallationSigningSecretCiphertext: "cipher:installation-secret"}
	timestamp := now.Format(time.RFC3339)
	canonical := canonicalAstrBotRequest("POST", "/path", timestamp, "nonce", "user-a", "session", "conversation", "{}")
	mac := hmac.New(sha256.New, []byte("installation-secret"))
	_, err := mac.Write([]byte(canonical))
	require.NoError(t, err)
	signature := hex.EncodeToString(mac.Sum(nil))

	err = svc.VerifyRequestSignature(context.Background(), token, timestamp, "nonce", "POST", "/path", "user-b", "session", "conversation", "{}", signature, now)

	require.ErrorIs(t, err, ErrAstrBotRequestSignature)
}

func TestAstrBotTokenServiceVerifyRequestSignatureRejectsInvalidTimeAndReplay(t *testing.T) {
	tests := []struct {
		name      string
		now       time.Time
		timestamp time.Time
		replay    bool
		want      error
	}{
		{name: "stale", now: time.Now().UTC(), timestamp: time.Now().UTC().Add(-6 * time.Minute), want: ErrAstrBotRequestSignature},
		{name: "future", now: time.Now().UTC(), timestamp: time.Now().UTC().Add(31 * time.Second), want: ErrAstrBotRequestSignature},
		{name: "replayed", now: time.Now().UTC(), timestamp: time.Now().UTC(), replay: true, want: ErrAstrBotRequestReplay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &astrBotTokenServiceRepoStub{nonceResult: !tt.replay, nonceResultSet: true}
			svc := NewAstrBotTokenServiceWithEncryptor(repo, nil, astrBotTestEncryptor{}, true)
			token := &AstrBotToken{TokenID: "token-id", InstallationID: "installation-id", InstallationSigningSecretCiphertext: "cipher:installation-secret"}
			timestamp := tt.timestamp.Format(time.RFC3339)
			canonical := canonicalAstrBotRequest("POST", "/path", timestamp, "nonce", "user", "session", "conversation", "{}")
			mac := hmac.New(sha256.New, []byte("installation-secret"))
			_, err := mac.Write([]byte(canonical))
			require.NoError(t, err)
			signature := hex.EncodeToString(mac.Sum(nil))
			err = svc.VerifyRequestSignature(context.Background(), token, timestamp, "nonce", "POST", "/path", "user", "session", "conversation", "{}", signature, tt.now)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func TestAstrBotTokenServiceVerifyRequestSignatureFailsClosedWithoutEncryptor(t *testing.T) {
	svc := NewAstrBotTokenService(&astrBotTokenServiceRepoStub{}, nil)
	err := svc.VerifyRequestSignature(context.Background(), &AstrBotToken{TokenID: "token-id", InstallationID: "installation-id", InstallationSigningSecretCiphertext: "cipher:secret"}, time.Now().UTC().Format(time.RFC3339), "nonce", "POST", "/path", "user", "session", "conversation", "{}", "signature", time.Now().UTC())
	require.ErrorIs(t, err, ErrAstrBotRequestSignature)
}
