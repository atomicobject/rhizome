package ontology

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func structuredProjection(t *testing.T, schema *ontoschema.Schema, path, content string) *NodeProjection {
	t.Helper()
	projection, err := ProjectNoteSourceSnapshot(testkit.NoteSource(t, path, content, 1), schema)
	require.NoError(t, err)
	return projection
}

func TestStructuredRecordLinksSurviveKeyedReorderAndRejectStalePositions(t *testing.T) {
	schema := recordFixtureSchema(t, `type Host @node(default:true){status:String}
type Row @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml","*.json"],select:"rows",key:{field:"id"},title:["id"],space:"test",logicalKind:"row"){id:ID @field(path:"id")}
type Pos @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml","*.json"],select:"positions",key:null,title:["$value"],scalarAs:"id",space:"test",logicalKind:"pos"){id:String @field(path:"id")}`)
	for _, path := range []string{"model file.yaml", "model file.json"} {
		t.Run(path, func(t *testing.T) {
			original := `{"rows":[{"id":"A"},{"id":"B"}],"positions":["first","second"]}`
			root := structuredProjection(t, schema, path, original)
			nodes, err := root.Prepared.Nodes()
			require.NoError(t, err)
			var keyed, positional NodeRef
			for _, node := range nodes {
				if node.Type != nil && node.Type.Name == "Row" && node.DisplayTitle() == "A" {
					keyed = node.Ref
				}
				if node.Type != nil && node.Type.Name == "Pos" && node.DisplayTitle() == "first" {
					positional = node.Ref
				}
			}
			require.NotEmpty(t, keyed.Fragment)
			require.NotEmpty(t, positional.Fragment)
			links := (&NodeLinkService{NoteReader: &obsidian.Note{}, Schema: schema, LoadPrepared: func(context.Context, string) (*PreparedDocument, error) { return root.Prepared, nil }}).WithNotePaths([]string{path})
			result, err := links.LinkTargets(t.Context(), LinkTargetRequest{Refs: []NodeRef{keyed, positional}, Ensure: EnsureLinkTargetPlan})
			require.NoError(t, err)
			require.Empty(t, result.Diagnostics)
			require.Nil(t, result.FixPlan)
			for _, ref := range []NodeRef{keyed, positional} {
				target := result.Targets[NodeSourceLocator(ref)]
				require.Empty(t, target.Wikilink)
				require.False(t, target.RequiresFix)
				require.Empty(t, target.BlockID)
				parsed, err := url.Parse(target.Markdown)
				require.NoError(t, err)
				require.Equal(t, path, parsed.Path)
				require.Equal(t, ref.Fragment, parsed.Fragment)
				require.Zero(t, ref.StartByte)
				require.Zero(t, ref.EndByte)
			}
			require.Equal(t, "durable", result.Targets[NodeSourceLocator(keyed)].Durability)
			require.Equal(t, "revision_bound", result.Targets[NodeSourceLocator(positional)].Durability)
			for _, content := range []string{
				`{"rows":[{"id":"B"},{"id":"A"}],"positions":["second","first"]}`,
				`{"rows":[{"id":"A"},{"id":"B"}],"positions":["inserted","first","second"]}`,
				original + " ",
			} {
				current := structuredProjection(t, schema, path, content)
				same, err := current.Prepared.Project(keyed)
				require.NoError(t, err)
				require.Equal(t, keyed.NodeID, same.Ref.NodeID)
				_, err = current.Prepared.Project(positional)
				require.ErrorContains(t, err, "record_locator_stale")
			}
			changed := structuredProjection(t, schema, path, strings.Replace(original, `"id":"A"`, `"id":"changed"`, 1))
			_, err = changed.Prepared.Project(keyed)
			require.ErrorContains(t, err, "record_locator_unresolved")
			duplicate := structuredProjection(t, schema, path, strings.Replace(original, `"id":"B"`, `"id":"A"`, 1))
			_, err = duplicate.Prepared.Project(keyed)
			require.ErrorContains(t, err, "record_key_ambiguous")
			require.Len(t, duplicate.Prepared.Diagnostics(), 2)
			require.Equal(t, "/rows/0", duplicate.Prepared.Diagnostics()[0].Address)
			require.Equal(t, "/rows/1", duplicate.Prepared.Diagnostics()[1].Address)
		})
	}
}

func TestOwnedStructuredSourcesRefuseBeforeParsingAndKeepExactBytes(t *testing.T) {
	root := testkit.Vault(t, map[string]string{".rhizome/source-ownership.yml": "sources:\n  - owner: Fixture author\n    reason: Use the owning tool.\n    paths: ['*.yaml', '*.json']\n"})
	schema := recordFixtureSchema(t, strings.ReplaceAll(recordRowsSDL, "*.native", "*.yaml"))
	formats, err := testkit.NoteMetadataIndexer(t).FormatRuntime()
	require.NoError(t, err)
	for _, path := range []string{"valid.yaml", "malformed.yaml", "valid.json", "malformed.json"} {
		content := "{\"rows\":[{\"id\":\"A\"}]}\r\n"
		if strings.HasPrefix(path, "malformed") {
			content = "{invalid\r\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
		ref := NodeRef{NotePath: path, Kind: NodeKindEmbedded, Fragment: "record:v1:unknown"}
		session := NewProviderAwareEditSession(obsidian.VaultDefinition{Root: root, Includes: []string{"*.yaml", "*.json"}}, &obsidian.Note{}, schema, formats)
		require.ErrorContains(t, session.SetScalarField(ref, "name", "changed"), "Use the owning tool.")
		require.ErrorContains(t, session.EnsureBlockID(ref, "new-id"), "Use the owning tool.")
		require.ErrorContains(t, session.DeleteNode(ref), "Use the owning tool.")
		called := false
		err = session.StageFileTransform(path, func(string) (string, error) { called = true; return "changed", nil })
		require.ErrorContains(t, err, "Use the owning tool.")
		require.False(t, called)
		actual, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		require.Equal(t, content, string(actual))
	}
}

// A duplicate container key invalidates its subtree in the catalog, so neither a
// saved child locator nor root navigation may reach those invalid records.
func TestInvalidContainerSubtreeIsNotReachableByLocatorOrChildren(t *testing.T) {
	schema := recordFixtureSchema(t, `type Host @node(default:true){status:String}
type Parent @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents",key:{field:"id"},title:["id"],space:"test",logicalKind:"parent"){id:ID @field(path:"id")}
type Child @node(locator:EMBEDDED) @source(shape:RECORD,paths:["*.yaml"],select:"parents[].children",key:{field:"id"},title:["id"],container:"Parent",space:"test",logicalKind:"child"){id:ID @field(path:"id")}`)
	valid := structuredProjection(t, schema, "model.yaml", "parents: [{id: P, children: [{id: C}]}]\n")
	nodes, err := valid.Prepared.Nodes()
	require.NoError(t, err)
	var saved NodeRef
	for _, node := range nodes {
		if node.Type != nil && node.Type.Name == "Child" {
			saved = node.Ref
		}
	}
	require.NotEmpty(t, saved.Fragment)

	duplicate := structuredProjection(t, schema, "model.yaml", "parents: [{id: P, children: [{id: C}]}, {id: P, children: [{id: D}]}]\n")
	_, err = duplicate.Prepared.Project(saved)
	require.ErrorContains(t, err, "record_locator_unresolved")
	_, _, err = duplicate.Prepared.SemanticParent(saved)
	require.ErrorContains(t, err, "record_locator_unresolved")

	children, err := duplicate.Prepared.Children(duplicate.Ref)
	require.NoError(t, err)
	require.Empty(t, children, "invalid parents and their children are not navigable")
}
