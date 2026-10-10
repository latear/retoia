package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestPlatformKey(t *testing.T) {
	cases := map[string]struct {
		want string
		ok   bool
	}{
		"retoia_mac_arm64.tar.gz":     {"mac_arm64", true},
		"retoia_linux_amd64.tar.gz":   {"linux_amd64", true},
		"checksums.txt":               {"", false},
		"retoia_mac_arm64.tar.gz.sig": {"", false}, // sig sibling, not an archive
		"LICENSE":                     {"", false},
	}
	for name, c := range cases {
		got, ok := platformKey(name)
		if got != c.want || ok != c.ok {
			t.Errorf("platformKey(%q) = (%q,%v), want (%q,%v)", name, got, ok, c.want, c.ok)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	in := "abc123  retoia_mac_arm64.tar.gz\ndef456  retoia_linux_amd64.tar.gz\n\n"
	got := parseChecksums(in)
	want := map[string]string{
		"retoia_mac_arm64.tar.gz":   "abc123",
		"retoia_linux_amd64.tar.gz": "def456",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseChecksums = %v, want %v", got, want)
	}
}

func TestKeyAllowed(t *testing.T) {
	s := &server{keys: []string{"sk-good", "sk-also-good"}}
	if !s.keyAllowed("sk-good") {
		t.Error("expected sk-good to be allowed")
	}
	if s.keyAllowed("sk-bad") {
		t.Error("sk-bad must not be allowed")
	}
}

func TestAuthorizeRejectsWithoutKey(t *testing.T) {
	s := &server{keys: []string{"sk-good"}}
	// no Authorization header -> 401, handler must not run
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/retoia/manifest.json", nil)
	if s.authorize(rec, req) {
		t.Error("authorize should fail without a bearer key")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}

	// valid allowlist key -> ok
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/retoia/manifest.json", nil)
	req2.Header.Set("Authorization", "Bearer sk-good")
	if !s.authorize(rec2, req2) {
		t.Error("authorize should accept an allowlisted key")
	}
}
