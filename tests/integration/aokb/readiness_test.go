//go:build integration

package aokb

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

func TestInstalledStarterAutomaticallyReachesReadyWithEmptyKnowledge(t *testing.T) {
	kb := &aoKB{t: t, root: filepath.Join(t.TempDir(), "vault"), home: t.TempDir()}
	kb.write(".rhizome/config.yml", "notes:\n  includes: [\"**/*.md\"]\n")
	kb.write(".kb/config/vault.md", "---\nschemaVersion: 5\nproductMode: single\n---\n")
	store, err := repositorytrust.DefaultStore()
	require.NoError(t, err)
	_, err = store.Trust(kb.root)
	require.NoError(t, err)
	t.Cleanup(func() { _, _, _ = store.Untrust(kb.root) })
	base := kb.serve()
	output, err := kb.rzm("init", "--add-addon", "ao-kb", "--agents", "none")
	require.NoError(t, err, output)
	var states []modulereadiness.Snapshot
	waitReady := func() {
		require.Eventually(t, func() bool {
			response, err := http.Get(base + "/api/v1/starter-readiness")
			if err != nil {
				return false
			}
			defer response.Body.Close()
			if json.NewDecoder(response.Body).Decode(&states) != nil {
				return false
			}
			return len(states) == 1 && states[0].State == modulereadiness.Ready
		}, 30*time.Second, 100*time.Millisecond, "readiness: %+v", states)
	}
	waitReady()
	selection := filepath.Join(kb.root, ".rhizome/modules/ao-kb/bindings/selection.json")
	data, err := os.ReadFile(selection)
	require.NoError(t, err)
	require.Contains(t, string(data), `"product": null`)
	receipt := filepath.Join(kb.root, ".rhizome/modules/ao-kb/bindings/index-receipt.json")
	info, err := os.Stat(receipt)
	require.NoError(t, err)
	fixture.StopRuntime(t, kb.root)
	base = kb.serve()
	waitReady()
	afterReopen, err := os.Stat(receipt)
	require.NoError(t, err)
	require.Equal(t, info.ModTime(), afterReopen.ModTime(), "reopen observes current queries without indexing again")
	// An unrelated file never triggers setup or rewrites the verified receipt.
	kb.write("unrelated.txt", "unchanged inputs")
	require.Never(t, func() bool { after, err := os.Stat(receipt); return err != nil || after.ModTime() != info.ModTime() }, 3*time.Second, 100*time.Millisecond)
}
