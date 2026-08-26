package llm

// ClaudeCodeAdapter keeps the Claude Code subscription as an explicit
// provider while reusing the project's Anthropic wire translator.
type ClaudeCodeAdapter struct {
	*AnthropicAdapter
}
