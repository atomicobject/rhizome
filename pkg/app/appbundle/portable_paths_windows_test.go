package appbundle

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPortableOwnershipAcceptsRustCanonicalWindowsPaths(t *testing.T) {
	root, record, image := portableFixture(t)
	extended := `\\?\` + image
	info, err := Resolve(extended)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.True(t, info.Portable)
	require.True(t, SamePath(record.AppPath, info.AppPath))
	require.True(t, OwnsPath(`\\?\`+ImagePath(root, strings.Repeat("b", 64))))
	require.False(t, OwnsPath(`\\?\`+filepath.Join(filepath.Dir(filepath.Dir(root)), "unowned-rzm.exe")))
	_, err = ReadInstallation(`\\?\` + root)
	require.NoError(t, err)
}

func TestWindowsInstallationPathSpellings(t *testing.T) {
	require.True(t, SamePath(`C:\Users\Drew\Rhizome.exe`, `\\?\c:\users\drew\Rhizome.exe`))
	require.True(t, SamePath(`\\server\share\Rhizome.exe`, `\\?\UNC\server\share\Rhizome.exe`))
	require.False(t, SamePath(`C:\Rhizome.exe`, `\\?\GLOBALROOT\C:\Rhizome.exe`))
}

func TestWindowsEntryPathPreservesForeignEntriesAndEmptyComponents(t *testing.T) {
	const directory = `C:\Rhizome\bin`
	require.Equal(t, `;C:\foreign;;`, entrySearchPathValue(`;C:\foreign;;C:\Rhizome\bin;`, directory, false))
	require.Equal(t, `;C:\foreign;`, entrySearchPathValue(`;C:\foreign;`, directory, false))
	require.Equal(t, `;C:\foreign;;C:\Rhizome\bin`, entrySearchPathValue(`;C:\foreign;`, directory, true))
	require.Equal(t, `C:\Rhizome\bin`, entrySearchPathValue("", directory, true))
	require.Equal(t, `C:\RHIZOME\bin;C:\foreign`, entrySearchPathValue(`C:\RHIZOME\bin;C:\foreign`, directory, true))
}
