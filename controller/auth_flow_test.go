package controller

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/fxamacker/cbor/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newSecurityLoginPasskey(t *testing.T, userID int) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	credentialID := sha256.Sum256(elliptic.Marshal(key.Curve, key.X, key.Y))
	publicKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.PasskeyCredential{
		UserID: userID, CredentialID: base64.StdEncoding.EncodeToString(credentialID[:]), PublicKey: base64.StdEncoding.EncodeToString(publicKey),
		UserPresent: true, UserVerified: true,
	}).Error)
	return key
}

func beginSecurityLoginPasskey(t *testing.T, parentToken string) (string, string) {
	t.Helper()
	body, err := common.Marshal(map[string]string{"flow_token": parentToken})
	require.NoError(t, err)
	response := securityEnrollmentRequest("POST", "/api/user/login/passkey/begin", string(body), "", service.AuthIdentity{}, LoginPasskeyBegin)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			FlowToken string `json:"flow_token"`
			Options   struct {
				PublicKey struct {
					Challenge        string `json:"challenge"`
					UserVerification string `json:"userVerification"`
				} `json:"publicKey"`
			} `json:"options"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	require.Equal(t, "required", result.Data.Options.PublicKey.UserVerification)
	return result.Data.FlowToken, result.Data.Options.PublicKey.Challenge
}

func TestNativeErrorDisplayUsesAiscooAndKeepsStableErrorCode(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/xcode/web/view", nil)
	xcodeAPIError(ctx, http.StatusForbidden, "XCODE_PLAN_EXPIRED", "XCode plan is expired")
	var payload map[string]any
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.Equal(t, "XCODE_PLAN_EXPIRED", payload["code"])
	assert.Equal(t, "aiscoo client plan is expired", payload["message"])
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestSecurityLoginCodeCompletesOnce(t *testing.T) {
	for _, test := range []struct {
		path   string
		backup bool
	}{{"/api/user/login/verify", false}, {"/api/user/login/2fa", false}, {"/api/user/login/verify", true}} {
		t.Run(fmt.Sprintf("%s/backup=%t", test.path, test.backup), func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			factor := &model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}
			require.NoError(t, model.DB.Create(factor).Error)
			challenge, err := service.StartLoginVerification(user, "password", nil)
			require.NoError(t, err)
			require.NotNil(t, challenge)
			code, err := totp.GenerateCode(factor.Secret, time.Now())
			require.NoError(t, err)
			if test.backup {
				code = "ABCD-1234"
				hash, err := common.HashBackupCode(code)
				require.NoError(t, err)
				require.NoError(t, model.DB.Create(&model.TwoFABackupCode{UserId: user.Id, CodeHash: hash}).Error)
			}
			body, err := common.Marshal(map[string]string{"flow_token": challenge.FlowToken, "code": code})
			require.NoError(t, err)
			router := gin.New()
			router.POST("/api/user/login/verify", VerifyLogin)
			router.POST("/api/user/login/2fa", Verify2FALogin)
			for attempt := range 2 {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("POST", test.path, strings.NewReader(string(body))))
				var result securityEnrollmentResponse
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.Equal(t, attempt == 0, result.Success, response.Body.String())
				if attempt == 0 {
					var bundle service.AuthBundle
					require.NoError(t, common.Unmarshal(result.Data, &bundle))
					assert.NotEmpty(t, bundle.AccessToken)
					assert.Equal(t, "password", bundle.Session.LoginMethod)
					assert.NotEmpty(t, response.Header().Values("Set-Cookie"))
				} else {
					assert.Empty(t, response.Header().Values("Set-Cookie"))
				}
			}
			count, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
			require.NoError(t, err)
			assert.EqualValues(t, 2, count)
		})
	}
}

func TestSecurityLoginRejectsChangedOrExpiredAuthorization(t *testing.T) {
	for _, change := range []string{"expired", "disabled user", "auth version", "factor removed", "factor locked", "legacy flow", "wrong method", "other purpose"} {
		t.Run(change, func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			factor := &model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}
			require.NoError(t, model.DB.Create(factor).Error)
			challenge, err := service.StartLoginVerification(user, "password", nil)
			require.NoError(t, err)
			method := "2fa"
			switch change {
			case "expired":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeLoginVerification).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			case "disabled user":
				require.NoError(t, model.DB.Model(user).Update("status", common.UserStatusDisabled).Error)
			case "auth version":
				require.NoError(t, model.DB.Model(user).Update("auth_version", user.AuthVersion+1).Error)
			case "factor removed":
				require.NoError(t, model.DB.Delete(factor).Error)
			case "factor locked":
				require.NoError(t, model.DB.Model(factor).Update("locked_until", time.Now().Add(time.Minute)).Error)
			case "legacy flow":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeLoginVerification).Update("purpose", model.AuthFlowPurposeTwoFALogin).Error)
			case "wrong method":
				method = "password"
			case "other purpose":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeLoginVerification).Update("purpose", model.AuthFlowPurposeSecurityProof).Error)
			}
			code, err := totp.GenerateCode(factor.Secret, time.Now())
			require.NoError(t, err)
			body, err := common.Marshal(map[string]string{"flow_token": challenge.FlowToken, "code": code, "method": method})
			require.NoError(t, err)
			response := securityEnrollmentRequest("POST", "/api/user/login/verify", string(body), "", service.AuthIdentity{}, VerifyLogin)
			var result securityEnrollmentResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success, change)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			count, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
			require.NoError(t, err)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestSecurityLoginPasskeyDoesNotRequireAdditionalTwoFA(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(fmt.Sprintf("direct=%t", direct), func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			key := newSecurityLoginPasskey(t, user.Id)
			require.NoError(t, model.DB.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
			for _, verified := range []bool{false, true} {
				var flowToken, challenge, parentToken string
				if direct {
					response := securityEnrollmentRequest("POST", "/api/user/passkey/login/begin", "", "", service.AuthIdentity{}, PasskeyLoginBegin)
					var result struct {
						Data struct {
							FlowToken string `json:"flow_token"`
							Options   struct {
								PublicKey struct {
									Challenge        string `json:"challenge"`
									UserVerification string `json:"userVerification"`
								} `json:"publicKey"`
							} `json:"options"`
						} `json:"data"`
					}
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					require.Equal(t, "required", result.Data.Options.PublicKey.UserVerification)
					flowToken, challenge = result.Data.FlowToken, result.Data.Options.PublicKey.Challenge
				} else {
					pending, err := service.StartLoginVerification(user, "oauth:github", nil)
					require.NoError(t, err)
					parentToken = pending.FlowToken
					flowToken, challenge = beginSecurityLoginPasskey(t, parentToken)
				}
				var assertion map[string]any
				require.NoError(t, common.Unmarshal(securityPasskeyResponse(t, key, challenge, false, 0, verified), &assertion))
				if direct {
					response, ok := assertion["response"].(map[string]any)
					require.True(t, ok)
					response["userHandle"] = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprint(user.Id)))
				}
				payload := map[string]any{"flow_token": flowToken, "credential": assertion}
				handler, path := PasskeyLoginFinish, "/api/user/passkey/login/finish"
				if !direct {
					payload["flow_token"], payload["passkey_flow_token"] = parentToken, flowToken
					handler, path = LoginPasskeyFinish, "/api/user/login/passkey/finish"
				}
				body, err := common.Marshal(payload)
				require.NoError(t, err)
				router := gin.New()
				router.POST(path, handler)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("POST", path, strings.NewReader(string(body))))
				var result securityEnrollmentResponse
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				require.Equal(t, verified, result.Success, response.Body.String())
				if verified {
					var bundle service.AuthBundle
					require.NoError(t, common.Unmarshal(result.Data, &bundle))
					assert.NotEmpty(t, bundle.AccessToken)
				} else {
					assert.Empty(t, response.Header().Values("Set-Cookie"))
				}
			}
		})
	}
}

func TestSecurityLoginPasskeyConcurrentCompletionCreatesOneSession(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	key := newSecurityLoginPasskey(t, user.Id)
	pending, err := service.StartLoginVerification(user, "password", nil)
	require.NoError(t, err)
	requests := make([]string, 2)
	for index := range requests {
		token, challenge := beginSecurityLoginPasskey(t, pending.FlowToken)
		body, err := common.Marshal(map[string]any{
			"flow_token": pending.FlowToken, "passkey_flow_token": token,
			"credential": securityPasskeyResponse(t, key, challenge, false, 0),
		})
		require.NoError(t, err)
		requests[index] = string(body)
	}
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var workers sync.WaitGroup
	for _, body := range requests {
		workers.Go(func() {
			<-start
			responses <- securityEnrollmentRequest("POST", "/api/user/login/passkey/finish", body, "", service.AuthIdentity{}, LoginPasskeyFinish)
		})
	}
	close(start)
	workers.Wait()
	close(responses)
	successes := 0
	for response := range responses {
		var result securityEnrollmentResponse
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		if result.Success {
			successes++
		}
	}
	assert.Equal(t, 1, successes)
	count, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestSecurityLoginSessionFailureRollsBackChallengeConsumption(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	key := newSecurityLoginPasskey(t, user.Id)
	pending, err := service.StartLoginVerification(user, "password", nil)
	require.NoError(t, err)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("login_session_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_sessions" {
			tx.AddError(errors.New("private session creation failure"))
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("login_session_failure") })
	for _, fail := range []bool{true, false} {
		token, challenge := beginSecurityLoginPasskey(t, pending.FlowToken)
		body, err := common.Marshal(map[string]any{"flow_token": pending.FlowToken, "passkey_flow_token": token, "credential": securityPasskeyResponse(t, key, challenge, false, 0)})
		require.NoError(t, err)
		response := securityEnrollmentRequest("POST", "/api/user/login/passkey/finish", string(body), "", service.AuthIdentity{}, LoginPasskeyFinish)
		var result securityEnrollmentResponse
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
		assert.Equal(t, !fail, result.Success, response.Body.String())
		if fail {
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			assert.NotContains(t, response.Body.String(), "private")
			_, err = model.GetAuthFlow(pending.FlowToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeLoginVerification})
			require.NoError(t, err, "a failed session transaction must leave the parent challenge usable")
			require.NoError(t, model.DB.Callback().Create().Remove("login_session_failure"))
		}
	}
	count, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
}

func TestSecurityLoginFactorStateDoesNotAddPasswordLoginQueries(t *testing.T) {
	user, _ := setupSecurityEnrollmentTest(t)
	newSecurityLoginPasskey(t, user.Id)
	previousPasswordLogin := common.PasswordLoginEnabled
	common.PasswordLoginEnabled = true
	t.Cleanup(func() { common.PasswordLoginEnabled = previousPasswordLogin })
	queries := 0
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("login_query_count", func(tx *gorm.DB) {
		if !tx.DryRun {
			queries++
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("login_query_count") })
	state, err := model.GetUserVerificationState(user.Id)
	require.NoError(t, err)
	assert.True(t, state.HasPassword)
	assert.True(t, state.HasPasskey)
	assert.False(t, state.HasTwoFA)
	assert.Equal(t, 1, queries, "factor availability must be one database round trip")
	queries = 0
	response := securityEnrollmentRequest("POST", "/api/user/login", `{"username":"enrollment-user","password":"enrollment-password"}`, "", service.AuthIdentity{}, Login)
	assert.Contains(t, response.Body.String(), `"require_verification":true`)
	assert.Equal(t, 2, queries, "only the existing credential lookup and the replacement factor-state lookup run before the challenge")
}

type boundLoginOAuthProvider struct {
	authFlowTestOAuthProvider
	userID int
}

func (*boundLoginOAuthProvider) IsUserIDTaken(string) bool    { return true }
func (*boundLoginOAuthProvider) ProviderUserIDColumn() string { return "github_id" }
func (provider *boundLoginOAuthProvider) FillUserByProviderID(user *model.User, _ string) error {
	return model.DB.First(user, provider.userID).Error
}

func TestSecurityLoginAllPrimaryTransportsRequireAdditionalVerification(t *testing.T) {
	for _, transport := range []string{"oauth", "custom oauth", "wechat", "telegram"} {
		t.Run(transport, func(t *testing.T) {
			var user *model.User
			var telegram *telegramOAuthFixture
			if transport == "telegram" {
				telegram = setupTelegramOAuthTest(t)
				user = telegram.user
			} else {
				user, _ = setupSecurityEnrollmentTest(t)
			}
			newSecurityLoginPasskey(t, user.Id)
			var response *httptest.ResponseRecorder
			switch transport {
			case "telegram":
				require.NoError(t, model.DB.Model(user).Update("telegram_id", "42").Error)
				state, code := telegram.authorization(t, "login", service.AuthIdentity{}, "", telegramIdentityClaims(42))
				response = telegramOAuthCallback(state, code, service.AuthIdentity{})
			case "wechat":
				require.NoError(t, model.DB.Model(user).Update("wechat_id", "bound-wechat").Error)
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					assert.Equal(t, "wechat-code", r.URL.Query().Get("code"))
					_, _ = w.Write([]byte(`{"success":true,"data":"bound-wechat"}`))
				}))
				t.Cleanup(upstream.Close)
				previousEnabled, previousAddress := common.WeChatAuthEnabled, common.WeChatServerAddress
				common.WeChatAuthEnabled, common.WeChatServerAddress = true, upstream.URL
				t.Cleanup(func() { common.WeChatAuthEnabled, common.WeChatServerAddress = previousEnabled, previousAddress })
				response = securityEnrollmentRequest("GET", "/api/oauth/wechat?code=wechat-code", "", "", service.AuthIdentity{}, WeChatAuth)
			default:
				const slug = "unified-login-test"
				if transport == "custom oauth" {
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						if r.URL.Path == "/token" {
							_, _ = w.Write([]byte(`{"access_token":"provider-token","token_type":"Bearer"}`))
							return
						}
						_, _ = w.Write([]byte(`{"sub":"bound-custom","name":"User"}`))
					}))
					t.Cleanup(upstream.Close)
					oauth.RegisterCustom(slug, oauth.NewGenericOAuthProvider(&model.CustomOAuthProvider{
						Id: 42, Slug: slug, Name: "Custom login", Enabled: true, ClientId: "client", ClientSecret: "secret", UserIdField: "sub",
						TokenEndpoint: upstream.URL + "/token", UserInfoEndpoint: upstream.URL + "/userinfo",
					}))
					require.NoError(t, model.DB.Create(&model.UserOAuthBinding{UserId: user.Id, ProviderId: 42, ProviderUserId: "bound-custom"}).Error)
				} else {
					oauth.Register(slug, &boundLoginOAuthProvider{userID: user.Id})
				}
				t.Cleanup(func() { oauth.Unregister(slug) })
				token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: model.AuthFlowPurposeOAuth, Provider: slug, Intent: model.AuthFlowIntentLogin, Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute)})
				require.NoError(t, err)
				router := gin.New()
				router.GET("/api/oauth/:provider", HandleOAuth)
				response = httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest("GET", "/api/oauth/"+slug+"?state="+token+"&code=provider-code", nil))
			}
			var result struct {
				Success bool                   `json:"success"`
				Data    service.LoginChallenge `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			assert.True(t, result.Data.RequireVerification)
			assert.Equal(t, []service.VerificationMethodOption{{Method: "passkey", Available: true}}, result.Data.Methods)
			assert.NotEmpty(t, result.Data.FlowToken)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			count, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
			require.NoError(t, err)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestSecurityLoginPasskeyCannotCompleteAnotherChallenge(t *testing.T) {
	for _, otherUser := range []bool{false, true} {
		t.Run(fmt.Sprintf("other-user=%t", otherUser), func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			key := newSecurityLoginPasskey(t, user.Id)
			first, err := service.StartLoginVerification(user, "password", nil)
			require.NoError(t, err)
			passkeyToken, challenge := beginSecurityLoginPasskey(t, first.FlowToken)
			if otherUser {
				user = &model.User{Username: "other-login", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "other-login", AuthVersion: 1}
				require.NoError(t, model.DB.Create(user).Error)
				newSecurityLoginPasskey(t, user.Id)
			}
			second, err := service.StartLoginVerification(user, "password", nil)
			require.NoError(t, err)
			body, err := common.Marshal(map[string]any{"flow_token": second.FlowToken, "passkey_flow_token": passkeyToken, "credential": securityPasskeyResponse(t, key, challenge, false, 0)})
			require.NoError(t, err)
			response := securityEnrollmentRequest("POST", "/api/user/login/passkey/finish", string(body), "", service.AuthIdentity{}, LoginPasskeyFinish)
			var result securityEnrollmentResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			var count int64
			require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestSecurityLoginPasskeyCanManageTwoFAWithScopedProofs(t *testing.T) {
	for _, scope := range []string{service.VerificationScopeTwoFADisable, service.VerificationScopeTwoFABackupCodes} {
		t.Run(scope, func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			key := newSecurityLoginPasskey(t, user.Id)
			require.NoError(t, model.DB.Create(&model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}).Error)
			hash, err := common.HashBackupCode("ABCD-1234")
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.TwoFABackupCode{UserId: user.Id, CodeHash: hash}).Error)
			if scope == service.VerificationScopeTwoFABackupCodes {
				_, err := service.VerifySecurityInput(identity, service.VerificationInput{Scope: scope, Method: "2fa", Code: "ABCD-1234"})
				assert.ErrorIs(t, err, service.ErrVerificationFailed)
				remaining, err := model.GetUnusedBackupCodeCount(user.Id)
				require.NoError(t, err)
				assert.Equal(t, 1, remaining, "backup codes cannot authorize generating replacement backup codes")
			}
			beginBody, err := common.Marshal(map[string]string{"scope": scope})
			require.NoError(t, err)
			response := securityEnrollmentRequest("POST", "/api/user/passkey/verify/begin", string(beginBody), "", identity, PasskeyVerifyBegin)
			var started struct {
				Success bool `json:"success"`
				Data    struct {
					FlowToken string `json:"flow_token"`
					Options   struct {
						PublicKey struct {
							Challenge        string `json:"challenge"`
							UserVerification string `json:"userVerification"`
						} `json:"publicKey"`
					} `json:"options"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &started))
			require.True(t, started.Success, response.Body.String())
			assert.Equal(t, "required", started.Data.Options.PublicKey.UserVerification)
			finish, err := common.Marshal(passkeyFinishRequest{FlowToken: started.Data.FlowToken, Credential: securityPasskeyResponse(t, key, started.Data.Options.PublicKey.Challenge, false, 0)})
			require.NoError(t, err)
			response = securityEnrollmentRequest("POST", "/api/user/passkey/verify/finish", string(finish), "", identity, PasskeyVerifyFinish)
			var result securityEnrollmentResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			var proof service.SecurityProof
			require.NoError(t, common.Unmarshal(result.Data, &proof))
			handler, path := Disable2FA, "/api/user/2fa/disable"
			if scope == service.VerificationScopeTwoFABackupCodes {
				handler, path = RegenerateBackupCodes, "/api/user/2fa/backup_codes"
				response = securityEnrollmentRequest("POST", "/api/user/2fa/disable", `{}`, proof.ProofToken, identity, Disable2FA)
				assert.Contains(t, response.Body.String(), "SECURITY_PROOF_SCOPE_MISMATCH")
			}
			response = securityEnrollmentRequest("POST", path, `{}`, proof.ProofToken, identity, handler)
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			stored, err := model.GetUserById(user.Id, false)
			require.NoError(t, err)
			assert.Equal(t, identity.UserAuthVersion+1, stored.AuthVersion)
			_, err = model.GetPasskeyByUserID(user.Id)
			require.NoError(t, err, "managing 2FA must retain the alternative Passkey factor")
			factor, err := model.GetTwoFAByUserId(user.Id)
			require.NoError(t, err)
			if scope == service.VerificationScopeTwoFADisable {
				assert.Nil(t, factor)
			} else {
				require.NotNil(t, factor)
				assert.True(t, factor.IsEnabled)
				assert.Contains(t, string(result.Data), "backup_codes")
			}
			response = securityEnrollmentRequest("POST", path, `{}`, proof.ProofToken, identity, handler)
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.False(t, result.Success, "the proof cannot authorize a second mutation")
		})
	}
}

func TestSecurityLoginRegisteredPasskeyRequiresUserVerification(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprintf("verified=%t", verified), func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			require.NoError(t, err)
			proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopePasskeyRegister}, service.VerificationMethodPassword)
			response := securityEnrollmentRequest("POST", "/api/user/passkey/register/begin", "", proof, identity, PasskeyRegisterBegin)
			var result struct {
				Success bool `json:"success"`
				Data    struct {
					FlowToken string `json:"flow_token"`
					Options   struct {
						PublicKey struct {
							Challenge              string `json:"challenge"`
							AuthenticatorSelection struct {
								UserVerification string `json:"userVerification"`
							} `json:"authenticatorSelection"`
						} `json:"publicKey"`
					} `json:"options"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success, response.Body.String())
			assert.Equal(t, "required", result.Data.Options.PublicKey.AuthenticatorSelection.UserVerification)
			body, err := common.Marshal(passkeyFinishRequest{FlowToken: result.Data.FlowToken, Credential: securityPasskeyResponse(t, key, result.Data.Options.PublicKey.Challenge, true, 0, verified)})
			require.NoError(t, err)
			response = securityEnrollmentRequest("POST", "/api/user/passkey/register/finish", string(body), "", identity, PasskeyRegisterFinish)
			var finished securityEnrollmentResponse
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &finished))
			assert.Equal(t, verified, finished.Success, response.Body.String())
			_, err = model.GetPasskeyByUserID(user.Id)
			if verified {
				require.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, model.ErrPasskeyNotFound)
			}
		})
	}
}

func TestSecurityLoginRequiresConfiguredFactors(t *testing.T) {
	for _, test := range []struct {
		name             string
		twoFA, passkey   bool
		locked, disabled bool
		methods          []service.VerificationMethodOption
		unavailable      bool
	}{
		{name: "password without additional factors"},
		{name: "passkey requires verification", passkey: true, methods: []service.VerificationMethodOption{{Method: "passkey", Available: true}}},
		{name: "twofa requires verification", twoFA: true, methods: []service.VerificationMethodOption{{Method: "2fa", Available: true}}},
		{name: "both factors are alternatives", twoFA: true, passkey: true, methods: []service.VerificationMethodOption{{Method: "2fa", Available: true}, {Method: "passkey", Available: true}}},
		{name: "locked twofa permits passkey", twoFA: true, passkey: true, locked: true, methods: []service.VerificationMethodOption{{Method: "2fa", Available: false, Reason: service.ErrVerificationLocked.Error()}, {Method: "passkey", Available: true}}},
		{name: "disabled passkey permits twofa", twoFA: true, passkey: true, disabled: true, methods: []service.VerificationMethodOption{{Method: "2fa", Available: true}, {Method: "passkey", Available: false, Reason: "Passkey authentication is disabled."}}},
		{name: "only passkey disabled blocks password", passkey: true, disabled: true, unavailable: true},
		{name: "both factors unavailable block password", passkey: true, twoFA: true, disabled: true, locked: true, unavailable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			user, _ := setupSecurityEnrollmentTest(t)
			previousPasswordLogin := common.PasswordLoginEnabled
			common.PasswordLoginEnabled = true
			t.Cleanup(func() { common.PasswordLoginEnabled = previousPasswordLogin })
			if test.passkey {
				require.NoError(t, model.DB.Create(&model.PasskeyCredential{UserID: user.Id, CredentialID: "login-key", PublicKey: "public-key"}).Error)
			}
			if test.twoFA {
				factor := &model.TwoFA{UserId: user.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}
				if test.locked {
					until := time.Now().Add(time.Minute)
					factor.LockedUntil = &until
				}
				require.NoError(t, model.DB.Create(factor).Error)
			}
			system_setting.GetPasskeySettings().Enabled = !test.disabled
			before, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
			require.NoError(t, err)
			response := securityEnrollmentRequest(http.MethodPost, "/api/user/login", `{"username":"enrollment-user","password":"enrollment-password"}`, "", service.AuthIdentity{}, Login)
			var result struct {
				Success bool `json:"success"`
				Data    struct {
					RequireVerification bool                               `json:"require_verification"`
					FlowToken           string                             `json:"flow_token"`
					ExpiresAt           int64                              `json:"expires_at"`
					AccessToken         string                             `json:"access_token"`
					Methods             []service.VerificationMethodOption `json:"methods"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			after, err := model.CountActiveUserSessions(user.Id, time.Now().Unix())
			require.NoError(t, err)
			if test.unavailable {
				assert.False(t, result.Success)
				assert.Equal(t, before, after)
				assert.Empty(t, response.Header().Values("Set-Cookie"))
				return
			}
			require.True(t, result.Success, response.Body.String())
			if len(test.methods) == 0 {
				assert.False(t, result.Data.RequireVerification)
				assert.NotEmpty(t, result.Data.AccessToken)
				assert.Equal(t, before+1, after)
				return
			}
			assert.True(t, result.Data.RequireVerification)
			assert.NotEmpty(t, result.Data.FlowToken)
			assert.Greater(t, result.Data.ExpiresAt, time.Now().Unix())
			assert.LessOrEqual(t, result.Data.ExpiresAt, time.Now().Add(5*time.Minute).Unix())
			assert.Equal(t, test.methods, result.Data.Methods)
			assert.Empty(t, result.Data.AccessToken)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			assert.Equal(t, before, after, "a pending challenge must not create a session")
		})
	}
}

type authFlowTestOAuthProvider struct {
	exchangeErr   error
	userInfoErr   error
	exchangeCalls int
	userInfoCalls int
}

func (*authFlowTestOAuthProvider) GetName() string { return "Auth Flow Test" }
func (*authFlowTestOAuthProvider) IsEnabled() bool { return true }
func (provider *authFlowTestOAuthProvider) ExchangeToken(context.Context, string, *gin.Context) (*oauth.OAuthToken, error) {
	provider.exchangeCalls++
	if provider.exchangeErr != nil {
		return nil, provider.exchangeErr
	}
	return &oauth.OAuthToken{}, nil
}
func (provider *authFlowTestOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	provider.userInfoCalls++
	if provider.userInfoErr != nil {
		return nil, provider.userInfoErr
	}
	return &oauth.OAuthUser{ProviderUserID: "external-user"}, nil
}
func (*authFlowTestOAuthProvider) IsUserIDTaken(string) bool                      { return false }
func (*authFlowTestOAuthProvider) FillUserByProviderID(*model.User, string) error { return nil }
func (*authFlowTestOAuthProvider) SetProviderUserID(*model.User, string)          {}
func (*authFlowTestOAuthProvider) GetProviderPrefix() string                      { return "flow_" }
func (*authFlowTestOAuthProvider) ProviderUserIDColumn() string                   { return "" }

func setupAuthFlowControllerTest(t *testing.T) *authFlowTestOAuthProvider {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousRedis := common.RedisEnabled
	common.RedisEnabled = false
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.AuthFlow{}, &model.User{}, &model.UserSession{}, &model.AuditLog{}))
	model.DB, model.LOG_DB = db, db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	provider := &authFlowTestOAuthProvider{}
	oauth.Register("auth-flow-test", provider)
	t.Cleanup(func() {
		oauth.Unregister("auth-flow-test")
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RedisEnabled = previousRedis
		common.SetMainDatabaseType(previousType)
	})
	return provider
}

func TestGenerateOAuthCodeCarriesAffiliateInLoginFlow(t *testing.T) {
	setupAuthFlowControllerTest(t)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/oauth/state", strings.NewReader(`{"provider":"auth-flow-test","intent":"login","aff":"invite-code"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	GenerateOAuthCode(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			FlowToken string `json:"flow_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	flow, err := model.GetAuthFlow(response.Data.FlowToken, model.AuthFlowMatch{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentLogin,
	})
	require.NoError(t, err)
	var payload oauthFlowPayload
	require.NoError(t, common.UnmarshalJsonStr(flow.Payload, &payload))
	assert.Equal(t, "invite-code", payload.AffiliateCode)
	assert.Zero(t, flow.UserId)
	assert.Empty(t, flow.SessionId)
}

func TestGenerateOAuthCodeBindsFlowToAuthenticatedSession(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	oauth.Register("auth-flow-test", &authFlowTestOAuthProvider{})
	t.Cleanup(func() { oauth.Unregister("auth-flow-test") })
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: []byte(`{"provider":"auth-flow-test"}`)}, service.VerificationMethodPassword)
	response := securityEnrollmentRequest(http.MethodPost, "/api/oauth/state", `{"provider":"auth-flow-test","intent":"bind"}`, proof, identity, GenerateOAuthCode)
	var result struct {
		Success bool `json:"success"`
		Data    struct {
			FlowToken string `json:"flow_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	flow, err := model.GetAuthFlow(result.Data.FlowToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentBind, UserId: identity.UserID, SessionId: identity.SessionID})
	require.NoError(t, err)
	assert.Equal(t, identity.UserID, flow.UserId)
	assert.Equal(t, identity.SessionID, flow.SessionId)
}

func TestOAuthLoginConsumesFlowOnlyAfterProviderIdentity(t *testing.T) {
	provider := setupAuthFlowControllerTest(t)

	tests := []struct {
		name        string
		exchangeErr error
		userInfoErr error
	}{
		{name: "exchange failure", exchangeErr: errors.New("exchange failed")},
		{name: "user info failure", userInfoErr: errors.New("user info failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider.exchangeErr = test.exchangeErr
			provider.userInfoErr = test.userInfoErr
			token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
				Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentLogin,
				Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute),
			})
			require.NoError(t, err)

			router := gin.New()
			router.GET("/api/oauth/:provider", HandleOAuth)
			request := httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+token+"&code=test", nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			flow, err := model.GetAuthFlow(token, model.AuthFlowMatch{
				Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentLogin,
			})
			require.NoError(t, err)
			assert.Nil(t, flow.ConsumedAt)
		})
	}
}

func TestOAuthLoginConsumesFlowAfterProviderIdentityAndOnProviderError(t *testing.T) {
	provider := setupAuthFlowControllerTest(t)

	provider.exchangeErr = nil
	provider.userInfoErr = nil
	successToken, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentLogin,
		Payload: `{invalid`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/api/oauth/:provider", HandleOAuth)
	request := httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+successToken+"&code=test", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	_, err = model.GetAuthFlow(successToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, model.ErrAuthFlowConsumed)
	assert.Equal(t, 1, provider.exchangeCalls)
	assert.Equal(t, 1, provider.userInfoCalls)

	providerErrorToken, _, err := model.CreateAuthFlow(model.AuthFlowCreate{
		Purpose: model.AuthFlowPurposeOAuth, Provider: "auth-flow-test", Intent: model.AuthFlowIntentLogin,
		Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute),
	})
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+providerErrorToken+"&error=access_denied", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	_, err = model.GetAuthFlow(providerErrorToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, model.ErrAuthFlowConsumed)
	assert.Equal(t, 1, provider.exchangeCalls)
	assert.Equal(t, 1, provider.userInfoCalls)
}

func TestOAuthBindProviderErrorConsumesSessionBoundFlow(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	provider := &authFlowTestOAuthProvider{}
	oauth.Register("auth-flow-test", provider)
	t.Cleanup(func() { oauth.Unregister("auth-flow-test") })
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: []byte(`{"provider":"auth-flow-test"}`)}, service.VerificationMethodPassword)
	started := securityEnrollmentRequest(http.MethodPost, "/api/oauth/state", `{"provider":"auth-flow-test","intent":"bind"}`, proof, identity, GenerateOAuthCode)
	var result struct {
		Data struct {
			FlowToken string `json:"flow_token"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(started.Body.Bytes(), &result))
	require.NotEmpty(t, result.Data.FlowToken)
	response := securityEnrollmentRequest(http.MethodGet, "/api/oauth/auth-flow-test?state="+result.Data.FlowToken+"&error=access_denied&error_description=cancelled", "", "", identity, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "provider", Value: "auth-flow-test"}}
		HandleOAuth(c)
	})
	assert.Equal(t, http.StatusOK, response.Code)
	_, err := model.GetAuthFlow(result.Data.FlowToken, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	assert.ErrorIs(t, err, model.ErrAuthFlowConsumed)
	assert.Zero(t, provider.exchangeCalls)
	assert.Zero(t, provider.userInfoCalls)
}

// legacyGitHubOAuthProvider follows the GitHub provider contract: the numeric
// account ID identifies the user, Extra["legacy_id"] carries the username and
// the verified email list is served on demand. Lookups go through the real
// github_id model queries.
type legacyGitHubOAuthProvider struct {
	authFlowTestOAuthProvider
	providerUserID     string
	legacyID           string
	verifiedEmails     []string
	verifiedEmailsErr  error
	verifiedEmailCalls int
}

func (*legacyGitHubOAuthProvider) GetName() string { return "GitHub" }
func (provider *legacyGitHubOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: provider.providerUserID, Username: provider.legacyID, Extra: map[string]any{"legacy_id": provider.legacyID}}, nil
}
func (provider *legacyGitHubOAuthProvider) GetVerifiedEmails(context.Context, *oauth.OAuthToken) ([]string, error) {
	provider.verifiedEmailCalls++
	return provider.verifiedEmails, provider.verifiedEmailsErr
}
func (*legacyGitHubOAuthProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsGitHubIdAlreadyTaken(providerUserID)
}
func (*legacyGitHubOAuthProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.GitHubId = providerUserID
	return user.FillUserByGitHubId()
}
func (*legacyGitHubOAuthProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.GitHubId = providerUserID
}
func (*legacyGitHubOAuthProvider) GetProviderPrefix() string    { return "github_" }
func (*legacyGitHubOAuthProvider) ProviderUserIDColumn() string { return "github_id" }

// legacyGitHubOAuthLogin registers provider for one test and completes an OAuth
// login callback with it.
func legacyGitHubOAuthLogin(t *testing.T, provider *legacyGitHubOAuthProvider) *httptest.ResponseRecorder {
	t.Helper()
	const slug = "github-legacy-login-test"
	oauth.Register(slug, provider)
	t.Cleanup(func() { oauth.Unregister(slug) })
	token, _, err := model.CreateAuthFlow(model.AuthFlowCreate{Purpose: model.AuthFlowPurposeOAuth, Provider: slug, Intent: model.AuthFlowIntentLogin, Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute)})
	require.NoError(t, err)
	router := gin.New()
	router.GET("/api/oauth/:provider", HandleOAuth)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/oauth/"+slug+"?state="+token+"&code=provider-code", nil))
	return response
}

// legacyGitHubBindingAudits returns the account binding audit rows in creation
// order together with their encoded parameters.
func legacyGitHubBindingAudits(t *testing.T) ([]model.AuditLog, []string) {
	t.Helper()
	var audits []model.AuditLog
	require.NoError(t, model.LOG_DB.Where("action = ?", "user.binding_bind").Order("id").Find(&audits).Error)
	params := make([]string, 0, len(audits))
	for _, audit := range audits {
		require.NotNil(t, audit.Other.Op)
		encoded, err := common.Marshal(audit.Other.Op.Params)
		require.NoError(t, err)
		params = append(params, string(encoded))
	}
	return audits, params
}

func TestOAuthLoginLegacyGitHubBindingRequiresAccountEvidence(t *testing.T) {
	const declined = "This GitHub account cannot be linked to an existing account automatically, please sign in or register another way and then link GitHub in account settings"
	tests := []struct {
		name              string
		existingGitHubID  string
		existingDeleted   bool
		withoutEmail      bool
		legacyID          string
		verifiedEmails    []string
		verifiedEmailsErr error
		registerEnabled   bool
		expectLogin       bool
		expectMigration   bool
		expectNewAccount  bool
		expectEmailCalls  int
		expectAuditParams string
	}{
		{name: "numeric legacy value registers a new account", existingGitHubID: "424242", legacyID: "424242", verifiedEmails: []string{"legacy-github@example.com"}, registerEnabled: true, expectLogin: true, expectNewAccount: true},
		{name: "numeric legacy value with registration disabled", existingGitHubID: "424242", legacyID: "424242", verifiedEmails: []string{"legacy-github@example.com"}},
		{name: "soft-deleted legacy row registers a new account", existingGitHubID: "octocat-legacy", existingDeleted: true, legacyID: "octocat-legacy", verifiedEmails: []string{"legacy-github@example.com"}, registerEnabled: true, expectLogin: true, expectNewAccount: true},
		{
			name: "verified email match migrates", existingGitHubID: "octocat-legacy", legacyID: "octocat-legacy",
			verifiedEmails: []string{"other@example.com", " Legacy-GitHub@Example.com "}, expectLogin: true, expectMigration: true, expectEmailCalls: 1,
			// No SMTP server is configured in tests, so the notification attempt is recorded as failed.
			expectAuditParams: `{"provider":"github","legacy_migration":true,"legacy_id":"octocat-legacy","provider_user_id":"900001","verified_email_matched":true,"success":true,"notification_failed":true}`,
		},
		{
			name: "no matching verified email declines", existingGitHubID: "octocat-legacy", legacyID: "octocat-legacy",
			verifiedEmails: []string{"other@example.com"}, registerEnabled: true, expectEmailCalls: 1,
			expectAuditParams: `{"provider":"github","legacy_migration":true,"legacy_id":"octocat-legacy","provider_user_id":"900001","success":false,"reason":"no_matching_evidence"}`,
		},
		{
			name: "verified emails failure declines", existingGitHubID: "octocat-legacy", legacyID: "octocat-legacy",
			verifiedEmailsErr: errors.New("emails unavailable"), registerEnabled: true, expectEmailCalls: 1,
			expectAuditParams: `{"provider":"github","legacy_migration":true,"legacy_id":"octocat-legacy","provider_user_id":"900001","success":false,"reason":"verified_emails_unavailable"}`,
		},
		{
			name: "account without email declines before asking the provider", existingGitHubID: "octocat-legacy", withoutEmail: true, legacyID: "octocat-legacy",
			verifiedEmails: []string{"legacy-github@example.com"}, registerEnabled: true,
			expectAuditParams: `{"provider":"github","legacy_migration":true,"legacy_id":"octocat-legacy","provider_user_id":"900001","success":false,"reason":"no_matching_evidence"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			setupSecurityEnrollmentTest(t)
			previousRegister := common.RegisterEnabled
			common.RegisterEnabled = test.registerEnabled
			t.Cleanup(func() { common.RegisterEnabled = previousRegister })
			existing := &model.User{Username: "legacy-github", Email: "legacy-github@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "legacy-github", AuthVersion: 1, GitHubId: test.existingGitHubID}
			if test.withoutEmail {
				existing.Email = ""
			}
			require.NoError(t, model.DB.Create(existing).Error)
			if test.existingDeleted {
				require.NoError(t, model.DB.Delete(existing).Error)
			}
			provider := &legacyGitHubOAuthProvider{providerUserID: "900001", legacyID: test.legacyID, verifiedEmails: test.verifiedEmails, verifiedEmailsErr: test.verifiedEmailsErr}
			response := legacyGitHubOAuthLogin(t, provider)
			var result struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
				Data    struct {
					User struct {
						Id int `json:"id"`
					} `json:"user"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, test.expectLogin, result.Success, response.Body.String())
			assert.Equal(t, test.expectEmailCalls, provider.verifiedEmailCalls)

			var reloaded model.User
			require.NoError(t, model.DB.Unscoped().First(&reloaded, existing.Id).Error)
			audits, params := legacyGitHubBindingAudits(t)
			if test.expectAuditParams == "" {
				assert.Empty(t, audits)
			} else {
				require.Len(t, audits, 1)
				assert.Equal(t, existing.Id, audits[0].UserId)
				assert.Equal(t, common.RoleCommonUser, audits[0].ActorRole)
				assert.Equal(t, test.expectMigration, audits[0].Success)
				assert.JSONEq(t, test.expectAuditParams, params[0])
			}
			if test.expectMigration {
				assert.Equal(t, existing.Id, result.Data.User.Id)
				assert.Equal(t, "900001", reloaded.GitHubId)
				assert.NotEmpty(t, response.Header().Values("Set-Cookie"))
				return
			}
			assert.Equal(t, test.existingGitHubID, reloaded.GitHubId, "the existing binding must stay untouched")
			if test.expectNewAccount {
				var created model.User
				require.NoError(t, model.DB.Where("github_id = ?", "900001").First(&created).Error)
				assert.NotEqual(t, existing.Id, created.Id)
				assert.Equal(t, created.Id, result.Data.User.Id)
				return
			}
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			if test.expectAuditParams != "" {
				assert.Equal(t, declined, result.Message)
			}
		})
	}
}

func TestOAuthLoginLegacyGitHubBindingMigratesAfterLoginVerification(t *testing.T) {
	for _, interference := range []string{"none", "pending id claimed by another account", "binding relinked meanwhile"} {
		t.Run(interference, func(t *testing.T) {
			setupSecurityEnrollmentTest(t)
			existing := &model.User{Username: "legacy-github", Email: "legacy-github@example.com", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "legacy-github", AuthVersion: 1, GitHubId: "octocat-legacy"}
			require.NoError(t, model.DB.Create(existing).Error)
			factor := &model.TwoFA{UserId: existing.Id, Secret: "JBSWY3DPEHPK3PXP", IsEnabled: true}
			require.NoError(t, model.DB.Create(factor).Error)
			provider := &legacyGitHubOAuthProvider{providerUserID: "900001", legacyID: "octocat-legacy", verifiedEmails: []string{"legacy-github@example.com"}}
			response := legacyGitHubOAuthLogin(t, provider)
			var challenge struct {
				Success bool                   `json:"success"`
				Data    service.LoginChallenge `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &challenge))
			require.True(t, challenge.Success, response.Body.String())
			require.True(t, challenge.Data.RequireVerification)
			assert.Equal(t, []service.VerificationMethodOption{{Method: "2fa", Available: true}}, challenge.Data.Methods)
			assert.Empty(t, response.Header().Values("Set-Cookie"))
			assert.Zero(t, provider.verifiedEmailCalls, "a second factor is checked before the provider emails")
			var reloaded model.User
			require.NoError(t, model.DB.First(&reloaded, existing.Id).Error)
			assert.Equal(t, "octocat-legacy", reloaded.GitHubId, "the binding waits for the verification")
			audits, _ := legacyGitHubBindingAudits(t)
			assert.Empty(t, audits)

			expectedGitHubID, expectLogin := "900001", true
			switch interference {
			case "pending id claimed by another account":
				other := &model.User{Username: "other-github", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "other-github", AuthVersion: 1, GitHubId: "900001"}
				require.NoError(t, model.DB.Create(other).Error)
				expectedGitHubID, expectLogin = "octocat-legacy", false
			case "binding relinked meanwhile":
				require.NoError(t, model.DB.Model(existing).Update("github_id", "relinked-legacy").Error)
				expectedGitHubID = "relinked-legacy"
			}
			code, err := totp.GenerateCode(factor.Secret, time.Now())
			require.NoError(t, err)
			body, err := common.Marshal(map[string]string{"flow_token": challenge.Data.FlowToken, "code": code})
			require.NoError(t, err)
			for attempt := range 2 {
				response = securityEnrollmentRequest(http.MethodPost, "/api/user/login/verify", string(body), "", service.AuthIdentity{}, VerifyLogin)
				var result securityEnrollmentResponse
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
				assert.Equal(t, attempt == 0 && expectLogin, result.Success, response.Body.String())
				if attempt == 0 && !expectLogin {
					assert.Equal(t, "ACCOUNT_ALREADY_BOUND", result.Code)
				}
				if result.Success {
					assert.NotEmpty(t, response.Header().Values("Set-Cookie"))
				} else {
					assert.Empty(t, response.Header().Values("Set-Cookie"))
				}
			}
			require.NoError(t, model.DB.First(&reloaded, existing.Id).Error)
			assert.Equal(t, expectedGitHubID, reloaded.GitHubId)
			sessions, err := model.CountActiveUserSessions(existing.Id, time.Now().Unix())
			require.NoError(t, err)
			expectedSessions := 0
			if expectLogin {
				expectedSessions = 1
			}
			assert.EqualValues(t, expectedSessions, sessions, "the consumed challenge cannot be replayed")
			audits, params := legacyGitHubBindingAudits(t)
			if interference != "none" {
				assert.Empty(t, audits)
				return
			}
			require.Len(t, audits, 1)
			assert.Equal(t, existing.Id, audits[0].UserId)
			assert.Equal(t, common.RoleCommonUser, audits[0].ActorRole)
			assert.True(t, audits[0].Success)
			// No SMTP server is configured in tests, so the notification attempt is recorded as failed.
			assert.JSONEq(t, `{"provider":"github","legacy_migration":true,"legacy_id":"octocat-legacy","provider_user_id":"900001","verification_method":"2fa","success":true,"notification_failed":true}`, params[0])
		})
	}
}

func TestOAuthBindIgnoresLegacyGitHubUsernames(t *testing.T) {
	tests := []struct {
		name          string
		ownGitHubID   string
		otherGitHubID string
		legacyID      string
		expectBound   bool
	}{
		{name: "another account's legacy username does not block binding", otherGitHubID: "octocat-legacy", legacyID: "octocat-legacy", expectBound: true},
		{name: "own legacy username is replaced", ownGitHubID: "octocat-legacy", otherGitHubID: "unrelated-legacy", legacyID: "octocat-legacy", expectBound: true},
		{name: "numeric legacy value does not collide", otherGitHubID: "424242", legacyID: "424242", expectBound: true},
		{name: "numeric ID held by another account is rejected", otherGitHubID: "900001", legacyID: "octocat-legacy"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			if test.ownGitHubID != "" {
				require.NoError(t, model.DB.Model(user).Update("github_id", test.ownGitHubID).Error)
			}
			other := &model.User{Username: "other-github", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AffCode: "other-github", AuthVersion: 1, GitHubId: test.otherGitHubID}
			require.NoError(t, model.DB.Create(other).Error)
			const slug = "github-legacy-bind-test"
			oauth.Register(slug, &legacyGitHubOAuthProvider{providerUserID: "900001", legacyID: test.legacyID})
			t.Cleanup(func() { oauth.Unregister(slug) })
			proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopeAccountBind, Context: []byte(`{"provider":"` + slug + `"}`)}, service.VerificationMethodPassword)
			started := securityEnrollmentRequest(http.MethodPost, "/api/oauth/state", `{"provider":"`+slug+`","intent":"bind"}`, proof, identity, GenerateOAuthCode)
			var flow struct {
				Data struct {
					FlowToken string `json:"flow_token"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(started.Body.Bytes(), &flow))
			require.NotEmpty(t, flow.Data.FlowToken, started.Body.String())
			response := securityEnrollmentRequest(http.MethodGet, "/api/oauth/"+slug+"?state="+flow.Data.FlowToken+"&code=provider-code", "", "", identity, func(c *gin.Context) {
				c.Params = gin.Params{{Key: "provider", Value: slug}}
				HandleOAuth(c)
			})
			var result struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			assert.Equal(t, test.expectBound, result.Success, response.Body.String())

			var bound, untouched model.User
			require.NoError(t, model.DB.First(&bound, user.Id).Error)
			require.NoError(t, model.DB.First(&untouched, other.Id).Error)
			assert.Equal(t, test.otherGitHubID, untouched.GitHubId)
			if test.expectBound {
				assert.Equal(t, "900001", bound.GitHubId)
				return
			}
			assert.Equal(t, test.ownGitHubID, bound.GitHubId)
			assert.Equal(t, "This GitHub account has already been bound", result.Message)
		})
	}
}

func setupXCodeAuthTest(t *testing.T) (*model.User, service.AuthIdentity) {
	t.Helper()
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.XCodeRevokeOperation{}))
	return user, identity
}

func xcodeAuthorizeInput() service.XCodeAuthorizeRequest {
	return service.XCodeAuthorizeRequest{
		ClientID: "xcode-desktop", ResponseType: "code", RedirectURI: "zcode://oauth/callback",
		State: "deterministic-state-128bits", CodeChallenge: "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", CodeChallengeMethod: "S256",
	}
}

func xcodeCodeForIdentity(t *testing.T, identity service.AuthIdentity, redirects ...string) service.XCodeTokenRequest {
	t.Helper()
	input := xcodeAuthorizeInput()
	require.LessOrEqual(t, len(redirects), 1)
	if len(redirects) == 1 {
		input.RedirectURI = redirects[0]
	}
	flow, err := service.StartXCodeAuthorization(input)
	require.NoError(t, err)
	view, err := service.GetXCodeAuthorizationRequest(flow)
	require.NoError(t, err)
	assert.Equal(t, "xcode-desktop", view.ClientID)
	assert.InDelta(t, time.Now().Add(10*time.Minute).Unix(), view.ExpiresAt, 2)
	redirect, err := service.CompleteXCodeAuthorization(identity, flow)
	require.NoError(t, err)
	base, _, _ := strings.Cut(redirect, "?")
	assert.Equal(t, input.RedirectURI, base)
	callback, err := url.Parse(redirect)
	require.NoError(t, err)
	assert.Equal(t, "oauth", callback.Host)
	assert.Equal(t, "/callback", callback.Path)
	assert.Len(t, callback.Query(), 3)
	assert.Equal(t, "new-api", callback.Query().Get("provider"))
	assert.Equal(t, input.State, callback.Query().Get("state"))
	require.NotEmpty(t, callback.Query().Get("code"))
	_, err = service.CompleteXCodeAuthorization(identity, flow)
	assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
	return service.XCodeTokenRequest{
		GrantType: "authorization_code", ClientID: input.ClientID, RedirectURI: input.RedirectURI,
		Code: callback.Query().Get("code"), CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
	}
}

func TestXCodeAuthorizationRejectsInvalidProtocol(t *testing.T) {
	setupXCodeAuthTest(t)
	for _, test := range []struct{ field, value string }{
		{"client", "other"}, {"response", "token"}, {"redirect", "zcode://oauth/callback/"},
		{"redirect", "zcode://oauth/callback?extra=1"}, {"redirect", "ZCODE://oauth/callback"},
		{"method", "plain"}, {"method", ""}, {"state", ""}, {"state", strings.Repeat("a", 21)},
		{"state", strings.Repeat("a", 257)}, {"state", "invalid state with spaces"},
		{"challenge", ""}, {"challenge", strings.Repeat("a", 42)}, {"challenge", strings.Repeat("a", 44)},
		{"challenge", strings.Repeat("a", 42) + "="}, {"challenge", strings.Repeat("a", 42) + "B"},
	} {
		t.Run(test.field+"/"+test.value, func(t *testing.T) {
			input := xcodeAuthorizeInput()
			switch test.field {
			case "client":
				input.ClientID = test.value
			case "response":
				input.ResponseType = test.value
			case "redirect":
				input.RedirectURI = test.value
			case "method":
				input.CodeChallengeMethod = test.value
			case "state":
				input.State = test.value
			case "challenge":
				input.CodeChallenge = test.value
			}
			flow, err := service.StartXCodeAuthorization(input)
			assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
			assert.Empty(t, flow)
		})
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Count(&count).Error)
	assert.Zero(t, count, "invalid requests must not create anonymous flows")
}

func TestXCodeDesktopRegisteredRedirectsRemainCodeBound(t *testing.T) {
	redirects := []string{"aiscoo://oauth/callback", "aiscoo-dev://oauth/callback", "aiscoo-preview://oauth/callback", "zcode://oauth/callback"}
	for _, redirect := range redirects {
		t.Run(redirect, func(t *testing.T) {
			_, identity := setupXCodeAuthTest(t)
			for _, method := range []string{"plain", ""} {
				request := xcodeAuthorizeInput()
				request.RedirectURI, request.CodeChallengeMethod = redirect, method
				flow, err := service.StartXCodeAuthorization(request)
				assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
				assert.Empty(t, flow)
			}
			input := xcodeCodeForIdentity(t, identity, redirect)
			for _, other := range redirects {
				if other == redirect {
					continue
				}
				wrong := input
				wrong.RedirectURI = other
				bundle, err := service.ExchangeXCodeAuthorizationCode(wrong, "", "")
				assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid, "code is bound to %s, not %s", redirect, other)
				assert.Nil(t, bundle)
			}
			wrong := input
			wrong.ClientID, wrong.RedirectURI = "xwork-cli", "http://127.0.0.1:49152/oauth/callback"
			_, err := service.ExchangeXCodeAuthorizationCode(wrong, "", "")
			assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			wrong = input
			wrong.CodeVerifier = strings.Repeat("a", 43)
			_, err = service.ExchangeXCodeAuthorizationCode(wrong, "", "")
			assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			var keys int64
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&keys).Error)
			assert.Zero(t, keys, "failed proofs must not issue credentials or consume the code")
			bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "desktop-identity-test")
			require.NoError(t, err)
			assert.Equal(t, "xcode-desktop", bundle.Session.ClientID)
			assert.Equal(t, model.ClientPurposeXCode, bundle.Session.ClientPurpose)
			control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			for _, other := range redirects {
				if other == redirect {
					continue
				}
				wrong := input
				wrong.RedirectURI = other
				_, err := service.ExchangeXCodeAuthorizationCode(wrong, "", "")
				assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			}
			_, _, err = service.ValidateXCodeLoginSession(control)
			require.NoError(t, err, "a replay with another registered URI cannot revoke the issued session")
			_, err = service.ExchangeXCodeAuthorizationCode(input, "", "")
			assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			_, _, err = service.ValidateXCodeLoginSession(control)
			assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
			key, err := model.GetTokenById(bundle.Token.Id)
			require.NoError(t, err)
			assert.Equal(t, common.TokenStatusDisabled, key.Status)
			_, _, err = service.ValidateLoginSession(identity)
			require.NoError(t, err, "code replay must preserve the browser's session")
		})
	}
}

func TestXCodeDesktopRejectsUnregisteredRedirects(t *testing.T) {
	setupXCodeAuthTest(t)
	for _, scheme := range []string{"aiscoo", "aiscoo-dev", "aiscoo-preview", "zcode"} {
		for _, redirect := range []string{
			strings.ToUpper(scheme) + "://oauth/callback", scheme + "://OAUTH/callback",
			scheme + "://oauth/callback/", scheme + "://oauth/callback?extra=1", scheme + "://oauth/callback#fragment",
			scheme + "://oauth/callback?", scheme + "://oauth/callback#",
			scheme + "://user@oauth/callback", scheme + "://oauth:443/callback",
			scheme + "://oauth/%63allback", scheme + "://oauth/../callback",
			scheme + "://oauth/callback.evil", scheme + "://oauth.evil/callback",
			" " + scheme + "://oauth/callback", scheme + "://oauth/callback\n",
		} {
			t.Run(redirect, func(t *testing.T) {
				request := xcodeAuthorizeInput()
				request.RedirectURI = redirect
				flow, err := service.StartXCodeAuthorization(request)
				assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
				assert.Empty(t, flow)
				bundle, err := service.ExchangeXCodeAuthorizationCode(service.XCodeTokenRequest{
					GrantType: "authorization_code", ClientID: request.ClientID, RedirectURI: redirect,
					Code: strings.Repeat("a", 43), CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk",
				}, "", "")
				assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
				assert.Nil(t, bundle)
			})
		}
		request := xcodeAuthorizeInput()
		request.RedirectURI = scheme + "://oauth/callback"
		for _, clientID := range []string{"xwork-cli", "web", "aiscoo-desktop"} {
			request.ClientID = clientID
			flow, err := service.StartXCodeAuthorization(request)
			assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid, clientID)
			assert.Empty(t, flow)
		}
	}
	for _, redirect := range []string{"http://127.0.0.1:49152/oauth/callback", "http://localhost:49152/oauth/callback", "https://oauth/callback", "aiscoo-other://oauth/callback"} {
		request := xcodeAuthorizeInput()
		request.RedirectURI = redirect
		flow, err := service.StartXCodeAuthorization(request)
		assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid, redirect)
		assert.Empty(t, flow)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.AuthFlow{}).Count(&count).Error)
	assert.Zero(t, count, "rejected requests must not create flows")
}

func TestXCodeDesktopRegisteredRedirectsExpire(t *testing.T) {
	for _, redirect := range []string{"aiscoo://oauth/callback", "aiscoo-dev://oauth/callback", "aiscoo-preview://oauth/callback", "zcode://oauth/callback"} {
		for _, purpose := range []string{model.AuthFlowPurposeXCodeRequest, model.AuthFlowPurposeXCodeCode} {
			t.Run(redirect+"/"+purpose, func(t *testing.T) {
				_, identity := setupXCodeAuthTest(t)
				if purpose == model.AuthFlowPurposeXCodeRequest {
					request := xcodeAuthorizeInput()
					request.RedirectURI = redirect
					flow, err := service.StartXCodeAuthorization(request)
					require.NoError(t, err)
					require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", purpose).Update("expires_at", time.Now().Add(-time.Minute)).Error)
					_, err = service.GetXCodeAuthorizationRequest(flow)
					assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
					callback, err := service.CompleteXCodeAuthorization(identity, flow)
					assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
					assert.Empty(t, callback)
				} else {
					input := xcodeCodeForIdentity(t, identity, redirect)
					var code model.AuthFlow
					require.NoError(t, model.DB.Where("purpose = ?", purpose).First(&code).Error)
					assert.InDelta(t, time.Now().Add(time.Minute).Unix(), code.ExpiresAt.Unix(), 2)
					require.NoError(t, model.DB.Model(&code).Update("expires_at", time.Now().Add(-time.Minute)).Error)
					bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
					assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
					assert.Nil(t, bundle)
				}
				var keys int64
				require.NoError(t, model.DB.Model(&model.Token{}).Count(&keys).Error)
				assert.Zero(t, keys)
			})
		}
	}
}

func TestXCodeDesktopRegisteredRedirectsPreserveRefreshAndLogout(t *testing.T) {
	for _, redirect := range []string{"aiscoo://oauth/callback", "aiscoo-dev://oauth/callback", "aiscoo-preview://oauth/callback", "zcode://oauth/callback"} {
		t.Run(redirect, func(t *testing.T) {
			_, identity := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, identity, redirect), "", "")
			require.NoError(t, err)
			_, err = service.RefreshXCodeSession(service.XCodeRefreshRequest{ClientID: "xwork-cli", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}, "", "")
			assert.ErrorIs(t, err, service.ErrAuthCredentialScopeDenied)
			assert.ErrorIs(t, service.LogoutXCodeSession(nil, service.XCodeLogoutRequest{ClientID: "xwork-cli", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}), service.ErrAuthCredentialScopeDenied)
			refreshed, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}, "", "")
			require.NoError(t, err)
			assert.Equal(t, bundle.Session.SID, refreshed.Session.SID)
			assert.Equal(t, "xcode-desktop", refreshed.Session.ClientID)
			assert.Equal(t, bundle.Token.Id, refreshed.Token.Id)
			assert.Equal(t, bundle.Token.Key, refreshed.Token.Key)
			assert.NotEqual(t, bundle.RefreshToken, refreshed.RefreshToken)
			require.NoError(t, service.LogoutXCodeSession(nil, service.XCodeLogoutRequest{ClientID: "xcode-desktop", SessionID: refreshed.Session.SID, RefreshToken: refreshed.RefreshToken}))
			control, err := service.ParseXCodeAccessToken(refreshed.AccessToken)
			require.NoError(t, err)
			_, _, err = service.ValidateXCodeLoginSession(control)
			assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
			_, _, err = service.ValidateLoginSession(identity)
			require.NoError(t, err, "desktop logout must preserve the browser's session")
		})
	}
}

func TestXCodeCLIAuthorizationIsolatedSession(t *testing.T) {
	_, identity := setupXCodeAuthTest(t)
	input := xcodeAuthorizeInput()
	input.ClientID = "xwork-cli"
	input.RedirectURI = "http://127.0.0.1:49152/oauth/callback"
	flow, err := service.StartXCodeAuthorization(input)
	require.NoError(t, err)
	callback, err := service.CompleteXCodeAuthorization(identity, flow)
	require.NoError(t, err)
	u, err := url.Parse(callback)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:49152", u.Host)
	assert.Equal(t, "/oauth/callback", u.Path)
	assert.Equal(t, input.State, u.Query().Get("state"))
	tokenInput := service.XCodeTokenRequest{GrantType: "authorization_code", ClientID: input.ClientID, RedirectURI: input.RedirectURI, Code: u.Query().Get("code"), CodeVerifier: "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"}
	// 与 xcodeAuthorizeInput 的独立固定 verifier 一致，错误 URI 不能消费 code。
	wrong := tokenInput
	wrong.RedirectURI = "http://127.0.0.1:49153/oauth/callback"
	_, err = service.ExchangeXCodeAuthorizationCode(wrong, "", "")
	assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
	bundle, err := service.ExchangeXCodeAuthorizationCode(tokenInput, "", "cli-test")
	require.NoError(t, err)
	assert.Equal(t, "xwork-cli", bundle.Session.ClientID)
	_, err = service.RefreshXCodeSession(service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}, "", "")
	assert.Error(t, err)
	refreshed, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{ClientID: "xwork-cli", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}, "", "")
	require.NoError(t, err)
	assert.Error(t, service.LogoutXCodeSession(nil, service.XCodeLogoutRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: refreshed.RefreshToken}))
	require.NoError(t, service.LogoutXCodeSession(nil, service.XCodeLogoutRequest{ClientID: "xwork-cli", SessionID: bundle.Session.SID, RefreshToken: refreshed.RefreshToken}))
	_, _, err = service.ValidateLoginSession(identity)
	require.NoError(t, err)
}

func TestXCodeCodeVerifierFailureDoesNotConsumeCode(t *testing.T) {
	_, identity := setupXCodeAuthTest(t)
	input := xcodeCodeForIdentity(t, identity)
	for _, verifier := range []string{"", strings.Repeat("a", 42), strings.Repeat("a", 129), strings.Repeat("!", 43), strings.Repeat("a", 43), xcodeAuthorizeInput().CodeChallenge} {
		invalid := input
		invalid.CodeVerifier = verifier
		_, err := service.ExchangeXCodeAuthorizationCode(invalid, "", "")
		assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
	}
	for _, field := range []string{"client", "redirect", "grant"} {
		invalid := input
		switch field {
		case "client":
			invalid.ClientID = "other"
		case "redirect":
			invalid.RedirectURI += "/"
		case "grant":
			invalid.GrantType = "refresh_token"
		}
		_, err := service.ExchangeXCodeAuthorizationCode(invalid, "", "")
		assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
	}
	bundle, err := service.ExchangeXCodeAuthorizationCode(input, "127.0.0.1", "xcode-test")
	require.NoError(t, err)
	assert.NotEqual(t, identity.SessionID, bundle.Session.SID)
	assert.Equal(t, model.ClientPurposeXCode, bundle.Session.ClientPurpose)
	assert.Equal(t, bundle.Session.SID, bundle.Token.ClientSessionSID)
	assert.True(t, bundle.Token.UnlimitedQuota)
	assert.Zero(t, bundle.Token.RemainQuota)
	assert.Empty(t, bundle.Token.Group)
	assert.LessOrEqual(t, bundle.Token.ExpiredTime, bundle.Session.ExpiresAt)
	control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, bundle.Session.SID, control.SessionID)
	_, err = service.ParseAccessToken(bundle.AccessToken)
	assert.Error(t, err)
	wrongReplay := input
	wrongReplay.CodeVerifier = strings.Repeat("a", 43)
	_, err = service.ExchangeXCodeAuthorizationCode(wrongReplay, "", "")
	assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
	_, _, err = service.ValidateXCodeLoginSession(control)
	require.NoError(t, err, "invalid verifier replay cannot revoke a live client")
	_, err = service.ExchangeXCodeAuthorizationCode(input, "", "")
	assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
	_, _, err = service.ValidateXCodeLoginSession(control)
	assert.Error(t, err, "valid replay revokes only the code's issued client")
	key, err := model.GetTokenById(bundle.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, key.Status)
	source, err := model.GetUserSessionBySID(identity.SessionID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, source.Status)
}

func TestXCodeConcurrentReplayRevokesIssuanceAfterSourceRevocation(t *testing.T) {
	user, identity := setupXCodeAuthTest(t)
	input := xcodeCodeForIdentity(t, identity)
	preflight, resume := make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("test:pause_code_preflight", func(tx *gorm.DB) {
		flow, ok := tx.Statement.Dest.(*model.AuthFlow)
		if ok && flow.Purpose == model.AuthFlowPurposeXCodeCode && flow.ConsumedAt == nil && paused.CompareAndSwap(false, true) {
			close(preflight)
			<-resume
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("test:pause_code_preflight") })
	replayed := make(chan error, 1)
	go func() {
		_, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
		replayed <- err
	}()
	select {
	case <-preflight:
	case <-time.After(10 * time.Second):
		t.Fatal("code preflight did not reach the race barrier")
	}
	bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
	require.NoError(t, err)
	_, err = model.RevokeUserSession(user.Id, identity.SessionID, "source_revoke")
	require.NoError(t, err)
	close(resume)
	assert.ErrorIs(t, <-replayed, service.ErrXCodeAuthCodeInvalid)
	control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	_, _, err = service.ValidateXCodeLoginSession(control)
	assert.ErrorIs(t, err, service.ErrLoginSessionRevoked, "valid concurrent replay must revoke its issued session even when its source is now invalid")
	key, err := model.GetTokenById(bundle.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, key.Status)
}

func TestXCodeCodeSourceAuthorityAndExpiry(t *testing.T) {
	for _, change := range []string{"code expiry", "source revoked", "auth version", "source expiry", "source purpose", "source refresh"} {
		t.Run(change, func(t *testing.T) {
			user, identity := setupXCodeAuthTest(t)
			input := xcodeCodeForIdentity(t, identity)
			switch change {
			case "code expiry":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id = ?", user.Id).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			case "source revoked":
				_, err := model.RevokeUserSession(user.Id, identity.SessionID, "test")
				require.NoError(t, err)
			case "auth version":
				_, err := model.BumpUserAuthVersion(user.Id)
				require.NoError(t, err)
			case "source expiry":
				require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", identity.SessionID).Update("expires_at", time.Now().Add(-time.Minute).Unix()).Error)
			case "source purpose":
				require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", identity.SessionID).Update("client_purpose", model.ClientPurposeXCode).Error)
			case "source refresh":
				require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", identity.SessionID).Update("version", identity.SessionVersion+1).Error)
			}
			bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
			if change == "source refresh" {
				require.NoError(t, err)
				assert.NotEmpty(t, bundle.AccessToken)
				return
			}
			assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			var count int64
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestXCodeExchangeRollsBackAllIssuanceFacts(t *testing.T) {
	for _, failure := range []string{"key limit", "session limit", "issuance limit", "key insert"} {
		t.Run(failure, func(t *testing.T) {
			_, identity := setupXCodeAuthTest(t)
			input := xcodeCodeForIdentity(t, identity)
			active, issuance, tokens := common.UserSessionActiveLimit, common.UserSessionIssuanceLimit, operation_setting.GetMaxUserTokens()
			callbackRegistered := false
			t.Cleanup(func() {
				common.UserSessionActiveLimit, common.UserSessionIssuanceLimit = active, issuance
				operation_setting.GetTokenSetting().MaxUserTokens = tokens
			})
			switch failure {
			case "key limit":
				operation_setting.GetTokenSetting().MaxUserTokens = 0
			case "session limit":
				common.UserSessionActiveLimit = 1
			case "issuance limit":
				common.UserSessionIssuanceLimit = 1
			case "key insert":
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("xcode_test_key_failure", func(tx *gorm.DB) {
					if _, ok := tx.Statement.Dest.(*model.Token); ok {
						tx.AddError(errors.New("key insert failed"))
					}
				}))
				callbackRegistered = true
				t.Cleanup(func() {
					if callbackRegistered {
						_ = model.DB.Callback().Create().Remove("xcode_test_key_failure")
					}
				})
			}
			bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
			require.Error(t, err)
			assert.Nil(t, bundle)
			var sessions, keys int64
			require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&keys).Error)
			assert.EqualValues(t, 1, sessions)
			assert.Zero(t, keys)
			var codes []model.AuthFlow
			require.NoError(t, model.DB.Where("user_id = ?", identity.UserID).Find(&codes).Error)
			require.Len(t, codes, 1)
			assert.Nil(t, codes[0].ConsumedAt)
			common.UserSessionActiveLimit, common.UserSessionIssuanceLimit = active, issuance
			operation_setting.GetTokenSetting().MaxUserTokens = tokens
			if callbackRegistered {
				require.NoError(t, model.DB.Callback().Create().Remove("xcode_test_key_failure"))
				callbackRegistered = false
			}
			_, err = service.ExchangeXCodeAuthorizationCode(input, "", "")
			require.NoError(t, err, "rolled-back code remains usable")
		})
	}
}
func enableXCodeAuthTestRedis(t *testing.T, userID int) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	previous := common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = false, previous
		require.NoError(t, client.Close())
	})
	require.NoError(t, model.PublishUserAuthCache(userID))
	return server
}

func TestXCodeExchangeSerializesLastIssuanceSlot(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, cached := range []bool{false, true} {
			for _, limit := range []string{"session", "issuance", "key"} {
				t.Run(fmt.Sprintf("native=%t/redis=%t/%s", native, cached, limit), func(t *testing.T) {
					user, identity := setupXCodeAuthTest(t)
					if cached {
						enableXCodeAuthTestRedis(t, user.Id)
					}
					losingCode := xcodeCodeForIdentity(t, identity)
					firstCode := service.XCodeTokenRequest{}
					if !native {
						firstCode = xcodeCodeForIdentity(t, identity)
					}
					active, issuance, keys := common.UserSessionActiveLimit, common.UserSessionIssuanceLimit, operation_setting.GetMaxUserTokens()
					t.Cleanup(func() {
						common.UserSessionActiveLimit, common.UserSessionIssuanceLimit = active, issuance
						operation_setting.GetTokenSetting().MaxUserTokens = keys
					})
					common.UserSessionActiveLimit, common.UserSessionIssuanceLimit = 100, 100
					operation_setting.GetTokenSetting().MaxUserTokens = 100
					want := model.ErrUserSessionLimit
					switch limit {
					case "session":
						common.UserSessionActiveLimit = 2
					case "issuance":
						common.UserSessionIssuanceLimit = 2
						want = model.ErrUserSessionIssuanceLimit
					case "key":
						operation_setting.GetTokenSetting().MaxUserTokens = 1
						want = model.ErrUserTokenLimit
					}
					connection, err := model.DB.DB()
					require.NoError(t, err)
					connection.SetMaxOpenConns(4)
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					model.DB = model.DB.WithContext(ctx)
					creating, attempting := make(chan struct{}), make(chan struct{})
					releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
					var firstOnce, releaseFirstOnce, releaseSecondOnce sync.Once
					var secondStarted atomic.Bool
					release := func() {
						releaseFirstOnce.Do(func() { close(releaseFirst) })
						releaseSecondOnce.Do(func() { close(releaseSecond) })
					}
					defer release()
					require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("xcode_last_slot_create", func(tx *gorm.DB) {
						if tx.Statement.Table != "user_sessions" && tx.Statement.Table != "tokens" {
							return
						}
						firstOnce.Do(func() {
							close(creating)
							select {
							case <-releaseFirst:
							case <-ctx.Done():
								tx.AddError(ctx.Err())
							}
						})
					}))
					// Hold the competing row-lock attempt until the first issuance commits.
					// SQLite's production BEGIN IMMEDIATE already serializes the writers.
					secondBarrier := func(tx *gorm.DB) {
						select {
						case <-creating:
						default:
							return
						}
						_, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter)
						_, locking := tx.Statement.Clauses["FOR"]
						if common.UsingMainDatabase(common.DatabaseTypeSQLite) || tx.Statement.Table != "users" || !inTransaction || !locking {
							return
						}
						if !secondStarted.CompareAndSwap(false, true) {
							return
						}
						close(attempting)
						select {
						case <-releaseSecond:
						case <-ctx.Done():
							tx.AddError(ctx.Err())
						}
					}
					require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("xcode_last_slot_owner", secondBarrier))
					t.Cleanup(func() {
						_ = model.DB.Callback().Create().Remove("xcode_last_slot_create")
						_ = model.DB.Callback().Query().Remove("xcode_last_slot_owner")
					})
					firstResult, secondResult := make(chan error, 1), make(chan error, 1)
					go func() {
						if !native {
							_, err := service.ExchangeXCodeAuthorizationCode(firstCode, "", "")
							firstResult <- err
						} else if limit == "key" {
							token := &model.Token{UserId: user.Id, Key: strings.Repeat("k", 48), Status: common.TokenStatusEnabled, Name: "native", ExpiredTime: -1, UnlimitedQuota: true}
							firstResult <- token.Insert()
						} else {
							_, err := service.CreateLoginSession(user.Id, "password", "", "")
							firstResult <- err
						}
					}()
					select {
					case <-creating:
					case <-ctx.Done():
						t.Fatal("first issuance did not reach its write barrier")
					}
					go func() {
						if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
							if secondStarted.CompareAndSwap(false, true) {
								close(attempting)
							}
						}
						bundle, err := service.ExchangeXCodeAuthorizationCode(losingCode, "", "")
						if bundle != nil {
							secondResult <- errors.New("last-slot loser returned credentials")
							return
						}
						secondResult <- err
					}()
					select {
					case <-attempting:
					case <-ctx.Done():
						t.Fatal("competing exchange never reached its issuance rendezvous")
					}
					releaseFirstOnce.Do(func() { close(releaseFirst) })
					require.NoError(t, <-firstResult)
					releaseSecondOnce.Do(func() { close(releaseSecond) })
					assert.ErrorIs(t, <-secondResult, want)
					var sessions, tokens int64
					require.NoError(t, model.DB.Model(&model.UserSession{}).Where("user_id = ?", user.Id).Count(&sessions).Error)
					require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&tokens).Error)
					wantSessions, wantTokens := int64(2), int64(1)
					if native && limit == "key" {
						wantSessions = 1
					} else if native {
						wantTokens = 0
					}
					assert.Equal(t, wantSessions, sessions)
					assert.Equal(t, wantTokens, tokens)
					flow, err := model.GetAuthFlow(losingCode.Code, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeXCodeCode})
					require.NoError(t, err, "the losing code remains unconsumed")
					assert.NotContains(t, flow.Payload, "issued_session_sid")
					common.UserSessionActiveLimit, common.UserSessionIssuanceLimit = 100, 100
					operation_setting.GetTokenSetting().MaxUserTokens = 100
					_, err = service.ExchangeXCodeAuthorizationCode(losingCode, "", "")
					require.NoError(t, err, "the same code is retryable when capacity becomes available")
				})
			}
		}
	}
}

func TestXCodeExchangeCannotInvertHardDeleteOwnerLock(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("redis=%t", cached), func(t *testing.T) {
			user, identity := setupXCodeAuthTest(t)
			input := xcodeCodeForIdentity(t, identity)
			if cached {
				enableXCodeAuthTestRedis(t, user.Id)
			}
			require.NoError(t, model.DB.AutoMigrate(&model.ExternalIdentityClaim{}))
			connection, err := model.DB.DB()
			require.NoError(t, err)
			connection.SetMaxOpenConns(4)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			model.DB = model.DB.WithContext(ctx)
			deleting, attempting, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var deletingOnce, attemptingOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("xcode_delete_owner", func(tx *gorm.DB) {
				if tx.Statement.Table != "users" || tx.Error != nil {
					return
				}
				deletingOnce.Do(func() {
					close(deleting)
					select {
					case <-release:
					case <-ctx.Done():
						tx.AddError(ctx.Err())
					}
				})
			}))
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("xcode_delete_exchange", func(tx *gorm.DB) {
				select {
				case <-deleting:
				default:
					return
				}
				_, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter)
				_, locking := tx.Statement.Clauses["FOR"]
				if common.UsingMainDatabase(common.DatabaseTypeSQLite) || tx.Statement.Table != "users" || !inTransaction || !locking {
					return
				}
				attemptingOnce.Do(func() { close(attempting) })
			}))
			t.Cleanup(func() {
				_ = model.DB.Callback().Update().Remove("xcode_delete_owner")
				_ = model.DB.Callback().Query().Remove("xcode_delete_exchange")
			})
			deleted, exchanged := make(chan error, 1), make(chan error, 1)
			go func() {
				_, err := user.HardDelete()
				deleted <- err
			}()
			select {
			case <-deleting:
			case <-ctx.Done():
				t.Fatal("HardDelete did not reach its owner-locked barrier")
			}
			go func() {
				if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
					attemptingOnce.Do(func() { close(attempting) })
				}
				bundle, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
				if bundle != nil {
					exchanged <- errors.New("deleted source issued credentials")
					return
				}
				exchanged <- err
			}()
			earlyRejected := false
			var exchangeErr error
			select {
			case exchangeErr = <-exchanged:
				assert.True(t, cached, "without Redis the exchange must serialize on the owner")
				earlyRejected = true
			case <-attempting:
			case <-ctx.Done():
				t.Fatal("exchange did not reject its fence or reach owner serialization")
			}
			unblock()
			deleteErr := <-deleted
			if !earlyRejected {
				exchangeErr = <-exchanged
			}
			assert.NoError(t, deleteErr, "deletion must commit without a flow/user lock cycle")
			assert.ErrorIs(t, exchangeErr, service.ErrXCodeAuthCodeInvalid)
			for _, table := range []any{&model.User{}, &model.AuthFlow{}, &model.UserSession{}, &model.Token{}} {
				var count int64
				query := model.DB.Unscoped().Model(table)
				if _, ok := table.(*model.User); ok {
					query = query.Where("id = ?", user.Id)
				} else {
					query = query.Where("user_id = ?", user.Id)
				}
				require.NoError(t, query.Count(&count).Error)
				assert.Zero(t, count, "HardDelete must leave no authority or credentials")
			}
		})
	}
}

func TestXCodeAuthorizationRequiresLiveDashboardSource(t *testing.T) {
	for _, source := range []string{"anonymous", "PAT", "xcode", "unknown purpose", "revoked", "stale version", "expired request", "other purpose"} {
		t.Run(source, func(t *testing.T) {
			user, identity := setupXCodeAuthTest(t)
			flow, err := service.StartXCodeAuthorization(xcodeAuthorizeInput())
			require.NoError(t, err)
			switch source {
			case "anonymous":
				identity = service.AuthIdentity{}
			case "PAT":
				identity.SessionID = "pat:1"
			case "xcode", "unknown purpose":
				purpose := model.ClientPurposeXCode
				if source == "unknown purpose" {
					purpose = "other"
				}
				require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", identity.SessionID).Update("client_purpose", purpose).Error)
			case "revoked":
				_, err = model.RevokeUserSession(user.Id, identity.SessionID, "test")
				require.NoError(t, err)
			case "stale version":
				identity.SessionVersion++
			case "expired request":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id = 0").Update("expires_at", time.Now().Add(-time.Minute)).Error)
				_, err = service.GetXCodeAuthorizationRequest(flow)
				assert.ErrorIs(t, err, service.ErrXCodeAuthRequestInvalid)
			case "other purpose":
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id = 0").Update("purpose", model.AuthFlowPurposeOAuth).Error)
			}
			redirect, err := service.CompleteXCodeAuthorization(identity, flow)
			assert.Error(t, err)
			assert.Empty(t, redirect)
			var count int64
			require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("user_id > 0").Count(&count).Error)
			assert.Zero(t, count, "failed confirmation must not issue a code")
		})
	}
}

func TestXCodeRefreshPreservesKeyAndRejectsRecreation(t *testing.T) {
	for _, change := range []string{"rotate", "deleted key", "disabled key", "dashboard refresh", "wrong SID", "wrong client"} {
		t.Run(change, func(t *testing.T) {
			_, identity := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, identity), "", "")
			require.NoError(t, err)
			input := service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}
			switch change {
			case "deleted key":
				require.NoError(t, bundle.Token.Delete())
			case "disabled key":
				bundle.Token.Status = common.TokenStatusDisabled
				require.NoError(t, bundle.Token.SelectUpdate())
			case "dashboard refresh":
				dashboard, err := service.CreateLoginSession(identity.UserID, "password", "", "")
				require.NoError(t, err)
				input.SessionID, input.RefreshToken = dashboard.Session.SID, dashboard.RefreshToken
			case "wrong SID":
				input.SessionID = identity.SessionID
			case "wrong client":
				input.ClientID = "other"
			}
			refreshed, err := service.RefreshXCodeSession(input, "", "")
			if change == "rotate" {
				require.NoError(t, err)
				assert.Equal(t, bundle.Token.Id, refreshed.Token.Id)
				assert.Equal(t, bundle.Token.Key, refreshed.Token.Key)
				assert.Equal(t, bundle.Session.SID, refreshed.Session.SID)
				assert.NotEqual(t, bundle.RefreshToken, refreshed.RefreshToken)
			} else {
				assert.Error(t, err)
				assert.Nil(t, refreshed)
			}
			var count int64
			require.NoError(t, model.DB.Unscoped().Model(&model.Token{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}

func TestXCodeLogoutRequiresCredentialAndPreservesOtherSessions(t *testing.T) {
	for _, credential := range []string{"control", "refresh", "bare SID", "wrong refresh", "wrong control SID", "dashboard", "PAT"} {
		t.Run(credential, func(t *testing.T) {
			_, dashboard := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
			require.NoError(t, err)
			other, err := service.CreateLoginSession(dashboard.UserID, "password", "", "")
			require.NoError(t, err)
			control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			input := service.XCodeLogoutRequest{SessionID: bundle.Session.SID}
			var identity *service.AuthIdentity
			switch credential {
			case "control":
				identity = &control
			case "refresh":
				input.ClientID, input.RefreshToken = "xcode-desktop", bundle.RefreshToken
			case "wrong refresh":
				input.ClientID, input.RefreshToken = "xcode-desktop", bundle.Session.SID+"."+strings.Repeat("a", 64)
			case "wrong control SID":
				identity = &control
				input.SessionID = other.Session.SID
			case "dashboard":
				identity = &dashboard
			case "PAT":
				pat := dashboard
				pat.SessionID = "pat:1"
				identity = &pat
			}
			err = service.LogoutXCodeSession(identity, input)
			if credential == "control" || credential == "refresh" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			stored, err := model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			key, err := model.GetTokenById(bundle.Token.Id)
			require.NoError(t, err)
			if credential == "control" || credential == "refresh" {
				assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
				assert.Equal(t, common.TokenStatusDisabled, key.Status)
			} else {
				assert.Equal(t, model.UserSessionStatusActive, stored.Status)
				assert.Equal(t, common.TokenStatusEnabled, key.Status)
			}
			for _, sid := range []string{dashboard.SessionID, other.Session.SID} {
				native, err := model.GetUserSessionBySID(sid)
				require.NoError(t, err)
				assert.Equal(t, model.UserSessionStatusActive, native.Status)
			}
		})
	}
}

func TestXCodeRevokeOperationRetryAndCredentialBinding(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	rotated, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{
		SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop",
	}, "", "")
	require.NoError(t, err)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: rotated.RefreshToken,
		ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	result, err := service.SubmitXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	storedTarget, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusRevoked, storedTarget.Status)
	targetKey, err := model.GetTokenById(bundle.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, targetKey.Status)
	// 响应丢失后过了刷新宽限期，仍只能凭原操作凭据读取已提交结果。
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", 1).Error)
	result, err = service.SubmitXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	result, err = service.QueryXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	wrong := input
	wrong.RefreshToken = other.RefreshToken
	_, err = service.QueryXCodeRevokeOperation(wrong)
	require.Error(t, err)
	wrong = input
	wrong.OperationID = "fedcba9876543210fedcba9876543210"
	_, err = service.QueryXCodeRevokeOperation(wrong)
	require.Error(t, err)
	stored, err := model.GetUserSessionBySID(other.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	// 会话可早于操作记录清理，已提交结果仍须能用原凭据查询。
	require.NoError(t, model.DB.Where("sid = ?", input.SessionID).Delete(&model.UserSession{}).Error)
	result, err = service.QueryXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	require.NoError(t, model.DeleteExpiredXCodeRevokeOperations(time.Now().Add(8*24*time.Hour).Unix()))
	_, err = service.QueryXCodeRevokeOperation(input)
	require.Error(t, err)
}

func TestXCodeRevokeOperationSecurityBoundaries(t *testing.T) {
	user, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	otherUser := &model.User{Username: "other-revoke-user", Password: user.Password, AffCode: "other-revoke-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
	require.NoError(t, model.DB.Create(otherUser).Error)
	require.NoError(t, model.PublishUserAuthCache(otherUser.Id))
	otherDashboard, err := service.CreateLoginSession(otherUser.Id, "password", "", "")
	require.NoError(t, err)
	otherIdentity, err := service.ParseAccessToken(otherDashboard.AccessToken)
	require.NoError(t, err)
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, otherIdentity), "", "")
	require.NoError(t, err)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	wrong := input
	wrong.RefreshToken = other.RefreshToken
	_, err = service.SubmitXCodeRevokeOperation(wrong)
	require.Error(t, err)
	wrong = input
	wrong.ClientID = "xwork-cli"
	_, err = service.SubmitXCodeRevokeOperation(wrong)
	require.Error(t, err)
	_, err = service.QueryXCodeRevokeOperation(input)
	require.Error(t, err)
	rotated, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{SessionID: input.SessionID, RefreshToken: input.RefreshToken, ClientID: input.ClientID}, "", "")
	require.NoError(t, err)
	// 前代凭据在并发宽限内可撤销；操作证明在宽限结束后仍可查询。
	result, err := service.SubmitXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", input.SessionID).Update("previous_valid_until", 1).Error)
	result, err = service.QueryXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	wrong = input
	wrong.RefreshToken = rotated.RefreshToken
	_, err = service.QueryXCodeRevokeOperation(wrong)
	require.Error(t, err)
	// 键按 SID 隔离：另一用户的会话以自己的有效凭据使用同一 operation_id 独立成功，
	// 不受第一个会话的记录影响。
	wrong = service.XCodeRevokeOperationRequest{SessionID: other.Session.SID, RefreshToken: other.RefreshToken, ClientID: "xcode-desktop", OperationID: input.OperationID}
	result, err = service.SubmitXCodeRevokeOperation(wrong)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
	otherStored, err := model.GetUserSessionBySID(other.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusRevoked, otherStored.Status)
	key, err := model.GetTokenById(other.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, key.Status)
}

func TestXCodeRevokeOperationConcurrentAndOtherRevocation(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	start := make(chan struct{})
	var workers sync.WaitGroup
	results := make(chan string, 2)
	errors := make(chan error, 2)
	for range 2 {
		workers.Go(func() {
			<-start
			result, err := service.SubmitXCodeRevokeOperation(input)
			results <- result
			errors <- err
		})
	}
	close(start)
	workers.Wait()
	for range 2 {
		require.NoError(t, <-errors)
		assert.Equal(t, "committed", <-results)
	}
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	revoked, err := model.RevokeUserSession(dashboard.UserID, other.Session.SID, "user_revoked")
	require.NoError(t, err)
	require.True(t, revoked)
	otherInput := service.XCodeRevokeOperationRequest{SessionID: other.Session.SID, RefreshToken: other.RefreshToken, ClientID: "xcode-desktop", OperationID: "fedcba9876543210fedcba9876543210"}
	_, err = service.SubmitXCodeRevokeOperation(otherInput)
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	_, err = service.QueryXCodeRevokeOperation(otherInput)
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
}

func TestXCodeRevokeOperationSameIDAcrossSessionsIsIndependent(t *testing.T) {
	user, dashboard := setupXCodeAuthTest(t)
	first, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	otherUser := &model.User{Username: "concurrent-revoke-user", Password: user.Password, AffCode: "concurrent-revoke-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
	require.NoError(t, model.DB.Create(otherUser).Error)
	require.NoError(t, model.PublishUserAuthCache(otherUser.Id))
	otherDashboard, err := service.CreateLoginSession(otherUser.Id, "password", "", "")
	require.NoError(t, err)
	identity, err := service.ParseAccessToken(otherDashboard.AccessToken)
	require.NoError(t, err)
	second, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, identity), "", "")
	require.NoError(t, err)
	// 键按 SID 隔离：两个不同用户的会话并发使用同一 operation_id 都必须各自成功。
	const sharedID = "0123456789abcdef0123456789abcdef"
	bundles := []*service.XCodeSessionBundle{first, second}
	start := make(chan struct{})
	var workers sync.WaitGroup
	results := make([]string, len(bundles))
	errs := make([]error, len(bundles))
	for i := range bundles {
		workers.Go(func() {
			<-start
			input := service.XCodeRevokeOperationRequest{SessionID: bundles[i].Session.SID, RefreshToken: bundles[i].RefreshToken, ClientID: "xcode-desktop", OperationID: sharedID}
			results[i], errs[i] = service.SubmitXCodeRevokeOperation(input)
		})
	}
	close(start)
	workers.Wait()
	for i := range bundles {
		require.NoError(t, errs[i], "session %d must not collide with another session's operation_id", i)
		assert.Equal(t, "committed", results[i])
		stored, err := model.GetUserSessionBySID(bundles[i].Session.SID)
		require.NoError(t, err)
		assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
		key, err := model.GetTokenById(bundles[i].Token.Id)
		require.NoError(t, err)
		assert.Equal(t, common.TokenStatusDisabled, key.Status)
	}
	var operations []model.XCodeRevokeOperation
	require.NoError(t, model.DB.Order("session_id").Find(&operations).Error)
	require.Len(t, operations, 2)
	assert.NotEqual(t, operations[0].OperationHash, operations[1].OperationHash, "the same operation_id maps to disjoint per-session keys")
	assert.NotEqual(t, operations[0].SessionID, operations[1].SessionID)

	// 每个会话只能用自己的凭据读取自己的结果；另一会话的凭据查询同一 ID 得到统一失败。
	for i := range bundles {
		own := service.XCodeRevokeOperationRequest{SessionID: bundles[i].Session.SID, RefreshToken: bundles[i].RefreshToken, ClientID: "xcode-desktop", OperationID: sharedID}
		result, err := service.QueryXCodeRevokeOperation(own)
		require.NoError(t, err)
		assert.Equal(t, "committed", result)
		cross := own
		cross.SessionID, cross.RefreshToken = bundles[1-i].Session.SID, bundles[1-i].RefreshToken
		cross.OperationID = "ffffffffffffffffffffffffffffffff"
		_, err = service.QueryXCodeRevokeOperation(cross)
		assert.ErrorIs(t, err, service.ErrRefreshTokenInvalid, "another session's query must not observe this session's result")
	}
}

func TestXCodeRevokeOperationSerializesRefreshBeforeFence(t *testing.T) {
	user, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	enableXCodeAuthTestRedis(t, user.Id)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	model.DB = model.DB.WithContext(ctx)
	updated, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("revoke_refresh_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table != "user_sessions" || tx.Error != nil {
			return
		}
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); !inTransaction {
			return
		}
		once.Do(func() {
			close(updated)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("revoke_refresh_barrier") })
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	revoked := make(chan error, 1)
	go func() { _, err := service.SubmitXCodeRevokeOperation(input); revoked <- err }()
	select {
	case <-updated:
	case <-ctx.Done():
		t.Fatal("revoke did not reach database update")
	}
	refreshed := make(chan error, 1)
	go func() {
		_, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{SessionID: input.SessionID, RefreshToken: input.RefreshToken, ClientID: input.ClientID}, "", "")
		refreshed <- err
	}()
	close(release)
	require.NoError(t, <-revoked)
	require.Error(t, <-refreshed)
	result, err := service.QueryXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
}

func TestXCodeRevokeOperationFenceExpiresBeforeCommit(t *testing.T) {
	if os.Getenv("TEST_SECURITY_DIALECT") == "sqlite" {
		t.Skip("SQLite readers block on fixture transaction mode")
	}
	user, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	server := miniredis.RunT(t)
	previous := common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RedisEnabled, common.RDB = false, previous; _ = client.Close() })
	require.NoError(t, model.PublishUserAuthCache(user.Id))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	model.DB = model.DB.WithContext(ctx)
	atCommit, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("revoke_commit_barrier", func(tx *gorm.DB) {
		if tx.Statement.Table != "tokens" || tx.Error != nil {
			return
		}
		once.Do(func() {
			close(atCommit)
			select {
			case <-release:
			case <-ctx.Done():
				tx.AddError(ctx.Err())
			}
		})
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("revoke_commit_barrier") })
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	outcome := make(chan error, 1)
	go func() { _, err := service.SubmitXCodeRevokeOperation(input); outcome <- err }()
	select {
	case <-atCommit:
	case <-ctx.Done():
		t.Fatal("revoke did not reach barrier")
	}
	server.FastForward(2 * time.Hour)
	stale, err := model.GetUserSessionCached(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stale.Status)
	// 模拟另一进程在旧快照上填充缓存，随后提交后的 tombstone 写入失败。
	key := "auth:session:" + common.GenerateHMACWithKey([]byte("user-session-cache-v1:"+common.SessionSecret), bundle.Session.SID)
	require.NoError(t, client.HSet(ctx, key, map[string]any{
		"SID": bundle.Session.SID, "UserID": user.Id, "Version": bundle.Session.Version,
		"UserAuthVersion": bundle.Session.UserAuthVersion, "Status": model.UserSessionStatusActive,
		"ExpiresAt": bundle.Session.ExpiresAt, "CacheSchema": 3, "ClientPurpose": model.ClientPurposeXCode,
	}).Err())
	server.SetError("ERR injected tombstone failure")
	close(release)
	require.NoError(t, <-outcome)
	server.SetError("")
	_, err = model.GetUserSessionCached(bundle.Session.SID)
	assert.ErrorIs(t, err, model.ErrUserSessionInactive, "stale active cache must not authorize after commit")
	control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
	require.NoError(t, err)
	_, _, err = service.ValidateXCodeLoginSession(control)
	assert.Error(t, err, "old control token must not authorize after commit")
}

func TestXCodeRevokeOperationAlreadyRevokedBindsIDAndCanonicalProof(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	revoked, err := model.RevokeUserSession(dashboard.UserID, bundle.Session.SID, "user_revoked")
	require.NoError(t, err)
	require.True(t, revoked)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	for _, id := range []string{input.OperationID, "11111111111111111111111111111111", "22222222222222222222222222222222"} {
		input.OperationID = id
		_, err := service.SubmitXCodeRevokeOperation(input)
		require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
		_, err = service.QueryXCodeRevokeOperation(input)
		require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&count).Error)
	assert.Zero(t, count)
	wrong := input
	wrong.SessionID = other.Session.SID
	_, err = service.SubmitXCodeRevokeOperation(wrong)
	require.Error(t, err)
	stored, err := model.GetUserSessionBySID(other.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	input.RefreshToken = " " + input.RefreshToken + " "
	_, err = service.QueryXCodeRevokeOperation(input)
	require.Error(t, err, "noncanonical refresh must be rejected consistently")
}

func TestXCodeRevokeOperationRejectsExpiredPreviousRefresh(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	_, err = service.RefreshXCodeSession(service.XCodeRefreshRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop"}, "", "")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", 1).Error)
	_, err = service.SubmitXCodeRevokeOperation(service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"})
	require.Error(t, err)
	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
}

func TestXCodeRevokeOperationDatabaseFailureRollsBack(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	enableXCodeAuthTestRedis(t, dashboard.UserID)
	injected := errors.New("injected transaction failure")
	require.NoError(t, model.DB.Callback().Update().After("gorm:update").Register("revoke_injected_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Update().Remove("revoke_injected_failure") })
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	_, err = service.SubmitXCodeRevokeOperation(input)
	require.ErrorIs(t, err, injected)
	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	var count int64
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&count).Error)
	assert.Zero(t, count)
	_, err = service.QueryXCodeRevokeOperation(input)
	assert.Error(t, err)
}

var (
	errInjectedCommit       = errors.New("injected commit failure")
	errInjectedDatabaseDown = errors.New("injected database outage")
)

// commitFaultPool 包装 *sql.DB，使显式事务的 COMMIT 阶段可注入故障。GORM 的 callback
// 只覆盖单条语句的隐式事务，无法影响 DB.Transaction 的提交，所以在连接池层包装 BeginTx。
// applied 为 true 时先真实提交再报错（数据库已生效但确认丢失）；为 false 时回滚后报错
// （提交未生效）。两种都对调用方表现为“提交结果不明”。unavailable 为 true 时所有访问
// 立即失败，模拟主库整体不可用。
type commitFaultPool struct {
	*sql.DB
	applied     bool
	failing     atomic.Bool
	unavailable atomic.Bool
	// transactionsDown 只让事务无法开始，读取仍然可用：数据库可读但撤销事务无法持久化。
	transactionsDown atomic.Bool
	// commitAttempts 统计被故障注入拦截的 COMMIT 次数，证明失败确实发生在提交阶段。
	commitAttempts atomic.Int32
	// beginAttempts 统计开启事务的次数：任何行锁都以事务为前提，因此“没有开启事务”
	// 在 SQLite（不支持 FOR UPDATE）上同样证明没有取得行锁。
	beginAttempts atomic.Int32
}

func (pool *commitFaultPool) GetDBConn() (*sql.DB, error) { return pool.DB, nil }

func (pool *commitFaultPool) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if pool.unavailable.Load() {
		return nil, errInjectedDatabaseDown
	}
	return pool.DB.PrepareContext(ctx, query)
}

func (pool *commitFaultPool) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if pool.unavailable.Load() {
		return nil, errInjectedDatabaseDown
	}
	return pool.DB.ExecContext(ctx, query, args...)
}

func (pool *commitFaultPool) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if pool.unavailable.Load() {
		return nil, errInjectedDatabaseDown
	}
	return pool.DB.QueryContext(ctx, query, args...)
}

func (pool *commitFaultPool) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if pool.unavailable.Load() {
		// 已取消的 context 使 *sql.Row 携带错误，Scan 时返回，等价于连接失败。
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		return pool.DB.QueryRowContext(canceled, query, args...)
	}
	return pool.DB.QueryRowContext(ctx, query, args...)
}

func (pool *commitFaultPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	pool.beginAttempts.Add(1)
	if pool.unavailable.Load() || pool.transactionsDown.Load() {
		return nil, errInjectedDatabaseDown
	}
	tx, err := pool.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if !pool.failing.Load() {
		return tx, nil
	}
	return &commitFaultTx{Tx: tx, applied: pool.applied, attempts: &pool.commitAttempts}, nil
}

type commitFaultTx struct {
	*sql.Tx
	applied  bool
	attempts *atomic.Int32
}

func (tx *commitFaultTx) Commit() error {
	tx.attempts.Add(1)
	if !tx.applied {
		_ = tx.Tx.Rollback()
		return errInjectedCommit
	}
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	return errInjectedCommit
}

// 凭据不匹配的首次提交必须在开启事务、获取任何行锁之前被拒绝：否则无效凭据也能在
// 用户行锁和会话行锁上排队，放大拒绝服务面。
func TestXCodeRevokeOperationRejectsBadCredentialBeforeAnyLock(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)

	base := model.DB
	sqlDB, err := base.DB()
	require.NoError(t, err)
	pool := &commitFaultPool{DB: sqlDB}
	counted := base.WithContext(context.Background())
	counted.Statement.ConnPool, counted.Config.ConnPool = pool, pool
	model.DB = counted
	t.Cleanup(func() { model.DB = base })
	// 同时观察 SELECT ... FOR UPDATE（MySQL/PostgreSQL 发出；SQLite 不支持该子句，由事务计数兜底）。
	var lockingReads atomic.Int32
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("bad_credential_lock_observer", func(tx *gorm.DB) {
		if _, locking := tx.Statement.Clauses["FOR"]; locking {
			lockingReads.Add(1)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("bad_credential_lock_observer") })

	valid := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	wrongSecret := valid
	wrongSecret.RefreshToken = bundle.Session.SID + ".not-the-issued-secret"
	// 他人的合法 refresh 也不能用于本会话：SID 与 secret 必须同属一个会话。
	foreignSecret := valid
	foreignSecret.RefreshToken = bundle.Session.SID + "." + strings.TrimPrefix(other.RefreshToken, other.Session.SID+".")
	// 前代凭据超出并发宽限期后不再有效（旧代 refresh 的重放由 refresh 流程处理，这里只验证不抢锁）。
	rotated, err := service.RefreshXCodeSession(service.XCodeRefreshRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop"}, "", "")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", 1).Error)
	expiredPrevious := valid

	response := func(input service.XCodeRevokeOperationRequest) (int, string) {
		body, err := common.Marshal(input)
		require.NoError(t, err)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/xcode/v1/auth/revoke-operations", bytes.NewReader(body))
		SubmitXCodeRevokeOperation(c)
		return recorder.Code, strings.ReplaceAll(recorder.Body.String(), c.GetString(common.RequestIdKey), "")
	}
	// 基线：凭据错误本来的响应（既不依赖本次预检，也不依赖是否取得锁）。
	baselineStatus, baselineBody := response(service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: "not-a-token", ClientID: "xcode-desktop", OperationID: valid.OperationID})
	require.Equal(t, http.StatusUnauthorized, baselineStatus)

	for name, input := range map[string]service.XCodeRevokeOperationRequest{
		"wrong secret":                      wrongSecret,
		"another session's secret":          foreignSecret,
		"previous refresh beyond the grace": expiredPrevious,
	} {
		pool.beginAttempts.Store(0)
		lockingReads.Store(0)
		status, body := response(input)
		assert.Equal(t, baselineStatus, status, name)
		assert.Equal(t, baselineBody, body, "%s must be indistinguishable from any other bad credential", name)
		assert.Zero(t, pool.beginAttempts.Load(), "%s must not open a transaction", name)
		assert.Zero(t, lockingReads.Load(), "%s must not issue SELECT ... FOR UPDATE", name)
	}

	// 有效凭据仍然正常撤销，说明上面的“零事务”不是因为整条路径不可用。
	valid.RefreshToken = rotated.RefreshToken
	pool.beginAttempts.Store(0)
	status, body := response(valid)
	assert.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "committed")
	assert.EqualValues(t, 1, pool.beginAttempts.Load(), "a valid first submit opens exactly one transaction")
}

// 预检基于事务外的快照，只能提前拒绝、不能授权。凭据在预检通过之后、行锁取得之前失效
// （refresh 轮换并且宽限期已过）时，事务内的锁下复核必须仍然拒绝：不撤销会话、不写记录、
// 不停用托管 key。这是预检不可替代锁内复核（TOCTOU）的回归。
func TestXCodeRevokeOperationRecheckUnderLockAfterPrecheck(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}

	base := model.DB
	sqlDB, err := base.DB()
	require.NoError(t, err)
	pool := &commitFaultPool{DB: sqlDB}
	counted := base.WithContext(context.Background())
	counted.Statement.ConnPool, counted.Config.ConnPool = pool, pool
	model.DB = counted
	t.Cleanup(func() { model.DB = base })

	// 注入点：事务外读取会话快照（预检的输入）刚返回之后、事务开始之前。此刻没有任何事务和
	// 行锁，所以在 SQLite 的 immediate 事务模式下用独立连接写入也不会阻塞。写入等价于另一
	// 节点上已提交的刷新：本次所用凭据变成前代，并且已超出并发宽限期。
	var raced atomic.Int32
	require.NoError(t, model.DB.Callback().Query().After("gorm:query").Register("precheck_toctou_rotate", func(tx *gorm.DB) {
		if _, inTransaction := tx.Statement.ConnPool.(gorm.TxCommitter); inTransaction || tx.Statement.Table != "user_sessions" || tx.Error != nil {
			return
		}
		if !raced.CompareAndSwap(0, 1) {
			return
		}
		require.NoError(t, base.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Updates(map[string]any{
			"previous_refresh_hash": bundle.Session.RefreshHash, "previous_valid_until": 1,
			"refresh_hash": strings.Repeat("f", 64),
		}).Error)
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove("precheck_toctou_rotate") })

	result, err := service.SubmitXCodeRevokeOperation(input)
	require.EqualValues(t, 1, raced.Load(), "the race must have been injected between the snapshot read and the transaction")
	require.ErrorIs(t, err, service.ErrRefreshTokenInvalid)
	assert.Empty(t, result)
	// 事务确实被打开过，说明预检（旧快照）放行了，拒绝来自事务内的锁下复核。
	assert.EqualValues(t, 1, pool.beginAttempts.Load(), "the stale snapshot must pass the pre-check so that the in-lock re-check is what rejects")

	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status, "a credential that went stale after the pre-check must not revoke the session")
	key, err := model.GetTokenById(bundle.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusEnabled, key.Status)
	var records int64
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&records).Error)
	assert.Zero(t, records)
}

func TestXCodeRevokeOperationCommitOutcomeUnknown(t *testing.T) {
	for _, applied := range []bool{true, false} {
		name := "commit not applied"
		if applied {
			name = "commit applied but acknowledgement lost"
		}
		t.Run(name, func(t *testing.T) {
			_, dashboard := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
			require.NoError(t, err)
			control, err := service.ParseXCodeAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			server := enableXCodeAuthTestRedis(t, dashboard.UserID)

			base := model.DB
			sqlDB, err := base.DB()
			require.NoError(t, err)
			pool := &commitFaultPool{DB: sqlDB, applied: applied}
			faulty := base.WithContext(context.Background())
			faulty.Statement.ConnPool, faulty.Config.ConnPool = pool, pool
			model.DB = faulty
			t.Cleanup(func() { model.DB = base })

			input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
			// 经真实 HTTP 处理器提交：无论提交是否真正生效，本次 submit 都只能返回
			// 503 XCODE_BACKEND_UNAVAILABLE，绝不返回 committed。
			body, err := common.Marshal(input)
			require.NoError(t, err)
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/xcode/v1/auth/revoke-operations", bytes.NewReader(body))
			pool.failing.Store(true)
			SubmitXCodeRevokeOperation(c)
			pool.failing.Store(false)
			assert.Equal(t, http.StatusServiceUnavailable, response.Code)
			assert.Contains(t, response.Body.String(), "XCODE_BACKEND_UNAVAILABLE")
			assert.NotContains(t, response.Body.String(), "committed")
			assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			assert.EqualValues(t, 1, pool.commitAttempts.Load(), "the failure must come from the COMMIT stage, not an earlier step")

			stored, err := model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			key, err := model.GetTokenById(bundle.Token.Id)
			require.NoError(t, err)
			var records int64
			require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&records).Error)
			queried, queryErr := service.QueryXCodeRevokeOperation(input)
			_, cachedErr := model.GetUserSessionCached(bundle.Session.SID)
			assert.ErrorIs(t, cachedErr, model.ErrUserSessionInactive, "the pre-commit fence still fails closed")

			if applied {
				assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
				assert.Equal(t, common.TokenStatusDisabled, key.Status)
				assert.EqualValues(t, 1, records)
				require.NoError(t, queryErr, "the client confirms the outcome with query")
				assert.Equal(t, "committed", queried)
				// 即使 Redis 栅栏过期，主库已提交的撤销仍使旧凭据失效。
				server.FastForward(2 * time.Hour)
				_, cachedErr = model.GetUserSessionCached(bundle.Session.SID)
				assert.ErrorIs(t, cachedErr, model.ErrUserSessionInactive)
				_, _, validateErr := service.ValidateXCodeLoginSession(control)
				assert.ErrorIs(t, validateErr, service.ErrLoginSessionRevoked)
			} else {
				assert.Equal(t, model.UserSessionStatusActive, stored.Status)
				assert.Equal(t, common.TokenStatusEnabled, key.Status)
				assert.Zero(t, records)
				assert.ErrorIs(t, queryErr, service.ErrRefreshTokenInvalid, "no record means the outcome cannot be inferred as success")
				// 栅栏只是保守拒绝：过期后未提交的会话恢复有效，没有任何权威状态被破坏。
				server.FastForward(2 * time.Hour)
				_, cachedErr = model.GetUserSessionCached(bundle.Session.SID)
				assert.NoError(t, cachedErr)
			}

			// 故障排除后用同一 operation_id 重试：幂等地得到 committed，且只有一条记录。
			result, err := service.SubmitXCodeRevokeOperation(input)
			require.NoError(t, err)
			assert.Equal(t, "committed", result)
			require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&records).Error)
			assert.EqualValues(t, 1, records)
			stored, err = model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
			key, err = model.GetTokenById(bundle.Token.Id)
			require.NoError(t, err)
			assert.Equal(t, common.TokenStatusDisabled, key.Status)
		})
	}
}

func TestXCodeRevokeOperationRedisFailureRollsBack(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	server := miniredis.RunT(t)
	previousClient := common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RedisEnabled = false; common.RDB = previousClient; _ = client.Close() })
	server.Close()
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	_, err = service.SubmitXCodeRevokeOperation(input)
	require.Error(t, err)
	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status)
	var count int64
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestXCodeRevokeOperationHTTPAndSecretProtection(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	input := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	body, err := common.Marshal(input)
	require.NoError(t, err)
	for _, handler := range []gin.HandlerFunc{SubmitXCodeRevokeOperation, QueryXCodeRevokeOperation} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/xcode/v1/auth/revoke-operations", bytes.NewReader(body))
		handler(c)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		assert.NotContains(t, response.Body.String(), bundle.RefreshToken)
		assert.Contains(t, response.Body.String(), `"committed"`)
	}
	var operations []model.XCodeRevokeOperation
	require.NoError(t, model.DB.Find(&operations).Error)
	require.Len(t, operations, 1)
	assert.NotContains(t, operations[0].CredentialHash, bundle.RefreshToken)
	assert.NotEqual(t, input.OperationID, operations[0].OperationHash)
	assert.NotEqual(t, bundle.Session.RefreshHash, operations[0].CredentialHash)
	previousSecret := common.SessionSecret
	common.SessionSecret = "different-test-secret"
	_, err = service.QueryXCodeRevokeOperation(input)
	common.SessionSecret = previousSecret
	require.Error(t, err)
	result, err := service.QueryXCodeRevokeOperation(input)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
}

// recordQueriedTables 记录回调期间 GORM 查询的表名序列，用于证明不同失败原因
// 经过完全相同的数据库访问路径。
func recordQueriedTables(t *testing.T, name string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var tables []string
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		mu.Lock()
		defer mu.Unlock()
		tables = append(tables, tx.Statement.Table)
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Query().Remove(name) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		observed := append([]string(nil), tables...)
		tables = tables[:0]
		return observed
	}
}

func TestXCodeRevokeOperationQueryFailuresAreUniform(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	other, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	committed := service.XCodeRevokeOperationRequest{SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
	_, err = service.SubmitXCodeRevokeOperation(committed)
	require.NoError(t, err)

	// 另一个会话提交后让记录过期：凭据正确但结果保留期已过。
	expiredBundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	expired := service.XCodeRevokeOperationRequest{SessionID: expiredBundle.Session.SID, RefreshToken: expiredBundle.RefreshToken, ClientID: "xcode-desktop", OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	_, err = service.SubmitXCodeRevokeOperation(expired)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Where("session_id = ?", expiredBundle.Session.SID).Update("expires_at", 1).Error)

	wrongCredential := committed
	wrongCredential.RefreshToken = bundle.Session.SID + ".not-the-issued-secret"
	wrongClient := committed
	wrongClient.ClientID = "xwork-cli"
	unknownID := committed
	unknownID.OperationID = "fedcba9876543210fedcba9876543210"
	otherSessionSameID := service.XCodeRevokeOperationRequest{SessionID: other.Session.SID, RefreshToken: other.RefreshToken, ClientID: "xcode-desktop", OperationID: committed.OperationID}
	cases := map[string]service.XCodeRevokeOperationRequest{
		"wrong credential for an existing operation":         wrongCredential,
		"wrong client for an existing operation":             wrongClient,
		"operation id that was never submitted":              unknownID,
		"same id used by another session that never ran it":  otherSessionSameID,
		"correct credential but the retained record expired": expired,
	}

	// 三种失败必须给出完全相同的 HTTP 状态、响应体（除 request_id）和数据库访问序列。
	type observation struct {
		status int
		body   string
		tables []string
	}
	drain := recordQueriedTables(t, "query_uniformity_recorder")
	observe := func(input service.XCodeRevokeOperationRequest) observation {
		body, err := common.Marshal(input)
		require.NoError(t, err)
		drain()
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest(http.MethodPost, "/api/xcode/v1/auth/revoke-operations/query", bytes.NewReader(body))
		QueryXCodeRevokeOperation(c)
		return observation{status: response.Code, body: response.Body.String(), tables: drain()}
	}
	var baseline *observation
	for name, input := range cases {
		got := observe(input)
		assert.NotEqual(t, http.StatusOK, got.status, name)
		assert.NotContains(t, got.body, "committed", name)
		assert.NotContains(t, got.body, bundle.RefreshToken, name)
		// 固定期望序列，防止记录器失效时两个空序列“相等”造成空洞通过。
		assert.Equal(t, []string{"x_code_revoke_operations"}, got.tables, "%s must issue exactly one keyed read", name)
		if baseline == nil {
			baseline = &got
			continue
		}
		assert.Equal(t, baseline.status, got.status, name)
		assert.Equal(t, baseline.body, got.body, name)
		assert.Equal(t, baseline.tables, got.tables, "%s must follow the same database access path", name)
	}
	require.NotNil(t, baseline)
	// 已提交的合法查询仍可读到结果，说明上面的失败不是因为路径整体不可用。
	result, err := service.QueryXCodeRevokeOperation(committed)
	require.NoError(t, err)
	assert.Equal(t, "committed", result)
}

func TestXCodeRevokeOperationRejectsNonCanonicalSessionID(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	upper := strings.ToUpper(bundle.Session.SID)
	secret := strings.TrimPrefix(bundle.RefreshToken, bundle.Session.SID+".")
	for name, sid := range map[string]string{"upper case": upper, "braced": "{" + bundle.Session.SID + "}", "urn": "urn:uuid:" + bundle.Session.SID} {
		input := service.XCodeRevokeOperationRequest{SessionID: sid, RefreshToken: sid + "." + secret, ClientID: "xcode-desktop", OperationID: "0123456789abcdef0123456789abcdef"}
		_, err := service.SubmitXCodeRevokeOperation(input)
		assert.ErrorIs(t, err, service.ErrRefreshTokenInvalid, name)
		_, err = service.QueryXCodeRevokeOperation(input)
		assert.ErrorIs(t, err, service.ErrRefreshTokenInvalid, name)
	}
	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, stored.Status, "a non-canonical spelling must not revoke or create a second key space")
	var count int64
	require.NoError(t, model.DB.Model(&model.XCodeRevokeOperation{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestXCodeEntryConfigurationRejectsUntrustedOrigins(t *testing.T) {
	t.Setenv("XCODE_INTEGRATION_ENABLED", "")
	assert.False(t, common.XCodeIntegrationEnabled())
	t.Setenv("XCODE_INTEGRATION_ENABLED", "true")
	assert.True(t, common.XCodeIntegrationEnabled())
	for _, test := range []struct{ input, want string }{
		{"https://gateway.example", "https://gateway.example"},
		{" https://gateway.example/ ", "https://gateway.example"},
		{"http://localhost:3001", "http://localhost:3001"},
		{"http://127.0.0.1:3001/", "http://127.0.0.1:3001"},
		{"http://[::1]:3001", "http://[::1]:3001"},
		{"", ""}, {"http://gateway.example", ""}, {"https://user:secret@gateway.example", ""},
		{"https://gateway.example/path", ""}, {"https://gateway.example?query=1", ""},
		{"https://gateway.example#fragment", ""}, {"https:gateway.example", ""},
		{"ftp://gateway.example", ""}, {"https://gateway.example:invalid", ""},
		{"https://gateway.example:70000", ""}, {"http://localhost.attacker.example", ""},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Setenv("XCODE_PUBLIC_ORIGIN", test.input)
			origin, err := common.GetXCodePublicOrigin()
			if test.want == "" {
				assert.Error(t, err)
				assert.Empty(t, origin)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.want, origin)
			}
		})
	}
}

func TestXCodeCodeHonorsPrecommitAuthorityFences(t *testing.T) {
	for _, fence := range []string{"source SID", "user auth floor"} {
		t.Run(fence, func(t *testing.T) {
			user, identity := setupXCodeAuthTest(t)
			input := xcodeCodeForIdentity(t, identity)
			server := miniredis.RunT(t)
			previousClient := common.RDB
			client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			common.RedisEnabled, common.RDB = true, client
			t.Cleanup(func() {
				common.RedisEnabled = false
				common.RDB = previousClient
				require.NoError(t, client.Close())
			})
			require.NoError(t, model.PublishUserAuthCache(user.Id))
			if fence == "user auth floor" {
				require.NoError(t, model.SetUserAuthVersionFence(user.Id, user.AuthVersion+1))
			} else {
				snapshot, err := model.GetUserSessionBySID(identity.SessionID)
				require.NoError(t, err)
				require.NoError(t, model.PublishCreatedUserSession(snapshot, model.UserSessionCacheDeadline()))
				keys := server.Keys()
				var cacheKey string
				for _, key := range keys {
					if strings.HasPrefix(key, "auth:session:") {
						cacheKey = key
					}
				}
				require.NotEmpty(t, cacheKey)
				server.HSet(cacheKey, "Status", model.UserSessionStatusRevoking)
			}
			_, err := service.ExchangeXCodeAuthorizationCode(input, "", "")
			assert.ErrorIs(t, err, service.ErrXCodeAuthCodeInvalid)
			var count int64
			require.NoError(t, model.DB.Model(&model.Token{}).Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

func TestXCodeSessionAuthorityAcrossDatabaseDialects(t *testing.T) {
	for _, field := range []string{"auth_version", "status"} {
		t.Run(field, func(t *testing.T) {
			user, dashboard := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
			require.NoError(t, err)
			enableXCodeAuthTestRedis(t, user.Id)
			identity, err := service.ParseXCodeAccessToken(bundle.AccessToken)
			require.NoError(t, err)
			value := any(user.AuthVersion + 1)
			if field == "status" {
				value = common.UserStatusDisabled
			}
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update(field, value).Error)
			_, _, err = service.ValidateXCodeLoginSession(identity)
			assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
		})
	}
}

func TestXCodeRefreshReplayRedisOutageAcrossDatabaseDialects(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	input := service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}
	_, err = service.RefreshXCodeSession(input, "", "")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", 1).Error)
	server := miniredis.RunT(t)
	previousClient := common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() { common.RedisEnabled = false; common.RDB = previousClient; _ = client.Close() })
	server.SetError("injected Redis outage")
	_, err = service.RefreshXCodeSession(input, "", "")
	require.ErrorIs(t, err, service.ErrLoginSessionRevoked)
	server.SetError("")
	stored, err := model.GetUserSessionBySID(bundle.Session.SID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
	key, err := model.GetTokenById(bundle.Token.Id)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, key.Status)
}

// 数据库与 Redis 同时故障是无法持久化的物理边界：本测试固定的是失败关闭行为，
// 而不是“恢复后自动补做撤销”。
func TestXCodeRefreshReplayDuringOutageFailsClosed(t *testing.T) {
	type outage struct {
		name string
		// apply 返回恢复函数。
		apply func(pool *commitFaultPool) (restore func())
	}
	for _, scenario := range []outage{
		{"database unreachable", func(pool *commitFaultPool) func() {
			pool.unavailable.Store(true)
			return func() { pool.unavailable.Store(false) }
		}},
		{"database readable but revocation transaction cannot be persisted", func(pool *commitFaultPool) func() {
			pool.transactionsDown.Store(true)
			return func() { pool.transactionsDown.Store(false) }
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, dashboard := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
			require.NoError(t, err)
			oldInput := service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}
			rotated, err := service.RefreshXCodeSession(oldInput, "", "")
			require.NoError(t, err)
			control, err := service.ParseXCodeAccessToken(rotated.AccessToken)
			require.NoError(t, err)
			// 旧代凭据已超出并发宽限期：再次出现即属于已识别的重放。
			require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", 1).Error)
			server := enableXCodeAuthTestRedis(t, dashboard.UserID)

			base := model.DB
			sqlDB, err := base.DB()
			require.NoError(t, err)
			pool := &commitFaultPool{DB: sqlDB}
			faulty := base.WithContext(context.Background())
			faulty.Statement.ConnPool, faulty.Config.ConnPool = pool, pool
			model.DB = faulty
			t.Cleanup(func() { model.DB = base })

			server.SetError("injected Redis outage")
			restore := scenario.apply(pool)
			replay, err := service.RefreshXCodeSession(oldInput, "", "")
			// 重放请求本身返回故障错误，不签发任何凭据；也不得宣称会话已撤销。
			require.ErrorIs(t, err, errInjectedDatabaseDown)
			assert.NotErrorIs(t, err, service.ErrLoginSessionRevoked)
			assert.Nil(t, replay)
			if scenario.name == "database unreachable" {
				// 数据库不可达时所有授权同样失败关闭，包括仍在有效期内的当前凭据。
				_, _, err = service.ValidateXCodeLoginSession(control)
				require.Error(t, err)
				_, err = model.GetUserSessionCached(bundle.Session.SID)
				require.Error(t, err)
			}

			restore()
			server.SetError("")
			// 不承诺故障期间已持久记录泄漏：恢复后会话仍为 active，不会被自动补做撤销。
			stored, err := model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			assert.Equal(t, model.UserSessionStatusActive, stored.Status)
			// 当前代凭据按原有规则继续可用。
			_, _, err = service.ValidateXCodeLoginSession(control)
			require.NoError(t, err)
			// 故障恢复后，同一旧代凭据的新一次重放按既定规则撤销会话（这是新的请求，
			// 不是对故障期间事件的补偿）。
			_, err = service.RefreshXCodeSession(oldInput, "", "")
			require.ErrorIs(t, err, service.ErrLoginSessionRevoked)
			stored, err = model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
			key, err := model.GetTokenById(bundle.Token.Id)
			require.NoError(t, err)
			assert.Equal(t, common.TokenStatusDisabled, key.Status)
		})
	}
}

func TestXCodeRefreshRecoveryAndReplayRemainSIDBound(t *testing.T) {
	_, dashboard := setupXCodeAuthTest(t)
	bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, dashboard), "", "")
	require.NoError(t, err)
	input := service.XCodeRefreshRequest{ClientID: "xcode-desktop", SessionID: bundle.Session.SID, RefreshToken: bundle.RefreshToken}
	rotated, err := service.RefreshXCodeSession(input, "", "")
	require.NoError(t, err)
	recovered, err := service.RefreshXCodeSession(input, "", "")
	require.NoError(t, err)
	assert.Equal(t, rotated.RefreshToken, recovered.RefreshToken)
	assert.Equal(t, bundle.Token.Key, recovered.Token.Key)
	require.NoError(t, model.DB.Model(&model.UserSession{}).Where("sid = ?", bundle.Session.SID).Update("previous_valid_until", time.Now().Add(-time.Minute).Unix()).Error)
	_, err = service.RefreshXCodeSession(input, "", "")
	assert.ErrorIs(t, err, service.ErrLoginSessionRevoked)
	native, err := model.GetUserSessionBySID(dashboard.SessionID)
	require.NoError(t, err)
	assert.Equal(t, model.UserSessionStatusActive, native.Status)
}

func TestXCodeLogoutRefreshCleansDeletedOrDisabledManagedKey(t *testing.T) {
	for _, action := range []string{"delete", "disable"} {
		t.Run(action, func(t *testing.T) {
			_, identity := setupXCodeAuthTest(t)
			bundle, err := service.ExchangeXCodeAuthorizationCode(xcodeCodeForIdentity(t, identity), "", "")
			require.NoError(t, err)
			if action == "delete" {
				require.NoError(t, bundle.Token.Delete())
			} else {
				bundle.Token.Status = common.TokenStatusDisabled
				require.NoError(t, bundle.Token.SelectUpdate())
			}
			require.NoError(t, service.LogoutXCodeSession(nil, service.XCodeLogoutRequest{
				SessionID: bundle.Session.SID, ClientID: "xcode-desktop", RefreshToken: bundle.RefreshToken,
			}))
			stored, err := model.GetUserSessionBySID(bundle.Session.SID)
			require.NoError(t, err)
			assert.Equal(t, model.UserSessionStatusRevoked, stored.Status)
		})
	}
}
