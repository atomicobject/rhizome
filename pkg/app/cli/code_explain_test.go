package actions

import (
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestClassifyCodeExplainInputs(t *testing.T) {
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)

	tests := []struct {
		name       string
		vault      func(string) obsidian.VaultDefinition
		input      string
		wantKind   string
		wantFormat string
		wantError  string
	}{
		{
			name: "classic markdown note",
			vault: func(root string) obsidian.VaultDefinition {
				return obsidian.VaultDefinition{Path: root}
			},
			input:      "notes/reference.md",
			wantKind:   CodeExplainNote,
			wantFormat: "markdown",
		},
		{
			name: "configured HTML note",
			vault: func(root string) obsidian.VaultDefinition {
				return obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.html"}}
			},
			input:      "notes/reference.HTML",
			wantKind:   CodeExplainNote,
			wantFormat: "html",
		},
		{
			name: "supported code file",
			vault: func(root string) obsidian.VaultDefinition {
				return obsidian.VaultDefinition{Path: root}
			},
			input:    "src/service.py",
			wantKind: CodeExplainFile,
		},
		{
			name: "unowned file",
			vault: func(root string) obsidian.VaultDefinition {
				return obsidian.VaultDefinition{Path: root}
			},
			input:     "notes/reference.asset",
			wantError: "has no configured ownership",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, filepath.FromSlash(test.input))
			inputs, err := ClassifyCodeExplainInputs(test.vault(root), runtime, []string{input})
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			require.Len(t, inputs, 1)
			require.Equal(t, input, inputs[0].Path)
			require.Equal(t, test.wantKind, inputs[0].Kind)
			require.Equal(t, test.wantFormat, string(inputs[0].Descriptor.ID))
		})
	}
}

func TestClassifyCodeExplainInputsRejectsPathOutsideVault(t *testing.T) {
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)

	_, err = ClassifyCodeExplainInputs(
		obsidian.VaultDefinition{Path: t.TempDir()},
		runtime,
		[]string{filepath.Join(t.TempDir(), "outside.md")},
	)
	require.ErrorContains(t, err, "has no configured ownership")
}
