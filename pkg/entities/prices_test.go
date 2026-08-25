package entities

import "testing"

func TestCalculateCostAndMissingPrice(t *testing.T) {
	u := TokenUsage{PromptTokens: 1_000_000, CompletionTokens: 100_000, CacheReadTokens: 10, CacheWriteTokens: 20}
	missing := CalculateCost(nil, u)
	if missing.Priced || missing.USD != 0 {
		t.Fatalf("missing price must remain explicitly unpriced: %+v", missing)
	}
	p := &Price{InputPerM: 3, OutputPerM: 15, CachedInputPerM: 0.3, CacheWritePerM: 3.75}
	got := CalculateCost(p, u)
	want := 3.0 + 1.5 + 10*0.3/1e6 + 20*3.75/1e6
	if !got.Priced || got.USD != want {
		t.Fatalf("got %+v, want priced cost %.10f", got, want)
	}
}

func TestZeroPriceIsStillPriced(t *testing.T) {
	got := CalculateCost(&Price{}, TokenUsage{PromptTokens: 10})
	if !got.Priced || got.USD != 0 {
		t.Fatalf("zero-priced model must not be reported as unpriced: %+v", got)
	}
}
