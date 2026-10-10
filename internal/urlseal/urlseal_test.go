package urlseal

import (
	"errors"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	const url = "https://gateway.example/v1"
	const pass = "correct horse battery staple"

	blob, err := Seal(url, pass)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if strings.Contains(blob, url) {
		t.Fatal("blob leaks the plaintext URL")
	}

	got, err := Open(blob, pass)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != url {
		t.Errorf("Open = %q, want %q", got, url)
	}
}

func TestOpenWrongPassphrase(t *testing.T) {
	blob, err := Seal("https://gateway.example/v1", "right")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(blob, "wrong"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("Open with wrong passphrase err = %v, want ErrWrongPassphrase", err)
	}
}

func TestOpenMalformed(t *testing.T) {
	for _, bad := range []string{"", "not-base64!!", "YWJj"} { // "abc" decodes but is too short
		if _, err := Open(bad, "pw"); !errors.Is(err, ErrMalformed) {
			t.Errorf("Open(%q) err = %v, want ErrMalformed", bad, err)
		}
	}
}

func TestSealIsRandomized(t *testing.T) {
	a, err := Seal("https://gateway.example/v1", "pw")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Seal("https://gateway.example/v1", "pw")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two seals of the same input are identical; salt/nonce not random")
	}
}

func TestSealRejectsEmptyPassphrase(t *testing.T) {
	if _, err := Seal("https://gateway.example/v1", ""); err == nil {
		t.Error("Seal accepted an empty passphrase; want an error")
	}
}
