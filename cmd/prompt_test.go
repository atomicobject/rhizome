package cmd

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type promptTestTransport struct {
	fail  bool
	calls int
}

func (transport *promptTestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	if transport.fail {
		return nil, fmt.Errorf("test provider unavailable")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"compact context"}}]}`)),
		Header:     make(http.Header),
	}, nil
}

func TestPromptOutput(t *testing.T) {
	t.Setenv("CEREBRAS_API_KEY", "test-key")
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	for _, tc := range []struct {
		name, config, warning string
		compress, empty, fail bool
		compressed            bool
	}{
		{name: "raw"},
		{name: "disabled", compress: true, config: "enabled: false", warning: "no compressor available"},
		{name: "missing key", compress: true, config: "provider: nonexistent", warning: "no compressor available"},
		{name: "provider failure", compress: true, fail: true, warning: "compression failed"},
		{name: "compressed", compress: true, compressed: true},
		{name: "empty raw", empty: true},
		{name: "empty compressed", compress: true, empty: true},
	} {
		for _, absolute := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/absolute=%t", tc.name, absolute), func(t *testing.T) {
				transport := &promptTestTransport{fail: tc.fail}
				http.DefaultTransport = transport
				vault := setupAgentTestVault(t, map[string]string{
					".rhizome/config.yml": "notes:\n  includes: [\"**/*.md\"]\ncompression:\n  enabled: true\n  " + tc.config + "\n",
					"note.md":             "# Note\n\nKeep this content.\n",
				})
				input := "find:note"
				if tc.empty {
					input = "find:absent-search-zxy"
				}
				args := []string{"prompt", "--vault", vault.name, input}
				if tc.compress {
					args = append(args, "--compress")
				}
				if absolute {
					args = append(args, "--absolute")
				}
				stdout, stderr, err := runRootCLI(t, nil, args)
				require.NoError(t, err)

				want := "<obsidian-vault name=\"testvault\">\n\n"
				switch {
				case tc.compressed:
					want += "compact context\n"
				case !tc.empty:
					path := "note.md"
					if absolute {
						path = filepath.Join(vault.path, path)
					}
					want += fmt.Sprintf("<file path=\"%s\">\n# Note\n\nKeep this content.\n\n</file>\n\n", path)
				}
				want += "</obsidian-vault>"
				require.Equal(t, want, stdout)
				if tc.name == "disabled" || tc.name == "missing key" {
					require.Equal(t, "Warning: no compressor available (check API key), falling back to normal output", stderr)
				} else if tc.warning != "" {
					require.Contains(t, stderr, tc.warning)
				} else if !tc.compressed {
					require.Empty(t, stderr)
				}
				if tc.compressed || tc.fail {
					require.Equal(t, 1, transport.calls)
				} else {
					require.Zero(t, transport.calls)
				}
			})
		}
	}
}
