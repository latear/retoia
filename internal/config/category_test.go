package config

import "testing"

func TestModelCategory(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"auto", CategoryAutomatic},
		{"model-a", CategoryCalden},
		{"cloud-kk", CategoryCloud},
		{"esfuerzo-medio", CategoryByEffort},
		{"model-c", CategoryCalden}, // gateway-style id with no known prefix
	}
	for _, tt := range tests {
		m := Model{ID: tt.id}
		if got := m.Category(); got != tt.want {
			t.Errorf("Model{ID: %q}.Category() = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestGroupModelsByCategoryEffortOrder(t *testing.T) {
	// Shuffled input; the effort category must still come out in the
	// explicit level order (bajo < medio < alto < maximo), not alphabetical.
	models := []Model{
		{ID: "esfuerzo-maximo"},
		{ID: "esfuerzo-bajo"},
		{ID: "esfuerzo-alto"},
		{ID: "esfuerzo-medio"},
	}

	groups := GroupModelsByCategory(models)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d: %+v", len(groups), groups)
	}

	got := groups[0]
	if got.Category != CategoryByEffort {
		t.Fatalf("expected category %q, got %q", CategoryByEffort, got.Category)
	}

	wantOrder := []string{"esfuerzo-bajo", "esfuerzo-medio", "esfuerzo-alto", "esfuerzo-maximo"}
	if len(got.Models) != len(wantOrder) {
		t.Fatalf("expected %d models, got %d: %+v", len(wantOrder), len(got.Models), got.Models)
	}
	for i, id := range wantOrder {
		if got.Models[i].ID != id {
			t.Fatalf("model[%d] = %q, want %q (full: %+v)", i, got.Models[i].ID, id, got.Models)
		}
	}
}

func TestGroupModelsByCategoryUnknownEffortSuffixSortsLast(t *testing.T) {
	models := []Model{
		{ID: "esfuerzo-medio"},
		{ID: "esfuerzo-zzz"}, // unknown suffix
		{ID: "esfuerzo-bajo"},
	}

	groups := GroupModelsByCategory(models)
	got := groups[0].Models

	wantOrder := []string{"esfuerzo-bajo", "esfuerzo-medio", "esfuerzo-zzz"}
	for i, id := range wantOrder {
		if got[i].ID != id {
			t.Fatalf("model[%d] = %q, want %q (full: %+v)", i, got[i].ID, id, got)
		}
	}
}

func TestGroupModelsByCategoryOrderAndEmptyOmitted(t *testing.T) {
	models := []Model{
		{ID: "cloud-kk"},
		{ID: "model-a"},
		{ID: "auto"},
		{ID: "cloud-abc"},
	}

	groups := GroupModelsByCategory(models)

	// "By effort" has no models and must be omitted; order must remain
	// Automatic, Calden, Cloud (fixed order minus the empty category).
	wantCats := []string{CategoryAutomatic, CategoryCalden, CategoryCloud}
	if len(groups) != len(wantCats) {
		t.Fatalf("expected %d groups, got %d: %+v", len(wantCats), len(groups), groups)
	}
	for i, cat := range wantCats {
		if groups[i].Category != cat {
			t.Fatalf("group[%d].Category = %q, want %q", i, groups[i].Category, cat)
		}
	}

	cloud := groups[2]
	if len(cloud.Models) != 2 || cloud.Models[0].ID != "cloud-abc" || cloud.Models[1].ID != "cloud-kk" {
		t.Fatalf("expected cloud models alphabetical [cloud-abc, cloud-kk], got %+v", cloud.Models)
	}
}

func TestCategoryOrderIsFixed(t *testing.T) {
	want := []string{CategoryAutomatic, CategoryCalden, CategoryByEffort, CategoryCloud}
	got := CategoryOrder()
	if len(got) != len(want) {
		t.Fatalf("expected %d categories, got %d: %+v", len(want), len(got), got)
	}
	for i, cat := range want {
		if got[i] != cat {
			t.Fatalf("CategoryOrder()[%d] = %q, want %q", i, got[i], cat)
		}
	}
}
