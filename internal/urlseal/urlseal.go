// Package urlseal seals a short secret (the gateway URL) so it can ship inside
// the binary without exposing it: it is encrypted with a key derived from a
// passphrase the distributor shares out-of-band. Open recovers the plaintext
// only with the right passphrase.
//
// This is an access gate, not DRM: whoever has the passphrase can recover the
// plaintext, and once installed the URL lands in OpenCode's config in the clear
// (OpenCode needs it to route). It keeps the URL out of the repo and the binary
// for anyone without the passphrase — nothing more.
//
// It uses only the standard library (PBKDF2-HMAC-SHA256 + AES-256-GCM), so the
// binary stays dependency-free.
package urlseal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrWrongPassphrase is returned by Open when the passphrase does not decrypt
// the blob (GCM authentication failed). ErrMalformed is returned when the blob
// is not a well-formed sealed value.
var (
	ErrWrongPassphrase = errors.New("urlseal: wrong passphrase")
	ErrMalformed       = errors.New("urlseal: malformed sealed value")
)

const (
	sealVersion = 1  // first (and only) blob layout
	saltLen     = 16 // PBKDF2 salt
	nonceLen    = 12 // AES-GCM standard nonce
	keyLen      = 32 // AES-256
	iterations  = 600_000
)

// deriveKey stretches the passphrase into a 32-byte AES key with PBKDF2.
func deriveKey(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iterations, keyLen)
}

// Seal encrypts plaintext under passphrase and returns a base64 blob laid out as
// version(1) | salt(16) | nonce(12) | ciphertext+tag. The salt and nonce are
// random, so sealing the same input twice yields different blobs.
func Seal(plaintext, passphrase string) (string, error) {
	if passphrase == "" {
		return "", errors.New("urlseal: empty passphrase")
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("urlseal: reading salt: %w", err)
	}
	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return "", fmt.Errorf("urlseal: deriving key: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("urlseal: reading nonce: %w", err)
	}

	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	blob := make([]byte, 0, 1+saltLen+nonceLen+len(ct))
	blob = append(blob, sealVersion)
	blob = append(blob, salt...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return base64.StdEncoding.EncodeToString(blob), nil
}

// Open decrypts a blob produced by Seal. It returns ErrMalformed for a blob that
// is not well-formed and ErrWrongPassphrase when the passphrase is incorrect.
func Open(blob, passphrase string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if len(raw) < 1+saltLen+nonceLen+1 || raw[0] != sealVersion {
		return "", ErrMalformed
	}
	salt := raw[1 : 1+saltLen]
	nonce := raw[1+saltLen : 1+saltLen+nonceLen]
	ct := raw[1+saltLen+nonceLen:]

	key, err := deriveKey(passphrase, salt)
	if err != nil {
		return "", fmt.Errorf("urlseal: deriving key: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// GCM authentication failed: wrong passphrase or tampered blob.
		return "", ErrWrongPassphrase
	}
	return string(pt), nil
}

// newGCM builds an AES-256-GCM AEAD from a 32-byte key.
func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("urlseal: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("urlseal: gcm: %w", err)
	}
	return gcm, nil
}
