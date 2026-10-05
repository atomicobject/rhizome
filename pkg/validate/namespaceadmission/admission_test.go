package namespaceadmission

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckFencesOnlyUnsettledNativePublication(t *testing.T) {
	for _, test := range []struct {
		name, purpose, marker string
		refused               bool
	}{
		{"generic-prepared", "", "", false},
		{"native-prepared", `,"namespace":{"version":1}`, "", true},
		{"native-committed", `,"namespace":{"version":1,"settledDecision":"committed"}`, CommittedMarker, false},
		{"native-restored", `,"namespace":{"version":1,"settledDecision":"restored"}`, RestoredMarker, false},
		{"committed-no-git-unsettled", `,"namespace":{"version":1}`, CommittedMarker, true},
		{"restored-no-git-unsettled", `,"namespace":{"version":1}`, RestoredMarker, true},
		{"committed-git-pending", `,"namespace":{"version":1,"git":{"releaseSettled":false}}`, CommittedMarker, true},
		{"restored-git-settled", `,"namespace":{"version":1,"settledDecision":"restored","git":{}}`, RestoredMarker, false},
		{"settled-marker-mismatch", `,"namespace":{"version":1,"settledDecision":"restored"}`, CommittedMarker, true},
		{"unknown-purpose", `,"namespace":{"version":3}`, CommittedMarker, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			sum := sha256.Sum256([]byte("transaction"))
			dir := filepath.Join(root, ".rhizome", "repair-journal", "v1", hex.EncodeToString(sum[:]))
			require.NoError(t, os.MkdirAll(dir, 0o700))
			version := "1"
			if test.purpose != "" {
				version = "2"
			}
			manifest := `{"version":` + version + `,"transactionId":"transaction"` + test.purpose + `}`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o600))
			if test.marker != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, test.marker), []byte(strings.ToLower(test.marker)+"\n"), 0o600))
			}
			err := Check(root)
			if test.refused {
				require.Error(t, err)
				require.ErrorContains(t, err, "rzm validate fix --apply")
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []byte(manifest), read(t, filepath.Join(dir, "manifest.json")))
		})
	}
}

func TestCheckRejectsMalformedNamespaceEvidence(t *testing.T) {
	for _, change := range []string{"directory", "manifest", "version", "both-markers", "marker-type", "marker-bytes"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			sum := sha256.Sum256([]byte("transaction"))
			dir := filepath.Join(root, ".rhizome", "repair-journal", "v1", hex.EncodeToString(sum[:]))
			require.NoError(t, os.MkdirAll(dir, 0o700))
			data := `{"version":2,"transactionId":"transaction","namespace":{"version":1}}`
			if change == "version" {
				data = strings.Replace(data, `"version":2`, `"version":9`, 1)
			}
			if change == "manifest" {
				data = "{"
			}
			require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(data), 0o600))
			switch change {
			case "directory":
				require.NoError(t, os.Rename(dir, dir+"-wrong"))
			case "both-markers":
				require.NoError(t, os.WriteFile(filepath.Join(dir, CommittedMarker), []byte("committed\n"), 0o600))
				require.NoError(t, os.WriteFile(filepath.Join(dir, RestoredMarker), []byte("restored\n"), 0o600))
			case "marker-type":
				require.NoError(t, os.Mkdir(filepath.Join(dir, CommittedMarker), 0o700))
			case "marker-bytes":
				require.NoError(t, os.WriteFile(filepath.Join(dir, CommittedMarker), []byte("unproven"), 0o600))
			}
			require.Error(t, Check(root))
		})
	}
}

func TestCheckPermitsTerminalCleanupNameWithoutReplayMetadata(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".rhizome", "repair-journal", "v1", CleanupPrefix+strings.Repeat("a", 64))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, Check(root))
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
