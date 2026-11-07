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
