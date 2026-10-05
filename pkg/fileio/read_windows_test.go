package fileio

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestReadPathPreservesLongPaths(t *testing.T) {
	long := strings.Repeat(`nested\`, 40) + "config.yml"
	for _, tc := range []struct{ input, want string }{
		{`C:\vault\runtime.json`, `C:\vault\runtime.json`},
		{`C:\` + long, `\\?\C:\` + long},
		{`\\server\share\` + long, `\\?\UNC\server\share\` + long},
		{`\\?\C:\` + long, `\\?\C:\` + long},
		{`\??\C:\` + long, `\??\C:\` + long},
		{`\\.\C:\` + long, `\\.\C:\` + long},
	} {
		require.Equal(t, tc.want, readPath(tc.input))
	}
	full, err := windows.FullPath(long)
	require.NoError(t, err)
	require.Equal(t, `\\?\`+full, readPath(long))
}
