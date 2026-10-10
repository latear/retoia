package onboard

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ficopix/caldensec/internal/keystore"
	"github.com/ficopix/caldensec/internal/urlseal"
)

// TestRealTemplates_Unseal is a verification harness, not a CI test: it is
// skipped unless $CALDENSEC_UNLOCK is set. With the real install passphrase it
// proves the baked templates in sealed.go unseal correctly AND with the SAME
// passphrase as the gateway URL (AES-256-GCM is authenticated, so a wrong
// passphrase fails cleanly here instead of silently falling back at install).
// It also catches stale agent names. Run it via ./verify-template.sh.
func TestRealTemplates_Unseal(t *testing.T) {
	pass := os.Getenv("CALDENSEC_UNLOCK")
	if pass == "" {
		t.Skip("set CALDENSEC_UNLOCK (via ./verify-template.sh) to verify the real sealed templates")
	}
	if _, err := UnsealBase(pass); err != nil {
		t.Fatalf("UnsealBase failed — this passphrase does not match the sealed gateway URL; reseal the templates with the SAME passphrase: %v", err)
	}
	calden, cloud, both, err := UnsealAgents(pass)
	if err != nil {
		t.Fatalf("UnsealAgents failed (wrong passphrase or corrupt blob): %v", err)
	}
	// Every agent that exists must be covered, under its CURRENT name.
	wantAgents := []string{
		"Calden-Orchestrator", "Calden-alone",
		"cs-coder", "cs-recon", "cs-pentester", "cs-reviewer", "cs-reporter",
	}
	for _, slot := range []struct {
		name string
		m    map[string]string
	}{{"calden", calden}, {"cloud", cloud}, {"both", both}} {
		if len(slot.m) == 0 {
			t.Errorf("slot %q unsealed empty", slot.name)
			continue
		}
		keys := make([]string, 0, len(slot.m))
		for k, v := range slot.m {
			keys = append(keys, k+"="+v)
		}
		sort.Strings(keys)
		t.Logf("slot %q (%d): %s", slot.name, len(slot.m), strings.Join(keys, "  "))
		for _, a := range wantAgents {
			if slot.m[a] == "" {
				t.Errorf("slot %q missing agent %q (stale template with old names?)", slot.name, a)
			}
		}
	}
}

// TestResolveBase covers the plaintext precedence: an explicit flag wins, then
// $CALDENSEC_BASE. With neither and no sealed URL baked in, it reports
// ErrNoGateway.
func TestResolveBase(t *testing.T) {
	// Ensure no passphrase leaks in from the real environment.
	t.Setenv(unlockEnv, "")
	// This test covers the plaintext precedence and the no-gateway case, so it
	// must not depend on whatever URL is sealed into the build.
	old := sealedBase
	sealedBase = ""
	t.Cleanup(func() { sealedBase = old })

	tests := []struct {
		name    string
		flagVal string
		envVal  string
		want    string
		wantErr error
	}{
		{name: "flag wins over env", flagVal: "https://flag.example/v1", envVal: "https://env.example/v1", want: "https://flag.example/v1"},
		{name: "flag wins with empty env", flagVal: "https://flag.example/v1", envVal: "", want: "https://flag.example/v1"},
		{name: "env used when no flag", flagVal: "", envVal: "https://env.example/v1", want: "https://env.example/v1"},
		{name: "no gateway when both empty and nothing sealed", flagVal: "", envVal: "", wantErr: ErrNoGateway},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveBase(tc.flagVal, tc.envVal)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ResolveBase err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveBase unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveBase(%q, %q) = %q, want %q", tc.flagVal, tc.envVal, got, tc.want)
			}
		})
	}
}

// TestResolveBaseSealed covers the sealed-URL path: with a sealed value baked in
// (simulated by setting the package var), the passphrase in $CALDENSEC_UNLOCK
// opens it; a missing passphrase is ErrLocked and a wrong one surfaces
// urlseal.ErrWrongPassphrase.
func TestResolveBaseSealed(t *testing.T) {
	const url = "https://sealed.example/v1"
	const pass = "install-passphrase"
	blob, err := urlseal.Seal(url, pass)
	if err != nil {
		t.Fatal(err)
	}

	old := sealedBase
	sealedBase = blob
	t.Cleanup(func() { sealedBase = old })

	t.Run("opens with the right passphrase", func(t *testing.T) {
		t.Setenv(unlockEnv, pass)
		got, err := ResolveBase("", "")
		if err != nil {
			t.Fatalf("ResolveBase: %v", err)
		}
		if got != url {
			t.Errorf("ResolveBase = %q, want %q", got, url)
		}
	})

	t.Run("locked without a passphrase", func(t *testing.T) {
		t.Setenv(unlockEnv, "")
		if _, err := ResolveBase("", ""); !errors.Is(err, ErrLocked) {
			t.Errorf("err = %v, want ErrLocked", err)
		}
	})

	t.Run("wrong passphrase", func(t *testing.T) {
		t.Setenv(unlockEnv, "nope")
		if _, err := ResolveBase("", ""); !errors.Is(err, urlseal.ErrWrongPassphrase) {
			t.Errorf("err = %v, want ErrWrongPassphrase", err)
		}
	})

	t.Run("flag still wins over a sealed value", func(t *testing.T) {
		t.Setenv(unlockEnv, pass)
		got, err := ResolveBase("https://flag.example/v1", "")
		if err != nil || got != "https://flag.example/v1" {
			t.Errorf("ResolveBase = %q, %v; want flag URL", got, err)
		}
	})
}

// TestUnsealBase covers opening the baked-in sealed URL with a passphrase,
// independent of the environment: empty passphrase is ErrLocked, a wrong one
// surfaces urlseal.ErrWrongPassphrase, and nothing sealed is ErrNoGateway.
func TestUnsealBase(t *testing.T) {
	const url = "https://sealed.example/v1"
	const pass = "install-passphrase"

	t.Run("nothing sealed", func(t *testing.T) {
		old := sealedBase
		sealedBase = ""
		t.Cleanup(func() { sealedBase = old })
		if Sealed() {
			t.Error("Sealed() = true with empty sealedBase")
		}
		if _, err := UnsealBase(pass); !errors.Is(err, ErrNoGateway) {
			t.Errorf("err = %v, want ErrNoGateway", err)
		}
	})

	blob, err := urlseal.Seal(url, pass)
	if err != nil {
		t.Fatal(err)
	}
	old := sealedBase
	sealedBase = blob
	t.Cleanup(func() { sealedBase = old })

	if !Sealed() {
		t.Error("Sealed() = false with a sealed value")
	}
	if got, err := UnsealBase(pass); err != nil || got != url {
		t.Errorf("UnsealBase = %q, %v; want %q, nil", got, err, url)
	}
	if _, err := UnsealBase(""); !errors.Is(err, ErrLocked) {
		t.Errorf("empty passphrase err = %v, want ErrLocked", err)
	}
	if _, err := UnsealBase("wrong"); !errors.Is(err, urlseal.ErrWrongPassphrase) {
		t.Errorf("wrong passphrase err = %v, want ErrWrongPassphrase", err)
	}
}

// TestUnsealAgents covers the sealed calden/cloud/both templates: empty slots
// yield nil maps; sealed slots open with the right passphrase; a wrong or empty
// passphrase errors.
func TestUnsealAgents(t *testing.T) {
	t.Run("nothing sealed", func(t *testing.T) {
		oc0, oc1, ob0 := sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth
		sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth = "", "", ""
		t.Cleanup(func() { sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth = oc0, oc1, ob0 })
		cd, cl, bo, err := UnsealAgents("pw")
		if err != nil || cd != nil || cl != nil || bo != nil {
			t.Errorf("got (%v, %v, %v, %v), want all nil", cd, cl, bo, err)
		}
	})

	const pass = "install-pass"
	cdb, _ := urlseal.Seal(`{"cs-recon":"model-b"}`, pass)
	clb, _ := urlseal.Seal(`{"cs-recon":"cloud-x"}`, pass)
	bob, _ := urlseal.Seal(`{"cs-recon":"both-x"}`, pass)
	oc0, oc1, ob0 := sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth
	sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth = cdb, clb, bob
	t.Cleanup(func() { sealedAgentsCalden, sealedAgentsCloud, sealedAgentsBoth = oc0, oc1, ob0 })

	cd, cl, bo, err := UnsealAgents(pass)
	if err != nil {
		t.Fatalf("UnsealAgents: %v", err)
	}
	if cd["cs-recon"] != "model-b" || cl["cs-recon"] != "cloud-x" || bo["cs-recon"] != "both-x" {
		t.Errorf("got calden=%v cloud=%v both=%v, want the sealed maps", cd, cl, bo)
	}
	if _, _, _, err := UnsealAgents(""); !errors.Is(err, ErrLocked) {
		t.Errorf("empty passphrase err = %v, want ErrLocked", err)
	}
	if _, _, _, err := UnsealAgents("wrong"); !errors.Is(err, urlseal.ErrWrongPassphrase) {
		t.Errorf("wrong passphrase err = %v, want ErrWrongPassphrase", err)
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
