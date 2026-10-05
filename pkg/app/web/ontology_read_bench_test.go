package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

const ontologyBenchSchema = `
interface SummaryDoc {
  summary: String!
}

type Decision implements SummaryDoc
  @node(paths: ["decisions/*.md"], label: "Decision") {
  summary: String!
}

type Project implements SummaryDoc
  @node(paths: ["projects/*.md"], label: "Project") {
  summary: String!
}

type Person
  @node(paths: ["people/*.md"], label: "Person") {
  summary: String!
}

type Meeting
  @node(paths: ["meetings/*.md"], label: "Meeting") {
  summary: String!
}
`

type ontologyBenchFixture struct {
	server      *Server
	notes       int
	decisions   int
	issueNotes  int
	typedNotes  int
	interfaces  int
	decisionDir string
}

func benchNoteMetadataIndexer(tb testing.TB) notemeta.Indexer {
	tb.Helper()
	runtime, err := builtin.NewRuntime()
	require.NoError(tb, err)
	indexer, err := notemeta.NewIndexer(runtime)
	require.NoError(tb, err)
	return indexer
}

// buildOntologyBenchFixture writes a vault of noteCount notes across four note
// types (two of them implementing one interface) with a 10% issue rate on
// Decision notes, indexes it, and returns a server serving that store.
func buildOntologyBenchFixture(tb testing.TB, noteCount int) ontologyBenchFixture {
	tb.Helper()

	root := tb.TempDir()
	require.NoError(tb, os.MkdirAll(filepath.Join(root, ".rhizome", "ontology"), 0o755))
	for _, dir := range []string{"decisions", "projects", "people", "meetings"} {
		require.NoError(tb, os.MkdirAll(filepath.Join(root, dir), 0o755))
	}
	require.NoError(tb, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(ontologyBenchSchema), 0o644))

	decisions := noteCount * 4 / 10
	projects := noteCount * 2 / 10
	people := noteCount * 2 / 10
	meetings := noteCount - decisions - projects - people

	issueNotes := 0
	write := func(dir string, index int, withSummary bool) {
		body := "# Note\n\nSome common body text for search and validation.\n"
		if withSummary {
			body = fmt.Sprintf("---\nsummary: Note %d summary\n---\n%s", index, body)
		}
		require.NoError(tb, os.WriteFile(filepath.Join(root, dir, fmt.Sprintf("n%04d.md", index)), []byte(body), 0o644))
	}
	for i := 0; i < decisions; i++ {
		withSummary := i%10 != 0
		if !withSummary {
			issueNotes++
		}
		write("decisions", i, withSummary)
	}
	for i := 0; i < projects; i++ {
		write("projects", i, true)
	}
	for i := 0; i < people; i++ {
		write("people", i, true)
	}
	for i := 0; i < meetings; i++ {
		write("meetings", i, true)
	}

	store, err := semdb.Open(filepath.Join(root, ".rhizome", "db.sqlite"))
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = store.Close() })

	vaultDef := obsidian.VaultDefinition{Name: "bench", Path: root, Links: obsidian.LinkTypeBoth}
	_, err = ontology.EnsureFreshRuntimeWithStore(context.Background(), benchNoteMetadataIndexer(tb), vaultDef, &obsidian.Note{}, store)
	require.NoError(tb, err)

	srv, err := NewServer(context.Background(), Config{
		Vault:        &obsidian.Vault{Name: "bench"},
		VaultDef:     vaultDef,
		VaultPath:    root,
		Runtime:      &Runtime{IntelStore: store},
		NoteMetadata: benchNoteMetadataIndexer(tb),
	}, nil)
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = srv.Close() })

	return ontologyBenchFixture{
		server:      srv,
		notes:       noteCount,
		decisions:   decisions,
		issueNotes:  issueNotes,
		typedNotes:  noteCount,
		interfaces:  1,
		decisionDir: "decisions",
	}
}

func BenchmarkOntologyReadHandlers(b *testing.B) {
	ctx := context.Background()
	for _, noteCount := range []int{1000, 10000} {
		fixture := buildOntologyBenchFixture(b, noteCount)
		srv := fixture.server

		summary, err := srv.ontologySummary(ctx)
		require.NoError(b, err)
		require.Equal(b, fixture.notes, summary.TotalNotes)
		require.Equal(b, fixture.typedNotes, summary.TypedNotes)
		require.Equal(b, fixture.issueNotes, summary.IssueNotes)
		require.Equal(b, 0, summary.AmbiguousNotes)
		require.Len(b, summary.Types, 4)
		require.Len(b, summary.Interfaces, fixture.interfaces)

		decisionType, err := srv.ontologyType(ctx, "Decision", nil)
		require.NoError(b, err)
		require.Equal(b, fixture.decisions, decisionType.Count)
		require.Equal(b, fixture.issueNotes, decisionType.IssueCount)

		allType, err := srv.ontologyType(ctx, pseudoTypeAll, nil)
		require.NoError(b, err)
		require.Equal(b, fixture.notes, allType.Count)

		b.Run(fmt.Sprintf("notes=%d/summary", noteCount), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := srv.ontologySummary(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("notes=%d/atlas", noteCount), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := srv.ontologyAtlas(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("notes=%d/type-Decision", noteCount), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := srv.ontologyType(ctx, "Decision", nil); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("notes=%d/type-__all__", noteCount), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := srv.ontologyType(ctx, pseudoTypeAll, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
