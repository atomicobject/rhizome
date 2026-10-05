package web

import (
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

func TestEditBaseDocumentsFromOpsVerifiesWorkspaceRevision(t *testing.T) {
	content := "---\ntype: Note\n---\n\nBody.\n"
	snapshot, err := ontology.BuildDocumentSnapshot("notes/a.md", content, time.Time{})
	require.NoError(t, err)
	documents, err := editBaseDocumentsFromOps([]OntologyEditOp{{
		Kind: "setField", Path: "notes/a.md", Expected: &OntologyEditExpected{
			SourceHash: snapshot.ContentFingerprint, SourceContent: content,
		},
	}})
	require.NoError(t, err)
	require.Equal(t, []ontology.EditBaseDocument{{
		NotePath: "notes/a.md", Fingerprint: snapshot.ContentFingerprint, Content: content,
	}}, documents)

	_, err = editBaseDocumentsFromOps([]OntologyEditOp{{
		Kind: "setField", Path: "notes/a.md", Expected: &OntologyEditExpected{
			SourceHash: "wrong", SourceContent: content,
		},
	}})
	require.ErrorContains(t, err, "fingerprint mismatch")
}

func TestValidateOntologyEditSnapshotBasesRequiresEveryTouchedFile(t *testing.T) {
	empty, err := ontology.BuildDocumentSnapshot("notes/empty.md", "", time.Time{})
	require.NoError(t, err)
	snapshot := &OntologyEditSessionSnapshot{
		Version: 3,
		Ops: []OntologyEditOp{
			{Kind: "setSource", Path: "notes/empty.md"},
			{Kind: "setField", Path: "notes/other.md", Field: "summary"},
		},
		BaseDocuments: []ontology.EditBaseDocument{{NotePath: "notes/empty.md", Fingerprint: empty.ContentFingerprint, Content: ""}},
	}
	err = validateOntologyEditSnapshotBases(snapshot)
	require.ErrorContains(t, err, "missing the verified base for notes/other.md")
	snapshot.Ops = snapshot.Ops[:1]
	require.NoError(t, validateOntologyEditSnapshotBases(snapshot), "empty source content is valid recovery evidence")
}

func TestValidateBoundedSourceEditProtectsGraphIdentity(t *testing.T) {
	base := "---\nid: NOTE-1\n---\n# Heading\n\nBody.\n\nSee [[Target]].\n^anchor\n"
	require.NoError(t, validateBoundedSourceEdit(base, "---\nid: NOTE-1\n---\n# Heading\n\nEdited body.\n\nSee [[Target]].\n^anchor\n"))
	require.Error(t, validateBoundedSourceEdit(base, "---\nid: NOTE-2\n---\n# Heading\n\nBody.\n\nSee [[Target]].\n^anchor\n"))
	require.Error(t, validateBoundedSourceEdit(base, "---\nid: NOTE-1\n---\n# Renamed\n\nBody.\n\nSee [[Target]].\n^anchor\n"))
	require.Error(t, validateBoundedSourceEdit(base, "---\nid: NOTE-1\n---\n# Heading\n\nBody.\n\nSee [[Other]].\n^anchor\n"))
	require.NoError(t, validateBoundedSourceEdit(base, "---\nid: NOTE-1\nsummary: Editable\n---\n# Heading\n\nBody.\n\nSee [[Target]].\n^anchor\n"))
	require.Error(t, validateBoundedSourceEdit("Heading\n=======\n\n[Target](target.md)\n", "Renamed\n=======\n\n[Target](target.md)\n"))
	require.Error(t, validateBoundedSourceEdit("Heading\n=======\n\n[Target](target.md)\n", "Heading\n=======\n\n[Target](other.md)\n"))
	require.NoError(t, validateBoundedSourceEdit("# Heading\n\n```md\n# Example\n[[Fake]]\n```\n", "# Heading\n\n```md\n# Changed example\n[[Other fake]]\n```\n"))
	require.Error(t, validateBoundedSourceEdit(
		"# Root\n\n## First\nid:: FIRST\n\n## Second\nid:: SECOND\n",
		"# Root\n\n## First\nid:: SECOND\n\n## Second\nid:: FIRST\n",
	))
	require.Error(t, validateBoundedSourceEdit(
		"# Root\n\n- First\n  ^first\n- Second\n  ^second\n",
		"# Root\n\n- First\n  ^second\n- Second\n  ^first\n",
	))
}
