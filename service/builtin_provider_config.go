package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/builtinconfig"
)

// resolveZenCatalogModel uses the same central rule inputs as the native client
// account anchors. Channel upstream URLs are not client Provider site URLs.
func resolveZenCatalogModel(modelID string, metadata *model.Model, release *builtinconfig.Release, abilities []model.ZenAvailableAbility) (dto.ZenCatalogModel, error) {
	entry := dto.ZenCatalogModel{ModelID: modelID, DisplayName: modelID}
	disable := func(reason string) dto.ZenCatalogModel {
		entry.Enabled = false
		entry.DisabledReason = &reason
		return entry
	}
	if metadata != nil && metadata.Status != 1 {
		return disable("model_disabled"), nil
	}
	protocols := []string{dto.ZenAPITypeOpenAIChatCompletions, dto.ZenAPITypeAnthropicMessages, dto.ZenAPITypeOpenAIResponses}
	// Legacy API type belongs to the migrated exact rule identity. It is no
	// longer read from the archived per-model field on catalog/admission paths.
	declared := []string{}
	for _, apiType := range protocols {
		for _, rule := range release.Config.ModelConfigRules.BuiltinProviderModelRules {
			if rule["providerId"] == "account:new-api-"+apiType && rule["modelId"] == modelID {
				declared = append(declared, apiType)
				break
			}
		}
	}
	if len(declared) > 0 {
		protocols = declared
	}
	origin, originErr := common.GetZenPublicOrigin()
	if originErr != nil && len(release.Config.ModelConfigRules.ProviderSiteRules) > 0 {
		return entry, originErr
	}
	for _, apiType := range protocols {
		id := "account:new-api-" + apiType
		provider, templateID, providerEnabled, _ := release.Provider(id)
		if api, ok := provider["api"].(map[string]any); ok {
			if configured, exists := api["type"]; exists && configured != apiType {
				continue
			}
		}
		allRoutes := len(abilities) > 0
		for _, ability := range abilities {
			eligible, _ := model.ChannelSatisfiesFilters(ability.RoutingChannel(), modelID, []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: zenRequestPath(apiType)}})
			if !eligible {
				allRoutes = false
				break
			}
		}
		if !allRoutes && len(declared) == 0 {
			continue
		}
		baseURL := origin
		if apiType != dto.ZenAPITypeAnthropicMessages {
			baseURL += "/v1"
		}
		config, err := release.Resolve(builtinconfig.ResolutionInput{ProviderID: id, TemplateID: templateID, ModelID: modelID, APIType: apiType, BaseURL: baseURL})
		if err != nil {
			return entry, err
		}
		body, err := common.Marshal(map[string]any{"schema_version": 1, "api_type": apiType, "model_config": config})
		if err != nil {
			return entry, err
		}
		parsed, err := model.ParseZenModelMetadata(string(body))
		if err != nil {
			return disable("model_config_incomplete"), nil
		}
		entry.APIType, entry.ModelConfig = &parsed.APIType, &parsed.ModelConfig
		if !providerEnabled || !parsed.ModelConfig.Enabled {
			return disable("model_config_disabled"), nil
		}
		if !allRoutes {
			return disable("model_route_unavailable"), nil
		}
		entry.Enabled = true
		return entry, nil
	}
	return disable("model_route_unavailable"), nil
}
