package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kimnt93/gorouter/platform/llm"
)

var (
	port     = flag.Int("port", 9099, "listen port")
	failN    = flag.Int("fail-first", 0, "fail the first N requests with HTTP 500 (then succeed)")
	stream   = flag.Bool("stream", true, "support stream=true requests")
	latency  = flag.Int("latency-ms", 30, "simulated latency")
	requests atomic.Int64
)

type mockModelList struct {
	Object string          `json:"object"`
	Data   []llm.ModelInfo `json:"data"`
}

type mockErrorEnvelope struct {
	Error mockError `json:"error"`
}

type mockError struct {
	Type    string `json:"type,omitempty"`
	Message string `json:"message"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Model        string             `json:"model"`
	Content      []anthropicContent `json:"content"`
	StopReason   string             `json:"stop_reason"`
	StopSequence *string            `json:"stop_sequence"`
	Usage        anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens,omitempty"`
}

type anthropicEvent struct {
	Type         string             `json:"type"`
	Index        int                `json:"index,omitempty"`
	Message      *anthropicResponse `json:"message,omitempty"`
	ContentBlock *anthropicContent  `json:"content_block,omitempty"`
	Delta        *anthropicDelta    `json:"delta,omitempty"`
	Usage        *anthropicUsage    `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type       string `json:"type,omitempty"`
	Text       string `json:"text,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
}

type oauthTokenRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
}

type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

func main() {
	flag.Parse()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("mock upstream on http://%s (openai /v1/chat/completions + /v1/models, anthropic /v1/messages)", addr)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, mockModelList{Object: "list", Data: []llm.ModelInfo{{ID: "mock-gpt", Object: "model"}, {ID: "mock-mini", Object: "model"}}})
	})
	mux.HandleFunc("POST /v1/chat/completions", handleOpenAI)
	mux.HandleFunc("POST /v1/messages", handleAnthropic)
	mux.HandleFunc("POST /oauth/token", handleOAuthToken)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func maybeFail() bool {
	n := requests.Add(1)
	return *failN > 0 && n <= int64(*failN)
}

func handleOpenAI(w http.ResponseWriter, r *http.Request) {
	var req llm.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatusJSON(w, http.StatusBadRequest, mockErrorEnvelope{Error: mockError{Message: "bad json"}})
		return
	}
	time.Sleep(time.Duration(*latency) * time.Millisecond)
	if maybeFail() {
		writeStatusJSON(w, http.StatusInternalServerError, mockErrorEnvelope{Error: mockError{Message: "mock upstream failure"}})
		return
	}
	var promptText strings.Builder
	for _, message := range req.Messages {
		promptText.Write(message.Content)
	}
	usage := llm.Usage{PromptTokens: int64(len(promptText.String())/4 + 3), CompletionTokens: int64(5 + rand.Intn(10))}
	content := fmt.Sprintf("mock reply from %s (req #%d)", req.Model, requests.Load())
	id := fmt.Sprintf("chatcmpl-mock%d", time.Now().UnixNano())
	if !req.Stream || !*stream {
		writeJSON(w, llm.Response{ID: id, Object: "chat.completion", Created: time.Now().Unix(), Model: req.Model,
			Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: content}, FinishReason: "stop"}}, Usage: usage})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	send := func(chunk llm.Chunk) {
		body, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		flusher.Flush()
	}
	base := llm.Chunk{ID: id, Object: "chat.completion.chunk", Created: time.Now().Unix(), Model: req.Model}
	base.Choices = []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Role: "assistant"}}}
	send(base)
	for _, word := range strings.Fields(content) {
		chunk := base
		chunk.Choices = []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{Content: word + " "}}}
		send(chunk)
	}
	final := base
	final.Choices = []llm.ChunkChoice{{Index: 0, Delta: llm.Delta{}, FinishReason: "stop"}}
	final.Usage = &usage
	send(final)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func handleAnthropic(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" && r.Header.Get("x-api-key") == "" {
		writeStatusJSON(w, http.StatusUnauthorized, mockErrorEnvelope{Error: mockError{Type: "authentication_error", Message: "no auth"}})
		return
	}
	var req llm.AnthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatusJSON(w, http.StatusBadRequest, mockErrorEnvelope{Error: mockError{Type: "invalid_request_error", Message: "bad json"}})
		return
	}
	time.Sleep(time.Duration(*latency) * time.Millisecond)
	if maybeFail() {
		writeStatusJSON(w, 529, mockErrorEnvelope{Error: mockError{Type: "overloaded_error", Message: "mock overloaded"}})
		return
	}
	content := fmt.Sprintf("anthropic mock reply for %s", req.Model)
	response := anthropicResponse{ID: fmt.Sprintf("msg_mock%d", time.Now().UnixNano()), Type: "message", Role: "assistant", Model: req.Model,
		Content: []anthropicContent{{Type: "text", Text: content}}, StopReason: "end_turn",
		Usage: anthropicUsage{InputTokens: 21, OutputTokens: 11, CacheReadInputTokens: 4, CacheCreationInputTokens: 7}}
	if !req.Stream {
		writeJSON(w, response)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	send := func(event string, value anthropicEvent) {
		body, _ := json.Marshal(value)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body)
		flusher.Flush()
	}
	startMessage := response
	startMessage.Content = []anthropicContent{}
	startMessage.Usage = anthropicUsage{InputTokens: 21, OutputTokens: 1}
	send("message_start", anthropicEvent{Type: "message_start", Message: &startMessage})
	send("content_block_start", anthropicEvent{Type: "content_block_start", ContentBlock: &anthropicContent{Type: "text"}})
	send("content_block_delta", anthropicEvent{Type: "content_block_delta", Delta: &anthropicDelta{Type: "text_delta", Text: content}})
	send("content_block_stop", anthropicEvent{Type: "content_block_stop"})
	send("message_delta", anthropicEvent{Type: "message_delta", Delta: &anthropicDelta{StopReason: "end_turn"}, Usage: &anthropicUsage{OutputTokens: 11}})
	send("message_stop", anthropicEvent{Type: "message_stop"})
}

func handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	var req oauthTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.GrantType != "refresh_token" || req.RefreshToken == "" {
		writeStatusJSON(w, http.StatusBadRequest, mockErrorEnvelope{Error: mockError{Type: "invalid_request", Message: "refresh token required"}})
		return
	}
	writeJSON(w, oauthTokenResponse{AccessToken: "sk-ant-oat01-refreshed-" + fmt.Sprint(time.Now().Unix()), RefreshToken: req.RefreshToken, ExpiresIn: 3600})
}

func writeStatusJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeJSON(w http.ResponseWriter, value any) {
	writeStatusJSON(w, http.StatusOK, value)
}
