package ontology

import (
	"errors"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	codeanchorsqlite "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func countOntologyTestNodesByKindAndType(nodes []readmodel.NodeRow, kind, typeName string) int {
	count := 0
	for _, node := range nodes {
		if node.NodeKind == kind && node.TypeName == typeName {
			count++
		}
	}
	return count
}

func writeOntologyTestConfig(t *testing.T, root string) {
	t.Helper()
	rhizomeDir := filepath.Join(root, ".rhizome")
	require.NoError(t, os.MkdirAll(rhizomeDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(rhizomeDir, "config.yml"), []byte("notes:\n  includes: [\"**/*.md\"]\n"), 0o644))
}

func writeOntologySchema(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".rhizome", "ontology")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.graphql"), []byte(body), 0o644))
}

func writeOntologyNote(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

func requireEdge(t *testing.T, edges []codeanchorsqlite.OntologyEdgeRow, src, relation, dst, provenance string, structural bool) {
	t.Helper()
	for _, edge := range edges {
		if edge.SrcPath == src && edge.RelationName == relation && edge.DstPath == dst && edge.Provenance == provenance && edge.Structural == structural {
			return
		}
	}
	t.Fatalf("expected edge %s --%s [%s/%t]--> %s", src, relation, provenance, structural, dst)
}

func requireIssueCode(t *testing.T, issues []ValidationIssue, code string) {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected issue code %s", code)
}

func requireIssue(t *testing.T, issues []ValidationIssue, code string, notePath string) ValidationIssue {
	t.Helper()
	for _, issue := range issues {
		if issue.Code == code && issue.NotePath == notePath {
			return issue
		}
	}
	t.Fatalf("expected issue %s for %s", code, notePath)
	return ValidationIssue{}
}

func requireNoIssueForPath(t *testing.T, issues []ValidationIssue, notePath string) {
	t.Helper()
	for _, issue := range issues {
		require.NotEqual(t, notePath, issue.NotePath, "unexpected issue for %s: %#v", notePath, issue)
	}
}

func requireNoteType(t *testing.T, rows []codeanchorsqlite.OntologyNoteTypeRow, notePath, typeName string) {
	t.Helper()
	for _, row := range rows {
		if row.NotePath == notePath && row.TypeName == typeName {
			return
		}
	}
	t.Fatalf("expected note type %s => %s", notePath, typeName)
}

func requireOntologyNode(t *testing.T, nodes []readmodel.NodeRow, typeName, title string) readmodel.NodeRow {
	t.Helper()
	for _, node := range nodes {
		if node.TypeName == typeName && node.Title == title {
			return node
		}
	}
	t.Fatalf("expected ontology node %s titled %q", typeName, title)
	return readmodel.NodeRow{}
}

func requireOntologyNodes(t *testing.T, nodes []readmodel.NodeRow, typeName string) []readmodel.NodeRow {
	t.Helper()
	out := make([]readmodel.NodeRow, 0)
	for _, node := range nodes {
		if node.TypeName == typeName {
			out = append(out, node)
		}
	}
	if len(out) == 0 {
		t.Fatalf("expected ontology nodes of type %s", typeName)
	}
	return out
}

func requireAssessment(t *testing.T, assessments []NoteAssessment, notePath, resolvedType string) NoteAssessment {
	t.Helper()
	for _, assessment := range assessments {
		if assessment.NotePath != notePath {
			continue
		}
		require.Equal(t, resolvedType, assessment.ResolvedType)
		return assessment
	}
	t.Fatalf("expected assessment for %s", notePath)
	return NoteAssessment{}
}

type failingNoteReader struct{}

func (failingNoteReader) GetContents(obsidian.VaultDefinition, string) (string, error) {
	return "", errors.New("should not read note contents")
}

func (failingNoteReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return nil, errors.New("should not list notes")
}

func (failingNoteReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Time{}, errors.New("should not stat notes")
}

func (failingNoteReader) Title(path string) (string, bool) {
	return path, true
}
