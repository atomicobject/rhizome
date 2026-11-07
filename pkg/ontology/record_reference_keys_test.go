package ontology

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordReferencesUseExactProjectedKeys(t *testing.T) {
	sdl := `type Evidence @node(paths:["evidence/*.md"]){aoId:String @field(source:"id")}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:{field:"id"},title:["id"],space:"alpha",logicalKind:"row"){
 refs:[String!] @field(path:"refs") @references(to:["Evidence"],targetKey:"aoId",mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`
	for _, mode := range []string{"STRICT", "TOLERANT"} {
		t.Run(mode, func(t *testing.T) {
			schema := recordFixtureSchema(t, strings.Replace(sdl, "mode:STRICT", "mode:"+mode, 1))
			host, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "M", "refs": []any{"a-Key", "A-Key", "a-key", " a-Key ", "evidence/source.md"}}}}), schema)
			require.NoError(t, err)
			snapshot, err := BuildDocumentSnapshot("evidence/source.md", "---\nid: a-Key\n---\n# Evidence\n", time.Time{})
			require.NoError(t, err)
			evidence, err := prepareMarkdownSnapshot(snapshot, schema)
			require.NoError(t, err)
			BindRecordDocuments(host, evidence)
			refs := host.records[0].projection.Fields["refs"]
			require.Equal(t, []string{"a-Key", "A-Key", "a-key", " a-Key ", "evidence/source.md"}, refs.Values)
			require.Len(t, refs.ReferenceTargets, 1)
			require.Equal(t, "evidence/source.md", refs.ReferenceTargets[0].NotePath)
			require.Len(t, refs.ReferenceOccurrences, 5)
			for _, occurrence := range refs.ReferenceOccurrences[1:] {
				require.Nil(t, occurrence.Target)
				if mode == "STRICT" {
					require.Equal(t, "record_reference_unresolved", occurrence.Diagnostic)
				} else {
					require.Empty(t, occurrence.Diagnostic)
				}
			}
		})
	}
}

// AO cites an assertion by its file stem first and its frontmatter id second
// (kb-site trace.py at e9dce951). Neither ".md" nor a path spelling resolves,
// and a stem shared by two notes is ambiguous instead of falling through.
func TestRecordReferencesResolveOrderedTargetKeys(t *testing.T) {
	schema := recordFixtureSchema(t, `type Evidence @node(paths:["evidence/**/*.md"]){aoId:String @field(source:"id")}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:{field:"id"},title:["id"],space:"alpha",logicalKind:"row"){
 refs:[String!] @field(path:"refs") @references(to:["Evidence"],targetKey:["$fileStem","aoId"],mode:STRICT,values:ELEMENTS,edge:{name:"evidenced-by"})
}`)
	authored := []any{"claim-one", "a-1", "claim-two", "a-2", "claim-one.md", "evidence/claim-one", "evidence/claim-one.md", "Claim-One", "twin", "a-t1", "no-id"}
	host, err := PrepareDocument(recordFixtureRoot(t, "model.native", map[string]any{"rows": []any{map[string]any{"id": "M", "refs": authored}}}), schema)
	require.NoError(t, err)
	documents := []*PreparedDocument{host}
	for path, id := range map[string]string{
		"evidence/claim-one.md": "a-1",
		// This note's id spells the other note's stem; the stem wins.
		"evidence/claim-two.md": "claim-one",
		"evidence/twin.md":      "a-t2",
		"evidence/dup/twin.md":  "a-t1",
		"evidence/no-id.md":     "",
	} {
		frontmatter := ""
		if id != "" {
			frontmatter = "---\nid: " + id + "\n---\n"
		}
		snapshot, err := BuildDocumentSnapshot(path, frontmatter+"# Evidence\n", time.Time{})
		require.NoError(t, err)
		document, err := prepareMarkdownSnapshot(snapshot, schema)
		require.NoError(t, err)
		documents = append(documents, document)
	}
	BindRecordDocuments(documents...)
	refs := host.records[0].projection.Fields["refs"]
	require.Len(t, refs.ReferenceOccurrences, len(authored))
	type outcome struct{ target, diagnostic string }
	got := map[string]outcome{}
	for _, occurrence := range refs.ReferenceOccurrences {
		result := outcome{diagnostic: occurrence.Diagnostic}
		if occurrence.Target != nil {
			result.target = occurrence.Target.NotePath
		}
		got[occurrence.Authored] = result
	}
	unresolved := outcome{diagnostic: "record_reference_unresolved"}
	require.Equal(t, map[string]outcome{
		"claim-one":             {target: "evidence/claim-one.md"},
		"a-1":                   {target: "evidence/claim-one.md"},
		"claim-two":             {target: "evidence/claim-two.md"},
		"a-2":                   unresolved,
		"claim-one.md":          unresolved,
		"evidence/claim-one":    unresolved,
		"evidence/claim-one.md": unresolved,
		"Claim-One":             unresolved,
		"twin":                  {diagnostic: "record_reference_ambiguous"},
		"a-t1":                  {target: "evidence/dup/twin.md"},
		"no-id":                 {target: "evidence/no-id.md"},
	}, got)
	require.Len(t, refs.ReferenceTargets, 5)
}
