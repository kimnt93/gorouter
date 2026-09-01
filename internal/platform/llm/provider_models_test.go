package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestSubscriptionCatalogsNeverCreateReasoningEffortModelAliases(t *testing.T) {
	discoverers := map[string]credential.ModelDiscoverer{
		"github-copilot": &CopilotAdapter{}, "cursor": &CursorAdapter{}, "grok-build": &GrokBuildAdapter{},
		"kimi-code": &KimiCodeAdapter{}, "kiro": &KiroAdapter{}, "amazon-q": &AmazonQAdapter{}, "antigravity": &AntigravityAdapter{},
	}
	efforts := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	for providerID, discoverer := range discoverers {
		if providerID == "antigravity" {
			// Antigravity discovery is intentionally account-authenticated and is
			// covered with a mock live catalog in antigravity_test.go.
			continue
		}
		models, err := discoverer.DiscoverModels(context.Background(), &entities.CredentialRuntime{Provider: providerID})
		if err != nil {
			t.Fatalf("%s: %v", providerID, err)
		}
		if len(models) == 0 {
			t.Fatalf("%s returned no models", providerID)
		}
		for _, model := range models {
			for _, effort := range efforts {
				if strings.HasSuffix(strings.ToLower(model.ID), "-"+effort) {
					t.Errorf("%s created effort alias %q", providerID, model.ID)
				}
			}
		}
	}
}

func TestAntigravityRequestKeepsReasoningInBodyNotModelName(t *testing.T) {
	effort := "medium"
	payload, err := antigravityRequest(ChatRequest{Messages: []Message{{Role: "user", Content: []byte(`"hello"`)}}, Reasoning: &Reasoning{Effort: effort}}, "gpt-5.6-sol", "project")
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "gpt-5.6-sol-medium") {
		t.Fatalf("reasoning leaked into model id: %s", text)
	}
	if !strings.Contains(text, `"thinkingLevel":"medium"`) {
		t.Fatalf("request-scoped effort missing: %s", text)
	}
}
