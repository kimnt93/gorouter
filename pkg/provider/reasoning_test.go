package provider

import (
	"testing"

	"github.com/kimnt93/gorouter/pkg/entities"
)

func TestReasoningFallbackAndUpstreamPrecedence(t *testing.T) {
	for _, provider := range Catalog() {
		def, levels, source := ReasoningOptions(&entities.ModelMetadata{Provider: provider.ID})
		if def != "medium" || len(levels) != 3 || source != "static_fallback" {
			t.Fatalf("fallback for %s", provider.ID)
		}
	}
	metadata := &entities.ModelMetadata{DefaultReasoningLevel: "custom", SupportedReasoningLevels: []entities.ModelReasoningLevel{{Effort: "custom"}}}
	def, levels, source := ReasoningOptions(metadata)
	if def != "custom" || len(levels) != 1 || source != "upstream" {
		t.Fatal("overrode upstream")
	}
	levels[0].Effort = "changed"
	if metadata.SupportedReasoningLevels[0].Effort != "custom" {
		t.Fatal("mutated snapshot")
	}
}

func TestReasoningOptionsSelectsSupportedDefaultWhenProviderOmitsIt(t *testing.T) {
	metadata := &entities.ModelMetadata{SupportedReasoningLevels: []entities.ModelReasoningLevel{{Effort: "high"}, {Effort: "medium"}, {Effort: "low"}}}
	def, levels, source := ReasoningOptions(metadata)
	if def != "medium" || len(levels) != 3 || source != "upstream" {
		t.Fatalf("default=%q levels=%+v source=%q", def, levels, source)
	}

	metadata.SupportedReasoningLevels = []entities.ModelReasoningLevel{{Effort: "custom"}, {Effort: "low"}}
	def, _, _ = ReasoningOptions(metadata)
	if def != "custom" {
		t.Fatalf("default=%q, want first supported effort", def)
	}
}
