package ontology

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/html"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/noteformat/structured"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type countedStructuredProvider struct {
	noteformat.Projector
	version string
	calls   *sync.Map
}

func (p countedStructuredProvider) Descriptor() noteformat.Descriptor {
	d := p.Projector.Descriptor()
	if p.version != "" {
		d.ProviderVersion += "-" + p.version
		d.ProjectionVersion += "-" + p.version
	}
	return d
}
func (p countedStructuredProvider) Project(source noteformat.AuthoredSource) (noteformat.Projection, error) {
	v, _ := p.calls.LoadOrStore(source.Path().String(), new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
	projection, err := p.Projector.Project(source)
	d := p.Descriptor()
	projection.ProviderVersion, projection.ProjectionVersion = d.ProviderVersion, d.ProjectionVersion
	return projection, err
}
func structuredIndexer(t *testing.T, version string) (notemeta.Indexer, *sync.Map) {
	t.Helper()
	calls := &sync.Map{}
	yaml := countedStructuredProvider{structured.NewYAML(), version, calls}
	json := countedStructuredProvider{structured.NewJSON(), "", calls}
	registry, err := noteformat.NewRegistry(markdown.New(), html.New(), yaml, json)
	require.NoError(t, err)
	formats, err := noteformat.NewRuntime(registry, markdown.New(), html.New(), yaml, json)
	require.NoError(t, err)
	indexer, err := notemeta.NewIndexer(formats)
	require.NoError(t, err)
	return indexer, calls
}

// Compare the durable user-visible records, values, edges and diagnostics;
// database row IDs and publication times are intentionally not identities.
func structuredSnapshot(t *testing.T, store *semdb.Store) []string {
	t.Helper()
	ctx := t.Context()
	metadata, err := store.CurrentNoteMetadataRows(ctx)
	require.NoError(t, err)
	var paths, nodeIDs, out []string
	add := func(kind string, value any) {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		out = append(out, kind+":"+string(data))
	}
	for _, row := range metadata {
		paths = append(paths, row.Path)
		add("source", []string{row.Path, row.FormatID, row.ContentHash, string(row.Projection.Status), row.Projection.ProviderVersion, row.Projection.ProjectionVersion, row.Projection.DiagnosticCode, row.Projection.DiagnosticDetail})
	}
	nodes, err := store.OntologyNodesByPaths(ctx, paths)
	require.NoError(t, err)
	for _, row := range nodes {
		nodeIDs = append(nodeIDs, row.NodeID)
		row.UpdatedAt = 0
		add("node", row)
	}
	assessments, err := store.OntologyAssessmentsByPaths(ctx, paths)
	require.NoError(t, err)
	for _, row := range assessments {
		row.UpdatedAt = 0
		add("assessment", row)
	}
	fields, err := store.OntologyNodeFieldValuesByNodeIDs(ctx, nodeIDs, nil)
	require.NoError(t, err)
	for _, row := range fields {
		row.UpdatedAt = 0
		add("field", row)
	}
	edges, err := store.OntologyEdgesForPaths(ctx, paths, true, "", 0)
	require.NoError(t, err)
	for _, row := range edges {
		row.UpdatedAt = 0
		add("edge", row)
	}
	facts, err := store.DurableNoteFactsByPaths(ctx, paths)
	require.NoError(t, err)
	for _, row := range facts.ProjectionDiagnostics {
		row.NoteID = 0
		add("diagnostic", row)
	}
	// Record lexical rows follow their nodes, with no orphaned FTS rows.
	rows, err := store.DB().QueryContext(ctx, `SELECT r.item_id, f.path, f.title, f.body FROM intel_fts_rowid r JOIN intel_fts f ON f.rowid = r.fts_rowid WHERE r.item_type = 'ontology_record'`)
	require.NoError(t, err)
	for rows.Next() {
		var row [4]string
		require.NoError(t, rows.Scan(&row[0], &row[1], &row[2], &row[3]))
		add("record_fts", row)
	}
	require.NoError(t, rows.Close())
	var ftsRows int
	require.NoError(t, store.DB().QueryRowContext(ctx, `SELECT count(*) FROM intel_fts WHERE item_type = 'ontology_record'`).Scan(&ftsRows))
	add("record_fts_rows", ftsRows)
	sort.Strings(out)
	return out
}

func assertStructuredPublicationConverges(t *testing.T, indexer notemeta.Indexer, vault obsidian.VaultDefinition, store *semdb.Store) {
	t.Helper()
	full, err := semdb.Open(filepath.Join(t.TempDir(), "full.sqlite"))
	require.NoError(t, err)
	defer full.Close()
	_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, full, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, structuredSnapshot(t, full), structuredSnapshot(t, store))
}

func TestStructuredFullAndIncrementalPublicationConverge(t *testing.T) {
	sdl := strings.ReplaceAll(recordRowsSDL, "*.native", "model/*.{yaml,yml,json}") + `
type Position @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model/*.yaml"],select:"positions",key:null,title:["$value"],scalarAs:"id",space:"test",logicalKind:"position"){id:String @field(path:"id")}`
	sdl = strings.Replace(sdl, `to: ["Row"]`, `to: ["Row", "Evidence"], targetKey: "aoId"`, 1) + `
type Evidence @node(matches:["evidence AND kind:evidence"]){aoId:String @field(source:"id")}`
	sdl = strings.Replace(sdl, `refs: [Row!]`, `refs: [String!]`, 1)
	initial := `{"rows":[{"id":"A","name":"First","refs":["B","B","missing"]}],"positions":["one","two"]}`
	root := testkit.Vault(t, map[string]string{".rhizome/ontology/schema.graphql": sdl, "model/a.yaml": initial, "model/b.json": `{"rows":[{"id":"B","name":"Second","refs":[]}]}`})
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"model/*.{yaml,yml}", "model/*.json", "evidence/*.md"}}
	db := filepath.Join(root, ".rhizome/db.sqlite")
	store, err := semdb.Open(db)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	indexer, calls := structuredIndexer(t, "")
	_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	for _, path := range []string{"model/a.yaml", "model/b.json"} {
		value, ok := calls.Load(path)
		require.True(t, ok)
		require.EqualValues(t, 1, value.(*atomic.Int64).Load(), "one parse per indexed host")
	}
	assertFull := func(t *testing.T) { assertStructuredPublicationConverges(t, indexer, vault, store) }
	assertFull(t)
	stamp := time.Unix(1900000000, 0)
	write := func(path, text string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(text), 0o644))
		stamp = stamp.Add(time.Second)
		require.NoError(t, os.Chtimes(filepath.Join(root, path), stamp, stamp))
	}
	current := initial
	for _, step := range []struct {
		name  string
		apply func() ([]string, []string)
	}{
		{"modify", func() ([]string, []string) {
			current = strings.Replace(current, "First", "Changed", 1)
			write("model/a.yaml", current)
			return []string{"model/a.yaml"}, nil
		}},
		{"add", func() ([]string, []string) {
			write("model/c.yml", `{"rows":[{"id":"C","name":"Added","refs":["A"]}]}`)
			return []string{"model/c.yml"}, nil
		}},
		{"keyed reorder", func() ([]string, []string) {
			current = `{"rows":[{"id":"D","name":"Fourth"},{"id":"A","name":"Changed","refs":["B","B","missing"]}],"positions":["one","two"]}`
			write("model/a.yaml", current)
			return []string{"model/a.yaml"}, nil
		}},
		{"positional reorder", func() ([]string, []string) {
			current = strings.Replace(current, `["one","two"]`, `["two","one"]`, 1)
			write("model/a.yaml", current)
			return []string{"model/a.yaml"}, nil
		}},
		{"rename", func() ([]string, []string) {
			require.NoError(t, os.Rename(filepath.Join(root, "model/b.json"), filepath.Join(root, "model/renamed.json")))
			return []string{"model/renamed.json"}, []string{"model/b.json"}
		}},
		{"invalid YAML", func() ([]string, []string) { write("model/a.yaml", "rows: ["); return []string{"model/a.yaml"}, nil }},
		{"YAML recovery", func() ([]string, []string) { write("model/a.yaml", current); return []string{"model/a.yaml"}, nil }},
		{"delete", func() ([]string, []string) {
			require.NoError(t, os.Remove(filepath.Join(root, "model/renamed.json")))
			return nil, []string{"model/renamed.json"}
		}},
		{"invalid JSON", func() ([]string, []string) {
			write("model/renamed.json", `{"rows": [}`)
			return []string{"model/renamed.json"}, nil
		}},
		{"JSON recovery", func() ([]string, []string) {
			write("model/renamed.json", `{"rows":[{"id":"B","name":"Recovered","refs":[]}]}`)
			return []string{"model/renamed.json"}, nil
		}},
		{"Markdown target added", func() ([]string, []string) {
			write("evidence/source.md", "---\nid: missing\nkind: evidence\n---\n# Evidence\n")
			return []string{"evidence/source.md"}, nil
		}},
		{"Markdown target key changed", func() ([]string, []string) {
			write("evidence/source.md", "---\nid: other\nkind: evidence\n---\n# Evidence\n")
			return []string{"evidence/source.md"}, nil
		}},
		{"Markdown target retyped", func() ([]string, []string) {
			write("evidence/source.md", "---\nid: missing\nkind: other\n---\n# Evidence\n")
			return []string{"evidence/source.md"}, nil
		}},
		{"Markdown target deleted", func() ([]string, []string) {
			require.NoError(t, os.Remove(filepath.Join(root, "evidence/source.md")))
			return nil, []string{"evidence/source.md"}
		}},
		{"Markdown target restored", func() ([]string, []string) {
			write("evidence/source.md", "---\nid: missing\nkind: evidence\n---\n# Evidence\n")
			return []string{"evidence/source.md"}, nil
		}},
		{"binding refresh", func() ([]string, []string) {
			write(".rhizome/ontology/schema.graphql", strings.Replace(sdl, `title: ["name", "$key"]`, `title: ["id"]`, 1))
			return nil, nil
		}},
		{"provider refresh", func() ([]string, []string) { indexer, _ = structuredIndexer(t, "test-v2"); return nil, nil }},
	} {
		t.Run(step.name, func(t *testing.T) {
			changed, deleted := step.apply()
			_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, changed, deleted)
			require.NoError(t, err)
			assertFull(t)
		})
	}
	before := structuredSnapshot(t, store)
	require.NoError(t, store.Close())
	store, err = semdb.Open(db)
	require.NoError(t, err)
	require.Equal(t, before, structuredSnapshot(t, store), "restart retains the complete materialization")
	_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
	require.NoError(t, err)
	assertFull(t)
}
