package modulelifecycle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFingerprintTracksMissingAddedAndChangedInputs(t *testing.T) {
	root := t.TempDir()
	d := Descriptor{Module: "demo", Inputs: []string{"input"}}
	first, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("one"), 0644))
	second, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NotEqual(t, first, second)
	require.NoError(t, os.WriteFile(filepath.Join(root, "input"), []byte("two"), 0644))
	third, err := Fingerprint(t.Context(), root, d)
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
	_, err := Fingerprint(t.Context(), root, Descriptor{Module: "demo", Inputs: []string{"linked/input"}})
	require.Error(t, err)
}

func TestExcludedEvidenceDoesNotInvalidateReadiness(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sources/_raw"), 0755))
	file := filepath.Join(root, "sources/_raw/recording.bin")
	require.NoError(t, os.WriteFile(file, []byte("old"), 0644))
	d := Descriptor{Module: "demo", Inputs: []string{"sources"}, Excludes: []string{"sources/_raw"}}
	first, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(file, []byte("new"), 0644))
	second, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.Equal(t, first, second)
}

func TestFingerprintTracksEmptyDirectoryChanges(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(root, "artifacts")
	require.NoError(t, os.Mkdir(artifacts, 0755))
	d := Descriptor{Module: "demo", Inputs: []string{"artifacts"}}
	baseline, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(artifacts, "discovery"), 0755))
	added, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NotEqual(t, baseline, added)
	require.NoError(t, os.Rename(filepath.Join(artifacts, "discovery"), filepath.Join(artifacts, "delivery")))
	renamed, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.NotEqual(t, added, renamed)
	require.NoError(t, os.Remove(filepath.Join(artifacts, "delivery")))
	removed, err := Fingerprint(t.Context(), root, d)
	require.NoError(t, err)
	require.Equal(t, baseline, removed)
}

func TestFingerprintBoundsEmptyDirectoryVisits(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input")
	require.NoError(t, os.Mkdir(input, 0755))
	for i := range 700 {
		require.NoError(t, os.Mkdir(filepath.Join(input, fmt.Sprintf("phase-%d", i)), 0755))
	}
	var inputs []string
	for range 32 {
		inputs = append(inputs, "input")
	}
	_, err := Fingerprint(t.Context(), root, Descriptor{Module: "demo", Inputs: inputs})
	require.ErrorContains(t, err, "20000 paths")
}

func TestFingerprintHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Fingerprint(ctx, t.TempDir(), Descriptor{Module: "demo", Inputs: []string{"input"}})
	require.ErrorIs(t, err, context.Canceled)
}
