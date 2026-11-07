package ontology

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/internal/testkit"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestStructuredReferencesRefreshWhenMarkdownTargetsChange(t *testing.T) {
	for _, variant := range []struct{ name, target, keyArgument string }{
		{"path", "evidence/source.md", ""},
		{"projected key", "a-000000000001", `targetKey:"aoId",`},
		{"file stem", "source", `targetKey:["$fileStem","aoId"],`},
	} {
		name, target, keyArgument := variant.name, variant.target, variant.keyArgument
		keyed, stem := name == "projected key", name == "file stem"
		t.Run(name, func(t *testing.T) {
			root := testkit.Vault(t, map[string]string{
				".rhizome/ontology/schema.graphql": `type Host @node(default:true){status:String}
interface Evidence {aoId:String}
type Assertion implements Evidence @node(matches:["evidence AND kind:assertion"]){aoId:String @field(source:"id")}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model.yaml"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row"){
id:ID @field(path:"id")
evidence:[Evidence!] @field(path:"evidence") @references(to:["Evidence"],` + keyArgument + `mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`,
				"model.yaml": "rows: [{id: A, evidence: [" + target + "]}]\n",
			})
			vault := obsidian.VaultDefinition{Root: root, Includes: []string{"model.yaml", "evidence/**/*.md"}}
			indexer := testkit.NoteMetadataIndexer(t)
			db := filepath.Join(t.TempDir(), "incremental.sqlite")
			store, err := semdb.Open(db)
			require.NoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
			require.NoError(t, err)
			stamp := time.Unix(1900000000, 0)
			write := func(path, content string) {
				path = filepath.Join(root, path)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
				stamp = stamp.Add(time.Second)
				require.NoError(t, os.Chtimes(path, stamp, stamp))
			}
			note := func(key, kind string) string { return "---\nid: " + key + "\nkind: " + kind + "\n---\n# Evidence\n" }
			for _, step := range []struct {
				name, path, content    string
				remove                 bool
				wantTarget, diagnostic string
			}{
				{"added", "evidence/source.md", note("a-000000000001", "assertion"), false, "evidence/source.md", ""},
				{"key changed", "evidence/source.md", note("different", "assertion"), false, "evidence/source.md", ""},
				{"key restored", "evidence/source.md", note("a-000000000001", "assertion"), false, "evidence/source.md", ""},
				{"duplicate", "evidence/duplicate.md", note("a-000000000001", "assertion"), false, "evidence/source.md", ""},
				{"duplicate removed", "evidence/duplicate.md", "", true, "evidence/source.md", ""},
				{"same stem", "evidence/other/source.md", note("a-000000000002", "assertion"), false, "evidence/source.md", ""},
				{"same stem removed", "evidence/other/source.md", "", true, "evidence/source.md", ""},
				{"retyped", "evidence/source.md", note("a-000000000001", "other"), false, "", "record_reference_unresolved"},
				{"type restored", "evidence/source.md", note("a-000000000001", "assertion"), false, "evidence/source.md", ""},
				{"deleted", "evidence/source.md", "", true, "", "record_reference_unresolved"},
			} {
				t.Run(step.name, func(t *testing.T) {
					var changed, deleted []string
					if step.remove {
						require.NoError(t, os.Remove(filepath.Join(root, step.path)))
						deleted = []string{step.path}
					} else {
						write(step.path, step.content)
						changed = []string{step.path}
					}
					if keyed && step.name == "key changed" {
						step.wantTarget, step.diagnostic = "", "record_reference_unresolved"
					}
					if keyed && step.name == "duplicate" {
						step.wantTarget, step.diagnostic = "", "record_reference_ambiguous"
					}
					if stem && step.name == "same stem" {
						step.wantTarget, step.diagnostic = "", "record_reference_ambiguous"
					}
					_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, changed, deleted)
					require.NoError(t, err)
					assertStructuredPublicationConverges(t, indexer, vault, store)
					edges, err := store.OntologyEdgesForPaths(t.Context(), []string{"model.yaml"}, true, "", 0)
					require.NoError(t, err)
					var targets []string
					for _, edge := range edges {
						if edge.RelationName == "evidenced-by" {
							targets = append(targets, edge.DstPath)
						}
					}
					if step.wantTarget == "" {
						require.Empty(t, targets)
					} else {
						require.Equal(t, []string{step.wantTarget}, targets)
					}
					assessments, err := store.OntologyAssessmentsByPaths(t.Context(), []string{"model.yaml"})
					require.NoError(t, err)
					assessment, err := AssessmentFromJSON(assessments["model.yaml"].AssessmentJSON)
					require.NoError(t, err)
					var codes []string
					for _, diagnostic := range assessment.Issues {
						codes = append(codes, diagnostic.Code)
					}
					if step.diagnostic == "" {
						require.Empty(t, codes)
					} else {
						require.Equal(t, []string{step.diagnostic}, codes)
					}
					before := structuredSnapshot(t, store)
					require.NoError(t, store.Close())
					store, err = semdb.Open(db)
					require.NoError(t, err)
					require.Equal(t, before, structuredSnapshot(t, store))
					_, err = syncPathsForTest(t.Context(), indexer, vault, &obsidian.Note{}, store, nil, nil, nil)
					require.NoError(t, err)
					assertStructuredPublicationConverges(t, indexer, vault, store)
				})
			}
		})
	}
}
