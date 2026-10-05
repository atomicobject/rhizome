package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratorIncludesTypeSafeAndRequiresEveryKey(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "encrypt")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	source, err := filepath.Abs("encrypt.go")
	require.NoError(t, err)
	build := exec.Command("go", "build", "-o", binary, source)
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "pkg", "teamkeys"), 0o755))
	key := make([]byte, 32)
	variables := map[string]string{
		"ATOMIC_RHIZOME_KEY": base64.StdEncoding.EncodeToString(key),
		"VOYAGE_API_KEY":     "synthetic-voyage",
		"TYPESAFE_API_KEY":   "synthetic-typesafe",
	}
	run := func(omit string) ([]byte, error) {
		cmd := exec.Command(binary)
		cmd.Dir = dir
		// Never inherit real credentials into a test of the bundling script.
		cmd.Env = nil
		for name, value := range variables {
			if name != omit {
				cmd.Env = append(cmd.Env, name+"="+value)
			}
		}
		return cmd.CombinedOutput()
	}
	output, err = run("")
	require.NoError(t, err, "%s", output)
	generated := output
	for name, value := range variables {
		if name != "ATOMIC_RHIZOME_KEY" {
			require.NotContains(t, string(generated), value)
			require.NotContains(t, string(output), value)
		}
	}
	match := regexp.MustCompile(`var encryptedKeys = ("[^"]+")`).FindSubmatch(generated)
	require.Len(t, match, 2)
	encoded, err := strconv.Unquote(string(match[1]))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(encoded, "rhizome-internal-keys-v1:"))
	_, encoded, _ = strings.Cut(encoded, ":")
	blob, err := base64.StdEncoding.DecodeString(encoded)
	require.NoError(t, err)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	plaintext, err := gcm.Open(nil, blob[:gcm.NonceSize()], blob[gcm.NonceSize():], nil)
	require.NoError(t, err)
	var decoded map[string]string
	require.NoError(t, json.Unmarshal(plaintext, &decoded))
	require.Equal(t, map[string]string{
		"voyage": "synthetic-voyage", "typesafe": "synthetic-typesafe",
	}, decoded)
	for name := range variables {
		t.Run(name, func(t *testing.T) {
			output, err := run(name)
			require.Error(t, err)
			require.True(t, strings.Contains(string(output), name+" not set"))
			require.NotContains(t, string(output), "encryptedKeys", "missing credentials must produce no bundle")
		})
	}
}
