package appbundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func portableFixture(t *testing.T) (string, Installation, string) {
	t.Helper()
	base := t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	original := portableBaseDirectory
	portableBaseDirectory = func() (string, error) { return base, nil }
	t.Cleanup(func() { portableBaseDirectory = original })
	root := filepath.Join(base, ReleaseIdentifier)
	app := filepath.Join(t.TempDir(), "Rhizome")
	require.NoError(t, os.WriteFile(app, []byte("GUI"), 0o700))
	payload := []byte("bundled executable")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	image := ImagePath(root, hash)
	require.NoError(t, os.MkdirAll(filepath.Dir(image), 0o700))
	require.NoError(t, os.WriteFile(image, payload, 0o700))
	record := Installation{V: 1, Identifier: ReleaseIdentifier, Generation: 4, AppPath: app, Version: "2.0.0", ImageHash: hash, Enabled: true}
	writePortableFixture(t, root, record)
	return root, record, image
}

func writePortableFixture(t *testing.T, root string, record Installation) {
	t.Helper()
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "installation.json"), data, 0o600))
}

func TestPortableOwnershipKeepsInvokingImageSeparateFromCurrentApp(t *testing.T) {
	root, record, image := portableFixture(t)
	older := ImagePath(root, strings.Repeat("a", 64))
	require.NoError(t, os.MkdirAll(filepath.Dir(older), 0o700))
	require.NoError(t, os.WriteFile(older, []byte("older executable"), 0o700))
	info, err := Resolve(older)
	require.NoError(t, err)
	require.True(t, info.Portable)
	require.Equal(t, "2.0.0", info.Version)
	require.Equal(t, older, info.ExecutablePath)
	require.Equal(t, record.AppPath, info.AppPath)
	require.True(t, OwnsPath(ImagePath(root, strings.Repeat("b", 64))))
	require.False(t, OwnsPath(filepath.Join(t.TempDir(), "rzm")))
	current, err := Read(root)
	require.NoError(t, err)
	require.Equal(t, image, current.ExecutablePath)
}

func TestPortableRegistrationRejectsMalformedOwnership(t *testing.T) {
	root, record, _ := portableFixture(t)
	for _, malformed := range []string{
		`{"v":2}`, `{"v":1,"unknown":true}`, `{} {}`,
		strings.Repeat(" ", (64<<10)+1),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(root, "installation.json"), []byte(malformed), 0o600))
		_, err := ReadInstallation(root)
		require.Error(t, err)
	}
	record.AppPath = "relative"
	writePortableFixture(t, root, record)
	_, err := ReadInstallation(root)
	require.Error(t, err)
}

func TestPortableInvocationFailsAfterOwnerRemovalAndHonorsDisabledRegistration(t *testing.T) {
	root, record, image := portableFixture(t)
	var errors bytes.Buffer
	handled, _ := dispatchEntry(image, []string{"search", "x"}, bytes.NewReader(nil), &bytes.Buffer{}, &errors)
	require.False(t, handled)
	record.Enabled = false
	writePortableFixture(t, root, record)
	handled, _ = dispatchEntry(image, []string{"serve", "--headless"}, bytes.NewReader(nil), &bytes.Buffer{}, &errors)
	require.False(t, handled, "removing desktop integration must not stop the still-open GUI's runtime launch")
	handled, _ = dispatchEntry(image, []string{"desktop-bridge"}, bytes.NewReader(nil), &bytes.Buffer{}, &errors)
	require.False(t, handled, "the open GUI can explicitly restore integration")
	require.NoError(t, os.Remove(record.AppPath))
	handled, code := dispatchEntry(image, []string{"desktop-bridge"}, bytes.NewReader(nil), &bytes.Buffer{}, &errors)
	require.True(t, handled)
	require.Equal(t, 1, code)
	require.Contains(t, errors.String(), "missing")
}

func TestPortableEntryPreservesForeignFiles(t *testing.T) {
	root, _, image := portableFixture(t)
	bundle, err := Read(root)
	require.NoError(t, err)
	entry := filepath.Join(t.TempDir(), executableName())
	require.NoError(t, os.WriteFile(entry, []byte("foreign"), 0o700))
	require.ErrorContains(t, InstallEntry(context.Background(), bundle, entry), "another installation")
	bytes, err := os.ReadFile(entry)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(bytes))
	require.ErrorContains(t, RemoveEntry(context.Background(), bundle, entry), "another installation")
	require.FileExists(t, image, "removing integration must not remove an executing image")
}

func TestPortableEntryRemovalRejectsANewerInstallationBeforeMutation(t *testing.T) {
	root, record, image := portableFixture(t)
	bundle, err := Read(root)
	require.NoError(t, err)
	entry := filepath.Join(t.TempDir(), executableName())
	if runtime.GOOS == "windows" {
		payload, err := os.ReadFile(image)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(entry, payload, 0o700))
	} else {
		require.NoError(t, os.Symlink(image, entry))
	}
	record.Generation++
	writePortableFixture(t, root, record)
	require.ErrorContains(t, RemoveEntryExpected(context.Background(), bundle, entry, record.Generation-1), "installation changed")
	payload, err := os.ReadFile(entry)
	require.NoError(t, err)
	require.Equal(t, "bundled executable", string(payload))
	current, err := ReadInstallation(root)
	require.NoError(t, err)
	require.Equal(t, record.Generation, current.Generation)
	require.True(t, current.Enabled)
}

func TestPortableImageIntegrityCheck(t *testing.T) {
	_, record, image := portableFixture(t)
	require.NoError(t, verifyImage(image, record.ImageHash))
	require.NoError(t, os.WriteFile(image, []byte("changed"), 0o700))
	require.ErrorContains(t, verifyImage(image, record.ImageHash), "content check")
}

func TestRegisteredAppPathsRetainDisabledOwnership(t *testing.T) {
	root, record, _ := portableFixture(t)
	record.Enabled = false
	writePortableFixture(t, root, record)
	paths, err := RegisteredAppPaths()
	require.NoError(t, err)
	require.Contains(t, paths, record.AppPath)
	if runtime.GOOS == "windows" {
		require.Contains(t, paths, filepath.Join(filepath.Dir(record.AppPath), "rzm.exe"))
	} else {
		require.NotContains(t, paths, filepath.Join(filepath.Dir(record.AppPath), "rzm"), "an AppImage has no adjacent installed sidecar")
	}
}
