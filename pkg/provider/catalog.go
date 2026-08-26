package provider

import "strings"

const (
	AuthAPIKey = "api_key"
	AuthOAuth  = "oauth"

	ProtocolOpenAI      = "openai"
	ProtocolAnthropic   = "anthropic"
	ProtocolCodex       = "codex"
	ProtocolCopilot     = "copilot"
	ProtocolCursor      = "cursor"
	ProtocolKimi        = "kimi"
	ProtocolKiro        = "kiro"
	ProtocolAntigravity = "antigravity"
)

// Definition is safe, static provider metadata exposed to the console and API.
// Secrets and mutable connection state never belong in this catalog.
type Definition struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Description          string `json:"description"`
	Auth                 string `json:"auth"`
	Protocol             string `json:"protocol"`
	DefaultBaseURL       string `json:"default_base_url"`
	ModelPrefix          string `json:"model_prefix"`
	CustomBaseURL        bool   `json:"custom_base_url"`
	OAuthSupported       bool   `json:"oauth_supported"`
	OAuthRefreshRequired bool   `json:"oauth_refresh_required"`
	QuotaSupported       bool   `json:"quota_supported"`
}

var definitions = []Definition{
	{ID: "claude", Name: "Claude Code", Description: "Connect a Claude subscription using the browser OAuth flow.", Auth: AuthOAuth, Protocol: ProtocolAnthropic, DefaultBaseURL: "https://api.anthropic.com", ModelPrefix: "cc", OAuthSupported: true, OAuthRefreshRequired: true, QuotaSupported: true},
	{ID: "codex", Name: "OpenAI Codex", Description: "Connect a ChatGPT/Codex subscription using browser OAuth.", Auth: AuthOAuth, Protocol: ProtocolCodex, DefaultBaseURL: "https://chatgpt.com/backend-api", ModelPrefix: "cx", OAuthSupported: true, OAuthRefreshRequired: true, QuotaSupported: true},
	{ID: "github-copilot", Name: "GitHub Copilot", Description: "Connect Copilot using GitHub's device authorization flow.", Auth: AuthOAuth, Protocol: ProtocolCopilot, DefaultBaseURL: "https://api.githubcopilot.com", ModelPrefix: "ghc", OAuthSupported: true},
	{ID: "cursor", Name: "Cursor", Description: "Connect Cursor using deep-control login or imported credentials.", Auth: AuthOAuth, Protocol: ProtocolCursor, DefaultBaseURL: "https://api2.cursor.sh", ModelPrefix: "cursor", OAuthSupported: true},
	{ID: "grok-build", Name: "Grok Build", Description: "Connect the Grok coding subscription using device authorization.", Auth: AuthOAuth, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://cli-chat-proxy.grok.com/v1", ModelPrefix: "gb", OAuthSupported: true, OAuthRefreshRequired: true},
	{ID: "xai-oauth", Name: "xAI Grok", Description: "Connect xAI API entitlements using browser OAuth.", Auth: AuthOAuth, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.x.ai/v1", ModelPrefix: "xai", OAuthSupported: true, OAuthRefreshRequired: true},
	{ID: "kimi-code", Name: "Kimi Code", Description: "Connect Kimi Code using device authorization.", Auth: AuthOAuth, Protocol: ProtocolKimi, DefaultBaseURL: "https://api.kimi.com/coding", ModelPrefix: "kimi", OAuthSupported: true, OAuthRefreshRequired: true},
	{ID: "cline", Name: "Cline", Description: "Connect a Cline account using its browser callback flow.", Auth: AuthOAuth, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.cline.bot/api/v1", ModelPrefix: "cline", OAuthSupported: true},
	{ID: "clinepass", Name: "ClinePass", Description: "Connect a ClinePass account using the Cline callback flow.", Auth: AuthOAuth, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.cline.bot/api/v1", ModelPrefix: "clinepass", OAuthSupported: true},
	{ID: "kilo-code", Name: "Kilo Code", Description: "Connect Kilo Code using device authorization.", Auth: AuthOAuth, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.kilo.ai/api/openrouter", ModelPrefix: "kilo", OAuthSupported: true},
	{ID: "kiro", Name: "Kiro", Description: "Connect Kiro through AWS Builder ID device authorization.", Auth: AuthOAuth, Protocol: ProtocolKiro, DefaultBaseURL: "https://codewhisperer.us-east-1.amazonaws.com", ModelPrefix: "kiro", OAuthSupported: true, OAuthRefreshRequired: true, QuotaSupported: true},
	{ID: "amazon-q", Name: "Amazon Q Developer", Description: "Connect Amazon Q Developer through AWS Builder ID.", Auth: AuthOAuth, Protocol: ProtocolKiro, DefaultBaseURL: "https://codewhisperer.us-east-1.amazonaws.com", ModelPrefix: "amazonq", OAuthSupported: true, OAuthRefreshRequired: true, QuotaSupported: true},
	{ID: "antigravity", Name: "Google Antigravity", Description: "Connect Google Cloud Code Assist using browser OAuth.", Auth: AuthOAuth, Protocol: ProtocolAntigravity, DefaultBaseURL: "https://cloudcode-pa.googleapis.com", ModelPrefix: "ag", OAuthSupported: true, OAuthRefreshRequired: true},
	{ID: "openai", Name: "OpenAI", Description: "OpenAI API models and compatible chat completions.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.openai.com/v1", ModelPrefix: "openai"},
	{ID: "anthropic", Name: "Anthropic", Description: "Claude models using an Anthropic API key.", Auth: AuthAPIKey, Protocol: ProtocolAnthropic, DefaultBaseURL: "https://api.anthropic.com", ModelPrefix: "anthropic"},
	{ID: "gemini", Name: "Google Gemini", Description: "Gemini through Google's OpenAI-compatible API.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", ModelPrefix: "gemini"},
	{ID: "groq", Name: "Groq", Description: "Low-latency hosted open models.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.groq.com/openai/v1", ModelPrefix: "groq"},
	{ID: "openrouter", Name: "OpenRouter", Description: "One API key for models from many providers.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://openrouter.ai/api/v1", ModelPrefix: "openrouter"},
	{ID: "opencode-zen", Name: "OpenCode Zen", Description: "OpenCode Zen hosted and free-tier models.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://opencode.ai/zen/v1", ModelPrefix: "ocz", QuotaSupported: true},
	{ID: "opencode-go", Name: "OpenCode Go", Description: "OpenCode Go models using an OpenCode API key.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://opencode.ai/zen/go/v1", ModelPrefix: "ocg", QuotaSupported: true},
	{ID: "xai", Name: "xAI", Description: "Grok models through the xAI API.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.x.ai/v1", ModelPrefix: "xai"},
	{ID: "deepseek", Name: "DeepSeek", Description: "DeepSeek chat and reasoning models.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.deepseek.com/v1", ModelPrefix: "deepseek"},
	{ID: "moonshot", Name: "Moonshot", Description: "Moonshot and Kimi API models.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://api.moonshot.ai/v1", ModelPrefix: "moonshot"},
	{ID: "qwen", Name: "Qwen", Description: "Alibaba DashScope OpenAI-compatible models.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, DefaultBaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", ModelPrefix: "qwen"},
	{ID: "openai-compatible", Name: "OpenAI-compatible", Description: "A custom OpenAI-compatible endpoint.", Auth: AuthAPIKey, Protocol: ProtocolOpenAI, ModelPrefix: "custom", CustomBaseURL: true},
}

func Catalog() []Definition {
	out := make([]Definition, len(definitions))
	copy(out, definitions)
	return out
}

func Lookup(id string) (Definition, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, definition := range definitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

func ResolveBaseURL(id, supplied string) (string, bool) {
	definition, ok := Lookup(id)
	if !ok {
		return "", false
	}
	if value := strings.TrimRight(strings.TrimSpace(supplied), "/"); value != "" {
		return value, true
	}
	return definition.DefaultBaseURL, definition.DefaultBaseURL != ""
}

func ProtocolFor(id string) string {
	if definition, ok := Lookup(id); ok {
		return definition.Protocol
	}
	return ""
}

func UsesAnthropicWire(id string) bool {
	protocol := ProtocolFor(id)
	return protocol == ProtocolAnthropic || protocol == ProtocolKimi
}

func PublicModelID(providerID, upstream string) string {
	definition, ok := Lookup(providerID)
	if !ok || definition.ModelPrefix == "" {
		return upstream
	}
	return definition.ModelPrefix + "/" + strings.TrimPrefix(upstream, definition.ModelPrefix+"/")
}
