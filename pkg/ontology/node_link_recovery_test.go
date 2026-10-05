package ontology

import (
	"context"
	"os"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestNodeLinkServiceApplyRecoversCommittedEditAfterLaterSourceChange(t *testing.T) {
	f := newEditRecoveryFixture(t, "committed", true)
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
	schema, err := LoadSchema(f.root)
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
	require.Equal(t, "receipt\n", mustReadEditTransactionFile(t, f.journal.Completion.TargetPath))
	for _, path := range []string{f.temp, f.backup, f.journalPath} {
		require.NoFileExists(t, path)
	}
	require.NoError(t, RecoverInterruptedEdits(f.root))
}
