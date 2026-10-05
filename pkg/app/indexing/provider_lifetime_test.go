package indexing

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/stretchr/testify/require"
)

func semanticDatabaseOwners() int {
	stack := make([]byte, 2<<20)
	n := runtime.Stack(stack, true)
	return strings.Count(string(stack[:n]), "database/sql.(*DB).connectionOpener(")
}

func TestUnifiedSemanticResourcesCloseCachedProviders(t *testing.T) {
	for _, failure := range []string{"", "code store", "note store", "note provider"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
			codeCfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: "http://127.0.0.1:1", IndexPath: filepath.Join(root, "code.sqlite")}
			noteCfg := codeCfg
			noteCfg.IndexPath = filepath.Join(root, "note.sqlite")
			if failure == "code store" {
				codeCfg.IndexPath = t.TempDir()
			}
			if failure == "note store" {
				noteCfg.IndexPath = t.TempDir()
			}
			if failure == "note provider" {
				noteCfg.Provider = "unsupported"
			}
			before := semanticDatabaseOwners()
			for range 3 {
				resources, err := prepareUnifiedSemanticResources(t.Context(), unifiedPostIndexOptions{VaultPath: root, APIKey: "synthetic-only"}, noopProgress{}, &sync.Mutex{}, nil, nil, nil, nil, nil, nil, nil, nil, codeanchor.Config{}, noteCfg, codeCfg)
				if failure != "" {
					require.Error(t, err)
					require.Nil(t, resources)
				} else {
					require.NoError(t, err)
					// Equal configurations share a node, while each factory-created
					// provider still owns its separate cache connection.
					require.Same(t, resources.noteNode, resources.codeNode)
					resources.close()
					resources.close()
				}
				require.Eventually(t, func() bool { return semanticDatabaseOwners() == before }, 3*time.Second, 10*time.Millisecond)
			}
		})
	}
}
