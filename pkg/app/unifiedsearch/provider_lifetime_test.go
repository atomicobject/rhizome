package unifiedsearch

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

// Real cache owners have a database/sql connection-opener goroutine even while
// idle. Count those owners, not total goroutines or open OS file descriptors.
func databaseOwners() int {
	stack := make([]byte, 2<<20)
	n := runtime.Stack(stack, true)
	return strings.Count(string(stack[:n]), "database/sql.(*DB).connectionOpener(")
}

func requireDatabaseOwners(t *testing.T, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return databaseOwners() == want }, 3*time.Second, 10*time.Millisecond)
}

// The opener count is process-wide, and another test's DB.Close may still be
// finishing when a baseline is captured. Give each ownership workload a fresh
// process so those unrelated exits cannot either fail or mask its leak check.
func providerLifetimeSubprocess(t *testing.T) bool {
	t.Helper()
	const childCase = "RHIZOME_PROVIDER_LIFETIME_CASE"
	if os.Getenv(childCase) == t.Name() {
		requireDatabaseOwners(t, 0)
		return false
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	t.Setenv(childCase, t.Name())
	path := strings.Split(t.Name(), "/")
	for i := range path {
		path[i] = "^" + regexp.QuoteMeta(path[i]) + "$"
	}
	args := []string{"-test.run=" + strings.Join(path, "/"), "-test.v"}
	if coverDir := flag.Lookup("test.gocoverdir"); coverDir != nil && coverDir.Value.String() != "" {
		args = append(args, "-test.gocoverdir="+coverDir.Value.String())
	}
	if deadline, ok := t.Deadline(); ok {
		args = append(args, "-test.timeout="+time.Until(deadline).String())
	}
	cmd := exec.CommandContext(t.Context(), executable, args...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "provider-lifetime subprocess failed:\n%s", output)
	t.Logf("%s", output)
	return true
}

func TestRunClosesCachedProvidersAfterSetupFailure(t *testing.T) {
	for _, codeOnly := range []bool{false, true} {
		name := "note index"
		if codeOnly {
			name = "code index"
		}
		t.Run(name, func(t *testing.T) {
			if providerLifetimeSubprocess(t) {
				return
			}
			root := t.TempDir()
			t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
			cfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: "http://127.0.0.1:1", IndexPath: t.TempDir()}
			if codeOnly {
				require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
				require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome", "db.sqlite"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "config.yml"), []byte("codeEmbeddings:\n  enabled: true\n  provider: openai\n  model: synthetic\n  dimensions: 4\n  endpoint: http://127.0.0.1:1\n"), 0o600))
				cfg.Enabled = false
			}
			before := databaseOwners()
			for range 8 {
				_, err := Run(t.Context(), Options{Query: "synthetic", UseVector: true, VaultPath: root, EmbCfg: cfg, ProviderAPIKey: "synthetic-only"})
				require.Error(t, err)
			}
			runtime.GC()
			requireDatabaseOwners(t, before)
		})
	}
}

func TestRunCleanupClosesSharedCachedProvider(t *testing.T) {
	if providerLifetimeSubprocess(t) {
		return
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		data := make([]map[string]any, len(request.Input))
		for i := range data {
			data[i] = map[string]any{"index": i, "embedding": []float32{1, 0, 0, 0}}
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": data}))
	}))
	defer endpoint.Close()
	root := t.TempDir()
	t.Setenv("RHIZOME_EMBEDDING_CACHE", filepath.Join(root, "cache.sqlite"))
	cfg := embeddings.Config{Enabled: true, Provider: "openai", Model: "synthetic", Dimensions: 4, Endpoint: endpoint.URL, IndexPath: filepath.Join(root, ".rhizome", "db.sqlite")}
	require.NoError(t, os.Mkdir(filepath.Join(root, ".rhizome"), 0o755))
	require.NoError(t, obsidian.SaveLocalConfig(root, obsidian.LocalConfig{NoteEmbeddings: &cfg, CodeEmbeddings: &cfg}))
	before := databaseOwners()
	for range 3 {
		result, err := Run(t.Context(), Options{Query: "synthetic", UseVector: true, VaultPath: root, EmbCfg: cfg, ProviderAPIKey: "synthetic-only"})
		require.NoError(t, err)
		require.NotNil(t, result.Cleanup)
		t.Cleanup(result.Cleanup)
		// One provider cache, note/code embedding stores, and an Intel reader.
		// A discarded equivalent provider would add a fifth database owner.
		requireDatabaseOwners(t, before+4)
		result.Cleanup()
		result.Cleanup()
		requireDatabaseOwners(t, before)
	}
}

func TestRunCleanupBorrowsInjectedCachedProviders(t *testing.T) {
	cfg := embeddings.ProviderConfig{Provider: "test", Model: "synthetic", Dimensions: 4}
	provider, err := embeddings.NewCachedProvider(embeddings.NewDeterministicProvider(cfg), cfg, filepath.Join(t.TempDir(), "cache.sqlite"))
	require.NoError(t, err)
	closer := provider.(io.Closer)
	defer func() { require.NoError(t, closer.Close()) }()
	root := t.TempDir()
	store, err := semdb.Open(filepath.Join(root, "intel.sqlite"))
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	result, err := Run(context.Background(), Options{Query: "synthetic", UseVector: true, UseIntel: true, VaultPath: root, IntelStore: store, NoteProvider: provider, NoteProviderConfig: cfg, CodeProvider: provider, CodeProviderConfig: cfg})
	require.NoError(t, err)
	result.Cleanup()
	_, err = provider.EmbedTexts(t.Context(), []string{"still borrowed"})
	require.NoError(t, err)
	require.NoError(t, closer.Close())
	_, err = provider.EmbedTexts(t.Context(), []string{"explicit close"})
	require.ErrorContains(t, err, "database is closed")
}
