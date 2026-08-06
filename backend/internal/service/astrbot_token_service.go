package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"sort"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrAstrBotTokenInvalid     = infraerrors.Unauthorized("ASTRBOT_TOKEN_INVALID", "invalid AstrBot bearer token")
	ErrAstrBotTokenNotFound    = infraerrors.NotFound("ASTRBOT_TOKEN_NOT_FOUND", "AstrBot token not found")
	ErrAstrBotScopeForbidden   = infraerrors.Forbidden("ASTRBOT_SCOPE_FORBIDDEN", "required AstrBot scope is missing")
	ErrAstrBotTokenName        = infraerrors.BadRequest("ASTRBOT_TOKEN_NAME_INVALID", "AstrBot token name is invalid")
	ErrAstrBotTokenScopes      = infraerrors.BadRequest("ASTRBOT_TOKEN_SCOPES_INVALID", "AstrBot token scopes are invalid")
	ErrAstrBotTokenExpiry      = infraerrors.BadRequest("ASTRBOT_TOKEN_EXPIRY_INVALID", "AstrBot token expiry is invalid")
	ErrAstrBotTokenInput       = infraerrors.BadRequest("ASTRBOT_TOKEN_INPUT_INVALID", "AstrBot token input is invalid")
	ErrAstrBotResourceScope    = infraerrors.BadRequest("ASTRBOT_RESOURCE_SCOPE_INVALID", "AstrBot resource allowlist is invalid")
	ErrAstrBotResourceNotFound = infraerrors.NotFound("ASTRBOT_RESOURCE_NOT_FOUND", "AstrBot resource not found")
	ErrAstrBotRequestSignature = infraerrors.Unauthorized("ASTRBOT_REQUEST_SIGNATURE_INVALID", "invalid AstrBot request signature")
	ErrAstrBotRequestReplay    = infraerrors.Unauthorized("ASTRBOT_REQUEST_REPLAYED", "AstrBot request nonce was already used")
	ErrAstrBotTokenCollision   = stderrors.New("astrbot token collision")
	ErrAstrBotTokenCreateFail  = infraerrors.InternalServer("ASTRBOT_TOKEN_CREATE_FAILED", "could not create AstrBot token")
	ErrAstrBotTokenIdempotency = infraerrors.ServiceUnavailable("ASTRBOT_TOKEN_IDEMPOTENCY_UNAVAILABLE", "retry-safe AstrBot token creation is unavailable")
)

const (
	astrBotBearerPrefix = "astrbot_"
	astrBotTokenIDBytes = 12
	astrBotTokenIDLen   = 16
	astrBotSecretBytes  = 32
	astrBotSecretLen    = 43
	astrBotMaxBearerLen = 256
	astrBotCreateTries  = 5
	// Token creation replay records contain encrypted bearer material and should
	// expire independently of the general write idempotency retention window.
	astrBotTokenReplayTTL = 15 * time.Minute
)

type AstrBotTokenService struct {
	repo                    AstrBotTokenRepository
	userRepo                UserRepository
	encryptor               SecretEncryptor
	encryptionKeyConfigured bool
}

func NewAstrBotTokenService(repo AstrBotTokenRepository, userRepo UserRepository) *AstrBotTokenService {
	return &AstrBotTokenService{repo: repo, userRepo: userRepo}
}

// NewAstrBotTokenServiceWithEncryptor enables retry-safe token creation. A
// stable configured key is required because replay data must survive restarts.
func NewAstrBotTokenServiceWithEncryptor(repo AstrBotTokenRepository, userRepo UserRepository, encryptor SecretEncryptor, encryptionKeyConfigured bool) *AstrBotTokenService {
	return &AstrBotTokenService{repo: repo, userRepo: userRepo, encryptor: encryptor, encryptionKeyConfigured: encryptionKeyConfigured}
}

type CreateAstrBotTokenInput struct {
	UserID             int64
	Name               string
	Scopes             []string
	InstallationID     string
	ResourceAllowlist  AstrBotResourceAllowlist
	ExpiresAt          *time.Time
	IdempotencyScope   string
	IdempotencyKeyHash string
	RequestFingerprint string
	Audit              *AuditLog
}

type CreatedAstrBotToken struct {
	Token              *AstrBotToken
	RawToken           string
	InstallationSecret string
	Replayed           bool
}

func (s *AstrBotTokenService) Create(ctx context.Context, input CreateAstrBotTokenInput) (*CreatedAstrBotToken, error) {
	if s == nil || s.repo == nil || input.UserID <= 0 {
		return nil, ErrAstrBotTokenInput
	}
	name := strings.TrimSpace(input.Name)
	if name == "" || len(name) > 128 {
		return nil, ErrAstrBotTokenName
	}
	scopes, err := normalizeAstrBotScopes(input.Scopes)
	if err != nil {
		return nil, err
	}
	installationID := strings.TrimSpace(input.InstallationID)
	if len(installationID) > 128 {
		return nil, ErrAstrBotResourceScope
	}
	allowlist, err := normalizeAstrBotResourceAllowlist(input.ResourceAllowlist)
	if err != nil {
		return nil, err
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return nil, ErrAstrBotTokenExpiry
	}

	// Keep the legacy service constructor usable for narrow internal callers and
	// tests. The HTTP creation path always supplies an idempotency operation.
	if input.IdempotencyScope == "" && input.IdempotencyKeyHash == "" && input.RequestFingerprint == "" {
		return s.createLegacy(ctx, input.UserID, name, scopes, input.ExpiresAt)
	}
	if installationID == "" || s.encryptor == nil || !s.encryptionKeyConfigured || input.IdempotencyScope == "" || input.IdempotencyKeyHash == "" || input.RequestFingerprint == "" || input.Audit == nil {
		return nil, ErrAstrBotTokenIdempotency
	}

	now := time.Now()
	lockedUntil := now.Add(30 * time.Second)
	expiresAt := now.Add(astrBotTokenReplayTTL)
	for attempt := 0; attempt < astrBotCreateTries; attempt++ {
		tokenID, err := randomURLToken(astrBotTokenIDBytes)
		if err != nil {
			return nil, fmt.Errorf("generate astrbot token id: %w", err)
		}
		secret, err := randomURLToken(astrBotSecretBytes)
		if err != nil {
			return nil, fmt.Errorf("generate astrbot token secret: %w", err)
		}
		raw := astrBotBearerPrefix + tokenID + "_" + secret
		encryptedRawToken, err := s.encryptor.Encrypt(raw)
		if err != nil {
			return nil, ErrAstrBotTokenIdempotency.WithCause(err)
		}
		installationSecret, err := randomURLToken(astrBotSecretBytes)
		if err != nil {
			return nil, fmt.Errorf("generate AstrBot installation secret: %w", err)
		}
		encryptedInstallationSecret, err := s.encryptor.Encrypt(installationSecret)
		if err != nil {
			return nil, ErrAstrBotTokenIdempotency.WithCause(err)
		}
		token := &AstrBotToken{TokenID: tokenID, UserID: input.UserID, Name: name, Scopes: scopes, InstallationID: installationID, ResourceAllowlist: allowlist, InstallationSigningSecretCiphertext: encryptedInstallationSecret, ExpiresAt: input.ExpiresAt}
		result, err := s.repo.CreateIdempotent(ctx, token, hashAstrBotBearer(raw), AstrBotTokenCreateOperation{
			Scope: input.IdempotencyScope, IdempotencyKeyHash: input.IdempotencyKeyHash,
			RequestFingerprint: input.RequestFingerprint, LockedUntil: lockedUntil, ExpiresAt: expiresAt,
		}, AstrBotTokenCreateReplay{EncryptedRawToken: encryptedRawToken, EncryptedInstallationSecret: encryptedInstallationSecret}, input.Audit)

		if err != nil {
			if stderrors.Is(err, ErrAstrBotTokenCollision) && attempt+1 < astrBotCreateTries {
				continue
			}
			return nil, err
		}
		if result == nil || result.Token == nil || result.EncryptedRawToken == "" || result.EncryptedInstallationSecret == "" {
			return nil, ErrAstrBotTokenIdempotency
		}
		replayedRaw, err := s.encryptor.Decrypt(result.EncryptedRawToken)
		if err != nil || !validAstrBotBearerForToken(result.Token.TokenID, replayedRaw) {
			return nil, ErrAstrBotTokenIdempotency
		}
		replayedInstallationSecret, err := s.encryptor.Decrypt(result.EncryptedInstallationSecret)
		if err != nil || replayedInstallationSecret == "" {
			return nil, ErrAstrBotTokenIdempotency
		}
		return &CreatedAstrBotToken{Token: result.Token, RawToken: replayedRaw, InstallationSecret: replayedInstallationSecret, Replayed: result.Replayed}, nil
	}
	return nil, ErrAstrBotTokenCreateFail
}

func (s *AstrBotTokenService) createLegacy(ctx context.Context, userID int64, name string, scopes []string, expiresAt *time.Time) (*CreatedAstrBotToken, error) {
	for attempt := 0; attempt < astrBotCreateTries; attempt++ {
		tokenID, err := randomURLToken(astrBotTokenIDBytes)
		if err != nil {
			return nil, fmt.Errorf("generate astrbot token id: %w", err)
		}
		secret, err := randomURLToken(astrBotSecretBytes)
		if err != nil {
			return nil, fmt.Errorf("generate astrbot token secret: %w", err)
		}
		raw := astrBotBearerPrefix + tokenID + "_" + secret
		token := &AstrBotToken{TokenID: tokenID, UserID: userID, Name: name, Scopes: scopes, ExpiresAt: expiresAt}
		if err := s.repo.Create(ctx, token, hashAstrBotBearer(raw)); err != nil {
			if stderrors.Is(err, ErrAstrBotTokenCollision) && attempt+1 < astrBotCreateTries {
				continue
			}
			if stderrors.Is(err, ErrAstrBotTokenCollision) {
				return nil, ErrAstrBotTokenCreateFail
			}
			return nil, err
		}
		return &CreatedAstrBotToken{Token: token, RawToken: raw}, nil
	}
	return nil, ErrAstrBotTokenCreateFail
}

func validAstrBotBearerForToken(tokenID, bearer string) bool {
	parsedID, ok := parseAstrBotBearer(bearer)
	return ok && parsedID == tokenID
}

func (s *AstrBotTokenService) List(ctx context.Context, userID int64) ([]*AstrBotToken, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrAstrBotTokenInvalid
	}
	return s.repo.ListByUserID(ctx, userID)
}

func (s *AstrBotTokenService) Revoke(ctx context.Context, userID int64, tokenID string) error {
	if s == nil || s.repo == nil || userID <= 0 || !validAstrBotTokenID(tokenID) {
		return ErrAstrBotTokenNotFound
	}
	ok, err := s.repo.RevokeByTokenIDAndUserID(ctx, tokenID, userID, time.Now())
	if err != nil {
		return err
	}
	if !ok {
		return ErrAstrBotTokenNotFound
	}
	return nil
}

func (s *AstrBotTokenService) Authenticate(ctx context.Context, bearer string) (*AstrBotToken, error) {
	if s == nil || s.repo == nil || len(bearer) == 0 || len(bearer) > astrBotMaxBearerLen {
		return nil, ErrAstrBotTokenInvalid
	}
	tokenID, ok := parseAstrBotBearer(bearer)
	if !ok {
		return nil, ErrAstrBotTokenInvalid
	}
	token, storedHash, err := s.repo.GetActiveByTokenID(ctx, tokenID)
	if err != nil {
		return nil, ErrAstrBotTokenInvalid
	}
	presentedHash := hashAstrBotBearer(bearer)
	if subtle.ConstantTimeCompare([]byte(presentedHash), []byte(storedHash)) != 1 {
		return nil, ErrAstrBotTokenInvalid
	}
	if token.ExpiresAt != nil && !time.Now().Before(*token.ExpiresAt) {
		return nil, ErrAstrBotTokenInvalid
	}
	if s.userRepo != nil {
		user, userErr := s.userRepo.GetByID(ctx, token.UserID)
		if userErr != nil || user == nil || !user.IsActive() {
			return nil, ErrAstrBotTokenInvalid
		}
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.repo.TouchLastUsed(bg, token.TokenID, time.Now())
	}()
	return token, nil
}

func canonicalAstrBotRequest(method, path, timestamp, nonce, platformUser, sessionID, conversationID, body string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + "\n" + path + "\n" + timestamp + "\n" + nonce + "\n" + platformUser + "\n" + sessionID + "\n" + conversationID + "\n" + body
}

func validAstrBotCanonicalBindingValue(value string) bool {
	return !strings.ContainsAny(value, "\r\n\x00") && len(value) <= 256
}

func (s *AstrBotTokenService) HasScope(token *AstrBotToken, scope string) bool {
	if token == nil {
		return false
	}
	for _, candidate := range token.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func normalizeAstrBotResourceAllowlist(input AstrBotResourceAllowlist) (AstrBotResourceAllowlist, error) {
	if len(input.AccountIDs) > 1000 || len(input.ChannelIDs) > 1000 || len(input.GroupIDs) > 1000 || len(input.UserIDs) > 1000 || len(input.LogSources) > 100 {
		return AstrBotResourceAllowlist{}, ErrAstrBotResourceScope
	}
	result := AstrBotResourceAllowlist{
		AccountIDs: append([]int64(nil), input.AccountIDs...), ChannelIDs: append([]int64(nil), input.ChannelIDs...),
		GroupIDs: append([]int64(nil), input.GroupIDs...), UserIDs: append([]int64(nil), input.UserIDs...), LogSources: append([]string(nil), input.LogSources...),
	}
	for _, values := range [][]int64{result.AccountIDs, result.ChannelIDs, result.GroupIDs, result.UserIDs} {
		for _, value := range values {
			if value <= 0 {
				return AstrBotResourceAllowlist{}, ErrAstrBotResourceScope
			}
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for i := 1; i < len(values); i++ {
			if values[i] == values[i-1] {
				return AstrBotResourceAllowlist{}, ErrAstrBotResourceScope
			}
		}
	}
	for i, source := range result.LogSources {
		result.LogSources[i] = strings.TrimSpace(source)
		if result.LogSources[i] == "" || len(result.LogSources[i]) > 64 {
			return AstrBotResourceAllowlist{}, ErrAstrBotResourceScope
		}
		if i > 0 && result.LogSources[i] <= result.LogSources[i-1] {
			return AstrBotResourceAllowlist{}, ErrAstrBotResourceScope
		}
	}
	return result, nil
}

func (s *AstrBotTokenService) VerifyRequestSignature(ctx context.Context, token *AstrBotToken, timestamp, nonce, method, path, platformUser, sessionID, conversationID, body, signature string, now time.Time) error {
	if s == nil || s.repo == nil || s.encryptor == nil || token == nil || token.TokenID == "" || strings.TrimSpace(token.InstallationID) == "" || strings.TrimSpace(token.InstallationSigningSecretCiphertext) == "" {
		return ErrAstrBotRequestSignature
	}
	timestamp = strings.TrimSpace(timestamp)
	nonce = strings.TrimSpace(nonce)
	signature = strings.TrimSpace(signature)
	if timestamp == "" || nonce == "" || signature == "" || len(nonce) > 128 || len(signature) > 128 {
		return ErrAstrBotRequestSignature
	}
	if !validAstrBotCanonicalBindingValue(platformUser) || !validAstrBotCanonicalBindingValue(sessionID) || !validAstrBotCanonicalBindingValue(conversationID) {
		return ErrAstrBotRequestSignature
	}
	parsed, err := time.Parse(time.RFC3339, timestamp)
	if err != nil || parsed.Before(now.Add(-5*time.Minute)) || parsed.After(now.Add(30*time.Second)) {
		return ErrAstrBotRequestSignature
	}
	secret, err := s.encryptor.Decrypt(token.InstallationSigningSecretCiphertext)
	if err != nil || len(secret) == 0 {
		return ErrAstrBotRequestSignature
	}
	canonical := canonicalAstrBotRequest(method, path, timestamp, nonce, platformUser, sessionID, conversationID, body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) != 1 {
		return ErrAstrBotRequestSignature
	}
	consumed, err := s.repo.ConsumeRequestNonce(ctx, token.TokenID, token.InstallationID, hashAstrBotRequestNonce(nonce), now.Add(5*time.Minute))
	if err != nil {
		return ErrAstrBotRequestSignature
	}
	if !consumed {
		return ErrAstrBotRequestReplay
	}
	return nil
}

func hashAstrBotRequestNonce(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeAstrBotScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, ErrAstrBotTokenScopes
	}
	seen := make(map[string]struct{}, len(scopes))
	result := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope != AstrBotScopeRead && scope != AstrBotScopeWrite {
			return nil, ErrAstrBotTokenScopes
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		result = append(result, scope)
	}
	if len(result) == 0 {
		return nil, ErrAstrBotTokenScopes
	}
	return result, nil
}

func randomURLToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashAstrBotBearer(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func parseAstrBotBearer(value string) (string, bool) {
	if len(value) > astrBotMaxBearerLen || !strings.HasPrefix(value, astrBotBearerPrefix) {
		return "", false
	}
	body := strings.TrimPrefix(value, astrBotBearerPrefix)
	if len(body) != astrBotTokenIDLen+1+astrBotSecretLen || body[astrBotTokenIDLen] != '_' {
		return "", false
	}
	tokenID := body[:astrBotTokenIDLen]
	secret := body[astrBotTokenIDLen+1:]
	if !validAstrBotTokenID(tokenID) || secret == "" {
		return "", false
	}
	for _, r := range secret {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return "", false
		}
	}
	return tokenID, true
}

func validAstrBotTokenID(value string) bool {
	if value == "" || len(value) < 8 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
