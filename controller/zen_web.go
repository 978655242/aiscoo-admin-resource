package controller

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func zenWebTransport(c *gin.Context) (string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if !common.ZenIntegrationEnabled() {
		zenAPIError(c, http.StatusNotFound, "XCODE_AUTHORIZATION_DISABLED", "aiscoo zen authorization is disabled")
		return "", false
	}
	origin, err := service.ValidateZenWebRequest(c.Request)
	if errors.Is(err, service.ErrZenWebOriginForbidden) {
		zenAPIError(c, http.StatusForbidden, "XCODE_ORIGIN_FORBIDDEN", "Request origin is not permitted")
		return "", false
	}
	if err != nil {
		zenAuthenticationError(c, err)
		return "", false
	}
	return origin, true
}

func CreateZenWebHandoff(c *gin.Context) {
	if _, ok := zenWebTransport(c); !ok {
		return
	}
	identity, ok := middleware.GetAuthIdentity(c)
	if !ok {
		zenAuthenticationError(c, service.ErrLoginSessionInvalid)
		return
	}
	response, err := service.CreateZenWebHandoff(identity)
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ConsumeZenWebHandoff(c *gin.Context) {
	origin, ok := zenWebTransport(c)
	if !ok {
		return
	}
	c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
	c.Header("X-Frame-Options", "DENY")
	if c.Request.Method != http.MethodGet || c.Request.URL.Path != "/xcode/plans" {
		zenAuthenticationError(c, service.ErrAuthCredentialScopeDenied)
		return
	}
	if c.Request.URL.RawQuery == "" {
		return
	}
	handoff, ok := strings.CutPrefix(c.Request.URL.RawQuery, "handoff=")
	if !ok || len(handoff) != 43 {
		zenAuthenticationError(c, service.ErrZenAuthCodeInvalid)
		return
	}
	webToken, expiresAt, err := service.ConsumeZenWebHandoff(handoff)
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	maxAge := int(time.Until(time.Unix(expiresAt, 0)) / time.Second)
	if maxAge <= 0 {
		zenAuthenticationError(c, service.ErrLoginSessionRevoked)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: service.ZenWebCookieName(origin), Value: webToken, Path: "/api/xcode/v1/web",
		HttpOnly: true, Secure: strings.HasPrefix(origin, "https://"), SameSite: http.SameSiteStrictMode,
		MaxAge: maxAge, Expires: time.Unix(expiresAt, 0),
	})
	c.Redirect(http.StatusSeeOther, "/xcode/plans")
}

func GetZenWebView(c *gin.Context) {
	view, err := service.GetZenWebView(c.GetInt("id"))
	if err != nil {
		zenAuthenticationError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
