package agentservices_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/internal/testkit"
	"github.com/atomicobject/rhizome/pkg/app/agentapi"
	"github.com/atomicobject/rhizome/pkg/app/agentservices"
	"github.com/atomicobject/rhizome/pkg/app/mcp"
	"github.com/atomicobject/rhizome/pkg/app/runtimeview"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	vaultconfig "github.com/atomicobject/rhizome/pkg/vault/config"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestComposedConfigReadsSelectedVaultAndHonorsSuppression(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"first vault body", "second vault body"} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			env := testkit.Env(t)
			root := testkit.Vault(t, map[string]string{
				"public.md": body,
				"hidden.md": "---\ntags: [private]\n---\nprivate body\n",
			})
			resolved, err := repoconfig.Load(root)
			require.NoError(t, err)
			definition := obsidian.VaultDefinition{Path: root}
			metadata := testkit.NoteMetadataIndexer(t)
			store, err := semdb.Open(filepath.Join(t.TempDir(), "facts.db"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })
			_, err = metadata.EnsureIndexed(t.Context(), definition, &obsidian.Note{}, store)
			require.NoError(t, err)
			config := agentservices.Compose(runtimeview.Snapshot{
				VaultDefinition: definition,
				Configuration:   &resolved,
				IntelStore:      store,
			}, agentservices.Options{
				Vault:            &repoconfig.Vault{Env: env, Name: root},
				NoteMetadata:     metadata,
				BudgetChars:      100,
				SuppressedTags:   []string{"private"},
				IntelStorePolicy: mcp.IntelStoreNoFallback,
			})
			payload, err := agentapi.CallJSON(vaultconfig.WithEnv(t.Context(), env), config, "files", map[string]any{
				"inputs": []string{"public.md", "hidden.md"}, "includeContent": true,
			})
			require.NoError(t, err)
			var response mcp.FilesResponse
			require.NoError(t, json.Unmarshal(payload, &response))
			require.Len(t, response.Files, 1)
			require.Equal(t, "public.md", response.Files[0].Path)
			require.Equal(t, body, response.Files[0].Content)
			require.NoFileExists(t, filepath.Join(root, ".rhizome", "db.sqlite"))
		})
	}
}
