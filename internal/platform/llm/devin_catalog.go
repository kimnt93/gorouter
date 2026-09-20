package llm

import (
	"regexp"
	"sort"
	"strings"

	"github.com/kimnt93/gorouter/pkg/credential"
	"github.com/kimnt93/gorouter/pkg/entities"
)

// Official `devin models list --format json` wire. Family slugs and labels are
// upstream-owned; no model-family allowlist or synthesized UID is used.
type devinCatalog struct {
	Families []devinFamily `json:"families"`
}
type devinFamily struct {
	Slug     string         `json:"slug"`
	Label    string         `json:"family_label"`
	UID      string         `json:"family_uid"`
	Variants []devinVariant `json:"variants"`
}
type devinVariant struct {
	UID     string `json:"model_uid"`
	Label   string `json:"label"`
	Context int64  `json:"max_context_tokens"`
	Output  int64  `json:"max_output_tokens"`
}
type devinChoice struct {
	Effort  string       `json:"effort"`
	Variant devinVariant `json:"variant"`
}
type devinModel struct {
	Metadata credential.ProviderModel `json:"metadata"`
	Choices  []devinChoice            `json:"choices"`
}

var devinSpeed = regexp.MustCompile(`(?i)(^|[\s_-])(fast|priority)([\s_-]|$)`)
var devinEffort = regexp.MustCompile(`(?i)^(none|no thinking|off|minimal|low|medium|high|x[- ]?high|max|thinking)(?: thinking)?(?: \d+[km])?$`)

func normalizeDevinCatalog(catalog devinCatalog) ([]devinModel, error) {
	if len(catalog.Families) == 0 || len(catalog.Families) > 512 {
		return nil, devinFailure(502, "Devin returned an empty or oversized family catalog")
	}
	out := []devinModel{}
	seen := map[string]bool{}
	for _, family := range catalog.Families {
		if strings.TrimSpace(family.Label) == "" || !devinModelID.MatchString(family.Slug) || seen[family.Slug] || len(family.Variants) > 2048 || devinSpeed.MatchString(family.Slug) {
			continue
		}
		hasEffort := false
		for _, v := range family.Variants {
			if devinEffort.MatchString(strings.TrimSpace(strings.TrimPrefix(v.Label, family.Label))) {
				hasEffort = true
			}
		}
		model := devinModel{Metadata: credential.ProviderModel{ID: family.Slug, Name: family.Label, Root: family.Slug, Object: "model", OwnedBy: "devin-cli", APIFormat: "chat/completions", SupportedEndpoints: []string{"chat/completions"}, InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
		for _, v := range family.Variants {
			// Fusion is multi-model routing, not a reasoning setting on a single model.
			// Do not accidentally turn a sidekick's effort into the lead model's effort.
			if v.Context < 0 || v.Output < 0 || !strings.HasPrefix(v.Label, family.Label) || !devinModelID.MatchString(v.UID) || strings.Contains(v.UID, "-sidekick-") || devinSpeed.MatchString(v.UID) || devinSpeed.MatchString(v.Label) {
				continue
			}
			label := strings.TrimSpace(strings.TrimPrefix(v.Label, family.Label))
			effort := ""
			if match := devinEffort.FindStringSubmatch(label); len(match) > 1 {
				effort = strings.ToLower(match[1])
				effort = strings.ReplaceAll(strings.ReplaceAll(effort, "-", ""), " ", "")
				if effort == "nothinking" {
					effort = "none"
				}
			} else if label != "" && !regexpContextOnly.MatchString(label) {
				continue
			} // don't silently mislabel an unknown mode
			if effort == "" && hasEffort {
				effort = "none"
			}
			candidate := devinChoice{Effort: effort, Variant: v}
			found := -1
			for i, prior := range model.Choices {
				if prior.Effort == effort {
					found = i
					break
				}
			}
			if found < 0 {
				model.Choices = append(model.Choices, candidate)
			} else if v.Context > 0 && (model.Choices[found].Variant.Context == 0 || v.Context < model.Choices[found].Variant.Context) || v.Context == model.Choices[found].Variant.Context && v.UID < model.Choices[found].Variant.UID {
				model.Choices[found] = candidate
			}
		}
		if len(model.Choices) == 0 {
			continue
		}
		// Deterministic default, independent of provider promotional sorting.
		sort.SliceStable(model.Choices, func(i, j int) bool {
			return effortOrder(model.Choices[i].Effort) < effortOrder(model.Choices[j].Effort)
		})
		model.Metadata.DefaultReasoningLevel = model.Choices[0].Effort
		for _, c := range model.Choices {
			if c.Effort == "medium" {
				model.Metadata.DefaultReasoningLevel = c.Effort
			}
			if c.Effort != "" {
				model.Metadata.SupportedReasoningLevels = append(model.Metadata.SupportedReasoningLevels, entities.ModelReasoningLevel{Effort: c.Effort, Description: "Provider-reported " + c.Effort + " reasoning variant"})
			}
			if c.Variant.Context > 0 && (model.Metadata.ContextLength == 0 || c.Variant.Context < model.Metadata.ContextLength) {
				model.Metadata.ContextLength = c.Variant.Context
			}
			if c.Variant.Output > 0 && (model.Metadata.MaxOutputTokens == 0 || c.Variant.Output < model.Metadata.MaxOutputTokens) {
				model.Metadata.MaxOutputTokens = c.Variant.Output
			}
		}
		seen[family.Slug] = true
		out = append(out, model)
	}
	if len(out) == 0 {
		return nil, devinFailure(502, "Devin returned no standard model variants")
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Metadata.ID == "adaptive" {
			return true
		}
		if out[j].Metadata.ID == "adaptive" {
			return false
		}
		return out[i].Metadata.ID < out[j].Metadata.ID
	})
	return out, nil
}

var regexpContextOnly = regexp.MustCompile(`(?i)^\d+(?:k|m)$`)

func effortOrder(s string) int {
	for i, v := range []string{"", "none", "off", "minimal", "low", "medium", "high", "xhigh", "max", "thinking"} {
		if s == v {
			return i
		}
	}
	return 99
}
func selectDevinVariant(models []devinModel, model, effort string) (string, error) {
	for _, m := range models {
		if m.Metadata.ID != model {
			continue
		}
		if effort == "" {
			effort = m.Metadata.DefaultReasoningLevel
		}
		for _, c := range m.Choices {
			if c.Effort == effort {
				return c.Variant.UID, nil
			}
		}
		return "", devinFailure(400, "Devin does not advertise that reasoning level for this model")
	}
	return "", devinFailure(400, "Devin model is not in the current account catalog; refresh models")
}
