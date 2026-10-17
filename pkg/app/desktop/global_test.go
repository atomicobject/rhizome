package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBundledGlobalExecutableUsesInvokingBundleUnlessOverridden(t *testing.T) {
	s := testService(t)
	original := desktopExecutable
	t.Cleanup(func() { desktopExecutable = original })
	bundle := filepath.Join(t.TempDir(), "Rhizome.app")
	target := workingScript(t, filepath.Join(bundle, "Contents", "MacOS", "rzm"))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>com.atomicobject.rhizome</string><key>CFBundleShortVersionString</key><string>1.2.3</string></dict></plist>`), 0o644))
	link := filepath.Join(t.TempDir(), "rzm")
	require.NoError(t, os.Symlink(target, link))
	desktopExecutable = func() (string, error) { return link, nil }
	chosen, err := s.globalExecutable("")
	require.NoError(t, err)
	resolved, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	require.Equal(t, resolved, chosen)
	status, err := s.GlobalStatus(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, GlobalInfo{Path: resolved, Version: "v1.2.3", Bundled: true}, status)
	override := workingScript(t, filepath.Join(t.TempDir(), "override-rzm"))
	chosen, err = s.globalExecutable(override)
	require.NoError(t, err)
	require.Equal(t, override, chosen)
	status, err = s.GlobalStatus(context.Background(), override)
	require.NoError(t, err)
	require.False(t, status.Bundled)
}
