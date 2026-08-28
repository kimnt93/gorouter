package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageNormalizesOpenAICacheDetails(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":1000,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":900}}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 100 || usage.CacheReadTokens != 900 || usage.CompletionTokens != 20 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestUsageNormalizesResponsesCacheDetails(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{"input_tokens":1200,"output_tokens":12,"input_tokens_details":{"cached_tokens":1000,"cache_creation_tokens":100}}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 100 || usage.CacheReadTokens != 1000 || usage.CacheWriteTokens != 100 || usage.CompletionTokens != 12 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestUsageNormalizesDeepSeekCacheFields(t *testing.T) {
	var usage Usage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":4214,"completion_tokens":12,"prompt_cache_hit_tokens":4096,"prompt_cache_miss_tokens":118}`), &usage); err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 118 || usage.CacheReadTokens != 4096 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestStablePromptCacheKeyUsesOnlyReusablePrefix(t *testing.T) {
	base := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable instructions"`)}, {Role: "user", Content: json.RawMessage(`"first question"`)}}}
	changed := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable instructions"`)}, {Role: "user", Content: json.RawMessage(`"different question"`)}}}
	if key := StablePromptCacheKey(base); key == "" || key != StablePromptCacheKey(changed) || !strings.HasPrefix(key, "gorouter-") {
		t.Fatalf("unstable cache keys: %q %q", key, StablePromptCacheKey(changed))
	}
}

func TestExplicitRouteAffinityUsesOnlyCallerSessionIdentity(t *testing.T) {
	request := &ChatRequest{PromptCacheKey: " cache-key ", SessionID: "session-id", ConversationID: "conversation-id"}
	if got := request.ExplicitRouteAffinity(); got != "cache-key" {
		t.Fatalf("affinity=%q", got)
	}
	request.PromptCacheKey = ""
	if got := request.ExplicitRouteAffinity(); got != "session-id" {
		t.Fatalf("session affinity=%q", got)
	}
	request.SessionID = strings.Repeat("x", 513)
	if got := request.ExplicitRouteAffinity(); got != "conversation-id" {
		t.Fatalf("bounded affinity=%q", got)
	}
}

func TestProviderPromptCacheKeyPrefersConversationIdentity(t *testing.T) {
	request := &ChatRequest{
		ConversationID: "conversation-123",
		Messages: []Message{
			{Role: "developer", Content: json.RawMessage(`"stable instructions"`)},
			{Role: "user", Content: json.RawMessage(`"question"`)},
		},
	}
	if got := ProviderPromptCacheKey(request); got != "conversation-123" {
		t.Fatalf("provider cache key=%q", got)
	}
	request.ConversationID = ""
	if got := ProviderPromptCacheKey(request); !strings.HasPrefix(got, "gorouter-") {
		t.Fatalf("fallback provider cache key=%q", got)
	}
}

func TestStableConversationIDFollowsExplicitSessionAndPrefix(t *testing.T) {
	base := &ChatRequest{SessionID: "session-a", Messages: []Message{{Role: "user", Content: json.RawMessage(`"one"`)}}}
	changedTurn := &ChatRequest{SessionID: "session-a", Messages: []Message{{Role: "user", Content: json.RawMessage(`"two"`)}}}
	otherSession := &ChatRequest{SessionID: "session-b", Messages: changedTurn.Messages}
	first := StableConversationID(base)
	if first == "" || first != StableConversationID(changedTurn) || first == StableConversationID(otherSession) {
		t.Fatalf("conversation IDs first=%q changed=%q other=%q", first, StableConversationID(changedTurn), StableConversationID(otherSession))
	}
	if len(first) != 36 || first[14] != '5' {
		t.Fatalf("conversation ID is not UUIDv5-shaped: %q", first)
	}

	prefixOne := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable"`)}, {Role: "user", Content: json.RawMessage(`"one"`)}}}
	prefixTwo := &ChatRequest{Messages: []Message{{Role: "developer", Content: json.RawMessage(`"stable"`)}, {Role: "user", Content: json.RawMessage(`"two"`)}}}
	if got := StableConversationID(prefixOne); got == "" || got != StableConversationID(prefixTwo) {
		t.Fatalf("prefix conversation IDs %q %q", got, StableConversationID(prefixTwo))
	}
}

func TestOpenAIPromptCacheKeyCapabilityIsProviderSpecific(t *testing.T) {
	for _, providerID := range []string{"openai", "codex", "opencode-zen", "opencode-go", "grok-build"} {
		if !SupportsOpenAIPromptCacheKey(providerID) {
			t.Errorf("%s should support prompt_cache_key", providerID)
		}
	}
	for _, providerID := range []string{"groq", "gemini", "openrouter", "xai", "openai-compatible"} {
		if SupportsOpenAIPromptCacheKey(providerID) {
			t.Errorf("%s must remain conservative", providerID)
		}
	}
}
