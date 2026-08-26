package llm

import "github.com/kimnt93/gorouter/pkg/credential"

func modelsFor(providerID string, ids ...string) []credential.ProviderModel {
	out := make([]credential.ProviderModel, 0, len(ids))
	for _, id := range ids {
		out = append(out, credential.ProviderModel{ID: id, Object: "model", Root: id, OwnedBy: providerID, APIFormat: "chat/completions", SupportedEndpoints: []string{"chat/completions"}, Capabilities: map[string]any{"tool_calling": true, "reasoning": true}})
	}
	return out
}
