package ontology

import (
	"context"
	"encoding/json"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNodeLinkServiceApplyRecoversCommittedEditAfterLaterSourceChange(t *testing.T) {
	f := writeNodeLinkLegacyRecoveryFixture(t)
	later := "later authored note\n"
	require.NoError(t, os.WriteFile(f.note, []byte(later), 0o640))
	writeOntologyTestConfig(t, f.root)
	writeOntologySchema(t, f.root, `
type UserStory implements Section @node(locator: EMBEDDED) {
  id: ID! @field @identifier(preferred: true, derivedSuffix: "US")
  status: String @field
}
type UserStoriesSection implements Section {
  stories: [UserStory!] @contains(level: H3)
}
type Spec @node(paths: ["specs/*/spec.md"]) {
  summary: String!
  userStoriesSection: UserStoriesSection @contains(level: H2, heading: "User Stories")
}
`)
	writeOntologyNote(t, f.root, "specs/001-test/spec.md", `---
type: Spec
summary: Test
---
# Spec
## User Stories
### Story A
id:: SPEC-0023.US1
status:: TODO
`)
	schema, err := ontoschema.LoadSchema(f.root)
	require.NoError(t, err)
	ref := firstStoryRef(t, f.root, schema)
	service := &NodeLinkService{VaultDef: obsidian.VaultDefinition{Path: f.root}, NoteReader: &obsidian.Note{}, Schema: schema}
	result, err := service.LinkTargets(context.Background(), LinkTargetRequest{Refs: []NodeRef{ref}, Ensure: EnsureLinkTargetApply})
	require.NoError(t, err)
	require.True(t, result.Applied)
	target := result.Targets[ref.String()]
	require.True(t, target.Exists)
	require.False(t, target.RequiresFix)
	require.Equal(t, "SPEC-0023-US1", target.BlockID)
	require.Equal(t, "[[spec#^SPEC-0023-US1]]", target.Wikilink)
	content, err := service.NoteReader.GetContents(service.VaultDef, ref.NotePath)
	require.NoError(t, err)
	require.Contains(t, content, "id:: ^SPEC-0023-US1")
	projection, err := ProjectNode(context.Background(), service.VaultDef, service.NoteReader, schema, NodeRef{
		NotePath: ref.NotePath, Fragment: "^SPEC-0023-US1", Kind: NodeKindEmbedded,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"SPEC-0023-US1"}, projection.Fields["id"].Values)
	require.Equal(t, later, mustReadEditTransactionFile(t, f.note))
	require.Equal(t, "receipt\n", mustReadEditTransactionFile(t, f.receipt))
	for _, path := range []string{f.temp, f.backup, f.journalPath} {
		require.NoFileExists(t, path)
	}
	require.NoError(t, RecoverInterruptedEdits(f.root))
}

type nodeLinkLegacyRecoveryFixture struct{ root, note, temp, backup, journalPath, receipt string }

func writeNodeLinkLegacyRecoveryFixture(t *testing.T) nodeLinkLegacyRecoveryFixture {
	t.Helper()
	root := paths.ResolveSymlinks(t.TempDir()).String()
	note := filepath.Join(root, "notes", "a.md")
	receipt := filepath.Join(root, ".rhizome", "edit-receipts", "receipt.json")
	journalPath := filepath.Join(root, ".rhizome", "edit-journal", "legacy.json")
	temp := note + ".rhizome-write-test"
	backup := filepath.Join(filepath.Dir(note), ".a.md.rhizome-backup-test")
	for _, dir := range []string{filepath.Dir(note), filepath.Dir(receipt), filepath.Dir(journalPath)} {
		require.NoError(t, os.MkdirAll(dir, 0755))
	}
	require.NoError(t, os.WriteFile(note, []byte("new\n"), 0640))
	require.NoError(t, os.WriteFile(backup, []byte("old\n"), 0640))
	require.NoError(t, os.WriteFile(receipt, []byte("receipt\n"), 0600))
	manifest := map[string]any{"version": 1, "transactionId": "legacy-test", "phase": "committed", "entries": []map[string]any{{"notePath": "notes/a.md", "tempPath": temp, "backupPath": backup, "oldFingerprint": hashText("old\n"), "newFingerprint": hashText("new\n"), "mode": 0640}}, "completion": map[string]any{"targetPath": receipt, "tempPath": "", "fingerprint": hashText("receipt\n")}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(journalPath, encoded, 0600))
	return nodeLinkLegacyRecoveryFixture{root: root, note: note, temp: temp, backup: backup, journalPath: journalPath, receipt: receipt}
}

func mustReadEditTransactionFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}
