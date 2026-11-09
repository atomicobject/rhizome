//go:build integration

package aokb

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/tests/integration/internal/fixture"
	"github.com/stretchr/testify/require"
)

// SPEC-0135 US6 and R2b: a single-product schemaVersion 5 KB completes setup through the
// knowledge-only route with the real binary and no uv on PATH, the exported orientation
// query reads every record, and no KB file changes.
func TestAOKBLegacyKnowledgeOnlySetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the PATH without uv is built from symlinks")
	}
	node, err := exec.LookPath("node")
	require.NoError(t, err, "the AO KB binding compiler needs Node")
	repo := fixture.RepoRoot(t)
	kb := &aoKB{t: t, root: filepath.Join(t.TempDir(), "vault"), home: t.TempDir()}
	require.NoError(t, os.CopyFS(kb.root, os.DirFS(filepath.Join(repo, "testdata/ao-kb-legacy"))))
	kb.write(".rhizome/config.yml", "notes:\n  includes:\n    - \"**/*.md\"\n    - \".kb/curation/*.md\"\n")
	output, err := kb.rzm("init", "--add-addon", "ao-kb", "--agents", "none")
	require.NoError(t, err, output)
	kb.authored = kb.files()

	// PATH holds node and rzm only, so uv is absent.
	bin := t.TempDir()
	require.NoError(t, os.Symlink(node, filepath.Join(bin, "node")))
	require.NoError(t, os.Symlink(rzmBinary(t), filepath.Join(bin, "rzm")))
	env := []string{}
	for _, value := range kb.env() {
		if !strings.HasPrefix(value, "PATH=") && !strings.HasPrefix(value, "RZM=") {
			env = append(env, value)
		}
	}
	env = append(env, "PATH="+bin)
	t.Cleanup(func() { fixture.StopRuntime(t, kb.root) })
	run := func(args ...string) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, filepath.Join(bin, "node"), args...)
		command.Dir, command.Env, command.Stdin = kb.root, env, strings.NewReader("{}")
		out, _ := command.Output()
		var report map[string]any
		require.NoError(t, json.Unmarshal(out, &report), "%s: %s", strings.Join(args, " "), out)
		return report
	}
	const compiler, check = ".rhizome/modules/ao-kb/bindings/cli.mjs", ".rhizome/modules/ao-kb/scripts/check.mjs"
	model := func(report map[string]any) string {
		return report["bindings"].(map[string]any)["model"].(map[string]any)["status"].(string)
	}

	refreshed := run(compiler, "refresh-bindings", "--single")
	require.Equal(t, true, refreshed["bindingsReady"], refreshed)
	require.Equal(t, false, refreshed["modelReady"])
	require.Equal(t, "not applicable", model(refreshed))
	require.Empty(t, refreshed["dependencies"], "uv is not looked up")

	report := run(check)
	require.Equal(t, true, report["bindingsReady"], report)
	require.Equal(t, false, report["indexReady"])
	rows := map[string]string{}
	for _, row := range report["checks"].([]any) {
		row := row.(map[string]any)
		rows[row["name"].(string)] = row["status"].(string)
	}
	require.Equal(t, "ok", rows["schemaVersion"])
	require.Equal(t, "ok", rows["note includes"])
	for _, name := range []string{"model compiler dependencies", "model bindings", "model source ownership"} {
		require.Equal(t, "not applicable", rows[name], name)
	}

	indexed := run(compiler, "index")
	require.Equal(t, true, indexed["indexReady"], indexed)
	report = run(check)
	require.Equal(t, true, report["indexReady"], report)

	// The exported orientation query, run by the real binary, reads every record completely.
	module := filepath.Join(kb.root, ".rhizome/modules/ao-kb/views/knowledge/lib/queries.ts")
	text, err := exec.Command(node, "--experimental-strip-types", "--input-type=module", "-e",
		"process.stdout.write((await import(process.argv[1])).ORIENTATION_QUERY)", module).Output()
	require.NoError(t, err)
	output, err = kb.rzm("ontology", "query", "--query", string(text))
	require.NoError(t, err, output)
	var result struct {
		Data       map[string][]map[string]any
		Extensions struct {
			TypedRoots map[string]struct{ Status string }
		}
	}
	require.NoError(t, json.Unmarshal([]byte(output), &result), output)
	for root, count := range map[string]int{"assertion": 43, "source": 7, "concept": 4, "artifact": 5, "mapOfContent": 3} {
		require.Len(t, result.Data[root], count, root)
		require.Equal(t, "complete", result.Extensions.TypedRoots[root].Status, root)
	}
	kb.requireUnchanged()
}
