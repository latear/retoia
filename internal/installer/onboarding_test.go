package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteOnboardingDocsFresh(t *testing.T) {
	dir := t.TempDir()
	wroteReto, refreshed, err := WriteOnboardingDocs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !wroteReto || !refreshed {
		t.Errorf("wroteReto=%v refreshed=%v, want both true", wroteReto, refreshed)
	}
	reto, _ := os.ReadFile(filepath.Join(dir, "RETO.md"))
	if !strings.Contains(string(reto), "# Reglas del Reto") {
		t.Errorf("RETO.md missing template:\n%s", reto)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	s := string(agents)
	i, j := strings.Index(s, onboardingStart), strings.Index(s, onboardingEnd)
	if i < 0 || j < i || !strings.Contains(s[i:j], "ENGAGEMENT.md") {
		t.Errorf("block not between markers:\n%s", s)
	}
}

func TestWriteOnboardingDocsKeepsExistingReto(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "RETO.md")
	if err := os.WriteFile(p, []byte("mis reglas"), 0o644); err != nil {
		t.Fatal(err)
	}
	wroteReto, _, err := WriteOnboardingDocs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if wroteReto {
		t.Error("wroteReto = true for an existing RETO.md")
	}
	if b, _ := os.ReadFile(p); string(b) != "mis reglas" {
		t.Errorf("RETO.md was clobbered: %q", b)
	}
}

func TestWriteOnboardingDocsMergesAgents(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(p, []byte("# Mis notas\n\nhola\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := WriteOnboardingDocs(dir); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.HasPrefix(s, "# Mis notas\n\nhola\n") {
		t.Errorf("existing content not preserved:\n%s", s)
	}
	if n := strings.Count(s, onboardingStart); n != 1 {
		t.Errorf("block duplicated (%d starts):\n%s", n, s)
	}
	// Stale block content is replaced, trailing user content kept.
	stale := "intro\n" + onboardingStart + "\nviejo\n" + onboardingEnd + "\ncola\n"
	if err := os.WriteFile(p, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteOnboardingDocs(dir); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	s = string(b)
	if strings.Contains(s, "viejo") || !strings.HasPrefix(s, "intro\n") || !strings.HasSuffix(s, "\ncola\n") {
		t.Errorf("stale block not replaced cleanly:\n%s", s)
	}
}
