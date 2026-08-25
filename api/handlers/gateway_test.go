package handlers

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/kimnt93/gorouter/pkg/chat"
	"github.com/kimnt93/gorouter/platform/llm"
)

func TestRetryableStatus(t *testing.T) {
	for _, status := range []int{408, 429, 500, 502, 503, 504} {
		if !retryableStatus(status) {
			t.Errorf("status %d should retry", status)
		}
	}
	for _, status := range []int{400, 401, 403, 404, 409, 422, 501} {
		if retryableStatus(status) {
			t.Errorf("status %d must not retry", status)
		}
	}
}

func TestReplayStreamHasRequiredHeadersUsageAndDone(t *testing.T) {
	response := llm.Response{
		ID: "chatcmpl-cached", Object: "chat.completion", Model: "model-a",
		Choices: []llm.Choice{{Index: 0, Message: &llm.ResponseMessage{Role: "assistant", Content: "cached reply"}, FinishReason: "stop"}},
		Usage:   llm.Usage{PromptTokens: 7, CompletionTokens: 3},
	}
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	entry := &chat.CacheEntry{Status: 200, ContentType: "application/json", Body: body, Stream: true, PromptTok: 7, Completion: 3}
	app := fiber.New()
	app.Get("/stream", func(c fiber.Ctx) error {
		c.Set("X-Cache", "hit")
		return (&Gateway{}).replayStream(c, entry)
	})
	res, err := app.Test(httptest.NewRequest("GET", "/stream", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Type") != "text/event-stream" || res.Header.Get("Cache-Control") != "no-cache" || res.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("missing SSE headers: %v", res.Header)
	}
	text := string(got)
	if !strings.Contains(text, `"prompt_tokens":7`) || !strings.Contains(text, "data: [DONE]\n\n") {
		t.Fatalf("invalid replay stream: %s", text)
	}
}
