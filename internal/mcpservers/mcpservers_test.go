package mcpservers

import "testing"

func TestCatalogSortedAndValid(t *testing.T) {
	cat := Catalog()
	if len(cat) == 0 {
		t.Fatal("catalog is empty")
	}
	for i, s := range cat {
		if s.ID == "" || s.Name == "" || s.Description == "" || s.RepoURL == "" {
			t.Errorf("catalog[%d] is missing required fields: %+v", i, s)
		}
		// A wireable entry carries a command (local) OR a url (remote); a manual
		// one carries neither.
		if s.Manual() && (len(s.Command) != 0 || s.URL != "") {
			t.Errorf("%s: Manual() true but has a command/url", s.ID)
		}
		if !s.Manual() && len(s.Command) == 0 && s.URL == "" {
			t.Errorf("%s: not manual but has neither command nor url", s.ID)
		}
		if i > 0 && cat[i-1].ID >= s.ID {
			t.Errorf("catalog not sorted at %d: %q >= %q", i, cat[i-1].ID, s.ID)
		}
	}
}

func TestFind(t *testing.T) {
	// context7 is wireable via a remote URL (no local command to bake, and no
	// setup steps to run).
	s, ok := Find("context7")
	if !ok {
		t.Fatal("context7 not found in catalog")
	}
	if !s.Wireable() || s.URL == "" {
		t.Errorf("context7 should be wireable with a URL, got %q", s.URL)
	}
	if s.NeedsSetup() {
		t.Error("context7 should not report NeedsSetup (no setup steps)")
	}

	if _, ok := Find("does-not-exist"); ok {
		t.Error("Find returned ok for a missing id")
	}
}

// TestNoAPIKeyPolicy enforces the catalog rule: no entry requires an API key or
// subscription (nothing lands in Env).
func TestNoAPIKeyPolicy(t *testing.T) {
	for _, s := range Catalog() {
		if len(s.Env) > 0 {
			t.Errorf("%s declares env %v; catalog policy forbids API-key/subscription servers", s.ID, s.Env)
		}
	}
}

// TestLookup covers the id lookup used by the install TUI to wire context7.
func TestLookup(t *testing.T) {
	s, ok := Lookup("context7")
	if !ok {
		t.Fatal("context7 not found in the catalog")
	}
	if s.URL == "" {
		t.Errorf("context7 should be wireable (have a URL); got none")
	}
	if _, ok := Lookup("does-not-exist"); ok {
		t.Error("Lookup of an unknown id should return ok=false")
	}
}
