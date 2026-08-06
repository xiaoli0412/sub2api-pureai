package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	AstrBotOperationTypeChannelToggle         = "channel.toggle"
	AstrBotOperationTypeAccountToggle         = "account.toggle"
	AstrBotOperationTypeAccountRateMultiplier = "account.rate_multiplier"
	AstrBotOperationTypeChannelPricing        = "channel.pricing"
	AstrBotOperationTypeCacheRefresh          = "cache.refresh"
	AstrBotOperationStatePrepared             = "prepared"
	AstrBotOperationStateConfirmedOnce        = "confirmed_once"
	AstrBotOperationStateConfirmedTwice       = "confirmed_twice"
	AstrBotOperationStateExecuting            = "executing"
	AstrBotOperationStateSucceeded            = "succeeded"
	AstrBotOperationStateFailedRetryable      = "failed_retryable"
	AstrBotOperationStateCancelled            = "cancelled"
	AstrBotOperationStateExpired              = "expired"
	astrBotOperationTTL                       = 15 * time.Minute
	astrBotOperationChallengeTTL              = 5 * time.Minute
	astrBotOperationExecutionLease            = 30 * time.Second
	astrBotOperationMaxPayloadBytes           = 64 * 1024
	astrBotOperationMaxDiffBytes              = 32 * 1024
)

var (
	ErrAstrBotOperationNotFound      = infraerrors.NotFound("ASTRBOT_OPERATION_NOT_FOUND", "AstrBot operation not found")
	ErrAstrBotOperationConflict      = infraerrors.Conflict("ASTRBOT_OPERATION_CONFLICT", "AstrBot operation conflicts with an existing operation")
	ErrAstrBotOperationPayload       = infraerrors.BadRequest("ASTRBOT_OPERATION_PAYLOAD_INVALID", "AstrBot operation payload is invalid")
	ErrAstrBotOperationType          = infraerrors.BadRequest("ASTRBOT_OPERATION_TYPE_INVALID", "AstrBot operation type is invalid")
	ErrAstrBotOperationBinding       = infraerrors.Conflict("ASTRBOT_OPERATION_BINDING_MISMATCH", "AstrBot operation binding does not match")
	ErrAstrBotOperationState         = infraerrors.Conflict("ASTRBOT_OPERATION_STATE_INVALID", "AstrBot operation is not in the required state")
	ErrAstrBotOperationExpired       = infraerrors.Conflict("ASTRBOT_OPERATION_EXPIRED", "AstrBot operation has expired")
	ErrAstrBotOperationCancelled     = infraerrors.Conflict("ASTRBOT_OPERATION_CANCELLED", "AstrBot operation has been cancelled")
	ErrAstrBotOperationInProgress    = infraerrors.Conflict("ASTRBOT_OPERATION_IN_PROGRESS", "AstrBot operation is already executing")
	ErrAstrBotOperationChallenge     = infraerrors.Conflict("ASTRBOT_CHALLENGE_INVALID", "AstrBot confirmation challenge is invalid or expired")
	ErrAstrBotChallengeAlreadyIssued = infraerrors.Conflict("ASTRBOT_CHALLENGE_ALREADY_ISSUED", "AstrBot confirmation challenge was already issued")
	ErrAstrBotOperationStore         = infraerrors.ServiceUnavailable("ASTRBOT_OPERATION_STORE_UNAVAILABLE", "AstrBot operation store unavailable")
)

type AstrBotOperation struct {
	ID                        int64
	OperationID               string
	UserID                    int64
	TokenID                   string
	InstallationID            string
	PlatformUserHash          string
	SessionHash               string
	ConversationHash          string
	OperationType             string
	Method                    string
	Route                     string
	TargetID                  *int64
	PayloadJSON               string
	DiffJSON                  string
	CanonicalPayloadHash      string
	IdempotencyKeyHash        string
	State                     string
	ConfirmationCount         int
	FirstConfirmationKeyHash  string
	FirstConfirmerUserID      *int64
	FirstConfirmedAt          *time.Time
	SecondConfirmationKeyHash string
	SecondConfirmerUserID     *int64
	SecondConfirmedAt         *time.Time
	ChallengeHash             string
	ChallengeExpiresAt        *time.Time
	LockedUntil               *time.Time
	ResponseStatus            *int
	ResponseBody              string
	ErrorReason               string
	ExpiresAt                 time.Time
	CompletedAt               *time.Time
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
}

type AstrBotOperationRepository interface {
	Create(ctx context.Context, operation *AstrBotOperation) error
	GetByID(ctx context.Context, operationID, tokenID string, userID int64) (*AstrBotOperation, error)
	GetByIdentity(ctx context.Context, tokenID, operationType string, targetID *int64, idempotencyKeyHash string) (*AstrBotOperation, error)
	ConfirmFirst(ctx context.Context, operationID, tokenID string, userID int64, installationID, keyHash, challengeHash string, challengeExpiresAt, now time.Time) (bool, error)
	ConfirmSecond(ctx context.Context, operationID, tokenID string, userID int64, installationID, keyHash, challengeHash string, now time.Time) (bool, error)
	ClaimExecution(ctx context.Context, operationID, tokenID string, userID int64, installationID string, now, lockedUntil time.Time) (bool, error)
	MarkSucceeded(ctx context.Context, operationID string, responseStatus int, responseBody string, completedAt time.Time) error
	MarkFailedRetryable(ctx context.Context, operationID, errorReason string, lockedUntil time.Time) error
	Cancel(ctx context.Context, operationID, tokenID string, userID int64, installationID string, now time.Time) (bool, error)
	Expire(ctx context.Context, operationID, tokenID string, userID int64, now time.Time) error
}

type AstrBotOperationPrepareInput struct {
	UserID             int64
	TokenID            string
	InstallationID     string
	PlatformUserHash   string
	SessionHash        string
	ConversationHash   string
	OperationType      string
	TargetID           *int64
	Payload            json.RawMessage
	Diff               json.RawMessage
	IdempotencyKeyHash string
}

type AstrBotOperationConfirmInput struct {
	OperationID      string
	UserID           int64
	TokenID          string
	InstallationID   string
	PlatformUserHash string
	SessionHash      string
	ConversationHash string
	KeyHash          string
}

type AstrBotOperationConfirmAgainInput struct {
	OperationID      string
	UserID           int64
	TokenID          string
	InstallationID   string
	PlatformUserHash string
	SessionHash      string
	ConversationHash string
	KeyHash          string
	Challenge        string
}

type AstrBotOperationExecuteInput struct {
	OperationID        string
	UserID             int64
	TokenID            string
	InstallationID     string
	PlatformUserHash   string
	SessionHash        string
	ConversationHash   string
	OperationType      string
	TargetID           *int64
	Payload            json.RawMessage
	IdempotencyKeyHash string
}

type AstrBotOperationService struct {
	repo           AstrBotOperationRepository
	operationTTL   time.Duration
	challengeTTL   time.Duration
	executionLease time.Duration
}

func NewAstrBotOperationService(repo AstrBotOperationRepository) *AstrBotOperationService {
	return &AstrBotOperationService{
		repo:           repo,
		operationTTL:   astrBotOperationTTL,
		challengeTTL:   astrBotOperationChallengeTTL,
		executionLease: astrBotOperationExecutionLease,
	}
}

func (s *AstrBotOperationService) Prepare(ctx context.Context, input AstrBotOperationPrepareInput) (*AstrBotOperation, error) {
	if s == nil || s.repo == nil || input.UserID <= 0 || strings.TrimSpace(input.TokenID) == "" || strings.TrimSpace(input.InstallationID) == "" || strings.TrimSpace(input.IdempotencyKeyHash) == "" {
		return nil, ErrAstrBotOperationPayload
	}
	method, route, err := astrBotOperationRoute(input.OperationType, input.TargetID)
	if err != nil {
		return nil, err
	}
	payloadJSON, payloadHash, err := NormalizeAstrBotOperationPayload(input.OperationType, input.TargetID, input.Payload)
	if err != nil {
		return nil, err
	}
	diffJSON := "{}"
	if len(input.Diff) > 0 {
		diffJSON, err = normalizeOperationJSON(input.Diff, astrBotOperationMaxDiffBytes)
		if err != nil {
			return nil, ErrAstrBotOperationPayload
		}
	}
	if existing, getErr := s.repo.GetByIdentity(ctx, input.TokenID, input.OperationType, input.TargetID, input.IdempotencyKeyHash); getErr == nil && existing != nil {
		if existing.CanonicalPayloadHash != payloadHash || existing.InstallationID != input.InstallationID {
			return nil, ErrAstrBotOperationConflict
		}
		return existing, nil
	} else if getErr != nil && !errors.Is(getErr, ErrAstrBotOperationNotFound) {
		return nil, ErrAstrBotOperationStore.WithCause(getErr)
	}

	opID, err := randomAstrBotOperationID()
	if err != nil {
		return nil, ErrAstrBotOperationStore.WithCause(err)
	}
	now := time.Now().UTC()
	op := &AstrBotOperation{
		OperationID:          opID,
		UserID:               input.UserID,
		TokenID:              input.TokenID,
		InstallationID:       input.InstallationID,
		PlatformUserHash:     input.PlatformUserHash,
		SessionHash:          input.SessionHash,
		ConversationHash:     input.ConversationHash,
		OperationType:        input.OperationType,
		Method:               method,
		Route:                route,
		TargetID:             input.TargetID,
		PayloadJSON:          payloadJSON,
		DiffJSON:             diffJSON,
		CanonicalPayloadHash: payloadHash,
		IdempotencyKeyHash:   input.IdempotencyKeyHash,
		State:                AstrBotOperationStatePrepared,
		ExpiresAt:            now.Add(s.operationTTL),
	}
	if err := s.repo.Create(ctx, op); err != nil {
		if errors.Is(err, ErrAstrBotOperationConflict) {
			existing, getErr := s.repo.GetByIdentity(ctx, input.TokenID, input.OperationType, input.TargetID, input.IdempotencyKeyHash)
			if getErr != nil || existing == nil {
				return nil, ErrAstrBotOperationStore.WithCause(getErr)
			}
			if existing.CanonicalPayloadHash != payloadHash || existing.InstallationID != input.InstallationID {
				return nil, ErrAstrBotOperationConflict
			}
			return existing, nil
		}
		return nil, ErrAstrBotOperationStore.WithCause(err)
	}
	return op, nil
}

func (s *AstrBotOperationService) Get(ctx context.Context, input AstrBotOperationExecuteInput) (*AstrBotOperation, error) {
	op, err := s.repo.GetByID(ctx, input.OperationID, input.TokenID, input.UserID)
	if err != nil || op == nil {
		return nil, ErrAstrBotOperationNotFound
	}
	if !astrBotOperationBindingMatches(op, input.InstallationID, input.PlatformUserHash, input.SessionHash, input.ConversationHash) {
		return nil, ErrAstrBotOperationNotFound
	}
	if operationNeedsExpiry(op.State) && !op.ExpiresAt.After(time.Now()) {
		_ = s.repo.Expire(ctx, op.OperationID, op.TokenID, op.UserID, time.Now())
		return nil, ErrAstrBotOperationExpired
	}
	return op, nil
}

func (s *AstrBotOperationService) Confirm(ctx context.Context, input AstrBotOperationConfirmInput) (*AstrBotOperation, string, error) {
	op, err := s.repo.GetByID(ctx, input.OperationID, input.TokenID, input.UserID)
	if err != nil || op == nil || !astrBotOperationBindingMatches(op, input.InstallationID, input.PlatformUserHash, input.SessionHash, input.ConversationHash) {
		return nil, "", ErrAstrBotOperationNotFound
	}
	if !operationNeedsExpiry(op.State) || !op.ExpiresAt.After(time.Now()) {
		return nil, "", ErrAstrBotOperationExpired
	}
	if op.State == AstrBotOperationStateConfirmedOnce && op.FirstConfirmationKeyHash == input.KeyHash {
		return op, "", ErrAstrBotChallengeAlreadyIssued
	}
	if op.State != AstrBotOperationStatePrepared {
		return nil, "", ErrAstrBotOperationState
	}
	challenge, err := randomAstrBotChallenge()
	if err != nil {
		return nil, "", ErrAstrBotOperationStore.WithCause(err)
	}
	now := time.Now().UTC()
	ok, err := s.repo.ConfirmFirst(ctx, input.OperationID, input.TokenID, input.UserID, input.InstallationID, input.KeyHash, hashAstrBotOperationSecret(challenge), now.Add(s.challengeTTL), now)
	if err != nil {
		return nil, "", ErrAstrBotOperationStore.WithCause(err)
	}
	if !ok {
		return nil, "", ErrAstrBotOperationState
	}
	op, err = s.repo.GetByID(ctx, input.OperationID, input.TokenID, input.UserID)
	if err != nil {
		return nil, "", ErrAstrBotOperationStore.WithCause(err)
	}
	return op, challenge, nil
}

func (s *AstrBotOperationService) ConfirmAgain(ctx context.Context, input AstrBotOperationConfirmAgainInput) (*AstrBotOperation, error) {
	if strings.TrimSpace(input.Challenge) == "" {
		return nil, ErrAstrBotOperationChallenge
	}
	op, err := s.repo.GetByID(ctx, input.OperationID, input.TokenID, input.UserID)
	if err != nil || op == nil || !astrBotOperationBindingMatches(op, input.InstallationID, input.PlatformUserHash, input.SessionHash, input.ConversationHash) {
		return nil, ErrAstrBotOperationNotFound
	}
	if op.State == AstrBotOperationStateConfirmedTwice && op.SecondConfirmationKeyHash == input.KeyHash {
		return op, nil
	}
	if op.State != AstrBotOperationStateConfirmedOnce || op.ChallengeExpiresAt == nil || !op.ChallengeExpiresAt.After(time.Now()) {
		return nil, ErrAstrBotOperationChallenge
	}
	if !constantTimeStringEqual(op.ChallengeHash, hashAstrBotOperationSecret(input.Challenge)) {
		return nil, ErrAstrBotOperationChallenge
	}
	now := time.Now().UTC()
	ok, err := s.repo.ConfirmSecond(ctx, input.OperationID, input.TokenID, input.UserID, input.InstallationID, input.KeyHash, op.ChallengeHash, now)
	if err != nil {
		return nil, ErrAstrBotOperationStore.WithCause(err)
	}
	if !ok {
		return nil, ErrAstrBotOperationChallenge
	}
	return s.repo.GetByID(ctx, input.OperationID, input.TokenID, input.UserID)
}

func (s *AstrBotOperationService) Claim(ctx context.Context, input AstrBotOperationExecuteInput) (*AstrBotOperation, bool, error) {
	op, err := s.Get(ctx, input)
	if err != nil {
		return nil, false, err
	}
	if err := validateAstrBotOperationExecution(op, input); err != nil {
		return nil, false, err
	}
	if op.State == AstrBotOperationStateSucceeded {
		return op, false, nil
	}
	if op.State == AstrBotOperationStateCancelled {
		return nil, false, ErrAstrBotOperationCancelled
	}
	now := time.Now().UTC()
	canClaim := op.State == AstrBotOperationStateConfirmedTwice || op.State == AstrBotOperationStateFailedRetryable
	if op.State == AstrBotOperationStateExecuting {
		if op.LockedUntil != nil && op.LockedUntil.After(now) {
			return nil, false, ErrAstrBotOperationInProgress
		}
		canClaim = true
	}
	if !canClaim {
		return nil, false, ErrAstrBotOperationState
	}
	ok, err := s.repo.ClaimExecution(ctx, op.OperationID, op.TokenID, op.UserID, op.InstallationID, now, now.Add(s.executionLease))
	if err != nil {
		return nil, false, ErrAstrBotOperationStore.WithCause(err)
	}
	if !ok {
		latest, getErr := s.repo.GetByID(ctx, op.OperationID, op.TokenID, op.UserID)
		if getErr == nil && latest != nil && latest.State == AstrBotOperationStateSucceeded {
			return latest, false, nil
		}
		return nil, false, ErrAstrBotOperationInProgress
	}
	op.State = AstrBotOperationStateExecuting
	return op, true, nil
}

func validateAstrBotOperationExecution(op *AstrBotOperation, input AstrBotOperationExecuteInput) error {
	if op == nil {
		return ErrAstrBotOperationNotFound
	}
	if input.OperationType != "" && op.OperationType != input.OperationType {
		return ErrAstrBotOperationBinding
	}
	if !sameAstrBotTargetID(op.TargetID, input.TargetID) {
		return ErrAstrBotOperationBinding
	}
	if input.IdempotencyKeyHash != "" && op.IdempotencyKeyHash != input.IdempotencyKeyHash {
		return ErrAstrBotOperationBinding
	}
	if len(input.Payload) > 0 {
		_, payloadHash, err := NormalizeAstrBotOperationPayload(op.OperationType, op.TargetID, input.Payload)
		if err != nil {
			return err
		}
		if payloadHash != op.CanonicalPayloadHash {
			return ErrAstrBotOperationBinding
		}
	}
	return nil
}

func sameAstrBotTargetID(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *AstrBotOperationService) MarkSucceeded(ctx context.Context, operationID string, data any) error {
	body, err := json.Marshal(data)
	if err != nil || len(body) > astrBotOperationMaxPayloadBytes {
		return ErrAstrBotOperationPayload
	}
	if err := s.repo.MarkSucceeded(ctx, operationID, 200, string(body), time.Now().UTC()); err != nil {
		return ErrAstrBotOperationStore.WithCause(err)
	}
	return nil
}

func (s *AstrBotOperationService) MarkFailedRetryable(ctx context.Context, operationID string, cause error) error {
	reason := "mutation_failed"
	if cause != nil {
		reason = truncateAstrBotOperationReason(cause.Error())
	}
	if err := s.repo.MarkFailedRetryable(ctx, operationID, reason, time.Now().UTC().Add(5*time.Second)); err != nil {
		return ErrAstrBotOperationStore.WithCause(err)
	}
	return nil
}

func (s *AstrBotOperationService) Cancel(ctx context.Context, input AstrBotOperationExecuteInput) (*AstrBotOperation, error) {
	op, err := s.Get(ctx, input)
	if err != nil {
		return nil, err
	}
	if op.State == AstrBotOperationStateSucceeded {
		return nil, ErrAstrBotOperationState
	}
	ok, err := s.repo.Cancel(ctx, op.OperationID, op.TokenID, op.UserID, op.InstallationID, time.Now().UTC())
	if err != nil {
		return nil, ErrAstrBotOperationStore.WithCause(err)
	}
	if !ok {
		return nil, ErrAstrBotOperationState
	}
	return s.repo.GetByID(ctx, op.OperationID, op.TokenID, op.UserID)
}

func DecodeAstrBotOperationPayload(op *AstrBotOperation) (json.RawMessage, error) {
	if op == nil || strings.TrimSpace(op.PayloadJSON) == "" {
		return nil, ErrAstrBotOperationPayload
	}
	var envelope struct {
		OperationType string          `json:"operation_type"`
		TargetID      *int64          `json:"target_id"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(op.PayloadJSON), &envelope); err != nil || len(envelope.Payload) == 0 {
		return nil, ErrAstrBotOperationPayload
	}
	if envelope.OperationType != op.OperationType || !sameAstrBotTargetID(envelope.TargetID, op.TargetID) {
		return nil, ErrAstrBotOperationBinding
	}
	return append(json.RawMessage(nil), envelope.Payload...), nil
}

func DecodeAstrBotOperationResponse(op *AstrBotOperation) (any, error) {
	if op == nil || strings.TrimSpace(op.ResponseBody) == "" {
		return nil, ErrAstrBotOperationStore
	}
	var value any
	if err := json.Unmarshal([]byte(op.ResponseBody), &value); err != nil {
		return nil, ErrAstrBotOperationStore.WithCause(err)
	}
	return value, nil
}

func BuildAstrBotOperationCanonicalPayload(operationType string, targetID *int64, payload any) (string, string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "", ErrAstrBotOperationPayload.WithCause(err)
	}
	return NormalizeAstrBotOperationPayload(operationType, targetID, raw)
}

func NormalizeAstrBotOperationPayload(operationType string, targetID *int64, raw json.RawMessage) (string, string, error) {
	if _, _, err := astrBotOperationRoute(operationType, targetID); err != nil {
		return "", "", err
	}
	payloadJSON, err := normalizeOperationJSON(raw, astrBotOperationMaxPayloadBytes)
	if err != nil {
		return "", "", ErrAstrBotOperationPayload
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil || payload == nil {
		return "", "", ErrAstrBotOperationPayload
	}
	if err := validateAstrBotOperationFields(operationType, payload); err != nil {
		return "", "", err
	}
	envelope := map[string]any{"operation_type": operationType, "payload": payload}
	if targetID != nil {
		envelope["target_id"] = *targetID
	}
	canonical, err := json.Marshal(envelope)
	if err != nil {
		return "", "", ErrAstrBotOperationPayload.WithCause(err)
	}
	sum := sha256.Sum256(canonical)
	return string(canonical), hex.EncodeToString(sum[:]), nil
}

func astrBotOperationRoute(operationType string, targetID *int64) (string, string, error) {
	if targetID != nil && *targetID <= 0 {
		return "", "", ErrAstrBotOperationPayload
	}
	switch operationType {
	case AstrBotOperationTypeChannelToggle:
		if targetID == nil {
			return "", "", ErrAstrBotOperationPayload
		}
		return "POST", "/api/v1/bot/channels/:id/toggle", nil
	case AstrBotOperationTypeAccountToggle:
		if targetID == nil {
			return "", "", ErrAstrBotOperationPayload
		}
		return "POST", "/api/v1/bot/accounts/:id/toggle", nil
	case AstrBotOperationTypeAccountRateMultiplier:
		if targetID == nil {
			return "", "", ErrAstrBotOperationPayload
		}
		return "PUT", "/api/v1/bot/accounts/:id/rate-multiplier", nil
	case AstrBotOperationTypeChannelPricing:
		if targetID == nil {
			return "", "", ErrAstrBotOperationPayload
		}
		return "PUT", "/api/v1/bot/channels/:id/pricing", nil
	case AstrBotOperationTypeCacheRefresh:
		if targetID != nil {
			return "", "", ErrAstrBotOperationPayload
		}
		return "POST", "/api/v1/bot/cache/refresh", nil
	default:
		return "", "", ErrAstrBotOperationType
	}
}

func validateAstrBotOperationFields(operationType string, payload map[string]any) error {
	allowed := map[string]map[string]struct{}{
		AstrBotOperationTypeChannelToggle:         {"enabled": {}},
		AstrBotOperationTypeAccountToggle:         {"enabled": {}},
		AstrBotOperationTypeAccountRateMultiplier: {"rate_multiplier": {}},
		AstrBotOperationTypeChannelPricing:        {"pricing": {}},
		AstrBotOperationTypeCacheRefresh:          {"refresh": {}},
	}
	keys, ok := allowed[operationType]
	if !ok {
		return ErrAstrBotOperationType
	}
	for key := range payload {
		if _, ok := keys[key]; !ok {
			return ErrAstrBotOperationPayload
		}
	}
	switch operationType {
	case AstrBotOperationTypeChannelToggle, AstrBotOperationTypeAccountToggle:
		if _, ok := payload["enabled"].(bool); !ok {
			return ErrAstrBotOperationPayload
		}
	case AstrBotOperationTypeAccountRateMultiplier:
		value, ok := payload["rate_multiplier"].(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return ErrAstrBotOperationPayload
		}
	case AstrBotOperationTypeChannelPricing:
		pricing, ok := payload["pricing"].([]any)
		if !ok || len(pricing) == 0 {
			return ErrAstrBotOperationPayload
		}
		for _, item := range pricing {
			entry, ok := item.(map[string]any)
			if !ok {
				return ErrAstrBotOperationPayload
			}
			for key := range entry {
				switch key {
				case "platform", "models", "billing_mode", "input_price", "output_price", "cache_write_price", "cache_read_price", "image_input_price", "image_output_price", "per_request_price", "intervals":
				default:
					return ErrAstrBotOperationPayload
				}
			}
		}
	case AstrBotOperationTypeCacheRefresh:
		if value, ok := payload["refresh"].(bool); !ok || !value {
			return ErrAstrBotOperationPayload
		}
	}
	return nil
}

func normalizeOperationJSON(raw []byte, maxBytes int) (string, error) {
	if len(raw) == 0 || len(raw) > maxBytes {
		return "", fmt.Errorf("invalid operation json size")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	if value == nil {
		return "", fmt.Errorf("operation json must not be null")
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxBytes {
		return "", fmt.Errorf("invalid operation json")
	}
	return string(encoded), nil
}

func astrBotOperationBindingMatches(op *AstrBotOperation, installationID, platformUserHash, sessionHash, conversationHash string) bool {
	if op == nil || op.InstallationID != strings.TrimSpace(installationID) {
		return false
	}
	return equalOptionalOperationBinding(op.PlatformUserHash, platformUserHash) &&
		equalOptionalOperationBinding(op.SessionHash, sessionHash) &&
		equalOptionalOperationBinding(op.ConversationHash, conversationHash)
}

func equalOptionalOperationBinding(expected, actual string) bool {
	expected = strings.TrimSpace(expected)
	actual = strings.TrimSpace(actual)
	if expected == "" {
		return actual == ""
	}
	return expected == actual
}

func operationNeedsExpiry(state string) bool {
	return state != AstrBotOperationStateSucceeded && state != AstrBotOperationStateCancelled && state != AstrBotOperationStateExpired
}

func randomAstrBotOperationID() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "op_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomAstrBotChallenge() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashAstrBotOperationSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func constantTimeStringEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var diff byte
	for i := range []byte(left) {
		diff |= left[i] ^ right[i]
	}
	return diff == 0
}

func truncateAstrBotOperationReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 128 {
		return value[:128]
	}
	if value == "" {
		return "mutation_failed"
	}
	return value
}
