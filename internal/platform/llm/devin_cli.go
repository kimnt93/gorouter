package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// DevinCLIAdapter owns isolated no-tool ACP sessions. Every replica needs the
// CLI installed. Its semaphore bounds local OS processes, not authorization or
// quota; credentials/catalog caching remain scoped by the existing services.
type DevinCLIAdapter struct {
	Binary       string
	WorkDir      string
	MaxProcesses int
	once         sync.Once
	slots        chan struct{}
}

var devinModelID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (a *DevinCLIAdapter) Probe(ctx context.Context, cr *entities.CredentialRuntime) (int, error) {
	if cr == nil {
		return 0, errors.New("missing Devin credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	s, err := a.open(ctx, cr.APIKey)
	if err != nil {
		return devinStatus(err), devinSafeError(err)
	}
	defer s.Close()
	// session/new fetches authenticated team settings; never send a paid prompt.
	return http.StatusOK, nil
}
func (a *DevinCLIAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	if cr == nil {
		return nil, errors.New("missing Devin credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	s, err := a.open(ctx, cr.APIKey)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	values := devinValues(s.selector("model"))
	if len(values) == 0 && strings.HasPrefix(strings.TrimSpace(cr.APIKey), "cog_") {
		// Current Cognition PATs authenticate the CLI, but ACP summarizer sessions
		// may omit the model selector because account policy manages selection.
		// Expose only Adaptive rather than inventing a foundation-model catalog.
		return []credential.ProviderModel{{ID: "adaptive", Name: "Adaptive", Description: "Cognition-managed adaptive model routing", Root: "adaptive", Object: "model", OwnedBy: "devin-cli", APIFormat: "chat/completions", SupportedEndpoints: []string{"chat/completions"}, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}, nil
	}
	if len(values) == 0 || len(values) > 256 {
		return nil, devinFailure(502, "Devin CLI returned no supported model selector or too many models")
	}
	out := make([]credential.ProviderModel, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		if !devinModelID.MatchString(v.Value) || seen[v.Value] {
			continue
		}
		seen[v.Value] = true
		// Thought levels are model-specific. Never copy one model's options to all.
		if err = s.selectValue("model", v.Value); err != nil {
			return nil, err
		}
		model := credential.ProviderModel{ID: v.Value, Name: v.Name, Description: v.Description, Root: v.Value, Object: "model", OwnedBy: "devin-cli", APIFormat: "chat/completions", SupportedEndpoints: []string{"chat/completions"}, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}
		if thought := s.selector("thought_level"); thought != nil {
			model.DefaultReasoningLevel = thought.CurrentValue
			for _, level := range devinValues(thought) {
				if level.Value != "" {
					model.SupportedReasoningLevels = append(model.SupportedReasoningLevels, entities.ModelReasoningLevel{Effort: level.Value, Description: level.Description})
				}
			}
		}
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil, devinFailure(502, "Devin CLI returned no usable models")
	}
	return out, nil
}

type devinErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func devinReject(err error) *entities.UpstreamResult {
	// Return statuses rather than transport errors so a rejected credential or
	// unsupported model does not consume the transient retry budget.
	payload := devinErrorBody{}
	payload.Error.Message = devinSafeError(err).Error()
	payload.Error.Type = "upstream_error"
	body, _ := json.Marshal(payload)
	return &entities.UpstreamResult{StatusCode: devinStatus(err), Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}
}

func (a *DevinCLIAdapter) Send(ctx context.Context, cr *entities.CredentialRuntime, model string, raw []byte) (*entities.UpstreamResult, error) {
	if cr == nil {
		return nil, errors.New("missing Devin credential")
	}
	var req ChatRequest
	if json.Unmarshal(raw, &req) != nil {
		return devinReject(devinFailure(400, "invalid Devin chat request")), nil
	}
	prompt, err := devinPrompt(req)
	if err != nil {
		return devinReject(err), nil
	}
	if !devinModelID.MatchString(model) {
		return devinReject(devinFailure(400, "invalid Devin model")), nil
	}
	s, err := a.open(ctx, cr.APIKey)
	if err != nil {
		return devinReject(err), nil
	}
	if s.selector("model") == nil && strings.HasPrefix(strings.TrimSpace(cr.APIKey), "cog_") && model == "adaptive" {
		// The current PAT-authenticated summarizer may be policy-managed and omit
		// selectors. Its default is the documented Adaptive router.
	} else if err = s.selectValue("model", model); err != nil {
		s.Close()
		return devinReject(err), nil
	}
	if req.Reasoning != nil && req.Reasoning.Effort != "" {
		if err = s.selectValue("thought_level", req.Reasoning.Effort); err != nil {
			s.Close()
			return devinReject(err), nil
		}
	}
	id := entities.NewID("chatcmpl")
	created := time.Now().Unix()
	if req.Stream {
		reader, writer := io.Pipe()
		// Cancel producer even when the consumer stops while a pipe write is blocked.
		stop := context.AfterFunc(s.ctx, func() { _ = writer.CloseWithError(devinFailure(504, "Devin stream canceled or timed out")) })
		go func() {
			defer s.Close()
			defer stop()
			emit := func(d Delta, finish *string, usage *Usage) error {
				b, e := json.Marshal(devinChunk{ID: id, Object: "chat.completion.chunk", Created: created, Model: model, Choices: []devinChunkChoice{{Delta: d, FinishReason: finish}}, Usage: usage})
				if e != nil {
					return e
				}
				_, e = writer.Write(append(append([]byte("data: "), b...), '\n', '\n'))
				return e
			}
			result, e := s.prompt(prompt, func(d Delta) error { return emit(d, nil, nil) })
			if e == nil {
				e = emit(Delta{}, &result.finish, &result.usage)
			}
			if e == nil {
				_, e = io.WriteString(writer, "data: [DONE]\n\n")
			}
			if e != nil {
				e = devinSafeError(e)
			}
			_ = writer.CloseWithError(e)
		}()
		return &entities.UpstreamResult{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: &devinStreamBody{PipeReader: reader, cancel: s.cancel}}, nil
	}
	defer s.Close()
	result, err := s.prompt(prompt, nil)
	if err != nil {
		return devinReject(err), nil
	}
	body, _ := json.Marshal(Response{ID: id, Object: "chat.completion", Created: created, Model: model, Choices: []Choice{{Message: &result.message, FinishReason: result.finish}}, Usage: result.usage})
	return &entities.UpstreamResult{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

type devinStreamBody struct {
	*io.PipeReader
	cancel context.CancelFunc
}

func (b *devinStreamBody) Close() error { b.cancel(); return b.PipeReader.Close() }

type devinChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []devinChunkChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}
type devinChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}
type devinTurn struct {
	message ResponseMessage
	usage   Usage
	finish  string
}

type devinTurnUsage struct {
	Input      int64 `json:"inputTokens"`
	Output     int64 `json:"outputTokens"`
	Thought    int64 `json:"thoughtTokens"`
	CacheRead  int64 `json:"cachedReadTokens"`
	CacheWrite int64 `json:"cachedWriteTokens"`
}

func (s *devinSession) prompt(text string, emit func(Delta) error) (devinTurn, error) {
	result := devinTurn{message: ResponseMessage{Role: "assistant"}}
	var content, thought strings.Builder
	var done struct {
		StopReason string          `json:"stopReason"`
		Usage      *devinTurnUsage `json:"usage"`
	}
	err := s.call("session/prompt", devinPromptParams{SessionID: s.sessionID, Prompt: []devinContent{{Type: "text", Text: text}}}, &done, func(u devinUpdate) error {
		switch u.Update.Kind {
		case "agent_message_chunk", "agent_thought_chunk":
			if u.Update.Content.Type != "text" {
				return devinFailure(502, "unsupported Devin content type")
			}
			t := u.Update.Content.Text
			if content.Len()+thought.Len()+len(t) > 4<<20 {
				return devinFailure(502, "Devin output exceeds limit")
			}
			delta := Delta{Role: "assistant"}
			if u.Update.Kind == "agent_message_chunk" {
				content.WriteString(t)
				delta.Content = t
			} else {
				thought.WriteString(t)
				delta.ReasoningContent = t
			}
			if emit != nil {
				return emit(delta)
			}
		case "tool_call", "tool_call_update":
			return devinFailure(502, "unexpected tool call in Devin no-tool session")
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	switch done.StopReason {
	case "end_turn":
		result.finish = "stop"
	case "max_tokens", "max_turn_requests":
		result.finish = "length"
	case "refusal":
		result.finish = "content_filter"
	case "cancelled":
		return result, devinFailure(502, "Devin turn canceled")
	default:
		return result, devinFailure(502, "Devin omitted a valid stop reason")
	}
	result.message.Content = content.String()
	result.message.ReasoningContent = thought.String()
	// Each subprocess has one prompt turn, so optional ACP session token totals
	// equal this turn. Context-window 'used' notifications are NOT token usage.
	// Keep cache components separate; do not bill thought tokens a second time.
	result.usage = Usage{PromptTokens: EstimateTextTokens(text), CompletionTokens: EstimateTextTokens(content.String() + thought.String())}
	if done.Usage != nil {
		u := done.Usage
		if u.Input < 0 || u.Output < 0 || u.CacheRead < 0 || u.CacheWrite < 0 || u.Thought < 0 {
			return result, devinFailure(502, "invalid Devin token usage")
		}
		result.usage = Usage{PromptTokens: u.Input, CompletionTokens: u.Output, CacheReadTokens: u.CacheRead, CacheWriteTokens: u.CacheWrite}
	}
	return result, nil
}
func devinPrompt(req ChatRequest) (string, error) {
	if len(req.Tools) > 0 || len(req.ToolChoice) > 0 && string(req.ToolChoice) != "null" && string(req.ToolChoice) != "\"none\"" {
		return "", devinFailure(400, "Devin summarizer does not support tools")
	}
	if req.N != nil && *req.N != 1 {
		return "", devinFailure(400, "Devin supports one choice")
	}
	if req.Reasoning != nil && req.Reasoning.Summary != "" {
		return "", devinFailure(400, "Devin does not support reasoning summary selection")
	}
	var lines []string
	for _, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" && m.Role != "system" && m.Role != "developer" || len(m.ToolCalls) > 0 || m.ToolCallID != "" {
			return "", devinFailure(400, "Devin summarizer accepts text conversation only")
		}
		var text string
		if len(m.Content) > 0 && string(m.Content) != "null" && json.Unmarshal(m.Content, &text) != nil {
			var blocks []devinContent
			if json.Unmarshal(m.Content, &blocks) != nil {
				return "", devinFailure(400, "invalid Devin text content")
			}
			for _, b := range blocks {
				if b.Type != "text" {
					return "", devinFailure(400, "Devin summarizer accepts text only")
				}
				text += b.Text
			}
		}
		lines = append(lines, "["+m.Role+"]\n"+text)
	}
	if len(lines) == 0 {
		return "", devinFailure(400, "messages are required")
	}
	text := strings.Join(lines, "\n\n")
	if len(text) > devinMaxFrame/2 {
		return "", devinFailure(400, "Devin prompt exceeds limit")
	}
	return text, nil
}
