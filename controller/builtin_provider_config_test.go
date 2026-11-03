package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	zenDTO "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/builtinconfig"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuiltinReleaseValidationAndSharedResolution(t *testing.T) {
	seed := builtinconfig.EmbeddedRelease()
	require.Greater(t, len(seed), 65535, "the bundled release exercises storage beyond MySQL TEXT")
	_, err := builtinconfig.Parse(string(seed))
	require.NoError(t, err)
	data, err := os.ReadFile("../pkg/builtinconfig/testdata/rule-resolution.json")
	require.NoError(t, err)
	var fixture struct {
		Release common.RawMessage `json:"release"`
		Cases   []struct {
			Name     string                        `json:"name"`
			Input    builtinconfig.ResolutionInput `json:"input"`
			Expected common.RawMessage             `json:"expected"`
		} `json:"cases"`
	}
	require.NoError(t, common.Unmarshal(data, &fixture))
	release, err := builtinconfig.Parse(string(fixture.Release))
	require.NoError(t, err)
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			actual, err := release.Resolve(tc.Input)
			require.NoError(t, err)
			encoded, err := common.Marshal(actual)
			require.NoError(t, err)
			assert.JSONEq(t, string(tc.Expected), string(encoded))
		})
	}
	for _, revision := range []string{"43.0", "4.3e1"} {
		_, err := builtinconfig.Parse(strings.Replace(string(fixture.Release), `"revision": 43`, `"revision":`+revision, 1))
		assert.NoError(t, err, "safe integer JSON notation is upstream compatible")
	}
	_, err = builtinconfig.Parse(strings.Replace(string(fixture.Release), `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1?api-version=2025-01-01&format=json","headers":{"OpenAI-Beta":"","anthropic-version":"2023-06-01","anthropic-beta":"feature-2025-01-01","Accept":"application/json","Content-Type":"application/json"}`, 1))
	assert.NoError(t, err, "public version and protocol metadata, including empty allowed static headers, are upstream data")
	patternOnly, err := builtinconfig.Parse(strings.Replace(string(fixture.Release), `"baseUrlMatch":"https://example\\.invalid/v1\\?x=A#B"`, `"baseUrlMatch":"https://example\\.invalid/v1\\?token=.*"`, 1))
	require.NoError(t, err, "baseUrlMatch is a JavaScript pattern, not a credential URL")
	assert.Equal(t, `https://example\.invalid/v1\?token=.*`, patternOnly.Config.ModelConfigRules.ProviderSiteRules[0]["baseUrlMatch"])
	for _, tc := range []struct{ name, old, next string }{
		{"unsafe revision", `"revision": 43`, `"revision":9007199254740992`},
		{"fraction rounded by float", `"revision": 43`, `"revision":9007199254740990.5`},
		{"negative revision", `"revision": 43`, `"revision":-1`},
		{"unbounded integer exponent", `"revision": 43`, `"revision":0e-999999999`},
		{"unknown envelope", `"schemaVersion": 1`, `"extra":true,"schemaVersion":1`},
		{"unknown model leaf", `"contextWindow":8192`, `"contextWindow":8192,"invented":true`},
		{"invalid JS pattern", `"modelMatch":".*"`, `"modelMatch":"["`},
		{"dotnet only pattern", `"modelMatch":".*"`, `"modelMatch":"(?i)glm"`},
		{"unsafe context", `"contextWindow":8192`, `"contextWindow":9007199254740992`},
		{"duplicate values", `["off","low","high"]`, `["off","off"]`},
		{"blank option expression", `"map":"{}"`, `"map":" "`},
		{"private enum", `"group":"bigmodel-family"`, `"group":"new-api-family"`},
		{"secret header", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1","headers":{"Authorization":"redacted"}`},
		{"URL userinfo", `https://example.invalid/v1`, `https://user:password@example.invalid/v1`},
		{"URL query secret", `https://example.invalid/v1`, `https://example.invalid/v1?api_key=redacted`},
		{"opaque query key credential", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1?key=opaque-test-value"`},
		{"opaque query token credential", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1?token=opaque-test-value"`},
		{"opaque auth header credential", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1","headers":{"X-Auth-Token":"opaque-test-value"}`},
		{"unknown static header", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1","headers":{"X-Unknown":"opaque-test-value"}`},
		{"unknown query field", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1?unknown=opaque-test-value"`},
		{"malformed query cannot bypass allowlist", `"baseUrl":"https://example.invalid/v1"`, `"baseUrl":"https://example.invalid/v1?key=opaque-test-value;%ZZ"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(string(fixture.Release), tc.old, tc.next, 1)
			require.NotEqual(t, string(fixture.Release), raw)
			_, err := builtinconfig.Parse(raw)
			assert.Error(t, err)
		})
	}
	t.Run("regex execution budget fails explicitly", func(t *testing.T) {
		copy, err := builtinconfig.Parse(string(fixture.Release))
		require.NoError(t, err)
		copy.Config.ModelConfigRules.ModelRules = []map[string]any{{"modelMatch": "(a+)+(?=$)", "config": map[string]any{"enabled": true}}}
		_, err = copy.Resolve(builtinconfig.ResolutionInput{ModelID: strings.Repeat("a", 30) + "b"})
		assert.Error(t, err)
	})
}

// publishZenTestMetadata modifies only the central saved draft and publication;
// the retired model column remains useful as a representative upgrade fixture.
func publishZenTestMetadata(t *testing.T, name, raw string) {
	t.Helper()
	snapshot, err := model.GetBuiltinProviderConfig()
	require.NoError(t, err)
	release, err := builtinconfig.Parse(snapshot.DraftJSON)
	require.NoError(t, err)
	rules := release.Config.ModelConfigRules.BuiltinProviderModelRules[:0]
	for _, rule := range release.Config.ModelConfigRules.BuiltinProviderModelRules {
		if rule["modelId"] != name || !strings.HasPrefix(fmt.Sprint(rule["providerId"]), "account:new-api-") {
			rules = append(rules, rule)
		}
	}
	release.Config.ModelConfigRules.BuiltinProviderModelRules = rules
	parsed, err := model.ParseZenModelMetadata(raw)
	require.NoError(t, err)
	encoded, err := common.Marshal(parsed.ModelConfig)
	require.NoError(t, err)
	var config map[string]any
	require.NoError(t, common.Unmarshal(encoded, &config))
	release.Config.ModelConfigRules.BuiltinProviderModelRules = append(rules, map[string]any{"providerId": "account:new-api-" + parsed.APIType, "modelId": name, "config": config})
	encoded, err = common.Marshal(release)
	require.NoError(t, err)
	snapshot, err = model.SaveBuiltinProviderConfig(snapshot.Version, string(encoded))
	require.NoError(t, err)
	_, err = model.PublishBuiltinProviderConfig(snapshot.Version)
	require.NoError(t, err)
}

func TestBuiltinConfigDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " for the isolated database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			t.Setenv("XCODE_PUBLIC_ORIGIN", "http://127.0.0.1:59080")
			require.NoError(t, db.AutoMigrate(&model.ZenAuthorization{}, &model.UserSubscription{}, &model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}))
			legacy := model.Model{ModelName: "legacy", NameRule: model.NameRuleExact, Status: 1, ZenConfig: model.LongText(zenModelConfigFixture(zenDTO.ZenAPITypeOpenAIResponses))}
			bad := model.Model{ModelName: "broken", NameRule: model.NameRuleExact, Status: 1, ZenConfig: `{"schema_version":1}`}
			require.NoError(t, db.Create(&legacy).Error)
			require.NoError(t, db.Create(&bad).Error)
			sensitive := strings.Replace(zenModelConfigFixture(zenDTO.ZenAPITypeOpenAIChatCompletions), `"map":"{}"`, `"map":"{\"Authorization\":\"history-sensitive-payload\"}"`, 1)
			require.NoError(t, db.Create(&model.Model{ModelName: "sensitive-legacy", Status: 1, ZenConfig: model.LongText(sensitive)}).Error)
			for _, apiType := range []string{zenDTO.ZenAPITypeOpenAIChatCompletions, zenDTO.ZenAPITypeOpenAIResponses} {
				require.NoError(t, db.Create(&model.Model{ModelName: "conflicting-legacy", Status: 1, ZenConfig: model.LongText(zenModelConfigFixture(apiType))}).Error)
			}
			require.NoError(t, model.InitializeBuiltinProviderConfig(db))
			initial, err := model.GetBuiltinProviderConfig()
			require.NoError(t, err)
			require.NotNil(t, initial.PublishedRevision)
			assert.Greater(t, *initial.PublishedRevision, int64(43))
			assert.Len(t, initial.MigrationIssues, 3)
			assert.Contains(t, strings.Join(initial.MigrationIssues, " "), "conflicting historical exact declarations")
			require.NoError(t, model.InitializeBuiltinProviderConfig(db))
			repeated, err := model.GetBuiltinProviderConfig()
			require.NoError(t, err)
			assert.Equal(t, initial, repeated)
			initialBody, err := model.GetBuiltinProviderRelease(initial.PublishedHash)
			require.NoError(t, err)
			assert.Equal(t, initial.DraftJSON, initialBody, "large draft and immutable release survive full database roundtrip")
			require.Greater(t, len(initialBody), 65535)
			var archive model.Option
			require.NoError(t, db.Where("value LIKE ?", "%legacy%").Where("value LIKE ?", "%original%").First(&archive).Error)
			assert.Contains(t, archive.Value, `\"api_type\":\"openai-responses\"`)
			assert.Contains(t, archive.Value, `{\"schema_version\":1}`)
			assert.Contains(t, archive.Value, "history-sensitive-payload")
			assert.NotContains(t, initialBody, "history-sensitive-payload")
			assert.Contains(t, strings.Join(initial.MigrationIssues, " "), "fails publication validation")
			var original model.Model
			require.NoError(t, db.First(&original, legacy.Id).Error)
			assert.Equal(t, legacy.ZenConfig, original.ZenConfig)

			user := model.User{Username: "builtin-catalog", AffCode: "builtin-catalog", Group: "catalog", Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			channel := model.Channel{Name: "builtin-route", Type: 1, Key: "never-exposed-credential", Group: "catalog", Models: "legacy,new-model,broken,conflicting-legacy,sensitive-legacy", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			for _, name := range []string{"legacy", "new-model", "broken", "conflicting-legacy", "sensitive-legacy"} {
				require.NoError(t, db.Create(&model.Ability{Group: "catalog", Model: name, ChannelId: channel.Id, Enabled: true}).Error)
			}
			catalog, err := service.GetZenModelCatalog(user.Id)
			require.NoError(t, err)
			for _, entry := range catalog.Models {
				switch entry.ModelID {
				case "legacy":
					require.True(t, entry.Enabled)
					require.NotNil(t, entry.APIType)
					assert.Equal(t, zenDTO.ZenAPITypeOpenAIResponses, *entry.APIType)
					assert.Equal(t, uint64(128), entry.ModelConfig.OptionSpecs.MaxOutputTokens.Max)
				case "new-model":
					assert.True(t, entry.Enabled, "models without retired JSON use the central baseline")
					assert.NotNil(t, entry.ModelConfig)
				case "broken", "conflicting-legacy", "sensitive-legacy":
					assert.False(t, entry.Enabled, "invalid historical records are explicitly disabled, not silently defaulted")
				}
			}
			_, err = service.ValidateZenModelRequest(user.Id, "legacy", zenDTO.ZenAPITypeOpenAIResponses, common.GetPointer(uint(129)))
			require.Error(t, err)
			_, err = service.ValidateZenModelRequest(user.Id, "legacy", zenDTO.ZenAPITypeOpenAIResponses, common.GetPointer(uint(128)))
			require.NoError(t, err)
			require.NoError(t, db.Model(&legacy).Update("x_code_config", zenModelConfigFixture(zenDTO.ZenAPITypeAnthropicMessages)).Error)
			afterArchiveEdit, err := service.GetZenModelCatalog(user.Id)
			require.NoError(t, err)
			assert.Equal(t, catalog.CatalogRevision, afterArchiveEdit.CatalogRevision, "archived raw data cannot mutate live central authority")

			t.Run("strict public and root API shapes", func(t *testing.T) {
				response := modelManagementRequest(t, GetBuiltinProviderConfig, http.MethodGet, "/api/builtin-provider-config", nil, nil)
				assert.Equal(t, http.StatusOK, response.Code)
				var wire struct {
					Success bool           `json:"success"`
					Data    map[string]any `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &wire))
				assert.True(t, wire.Success)
				assert.Len(t, wire.Data, 5)
				assert.Equal(t, "http://127.0.0.1:59080/api/v1/client/builtin-provider-config/"+initial.PublishedHash+".json", wire.Data["published_url"])
				router := gin.New()
				router.GET("/api/v1/client/configs", PublicClientConfigs)
				router.GET("/api/v1/client/builtin-provider-config/:digest", PublicBuiltinProviderConfig)
				request := httptest.NewRequest(http.MethodGet, "http://poisoned.invalid/api/v1/client/configs?app_version=1&platform=darwin", nil)
				response = httptest.NewRecorder()
				router.ServeHTTP(response, request)
				assert.Equal(t, http.StatusOK, response.Code)
				assert.JSONEq(t, fmt.Sprintf(`{"code":0,"data":{"configs":{"builtin_provider_config_json":"http://127.0.0.1:59080/api/v1/client/builtin-provider-config/%s.json"}}}`, initial.PublishedHash), response.Body.String())
				assert.Empty(t, response.Header().Get("Location"))
				response = httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/client/builtin-provider-config/"+initial.PublishedHash+".json", nil))
				assert.Equal(t, initialBody, response.Body.String())
				assert.Contains(t, response.Header().Get("Cache-Control"), "immutable")
				assert.NotContains(t, response.Body.String(), "never-exposed-credential")
				for _, role := range []int{0, common.RoleCommonUser, common.RoleAdminUser} {
					response := httptest.NewRecorder()
					ctx, _ := gin.CreateTestContext(response)
					ctx.Set("role", role)
					GetBuiltinProviderConfig(ctx)
					assert.Equal(t, http.StatusForbidden, response.Code)
				}
				common.OptionMap[archive.Key] = archive.Value
				response = modelManagementRequest(t, GetOptions, http.MethodGet, "/api/option/", nil, nil)
				assert.NotContains(t, response.Body.String(), archive.Key)
				response = modelManagementRequest(t, UpdateOption, http.MethodPut, "/api/option/", map[string]any{"key": archive.Key, "value": "overwrite"}, nil)
				assert.Equal(t, http.StatusBadRequest, response.Code)
				assert.Error(t, model.UpdateOption(archive.Key, "overwrite"))
				assert.Error(t, model.UpdateOptionsBulk(map[string]string{archive.Key: "overwrite"}))
				for _, field := range []string{"query-key", "auth-header"} {
					candidate, err := builtinconfig.Parse(initial.DraftJSON)
					require.NoError(t, err)
					api := candidate.Config.ProviderConfigRules.TemplateRules[0]["config"].(map[string]any)["api"].(map[string]any)
					if field == "query-key" {
						api["baseUrl"] = "https://example.invalid/v1?key=opaque-test-value"
					} else {
						api["headers"] = map[string]any{"X-Auth-Token": "opaque-test-value"}
					}
					body, err := common.Marshal(candidate)
					require.NoError(t, err)
					rejected := modelManagementRequest(t, SaveBuiltinProviderConfig, http.MethodPut, "/api/builtin-provider-config", map[string]any{"expected_version": initial.Version, "draft_json": string(body)}, nil)
					assert.Equal(t, http.StatusBadRequest, rejected.Code, field)
					assert.NotContains(t, rejected.Body.String(), "opaque-test-value")
					unchanged, err := model.GetBuiltinProviderConfig()
					require.NoError(t, err)
					assert.Equal(t, initial, unchanged, "rejected credentials cannot change draft or publication")
				}
			})

			var wg sync.WaitGroup
			start := make(chan struct{})
			errorsSeen := make(chan error, 2)
			for range 2 {
				wg.Go(func() {
					<-start
					_, err := model.SaveBuiltinProviderConfig(initial.Version, string(builtinconfig.EmbeddedRelease()))
					errorsSeen <- err
				})
			}
			close(start)
			wg.Wait()
			close(errorsSeen)
			success, conflicts := 0, 0
			for err := range errorsSeen {
				if err == nil {
					success++
				} else if errors.Is(err, model.ErrBuiltinConfigConflict) {
					conflicts++
				} else {
					require.NoError(t, err)
				}
			}
			assert.Equal(t, 1, success)
			assert.Equal(t, 1, conflicts)
			saved, err := model.GetBuiltinProviderConfig()
			require.NoError(t, err)
			assert.Equal(t, string(builtinconfig.EmbeddedRelease()), saved.DraftJSON, "unmodified 252 KiB seed round-trips exactly")
			require.NoError(t, model.InitializeBuiltinProviderConfig(db))
			repeated, err = model.GetBuiltinProviderConfig()
			require.NoError(t, err)
			assert.Equal(t, saved, repeated, "startup cannot overwrite an administrator draft")
			_, err = model.PublishBuiltinProviderConfig(initial.Version)
			assert.ErrorIs(t, err, model.ErrBuiltinConfigConflict)
			var published *model.BuiltinProviderSnapshot
			t.Run("concurrent publication accepts one expected version", func(t *testing.T) {
				publishRouter := gin.New()
				publishRouter.POST("/api/builtin-provider-config/publish", func(c *gin.Context) {
					c.Set("role", common.RoleRootUser)
					PublishBuiltinProviderConfig(c)
				})
				body := fmt.Sprintf(`{"expected_version":%d}`, saved.Version)
				ready := make(chan struct{}, 2)
				start := make(chan struct{})
				responses := make(chan *httptest.ResponseRecorder, 2)
				var wg sync.WaitGroup
				for range 2 {
					wg.Go(func() {
						request := httptest.NewRequest(http.MethodPost, "/api/builtin-provider-config/publish", strings.NewReader(body))
						response := httptest.NewRecorder()
						ready <- struct{}{}
						<-start
						publishRouter.ServeHTTP(response, request)
						responses <- response
					})
				}
				<-ready
				<-ready
				close(start)
				wg.Wait()
				close(responses)

				counts := map[int]int{}
				var success struct {
					Success bool                          `json:"success"`
					Data    model.BuiltinProviderSnapshot `json:"data"`
				}
				for response := range responses {
					counts[response.Code]++
					if response.Code == http.StatusOK {
						require.NoError(t, common.Unmarshal(response.Body.Bytes(), &success))
					}
				}
				require.Equal(t, 1, counts[http.StatusOK])
				require.Equal(t, 1, counts[http.StatusConflict])
				require.True(t, success.Success)
				published, err = model.GetBuiltinProviderConfig()
				require.NoError(t, err)
				require.NotNil(t, published.PublishedRevision)
				assert.Equal(t, saved.Version+1, published.Version)
				assert.Equal(t, saved.DraftJSON, published.DraftJSON)
				assert.Equal(t, published.Version, success.Data.Version)
				assert.Equal(t, published.PublishedRevision, success.Data.PublishedRevision)
				require.NotNil(t, success.Data.PublishedURL)
				assert.Equal(t, "http://127.0.0.1:59080/api/v1/client/builtin-provider-config/"+published.PublishedHash+".json", *success.Data.PublishedURL)
				publishedBody, err := model.GetBuiltinProviderRelease(published.PublishedHash)
				require.NoError(t, err)
				expected, err := builtinconfig.Parse(saved.DraftJSON)
				require.NoError(t, err)
				expected.Revision = *published.PublishedRevision
				expectedBody, err := common.Marshal(expected)
				require.NoError(t, err)
				assert.JSONEq(t, string(expectedBody), publishedBody)
			})
			require.NotNil(t, published)
			require.NotNil(t, published.PublishedRevision)
			assert.Greater(t, *published.PublishedRevision, *initial.PublishedRevision)
			reloaded, digest, err := model.GetPublishedBuiltinProviderConfig()
			require.NoError(t, err)
			assert.Equal(t, published.PublishedHash, digest)
			assert.Equal(t, *published.PublishedRevision, reloaded.Revision)
			old, err := model.GetBuiltinProviderRelease(initial.PublishedHash)
			require.NoError(t, err)
			assert.Equal(t, initialBody, old)
			response := modelManagementRequest(t, SaveBuiltinProviderConfig, http.MethodPut, "/api/builtin-provider-config", map[string]any{"expected_version": initial.Version, "draft_json": saved.DraftJSON}, nil)
			assert.Equal(t, http.StatusConflict, response.Code)
			response = modelManagementRequest(t, SaveBuiltinProviderConfig, http.MethodPut, "/api/builtin-provider-config", map[string]any{"expected_version": published.Version, "draft_json": "{}"}, nil)
			assert.Equal(t, http.StatusBadRequest, response.Code)

			t.Run("publication failure is atomic", func(t *testing.T) {
				before, err := model.GetBuiltinProviderConfig()
				require.NoError(t, err)
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_builtin_release", func(tx *gorm.DB) {
					if option, ok := tx.Statement.Dest.(*model.Option); ok && strings.Contains(option.Key, ".release.") {
						tx.AddError(errors.New("injected release write failure"))
					}
				}))
				_, err = model.PublishBuiltinProviderConfig(before.Version)
				assert.Error(t, err)
				require.NoError(t, db.Callback().Create().Remove("fail_builtin_release"))
				after, err := model.GetBuiltinProviderConfig()
				require.NoError(t, err)
				assert.Equal(t, before, after)
			})
			t.Run("rollback and imported revision retain monotonic safe revisions", func(t *testing.T) {
				snapshot, err := model.GetBuiltinProviderConfig()
				require.NoError(t, err)
				snapshot, err = model.SaveBuiltinProviderConfig(snapshot.Version, initialBody)
				require.NoError(t, err)
				rollback, err := model.PublishBuiltinProviderConfig(snapshot.Version)
				require.NoError(t, err)
				assert.Greater(t, *rollback.PublishedRevision, *published.PublishedRevision)
				imported, err := builtinconfig.Parse(initialBody)
				require.NoError(t, err)
				imported.Revision = builtinconfig.MaxSafeInteger
				body, err := common.Marshal(imported)
				require.NoError(t, err)
				snapshot, err = model.SaveBuiltinProviderConfig(rollback.Version, string(body))
				require.NoError(t, err)
				limit, err := model.PublishBuiltinProviderConfig(snapshot.Version)
				require.NoError(t, err)
				assert.Equal(t, builtinconfig.MaxSafeInteger, *limit.PublishedRevision)
				_, err = model.PublishBuiltinProviderConfig(limit.Version)
				assert.ErrorIs(t, err, model.ErrBuiltinConfigInvalid)
				unchanged, err := model.GetBuiltinProviderConfig()
				require.NoError(t, err)
				assert.Equal(t, limit, unchanged)
			})
		})
	}
}
