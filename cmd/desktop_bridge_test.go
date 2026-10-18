package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/desktop"
	"github.com/stretchr/testify/require"
)

func TestDesktopBridgeCommandServesOneJSONResponse(t *testing.T) {
	for _, test := range []struct{ name, stateDir, request, code string }{
		{"inspect", t.TempDir(), `{"protocol":1,"operation":"inspect","folder":"` + filepath.ToSlash(t.TempDir()) + `"}`, ""},
		{"invalid state directory", "relative", `{}`, "invalid_request"},
		{"invalid request", t.TempDir(), `{"protocol":1,"operation":"global-install"}`, "invalid_request"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := newRootCommand(commandEnv{Config: testkit.Env(t)})
			command, _, err := app.Find([]string{"desktop-bridge"})
			require.NoError(t, err)
			require.Equal(t, "desktop-bridge", command.Name())
			require.True(t, command.Hidden)
			require.True(t, isInertCommand(command))
			app.SetArgs([]string{"desktop-bridge", "--state-dir", test.stateDir})
			app.SetIn(strings.NewReader(test.request))
			var output, stderr bytes.Buffer
			app.SetOut(&output)
			app.SetErr(&stderr)
			err = app.ExecuteContext(t.Context())
			if test.code == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			decoder := json.NewDecoder(&output)
			var response desktop.Response
			require.NoError(t, decoder.Decode(&response))
			require.Equal(t, desktop.Protocol, response.Protocol)
			if test.code == "" {
				require.Nil(t, response.Error)
				require.NotNil(t, response.Result)
			} else {
				require.Equal(t, test.code, response.Error.Code)
			}
			require.ErrorIs(t, decoder.Decode(new(any)), io.EOF)
			require.Empty(t, stderr.String())
		})
	}
}
