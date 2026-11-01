package dto

const (
	ZenModelSchemaVersion                  = 1
	ZenAPITypeAnthropicMessages            = "anthropic-messages"
	ZenAPITypeOpenAIChatCompletions        = "openai-chat-completions"
	ZenAPITypeOpenAIResponses              = "openai-responses"
	ZenModelConfigMaxBytes                 = 64 * 1024
	ZenOptionMapMaxBytes                   = 8 * 1024
	ZenMaxSafeInteger               uint64 = 9007199254740991
)

type ZenModelMetadata struct {
	SchemaVersion int            `json:"schema_version"`
	APIType       string         `json:"api_type"`
	ModelConfig   ZenModelConfig `json:"model_config"`
}

type ZenModelConfig struct {
	Enabled     bool                `json:"enabled"`
	Properties  ZenModelProperties  `json:"properties"`
	OptionSpecs ZenModelOptionSpecs `json:"optionSpecs"`
}

type ZenModelProperties struct {
	RequiresMfjsToolSchema        bool                 `json:"requiresMfjsToolSchema"`
	ContextWindow                 uint64               `json:"contextWindow"`
	InputFormat                   ZenModelInputFormat  `json:"inputFormat"`
	OutputFormat                  ZenModelOutputFormat `json:"outputFormat"`
	SupportsToolCall              bool                 `json:"supportsToolCall"`
	SupportsJsonSchemaOutput      bool                 `json:"supportsJsonSchemaOutput"`
	SupportsNativeWebSearch       bool                 `json:"supportsNativeWebSearch"`
	SupportsMidConversationSystem bool                 `json:"supportsMidConversationSystem"`
}

type ZenModelInputFormat struct {
	SupportsText  bool `json:"supportsText"`
	SupportsImage bool `json:"supportsImage"`
	SupportsVideo bool `json:"supportsVideo"`
	SupportsAudio bool `json:"supportsAudio"`
	SupportsPdf   bool `json:"supportsPdf"`
}

type ZenModelOutputFormat struct {
	SupportsText bool `json:"supportsText"`
}

type ZenModelOptionSpecs struct {
	ReasoningLevel  ZenModelReasoningLevel  `json:"reasoningLevel"`
	MaxOutputTokens ZenModelMaxOutputTokens `json:"maxOutputTokens"`
}

type ZenModelReasoningLevel struct {
	Values []string `json:"values"`
	Map    string   `json:"map"`
}

type ZenModelMaxOutputTokens struct {
	Max uint64 `json:"max"`
	Map string `json:"map"`
}
