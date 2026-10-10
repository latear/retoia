package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestBaseFromProvider(t *testing.T) {
	t.Setenv("CALDENSEC_UPDATE_BASE", "")
	got, err := BaseFromProvider("https://gw.example/v1")
	if err != nil {
		t.Fatalf("BaseFromProvider: %v", err)
	}
	if got != "https://gw.example" {
		t.Errorf("derived base = %q, want https://gw.example", got)
	}

	t.Setenv("CALDENSEC_UPDATE_BASE", "https://override.example/")
	if got, _ := BaseFromProvider("https://gw.example/v1"); got != "https://override.example" {
		t.Errorf("env override = %q, want https://override.example (trailing slash trimmed)", got)
	}

	t.Setenv("CALDENSEC_UPDATE_BASE", "")
	if _, err := BaseFromProvider(""); err == nil {
		t.Error("expected an error when no baseURL and no env override")
	}
}

func TestSameVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.6", "0.2.6", true},
		{"0.2.6", "v0.2.6", true},
		{"v0.2.6", "v0.2.6", true},
		{"v0.2.6", "v0.2.7", false},
		{"dev", "v0.2.6", false},
	} {
		if got := sameVersion(c.a, c.b); got != c.want {
			t.Errorf("sameVersion(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("pretend tarball bytes")
	sum := sha256.Sum256(archive)
	good := Platform{SHA256: hex.EncodeToString(sum[:])}
	if err := verify(archive, good); err != nil {
		t.Errorf("verify with correct sha256: %v", err)
	}
	bad := Platform{SHA256: "deadbeef"}
	if err := verify(archive, bad); err == nil {
		t.Error("verify should fail on a wrong sha256")
	}
	if err := verify(archive, Platform{}); err == nil {
		t.Error("verify should fail when the sha256 is missing")
	}
}

func TestExtractBinary(t *testing.T) {
	// Build a GoReleaser-shaped archive: files wrapped in caldensec/.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	want := []byte("#!/bin/true\nthe-real-binary\n")
	for _, f := range []struct {
		name string
		body []byte
	}{
		{"caldensec/LICENSE", []byte("license text")},
		{"caldensec/caldensec", want},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()

	got, err := extractBinary(buf.Bytes())
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("extracted binary = %q, want %q", got, want)
	}
}

func TestWriteAndRename(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "caldensec")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAndRename(target, []byte("new-binary")); err != nil {
		t.Fatalf("writeAndRename: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new-binary" {
		t.Errorf("target content = %q, want new-binary", got)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o755 {
		t.Errorf("target mode = %v, want 0755", info.Mode().Perm())
	}
}
