package teamkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/stretchr/testify/require"
)

func mockNoGlobalHome(t *testing.T) {
	t.Helper()
	original := vaultconfig.UserHomeDirectory
	home := t.TempDir()
	vaultconfig.UserHomeDirectory = func() (string, error) { return home, nil }
	t.Cleanup(func() { vaultconfig.UserHomeDirectory = original })
}

func syntheticBundle(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	nonce := make([]byte, gcm.NonceSize())
	_, err = rand.Read(nonce)
	require.NoError(t, err)
	payload, err := json.Marshal(keys{Voyage: "synthetic-voyage", TypeSafe: "synthetic-typesafe"})
	require.NoError(t, err)
	original := encryptedKeys
	encryptedKeys = base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, payload, nil))
	ResetCache()
	t.Cleanup(func() { encryptedKeys = original; ResetCache() })
	return base64.StdEncoding.EncodeToString(key)
}

func TestSourceBuildHasNoCredentials(t *testing.T) {
	require.Empty(t, encryptedKeys)
	mockNoGlobalHome(t)
	t.Setenv("ATOMIC_RHIZOME_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	ResetCache()
	t.Cleanup(ResetCache)
	require.False(t, Available())
}

func TestDecryptSyntheticBundle(t *testing.T) {
	for _, mode := range []string{"environment", "global-config", "missing", "wrong"} {
		t.Run(mode, func(t *testing.T) {
			mockNoGlobalHome(t)
			unlock := syntheticBundle(t)
			t.Setenv("ATOMIC_RHIZOME_KEY", "")
			switch mode {
			case "environment":
				t.Setenv("ATOMIC_RHIZOME_KEY", unlock)
			case "wrong":
				t.Setenv("ATOMIC_RHIZOME_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
			case "global-config":
				home, err := vaultconfig.UserHomeDirectory()
				require.NoError(t, err)
				dir := filepath.Join(home, ".config", vaultconfig.RhizomeConfigDirectory)
				require.NoError(t, os.MkdirAll(dir, 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(dir, vaultconfig.RhizomeConfigFile), []byte("env:\n  ATOMIC_RHIZOME_KEY: "+unlock+"\n"), 0o600))
			}
			if mode == "environment" || mode == "global-config" {
				require.Equal(t, "synthetic-voyage", VoyageKey())
				require.Equal(t, "synthetic-typesafe", TypeSafeKey())
				require.True(t, Available())
			} else {
				require.Empty(t, VoyageKey())
				require.Empty(t, TypeSafeKey())
				require.False(t, Available())
			}
		})
	}
}

func TestBundledAndUnlocks(t *testing.T) {
	require.False(t, Bundled(), "source builds carry no bundle")
	require.False(t, Unlocks(base64.StdEncoding.EncodeToString(make([]byte, 32))))

	unlock := syntheticBundle(t)
	require.True(t, Bundled())
	require.True(t, Unlocks(unlock))
	require.True(t, Unlocks("  "+unlock+"\n"))
	require.False(t, Unlocks(base64.StdEncoding.EncodeToString(make([]byte, 32))))
	require.False(t, Unlocks("pa-not-a-team-key"))
}
