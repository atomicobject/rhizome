package init

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/queryrecipe"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

type aoParityEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rel  string `json:"rel"`
}
type aoParityDifference struct {
	aoParityEdge
	Reason string `json:"reason"`
}

func TestAOKBRecipesAndIndexParity(t *testing.T) {
	root := installAOKBFixture(t)
	vault := obsidian.VaultDefinition{Name: "booking", Root: root, Includes: []string{"**/*.md", ".kb/curation/_conflicts.md"}, Links: obsidian.LinkTypeBoth}
	metadata := testkit.NoteMetadataIndexer(t)
	runtime, cleanup, err := ontologytest.Open(context.Background(), metadata, vault, &obsidian.Note{})
	require.NoError(t, err)
	defer cleanup()
	require.True(t, runtime.Ready)
	executable, err := ontologyquery.BuildExecutableSchema(runtime.Schema)
	require.NoError(t, err)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)
	deps := ontologyquery.Deps{VaultDef: vault, NoteReader: &obsidian.Note{}, NoteFormats: formats, Store: runtime.Store}
	query := func(s string) map[string]any {
		t.Helper()
		prepared, errs := ontologyquery.Prepare(executable, s)
		require.Empty(t, errs)
		result := ontologyquery.ExecutePrepared(context.Background(), deps, runtime.Schema, executable, prepared)
		if strings.Contains(s, "assertion(first:100)") {
			require.Len(t, result.Errors, 1)
		} else {
			require.Empty(t, result.Errors)
		}
		for _, issue := range result.Errors {
			require.Equal(t, `field AssertionConnections.dependsOn target "[[missing-booking-rule]]" does not resolve to a note`, issue.Message)
		}
		return result.Data
	}
	t.Run("recipes execute against installed fixture", func(t *testing.T) {
		recipes, issues := queryrecipe.LoadPath(filepath.Join(root, ".rhizome/modules/ao-kb/recipes/ao-kb.yaml"))
		require.Empty(t, issues)
		require.Len(t, recipes, 5)
		paths := map[string]string{"ao-kb-claim-to-source": "assertions/clinic-patient-books-own-slot.md", "ao-kb-reverse-impact": "assertions/clinic-patient-books-own-slot.md", "ao-kb-concept-evidence": "concepts/self-service.md", "ao-kb-artifact-evidence": "artifacts/booking-prd.md", "ao-kb-model-to-evidence": "assertions/clinic-patient-books-own-slot.md"}
		for _, recipe := range recipes {
			t.Run(recipe.ID, func(t *testing.T) {
				compiled, issues := queryrecipe.Compile(recipe, executable, map[string]string{"path": paths[recipe.ID]})
				require.Empty(t, issues)
				result := ontologyquery.ExecutePrepared(context.Background(), deps, runtime.Schema, executable, compiled.Prepared)
				require.Empty(t, result.Errors)
				switch recipe.ID {
				case "ao-kb-claim-to-source":
					row := aoRows(result.Data["assertion"])[0]
					require.Equal(t, "s-000000000001", row["provenanceSource"])
					require.Equal(t, "L12-L18", row["provenanceLocator"])
					citations := aoRows(aoMap(row["sourcesSection"])["citations"])
					require.Len(t, citations, 3)
					require.Equal(t, "[[patient-booking-interview]]", citations[0]["recordedLink"])
					require.Equal(t, citations[0]["recordedLink"], citations[2]["recordedLink"])
					require.Len(t, aoMap(row["connectionsSection"])["corroboratedBy"], 2)
				case "ao-kb-concept-evidence":
					neighborhood := aoMap(aoRows(result.Data["concept"])[0]["neighborhood"])
					require.Equal(t, false, neighborhood["truncated"])
					require.ElementsMatch(t, []string{"a-000000000001", "a-000000000008"}, aoIDs(aoRows(neighborhood["nodes"])))
					for _, claim := range aoRows(neighborhood["nodes"]) {
						require.NotEmpty(t, claim["sourcesSection"])
					}
				case "ao-kb-artifact-evidence":
					row := aoRows(result.Data["artifact"])[0]
					require.Len(t, row["supportedBy"], 2)
					require.Len(t, row["additionalAssertionReferences"], 1)
					for _, claim := range aoRows(row["supportedBy"]) {
						require.NotEmpty(t, claim["sourcesSection"])
					}
				case "ao-kb-model-to-evidence":
					// This fixture installs knowledge without model bindings: the recipe still
					// compiles against the static schema and reports an empty, complete read.
					node := aoMap(result.Data["node"])
					require.Equal(t, "Assertion", node["__typename"])
					require.Equal(t, false, aoMap(node["citedBy"])["truncated"])
					require.Empty(t, aoMap(node["citedBy"])["edges"])
				case "ao-kb-reverse-impact":
					row := aoRows(result.Data["assertion"])[0]
					require.Len(t, row["supportedBy"], 2)
					neighborhood := aoMap(aoMap(result.Data["node"])["neighborhood"])
					require.Equal(t, false, neighborhood["truncated"])
					require.Len(t, neighborhood["nodes"], 4)
					for _, edge := range aoRows(neighborhood["edges"]) {
						require.Equal(t, paths[recipe.ID], aoMap(edge["source"])["notePath"])
						require.Equal(t, "INBOUND", edge["direction"])
					}
				}
			})
		}
	})

	fields := []string{"supports", "refines", "dependsOn", "contradicts", "supersedes", "resolves", "duplicate", "relatesTo", "tracesTo", "grounds", "exemplifies", "implements", "actorOf", "elementDependsOn", "supportedBy", "refinedBy", "contradictedBy", "supersededBy", "resolvedBy", "addressedBy", "blocks", "groundedBy", "corroboratedBy"}
	fmFields := []string{"frontmatterSupports", "frontmatterRefines", "frontmatterDependsOn", "frontmatterContradicts", "frontmatterSupersedes", "frontmatterResolves", "frontmatterRelatesTo", "frontmatterCorroboratedBy", "frontmatterSupportedBy", "frontmatterSupersededBy", "frontmatterResolvedBy"}
	selections := ""
	for _, field := range fields {
		selections += field + " { aoId path } "
	}
	data := query(`{ assertion(first:100) { aoId path content provenanceSource ` + strings.Join(fmFields, " ") + `
  sourcesSection { citations { recordedLink sources(first:100) { aoId path } } }
  connectionsSection { nodeId content ` + selections + ` } concepts(first:100) { aoId path } }
  source(first:100) { aoId path extractedAssertionIds }
  concept(first:100) { aoId path relatedConceptsSection { relatedConcepts(first:100) { aoId path } } }
  artifact(first:100) { aoId path supportedBy { aoId path } additionalAssertionReferences(first:100) { aoId path } conceptReferences(first:100) { aoId path } sourceReferences(first:100) { aoId path } artifactReferences(first:100) { aoId path } }
  conflictRegistry(first:5) { groups { entries { nodeId title locator { sourceLocator } assertions(first:100) { aoId } fields { label value } } } }
 }`)
	parityDir := filepath.Join("..", "..", "..", "..", "testdata", "ao-kb", "parity")
	var index struct {
		Nodes   map[string]struct{ Type string }
		Edges   []aoParityEdge
		Reverse map[string][]struct{ From, Rel string }
	}
	readJSON := func(name string, target any) {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(parityDir, name))
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, target))
	}
	readJSON("booking.kb-index.json", &index)
	nodes := map[string]string{}
	byRef := map[string]string{}
	for _, kind := range []string{"assertion", "source", "concept", "artifact"} {
		for _, row := range aoRows(data[kind]) {
			id := aoString(row["aoId"])
			path := aoString(row["path"])
			if kind == "artifact" {
				id = "art:" + strings.TrimSuffix(strings.TrimPrefix(path, "artifacts/"), ".md")
			}
			require.NotEmpty(t, id)
			require.Empty(t, nodes[id])
			nodes[id] = kind
			byRef[id] = id
			byRef[path] = id
			byRef[strings.TrimSuffix(filepath.Base(path), ".md")] = id
		}
	}
	expectedNodes := map[string]string{}
	for id, node := range index.Nodes {
		expectedNodes[id] = node.Type
	}
	require.Equal(t, expectedNodes, nodes)
	resolve := func(ref string) string {
		ref = strings.TrimPrefix(strings.TrimSuffix(ref, "]]"), "[[")
		ref = strings.Split(strings.Split(ref, "|")[0], "#")[0]
		return byRef[ref]
	}
	edges := map[aoParityEdge]bool{}
	add := func(from, to, rel string) {
		if from != "" && to != "" && from != to {
			edges[aoParityEdge{from, to, rel}] = true
		}
	}
	for _, row := range aoRows(data["assertion"]) {
		id := aoString(row["aoId"])
		for _, citation := range aoRows(aoMap(row["sourcesSection"])["citations"]) {
			// Duplicate unanchored citations retain recordedLink even when their native target is ambiguous.
			add(id, resolve(aoString(citation["recordedLink"])), "source")
		}
		add(id, resolve(aoString(row["provenanceSource"])), "source")
		for _, field := range fields {
			for _, target := range aoRows(aoMap(row["connectionsSection"])[field]) {
				from, to, rel := aoNormalize(id, aoString(target["aoId"]), field)
				add(from, to, rel)
			}
		}
		for _, field := range fmFields {
			for _, ref := range aoStrings(row[field]) {
				from, to, rel := aoNormalize(id, resolve(ref), strings.TrimPrefix(field, "frontmatter"))
				add(from, to, rel)
			}
		}
		for _, concept := range aoRows(row["concepts"]) {
			add(id, aoString(concept["aoId"]), "mentions")
		}
	}
	for _, row := range aoRows(data["source"]) {
		for _, id := range aoStrings(row["extractedAssertionIds"]) {
			add(aoString(row["aoId"]), resolve(id), "extracted")
		}
	}
	for _, row := range aoRows(data["concept"]) {
		for _, target := range aoRows(aoMap(row["relatedConceptsSection"])["relatedConcepts"]) {
			add(aoString(row["aoId"]), aoString(target["aoId"]), "related")
		}
	}
	for _, row := range aoRows(data["artifact"]) {
		for _, field := range []string{"supportedBy", "additionalAssertionReferences", "conceptReferences", "sourceReferences", "artifactReferences"} {
			for _, target := range aoRows(row[field]) {
				add(resolve(aoString(row["path"])), resolve(aoString(target["path"])), "references")
			}
		}
	}
	var differences []aoParityDifference
	readJSON("expected-differences.json", &differences)
	expectedEdges := map[aoParityEdge]bool{}
	for _, edge := range index.Edges {
		expectedEdges[edge] = true
	}
	for _, difference := range differences {
		require.NotEmpty(t, difference.Reason)
		require.True(t, expectedEdges[difference.aoParityEdge], "stale exception: %v", difference)
		require.False(t, edges[difference.aoParityEdge], "resolved exception must be removed: %v", difference)
		delete(expectedEdges, difference.aoParityEdge)
	}
	missing := []aoParityEdge{}
	extra := []aoParityEdge{}
	for edge := range expectedEdges {
		if !edges[edge] {
			missing = append(missing, edge)
		}
	}
	for edge := range edges {
		if !expectedEdges[edge] {
			extra = append(extra, edge)
		}
	}
	if len(missing)+len(extra) > 0 {
		b, _ := json.Marshal(map[string]any{"missing": missing, "extra": extra})
		t.Fatalf("parity differences: %s", b)
	}
	// Compare every AO reverse pair as well, using the same exact accepted differences.
	reverse := map[aoParityEdge]bool{}
	for to, incoming := range index.Reverse {
		for _, edge := range incoming {
			reverse[aoParityEdge{edge.From, to, edge.Rel}] = true
		}
	}
	for _, difference := range differences {
		delete(reverse, difference.aoParityEdge)
	}
	require.Equal(t, reverse, edges)
	entries := aoRows(aoRows(data["conflictRegistry"])[0]["groups"])[0]["entries"]
	require.Len(t, entries, 2)
	for _, entry := range aoRows(entries) {
		require.NotEmpty(t, entry["nodeId"])
		require.Len(t, entry["assertions"], 2)
		require.NotEmpty(t, entry["fields"])
		require.Contains(t, aoMap(entry["locator"])["sourceLocator"], ".kb/curation/_conflicts.md#")
	}
	t.Run("unknown and unresolved declarations remain inspectable", func(t *testing.T) {
		for _, row := range aoRows(data["assertion"]) {
			switch aoString(row["aoId"]) {
			case "a-000000000013":
				require.Contains(t, aoMap(row["connectionsSection"])["content"], "teleports-to:: [[clinic-patient-books-own-slot]]")
				require.Contains(t, row["content"], "depends-on:: [[missing-booking-rule]]")
			case "a-00000000000e":
				require.Contains(t, row["content"], "missing-guardian-interview")
			}
		}
	})
	t.Run("typed field focused graph and reader workspace agree", func(t *testing.T) {
		const path = "assertions/new-waitlist-after-snapshot.md"
		row := query(`{ assertion(path:"` + path + `",first:1) { connectionsSection { ` + selections + ` neighborhood(direction:OUTBOUND,first:100) { edges { source { notePath } target { notePath } relation } truncated } } } }`)
		section := aoMap(aoRows(row["assertion"])[0]["connectionsSection"])
		typed := map[aoParityEdge]bool{}
		for _, field := range fields {
			for _, target := range aoRows(section[field]) {
				from, to, rel := aoNormalize(path, aoString(target["path"]), field)
				typed[aoParityEdge{from, to, rel}] = true
			}
		}
		require.Len(t, typed, 10)
		graph := aoMap(section["neighborhood"])
		require.Equal(t, false, graph["truncated"])
		graphed := map[aoParityEdge]bool{}
		for _, edge := range aoRows(graph["edges"]) {
			from, to, rel := aoNormalize(aoString(aoMap(edge["source"])["notePath"]), aoString(aoMap(edge["target"])["notePath"]), aoString(edge["relation"]))
			graphed[aoParityEdge{from, to, rel}] = true
		}
		require.Equal(t, typed, graphed)
		reader := query(`{ node(ref:"` + path + `") { workspace { bodies(first:20) { fields { name links { resolved ref { notePath } } } } } } }`)
		read := map[aoParityEdge]bool{}
		for _, body := range aoRows(aoMap(aoMap(reader["node"])["workspace"])["bodies"]) {
			for _, field := range aoRows(body["fields"]) {
				for _, link := range aoRows(field["links"]) {
					if link["resolved"] != true {
						continue
					}
					from, to, rel := aoNormalize(path, aoString(aoMap(link["ref"])["notePath"]), aoString(field["name"]))
					read[aoParityEdge{from, to, rel}] = true
				}
			}
		}
		require.Equal(t, typed, read)

	})
}

func aoMap(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	return v.(map[string]any)
}
func aoRows(v any) []map[string]any {
	rows := []map[string]any{}
	if v != nil {
		for _, r := range v.([]any) {
			rows = append(rows, aoMap(r))
		}
	}
	return rows
}
func aoString(v any) string { s, _ := v.(string); return s }
func aoStrings(v any) []string {
	rows := []string{}
	if v != nil {
		for _, r := range v.([]any) {
			rows = append(rows, aoString(r))
		}
	}
	return rows
}
func aoIDs(rows []map[string]any) []string {
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, aoString(r["aoId"]))
	}
	sort.Strings(ids)
	return ids
}

var aoCamel = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func aoNormalize(from, to, field string) (string, string, string) {
	rel := strings.ToLower(aoCamel.ReplaceAllString(field, "${1}-${2}"))
	inverse := map[string]string{"supported-by": "supports", "refined-by": "refines", "contradicted-by": "contradicts", "superseded-by": "supersedes", "resolved-by": "resolves", "addressed-by": "resolves", "blocks": "depends-on", "grounded-by": "grounds", "corroborated-by": "grounds"}
	if canonical, ok := inverse[rel]; ok {
		return to, from, canonical
	}
	return from, to, rel
}
