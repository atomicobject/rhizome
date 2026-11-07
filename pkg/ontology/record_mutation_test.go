package ontology

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit/structuralfixture"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStructuralProviderRefusesUnsupportedMutationsAndPreservesBytes(t *testing.T) {
	root := t.TempDir()
	schema := recordFixtureSchema(t, recordRowsSDL)
	provider, err := structuralfixture.New(map[string]any{"rows": []any{map[string]any{"id": "A", "name": "Original"}}})
	require.NoError(t, err)
	registry, err := noteformat.NewRegistry(provider)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, provider)
	require.NoError(t, err)
	const content = "! native source with no Markdown structure !\r\n"
	const path = "model.native"
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	ref := NodeRef{NotePath: path, Kind: NodeKindEmbedded, Fragment: "record:v1:test", TypeName: "Row"}
	operations := map[string]func(*EditSession) error{
		"scalar": func(s *EditSession) error { return s.SetScalarField(ref, "name", "Changed") },
		"unset":  func(s *EditSession) error { return s.UnsetField(ref, "name") },
		"list":   func(s *EditSession) error { return s.SetScalarListField(ref, "name", []string{"Changed"}) },
		"link":   func(s *EditSession) error { return s.SetLinkField(ref, "refs", []string{"B"}) },
		"root metadata": func(s *EditSession) error {
			return s.SetRootMetadataList(NodeRef{NotePath: path, Kind: NodeKindNote}, "tags", []string{"changed"})
		},
		"body":              func(s *EditSession) error { return s.SetNarrative(ref, 0, len(content), content, "Changed") },
		"insert body":       func(s *EditSession) error { return s.InsertNarrative(ref, "Changed") },
		"add child":         func(s *EditSession) error { return s.AddEmbeddedNode(ref, "children", "New", "body", "") },
		"delete":            func(s *EditSession) error { return s.DeleteNode(ref) },
		"reorder":           func(s *EditSession) error { return s.ReorderCollection(ref, "children", []string{"one"}) },
		"ensure identifier": func(s *EditSession) error { return s.EnsureBlockID(ref, "new-id") },
		"set identifier":    func(s *EditSession) error { return s.SetBlockID(ref, "new-id") },
		"remove identifier": func(s *EditSession) error { return s.RemoveBlockID(ref) },
		"rewrite":           func(s *EditSession) error { return s.RewriteFile(path, "Changed") },
		"transform": func(s *EditSession) error {
			return s.StageFileTransform(path, func(string) (string, error) { return "Changed", nil })
		},
	}
	for name, stage := range operations {
		t.Run(name, func(t *testing.T) {
			session := NewProviderAwareEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema, formats)
			if err := stage(session); err == nil {
				_, err = session.Preview(t.Context())
				require.Error(t, err)
			}
			actual, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			require.Equal(t, content, string(actual))
		})
	}
}
