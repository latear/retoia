package onboard

import (
	"errors"
	"strings"
	"testing"

	"github.com/latear/retoia/internal/keystore"
)

// TestResolveBase covers the plaintext precedence: an explicit flag wins, then
// $RETOIA_BASE, then the default gateway.
func TestResolveBase(t *testing.T) {
	tests := []struct {
		name    string
		flagVal string
		envVal  string
		want    string
	}{
		{name: "flag wins over env", flagVal: "https://flag.example/v1", envVal: "https://env.example/v1", want: "https://flag.example/v1"},
		{name: "flag wins with empty env", flagVal: "https://flag.example/v1", envVal: "", want: "https://flag.example/v1"},
		{name: "env used when no flag", flagVal: "", envVal: "https://env.example/v1", want: "https://env.example/v1"},
		{name: "default when both empty", flagVal: "", envVal: "", want: "https://llm.retoia.app"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveBase(tc.flagVal, tc.envVal); got != tc.want {
				t.Errorf("ResolveBase(%q, %q) = %q, want %q", tc.flagVal, tc.envVal, got, tc.want)
			}
		})
	}
}

// TestResolveServerKey covers key precedence: the environment value wins,
// otherwise the stored "gateway" key is used, and any store error (including
// a not-found) yields an empty string rather than propagating.
func TestResolveServerKey(t *testing.T) {
	envKey := "sk-from-env"
	storeKey := "sk-from-store"

	tests := []struct {
		name      string
		env       string
		fromStore func(string) (string, error)
		want      string
	}{
		{
			name:      "env wins over store",
			env:       envKey,
			fromStore: func(string) (string, error) { return storeKey, nil },
			want:      envKey,
		},
		{
			name:      "store used when env empty",
			env:       "",
			fromStore: func(string) (string, error) { return storeKey, nil },
			want:      storeKey,
		},
		{
			name:      "empty when store not found",
			env:       "",
			fromStore: func(string) (string, error) { return "", keystore.ErrNotFound },
			want:      "",
		},
		{
			name:      "empty when store errors",
			env:       "",
			fromStore: func(string) (string, error) { return "", errors.New("keyring exploded") },
			want:      "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveServerKey(tc.env, tc.fromStore); got != tc.want {
				t.Errorf("ResolveServerKey(%q, fn) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

// TestResolveServerKey_QueriesGatewayAccount ensures the stored key is looked
// up under the "gateway" account, matching what Run persists on success.
func TestResolveServerKey_QueriesGatewayAccount(t *testing.T) {
	var asked string
	ResolveServerKey("", func(account string) (string, error) {
		asked = account
		return "", keystore.ErrNotFound
	})
	if asked != "gateway" {
		t.Errorf("ResolveServerKey queried account %q, want %q", asked, "gateway")
	}
}

// TestUnreachableMessage verifies the gateway-unreachable line names the base
// URL and never leaks anything model-related.
func TestUnreachableMessage(t *testing.T) {
	base := "https://gw.example/v1"
	got := unreachableMessage(base)
	if !strings.Contains(got, base) {
		t.Errorf("unreachableMessage(%q) = %q, want it to contain the base URL", base, got)
	}
	if !strings.Contains(got, "Check the URL") {
		t.Errorf("unreachableMessage(%q) = %q, want the network-check guidance", base, got)
	}
}
