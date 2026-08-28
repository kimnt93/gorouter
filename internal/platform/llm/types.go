package llm

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kimnt93/gorouter/pkg/entities"
)

type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type Tool struct {
	Type         string        `json:"type"`
	Function     ToolFunction  `json:"function"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type Message struct {
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content,omitempty"`
	Name         string          `json:"name,omitempty"`
	ToolCallID   string          `json:"tool_call_id,omitempty"`
	ToolCalls    []ToolCall      `json:"tool_calls,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

type ChatRequest struct {
	Model               string          `json:"model"`
	Messages            []Message       `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	N                   *int            `json:"n,omitempty"`
	Stream              bool            `json:"stream,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`
	MaxTokens           *int64          `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int64          `json:"max_completion_tokens,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"`
	Seed                *int64          `json:"seed,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	User                string          `json:"user,omitempty"`
	PromptCacheKey      string          `json:"prompt_cache_key,omitempty"`
	SessionID           string          `json:"session_id,omitempty"`
	ConversationID      string          `json:"conversation_id,omitempty"`
	Reasoning           *Reasoning      `json:"reasoning,omitempty"`
}

type CacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

// SupportsOpenAIPromptCacheKey is deliberately allowlisted. Generic OpenAI
// compatibility does not imply support for prompt_cache_key, and several
// providers reject unknown request fields.
func SupportsOpenAIPromptCacheKey(providerID string) bool {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "openai", "codex", "opencode-zen", "opencode-go", "grok-build":
		return true
	default:
		return false
	}
}

func StablePromptCacheKey(req *ChatRequest) string {
	if req == nil {
		return ""
	}
	var prefix strings.Builder
	for _, message := range req.Messages {
		if message.Role != "system" && message.Role != "developer" {
			break
		}
		prefix.WriteString(message.Role)
		prefix.WriteByte(0)
		prefix.Write(message.Content)
		prefix.WriteByte(0)
	}
	if len(req.Tools) > 0 {
		encoded, _ := json.Marshal(req.Tools)
		prefix.Write(encoded)
	}
	if prefix.Len() == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(prefix.String()))
	return fmt.Sprintf("gorouter-%x", sum[:16])
}

// ProviderPromptCacheKey prefers the caller's per-conversation identity and
// falls back to a stable reusable-prefix key. Provider adapters use this for
// upstream cache partitioning; routing affinity remains explicit-only so a
// common system prompt cannot collapse independent conversations onto one
// credential.
func ProviderPromptCacheKey(req *ChatRequest) string {
	if value := req.ExplicitRouteAffinity(); value != "" {
		return value
	}
	return StablePromptCacheKey(req)
}

// StableConversationID returns a UUID-shaped, non-reversible conversation ID
// for providers whose prompt cache is partitioned by a conversation field.
func StableConversationID(req *ChatRequest) string {
	key := ProviderPromptCacheKey(req)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}

// ExplicitRouteAffinity returns only caller-supplied conversation identity.
// The automatically derived prompt-cache key is intentionally excluded: a
// common system/tool prefix can be shared by many independent conversations
// and must not collapse round-robin distribution onto one credential.
func (req *ChatRequest) ExplicitRouteAffinity() string {
	if req == nil {
		return ""
	}
	for _, value := range []string{req.PromptCacheKey, req.SessionID, req.ConversationID} {
		if value = strings.TrimSpace(value); value != "" && len(value) <= 512 {
			return value
		}
	}
	return ""
}

// Reasoning stays request-scoped. It is never encoded in a public model ID.
type Reasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

func (r *ChatRequest) OutputLimit() int64 {
	if r.MaxCompletionTokens != nil && *r.MaxCompletionTokens > 0 {
		return *r.MaxCompletionTokens
	}
	if r.MaxTokens != nil && *r.MaxTokens > 0 {
		return *r.MaxTokens
	}
	return 0
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

func (u *Usage) UnmarshalJSON(data []byte) error {
	type wireUsage struct {
		PromptTokens             int64 `json:"prompt_tokens"`
		CompletionTokens         int64 `json:"completion_tokens"`
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadTokens          int64 `json:"cache_read_tokens"`
		CacheWriteTokens         int64 `json:"cache_write_tokens"`
		CachedTokens             int64 `json:"cached_tokens"`
		PromptCacheHitTokens     int64 `json:"prompt_cache_hit_tokens"`
		PromptCacheMissTokens    int64 `json:"prompt_cache_miss_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		PromptDetails            struct {
			CachedTokens        int64 `json:"cached_tokens"`
			CacheWriteTokens    int64 `json:"cache_write_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_tokens"`
		} `json:"prompt_tokens_details"`
		InputDetails struct {
			CachedTokens        int64 `json:"cached_tokens"`
			CacheWriteTokens    int64 `json:"cache_write_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_tokens"`
		} `json:"input_tokens_details"`
	}
	var wire wireUsage
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	totalInput := wire.PromptTokens
	if totalInput == 0 {
		totalInput = wire.InputTokens
	}
	cacheRead := wire.CacheReadTokens
	cacheWrite := wire.CacheWriteTokens
	if cacheRead == 0 {
		cacheRead = max(wire.PromptCacheHitTokens, wire.CacheReadInputTokens)
	}
	if cacheWrite == 0 {
		cacheWrite = wire.CacheCreationInputTokens
	}
	detailsRead := wire.PromptDetails.CachedTokens
	detailsWrite := max(wire.PromptDetails.CacheWriteTokens, wire.PromptDetails.CacheCreationTokens)
	if wire.InputDetails.CachedTokens > 0 || wire.InputDetails.CacheWriteTokens > 0 || wire.InputDetails.CacheCreationTokens > 0 {
		detailsRead = wire.InputDetails.CachedTokens
		detailsWrite = max(wire.InputDetails.CacheWriteTokens, wire.InputDetails.CacheCreationTokens)
	}
	if detailsRead > 0 {
		cacheRead = detailsRead
	}
	if detailsWrite > 0 {
		cacheWrite = detailsWrite
	}
	if cacheRead == 0 {
		cacheRead = wire.CachedTokens
	}
	// Standard OpenAI details report cached and created tokens as subsets of
	// prompt/input_tokens. Store only the uncached remainder in PromptTokens so
	// provider-cache rates and costs do not double-count the cached prefix.
	if detailsRead > 0 || detailsWrite > 0 {
		totalInput = max(int64(0), totalInput-detailsRead-detailsWrite)
	} else if wire.PromptCacheMissTokens > 0 {
		totalInput = wire.PromptCacheMissTokens
	} else if wire.PromptCacheHitTokens > 0 {
		totalInput = max(int64(0), totalInput-wire.PromptCacheHitTokens)
	}
	u.PromptTokens = totalInput
	u.CompletionTokens = wire.CompletionTokens
	if u.CompletionTokens == 0 {
		u.CompletionTokens = wire.OutputTokens
	}
	u.CacheReadTokens = cacheRead
	u.CacheWriteTokens = cacheWrite
	return nil
}

func (u Usage) Total() int64 {
	return u.PromptTokens + u.CompletionTokens
}

func (u Usage) TokenUsage() entities.TokenUsage {
	return entities.TokenUsage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
	}
}

type ResponseMessage struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Delta struct {
	Role             string     `json:"role,omitempty"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

type Choice struct {
	Index        int              `json:"index"`
	Message      *ResponseMessage `json:"message,omitempty"`
	Delta        *Delta           `json:"delta,omitempty"`
	FinishReason string           `json:"finish_reason"`
}

type Response struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Chunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
}

type ChunkChoice struct {
	Index        int    `json:"index"`
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type ModelInfo struct {
	ID            string          `json:"id"`
	Object        string          `json:"object"`
	OwnedBy       string          `json:"owned_by"`
	UpstreamModel string          `json:"upstream_model,omitempty"`
	Pricing       *entities.Price `json:"pricing,omitempty"`
}

type ModelList struct {
	Object string           `json:"object"`
	Data   []ModelInfo      `json:"data"`
	Models []CodexModelInfo `json:"models"`
}

// CodexModelInfo is the model-catalog shape consumed by current Codex CLI
// custom providers. It intentionally coexists with the OpenAI-compatible data
// list because the two clients require different identifiers and metadata.
type CodexModelInfo struct {
	Slug                     string             `json:"slug"`
	DisplayName              string             `json:"display_name"`
	Description              string             `json:"description"`
	ModelMessages            CodexModelMessages `json:"model_messages"`
	DefaultReasoningLevel    string             `json:"default_reasoning_level"`
	SupportedReasoningLevels []ReasoningLevel   `json:"supported_reasoning_levels"`
	ShellType                string             `json:"shell_type"`
	Visibility               string             `json:"visibility"`
	SupportedInAPI           bool               `json:"supported_in_api"`
	Priority                 int                `json:"priority"`
	IncludeSkillsUsage       bool               `json:"include_skills_usage_instructions"`
	IncludePluginUsage       bool               `json:"include_plugin_usage_instructions"`
	IncludeAppsUsage         bool               `json:"include_apps_usage_instructions"`
	DefaultReasoningSummary  string             `json:"default_reasoning_summary"`
	ApplyPatchToolType       string             `json:"apply_patch_tool_type"`
	WebSearchToolType        string             `json:"web_search_tool_type"`
	ContextWindow            int                `json:"context_window"`
	MaxContextWindow         int                `json:"max_context_window"`
	TruncationPolicy         TruncationPolicy   `json:"truncation_policy"`
	SupportsOriginalImage    bool               `json:"supports_image_detail_original"`
	CompHash                 string             `json:"comp_hash"`
	EffectiveContextPercent  int                `json:"effective_context_window_percent"`
	ExperimentalTools        []string           `json:"experimental_supported_tools"`
	InputModalities          []string           `json:"input_modalities"`
	SupportsSearchTool       bool               `json:"supports_search_tool"`
	UseResponsesLite         bool               `json:"use_responses_lite"`
	NodeReplAutoReview       bool               `json:"node_repl_auto_review_required"`
	NodeReplDisabled         bool               `json:"node_repl_disabled"`
	ToolMode                 *string            `json:"tool_mode"`
	MultiAgentVersion        *string            `json:"multi_agent_version"`
	SupportsReasoningSummary bool               `json:"supports_reasoning_summary_parameter"`
	SupportsParallelTools    bool               `json:"supports_parallel_tool_calls"`
	SupportVerbosity         bool               `json:"support_verbosity"`
	DefaultVerbosity         string             `json:"default_verbosity"`
}

type CodexModelMessages struct {
	InstructionsTemplate string `json:"instructions_template"`
}

type TruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

type ReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

type AnthropicRequest struct {
	Model         string                  `json:"model"`
	MaxTokens     int64                   `json:"max_tokens"`
	Messages      []AnthropicMessage      `json:"messages"`
	System        []AnthropicContentBlock `json:"system,omitempty"`
	Temperature   *float64                `json:"temperature,omitempty"`
	TopP          *float64                `json:"top_p,omitempty"`
	StopSequences []string                `json:"stop_sequences,omitempty"`
	Stream        bool                    `json:"stream"`
	Tools         []AnthropicTool         `json:"tools,omitempty"`
	Metadata      *AnthropicMetadata      `json:"metadata,omitempty"`
}

type AnthropicMetadata struct {
	UserID string `json:"user_id"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

type AnthropicContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      string          `json:"content,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}

type AnthropicTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
}
