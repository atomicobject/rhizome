package teamkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeSafeBundleDecryption(t *testing.T) {
	key := make([]byte, 32)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	for _, typesafe := range []string{"", "synthetic-typesafe-key"} {
		t.Run(typesafe, func(t *testing.T) {
			payload := map[string]string{"openai": "synthetic-openai", "voyage": "synthetic-voyage", "cerebras": "synthetic-cerebras"}
			if typesafe != "" {
				payload["typesafe"] = typesafe
			}
			plaintext, err := json.Marshal(payload)
			require.NoError(t, err)
			nonce := make([]byte, gcm.NonceSize())
			blob := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil))
			decoded, err := decryptKeys(base64.StdEncoding.EncodeToString(key), blob)
			require.NoError(t, err)
			require.Equal(t, "synthetic-voyage", decoded.Voyage)
			require.Equal(t, typesafe, decoded.TypeSafe)
		})
	}
}

func TestTypeSafeKeyUnavailableWithoutUnlock(t *testing.T) {
	mockNoGlobalHome(t)
	t.Setenv("ATOMIC_RHIZOME_KEY", "")
	ResetCache()
	t.Cleanup(ResetCache)
	require.Empty(t, TypeSafeKey())
}

func TestTypeSafeKeyAccessor(t *testing.T) {
	ResetCache()
	t.Cleanup(ResetCache)
	// The encrypted-format test above exercises decryption using synthetic keys.
	// Seed the cache here to keep accessor coverage independent of real credentials.
	decryptOnce.Do(func() { decrypted = &keys{TypeSafe: "synthetic-typesafe-key"} })
	require.Equal(t, "synthetic-typesafe-key", TypeSafeKey())
}
