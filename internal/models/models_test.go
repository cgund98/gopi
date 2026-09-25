package models

import "testing"

func TestEstimateCostUsesPrices(t *testing.T) {
	dollars, ok := EstimateCost("gpt-4o-mini", 1_000_000, 0, 0)
	if !ok || dollars != 0.15 {
		t.Fatalf("cost = %v ok = %v", dollars, ok)
	}
	cached, ok := EstimateCost("gpt-4o-mini", 1_000_000, 0, 1_000_000)
	if !ok || cached != 0.075 {
		t.Fatalf("cached cost = %v", cached)
	}
	if _, ok := EstimateCost("missing", 10, 10, 0); ok {
		t.Fatal("unknown model reported a price")
	}
	if ContextWindow("kimi/kimi-k2.6") != 262144 {
		t.Fatalf("window = %d", ContextWindow("kimi/kimi-k2.6"))
	}
}
