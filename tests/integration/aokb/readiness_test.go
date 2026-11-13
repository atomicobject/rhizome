//go:build integration

package aokb

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
	"github.com/atomicobject/rhizome/pkg/repositorytrust"
	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstalledStarterAutomaticallyReachesReady(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprintf("populated=%t", populated), func(t *testing.T) {
			testInstalledStarterReadiness(t, populated, false)
		})
	}
}

func TestInstalledStarterAcceptsLinkedGuidance(t *testing.T) {
	testInstalledStarterReadiness(t, false, true)
}

var warmNodeOnce sync.Once

// warmNode launches Node once before the readiness wait. The starter's
// readiness actions run Node, and on a fresh Windows runner the first
// node.exe launch spends 4 to 27 seconds starting (Defender on or off) while
// later launches take about 20 ms. Without this, that one-time runner cost
// lands inside the first readiness wait.
func warmNode(t *testing.T) {
	t.Helper()
	warmNodeOnce.Do(func() {
		output, err := exec.Command("node", "--input-type=module", "-e", "import 'node:fs/promises'").CombinedOutput()
		require.NoError(t, err, "the AO KB readiness actions need node on PATH: %s", output)
	})
}

func testInstalledStarterReadiness(t *testing.T, populated, linkedGuidance bool) {
	kb := &aoKB{t: t, root: filepath.Join(t.TempDir(), "vault"), home: t.TempDir()}
	if populated {
		require.NoError(t, os.CopyFS(kb.root, os.DirFS(filepath.Join(fixture.RepoRoot(t), "testdata/ao-kb-legacy"))))
		for i := range 50 {
			kb.write(fmt.Sprintf("assertions/additional-%d.md", i), "# Additional assertion\n")
		}
	}
	kb.write(".rhizome/config.yml", "rhizome:\n  binaryManager: external\nnotes:\n  includes: [\"**/*.md\"]\n")
	kb.write(".kb/config/vault.md", "---\nschemaVersion: 5\nproductMode: single\n---\n")
	if linkedGuidance {
		kb.write("AGENTS.md", "# Repository guidance\n")
		if err := os.Symlink("AGENTS.md", filepath.Join(kb.root, "CLAUDE.md")); err != nil {
			t.Skipf("guidance symlink unavailable: %v", err)
		}
	}
	store, err := repositorytrust.DefaultStore()
	require.NoError(t, err)
	_, err = store.Trust(kb.root)
	require.NoError(t, err)
	t.Cleanup(func() { _, _, _ = store.Untrust(kb.root) })
	warmNode(t)
	base := kb.serve()
	output, err := kb.rzm("init", "--add-addon", "ao-kb", "--agents", "none")
	require.NoError(t, err, output)
	kb.authored = kb.files()
	if populated {
		delete(kb.authored, ".rhizome/source-ownership.yml")
	}
	waitFor := func(state modulereadiness.State) modulereadiness.Snapshot {
		t.Helper()
		type observation struct {
			states   []modulereadiness.Snapshot
			response string
		}
		var latest atomic.Pointer[observation]
		latest.Store(&observation{})
		ready := assert.Eventually(t, func() bool {
			response, err := http.Get(base + "/api/v1/starter-readiness")
			if err != nil {
				latest.Store(&observation{response: err.Error()})
				return false
			}
			defer response.Body.Close()
			data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
			current := &observation{response: fmt.Sprintf("HTTP %d: %s", response.StatusCode, data)}
			valid := err == nil && json.Unmarshal(data, &current.states) == nil
			latest.Store(current)
			if !valid {
				return false
			}
			return len(current.states) == 1 && current.states[0].State == state
		}, 45*time.Second, 100*time.Millisecond)
		if !ready {
			for _, file := range kb.serveLogs {
				data, err := os.ReadFile(file)
				t.Logf("runtime log %s (%v):\n%s", file, err, data)
			}
			current := latest.Load()
			t.Fatalf("waiting for %s; readiness: %+v; last response: %s", state, current.states, current.response)
		}
		return latest.Load().states[0]
	}
	waitFor(modulereadiness.Ready)
	kb.requireUnchanged()
	if populated {
		require.NoFileExists(t, filepath.Join(kb.root, ".rhizome/source-ownership.yml"))
	}
	if linkedGuidance {
		target, err := os.Readlink(filepath.Join(kb.root, "CLAUDE.md"))
		require.NoError(t, err)
		require.Equal(t, "AGENTS.md", target)
	}
	selection := filepath.Join(kb.root, ".rhizome/modules/ao-kb/bindings/selection.json")
	data, err := os.ReadFile(selection)
	require.NoError(t, err)
	require.Contains(t, string(data), `"product": null`)
	receipt := filepath.Join(kb.root, ".rhizome/modules/ao-kb/bindings/index-receipt.json")
	info, err := os.Stat(receipt)
	require.NoError(t, err)
	fixture.StopRuntime(t, kb.root)
	base = kb.serve()
	waitFor(modulereadiness.Ready)
	afterReopen, err := os.Stat(receipt)
	require.NoError(t, err)
	require.Equal(t, info.ModTime(), afterReopen.ModTime(), "reopen observes current queries without indexing again")
	// An unrelated file never triggers setup or rewrites the verified receipt.
	kb.write("unrelated.txt", "unchanged inputs")
	require.Never(t, func() bool { after, err := os.Stat(receipt); return err != nil || after.ModTime() != info.ModTime() }, 3*time.Second, 100*time.Millisecond)
	if populated {
		for _, ignore := range []string{".rhizome/ignore", ".obsidianignore", ".gitignore"} {
			kb.write(ignore, "assertions/**\n")
			waitFor(modulereadiness.Failed)
			require.NoError(t, os.Remove(filepath.Join(kb.root, ignore)))
			waitFor(modulereadiness.Ready)
		}
		for _, guidance := range []string{"AGENTS.md", "CLAUDE.md"} {
			kb.write(guidance, "<!-- BEGIN RZM INIT RHIZOME BLOCK -->\nbroken fence\n")
			failure := waitFor(modulereadiness.Failed)
			require.Contains(t, failure.Reason, "malformed managed block in "+guidance)
			kb.write(guidance, "# Repaired guidance\n")
			waitFor(modulereadiness.Ready)
		}
	}
}
