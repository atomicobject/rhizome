package modulelifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFingerprintTracksMissingAddedAndChangedInputs(t *testing.T) {
	root := t.TempDir()
	d := Descriptor{Module: "demo", Inputs: []string{"input"}}
	first, err := Fingerprint(root, d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("one"), 0644))
	second, err := Fingerprint(root, d)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("two"), 0644))
	third, err := Fingerprint(root, d)
	require.NoError(t, err)
	require.NotEqual(t, second, third)
}
func TestInputsCannotEscapeThroughAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "input"), []byte("private"), 0644))
	if os.Symlink(outside, filepath.Join(root, "linked")) != nil {
		t.Skip("symlinks unavailable")
	}
	_, err := Fingerprint(root, Descriptor{Module: "demo", Inputs: []string{"linked/input"}})
	require.Error(t, err)
}

func TestExcludedEvidenceDoesNotInvalidateReadiness(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sources/_raw"), 0755))
	file := filepath.Join(root, "sources/_raw/recording.bin")
	require.NoError(t, os.WriteFile(file, []byte("old"), 0644))
	d := Descriptor{Module: "demo", Inputs: []string{"sources"}, Excludes: []string{"sources/_raw"}}
	first, err := Fingerprint(root, d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("new"), 0644))
	second, err := Fingerprint(root, d)
	require.NoError(t, err)
	require.Equal(t, first, second)
}
