// Package keystore provides secure storage for the gateway server key and
// BYOK (bring-your-own-key) provider keys used by caldensec.
//
// By default secrets live in a 0600-permission JSON file under the user's
// config directory (~/.config/caldensec/secrets.json) — the same approach the
// aws and gh CLIs take. The OS keyring is OFF by default and opt-in via
// CALDENSEC_KEYCHAIN=1, because on macOS a broken or locked login keychain
// makes the Security framework pop a blocking native dialog ("no keychain
// found — reset or cancel") that no timeout can dismiss and that stalls or
// crashes a non-interactive install, especially over SSH. With the keyring
// enabled, storage still falls back to the file store when a keyring call
// fails or times out. Secrets are never written to opencode.json and are
// always masked before being shown to the user.
package keystore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

// service is the keyring service name under which all caldensec secrets are
// stored.
const service = "caldensec"

// keyringEnvVar opts into the OS keyring. Unset (the default), caldensec never
// touches the OS keyring, so macOS can never raise its blocking keychain dialog.
const keyringEnvVar = "CALDENSEC_KEYCHAIN"

// keyringAllowed reports whether caldensec may touch the OS keyring. It is
// opt-in: only CALDENSEC_KEYCHAIN=1 enables it. See the package doc for why the
// keyring is off by default.
func keyringAllowed() bool {
	return os.Getenv(keyringEnvVar) == "1"
}

// keyringTimeout bounds every OS-keyring call. On macOS the keyring shells out
// to `security`, which can block indefinitely on a GUI authorization dialog —
// invisible, and unanswerable, over SSH. A working keyring answers in well under
// a second, so a call that exceeds this is treated as unavailable and storage
// falls back to the file store rather than hanging the whole program.
const keyringTimeout = 5 * time.Second

// errKeyringTimeout marks a keyring call that exceeded keyringTimeout.
var errKeyringTimeout = errors.New("keystore: keyring call timed out")

// keyringDo runs an error-returning keyring call under keyringTimeout. A call
// still in flight at the deadline is abandoned (its goroutine may linger until
// the underlying tool returns or the process exits).
func keyringDo(fn func() error) error {
	ch := make(chan error, 1)
	go func() { ch <- fn() }()
	select {
	case err := <-ch:
		return err
	case <-time.After(keyringTimeout):
		return errKeyringTimeout
	}
}

// keyringGet is keyringDo for the value-returning Get.
func keyringGet(account string) (string, error) {
	type result struct {
		secret string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		s, e := keyring.Get(service, account)
		ch <- result{s, e}
	}()
	select {
	case r := <-ch:
		return r.secret, r.err
	case <-time.After(keyringTimeout):
		return "", errKeyringTimeout
	}
}

// secretsFileName is the name of the fallback JSON store file, kept inside
// the fileStore's base directory.
const secretsFileName = "secrets.json"

// ErrNotFound is returned by Get when no secret is stored for the given
// account, whether the lookup went through the OS keyring or the file
// fallback.
var ErrNotFound = errors.New("keystore: not found")

// Set stores secret under account, preferring the OS keyring and falling
// back to a 0600 file store when the keyring is unavailable or rejects the
// write.
func Set(account, secret string) error {
	if keyringAllowed() {
		if err := keyringDo(func() error { return keyring.Set(service, account, secret) }); err == nil {
			return nil
		}
		// The keyring refused the write or did not answer in time; persist to the
		// file store instead.
	}
	return defaultFileStore().Set(account, secret)
}

// Get retrieves the secret stored for account. It returns ErrNotFound when no
// secret is stored. Any keyring lookup failure — including a keyring miss —
// falls through to the file store, so a secret written to the fallback (while
// the keyring was unavailable) is still found once the keyring is reachable.
func Get(account string) (string, error) {
	if keyringAllowed() {
		if secret, err := keyringGet(account); err == nil {
			return secret, nil
		}
	}
	return defaultFileStore().Get(account)
}

// Delete removes the secret stored for account from both the keyring and the
// file store, so it cannot linger in either. Deleting an account that has no
// stored secret is not an error.
func Delete(account string) error {
	// When the keyring is enabled, ignore its outcome (a miss, an unavailable
	// backend, or a timeout); always clear any file-store entry as well.
	if keyringAllowed() {
		_ = keyringDo(func() error { return keyring.Delete(service, account) })
	}
	return defaultFileStore().Delete(account)
}

// Mask returns a display-safe version of secret: a short head (matching a
// "sk-" style prefix when present) plus the last 4 characters, with the
// middle replaced by "...". Short or empty secrets never reveal any part of
// the value.
func Mask(secret string) string {
	const (
		minMaskable = 9 // must be long enough to hide a non-empty middle
		suffixLen   = 4
	)

	if secret == "" {
		return ""
	}
	if len(secret) < minMaskable {
		return "****"
	}

	head := secret[:3]
	tail := secret[len(secret)-suffixLen:]
	return head + "..." + tail
}

// fileStore is a JSON-file-backed secret store used as a fallback when the
// OS keyring is unavailable. It is kept independently testable: callers can
// point it at a temporary directory instead of relying on the OS keyring.
type fileStore struct {
	baseDir string
}

// defaultFileStore returns a fileStore rooted at ~/.config/caldensec.
func defaultFileStore() *fileStore {
	dir, err := configDir()
	if err != nil {
		// Fall back to a relative directory rather than panicking; Set/Get
		// will surface any resulting I/O error to the caller.
		dir = ".config/caldensec"
	}
	return &fileStore{baseDir: dir}
}

// configDir returns ~/.config/caldensec.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("keystore: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "caldensec"), nil
}

func (fs *fileStore) path() string {
	return filepath.Join(fs.baseDir, secretsFileName)
}

// load reads the current secrets map from disk. A missing file is treated
// as an empty map, not an error.
func (fs *fileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(fs.path())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("keystore: read file store: %w", err)
	}

	secrets := map[string]string{}
	if len(data) == 0 {
		return secrets, nil
	}
	if err := json.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("keystore: parse file store: %w", err)
	}
	return secrets, nil
}

// save persists the secrets map to disk with restrictive permissions: the
// parent directory is 0700 and the file itself is 0600.
func (fs *fileStore) save(secrets map[string]string) error {
	if err := os.MkdirAll(fs.baseDir, 0o700); err != nil {
		return fmt.Errorf("keystore: create file store directory: %w", err)
	}
	// os.MkdirAll does not change the mode of a pre-existing directory, so
	// enforce it explicitly to guarantee the parent is never group/other
	// accessible, regardless of how it was created.
	if err := os.Chmod(fs.baseDir, 0o700); err != nil {
		return fmt.Errorf("keystore: set file store directory permissions: %w", err)
	}

	data, err := json.Marshal(secrets)
	if err != nil {
		return fmt.Errorf("keystore: encode file store: %w", err)
	}

	if err := os.WriteFile(fs.path(), data, 0o600); err != nil {
		return fmt.Errorf("keystore: write file store: %w", err)
	}
	// os.WriteFile does not change the mode of a pre-existing file, so
	// enforce it explicitly in case the file already existed with looser
	// permissions.
	if err := os.Chmod(fs.path(), 0o600); err != nil {
		return fmt.Errorf("keystore: set file store permissions: %w", err)
	}
	return nil
}

// Set stores secret under account in the file store.
func (fs *fileStore) Set(account, secret string) error {
	secrets, err := fs.load()
	if err != nil {
		return err
	}
	secrets[account] = secret
	return fs.save(secrets)
}

// Get retrieves the secret stored for account, returning ErrNotFound if
// absent (including when the store file itself does not exist).
func (fs *fileStore) Get(account string) (string, error) {
	secrets, err := fs.load()
	if err != nil {
		return "", err
	}
	secret, ok := secrets[account]
	if !ok {
		return "", ErrNotFound
	}
	return secret, nil
}

// Delete removes the secret stored for account. Deleting an account with no
// stored secret, or when the store file does not exist at all, is not an
// error.
func (fs *fileStore) Delete(account string) error {
	secrets, err := fs.load()
	if err != nil {
		return err
	}
	if _, ok := secrets[account]; !ok {
		return nil
	}
	delete(secrets, account)
	return fs.save(secrets)
}
