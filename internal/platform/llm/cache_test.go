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
