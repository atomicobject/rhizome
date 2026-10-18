package ontology

import (
	"context"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEditConflictKindsFollowTypedCauses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		kind  ConflictKind
	}{
		{"quoted misleading words", fmt.Errorf("unsupported heading named missing node source changed not found in section"), ConflictKindUnsupportedTarget},
		{"wrapped missing node", fmt.Errorf("resolve target: %w", errMissingNode), ConflictKindMissingNode},
		{"wrapped source conflict", fmt.Errorf("transform target: %w", ErrEditSourceChanged), ConflictKindSourceChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeOntologyTestConfig(t, root)
			writeOntologyNote(t, root, "note.md", "Original\n")
			session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, nil)
			require.NoError(t, session.StageFileTransform("note.md", func(string) (string, error) { return "", tc.cause }))
			_, conflicts, err := session.PreviewCurrent(context.Background())
			require.NoError(t, err)
			require.Len(t, conflicts, 1)
			require.Equal(t, tc.kind, conflicts[0].Kind)
			require.Equal(t, "note.md", conflicts[0].NotePath)
			require.Equal(t, "note.md", conflicts[0].NodeRef)
		})
	}
}

func TestRestoredFingerprintCanonicalizesNoteIdentity(t *testing.T) {
	root := t.TempDir()
	writeOntologyTestConfig(t, root)
	writeOntologySchema(t, root, `type Spec @node(paths: ["notes/*.md"]) { summary: String status: String }`)
	const before = "---\nsummary: Before\nstatus: draft\n---\nBody\n"
	const current = "---\nsummary: External\nstatus: draft\n---\nBody\n"
	writeOntologyNote(t, root, "notes/spec.md", current)
	schema, err := ontoschema.LoadSchema(root)
	require.NoError(t, err)
	session := NewEditSession(obsidian.VaultDefinition{Path: root}, &obsidian.Note{}, schema)
	require.NoError(t, session.RestoreBaseFingerprints(context.Background(), map[string]string{"./notes/spec.md": hashText(before)}))
	require.NoError(t, session.SetScalarField(NodeRef{NotePath: "notes/spec.md", Kind: NodeKindNote}, "status", "done"))
	plan, conflicts, err := session.PreviewCurrent(context.Background())
	require.NoError(t, err)
	require.Empty(t, conflicts)
	require.Len(t, plan.Files, 1)
	require.True(t, plan.Files[0].Rebased)
	require.Equal(t, hashText(before), plan.Files[0].BaseFingerprint)
	require.Equal(t, "---\nsummary: External\nstatus: done\n---\nBody\n", plan.Files[0].UpdatedContentPreview)
}
