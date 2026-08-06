package service

import (
	"context"
	"time"
)

const (
	AstrBotScopeRead  = "bot:read"
	AstrBotScopeWrite = "bot:write"
)

type AstrBotResourceAllowlist struct {
	AccountIDs []int64  `json:"account_ids,omitempty"`
	ChannelIDs []int64  `json:"channel_ids,omitempty"`
	GroupIDs   []int64  `json:"group_ids,omitempty"`
	UserIDs    []int64  `json:"user_ids,omitempty"`
	LogSources []string `json:"log_sources,omitempty"`
}

func (a AstrBotResourceAllowlist) IsUnrestricted() bool {
	return len(a.AccountIDs) == 0 && len(a.ChannelIDs) == 0 && len(a.GroupIDs) == 0 && len(a.UserIDs) == 0 && len(a.LogSources) == 0
}

func (a AstrBotResourceAllowlist) AllowsAccount(id int64) bool {
	return a.allowsID(a.AccountIDs, id)
}

func (a AstrBotResourceAllowlist) AllowsChannel(id int64) bool {
	return a.allowsID(a.ChannelIDs, id)
}

func (a AstrBotResourceAllowlist) AllowsGroup(id int64) bool {
	return a.allowsID(a.GroupIDs, id)
}

func (a AstrBotResourceAllowlist) AllowsUser(id int64) bool {
	return a.allowsID(a.UserIDs, id)
}

func (a AstrBotResourceAllowlist) AllowsLogSource(source string) bool {
	if a.IsUnrestricted() {
		return true
	}
	for _, candidate := range a.LogSources {
		if candidate == source {
			return true
		}
	}
	return false
}

func (a AstrBotResourceAllowlist) allowsID(values []int64, id int64) bool {
	if a.IsUnrestricted() {
		return true
	}
	if id <= 0 {
		return false
	}
	for _, candidate := range values {
		if candidate == id {
			return true
		}
	}
	return false
}

type AstrBotToken struct {
	ID                                  int64
	TokenID                             string
	UserID                              int64
	Name                                string
	Scopes                              []string
	InstallationID                      string
	ResourceAllowlist                   AstrBotResourceAllowlist
	InstallationSigningSecretCiphertext string
	ExpiresAt                           *time.Time
	RevokedAt                           *time.Time
	LastUsedAt                          *time.Time
	CreatedAt                           time.Time
	UpdatedAt                           time.Time
}

// AstrBotTokenCreateOperation identifies one retry-safe token creation request.
// It deliberately contains only hashes and a request fingerprint; the bearer
// itself is never part of the idempotency identity.
type AstrBotTokenCreateOperation struct {
	Scope              string
	IdempotencyKeyHash string
	RequestFingerprint string
	LockedUntil        time.Time
	ExpiresAt          time.Time
}

type AstrBotTokenReplayMetadata struct {
	ID                int64                    `json:"id"`
	TokenID           string                   `json:"token_id"`
	UserID            int64                    `json:"user_id"`
	Name              string                   `json:"name"`
	Scopes            []string                 `json:"scopes"`
	InstallationID    string                   `json:"installation_id,omitempty"`
	ResourceAllowlist AstrBotResourceAllowlist `json:"resource_allowlist,omitempty"`
	ExpiresAt         *time.Time               `json:"expires_at,omitempty"`
	RevokedAt         *time.Time               `json:"revoked_at,omitempty"`
	LastUsedAt        *time.Time               `json:"last_used_at,omitempty"`
	CreatedAt         time.Time                `json:"created_at"`
	UpdatedAt         time.Time                `json:"updated_at"`
}

// AstrBotTokenCreateReplay is the only token material kept in the idempotency
// record. Encrypted values are ciphertext, never the bearer or signing secret.
type AstrBotTokenCreateReplay struct {
	Token                       AstrBotTokenReplayMetadata `json:"token"`
	EncryptedRawToken           string                     `json:"encrypted_raw_token"`
	EncryptedInstallationSecret string                     `json:"encrypted_installation_secret"`
}

type AstrBotTokenCreateResult struct {
	Token                       *AstrBotToken
	EncryptedRawToken           string
	EncryptedInstallationSecret string
	Replayed                    bool
}

type AstrBotTokenRepository interface {
	Create(ctx context.Context, token *AstrBotToken, tokenHash string) error
	CreateIdempotent(ctx context.Context, token *AstrBotToken, tokenHash string, operation AstrBotTokenCreateOperation, replay AstrBotTokenCreateReplay, audit *AuditLog) (*AstrBotTokenCreateResult, error)
	ListByUserID(ctx context.Context, userID int64) ([]*AstrBotToken, error)
	GetActiveByTokenID(ctx context.Context, tokenID string) (*AstrBotToken, string, error)
	ConsumeRequestNonce(ctx context.Context, tokenID, installationID, nonce string, expiresAt time.Time) (bool, error)
	RevokeByTokenIDAndUserID(ctx context.Context, tokenID string, userID int64, revokedAt time.Time) (bool, error)
	TouchLastUsed(ctx context.Context, tokenID string, now time.Time) error
}
