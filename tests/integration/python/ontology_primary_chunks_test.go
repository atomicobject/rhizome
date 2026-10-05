//go:build integration
// +build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"

	_ "github.com/mattn/go-sqlite3"
)

func TestPythonFixture_PrimaryOntologyChunksReplaceGeneratedCards(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Known Windows failure that CI used to swallow: TempDir cleanup cannot
		// remove .rhizome/agent/sessions.sqlite because a handle is still open.
		t.Skip("sessions.sqlite handle outlives the test on Windows; needs a product fix")
	}
	const notePath = "notes/specs/search-rewrite.md"

	ws := fixture.NewWorkspace(t)
	schema, err := ontology.LoadSchema(ws.CodeRoot)
	require.NoError(t, err)
	projection, err := ontology.ProjectNote(
		context.Background(),
		obsidian.VaultDefinition{Path: ws.CodeRoot},
		&obsidian.Note{},
		schema,
		notePath,
	)
	require.NoError(t, err)

	planned, err := semantic.BuildOntologyNodeChunks(
		schema,
		projection,
		embeddings.ProviderConfig{Provider: "test", Model: "test", Dimensions: 256},
		123,
	)
	require.NoError(t, err)

	var storyNodeID, storyChunkID, storyText, storyRefJSON string
	for _, node := range planned.Nodes {
		if node.TypeName == "UserStory" && node.NotePath == notePath {
			storyNodeID = node.NodeID
			storyRefJSON = node.NodeRefJSON
			break
		}
	}
	require.NotEmpty(t, storyNodeID)
	for _, chunk := range planned.Chunks {
		if chunk.OwnerID != storyNodeID {
			continue
		}
		require.Equal(t, semantic.GranularityOntologyNodeBody, chunk.Granularity)
		storyChunkID = chunk.ChunkID
		storyText = planned.Texts[chunk.ChunkID]
		break
	}
	require.NotEmpty(t, storyChunkID)
	require.Contains(t, storyText, "story-id:: STORY-001")
	require.Contains(t, storyText, "Ancestors: Spec > search-rewrite > stories / StoriesSection > Stories > stories")
	require.NotContains(t, storyText, "RequirementsSection")
	ancestorLine := lineWithPrefix(storyText, "Ancestors: ")
	require.NotEmpty(t, ancestorLine)
	require.LessOrEqual(t, len([]rune(strings.TrimPrefix(ancestorLine, "Ancestors: "))), 240)

	_, stderr, err := runOntologyCLI(t, ws.CodeRoot, "index")
	require.NoError(t, err, stderr)

	db, err := sql.Open("sqlite3", filepath.Join(ws.CodeRoot, ".rhizome", "db.sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var indexedChunkID, indexedGranularity, indexedRefJSON string
	var count int
	require.NoError(t, db.QueryRow(`
		SELECT c.chunk_id, c.granularity, n.node_ref_json
		FROM intel_chunks c
		JOIN ontology_nodes n ON n.node_id = c.owner_id
		WHERE n.type_name = 'UserStory'
		  AND n.note_path = ?
		  AND c.owner_id = ?`, notePath, storyNodeID,
	).Scan(&indexedChunkID, &indexedGranularity, &indexedRefJSON))
	require.Equal(t, storyChunkID, indexedChunkID)
	require.Equal(t, semantic.GranularityOntologyNodeBody, indexedGranularity)
	require.JSONEq(t, storyRefJSON, indexedRefJSON)
	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*)
		FROM intel_embeddings
		WHERE chunk_id = ?`, storyChunkID).Scan(&count))
	require.Equal(t, 1, count, "the enriched UserStory primary chunk should be embedded")

	var ref ontology.NodeRef
	require.NoError(t, json.Unmarshal([]byte(indexedRefJSON), &ref))
	require.Equal(t, notePath, ref.NotePath)
	require.Equal(t, ontology.NodeKindEmbedded, ref.Kind)
	require.Equal(t, "UserStory", ref.TypeName)
	require.Equal(t, notePath+"#^story-001", ref.NodeID)
	require.Equal(t, "^story-001", ref.Fragment)
	require.NotEmpty(t, ref.ParentID)
	require.Equal(t, notePath+"#^story-001", ref.String())

	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*)
		FROM intel_chunks
		WHERE owner_type = 'ontology_node'
		  AND granularity <> 'node_body'`).Scan(&count))
	require.Zero(t, count, "ontology nodes should have one source-owned chunk family")

	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*)
		FROM intel_chunks
		WHERE chunk_family = 'ontology_card'
		   OR granularity IN ('card', 'node_card')`).Scan(&count))
	require.Zero(t, count, "legacy generated chunks must converge away")

	require.NoError(t, db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
		  AND name IN (
			'indexed_card_specs',
			'indexed_cards',
			'indexed_card_facts',
			'indexed_card_context_targets',
			'indexed_card_context_diagnostics'
		  )`).Scan(&count))
	require.Zero(t, count, "legacy generated-card tables must not survive convergence")
}

func lineWithPrefix(text, prefix string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
