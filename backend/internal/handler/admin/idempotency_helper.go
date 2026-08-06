package admin

import (
	"context"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type idempotencyStoreUnavailableMode int

const (
	idempotencyStoreUnavailableFailClose idempotencyStoreUnavailableMode = iota
	idempotencyStoreUnavailableFailOpen
)

func executeAdminIdempotent(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) (*service.IdempotencyExecuteResult, error) {
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		data, err := execute(c.Request.Context())
		if err != nil {
			return nil, err
		}
		return &service.IdempotencyExecuteResult{Data: data}, nil
	}

	return coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope:          scope,
		ActorScope:     adminActorScope(c),
		Method:         c.Request.Method,
		Route:          c.FullPath(),
		IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Payload:        payload,
		RequireKey:     true,
		TTL:            ttl,
	}, execute)
}

func adminActorScope(c *gin.Context) string {
	actorScope := "admin:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "admin:" + strconv.FormatInt(subject.UserID, 10)
	}
	return actorScope
}

// buildInt64PointerUpdateParam 把 update 请求里的 (*int64 + clear flag) 转成 service 层的 **int64 三态。
//   - value == nil && clear == false：返回 nil（不更新）
//   - clear == true：返回 &nil（清空）
//   - value != nil：返回指向 value 的 **int64（覆盖）
func buildInt64PointerUpdateParam(value *int64, clear bool) **int64 {
	if clear {
		var nilPtr *int64
		return &nilPtr
	}
	if value == nil {
		return nil
	}
	v := *value
	inner := &v
	return &inner
}

func executeAdminIdempotentJSON(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithMode(c, scope, payload, ttl, idempotencyStoreUnavailableFailClose, execute)
}

func executeAdminIdempotentJSONFailOpenOnStoreUnavailable(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	execute func(context.Context) (any, error),
) {
	executeAdminIdempotentJSONWithMode(c, scope, payload, ttl, idempotencyStoreUnavailableFailOpen, execute)
}

func executeAdminIdempotentJSONWithMode(
	c *gin.Context,
	scope string,
	payload any,
	ttl time.Duration,
	mode idempotencyStoreUnavailableMode,
	execute func(context.Context) (any, error),
) {
	result, err := executeAdminIdempotent(c, scope, payload, ttl, execute)
	if err != nil {
		if infraerrors.Code(err) == infraerrors.Code(service.ErrIdempotencyStoreUnavail) {
			strategy := "fail_close"
			if mode == idempotencyStoreUnavailableFailOpen {
				strategy = "fail_open"
			}
			service.RecordIdempotencyStoreUnavailable(c.FullPath(), scope, "handler_"+strategy)
			logger.LegacyPrintf("handler.idempotency", "[Idempotency] store unavailable: method=%s route=%s scope=%s strategy=%s", c.Request.Method, c.FullPath(), scope, strategy)
			if mode == idempotencyStoreUnavailableFailOpen {
				data, fallbackErr := execute(c.Request.Context())
				if fallbackErr != nil {
					response.ErrorFrom(c, fallbackErr)
					return
				}
				c.Header("X-Idempotency-Degraded", "store-unavailable")
				response.Success(c, data)
				return
			}
		}
		if retryAfter := service.RetryAfterSecondsFromError(err); retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		response.ErrorFrom(c, err)
		return
	}
	if result != nil && result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	response.Success(c, result.Data)
}
