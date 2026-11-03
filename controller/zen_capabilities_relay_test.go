package controller

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestZenThinkingRelayConformance(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousStreamingTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousStreamingTimeout })
	user := zenCatalogFixture(t, db)
	now := model.GetDBTimestamp()
	require.NoError(t, db.Model(&user).Update("auth_version", 1).Error)
	sub := model.UserSubscription{UserId: user.Id, UsageScope: model.SubscriptionUsageScopeZen, Source: "admin", Status: "active", StartTime: now - 1, EndTime: now + 3600, AmountTotal: 10000000, QuotaResetPeriodSnapshot: model.SubscriptionResetNever}
	require.NoError(t, db.Create(&sub).Error)
	require.NoError(t, db.Create(&model.ZenAuthorization{UserId: user.Id, SubscriptionId: sub.Id, Revision: 1}).Error)
	require.NoError(t, db.Create(&model.UserSession{SID: "thinking-relay-session", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, ClientPurpose: model.ClientPurposeZen, RefreshHash: strings.Repeat("a", 64), LoginMethod: "password", ExpiresAt: now + 3600}).Error)
	key := "thinkingrelayisolated"
	require.NoError(t, db.Create(&model.Token{UserId: user.Id, Key: key, Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: now + 3600, ClientPurpose: model.ClientPurposeZen, ClientSessionSID: "thinking-relay-session"}).Error)
	previousGroups, previousRatios := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString()
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"catalog":"Catalog"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"catalog":1}`))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"chat":1,"fixture-upstream":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"chat":1,"fixture-upstream":1}`))
	service.InitHttpClient()
	var mutex sync.Mutex
	var bodies []map[string]any
	canceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid fixture request", 400)
			return
		}
		mutex.Lock()
		bodies = append(bodies, body)
		mutex.Unlock()
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			emit := func(delta any) {
				data, _ := json.Marshal(map[string]any{"id": "fixture-stream", "object": "chat.completion.chunk", "created": 1, "model": "fixture-upstream", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\n", data)
				w.(http.Flusher).Flush()
			}
			emit(map[string]any{"role": "assistant", "reasoning_content": "fixture reasoning"})
			// The relay buffers its last chunk; a second chunk makes the first observable before cancellation.
			emit(map[string]any{"reasoning_content": " continued"})
			<-r.Context().Done()
			close(canceled)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture-complete","object":"chat.completion","created":1,"model":"fixture-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"fixture complete"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`)
	}))
	t.Cleanup(upstream.Close)
	configure := func(channelType int, passThrough bool, override string) {
		channel, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		channel.Type = channelType
		channel.BaseURL = common.GetPointer(upstream.URL)
		if channelType == constant.ChannelTypeDeepSeek {
			channel.BaseURL = common.GetPointer(upstream.URL + "/v1")
		}
		channel.ModelMapping = common.GetPointer(`{"chat":"fixture-upstream"}`)
		channel.ParamOverride = common.GetPointer(override)
		channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: passThrough})
		require.NoError(t, db.Save(channel).Error)
	}
	var serial atomic.Int32
	router := gin.New()
	router.Use(middleware.BodyStorageCleanup())
	router.POST("/v1/chat/completions", middleware.RouteTag("relay"), middleware.TokenAuth(), ZenModelAdmission, middleware.Distribute(), func(c *gin.Context) {
		c.Set(common.RequestIdKey, fmt.Sprintf("isolated-thinking-%d", serial.Add(1)))
		Relay(c, types.RelayFormatOpenAI)
	})
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	for _, tc := range []struct {
		name                                   string
		kind                                   int
		pass                                   bool
		override, expectedModel, expectedLevel string
	}{
		{"deepseek converter and alias", constant.ChannelTypeDeepSeek, false, "{}", "fixture-upstream", "high"},
		{"channel parameter override", constant.ChannelTypeOpenAI, false, `{"reasoning_effort":"max"}`, "fixture-upstream", "max"},
		{"raw pass-through", constant.ChannelTypeOpenAI, true, "{}", "chat", "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			configure(tc.kind, tc.pass, tc.override)
			request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"chat","max_tokens":48,"thinking":{"type":"enabled"},"reasoning_effort":"high","messages":[{"role":"user","content":"synthetic fixture"},{"role":"assistant","content":null,"reasoning_content":"fixture reasoning","tool_calls":[{"id":"lookup-1","type":"function","function":{"name":"lookup","arguments":"{\"query\":\"fixture\"}"}}]},{"role":"tool","tool_call_id":"lookup-1","content":"fixture tool result"}]}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+key)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, 200, response.StatusCode, string(data))
			mutex.Lock()
			body := bodies[len(bodies)-1]
			mutex.Unlock()
			require.Equal(t, tc.expectedModel, body["model"])
			require.Equal(t, tc.expectedLevel, body["reasoning_effort"])
			require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
			messages := body["messages"].([]any)
			assistant := messages[1].(map[string]any)
			tool := messages[2].(map[string]any)
			require.Equal(t, "fixture reasoning", assistant["reasoning_content"])
			require.Equal(t, "lookup-1", tool["tool_call_id"])
			require.Equal(t, "fixture tool result", tool["content"])
		})
	}
	t.Run("streamed reasoning and prompt client cancellation", func(t *testing.T) {
		configure(constant.ChannelTypeDeepSeek, false, "{}")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(`{"model":"chat","max_tokens":48,"stream":true,"thinking":{"type":"enabled"},"reasoning_effort":"high","messages":[{"role":"user","content":"cancel fixture"}]}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+key)
		response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
		require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		reader := bufio.NewReader(response.Body)
		for {
			line, err := reader.ReadString('\n')
			require.NoError(t, err)
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var chunk dto.ChatCompletionsStreamResponse
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &chunk))
			require.Len(t, chunk.Choices, 1)
			require.Equal(t, "fixture reasoning", chunk.Choices[0].Delta.GetReasoningContent())
			break
		}
		mutex.Lock()
		body := bodies[len(bodies)-1]
		mutex.Unlock()
		require.Equal(t, "fixture-upstream", body["model"])
		require.Equal(t, "high", body["reasoning_effort"])
		require.Equal(t, map[string]any{"type": "enabled"}, body["thinking"])
		select {
		case <-canceled:
			t.Fatal("upstream ended before client cancellation")
		default:
		}
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
		cancel()
		select {
		case <-canceled:
		case <-deadline.C:
			t.Fatal("upstream did not observe client cancellation within 1s (relay idle timeout is 10s)")
		}
	})
}

func TestZenFinalOutputLimitAfterChannelOverride(t *testing.T) {
	db := modelManagementDB(t, "sqlite", "")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// 单连接避免并发测试写入 SQLite；退款等待直接使用 database/sql，避免 GORM 语句缓存互锁。
	sqlDB.SetMaxOpenConns(1)
	waitForRefund := func(t *testing.T, requestID string) int64 {
		t.Helper()
		var status string
		var amount int64
		require.Eventually(t, func() bool {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			err := sqlDB.QueryRowContext(ctx, "SELECT status, pre_consumed FROM subscription_pre_consume_records WHERE request_id = ?", requestID).Scan(&status, &amount)
			return err == nil && status == "refunded"
		}, 3*time.Second, 10*time.Millisecond, "预扣失败后必须完成退款")
		require.Positive(t, amount, "必须验证实际非零预扣的退款")
		return amount
	}
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	user := zenCatalogFixture(t, db)
	now := model.GetDBTimestamp()
	require.NoError(t, db.Model(&user).Update("auth_version", 1).Error)
	subscription := model.UserSubscription{UserId: user.Id, UsageScope: model.SubscriptionUsageScopeZen, Source: "admin", Status: "active", StartTime: now - 1, EndTime: now + 3600, AmountTotal: 10000000, QuotaResetPeriodSnapshot: model.SubscriptionResetNever}
	require.NoError(t, db.Create(&subscription).Error)
	require.NoError(t, db.Create(&model.ZenAuthorization{UserId: user.Id, SubscriptionId: subscription.Id, Revision: 1}).Error)
	const sessionID = "output-limit-fixture-session"
	require.NoError(t, db.Create(&model.UserSession{SID: sessionID, UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, ClientPurpose: model.ClientPurposeZen, RefreshHash: strings.Repeat("a", 64), LoginMethod: "password", ExpiresAt: now + 3600}).Error)
	secret := make([]byte, 24)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	key := hex.EncodeToString(secret)
	require.NoError(t, db.Create(&model.Token{UserId: user.Id, Key: key, Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: now + 3600, ClientPurpose: model.ClientPurposeZen, ClientSessionSID: sessionID}).Error)
	nativeKey := key + "ordinary"
	require.NoError(t, db.Create(&model.Token{UserId: user.Id, Key: nativeKey, Status: common.TokenStatusEnabled, UnlimitedQuota: true, ExpiredTime: now + 3600}).Error)

	previousGroups, previousRatios, previousRetries := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), common.RetryTimes
	previousCountToken := constant.CountToken
	constant.CountToken = true
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"catalog":"Catalog"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"catalog":1}`))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"chat":1,"responses":1,"claude":1,"fixture-upstream":1}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"chat":1,"responses":1,"claude":1,"fixture-upstream":1}`))
	common.RetryTimes = 1
	t.Cleanup(func() {
		common.RetryTimes = previousRetries
		constant.CountToken = previousCountToken
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousRatios))
	})

	service.InitHttpClient()
	client := &http.Client{Timeout: 5 * time.Second}
	var mutex sync.Mutex
	var requests []map[string]any
	var requestBytes [][]byte
	var failFirstRetryAttempt, failRevocationAttempt, failResponsesAttempt atomic.Bool
	responsesStarted, responsesRelease := make(chan struct{}), make(chan struct{})
	var responsesReleaseOnce sync.Once
	releaseResponses := func() { responsesReleaseOnce.Do(func() { close(responsesRelease) }) }
	firstRetrySubmitted := make(chan struct{})
	allowRetryResponse := make(chan struct{})
	firstRevocationSubmitted := make(chan struct{})
	allowRevocationResponse := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body map[string]any
		if err := common.Unmarshal(raw, &body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mutex.Lock()
		requests = append(requests, body)
		requestBytes = append(requestBytes, raw)
		mutex.Unlock()
		if failResponsesAttempt.CompareAndSwap(true, false) {
			close(responsesStarted)
			<-responsesRelease
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"fixture retry","type":"api_error","code":"provider_busy"}}`)
			return
		}
		if failFirstRetryAttempt.CompareAndSwap(true, false) {
			close(firstRetrySubmitted)
			<-allowRetryResponse
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"fixture retry","type":"api_error","code":"provider_busy"}}`)
			return
		}
		if failRevocationAttempt.CompareAndSwap(true, false) {
			close(firstRevocationSubmitted)
			<-allowRevocationResponse
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"fixture retry","type":"api_error","code":"provider_busy"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/responses":
			fmt.Fprint(w, `{"id":"response-fixture","object":"response","created_at":1,"status":"completed","model":"fixture-upstream","output":[],"usage":{"input_tokens":20,"output_tokens":8,"total_tokens":28}}`)
		case "/v1/messages":
			fmt.Fprint(w, `{"id":"message-fixture","type":"message","role":"assistant","model":"fixture-upstream","content":[{"type":"text","text":"fixture"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":8}}`)
		default:
			fmt.Fprint(w, `{"id":"chat-fixture","object":"chat.completion","created":1,"model":"fixture-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"fixture"},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":8,"total_tokens":28}}`)
		}
	}))
	t.Cleanup(upstream.Close)
	// DeepSeek 适配器在 BaseURL 后追加 /v1/chat/completions，故与既有 Thinking 测试一样使用 /v1 前缀。
	baseFor := func(channelType int) *string {
		if channelType == constant.ChannelTypeDeepSeek {
			return common.GetPointer(upstream.URL + "/v1")
		}
		return common.GetPointer(upstream.URL)
	}

	// 低优先级的合法渠道用于证明本地超限错误不会触发下一渠道重试。
	priority := int64(10)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", 1, "chat").Update("priority", priority).Error)
	fallback := model.Channel{Name: "output-limit-fallback", Type: constant.ChannelTypeOpenAI, Key: key, BaseURL: baseFor(constant.ChannelTypeOpenAI), Status: common.ChannelStatusEnabled, Group: "catalog", Models: "chat"}
	require.NoError(t, db.Create(&fallback).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "catalog", Model: "chat", ChannelId: fallback.Id, Enabled: true}).Error)

	type protocol struct {
		name, path, model, field string
		channelID, channelType   int
		format                   types.RelayFormat
	}
	protocols := []protocol{
		{"chat-max-tokens", "/v1/chat/completions", "chat", "max_tokens", 1, constant.ChannelTypeOpenAI, types.RelayFormatOpenAI},
		{"chat-max-completion-tokens", "/v1/chat/completions", "chat", "max_completion_tokens", 1, constant.ChannelTypeOpenAI, types.RelayFormatOpenAI},
		{"deepseek-chat-max-tokens", "/v1/chat/completions", "chat", "max_tokens", 1, constant.ChannelTypeDeepSeek, types.RelayFormatOpenAI},
		{"responses", "/v1/responses", "responses", "max_output_tokens", 3, constant.ChannelTypeOpenAI, types.RelayFormatOpenAIResponses},
		{"anthropic", "/v1/messages", "claude", "max_tokens", 2, constant.ChannelTypeAnthropic, types.RelayFormatClaude},
	}
	router := gin.New()
	var policyEvents sync.Map
	var liveSDKRequests atomic.Int32
	liveSDKDone := make(chan struct{}, 1)
	if os.Getenv("AISCOO_TEST_SDK_STREAM_FIXTURE") != "" || os.Getenv("AISCOO_TEST_SDK_RESPONSES_FIXTURE") != "" {
		router.POST("/__fixture_done", func(c *gin.Context) {
			liveSDKDone <- struct{}{}
			c.Status(http.StatusNoContent)
		})
		router.POST("/__fixture_release", func(c *gin.Context) {
			releaseResponses()
			c.Status(http.StatusNoContent)
		})
	}
	router.Use(middleware.BodyStorageCleanup(), func(c *gin.Context) {
		c.Next()
		policyEvents.Store(c.GetHeader("X-Fixture-Request-ID"), service.RequestPolicy(c).Events())
	})
	for _, endpoint := range []struct {
		path   string
		format types.RelayFormat
	}{{"/v1/chat/completions", types.RelayFormatOpenAI}, {"/v1/responses", types.RelayFormatOpenAIResponses}, {"/v1/messages", types.RelayFormatClaude}} {
		router.POST(endpoint.path, middleware.RouteTag("relay"), middleware.TokenAuth(), ZenModelAdmission, middleware.Distribute(), func(c *gin.Context) {
			c.Set(common.RequestIdKey, c.GetHeader("X-Fixture-Request-ID"))
			if c.GetHeader("X-Fixture-Committed-Stream") == "true" {
				if c.GetHeader("X-Fixture-SDK") == "true" {
					liveSDKRequests.Add(1)
				}
				// 模拟心跳先提交响应，随后仍由真实 Relay 完成转换和错误输出。
				c.Header("Content-Type", "text/event-stream")
				_, _ = c.Writer.WriteString(": PING\n\n")
				c.Writer.Flush()
			}
			Relay(c, endpoint.format)
		})
	}
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	var serial atomic.Int32

	for _, p := range protocols {
		for _, tc := range []struct {
			name                 string
			incoming, override   int
			passThrough          bool
			wantStatus, wantSent int
			wantRefund           bool
		}{
			{"override exceeds limit", 48, 129, false, http.StatusBadRequest, 0, true},
			{"override equals limit", 48, 128, false, http.StatusOK, 128, false},
			{"incoming exceeds limit even when override lowers it", 129, 128, false, http.StatusBadRequest, 0, false},
			{"raw passthrough keeps admitted value", 48, 129, true, http.StatusOK, 48, false},
		} {
			t.Run(p.name+"/"+tc.name, func(t *testing.T) {
				channel, err := model.GetChannelById(p.channelID, true)
				require.NoError(t, err)
				channel.Type = p.channelType
				channel.BaseURL = baseFor(channel.Type)
				channel.ModelMapping = common.GetPointer(`{"chat":"fixture-upstream","responses":"fixture-upstream","claude":"fixture-upstream"}`)
				channel.ParamOverride = common.GetPointer(fmt.Sprintf(`{"%s":%d}`, p.field, tc.override))
				channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: tc.passThrough})
				require.NoError(t, db.Save(channel).Error)
				var before model.UserSubscription
				require.NoError(t, db.First(&before, subscription.Id).Error)
				body := map[string]any{"model": p.model, p.field: tc.incoming}
				if p.format == types.RelayFormatOpenAIResponses {
					body["input"] = "synthetic fixture"
				} else {
					body["messages"] = []any{map[string]any{"role": "user", "content": "synthetic fixture"}}
				}
				encoded, err := common.Marshal(body)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, gateway.URL+p.path, strings.NewReader(string(encoded)))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer sk-"+key)
				requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
				request.Header.Set("X-Fixture-Request-ID", requestID)
				mutex.Lock()
				count := len(requests)
				mutex.Unlock()
				response, err := client.Do(request)
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				mutex.Lock()
				newRequests := append([]map[string]any(nil), requests[count:]...)
				newRequestBytes := append([][]byte(nil), requestBytes[count:]...)
				mutex.Unlock()
				if response.StatusCode != tc.wantStatus {
					t.Errorf("status = %d, want %d", response.StatusCode, tc.wantStatus)
				}
				if tc.wantSent == 0 {
					if len(newRequests) != 0 {
						t.Errorf("fake upstream received %d requests, want zero", len(newRequests))
					}
					if response.StatusCode != tc.wantStatus || !tc.wantRefund {
						return
					}
					waitForRefund(t, requestID)
					require.NoError(t, model.RefundZenSubscriptionRequest(requestID, user.Id, subscription.Id), "重复退款必须幂等")
					captured, exists := policyEvents.Load(requestID)
					require.True(t, exists)
					var localFailure, localStop bool
					for _, event := range captured.([]service.PolicyEvent) {
						localFailure = localFailure || event.ErrorSource == "local" && event.Decision.Reason == "local_rejection"
						localStop = localStop || event.Decision.Action == "stop" && event.Health == "unchanged"
						require.NotEqual(t, "retry", event.Decision.Action)
					}
					require.True(t, localFailure, "超限是本地策略拒绝")
					require.True(t, localStop, "本地拒绝不能改变渠道健康")
					var after model.UserSubscription
					require.NoError(t, db.First(&after, subscription.Id).Error)
					require.Equal(t, before.AmountUsed, after.AmountUsed)
					return
				}
				require.Len(t, newRequests, 1)
				require.Equal(t, float64(tc.wantSent), newRequests[0][p.field])
				if tc.passThrough {
					require.Equal(t, encoded, newRequestBytes[0], "透传校验不能改变最终正文")
				}
			})
		}
	}

	t.Run("raw ambiguous limits fail closed", func(t *testing.T) {
		channel, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		channel.Type = constant.ChannelTypeOpenAI
		channel.BaseURL = baseFor(channel.Type)
		channel.ModelMapping = common.GetPointer(`{}`)
		channel.ParamOverride = common.GetPointer(`{}`)
		channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: true})
		require.NoError(t, db.Save(channel).Error)
		for _, tc := range []struct{ name, fields string }{
			{"duplicate", `"max_tokens":129,"max_tokens":48`},
			{"escaped duplicate", `"max_tokens":129,"max\u005ftokens":48`},
			{"noncanonical limit key", `"MAX_TOKENS":48`},
			{"conflicting aliases", `"max_tokens":48,"max_completion_tokens":128`},
			{"duplicate candidate count", `"max_tokens":48,"n":2,"n":1`},
			{"folded duplicate candidate count", `"max_tokens":48,"N":2,"n":1`},
			{"noncanonical candidate count", `"max_tokens":48,"N":1`},
			{"zero", `"max_tokens":0`},
			{"missing", `"temperature":0.5`},
			{"decimal", `"max_tokens":48.0`},
			{"exponent", `"max_tokens":4.8e1`},
			{"string", `"max_tokens":"48"`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],` + tc.fields + `}`
				request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer sk-"+key)
				request.Header.Set("X-Fixture-Request-ID", fmt.Sprintf("output-limit-%d", serial.Add(1)))
				mutex.Lock()
				count := len(requests)
				mutex.Unlock()
				response, err := client.Do(request)
				require.NoError(t, err)
				responseBody, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				mutex.Lock()
				sent := len(requests) - count
				mutex.Unlock()
				require.Equal(t, http.StatusBadRequest, response.StatusCode, string(responseBody))
				require.Zero(t, sent, "语义不确定时不能提交生成请求")
			})
		}
	})

	// 残留风险的特征测试：本方案只校验宿主发出的最终请求参数，不证明上游遵守该字段。
	// 自定义网关即使静默忽略限额字段，托管请求仍照常放行；这里断言宿主发出的参数符合上限，
	// 并如实记录“网关实际生成量不受保证”。见 docs/specs/zen-integration.md §6.5。
	t.Run("custom gateway is not blocked and its compliance is not guaranteed", func(t *testing.T) {
		for _, p := range []protocol{protocols[0], protocols[3], protocols[4]} {
			t.Run(p.name, func(t *testing.T) {
				var received atomic.Int32
				var sentLimit atomic.Int64
				ignoring := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					received.Add(1)
					var body map[string]any
					if err := common.DecodeJson(r.Body, &body); err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					if value, ok := body[p.field].(float64); ok {
						sentLimit.Store(int64(value))
					}
					// 该网关忽略限额字段，实际使用更大的内部默认值并如数计量。
					w.Header().Set("Content-Type", "application/json")
					switch p.format {
					case types.RelayFormatOpenAIResponses:
						fmt.Fprint(w, `{"id":"response-fixture","object":"response","status":"completed","model":"fixture-upstream","output":[],"usage":{"input_tokens":1,"output_tokens":1024,"total_tokens":1025}}`)
					case types.RelayFormatClaude:
						fmt.Fprint(w, `{"id":"message-fixture","type":"message","role":"assistant","model":"fixture-upstream","content":[{"type":"text","text":"fixture"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1024}}`)
					default:
						fmt.Fprint(w, `{"id":"chat-fixture","object":"chat.completion","created":1,"model":"fixture-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"fixture"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1024,"total_tokens":1025}}`)
					}
				}))
				defer ignoring.Close()
				channel, err := model.GetChannelById(p.channelID, true)
				require.NoError(t, err)
				channel.Type = p.channelType
				channel.BaseURL = common.GetPointer(ignoring.URL)
				channel.ParamOverride = common.GetPointer(`{}`)
				channel.SetSetting(dto.ChannelSettings{})
				require.NoError(t, db.Save(channel).Error)
				body := map[string]any{"model": p.model, p.field: 48}
				if p.format == types.RelayFormatOpenAIResponses {
					body["input"] = "fixture"
				} else {
					body["messages"] = []any{map[string]any{"role": "user", "content": "fixture"}}
				}
				encoded, err := common.Marshal(body)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, gateway.URL+p.path, strings.NewReader(string(encoded)))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer sk-"+key)
				request.Header.Set("X-Fixture-Request-ID", fmt.Sprintf("output-limit-%d", serial.Add(1)))
				response, err := client.Do(request)
				require.NoError(t, err)
				data, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.Equal(t, http.StatusOK, response.StatusCode, string(data))
				require.Equal(t, int32(1), received.Load(), "自定义网关渠道照常可用")
				require.Equal(t, int64(48), sentLimit.Load(), "宿主发出的参数符合准入上限")
			})
		}
	})

	t.Run("unicode folded limit aliases cannot bypass admission or final validation", func(t *testing.T) {
		for _, target := range []struct {
			protocol          protocol
			canonical, folded string
		}{
			{protocols[0], "max_tokens", "max_tokenſ"},
			{protocols[1], "max_completion_tokens", "max_completion_tokenſ"},
			{protocols[3], "max_output_tokens", "max_output_tokenſ"},
			{protocols[4], "max_tokens", "max_tokenſ"},
			{protocols[4], "max_tokens_to_sample", "max_tokenſ_to_sample"},
		} {
			for _, pass := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/passthrough=%t", target.protocol.name, target.canonical, pass), func(t *testing.T) {
					p := target.protocol
					channel, err := model.GetChannelById(p.channelID, true)
					require.NoError(t, err)
					channel.Type = p.channelType
					channel.BaseURL = baseFor(channel.Type)
					channel.ParamOverride = common.GetPointer(fmt.Sprintf(`{%q:129}`, target.folded))
					channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: pass})
					require.NoError(t, db.Save(channel).Error)
					mainLimit := ""
					if target.canonical == "max_tokens_to_sample" {
						mainLimit = `,"max_tokens":48`
					}
					foldedLimit := ""
					if pass {
						// 最后一个规范字段让 Go 入站 DTO 看见 48，其他解析器仍可能读取前面的 129。
						foldedLimit = fmt.Sprintf(`,%q:129`, target.folded)
					}
					input := `,"messages":[{"role":"user","content":"fixture"}]`
					if p.format == types.RelayFormatOpenAIResponses {
						input = `,"input":"fixture"`
					}
					body := fmt.Sprintf(`{"model":%q%s%s%s,%q:48}`, p.model, input, mainLimit, foldedLimit, target.canonical)
					request, err := http.NewRequest(http.MethodPost, gateway.URL+p.path, strings.NewReader(body))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Authorization", "Bearer sk-"+key)
					requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
					request.Header.Set("X-Fixture-Request-ID", requestID)
					mutex.Lock()
					count := len(requests)
					mutex.Unlock()
					response, err := client.Do(request)
					require.NoError(t, err)
					data, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					mutex.Lock()
					sent := len(requests) - count
					raw := append([][]byte(nil), requestBytes[count:]...)
					mutex.Unlock()
					if sent > 0 && !pass {
						// 真实下游若使用 Go 的大小写折叠，渠道覆盖可变成有效的 129。
						switch p.format {
						case types.RelayFormatOpenAI:
							var parsed dto.GeneralOpenAIRequest
							require.NoError(t, common.Unmarshal(raw[0], &parsed))
							if target.canonical == "max_completion_tokens" {
								t.Errorf("fake upstream parsed max_completion_tokens=%d", *parsed.MaxCompletionTokens)
							} else {
								t.Errorf("fake upstream parsed max_tokens=%d", *parsed.MaxTokens)
							}
						case types.RelayFormatOpenAIResponses:
							var parsed dto.OpenAIResponsesRequest
							require.NoError(t, common.Unmarshal(raw[0], &parsed))
							t.Errorf("fake upstream parsed max_output_tokens=%d", *parsed.MaxOutputTokens)
						case types.RelayFormatClaude:
							var parsed dto.ClaudeRequest
							require.NoError(t, common.Unmarshal(raw[0], &parsed))
							if target.canonical == "max_tokens_to_sample" {
								t.Errorf("fake upstream parsed max_tokens_to_sample=%d", *parsed.MaxTokensToSample)
							} else {
								t.Errorf("fake upstream parsed max_tokens=%d", *parsed.MaxTokens)
							}
						}
					}
					require.Equal(t, http.StatusBadRequest, response.StatusCode, string(data))
					require.Zero(t, sent, "非规范 Unicode 折叠字段不得触发生成提交")
					if !pass {
						waitForRefund(t, requestID)
					}
				})
			}
		}
	})

	t.Run("matching limit aliases and final conflicts", func(t *testing.T) {
		for _, target := range []struct {
			protocol protocol
			second   string
		}{{protocols[0], "max_completion_tokens"}, {protocols[4], "max_tokens_to_sample"}} {
			for _, tc := range []struct {
				name             string
				pass             bool
				first, second    int
				status, wantSent int
			}{
				{"matching boundary", false, 128, 128, http.StatusOK, 128},
				{"conflicting limits within cap", false, 48, 128, http.StatusBadRequest, 0},
				{"matching raw passthrough", true, 48, 129, http.StatusOK, 48},
			} {
				t.Run(target.protocol.name+"/"+tc.name, func(t *testing.T) {
					p := target.protocol
					channel, err := model.GetChannelById(p.channelID, true)
					require.NoError(t, err)
					channel.Type = p.channelType
					channel.BaseURL = baseFor(channel.Type)
					channel.ParamOverride = common.GetPointer(fmt.Sprintf(`{"%s":%d,"%s":%d}`, p.field, tc.first, target.second, tc.second))
					channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: tc.pass})
					require.NoError(t, db.Save(channel).Error)
					body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"fixture"}],%q:48,%q:48}`, p.model, p.field, target.second)
					request, err := http.NewRequest(http.MethodPost, gateway.URL+p.path, strings.NewReader(body))
					require.NoError(t, err)
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Authorization", "Bearer sk-"+key)
					requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
					request.Header.Set("X-Fixture-Request-ID", requestID)
					mutex.Lock()
					count := len(requests)
					mutex.Unlock()
					response, err := client.Do(request)
					require.NoError(t, err)
					data, err := io.ReadAll(response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					mutex.Lock()
					calls := append([]map[string]any(nil), requests[count:]...)
					raw := append([][]byte(nil), requestBytes[count:]...)
					mutex.Unlock()
					require.Equal(t, tc.status, response.StatusCode, string(data))
					if tc.wantSent == 0 {
						require.Empty(t, calls)
						waitForRefund(t, requestID)
					} else {
						require.Len(t, calls, 1)
						require.Equal(t, float64(tc.wantSent), calls[0][p.field])
						require.Equal(t, float64(tc.wantSent), calls[0][target.second])
						if tc.pass {
							require.Equal(t, []byte(body), raw[0])
						}
					}
				})
			}
		}
	})

	t.Run("deepseek maps completion alias only for managed conversion", func(t *testing.T) {
		for _, tc := range []struct {
			name, override string
			pass, ordinary bool
			wantStatus     int
		}{
			{"managed conversion", `{}`, false, false, http.StatusOK},
			{"raw alias rejected", `{}`, true, false, http.StatusBadRequest},
			{"overridden limit rejected", `{"max_tokens":129}`, false, false, http.StatusBadRequest},
			{"ordinary unchanged", `{}`, false, true, http.StatusOK},
		} {
			t.Run(tc.name, func(t *testing.T) {
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type = constant.ChannelTypeDeepSeek
				channel.BaseURL = baseFor(channel.Type)
				channel.ParamOverride = common.GetPointer(tc.override)
				channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: tc.pass})
				require.NoError(t, db.Save(channel).Error)
				body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_completion_tokens":48}`
				if generatedPath := os.Getenv("AISCOO_TEST_SDK_DEEPSEEK_BODY"); generatedPath != "" {
					generated, err := os.ReadFile(generatedPath)
					require.NoError(t, err)
					body = string(generated)
				}
				request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				chosenKey := key
				if tc.ordinary {
					chosenKey = nativeKey
				}
				request.Header.Set("Authorization", "Bearer sk-"+chosenKey)
				requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
				request.Header.Set("X-Fixture-Request-ID", requestID)
				mutex.Lock()
				count := len(requests)
				mutex.Unlock()
				response, err := client.Do(request)
				require.NoError(t, err)
				data, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				mutex.Lock()
				calls := append([]map[string]any(nil), requests[count:]...)
				mutex.Unlock()
				if tc.wantStatus != http.StatusOK {
					require.Equal(t, http.StatusBadRequest, response.StatusCode, string(data))
					require.Empty(t, calls, "不可信透传或超限覆盖不能提交生成请求")
					waitForRefund(t, requestID)
					return
				}
				require.Equal(t, http.StatusOK, response.StatusCode, string(data))
				require.Len(t, calls, 1)
				if tc.ordinary {
					require.Equal(t, float64(48), calls[0]["max_completion_tokens"])
					require.NotContains(t, calls[0], "max_tokens")
				} else {
					require.Equal(t, float64(48), calls[0]["max_tokens"])
					require.NotContains(t, calls[0], "max_completion_tokens")
				}
			})
		}
	})

	t.Run("candidate count is bounded for managed chat", func(t *testing.T) {
		for _, tc := range []struct {
			name, count, override string
			pass, ordinary        bool
			status                int
			wantSent              int
			refund                bool
		}{
			{"final override exceeds one", "1", "2", false, false, http.StatusBadRequest, 0, true},
			{"incoming two cannot be lowered", "2", "1", false, false, http.StatusBadRequest, 0, false},
			{"one remains legal", "1", "1", false, false, http.StatusOK, 1, false},
			{"raw two rejected", "2", "1", true, false, http.StatusBadRequest, 0, false},
			{"ordinary override remains legal", "1", "2", false, true, http.StatusOK, 2, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type = constant.ChannelTypeOpenAI
				channel.BaseURL = baseFor(channel.Type)
				channel.ParamOverride = common.GetPointer(`{"n":` + tc.override + `}`)
				channel.SetSetting(dto.ChannelSettings{PassThroughBodyEnabled: tc.pass})
				require.NoError(t, db.Save(channel).Error)
				body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_tokens":48,"n":` + tc.count + `}`
				request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				chosenKey := key
				if tc.ordinary {
					chosenKey = nativeKey
				}
				request.Header.Set("Authorization", "Bearer sk-"+chosenKey)
				requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
				request.Header.Set("X-Fixture-Request-ID", requestID)
				mutex.Lock()
				count := len(requests)
				mutex.Unlock()
				response, err := client.Do(request)
				require.NoError(t, err)
				data, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				mutex.Lock()
				calls := append([]map[string]any(nil), requests[count:]...)
				mutex.Unlock()
				require.Equal(t, tc.status, response.StatusCode, string(data))
				if tc.wantSent == 0 {
					require.Empty(t, calls)
				} else {
					require.Len(t, calls, 1)
					require.Equal(t, float64(tc.wantSent), calls[0]["n"])
				}
				if tc.refund {
					waitForRefund(t, requestID)
				}
			})
		}
	})

	t.Run("unsupported channel is local and non-retryable", func(t *testing.T) {
		channel, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		channel.Type = constant.ChannelTypeOllama
		channel.ParamOverride = common.GetPointer(`{}`)
		channel.SetSetting(dto.ChannelSettings{})
		require.NoError(t, db.Save(channel).Error)
		body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_tokens":48}`
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+key)
		requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
		request.Header.Set("X-Fixture-Request-ID", requestID)
		mutex.Lock()
		count := len(requests)
		mutex.Unlock()
		response, err := client.Do(request)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusBadRequest, response.StatusCode, string(data))
		mutex.Lock()
		sent := len(requests) - count
		mutex.Unlock()
		require.Zero(t, sent)
		captured, exists := policyEvents.Load(requestID)
		require.True(t, exists)
		var localFailure, stopped bool
		for _, event := range captured.([]service.PolicyEvent) {
			localFailure = localFailure || event.ErrorSource == "local" && event.Decision.Reason == "local_rejection"
			stopped = stopped || event.Decision.Action == "stop" && event.Health == "unchanged"
			require.NotEqual(t, "retry", event.Decision.Action)
		}
		require.True(t, localFailure)
		require.True(t, stopped)
		waitForRefund(t, requestID)
	})

	t.Run("ordinary request keeps channel override", func(t *testing.T) {
		channel, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		channel.Type = constant.ChannelTypeOpenAI
		channel.BaseURL = baseFor(channel.Type)
		channel.ParamOverride = common.GetPointer(`{"max_tokens":129}`)
		channel.SetSetting(dto.ChannelSettings{})
		require.NoError(t, db.Save(channel).Error)
		body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_tokens":48}`
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+nativeKey)
		request.Header.Set("X-Fixture-Request-ID", fmt.Sprintf("output-limit-%d", serial.Add(1)))
		mutex.Lock()
		count := len(requests)
		mutex.Unlock()
		response, err := client.Do(request)
		require.NoError(t, err)
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode, string(data))
		mutex.Lock()
		require.Len(t, requests[count:], 1)
		require.Equal(t, float64(129), requests[count]["max_tokens"])
		mutex.Unlock()
		channel.ParamOverride = common.GetPointer(`{"max_tokenſ":129}`)
		require.NoError(t, db.Save(channel).Error)
		request, err = http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+nativeKey)
		request.Header.Set("X-Fixture-Request-ID", fmt.Sprintf("output-limit-%d", serial.Add(1)))
		mutex.Lock()
		count = len(requests)
		mutex.Unlock()
		response, err = client.Do(request)
		require.NoError(t, err)
		data, err = io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode, string(data))
		mutex.Lock()
		require.Len(t, requestBytes[count:], 1)
		raw := append([]byte(nil), requestBytes[count]...)
		mutex.Unlock()
		var parsed dto.GeneralOpenAIRequest
		require.NoError(t, common.Unmarshal(raw, &parsed))
		require.NotNil(t, parsed.MaxTokens)
		require.Equal(t, uint(129), *parsed.MaxTokens, "非托管渠道覆盖维持原样")
	})

	for _, p := range []protocol{protocols[0], protocols[3], protocols[4]} {
		t.Run("committed stream/"+p.name, func(t *testing.T) {
			channel, err := model.GetChannelById(p.channelID, true)
			require.NoError(t, err)
			channel.Type = p.channelType
			channel.BaseURL = baseFor(channel.Type)
			channel.ParamOverride = common.GetPointer(fmt.Sprintf(`{"%s":129}`, p.field))
			channel.SetSetting(dto.ChannelSettings{})
			require.NoError(t, db.Save(channel).Error)
			body := map[string]any{"model": p.model, p.field: 48, "stream": true}
			if p.format == types.RelayFormatOpenAIResponses {
				body["input"] = "fixture"
			} else {
				body["messages"] = []any{map[string]any{"role": "user", "content": "fixture"}}
			}
			encoded, err := common.Marshal(body)
			require.NoError(t, err)
			request, err := http.NewRequest(http.MethodPost, gateway.URL+p.path, strings.NewReader(string(encoded)))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+key)
			request.Header.Set("X-Fixture-Committed-Stream", "true")
			requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
			request.Header.Set("X-Fixture-Request-ID", requestID)
			mutex.Lock()
			count := len(requests)
			mutex.Unlock()
			response, err := client.Do(request)
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Contains(t, response.Header.Get("Content-Type"), "text/event-stream")
			if p.format == types.RelayFormatClaude {
				require.Contains(t, string(data), "event: error")
			}
			var streamError map[string]any
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "data: {") {
					require.NoError(t, common.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &streamError))
					break
				}
			}
			require.NotNil(t, streamError)
			errorBody, ok := streamError["error"].(map[string]any)
			require.True(t, ok, string(data))
			require.Equal(t, "400", errorBody["code"])
			require.NotEmpty(t, errorBody["message"])
			if p.format == types.RelayFormatClaude {
				require.Equal(t, "error", streamError["type"])
				require.Equal(t, "invalid_request_error", errorBody["type"])
			}
			mutex.Lock()
			sent := len(requests) - count
			mutex.Unlock()
			require.Zero(t, sent, "已提交流的违规尝试仍不得发送上游")
			waitForRefund(t, requestID)
		})
	}

	t.Run("responses heartbeat then retry ends with native error event", func(t *testing.T) {
		settings := operation_setting.GetGeneralSetting()
		oldEnabled, oldInterval, oldRetries := settings.PingIntervalEnabled, settings.PingIntervalSeconds, common.RetryTimes
		settings.PingIntervalEnabled, settings.PingIntervalSeconds, common.RetryTimes = true, 1, 1
		defer func() {
			settings.PingIntervalEnabled, settings.PingIntervalSeconds, common.RetryTimes = oldEnabled, oldInterval, oldRetries
		}()
		first, err := model.GetChannelById(3, true)
		require.NoError(t, err)
		first.Type = constant.ChannelTypeOpenAI
		first.BaseURL = baseFor(constant.ChannelTypeOpenAI)
		first.ParamOverride = common.GetPointer(`{"max_output_tokens":128}`)
		first.HeaderOverride = common.GetPointer(`{}`)
		first.SetSetting(dto.ChannelSettings{})
		require.NoError(t, db.Save(first).Error)
		priority := int64(20)
		require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", first.Id, "responses").Update("priority", priority).Error)
		fallbackPriority := int64(10)
		second := model.Channel{Name: "responses-stream-fallback", Type: constant.ChannelTypeOpenAI, Key: key, BaseURL: baseFor(constant.ChannelTypeOpenAI), Status: common.ChannelStatusEnabled, Group: "catalog", Models: "responses", Priority: &fallbackPriority, ParamOverride: common.GetPointer(`{"max_output_tokens":129}`)}
		require.NoError(t, db.Create(&second).Error)
		require.NoError(t, db.Create(&model.Ability{Group: "catalog", Model: "responses", ChannelId: second.Id, Enabled: true, Priority: &fallbackPriority}).Error)
		if fixturePath := os.Getenv("AISCOO_TEST_SDK_RESPONSES_FIXTURE"); fixturePath != "" {
			defer releaseResponses()
			mutex.Lock()
			count := len(requests)
			mutex.Unlock()
			failResponsesAttempt.Store(true)
			fixture, err := common.Marshal(map[string]string{"url": gateway.URL, "key": "sk-" + key})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(fixturePath, fixture, 0600))
			select {
			case <-liveSDKDone:
			case <-time.After(30 * time.Second):
				t.Fatal("真实 SDK 验收未结束")
			}
			select {
			case <-responsesStarted:
			default:
				t.Fatal("SDK 未经过首个合法渠道")
			}
			mutex.Lock()
			sent := len(requests) - count
			mutex.Unlock()
			require.Equal(t, 1, sent, "SDK 两渠道重试只能提交首个合法渠道")
			waitForRefund(t, os.Getenv("AISCOO_TEST_SDK_RESPONSES_REQUEST_ID"))
			return
		}
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/responses", strings.NewReader(`{"model":"responses","input":"fixture","max_output_tokens":48,"stream":true}`))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+key)
		requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
		request.Header.Set("X-Fixture-Request-ID", requestID)
		mutex.Lock()
		count := len(requests)
		mutex.Unlock()
		failResponsesAttempt.Store(true)
		defer releaseResponses()
		type result struct {
			response *http.Response
			err      error
		}
		responses := make(chan result, 1)
		go func() { response, err := client.Do(request); responses <- result{response, err} }()
		select {
		case <-responsesStarted:
		case <-time.After(3 * time.Second):
			t.Fatal("首个合法渠道未接收请求")
		}
		var resultValue result
		select {
		case resultValue = <-responses:
		case <-time.After(3 * time.Second):
			t.Fatal("首个渠道等待时没有实际心跳")
		}
		require.NoError(t, resultValue.err)
		defer resultValue.response.Body.Close()
		require.Equal(t, http.StatusOK, resultValue.response.StatusCode)
		reader := bufio.NewReader(resultValue.response.Body)
		ping, err := reader.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, ": PING\n", ping)
		blank, err := reader.ReadString('\n')
		require.NoError(t, err)
		require.Equal(t, "\n", blank)
		releaseResponses()
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		var event map[string]any
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "data: {") {
				require.NoError(t, common.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data: "))), &event))
				break
			}
		}
		require.Equal(t, "error", event["type"], string(data))
		require.Equal(t, float64(0), event["sequence_number"])
		errorBody, ok := event["error"].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "400", errorBody["code"])
		require.NotEmpty(t, errorBody["message"])
		mutex.Lock()
		sent := len(requests) - count
		mutex.Unlock()
		require.Equal(t, 1, sent, "第二渠道校验拒绝后不得提交上游")
		waitForRefund(t, requestID)
	})

	t.Run("retry keeps original admission and stops before invalid second submission", func(t *testing.T) {
		first, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		first.Type = constant.ChannelTypeOpenAI
		first.BaseURL = baseFor(first.Type)
		first.ModelMapping = common.GetPointer(`{"chat":"first-upstream"}`)
		first.ParamOverride = common.GetPointer(`{"max_tokens":128}`)
		first.SetSetting(dto.ChannelSettings{})
		require.NoError(t, db.Save(first).Error)
		fallback.ModelMapping = common.GetPointer(`{"chat":"second-upstream"}`)
		fallback.ParamOverride = common.GetPointer(`{"max_tokens":129}`)
		require.NoError(t, db.Save(&fallback).Error)
		thirdPriority := int64(-10)
		third := model.Channel{Name: "output-limit-third", Type: constant.ChannelTypeOpenAI, Key: key, BaseURL: baseFor(constant.ChannelTypeOpenAI), Status: common.ChannelStatusEnabled, Group: "catalog", Models: "chat", Priority: &thirdPriority}
		require.NoError(t, db.Create(&third).Error)
		require.NoError(t, db.Create(&model.Ability{Group: "catalog", Model: "chat", ChannelId: third.Id, Enabled: true, Priority: &thirdPriority}).Error)
		oldRetries := common.RetryTimes
		common.RetryTimes = 2
		defer func() { common.RetryTimes = oldRetries }()
		original := zenModelConfigFixture("openai-chat-completions")
		publishZenTestMetadata(t, "chat", original)
		defer publishZenTestMetadata(t, "chat", original)
		body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_tokens":48}`
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+key)
		requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
		request.Header.Set("X-Fixture-Request-ID", requestID)
		mutex.Lock()
		count := len(requests)
		mutex.Unlock()
		failFirstRetryAttempt.Store(true)
		type responseResult struct {
			response *http.Response
			err      error
		}
		result := make(chan responseResult, 1)
		go func() {
			response, err := client.Do(request)
			result <- responseResult{response: response, err: err}
		}()
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(allowRetryResponse) }) }
		defer release()
		select {
		case <-firstRetrySubmitted:
		case <-time.After(3 * time.Second):
			t.Fatal("首个合法渠道未提交")
		}
		// 准入后发布更宽的配置；本次请求的后续尝试仍须使用原来的 128。
		publishZenTestMetadata(t, "chat", strings.Replace(original, `"max":128`, `"max":1024`, 1))
		release()
		var completed responseResult
		select {
		case completed = <-result:
		case <-time.After(5 * time.Second):
			t.Fatal("重试未结束")
		}
		require.NoError(t, completed.err)
		data, err := io.ReadAll(completed.response.Body)
		require.NoError(t, err)
		require.NoError(t, completed.response.Body.Close())
		require.Equal(t, http.StatusBadRequest, completed.response.StatusCode, string(data))
		mutex.Lock()
		attempts := append([]map[string]any(nil), requests[count:]...)
		mutex.Unlock()
		require.Len(t, attempts, 1, "仅第一个合法尝试可以提交")
		require.Equal(t, "first-upstream", attempts[0]["model"])
		require.Equal(t, float64(128), attempts[0]["max_tokens"])
		captured, exists := policyEvents.Load(requestID)
		require.True(t, exists)
		var selected []int
		var sawLocalStop bool
		for _, event := range captured.([]service.PolicyEvent) {
			if event.Decision.Action == "attempt" {
				selected = append(selected, event.ChannelID)
			}
			sawLocalStop = sawLocalStop || event.ErrorSource == "local" && event.Decision.Action == "stop" && event.Health == "unchanged"
		}
		require.Equal(t, []int{first.Id, fallback.Id}, selected)
		require.True(t, sawLocalStop)
		waitForRefund(t, requestID)
	})

	t.Run("retry rechecks live channel eligibility", func(t *testing.T) {
		first, err := model.GetChannelById(1, true)
		require.NoError(t, err)
		first.Type = constant.ChannelTypeOpenAI
		first.BaseURL = baseFor(first.Type)
		first.ModelMapping = common.GetPointer(`{"chat":"first-upstream"}`)
		first.ParamOverride = common.GetPointer(`{"max_tokens":128}`)
		first.SetSetting(dto.ChannelSettings{})
		require.NoError(t, db.Save(first).Error)
		fallback.ParamOverride = common.GetPointer(`{"max_tokens":128}`)
		require.NoError(t, db.Save(&fallback).Error)
		var third model.Channel
		require.NoError(t, db.Where("name = ?", "output-limit-third").First(&third).Error)
		third.ModelMapping = common.GetPointer(`{"chat":"third-upstream"}`)
		require.NoError(t, db.Save(&third).Error)
		body := `{"model":"chat","messages":[{"role":"user","content":"fixture"}],"max_tokens":48}`
		request, err := http.NewRequest(http.MethodPost, gateway.URL+"/v1/chat/completions", strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer sk-"+key)
		requestID := fmt.Sprintf("output-limit-%d", serial.Add(1))
		request.Header.Set("X-Fixture-Request-ID", requestID)
		mutex.Lock()
		count := len(requests)
		mutex.Unlock()
		failRevocationAttempt.Store(true)
		type responseResult struct {
			response *http.Response
			err      error
		}
		result := make(chan responseResult, 1)
		go func() {
			response, err := client.Do(request)
			result <- responseResult{response: response, err: err}
		}()
		var releaseOnce sync.Once
		release := func() { releaseOnce.Do(func() { close(allowRevocationResponse) }) }
		defer release()
		select {
		case <-firstRevocationSubmitted:
		case <-time.After(3 * time.Second):
			t.Fatal("首个合法渠道未提交")
		}
		// 请求开始后移除第二渠道资格；重试仍要按实时渠道事实过滤。
		require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ? AND model = ?", fallback.Id, "chat").Update("enabled", false).Error)
		release()
		var completed responseResult
		select {
		case completed = <-result:
		case <-time.After(5 * time.Second):
			t.Fatal("重试未结束")
		}
		require.NoError(t, completed.err)
		data, err := io.ReadAll(completed.response.Body)
		require.NoError(t, err)
		require.NoError(t, completed.response.Body.Close())
		require.Equal(t, http.StatusOK, completed.response.StatusCode, string(data))
		mutex.Lock()
		attempts := append([]map[string]any(nil), requests[count:]...)
		mutex.Unlock()
		require.Len(t, attempts, 2)
		require.Equal(t, "first-upstream", attempts[0]["model"])
		require.Equal(t, "third-upstream", attempts[1]["model"], "已撤销资格的第二渠道不能接收请求")
		var captured any
		require.Eventually(t, func() bool {
			var exists bool
			captured, exists = policyEvents.Load(requestID)
			return exists
		}, time.Second, 10*time.Millisecond, "等待请求结束后记录渠道决策")
		var selected []int
		for _, event := range captured.([]service.PolicyEvent) {
			if event.Decision.Action == "attempt" {
				selected = append(selected, event.ChannelID)
			}
		}
		require.Equal(t, []int{first.Id, third.Id}, selected)
	})

	if fixturePath := os.Getenv("AISCOO_TEST_SDK_STREAM_FIXTURE"); fixturePath != "" {
		t.Run("live SDK stream fixture", func(t *testing.T) {
			for _, p := range []protocol{protocols[0], protocols[3], protocols[4]} {
				channel, err := model.GetChannelById(p.channelID, true)
				require.NoError(t, err)
				channel.Type = p.channelType
				channel.BaseURL = baseFor(channel.Type)
				channel.ParamOverride = common.GetPointer(fmt.Sprintf(`{"%s":129}`, p.field))
				channel.SetSetting(dto.ChannelSettings{})
				require.NoError(t, db.Save(channel).Error)
			}
			mutex.Lock()
			count := len(requests)
			mutex.Unlock()
			fixture, err := common.Marshal(map[string]string{"url": gateway.URL, "key": "sk-" + key})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(fixturePath, fixture, 0600))
			select {
			case <-liveSDKDone:
			case <-time.After(45 * time.Second):
				t.Fatal("SDK 验收未在时限内结束")
			}
			require.Equal(t, int32(3), liveSDKRequests.Load(), "每种协议都必须经过真实 Relay")
			for _, kind := range []string{"anthropic", "openai-compatible", "openai"} {
				waitForRefund(t, "sdk-stream-"+kind)
			}
			mutex.Lock()
			defer mutex.Unlock()
			require.Equal(t, count, len(requests), "三种违规尝试均不能提交假上游")
		})
	}
}
