// Package teamkeys provides encrypted API keys for team members.
// Keys are decrypted at runtime using the ATOMIC_RHIZOME_KEY environment variable.
package teamkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
)

type keys struct {
	Voyage   string `json:"voyage"`
	TypeSafe string `json:"typesafe,omitempty"`
}

var (
	decryptOnce sync.Once
	decrypted   *keys
	decryptErr  error
)

// ResetCache clears the cached decrypted keys so a newly provided
// ATOMIC_RHIZOME_KEY can be picked up within the same process.
func ResetCache() {
	decryptOnce = sync.Once{}
	decrypted = nil
	decryptErr = nil
}

func decrypt() (*keys, error) {
	decryptOnce.Do(func() {
		unlockKey := vaultconfig.ResolveValue("ATOMIC_RHIZOME_KEY")
		if unlockKey == "" || encryptedKeys == "" {
			// No unlock key = not a team member, just return nil (not an error)
			return
		}
		decrypted, decryptErr = decryptKeys(unlockKey, encryptedKeys)
	})

	return decrypted, decryptErr
}

func decryptKeys(unlockKey, encoded string) (*keys, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(unlockKey)
	if err != nil {
		return nil, err
	}
	if len(keyBytes) != 32 {
		return nil, nil // Invalid key length, silently fail
	}

	// New release bundles carry a marker for pre-publication artifact checks.
	// Unprefixed bundles remain readable for existing installations.
	if _, payload, marked := strings.Cut(encoded, ":"); marked {
		encoded = payload
	}
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, nil // Invalid ciphertext
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		// Decryption failed (wrong key), silently fail
		return nil, nil
	}

	var k keys
	if err := json.Unmarshal(plaintext, &k); err != nil {
		return nil, err
	}

	return &k, nil
}

// VoyageKey returns the team Voyage AI API key, or empty string if unavailable.
func VoyageKey() string {
	k, _ := decrypt()
	if k == nil {
		return ""
	}
	return k.Voyage
}

// TypeSafeKey returns the team TypeSafe API key, or empty string if unavailable.
// A bundle created before TypeSafe was added legitimately has no such key.
func TypeSafeKey() string {
	k, _ := decrypt()
	if k == nil {
		return ""
	}
	return k.TypeSafe
}

// Available returns true if team keys are available (ATOMIC_RHIZOME_KEY is set and valid).
func Available() bool {
	k, _ := decrypt()
	return k != nil
}

// Bundled reports whether this binary carries the encrypted team-key bundle.
// Source and public builds do not, so they never offer the Atomic Object key.
func Bundled() bool {
	return encryptedKeys != ""
}

// Unlocks reports whether value is an Atomic Object Rhizome key that opens
// this binary's bundle. It lets one prompt accept either that key or a
// provider key.
func Unlocks(value string) bool {
	if !Bundled() {
		return false
	}
	k, err := decryptKeys(strings.TrimSpace(value), encryptedKeys)
	return err == nil && k != nil
}
