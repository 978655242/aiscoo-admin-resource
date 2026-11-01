package controller

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func zenAPIError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{
		"success": false, "code": code, "message": strings.ReplaceAll(strings.ReplaceAll(message, "XCode", common.DefaultSystemName+" client"), "XWork", common.DefaultSystemName+" client"),
		"request_id": c.GetString(common.RequestIdKey),
	})
}

func zenReadError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrZenUsageRequestInvalid) {
		zenAPIError(c, http.StatusBadRequest, "XCODE_USAGE_REQUEST_INVALID", "Invalid usage date range or time zone")
		return
	}
	logger.LogError(c, "aiscoo zen read unavailable: "+common.LocalLogPreview(err.Error()))
	if errors.Is(err, service.ErrZenUsageUnavailable) {
		zenAPIError(c, http.StatusServiceUnavailable, "XCODE_USAGE_UNAVAILABLE", "Recorded account usage is temporarily unavailable")
		return
	}
	zenAPIError(c, http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE", "Account data is temporarily unavailable")
}

func GetZenMe(c *gin.Context) {
	snapshot, err := service.GetZenMe(c.GetInt("id"), c.GetString("session_id"))
	if err != nil {
		zenReadError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func GetZenPlans(c *gin.Context) {
	snapshot, err := service.GetZenPlans()
	if err != nil {
		zenReadError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func GetZenEntitlements(c *gin.Context) {
	snapshot, err := service.GetZenEntitlements(c.GetInt("id"))
	if err != nil {
		zenReadError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func GetZenUsage(c *gin.Context) {
	snapshot, err := service.GetZenAccountUsage(c.GetInt("id"), dto.ZenUsageQuery{
		Range: c.Query("range"), TimeZone: c.Query("time_zone"),
		StartDate: c.Query("start_date"), EndDate: c.Query("end_date"),
	}, time.Now())
	if err != nil {
		zenReadError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func GetZenClientConfig(c *gin.Context) {
	snapshot, err := service.GetZenClientConfig()
	if err != nil {
		zenReadError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func GetZenModels(c *gin.Context) {
	snapshot, err := service.GetZenModelCatalog(c.GetInt("id"))
	if err != nil {
		logger.LogError(c, "aiscoo zen model catalog unavailable: "+err.Error())
		zenAPIError(c, http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE", "Model catalog is temporarily unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, snapshot)
}

func zenAuthorizationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrZenAuthorizationConflict), errors.Is(err, model.ErrSubscriptionRequestConflict):
		zenAPIError(c, http.StatusConflict, "XCODE_AUTHORIZATION_CONFLICT", "Authorization changed or this action is not valid for its current state")
	case errors.Is(err, model.ErrZenPlanConfigInvalid):
		zenAPIError(c, http.StatusBadRequest, "XCODE_PLAN_CONFIG_INVALID", "The enabled plan must allow unlimited historical purchases and have a valid duration and quota configuration")
	case errors.Is(err, model.ErrZenPaymentComplianceRequired):
		zenAPIError(c, http.StatusForbidden, "XCODE_PLAN_CONFIG_INVALID", "An administrator must confirm the existing payment compliance declaration first")
	default:
		logger.LogError(c, "aiscoo zen authorization unavailable: "+err.Error())
		zenAPIError(c, http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE", "Authorization is temporarily unavailable")
	}
}

func AdminGetZenAuthorization(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "Invalid target user ID")
		return
	}
	state, err := model.GetZenAuthorizationState(userID)
	if err != nil {
		zenAuthorizationError(c, err)
		return
	}
	snapshot, err := service.ZenAuthorizationSnapshot(state)
	if err != nil {
		zenAuthorizationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, snapshot)
}

func AdminMutateZenAuthorization(c *gin.Context) {
	userID, err := strconv.Atoi(c.Param("id"))
	if err != nil || userID <= 0 {
		zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "Invalid target user ID")
		return
	}
	var input dto.ZenAuthorizationMutation
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "Invalid authorization request")
		return
	}
	revision, err := strconv.ParseInt(input.ExpectedRevision, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != input.ExpectedRevision {
		zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "expected_revision must be a nonnegative decimal string")
		return
	}
	planID := 0
	switch input.Action {
	case "grant", "renew", "replace":
		planID, err = strconv.Atoi(input.PlanID)
		if err != nil || planID <= 0 || strconv.Itoa(planID) != input.PlanID || input.AdvanceResetTime != nil {
			zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "This action requires a valid plan_id decimal string")
			return
		}
	case "reset":
		if input.AdvanceResetTime == nil || input.PlanID != "" {
			zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "reset requires an explicit advance_reset_time boolean and no plan_id")
			return
		}
	case "revoke":
		if input.PlanID != "" || input.AdvanceResetTime != nil {
			zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "revoke does not accept plan or reset parameters")
			return
		}
	default:
		zenAPIError(c, http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID", "Unknown authorization action")
		return
	}
	advance := input.AdvanceResetTime != nil && *input.AdvanceResetTime
	state, mutationErr := model.MutateZenAuthorization(userID, input.Action, revision, planID, advance)
	if state != nil && state.Audit != nil {
		params := map[string]any{
			"action": input.Action, "old_subscription_id": state.Audit.OldSubscriptionID,
			"new_subscription_id":    state.Authorization.SubscriptionId,
			"authorization_revision": strconv.FormatInt(state.Authorization.Revision, 10),
			"old_end_time":           state.Audit.OldEndTime, "new_end_time": state.Subscription.EndTime,
			"old_last_reset_time": state.Audit.OldLastResetTime, "new_last_reset_time": state.Subscription.LastResetTime,
			"old_next_reset_time": state.Audit.OldNextResetTime, "new_next_reset_time": state.Subscription.NextResetTime,
			"old_reset_generation": state.Audit.OldResetGeneration, "new_reset_generation": state.Subscription.QuotaResetGeneration,
			"advance_reset_time": advance,
		}
		recordManageAuditFor(c, userID, "xcode.authorization."+input.Action, params)
	}
	if mutationErr != nil {
		zenAuthorizationError(c, mutationErr)
		return
	}
	snapshot, err := service.ZenAuthorizationSnapshot(state)
	if err != nil {
		zenAuthorizationError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, snapshot)
}

func zenAuthenticationError(c *gin.Context, err error) {
	status, code := http.StatusServiceUnavailable, "XCODE_BACKEND_UNAVAILABLE"
	switch {
	case errors.Is(err, service.ErrZenAuthRequestInvalid):
		status, code = http.StatusBadRequest, "XCODE_AUTH_REQUEST_INVALID"
	case errors.Is(err, service.ErrZenAuthCodeInvalid):
		status, code = http.StatusBadRequest, "XCODE_AUTH_CODE_INVALID"
	case errors.Is(err, model.ErrUserTokenLimit):
		status, code = http.StatusConflict, "XCODE_TOKEN_LIMIT"
	case errors.Is(err, service.ErrAuthCredentialScopeDenied):
		status, code = http.StatusForbidden, "XCODE_CREDENTIAL_SCOPE_DENIED"
	case errors.Is(err, service.ErrAuthTokenExpired):
		status, code = http.StatusUnauthorized, "XCODE_ACCESS_EXPIRED"
	case errors.Is(err, service.ErrRefreshTokenInvalid), errors.Is(err, service.ErrAuthTokenInvalid),
		errors.Is(err, service.ErrLoginSessionInvalid), errors.Is(err, service.ErrLoginSessionRevoked),
		errors.Is(err, model.ErrUserSessionInvalid), errors.Is(err, model.ErrUserSessionInactive):
		status, code = http.StatusUnauthorized, "XCODE_SESSION_INVALID"
	case errors.Is(err, model.ErrUserSessionLimit), errors.Is(err, model.ErrUserSessionIssuanceLimit),
		errors.Is(err, service.ErrLoginSessionMismatch), errors.Is(err, service.ErrRefreshRace):
		status, code = service.AuthSessionErrorCode(err)
	}
	zenAPIError(c, status, code, http.StatusText(status))
}

func zenAuthorizationOrigin(c *gin.Context) (string, bool) {
	if !common.ZenIntegrationEnabled() {
		zenAPIError(c, http.StatusNotFound, "XCODE_AUTHORIZATION_DISABLED", "aiscoo zen authorization is disabled")
		return "", false
	}
	origin, err := common.GetZenPublicOrigin()
	if err != nil {
		zenAuthenticationError(c, err)
		return "", false
	}
	return origin, true
}

func StartZenAuthorization(c *gin.Context) {
	origin, ok := zenAuthorizationOrigin(c)
	if !ok {
		return
	}
	query := c.Request.URL.Query()
	for _, key := range []string{"client_id", "response_type", "redirect_uri", "state", "code_challenge", "code_challenge_method"} {
		if len(query[key]) != 1 {
			zenAuthenticationError(c, service.ErrZenAuthRequestInvalid)
			return
		}
	}
	flowToken, err := service.StartZenAuthorization(service.ZenAuthorizeRequest{
		ClientID: query.Get("client_id"), ResponseType: query.Get("response_type"),
		RedirectURI: query.Get("redirect_uri"), State: query.Get("state"),
		CodeChallenge: query.Get("code_challenge"), CodeChallengeMethod: query.Get("code_challenge_method"),
	})
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	c.Redirect(http.StatusFound, origin+"/xcode/authorize?"+url.Values{"flow_token": {flowToken}}.Encode())
}

func GetZenAuthorizationRequest(c *gin.Context) {
	if _, ok := zenAuthorizationOrigin(c); !ok {
		return
	}
	view, err := service.GetZenAuthorizationRequest(c.Query("flow_token"))
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	common.ApiSuccess(c, dto.ZenAuthorizationRequestView{
		SchemaVersion: 1, ServerTime: time.Now().UnixMilli(), ClientID: view.ClientID, ExpiresAt: view.ExpiresAt * 1000,
	})
}

func CompleteZenAuthorization(c *gin.Context) {
	if _, ok := zenAuthorizationOrigin(c); !ok {
		return
	}
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		zenAuthenticationError(c, service.ErrAuthCredentialScopeDenied)
		return
	}
	var input struct {
		FlowToken string `json:"flow_token"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		zenAuthenticationError(c, service.ErrZenAuthRequestInvalid)
		return
	}
	redirectURL, err := service.CompleteZenAuthorization(identity, input.FlowToken)
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	common.ApiSuccess(c, dto.ZenAuthorizationCompleteResponse{
		SchemaVersion: 1, ServerTime: time.Now().UnixMilli(), RedirectURL: redirectURL,
	})
}

func ExchangeZenAuthorizationCode(c *gin.Context) {
	if _, ok := zenAuthorizationOrigin(c); !ok {
		return
	}
	var input service.ZenTokenRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		zenAuthenticationError(c, service.ErrZenAuthCodeInvalid)
		return
	}
	bundle, err := service.ExchangeZenAuthorizationCode(input, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	writeZenSessionBundle(c, bundle)
}

func RefreshZenSession(c *gin.Context) {
	var input service.ZenRefreshRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		zenAuthenticationError(c, service.ErrRefreshTokenInvalid)
		return
	}
	bundle, err := service.RefreshZenSession(input, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	writeZenSessionBundle(c, bundle)
}

func LogoutZenSession(c *gin.Context) {
	var input service.ZenLogoutRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		zenAuthenticationError(c, service.ErrLoginSessionInvalid)
		return
	}
	var identity *service.AuthIdentity
	if header := strings.TrimSpace(c.GetHeader("Authorization")); header != "" {
		scheme, raw, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			zenAuthenticationError(c, service.ErrAuthTokenInvalid)
			return
		}
		parsed, err := service.ParseZenAccessToken(strings.TrimSpace(raw))
		if err != nil {
			zenAuthenticationError(c, err)
			return
		}
		identity = &parsed
	}
	if err := service.LogoutZenSession(identity, input); err != nil {
		zenAuthenticationError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"schema_version": 1, "server_time": time.Now().UnixMilli(), "revoked": true})
}

func writeZenSessionBundle(c *gin.Context, bundle *service.ZenSessionBundle) {
	if bundle == nil || bundle.User == nil || bundle.Session == nil || bundle.Token == nil {
		zenAuthenticationError(c, service.ErrZenBackendUnavailable)
		return
	}
	for _, deadline := range [3]int64{bundle.AccessExpiresAt, bundle.RefreshExpiresAt, bundle.Token.ExpiredTime} {
		if deadline <= 0 || deadline > 9007199254740991/1000 {
			zenAuthenticationError(c, service.ErrZenBackendUnavailable)
			return
		}
	}
	common.ApiSuccess(c, dto.ZenSessionBundle{
		SchemaVersion: 1, ServerTime: time.Now().UnixMilli(), AccessToken: bundle.AccessToken,
		AccessExpiresAt: bundle.AccessExpiresAt * 1000, RefreshToken: bundle.RefreshToken,
		RefreshExpiresAt: bundle.RefreshExpiresAt * 1000, TokenType: "Bearer", SessionID: bundle.Session.SID,
		User:            dto.ZenUser{ID: strconv.Itoa(bundle.User.Id), Username: bundle.User.Username, DisplayName: bundle.User.DisplayName},
		RelayCredential: dto.ZenRelayCredential{Token: "sk-" + bundle.Token.GetFullKey(), ExpiresAt: bundle.Token.ExpiredTime * 1000},
	})
}
