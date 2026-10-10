package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelCategoryLabelOverrides(t *testing.T) {
	m := Model{ID: "cloud-x", CategoryLabel: "z.ai"}
	if got := m.Category(); got != "z.ai" {
		t.Errorf("Category() = %q, want the explicit label z.ai", got)
	}
	// Without a label, the id prefix still decides.
	if got := (Model{ID: "cloud-x"}).Category(); got != CategoryCloud {
		t.Errorf("Category() = %q, want %q", got, CategoryCloud)
	}
}

func TestGroupModelsByCategoryAppendsCustom(t *testing.T) {
	models := []Model{
		{ID: "auto"},                            // Automatic (known)
		{ID: "glm-4", CategoryLabel: "z.ai"},    // custom
		{ID: "gpt-4o", CategoryLabel: "OpenAI"}, // custom
		{ID: "cloud-kimi"},                      // Cloud (known)
	}
	groups := GroupModelsByCategory(models)
	var order []string
	for _, g := range groups {
		order = append(order, g.Category)
	}
	// Known categories first (in fixed order), then custom ones alphabetically.
	want := []string{CategoryAutomatic, CategoryCloud, "OpenAI", "z.ai"}
	if len(order) != len(want) {
		t.Fatalf("categories = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("category[%d] = %q, want %q (all: %v)", i, order[i], want[i], order)
		}
	}
}

func TestManagedAgentsKeepsRetoiaAgentOnCustomProvider(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{
	  "agent": {
	    "cs-coder": {"mode": "subagent", "model": "zai/glm-4.6"},
	    "some-other":   {"mode": "primary",  "model": "openai/gpt-4o"}
	  },
	  "provider": {
	    "retoia": {"options": {"baseURL": "x"}, "models": {"model-a": {"name": "o"}}},
	    "zai":       {"options": {"baseURL": "y"}, "models": {"glm-4.6": {"name": "GLM"}}}
	  }
	}`
	if err := os.WriteFile(cfg, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(cfg, "retoia")
	if err != nil {
		t.Fatal(err)
	}

	agents := c.ManagedAgents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 managed agent (the Retoia one), got %d: %+v", len(agents), agents)
	}
	if agents[0].Name != "cs-coder" || agents[0].CurrentModel != "glm-4.6" {
		t.Errorf("got %+v, want cs-coder with current model glm-4.6 (prefix stripped)", agents[0])
	}

	// Provider enumeration and per-provider models.
	ids := c.ProviderIDs()
	if len(ids) != 2 || ids[0] != "retoia" || ids[1] != "zai" {
		t.Errorf("ProviderIDs = %v, want [retoia zai]", ids)
	}
	zai := c.ProviderModels("zai")
	if len(zai) != 1 || zai[0].ID != "glm-4.6" || zai[0].Provider != "zai" {
		t.Errorf("ProviderModels(zai) = %+v, want one glm-4.6 tagged zai", zai)
	}
}
