package cost

import "testing"

func TestCompute(t *testing.T) {
	p := &Prices{InputPerM: 3, OutputPerM: 15, CachedInputPerM: 0.30, CacheWritePerM: 3.75}
	got := Compute(p, Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000})
	want := 3 + 1.5
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("got %f want %f", got, want)
	}
	cache := Compute(p, Usage{CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000})
	if cache != 0.30+3.75 {
		t.Fatalf("cache cost got %f", cache)
	}
}

func TestComputeNilPrices(t *testing.T) {
	if c := Compute(nil, Usage{PromptTokens: 100}); c != 0 {
		t.Fatalf("expected zero cost for missing prices, got %f", c)
	}
}

func TestComputeFractional(t *testing.T) {
	p := &Prices{InputPerM: 0.15, OutputPerM: 0.60}
	got := Compute(p, Usage{PromptTokens: 1000, CompletionTokens: 2000})
	want := (1000*0.15 + 2000*0.60) / 1e6
	if got != want {
		t.Fatalf("got %f want %f", got, want)
	}
}
