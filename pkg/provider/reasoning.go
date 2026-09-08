package provider

import "github.com/kimnt93/gorouter/pkg/entities"

// ReasoningOptions returns catalog display options. Static fallback options are
// suggestions, not a claim of upstream parameter support. Never use this helper
// to inject an effort into a request or override reported provider capabilities.
func ReasoningOptions(metadata *entities.ModelMetadata) (string, []entities.ModelReasoningLevel, string) {
	if metadata != nil && len(metadata.SupportedReasoningLevels) > 0 {
		levels := append([]entities.ModelReasoningLevel(nil), metadata.SupportedReasoningLevels...)
		defaultLevel := metadata.DefaultReasoningLevel
		if defaultLevel == "" {
			defaultLevel = levels[0].Effort
			for _, level := range levels {
				if level.Effort == "medium" {
					defaultLevel = level.Effort
					break
				}
			}
		}
		return defaultLevel, levels, "upstream"
	}
	defaultLevel := "medium"
	if metadata != nil && metadata.DefaultReasoningLevel != "" {
		defaultLevel = metadata.DefaultReasoningLevel
	}
	levels := []entities.ModelReasoningLevel{
		{Effort: "low", Description: "Suggested low effort; upstream support not verified"},
		{Effort: "medium", Description: "Suggested medium effort; upstream support not verified"},
		{Effort: "high", Description: "Suggested high effort; upstream support not verified"},
	}
	found := false
	for _, level := range levels {
		if level.Effort == defaultLevel {
			found = true
		}
	}
	if !found {
		levels = append(levels, entities.ModelReasoningLevel{Effort: defaultLevel, Description: "Provider-reported default effort"})
	}
	return defaultLevel, levels, "static_fallback"
}
