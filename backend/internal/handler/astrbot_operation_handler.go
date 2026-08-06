package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type astrBotOperationBinding struct {
	Principal        middleware.AstrBotPrincipal
	InstallationID   string
	PlatformUserHash string
	SessionHash      string
	ConversationHash string
}

type astrBotOperationPrepareRequest struct {
	OperationType string          `json:"operation_type" binding:"required"`
	TargetID      *int64          `json:"target_id,omitempty"`
	Payload       json.RawMessage `json:"payload" binding:"required"`
	Diff          json.RawMessage `json:"diff,omitempty"`
}

type astrBotOperationConfirmAgainRequest struct {
	Challenge string `json:"challenge" binding:"required"`
}

type astrBotOperationExecuteRequest struct {
	OperationType string          `json:"operation_type" binding:"required"`
	TargetID      *int64          `json:"target_id,omitempty"`
	Payload       json.RawMessage `json:"payload" binding:"required"`
}

type astrBotOperationDTO struct {
	OperationID          string          `json:"operation_id"`
	OperationType        string          `json:"operation_type"`
	Method               string          `json:"method"`
	Route                string          `json:"route"`
	TargetID             *int64          `json:"target_id,omitempty"`
	Payload              json.RawMessage `json:"payload"`
	Diff                 json.RawMessage `json:"diff,omitempty"`
	CanonicalPayloadHash string          `json:"canonical_payload_hash"`
	State                string          `json:"state"`
	ConfirmationCount    int             `json:"confirmation_count"`
	ExpiresAt            time.Time       `json:"expires_at"`
	ChallengeExpiresAt   *time.Time      `json:"challenge_expires_at,omitempty"`
	CompletedAt          *time.Time      `json:"completed_at,omitempty"`
}

func toAstrBotOperationDTO(op *service.AstrBotOperation) (astrBotOperationDTO, error) {
	if op == nil {
		return astrBotOperationDTO{}, service.ErrAstrBotOperationNotFound
	}
	payload, err := service.DecodeAstrBotOperationPayload(op)
	if err != nil {
		return astrBotOperationDTO{}, err
	}
	dto := astrBotOperationDTO{
		OperationID: op.OperationID, OperationType: op.OperationType, Method: op.Method,
		Route: op.Route, TargetID: op.TargetID, Payload: payload,
		CanonicalPayloadHash: op.CanonicalPayloadHash, State: op.State,
		ConfirmationCount: op.ConfirmationCount, ExpiresAt: op.ExpiresAt,
		ChallengeExpiresAt: op.ChallengeExpiresAt, CompletedAt: op.CompletedAt,
	}
	if strings.TrimSpace(op.DiffJSON) != "" && op.DiffJSON != "{}" {
		dto.Diff = json.RawMessage(op.DiffJSON)
	}
	return dto, nil
}

func astrBotOperationBindingFromContext(c *gin.Context) (astrBotOperationBinding, error) {
	principal, ok := middleware.GetAstrBotPrincipal(c)
	if !ok || principal.UserID <= 0 || strings.TrimSpace(principal.TokenID) == "" {
		return astrBotOperationBinding{}, service.ErrAstrBotTokenInvalid
	}
	installationID := strings.TrimSpace(principal.InstallationID)
	if installationID == "" || len(installationID) > 128 {
		return astrBotOperationBinding{}, service.ErrAstrBotOperationPayload
	}
	return astrBotOperationBinding{
		Principal:        principal,
		InstallationID:   installationID,
		PlatformUserHash: hashAstrBotBindingHeader(principal.PlatformUser),
		SessionHash:      hashAstrBotBindingHeader(principal.SessionID),
		ConversationHash: hashAstrBotBindingHeader(principal.ConversationID),
	}, nil
}

func hashAstrBotBindingHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func astrBotOperationRequestKey(c *gin.Context) (string, error) {
	key, err := service.NormalizeIdempotencyKey(c.GetHeader("Idempotency-Key"))
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", service.ErrIdempotencyKeyRequired
	}
	return key, nil
}

func (h *AstrBotHandler) requireAstrBotOperations(c *gin.Context) bool {
	if h == nil || h.operations == nil {
		response.ErrorFrom(c, service.ErrAstrBotOperationStore)
		return false
	}
	return true
}

func (h *AstrBotHandler) PrepareOperation(c *gin.Context) {
	if !h.requireAstrBotOperations(c) {
		return
	}
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotOperationPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrAstrBotOperationPayload)
		return
	}
	if !requireAstrBotOperationResource(c, strings.TrimSpace(req.OperationType), req.TargetID) {
		return
	}
	operation, err := h.operations.Prepare(c.Request.Context(), service.AstrBotOperationPrepareInput{
		UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
		OperationType: strings.TrimSpace(req.OperationType), TargetID: req.TargetID,
		Payload: req.Payload, Diff: req.Diff, IdempotencyKeyHash: service.HashIdempotencyKey(key),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	dto, err := toAstrBotOperationDTO(operation)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto)
}

func (h *AstrBotHandler) ConfirmOperation(c *gin.Context) {
	if !h.requireAstrBotOperations(c) {
		return
	}
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	op, challenge, err := h.operations.Confirm(c.Request.Context(), service.AstrBotOperationConfirmInput{
		OperationID: c.Param("id"), UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
		KeyHash: service.HashIdempotencyKey(key),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	dto, err := toAstrBotOperationDTO(op)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"operation": dto, "challenge": challenge})
}

func (h *AstrBotHandler) ConfirmOperationAgain(c *gin.Context) {
	if !h.requireAstrBotOperations(c) {
		return
	}
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotOperationConfirmAgainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrAstrBotOperationChallenge)
		return
	}
	op, err := h.operations.ConfirmAgain(c.Request.Context(), service.AstrBotOperationConfirmAgainInput{
		OperationID: c.Param("id"), UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
		KeyHash: service.HashIdempotencyKey(key), Challenge: req.Challenge,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	dto, err := toAstrBotOperationDTO(op)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"operation": dto})
}

func (h *AstrBotHandler) ExecuteOperation(c *gin.Context) {
	if !h.requireAstrBotOperations(c) {
		return
	}
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	var req astrBotOperationExecuteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorFrom(c, service.ErrAstrBotOperationPayload)
		return
	}
	if !requireAstrBotOperationResource(c, strings.TrimSpace(req.OperationType), req.TargetID) {
		return
	}
	input := service.AstrBotOperationExecuteInput{
		OperationID: c.Param("id"), UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
		OperationType: strings.TrimSpace(req.OperationType), TargetID: req.TargetID,
		Payload: req.Payload, IdempotencyKeyHash: service.HashIdempotencyKey(key),
	}
	result, err := h.executeOperation(c, input, req.Payload)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}

func (h *AstrBotHandler) CancelOperation(c *gin.Context) {
	if !h.requireAstrBotOperations(c) {
		return
	}
	binding, err := astrBotOperationBindingFromContext(c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	op, err := h.operations.Cancel(c.Request.Context(), service.AstrBotOperationExecuteInput{
		OperationID: c.Param("id"), UserID: binding.Principal.UserID, TokenID: binding.Principal.TokenID,
		InstallationID: binding.InstallationID, PlatformUserHash: binding.PlatformUserHash,
		SessionHash: binding.SessionHash, ConversationHash: binding.ConversationHash,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	dto, err := toAstrBotOperationDTO(op)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"operation": dto, "cancelled": true})
}

func (h *AstrBotHandler) executeOperation(c *gin.Context, input service.AstrBotOperationExecuteInput, rawPayload json.RawMessage) (*service.IdempotencyExecuteResult, error) {
	if h.operations == nil {
		return nil, service.ErrAstrBotOperationStore
	}
	if h.idempotency == nil {
		return nil, service.ErrIdempotencyStoreUnavail
	}
	key, err := astrBotOperationRequestKey(c)
	if err != nil {
		return nil, err
	}
	principal := input.TokenID
	return h.idempotency.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope: "astrbot:operation:" + input.OperationID, ActorScope: "astrbot-token:" + principal,
		Method: c.Request.Method, Route: c.FullPath(), IdempotencyKey: key, Payload: rawPayload,
		TTL: service.DefaultWriteIdempotencyTTL(), RequireKey: true,
	}, func(ctx context.Context) (any, error) {
		op, claimed, claimErr := h.operations.Claim(ctx, input)
		if claimErr != nil {
			return nil, claimErr
		}
		if !claimed {
			return service.DecodeAstrBotOperationResponse(op)
		}
		data, mutationErr := h.mutateAstrBotOperation(ctx, op, rawPayload)
		if mutationErr != nil {
			_ = h.operations.MarkFailedRetryable(ctx, op.OperationID, mutationErr)
			return nil, mutationErr
		}
		if err := h.operations.MarkSucceeded(ctx, op.OperationID, data); err != nil {
			return nil, err
		}
		return data, nil
	})
}

func (h *AstrBotHandler) mutateAstrBotOperation(ctx context.Context, op *service.AstrBotOperation, rawPayload json.RawMessage) (any, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, service.ErrAstrBotOperationPayload
	}
	switch op.OperationType {
	case service.AstrBotOperationTypeChannelToggle:
		var req astrBotToggleRequest
		if err := json.Unmarshal(payload["enabled"], &req.Enabled); err != nil {
			return nil, service.ErrAstrBotOperationPayload
		}
		status := service.StatusDisabled
		if req.Enabled {
			status = service.StatusActive
		}
		channel, err := h.channels.Update(ctx, *op.TargetID, &service.UpdateChannelInput{Status: status})
		if err != nil {
			return nil, err
		}
		return gin.H{"channel": toAstrBotChannelDTO(channel), "enabled": req.Enabled}, nil
	case service.AstrBotOperationTypeAccountToggle:
		var req astrBotToggleRequest
		if err := json.Unmarshal(payload["enabled"], &req.Enabled); err != nil {
			return nil, service.ErrAstrBotOperationPayload
		}
		status := service.StatusDisabled
		if req.Enabled {
			status = service.StatusActive
		}
		account, err := h.admin.UpdateAccount(ctx, *op.TargetID, &service.UpdateAccountInput{Status: status})
		if err != nil {
			return nil, err
		}
		return gin.H{"account": toAstrBotAccountDTO(account), "enabled": req.Enabled}, nil
	case service.AstrBotOperationTypeAccountRateMultiplier:
		var rate float64
		if err := json.Unmarshal(payload["rate_multiplier"], &rate); err != nil || rate < 0 {
			return nil, service.ErrAstrBotOperationPayload
		}
		account, err := h.admin.UpdateAccount(ctx, *op.TargetID, &service.UpdateAccountInput{RateMultiplier: &rate})
		if err != nil {
			return nil, err
		}
		return gin.H{"account": toAstrBotAccountDTO(account)}, nil
	case service.AstrBotOperationTypeChannelPricing:
		var req astrBotPricingRequest
		if err := json.Unmarshal(payload["pricing"], &req.Pricing); err != nil {
			return nil, service.ErrAstrBotOperationPayload
		}
		pricing, err := req.toServicePricing()
		if err != nil {
			return nil, err
		}
		channel, err := h.channels.Update(ctx, *op.TargetID, &service.UpdateChannelInput{ModelPricing: &pricing})
		if err != nil {
			return nil, err
		}
		return gin.H{"channel": toAstrBotChannelDTO(channel)}, nil
	case service.AstrBotOperationTypeCacheRefresh:
		if err := h.channels.RefreshCache(ctx); err != nil {
			return nil, err
		}
		if err := h.pricing.ForceUpdate(); err != nil {
			return nil, err
		}
		return gin.H{"refreshed": true}, nil
	default:
		return nil, service.ErrAstrBotOperationType
	}
}
