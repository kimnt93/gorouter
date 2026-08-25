package llm

import (
	"encoding/json"
	"strings"
)

const charsPerToken = 4

func EstimateTextTokens(s string) int64 {
	n := len(s)
	if n == 0 {
		return 0
	}
	return int64((n + charsPerToken - 1) / charsPerToken)
}

func contentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}

func (r *ChatRequest) PromptText() string {
	var b strings.Builder
	for i := range r.Messages {
		b.WriteString(r.Messages[i].Role)
		b.WriteByte(' ')
		b.WriteString(contentText(r.Messages[i].Content))
		b.WriteByte('\n')
		for _, tc := range r.Messages[i].ToolCalls {
			b.WriteString(tc.Function.Name)
			b.WriteByte(' ')
			b.WriteString(tc.Function.Arguments)
			b.WriteByte(' ')
		}
	}
	for _, tl := range r.Tools {
		b.WriteString(tl.Function.Name)
		b.WriteByte(' ')
		b.WriteString(tl.Function.Description)
		b.WriteByte(' ')
		b.WriteString(string(tl.Function.Parameters))
		b.WriteByte(' ')
	}
	return b.String()
}

func (r *ChatRequest) EstimatePromptTokens() int64 {
	t := EstimateTextTokens(r.PromptText()) + int64(len(r.Messages))*4 + 3
	for i := range r.Messages {
		for _, tc := range r.Messages[i].ToolCalls {
			t += EstimateTextTokens(tc.Function.Arguments)/2 + 8
		}
	}
	return t
}

func (r *ChatRequest) EstimateOutputTokens() int64 {
	if lim := r.OutputLimit(); lim > 0 {
		if lim > 8192 {
			return 8192
		}
		return lim
	}
	return 1024
}
