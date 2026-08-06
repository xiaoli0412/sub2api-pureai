package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	AstrBotRequestTimestampHeader = "X-AstrBot-Timestamp"
	AstrBotRequestNonceHeader     = "X-AstrBot-Nonce"
	AstrBotRequestSignatureHeader = "X-AstrBot-Signature"
	AstrBotPlatformUserHeader     = "X-AstrBot-Platform-User"
	AstrBotSessionIDHeader        = "X-AstrBot-Session-ID"
	AstrBotConversationIDHeader   = "X-AstrBot-Conversation-ID"
	astrBotRequestBodyLimit       = 64 * 1024
)

type AstrBotRequestSignatureMiddleware gin.HandlerFunc

func NewAstrBotRequestSignatureMiddleware(tokens *service.AstrBotTokenService) AstrBotRequestSignatureMiddleware {
	return AstrBotRequestSignatureMiddleware(func(c *gin.Context) {
		if tokens == nil {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		value, ok := c.Get(AstrBotTokenKey)
		token, tokenOK := value.(*service.AstrBotToken)
		if !ok || !tokenOK || token == nil {
			abortAstrBotSignatureUnauthorized(c)
			return
		}

		body, err := readAndRestoreAstrBotBody(c)
		if err != nil {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		platformUser, valid := normalizeAstrBotBindingValue(c.GetHeader(AstrBotPlatformUserHeader))
		if !valid {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		sessionID, valid := normalizeAstrBotBindingValue(c.GetHeader(AstrBotSessionIDHeader))
		if !valid {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		conversationID, valid := normalizeAstrBotBindingValue(c.GetHeader(AstrBotConversationIDHeader))
		if !valid {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		path := c.Request.URL.RequestURI()
		if path == "" {
			path = c.Request.URL.Path
		}
		if err := tokens.VerifyRequestSignature(
			c.Request.Context(), token,
			c.GetHeader(AstrBotRequestTimestampHeader),
			c.GetHeader(AstrBotRequestNonceHeader),
			c.Request.Method,
			path,
			platformUser,
			sessionID,
			conversationID,
			string(body),
			c.GetHeader(AstrBotRequestSignatureHeader),
			time.Now().UTC(),
		); err != nil {
			abortAstrBotSignatureUnauthorized(c)
			return
		}

		principal, ok := GetAstrBotPrincipal(c)
		if !ok {
			abortAstrBotSignatureUnauthorized(c)
			return
		}
		principal.InstallationID = token.InstallationID
		principal.PlatformUser = platformUser
		principal.SessionID = sessionID
		principal.ConversationID = conversationID
		c.Set(AstrBotPrincipalKey, principal)
		c.Next()
	})
}

func readAndRestoreAstrBotBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		c.Request.Body = http.NoBody
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, astrBotRequestBodyLimit+1))
	if err != nil {
		return nil, err
	}
	_ = c.Request.Body.Close()
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > astrBotRequestBodyLimit {
		return nil, service.ErrAstrBotRequestSignature
	}
	return body, nil
}

func normalizeAstrBotBindingValue(value string) (string, bool) {
	value = strings.TrimSpace(value)
	return value, !strings.ContainsAny(value, "\r\n\x00") && len(value) <= 256
}

func abortAstrBotSignatureUnauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, map[string]any{
		"code": http.StatusUnauthorized, "message": "unauthorized", "reason": "ASTRBOT_REQUEST_SIGNATURE_INVALID",
	})
}
