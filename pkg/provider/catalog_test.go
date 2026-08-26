package provider

import "testing"

func TestCatalogHasStableUniqueDefinitions(t *testing.T) {
	seen := map[string]bool{}
	for _, definition := range Catalog() {
		if definition.ID == "" || definition.Name == "" || definition.Auth == "" || definition.Protocol == "" {
			t.Fatalf("incomplete definition: %+v", definition)
		}
		if seen[definition.ID] {
			t.Fatalf("duplicate provider %q", definition.ID)
		}
		seen[definition.ID] = true
	}
	for _, required := range []string{"claude", "codex", "openai", "anthropic", "gemini", "groq", "openrouter", "opencode-zen"} {
		if !seen[required] {
			t.Fatalf("missing provider %q", required)
		}
	}
}

func TestPublicModelIDUsesAliasWithoutReasoningSuffixes(t *testing.T) {
	if got := PublicModelID("codex", "gpt-5.3-codex"); got != "cx/gpt-5.3-codex" {
		t.Fatalf("Codex model = %q", got)
	}
	if got := PublicModelID("codex", "cx/gpt-5.3-codex"); got != "cx/gpt-5.3-codex" {
		t.Fatalf("already-prefixed Codex model = %q", got)
	}
	if got := PublicModelID("opencode-zen", "gpt-5-nano"); got != "ocz/gpt-5-nano" {
		t.Fatalf("OpenCode Zen model = %q", got)
	}
}

func TestKimiUsesAnthropicWireTranslation(t *testing.T) {
	if !UsesAnthropicWire("kimi-code") || !UsesAnthropicWire("claude") || UsesAnthropicWire("codex") {
		t.Fatal("Anthropic wire protocol classification is incorrect")
	}
}
