package llm

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

func TestDevinCatalogFamiliesAndExactReasoningVariants(t *testing.T) {
	catalog := devinCatalog{Families: []devinFamily{
		{Slug: "future-model", Label: "Future Model", Variants: []devinVariant{
			{UID: "opaque-low", Label: "Future Model Low", Context: 100, Output: 20},
			{UID: "opaque-low-1m", Label: "Future Model Low 1M", Context: 1000, Output: 20},
			{UID: "opaque-medium", Label: "Future Model Medium", Context: 100, Output: 30},
			{UID: "opaque-high", Label: "Future Model High", Context: 100, Output: 25},
			{UID: "opaque-high-fast", Label: "Future Model High Fast"},
			{UID: "opaque-none-priority", Label: "Future Model No Thinking"},
		}},
		{Slug: "another", Label: "Another", Variants: []devinVariant{{UID: "opaque-base", Label: "Another"}, {UID: "opaque-think", Label: "Another Thinking"}}},
		{Slug: "other-fast", Label: "Other Fast", Variants: []devinVariant{{UID: "other-fast", Label: "Other Fast"}}},
		{Slug: "fusion", Label: "Fusion", Variants: []devinVariant{{UID: "fusion-lead-high-sidekick-low", Label: "Fusion (Lead High + Sidekick Low)"}}},
	}}
	models, err := normalizeDevinCatalog(catalog)
	if err != nil || len(models) != 2 {
		t.Fatalf("count=%d err=%v", len(models), err)
	}
	m := models[1].Metadata
	if m.ID != "future-model" || m.DefaultReasoningLevel != "medium" || len(m.SupportedReasoningLevels) != 3 || m.ContextLength != 100 || m.MaxOutputTokens != 20 {
		t.Fatalf("metadata=%+v", m)
	}
	for effort, uid := range map[string]string{"": "opaque-medium", "low": "opaque-low", "high": "opaque-high"} {
		got, err := selectDevinVariant(models, "future-model", effort)
		if err != nil || got != uid {
			t.Fatalf("selection=%q err=%v", got, err)
		}
	}
	for _, effort := range []string{"none", "priority", "fast", "absent"} {
		if _, err := selectDevinVariant(models, "future-model", effort); err == nil {
			t.Fatal("invented effort")
		}
	}
	if _, err := selectDevinVariant(models, "future-model-high", ""); err == nil {
		t.Fatal("exposed variant as public model")
	}
	if got, err := selectDevinVariant(models, "another", "none"); err != nil || got != "opaque-base" {
		t.Fatal("base/thinking mapping")
	}
	if got, err := selectDevinVariant(models, "another", "thinking"); err != nil || got != "opaque-think" {
		t.Fatal("thinking mapping")
	}
}

func TestDevinCatalogNoFabricatedOrUnsafeEntries(t *testing.T) {
	for _, variants := range [][]devinVariant{
		{{UID: "x-fast", Label: "X High Fast"}},
		{{UID: "../../bad", Label: "X Low"}},
		{{UID: "x-unknown", Label: "X Unknown mode"}},
	} {
		if _, err := normalizeDevinCatalog(devinCatalog{Families: []devinFamily{{Slug: "x", Label: "X", Variants: variants}}}); err == nil {
			t.Fatal("invalid catalog accepted")
		}
	}
}

// Opt-in check against a metadata-only live CLI capture. Never record that
// catalog as a fixture: tests use synthetic future families above.
func TestDevinCapturedModelMetadata(t *testing.T) {
	file := os.Getenv("TEST_DEVIN_CATALOG_FILE")
	if file == "" {
		t.Skip("no safe metadata capture configured")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var catalog devinCatalog
	if json.Unmarshal(raw, &catalog) != nil {
		t.Fatal("invalid capture")
	}
	models, err := normalizeDevinCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	variants := 0
	for _, m := range models {
		if devinSpeed.MatchString(m.Metadata.ID) {
			t.Fatal("speed mode exposed")
		}
		for _, c := range m.Choices {
			variants++
			if strings.Contains(c.Variant.UID, "-priority") || strings.Contains(c.Variant.UID, "-fast") {
				t.Fatal("speed mode selected")
			}
			if _, err := selectDevinVariant(models, m.Metadata.ID, c.Effort); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Logf("normalized families=%d reasoning/default variants=%d", len(models), variants)
}

func TestDevinOutputLimitsCannotBeBypassedByIOCopy(t *testing.T) {
	output := &devinOutput{limit: 8}
	if _, err := io.Copy(output, strings.NewReader(strings.Repeat("x", 100))); err != nil {
		t.Fatal(err)
	}
	if !output.overflow || output.buffer.Len() != 8 {
		t.Fatal("unbounded child output")
	}
}
