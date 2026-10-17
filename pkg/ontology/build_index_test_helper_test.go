package ontology

import (
	"context"
	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"testing"
)

func buildIndexFromCanonicalSourcesForTest(t testing.TB, ctx context.Context, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader, schema *ontoschema.Schema, notesHash string) (*BuildResult, error) {
	sources, err := testkit.NoteMetadataIndexer(t).BuildNoteSourceSnapshots(ctx, vaultDef, noteMgr)
	if err != nil {
		return nil, err
	}
	return BuildIndexFromNoteSources(ctx, vaultDef, sources, schema, notesHash)
}

func validationIssuesForPath(issues []ValidationIssue, notePath string) []ValidationIssue {
	var matches []ValidationIssue
	for _, issue := range issues {
		if issue.NotePath == notePath {
			matches = append(matches, issue)
		}
	}
	return matches
}
