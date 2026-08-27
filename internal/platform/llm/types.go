package llm

import (
	"encoding/json"

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
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
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
	Reasoning           *Reasoning      `json:"reasoning,omitempty"`
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
	Description string `json:"description,omitempty"`
}

type AnthropicRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int64              `json:"max_tokens"`
	Messages      []AnthropicMessage `json:"messages"`
	System        string             `json:"system,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Stream        bool               `json:"stream"`
	Tools         []AnthropicTool    `json:"tools,omitempty"`
	Metadata      *AnthropicMetadata `json:"metadata,omitempty"`
}

type AnthropicMetadata struct {
	UserID string `json:"user_id"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}
