package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ZenBrowserAuth accepts native browser sessions only for the consent action.
func ZenBrowserAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := authorizationToken(c.GetHeader("Authorization"))
		if !ok {
			writeZenControlAuthError(c, service.ErrAuthTokenInvalid)
			return
		}
		identity, internal, err := service.ParseDashboardAccessToken(raw)
		if err != nil {
			writeZenControlAuthError(c, err)
			return
		}
		if !internal {
			writeZenControlAuthError(c, service.ErrAuthCredentialScopeDenied)
			return
		}
		_, user, err := service.ValidateLoginSession(identity)
		if err != nil {
			writeZenControlAuthError(c, err)
			return
		}
		setDashboardAuthContext(c, user, identity, false)
		c.Next()
	}
}

// ZenControlAuth accepts only the exact client control routes, never native
// administration, dashboard PATs, relay keys or the Web cookie purpose.
func ZenControlAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		raw, ok := authorizationToken(c.GetHeader("Authorization"))
		if !ok {
			writeZenControlAuthError(c, service.ErrAuthTokenInvalid)
			return
		}
		identity, err := service.ParseZenAccessToken(raw)
		if err != nil {
			writeZenControlAuthError(c, err)
			return
		}
		path := c.Request.URL.Path
		allowed := false
		if c.FullPath() == path {
			switch c.Request.Method {
			case http.MethodGet:
				switch path {
				case "/api/xcode/v1/me", "/api/xcode/v1/plans", "/api/xcode/v1/entitlements",
					"/api/xcode/v1/models", "/api/xcode/v1/usage", "/api/xcode/v1/client-config":
					allowed = true
				}
			case http.MethodPost:
				allowed = path == "/api/xcode/v1/web-handoffs" || path == "/api/xcode/v1/auth/logout"
			}
		}
		if !allowed {
			writeZenControlAuthError(c, service.ErrAuthCredentialScopeDenied)
			return
		}
		_, user, err := service.ValidateZenLoginSession(identity)
		if err != nil {
			writeZenControlAuthError(c, err)
			return
		}
		setDashboardAuthContext(c, user, identity, false)
		common.SetContextKey(c, constant.ContextKeyClientPurpose, model.ClientPurposeZen)
		c.Next()
	}
}

func writeZenControlAuthError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE"
	switch {
	case errors.Is(err, service.ErrAuthCredentialScopeDenied):
		status, code = http.StatusForbidden, "XCODE_CREDENTIAL_SCOPE_DENIED"
	case errors.Is(err, service.ErrAuthTokenExpired):
		status, code = http.StatusUnauthorized, "XCODE_ACCESS_EXPIRED"
	case errors.Is(err, service.ErrAuthTokenInvalid), errors.Is(err, service.ErrLoginSessionInvalid),
		errors.Is(err, service.ErrLoginSessionRevoked), errors.Is(err, model.ErrUserSessionInvalid),
		errors.Is(err, model.ErrUserSessionInactive), errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusUnauthorized, "XCODE_SESSION_INVALID"
	}
	abortZenFacadeError(c, status, code)
}

func abortZenFacadeError(c *gin.Context, status int, code string) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.AbortWithStatusJSON(status, gin.H{
		"success": false, "code": code, "message": http.StatusText(status),
		"request_id": c.GetString(common.RequestIdKey),
	})
}

// validateTokenPurposeRoute runs as soon as a stored key is resolved, including
// before native token-state errors. Unsupported managed routes always fail closed.
func validateTokenPurposeRoute(c *gin.Context, token *model.Token) error {
	switch token.ClientPurpose {
	case "", model.ClientPurposeGeneral:
		if token.ClientSessionSID != "" {
			return abortZenAdmission(c, http.StatusForbidden, "XCODE_CREDENTIAL_SCOPE_DENIED", "Invalid credential purpose binding")
		}
	case model.ClientPurposeZen:
		path := c.Request.URL.Path
		if c.Request.Method != http.MethodPost || c.FullPath() != path ||
			(path != "/v1/messages" && path != "/v1/chat/completions" && path != "/v1/responses") {
			if _, loaded := common.GetContextKeyType[dto.UserSetting](c, constant.ContextKeyUserSetting); !loaded {
				common.SetContextKey(c, constant.ContextKeyUserSetting, dto.UserSetting{Language: model.GetUserLanguage(token.UserId)})
			}
			return abortZenAdmission(c, http.StatusForbidden, "XCODE_RELAY_SCOPE_DENIED", "This route is not available to aiscoo zen relay credentials")
		}
	default:
		return abortZenAdmission(c, http.StatusForbidden, "XCODE_CREDENTIAL_SCOPE_DENIED", "Invalid credential purpose")
	}
	return nil
}

// admitTokenPurpose is shared by relay and readonly key authentication. Purpose
// and funding come only from stored credentials and the designated relationship.
func admitTokenPurpose(c *gin.Context, token *model.Token) error {
	if err := validateTokenPurposeRoute(c, token); err != nil {
		return err
	}
	if token.ClientPurpose != model.ClientPurposeZen {
		common.SetContextKey(c, constant.ContextKeyClientPurpose, model.ClientPurposeGeneral)
		common.SetContextKey(c, constant.ContextKeyFundingScope, model.SubscriptionUsageScopeGeneral)
		common.SetContextKey(c, constant.ContextKeyFundingSubscriptionID, 0)
		common.SetContextKey(c, constant.ContextKeyFundingAuthorizationRevision, int64(0))
		return nil
	}

	now := time.Now().Unix()
	if token.ClientSessionSID == "" || token.Status != common.TokenStatusEnabled ||
		(token.ExpiredTime != -1 && token.ExpiredTime < now) || (!token.UnlimitedQuota && token.RemainQuota <= 0) {
		return abortZenAdmission(c, http.StatusUnauthorized, "XCODE_SESSION_INVALID", "aiscoo zen relay credential is no longer valid")
	}
	session, err := model.GetUserSessionCached(token.ClientSessionSID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrUserSessionInvalid) || errors.Is(err, model.ErrUserSessionInactive) {
			return abortZenAdmission(c, http.StatusUnauthorized, "XCODE_SESSION_INVALID", "aiscoo zen session is no longer valid")
		}
		return abortZenAdmission(c, http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE", "aiscoo zen authentication is temporarily unavailable")
	}
	authVersion := c.GetInt64(string(constant.ContextKeyUserAuthVersion))
	// SessionVersion rotates during an ordinary refresh; it does not revoke the key.
	if session.UserID != token.UserId || session.ClientPurpose != model.ClientPurposeZen ||
		session.Status != model.UserSessionStatusActive || session.RevokedAt != 0 || session.ExpiresAt <= now ||
		authVersion <= 0 || session.UserAuthVersion != authVersion ||
		common.GetContextKeyInt(c, constant.ContextKeyUserStatus) != common.UserStatusEnabled {
		return abortZenAdmission(c, http.StatusUnauthorized, "XCODE_SESSION_INVALID", "aiscoo zen session is no longer valid")
	}

	target, err := model.GetZenFundingTarget(token.UserId)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrZenPlanUnassigned):
			return abortZenAdmission(c, http.StatusForbidden, "XCODE_PLAN_UNASSIGNED", "No aiscoo zen plan is assigned")
		case errors.Is(err, model.ErrZenPlanExpired):
			return abortZenAdmission(c, http.StatusForbidden, "XCODE_PLAN_EXPIRED", "The aiscoo zen plan has expired")
		case errors.Is(err, model.ErrZenPlanRevoked):
			return abortZenAdmission(c, http.StatusForbidden, "XCODE_PLAN_REVOKED", "The aiscoo zen plan has been revoked")
		default:
			return abortZenAdmission(c, http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE", "aiscoo zen authorization is temporarily unavailable")
		}
	}
	common.SetContextKey(c, constant.ContextKeyClientPurpose, model.ClientPurposeZen)
	common.SetContextKey(c, constant.ContextKeyFundingScope, target.Scope)
	common.SetContextKey(c, constant.ContextKeyFundingSubscriptionID, target.SubscriptionID)
	common.SetContextKey(c, constant.ContextKeyFundingAuthorizationRevision, target.AuthorizationRevision)
	return nil
}

func abortZenAdmission(c *gin.Context, status int, code, message string) error {
	switch message {
	case "This route is not available to aiscoo zen relay credentials":
		message = i18n.T(c, i18n.MsgClientRelayScopeDenied)
	case "aiscoo zen relay credential is no longer valid":
		message = i18n.T(c, i18n.MsgClientCredentialInvalid)
	case "aiscoo zen session is no longer valid":
		message = i18n.T(c, i18n.MsgClientSessionInvalid)
	case "aiscoo zen authentication is temporarily unavailable":
		message = i18n.T(c, i18n.MsgClientAuthenticationUnavailable)
	case "aiscoo zen authorization is temporarily unavailable":
		message = i18n.T(c, i18n.MsgClientAuthorizationUnavailable)
	case "No aiscoo zen plan is assigned":
		message = i18n.T(c, i18n.MsgClientPlanUnassigned)
	case "The aiscoo zen plan has expired":
		message = i18n.T(c, i18n.MsgClientPlanExpired)
	case "The aiscoo zen plan has been revoked":
		message = i18n.T(c, i18n.MsgClientPlanRevoked)
	default:
		message = strings.ReplaceAll(strings.ReplaceAll(message, "XCode", common.DefaultSystemName+" client"), "XWork", common.DefaultSystemName+" client")
	}
	if c.Request.URL.Path == "/v1/messages" {
		errorType := "permission_error"
		if status == http.StatusUnauthorized {
			errorType = "authentication_error"
		} else if status >= http.StatusInternalServerError {
			errorType = "api_error"
		}
		c.AbortWithStatusJSON(status, gin.H{
			"type": "error",
			"error": gin.H{
				"type": errorType, "code": code,
				"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			},
		})
	} else {
		abortWithOpenAiMessage(c, status, message, types.ErrorCode(code))
	}
	return errors.New(code)
}
