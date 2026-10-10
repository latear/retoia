package keystore

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestKeyringAllowed_DefaultOff locks in the macOS-safe default: the OS keyring
// is never touched unless CALDENSEC_KEYCHAIN=1 is set. Flipping this default
// would reintroduce the blocking macOS keychain dialog that stalls installs.
func TestKeyringAllowed_DefaultOff(t *testing.T) {
	t.Setenv(keyringEnvVar, "")
	if keyringAllowed() {
		t.Fatal("keyring must be OFF by default (unset env)")
	}
	for _, v := range []string{"0", "true", "yes", "on"} {
		t.Setenv(keyringEnvVar, v)
		if keyringAllowed() {
			t.Errorf("keyring must stay OFF for %s=%q (only \"1\" enables it)", keyringEnvVar, v)
		}
	}
	t.Setenv(keyringEnvVar, "1")
	if !keyringAllowed() {
		t.Errorf("%s=1 must enable the keyring", keyringEnvVar)
	}
}

func TestMask(t *testing.T) {
	cases := []struct {
		name   string
		secret string
		want   string
	}{
		{
			name:   "typical sk key",
			secret: "sk-1234567890abcdefOuYw",
			want:   "sk-...OuYw",
		},
		{
			name:   "empty",
			secret: "",
			want:   "",
		},
		{
			name:   "very short",
			secret: "abc",
			want:   "****",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Mask(tc.secret)
			if got != tc.want {
				t.Fatalf("Mask(%q) = %q, want %q", tc.secret, got, tc.want)
			}
			if tc.secret != "" && len(tc.secret) > 4 && got == tc.secret {
				t.Fatalf("Mask(%q) returned the secret unmasked", tc.secret)
			}
			if len(tc.secret) > 8 && got != "" && len(got) >= len(tc.secret) {
				t.Fatalf("Mask(%q) = %q did not shorten the secret", tc.secret, got)
			}
		})
	}
}

func TestFileStore_RoundTrip(t *testing.T) {
	fs := &fileStore{baseDir: t.TempDir()}

	if err := fs.Set("gateway", "sk-secret-value"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	got, err := fs.Get("gateway")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "sk-secret-value" {
		t.Fatalf("Get() = %q, want %q", got, "sk-secret-value")
	}

	if _, err := fs.Get("missing-account"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(missing) error = %v, want ErrNotFound", err)
	}

	if err := fs.Delete("gateway"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := fs.Get("gateway"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
	}

	if err := fs.Delete("gateway"); err != nil {
		t.Fatalf("Delete(missing) error = %v, want nil", err)
	}
}

func TestFileStore_Perms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}

	base := t.TempDir()
	fs := &fileStore{baseDir: base}

	if err := fs.Set("provider-deepseek", "sk-abc123456789OuYw"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	path := filepath.Join(base, secretsFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("secrets file perm = %o, want 0600", perm)
	}

	dirInfo, err := os.Stat(base)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", base, err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("secrets dir perm = %o, is accessible to group/other", perm)
	}
}

func TestFileStore_NotJSONLeak(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}

	base := t.TempDir()
	fs := &fileStore{baseDir: base}

	const secret = "sk-super-secret-value-OuYw"
	if err := fs.Set("provider-glm", secret); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	path := filepath.Join(base, secretsFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("secrets file perm = %o, is accessible to group/other", perm)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if !strings.Contains(string(data), secret) {
		t.Fatalf("expected the 0600 secrets file to contain the stored secret")
	}
}
