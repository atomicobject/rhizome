package noteownership

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestCompileSelector_UsesFullDiscoveryOwnershipRules(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src", "Note.MD"), []byte("# note"), 0o600))
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	selector, err := CompileSelector(SelectorInput{
		VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**", "trusted/*.html", "src/**"}, Excludes: []string{"notes/private/**"}},
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			if ref.Rel.String() == "src/main.go" || ref.Rel.String() == "src/Note.MD" {
				return "go"
			}
			return ""
		},
	})
	require.NoError(t, err)

	tests := map[string]struct {
		path     paths.RelPath
		owner    notediscovery.Owner
		provider string
		code     bool
	}{
		"mixed-case markdown": {path: "notes/Decision.MD", owner: notediscovery.Note, provider: "markdown"},
		"explicit HTML trust": {path: "trusted/Release.HTML", owner: notediscovery.Note, provider: "html"},
		"excluded path":       {path: "notes/private/secret.md", owner: notediscovery.Ignored},
		"untrusted HTML":      {path: "other/Release.HTML", owner: notediscovery.Unowned},
		"code candidate":      {path: "src/main.go", owner: notediscovery.Code, code: true},
		"note wins code":      {path: "src/Note.MD", owner: notediscovery.Note, provider: "markdown", code: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			selection, err := selector.Select(test.path)
			require.NoError(t, err)
			require.True(t, selection.Eligible)
			require.Equal(t, test.owner, selection.Owner)
			require.Equal(t, test.provider, string(selection.Provider))
			require.Equal(t, test.code, selection.CodeCandidate)
			if selection.Owner != notediscovery.Code {
				require.Empty(t, selection.Language)
			}
		})
	}
	_, err = selector.Select("./notes/Decision.MD")
	require.Error(t, err)
}

func TestSelector_HiddenAndDefaultDirectoriesAreIneligible(t *testing.T) {
	t.Parallel()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	selector, err := CompileSelector(SelectorInput{VaultDefinition: obsidian.VaultDefinition{Path: t.TempDir()}, Registry: runtime.Registry()})
	require.NoError(t, err)
	for _, path := range []paths.RelPath{".hidden.md", "notes/.private.md", "node_modules/pkg/note.md"} {
		selection, err := selector.Select(path)
		require.NoError(t, err)
		require.False(t, selection.Eligible)
		require.NotEqual(t, notediscovery.Note, selection.Owner)
	}
}

func TestCompileSelector_CollectionAndConcurrentSelectionAreStable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	selector, err := CompileSelector(SelectorInput{VaultDefinition: obsidian.VaultDefinition{Root: root}, Registry: runtime.Registry()})
	require.NoError(t, err)

	const workers = 20
	var group sync.WaitGroup
	errors := make(chan error, workers)
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			selection, err := selector.Select("Collection/Note.MD")
			if err != nil {
				errors <- err
				return
			}
			if selection.Owner != notediscovery.Note || selection.Provider != MarkdownFormatID || !selection.Eligible {
				errors <- fmt.Errorf("unexpected selection: %#v", selection)
			}
		}()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
}

func TestCompileSelector_ClassicVaultSelectsMarkdown(t *testing.T) {
	t.Parallel()
	runtime, err := builtin.NewRuntime()
	require.NoError(t, err)
	selector, err := CompileSelector(SelectorInput{VaultDefinition: obsidian.VaultDefinition{Path: t.TempDir()}, Registry: runtime.Registry()})
	require.NoError(t, err)
	selection, err := selector.Select("Notes/Decision.MD")
	require.NoError(t, err)
	require.Equal(t, notediscovery.Note, selection.Owner)
	require.Equal(t, MarkdownFormatID, selection.Provider)
}
