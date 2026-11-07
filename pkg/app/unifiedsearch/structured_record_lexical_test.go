package unifiedsearch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/indexing"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

const lexicalRecordSchema = `type Host @node(default:true){status:String}
type Service @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model/*.yaml"],select:"services",key:{field:"id"},title:["name"],space:"test",logicalKind:"service"){ id:ID @field(path:"id") name:String @field(path:"name") owner:String @field(path:"owner") }
`

// Without an embedding provider, YAML records must still be found by their
// title, bound field values, and unbound source text, each as its own record.
func TestLexicalSearchFindsStructuredRecordsWithoutEmbeddings(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		".rhizome/ontology/schema.graphql": lexicalRecordSchema,
		"model/services.yaml":              "services:\n  - id: alpha\n    name: Quokka ledger\n    owner: marmalade\n    notes: unbound zephyr detail\n  - id: beta\n    name: Quokka ledger mirror\n    owner: other\n",
		"notes/guide.md":                   "# Guide\n\nThe quokka ledger is documented here.\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o600))
	}
	vault := obsidian.VaultDefinition{Root: root, Includes: []string{"notes/*.md", "model/*.yaml"}}
	require.NoError(t, repoconfig.SaveLocalConfig(root, repoconfig.LocalConfig{
		Notes:          repoconfig.LocalVaultConfig{Includes: vault.Includes},
		NoteEmbeddings: &embeddings.Config{Enabled: false},
		CodeEmbeddings: &embeddings.Config{Enabled: false},
	}))
	metadata := testkit.NoteMetadataIndexer(t)
	lease, acquired, err := indexlock.TryAcquireLease(root, indexlock.AcquireOptions{Role: "test/unified-index"})
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, indexing.RunUnifiedCore(context.Background(), lease, indexing.UnifiedOptions{VaultPath: root, VaultDef: vault, NoteMetadata: metadata}))
	require.NoError(t, lease.Release())
	store, cleanup, err := indexstore.Open(root, indexstore.Options{Access: indexstore.ReadOnly, RequireEnabled: false})
	require.NoError(t, err)
	t.Cleanup(cleanup)
	formats, err := metadata.FormatRuntime()
	require.NoError(t, err)

	searchFor := func(query string) []search.RankedResult {
		t.Helper()
		result, err := Execute(context.Background(), ApplicationOptions{Profile: ProfileInteractive, VisibleLimit: 10, Runtime: Options{
			QueryInputs: []QueryInput{{Text: query}}, UseVector: true, UseIntel: true, UseGraph: true, UseRefs: true,
			VaultPath: root, VaultDef: vault, NoteFormats: formats, IntelStore: store,
		}})
		require.NoError(t, err)
		out := make([]search.RankedResult, 0, len(result.Sources))
		for _, source := range result.Sources {
			out = append(out, source.Result)
		}
		return out
	}
	recordTitles := func(results []search.RankedResult) []string {
		var titles []string
		for _, result := range results {
			if result.NodeRef != nil && strings.HasPrefix(result.NodeRef.Fragment, "record:") {
				require.Equal(t, "model/services.yaml", result.Path)
				require.Equal(t, ontology.NodeKindEmbedded, result.NodeRef.Kind)
				require.NotEmpty(t, result.NodeID)
				titles = append(titles, result.Title)
			}
		}
		return titles
	}

	both := searchFor("quokka ledger")
	require.ElementsMatch(t, []string{"Quokka ledger", "Quokka ledger mirror"}, recordTitles(both), "each record of one host is its own result")
	var paths []string
	for _, result := range both {
		paths = append(paths, result.Path)
	}
	require.Contains(t, paths, "notes/guide.md")

	require.Equal(t, []string{"Quokka ledger"}, recordTitles(searchFor("marmalade")), "a bound field value finds its record")
	require.Equal(t, []string{"Quokka ledger"}, recordTitles(searchFor("zephyr")), "unbound source text finds its record")
}
