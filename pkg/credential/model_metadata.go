package credential

import (
	"fmt"
	"strings"
	"time"

	"github.com/kimnt93/gorouter/pkg/entities"
)

// MetadataSnapshot converts provider discovery data into the bounded, typed
// metadata persisted with a configured model route. Missing capabilities stay
// missing so callers never advertise unsupported input types by assumption.
func MetadataSnapshot(providerID, credentialID string, model ProviderModel, refreshedAt time.Time) *entities.ModelMetadata {
	input := normalizedStrings(model.InputModalities)
	output := normalizedStrings(model.OutputModalities)
	if len(input) == 0 {
		if model.SupportsOriginalImage || capabilityBool(model.Capabilities, "vision", "image", "image_input", "supports_vision") {
			input = []string{"text", "image"}
		} else {
			input = []string{"text"}
		}
	}
	if len(output) == 0 {
		output = []string{"text"}
	}
	levels := normalizedReasoningLevels(model.SupportedReasoningLevels)
	if len(levels) == 0 {
		for _, key := range []string{"effort_tiers", "reasoning_efforts", "supported_reasoning_levels"} {
			levels = reasoningLevelsFromAny(model.Capabilities[key])
			if len(levels) != 0 {
				break
			}
		}
	}
	return &entities.ModelMetadata{
		Provider: strings.TrimSpace(providerID), SourceCredentialID: strings.TrimSpace(credentialID),
		DisplayName: strings.TrimSpace(model.Name), Description: strings.TrimSpace(model.Description),
		ContextWindow: model.ContextLength, MaxContextWindow: model.MaxContextWindow,
		MaxInputTokens: model.MaxInputTokens, MaxOutputTokens: model.MaxOutputTokens,
		InputModalities: input, OutputModalities: output,
		DefaultReasoningLevel: strings.TrimSpace(model.DefaultReasoningLevel), SupportedReasoningLevels: levels,
		SupportsOriginalImage:    model.SupportsOriginalImage,
		SupportsReasoningSummary: model.SupportsReasoningSummary,
		SupportsParallelTools:    model.SupportsParallelTools,
		SupportsVerbosity:        model.SupportsVerbosity, DefaultVerbosity: strings.TrimSpace(model.DefaultVerbosity),
		RefreshedAt: refreshedAt.UTC(),
	}
}

func normalizedStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func normalizedReasoningLevels(values []entities.ModelReasoningLevel) []entities.ModelReasoningLevel {
	seen := map[string]bool{}
	out := make([]entities.ModelReasoningLevel, 0, len(values))
	for _, value := range values {
		effort := strings.ToLower(strings.TrimSpace(value.Effort))
		if effort != "" && !seen[effort] {
			seen[effort] = true
			out = append(out, entities.ModelReasoningLevel{Effort: effort, Description: strings.TrimSpace(value.Description)})
		}
	}
	return out
}

func reasoningLevelsFromAny(value any) []entities.ModelReasoningLevel {
	var values []entities.ModelReasoningLevel
	switch typed := value.(type) {
	case []string:
		for _, effort := range typed {
			values = append(values, entities.ModelReasoningLevel{Effort: effort})
		}
	case []any:
		for _, item := range typed {
			switch item := item.(type) {
			case string:
				values = append(values, entities.ModelReasoningLevel{Effort: item})
			case map[string]any:
				values = append(values, entities.ModelReasoningLevel{Effort: fmt.Sprint(item["effort"]), Description: fmt.Sprint(item["description"])})
			}
		}
	}
	return normalizedReasoningLevels(values)
}

func capabilityBool(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := values[key].(bool); ok && value {
			return true
		}
	}
	return false
}
