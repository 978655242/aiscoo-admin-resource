// Package builtinconfig implements the upstream Built-in data contract. Option
// expressions remain opaque data for the client's restricted CEL compiler.
package builtinconfig

import (
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Calcium-Ion/moejs"
	"github.com/QuantumNous/new-api/common"
	"golang.org/x/net/idna"
)

const (
	MaxSafeInteger int64 = 9007199254740991
	MaxBytes             = 1024 * 1024
	maxRules             = 10000
)

//go:embed zcode-builtin.json
var embedded []byte

func EmbeddedRelease() []byte { return slices.Clone(embedded) }

type Release struct {
	SchemaVersion int     `json:"schemaVersion"`
	Revision      int64   `json:"revision"`
	Config        Content `json:"config"`
}

type Content struct {
	ProviderConfigRules ProviderRules `json:"providerConfigRules"`
	ModelConfigRules    ModelRules    `json:"modelConfigRules"`
}

type ProviderRules struct {
	TemplateRules []map[string]any `json:"templateRules"`
	ProviderRules []map[string]any `json:"providerRules"`
}

type ModelRules struct {
	ModelRules                []map[string]any `json:"modelRules"`
	ModelAPIRules             []map[string]any `json:"modelApiRules"`
	ProviderSiteRules         []map[string]any `json:"providerSiteRules"`
	TemplateModelRules        []map[string]any `json:"templateModelRules"`
	BuiltinProviderModelRules []map[string]any `json:"builtinProviderModelRules"`
}

type ResolutionInput struct {
	ProviderID string `json:"providerId"`
	TemplateID string `json:"templateId,omitempty"`
	ModelID    string `json:"modelId"`
	APIType    string `json:"apiType,omitempty"`
	BaseURL    string `json:"baseUrl,omitempty"`
}

// Parse checks every structural leaf, including sparse/null overlays, without
// interpreting option expressions. Numbers are checked before float decoding.
func Parse(raw string) (*Release, error) {
	if len(raw) == 0 || len(raw) > MaxBytes {
		return nil, errors.New("Built-in Release must be a JSON object of at most 1 MiB")
	}
	if err := releaseDataSchema.check(common.RawMessage(raw), "release"); err != nil {
		return nil, err
	}
	// JSON integers may use exponent/decimal notation in upstream releases.
	// Normalize only after exact safe-integer validation to avoid float rounding.
	var data map[string]any
	if err := common.UnmarshalJsonStr(raw, &data); err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(data)
	if err != nil {
		return nil, err
	}
	var release Release
	if err := common.Unmarshal(encoded, &release); err != nil {
		return nil, err
	}
	var patterns []string
	count := 0
	for _, group := range release.Config.ModelConfigRules.groups() {
		count += len(group)
		for _, rule := range group {
			for _, key := range []string{"modelMatch", "apiTypeMatch", "baseUrlMatch"} {
				if pattern, ok := rule[key].(string); ok {
					patterns = append(patterns, pattern)
				}
			}
		}
	}
	if count > maxRules {
		return nil, errors.New("Built-in Release has too many model rules")
	}
	for _, group := range []struct {
		rules []map[string]any
		id    string
	}{
		{release.Config.ProviderConfigRules.TemplateRules, "templateId"},
		{release.Config.ProviderConfigRules.ProviderRules, "providerId"},
	} {
		seen := map[string]bool{}
		for _, rule := range group.rules {
			id := rule[group.id].(string)
			if seen[id] {
				return nil, fmt.Errorf("duplicate %s: %s", group.id, id)
			}
			if id == "builtin:zapi" {
				return nil, errors.New("retired builtin:zapi provider is not supported")
			}
			seen[id] = true
		}
	}
	if _, err := runRules("validate", patterns); err != nil {
		return nil, fmt.Errorf("invalid JavaScript match expression: %w", err)
	}
	return &release, nil
}

// ValidateExactModelRule applies the same publication boundary during legacy
// migration, so unsafe historical values become migration issues, not a broken
// initial publication that prevents the administrator from opening settings.
func ValidateExactModelRule(rule map[string]any) error {
	data, err := common.Marshal(rule)
	if err != nil {
		return err
	}
	group := releaseDataSchema.fields["config"].fields["modelConfigRules"].fields["builtinProviderModelRules"]
	return group.element.check(data, "builtinProviderModelRule")
}

func (r ModelRules) groups() [][]map[string]any {
	return [][]map[string]any{r.ModelRules, r.ModelAPIRules, r.ProviderSiteRules, r.TemplateModelRules, r.BuiltinProviderModelRules}
}

// Resolve has the same group order, full-match RegExp and sparse overlay rules
// as upstream ModelConfigRules.resolve. JavaScript itself owns RegExp semantics;
// unsupported syntax or an execution budget exhaustion is an explicit error.
func (r *Release) Resolve(input ResolutionInput) (map[string]any, error) {
	if len(input.ModelID) > 4096 || len(input.ProviderID) > 4096 || len(input.BaseURL) > 8192 {
		return nil, errors.New("rule resolution input is too long")
	}
	input.BaseURL = normalizeBaseURL(input.BaseURL)
	data, err := common.Marshal([]any{r.Config.ModelConfigRules, input})
	if err != nil {
		return nil, err
	}
	var args []any
	if err := common.Unmarshal(data, &args); err != nil {
		return nil, err
	}
	result, err := runRules("resolve", args...)
	if err != nil {
		return nil, err
	}
	value, ok := result.(map[string]any)
	if !ok {
		return nil, errors.New("invalid Built-in resolution result")
	}
	return value, nil
}

// Provider returns the template overlay used by the client's provider registry.
// It does not supply runtime credentials, entitlements or catalog membership.
func (r *Release) Provider(id string) (config map[string]any, templateID string, enabled bool, found bool) {
	for _, rule := range r.Config.ProviderConfigRules.ProviderRules {
		if rule["providerId"] != id {
			continue
		}
		templateID, _ = rule["templateId"].(string)
		config = map[string]any{}
		for _, template := range r.Config.ProviderConfigRules.TemplateRules {
			if template["templateId"] == templateID {
				config = overlay(config, template["config"].(map[string]any))
			}
		}
		config = overlay(config, rule["config"].(map[string]any))
		enabled, _ = rule["enabled"].(bool)
		if _, exists := rule["enabled"]; !exists {
			enabled = true
		}
		return config, templateID, enabled, true
	}
	return map[string]any{}, "", true, false
}

func overlay(base, next map[string]any) map[string]any {
	result := maps.Clone(base)
	for key, value := range next {
		previous, ok := result[key].(map[string]any)
		object, nested := value.(map[string]any)
		if ok && nested && key != "headers" {
			result[key] = overlay(previous, object)
		} else {
			result[key] = value
		}
	}
	return result
}

func normalizeBaseURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	host, err := idna.Lookup.ToASCII(strings.ToLower(parsed.Hostname()))
	if err != nil {
		return ""
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value > 65535 {
			return ""
		}
		port = strconv.Itoa(value)
	}
	if (parsed.Scheme == "https" && port == "443") || (parsed.Scheme == "http" && port == "80") {
		port = ""
	}
	parsed.Host = host
	if port != "" {
		parsed.Host += ":" + port
	}
	// WHATWG URL parsing resolves literal/escaped dot segments before the
	// upstream matcher removes trailing slashes. Keep escaped slashes intact.
	segments := []string{}
	for segment := range strings.SplitSeq(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/") {
		switch strings.ToLower(segment) {
		case ".", "%2e":
			continue
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(segments) > 0 {
				segments = segments[:len(segments)-1]
			}
		default:
			segments = append(segments, segment)
		}
	}
	parsed.RawPath = "/" + strings.Join(segments, "/")
	parsed.Path, err = url.PathUnescape(parsed.RawPath)
	if err != nil {
		return ""
	}
	serialized := parsed.String()
	// Empty query/fragment markers remain part of the serialized endpoint,
	// exactly as URL.search/URL.hash do in the upstream normalization routine.
	if parsed.Fragment == "" && strings.HasSuffix(raw, "#") {
		serialized += "#"
	}
	suffix := ""
	if parsed.RawQuery != "" {
		suffix += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		suffix += "#" + parsed.EscapedFragment()
	}
	endpoint := serialized
	if suffix != "" {
		endpoint = serialized[:len(serialized)-len(suffix)]
	}
	return strings.TrimRight(endpoint, "/") + suffix
}

var rulesModule = sync.OnceValues(func() (*moejs.Module, error) {
	return moejs.Compile("builtin-rule-resolution.js", `
export function validate(patterns) {
  for (const pattern of patterns) { new RegExp('^(?:' + pattern + ')$'); new RegExp('^(?:' + pattern + ')$', 'i'); }
  return true;
}
function overlay(base, next) {
  const out = {...base};
  for (const key of Object.keys(next)) {
    const value = next[key], previous = out[key];
    out[key] = value !== null && previous !== null && typeof value === 'object' && typeof previous === 'object' && !Array.isArray(value) && !Array.isArray(previous) ? overlay(previous, value) : value;
  }
  return out;
}
function matches(pattern, value, ignoreCase) { return new RegExp('^(?:' + pattern + ')$', ignoreCase ? 'i' : undefined).test(value); }
export function resolve(groups, input) {
  let result = {};
  for (const name of ['modelRules','modelApiRules','providerSiteRules','templateModelRules','builtinProviderModelRules']) {
    for (const rule of groups[name]) {
      if (name === 'builtinProviderModelRules') {
        if (rule.providerId !== input.providerId || rule.modelId !== input.modelId) continue;
      } else if (name === 'templateModelRules') {
        if (rule.templateId !== input.templateId || rule.modelId !== input.modelId) continue;
      } else {
        if (!matches(rule.modelMatch, input.modelId, true)) continue;
        if (rule.apiTypeMatch !== undefined && (!input.apiType || !matches(rule.apiTypeMatch, input.apiType))) continue;
        if (name === 'providerSiteRules' && (!input.baseUrl || !matches(rule.baseUrlMatch, input.baseUrl))) continue;
      }
      result = overlay(result, rule.config);
    }
  }
  return result;
}`)
})

func runRules(name string, args ...any) (any, error) {
	module, err := rulesModule()
	if err != nil {
		return nil, err
	}
	runtime := moejs.NewRuntime(moejs.Options{DisableDynamicCode: true, MemoryLimit: 16 * 1024 * 1024})
	budget := time.AfterFunc(200*time.Millisecond, func() { runtime.Interrupt("Built-in rule execution budget exceeded") })
	defer budget.Stop()
	if err := runtime.Load(module); err != nil {
		return nil, err
	}
	hook, err := module.Hook(name)
	if err != nil {
		return nil, err
	}
	values := make([]moejs.Value, len(args))
	for i, arg := range args {
		values[i], err = runtime.FromGo(arg)
		if err != nil {
			return nil, err
		}
	}
	result, err := runtime.Call(hook, values...)
	if err != nil {
		return nil, err
	}
	return runtime.ToGo(result)
}

// schema is deliberately small: the upstream data schema contains only strict
// objects, bounded arrays and primitive sparse leaves. JSON number lexemes are
// checked as rationals to reject unsafe/fractional values before rounding.
type schema struct {
	kind               string
	fields             map[string]schema
	element            *schema
	optional, nullable bool
	values             []string
	max                int
}

func object(fields map[string]schema) schema  { return schema{kind: "object", fields: fields} }
func array(element schema) schema             { return schema{kind: "array", element: &element, max: maxRules} }
func optional(s schema, nullable bool) schema { s.optional, s.nullable = true, nullable; return s }
func enumeration(values ...string) schema     { return schema{kind: "string", values: values, max: 4096} }
func sparse(fields map[string]schema) schema {
	result := make(map[string]schema, len(fields))
	for key, value := range fields {
		result[key] = optional(value, true)
	}
	return object(result)
}

var credentialName = regexp.MustCompile(`(?i)(authorization|cookie|api[-_]?key|access[-_]?token|refresh[-_]?token|secret|password)`)
var credentialValue = regexp.MustCompile(`(?i)(\bsk-[a-z0-9_-]{8,}|\bbearer\s+[a-z0-9_.-]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----)`)

// Anonymous immutable releases may carry public protocol/version metadata only.
// 凭据可能完全不具备 token 特征；使用字段 allowlist，不能只靠值或名称黑名单。
var publicStaticHeaders = []string{"accept", "accept-encoding", "accept-language", "content-type", "anthropic-version", "anthropic-beta", "openai-beta", "api-version", "x-api-version"}
var publicURLQueryKeys = []string{"api-version", "api_version", "apiVersion", "version", "format", "alt", "stream", "tab", "lang", "locale"}

func (s schema) check(raw common.RawMessage, path string) error {
	kind := common.GetJsonType(raw)
	if kind == "null" && s.nullable {
		return nil
	}
	fail := func() error { return fmt.Errorf("invalid Built-in field %s (expected %s)", path, s.kind) }
	switch s.kind {
	case "access", "template-access":
		if kind != "object" {
			return fail()
		}
		var fields map[string]common.RawMessage
		if err := common.Unmarshal(raw, &fields); err != nil {
			return fail()
		}
		var accessType string
		if err := common.Unmarshal(fields["type"], &accessType); err != nil {
			return fail()
		}
		allowed := map[string]schema{"type": enumeration(accessType)}
		switch accessType {
		case "api-key", "zhipu-coding-plan-api-key":
			allowed["apiKeyManagementUrl"] = optional(schema{kind: "url", max: 8192}, true)
			if s.kind != "template-access" {
				allowed["apiKey"] = optional(schema{kind: "secret", max: 4096}, true)
			}
		case "zhipu-account":
			if s.kind == "template-access" {
				return fail()
			}
			allowed["accountType"] = optional(enumeration("zai", "bigmodel"), true)
			allowed["mode"] = optional(enumeration("start-plan", "individual-coding-plan", "team-coding-plan", "off-peak", "highspeed"), true)
			allowed["entitled"] = optional(schema{kind: "bool"}, true)
		default:
			return fail()
		}
		return object(allowed).check(raw, path)
	case "secret":
		var value string
		if kind != "string" || common.Unmarshal(raw, &value) != nil || value != "" {
			return fmt.Errorf("credential values are forbidden at %s", path)
		}
	case "object":
		if kind != "object" {
			return fail()
		}
		var values map[string]common.RawMessage
		if err := common.Unmarshal(raw, &values); err != nil {
			return fail()
		}
		for name, value := range values {
			field, known := s.fields[name]
			if !known {
				return fmt.Errorf("unknown Built-in field %s.%s", path, name)
			}
			if err := field.check(value, path+"."+name); err != nil {
				return err
			}
		}
		for name, field := range s.fields {
			if _, exists := values[name]; !exists && !field.optional {
				return fmt.Errorf("missing Built-in field %s.%s", path, name)
			}
		}
	case "headers", "names":
		if kind != "object" {
			return fail()
		}
		var values map[string]common.RawMessage
		if err := common.Unmarshal(raw, &values); err != nil {
			return fail()
		}
		for name, value := range values {
			if s.kind == "headers" && !slices.Contains(publicStaticHeaders, strings.ToLower(name)) {
				return fmt.Errorf("only public static protocol headers are allowed at %s; header %q is forbidden", path, name)
			}
			if s.kind == "names" && name != "zh-CN" && name != "en-US" {
				return fail()
			}
			valueKind := "string"
			if s.kind == "headers" {
				valueKind = "header-value"
			}
			if err := (schema{kind: valueKind, max: 8192}).check(value, path+"."+name); err != nil {
				return err
			}
		}
	case "array", "values":
		if kind != "array" {
			return fail()
		}
		var values []common.RawMessage
		if err := common.Unmarshal(raw, &values); err != nil || len(values) > s.max || (s.kind == "values" && len(values) == 0) {
			return fail()
		}
		seen := map[string]bool{}
		for i, value := range values {
			if err := s.element.check(value, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
			if s.kind == "values" {
				var text string
				_ = common.Unmarshal(value, &text)
				if strings.TrimSpace(text) == "" || seen[text] {
					return fail()
				}
				seen[text] = true
			}
		}
	case "bool":
		if kind != "bool" && kind != "boolean" {
			return fail()
		}
	case "integer", "revision", "version":
		if kind != "number" {
			return fail()
		}
		literal := strings.TrimSpace(string(raw))
		if len(literal) > 128 {
			return fail()
		}
		if _, exponent, exists := strings.Cut(strings.ToLower(literal), "e"); exists {
			value, err := strconv.Atoi(exponent)
			if err != nil || value < -1000 || value > 1000 {
				return fail()
			}
		}
		number, ok := new(big.Rat).SetString(literal)
		if !ok || !number.IsInt() || !number.Num().IsInt64() {
			return fail()
		}
		value := number.Num().Int64()
		if value < 0 || value > MaxSafeInteger || (s.kind == "integer" && value == 0) || (s.kind == "version" && value != 1) {
			return fail()
		}
	default:
		if kind != "string" {
			return fail()
		}
		var value string
		if err := common.Unmarshal(raw, &value); err != nil {
			return fail()
		}
		if (len(value) == 0 && s.kind != "header-value") || len(value) > s.max || credentialValue.MatchString(value) || (s.kind == "header-value" && strings.ContainsAny(value, "\r\n")) {
			return fail()
		}
		if len(s.values) > 0 && !slices.Contains(s.values, value) {
			return fail()
		}
		if s.kind == "map" && (strings.TrimSpace(value) == "" || credentialName.MatchString(value)) {
			return fail()
		}
		if s.kind == "url" {
			u, err := url.Parse(value)
			if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
				return fail()
			}
			query, err := url.ParseQuery(u.RawQuery)
			if err != nil {
				return fail()
			}
			for key := range query {
				if !slices.Contains(publicURLQueryKeys, key) {
					return fmt.Errorf("only public version, protocol or navigation query parameters are allowed at %s; parameter %q is forbidden", path, key)
				}
			}
		}
		if s.kind == "secret" && value != "" {
			return fmt.Errorf("credential values are forbidden at %s", path)
		}
	}
	return nil
}

var releaseDataSchema = func() schema {
	text := schema{kind: "string", max: 4096}
	boolean := schema{kind: "bool"}
	integer := schema{kind: "integer"}
	pattern := schema{kind: "pattern", max: 2048}
	urlField := schema{kind: "url", max: 8192}
	expression := schema{kind: "map", max: 8192}
	reasoningValues := schema{kind: "values", element: &text, max: 256}
	properties := sparse(map[string]schema{
		"requiresMfjsToolSchema": boolean, "contextWindow": integer,
		"inputFormat":      sparse(map[string]schema{"supportsText": boolean, "supportsImage": boolean, "supportsVideo": boolean, "supportsAudio": boolean, "supportsPdf": boolean}),
		"outputFormat":     sparse(map[string]schema{"supportsText": boolean}),
		"supportsToolCall": boolean, "supportsJsonSchemaOutput": boolean, "supportsNativeWebSearch": boolean, "supportsMidConversationSystem": boolean,
	})
	model := sparse(map[string]schema{"enabled": boolean, "properties": properties, "optionSpecs": sparse(map[string]schema{
		"reasoningLevel":  sparse(map[string]schema{"values": reasoningValues, "map": expression}),
		"maxOutputTokens": sparse(map[string]schema{"max": integer, "map": expression}),
	})})
	api := sparse(map[string]schema{"type": enumeration("anthropic-messages", "openai-chat-completions", "openai-responses"), "baseUrl": urlField, "headers": {kind: "headers"}})
	access := schema{kind: "access"}
	providerFields := map[string]schema{
		"group":  enumeration("zai-family", "bigmodel-family"),
		"logo":   optional(object(map[string]schema{"type": enumeration("builtin"), "key": text}), true),
		"access": optional(access, true), "api": optional(api, true), "builtinModelIds": optional(array(text), true),
		"visibility": optional(enumeration("visible", "hidden"), true),
	}
	provider := object(map[string]schema{"providerId": text, "templateId": optional(text, true), "providerName": optional(text, true), "enabled": optional(boolean, false), "config": object(providerFields)})
	template := object(map[string]schema{"templateId": text, "templateNameMap": {kind: "names"}, "config": sparse(map[string]schema{"logo": providerFields["logo"], "access": {kind: "template-access"}, "api": api, "builtinModelIds": array(text)})})
	return object(map[string]schema{"schemaVersion": {kind: "version"}, "revision": {kind: "revision"}, "config": object(map[string]schema{
		"providerConfigRules": object(map[string]schema{"templateRules": array(template), "providerRules": array(provider)}),
		"modelConfigRules": object(map[string]schema{
			"modelRules":                array(object(map[string]schema{"modelMatch": pattern, "config": model})),
			"modelApiRules":             array(object(map[string]schema{"modelMatch": pattern, "apiTypeMatch": pattern, "config": model})),
			"providerSiteRules":         array(object(map[string]schema{"modelMatch": pattern, "apiTypeMatch": optional(pattern, false), "baseUrlMatch": pattern, "config": model})),
			"templateModelRules":        array(object(map[string]schema{"templateId": text, "modelId": text, "config": model})),
			"builtinProviderModelRules": array(object(map[string]schema{"providerId": text, "modelId": text, "config": model})),
		}),
	})})
}()
