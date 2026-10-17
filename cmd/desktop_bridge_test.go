package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

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
			command := newDesktopBridgeCmd()
			require.True(t, command.Hidden)
			require.True(t, isInertCommand(command))
			command.SetArgs([]string{"--state-dir", test.stateDir})
			command.SetIn(strings.NewReader(test.request))
			var output, stderr bytes.Buffer
			command.SetOut(&output)
			command.SetErr(&stderr)
			err := command.Execute()
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
