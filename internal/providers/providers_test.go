package providers

import (
	"strings"
	"testing"
)

func TestKnownIsWellFormed(t *testing.T) {
	known := Known()
	if len(known) == 0 {
		t.Fatal("Known() returned no providers")
	}
	seen := map[string]bool{}
	for _, p := range known {
		if p.ID == "" || p.Name == "" || p.BaseURL == "" {
			t.Errorf("provider %+v has an empty field", p)
		}
		if !strings.HasPrefix(p.BaseURL, "https://") {
			t.Errorf("provider %q base URL %q is not https", p.ID, p.BaseURL)
		}
		if strings.ContainsAny(p.ID, "/ ") {
			t.Errorf("provider id %q must be a safe single token", p.ID)
		}
		if seen[p.ID] {
			t.Errorf("duplicate provider id %q", p.ID)
		}
		seen[p.ID] = true
	}
	// Reto IA ships a single provider: the gateway.
	if !seen["retoia"] {
		t.Errorf("expected known provider %q", "retoia")
	}
	if len(known) != 1 {
		t.Errorf("expected exactly one provider, got %d: %+v", len(known), known)
	}
}

func TestLookup(t *testing.T) {
	p, ok := Lookup("retoia")
	if !ok {
		t.Fatal("Lookup(retoia) not found")
	}
	if p.ID != "retoia" || p.BaseURL != DefaultGatewayBase {
		t.Errorf("unexpected provider: %+v", p)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup of unknown id should be false")
	}
}
