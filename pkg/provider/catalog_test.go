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
	for _, required := range []string{"claude", "codex", "openai", "anthropic", "gemini", "groq", "openrouter", "opencode-zen", "antigravity", "devin-cli"} {
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

func TestOrganizationModelIDUsesStableSlugAndProviderPrefix(t *testing.T) {
	if got := OrganizationModelID("Microsoft", "codex", "gpt-5.6-luna"); got != "microsoft/cx/gpt-5.6-luna" {
		t.Fatalf("organization model = %q", got)
	}
	if got := OrganizationModelID("VN Fin", "opencode-zen", "deepseek-v4-flash"); got != "vn-fin/ocz/deepseek-v4-flash" {
		t.Fatalf("organization model = %q", got)
	}
}

func TestKimiUsesAnthropicWireTranslation(t *testing.T) {
	if !UsesAnthropicWire("kimi-code") || !UsesAnthropicWire("claude") || UsesAnthropicWire("codex") {
		t.Fatal("Anthropic wire protocol classification is incorrect")
	}
}

func TestDevinCatalogRetiresCloudAndDesktopWithoutLosingHistory(t *testing.T) {
	for _, d := range Catalog() {
		if IsRetired(d.ID) {
			t.Fatal("retired provider is connectable")
		}
	}
	for _, id := range []string{"devin", "devin-desktop"} {
		if !IsRetired(id) {
			t.Fatal("missing retirement")
		}
		if _, ok := Lookup(id); !ok {
			t.Fatal("lost legacy namespace")
		}
	}
	if ProtocolFor("devin-cli") == ProtocolOpenAI {
		t.Fatal("Devin must never fall back to generic HTTP")
	}
}
