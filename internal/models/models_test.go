package models

import "testing"

func TestEstimateCostUsesPrices(t *testing.T) {
	dollars, ok := EstimateCost("gpt-5.6-luna", 1_000_000, 0, 0)
	if !ok || dollars != 0.20 {
		t.Fatalf("cost = %v ok = %v", dollars, ok)
	}
	cached, ok := EstimateCost("gpt-5.6-luna", 1_000_000, 0, 1_000_000)
	if !ok || cached != 0.02 {
		t.Fatalf("cached cost = %v", cached)
	}
	if _, ok := EstimateCost("missing", 10, 10, 0); ok {
		t.Fatal("unknown model reported a price")
	}
	if ContextWindow("kimi/kimi-k2.6") != 262144 {
		t.Fatalf("window = %d", ContextWindow("kimi/kimi-k2.6"))
	}
	if ContextWindow("gpt-5.6-sol") != 272000 {
		t.Fatalf("sol window = %d", ContextWindow("gpt-5.6-sol"))
	}
	provider, id, err := Parse("kimi/kimi-k2.6-nothink")
	if err != nil || provider != ProviderKimi || id != "kimi-k2.6" {
		t.Fatalf("nothink = %q %q %v", provider, id, err)
	}
	if entry, _ := Lookup("kimi/kimi-k2.6-nothink"); !entry.DisableThinking {
		t.Fatal("nothink entry keeps thinking on")
	}
	if entry, _ := Lookup("kimi/kimi-k2.6"); entry.DisableThinking {
		t.Fatal("default kimi entry disables thinking")
	}
	if _, _, err := Parse("gpt-4o-mini"); err == nil {
		t.Fatal("gpt-4o-mini is still supported")
	}
}
