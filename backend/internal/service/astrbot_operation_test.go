package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type astrBotOperationRepoStub struct {
	operation       *AstrBotOperation
	claimCalls      int
	claimResult     bool
	claimErr        error
	identity        *AstrBotOperation
	identityErr     error
	createErr       error
	confirmFirstErr error
}

func (r *astrBotOperationRepoStub) Create(_ context.Context, operation *AstrBotOperation) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.operation = operation
	return nil
}

func (r *astrBotOperationRepoStub) GetByID(_ context.Context, _, _ string, _ int64) (*AstrBotOperation, error) {
	if r.operation == nil {
		return nil, ErrAstrBotOperationNotFound
	}
	return r.operation, nil
}

func (r *astrBotOperationRepoStub) GetByIdentity(context.Context, string, string, *int64, string) (*AstrBotOperation, error) {
	if r.identityErr != nil {
		return nil, r.identityErr
	}
	if r.identity != nil {
		return r.identity, nil
	}
	return nil, ErrAstrBotOperationNotFound
}

func (r *astrBotOperationRepoStub) ConfirmFirst(context.Context, string, string, int64, string, string, string, time.Time, time.Time) (bool, error) {
	return r.confirmFirstErr == nil, r.confirmFirstErr
}

func (r *astrBotOperationRepoStub) ConfirmSecond(context.Context, string, string, int64, string, string, string, time.Time) (bool, error) {
	return true, nil
}

func (r *astrBotOperationRepoStub) ClaimExecution(_ context.Context, _, _ string, _ int64, _ string, _, _ time.Time) (bool, error) {
	r.claimCalls++
	return r.claimResult, r.claimErr
}

func (r *astrBotOperationRepoStub) MarkSucceeded(context.Context, string, int, string, time.Time) error {
	return nil
}

func (r *astrBotOperationRepoStub) MarkFailedRetryable(context.Context, string, string, time.Time) error {
	return nil
}

func (r *astrBotOperationRepoStub) Cancel(context.Context, string, string, int64, string, time.Time) (bool, error) {
	return true, nil
}

func (r *astrBotOperationRepoStub) Expire(context.Context, string, string, int64, time.Time) error {
	return nil
}

func operationFixture(t *testing.T, state string) (*AstrBotOperation, json.RawMessage, string) {
	t.Helper()
	targetID := int64(42)
	payload := json.RawMessage(`{"enabled":true}`)
	canonical, hash, err := NormalizeAstrBotOperationPayload(AstrBotOperationTypeChannelToggle, &targetID, payload)
	require.NoError(t, err)
	return &AstrBotOperation{
		OperationID:          "op_test",
		UserID:               7,
		TokenID:              "token_test",
		InstallationID:       "install_test",
		PlatformUserHash:     "platform_hash",
		SessionHash:          "session_hash",
		ConversationHash:     "conversation_hash",
		OperationType:        AstrBotOperationTypeChannelToggle,
		TargetID:             &targetID,
		PayloadJSON:          canonical,
		CanonicalPayloadHash: hash,
		IdempotencyKeyHash:   "key_hash",
		State:                state,
		ExpiresAt:            time.Now().UTC().Add(time.Minute),
		LockedUntil:          func() *time.Time { value := time.Now().UTC().Add(-time.Second); return &value }(),
	}, payload, hash
}

func operationClaimInput(payload json.RawMessage, targetID *int64) AstrBotOperationExecuteInput {
	return AstrBotOperationExecuteInput{
		OperationID: "op_test", UserID: 7, TokenID: "token_test", InstallationID: "install_test",
		PlatformUserHash: "platform_hash", SessionHash: "session_hash", ConversationHash: "conversation_hash",
		OperationType: AstrBotOperationTypeChannelToggle, TargetID: targetID, Payload: payload, IdempotencyKeyHash: "key_hash",
	}
}

func TestAstrBotOperationServiceClaimRejectsPayloadMismatchBeforeMutationClaim(t *testing.T) {
	op, _, _ := operationFixture(t, AstrBotOperationStateConfirmedTwice)
	repo := &astrBotOperationRepoStub{operation: op, claimResult: true}
	svc := NewAstrBotOperationService(repo)

	targetID := int64(42)
	_, claimed, err := svc.Claim(context.Background(), operationClaimInput(json.RawMessage(`{"enabled":false}`), &targetID))

	require.ErrorIs(t, err, ErrAstrBotOperationBinding)
	require.False(t, claimed)
	require.Zero(t, repo.claimCalls)
}

func TestAstrBotOperationServiceClaimReclaimsExpiredExecutionLease(t *testing.T) {
	op, payload, _ := operationFixture(t, AstrBotOperationStateExecuting)
	repo := &astrBotOperationRepoStub{operation: op, claimResult: true}
	svc := NewAstrBotOperationService(repo)

	targetID := int64(42)
	claimedOperation, claimed, err := svc.Claim(context.Background(), operationClaimInput(payload, &targetID))

	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, AstrBotOperationStateExecuting, claimedOperation.State)
	require.Equal(t, 1, repo.claimCalls)
}

func TestNormalizeAstrBotOperationPayloadRequiresExplicitMutationFields(t *testing.T) {
	targetID := int64(42)
	for name, payload := range map[string]string{
		"missing toggle field": `{}`,
		"unknown field":        `{"enabled":true,"status":"active"}`,
		"negative multiplier":  `{"rate_multiplier":-1}`,
		"disabled cache flag":  `{"refresh":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			operationType := AstrBotOperationTypeChannelToggle
			id := &targetID
			if name == "negative multiplier" {
				operationType = AstrBotOperationTypeAccountRateMultiplier
			} else if name == "disabled cache flag" {
				operationType = AstrBotOperationTypeCacheRefresh
				id = nil
			}
			_, _, err := NormalizeAstrBotOperationPayload(operationType, id, json.RawMessage(payload))
			require.ErrorIs(t, err, ErrAstrBotOperationPayload)
		})
	}
}
