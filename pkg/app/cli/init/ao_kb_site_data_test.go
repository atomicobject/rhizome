package init

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// siteQueries runs the starter's query module, so these tests read exactly the text the views run.
type siteQueries struct {
	Orientation, Review, Source, Artifact, Citation string
}

// nodes reads refs with selection the way the kit's readNodes does (one batch, `first` set).
func (q siteQueries) nodes(refs ...string) string {
	quoted, _ := json.Marshal(refs)
	return fmt.Sprintf(`{ nodes(refs: %s, first: %d) { items { requestedRef node { %s } error { code message } } pageInfo { truncated } } }`, quoted, len(refs), q.Citation)
}

func loadSiteQueries(t *testing.T, args map[string]any) siteQueries {
	t.Helper()
	node, err := exec.LookPath("node")
	if err == nil {
		err = requireNode22(node)
	}
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI must provide Node 22 or later for the ao-kb query module: %v", err)
		}
		t.Skipf("the ao-kb query module needs Node 22 or later: %v", err)
	}
	module, err := filepath.Abs("templates/starters/ao-kb/module/views/knowledge/lib/queries.ts")
	require.NoError(t, err)
	input, err := json.Marshal(args)
	require.NoError(t, err)
	// import() takes a URL; a raw Windows path (D:\...) reads as scheme "d:".
	script := `const q = await import((await import("node:url")).pathToFileURL(process.argv[1]).href); const a = JSON.parse(process.argv[2]) ?? {};
console.log(JSON.stringify({Orientation: q.ORIENTATION_QUERY, Review: q.REVIEW_DOCUMENTS_QUERY,
  Citation: q.CITATION_SELECTION, Source: q.sourceScopeQuery(a.source ?? ""), Artifact: q.artifactScopeQuery(a.artifact ?? "")}));`
	out, err := exec.Command(node, "--experimental-strip-types", "--input-type=module", "-e", script, module, string(input)).CombinedOutput()
	require.NoError(t, err, "%s", out)
	var queries siteQueries
	require.NoError(t, json.Unmarshal(out, &queries), "%s", out)
	return queries
}

type siteVault struct {
	t     *testing.T
	query func(string) (map[string]any, map[string]any)
}

// openSiteVault indexes root in process with the hidden curation folder included.
func openSiteVault(t *testing.T, root string) siteVault {
	t.Helper()
	vault := obsidian.VaultDefinition{Name: "site", Root: root, Includes: []string{"**/*.md", ".kb/curation/*.md"}, Links: obsidian.LinkTypeBoth}
	metadata := testkit.NoteMetadataIndexer(t)
	runtime, cleanup, err := ontologytest.Open(context.Background(), metadata, vault, &obsidian.Note{})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	require.True(t, runtime.Ready)
	exec, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	deps := ontologyquery.Deps{VaultDef: vault, NoteReader: &obsidian.Note{}, NoteFormats: formats, Store: runtime.Store}
	return siteVault{t: t, query: func(s string) (map[string]any, map[string]any) {
		t.Helper()
		prepared, errs := ontologyquery.Prepare(exec, s)
		require.Empty(t, errs, s)
		result := ontologyquery.ExecutePrepared(context.Background(), deps, runtime.Schema, exec, prepared)
		require.Empty(t, result.Errors, s)
		coverage := map[string]any{}
		if roots, ok := result.Extensions["typedRoots"]; ok {
			raw, err := json.Marshal(roots)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(raw, &coverage))
		}
		return result.Data, coverage
	}}
}

// installAOKBFixtureFrom copies an AO KB into a temporary vault and installs the starter.
func installAOKBFixtureFrom(t *testing.T, source string, extra map[string]string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.CopyFS(root, os.DirFS(source)))
	for path, content := range extra {
		writeTestFile(t, root, path, content)
	}
	env := testkit.Env(t)
	opts := RunOptions{Env: env, Dir: root, Workflow: "none", Addons: "ao-kb", Agents: "none", Hooks: "off", Search: "off", BinaryManager: "external", Stdout: io.Discard, Stderr: io.Discard}
	require.NoError(t, Run(opts))
	return root
}

func sitePaths(rows any) []string {
	var out []string
	for _, row := range aoRows(rows) {
		out = append(out, aoString(row["path"]))
	}
	sort.Strings(out)
	return out
}

func siteByPath(rows any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, row := range aoRows(rows) {
		out[aoString(row["path"])] = row
	}
	return out
}

func requireComplete(t *testing.T, coverage map[string]any, roots ...string) {
	t.Helper()
	for _, root := range roots {
		require.Equal(t, "complete", aoMap(coverage[root])["status"], root)
	}
}

func TestAOKBLegacySiteData(t *testing.T) {
	legacy := filepath.Join("..", "..", "..", "..", "testdata", "ao-kb-legacy")
	root := installAOKBFixtureFrom(t, legacy, map[string]string{
		// An explicitly empty current list is recorded support, even beside a legacy value.
		"artifacts/plans/empty-support.md": "---\nid: \"art-0099\"\nsupported_by: []\nsupportingAssertions: [\"a-0003\"]\n---\nNo recorded support.\n",
	})
	v := openSiteVault(t, root)

	t.Run("legacy spellings read when the current key is absent", func(t *testing.T) {
		data, _ := v.query(`{ assertion(first: 100) { path kind legacyKind speaker confidence } }`)
		rows := siteByPath(data["assertion"])
		require.Equal(t, "tribal-knowledge", rows["assertions/riders-check-board.md"]["kind"], "raw legacy value preserved")
		require.Equal(t, "tribal-knowledge", rows["assertions/riders-check-board.md"]["legacyKind"])
		require.Equal(t, "Dock staff", rows["assertions/riders-check-board.md"]["speaker"], "attestedBy reads as speaker")
		require.Nil(t, rows["assertions/riders-want-text-alerts.md"]["speaker"], "origin is not a role")
		conflict := rows["assertions/kind-conflict.md"]
		require.Equal(t, "decision", conflict["kind"], "kind wins when both keys are present")
		require.Equal(t, "constraint", conflict["legacyKind"])
		require.Equal(t, "high", rows["assertions/fare-is-flat.md"]["confidence"])
		require.Equal(t, "lesson", rows["assertions/pilot-lesson.md"]["kind"])
	})
	t.Run("source kind, recorded dates, and every linking assertion", func(t *testing.T) {
		data, _ := v.query(`{ source(first: 100) { path kind recordedDate linkingAssertions { path } } }`)
		rows := siteByPath(data["source"])
		require.Equal(t, "handbook", rows["sources/documents/ferry-handbook.md"]["kind"], "format reads as source kind")
		for path, date := range map[string]any{
			"sources/meetings/2026-02-10-dock-meeting.md":      "2026-02-10",
			"sources/interviews/accessibility-walkthrough.md":  "2026-04-01",
			"sources/documents/ferry-handbook.md":              "2025-11-20",
			"sources/documents/schedule-memo.md":               "20260115",
			"sources/interviews/2026-03-04-rider-interview.md": nil,
			"sources/documents/undated-notes.md":               nil,
		} {
			require.Equal(t, date, rows[path]["recordedDate"], path)
		}
		// Declared relation fields return every row; the generic neighbourhood stops at 20.
		linking := aoRows(rows["sources/documents/ferry-handbook.md"]["linkingAssertions"])
		require.Len(t, linking, 22)
		require.NotContains(t, rows, "sources/_raw/meetings/dock-meeting.txt")
	})
	t.Run("membership in both directions", func(t *testing.T) {
		data, _ := v.query(`{ concept(first: 10) { path linkedAssertions { path } linkingAssertions { path } } }`)
		rows := siteByPath(data["concept"])
		timetable := rows["concepts/timetable.md"]
		require.Equal(t, []string{"assertions/winter-schedule-thin.md"}, sitePaths(timetable["linkedAssertions"]), "a concept-side link counts")
		require.NotContains(t, sitePaths(timetable["linkingAssertions"]), "assertions/winter-schedule-thin.md")
		require.Len(t, aoRows(timetable["linkingAssertions"]), 30)
		require.Empty(t, rows["concepts/weather.md"]["linkedAssertions"])
		require.Empty(t, rows["concepts/weather.md"]["linkingAssertions"])
	})
	t.Run("support values keep the first recorded key", func(t *testing.T) {
		data, _ := v.query(`{ artifact(first: 20) { path supportValues } }`)
		rows := siteByPath(data["artifact"])
		require.Equal(t, []any{"a-0003", "a-0007"}, rows["artifacts/plans/service-plan.md"]["supportValues"])
		require.Equal(t, []any{"fare-is-flat"}, rows["artifacts/reviews/fare-review.md"]["supportValues"])
		require.Equal(t, []any{"a-0021", "a-4040"}, rows["artifacts/reviews/summary.md"]["supportValues"])
		require.Equal(t, []any{}, rows["artifacts/plans/empty-support.md"]["supportValues"])
		require.Empty(t, rows["artifacts/plans/summary.md"]["supportValues"], "absent support reads as an empty list")
		require.Contains(t, rows, "artifacts/plans/_working-notes.md", "underscore-named artifacts are typed")
	})
	t.Run("documents: maps of content, submissions and findings, curation", func(t *testing.T) {
		data, coverage := v.query(loadSiteQueries(t, nil).Review)
		requireComplete(t, coverage, "submission", "curationDocument", "conflictRegistry")
		submissions := siteByPath(data["submission"])
		require.Equal(t, []string{"_improvements/add-text-alerts.md", "_improvements/fix-ramp-note.md", "_improvements/fnd-0001-missing-dates.md",
			"_improvements/merge-fare-notes.md", "_improvements/rename-weather/rename-weather.md", "_improvements/split-timetable/split-timetable.md"}, sitePaths(data["submission"]))
		require.Equal(t, "finding", submissions["_improvements/fnd-0001-missing-dates.md"]["recordType"])
		require.Equal(t, "open", submissions["_improvements/fnd-0001-missing-dates.md"]["status"])
		require.Equal(t, "parked", submissions["_improvements/rename-weather/rename-weather.md"]["status"], "undocumented statuses stay verbatim")
		require.Equal(t, []string{".kb/curation/review-log.md", ".kb/curation/vocabulary-report.md"}, sitePaths(data["curationDocument"]))
		curation := siteByPath(data["curationDocument"])
		require.Equal(t, "2026-05-10", curation[".kb/curation/review-log.md"]["recordedUpdatedAt"])
		require.Nil(t, curation[".kb/curation/vocabulary-report.md"]["recordedUpdatedAt"])
		require.Equal(t, "2026-05-12", aoRows(data["conflictRegistry"])[0]["recordedUpdatedAt"])
		mocs, _ := v.query(`{ mapOfContent(first: 10) { path title aoId abstract } }`)
		require.Equal(t, []string{"MOCs/riders.md", "MOCs/routes.md", "_moc.md"}, sitePaths(mocs["mapOfContent"]))
		require.Equal(t, "Map of Content — Harbor Ferry", siteByPath(mocs["mapOfContent"])["_moc.md"]["title"])
	})
	t.Run("the exported orientation query reads every record completely", func(t *testing.T) {
		data, coverage := v.query(loadSiteQueries(t, nil).Orientation)
		requireComplete(t, coverage, "assertion", "source", "concept", "artifact", "mapOfContent")
		for root, count := range map[string]int{"assertion": 43, "source": 7, "concept": 4, "artifact": 6, "mapOfContent": 3} {
			require.Len(t, aoRows(data[root]), count, root)
		}
		provenance := siteByPath(data["assertion"])["assertions/provenance-only.md"]
		require.Equal(t, "s-0003", provenance["provenanceSource"], "a provenance-only citation is in the orientation query")
	})
	t.Run("scope reads", func(t *testing.T) {
		queries := loadSiteQueries(t, map[string]any{
			"source":   "sources/documents/ferry-handbook.md",
			"artifact": "artifacts/plans/service-plan.md",
		})
		data, _ := v.query(queries.nodes("assertions/riders-check-board.md", "assertions/body-linked.md"))
		items := aoRows(aoMap(data["nodes"])["items"])
		require.Len(t, items, 2)
		citations := aoRows(aoMap(aoMap(items[0]["node"])["sourcesSection"])["citations"])
		require.Equal(t, []string{"sources/interviews/2026-03-04-rider-interview.md"}, sitePaths(citations[0]["sources"]))
		data, _ = v.query(queries.Source)
		require.Len(t, aoRows(aoMap(data["node"])["linkingAssertions"]), 22)
		data, _ = v.query(queries.nodes("assertions/provenance-only.md"))
		require.Equal(t, "assertions/provenance-only.md", aoString(aoMap(aoRows(aoMap(data["nodes"])["items"])[0]["node"])["path"]))
		data, _ = v.query(queries.Artifact)
		node := aoMap(data["node"])
		require.Equal(t, []any{"a-0003", "a-0007"}, node["supportValues"])
		require.Equal(t, []string{"concepts/timetable.md"}, sitePaths(node["conceptReferences"]))
		require.Equal(t, []string{"assertions/storm-cancellations.md"}, sitePaths(node["additionalAssertionReferences"]))
	})
}

func TestAOKBSiteQueriesOnCurrentAndProductFixtures(t *testing.T) {
	queries := loadSiteQueries(t, nil)
	t.Run("booking", func(t *testing.T) {
		v := openSiteVault(t, installAOKBFixture(t))
		data, coverage := v.query(queries.Orientation)
		requireComplete(t, coverage, "assertion", "source", "concept", "artifact", "mapOfContent")
		for root, count := range map[string]int{"assertion": 22, "source": 9, "concept": 7, "artifact": 4, "mapOfContent": 0} {
			require.Len(t, aoRows(data[root]), count, root)
		}
	})
	t.Run("product admission keeps the account root and the selected product", func(t *testing.T) {
		product := filepath.Join("..", "..", "..", "..", "testdata", "ao-kb-product")
		admission, err := os.ReadFile(filepath.Join(product, "expected-knowledge.graphql"))
		require.NoError(t, err)
		root := installAOKBFixtureFrom(t, filepath.Join(product, "account"), map[string]string{
			".rhizome/modules/ao-kb/ontology/ao-knowledge.generated.graphql": string(admission),
			"products/beta/MOCs/beta-map.md":                                 "# Beta map\n",
			"products/beta/_improvements/beta-idea/beta-idea.md":             "---\nstatus: pending\n---\n# Beta idea\n",
			"products/beta/.kb/curation/beta-log.md":                         "# Beta log\n",
		})
		v := openSiteVault(t, root)
		data, coverage := v.query(queries.Orientation)
		requireComplete(t, coverage, "assertion", "source", "concept", "artifact", "mapOfContent")
		require.Equal(t, []string{"_moc.md", "products/alpha/MOCs/alpha-map.md"}, sitePaths(data["mapOfContent"]))
		require.NotContains(t, sitePaths(data["assertion"]), "assertions/beta-only.md")
		docs, _ := v.query(queries.Review)
		require.Equal(t, []string{"_improvements/shared-idea/shared-idea.md", "products/alpha/_improvements/alpha-idea/alpha-idea.md"}, sitePaths(docs["submission"]))
		require.Equal(t, []string{".kb/curation/review-log.md"}, sitePaths(docs["curationDocument"]))
	})
}

func TestAOKBSiteReadsAGenerated600AssertionKBCompletely(t *testing.T) {
	extra := map[string]string{}
	for i := 0; i < 600; i++ {
		extra[fmt.Sprintf("assertions/generated-%03d.md", i)] = fmt.Sprintf("---\nid: \"a-g%03d\"\nassertionKind: \"fact\"\nconfidence: \"validated\"\n---\nGenerated claim %d about [[bulk-topic]].\n", i, i)
	}
	extra["concepts/bulk-topic.md"] = "---\nid: \"cpt-bulk\"\n---\nA concept every generated claim links.\n"
	extra[".kb/config/vault.md"] = "---\nschemaVersion: 5\nproductMode: single\n---\n"
	root := installAOKBFixtureFrom(t, t.TempDir(), extra)
	v := openSiteVault(t, root)
	data, coverage := v.query(loadSiteQueries(t, nil).Orientation)
	requireComplete(t, coverage, "assertion", "concept")
	require.Len(t, aoRows(data["assertion"]), 600)
	require.Len(t, aoRows(aoRows(data["concept"])[0]["linkingAssertions"]), 600, "declared relations without first return every row")
}
