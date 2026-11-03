package service

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
)

var ErrZenModelUnavailable = errors.New("aiscoo zen model is unavailable for this protocol")

func zenRequestPath(apiType string) string {
	switch apiType {
	case dto.ZenAPITypeAnthropicMessages:
		return "/v1/messages"
	case dto.ZenAPITypeOpenAIChatCompletions:
		return "/v1/chat/completions"
	case dto.ZenAPITypeOpenAIResponses:
		return "/v1/responses"
	default:
		return ""
	}
}

func GetZenModelCatalog(userID int) (*dto.ZenCatalogSnapshot, error) {
	if userID <= 0 {
		return nil, model.ErrSubscriptionFundingTargetInvalid
	}
	var user model.User
	if err := model.DB.Select("id", "group").First(&user, userID).Error; err != nil {
		return nil, err
	}
	state, err := model.GetZenAuthorizationState(userID)
	if err != nil {
		return nil, err
	}
	abilities, err := model.GetZenAvailableAbilities(user.Group)
	if err != nil {
		return nil, err
	}
	release, publishedHash, err := model.GetPublishedBuiltinProviderConfig()
	if err != nil {
		return nil, err
	}
	// SQL collations differ across supported databases; authority is exact and
	// digest ordering must not depend on the database's collation.
	abilities = slices.DeleteFunc(abilities, func(ability model.ZenAvailableAbility) bool { return ability.Group != user.Group })
	slices.SortFunc(abilities, func(a, b model.ZenAvailableAbility) int {
		if order := cmp.Compare(a.Model, b.Model); order != 0 {
			return order
		}
		return cmp.Compare(a.ChannelId, b.ChannelId)
	})
	modelIDs := make([]string, 0, len(abilities))
	for _, ability := range abilities {
		if len(modelIDs) == 0 || modelIDs[len(modelIDs)-1] != ability.Model {
			modelIDs = append(modelIDs, ability.Model)
		}
	}
	var metadata []model.Model
	if len(modelIDs) > 0 {
		if err := model.DB.Select("model_name", "name_rule", "status", "icon", "vendor_id").
			Where("model_name IN ? AND name_rule = ?", modelIDs, model.NameRuleExact).Find(&metadata).Error; err != nil {
			return nil, err
		}
	}
	byName := make(map[string]*model.Model, len(metadata))
	vendorIDs := make([]int, 0, len(metadata))
	for i := range metadata {
		byName[metadata[i].ModelName] = &metadata[i]
		if metadata[i].Icon == "" && metadata[i].VendorID > 0 {
			vendorIDs = append(vendorIDs, metadata[i].VendorID)
		}
	}
	// ZEN: 图标与管理端模型列表同源：模型 icon 优先，空则取供应商 icon。
	vendorIcons := make(map[int]string)
	if len(vendorIDs) > 0 {
		var vendors []model.Vendor
		if err := model.DB.Select("id", "icon").Where("id IN ?", vendorIDs).Find(&vendors).Error; err != nil {
			return nil, err
		}
		for _, vendor := range vendors {
			vendorIcons[vendor.Id] = vendor.Icon
		}
	}
	snapshot := &dto.ZenCatalogSnapshot{
		SchemaVersion: dto.ZenModelSchemaVersion, ServerTime: time.Now().UnixMilli(), AuthorizationRevision: "0",
		Models: make([]dto.ZenCatalogModel, 0, len(modelIDs)),
	}
	modelAbilities := make(map[string][]model.ZenAvailableAbility)
	for _, ability := range abilities {
		modelAbilities[ability.Model] = append(modelAbilities[ability.Model], ability)
	}
	nativeMetadataStatus := make(map[string]int, len(metadata))
	for _, modelID := range modelIDs {
		row := byName[modelID]
		if row != nil {
			nativeMetadataStatus[modelID] = row.Status
		}
		entry, err := resolveZenCatalogModel(modelID, row, release, modelAbilities[modelID])
		if err != nil {
			return nil, err
		}
		if row != nil {
			if icon := cmp.Or(strings.TrimSpace(row.Icon), strings.TrimSpace(vendorIcons[row.VendorID])); icon != "" {
				entry.Icon = &icon
			}
		}
		snapshot.Models = append(snapshot.Models, entry)
	}
	// Hash normalized effective facts, not secrets, metadata prose or clocks.
	type routeFact struct {
		RequestPath, UpstreamPath, Converter, AuthType, AuthName string
		Eligible, PassThroughBody                                bool
	}
	type settingsFact struct {
		TaskPluginBindings                                                     []string
		ForceFormat, ThinkingToContent, PassThroughBody, SystemPromptOverride  bool
		AzureResponsesVersion, VertexKeyType, AwsKeyType, ToolLossPolicy       string
		OpenRouterEnterprise, ClaudeBetaQuery, OllamaOpenAIChat                bool
		AllowServiceTier, AllowInferenceGeo, AllowSpeed, AllowSafetyIdentifier bool
		DisableStore, AllowIncludeObfuscation                                  bool
	}
	type routingFact struct {
		Model                  string
		ChannelID, ChannelType int
		Priority               int64
		Weight                 uint
		Mapping                any
		Routes                 []routeFact
		Settings               settingsFact
	}
	facts := make([]routingFact, 0, len(abilities))
	eligibleModels := make(map[string]bool, len(modelIDs))
	modelIndex := 0
	for _, ability := range abilities {
		fact := routingFact{Model: ability.Model, ChannelID: ability.ChannelId, ChannelType: ability.ChannelType, Weight: ability.Weight, Routes: make([]routeFact, 0, 3)}
		channel := ability.RoutingChannel()
		setting, other := channel.GetSetting(), channel.GetOtherSettings()
		lossPolicy := strings.TrimSpace(other.ToolLossPolicy)
		if lossPolicy == "" {
			lossPolicy = "allow"
		}
		fact.Settings = settingsFact{
			TaskPluginBindings: setting.TaskPluginBindings(), ForceFormat: setting.ForceFormat, ThinkingToContent: setting.ThinkingToContent,
			PassThroughBody: setting.PassThroughBodyEnabled, SystemPromptOverride: setting.SystemPromptOverride,
			AzureResponsesVersion: other.AzureResponsesVersion, VertexKeyType: string(other.VertexKeyType), AwsKeyType: string(other.AwsKeyType), ToolLossPolicy: lossPolicy,
			OpenRouterEnterprise: other.IsOpenRouterEnterprise(), ClaudeBetaQuery: other.ClaudeBetaQuery, OllamaOpenAIChat: other.OllamaOpenAIChat,
			AllowServiceTier: other.AllowServiceTier, AllowInferenceGeo: other.AllowInferenceGeo, AllowSpeed: other.AllowSpeed,
			AllowSafetyIdentifier: other.AllowSafetyIdentifier, DisableStore: other.DisableStore, AllowIncludeObfuscation: other.AllowIncludeObfuscation,
		}
		for snapshot.Models[modelIndex].ModelID != ability.Model {
			modelIndex++
		}
		entry := snapshot.Models[modelIndex]
		for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
			eligible, _ := model.ChannelSatisfiesFilters(channel, ability.Model, []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: path}})
			route := routeFact{RequestPath: path, Eligible: eligible}
			if configured, matched := other.AdvancedCustom.MatchPathForModel(path, ability.Model); matched {
				// Route auth values and URL userinfo/query values can contain credentials.
				if target, err := url.Parse(strings.TrimSpace(configured.UpstreamPath)); err == nil {
					target.User, target.RawQuery, target.Fragment = nil, "", ""
					route.UpstreamPath = target.String()
				}
				route.Converter = strings.TrimSpace(configured.Converter)
				if route.Converter == "" {
					route.Converter = "none"
				}
				route.PassThroughBody = configured.PassThroughBodyEnabled
				if configured.Auth != nil {
					route.AuthType, route.AuthName = strings.TrimSpace(configured.Auth.Type), strings.TrimSpace(configured.Auth.Name)
				} else {
					route.AuthType, route.AuthName = "header", "Authorization"
				}
			}
			fact.Routes = append(fact.Routes, route)
			if eligible && entry.APIType != nil && zenRequestPath(*entry.APIType) == path {
				eligibleModels[ability.Model] = true
			}
		}
		if ability.Priority != nil {
			fact.Priority = *ability.Priority
		}
		if ability.ChannelModelMapping != nil && *ability.ChannelModelMapping != "" {
			var mapping map[string]string
			if common.UnmarshalJsonStr(*ability.ChannelModelMapping, &mapping) == nil {
				if len(mapping) > 0 {
					fact.Mapping = mapping
				}
			} else {
				fact.Mapping = *ability.ChannelModelMapping
			}
		}
		facts = append(facts, fact)
	}
	for i := range snapshot.Models {
		entry := &snapshot.Models[i]
		if entry.Enabled && !eligibleModels[entry.ModelID] {
			entry.Enabled = false
			reason := "model_route_unavailable"
			entry.DisabledReason = &reason
		}
	}
	type authorizationFact struct {
		Revision           int64
		SubscriptionID     int
		Status             string
		StartTime, EndTime int64
		AmountTotal        int64
		Exhausted          bool
	}
	authorization := authorizationFact{}
	if state.Authorization != nil {
		if state.Subscription == nil || state.Authorization.UserId != user.Id || state.Authorization.Revision <= 0 ||
			state.Subscription.Id != state.Authorization.SubscriptionId || state.Subscription.UserId != user.Id ||
			state.Subscription.UsageScope != model.SubscriptionUsageScopeZen || state.Subscription.Source != "admin" ||
			state.Subscription.AmountTotal < 0 || state.Subscription.AmountUsed < 0 ||
			(state.Subscription.Status != "active" && state.Subscription.Status != "cancelled" && state.Subscription.Status != "expired") {
			return nil, model.ErrSubscriptionFundingTargetInvalid
		}
		sub := state.Subscription
		snapshot.AuthorizationRevision = strconv.FormatInt(state.Authorization.Revision, 10)
		authorization = authorizationFact{Revision: state.Authorization.Revision, SubscriptionID: sub.Id, Status: sub.Status, StartTime: sub.StartTime, EndTime: sub.EndTime, AmountTotal: sub.AmountTotal,
			Exhausted: sub.AmountTotal > 0 && sub.AmountUsed >= sub.AmountTotal}
		if sub.Status == "active" && sub.EndTime <= state.Now {
			authorization.Status = "expired"
		}
		if sub.Status == "active" && sub.StartTime > state.Now {
			authorization.Status = "not_started"
		}
	}
	normalized, err := common.Marshal(struct {
		UserID               int
		Group                string
		Abilities            []routingFact
		Models               []dto.ZenCatalogModel
		PublishedConfig      string
		NativeMetadataStatus map[string]int
		Authorization        authorizationFact
	}{user.Id, user.Group, facts, snapshot.Models, publishedHash, nativeMetadataStatus, authorization})
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(normalized)
	snapshot.CatalogRevision = hex.EncodeToString(digest[:])
	return snapshot, nil
}

// ZenModelAdmission 是本次准入从已发布规则得到的请求内快照，不替代渠道和授权的实时检查。
// 原始模型身份与上限必须在重试时保持一致，不能使用渠道改写后的模型重新解析。
type ZenModelAdmission struct {
	Group           string
	ModelID         string
	APIType         string
	MaxOutputTokens uint64
}

// ValidateZenModelRequest 保留原有调用方式，实际准入和上限由同一次解析产生。
func ValidateZenModelRequest(userID int, modelID, apiType string, maxTokens ...*uint) (string, error) {
	admission, err := ValidateZenModelRequestWithLimit(userID, modelID, apiType, maxTokens...)
	return admission.Group, err
}

// ValidateZenModelRequestWithLimit reads only the requested model's live authority.
// For anthropic-messages the first pointer is max_tokens; a supplied legacy
// max_tokens_to_sample is additionally bounded but cannot replace that field.
func ValidateZenModelRequestWithLimit(userID int, modelID, apiType string, maxTokens ...*uint) (ZenModelAdmission, error) {
	if userID <= 0 {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenModelUnavailable, "XCODE_MODEL_UNAVAILABLE", http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	var user model.User
	if err := model.DB.Select("id", "group").First(&user, userID).Error; err != nil {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenBackendUnavailable, "XCODE_BACKEND_UNAVAILABLE", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	requestPath := zenRequestPath(apiType)
	if requestPath == "" {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenModelUnavailable, "XCODE_MODEL_UNAVAILABLE", http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	abilities, err := model.GetZenAvailableModelAbilities(user.Group, modelID)
	if err != nil {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenBackendUnavailable, "XCODE_BACKEND_UNAVAILABLE", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if len(abilities) == 0 {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenModelUnavailable, "XCODE_MODEL_UNAVAILABLE", http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	var candidates []model.Model
	err = model.DB.Select("model_name", "name_rule", "status").
		Where("model_name = ? AND name_rule = ?", modelID, model.NameRuleExact).Find(&candidates).Error
	if err != nil {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenBackendUnavailable, "XCODE_BACKEND_UNAVAILABLE", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	var metadata *model.Model
	for i := range candidates {
		if candidates[i].ModelName == modelID {
			metadata = &candidates[i]
			break
		}
	}
	release, _, err := model.GetPublishedBuiltinProviderConfig()
	if err != nil {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenBackendUnavailable, "XCODE_BACKEND_UNAVAILABLE", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	entry, err := resolveZenCatalogModel(modelID, metadata, release, abilities)
	if err != nil {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenBackendUnavailable, "XCODE_BACKEND_UNAVAILABLE", http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
	}
	if !entry.Enabled || entry.APIType == nil || *entry.APIType != apiType {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(ErrZenModelUnavailable, "XCODE_MODEL_UNAVAILABLE", http.StatusForbidden, types.ErrOptionWithSkipRetry())
	}
	supplied := false
	for _, value := range maxTokens {
		if value == nil {
			continue
		}
		supplied = true
		if uint64(*value) > entry.ModelConfig.OptionSpecs.MaxOutputTokens.Max {
			return ZenModelAdmission{}, types.NewErrorWithStatusCode(errors.New("output token limit exceeds this model's configured maximum"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	if !supplied || (apiType == dto.ZenAPITypeAnthropicMessages && (len(maxTokens) == 0 || maxTokens[0] == nil)) {
		return ZenModelAdmission{}, types.NewErrorWithStatusCode(errors.New("an explicit output token limit is required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return ZenModelAdmission{Group: user.Group, ModelID: modelID, APIType: apiType, MaxOutputTokens: entry.ModelConfig.OptionSpecs.MaxOutputTokens.Max}, nil
}

var ErrZenOutputLimit = errors.New("the managed output token limit cannot be verified")

// ZenOutputLimitError 不包含请求内容，确保本地拒绝不会触发渠道重试或禁用。
func ZenOutputLimitError() *types.NewAPIError {
	return types.NewErrorWithStatusCode(ErrZenOutputLimit, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

// ZenRelayFormatAllowed 只放行有明确最终限额字段、且经共享 HTTP 发送点的渠道类型与入站协议组合。
// 其他适配器（Gemini、Ollama、AWS 等）可能绕过共享发送点或没有可校验的字段，必须在调用适配器前拒绝。
// 这里只是参数级放行：不判断渠道 BaseURL 是官方还是自定义网关，也不证明上游一定遵守该字段。
func ZenRelayFormatAllowed(channelType int, format types.RelayFormat) bool {
	switch channelType {
	case constant.ChannelTypeOpenAI:
		return format == types.RelayFormatOpenAI || format == types.RelayFormatOpenAIResponses
	case constant.ChannelTypeAnthropic:
		return format == types.RelayFormatClaude
	case constant.ChannelTypeDeepSeek:
		return format == types.RelayFormatOpenAI
	default:
		return false
	}
}

// ValidateZenOutputBody 检查请求体中的显式输出限额，调用方应传入独立读取器。
// 不同 JSON 解析器可能选择重复键的不同值；覆盖前后的正文都必须拒绝歧义。
// 同时出现的多个限额字段（如 Chat 的 max_tokens 与 max_completion_tokens）必须数值相等。
// 它只校验宿主发出的请求参数，不能证明上游服务执行该字段。
// allowChatCompletionTokens 为 false 时（DeepSeek）只接受 max_tokens。
func ValidateZenOutputBody(reader io.Reader, apiType string, limit uint64, allowChatCompletionTokens bool) *types.NewAPIError {
	data, err := io.ReadAll(reader)
	if err != nil || !ojson.ValidBytes(data) {
		return ZenOutputLimitError()
	}
	root := ojson.GetBytesNoCopy(data, "@this")
	if !root.IsObject() {
		return ZenOutputLimitError()
	}
	var allowed map[string]bool
	var required string
	switch apiType {
	case dto.ZenAPITypeOpenAIChatCompletions:
		allowed = map[string]bool{"max_tokens": true}
		if allowChatCompletionTokens {
			allowed["max_completion_tokens"] = true
		}
	case dto.ZenAPITypeOpenAIResponses:
		allowed = map[string]bool{"max_output_tokens": true}
	case dto.ZenAPITypeAnthropicMessages:
		allowed = map[string]bool{"max_tokens": true, "max_tokens_to_sample": true}
		required = "max_tokens"
	default:
		return ZenOutputLimitError()
	}
	type node struct {
		value ojson.Result
		root  bool
	}
	stack := []node{{value: root, root: true}}
	var found, requiredFound bool
	var value uint64
	for len(stack) > 0 {
		last := len(stack) - 1
		item := stack[last]
		current := item.value
		stack = stack[:last]
		atRoot := item.root
		seen := make(map[string]bool)
		valid := true
		current.ForEach(func(key, member ojson.Result) bool {
			if current.IsObject() {
				name := key.Str
				if atRoot {
					// Go JSON 使用 Unicode 简单折叠匹配字段，仅 ToLower 会漏掉 ſ 等变体。
					// 非规范键可能被下游当作同一限额字段，故在原始与最终正文均拒绝。
					for _, sensitive := range []string{"n", "max_tokens", "max_completion_tokens", "max_output_tokens", "max_tokens_to_sample"} {
						if name != sensitive && strings.EqualFold(name, sensitive) {
							valid = false
							return false
						}
					}
					name = strings.ToLower(name)
				}
				if seen[name] {
					valid = false
					return false
				}
				seen[name] = true
				if atRoot && name == "n" {
					// 多候选的总输出无法由单候选限额证明，托管请求只允许一个候选。
					if key.Str != "n" || apiType != dto.ZenAPITypeOpenAIChatCompletions || member.Raw != "1" {
						valid = false
						return false
					}
				}
				if atRoot && (name == "max_tokens" || name == "max_completion_tokens" || name == "max_output_tokens" || name == "max_tokens_to_sample") {
					parsed, parseErr := strconv.ParseUint(member.Raw, 10, 64)
					// Go 的入站解析会忽略字段大小写，但上游未必如此；非标准拼写无法证明限额生效。
					if key.Str != name || !allowed[name] || parseErr != nil || parsed == 0 || parsed > limit || found && value != parsed {
						valid = false
						return false
					}
					found, value = true, parsed
					requiredFound = requiredFound || name == required
				}
			}
			if member.IsArray() || member.IsObject() {
				stack = append(stack, node{value: member})
			}
			return true
		})
		if !valid {
			return ZenOutputLimitError()
		}
	}
	if !found || required != "" && !requiredFound {
		return ZenOutputLimitError()
	}
	return nil
}
