package installer

import (
	"os"
	"path/filepath"
	"testing"
)

// copySample copies the shared opencode.json fixture into a temp file and
// returns its path.
func copySample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/opencode.sample.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return dst
}

func TestEnsurePATH(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("# existing rc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binDir := "/home/tester/.local/bin"

	added, line, err := EnsurePATH(rc, binDir)
	if err != nil {
		t.Fatalf("EnsurePATH: %v", err)
	}
	if !added {
		t.Fatal("expected added=true on first call")
	}
	if line == "" {
		t.Fatal("expected a non-empty exported line")
	}
	data, _ := os.ReadFile(rc)
	if !contains(string(data), binDir) {
		t.Errorf("rc does not contain binDir after add:\n%s", data)
	}

	// Idempotent: a second call does nothing.
	added2, _, err := EnsurePATH(rc, binDir)
	if err != nil {
		t.Fatalf("EnsurePATH (2nd): %v", err)
	}
	if added2 {
		t.Error("expected added=false when binDir already present")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

