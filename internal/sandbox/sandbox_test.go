package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteGeneratesArtifacts(t *testing.T) {
	dir := t.TempDir()
	written, err := Write(dir, true)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Core artifacts must exist on disk with sensible content.
	want := map[string]string{
		"Dockerfile.sandbox":   "FROM debian:stable-slim",
		"compose.sandbox.yaml": "RETOIA_API_KEY",
		"retoia-entrypoint.sh": "RETOIA_API_KEY",
		"SANDBOX.md":           "sandbox edition",
		".dockerignore":        ".git",
	}
	for name, needle := range want {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if !strings.Contains(string(data), needle) {
			t.Errorf("%s does not contain %q", name, needle)
		}
	}

	// Repo-less: the image bakes the host-generated config and installs OpenCode;
	// it must NOT build retoia from source.
	df, _ := os.ReadFile(filepath.Join(dir, "Dockerfile.sandbox"))
	if !strings.Contains(string(df), "COPY opencode-config/") || !strings.Contains(string(df), "opencode.ai/install") {
		t.Error("Dockerfile.sandbox should bake opencode-config and install OpenCode")
	}
	if strings.Contains(string(df), "go build") {
		t.Error("Dockerfile.sandbox must not build from source (repo-less)")
	}

	// Generic isolated OpenCode sandbox: no Kali base, pentest tooling or Tor.
	for name, content := range Files(true) {
		low := strings.ToLower(content)
		for _, banned := range []string{"kali", "nmap", "tor ", "torsocks", "proxychains", "iptables", "retoia_tor", "retoia_with_tor", "pentest"} {
			if strings.Contains(low, banned) {
				t.Errorf("%s still mentions %q", name, banned)
			}
		}
	}

	// The entrypoint script is executable.
	fi, err := os.Stat(filepath.Join(dir, "retoia-entrypoint.sh"))
	if err != nil || fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("entrypoint not executable: mode=%v err=%v", fi.Mode(), err)
	}

	if len(written) == 0 {
		t.Error("Write reported no files")
	}
}

func TestWriteKeepsExistingDockerignore(t *testing.T) {
	dir := t.TempDir()
	di := filepath.Join(dir, ".dockerignore")
	if err := os.WriteFile(di, []byte("my-custom-ignore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := Write(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	// The existing .dockerignore must be preserved.
	data, _ := os.ReadFile(di)
	if string(data) != "my-custom-ignore\n" {
		t.Errorf(".dockerignore was clobbered: %q", string(data))
	}
	kept := false
	for _, n := range written {
		if strings.Contains(n, ".dockerignore") && strings.Contains(n, "kept") {
			kept = true
		}
	}
	if !kept {
		t.Errorf("expected a 'kept existing' note for .dockerignore, got %v", written)
	}
}

func TestComposePersistMounts(t *testing.T) {
	on := Files(true)["compose.sandbox.yaml"]
	off := Files(false)["compose.sandbox.yaml"]
	for _, m := range []string{"./sandbox-data/engram:/root/.engram", "./sandbox-data/opencode:/root/.local/share/opencode"} {
		if !strings.Contains(on, m) {
			t.Errorf("persist=true compose missing %q", m)
		}
		if strings.Contains(off, m) {
			t.Errorf("persist=false compose must not contain %q", m)
		}
	}
	if !strings.Contains(off, "./sandbox-work:/work") || !strings.Contains(on, "./sandbox-work:/work") {
		t.Error("both modes must keep the ./sandbox-work:/work mount")
	}
	dir := t.TempDir()
	if _, err := Write(dir, true); err != nil {
		t.Fatal(err)
	}
	if !PersistEnabled(dir) {
		t.Error("PersistEnabled should be true after Write(persist=true)")
	}
	if _, err := Write(dir, false); err != nil {
		t.Fatal(err)
	}
	if PersistEnabled(dir) {
		t.Error("PersistEnabled should be false after Write(persist=false)")
	}
}
