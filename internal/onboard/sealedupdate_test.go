package onboard

import (
	"testing"

	"github.com/retoia/retoia-cli/internal/urlseal"
)

func TestUnsealUpdateBase(t *testing.T) {
	old := sealedUpdateBase
	defer func() { sealedUpdateBase = old }()

	// Empty slot → "" with no error (update base is optional).
	sealedUpdateBase = ""
	if v, err := UnsealUpdateBase("anything"); err != nil || v != "" {
		t.Errorf("empty slot: got (%q,%v), want (\"\",nil)", v, err)
	}

	blob, err := urlseal.Seal("https://cs-updates.example", "pass123")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	sealedUpdateBase = blob

	if v, err := UnsealUpdateBase("pass123"); err != nil || v != "https://cs-updates.example" {
		t.Errorf("unseal: got (%q,%v), want the URL", v, err)
	}
	if v, err := UnsealUpdateBase(""); err != nil || v != "" {
		t.Errorf("empty passphrase: got (%q,%v), want (\"\",nil)", v, err)
	}
}
