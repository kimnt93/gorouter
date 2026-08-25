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
)

var (
	port     = flag.Int("port", 9099, "listen port")
	failN    = flag.Int("fail-first", 0, "fail the first N requests with HTTP 500 (then succeed)")
	stream   = flag.Bool("stream", true, "support stream=true requests")
	latency  = flag.Int("latency-ms", 30, "simulated latency")
	requests atomic.Int64
)

func main() {
	flag.Parse()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("mock upstream on http://%s (openai /v1/chat/completions + /v1/models, anthropic /v1/messages)", addr)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"object": "list", "data": []any{
			map[string]any{"id": "mock-gpt", "object": "model"},
			map[string]any{"id": "mock-mini", "object": "model"},
		}})
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
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"bad json"}}`, 400)
		return
	}
	time.Sleep(time.Duration(*latency) * time.Millisecond)
	if maybeFail() {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"message":"mock upstream failure"}}`))
		return
	}
	var promptText strings.Builder
	for _, m := range req.Messages {
		promptText.Write(m.Content)
	}
	inTok := int64(len(promptText.String())/4 + 3)
	outTok := int64(5 + rand.Intn(10))

	content := fmt.Sprintf("mock reply from %s (req #%d)", req.Model, requests.Load())
	usage := map[string]any{
		"prompt_tokens": inTok, "completion_tokens": outTok, "total_tokens": inTok + outTok,
	}
	if !req.Stream || !*stream {
		writeJSON(w, map[string]any{
			"id": fmt.Sprintf("chatcmpl-mock%d", time.Now().UnixNano()), "object": "chat.completion",
			"created": time.Now().Unix(), "model": req.Model,
			"choices": []any{map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": usage,
		})
		return
	}
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	id := fmt.Sprintf("chatcmpl-mock%d", time.Now().UnixNano())
	send(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": req.Model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}})
	for _, word := range strings.Fields(content) {
		send(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": req.Model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": word + " "}}}})
	}
	send(map[string]any{"id": id, "object": "chat.completion.chunk", "created": time.Now().Unix(), "model": req.Model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		"usage":   usage})
	fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}

func handleAnthropic(w http.ResponseWriter, r *http.Request) {
	authz := r.Header.Get("Authorization")
	xkey := r.Header.Get("x-api-key")
	if authz == "" && xkey == "" {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"no auth"}}`))
		return
	}
	var req struct {
		Model     string `json:"model"`
		MaxTokens int64  `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		System    any    `json:"system"`
		Messages  []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad json"}}`))
		return
	}
	time.Sleep(time.Duration(*latency) * time.Millisecond)
	if maybeFail() {
		w.WriteHeader(529)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"mock overloaded"}}`))
		return
	}
	content := fmt.Sprintf("anthropic mock reply for %s", req.Model)
	base := map[string]any{
		"id": fmt.Sprintf("msg_mock%d", time.Now().UnixNano()), "type": "message", "role": "assistant",
		"model":       req.Model,
		"content":     []any{map[string]any{"type": "text", "text": content}},
		"stop_reason": "end_turn", "stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens": 21, "output_tokens": 11,
			"cache_read_input_tokens": 4, "cache_creation_input_tokens": 7,
		},
	}
	if !req.Stream {
		writeJSON(w, base)
		return
	}
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	send := func(event string, v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		fl.Flush()
	}
	msgID := base["id"].(string)
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": msgID, "type": "message", "role": "assistant", "model": req.Model, "content": []any{},
		"usage": map[string]any{"input_tokens": 21, "output_tokens": 1},
	}})
	send("content_block_start", map[string]any{"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "text", "text": ""}})
	send("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
		"delta": map[string]any{"type": "text_delta", "text": content}})
	send("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	send("message_delta", map[string]any{"type": "message_delta",
		"delta": map[string]any{"stop_reason": "end_turn"},
		"usage": map[string]any{"output_tokens": 11}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

func handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") == "" {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_request"}`))
		return
	}
	writeJSON(w, map[string]any{
		"access_token":  "sk-ant-oat01-refreshed-" + fmt.Sprint(time.Now().Unix()),
		"refresh_token": r.FormValue("refresh_token"),
		"expires_in":    3600,
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
