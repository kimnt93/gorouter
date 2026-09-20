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

// DevinCLIAdapter owns isolated, tool-disabled ACP sessions. Every replica needs the
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
	models, err := a.DiscoverModels(ctx, cr)
	if err != nil {
		return devinStatus(err), devinSafeError(err)
	}
	if len(models) == 0 {
		return 502, devinFailure(502, "Devin returned no models")
	}
	return http.StatusOK, nil
}
func (a *DevinCLIAdapter) DiscoverModels(ctx context.Context, cr *entities.CredentialRuntime) ([]credential.ProviderModel, error) {
	if cr == nil {
		return nil, devinFailure(400, "missing Devin credential")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	work, err := a.workspace(ctx, cr.APIKey)
	if err != nil {
		return nil, err
	}
	defer work.Close()
	models, err := work.catalog()
	if err != nil {
		return nil, err
	}
	out := make([]credential.ProviderModel, 0, len(models))
	for _, m := range models {
		out = append(out, m.Metadata)
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
	work, err := a.workspace(ctx, cr.APIKey)
	if err != nil {
		return devinReject(err), nil
	}
	models, err := work.catalog()
	if err != nil {
		work.Close()
		return devinReject(err), nil
	}
	effort := ""
	if req.Reasoning != nil {
		effort = req.Reasoning.Effort
	}
	// Also accept OpenAI's chat-completions spelling without changing other adapters.
	var options struct {
		Effort string `json:"reasoning_effort"`
	}
	if json.Unmarshal(raw, &options) != nil {
		work.Close()
		return devinReject(devinFailure(400, "Invalid reasoning effort")), nil
	}
	if options.Effort != "" {
		if effort != "" && effort != options.Effort {
			work.Close()
			return devinReject(devinFailure(400, "Conflicting reasoning settings")), nil
		}
		effort = options.Effort
	}
	variant, err := selectDevinVariant(models, model, effort)
	if err != nil {
		work.Close()
		return devinReject(err), nil
	}
	s, err := work.open(variant)
	if err != nil {
		return devinReject(err), nil
	}
	// The normal agent exposes actual UID selection; the old summarizer silently
	// ignored it. Require confirmation before any billed prompt (never fallback).
	if err = s.selectValue("model", variant); err != nil {
		s.Close()
		return devinReject(err), nil
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
		return "", devinFailure(400, "Devin text gateway does not support tools")
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
			return "", devinFailure(400, "Devin text gateway accepts text conversation only")
		}
		var text string
		if len(m.Content) > 0 && string(m.Content) != "null" && json.Unmarshal(m.Content, &text) != nil {
			var blocks []devinContent
			if json.Unmarshal(m.Content, &blocks) != nil {
				return "", devinFailure(400, "invalid Devin text content")
			}
			for _, b := range blocks {
				if b.Type != "text" {
					return "", devinFailure(400, "Devin text gateway accepts text only")
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
